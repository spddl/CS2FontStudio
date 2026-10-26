package main

import (
	"os"
	"path/filepath"
	"testing"
)

// useProfileDirs points the profile lookup at temp folders: exe folder, profiles folder, user folder.
func useProfileDirs(t *testing.T) (exe, prof, user string) {
	t.Helper()
	base := t.TempDir()
	exe, prof, user = base, filepath.Join(base, "profiles"), filepath.Join(t.TempDir(), "appdata")
	old := profileDirs
	profileDirs = func() []profileDir {
		return []profileDir{{"profiles", prof}, {"exe", exe}, {"user", user}}
	}
	t.Cleanup(func() { profileDirs = old })
	return
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProfilesFoundNextToExe(t *testing.T) {
	exe, prof, _ := useProfileDirs(t)
	write(t, filepath.Join(exe, "shared.json"), `{"app":"CS2FontConfigurator","rules":[{"group":"Stratum2","font":"Tahoma","scale":1}]}`)
	write(t, filepath.Join(exe, "bare.JSON"), `[{"group":"Stratum2 Mono","font":"Consolas"}]`)
	write(t, filepath.Join(exe, "config.json"), `{"path":"F:\\x","fonts":[]}`)    // another tool's config: ignored
	write(t, filepath.Join(exe, "broken.json"), `{not json`)                      // ignored
	write(t, filepath.Join(exe, "nogroup.json"), `{"rules":[{"font":"Tahoma"}]}`) // rule without group: ignored
	write(t, filepath.Join(prof, "mine.json"), `{"rules":[{"group":"Stratum2 TF","scale":1.2}]}`)
	write(t, filepath.Join(exe, "notes.txt"), `[{"group":"x"}]`)

	got := map[string]string{}
	for _, p := range scanProfiles() {
		got[p.Name] = p.Where
	}
	want := map[string]string{"shared": "exe", "bare": "exe", "mine": "profiles"}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for n, w := range want {
		if got[n] != w {
			t.Errorf("%s: where=%q, want %q", n, got[n], w)
		}
	}
}

func TestSaveLoadDeleteProfile(t *testing.T) {
	exe, prof, _ := useProfileDirs(t)
	rules := []Rule{
		{Group: "Stratum2", Font: "Tahoma", Scale: 1},
		{Group: "Stratum2 Mono", Scale: 1}, // trivial: not saved
	}
	info, err := saveProfile("My look", rules, 0)
	if err != nil {
		t.Fatal(err)
	}
	if info.Where != "profiles" || info.Rules != 1 || info.Key != "profiles:My look.json" {
		t.Fatalf("unexpected info %+v", info)
	}
	if !fileExists(filepath.Join(prof, "My look.json")) {
		t.Fatal("profile file was not written to the profiles folder")
	}
	e, ok := findProfile(info.Key)
	if !ok {
		t.Fatal("saved profile not found")
	}
	p, ok := readProfile(e.Path)
	if !ok || len(p.Rules) != 1 || p.Rules[0].Font != "Tahoma" {
		t.Fatalf("roundtrip failed: %+v", p)
	}
	if err := deleteProfile(info.Key); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(prof, "My look.json")) {
		t.Error("profile was not deleted")
	}

	// loose files next to the exe are read-only for the app
	write(t, filepath.Join(exe, "loose.json"), `[{"group":"Stratum2","font":"Tahoma"}]`)
	if err := deleteProfile("exe:loose.json"); err == nil {
		t.Error("deleting a file next to the exe must be refused")
	}
	if !fileExists(filepath.Join(exe, "loose.json")) {
		t.Error("the loose file must still exist")
	}
}

func TestSaveFallsBackToUserFolder(t *testing.T) {
	exe, _, user := useProfileDirs(t)
	// the profiles folder cannot be created: a file occupies its name
	write(t, filepath.Join(exe, "profiles"), "not a folder")
	info, err := saveProfile("x", []Rule{{Group: "Stratum2", Font: "Tahoma"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if info.Where != "user" || !fileExists(filepath.Join(user, "x.json")) {
		t.Fatalf("expected the app data fallback, got %+v", info)
	}
}

func TestProfileFileNameRejectsUnsafeNames(t *testing.T) {
	for _, bad := range []string{"", "  ", `..\evil`, "a/b", "a:b", "con", "x.", `a"b`, "a\x01b"} {
		if _, err := profileFileName(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if f, err := profileFileName(" Competitive HUD.json "); err != nil || f != "Competitive HUD.json" {
		t.Errorf("got %q, %v", f, err)
	}
}

func TestSaveRefusesEmptyAndUnknownKey(t *testing.T) {
	useProfileDirs(t)
	if _, err := saveProfile("empty", []Rule{{Group: "Stratum2", Scale: 1}}, 0); err == nil {
		t.Error("saving only trivial rules must fail")
	}
	if _, ok := findProfile(`profiles:..\..\Windows\win.ini`); ok {
		t.Error("a key that was not found on disk must never resolve")
	}
}

func TestProfileWithTextSizeOnly(t *testing.T) {
	_, prof, _ := useProfileDirs(t)
	info, err := saveProfile("big text", nil, 1.25)
	if err != nil {
		t.Fatalf("a text size alone is a valid configuration: %v", err)
	}
	e, ok := findProfile(info.Key)
	if !ok {
		t.Fatal("not found")
	}
	p, ok := readProfile(e.Path)
	if !ok || p.TextScale != 1.25 || len(p.Rules) != 0 {
		t.Fatalf("roundtrip failed: %+v", p)
	}
	if !fileExists(filepath.Join(prof, "big text.json")) {
		t.Fatal("file missing")
	}
	if _, err := saveProfile("same", nil, 1); err == nil {
		t.Error("text size 1 without rules is nothing to save")
	}
	if _, err := saveProfile("bad", nil, 9); err == nil {
		t.Error("an out-of-range text size must be refused")
	}
	// profiles written by the first version of this feature used a "dpi" field that the game never honoured: not a configuration
	write(t, filepath.Join(prof, "legacy.json"), `{"app":"CS2FontConfigurator","dpi":168}`)
	for _, pi := range scanProfiles() {
		if pi.Name == "legacy" {
			t.Error("a file with only the old dpi field must not be listed")
		}
	}
}
