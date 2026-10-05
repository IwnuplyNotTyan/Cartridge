package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"cartridge/internal/i18n"
)

// View: layout and rendering of the browser columns.
const (
	PaneConsoles Pane = iota
	PaneGames
	PaneMeta
)

func (m Model) helpView() string { return m.help.View(m.keys) }

func (m Model) helpHeight() int {
	if !m.showHelp {
		return 0
	}
	return max(lipgloss.Height(m.helpView()), 1)
}

// searchHeight reserves one row for the live search input line.
func (m Model) searchHeight() int {
	if m.searching {
		return 1
	}
	return 0
}

// renderSearch draws the one-line search input above the info bar:
// "/query▊", or a dim hint while empty.
func (m Model) renderSearch() string {
	w := m.contentW()
	text := "/" + string(m.searchBuf) + "▊"
	if len(m.searchBuf) == 0 {
		text += " " + infoStyle.Render(i18n.T("search.hint"))
	}
	return fit(text, w)
}

// contentW/H is the drawable area inside the outer window gap.
func (m Model) contentW() int {
	w := m.width
	if w <= 0 {
		w = 120
	}
	return max(w-2*appGap, 20)
}

func (m Model) contentH() int {
	h := m.height
	if h <= 0 {
		h = 30
	}
	return max(h-2*appGap, 10)
}

func (m Model) bodyH() int {
	return max(m.contentH()-1-m.helpHeight()-m.searchHeight(), 5) // single info-bar line, no slack
}

// Column breakpoints on content width: three panes fit comfortably,
// two panes share the space, one pane takes it all.
const (
	threeColMin = 90
	twoColMin   = 56
)

// visiblePanes returns the panes to draw for the current width and focus.
// Half screens keep consoles+games on the left side and games+metadata
// once a game is in play (games/meta focus); tiny screens show only the
// focused pane. The focused pane is always visible.
func (m Model) visiblePanes() []Pane {
	w := m.contentW()
	if w >= threeColMin {
		return []Pane{PaneConsoles, PaneGames, PaneMeta}
	}
	if w >= twoColMin {
		if m.focus == PaneConsoles {
			return []Pane{PaneConsoles, PaneGames}
		}
		return []Pane{PaneGames, PaneMeta}
	}
	return []Pane{m.focus}
}

// layout splits the content area into column inner widths (no borders,
// columns are separated by divider lines). Hidden panes get width 0.
// Widths account for column padding, so joined rows never overflow.
func (m Model) layout() (cw, gw, mw int) {
	w := m.contentW()
	vis := m.visiblePanes()
	if len(vis) == 3 {
		cw = clamp(w*30/100, 30, 40)
		mw = clamp(w*30/100, 28, 44)
		gw = max(w-cw-mw-2-4, 20) // 2 dividers + padding
		return cw, gw, mw
	}
	if len(vis) == 2 {
		first := clamp(w*38/100, 24, 40)
		second := max(w-first-4, 18) // 1 divider + column padding
		if vis[0] == PaneConsoles {
			return first, second, 0
		}
		return 0, first, second
	}
	// Single pane: keep room for the column padding around it.
	switch vis[0] {
	case PaneConsoles:
		return max(w-1, 10), 0, 0
	case PaneGames:
		return 0, max(w-2, 10), 0
	default:
		return 0, 0, max(w-1, 10)
	}
}

// View implements tea.Model.
// Hovering the settings row swaps the games+metadata panes for the
// settings panel; Enter moves the focus inside, Esc steps back out.
func (m Model) View() string {
	if !m.ready {
		return i18n.T("ui.waiting")
	}
	var body string
	cw, gw, mw := m.layout()
	sep := sepStyle.Render(strings.TrimSuffix(strings.Repeat("│\n", m.bodyH()), "\n"))
	if m.onSettingsRow() {
		sw := gw + mw
		if gw > 0 && mw > 0 {
			sw++ // absorb one divider: the panel spans both panes
		}
		if cw > 0 && sw > 0 {
			body = lipgloss.JoinHorizontal(lipgloss.Top,
				m.renderConsoles(cw), sep, m.column(m.renderSettingsLines(sw), sw, 1, 0))
		} else {
			// Too narrow for two panes: settings take the whole body.
			body = strings.Join(m.renderSettingsLines(m.contentW()), "\n")
		}
	} else {
		parts := make([]string, 0, 5)
		if cw > 0 {
			if len(parts) > 0 {
				parts = append(parts, sep)
			}
			parts = append(parts, m.renderConsoles(cw))
		}
		if gw > 0 {
			if len(parts) > 0 {
				parts = append(parts, sep)
			}
			parts = append(parts, m.renderGames(gw))
		}
		if mw > 0 {
			if len(parts) > 0 {
				parts = append(parts, sep)
			}
			parts = append(parts, m.renderMetaPane(mw))
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}
	var content string
	parts := []string{body}
	if m.searching {
		parts = append(parts, m.renderSearch())
	}
	if m.showHelp {
		parts = append(parts, m.renderHelp())
	}
	parts = append(parts, m.renderInfo())
	content = lipgloss.JoinVertical(lipgloss.Left, parts...)
	return appStyle.Render(content)
}

// column pads plain and pre-styled lines to exactly bodyH rows.
// Lines arrive already fitted to width; padL/padR add air around dividers.
func (m Model) column(lines []string, w, padL, padR int) string {
	_ = w
	l, r := strings.Repeat(" ", padL), strings.Repeat(" ", padR)
	for len(lines) < m.bodyH() {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = l + lines[i] + r
	}
	return strings.Join(lines[:m.bodyH()], "\n")
}

// styledRow highlights a pre-fitted plain row and marks it selected.
func styledRow(plain string) string {
	if len(plain) < 2 {
		return plain
	}
	return curStyle.Render("> ") + selStyle.Render(plain[2:])
}

// barRow marks a pre-fitted plain row with a left accent line instead of "> ".
func barRow(plain string) string {
	if len(plain) < 2 {
		return plain
	}
	return curStyle.Render("│ ") + plain[2:]
}

// styledBarRow highlights a pre-fitted plain row and marks it with a left
// accent line instead of "> ".
func styledBarRow(plain string) string {
	if len(plain) < 2 {
		return plain
	}
	return curStyle.Render("│ ") + selStyle.Render(plain[2:])
}

func (m Model) renderConsoles(w int) string {
	lines := []string{titleFor(i18n.T("pane.consoles"), m.focus == PaneConsoles && !m.settActive, w)}
	// Display order: Home without a count, a --- rule, consoles with
	// counts, another --- rule, the settings entry. Rules are display
	// only; consCur == len(rows) addresses the settings entry.
	type item struct {
		sep bool
		row int // index into m.rows, -1 for the settings entry
	}
	var items []item
	if len(m.rows) > 0 {
		items = append(items, item{row: 0})
		items = append(items, item{sep: true})
		for i := 1; i < len(m.rows); i++ {
			items = append(items, item{row: i})
		}
		items = append(items, item{sep: true})
	}
	items = append(items, item{row: -1})
	cur := 0
	for i, it := range items {
		if it.sep {
			continue
		}
		if (it.row == -1 && m.onSettingsRow()) || (it.row >= 0 && it.row == m.consCur) {
			cur = i
		}
	}
	visible := max(m.bodyH()-1, 1)
	start := clamp(cur-visible/2, 0, max(len(items)-visible, 0))
	for _, it := range items[start:min(start+visible, len(items))] {
		if it.sep {
			lines = append(lines, sepStyle.Render(fit(strings.Repeat("─", w), w)))
			continue
		}
		var plain string
		if it.row == -1 {
			plain = fit("  "+truncate(i18n.T("row.settings"), w-2), w)
			if m.onSettingsRow() {
				plain = styledRow(plain)
			}
		} else {
			r := m.rows[it.row]
			if it.row == 0 {
				// Home is a mode, not a console: no game count.
				plain = fit("  "+truncate(r.Name, w-2), w)
			} else {
				count := fmt.Sprintf("%d", r.Count)
				nameW := max(w-2-1-len(count), 4)
				name := truncate(r.Name, nameW)
				plain = fit("  "+name+strings.Repeat(" ", max(nameW-lipgloss.Width(name)+1, 1))+count, w)
			}
			if it.row == m.consCur {
				plain = styledRow(plain)
			}
		}
		lines = append(lines, plain)
	}
	return m.column(lines, w, 0, 1)
}

func (m Model) renderGames(w int) string {
	title := i18n.T("games.title.all", len(m.filtered))
	if m.consCur < len(m.rows) && m.rows[m.consCur].ID != "all" {
		title = i18n.T("games.title.filter", m.rows[m.consCur].Short, len(m.filtered))
	}
	lines := []string{}
	if len(m.filtered) == 0 {
		if q := strings.TrimSpace(m.activeQuery()); q != "" {
			title = i18n.T("games.title.search", q, len(m.filtered))
		}
		lines = append(lines, titleFor(truncate(title, w), m.focus == PaneGames, w))
		empty := i18n.T("games.empty")
		if strings.TrimSpace(m.activeQuery()) != "" {
			empty = i18n.T("games.empty.search")
		}
		lines = append(lines, infoStyle.Render(fit(empty, w)))
		return m.column(lines, w, 1, 1)
	}
	visible := max(m.bodyH()-1, 1)
	pos := m.shelfPos()
	start := clamp(pos-visible/2, 0, max(len(m.shelf)-visible, 0))
	end := min(start+visible, len(m.shelf))
	// The pane title sticks to the topmost visible section with a 2-row
	// delay, so it does not flicker on every boundary; the search filter
	// wins while querying. Flat category lists keep the plain title.
	if q := strings.TrimSpace(m.activeQuery()); q != "" {
		title = i18n.T("games.title.search", q, len(m.filtered))
	} else if label, ok := m.topSection(start, end); ok {
		title = label
	}
	lines = append(lines, titleFor(truncate(title, w), m.focus == PaneGames, w))
	for i := start; i < end; i++ {
		row := m.shelf[i]
		switch row.kind {
		case shelfBlank:
			lines = append(lines, "")
		case shelfHeader:
			lines = append(lines, titleDim.Render(fit(truncate(row.label, w), w)))
		case shelfHint:
			lines = append(lines, infoStyle.Render(fit("  "+truncate(row.label, w-2), w)))
		default:
			g := m.filtered[row.game]
			mark := ""
			if m.favorites.Has(g.ID) {
				mark = " ★"
			}
			plain := fit("  "+truncate(g.Title, w-2-lipgloss.Width(mark))+mark, w)
			if row.game == m.gameCur {
				plain = styledRow(plain)
			}
			lines = append(lines, plain)
		}
	}
	return m.column(lines, w, 1, 1)
}

func (m Model) renderMetaPane(w int) string {
	lines := []string{titleFor(i18n.T("pane.meta"), m.focus == PaneMeta, w)}
	for _, ln := range strings.Split(m.meta.View(), "\n") {
		lines = append(lines, ln)
	}
	return m.column(lines, w, 1, 0)
}

func dim(s string) string { return infoStyle.Render(s) }

// renderHelp is the bubbles full-help strip right above the bottom bar.
func (m Model) renderHelp() string {
	if !m.showHelp {
		return ""
	}
	return m.help.FullHelpView(m.keys.FullHelp())
}

func (m Model) renderInfo() string {
	w := m.contentW()
	// Full bar with shrinking text budgets while it overflows.
	romB, titleB, statusB := 40, 32, 40
	for {
		left, right := m.barHalves(romB, titleB, statusB)
		if lipgloss.Width(left)+lipgloss.Width(right)+1 <= w {
			return barJoin(w, left, right)
		}
		if romB <= 8 && titleB <= 8 && statusB <= 8 {
			break
		}
		romB, titleB, statusB = max(romB/2, 8), max(titleB/2, 8), max(statusB/2, 8)
	}

	// Compact bar for narrow screens: the middle stats are dropped,
	// the context is truncated to whatever is left.
	brand := brandBadge.Render(" cartridge ")
	helpTxt := i18n.T("bar.help")
	ctxBudget := w - lipgloss.Width(brand) - lipgloss.Width(helpTxt) - 1
	var right string
	if ctxBudget > 4 {
		if m.status != "" {
			right = barStyle.Render(truncate(m.status, ctxBudget))
		} else if g := m.selected(); g != nil {
			sid := g.ShortID()
			if tb := ctxBudget - lipgloss.Width(sid) - 3 - 2; tb >= 4 {
				right = barStyle.Render(sid+" • ") + titleBadge.Render(" "+truncate(g.Title, tb)+" ")
			} else if ctxBudget >= lipgloss.Width(sid) {
				right = barStyle.Render(truncate(sid, ctxBudget))
			}
		}
	}
	help := helpBadge.Render(helpTxt)
	if lipgloss.Width(brand)+1+lipgloss.Width(right)+lipgloss.Width(help) > w {
		// Extreme: even the help badge must shrink to fit.
		help = helpBadge.Render(truncate(helpTxt, max(w-lipgloss.Width(brand)-1-lipgloss.Width(right), 0)))
	}
	return barJoin(w, brand, right+help)
}

// barHalves builds the styled bar halves with shrinking text budgets:
// brand + stats on the left, context + help hint on the right.
func (m Model) barHalves(romB, titleB, statusB int) (string, string) {
	// Весь бар залит серым (barStyle): бейджи остаются цветными модулями,
	// а текст и пустое пространство между ними — на сером фоне.
	left := brandBadge.Render(" cartridge ") +
		barStyle.Render("  "+i18n.T("bar.stats",
			len(m.games), pluralWord(len(m.games), "word.rom"),
			len(m.rows)-1, pluralWord(len(m.rows)-1, "word.console"),
			truncate(m.romDir, romB)))

	var context string
	if m.status != "" {
		context = barStyle.Render(truncate(m.status, statusB))
	} else if g := m.selected(); g != nil {
		context = barStyle.Render(g.ShortID()+" • ") +
			titleBadge.Render(" "+truncate(g.Title, titleB)+" ")
	}
	right := context + helpBadge.Render(i18n.T("bar.help"))
	return left, right
}

// barJoin glues the halves with a grey gap, exactly w cells wide.
func barJoin(w int, left, right string) string {
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	bar := left + barStyle.Render(strings.Repeat(" ", gap)) + right
	// Добиваем бар серым до полной ширины, чтобы не осталось незакрашенных хвостов.
	if pad := w - lipgloss.Width(bar); pad > 0 {
		bar += barStyle.Render(strings.Repeat(" ", pad))
	}
	return bar
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
