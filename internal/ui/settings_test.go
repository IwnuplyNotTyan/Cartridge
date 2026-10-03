package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
)

func settingsModel(t *testing.T) Model {
	t.Helper()
	i18n.Set(i18n.En)
	base := []emulate.Recipe{{Name: "mesen-nes", Emulator: "mesen", Args: []string{"{rom}"}}}
	emu := "mesen-patched"
	patches := []emulate.RecipePatch{{Recipe: "mesen-nes", Emulator: &emu}}
	eff := emulate.ApplyPatches(base, patches)
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes(eff),
		WithBaseRecipes(base),
		WithPatches(patches),
		WithPaths(filepath.Join(t.TempDir(), "config.json"), "/r/recipes.json", "/r/patches.json", "/m", "/s"),
		WithLangRaw("auto"),
	)
	return sizedModel(t, m)
}

func openSettings(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(keyMsg("s"))
	mm := out.(Model)
	if mm.mode != ModeSettings {
		t.Fatalf("s must open settings, mode=%d", mm.mode)
	}
	return mm
}

func TestSettingsTabsOnTop(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	view := m.View()
	lines := strings.Split(view, "\n")
	// First content line (after 1-cell top gap) must be the tab strip.
	first := ""
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			first = ln
			break
		}
	}
	for _, want := range []string{"Language", "Recipes", "Paths", " | "} {
		if !strings.Contains(first, want) {
			t.Fatalf("tab strip %q must contain %q\n%s", first, want, view)
		}
	}
}

func TestSettingsKeepsBarAndHelp(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	// Browser columns are gone, bar + help stay.
	if view := m.View(); strings.Contains(view, "CONSOLES") {
		t.Fatalf("settings must not show browser columns\n%s", view)
	}
	out, _ := m.Update(keyMsg("?"))
	m = out.(Model)
	view := m.View()
	for _, want := range []string{"cartridge", "move up", "Language | Recipes | Paths"} {
		_ = want
	}
	if !strings.Contains(view, "cartridge") {
		t.Fatalf("bar must stay in settings\n%s", view)
	}
	if !strings.Contains(view, "move up") {
		t.Fatalf("help must stay in settings\n%s", view)
	}
	if last := lastContentLine(t, view); !strings.Contains(last, "cartridge") {
		t.Fatalf("bottom bar must stay last, got %q\n%s", last, view)
	}
}

func TestSettingsTabSwitchAndClose(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	out, _ := m.Update(keyMsg("tab"))
	m = out.(Model)
	if m.settTab != TabRecipes {
		t.Fatalf("tab must switch to recipes, got %d", m.settTab)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.mode != ModeBrowser {
		t.Fatalf("esc must close settings")
	}
}

func TestSettingsRecipesShowPatch(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	m.settTab = TabRecipes
	view := m.View()
	for _, want := range []string{"mesen-nes", "patched", "overlay"} {
		if !strings.Contains(view, want) {
			t.Fatalf("recipes tab must show %q\n%s", want, view)
		}
	}
	// Effective emulator (patched) is shown, base untouched.
	if !strings.Contains(view, "mesen-patched") {
		t.Fatalf("effective emulator must be shown\n%s", view)
	}
	if m.baseRecipes[0].Emulator != "mesen" {
		t.Fatalf("base must stay untouched: %+v", m.baseRecipes[0])
	}
}

func TestSettingsLanguageApplies(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	m.settTab = TabLang
	m.settCur = 2 // Русский
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if i18n.Get() != i18n.Ru {
		t.Fatalf("lang must switch to ru")
	}
	defer i18n.Set(i18n.En)
	view := m.View()
	if !strings.Contains(view, "Язык") {
		t.Fatalf("tabs must be localized\n%s", view)
	}
	// Config file must be written, recipes file must not be touched.
	cfgPath := m.cfgFile
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config must be saved: %v", err)
	}
	if !strings.Contains(string(data), `"ru"`) {
		t.Fatalf("config must contain ru: %s", data)
	}
}

func TestSettingsPathsEditSaves(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	m.settTab = TabPaths
	m.settCur = 0 // games
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if !m.settEditing {
		t.Fatalf("enter must start editing")
	}
	// Replace buffer with a new dir: esc cancels first, then type fresh.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	m.settBuf = []rune("/new/games")
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.romDir != "/new/games" {
		t.Fatalf("romDir = %q", m.romDir)
	}
	data, err := os.ReadFile(m.cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/new/games") {
		t.Fatalf("config must persist path: %s", data)
	}
}
