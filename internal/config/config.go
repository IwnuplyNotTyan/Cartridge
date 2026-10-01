// Package config resolves ROM/library paths and the UI language.
// Precedence: flags > $CARTRIDGE_* environment > config file > defaults.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cartridge/internal/i18n"
	"cartridge/internal/library"
)

// Env vars consulted during resolution.
const (
	EnvDir     = "CARTRIDGE_DIR"
	EnvLibrary = "CARTRIDGE_LIBRARY"
	EnvLang    = "CARTRIDGE_LANG"
	EnvConfig  = "CARTRIDGE_CONFIG"
	EnvRecipes = "CARTRIDGE_RECIPES"
)

// File is the on-disk JSON config (~/.config/cartridge/config.json).
type File struct {
	RomDir  string `json:"rom_dir"`
	Library string `json:"library"`
	Lang    string `json:"lang"`
	Recipes string `json:"recipes"`
}

// Config is the fully resolved runtime configuration.
type Config struct {
	RomDir     string
	Library    string
	Lang       i18n.Lang
	Recipes    string
	ConfigFile string // file actually read ("" when none)
}

// DefaultRecipesPath returns the standard recipes file location.
func DefaultRecipesPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "recipes.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "cartridge", "recipes.json")
}

// DefaultFilePath returns the standard config file location.
func DefaultFilePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "cartridge", "config.json")
}

// LoadFile reads path; a missing file yields a zero File and no error.
func LoadFile(path string) (File, error) {
	var f File
	if path == "" {
		return f, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("parse config %s: %w", path, err)
	}
	return f, nil
}

// Resolve merges flags, env, file and defaults into a Config.
// Empty flag values mean "not set".
func Resolve(dirFlag, libFlag, langFlag, cfgFlag, recipesFlag string) (Config, error) {
	cfgPath := firstNonEmpty(cfgFlag, os.Getenv(EnvConfig), DefaultFilePath())
	explicit := cfgFlag != "" || os.Getenv(EnvConfig) != ""
	var (
		f     File
		used  string
		found bool
	)
	if cfgPath != "" {
		if st, err := os.Stat(cfgPath); err == nil && !st.IsDir() {
			found = true
		} else if explicit {
			return Config{}, fmt.Errorf("config file not found: %s", cfgPath)
		}
		if found {
			var err error
			if f, err = LoadFile(cfgPath); err != nil {
				return Config{}, err
			}
			used = cfgPath
		}
	}

	lang := i18n.Parse(firstNonEmpty(langFlag, os.Getenv(EnvLang), f.Lang))
	if lang == i18n.Auto {
		lang = i18n.Detect()
	}

	return Config{
		RomDir:     expandPath(firstNonEmpty(dirFlag, os.Getenv(EnvDir), f.RomDir, defaultRomDir())),
		Library:    expandPath(firstNonEmpty(libFlag, os.Getenv(EnvLibrary), f.Library, library.DefaultPath())),
		Lang:       lang,
		Recipes:    expandPath(firstNonEmpty(recipesFlag, os.Getenv(EnvRecipes), f.Recipes, DefaultRecipesPath())),
		ConfigFile: used,
	}, nil
}

// defaultRomDir prefers ~/files/games, then ./games, then ".".
func defaultRomDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, "files", "games"); isDir(p) {
			return p
		}
	}
	for _, p := range []string{"./games", "."} {
		if isDir(p) {
			return p
		}
	}
	return "."
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// expandPath expands a leading ~ to the home dir and cleans the result.
func expandPath(p string) string {
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return filepath.Clean(p)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
