package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/andygrunwald/vdf"
	"golang.org/x/sys/windows/registry"
)

type userConfig struct {
	CSPath string `json:"csPath"`
}

func userConfigFile() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "CS2FontStudio", "config.json")
}

func loadUserConfig() (c userConfig) {
	if b, err := os.ReadFile(userConfigFile()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return
}

func saveUserConfig(c userConfig) error {
	f := userConfigFile()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(f, b, 0o644)
}

// csRoot returns ...\Counter-Strike Global Offensive (saved path before the Steam lookup)
func csRoot() (string, error) {
	if p := loadUserConfig().CSPath; p != "" && dirExists(gameFontsDir(p)) {
		return p, nil
	}
	return findCSPath()
}

func dirExists(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

func findCSPath() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Valve\Steam`, registry.READ)
	if err != nil {
		return "", err
	}
	defer k.Close()
	steam, _, err := k.GetStringValue("SteamPath")
	if err != nil {
		return "", err
	}
	f, err := os.Open(filepath.Join(steam, "SteamApps", "libraryfolders.vdf"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := vdf.NewParser(f).Parse()
	if err != nil {
		return "", err
	}
	lib, _ := data["libraryfolders"].(map[string]interface{})
	for _, v := range lib {
		folder, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		apps, _ := folder["apps"].(map[string]interface{})
		if _, has := apps["730"]; has {
			if p, ok := folder["path"].(string); ok {
				return filepath.Join(p, "steamapps", "common", "Counter-Strike Global Offensive"), nil
			}
		}
	}
	return "", errors.New("Counter-Strike 2 (App 730) not found in any Steam library")
}

func gameFontsDir(root string) string {
	return filepath.Join(root, "game", "csgo", "panorama", "fonts")
}

func confFile(root string) string {
	return filepath.Join(root, "game", "core", "panorama", "fonts", "conf.d", "99-spddl.conf")
}
