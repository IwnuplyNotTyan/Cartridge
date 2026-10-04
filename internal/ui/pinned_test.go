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

func pinnedModel(t *testing.T, opts ...Option) Model {
	t.Helper()
	i18n.Set(i18n.En)
	args := append([]Option{}, opts...)
	return sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json", args...))
}

func TestPinnedRowsOrderCounts(t *testing.T) {
	m := pinnedModel(t)
	want := []ConsoleRow{
		{ID: "favorites", Count: 0},
		{ID: "recent", Count: 0},
		{ID: "all", Count: 2},
		{ID: "nes", Count: 1},
		{ID: "gba", Count: 1},
	}
	if len(m.rows) != len(want) {
		t.Fatalf("rows = %+v", m.rows)
	}
	for i, w := range want {
		if m.rows[i].ID != w.ID || m.rows[i].Count != w.Count {
			t.Fatalf("row %d = %+v, want id=%q count=%d", i, m.rows[i], w.ID, w.Count)
		}
	}
	if !strings.Contains(m.rows[0].Name, "★") {
		t.Fatalf("favorites row must carry a star: %q", m.rows[0].Name)
	}
}

func TestStartupSelection(t *testing.T) {
	// Empty favorites: start on the full library, never an empty shelf.
	m := pinnedModel(t)
	if m.rows[m.consCur].ID != "all" {
		t.Fatalf("must start on all, got %q", m.rows[m.consCur].ID)
	}
	if len(m.filtered) != 2 {
		t.Fatalf("filtered = %+v", m.filtered)
	}
	// With favorites: start on the favorites shelf.
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("bbb222")
	m = pinnedModel(t, WithFavorites(fav))
	if m.rows[m.consCur].ID != "favorites" {
		t.Fatalf("must start on favorites, got %q", m.rows[m.consCur].ID)
	}
	if len(m.filtered) != 1 || m.filtered[0].Title != "Kirby Mirror" {
		t.Fatalf("favorites scope = %+v", m.filtered)
	}
}

func TestRecentScopeOrderLimit(t *testing.T) {
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
	// Rows: fav, recent, all, nes — All is selected (no favs), step up.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = out.(Model)
	if m.rows[m.consCur].ID != "recent" {
		t.Fatalf("must land on recent, got %q", m.rows[m.consCur].ID)
	}
	if len(m.filtered) != 5 {
		t.Fatalf("recent caps at 5, got %+v", m.filtered)
	}
	if m.filtered[0].ID != "g00" || m.filtered[4].ID != "c00" {
		t.Fatalf("recent must be newest-first: %+v", m.filtered)
	}
	if m.rows[1].Count != 5 {
		t.Fatalf("recent count = %d", m.rows[1].Count)
	}
}

func TestFavoriteToggleUpdatesCounts(t *testing.T) {
	favPath := filepath.Join(t.TempDir(), "favorites.json")
	fav, err := favorites.Load(favPath)
	if err != nil {
		t.Fatal(err)
	}
	m := pinnedModel(t, WithFavorites(fav))
	m.focus = PaneGames
	if m.rows[0].Count != 0 {
		t.Fatalf("fav count = %d", m.rows[0].Count)
	}
	out, _ := m.Update(keyMsg("f"))
	m = out.(Model)
	if !strings.Contains(m.status, "added to favorites") {
		t.Fatalf("status = %q", m.status)
	}
	if m.rows[0].Count != 1 {
		t.Fatalf("fav count must update live, got %d", m.rows[0].Count)
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
	out, _ = m.Update(keyMsg("f"))
	m = out.(Model)
	if !strings.Contains(m.status, "removed from favorites") {
		t.Fatalf("status = %q", m.status)
	}
	if m.rows[0].Count != 0 {
		t.Fatalf("fav count must drop live, got %d", m.rows[0].Count)
	}
}

func TestPinnedEmptyHints(t *testing.T) {
	m := pinnedModel(t)
	// consCur starts on All (no favs): up twice reaches Favorites.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = out.(Model)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = out.(Model)
	if m.rows[m.consCur].ID != "favorites" {
		t.Fatalf("must reach favorites, got %q", m.rows[m.consCur].ID)
	}
	if view := m.View(); !strings.Contains(view, "No favorites yet") {
		t.Fatalf("fav scope must hint\n%s", view)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = out.(Model)
	if view := m.View(); !strings.Contains(view, "No recent games yet") {
		t.Fatalf("recent scope must hint\n%s", view)
	}
}

func TestFavScopeLaunch(t *testing.T) {
	fav, err := favorites.Load(filepath.Join(t.TempDir(), "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	fav.Add("aaa111")
	recipes := []emulate.Recipe{{
		Name: "test", Consoles: []string{"nes", "gba"},
		Emulator: "true", Args: []string{"{rom}"},
	}}
	m := pinnedModel(t, WithFavorites(fav), WithRecipes(recipes))
	if m.rows[m.consCur].ID != "favorites" || len(m.filtered) != 1 {
		t.Fatalf("must start scoped: %+v", m.filtered)
	}
	out, cmd := m.Update(keyMsg("x"))
	m = out.(Model)
	if cmd == nil || !strings.Contains(m.status, "starting") {
		t.Fatalf("fav scope must launch: status=%q cmd=%v", m.status, cmd)
	}
}
