package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/rom"
)

func sampleGames() []rom.Game {
	return []rom.Game{
		{ID: "aaa111", Title: "Battletoads", ConsoleID: "nes", ConsoleName: "Nintendo Entertainment System", Path: "/x/a.nes", SHA256: "aaa111", Size: 1},
		{ID: "bbb222", Title: "Kirby Mirror", ConsoleID: "gba", ConsoleName: "Game Boy Advance", Path: "/x/b.zip", Inner: "b.gba", SHA256: "bbb222", Size: 2},
	}
}

func sizedModel(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	mm, ok := out.(Model)
	if !ok {
		t.Fatalf("Update did not return ui.Model")
	}
	return mm
}

func TestThreeColumnsRender(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	view := m.View()
	for _, want := range []string{"CONSOLES", "GAMES", "METADATA", "Battletoads", "Battletoads"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
}

func TestConsoleFilterAndFocus(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// Move to NES row: rows are [All, NES, GBA].
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = out.(Model)
	if m.rows[m.consCur].ID != "nes" {
		t.Fatalf("expected nes row, got %q", m.rows[m.consCur].ID)
	}
	if len(m.filtered) != 1 || m.filtered[0].Title != "Battletoads" {
		t.Fatalf("filter broken: %+v", m.filtered)
	}

	// Tab cycles focus consoles -> games -> meta.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = out.(Model)
	if m.focus != PaneGames {
		t.Fatalf("expected games pane, got %d", m.focus)
	}
}
