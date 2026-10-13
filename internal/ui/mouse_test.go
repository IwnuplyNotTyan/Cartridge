package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
	"cartridge/internal/playtime"
	"path/filepath"
)

func mouseClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func mouseRight(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight}
}

func mouseWheel(x, y int, up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

// layout for the default 120x30 model: consoles X 1..36, games 38..81,
// meta 83..118 (terminal coords, appGap=1). Body title is Y=1.
func TestMouseClickConsoleSelects(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// NES row is bodyY=3 -> terminal Y=4, X inside consoles.
	out, _ := m.Update(mouseClick(2, 4))
	m = out.(Model)
	if m.rows[m.consCur].ID != "nes" {
		t.Fatalf("click must select nes, got %q", m.rows[m.consCur].ID)
	}
	if m.focus != PaneConsoles {
		t.Fatalf("click must focus consoles, got %d", m.focus)
	}
}

func TestMouseClickGameSelects(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// First game row (Battletoads) is bodyY=7 -> terminal Y=8.
	out, _ := m.Update(mouseClick(41, 8))
	m = out.(Model)
	if m.focus != PaneGames {
		t.Fatalf("click must focus games, got %d", m.focus)
	}
	g := m.selected()
	if g == nil || g.Title != "Battletoads" {
		t.Fatalf("click must select Battletoads, got %+v", g)
	}
	// Second row (Kirby) is bodyY=8 -> Y=9.
	out, _ = m.Update(mouseClick(41, 9))
	m = out.(Model)
	if g := m.selected(); g == nil || g.Title != "Kirby Mirror" {
		t.Fatalf("click must select Kirby, got %+v", g)
	}
}

func TestMouseDoubleClickGameLaunches(t *testing.T) {
	i18n.Set(i18n.En)
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	recipes := []emulate.Recipe{{
		Name: "test-nes", Consoles: []string{"nes"},
		Emulator: "true", Args: []string{"{rom}"},
	}}
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes(recipes), WithPlaytime(play)))
	// Move to NES console so the first game row is Battletoads alone.
	m.consCur = 1
	m.refilter()
	// Games pane with a single game: shelf has no section headers,
	// the game sits at bodyY=1 -> terminal Y=2.
	out, _ := m.Update(mouseClick(41, 2))
	m = out.(Model)
	out, _ = m.Update(mouseClick(41, 2)) // fast second click = double
	m = out.(Model)
	if !m.runView || m.run == nil {
		t.Fatalf("double-click must launch the game")
	}
}

func TestMouseRightClickGameLaunches(t *testing.T) {
	i18n.Set(i18n.En)
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	recipes := []emulate.Recipe{{
		Name: "test-nes", Consoles: []string{"nes"},
		Emulator: "true", Args: []string{"{rom}"},
	}}
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes(recipes), WithPlaytime(play)))
	m.consCur = 1
	m.refilter()
	out, _ := m.Update(mouseRight(41, 2))
	m = out.(Model)
	if !m.runView || m.run == nil {
		t.Fatalf("right-click must launch the game")
	}
}

func TestMouseWheelScrollsGames(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneGames
	out, _ := m.Update(mouseWheel(41, 8, false)) // wheel down over games
	m = out.(Model)
	if m.gameCur != 1 {
		t.Fatalf("wheel down must move to Kirby, got %d", m.gameCur)
	}
	out, _ = m.Update(mouseWheel(41, 8, true)) // wheel up
	m = out.(Model)
	if m.gameCur != 0 {
		t.Fatalf("wheel up must move back, got %d", m.gameCur)
	}
}

func TestMouseClickMetaSelects(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// Meta pane X=90 (inside meta), id row is the 3rd visual meta line:
	// title(0) sep(1) id(2) -> bodyY=1+2=3 -> terminal Y=4.
	out, _ := m.Update(mouseClick(90, 4))
	m = out.(Model)
	if m.focus != PaneMeta {
		t.Fatalf("click must focus meta, got %d", m.focus)
	}
	if m.metaCur < 0 || m.metaCur >= len(m.metaRows) || m.metaRows[m.metaCur].kind != "id" {
		t.Fatalf("click must select the id row, cur=%d", m.metaCur)
	}
}

func TestMouseClickSettingsRowAndOpen(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// Settings entry is the last consoles item: bodyY=6 -> Y=7.
	out, _ := m.Update(mouseClick(2, 7))
	m = out.(Model)
	if !m.onSettingsRow() {
		t.Fatalf("click must hover the settings row, consCur=%d", m.consCur)
	}
	out, _ = m.Update(mouseClick(2, 7)) // double-click opens
	m = out.(Model)
	if !m.settActive {
		t.Fatalf("double-click must open settings")
	}
}

func TestMouseWheelInRunView(t *testing.T) {
	i18n.Set(i18n.En)
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	recipes := []emulate.Recipe{{
		Name: "test-nes", Consoles: []string{"nes"},
		Emulator: "true", Args: []string{"{rom}"},
	}}
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes(recipes), WithPlaytime(play)))
	m.consCur = 1
	m.refilter()
	out, _ := m.Update(mouseClick(41, 2))
	m = out.(Model)
	out, _ = m.Update(mouseClick(41, 2))
	m = out.(Model)
	if !m.runView {
		t.Fatalf("need run view for wheel test")
	}
	for i := 0; i < 60; i++ {
		m.appendRunLine("line")
	}
	m.followRun = true
	m.runScroll = m.runMaxScroll()
	out, _ = m.Update(mouseWheel(60, 10, true))
	m = out.(Model)
	if m.followRun || m.runScroll >= m.runMaxScroll() {
		t.Fatalf("wheel up must leave follow mode, scroll=%d max=%d", m.runScroll, m.runMaxScroll())
	}
}
