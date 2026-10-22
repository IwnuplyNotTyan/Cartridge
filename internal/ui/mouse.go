package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Mouse support: click to select/focus, double-click to activate,
// wheel to scroll, right-click to launch (on a game) or go back.
//
// Single left-click selects the row under the cursor and moves the
// focus into its pane (titles focus without selecting). A second
// left-click on the same row within doubleClickInterval activates it:
// games launch (x), settings rows open (enter), metadata rows expand
// or copy (enter), the settings entry opens the panel. Right-click on
// a game launches it at once; right-click elsewhere steps back like
// esc (collapse lists, leave settings, close the log screen — the
// game keeps running). The wheel scrolls the pane under the cursor,
// or the focused pane when the cursor is outside the columns.
const (
	doubleClickInterval = 500 * time.Millisecond
	wheelStep           = 3
)

// consItem is one consoles-column entry: a console row (row >= 0),
// the settings entry (row == -1), a separator or the empty hint.
type consItem struct {
	sep  bool
	hint bool
	row  int
}

// consoleItems rebuilds the consoles display order: Home, a rule,
// consoles, a rule, the settings entry. Mirrors renderConsoles.
func (m Model) consoleItems() []consItem {
	var items []consItem
	if len(m.rows) > 0 {
		items = append(items, consItem{row: 0})
		items = append(items, consItem{sep: true})
		for i := 1; i < len(m.rows); i++ {
			items = append(items, consItem{row: i})
		}
		if len(m.rows) == 1 {
			items = append(items, consItem{hint: true})
		}
		items = append(items, consItem{sep: true})
	}
	return append(items, consItem{row: -1})
}

// consoleCurPos returns the display index of the consoles cursor
// (the settings entry counts as a row).
func (m Model) consoleCurPos(items []consItem) int {
	for i, it := range items {
		if it.sep || it.hint {
			continue
		}
		if (it.row == -1 && m.onSettingsRow()) || (it.row >= 0 && it.row == m.consCur) {
			return i
		}
	}
	return 0
}

// gamesWindow returns the visible shelf slice [start, end) exactly
// like renderGames does (cursor-centered window of bodyH-1 rows).
func (m Model) gamesWindow() (start, end int) {
	visible := max(m.bodyH()-1, 1)
	if len(m.shelf) == 0 {
		return 0, 0
	}
	pos := m.shelfPos()
	start = clamp(pos-visible/2, 0, max(len(m.shelf)-visible, 0))
	end = min(start+visible, len(m.shelf))
	return start, end
}

// mouseLoc names the body area under the cursor.
type mouseLoc int

const (
	locNone mouseLoc = iota
	locConsoles
	locGames
	locMeta
	locSettings
	locRun
)

// locate maps terminal (x, y) onto a body location. paneX is the
// cursor offset inside the located span (for the settings tab strip),
// bodyY is the row inside the body (0 is the pane title).
func (m Model) locate(x, y int) (loc mouseLoc, paneX, bodyY int) {
	cx, cy := x-appGap, y-appGap
	if cx < 0 || cy < 0 || cx >= m.contentW() || cy >= m.contentH() {
		return locNone, 0, 0
	}
	if cy >= m.bodyH() {
		return locNone, 0, 0 // search/help/info bar: no mouse action
	}
	if m.runView {
		return locRun, cx, cy
	}
	if m.onSettingsRow() {
		cw, gw, mw := m.layout()
		sw := gw + mw
		if gw > 0 && mw > 0 {
			sw++ // settings panel absorbs one divider
		}
		if cw > 0 && sw > 0 {
			if cx < cw+1 {
				return locConsoles, cx, cy
			}
			if cx == cw+1 {
				return locNone, 0, 0 // divider
			}
			return locSettings, cx - (cw + 2), cy
		}
		return locSettings, cx, cy // too narrow: settings take it all
	}
	cw, gw, mw := m.layout()
	vis := m.visiblePanes()
	cur := 0
	for i, p := range vis {
		var inner int
		switch p {
		case PaneConsoles:
			inner = cw
		case PaneGames:
			inner = gw
		default:
			inner = mw
		}
		pad := 1
		if p == PaneGames {
			pad = 2
		}
		spanW := inner + pad
		if spanW > 0 && cx >= cur && cx < cur+spanW {
			switch p {
			case PaneConsoles:
				return locConsoles, cx - cur, cy
			case PaneGames:
				return locGames, cx - cur, cy
			default:
				return locMeta, cx - cur, cy
			}
		}
		cur += spanW
		if i < len(vis)-1 {
			if cx == cur {
				return locNone, 0, 0 // divider
			}
			cur++
		}
	}
	return locNone, 0, 0
}

// updateMouse handles tea.MouseMsg: left-click select, double-click
// activate, wheel scroll, right-click launch/back. Motion and release
// events are ignored; inline settings editing swallows every click so
// the edit buffer is never lost by accident.
func (m Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.ready {
		return m, nil
	}
	if m.settEditing {
		return m, nil
	}
	if tea.MouseEvent(msg).IsWheel() {
		up := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelLeft
		return m.mouseWheel(msg.X, msg.Y, up)
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		return m.mouseLeft(msg.X, msg.Y)
	case tea.MouseButtonRight:
		return m.mouseRight(msg.X, msg.Y)
	default:
		return m, nil
	}
}

// mouseLeft selects on single click, activates on double-click.
func (m Model) mouseLeft(x, y int) (tea.Model, tea.Cmd) {
	loc, paneX, bodyY := m.locate(x, y)
	switch loc {
	case locRun:
		return m, nil // log screen: wheel scrolls, esc/right-click leaves
	case locConsoles:
		return m.mouseLeftConsoles(bodyY)
	case locGames:
		return m.mouseLeftGames(bodyY)
	case locMeta:
		return m.mouseLeftMeta(bodyY)
	case locSettings:
		return m.mouseLeftSettings(paneX, bodyY)
	default:
		return m, nil
	}
}

// click consumes the double-click tracker: it reports whether id
// repeats the previous click fast enough, then records id as latest.
func (m *Model) click(id string) bool {
	dbl := m.mouseLastID == id && !m.mouseLastAt.IsZero() &&
		time.Since(m.mouseLastAt) < doubleClickInterval
	m.mouseLastID = id
	m.mouseLastAt = time.Now()
	if dbl {
		m.mouseLastID = "" // consume: a triple click starts over
	}
	return dbl
}

func (m Model) mouseLeftConsoles(bodyY int) (tea.Model, tea.Cmd) {
	items := m.consoleItems()
	if bodyY == 0 {
		if m.settActive {
			m.closeSettings()
		}
		m.setFocus(PaneConsoles)
		m.mouseLastID = ""
		return m, nil
	}
	visible := max(m.bodyH()-1, 1)
	cur := m.consoleCurPos(items)
	start := clamp(cur-visible/2, 0, max(len(items)-visible, 0))
	idx := start + (bodyY - 1)
	if bodyY < 1 || idx < 0 || idx >= len(items) || idx < start || idx >= start+visible {
		if m.settActive {
			m.closeSettings()
		}
		m.setFocus(PaneConsoles)
		m.mouseLastID = ""
		return m, nil
	}
	it := items[idx]
	if it.sep || it.hint {
		if !m.settActive {
			m.setFocus(PaneConsoles)
		}
		m.mouseLastID = ""
		return m, nil
	}
	if it.row == -1 {
		if m.click("cons:settings") {
			m.consCur = len(m.rows)
			m.gameCur = 0
			m.refilter()
			m.openSettings()
			return m, nil
		}
		m.consCur = len(m.rows)
		m.gameCur = 0
		m.refilter()
		m.setFocus(PaneConsoles)
		return m, nil
	}
	id := fmt.Sprintf("cons:%d", it.row)
	if m.click(id) {
		m.consCur = it.row
		m.gameCur = 0
		m.refilter()
		if m.settActive {
			m.closeSettings()
		}
		m.setFocus(PaneGames)
		return m, nil
	}
	if m.settActive {
		m.closeSettings()
	}
	m.consCur = it.row
	m.gameCur = 0
	m.refilter()
	m.setFocus(PaneConsoles)
	return m, nil
}

func (m Model) mouseLeftGames(bodyY int) (tea.Model, tea.Cmd) {
	if m.onSettingsRow() {
		return m, nil
	}
	if bodyY == 0 {
		m.setFocus(PaneGames)
		m.mouseLastID = ""
		return m, nil
	}
	start, end := m.gamesWindow()
	idx := start + (bodyY - 1)
	if bodyY < 1 || idx < start || idx >= end {
		m.setFocus(PaneGames)
		m.mouseLastID = ""
		return m, nil
	}
	row := m.shelf[idx]
	if row.kind != shelfGame {
		m.setFocus(PaneGames)
		m.mouseLastID = ""
		return m, nil
	}
	id := fmt.Sprintf("game:%d", row.game)
	if m.click(id) {
		m.selectGame(row.game)
		m.setFocus(PaneGames)
		return m.launchSelected()
	}
	m.selectGame(row.game)
	m.setFocus(PaneGames)
	return m, nil
}

func (m Model) mouseLeftMeta(bodyY int) (tea.Model, tea.Cmd) {
	if bodyY == 0 {
		m.setFocus(PaneMeta)
		m.mouseLastID = ""
		return m, nil
	}
	_, starts := m.renderMetaRows()
	if len(m.metaRows) == 0 || len(starts) == 0 {
		m.setFocus(PaneMeta)
		m.mouseLastID = ""
		return m, nil
	}
	line := m.meta.YOffset + (bodyY - 1)
	if line < 0 {
		m.setFocus(PaneMeta)
		m.mouseLastID = ""
		return m, nil
	}
	rowIdx := -1
	for i, s := range starts {
		var next int
		if i+1 < len(starts) {
			next = starts[i+1]
		} else {
			// total visual lines: every row wrote ≥1 line
			next = s + max(len(m.metaRows[i].lines), 1)
			if m.metaRows[i].sep {
				next = s + 1
			}
		}
		if line >= s && line < next {
			rowIdx = i
			break
		}
	}
	if rowIdx < 0 || rowIdx >= len(m.metaRows) {
		m.setFocus(PaneMeta)
		m.mouseLastID = ""
		return m, nil
	}
	if m.metaRows[rowIdx].sep {
		m.setFocus(PaneMeta)
		m.mouseLastID = ""
		return m, nil
	}
	id := fmt.Sprintf("meta:%d", rowIdx)
	if m.click(id) {
		m.metaCur = rowIdx
		m.setFocus(PaneMeta)
		m.refreshMeta()
		return m.metaSelect()
	}
	m.metaCur = rowIdx
	m.setFocus(PaneMeta)
	m.refreshMeta()
	return m, nil
}

// mouseRight launches a game under the cursor, otherwise steps back.
func (m Model) mouseRight(x, y int) (tea.Model, tea.Cmd) {
	m.mouseLastID = ""
	loc, _, bodyY := m.locate(x, y)
	if loc == locRun {
		m.runView = false
		return m, nil
	}
	if loc == locGames && bodyY > 0 && !m.onSettingsRow() {
		start, end := m.gamesWindow()
		if idx := start + (bodyY - 1); idx >= start && idx < end {
			if row := m.shelf[idx]; row.kind == shelfGame {
				m.selectGame(row.game)
				m.setFocus(PaneGames)
				return m.launchSelected()
			}
		}
	}
	return m.mouseBack()
}

// mouseBack is the right-click fallback: the esc ladder without
// touching the consoles cursor (collapse, leave settings, home).
func (m Model) mouseBack() (tea.Model, tea.Cmd) {
	if m.cheatsOpen || m.modsOpen || m.artOpen {
		m.cheatsOpen = false
		m.modsOpen = false
		m.artOpen = false
		m.refreshMeta()
		return m, nil
	}
	if m.settActive {
		m.closeSettings()
		return m, nil
	}
	if m.onSettingsRow() {
		m.leaveSettingsRow()
		return m, nil
	}
	m.setFocus(PaneGames)
	return m, nil
}

// mouseWheel scrolls the pane under the cursor (or the focused pane
// outside the columns) by wheelStep rows.
func (m Model) mouseWheel(x, y int, up bool) (tea.Model, tea.Cmd) {
	d := wheelStep
	if up {
		d = -d
	}
	loc, _, _ := m.locate(x, y)
	if loc == locNone {
		if m.runView {
			loc = locRun
		} else if m.settActive || m.onSettingsRow() {
			loc = locSettings
		} else {
			switch m.focus {
			case PaneConsoles:
				loc = locConsoles
			case PaneMeta:
				loc = locMeta
			default:
				loc = locGames
			}
		}
	}
	switch loc {
	case locRun:
		m.followRun = false
		m.runScroll = clamp(m.runScroll+d, 0, m.runMaxScroll())
		if m.runScroll >= m.runMaxScroll() {
			m.followRun = true
		}
		return m, nil
	case locConsoles:
		items := m.consoleItems()
		_ = items
		if m.settActive {
			m.closeSettings()
		}
		m.consCur = clamp(m.consCur+d, 0, len(m.rows))
		m.gameCur = 0
		m.refilter()
		m.setFocus(PaneConsoles)
		return m, nil
	case locGames:
		if m.onSettingsRow() || len(m.filtered) == 0 {
			return m, nil
		}
		m.selectGame(clamp(m.gameCur+d, 0, len(m.filtered)-1))
		m.setFocus(PaneGames)
		return m, nil
	case locMeta:
		m.setFocus(PaneMeta)
		m.metaMove(d)
		return m, nil
	case locSettings:
		return m.mouseWheelSettings(d), nil
	default:
		return m, nil
	}
}

// settingsTabAt maps a settings-panel X offset onto a tab index.
// Titles are left-aligned and joined by " | " like the render does;
// clicks on separators and past the end are ignored.
func (m Model) settingsTabAt(paneX int) (SettingsTab, bool) {
	x := paneX - 1 // settings lines carry 1 cell of column padding
	if x < 0 {
		return 0, false
	}
	pos := 0
	for t := SettingsTab(0); t < NumSettingsTabs; t++ {
		w := lipgloss.Width(tabTitle(t))
		if x >= pos && x < pos+w {
			return t, true
		}
		pos += w + lipgloss.Width(" | ")
	}
	return 0, false
}

func (m Model) mouseLeftSettings(paneX, bodyY int) (tea.Model, tea.Cmd) {
	if bodyY == 0 {
		if t, ok := m.settingsTabAt(paneX); ok {
			m.settTab = t
			m.settCur = 0
			m.settField = -1
			if !m.settActive {
				m.openSettings()
			}
			if t == TabIcons {
				m.refreshIconsStat()
			}
		} else if !m.settActive {
			m.openSettings()
		}
		m.mouseLastID = ""
		return m, nil
	}
	if bodyY == 1 {
		if !m.settActive {
			m.openSettings()
		}
		m.mouseLastID = ""
		return m, nil
	}
	switch m.settTab {
	case TabGeneral:
		return m.mouseSettingsGeneral(bodyY - 2)
	case TabPaths:
		return m.mouseSettingsSimple(bodyY-2, len(pathEntries()))
	case TabIcons:
		return m.mouseSettingsIcons(bodyY - 2)
	case TabSteam:
		return m.mouseSettingsSteam(bodyY - 2)
	default:
		return m.mouseSettingsRecipes(bodyY - 2)
	}
}

// mouseSettingsGeneral selects a General tab row: art, languages, then
// the Advanced switches. Blank lines and section strips only take focus.
// Double-click activates like Enter.
func (m Model) mouseSettingsGeneral(vis int) (tea.Model, tea.Cmd) {
	langStart, langEnd := 4, 4+langRowCount()
	openLine := langEnd + 3
	switch {
	case vis == 0:
		return m.mouseSettingsSimple(0, m.generalRows())
	case vis >= langStart && vis < langEnd:
		return m.mouseSettingsSimple(vis-3, m.generalRows())
	case vis == openLine:
		return m.mouseSettingsSimple(openAppsRow(), m.generalRows())
	default:
		if !m.settActive {
			m.openSettings()
		}
		m.mouseLastID = ""
		return m, nil
	}
}

// mouseSettingsSimple selects row vis (single click) and activates it
// like Enter on double-click. Used by fixed one-line-per-row tabs.
func (m Model) mouseSettingsSimple(vis, n int) (tea.Model, tea.Cmd) {
	if vis < 0 || vis >= n {
		if !m.settActive {
			m.openSettings()
		}
		m.mouseLastID = ""
		return m, nil
	}
	id := fmt.Sprintf("sett:%d:%d", m.settTab, vis)
	if !m.settActive {
		m.openSettings()
		m.settCur = vis
		m.mouseLastID = id
		m.mouseLastAt = time.Now()
		return m, nil
	}
	if m.click(id) {
		m.settCur = vis
		return m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	}
	m.settCur = vis
	return m, nil
}

func (m Model) mouseSettingsIcons(vis int) (tea.Model, tea.Cmd) {
	if vis < 0 || vis >= iconsRowsFor() {
		if !m.settActive {
			m.openSettings()
		}
		m.mouseLastID = ""
		return m, nil
	}
	return m.mouseSettingsSimple(vis, iconsRowsFor())
}

func (m Model) mouseSettingsSteam(vis int) (tea.Model, tea.Cmd) {
	// Content lines interleave divider blocks (blank + strip + blank)
	// before the consoles section and before the sync row: walk logical
	// rows and match. Strips and blanks only focus, rows activate.
	focusOnly := func() (tea.Model, tea.Cmd) {
		m.mouseLastID = ""
		if !m.settActive {
			m.openSettings()
		}
		return m, nil
	}
	n := steamRowsFor(m)
	hasCons := len(m.steamConsoleRows()) > 0
	pos := 0
	for i := 0; i < n; i++ {
		if i == steamRowConsoles && hasCons {
			if vis == pos || vis == pos+1 || vis == pos+2 {
				return focusOnly() // divider block: focus only
			}
			pos += 3
		}
		if m.steamSyncRow(i) {
			if vis == pos || vis == pos+1 || vis == pos+2 {
				return focusOnly()
			}
			pos += 3
		}
		if vis == pos {
			return m.mouseSettingsSimple(i, n)
		}
		pos++
	}
	if !m.settActive {
		m.openSettings()
	}
	m.mouseLastID = ""
	return m, nil
}

func (m Model) mouseSettingsRecipes(vis int) (tea.Model, tea.Cmd) {
	if len(m.baseRecipes) == 0 {
		m.mouseLastID = ""
		if !m.settActive {
			m.openSettings()
		}
		return m, nil
	}
	// Content: head, keys, blank, then recipe rows with the selected
	// recipe's detail rows inline underneath it.
	if vis < 3 {
		if !m.settActive {
			m.openSettings()
		}
		m.mouseLastID = ""
		return m, nil
	}
	vis -= 3
	fields := recipeFields()
	for i := range m.baseRecipes {
		if vis == 0 {
			id := fmt.Sprintf("sett:recipes:%d:-1", i)
			if !m.settActive {
				m.openSettings()
				m.settCur = i
				m.settField = -1
				m.mouseLastID = id
				m.mouseLastAt = time.Now()
				return m, nil
			}
			if m.settCur != i {
				m.settCur = i
				m.settField = -1
				m.mouseLastID = id
				m.mouseLastAt = time.Now()
				return m, nil
			}
			if m.click(id) {
				// Second click on the open header collapses,
				// on a closed one opens the details.
				if m.settField == -1 {
					m.settField = 0
				} else {
					m.settField = -1
				}
				return m, nil
			}
			return m, nil
		}
		vis--
		if i == m.settCur {
			for fi := range fields {
				if vis == 0 {
					fid := fmt.Sprintf("sett:recipes:%d:%d", i, fi)
					if !m.settActive {
						m.openSettings()
						m.settField = fi
						m.mouseLastID = fid
						m.mouseLastAt = time.Now()
						return m, nil
					}
					if m.settField != fi {
						m.settField = fi
						m.mouseLastID = fid
						m.mouseLastAt = time.Now()
						return m, nil
					}
					if m.click(fid) {
						m.startRecipeEdit(fields[clamp(m.settField, 0, len(fields)-1)].name)
					}
					return m, nil
				}
				vis--
			}
		}
	}
	if !m.settActive {
		m.openSettings()
	}
	m.mouseLastID = ""
	return m, nil
}

// mouseWheelSettings scrolls the settings cursor (or the open recipe
// detail cursor) without activating anything.
func (m Model) mouseWheelSettings(d int) Model {
	if !m.settActive {
		m.openSettings()
	}
	if m.settTab == TabRecipes && len(m.baseRecipes) > 0 && m.settField >= 0 {
		m.settField = clamp(m.settField+d, 0, len(recipeFields())-1)
		return m
	}
	m.settCur = clamp(m.settCur+d, 0, max(m.settingsRows()-1, 0))
	return m
}
