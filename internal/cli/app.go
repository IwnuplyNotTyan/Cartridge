package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"cartridge/internal/config"
	"cartridge/internal/emulate"
	"cartridge/internal/favorites"
	"cartridge/internal/i18n"
	"cartridge/internal/library"
	"cartridge/internal/overrides"
	"cartridge/internal/playtime"
	"cartridge/internal/rom"
	"cartridge/internal/steam"
	"cartridge/internal/ui"
)

// GlobalOpts mirrors the persistent flags shared by every command.
type GlobalOpts struct {
	Dir       string
	Library   string
	Lang      string
	Config    string
	Recipes   string
	Patches   string
	ModsDir   string
	CheatsDir string
	Patched   string
}

// AddGlobalFlags registers persistent flags on root (inherited by all).
func AddGlobalFlags(root *cobra.Command) {
	p := root.PersistentFlags()
	p.String("dir", "", "ROM directory ($CARTRIDGE_DIR)")
	p.String("library", "", "library database path ($CARTRIDGE_LIBRARY)")
	p.String("lang", "", "language: en, ru, auto ($CARTRIDGE_LANG)")
	p.String("config", "", "config file path ($CARTRIDGE_CONFIG)")
	p.String("recipes", "", "recipes file or dir ($CARTRIDGE_RECIPES)")
	p.String("patches", "", "recipe patches file or dir ($CARTRIDGE_PATCHES)")
	p.String("mods-dir", "", "mods/patches directory ($CARTRIDGE_MODS_DIR)")
	p.String("cheats-dir", "", "cheat files directory ($CARTRIDGE_CHEATS_DIR)")
	p.String("patched-dir", "", "hard-patched ROM instances dir ($CARTRIDGE_PATCHED_DIR)")
}

// globalOptsFromCmd reads persistent flag values (never fails: flags exist).
func globalOptsFromCmd(cmd *cobra.Command) GlobalOpts {
	get := func(name string) string {
		v, _ := cmd.Flags().GetString(name)
		if v != "" {
			return v
		}
		// Fall back to persistent / inherited value.
		v, _ = cmd.PersistentFlags().GetString(name)
		if v != "" {
			return v
		}
		v, _ = cmd.InheritedFlags().GetString(name)
		return v
	}
	// Cobra stores persistent flags on the parent; walk up to root.
	for c := cmd; c != nil; c = c.Parent() {
		if c.PersistentFlags().Lookup("dir") != nil {
			get = func(name string) string {
				v, _ := cmd.Root().PersistentFlags().GetString(name)
				if v == "" {
					v, _ = cmd.Flags().GetString(name)
				}
				return v
			}
			break
		}
	}
	return GlobalOpts{
		Dir:       get("dir"),
		Library:   get("library"),
		Lang:      get("lang"),
		Config:    get("config"),
		Recipes:   get("recipes"),
		Patches:   get("patches"),
		ModsDir:   get("mods-dir"),
		CheatsDir: get("cheats-dir"),
		Patched:   get("patched-dir"),
	}
}

// app is the fully resolved runtime context for a command.
type app struct {
	cfg     config.Config
	rawLang string
	base    []emulate.Recipe
	patches []emulate.RecipePatch
	recipes []emulate.Recipe
	games   []rom.Game
	fav     *favorites.Store
	play    *playtime.Store
	over    *overrides.Store
}

// resolveConfig merges flags/env/file/defaults and applies CLI overrides
// for paths that config.Resolve does not take as flags.
func resolveConfig(o GlobalOpts) (config.Config, error) {
	cfg, err := config.Resolve(o.Dir, o.Library, o.Lang, o.Config, o.Recipes)
	if err != nil {
		return config.Config{}, err
	}
	if strings.TrimSpace(o.Patches) != "" {
		cfg.Patches = config.ExpandPath(strings.TrimSpace(o.Patches))
	}
	if strings.TrimSpace(o.ModsDir) != "" {
		cfg.ModsDir = config.ExpandPath(strings.TrimSpace(o.ModsDir))
	}
	if strings.TrimSpace(o.CheatsDir) != "" {
		cfg.CheatsDir = config.ExpandPath(strings.TrimSpace(o.CheatsDir))
	}
	if strings.TrimSpace(o.Patched) != "" {
		cfg.Patched = config.ExpandPath(strings.TrimSpace(o.Patched))
	}
	return cfg, nil
}

// loadApp resolves config, loads locales/recipes/stores and refreshes the
// library with a scan (same flow as the TUI startup).
func loadApp(cmd *cobra.Command) (*app, error) {
	if _, err := i18n.LoadDir(i18n.ExternalDir()); err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: locales: %v\n", err)
	}
	o := globalOptsFromCmd(cmd)
	cfg, err := resolveConfig(o)
	if err != nil {
		return nil, err
	}
	i18n.Set(cfg.Lang)

	base, err := emulate.Load(cfg.Recipes)
	if err != nil {
		return nil, fmt.Errorf("recipes: %w", err)
	}
	patches, err := emulate.LoadPatches(cfg.Patches)
	if err != nil {
		return nil, fmt.Errorf("patches: %w", err)
	}
	recipes := emulate.ApplyPatches(base, patches)

	play, err := playtime.Load(playtime.DefaultPath(cfg.Library))
	if err != nil {
		return nil, err
	}
	over, err := overrides.Load(overrides.DefaultPath(cfg.Library))
	if err != nil {
		return nil, err
	}
	fav, err := favorites.Load(favorites.DefaultPath(cfg.Library))
	if err != nil {
		return nil, err
	}

	games, err := refreshLibrary(cfg)
	if err != nil {
		return nil, err
	}

	return &app{
		cfg: cfg, rawLang: rawLang(o.Lang, cfg),
		base: base, patches: patches, recipes: recipes,
		games: games, fav: fav, play: play, over: over,
	}, nil
}

// refreshLibrary loads the stored library, scans the ROM dir (reusing the
// stored rows as hash cache), merges and saves. Scan warnings go to stderr.
// When Steam auto-add is on, new games are synced to shortcuts.vdf.
func refreshLibrary(cfg config.Config) ([]rom.Game, error) {
	stored, err := library.Load(cfg.Library)
	if err != nil {
		return nil, err
	}
	games, errs := rom.ScanDir(cfg.RomDir, stored)
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("main.scan_warn", err))
	}
	games = library.Merge(stored, games)
	if err := library.Save(cfg.Library, games); err != nil {
		return nil, fmt.Errorf("save library: %w", err)
	}
	if cfg.Steam.AutoAdd {
		if err := steam.MaybeAutoAdd(cfg, games); err != nil {
			fmt.Fprintf(os.Stderr, "cartridge: steam auto-add: %v\n", err)
		}
	}
	return games, nil
}

// rawLang remembers the language source for the settings screen:
// flag > env > config file > auto.
func rawLang(flagVal string, cfg config.Config) string {
	if strings.TrimSpace(flagVal) != "" {
		return strings.TrimSpace(flagVal)
	}
	if v := strings.TrimSpace(os.Getenv(config.EnvLang)); v != "" {
		return v
	}
	if cfg.ConfigFile != "" {
		if f, err := config.LoadFile(cfg.ConfigFile); err == nil {
			if strings.TrimSpace(f.Lang) != "" {
				return strings.TrimSpace(f.Lang)
			}
		}
	}
	return "auto"
}

// runPrint is the legacy --print mode: plain library dump, no TUI.
func runPrint(cmd *cobra.Command) error {
	a, err := loadApp(cmd)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	for _, g := range a.games {
		fmt.Fprintf(out, "%s  %-4s  %-60s  %s\n", g.ShortID(), consoleShort(g), g.Title, g.Path)
	}
	return nil
}

// runTUI launches the full-screen shelf (default command).
func runTUI(cmd *cobra.Command, _ []string) error {
	a, err := loadApp(cmd)
	if err != nil {
		return err
	}
	m := ui.New(a.games, a.cfg.RomDir, a.cfg.Library,
		ui.WithRecipes(a.recipes),
		ui.WithBaseRecipes(a.base),
		ui.WithPatches(a.patches),
		ui.WithPaths(a.cfg.ConfigFile, a.cfg.Recipes, a.cfg.Patches, a.cfg.ModsDir, a.cfg.CheatsDir),
		ui.WithPatchedDir(a.cfg.Patched),
		ui.WithLangRaw(a.rawLang),
		ui.WithShowArt(a.cfg.ShowArt),
		ui.WithOpenApps(a.cfg.OpenApps),
		ui.WithSteam(a.cfg.Steam),
		ui.WithPlaytime(a.play),
		ui.WithOverrides(a.over),
		ui.WithFavorites(a.fav))
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

// --- game lookup helpers ---

// matchGames returns games matching query: exact ID, short-ID prefix,
// case-insensitive substring of title / path / console.
func matchGames(games []rom.Game, query string) []rom.Game {
	q := strings.TrimSpace(query)
	if q == "" {
		return games
	}
	lq := strings.ToLower(q)
	var out []rom.Game
	for _, g := range games {
		if strings.EqualFold(g.ID, q) || strings.HasPrefix(strings.ToLower(g.ID), lq) {
			out = append(out, g)
			continue
		}
		if strings.EqualFold(g.ShortID(), q) || strings.HasPrefix(strings.ToLower(g.ShortID()), lq) {
			out = append(out, g)
			continue
		}
		if strings.Contains(strings.ToLower(g.Title), lq) ||
			strings.Contains(strings.ToLower(g.Path), lq) ||
			strings.Contains(strings.ToLower(g.ConsoleID), lq) ||
			strings.Contains(strings.ToLower(g.ConsoleName), lq) {
			out = append(out, g)
		}
	}
	return out
}

// findOneGame resolves query to exactly one game or returns a helpful error.
func findOneGame(games []rom.Game, query string) (rom.Game, error) {
	cands := matchGames(games, query)
	if len(cands) == 0 {
		return rom.Game{}, fmt.Errorf("no game matches %q", query)
	}
	if len(cands) > 1 {
		names := make([]string, 0, 5)
		for i, g := range cands {
			if i >= 5 {
				break
			}
			names = append(names, fmt.Sprintf("%s [%s]", g.Title, g.ShortID()))
		}
		more := ""
		if len(cands) > 5 {
			more = fmt.Sprintf(" (+%d more)", len(cands)-5)
		}
		return rom.Game{}, fmt.Errorf("ambiguous %q, %d matches: %s%s", query, len(cands), strings.Join(names, "; "), more)
	}
	return cands[0], nil
}

// filterGames applies console + favorites + search filters for list-like cmds.
func filterGames(games []rom.Game, console, search string, favOnly bool, fav *favorites.Store) []rom.Game {
	out := games
	if console != "" {
		var kept []rom.Game
		for _, g := range out {
			if strings.EqualFold(g.ConsoleID, console) ||
				strings.EqualFold(rom.ByID(g.ConsoleID).Short, console) ||
				strings.EqualFold(g.ConsoleName, console) {
				kept = append(kept, g)
			}
		}
		out = kept
	}
	if favOnly && fav != nil {
		var kept []rom.Game
		for _, g := range out {
			if fav.Has(g.ID) {
				kept = append(kept, g)
			}
		}
		out = kept
	}
	if search != "" {
		out = matchGames(out, search)
	}
	return out
}

// sortGames sorts in place by key: title (default), console, size, recent.
func sortGames(games []rom.Game, key string) {
	switch strings.ToLower(key) {
	case "console":
		sort.Slice(games, func(i, j int) bool {
			if games[i].ConsoleName != games[j].ConsoleName {
				return games[i].ConsoleName < games[j].ConsoleName
			}
			return games[i].Title < games[j].Title
		})
	case "size":
		sort.Slice(games, func(i, j int) bool { return games[i].Size > games[j].Size })
	case "path":
		sort.Slice(games, func(i, j int) bool { return games[i].Path < games[j].Path })
	default: // title
		sort.Slice(games, func(i, j int) bool {
			if games[i].Title != games[j].Title {
				return games[i].Title < games[j].Title
			}
			return games[i].ConsoleName < games[j].ConsoleName
		})
	}
}

// shortPath trims the ROM dir prefix for compact display.
func shortPath(full, romDir string) string {
	if romDir != "" {
		if rel, err := filepath.Rel(romDir, full); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return full
}
