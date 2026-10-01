package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	accent   = lipgloss.Color("62")  // violet, focused pane title / selection
	divider  = lipgloss.Color("240") // grey column separators
	dimText  = lipgloss.Color("243")
	brightTx = lipgloss.Color("252")
	idColor  = lipgloss.Color("214") // amber for stable ID
)

var (
	sepStyle    = lipgloss.NewStyle().Foreground(divider)
	titleActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	titleDim    = lipgloss.NewStyle().Bold(true).Foreground(dimText)
	brandStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	infoStyle   = lipgloss.NewStyle().Foreground(dimText)
	selStyle    = lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("230")).Bold(true)
	curStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	metaKey     = lipgloss.NewStyle().Foreground(dimText).Width(9)
	metaVal     = lipgloss.NewStyle().Foreground(brightTx)
	metaID      = lipgloss.NewStyle().Foreground(idColor).Bold(true)
	helpBox     = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(divider).
			Padding(1, 2)
)

func titleFor(name string, focused bool, w int) string {
	st := titleDim
	if focused {
		st = titleActive
	}
	return st.Render(fit(name, w))
}

// truncate cuts s to at most n cells, adding … on overflow.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// fit truncates s to w cells and pads it to exactly w.
func fit(s string, w int) string {
	t := truncate(s, w)
	if pad := w - lipgloss.Width(t); pad > 0 {
		t += strings.Repeat(" ", pad)
	}
	return t
}
