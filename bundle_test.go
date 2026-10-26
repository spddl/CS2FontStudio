package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useImportDir points the imported-fonts folder at a temp folder.
func useImportDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fonts")
	old := importedFontsDir
	importedFontsDir = func() string { return dir }
	rescanFonts()
	t.Cleanup(func() { importedFontsDir = old; rescanFonts() })
	return dir
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, b := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBundleRoundTrip(t *testing.T) {
	useProfileDirs(t)
	dir := useImportDir(t)
	rules := []Rule{
		{Group: "Stratum2", Font: "Arial", Scale: 1.1},
		{Group: "Stratum2 Mono", Font: "Arial"}, // same font: packed once
		{Group: "Stratum2 TF", Scale: 1},        // changes nothing: dropped
	}
	b, err := writeBundle("Comp HUD", rules, 1.2)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var cfg bundleConfig
	nFonts := 0
	for _, f := range zr.File {
		if f.Name == bundleConfigName {
			data, _ := readZipFile(f, 1<<20)
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatal(err)
			}
		} else if strings.HasPrefix(f.Name, "fonts/") && isFontFile(f.Name) {
			nFonts++
		}
	}
	if len(cfg.Rules) != 2 || cfg.TextScale != 1.2 || cfg.Name != "Comp HUD" {
		t.Fatalf("config: %+v", cfg.profileFile)
	}
	if nFonts == 0 || nFonts != len(cfg.Fonts) {
		t.Fatalf("%d font files, %d listed", nFonts, len(cfg.Fonts))
	}

	// Arial is installed here, so the import copies nothing but still saves the profile
	res, err := importBundle(b, "ignored.zip")
	if err != nil {
		t.Fatal(err)
	}
	if res.Profile.Name != "Comp HUD" || len(res.Rules) != 2 || len(res.Installed) != 0 || len(res.Missing) != 0 {
		t.Fatalf("result: %+v", res)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("fonts copied although installed: %v", entries)
	}
	// a second import does not overwrite the first profile
	res2, err := importBundle(b, "")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Profile.Name != "Comp HUD (2)" {
		t.Errorf("second import named %q", res2.Profile.Name)
	}
}

func TestBundleInstallsMissingFont(t *testing.T) {
	useProfileDirs(t)
	dir := useImportDir(t)
	b, err := writeBundle("Imp", []Rule{{Group: "Stratum2", Font: "Arial"}, {Group: "Stratum2 TF", Font: "Not On This PC"}}, 0)
	if err == nil {
		t.Fatal("bundle with a missing font was written")
	}
	b, err = writeBundle("Imp", []Rule{{Group: "Stratum2", Font: "Arial"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// add a rule for a font the bundle does not carry, plus junk and a path that tries to leave the folder
	zr, _ := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	files := map[string][]byte{}
	for _, f := range zr.File {
		files["x/"+f.Name], _ = readZipFile(f, maxFontFileSize)
	}
	var cfg bundleConfig
	_ = json.Unmarshal(files["x/config.json"], &cfg)
	cfg.Rules = append(cfg.Rules, Rule{Group: "Stratum2 TF", Font: "Not On This PC"})
	files["x/config.json"], _ = json.Marshal(cfg)
	files["x/fonts/../../evil.ttf"] = files["x/"+cfg.Fonts[0].File]
	files["x/fonts/notafont.ttf"] = []byte("hello")

	// the other PC: no Windows fonts at all, only the app's import folder
	oldDirs := fontDirs
	fontDirs = func() []string {
		return []string{filepath.Join(t.TempDir(), "win"), filepath.Join(t.TempDir(), "local"), dir}
	}
	rescanFonts()
	t.Cleanup(func() { fontDirs = oldDirs; rescanFonts() })

	res, err := importBundle(zipOf(t, files), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Installed) != 1 || res.Installed[0] != "Arial" {
		t.Errorf("installed: %v", res.Installed)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "Not On This PC" {
		t.Errorf("missing: %v", res.Missing)
	}
	if fileExists(filepath.Join(filepath.Dir(dir), "evil.ttf")) {
		t.Error("file written outside the fonts folder")
	}
	if fileExists(filepath.Join(dir, "notafont.ttf")) {
		t.Error("a file that is no font was written")
	}
	if len(filesOfFamily("Arial")) == 0 {
		t.Fatal("imported font not found after the import")
	}
	// the imported font can be applied to the game like an installed one
	root := fakeRoot(t)
	if err := apply(root, res.Rules[:1], 0); err != nil {
		t.Fatal(err)
	}
	// importing again writes no second copy
	before, _ := os.ReadDir(dir)
	if _, err := importBundle(zipOf(t, files), ""); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadDir(dir); len(after) != len(before) {
		t.Errorf("second import: %d files, before %d", len(after), len(before))
	}
}

func TestImportBundleRejects(t *testing.T) {
	useProfileDirs(t)
	useImportDir(t)
	for name, b := range map[string][]byte{
		"not a zip":   []byte("PK nope"),
		"no config":   zipOf(t, map[string][]byte{"fonts/a.ttf": []byte("x")}),
		"bad config":  zipOf(t, map[string][]byte{"config.json": []byte(`{"path":"x"}`)}),
		"empty rules": zipOf(t, map[string][]byte{"config.json": []byte(`{"rules":[]}`)}),
	} {
		if _, err := importBundle(b, ""); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"fonts/a.ttf":         "a.ttf",
		`..\..\Windows\b.ttf`: "b.ttf",
		"fonts/../../c.ttf":   "c.ttf",
		`fonts/we:ird?.otf`:   "we_ird_.otf",
	} {
		if got := safeFileName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
