package ui

import (
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cartridge/internal/i18n"
	"cartridge/internal/rom"
)

// openHome switches to the home page (recent + favorites) and builds it.
func (m *Model) openHome() {
	if m.mode == ModeHome {
		return
	}
	m.searching = false // the search line belongs to the browser
	m.mode = ModeHome
	m.homeCur = 0
	m.rebuildHome()
}

// closeHome returns to the browser, keeping its cursor where it was.
func (m *Model) closeHome() {
	m.mode = ModeBrowser
	m.sizeMeta()
	m.refreshMeta()
}

// rebuildHome recomputes the home-page rows: the recent section lists up
// to recentLimit played games (newest first), the favorites section the
// favorited ones in library order. Missing IDs are skipped; the cursor is
// clamped onto a selectable row.
func (m *Model) rebuildHome() {
	byID := make(map[string]rom.Game, len(m.games))
	for _, g := range m.games {
		byID[g.ID] = g
	}
	var items []homeItem
	items = append(items, homeItem{header: true, title: i18n.T("home.recent")})
	type played struct {
		g  rom.Game
		at time.Time
	}
	var rec []played
	if m.play != nil {
		for id, g := range byID {
			if st, ok := m.play.Get(id); ok {
				rec = append(rec, played{g, st.LastPlayed})
			}
		}
		sort.Slice(rec, func(i, j int) bool {
			if rec[i].at.Equal(rec[j].at) {
				return rec[i].g.ID < rec[j].g.ID
			}
			return rec[i].at.After(rec[j].at)
		})
		if len(rec) > recentLimit {
			rec = rec[:recentLimit]
		}
	}
	if len(rec) == 0 {
		items = append(items, homeItem{title: i18n.T("home.empty.recent")})
	}
	for _, p := range rec {
		items = append(items, homeItem{
			selectable: true,
			title:      homeTitle(p.g, m.favorites.Has(p.g.ID)),
			sub:        homeConsole(p.g) + " • " + p.at.Format("2006-01-02"),
			game:       p.g,
		})
	}
	items = append(items, homeItem{header: true, title: i18n.T("home.favorites")})
	nFav := 0
	for _, g := range m.games {
		if !m.favorites.Has(g.ID) {
			continue
		}
		items = append(items, homeItem{
			selectable: true,
			title:      g.Title,
			sub:        homeConsole(g),
			game:       g,
		})
		nFav++
	}
	if nFav == 0 {
		items = append(items, homeItem{title: i18n.T("home.empty.favorites")})
	}
	m.homeItems = items
	if m.homeCur >= len(items) {
		m.homeCur = len(items) - 1
	}
	if m.homeCur < 0 {
		m.homeCur = 0
	}
	for m.homeCur < len(items) && !items[m.homeCur].selectable {
		m.homeCur++
	}
	if m.homeCur >= len(items) {
		m.homeCur = max(len(items)-1, 0)
	}
}

// homeSelected returns the game under the home cursor, if any.
func (m Model) homeSelected() *rom.Game {
	if m.homeCur < 0 || m.homeCur >= len(m.homeItems) {
		return nil
	}
	it := m.homeItems[m.homeCur]
	if !it.selectable {
		return nil
	}
	g := it.game
	return &g
}

// homeMove shifts the home cursor, skipping headers and hint lines.
func (m *Model) homeMove(d int) {
	if len(m.homeItems) == 0 {
		return
	}
	step := 1
	if d < 0 {
		step = -1
	}
	i := m.homeCur
	for n := 0; n < abs(d); n++ {
		j := i + step
		for j >= 0 && j < len(m.homeItems) && !m.homeItems[j].selectable {
			j += step
		}
		if j < 0 || j >= len(m.homeItems) {
			break
		}
		i = j
	}
	m.homeCur = i
}

// homeEdge jumps to the first (last=false) or last selectable row.
func (m *Model) homeEdge(last bool) {
	if len(m.homeItems) == 0 {
		return
	}
	if last {
		m.homeCur = len(m.homeItems) - 1
		for m.homeCur > 0 && !m.homeItems[m.homeCur].selectable {
			m.homeCur--
		}
	} else {
		m.homeCur = 0
		for m.homeCur < len(m.homeItems) && !m.homeItems[m.homeCur].selectable {
			m.homeCur++
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// toggleFavorite flips the favorite mark on the focused game (browser
// selection or home cursor) and persists it.
func (m *Model) toggleFavorite() {
	var g *rom.Game
	if m.mode == ModeHome {
		g = m.homeSelected()
	} else {
		g = m.selected()
	}
	if g == nil || m.favorites == nil {
		return
	}
	if m.favorites.Toggle(g.ID) {
		m.status = i18n.T("status.fav.added", g.Title)
	} else {
		m.status = i18n.T("status.fav.removed", g.Title)
	}
	if err := m.favorites.Save(); err != nil {
		m.status = i18n.T("status.save_fail", err)
		return
	}
	if m.mode == ModeHome {
		m.rebuildHome()
	}
	m.refreshMeta()
}

// launchHomeSelected starts the emulator for the game under the home
// cursor, exactly like a browser launch (timed, recorded to playtime).
func (m Model) launchHomeSelected() (tea.Model, tea.Cmd) {
	g := m.homeSelected()
	if g == nil {
		return m, nil
	}
	game := *g
	r := m.gameRecipe(game)
	if r == nil {
		m.status = i18n.T("status.no_recipe", game.Title)
		return m, nil
	}
	cmd, err := m.launchCmd(game, r)
	if err != nil {
		m.status = i18n.T("status.launch_fail", err)
		return m, nil
	}
	m.status = i18n.T("status.launching", game.Title)
	start := time.Now()
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return launchDoneMsg{gameID: game.ID, dur: time.Since(start), err: err}
	})
}

// updateHome handles keys on the home page.
func (m Model) updateHome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		return m, nil
	}
	if m.showHelp && msg.String() == "esc" {
		m.showHelp = false
		m.help.ShowAll = false
		return m, nil
	}
	switch {
	case key.Matches(msg, k.Up):
		m.homeMove(-1)
		return m, nil
	case key.Matches(msg, k.Down):
		m.homeMove(1)
		return m, nil
	case key.Matches(msg, k.PageUp):
		m.homeMove(-5)
		return m, nil
	case key.Matches(msg, k.PageDown):
		m.homeMove(5)
		return m, nil
	case key.Matches(msg, k.Top):
		m.homeEdge(false)
		return m, nil
	case key.Matches(msg, k.Bottom):
		m.homeEdge(true)
		return m, nil
	case key.Matches(msg, k.Select), key.Matches(msg, k.Launch):
		return m.launchHomeSelected()
	case key.Matches(msg, k.Favorite):
		m.toggleFavorite()
		return m, nil
	case key.Matches(msg, k.Rescan):
		m.status = i18n.T("status.scanning", m.romDir)
		return m, m.rescanCmd()
	case key.Matches(msg, k.Settings):
		m.openSettings()
		return m, nil
	}
	switch msg.String() {
	case "1":
		m.closeHome()
		m.setFocus(PaneConsoles)
		return m, nil
	case "2":
		m.closeHome()
		m.setFocus(PaneGames)
		return m, nil
	case "3":
		m.closeHome()
		m.setFocus(PaneMeta)
		return m, nil
	case "0", "esc":
		m.closeHome()
		return m, nil
	}
	return m, nil
}

// renderHome draws the home page: a full-width list with Recent and
// Favorites sections, the cursor highlighted like browser rows.
func (m Model) renderHome() string {
	w := m.contentW()
	lines := []string{titleFor(i18n.T("home.title"), true, w)}
	visible := max(m.bodyH()-1, 1)
	start := clamp(m.homeCur-visible/2, 0, max(len(m.homeItems)-visible, 0))
	for i := start; i < min(start+visible, len(m.homeItems)); i++ {
		it := m.homeItems[i]
		switch {
		case it.header:
			lines = append(lines, titleDim.Render(fit("  "+truncate(it.title, w-2), w)))
		case !it.selectable:
			lines = append(lines, infoStyle.Render(fit("  "+truncate(it.title, w-2), w)))
		default:
			sub := it.sub
			nameW := max(w-2-1-lipgloss.Width(sub), 4)
			name := truncate(it.title, nameW)
			plain := fit("  "+name+strings.Repeat(" ", max(nameW-lipgloss.Width(name)+1, 1))+sub, w)
			if i == m.homeCur {
				plain = styledRow(plain)
			}
			lines = append(lines, plain)
		}
	}
	return m.column(lines, w, 0, 0)
}

// homeConsole renders the short console tag ("SNES", "???").
func homeConsole(g rom.Game) string {
	return rom.ByID(g.ConsoleID).Short
}

// homeTitle renders a game title with a star when favorited.
func homeTitle(g rom.Game, fav bool) string {
	if fav {
		return g.Title + " ★"
	}
	return g.Title
}
