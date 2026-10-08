package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"

	"cartridge/internal/favorites"
	"cartridge/internal/rom"
)

// cprintf writes command output to stdout (cobra's cmd.Printf targets
// stderr, which breaks pipes like `cartridge list | head`).
func cprintf(cmd *cobra.Command, format string, a ...any) {
	fmt.Fprintf(cmd.OutOrStdout(), format, a...)
}

var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	keyStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	valueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	okStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("84"))
	warnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	errStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	favStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	consoleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("147"))
	idStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

func banner(title, subtitle string) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(title))
	if subtitle != "" {
		b.WriteString("\n" + dimStyle.Render(subtitle))
	}
	return b.String()
}

func kv(k, v string) string {
	return fmt.Sprintf("%s  %s", keyStyle.Render(k+":"), valueStyle.Render(v))
}

func ok(s string) string      { return okStyle.Render("✓ " + s) }
func warn(s string) string    { return warnStyle.Render("! " + s) }
func errMark(s string) string { return errStyle.Render("✗ " + s) }

// consoleShort returns the short console tag (NES, GBA, …).
func consoleShort(g rom.Game) string {
	for _, c := range rom.Consoles {
		if c.ID == g.ConsoleID {
			return c.Short
		}
	}
	return "???"
}

// formatSize renders bytes as human size.
func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	for _, u := range []string{"KB", "MB", "GB"} {
		f /= 1024
		if f < 1024 {
			return fmt.Sprintf("%.1f %s", f, u)
		}
	}
	return fmt.Sprintf("%.1f TB", f/1024)
}

func truncate(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	out := runewidth.Truncate(s, w-1, "")
	return out + "…"
}

// printGameTable renders a pretty aligned table of games.
func printGameTable(w io.Writer, games []rom.Game, fav *favorites.Store, romDir string) {
	if len(games) == 0 {
		fmt.Fprintln(w, dimStyle.Render("No games found."))
		return
	}
	// Column widths adapt to content (capped).
	tw, pw := 0, 0
	for _, g := range games {
		if n := runewidth.StringWidth(g.Title); n > tw {
			tw = n
		}
		if n := runewidth.StringWidth(shortPath(g.Path, romDir)); n > pw {
			pw = n
		}
	}
	if tw > 44 {
		tw = 44
	}
	if pw > 44 {
		pw = 44
	}
	if tw < 12 {
		tw = 12
	}
	head := fmt.Sprintf("%s  %s  %s  %s  %s",
		dimStyle.Render("SHORTID     "),
		consoleStyle.Render(fmt.Sprintf("%-5s", "SYS")),
		titleStyle.Render(pad("TITLE", tw)),
		dimStyle.Render(pad("PATH", pw)),
		dimStyle.Render("SIZE"),
	)
	fmt.Fprintln(w, head)
	for _, g := range games {
		star := " "
		if fav != nil && fav.Has(g.ID) {
			star = favStyle.Render("★")
		}
		fmt.Fprintf(w, "%s%s  %s  %s  %s  %s\n",
			star,
			idStyle.Render(g.ShortID()),
			consoleStyle.Render(fmt.Sprintf("%-5s", consoleShort(g))),
			truncate(g.Title, tw)+strings.Repeat(" ", max(0, tw-runewidth.StringWidth(truncate(g.Title, tw)))),
			dimStyle.Render(truncate(shortPath(g.Path, romDir), pw)+strings.Repeat(" ", max(0, pw-runewidth.StringWidth(truncate(shortPath(g.Path, romDir), pw))))),
			dimStyle.Render(formatSize(g.Size)),
		)
	}
	fmt.Fprintln(w, dimStyle.Render(fmt.Sprintf("— %d game(s) —", len(games))))
}

func pad(s string, w int) string {
	n := runewidth.StringWidth(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}
