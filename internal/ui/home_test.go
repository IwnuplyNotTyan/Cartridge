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
)

func homeModel(t *testing.T, opts ...Option) Model {
	t.Helper()
	i18n.Set(i18n.En)
	args := append([]Option{}, opts...)
	m := New(sampleGames(), "/x", "/tmp/lib.json", args...)
	return sizedModel(t, m)
}

func openHome(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(keyMsg("0"))
	mm := out.(Model)
	if mm.mode != ModeHome {
		t.Fatalf("0 must open home, mode=%d", mm.mode)
	}
	return mm
}

func TestHomeEmptySections(t *testing.T) {
	m := openHome(t, homeModel(t))
	view := m.View()
	for _, want := range []string{"Home", "Recent", "Favorites", "No recent games", "No favorites"} {
		if !strings.Contains(view, want) {
			t.Fatalf("home must show %q\n%s", want, view)
		}
	}
	// Nothing selectable: enter and moves are no-ops.
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil {
		t.Fatalf("empty home must not launch")
	}
	out, _ = m.Update(keyMsg("down"))
	if out.(Model).homeCur != m.homeCur {
		t.Fatalf("empty home cursor must not move")
	}
	// Esc returns to the browser.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if out.(Model).mode != ModeBrowser {
		t.Fatalf("esc must close home")
	}
}

func TestHomeRecentOrder(t *testing.T) {
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	play.Add("bbb222", time.Second)
	play.Add("aaa111", 2*time.Second)
	m := openHome(t, homeModel(t, WithPlaytime(play)))
	var titles []string
	for _, it := range m.homeItems {
		if it.selectable {
			titles = append(titles, it.title)
		}
	}
	if len(titles) != 2 || titles[0] != "Battletoads" || titles[1] != "Kirby Mirror" {
		t.Fatalf("recent must be newest-first: %v", titles)
	}
}

func TestHomeFavoritesSection(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("bbb222")
	m := openHome(t, homeModel(t, WithFavorites(fav)))
	var titles []string
	inFav := false
	for _, it := range m.homeItems {
		if it.header && it.title == "Favorites" {
			inFav = true
			continue
		}
		if it.header {
			inFav = false
		}
		if inFav && it.selectable {
			titles = append(titles, it.title)
		}
	}
	if len(titles) != 1 || titles[0] != "Kirby Mirror" {
		t.Fatalf("favorites section = %v", titles)
	}
}

func TestFavoriteTogglePersists(t *testing.T) {
	favPath := filepath.Join(t.TempDir(), "favorites.json")
	fav, err := favorites.Load(favPath)
	if err != nil {
		t.Fatal(err)
	}
	m := homeModel(t, WithFavorites(fav))
	m.focus = PaneGames
	out, _ := m.Update(keyMsg("f"))
	m = out.(Model)
	if !strings.Contains(m.status, "added to favorites") {
		t.Fatalf("status = %q", m.status)
	}
	if view := m.View(); !strings.Contains(view, "Battletoads ★") {
		t.Fatalf("games list must star favorites\n%s", view)
	}
	reloaded, err := favorites.Load(favPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Has("aaa111") {
		t.Fatalf("favorite must persist")
	}
	// Toggle again removes it.
	out, _ = m.Update(keyMsg("f"))
	m = out.(Model)
	if !strings.Contains(m.status, "removed from favorites") {
		t.Fatalf("status = %q", m.status)
	}
	if view := m.View(); strings.Contains(view, "★") {
		t.Fatalf("star must disappear\n%s", view)
	}
}

func TestHomeLaunch(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("aaa111")
	recipes := []emulate.Recipe{{
		Name: "test", Consoles: []string{"nes", "gba"},
		Emulator: "true", Args: []string{"{rom}"},
	}}
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := openHome(t, homeModel(t, WithFavorites(fav), WithRecipes(recipes), WithPlaytime(play)))
	// Cursor lands on the first selectable row (the favorited game).
	if got := m.homeSelected(); got == nil || got.Title != "Battletoads" {
		t.Fatalf("home cursor must start on Battletoads, got %+v", got)
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("enter on home must launch")
	}
	if !strings.Contains(m.status, "starting") {
		t.Fatalf("status = %q", m.status)
	}
	// Finishing the session tops Recent without reopening home.
	out, _ = m.Update(launchDoneMsg{gameID: "aaa111", dur: 10 * time.Second})
	m = out.(Model)
	if m.mode != ModeHome {
		t.Fatalf("must stay on home")
	}
	var titles []string
	for _, it := range m.homeItems {
		if it.selectable {
			titles = append(titles, it.title)
		}
	}
	// Recent tops with a star (still favorited), favorites repeat it plain.
	if len(titles) != 2 || titles[0] != "Battletoads ★" || titles[1] != "Battletoads" {
		t.Fatalf("recent must top after play: %v", titles)
	}
}

func TestHomeCursorSkipsHeaders(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("aaa111")
	fav.Add("bbb222")
	m := openHome(t, homeModel(t, WithFavorites(fav)))
	// Items: RecentH, hint, FavH, Kirby? — favs in library order.
	// m.games order: Battletoads, Kirby Mirror.
	var want []string
	for _, it := range m.homeItems {
		if it.selectable {
			want = append(want, it.title)
		}
	}
	if len(want) != 2 || want[0] != "Battletoads" || want[1] != "Kirby Mirror" {
		t.Fatalf("selectable = %v", want)
	}
	out, _ := m.Update(keyMsg("down"))
	m = out.(Model)
	if got := m.homeSelected(); got == nil || got.Title != "Kirby Mirror" {
		t.Fatalf("down must skip the section header, got %+v", got)
	}
	out, _ = m.Update(keyMsg("down"))
	m = out.(Model)
	if got := m.homeSelected(); got == nil || got.Title != "Kirby Mirror" {
		t.Fatalf("cursor must clamp at the end, got %+v", got)
	}
	// Unfavorite from home removes the row live.
	out, _ = m.Update(keyMsg("f"))
	m = out.(Model)
	if !strings.Contains(m.status, "removed from favorites") {
		t.Fatalf("status = %q", m.status)
	}
	n := 0
	for _, it := range m.homeItems {
		if it.selectable {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("row must vanish, selectable = %d", n)
	}
}

func TestHomeDigitKeys(t *testing.T) {
	m := openHome(t, homeModel(t))
	out, _ := m.Update(keyMsg("2"))
	m = out.(Model)
	if m.mode != ModeBrowser || m.focus != PaneGames {
		t.Fatalf("2 must return to games, mode=%d focus=%d", m.mode, m.focus)
	}
	out, _ = m.Update(keyMsg("0"))
	if out.(Model).mode != ModeHome {
		t.Fatalf("0 must reopen home")
	}
}
