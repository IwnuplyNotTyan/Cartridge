package ui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	accent   = lipgloss.Color("62")  // violet, focused pane
	dimEdge  = lipgloss.Color("240") // unfocused border
	dimText  = lipgloss.Color("243")
	brightTx = lipgloss.Color("252")
	barBg    = lipgloss.Color("235")
	barFg    = lipgloss.Color("252")
	idColor  = lipgloss.Color("214") // amber for stable ID
)

func paneStyle(focused bool, w, h int) lipgloss.Style {
	edge := dimEdge
	if focused {
		edge = accent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(edge).
		Width(w).
		Height(h).
		Padding(0, 1)
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	headerDim   = lipgloss.NewStyle().Foreground(dimText)
	barStyle    = lipgloss.NewStyle().Background(barBg).Foreground(barFg)
	selStyle    = lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("230")).Bold(true)
	curStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	metaKey     = lipgloss.NewStyle().Foreground(dimText).Width(9)
	metaVal     = lipgloss.NewStyle().Foreground(brightTx)
	metaID      = lipgloss.NewStyle().Foreground(idColor).Bold(true)
	paneTitle   = lipgloss.NewStyle().Bold(true).Foreground(brightTx)
)

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
