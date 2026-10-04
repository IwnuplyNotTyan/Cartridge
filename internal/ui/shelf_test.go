package ui

import (
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

// shelfTitles returns the display rows of the games pane: headers in
// brackets, games plain, blanks as "".
func shelfTitles(t *testing.T, m Model) []string {
	t.Helper()
	var out []string
	for _, r := range m.shelf {
		switch r.kind {
		case shelfHeader:
			out = append(out, "["+r.label+"]")
		case shelfBlank:
			out = append(out, "")
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
	// Rest: Battletoads is played but not favorited... wait, Battletoads
	// is recent, so the rest holds Battletoads? No: dedup puts it once.
	// Rest: nothing? Kirby is fav, Battletoads recent → rest holds both?
	// No: rest = neither recent nor fav. Battletoads is recent, Kirby is
	// fav → rest is empty → no trailing blank.
	want := []string{
		"[Recent]", "Battletoads", "",
		"[Favorites]", "Kirby Mirror",
	}
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
	play.Add("bbb222", 2*time.Second)
	m := shelfModel(t, WithFavorites(fav), WithPlaytime(play))
	got := shelfTitles(t, m)
	// Recent newest-first: Kirby, Battletoads. Fav section: Battletoads
	// is already above, so only... wait Battletoads IS favorited but shown
	// in recent → fav section lists favs not in recent → empty → hint.
	// Rest: nothing left (both games placed).
	want := []string{
		"[Recent]", "Kirby Mirror", "Battletoads", "",
		"[Favorites]", "hint:No favorites yet - press f on a game",
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
		"[Recent]", "hint:No recent games yet - launch something with x", "",
		"[Favorites]", "hint:No favorites yet - press f on a game", "",
		"Battletoads", "Kirby Mirror",
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
	// Shelf: [Recent][hint][blank][Favorites][Kirby][blank][Battletoads][Kirby].
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
	// No favs, no play: [Recent][hint][blank][Favorites][hint][blank][games...].
	// Cursor on Battletoads (idx 0).
	out, _ := m.Update(keyMsg("f"))
	m = out.(Model)
	if !strings.Contains(m.status, "added to favorites") {
		t.Fatalf("status = %q", m.status)
	}
	got := shelfTitles(t, m)
	want := []string{
		"[Recent]", "hint:No recent games yet - launch something with x", "",
		"[Favorites]", "Battletoads", "",
		"Kirby Mirror",
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
	// The matching section keeps its header; empty sections vanish,
	// so there are no blanks or hints.
	got := shelfTitles(t, m)
	want := []string{"[Favorites]", "Kirby Mirror"}
	if !equalStrings(got, want) {
		t.Fatalf("search shelf = %q", got)
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
