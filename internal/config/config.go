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
	"cartridge/internal/mods"
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
	EnvPatched = "CARTRIDGE_PATCHED_DIR"
	EnvSaves   = "CARTRIDGE_SAVES_DIR"
	EnvCheats  = "CARTRIDGE_CHEATS_DIR"
	// Steam integration overrides.
	EnvSteamAutoAdd  = "CARTRIDGE_STEAM_AUTO_ADD"
	EnvSteamPrefix   = "CARTRIDGE_STEAM_PREFIX"
	EnvSteamTemplate = "CARTRIDGE_STEAM_TEMPLATE"
	EnvSteamExe      = "CARTRIDGE_STEAM_EXE"
	EnvSteamGridKey  = "CARTRIDGE_STEAMGRIDDB_KEY"
	EnvIconsProvider = "CARTRIDGE_ICONS_PROVIDER"
	EnvIconsDir      = "CARTRIDGE_ICONS_DIR"
)

// SteamConfig controls the Steam non-Steam shortcuts integration.
//
// Tags (Steam categories) are built per game from Prefix + CategoryTemplate:
// by default every game gets ["Cartridge - GBA"], i.e. one category per
// console. The shared "all games" root tag is opt-in via AllGames.
// Template placeholders: {prefix} {console} {console_id} {short} {title}
// {name} ({name} is the per-console custom name from Categories, else the
// console short tag).
type SteamConfig struct {
	AutoAdd          bool              `json:"auto_add,omitempty"`
	Prefix           string            `json:"prefix,omitempty"`
	AllGames         bool              `json:"all_games,omitempty"`
	CategoryTemplate string            `json:"category_template,omitempty"`
	Categories       map[string]string `json:"categories,omitempty"`
	Exe              string            `json:"exe,omitempty"`
	Users            []string          `json:"users,omitempty"`
	// Icons controls cached artwork attached to shortcuts (on by default;
	// local lookup only — no network during normal syncs):
	// IconsProvider is auto | libretro | steamgriddb | off
	// (auto = free Libretro CDN first, SteamGridDB fills gaps when a
	// key is set; off disables Icon + grid entirely);
	// IconsDir overrides the XDG cache location;
	// SteamGridDBKey is the optional premium art API key.
	IconsProvider  string `json:"icons_provider,omitempty"`
	IconsDir       string `json:"icons_dir,omitempty"`
	SteamGridDBKey string `json:"steamgriddb_key,omitempty"`
}

// IconsEnabledOrDefault reports whether icon lookup applies.
// Zero value (field absent) means enabled — lookups are cheap local
// stats, the network fetch only runs on explicit `steam icons sync`.
func (s SteamConfig) IconsOn() bool {
	if s.IconsProvider == "off" {
		return false
	}
	return true
}

// EffectiveIconsProvider normalizes the provider name (default auto).
func (s SteamConfig) EffectiveIconsProvider() string {
	switch s.IconsProvider {
	case "libretro", "steamgriddb", "off":
		return s.IconsProvider
	case "auto", "":
		return "auto"
	default:
		return "auto"
	}
}

// DefaultSteamConfig returns the out-of-the-box Steam settings.
func DefaultSteamConfig() SteamConfig {
	return SteamConfig{
		AutoAdd:          false,
		Prefix:           "Cartridge",
		AllGames:         false,
		CategoryTemplate: "{prefix} - {name}",
		IconsProvider:    "auto",
	}
}

// EffectivePrefix returns the configured prefix (may be empty = disabled).
func (s SteamConfig) EffectivePrefix() string { return s.Prefix }

// EffectiveTemplate returns the category template or the default.
func (s SteamConfig) EffectiveTemplate() string {
	if strings.TrimSpace(s.CategoryTemplate) == "" {
		return "{prefix} - {name}"
	}
	return s.CategoryTemplate
}

// File is the on-disk JSON config (~/.config/cartridge/config.json).
type File struct {
	RomDir    string       `json:"rom_dir"`
	Library   string       `json:"library"`
	Lang      string       `json:"lang"`
	Recipes   string       `json:"recipes"`
	Patches   string       `json:"patches,omitempty"`
	ModsDir   string       `json:"mods_dir,omitempty"`
	Patched   string       `json:"patched_dir,omitempty"`
	SavesDir  string       `json:"saves_dir,omitempty"`
	CheatsDir string       `json:"cheats_dir,omitempty"`
	Steam     *SteamConfig `json:"steam,omitempty"`
}

// Config is the fully resolved runtime configuration.
type Config struct {
	RomDir     string
	Library    string
	Lang       i18n.Lang
	Recipes    string
	Patches    string
	ModsDir    string
	Patched    string
	SavesDir   string
	CheatsDir  string
	Steam      SteamConfig
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

// DefaultPatchedDir returns the standard directory for hard-patched ROM
// instances (one file per game+mod, outside the games dir so rescans
// never pick them up as library games).
func DefaultPatchedDir() string { return mods.DefaultPatchedDir() }

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

	steam := DefaultSteamConfig()
	if f.Steam != nil {
		steam = *f.Steam
		// Empty template in an explicit section means "use default".
		if strings.TrimSpace(steam.CategoryTemplate) == "" {
			steam.CategoryTemplate = DefaultSteamConfig().CategoryTemplate
		}
		// NOTE: empty Prefix in an explicit section stays empty (= no root tag).
		// Absent section (f.Steam == nil) keeps the default prefix above.
	}
	// Env overrides file for Steam.
	if v := strings.TrimSpace(os.Getenv(EnvSteamAutoAdd)); v != "" {
		steam.AutoAdd = parseBoolEnv(v, steam.AutoAdd)
	}
	if v, present := os.LookupEnv(EnvSteamPrefix); present {
		steam.Prefix = strings.TrimSpace(v)
	}
	if v, present := os.LookupEnv(EnvSteamTemplate); present {
		steam.CategoryTemplate = strings.TrimSpace(v)
		if steam.CategoryTemplate == "" {
			steam.CategoryTemplate = DefaultSteamConfig().CategoryTemplate
		}
	}
	if v, present := os.LookupEnv(EnvSteamExe); present {
		steam.Exe = strings.TrimSpace(v)
	}
	if v, present := os.LookupEnv(EnvSteamGridKey); present {
		steam.SteamGridDBKey = strings.TrimSpace(v)
	}
	if v, present := os.LookupEnv(EnvIconsProvider); present {
		if p := strings.ToLower(strings.TrimSpace(v)); p != "" {
			steam.IconsProvider = p
		}
	}
	if v, present := os.LookupEnv(EnvIconsDir); present {
		steam.IconsDir = strings.TrimSpace(v)
	}

	return Config{
		RomDir:     expandPath(firstNonEmpty(dirFlag, os.Getenv(EnvDir), f.RomDir, defaultRomDir())),
		Library:    expandPath(firstNonEmpty(libFlag, os.Getenv(EnvLibrary), f.Library, library.DefaultPath())),
		Lang:       lang,
		Recipes:    expandPath(firstNonEmpty(recipesFlag, os.Getenv(EnvRecipes), f.Recipes, DefaultRecipesPath())),
		Patches:    expandPath(firstNonEmpty(os.Getenv(EnvPatches), f.Patches, DefaultPatchesPath())),
		ModsDir:    expandPath(firstNonEmpty(os.Getenv(EnvMods), f.ModsDir, DefaultModsDir())),
		Patched:    expandPath(firstNonEmpty(os.Getenv(EnvPatched), f.Patched, DefaultPatchedDir())),
		SavesDir:   expandPath(firstNonEmpty(os.Getenv(EnvSaves), f.SavesDir, DefaultSavesDir())),
		CheatsDir:  expandPath(firstNonEmpty(os.Getenv(EnvCheats), f.CheatsDir, DefaultCheatsDir())),
		Steam:      steam,
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
	// Any loaded language code round-trips ("en", "ru", "de", ...);
	// unknown values persist as empty (system default).
	lang := ""
	if i18n.Valid(c.Lang) {
		lang = string(c.Lang)
	}
	steam := c.Steam
	return File{
		RomDir:    c.RomDir,
		Library:   c.Library,
		Lang:      lang,
		Recipes:   c.Recipes,
		Patches:   c.Patches,
		ModsDir:   c.ModsDir,
		Patched:   c.Patched,
		SavesDir:  c.SavesDir,
		CheatsDir: c.CheatsDir,
		Steam:     &steam,
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

// parseBoolEnv parses 1/true/yes/on (and 0/false/no/off) case-insensitively.
func parseBoolEnv(v string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}
