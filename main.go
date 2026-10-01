// Command cartridge is a retro-ROM shelf with a 3-column TUI:
// consoles | games | metadata, plus a bottom bar.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/library"
	"cartridge/internal/rom"
	"cartridge/internal/ui"
)

func defaultRomDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, "files", "games")
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	for _, p := range []string{"./games", "."} {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return "."
}

func consoleShort(g rom.Game) string {
	for _, c := range rom.Consoles {
		if c.ID == g.ConsoleID {
			return c.Short
		}
	}
	return "???"
}

func main() {
	dir := flag.String("dir", defaultRomDir(), "directory with ROM files to scan")
	lib := flag.String("library", library.DefaultPath(), "path to library database (JSON)")
	printOnly := flag.Bool("print", false, "print scanned library and exit (no TUI)")
	flag.Parse()

	games, errs := rom.ScanDir(*dir)
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "cartridge: scan warning: %v\n", err)
	}

	stored, err := library.Load(*lib)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
	games = library.Merge(stored, games)
	if err := library.Save(*lib, games); err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: save library: %v\n", err)
		os.Exit(1)
	}

	if *printOnly {
		for _, g := range games {
			fmt.Printf("%s  %-4s  %-60s  %s\n", g.ShortID(), consoleShort(g), g.Title, g.Path)
		}
		return
	}

	m := ui.New(games, *dir, *lib)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
}
