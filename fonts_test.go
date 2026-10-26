package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestWriteBuiltinFonts(t *testing.T) {
	dir := t.TempDir()
	old := builtinFontsDir
	builtinFontsDir = func() string { return dir }
	t.Cleanup(func() { builtinFontsDir = old; rescanFonts() })

	fsys := fstest.MapFS{
		"fonts/A-Regular.ttf":     {Data: []byte("a")},
		"fonts/preview-fonts.css": {Data: []byte("css")},
	}
	if err := writeBuiltinFonts(fsys, "fonts"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "A-Regular.ttf")); err != nil || string(b) != "a" {
		t.Fatalf("font not written: %q %v", b, err)
	}
	if fileExists(filepath.Join(dir, "preview-fonts.css")) {
		t.Error("only font files belong there")
	}
	// a newer exe replaces an outdated copy
	fsys["fonts/A-Regular.ttf"] = &fstest.MapFile{Data: []byte("b")}
	if err := writeBuiltinFonts(fsys, "fonts"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "A-Regular.ttf")); string(b) != "b" {
		t.Errorf("outdated copy kept: %q", b)
	}
}
