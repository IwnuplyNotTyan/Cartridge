package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/i18n"
)

func TestSteamTabRowsAndToggle(t *testing.T) {
	i18n.Set(i18n.En)
	m := openSettings(t, settingsModel(t))
	m.settTab = TabSteam
	// 2 toggles + 3 fields + 2 consoles (nes, gba) + sync.
	if n := steamRowsFor(m); n != 2+3+2+1 {
		t.Fatalf("steam rows = %d", n)
	}
	view := m.View()
	for _, want := range []string{"Auto-add", "All-games", "NES →", "GBA →"} {
		if !strings.Contains(view, want) {
			t.Fatalf("steam tab must show %q\n%s", want, view)
		}
	}
	// Enter on row 0 toggles auto-add and persists.
	m.settCur = 0
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := out.(Model)
	if !mm.steam.AutoAdd {
		t.Fatalf("enter must toggle auto-add on")
	}
	// Enter on row 1 toggles the all-games root category.
	mm.settCur = 1
	out, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm = out.(Model)
	if !mm.steam.AllGames {
		t.Fatalf("enter must toggle all-games on")
	}
}

func TestSteamToggleHighlight(t *testing.T) {
	i18n.Set(i18n.En)
	m := openSettings(t, settingsModel(t))
	m.settTab = TabSteam
	m.settCur = -1 // no cursor row: plain idle rendering
	// Disabled toggles stay idle with no highlight background.
	if row := m.renderSteamRow(steamRowAuto, 40); !strings.Contains(row, pathIdle.Render("off")) {
		t.Fatalf("disabled auto-add must idle the off word, got %q", row)
	} else if strings.Contains(row, "48;") {
		t.Fatalf("disabled toggle must have no highlight background: %q", row)
	}
	// Enabled toggles glow like hovered text on the on/off word itself.
	m.steam.AutoAdd = true
	if row := m.renderSteamRow(steamRowAuto, 40); !strings.Contains(row, selStyle.Render("on")) {
		t.Fatalf("enabled auto-add must highlight the on word, got %q", row)
	}
	m.steam.AllGames = true
	row := m.renderSteamRow(steamRowAll, 40)
	if !strings.Contains(row, selStyle.Render("on")) {
		t.Fatalf("enabled all-games must highlight the on word, got %q", row)
	}
	// The "(prefix)" suffix stays out of the highlight.
	if !strings.Contains(row, pathIdle.Render(" (Cartridge)")) {
		t.Fatalf("prefix suffix must stay idle, got %q", row)
	}
}

func TestSteamStripsSpaced(t *testing.T) {
	i18n.Set(i18n.En)
	m := openSettings(t, settingsModel(t))
	m.settTab = TabSteam
	lines := m.renderSteamTab(80)
	// Every divider strip breathes: a blank line before and after it.
	strips := 0
	for i, ln := range lines {
		if !strings.Contains(ln, "─") {
			continue
		}
		strips++
		if i == 0 || lines[i-1] != "" {
			t.Fatalf("strip %d must have a blank line before, got %q", strips, lines[max(i-1, 0)])
		}
		if i+1 >= len(lines) || lines[i+1] != "" {
			t.Fatalf("strip %d must have a blank line after", strips)
		}
	}
	// Consoles strip + sync strip.
	if strips != 2 {
		t.Fatalf("steam tab must have 2 strips, got %d", strips)
	}
}

func TestSteamTabCustomName(t *testing.T) {
	i18n.Set(i18n.En)
	m := openSettings(t, settingsModel(t))
	m.settTab = TabSteam
	// Console rows start at steamRowConsoles; find the GBA row.
	idx := -1
	for i, c := range m.steamConsoleRows() {
		if c.ID == "gba" {
			idx = steamRowConsoles + i
		}
	}
	if idx < 0 {
		t.Fatalf("no gba console row")
	}
	m.settCur = idx
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := out.(Model)
	if !mm.settEditing {
		t.Fatalf("enter on console row must start editing")
	}
	mm.settBuf = []rune("My GBA")
	out, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm = out.(Model)
	if mm.steam.Categories["gba"] != "My GBA" {
		t.Fatalf("custom name = %+v", mm.steam.Categories)
	}
	// Custom names apply through the {name} placeholder.
	mm.steam.CategoryTemplate = "{prefix} - {name}"
	if got := mm.steamCategoryFor("gba", "Game Boy Advance"); !strings.Contains(got, "My GBA") {
		t.Fatalf("effective category = %q", got)
	}
}
