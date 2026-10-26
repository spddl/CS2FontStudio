package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

type fontFile struct {
	Family string
	Style  string
	Dir    string
	File   string
}

// styleWeight derives CSS weight and slant from the style name (for the browser preview).
func styleWeight(style, file string) (weight int, italic bool) {
	s := strings.ToLower(style + " " + file)
	italic = strings.Contains(s, "italic") || strings.Contains(s, "oblique")
	switch {
	case strings.Contains(s, "variable"):
		return 0, italic // 0 = variable range
	case strings.Contains(s, "black"), strings.Contains(s, "heavy"), strings.Contains(s, "ultra"):
		return 900, italic
	case strings.Contains(s, "extrabold"), strings.Contains(s, "extra bold"):
		return 800, italic
	case strings.Contains(s, "semibold"), strings.Contains(s, "semi bold"), strings.Contains(s, "demi"):
		return 600, italic
	case strings.Contains(s, "bold"):
		return 700, italic
	case strings.Contains(s, "medium"):
		return 500, italic
	case strings.Contains(s, "extralight"), strings.Contains(s, "thin"), strings.Contains(s, "hairline"):
		return 200, italic
	case strings.Contains(s, "light"):
		return 300, italic
	}
	return 400, italic
}

var (
	fontsMu   sync.Mutex
	fontsDone bool
	fontFiles []fontFile
)

// importedFontsDir holds fonts that came with an imported bundle. They are not installed in Windows,
// only this app (and, after "Apply", the game) uses them. Replaced in tests.
var importedFontsDir = func() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "CS2FontStudio", "fonts")
}

// builtinFontsDir holds the preview's stand-in fonts (web/fonts, embedded in the exe), written out at start so they can be
// chosen and applied like installed fonts ("Preview fonts" preset). Replaced in tests.
var builtinFontsDir = func() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "CS2FontStudio", "builtin")
}

// fontDirs: the order is part of the /api/fontfile URLs (index d), so new folders go at the end. Replaced in tests.
var fontDirs = func() []string {
	return []string{
		filepath.Join(os.Getenv("WINDIR"), "Fonts"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Fonts"),
		importedFontsDir(),
		builtinFontsDir(),
	}
}

// writeBuiltinFonts copies the font files of dir in fsys to builtinFontsDir, leaving files that are already up to date alone.
func writeBuiltinFonts(fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return err
	}
	out := builtinFontsDir()
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".ttf", ".otf":
		default:
			continue
		}
		data, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return err
		}
		dst := filepath.Join(out, e.Name())
		if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, data) {
			continue
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	rescanFonts()
	return nil
}

// rescanFonts lets the next scanFonts read the folders again (after fonts were imported).
func rescanFonts() {
	fontsMu.Lock()
	fontsDone = false
	fontsMu.Unlock()
}

// scanFonts reads the family names of all installed TTF/OTF/TTC files (once, until rescanFonts).
func scanFonts() []fontFile {
	fontsMu.Lock()
	defer fontsMu.Unlock()
	if !fontsDone {
		fontsDone = true
		fontFiles = nil
		type job struct{ dir, file string }
		jobs := make(chan job)
		var mu sync.Mutex
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					found := readFont(j.dir, j.file)
					mu.Lock()
					fontFiles = append(fontFiles, found...)
					mu.Unlock()
				}
			}()
		}
		for _, d := range fontDirs() {
			entries, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			for _, e := range entries {
				switch strings.ToLower(filepath.Ext(e.Name())) {
				case ".ttf", ".otf", ".ttc":
					jobs <- job{d, e.Name()}
				}
			}
		}
		close(jobs)
		wg.Wait()
	}
	return fontFiles
}

func readFont(dir, file string) []fontFile {
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return nil
	}
	out := parseFontData(data, file)
	for i := range out {
		out[i].Dir = dir
	}
	return out
}

// parseFontData reads family and style of every face in a TTF/OTF/TTC file (nil = not a font).
func parseFontData(data []byte, file string) []fontFile {
	var fonts []*sfnt.Font
	if strings.EqualFold(filepath.Ext(file), ".ttc") {
		col, err := opentype.ParseCollection(data)
		if err != nil {
			return nil
		}
		for i := 0; i < col.NumFonts(); i++ {
			if f, err := col.Font(i); err == nil {
				fonts = append(fonts, f)
			}
		}
	} else if f, err := opentype.Parse(data); err == nil {
		fonts = append(fonts, f)
	}
	var out []fontFile
	var buf sfnt.Buffer
	for _, f := range fonts {
		fam, err := f.Name(&buf, sfnt.NameIDFamily)
		if err != nil || fam == "" {
			continue
		}
		sub, _ := f.Name(&buf, sfnt.NameIDSubfamily)
		out = append(out, fontFile{Family: fam, Style: sub, File: file})
	}
	return out
}

// familyList: unique family names, sorted.
func familyList() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range scanFonts() {
		if !seen[f.Family] {
			seen[f.Family] = true
			out = append(out, f.Family)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// filesOfFamily: all files of a family (all styles), each file once.
func filesOfFamily(family string) []fontFile {
	seen := map[string]bool{}
	var out []fontFile
	for _, f := range scanFonts() {
		if strings.EqualFold(f.Family, family) && !seen[f.File] {
			seen[f.File] = true
			out = append(out, f)
		}
	}
	return out
}
