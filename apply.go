package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The game only loads font files whose name matches a fontpattern from its fonts.conf ("notosans").
const copyPrefix = "notosans-"

type Rule struct {
	Group string  `json:"group"` // family name used by the game, e.g. "Stratum2 Condensed"
	Font  string  `json:"font"`  // Windows family name, empty = change size only
	Scale float64 `json:"scale"` // 1 = unchanged

	Weight int     `json:"weight,omitempty"` // CSS weight 100..900, 0 = as requested by the game
	Italic bool    `json:"italic,omitempty"` // slanted (real italic face if the font has one, else sheared)
	Width  float64 `json:"width,omitempty"`  // horizontal scale of the glyphs, 0/1 = unchanged
}

// trivial reports whether the rule changes nothing.
func (r Rule) trivial() bool {
	return r.Font == "" && (r.Scale <= 0 || r.Scale == 1) && r.Weight == 0 && !r.Italic && (r.Width <= 0 || r.Width == 1)
}

// fcWeight maps CSS weights to fontconfig weights.
func fcWeight(css int) int {
	switch {
	case css <= 100:
		return 0
	case css <= 200:
		return 40
	case css <= 300:
		return 50
	case css <= 400:
		return 80
	case css <= 500:
		return 100
	case css <= 600:
		return 180
	case css <= 700:
		return 200
	case css <= 800:
		return 205
	}
	return 210
}

// hasItalicFace tells whether the Windows family has a real italic/oblique face.
func hasItalicFace(family string) bool {
	for _, f := range filesOfFamily(family) {
		if _, it := styleWeight(f.Style, f.File); it {
			return true
		}
	}
	return false
}

type State struct {
	Rules  []Rule   `json:"rules"`
	Copied []string `json:"copied"`
	// TextScale multiplies the pixel size of every font (0 = unchanged). The game's Pango resolution is fixed
	// at 72 dpi and fontconfig's "dpi" is not consulted, so a rule on pixelsize is what scales all text.
	TextScale float64 `json:"textScale,omitempty"`
}

const (
	minTextScale = 0.5
	maxTextScale = 2.0
)

// normalizeTextScale returns 0 for "unchanged" and an error for values outside the supported range.
func normalizeTextScale(x float64) (float64, error) {
	if x == 0 || x == 1 {
		return 0, nil
	}
	if x != x || x < minTextScale || x > maxTextScale { // NaN or out of range
		return 0, fmt.Errorf("the text size must be between %d%% and %d%%", int(minTextScale*100), int(maxTextScale*100))
	}
	return x, nil
}

// Earlier versions wrote "csfc-state"; still read so their copied fonts are cleaned up.
var stateRe = regexp.MustCompile(`(?s)<!-- (?:csfc-)?state: (.*?) -->`)

func readState(root string) (State, bool) {
	var st State
	b, err := os.ReadFile(confFile(root))
	if err != nil {
		return st, false
	}
	m := stateRe.FindSubmatch(b)
	if m == nil {
		return st, false // a 99-spddl.conf not written by this app
	}
	_ = json.Unmarshal(m[1], &st)
	return st, true
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func buildConf(st State) []byte {
	js, _ := json.Marshal(st)
	var b bytes.Buffer
	b.WriteString("<?xml version='1.0'?>\n<!DOCTYPE fontconfig SYSTEM 'fonts.dtd'>\n<fontconfig>\n")
	b.WriteString("\t<!-- CS2 Font Studio. Delete this file to remove all changes. -->\n")
	fmt.Fprintf(&b, "\t<!-- state: %s -->\n", strings.ReplaceAll(string(js), "--", `--`))
	if st.TextScale > 0 && st.TextScale != 1 {
		// every font, fallbacks included: the font's pixel size is multiplied after the game's own rules
		fmt.Fprintf(&b, "\t<match target=\"font\"><edit name=\"pixelsize\" mode=\"assign\"><times><name>pixelsize</name><double>%g</double></times></edit></match>\n", st.TextScale)
	}
	for _, r := range st.Rules {
		g, f := esc(r.Group), esc(r.Font)
		if r.Font != "" {
			fmt.Fprintf(&b, "\t<match target=\"font\"><test name=\"family\"><string>%s</string></test><edit name=\"family\" mode=\"assign\"><string>%s</string></edit></match>\n", g, f)
			fmt.Fprintf(&b, "\t<match target=\"pattern\"><test name=\"family\"><string>%s</string></test><edit name=\"family\" mode=\"prepend\" binding=\"strong\"><string>%s</string></edit></match>\n", g, f)
		}
		if r.Scale > 0 && r.Scale != 1 {
			fmt.Fprintf(&b, "\t<match target=\"font\"><test name=\"family\" target=\"pattern\"><string>%s</string></test><edit name=\"pixelsize\" mode=\"assign\"><times><name>pixelsize</name><double>%g</double></times></edit></match>\n", g, r.Scale)
		}
		if r.Weight != 0 || r.Italic { // face selection: edit the request
			fmt.Fprintf(&b, "\t<match target=\"pattern\"><test name=\"family\"><string>%s</string></test>", g)
			if r.Weight != 0 {
				fmt.Fprintf(&b, "<edit name=\"weight\" mode=\"assign\" binding=\"strong\"><int>%d</int></edit>", fcWeight(r.Weight))
			}
			if r.Italic {
				b.WriteString("<edit name=\"slant\" mode=\"assign\" binding=\"strong\"><const>italic</const></edit>")
			}
			b.WriteString("</match>\n")
		}
		width := r.Width
		if width <= 0 {
			width = 1
		}
		shear := 0.0
		if r.Italic && (r.Font == "" || !hasItalicFace(r.Font)) {
			shear = 0.2 // no real italic face: slant the glyphs
		}
		if width != 1 || shear != 0 { // glyph transformation (the game hands the matrix to FT_Set_Transform): edit the font
			fmt.Fprintf(&b, "\t<match target=\"font\"><test name=\"family\" target=\"pattern\"><string>%s</string></test>", g)
			fmt.Fprintf(&b, "<edit name=\"matrix\" mode=\"assign\"><times><name>matrix</name><matrix><double>%g</double><double>%g</double><double>0</double><double>1</double></matrix></times></edit>", width, shear)
			b.WriteString("</match>\n")
		}
	}
	b.WriteString("</fontconfig>\n")
	return b.Bytes()
}

func clearFontconfigCache() { _ = os.RemoveAll(filepath.Join(os.Getenv("TEMP"), "fontconfig")) }

// valveFonts are the font files the game itself ships in its font folder; they are never removed.
var valveFonts = []string{
	"notomono-regular.ttf",
	"notosans-bold.ttf", "notosans-bolditalic.ttf", "notosans-italic.ttf", "notosans-regular.ttf",
	"notosansjp-bold.ttf", "notosansjp-light.ttf", "notosansjp-regular.ttf",
	"notosanskr-bold.ttf", "notosanskr-light.ttf", "notosanskr-regular.ttf",
	"notosanssc-bold.ttf", "notosanssc-light.ttf", "notosanssc-regular.ttf",
	"notosanssymbols-regular.ttf",
	"notosanstc-bold.ttf", "notosanstc-light.ttf", "notosanstc-regular.ttf",
	"notosansthai-bold.ttf", "notosansthai-light.ttf", "notosansthai-regular.ttf",
	"notoserif-bold.ttf", "notoserif-bolditalic.ttf", "notoserif-boliitalic.ttf", "notoserif-italic.ttf", "notoserif-regular.ttf",
}

// clearUpFontsFolder removes every font file from the game's font folder that is not one of the game's own: copies of
// earlier settings, of older versions of this app, or put there by hand or by other tools. The game loads every font in the
// folder, so unused ones only cost start-up time. Other files (fonts.conf, stratum2.uifont, ...) are left alone.
func clearUpFontsFolder(root string) {
	dir := gameFontsDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		switch strings.ToLower(filepath.Ext(n)) {
		case ".ttf", ".otf", ".ttc":
			if e.Type().IsRegular() && !slices.Contains(valveFonts, strings.ToLower(n)) {
				_ = os.Remove(filepath.Join(dir, n))
			}
		}
	}
}

func apply(root string, rules []Rule, textScale float64) error {
	textScale, err := normalizeTextScale(textScale)
	if err != nil {
		return err
	}
	clearUpFontsFolder(root)
	st := State{TextScale: textScale}
	dir := gameFontsDir(root)
	copied := map[string]bool{} // source file -> already copied
	// a name that is still taken belongs to the game (notosans-regular.ttf, ...): never overwrite it
	taken := func(n string) bool { return fileExists(filepath.Join(dir, n)) }
	for _, r := range rules {
		if r.Group == "" || r.trivial() {
			continue
		}
		if r.Font != "" {
			files := filesOfFamily(r.Font)
			if len(files) == 0 {
				return fmt.Errorf("font %q not found", r.Font)
			}
			for _, f := range files {
				src := filepath.Join(f.Dir, f.File)
				if copied[strings.ToLower(src)] {
					continue
				}
				data, err := os.ReadFile(src)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				dst := freeName(copyPrefix+f.File, taken)
				if err := os.WriteFile(filepath.Join(dir, dst), data, 0o644); err != nil {
					return err
				}
				copied[strings.ToLower(src)] = true
				st.Copied = append(st.Copied, dst)
			}
		}
		st.Rules = append(st.Rules, r)
	}
	if len(st.Rules) == 0 && st.TextScale == 0 {
		return reset(root)
	}
	if err := os.WriteFile(confFile(root), buildConf(st), 0o644); err != nil {
		return err
	}
	clearFontconfigCache()
	markChanged()
	return nil
}

func reset(root string) error {
	clearUpFontsFolder(root)
	if err := os.Remove(confFile(root)); err != nil && !os.IsNotExist(err) {
		return err
	}
	clearFontconfigCache()
	markChanged()
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// ---- HTTP-API

func registerAPI(mux *http.ServeMux) {
	jsonOut := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, code int, err error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
	}
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		root, err := csRoot()
		found := err == nil && dirExists(gameFontsDir(root))
		info := map[string]any{"csPath": root, "found": found}
		_, started, running := csProcess()
		info["running"] = running
		if err == nil {
			info["needsRestart"] = needsRestart(running, started, changedAt(root))
		}
		if err != nil {
			info["error"] = err.Error()
		}
		if found {
			st, ok := readState(root)
			info["installed"] = ok
			info["rules"] = st.Rules
			info["textScale"] = st.TextScale
		}
		jsonOut(w, info)
	})
	mux.HandleFunc("/api/fonts", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, familyList()) })
	// font files of a family for the browser preview (@font-face), whether or not the browser allows local fonts
	mux.HandleFunc("/api/fontinfo", func(w http.ResponseWriter, r *http.Request) {
		type face struct {
			URL    string `json:"url"`
			Weight int    `json:"weight"`
			Italic bool   `json:"italic"`
		}
		var out []face
		for _, f := range filesOfFamily(r.URL.Query().Get("family")) {
			if strings.EqualFold(filepath.Ext(f.File), ".ttc") {
				continue // browsers cannot load @font-face from collections
			}
			wgt, it := styleWeight(f.Style, f.File)
			out = append(out, face{URL: "/api/fontfile?d=" + fmt.Sprint(dirIndex(f.Dir)) + "&f=" + url.QueryEscape(f.File), Weight: wgt, Italic: it})
		}
		jsonOut(w, out)
	})
	mux.HandleFunc("/api/fontfile", func(w http.ResponseWriter, r *http.Request) {
		dirs := fontDirs()
		d, err := strconv.Atoi(r.URL.Query().Get("d"))
		name := r.URL.Query().Get("f")
		if err != nil || d < 0 || d >= len(dirs) || name != filepath.Base(name) {
			http.NotFound(w, r)
			return
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".ttf", ".otf":
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		http.ServeFile(w, r, filepath.Join(dirs[d], name))
	})
	mux.HandleFunc("/api/path", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Path string }
		if r.Method != http.MethodPost {
			fail(w, 405, errors.New("POST only"))
			return
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || !dirExists(gameFontsDir(in.Path)) {
			fail(w, 400, errors.New("the folder does not contain game\\csgo\\panorama\\fonts"))
			return
		}
		if err := saveUserConfig(userConfig{CSPath: in.Path}); err != nil {
			fail(w, 500, err)
			return
		}
		jsonOut(w, "ok")
	})
	mux.HandleFunc("/api/apply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fail(w, 405, errors.New("POST only"))
			return
		}
		root, err := csRoot()
		if err != nil {
			fail(w, 400, err)
			return
		}
		var in struct {
			Rules     []Rule
			TextScale float64
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, err)
			return
		}
		if err := apply(root, in.Rules, in.TextScale); err != nil {
			fail(w, 500, err)
			return
		}
		jsonOut(w, "ok")
	})
	mux.HandleFunc("/api/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fail(w, 405, errors.New("POST only"))
			return
		}
		if err := restartCS(); err != nil {
			fail(w, 500, err)
			return
		}
		jsonOut(w, "ok")
	})
	mux.HandleFunc("/api/profiles", func(w http.ResponseWriter, r *http.Request) {
		items := scanProfiles()
		if items == nil {
			items = []ProfileInfo{}
		}
		jsonOut(w, map[string]any{"items": items, "dir": profilesSaveDir()})
	})
	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		e, ok := findProfile(r.URL.Query().Get("key"))
		if !ok {
			fail(w, 404, errors.New("configuration not found"))
			return
		}
		p, ok := readProfile(e.Path)
		if !ok {
			fail(w, 500, errors.New("the file is no longer a valid configuration"))
			return
		}
		jsonOut(w, map[string]any{"name": e.Name, "rules": p.Rules, "textScale": p.TextScale})
	})
	mux.HandleFunc("/api/profile/save", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name      string
			Rules     []Rule
			TextScale float64
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&in) != nil {
			fail(w, 400, errors.New("invalid request"))
			return
		}
		info, err := saveProfile(in.Name, in.Rules, in.TextScale)
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, info)
	})
	mux.HandleFunc("/api/profile/delete", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Key string }
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&in) != nil {
			fail(w, 400, errors.New("invalid request"))
			return
		}
		if err := deleteProfile(in.Key); err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, "ok")
	})
	// configuration + font files as one zip: POST = the current settings, GET ?key= = a saved profile
	mux.HandleFunc("/api/bundle", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name      string
			Rules     []Rule
			TextScale float64
		}
		switch r.Method {
		case http.MethodPost:
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				fail(w, 400, errors.New("invalid request"))
				return
			}
		case http.MethodGet:
			e, ok := findProfile(r.URL.Query().Get("key"))
			if !ok {
				fail(w, 404, errors.New("configuration not found"))
				return
			}
			p, ok := readProfile(e.Path)
			if !ok {
				fail(w, 500, errors.New("the file is no longer a valid configuration"))
				return
			}
			in.Name, in.Rules, in.TextScale = e.Name, p.Rules, p.TextScale
		default:
			fail(w, 405, errors.New("GET or POST only"))
			return
		}
		b, err := writeBundle(in.Name, in.Rules, in.TextScale)
		if err != nil {
			fail(w, 400, err)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": in.Name + ".zip"}))
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/api/bundle/import", func(w http.ResponseWriter, r *http.Request) {
		// application/zip is not a "simple" content type, so a foreign web page cannot post here without a CORS preflight
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/zip" {
			fail(w, 400, errors.New("invalid request"))
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBundleSize))
		if err != nil {
			fail(w, 400, errors.New("the archive is too large"))
			return
		}
		res, err := importBundle(data, r.URL.Query().Get("name"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, res)
	})
	mux.HandleFunc("/api/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fail(w, 405, errors.New("POST only"))
			return
		}
		root, err := csRoot()
		if err != nil {
			fail(w, 400, err)
			return
		}
		if err := reset(root); err != nil {
			fail(w, 500, err)
			return
		}
		jsonOut(w, "ok")
	})
}

func dirIndex(dir string) int {
	for i, d := range fontDirs() {
		if d == dir {
			return i
		}
	}
	return -1
}
