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
	for _, want := range []string{"CONSOLES", "GAMES"} {
		if !strings.Contains(view, want) {
			t.Fatalf("medium view must show %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "METADATA") {
		t.Fatalf("medium view must hide metadata\n%s", view)
	}
	// A selected game keeps metadata on the right: games or meta focus
	// shows the games+meta pair.
	for _, f := range []Pane{PaneGames, PaneMeta} {
		m.focus = f
		view = m.View()
		for _, want := range []string{"GAMES", "METADATA"} {
			if !strings.Contains(view, want) {
				t.Fatalf("focus %v view must show %q\n%s", f, want, view)
			}
		}
		if strings.Contains(view, "CONSOLES") {
			t.Fatalf("focus %v view must hide consoles\n%s", f, view)
		}
	}
	m = resizeModel(t, m, 40)
	m.focus = PaneGames
	view = m.View()
	if !strings.Contains(view, "GAMES") {
		t.Fatalf("tiny view must show focused pane\n%s", view)
	}
	for _, gone := range []string{"CONSOLES", "METADATA"} {
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
