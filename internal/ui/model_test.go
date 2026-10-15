package ui

import (
	"bytes"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	rompatcher "github.com/olsonb97/RomPatcher.go"

	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
	"cartridge/internal/overrides"
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
		"Consoles", "Recent", "Metadata", // pane titles
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
	for _, want := range []string{"move up", "rescan", "quit", "Consoles", "Recent", "Metadata"} {
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
	// rows are [All, NES, GBA] + settings entry: pgdown jumps to the
	// settings entry at the bottom, pgup back to the start.
	out, _ := m.Update(keyMsg("pgdown"))
	m = out.(Model)
	if !m.onSettingsRow() {
		t.Fatalf("expected settings row after pgdown, consCur=%d", m.consCur)
	}
	out, _ = m.Update(keyMsg("pgup"))
	m = out.(Model)
	if m.rows[m.consCur].ID != "all" {
		t.Fatalf("expected all row after pgup, got %q", m.rows[m.consCur].ID)
	}
}

func TestConsolesSettingsRow(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	view := m.View()
	if !strings.Contains(view, "Settings") {
		t.Fatalf("consoles must show a settings entry\n%s", view)
	}
	if !strings.Contains(view, "───") {
		t.Fatalf("consoles must separate Home/consoles/settings with ─── rules\n%s", view)
	}
	// Hover the settings row: games+metadata give way to the panel.
	m.consCur = len(m.rows)
	if view := m.View(); strings.Contains(view, "Metadata") {
		t.Fatalf("hovering settings must replace metadata\n%s", view)
	}
	// Enter focuses inside, esc steps back out to the consoles.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if !m.settActive {
		t.Fatalf("enter must focus settings")
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.settActive {
		t.Fatalf("esc must leave settings")
	}
	// Esc from inside steps back to the settings row (hover, panel
	// stays); a second esc on the row returns Home with the full
	// shelf, not a console filter.
	if !m.onSettingsRow() {
		t.Fatalf("esc must step back to the settings row, consCur=%d", m.consCur)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.onSettingsRow() {
		t.Fatalf("esc on the row must leave it, consCur=%d", m.consCur)
	}
	if m.consCur != 0 {
		t.Fatalf("esc must return to Home, consCur=%d", m.consCur)
	}
	if view := m.View(); !strings.Contains(view, "Metadata") {
		t.Fatalf("esc must restore games+metadata\n%s", view)
	}
	if view := m.View(); !strings.Contains(view, "All games") {
		t.Fatalf("esc must restore the full shelf, not a console filter\n%s", view)
	}
	// Left steps off the row too; tab enters settings.
	m.consCur = len(m.rows)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = out.(Model)
	if m.onSettingsRow() {
		t.Fatalf("left must leave the settings row, consCur=%d", m.consCur)
	}
	m.consCur = len(m.rows)
	out, _ = m.Update(keyMsg("tab"))
	if !out.(Model).settActive {
		t.Fatalf("tab must enter settings")
	}
	// h/Left flip tabs, except on the first tab where they step out
	// to the consoles list. Tabs are set explicitly: entering
	// settings keeps the last used tab.
	m.consCur = len(m.rows)
	out, _ = m.Update(keyMsg("tab"))
	m = out.(Model)
	m.settTab = TabRecipes
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = out.(Model)
	if !m.settActive || m.settTab != TabGeneral {
		t.Fatalf("left on recipes must go to previous tab, active=%v tab=%d", m.settActive, m.settTab)
	}
	m.settTab = TabRecipes
	out, _ = m.Update(keyMsg("h"))
	m = out.(Model)
	if !m.settActive || m.settTab != TabGeneral {
		t.Fatalf("h on recipes must go to previous tab, active=%v tab=%d", m.settActive, m.settTab)
	}
	// First tab: h/Left step back to the settings row (hover).
	out, _ = m.Update(keyMsg("h"))
	m = out.(Model)
	if m.settActive || !m.onSettingsRow() {
		t.Fatalf("h on first tab must step back to the row, active=%v consCur=%d", m.settActive, m.consCur)
	}
	m.consCur = len(m.rows)
	out, _ = m.Update(keyMsg("tab"))
	m = out.(Model)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = out.(Model)
	if m.settActive || !m.onSettingsRow() {
		t.Fatalf("left on first tab must step back to the row, active=%v consCur=%d", m.settActive, m.consCur)
	}
	// shift+tab still flips to the previous tab.
	m.consCur = len(m.rows)
	out, _ = m.Update(keyMsg("tab"))
	m = out.(Model)
	m.settTab = TabRecipes
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = out.(Model)
	if !m.settActive || m.settTab != TabGeneral {
		t.Fatalf("shift+tab must go to previous tab, active=%v tab=%d", m.settActive, m.settTab)
	}
}

func TestConsolesEmptyHint(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(nil, "/x", "/tmp/lib.json"))
	view := m.View()
	if !strings.Contains(view, "Insert cartridge") {
		t.Fatalf("empty library must hint to insert cartridge\n%s", view)
	}
}

func TestRussianRender(t *testing.T) {
	prev := i18n.Get()
	i18n.Set(i18n.Ru)
	defer i18n.Set(prev)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	view := m.View()
	for _, want := range []string{
		"Консоли", "Недавние", "Метаданные",
		"Главная", "Помощь", "1 байт", "МиБ",
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
	out, _ := m.Update(keyMsg("x"))
	m = out.(Model)
	if m.runView || m.run != nil {
		t.Fatalf("no recipe must not start a run")
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
	// The log screen stays up (game title, exit line, played status).
	view := m.View()
	for _, want := range []string{"Battletoads", "exited after 1 min 30 s", "played 1 min 30 s"} {
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

// metaCursorTo moves the meta cursor to the first row of the given kind.
func metaCursorTo(t *testing.T, m Model, kind string) Model {
	t.Helper()
	for i := 0; i < 60; i++ {
		if m.metaCur >= 0 && m.metaCur < len(m.metaRows) && m.metaRows[m.metaCur].kind == kind {
			return m
		}
		out, _ := m.Update(keyMsg("down"))
		m = out.(Model)
	}
	t.Fatalf("meta has no %q row", kind)
	return m
}

func TestMetaCursorSkipsSeps(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneMeta
	if m.metaCur != 0 || m.metaRows[m.metaCur].kind != "title" {
		t.Fatalf("cursor must start on title, cur=%d", m.metaCur)
	}
	// title(0) + sep(1) -> id(2).
	out, _ := m.Update(keyMsg("down"))
	m = out.(Model)
	if m.metaCur != 2 || m.metaRows[m.metaCur].kind != "id" {
		t.Fatalf("down must skip separator, cur=%d", m.metaCur)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = out.(Model)
	if m.metaCur != 0 {
		t.Fatalf("up must skip separator back, cur=%d", m.metaCur)
	}
}

func TestMetaCopy(t *testing.T) {
	prev := i18n.Get()
	i18n.Set(i18n.En)
	defer i18n.Set(prev)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneMeta
	m = metaCursorTo(t, m, "id")
	out, cmd := m.Update(keyMsg("c"))
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("copy must return a cmd")
	}
	if !strings.Contains(m.status, "copied id") {
		t.Fatalf("status = %q, want copied id", m.status)
	}
	// enter copies too.
	out, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil || !strings.Contains(m.status, "copied id") {
		t.Fatalf("enter must copy too: status=%q cmd=%v", m.status, cmd)
	}
}

func TestMetaRecipeAltCycle(t *testing.T) {
	prev := i18n.Get()
	i18n.Set(i18n.En)
	defer i18n.Set(prev)
	ext := emulate.Recipe{Name: "ext-nes", Extensions: []string{"nes"}, Emulator: "e1"}
	con := emulate.Recipe{Name: "con-nes", Consoles: []string{"nes"}, Emulator: "e2"}
	ovPath := filepath.Join(t.TempDir(), "overrides.json")
	ov, err := overrides.Load(ovPath)
	if err != nil {
		t.Fatal(err)
	}
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{ext, con}),
		WithOverrides(ov)))
	m.focus = PaneMeta
	m = metaCursorTo(t, m, "recipe")
	if view := m.View(); !strings.Contains(view, "ext-nes (1/2)") {
		t.Fatalf("hierarchy winner must show (1/2)\n%s", view)
	}
	out, _ := m.Update(keyMsg("n"))
	m = out.(Model)
	if got := m.gameRecipe(*m.selected()); got == nil || got.Name != "con-nes" {
		t.Fatalf("n must switch emulator, got %+v", got)
	}
	if view := m.View(); !strings.Contains(view, "con-nes (2/2)") {
		t.Fatalf("view must show (2/2)\n%s", view)
	}
	// The pick persists across restarts.
	reloaded, err := overrides.Load(ovPath)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := reloaded.Get("aaa111")
	if !ok || c.Recipe != "con-nes" {
		t.Fatalf("recipe pick must persist: %+v ok=%v", c, ok)
	}
	out, _ = m.Update(keyMsg("p"))
	m = out.(Model)
	if got := m.gameRecipe(*m.selected()); got == nil || got.Name != "ext-nes" {
		t.Fatalf("p must switch back, got %+v", got)
	}
}

func TestMetaCoreCycle(t *testing.T) {
	prev := i18n.Get()
	i18n.Set(i18n.En)
	defer i18n.Set(prev)
	ra := emulate.Recipe{Name: "ra", Consoles: []string{"nes"}, Emulator: "e1",
		Args:  []string{"-L", "{core}", "{rom}"},
		Cores: map[string][]string{"nes": {"a.so", "b.so"}}}
	ovPath := filepath.Join(t.TempDir(), "overrides.json")
	ov, err := overrides.Load(ovPath)
	if err != nil {
		t.Fatal(err)
	}
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{ra}),
		WithOverrides(ov)))
	m.focus = PaneMeta
	m = metaCursorTo(t, m, "core")
	if view := m.View(); !strings.Contains(view, "a.so") {
		t.Fatalf("default core must show\n%s", view)
	}
	out, _ := m.Update(keyMsg("n"))
	m = out.(Model)
	core, cs, idx := m.gameCore(*m.selected(), m.gameRecipe(*m.selected()))
	if core != "b.so" || len(cs) != 2 || idx != 1 {
		t.Fatalf("core = %q %+v %d", core, cs, idx)
	}
	if view := m.View(); !strings.Contains(view, "b.so (2/2)") {
		t.Fatalf("view must show picked core\n%s", view)
	}
	// The pick persists across restarts.
	reloaded, err := overrides.Load(ovPath)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := reloaded.Get("aaa111")
	if !ok || c.Core != "b.so" {
		t.Fatalf("core pick must persist: %+v ok=%v", c, ok)
	}
	// Launch path works with the picked core.
	out, cmd := m.Update(keyMsg("x"))
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("expected launch command with picked core")
	}
	_ = m
}

func TestMetaCursorBarFocusOnly(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// Bars mark the cursor row instead of "> " arrows, and only while
	// the metadata pane is focused.
	m.focus = PaneMeta
	content, _ := m.renderMetaRows()
	if !strings.Contains(content, "│") {
		t.Fatalf("focused meta must mark cursor with a bar\n%s", content)
	}
	if strings.Contains(content, "> ") {
		t.Fatalf("meta must not use arrow markers\n%s", content)
	}
	m.focus = PaneGames
	content, _ = m.renderMetaRows()
	if strings.Contains(content, "│") {
		t.Fatalf("unfocused meta must not highlight\n%s", content)
	}
}

// resizeModel applies a terminal size to the model.
func resizeModel(t *testing.T, m Model, w int) Model {
	t.Helper()
	out, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
	mm, ok := out.(Model)
	if !ok {
		t.Fatalf("Update did not return ui.Model")
	}
	return mm
}

func TestAdaptiveVisiblePanes(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// Wide screen (120 → content 118): all three panes.
	if got := m.visiblePanes(); len(got) != 3 {
		t.Fatalf("wide must show 3 panes, got %v", got)
	}
	// Half screen (70 → content 68): consoles focus keeps the left
	// pair, games/meta focus shows metadata on the right.
	m = resizeModel(t, m, 70)
	m.focus = PaneConsoles
	if got := m.visiblePanes(); len(got) != 2 || got[0] != PaneConsoles || got[1] != PaneGames {
		t.Fatalf("consoles focus must show consoles+games, got %v", got)
	}
	m.focus = PaneGames
	if got := m.visiblePanes(); len(got) != 2 || got[0] != PaneGames || got[1] != PaneMeta {
		t.Fatalf("games focus must show games+meta, got %v", got)
	}
	m.focus = PaneMeta
	if got := m.visiblePanes(); len(got) != 2 || got[0] != PaneGames || got[1] != PaneMeta {
		t.Fatalf("meta focus must show games+meta, got %v", got)
	}
	// Tiny screen (40 → content 38): only the focused pane.
	m = resizeModel(t, m, 40)
	for _, f := range []Pane{PaneConsoles, PaneGames, PaneMeta} {
		m.focus = f
		if got := m.visiblePanes(); len(got) != 1 || got[0] != f {
			t.Fatalf("tiny must show only focus %v, got %v", f, got)
		}
	}
}

func TestAdaptiveViewHidesPanes(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m = resizeModel(t, m, 70)
	m.focus = PaneConsoles
	view := m.View()
	for _, want := range []string{"Consoles", "Recent"} {
		if !strings.Contains(view, want) {
			t.Fatalf("medium view must show %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "Metadata") {
		t.Fatalf("medium view must hide metadata\n%s", view)
	}
	// A selected game keeps metadata on the right: games or meta focus
	// shows the games+meta pair.
	for _, f := range []Pane{PaneGames, PaneMeta} {
		m.focus = f
		view = m.View()
		for _, want := range []string{"Recent", "Metadata"} {
			if !strings.Contains(view, want) {
				t.Fatalf("focus %v view must show %q\n%s", f, want, view)
			}
		}
		if strings.Contains(view, "Consoles") {
			t.Fatalf("focus %v view must hide consoles\n%s", f, view)
		}
	}
	m = resizeModel(t, m, 40)
	m.focus = PaneGames
	view = m.View()
	if !strings.Contains(view, "Recent") {
		t.Fatalf("tiny view must show focused pane\n%s", view)
	}
	for _, gone := range []string{"Consoles", "Metadata"} {
		if strings.Contains(view, gone) {
			t.Fatalf("tiny view must hide %q\n%s", gone, view)
		}
	}
}

// TestNoOverflowOnResize resizes up and down across breakpoints and
// requires every rendered line to fit the terminal: the bar used to
// overflow on narrow screens and stretch the whole view with it.
func TestNoOverflowOnResize(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	for _, w := range []int{120, 100, 70, 60, 56, 40, 30, 24} {
		for _, f := range []Pane{PaneConsoles, PaneGames, PaneMeta} {
			mm := resizeModel(t, m, w)
			mm.focus = f
			mm.status = strings.Repeat("st", 60)
			for i, ln := range strings.Split(mm.View(), "\n") {
				if got := lipgloss.Width(ln); got > w {
					t.Fatalf("w=%d focus=%v line %d overflows: %d\n%s", w, f, i, got, mm.View())
				}
			}
		}
		m = resizeModel(t, m, w)
	}
}

func TestGameSwitchResetsMetaTop(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneGames
	// Pretend the cursor sat deep in a long meta card.
	m.metaCur = 5
	m.meta.SetYOffset(3)
	out, _ := m.Update(keyMsg("down")) // Battletoads -> Kirby
	m = out.(Model)
	if m.gameCur != 1 {
		t.Fatalf("gameCur = %d", m.gameCur)
	}
	if m.metaCur != 0 {
		t.Fatalf("meta cursor must reset on game switch, got %d", m.metaCur)
	}
	if m.meta.YOffset != 0 {
		t.Fatalf("meta scroll must reset on game switch, got %d", m.meta.YOffset)
	}
	// The new game's title must be visible, not a stale deep tail.
	view := m.View()
	if !strings.Contains(view, "Kirby Mirror") {
		t.Fatalf("new game title must show\n%s", view)
	}
}

func TestMetaResyncsWidthOnFocusChange(t *testing.T) {
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m = resizeModel(t, m, 70)
	// Focus consoles: meta hidden, its viewport width collapses.
	m.focus = PaneConsoles
	m.sizeMeta()
	m.refreshMeta()
	if m.meta.Width != 10 {
		t.Fatalf("hidden meta width = %d, want 10", m.meta.Width)
	}
	// Tab over to metadata: it must render full pane width again,
	// not stay cut to ~10 cells by the stale viewport width.
	out, _ := m.Update(keyMsg("tab")) // games
	m = out.(Model)
	out, _ = m.Update(keyMsg("tab")) // meta
	m = out.(Model)
	if m.meta.Width <= 10 {
		t.Fatalf("shown meta width = %d, want full pane", m.meta.Width)
	}
	maxW := 0
	for _, ln := range strings.Split(m.meta.View(), "\n") {
		if w := lipgloss.Width(ln); w > maxW {
			maxW = w
		}
	}
	if maxW <= 10 {
		t.Fatalf("meta lines cut to %d cells after focus change", maxW)
	}
	if maxW > m.meta.Width {
		t.Fatalf("meta lines overflow pane: %d > %d", maxW, m.meta.Width)
	}
}

const testCHT = `cheats = 2

cheat0_desc = "Infinite Lives"
cheat0_code = "AAA"
cheat0_enable = false

cheat1_desc = "Health"
cheat1_code = "BBB"
cheat1_enable = true
`

// cheatTestModel builds a browser model whose nes recipe points cheats
// at chtPath (a plain absolute path needs no placeholders).
func cheatTestModel(t *testing.T, chtPath string) Model {
	t.Helper()
	i18n.Set(i18n.En)
	r := emulate.Recipe{Name: "ra-nes", Consoles: []string{"nes"}, Emulator: "e1",
		Args:   []string{"{rom}"},
		Cheats: emulate.Cheats{Supported: true, Path: chtPath}}
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{r}))
	m = sizedModel(t, m)
	m.focus = PaneMeta
	return m
}

func TestMetaCheatsExpand(t *testing.T) {
	cht := filepath.Join(t.TempDir(), "g.cht")
	if err := os.WriteFile(cht, []byte(testCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	m := cheatTestModel(t, cht)
	m = metaCursorTo(t, m, "cheats")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if !m.cheatsOpen {
		t.Fatalf("enter must expand the list")
	}
	view := m.View()
	for _, want := range []string{"Infinite Lives", "Health"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expanded list must show %q\n%s", want, view)
		}
	}
	// Expanding normalizes everything to enabled on disk.
	data, err := os.ReadFile(cht)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "_enable = true"); got != 2 {
		t.Fatalf("all cheats must save enabled:\n%s", data)
	}
}

func TestMetaCheatRowCopies(t *testing.T) {
	cht := filepath.Join(t.TempDir(), "g.cht")
	if err := os.WriteFile(cht, []byte(testCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	m := cheatTestModel(t, cht)
	m = metaCursorTo(t, m, "cheats")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	m = metaCursorTo(t, m, "cheat")
	// Enter on a cheat copies its code; space does the same.
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, keyMsg(" ")} {
		out, cmd := m.Update(key)
		m = out.(Model)
		if cmd == nil {
			t.Fatalf("cheat row must copy")
		}
		if !strings.Contains(m.status, "copied") {
			t.Fatalf("status = %q", m.status)
		}
	}
}

// pileLiveTestModel builds a browser model whose recipe cheat template
// points at a missing live path while the pile dir holds a match.
func pileLiveTestModel(t *testing.T, live, pileDir, pileBody string) Model {
	t.Helper()
	i18n.Set(i18n.En)
	if err := os.WriteFile(filepath.Join(pileDir, "a.cht"), []byte(pileBody), 0o644); err != nil {
		t.Fatal(err)
	}
	r := emulate.Recipe{Name: "ra-nes", Consoles: []string{"nes"}, Emulator: "e1",
		Args:   []string{"{rom}"},
		Cheats: emulate.Cheats{Supported: true, Path: live}}
	ov, err := overrides.Load(filepath.Join(t.TempDir(), "overrides.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{r}),
		WithOverrides(ov))
	m.cheatsDir = pileDir
	m = sizedModel(t, m)
	m.focus = PaneMeta
	return m
}

func TestMetaMaterializeFromPile(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live", "game.cht")
	m := pileLiveTestModel(t, live, dir, testCHT)
	// Missing live file hides the section entirely now.
	for _, r := range m.metaRows {
		if !r.sep && (r.kind == "cheats" || r.kind == "cheat") {
			t.Fatalf("missing live file must hide cheats, got %+v", r)
		}
	}
}

func TestMetaMaterializeSkeleton(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live", "game.cht")
	m := pileLiveTestModel(t, live, dir, "cheats = 0\n")
	// Missing live file hides the section entirely now.
	for _, r := range m.metaRows {
		if !r.sep && (r.kind == "cheats" || r.kind == "cheat") {
			t.Fatalf("missing live file must hide cheats, got %+v", r)
		}
	}
}

func TestMetaCheatsEscCollapse(t *testing.T) {
	cht := filepath.Join(t.TempDir(), "g.cht")
	if err := os.WriteFile(cht, []byte(testCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	m := cheatTestModel(t, cht)
	m = metaCursorTo(t, m, "cheats")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.cheatsOpen {
		t.Fatalf("esc must collapse the list")
	}
	if view := m.View(); strings.Contains(view, "Infinite Lives") {
		t.Fatalf("collapsed list must hide cheats\n%s", view)
	}
}

func TestMetaCheatsNoFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.cht")
	m := cheatTestModel(t, missing)
	// Missing file hides the section entirely now.
	for _, r := range m.metaRows {
		if !r.sep && (r.kind == "cheats" || r.kind == "cheat") {
			t.Fatalf("missing file must hide cheats, got %+v", r)
		}
	}
}

// metaRowText returns the joined plain text of the first meta row of
// kind. Unlike View assertions it never breaks on line wraps, which move
// with the temp-dir path length.
func metaRowText(t *testing.T, m Model, kind string) string {
	t.Helper()
	for _, r := range m.metaRows {
		if !r.sep && r.kind == kind {
			return strings.Join(r.lines, " ")
		}
	}
	t.Fatalf("no %q row in meta", kind)
	return ""
}

func TestMetaCheatsBadFile(t *testing.T) {
	dir := t.TempDir() // a directory is not a readable cheat file
	m := cheatTestModel(t, dir)
	m = metaCursorTo(t, m, "cheats")
	if got := metaRowText(t, m, "cheats"); !strings.Contains(got, "(bad file)") {
		t.Fatalf("unreadable file must show, got %q", got)
	}
}

func TestMetaCheatsCloseOnGameSwitch(t *testing.T) {
	cht := filepath.Join(t.TempDir(), "g.cht")
	if err := os.WriteFile(cht, []byte(testCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	m := cheatTestModel(t, cht)
	m = metaCursorTo(t, m, "cheats")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	m.focus = PaneGames
	out, _ = m.Update(keyMsg("down")) // Battletoads(nes) -> Kirby(gba)
	m = out.(Model)
	if m.cheatsOpen {
		t.Fatalf("game switch must close the list")
	}
	if view := m.View(); strings.Contains(view, "Infinite Lives") {
		t.Fatalf("cheats must follow the game\n%s", view)
	}
}

// pileTestModel builds a browser model with no recipe cheat path and a
// shared cheats dir + overrides store, like a dumped cheat pile.
func pileTestModel(t *testing.T, dir, ovPath string) Model {
	t.Helper()
	i18n.Set(i18n.En)
	r := emulate.Recipe{Name: "ra-nes", Consoles: []string{"nes"}, Emulator: "e1",
		Args:   []string{"{rom}"},
		Cheats: emulate.Cheats{Supported: true}}
	ov, err := overrides.Load(ovPath)
	if err != nil {
		t.Fatal(err)
	}
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{r}),
		WithOverrides(ov))
	m.cheatsDir = dir
	m = sizedModel(t, m)
	m.focus = PaneMeta
	return m
}

const pileCHT = `cheats = 2

cheat0_desc = "Pile One"
cheat0_code = "AAA"
cheat0_enable = true

cheat1_desc = "Pile Two"
cheat1_code = "BBB"
cheat1_enable = true
`

func TestMetaCheatsAutoDetect(t *testing.T) {
	dir := t.TempDir()
	// ROM base is "a" (/x/a.nes); title file must not shadow it.
	if err := os.WriteFile(filepath.Join(dir, "a.cht"), []byte(pileCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Battletoads.cht"), []byte("cheats = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := pileTestModel(t, dir, filepath.Join(t.TempDir(), "overrides.json"))
	m = metaCursorTo(t, m, "cheats")
	// ROM base wins over title: the pile file resolves, not the empty one.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if view := m.View(); !strings.Contains(view, "Pile One") {
		t.Fatalf("auto-detect must resolve the pile file\n%s", view)
	}
}

func TestMetaCheatsTemplateWins(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.cht"), []byte(pileCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(t.TempDir(), "hand.cht")
	if err := os.WriteFile(explicit, []byte(testCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	i18n.Set(i18n.En)
	r := emulate.Recipe{Name: "ra-nes", Consoles: []string{"nes"}, Emulator: "e1",
		Args:   []string{"{rom}"},
		Cheats: emulate.Cheats{Supported: true, Path: explicit}}
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{r}))
	m.cheatsDir = dir
	m = sizedModel(t, m)
	m.focus = PaneMeta
	m = metaCursorTo(t, m, "cheats")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if view := m.View(); !strings.Contains(view, "Infinite Lives") {
		t.Fatalf("explicit path must win over the pile\n%s", view)
	}
}

func TestMetaCheatsPileMissShowsNoFile(t *testing.T) {
	m := pileTestModel(t, t.TempDir(), filepath.Join(t.TempDir(), "overrides.json"))
	// Pile miss hides the section entirely now, not bad file.
	for _, r := range m.metaRows {
		if !r.sep && (r.kind == "cheats" || r.kind == "cheat") {
			t.Fatalf("pile miss must hide cheats, got %+v", r)
		}
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

func TestTruncateLeftKeepsTail(t *testing.T) {
	if got := truncateLeft("abcdef", 10); got != "abcdef" {
		t.Fatalf("short text must pass through: %q", got)
	}
	got := truncateLeft("abcdefghij", 5)
	if !strings.HasSuffix(got, "ghij") || !strings.HasPrefix(got, "…") {
		t.Fatalf("tail must survive: %q", got)
	}
	if w := lipgloss.Width(got); w != 5 {
		t.Fatalf("width = %d, want 5: %q", w, got)
	}
	if got := truncateLeft("abcdefghij", 1); got != "…" {
		t.Fatalf("n=1 must be ellipsis: %q", got)
	}
}

func TestEditLineKeepsCursorVisible(t *testing.T) {
	long := strings.Repeat("ab", 60) // 120 cells of buffer
	line := editLine("    ", "cores", long, 40)
	plain := stripANSI(line)
	if lipgloss.Width(plain) != 40 {
		t.Fatalf("width = %d, want 40: %q", lipgloss.Width(plain), plain)
	}
	if !strings.HasPrefix(plain, "    cores: ") {
		t.Fatalf("label must stay: %q", plain)
	}
	if !strings.HasSuffix(plain, "▊") {
		t.Fatalf("cursor must stay on screen: %q", plain)
	}
	if !strings.Contains(plain, "…") {
		t.Fatalf("overflow must mark the cut head: %q", plain)
	}
	// The visible tail must be the buffer end, not its head.
	tail := strings.TrimSuffix(strings.TrimPrefix(plain, "    cores: …"), "▊")
	if !strings.HasSuffix(long, tail) {
		t.Fatalf("tail %q is not the buffer end", tail)
	}
	// Short buffers render in full.
	line = editLine("  ", "Games", "/new/games", 40)
	if plain := stripANSI(line); !strings.Contains(plain, "/new/games▊") {
		t.Fatalf("short buffer must show fully: %q", plain)
	}
}

// stripANSI drops escape sequences for plain-text assertions.
func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && (r == 'm' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// modTestModel builds a browser model whose nes recipe supports mods
// with the shared pile dir.
func modTestModel(t *testing.T, pileDir string) Model {
	t.Helper()
	i18n.Set(i18n.En)
	r := emulate.Recipe{Name: "ra-nes", Consoles: []string{"nes"}, Emulator: "e1",
		Args: []string{"{rom}"},
		Mods: emulate.Mods{Supported: true, Path: filepath.Join(t.TempDir(), "live-mods")}}
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{r}))
	m.modsDir = pileDir
	m = sizedModel(t, m)
	m.focus = PaneMeta
	return m
}

func TestMetaModsExpand(t *testing.T) {
	dir := t.TempDir()
	// ROM base is "a" (/x/a.nes): the pile match wins by stem.
	if err := os.WriteFile(filepath.Join(dir, "a.ips"), []byte("PATCH"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := modTestModel(t, dir)
	m = metaCursorTo(t, m, "mods")
	if got := metaRowText(t, m, "mods"); !strings.Contains(got, "(1)") {
		t.Fatalf("mods row must show count, got %q", got)
	}
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if !m.modsOpen {
		t.Fatalf("enter must expand mods")
	}
	if view := m.View(); !strings.Contains(view, "a.ips") {
		t.Fatalf("expanded mods must show file\n%s", view)
	}
	// Esc collapses the list.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.modsOpen {
		t.Fatalf("esc must collapse mods")
	}
}

// modDeployModel builds a browser model over a real temp games dir so
// instance builds actually touch the disk. The pile holds hack.ips for
// the game game.nes; patched instances go to a temp dir.
func modDeployModel(t *testing.T) (Model, string, string, string) {
	t.Helper()
	i18n.Set(i18n.En)
	gamesDir := t.TempDir()
	romPath := filepath.Join(gamesDir, "game.nes")
	if err := os.WriteFile(romPath, bytes.Repeat([]byte{0xAB}, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	pile := t.TempDir()
	src := filepath.Join(pile, "game", "hack.ips")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	// A valid IPS fitting the 64-byte ROM: no [bad base]/[bad file] marks.
	writeIPSFile(t, src, 10, []byte("HELLO"))
	games := []rom.Game{{
		ID: "aaa111", Title: "game", ConsoleID: "nes",
		ConsoleName: "Nintendo Entertainment System", Path: romPath,
		SHA256: "aaa111",
	}}
	r := emulate.Recipe{Name: "ra-nes", Consoles: []string{"nes"}, Emulator: "e1",
		Args: []string{"{rom}"},
		Mods: emulate.Mods{Supported: true}}
	m := New(games, gamesDir, "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{r}))
	m.modsDir = pile
	m.patchedDir = t.TempDir()
	m = sizedModel(t, m)
	m.focus = PaneMeta
	return m, romPath, src, m.patchedDir
}

func TestMetaModsDeployShowsExactName(t *testing.T) {
	m, _, _, _ := modDeployModel(t)
	m = metaCursorTo(t, m, "mods")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	m = metaCursorTo(t, m, "mod")
	// The row names the exact deployed file RetroArch wants.
	if got := metaRowText(t, m, "mod"); !strings.Contains(got, "hack.ips → game.ips") {
		t.Fatalf("mod row must show pile → target, got %q", got)
	}
}

func TestMetaModsEnterLaunchesInstance(t *testing.T) {
	m, romPath, _, patchedDir := modDeployModel(t)
	m = metaCursorTo(t, m, "mods")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	m = metaCursorTo(t, m, "mod")
	// Enter builds the hard-patched instance and launches it.
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("enter must launch the patched instance")
	}
	inst := filepath.Join(patchedDir, "game [hack].nes")
	data, err := os.ReadFile(inst)
	if err != nil {
		t.Fatalf("instance must be built: %v", err)
	}
	rom, _ := os.ReadFile(romPath)
	want := append([]byte(nil), rom...)
	copy(want[10:], "HELLO")
	for i := range want {
		if data[i] != want[i] {
			t.Fatalf("instance byte %d differs", i)
		}
	}
	if !strings.Contains(m.status, "hack.ips") {
		t.Fatalf("status must name the mod, got %q", m.status)
	}
	if got := metaRowText(t, m, "mod"); !strings.Contains(got, "[built]") {
		t.Fatalf("built mod must show built, got %q", got)
	}
	// The pile source and the original ROM are untouched.
	if data, _ := os.ReadFile(romPath); !bytes.Equal(data, rom) {
		t.Fatalf("original ROM must survive")
	}
}

func TestMetaModsEnterBuildFail(t *testing.T) {
	m, _, _, _ := modDeployModel(t)
	// Swap the pile file for a BPS built for another dump.
	bad := filepath.Join(m.modsDir, "game", "hack.ips")
	other := bytes.Repeat([]byte{0xCD}, 64)
	patch := mustCreateMod(t, other, mutateMod(other, 20, []byte("X")))
	if err := os.WriteFile(bad, patch, 0o644); err != nil {
		t.Fatal(err)
	}
	// Force a recollect: the pile dir mtime may not have changed.
	m.modKey = ""
	m = metaCursorTo(t, m, "mods")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	m = metaCursorTo(t, m, "mod")
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.runView || m.run != nil {
		t.Fatalf("broken base must not launch")
	}
	if !strings.Contains(m.status, "does not match") {
		t.Fatalf("status must explain the mismatch, got %q", m.status)
	}
}

// mustCreateMod builds a BPS patch in tests via RomPatcher.go itself.
func mustCreateMod(t *testing.T, original, modified []byte) []byte {
	t.Helper()
	p, err := rompatcher.Create(original, modified, rompatcher.FormatBPS, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mutateMod(rom []byte, off int, payload []byte) []byte {
	out := append([]byte(nil), rom...)
	copy(out[off:], payload)
	return out
}

func TestMetaModsSoftpatchToggle(t *testing.T) {
	m, romPath, _, _ := modDeployModel(t)
	// game.ips occupied by foreign bytes: d must stack to game.ips1.
	if err := os.WriteFile(filepath.Join(filepath.Dir(romPath), "game.ips"), []byte("FOREIGN"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = metaCursorTo(t, m, "mods")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	m = metaCursorTo(t, m, "mod")
	if got := metaRowText(t, m, "mod"); !strings.Contains(got, "→ game.ips1") {
		t.Fatalf("occupied slot must stack, got %q", got)
	}
	out, _ = m.Update(keyMsg("d"))
	m = out.(Model)
	if _, err := os.Stat(filepath.Join(filepath.Dir(romPath), "game.ips1")); err != nil {
		t.Fatalf("stacked softpatch must land: %v", err)
	}
	// d again removes our copy but never the foreign game.ips.
	m = metaCursorTo(t, m, "mod")
	_, _ = m.Update(keyMsg("d"))
	if _, err := os.Stat(filepath.Join(filepath.Dir(romPath), "game.ips1")); !os.IsNotExist(err) {
		t.Fatalf("d must remove our softpatch copy")
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(romPath), "game.ips")); err != nil || string(data) != "FOREIGN" {
		t.Fatalf("foreign game.ips must survive, err=%v", err)
	}
}

// ipsBytes builds a minimal valid IPS: PATCH + one record + EOF.
func ipsBytes(off int, payload []byte) []byte {
	data := []byte("PATCH")
	data = append(data, byte(off>>16), byte(off>>8), byte(off))
	data = append(data, byte(len(payload)>>8), byte(len(payload)))
	data = append(data, payload...)
	return append(data, "EOF"...)
}

func writeIPSFile(t *testing.T, path string, off int, payload []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, ipsBytes(off, payload), 0o644); err != nil {
		t.Fatal(err)
	}
}

// encodeUPSVarint encodes a byuu-style varint (test-side).
func encodeUPSVarint(n uint64) []byte {
	var out []byte
	for {
		b := byte(n & 0x7F)
		n >>= 7
		if n == 0 {
			return append(out, 0x80|b)
		}
		out = append(out, b)
		n--
	}
}

func appendU32Test(data []byte, v uint32) []byte {
	return append(data, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// snesModModel builds a browser model over a real temp games dir with one
// SNES game and one pile file for it, returning model, rom path, pile path.
func snesModModel(t *testing.T, romSize int, pileName string, pileBytes []byte) (Model, string, string) {
	t.Helper()
	i18n.Set(i18n.En)
	gamesDir := t.TempDir()
	romPath := filepath.Join(gamesDir, "smw.sfc")
	if err := os.WriteFile(romPath, bytes.Repeat([]byte{0xAB}, romSize), 0o644); err != nil {
		t.Fatal(err)
	}
	pile := t.TempDir()
	src := filepath.Join(pile, pileName)
	if err := os.WriteFile(src, pileBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	games := []rom.Game{{
		ID: "ccc333", Title: "smw", ConsoleID: "snes",
		ConsoleName: "Super Nintendo", Path: romPath, SHA256: "ccc333",
	}}
	r := emulate.Recipe{Name: "ra-snes", Consoles: []string{"snes"}, Emulator: "e1",
		Args: []string{"{rom}"},
		Mods: emulate.Mods{Supported: true}}
	m := New(games, gamesDir, "/tmp/lib.json",
		WithRecipes([]emulate.Recipe{r}))
	m.modsDir = pile
	m = sizedModel(t, m)
	m.focus = PaneMeta
	return m, romPath, src
}

func openModsList(t *testing.T, m Model) Model {
	t.Helper()
	m = metaCursorTo(t, m, "mods")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return out.(Model)
}

func TestMetaModsBadBaseHeaderHint(t *testing.T) {
	// BPS built for the headered dump (+512) vs a headerless SNES ROM:
	// [bad base] plus the SMC-header hint.
	headered := bytes.Repeat([]byte{0xAB}, 1024+512)
	var data []byte
	data = append(data, "BPS1"...)
	data = append(data, encodeUPSVarint(uint64(len(headered)))...)
	data = append(data, encodeUPSVarint(uint64(len(headered)))...)
	data = append(data, encodeUPSVarint(0)...)
	data = appendU32Test(data, crc32.Checksum(headered, crc32.IEEETable))
	data = appendU32Test(data, crc32.Checksum(headered, crc32.IEEETable))
	data = appendU32Test(data, crc32.Checksum(data, crc32.IEEETable))
	m, _, _ := snesModModel(t, 1024, "smw.bps", data)
	m = openModsList(t, m)
	m = metaCursorTo(t, m, "mod")
	got := metaRowText(t, m, "mod")
	for _, want := range []string{"smw.bps → smw.bps", "[bad base]", "SMC header"} {
		if !strings.Contains(got, want) {
			t.Fatalf("row must show %q, got %q", want, got)
		}
	}
}

func TestMetaModsBrokenFile(t *testing.T) {
	// An HTML page saved as .ips (classic bad download).
	m, _, _ := snesModModel(t, 64, "smw.ips", []byte("<html>not a patch</html>"))
	m = openModsList(t, m)
	m = metaCursorTo(t, m, "mod")
	if got := metaRowText(t, m, "mod"); !strings.Contains(got, "[bad file]") {
		t.Fatalf("html download must show bad file, got %q", got)
	}
}

func TestMetaModsOKNoBadge(t *testing.T) {
	rom := bytes.Repeat([]byte{0xAB}, 64)
	var data []byte
	data = append(data, "BPS1"...)
	data = append(data, encodeUPSVarint(uint64(len(rom)))...)
	data = append(data, encodeUPSVarint(uint64(len(rom)))...)
	data = append(data, encodeUPSVarint(0)...)
	data = appendU32Test(data, crc32.Checksum(rom, crc32.IEEETable))
	data = appendU32Test(data, crc32.Checksum(rom, crc32.IEEETable))
	data = appendU32Test(data, crc32.Checksum(data, crc32.IEEETable))
	m, _, _ := snesModModel(t, 64, "smw.bps", data)
	m = openModsList(t, m)
	m = metaCursorTo(t, m, "mod")
	if got := metaRowText(t, m, "mod"); strings.Contains(got, "[") {
		t.Fatalf("matching bps must have no badge, got %q", got)
	}
}

func TestMetaModsNoFile(t *testing.T) {
	m := modTestModel(t, t.TempDir())
	for _, r := range m.metaRows {
		if !r.sep && (r.kind == "mods" || r.kind == "mod") {
			t.Fatalf("empty pile must hide mods, got %+v", r)
		}
	}
}

func TestSetGamesFiltersInvalid(t *testing.T) {
	i18n.Set(i18n.En)
	games := append(sampleGames(),
		rom.Game{ID: "ghost1", Title: "hack", ConsoleID: "unknown", ConsoleName: "Unknown", Path: "/x/hack.ips"},
		rom.Game{ID: "ghost2", Title: "readme", ConsoleID: "unknown", ConsoleName: "Unknown", Path: "/x/b.zip", Inner: "readme.txt"},
		rom.Game{ID: "ghost3", Title: "notes", ConsoleID: "unknown", ConsoleName: "Unknown", Path: "/x/notes.txt"},
	)
	m := sizedModel(t, New(games, "/x", "/tmp/lib.json"))
	if len(m.games) != len(sampleGames()) {
		t.Fatalf("stale mod/junk rows must be filtered, got %+v", m.games)
	}
	for _, g := range m.games {
		if !rom.ValidGame(g) {
			t.Fatalf("invalid game leaked into view: %+v", g)
		}
	}
}

// typeIntoSearch feeds runes one by one like real typing.
func typeIntoSearch(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = out.(Model)
	}
	return m
}

func startSearch(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(keyMsg("/"))
	m = out.(Model)
	if !m.searching {
		t.Fatalf("slash must open search")
	}
	return m
}

func TestSearchLiveFilter(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneGames
	m = startSearch(t, m)
	m = typeIntoSearch(t, m, "kir")
	if len(m.filtered) != 1 || m.filtered[0].Title != "Kirby Mirror" {
		t.Fatalf("live filter broken: %+v", m.filtered)
	}
	if view := m.View(); !strings.Contains(view, "/kir") {
		t.Fatalf("search line must show input\n%s", view)
	}
	// Backspace to empty restores the full list with a hint.
	for range 3 {
		out, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = out.(Model)
	}
	if len(m.filtered) != 2 {
		t.Fatalf("empty input must restore all games: %+v", m.filtered)
	}
	if view := m.View(); !strings.Contains(view, "type to filter") {
		t.Fatalf("empty search must hint\n%s", view)
	}
	// Esc drops the input entirely.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.searching || m.query != "" || len(m.filtered) != 2 {
		t.Fatalf("esc must clear search: searching=%v query=%q %+v", m.searching, m.query, m.filtered)
	}
}

func TestSearchEnterKeepsEscClears(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneGames
	m = startSearch(t, m)
	m = typeIntoSearch(t, m, "battle")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.searching || m.query != "battle" {
		t.Fatalf("enter must commit filter: searching=%v query=%q", m.searching, m.query)
	}
	if len(m.filtered) != 1 || m.focus != PaneGames {
		t.Fatalf("committed filter broken: %+v focus=%d", m.filtered, m.focus)
	}
	if view := m.View(); !strings.Contains(view, "/battle") {
		t.Fatalf("games title must show the filter\n%s", view)
	}
	// Slash reopens with the committed filter prefilled for refining.
	m = startSearch(t, m)
	if view := m.View(); !strings.Contains(view, "/battle") {
		t.Fatalf("reopened search must prefill\n%s", view)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.searching || m.query != "" || len(m.filtered) != 2 {
		t.Fatalf("esc must clear committed filter: %+v", m.query)
	}
}

func TestSearchNoMatches(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneGames
	m = startSearch(t, m)
	m = typeIntoSearch(t, m, "zzz")
	if len(m.filtered) != 0 {
		t.Fatalf("must match nothing: %+v", m.filtered)
	}
	view := m.View()
	for _, want := range []string{"/zzz", "no matches"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view must show %q\n%s", want, view)
		}
	}
}

func TestSearchRespectsConsole(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	// Rows are [All, NES, GBA]: down moves to NES.
	out, _ := m.Update(keyMsg("down"))
	m = out.(Model)
	if m.rows[m.consCur].ID != "nes" {
		t.Fatalf("expected nes row, got %q", m.rows[m.consCur].ID)
	}
	m = startSearch(t, m)
	m = typeIntoSearch(t, m, "kirby")
	if len(m.filtered) != 0 {
		t.Fatalf("search must stay inside the console scope: %+v", m.filtered)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	m = startSearch(t, m)
	m = typeIntoSearch(t, m, "battle")
	if len(m.filtered) != 1 {
		t.Fatalf("in-scope search broken: %+v", m.filtered)
	}
}

func TestSearchArrowsHelpWidths(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneGames
	out, _ := m.Update(keyMsg("?"))
	m = out.(Model)
	m = startSearch(t, m)
	m = typeIntoSearch(t, m, "o") // both sample games contain o
	if len(m.filtered) != 2 {
		t.Fatalf("expected both games, got %+v", m.filtered)
	}
	out, _ = m.Update(keyMsg("down"))
	m = out.(Model)
	if m.gameCur != 1 || !m.searching {
		t.Fatalf("arrows must browse without leaving search: cur=%d", m.gameCur)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = out.(Model)
	if m.gameCur != 0 {
		t.Fatalf("up must move back: cur=%d", m.gameCur)
	}
	// Help lists the search key; the search line fits narrow screens.
	if view := m.View(); !strings.Contains(view, "search") {
		t.Fatalf("help must document search\n%s", view)
	}
	for _, w := range []int{120, 70, 40, 24} {
		mm := resizeModel(t, m, w)
		for i, ln := range strings.Split(mm.View(), "\n") {
			if got := lipgloss.Width(ln); got > w {
				t.Fatalf("w=%d line %d overflows: %d\n%s", w, i, got, mm.View())
			}
		}
	}
}

func TestSearchIgnoredInSettings(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	out, _ := m.Update(keyMsg("/"))
	if out.(Model).searching {
		t.Fatalf("slash in settings must not start search")
	}
}
