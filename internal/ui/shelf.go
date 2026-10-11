package ui

import (
	"sort"
	"strings"

	"cartridge/internal/i18n"
	"cartridge/internal/rom"
)

// Shelf: console filtering and the games-pane rows.
// shelfKind is one rendered row of the games pane.
type shelfKind int

const (
	shelfGame shelfKind = iota
	shelfBlank
	shelfHint
	shelfHeader
)

// stickyDelay — на сколько строк запаздывает липкая шапка:
// новый раздел захватывает заголовок, только когда топ ушёл на
// столько строк past его первой строки. Разделитель-пустышка
// относится к секции сверху.
const stickyDelay = 1

// shelfSection ids tag game and hint rows with their shelf block.
const (
	secRecent    = "recent"
	secFavorites = "favorites"
	secAll       = "all"
)

// shelfRow is one rendered row of the games pane: a single blank
// separator between blocks, a section header ("Recent", "Favorites",
// "All games"), a dim hint under an empty section, or a
// game (game indexes m.filtered, section names its block).
type shelfRow struct {
	kind    shelfKind
	label   string // header text or hint text
	game    int
	section string
}

// recentLimit caps how many played games top the shelf.
const recentLimit = 5

func (m *Model) refilter() {
	id := "all"
	if m.consCur < len(m.rows) {
		id = m.rows[m.consCur].ID
	}
	q := m.query
	if m.searching {
		q = string(m.searchBuf)
	}
	q = strings.ToLower(strings.TrimSpace(q))
	var scoped []rom.Game
	for _, g := range m.games {
		if id != "all" && g.ConsoleID != id {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(g.Title), q) {
			continue
		}
		scoped = append(scoped, g)
	}
	// The shelf lives only on the All view: recent (newest first,
	// capped), then favorites, then the rest, each game shown once.
	// Console categories stay a plain list in library order.
	var recent, favs, rest []rom.Game
	if id == "all" {
		played := map[string]int64{}
		if m.play != nil {
			for _, g := range scoped {
				if st, ok := m.play.Get(g.ID); ok {
					played[g.ID] = st.LastPlayed.UnixNano()
				}
			}
		}
		inShelf := map[string]bool{}
		if len(played) > 0 {
			type playedGame struct {
				g  rom.Game
				at int64
			}
			var pl []playedGame
			for _, g := range scoped {
				if at, ok := played[g.ID]; ok {
					pl = append(pl, playedGame{g, at})
				}
			}
			sort.Slice(pl, func(i, j int) bool {
				if pl[i].at == pl[j].at {
					return pl[i].g.ID < pl[j].g.ID
				}
				return pl[i].at > pl[j].at
			})
			for i, p := range pl {
				if i >= recentLimit {
					break
				}
				recent = append(recent, p.g)
				inShelf[p.g.ID] = true
			}
		}
		for _, g := range scoped {
			if inShelf[g.ID] {
				continue
			}
			if m.favorites.Has(g.ID) {
				favs = append(favs, g)
			} else {
				rest = append(rest, g)
			}
			inShelf[g.ID] = true
		}
	} else {
		rest = scoped
	}
	m.filtered = m.filtered[:0]
	m.filtered = append(m.filtered, recent...)
	m.filtered = append(m.filtered, favs...)
	m.filtered = append(m.filtered, rest...)
	// Display rows: headers per section (a hint when it is empty and
	// nothing filters), single blank lines between blocks. Categories
	// stay flat: no sections outside the All view.
	m.shelf = m.buildShelf(id == "all", recent, favs, rest, q != "")
	if m.gameCur >= len(m.filtered) {
		m.gameCur = max(len(m.filtered)-1, 0)
	}
	m.metaCur = 0
	m.cheatsOpen = false
	m.modsOpen = false
	m.refreshMeta()
}

// buildShelf renders display rows for filtered games already appended to
// m.filtered in shelf order (recent, favorites, rest). Every block after
// the first starts with its header row ("Favorites", "All games", ...),
// blocks are split by a single blank line. The very first block has no
// header: the sticky pane title already names it, so it would only
// duplicate. While filtering the pane title shows the query instead,
// so headers stay on every block. Outside the All view
// (shelfOn=false) it is a plain game list with no sections.
func (m *Model) buildShelf(shelfOn bool, recent, favs, rest []rom.Game, filtered bool) []shelfRow {
	rows := m.shelf[:0]
	game := 0
	if !shelfOn {
		for range m.filtered {
			rows = append(rows, shelfRow{kind: shelfGame, game: game})
			game++
		}
		return rows
	}
	first := true
	block := func(sec, hintKey string, list []rom.Game) {
		if len(list) == 0 && filtered {
			return // querying: empty sections vanish entirely
		}
		if len(rows) > 0 {
			rows = append(rows, shelfRow{kind: shelfBlank})
		}
		// First block: no inline header, the sticky title covers it.
		// While filtering the title shows the query, so keep headers.
		if filtered || !first {
			rows = append(rows, shelfRow{kind: shelfHeader, label: sectionLabel(sec), section: sec})
		}
		first = false
		if len(list) == 0 {
			if !filtered {
				rows = append(rows, shelfRow{kind: shelfHint, label: i18n.T(hintKey), section: sec})
			}
			return
		}
		for range list {
			rows = append(rows, shelfRow{kind: shelfGame, game: game, section: sec})
			game++
		}
	}
	block(secRecent, "home.empty.recent", recent)
	block(secFavorites, "home.empty.favorites", favs)
	if len(rest) > 0 {
		if len(rows) > 0 {
			rows = append(rows, shelfRow{kind: shelfBlank})
		}
		if filtered || !first {
			rows = append(rows, shelfRow{kind: shelfHeader, label: sectionLabel(secAll), section: secAll})
		}
		first = false
		for range rest {
			rows = append(rows, shelfRow{kind: shelfGame, game: game, section: secAll})
			game++
		}
	}
	return rows
}

// sectionLabel renders a shelf section id for the pane title.
func sectionLabel(sec string) string {
	switch sec {
	case secRecent:
		return i18n.T("home.recent")
	case secFavorites:
		return i18n.T("home.favorites")
	default:
		return i18n.T("shelf.all")
	}
}

// shelfPos maps the game cursor onto its display row (game indexes are
// unique: every game shows once).
func (m Model) shelfPos() int {
	for i, r := range m.shelf {
		if r.kind == shelfGame && r.game == m.gameCur {
			return i
		}
	}
	return 0
}

// topSection returns the sticky pane title for the window [start, end):
// the section owning its top, but with a stickyDelay hysteresis AFTER
// the boundary: a new section takes over only when the top has moved
// stickyDelay rows past its first row. A blank separator belongs to
// the section above it, so the title never flips before the header.
// Flat category lists carry no sections at all.
func (m Model) topSection(start, end int) (string, bool) {
	if len(m.shelf) == 0 {
		return "", false
	}
	if start < 0 {
		start = 0
	}
	if end > len(m.shelf) {
		end = len(m.shelf)
	}
	if start >= len(m.shelf) || start >= end {
		return "", false
	}
	// secAt resolves a row to its section: blanks belong to the section
	// above (trailing separator), never to the one below.
	secAt := func(i int) string {
		for j := i; j >= 0; j-- {
			if m.shelf[j].kind == shelfBlank {
				continue
			}
			return m.shelf[j].section
		}
		for j := i + 1; j < len(m.shelf); j++ {
			if m.shelf[j].kind == shelfBlank {
				continue
			}
			return m.shelf[j].section
		}
		return ""
	}
	cur := secAt(start)
	if cur == "" {
		return "", false
	}
	// First row of the current block.
	boundary := start
	for boundary > 0 && secAt(boundary-1) == cur {
		boundary--
	}
	var prev string
	if boundary > 0 {
		prev = secAt(boundary - 1)
	}
	if prev == "" || prev == cur {
		return sectionLabel(cur), true
	}
	if start-boundary < stickyDelay {
		return sectionLabel(prev), true
	}
	return sectionLabel(cur), true
}

func (m Model) selected() *rom.Game {
	if len(m.filtered) == 0 || m.gameCur >= len(m.filtered) {
		return nil
	}
	return &m.filtered[m.gameCur]
}

// selectGame moves the game cursor and resets the meta pane to the top,
// so the new game's title is always visible instead of a deep-scrolled
// tail that looks identical across games of one console.
func (m *Model) selectGame(i int) {
	m.gameCur = i
	m.metaCur = 0
	m.cheatsOpen = false
	m.modsOpen = false
	m.artOpen = false
	m.refreshMeta()
}

// setFocus moves browser focus, re-syncing the meta viewport geometry.
// The visible pane set depends on focus, so without this a meta pane
// shown again after being hidden would keep its stale narrow width
// and render ~10 cells wide until the next resize.
func (m *Model) setFocus(p Pane) {
	if m.focus == p {
		return
	}
	m.focus = p
	m.sizeMeta()
	m.refreshMeta()
}
