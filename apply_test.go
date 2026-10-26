package main

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fakeRoot(t *testing.T) string {
	root := t.TempDir()
	for _, d := range []string{
		filepath.Join("game", "csgo", "panorama", "fonts"),
		filepath.Join("game", "core", "panorama", "fonts", "conf.d"),
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestApplyAndReset(t *testing.T) {
	root := fakeRoot(t)
	rules := []Rule{
		{Group: "Stratum2 Mono", Font: "Impact", Scale: 0.9},
		{Group: "Stratum2 Condensed", Scale: 1.2}, // size only
		{Group: "Stratum2", Font: "", Scale: 1},   // ignored
	}
	if err := apply(root, rules, 0); err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile(confFile(root))
	if err != nil {
		t.Fatal(err)
	}
	s := string(conf)
	for _, want := range []string{"Stratum2 Mono", "Impact", "<double>0.9</double>", "<double>1.2</double>", "<!-- state: "} {
		if !strings.Contains(s, want) {
			t.Errorf("conf lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "<string>Stratum2</string>") {
		t.Error("empty rule written")
	}
	st, ok := readState(root)
	if !ok || len(st.Rules) != 2 || len(st.Copied) == 0 {
		t.Fatalf("state: %+v ok=%v", st, ok)
	}
	for _, n := range st.Copied {
		if _, err := os.Stat(filepath.Join(gameFontsDir(root), n)); err != nil {
			t.Errorf("font not copied: %v", err)
		}
	}
	// applying again removes the old copies
	if err := apply(root, []Rule{{Group: "Stratum2 Mono", Font: "Arial"}}, 0); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(gameFontsDir(root))
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name()), "impact") {
			t.Error("old font not removed")
		}
	}
	if err := reset(root); err != nil {
		t.Fatal(err)
	}
	if fileExists(confFile(root)) {
		t.Error("conf not deleted")
	}
	entries, _ = os.ReadDir(gameFontsDir(root))
	if len(entries) != 0 {
		t.Errorf("fonts not deleted: %v", entries)
	}
}

func TestGameFontIsKept(t *testing.T) {
	root := fakeRoot(t)
	game := filepath.Join(gameFontsDir(root), "notosans-Regular.ttf")
	if err := os.WriteFile(game, []byte("game"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := apply(root, []Rule{{Group: "X", Font: "Arial"}}, 0); err != nil {
		t.Fatal(err)
	}
	if err := reset(root); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(game); err != nil || string(b) != "game" {
		t.Errorf("the game's file was changed or deleted: %q %v", b, err)
	}
}

func TestUnusedFontsAreRemoved(t *testing.T) {
	root := fakeRoot(t)
	dir := gameFontsDir(root)
	for _, n := range []string{"notosans-arial.ttf", "notosans-BebasNeue-Regular.ttf", "notosans-blank.otf", "stratum2.uifont", "fonts.conf"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := apply(root, []Rule{{Group: "X", Font: "Arial"}}, 0); err != nil {
		t.Fatal(err)
	}
	st, _ := readState(root)
	if !slices.Contains(st.Copied, "notosans-arial.ttf") {
		t.Errorf("copied: %v", st.Copied) // the stale copy is gone, so the name is free again
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notosans-arial.ttf")); string(b) == "x" {
		t.Error("stale copy not replaced")
	}
	for n, want := range map[string]bool{"notosans-BebasNeue-Regular.ttf": false, "notosans-blank.otf": false, "stratum2.uifont": true, "fonts.conf": true} {
		if fileExists(filepath.Join(dir, n)) != want {
			t.Errorf("%s: exists=%v, want %v", n, !want, want)
		}
	}
}

func TestOldStateIsCleanedUp(t *testing.T) {
	root := fakeRoot(t)
	old := filepath.Join(gameFontsDir(root), "notosans-csfc-arial.ttf")
	conf := `<fontconfig><!-- csfc-state: {"rules":[{"group":"X","font":"Arial"}],"copied":["notosans-csfc-arial.ttf"]} --></fontconfig>`
	if err := os.WriteFile(old, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(confFile(root), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, ok := readState(root); !ok || len(st.Rules) != 1 {
		t.Fatalf("old state not read: %+v %v", st, ok)
	}
	if err := apply(root, []Rule{{Group: "X", Font: "Arial"}}, 0); err != nil {
		t.Fatal(err)
	}
	if fileExists(old) {
		t.Error("copy from the old version not removed")
	}
}

func TestForeignConfIsOverwritten(t *testing.T) {
	root := fakeRoot(t)
	if err := os.WriteFile(confFile(root), []byte("<fontconfig/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := apply(root, []Rule{{Group: "X", Font: "Arial"}}, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := readState(root); !ok {
		t.Error("file was not overwritten")
	}
	if err := os.WriteFile(confFile(root), []byte("<fontconfig/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reset(root); err != nil || fileExists(confFile(root)) {
		t.Errorf("foreign file not deleted: %v", err)
	}
}

func TestFontScan(t *testing.T) {
	fams := familyList()
	if len(fams) < 20 {
		t.Fatalf("too few fonts: %d", len(fams))
	}
	t.Logf("%d families, e.g. %v", len(fams), fams[:5])
}

func TestStyleRules(t *testing.T) {
	root := fakeRoot(t)
	rules := []Rule{
		{Group: "Stratum2 Mono", Font: "Impact", Weight: 700, Italic: true, Width: 0.8},
		{Group: "Stratum2", Weight: 300},
		{Group: "Stratum2 Condensed", Width: 1},
	}
	if err := apply(root, rules, 0); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(confFile(root))
	s := string(b)
	for _, want := range []string{"<int>200</int>", "<const>italic</const>", "<double>0.8</double>", "<double>0.2</double>", "<int>50</int>"} {
		if !strings.Contains(s, want) {
			t.Errorf("conf lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Stratum2 Condensed") {
		t.Error("trivial rule written")
	}
	if strings.Contains(s, "embolden") {
		t.Error("the game ignores embolden, it must not be written")
	}
	if err := apply(root, nil, 0); err != nil {
		t.Fatal(err)
	}
}

func TestTextScaleRule(t *testing.T) {
	root := fakeRoot(t)
	const rule = `<match target="font"><edit name="pixelsize" mode="assign"><times><name>pixelsize</name><double>1.25</double></times></edit></match>`
	// a text size alone is a valid change, even without any group rule
	if err := apply(root, nil, 1.25); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(confFile(root))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, rule) || !strings.Contains(s, "<!-- state: ") {
		t.Errorf("conf lacks the global pixelsize rule:\n%s", s)
	}
	if strings.Contains(s, `name="dpi"`) {
		t.Error("the game does not read fontconfig's dpi, it must not be written")
	}
	if st, ok := readState(root); !ok || st.TextScale != 1.25 || len(st.Rules) != 0 {
		t.Errorf("state not read back: %+v %v", st, ok)
	}
	// together with group rules, both are written
	if err := apply(root, []Rule{{Group: "Stratum2", Scale: 1.2}}, 2); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(confFile(root))
	if !strings.Contains(string(b), "<double>2</double>") || !strings.Contains(string(b), "<double>1.2</double>") {
		t.Errorf("rule and text size expected:\n%s", b)
	}
	// 1 or 0 means "unchanged": with no rules nothing is written and the file goes away
	for _, x := range []float64{1, 0} {
		if err := apply(root, nil, 1.25); err != nil {
			t.Fatal(err)
		}
		if err := apply(root, nil, x); err != nil {
			t.Fatal(err)
		}
		if fileExists(confFile(root)) {
			t.Errorf("text size %v and no rules must remove the conf file", x)
		}
	}
	// 1 together with a rule: only the rule is written
	if err := apply(root, []Rule{{Group: "Stratum2", Scale: 1.2}}, 1); err != nil {
		t.Fatal(err)
	}
	if b, _ = os.ReadFile(confFile(root)); strings.Count(string(b), "pixelsize") != 2 { // the group rule mentions it twice (edit name and <name>)
		t.Errorf("only the group rule may touch pixelsize:\n%s", b)
	}
}

func TestNormalizeTextScale(t *testing.T) {
	for _, bad := range []float64{0.1, 0.49, 2.01, 10, -1, math.NaN(), math.Inf(1)} {
		if _, err := normalizeTextScale(bad); err == nil {
			t.Errorf("%v must be rejected", bad)
		}
	}
	for in, want := range map[float64]float64{0: 0, 1: 0, 0.5: 0.5, 1.25: 1.25, 2: 2} {
		if got, err := normalizeTextScale(in); err != nil || got != want {
			t.Errorf("normalizeTextScale(%v) = %v, %v; want %v", in, got, err, want)
		}
	}
	root := fakeRoot(t)
	if err := apply(root, nil, 5); err == nil {
		t.Error("apply must refuse an out-of-range text size")
	}
}
