package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/charmtone"
)

// appGap is the outer gap (in cells) around the whole window.
const appGap = 1

// tone maps a CharmTone key onto a lipgloss color.
func tone(k charmtone.Key) lipgloss.Color { return lipgloss.Color(k.Hex()) }

var (
	accent   = tone(charmtone.Charple) // focused pane title / selection
	divider  = tone(charmtone.Iron)    // column separators
	dimText  = tone(charmtone.Squid)
	brightTx = tone(charmtone.Salt)
	idColor  = tone(charmtone.Tang) // stable ID
)

// metaKeyWidth fits the longest meta label ("achievements") plus a gap.
const metaKeyWidth = 13

var (
	appStyle    = lipgloss.NewStyle().Margin(appGap)
	sepStyle    = lipgloss.NewStyle().Foreground(divider)
	titleActive = lipgloss.NewStyle().Bold(true).Foreground(tone(charmtone.Dolly))
	titleDim    = lipgloss.NewStyle().Bold(true).Foreground(dimText)
	brandBadge  = lipgloss.NewStyle().Foreground(tone(charmtone.Yam)).Background(tone(charmtone.Plum))
	titleBadge  = lipgloss.NewStyle().Foreground(tone(charmtone.Butter)).Background(tone(charmtone.Grape))
	helpBadge   = lipgloss.NewStyle().Foreground(lipgloss.Color("#767676")).Background(tone(charmtone.Char))
	barStyle    = lipgloss.NewStyle().Foreground(tone(charmtone.Smoke)).Background(lipgloss.Color("#262626"))
	infoStyle   = lipgloss.NewStyle().Foreground(dimText)
	pathIdle    = lipgloss.NewStyle().Foreground(dimText).Italic(true)
	selStyle    = lipgloss.NewStyle().Background(accent).Foreground(tone(charmtone.Butter)).Bold(true)
	curStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
	metaKey     = lipgloss.NewStyle().Foreground(dimText).Width(metaKeyWidth)
	metaVal     = lipgloss.NewStyle().Foreground(brightTx)
	metaID      = lipgloss.NewStyle().Foreground(idColor).Bold(true)
)

// idlePathRow renders "  Label: value" fitted to w cells where only the
// path value is grey + italic; the label name stays plain.
func idlePathRow(label, val string, w int) string {
	prefix := "  " + label + ": "
	valW := w - lipgloss.Width(prefix)
	if valW < 1 {
		valW = 1
	}
	line := prefix + pathIdle.Render(truncate(val, valW))
	if pad := w - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

func titleFor(name string, focused bool, w int) string {
	st := titleDim
	if focused {
		st = titleActive
	}
	return st.Render(fit(name, w))
}

// truncate cuts s to at most n cells, adding … on overflow.
// Width-aware: wide runes count by cell width, so CJK/emoji never
// break the column layout the way a hard rune-count cut would.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > n-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// fit truncates s to w cells and pads it to exactly w.
func fit(s string, w int) string {
	t := truncate(s, w)
	if pad := w - lipgloss.Width(t); pad > 0 {
		t += strings.Repeat(" ", pad)
	}
	return t
}

// wrapSoft word-wraps s into lines of at most w cells.
// Words longer than w are split only as a last resort, so unlike a hard
// rune-count cut no text is ever replaced by "…" and lost.
func wrapSoft(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	var out []string
	var line strings.Builder
	lineW := 0
	flush := func() {
		out = append(out, line.String())
		line.Reset()
		lineW = 0
	}
	for _, word := range strings.Fields(s) {
		for lipgloss.Width(word) > w {
			if lineW > 0 {
				flush()
			}
			head, tail := cutWord(word, w)
			out = append(out, head)
			word = tail
		}
		ww := lipgloss.Width(word)
		switch {
		case lineW == 0:
			line.WriteString(word)
			lineW = ww
		case lineW+1+ww <= w:
			line.WriteString(" " + word)
			lineW += 1 + ww
		default:
			flush()
			line.WriteString(word)
			lineW = ww
		}
	}
	if lineW > 0 || len(out) == 0 {
		out = append(out, line.String())
	}
	return out
}

// cutWord splits word into a head of at most w cells and the tail.
func cutWord(word string, w int) (head, tail string) {
	used := 0
	for i, r := range word {
		rw := lipgloss.Width(string(r))
		if used+rw > w {
			return word[:i], word[i:]
		}
		used += rw
	}
	return word, ""
}
