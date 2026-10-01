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
	"cartridge/internal/i18n"
	"cartridge/internal/library"
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

func main() {
	// Preliminary language so --help itself is localized.
	i18n.Set(usageLang())

	dir := flag.String("dir", "", i18n.T("flag.dir"))
	lib := flag.String("library", "", i18n.T("flag.library"))
	lang := flag.String("lang", "", i18n.T("flag.lang"))
	cfgPath := flag.String("config", "", i18n.T("flag.config"))
	printOnly := flag.Bool("print", false, i18n.T("flag.print"))
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s\n%s\n\n",
			i18n.T("flag.usage.head"), i18n.T("flag.usage.src"))
		flag.PrintDefaults()
	}
	flag.Parse()

	cfg, err := config.Resolve(*dir, *lib, *lang, *cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
	i18n.Set(cfg.Lang)

	games, errs := rom.ScanDir(cfg.RomDir)
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("main.scan_warn", err))
	}

	stored, err := library.Load(cfg.Library)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
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

	m := ui.New(games, cfg.RomDir, cfg.Library)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
}
