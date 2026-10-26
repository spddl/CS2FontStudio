package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Bundles move a configuration to another PC: one zip with config.json (a profile file, plus the list of
// fonts it brought along) and the font files under fonts/. Importing one copies the fonts that are not on
// the PC yet into importedFontsDir and saves the configuration as a profile.

const (
	bundleConfigName = "config.json"
	maxBundleSize    = 256 << 20 // whole zip, also the limit for the uncompressed contents
	maxBundleFiles   = 512
	maxFontFileSize  = 64 << 20 // large CJK fonts are around 20 MB
)

type bundleFont struct {
	Family string `json:"family"`
	File   string `json:"file"` // path inside the zip
}

type bundleConfig struct {
	profileFile
	Fonts []bundleFont `json:"fonts,omitempty"`
}

// writeBundle packs the configuration and every file of every font it uses (all styles).
func writeBundle(name string, rules []Rule, textScale float64) ([]byte, error) {
	p, _, err := newProfile(name, rules, textScale)
	if err != nil {
		return nil, err
	}
	cfg := bundleConfig{profileFile: p}
	now := time.Now()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	added := map[string]bool{}    // dir + file already in the zip
	usedName := map[string]bool{} // lower-case zip paths, two folders may hold files with the same name
	doneFamily := map[string]bool{}
	for _, r := range p.Rules {
		if r.Font == "" || doneFamily[strings.ToLower(r.Font)] {
			continue
		}
		doneFamily[strings.ToLower(r.Font)] = true
		files := filesOfFamily(r.Font)
		if len(files) == 0 {
			return nil, fmt.Errorf("font %q is not installed on this PC", r.Font)
		}
		for _, f := range files {
			src := filepath.Join(f.Dir, f.File)
			if added[strings.ToLower(src)] {
				continue
			}
			added[strings.ToLower(src)] = true
			data, err := os.ReadFile(src)
			if err != nil {
				return nil, err
			}
			zp := freeName("fonts/"+f.File, func(n string) bool { return usedName[strings.ToLower(n)] })
			usedName[strings.ToLower(zp)] = true
			w, err := zw.CreateHeader(&zip.FileHeader{Name: zp, Method: zip.Deflate, Modified: now})
			if err != nil {
				return nil, err
			}
			if _, err := w.Write(data); err != nil {
				return nil, err
			}
			cfg.Fonts = append(cfg.Fonts, bundleFont{Family: f.Family, File: zp})
		}
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	w, err := zw.CreateHeader(&zip.FileHeader{Name: bundleConfigName, Method: zip.Deflate, Modified: now})
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// freeName returns name, or name-2.ext, name-3.ext, ... until taken reports false.
func freeName(name string, taken func(string) bool) string {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i, n := 2, name; ; i++ {
		if !taken(n) {
			return n
		}
		n = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
}

type ImportResult struct {
	Profile   ProfileInfo `json:"profile"`
	Rules     []Rule      `json:"rules"`
	TextScale float64     `json:"textScale"`
	Installed []string    `json:"installed"` // families copied to this PC
	Present   []string    `json:"present"`   // families in the bundle that were already here
	Missing   []string    `json:"missing"`   // families the rules need but neither the PC nor the bundle has
}

func isFontFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".ttf", ".otf", ".ttc":
		return true
	}
	return false
}

// safeFileName keeps only the last path element and replaces characters Windows does not allow.
func safeFileName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	return strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
}

func readZipFile(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is too large", f.Name)
	}
	return b, nil
}

// importBundle installs the fonts of a bundle for this app and saves its configuration as a profile.
// fallbackName names the profile when config.json has no name (usually the zip's file name).
// Nothing from the zip is used as a path: fonts are written by their base name into importedFontsDir only.
func importBundle(data []byte, fallbackName string) (ImportResult, error) {
	var res ImportResult
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return res, errors.New("the file is not a zip archive")
	}
	if len(zr.File) > maxBundleFiles {
		return res, errors.New("the archive holds too many files")
	}
	var cfgFile *zip.File
	var fontEntries []*zip.File
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		total += f.UncompressedSize64
		if total > maxBundleSize {
			return res, errors.New("the archive is too large")
		}
		switch {
		case strings.EqualFold(path.Base(f.Name), bundleConfigName):
			// the one nearest to the top wins, so a re-zipped folder ("name/config.json") works too
			if cfgFile == nil || strings.Count(f.Name, "/") < strings.Count(cfgFile.Name, "/") {
				cfgFile = f
			}
		case isFontFile(f.Name):
			fontEntries = append(fontEntries, f)
		}
	}
	if cfgFile == nil {
		return res, errors.New("the archive has no config.json: it is not a CS2 Font Studio export")
	}
	b, err := readZipFile(cfgFile, 1<<20)
	if err != nil {
		return res, err
	}
	p, ok := parseProfile(b)
	if !ok {
		return res, errors.New("config.json in the archive is not a valid configuration")
	}

	have := map[string]bool{} // lower-case families on this PC
	for _, f := range scanFonts() {
		have[strings.ToLower(f.Family)] = true
	}
	dir := importedFontsDir()
	installed, present := map[string]bool{}, map[string]bool{}
	var read int
	for _, f := range fontEntries {
		data, err := readZipFile(f, maxFontFileSize)
		if err != nil {
			return res, err
		}
		if read += len(data); read > maxBundleSize { // the sizes in the zip headers are only claims
			return res, errors.New("the archive is too large")
		}
		file := safeFileName(f.Name)
		faces := parseFontData(data, file)
		if len(faces) == 0 {
			continue // not a font we can read: never written to disk
		}
		isNew := false
		for _, fc := range faces {
			if !have[strings.ToLower(fc.Family)] {
				isNew = true
			}
		}
		if !isNew {
			for _, fc := range faces {
				if !installed[fc.Family] {
					present[fc.Family] = true
				}
			}
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return res, err
		}
		// same name and same bytes: already there; same name, other bytes: keep both
		dst := freeName(file, func(n string) bool {
			old, err := os.ReadFile(filepath.Join(dir, n))
			return err == nil && !bytes.Equal(old, data)
		})
		if err := os.WriteFile(filepath.Join(dir, dst), data, 0o644); err != nil {
			return res, err
		}
		for _, fc := range faces {
			installed[fc.Family] = true
			delete(present, fc.Family)
		}
	}
	if len(installed) > 0 {
		rescanFonts()
	}
	for _, f := range scanFonts() {
		have[strings.ToLower(f.Family)] = true
	}
	missing := map[string]bool{}
	for _, r := range p.Rules {
		if r.Font != "" && !have[strings.ToLower(r.Font)] {
			missing[r.Font] = true
		}
	}

	name := strings.TrimSpace(p.Name)
	if _, err := profileFileName(name); err != nil {
		name = strings.TrimSpace(strings.TrimSuffix(fallbackName, path.Ext(fallbackName)))
	}
	if _, err := profileFileName(name); err != nil {
		name = "Imported"
	}
	info, err := saveProfile(freeProfileName(name), p.Rules, p.TextScale)
	if err != nil {
		return res, err
	}
	res = ImportResult{Profile: info, Rules: p.Rules, TextScale: p.TextScale,
		Installed: sortedKeys(installed), Present: sortedKeys(present), Missing: sortedKeys(missing)}
	return res, nil
}

func sortedKeys(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}
