// Command cartridge is a retro-ROM shelf with a 3-column TUI:
// consoles | games | metadata, plus a bottom bar.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/config"
	"cartridge/internal/emulate"
	"cartridge/internal/favorites"
	"cartridge/internal/i18n"
	"cartridge/internal/library"
	"cartridge/internal/overrides"
	"cartridge/internal/playtime"
	"cartridge/internal/rom"
	"cartridge/internal/ui"
)

func consoleShort(g rom.Game) string {
	for _, c := range rom.Consoles {
		if c.ID == g.ConsoleID {
			return c.Short
		}
	}
	return "???"
}

// usageLang pre-detects the language for --help output:
// an explicit --lang value wins, then $CARTRIDGE_LANG, then the locale.
func usageLang() i18n.Lang {
	args := os.Args[1:]
	for i, a := range args {
		var v string
		switch {
		case a == "--lang" || a == "-lang":
			if i+1 < len(args) {
				v = args[i+1]
			}
		case strings.HasPrefix(a, "--lang="):
			v = strings.TrimPrefix(a, "--lang=")
		case strings.HasPrefix(a, "-lang="):
			v = strings.TrimPrefix(a, "-lang=")
		default:
			continue
		}
		if l := i18n.Parse(v); l != i18n.Auto {
			return l
		}
		return i18n.Detect()
	}
	if v := os.Getenv(config.EnvLang); v != "" {
		if l := i18n.Parse(v); l != i18n.Auto {
			return l
		}
	}
	return i18n.Detect()
}

// rawLang remembers the language source for the settings screen:
// flag > env > config file > auto. The resolved cfg.Lang is already
// effective; the raw string keeps the "auto" choice visible.
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

func main() {
	// Extra languages: drop de.json into the locales dir and it appears
	// in the language tab with no rebuild. Broken files warn, never kill.
	if _, err := i18n.LoadDir(i18n.ExternalDir()); err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: locales: %v\n", err)
	}
	// Preliminary language so --help itself is localized.
	i18n.Set(usageLang())

	dir := flag.String("dir", "", i18n.T("flag.dir"))
	lib := flag.String("library", "", i18n.T("flag.library"))
	lang := flag.String("lang", "", i18n.T("flag.lang"))
	cfgPath := flag.String("config", "", i18n.T("flag.config"))
	recipesPath := flag.String("recipes", "", i18n.T("flag.recipes"))
	patchesPath := flag.String("patches", "", i18n.T("flag.patches"))
	recipeExample := flag.Bool("recipe-example", false, i18n.T("flag.recipe_example"))
	printOnly := flag.Bool("print", false, i18n.T("flag.print"))
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s\n%s\n\n",
			i18n.T("flag.usage.head"), i18n.T("flag.usage.src"))
		flag.PrintDefaults()
	}
	flag.Parse()

	if *recipeExample {
		fmt.Printf("%s", emulate.ExampleJSON())
		return
	}

	cfg, err := config.Resolve(*dir, *lib, *lang, *cfgPath, *recipesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
	if strings.TrimSpace(*patchesPath) != "" {
		cfg.Patches = config.ExpandPath(strings.TrimSpace(*patchesPath))
	}
	i18n.Set(cfg.Lang)

	base, err := emulate.Load(cfg.Recipes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("main.recipes_fail", err))
		os.Exit(1)
	}
	patches, err := emulate.LoadPatches(cfg.Patches)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("main.recipes_fail", err))
		os.Exit(1)
	}
	recipes := emulate.ApplyPatches(base, patches)
	play, err := playtime.Load(playtime.DefaultPath(cfg.Library))
	if err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
	over, err := overrides.Load(overrides.DefaultPath(cfg.Library))
	if err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
	fav, err := favorites.Load(favorites.DefaultPath(cfg.Library))
	if err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}

	stored, err := library.Load(cfg.Library)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}

	// The stored library doubles as the scan cache: untouched files reuse
	// their hashed entry instead of being re-read every start.
	games, errs := rom.ScanDir(cfg.RomDir, stored)
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("main.scan_warn", err))
	}

	games = library.Merge(stored, games)
	if err := library.Save(cfg.Library, games); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("main.save_fail", err))
		os.Exit(1)
	}

	if *printOnly {
		for _, g := range games {
			fmt.Printf("%s  %-4s  %-60s  %s\n", g.ShortID(), consoleShort(g), g.Title, g.Path)
		}
		return
	}

	m := ui.New(games, cfg.RomDir, cfg.Library,
		ui.WithRecipes(recipes),
		ui.WithBaseRecipes(base),
		ui.WithPatches(patches),
		ui.WithPaths(cfg.ConfigFile, cfg.Recipes, cfg.Patches, cfg.ModsDir, cfg.SavesDir, cfg.CheatsDir),
		ui.WithPatchedDir(cfg.Patched),
		ui.WithLangRaw(rawLang(*lang, cfg)),
		ui.WithPlaytime(play),
		ui.WithOverrides(over),
		ui.WithFavorites(fav))
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
}
