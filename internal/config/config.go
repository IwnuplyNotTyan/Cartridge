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
	EnvPatches = "CARTRIDGE_PATCHES"
	EnvMods    = "CARTRIDGE_MODS_DIR"
	EnvSaves   = "CARTRIDGE_SAVES_DIR"
	EnvCheats  = "CARTRIDGE_CHEATS_DIR"
)

// File is the on-disk JSON config (~/.config/cartridge/config.json).
type File struct {
	RomDir    string `json:"rom_dir"`
	Library   string `json:"library"`
	Lang      string `json:"lang"`
	Recipes   string `json:"recipes"`
	Patches   string `json:"patches,omitempty"`
	ModsDir   string `json:"mods_dir,omitempty"`
	SavesDir  string `json:"saves_dir,omitempty"`
	CheatsDir string `json:"cheats_dir,omitempty"`
}

// Config is the fully resolved runtime configuration.
type Config struct {
	RomDir     string
	Library    string
	Lang       i18n.Lang
	Recipes    string
	Patches    string
	ModsDir    string
	SavesDir   string
	CheatsDir  string
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

// DefaultPatchesPath returns the standard patches file location.
// Patches overlay base recipes in memory and never rewrite recipes file.
func DefaultPatchesPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "patches.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "cartridge", "patches.json")
}

// DefaultModsDir returns the standard directory for mods.
func DefaultModsDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "mods")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "mods"
	}
	return filepath.Join(home, ".local", "share", "cartridge", "mods")
}

// DefaultSavesDir returns the standard directory for saves/states.
func DefaultSavesDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "saves")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "saves"
	}
	return filepath.Join(home, ".local", "share", "cartridge", "saves")
}

// DefaultCheatsDir returns the standard directory for cheat files.
func DefaultCheatsDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "cheats")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "cheats"
	}
	return filepath.Join(home, ".local", "share", "cartridge", "cheats")
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
		Patches:    expandPath(firstNonEmpty(os.Getenv(EnvPatches), f.Patches, DefaultPatchesPath())),
		ModsDir:    expandPath(firstNonEmpty(os.Getenv(EnvMods), f.ModsDir, DefaultModsDir())),
		SavesDir:   expandPath(firstNonEmpty(os.Getenv(EnvSaves), f.SavesDir, DefaultSavesDir())),
		CheatsDir:  expandPath(firstNonEmpty(os.Getenv(EnvCheats), f.CheatsDir, DefaultCheatsDir())),
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

// ExpandPath expands a leading ~ to the home dir and cleans the result.
func ExpandPath(p string) string { return expandPath(p) }

// ToFile converts a resolved Config back to its on-disk form.
func (c Config) ToFile() File {
	lang := ""
	switch c.Lang {
	case i18n.Ru:
		lang = "ru"
	case i18n.En:
		lang = "en"
	}
	return File{
		RomDir:    c.RomDir,
		Library:   c.Library,
		Lang:      lang,
		Recipes:   c.Recipes,
		Patches:   c.Patches,
		ModsDir:   c.ModsDir,
		SavesDir:  c.SavesDir,
		CheatsDir: c.CheatsDir,
	}
}

// SaveFile writes f to path atomically (tmp + rename), creating dirs.
func SaveFile(path string, f File) error {
	if path == "" {
		return fmt.Errorf("empty config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SavePath resolves where to persist c (explicit file or default location).
func (c Config) SavePath() string {
	if c.ConfigFile != "" {
		return c.ConfigFile
	}
	return DefaultFilePath()
}

// Save persists the resolved config back to its file.
func (c Config) Save() error { return SaveFile(c.SavePath(), c.ToFile()) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
