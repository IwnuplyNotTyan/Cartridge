package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
	"cartridge/internal/playtime"
	"cartridge/internal/rom"
)

var errTestBoom = errors.New("boom")

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

func keyMsg(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	}
	panic("unknown key: " + s)
}

// lastContentLine returns the last non-empty line (skips the outer gap).
func lastContentLine(t *testing.T, view string) string {
	t.Helper()
	lines := strings.Split(view, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	t.Fatalf("view has no content lines\n%s", view)
	return ""
}

func TestThreeColumnsRender(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	view := m.View()
	for _, want := range []string{
		"CONSOLES", "GAMES", "METADATA", // pane titles
		"Battletoads",                   // game + metadata
		"Nintendo Entertainment System", // full console name
		"│",                             // grey dividers, no full borders
		"cartridge",                     // small brand badge, bottom bar
		"? Help",                        // help badge right of the game title
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
	for _, gone := range []string{"▛", "tab/1-3", "panes"} {
		if strings.Contains(view, gone) {
			t.Fatalf("view should not contain %q\n%s", gone, view)
		}
	}
}

func TestConsoleFilterAndFocus(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// Move to NES row: rows are [All, NES, GBA].
	out, _ := m.Update(keyMsg("down"))
	m = out.(Model)
	if m.rows[m.consCur].ID != "nes" {
		t.Fatalf("expected nes row, got %q", m.rows[m.consCur].ID)
	}
	if len(m.filtered) != 1 || m.filtered[0].Title != "Battletoads" {
		t.Fatalf("filter broken: %+v", m.filtered)
	}

	// Tab cycles focus consoles -> games -> meta.
	out, _ = m.Update(keyMsg("tab"))
	m = out.(Model)
	if m.focus != PaneGames {
		t.Fatalf("expected games pane, got %d", m.focus)
	}
}

func TestHelpToggle(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// No short help above the bar by default.
	if view := m.View(); strings.Contains(view, "move up") {
		t.Fatalf("default view must not show help details\n%s", view)
	}
	out, _ := m.Update(keyMsg("?"))
	m = out.(Model)
	if !m.showHelp {
		t.Fatalf("expected full help after ?")
	}
	view := m.View()
	for _, want := range []string{"move up", "rescan", "quit", "CONSOLES", "GAMES", "METADATA"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help view missing %q\n%s", want, view)
		}
	}
	// Full help must sit above the bottom bar: columns stay, bar stays last.
	if last := lastContentLine(t, view); !strings.Contains(last, "cartridge") {
		t.Fatalf("bottom bar must stay last, got %q\n%s", last, view)
	}
	out, _ = m.Update(keyMsg("?"))
	if out.(Model).showHelp {
		t.Fatalf("expected full help to collapse on second ?")
	}
}

func TestOuterGap(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "" {
		t.Fatalf("expected 1-cell top gap, first line %q\n%s", lines[0], view)
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "" {
		t.Fatalf("expected 1-cell bottom gap, last line %q\n%s", lines[len(lines)-1], view)
	}
}

func TestConsolePageUpDown(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	if m.focus != PaneConsoles {
		t.Fatalf("expected consoles focus by default, got %d", m.focus)
	}
	// rows are [All, NES, GBA]: pgdown jumps to the end, pgup back to start.
	out, _ := m.Update(keyMsg("pgdown"))
	m = out.(Model)
	if m.rows[m.consCur].ID != "gba" {
		t.Fatalf("expected gba row after pgdown, got %q", m.rows[m.consCur].ID)
	}
	out, _ = m.Update(keyMsg("pgup"))
	m = out.(Model)
	if m.rows[m.consCur].ID != "all" {
		t.Fatalf("expected all row after pgup, got %q", m.rows[m.consCur].ID)
	}
}

func TestRussianRender(t *testing.T) {
	prev := i18n.Get()
	i18n.Set(i18n.Ru)
	defer i18n.Set(prev)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	view := m.View()
	for _, want := range []string{
		"КОНСОЛИ", "ИГРЫ", "МЕТАДАННЫЕ",
		"Все консоли", "Помощь", "1 байт", "МиБ",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("ru view missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "CONSOLES") {
		t.Fatalf("ru view leaked english\n%s", view)
	}
	// Full help above the bar is translated too.
	out, _ := m.Update(keyMsg("?"))
	view = out.(Model).View()
	for _, want := range []string{"рескан", "выход", "открыть"} {
		if !strings.Contains(view, want) {
			t.Fatalf("ru help missing %q\n%s", want, view)
		}
	}
}

func TestLaunchNoRecipe(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	out, cmd := m.Update(keyMsg("x"))
	m = out.(Model)
	if cmd != nil {
		t.Fatalf("expected no command without recipes")
	}
	if !strings.Contains(m.status, "no recipe") {
		t.Fatalf("status = %q, want no-recipe hint", m.status)
	}
}

func TestLaunchRecordsPlaytime(t *testing.T) {
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	recipes := []emulate.Recipe{{
		Name: "test-nes", Consoles: []string{"nes"},
		Emulator: "true", Args: []string{"{rom}"},
		Cheats: emulate.Cheats{Supported: true, Path: "{emu_dir}/cheats/{title}.cht"},
	}}
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes(recipes), WithPlaytime(play)))
	out, cmd := m.Update(keyMsg("x"))
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("expected launch command")
	}
	if !strings.Contains(m.status, "starting") {
		t.Fatalf("status = %q, want launching hint", m.status)
	}

	out, _ = m.Update(launchDoneMsg{gameID: "aaa111", dur: 90 * time.Second})
	m = out.(Model)
	if !strings.Contains(m.status, "played 1 min 30 s") {
		t.Fatalf("status = %q, want played duration", m.status)
	}
	st, ok := play.Get("aaa111")
	if !ok || st.Sessions != 1 || st.TotalSeconds != 90 {
		t.Fatalf("playtime = %+v ok=%v", st, ok)
	}
	view := m.View()
	for _, want := range []string{"test-nes", "played", "1 min 30 s", "cheats"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
}

func TestLaunchDoneError(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	out, _ := m.Update(launchDoneMsg{gameID: "aaa111", dur: time.Second, err: errTestBoom})
	if got := out.(Model).status; !strings.Contains(got, "launch failed") {
		t.Fatalf("status = %q, want launch failure", got)
	}
}

func TestWrapSoftNoLoss(t *testing.T) {
	// A spaceless hash must survive wrapping without "…" losses.
	src := "aaa111bbb222ccc333ddd444eee555fff666ggg777hhh888iii999"
	var got strings.Builder
	for _, ln := range wrapSoft(src, 10) {
		if lipgloss.Width(ln) > 10 {
			t.Fatalf("line wider than 10: %q", ln)
		}
		got.WriteString(ln)
	}
	if got.String() != src {
		t.Fatalf("wrap lost text: got %q want %q", got.String(), src)
	}
}
