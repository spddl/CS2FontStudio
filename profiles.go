package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Profiles are saved font configurations (JSON files with a list of rules).
// They are found next to the exe (any *.json that holds rules, and the "profiles" folder there)
// and in the user's app data folder. New ones are written to <exe folder>\profiles,
// or to the app data folder when the exe folder is read-only.

const profileApp = "CS2FontStudio"

type profileFile struct {
	App       string  `json:"app,omitempty"`
	Version   int     `json:"version,omitempty"`
	Name      string  `json:"name,omitempty"`
	Rules     []Rule  `json:"rules"`
	TextScale float64 `json:"textScale,omitempty"` // global text size, see State.TextScale
}

type ProfileInfo struct {
	Key   string `json:"key"`   // where + ":" + file name, how the UI refers to a profile
	Name  string `json:"name"`  // file name without .json
	Where string `json:"where"` // "profiles" (profiles folder), "exe" (loose file next to the exe), "user" (app data)
	Rules int    `json:"rules"`
}

type profileDir struct{ where, dir string }

// profileDirs lists the folders searched for profiles, in priority order (replaced in tests).
var profileDirs = func() []profileDir {
	var dirs []profileDir
	if exe, err := os.Executable(); err == nil {
		ed := filepath.Dir(exe)
		dirs = append(dirs, profileDir{"profiles", filepath.Join(ed, "profiles")}, profileDir{"exe", ed})
	}
	if ud, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, profileDir{"user", filepath.Join(ud, "CS2FontStudio", "profiles")})
	}
	return dirs
}

// readProfile parses a file as a profile: {"rules":[...]} or a bare list of rules.
// Anything else (for example another tool's config.json) is not a profile.
func readProfile(path string) (profileFile, bool) {
	var p profileFile
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Size() > 1<<20 {
		return p, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return p, false
	}
	return parseProfile(b)
}

// parseProfile checks the contents of a profile file (also used for config.json in a bundle).
func parseProfile(b []byte) (profileFile, bool) {
	var p profileFile
	if json.Unmarshal(b, &p) != nil {
		var bare []Rule
		if json.Unmarshal(b, &bare) != nil {
			return p, false
		}
		p = profileFile{Rules: bare}
	}
	if len(p.Rules) == 0 && p.TextScale == 0 {
		return p, false
	}
	for _, r := range p.Rules {
		if r.Group == "" {
			return p, false
		}
	}
	return p, true
}

type profileEntry struct {
	ProfileInfo
	Path string
}

func scanProfileEntries() []profileEntry {
	var out []profileEntry
	for _, d := range profileDirs() {
		files, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.EqualFold(filepath.Ext(f.Name()), ".json") {
				continue
			}
			path := filepath.Join(d.dir, f.Name())
			p, ok := readProfile(path)
			if !ok {
				continue
			}
			out = append(out, profileEntry{
				ProfileInfo: ProfileInfo{
					Key:   d.where + ":" + f.Name(),
					Name:  strings.TrimSuffix(f.Name(), filepath.Ext(f.Name())),
					Where: d.where,
					Rules: len(p.Rules),
				},
				Path: path,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func scanProfiles() []ProfileInfo {
	entries := scanProfileEntries()
	out := make([]ProfileInfo, len(entries))
	for i, e := range entries {
		out[i] = e.ProfileInfo
	}
	return out
}

// findProfile resolves a key from the UI against what was actually found on disk,
// so no path from the request is ever used directly.
func findProfile(key string) (profileEntry, bool) {
	for _, e := range scanProfileEntries() {
		if e.Key == key {
			return e, true
		}
	}
	return profileEntry{}, false
}

var reservedNames = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}

// profileFileName turns a user-entered name into a safe file name.
func profileFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".json")
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New("enter a name for the configuration")
	case len([]rune(name)) > 60:
		return "", errors.New("the name is too long (60 characters at most)")
	case strings.HasSuffix(name, "."):
		return "", errors.New("the name must not end with a dot")
	case reservedNames[strings.ToUpper(name)]:
		return "", errors.New("this name is reserved by Windows")
	}
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return "", errors.New(`the name must not contain \ / : * ? " < > |`)
		}
	}
	return name + ".json", nil
}

// profilesSaveDir is where new profiles go first (the folder is created when something is saved).
func profilesSaveDir() string {
	for _, d := range profileDirs() {
		if d.where == "profiles" {
			return d.dir
		}
	}
	return ""
}

// newProfile drops rules that change nothing and checks the text size; file is the safe file name.
func newProfile(name string, rules []Rule, textScale float64) (p profileFile, file string, err error) {
	file, err = profileFileName(name)
	if err != nil {
		return p, "", err
	}
	var keep []Rule
	for _, r := range rules {
		if r.Group != "" && !r.trivial() {
			keep = append(keep, r)
		}
	}
	textScale, err = normalizeTextScale(textScale)
	if err != nil {
		return p, "", err
	}
	if len(keep) == 0 && textScale == 0 {
		return p, "", errors.New("nothing is changed, there is nothing to save")
	}
	return profileFile{App: profileApp, Version: 1, Name: strings.TrimSuffix(file, ".json"), Rules: keep, TextScale: textScale}, file, nil
}

// freeProfileName returns name, or "name (2)", "name (3)", ... if a saved profile already uses it.
func freeProfileName(name string) string {
	taken := map[string]bool{}
	for _, p := range scanProfiles() {
		taken[strings.ToLower(p.Name)] = true
	}
	for i, n := 2, name; ; i++ {
		if !taken[strings.ToLower(n)] {
			return n
		}
		n = name + " (" + strconv.Itoa(i) + ")"
	}
}

// saveProfile writes the rules as a named profile and returns where it ended up.
func saveProfile(name string, rules []Rule, textScale float64) (ProfileInfo, error) {
	p, file, err := newProfile(name, rules, textScale)
	if err != nil {
		return ProfileInfo{}, err
	}
	keep := p.Rules
	b, _ := json.MarshalIndent(p, "", "  ")
	var firstErr error
	for _, d := range profileDirs() {
		if d.where == "exe" {
			continue // loose files next to the exe are only read, never written
		}
		if err := os.MkdirAll(d.dir, 0o755); err != nil {
			firstErr = errors.Join(firstErr, err)
			continue
		}
		if err := os.WriteFile(filepath.Join(d.dir, file), b, 0o644); err != nil {
			firstErr = errors.Join(firstErr, err)
			continue
		}
		return ProfileInfo{Key: d.where + ":" + file, Name: strings.TrimSuffix(file, ".json"), Where: d.where, Rules: len(keep)}, nil
	}
	if firstErr == nil {
		firstErr = errors.New("no folder to save to")
	}
	return ProfileInfo{}, firstErr
}

// deleteProfile removes a saved profile. Loose files next to the exe belong to the user and are never deleted.
func deleteProfile(key string) error {
	e, ok := findProfile(key)
	if !ok {
		return errors.New("configuration not found")
	}
	if e.Where == "exe" {
		return errors.New("files next to the exe are not deleted by the app: remove it in Explorer")
	}
	return os.Remove(e.Path)
}
