package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/emulate"
	"cartridge/internal/favorites"
	"cartridge/internal/i18n"
	"cartridge/internal/playtime"
	"cartridge/internal/rom"
)

func shelfModel(t *testing.T, opts ...Option) Model {
	t.Helper()
	i18n.Set(i18n.En)
	args := append([]Option{}, opts...)
	return sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json", args...))
}

// shelfTitles returns the display rows of the games pane: games plain,
// blanks as "", headers as "header:Label", hints as "hint:label".
func shelfTitles(t *testing.T, m Model) []string {
	t.Helper()
	var out []string
	for _, r := range m.shelf {
		switch r.kind {
		case shelfBlank:
			out = append(out, "")
		case shelfHeader:
			out = append(out, "header:"+r.label)
		case shelfHint:
			out = append(out, "hint:"+r.label)
		default:
			out = append(out, m.filtered[r.game].Title)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestShelfSectionsOrder(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("bbb222") // Kirby Mirror
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	play.Add("aaa111", time.Second) // Battletoads played
	m := shelfModel(t, WithFavorites(fav), WithPlaytime(play))
	got := shelfTitles(t, m)
	// Recent: Battletoads. Favorites (not in recent): Kirby.
	// Rest is empty. First block has no header: the sticky title covers it.
	want := []string{"Battletoads", "", "header:Favorites", "Kirby Mirror"}
	if !equalStrings(got, want) {
		t.Fatalf("shelf =\n%q\nwant\n%q", got, want)
	}
}

func TestShelfDedupAndFavMark(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("aaa111") // Battletoads is recent AND favorite: shown once.
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	play.Add("aaa111", time.Second)
	time.Sleep(time.Millisecond) // Keep LastPlayed order distinct on fast CI runners.
	play.Add("bbb222", 2*time.Second)
	m := shelfModel(t, WithFavorites(fav), WithPlaytime(play))
	got := shelfTitles(t, m)
	// Recent newest-first: Kirby, Battletoads. Fav section: Battletoads
	// is already above, so only its hint shows. Rest is empty.
	// First block has no header.
	want := []string{
		"Kirby Mirror", "Battletoads", "",
		"header:Favorites", "hint:No favorites yet - press f on a game",
	}
	if !equalStrings(got, want) {
		t.Fatalf("shelf =\n%q\nwant\n%q", got, want)
	}
	// Flat list has no duplicates.
	if len(m.filtered) != 2 {
		t.Fatalf("filtered must dedup: %+v", m.filtered)
	}
}

func TestShelfEmptyHints(t *testing.T) {
	m := shelfModel(t)
	got := shelfTitles(t, m)
	want := []string{
		"hint:No recent games yet - launch something with x", "",
		"header:Favorites", "hint:No favorites yet - press f on a game", "",
		"header:All games", "Battletoads", "Kirby Mirror",
	}
	if !equalStrings(got, want) {
		t.Fatalf("shelf =\n%q\nwant\n%q", got, want)
	}
}

func TestShelfNavigationSkipsDecor(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("bbb222")
	m := shelfModel(t, WithFavorites(fav))
	m.focus = PaneGames
	// Shelf rows: hint, blank, header, Kirby, blank, header, Battletoads.
	// Cursor starts on the first game row (Kirby, idx 0).
	if got := m.selected(); got == nil || got.Title != "Kirby Mirror" {
		t.Fatalf("cursor must start on first game, got %+v", got)
	}
	out, _ := m.Update(keyMsg("down"))
	m = out.(Model)
	if got := m.selected(); got == nil || got.Title != "Battletoads" {
		t.Fatalf("down must skip blanks/headers, got %+v", got)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = out.(Model)
	if got := m.selected(); got == nil || got.Title != "Kirby Mirror" {
		t.Fatalf("up must skip back, got %+v", got)
	}
}

func TestShelfToggleMovesSections(t *testing.T) {
	favPath := filepath.Join(t.TempDir(), "favorites.json")
	fav, err := favorites.Load(favPath)
	if err != nil {
		t.Fatal(err)
	}
	m := shelfModel(t, WithFavorites(fav))
	m.focus = PaneGames
	// No favs, no play: [hint][blank][header+hint][blank][header+games].
	// Cursor on Battletoads (idx 0).
	out, _ := m.Update(keyMsg("f"))
	m = out.(Model)
	if !strings.Contains(m.status, "added to favorites") {
		t.Fatalf("status = %q", m.status)
	}
	got := shelfTitles(t, m)
	want := []string{
		"hint:No recent games yet - launch something with x", "",
		"header:Favorites", "Battletoads", "",
		"header:All games", "Kirby Mirror",
	}
	if !equalStrings(got, want) {
		t.Fatalf("shelf =\n%q\nwant\n%q", got, want)
	}
	// Cursor stayed on the toggled game.
	if got := m.selected(); got == nil || got.Title != "Battletoads" {
		t.Fatalf("cursor must follow the game, got %+v", got)
	}
	if view := m.View(); !strings.Contains(view, "Battletoads ★") {
		t.Fatalf("star must render\n%s", view)
	}
}

func TestShelfSearchFlattens(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("bbb222")
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	play.Add("aaa111", time.Second)
	m := shelfModel(t, WithFavorites(fav), WithPlaytime(play))
	m.focus = PaneGames
	out, _ := m.Update(keyMsg("/"))
	m = out.(Model)
	for _, r := range "kir" {
		out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = out.(Model)
	}
	if len(m.filtered) != 1 {
		t.Fatalf("search must filter: %+v", m.filtered)
	}
	// Sections keep their headers while filtering; empty sections vanish,
	// so there are no blanks or hints.
	got := shelfTitles(t, m)
	want := []string{"header:Favorites", "Kirby Mirror"}
	if !equalStrings(got, want) {
		t.Fatalf("search shelf = %q", got)
	}
}

func TestShelfConsoleScopeIsFlat(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("bbb222")
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	play.Add("aaa111", time.Second)
	m := shelfModel(t, WithFavorites(fav), WithPlaytime(play))
	// Rows: All, NES, GBA. One step down reaches NES.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = out.(Model)
	if m.rows[m.consCur].ID != "nes" {
		t.Fatalf("must land on nes, got %q", m.rows[m.consCur].ID)
	}
	// No sections outside the All view: plain games, library order.
	for _, r := range m.shelf {
		if r.kind != shelfGame {
			t.Fatalf("console scope must be flat, got %+v", r)
		}
	}
	if len(m.filtered) != 1 || m.filtered[0].Title != "Battletoads" {
		t.Fatalf("nes scope = %+v", m.filtered)
	}
	if view := m.View(); strings.Contains(view, "Recent") || strings.Contains(view, "All games") {
		t.Fatalf("no section headers in categories\n%s", view)
	}
}

func TestShelfFavScopeLaunch(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("aaa111")
	recipes := []emulate.Recipe{{
		Name: "test", Consoles: []string{"nes", "gba"},
		Emulator: "true", Args: []string{"{rom}"},
	}}
	m := shelfModel(t, WithFavorites(fav), WithRecipes(recipes))
	m.focus = PaneGames
	// Shelf top is the fav section: x launches straight away.
	out, cmd := m.Update(keyMsg("x"))
	m = out.(Model)
	_ = m
	if cmd == nil {
		t.Fatalf("x must launch from the shelf")
	}
}

func TestShelfSevenGamesLimit(t *testing.T) {
	var games []rom.Game
	for i := 0; i < 7; i++ {
		id := string(rune('a'+i)) + "00"
		games = append(games, rom.Game{
			ID: id, Title: "Game " + id, ConsoleID: "nes",
			ConsoleName: "Nintendo Entertainment System",
			Path:        "/x/" + id + ".nes", SHA256: id,
		})
	}
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range games {
		play.Add(g.ID, time.Second)
		time.Sleep(time.Millisecond) // Guarantee each later game is more recent.
	}
	i18n.Set(i18n.En)
	m := sizedModel(t, New(games, "/x", "/tmp/lib.json", WithPlaytime(play)))
	nRecent := 0
	for _, r := range m.shelf {
		if r.kind == shelfGame {
			nRecent++
		}
	}
	// 5 recent + 2 rest, no favs: headers + hints + blanks included.
	if nRecent != 7 {
		t.Fatalf("all games shown once: %d", nRecent)
	}
	// First five selectable rows are the newest played.
	var first []string
	for _, r := range m.shelf {
		if r.kind == shelfGame {
			first = append(first, m.filtered[r.game].Title)
		}
		if len(first) == 5 {
			break
		}
	}
	for i, want := range []string{"Game g00", "Game f00", "Game e00", "Game d00", "Game c00"} {
		if first[i] != want {
			t.Fatalf("recent order = %v", first)
		}
	}
}

// shelfScrollModel builds a 40-game shelf that scrolls at 120x30:
// 5 played + blank + header + 1 fav + blank + header + 34 rest
// (44 rows, 26 visible; first block has no header).
func shelfScrollModel(t *testing.T) Model {
	t.Helper()
	i18n.Set(i18n.En)
	var games []rom.Game
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("g%02d", i)
		games = append(games, rom.Game{
			ID: id, Title: "Game " + id, ConsoleID: "nes",
			ConsoleName: "Nintendo Entertainment System",
			Path:        "/x/" + id + ".nes", SHA256: id,
		})
	}
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		play.Add(fmt.Sprintf("g%02d", i), time.Second)
	}
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("g10")
	m := sizedModel(t, New(games, "/x", "/tmp/lib.json",
		WithFavorites(fav), WithPlaytime(play)))
	m.focus = PaneGames
	if len(m.shelf) != 44 {
		t.Fatalf("fixture must scroll: %d rows", len(m.shelf))
	}
	return m
}

// viewLines splits a rendered view (line 0 is the outer gap, line 1 the
// pane title, line 2 the first shelf row).
func viewLines(view string) []string { return strings.Split(view, "\n") }

func TestShelfStickyBottom(t *testing.T) {
	m := shelfScrollModel(t)
	m.gameCur = len(m.filtered) - 1 // last game, "All games" section
	lines := viewLines(m.View())
	// The pane title itself becomes the section: no "Games (40)" left.
	if !strings.Contains(lines[1], "All games") {
		t.Fatalf("title must stick to the section:\n%s", m.View())
	}
	if strings.Contains(m.View(), "Games (40)") {
		t.Fatalf("plain title must be replaced:\n%s", m.View())
	}
	// The inline "All games" header has scrolled off: only the sticky
	// title names it.
	if n := strings.Count(m.View(), "All games"); n != 1 {
		t.Fatalf("section title must not duplicate: %d", n)
	}
}

func TestShelfStickyMiddle(t *testing.T) {
	m := shelfScrollModel(t)
	m.gameCur = 17 // window starts on the blank after Favorites
	lines := viewLines(m.View())
	// One row past the Fav boundary: the title has flipped to Favorites.
	if !strings.Contains(lines[1], "Favorites") {
		t.Fatalf("title must stick to Favorites:\n%s", m.View())
	}
	if n := strings.Count(m.View(), "Favorites"); n != 1 {
		t.Fatalf("section title must not duplicate: %d", n)
	}
}

func TestShelfTitleTopShowsSection(t *testing.T) {
	m := shelfScrollModel(t)
	m.gameCur = 0 // top, nothing scrolled: the title names Recent anyway
	lines := viewLines(m.View())
	if !strings.Contains(lines[1], "Recent") {
		t.Fatalf("top title must name the section:\n%s", m.View())
	}
	if strings.Contains(m.View(), "Games (") {
		t.Fatalf("plain title must be replaced:\n%s", m.View())
	}
}

func TestShelfTitleBlankTop(t *testing.T) {
	m := shelfScrollModel(t)
	m.gameCur = 17 // window opens exactly on the blank after Favorites
	lines := viewLines(m.View())
	// A blank separator belongs to the section above it, so the title
	// still names Favorites, not the All games below.
	if !strings.Contains(lines[1], "Favorites") {
		t.Fatalf("blank top must keep the section above:\n%s", m.View())
	}
}

func TestShelfStickyDelayOneRow(t *testing.T) {
	m := shelfScrollModel(t)
	// Layout: [0-4 games Recent][5 blank][6 header Fav][7 fav]
	// [8 blank][9 header All][10+ rest]. The sticky title flips only
	// 1 row AFTER the new block starts: blanks belong above.
	// Fav header just reached the top: still Recent.
	if label, _ := m.topSection(6, 7); label != "Recent" {
		t.Fatalf("header at top must keep old title: got %q", label)
	}
	// Fav game at top (1 past boundary): Favorites takes over.
	if label, _ := m.topSection(7, 8); label != "Favorites" {
		t.Fatalf("1 past must flip the title: got %q", label)
	}
	// Blank after Fav (2 past): still Favorites.
	if label, _ := m.topSection(8, 9); label != "Favorites" {
		t.Fatalf("2 past must keep the title: got %q", label)
	}
	// All-games header just reached the top: still Favorites.
	if label, _ := m.topSection(9, 10); label != "Favorites" {
		t.Fatalf("new header must keep old title: got %q", label)
	}
	// First All game at top (1 past): All games takes over.
	if label, _ := m.topSection(10, 11); label != "All games" {
		t.Fatalf("1 past must flip the title: got %q", label)
	}
}

func TestShelfFlatScrollNoSticky(t *testing.T) {
	m := shelfScrollModel(t)
	m.consCur = 1 // NES scope: flat list, no sections at all
	m.refilter()
	m.gameCur = len(m.filtered) - 1
	view := m.View()
	if !strings.Contains(view, "Game g39") {
		t.Fatalf("flat list must scroll to the end:\n%s", view)
	}
	for _, s := range []string{"Recent", "Favorites", "All games"} {
		if strings.Contains(view, s) {
			t.Fatalf("flat scope must not pin %q:\n%s", s, view)
		}
	}
}
