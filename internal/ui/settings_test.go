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
		WithPaths(filepath.Join(t.TempDir(), "config.json"), "/r/recipes.json", "/r/patches.json", "/m", "/s", "/c"),
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
	if !strings.Contains(view, "Language") || !strings.Contains(view, " | ") {
		t.Fatalf("tabs must stay visible with help\n%s", view)
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
	for _, want := range []string{"mesen-nes", "patched", "patches (1)"} {
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

func moveTestModel(t *testing.T) (Model, string) {
	t.Helper()
	i18n.Set(i18n.En)
	dir := t.TempDir()
	recipesPath := filepath.Join(dir, "recipes.json")
	base := []emulate.Recipe{
		{Name: "r1", Consoles: []string{"nes"}, Emulator: "e1"},
		{Name: "r2", Consoles: []string{"snes"}, Emulator: "e2"},
	}
	if err := emulate.SaveRecipes(recipesPath, base); err != nil {
		t.Fatal(err)
	}
	loaded, err := emulate.Load(recipesPath)
	if err != nil {
		t.Fatal(err)
	}
	emu := "e1-patched"
	patches := []emulate.RecipePatch{{Recipe: "r1", Emulator: &emu}}
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes(emulate.ApplyPatches(loaded, patches)),
		WithBaseRecipes(loaded),
		WithPatches(patches),
		WithPaths(filepath.Join(dir, "config.json"), recipesPath, filepath.Join(dir, "patches.json"), "/m", "/s", "/c"),
		WithLangRaw("auto"),
	)
	m = sizedModel(t, m)
	m = openSettings(t, m)
	m.settTab = TabRecipes
	return m, recipesPath
}

func fileRecipeOrder(t *testing.T, path string) []string {
	t.Helper()
	loaded, err := emulate.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(loaded))
	for i, r := range loaded {
		names[i] = r.Name
	}
	return names
}

func TestSettingsRecipeMove(t *testing.T) {
	m, recipesPath := moveTestModel(t)
	// J moves r1 down: r2 first, cursor follows r1, patch sticks to r1.
	out, _ := m.Update(keyMsg("J"))
	m = out.(Model)
	if m.baseRecipes[0].Name != "r2" || m.baseRecipes[1].Name != "r1" {
		t.Fatalf("base order broken: %+v", m.baseRecipes)
	}
	if m.settCur != 1 {
		t.Fatalf("cursor must follow moved recipe, settCur=%d", m.settCur)
	}
	if m.recipes[0].Name != "r2" || m.recipes[1].Emulator != "e1-patched" {
		t.Fatalf("effective order/patch broken: %+v", m.recipes)
	}
	if got := fileRecipeOrder(t, recipesPath); got[0] != "r2" || got[1] != "r1" {
		t.Fatalf("file order broken: %v", got)
	}
	if !strings.Contains(m.status, "r1") {
		t.Fatalf("status must name moved recipe, got %q", m.status)
	}
	// J at the bottom is a no-op.
	out, _ = m.Update(keyMsg("J"))
	m = out.(Model)
	if m.baseRecipes[1].Name != "r1" || m.settCur != 1 {
		t.Fatalf("bottom J must be no-op: %+v cur=%d", m.baseRecipes, m.settCur)
	}
	// K moves r1 back up.
	out, _ = m.Update(keyMsg("K"))
	m = out.(Model)
	if m.baseRecipes[0].Name != "r1" || m.settCur != 0 {
		t.Fatalf("K must move up: %+v cur=%d", m.baseRecipes, m.settCur)
	}
	if got := fileRecipeOrder(t, recipesPath); got[0] != "r1" || got[1] != "r2" {
		t.Fatalf("file order not restored: %v", got)
	}
	// K at the top is a no-op.
	out, _ = m.Update(keyMsg("K"))
	m = out.(Model)
	if m.baseRecipes[0].Name != "r1" || m.settCur != 0 {
		t.Fatalf("top K must be no-op: %+v cur=%d", m.baseRecipes, m.settCur)
	}
}

func TestCoresParseJoinRoundTrip(t *testing.T) {
	in := "gba: mgba_libretro.so; nes: nestopia_libretro.so, mesen_libretro.so"
	m := parseCores(in)
	if len(m) != 2 || len(m["nes"]) != 2 || m["nes"][0] != "nestopia_libretro.so" {
		t.Fatalf("parse broken: %+v", m)
	}
	if got := joinCores(m); got != in {
		t.Fatalf("join = %q, want %q", got, in)
	}
	m = parseCores("nes: a.so, , ; junk-without-colon; : x.so; snes:  ")
	if len(m) != 1 || len(m["nes"]) != 1 || m["nes"][0] != "a.so" {
		t.Fatalf("garbage must drop out: %+v", m)
	}
	if m := parseCores("  "); len(m) != 0 || joinCores(m) != "" {
		t.Fatalf("empty must stay empty: %+v", m)
	}
}

func TestSettingsCoresEditSavesPatch(t *testing.T) {
	m, _ := moveTestModel(t)
	m.settCur = 0 // r1
	m.startRecipeEdit("cores")
	if !m.settEditing || m.settPatchField != "cores" {
		t.Fatalf("cores edit not started: editing=%v field=%q", m.settEditing, m.settPatchField)
	}
	m.settBuf = []rune("nes: nestopia_libretro.so, mesen_libretro.so")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.settEditing {
		t.Fatalf("still editing: %q", m.status)
	}
	got := m.recipes[0].Cores["nes"]
	if len(got) != 2 || got[0] != "nestopia_libretro.so" {
		t.Fatalf("effective cores broken: %+v", m.recipes[0].Cores)
	}
	if view := m.View(); !strings.Contains(view, "cores:") || !strings.Contains(view, "nestopia_libretro.so") {
		t.Fatalf("cores row must show\n%s", view)
	}
	data, err := os.ReadFile(m.patchesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cores"`) {
		t.Fatalf("patches file must carry cores: %s", data)
	}
}

func TestSettingsCoreNamesEditSavesPatch(t *testing.T) {
	m, _ := moveTestModel(t)
	m.settCur = 0 // r1
	m.startRecipeEdit("core_names")
	if !m.settEditing || m.settPatchField != "core_names" {
		t.Fatalf("core_names edit not started: editing=%v field=%q", m.settEditing, m.settPatchField)
	}
	m.settBuf = []rune("gba=mGBA; nes=Nestopia")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.settEditing {
		t.Fatalf("still editing: %q", m.status)
	}
	got := m.recipes[0].CoreNames
	if len(got) != 2 || got["gba"] != "mGBA" || got["nes"] != "Nestopia" {
		t.Fatalf("effective core_names broken: %+v", got)
	}
	if view := m.View(); !strings.Contains(view, "core names:") || !strings.Contains(view, "gba=mGBA") {
		t.Fatalf("core names row must show\n%s", view)
	}
	data, err := os.ReadFile(m.patchesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"core_names"`) {
		t.Fatalf("patches file must carry core_names: %s", data)
	}
}

func TestCoreNamesParseJoin(t *testing.T) {
	m := parseCoreNames("gba=mGBA; nes=Nestopia; junk; =x; snes= ")
	if len(m) != 2 || m["gba"] != "mGBA" {
		t.Fatalf("parse broken: %+v", m)
	}
	if got := joinCoreNames(m); got != "gba=mGBA; nes=Nestopia" {
		t.Fatalf("join = %q", got)
	}
	if len(parseCoreNames("  ")) != 0 {
		t.Fatalf("empty must stay empty")
	}
}

// metaRecipeValue returns the browser meta "recipe" row text.
func metaRecipeValue(t *testing.T, m Model) string {
	t.Helper()
	for _, r := range m.metaRows {
		if !r.sep && r.label == i18n.T("meta.recipe") {
			return strings.Join(r.lines, " ")
		}
	}
	t.Fatalf("no recipe row in meta")
	return ""
}

// metaCheatsValue returns the browser meta "cheats" row text.
func metaCheatsValue(t *testing.T, m Model) string {
	t.Helper()
	for _, r := range m.metaRows {
		if !r.sep && r.label == i18n.T("meta.cheats") {
			return strings.Join(r.lines, " ")
		}
	}
	t.Fatalf("no cheats row in meta")
	return ""
}

func TestSettingsConsolesPatchRefreshesMeta(t *testing.T) {
	m, _ := moveTestModel(t) // r1(nes) matches Battletoads
	if got := metaRecipeValue(t, m); !strings.Contains(got, "r1") {
		t.Fatalf("precondition: meta recipe = %q", got)
	}
	// Move r1 away to snes: Battletoads must lose its recipe at once,
	// without any resize/help/focus dance.
	m.settCur = 0
	m.startRecipeEdit("consoles")
	m.settBuf = []rune("snes")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if got := metaRecipeValue(t, m); !strings.Contains(got, i18n.T("recipe.none")) {
		t.Fatalf("meta must lose the recipe, got %q", got)
	}
}

func TestSettingsDropPatchRefreshesMeta(t *testing.T) {
	i18n.Set(i18n.En)
	dir := t.TempDir()
	recipesPath := filepath.Join(dir, "recipes.json")
	base := []emulate.Recipe{{
		Name: "r1", Consoles: []string{"nes"}, Emulator: "/base/emu",
		Cheats: emulate.Cheats{Supported: true, Path: "{emu_dir}/c.cht"},
	}}
	if err := emulate.SaveRecipes(recipesPath, base); err != nil {
		t.Fatal(err)
	}
	loaded, err := emulate.Load(recipesPath)
	if err != nil {
		t.Fatal(err)
	}
	emu := "/patched/emu"
	patches := []emulate.RecipePatch{{Recipe: "r1", Emulator: &emu}}
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes(emulate.ApplyPatches(loaded, patches)),
		WithBaseRecipes(loaded),
		WithPatches(patches),
		WithPaths(filepath.Join(dir, "config.json"), recipesPath, filepath.Join(dir, "patches.json"), "/m", "/s", "/c"),
		WithLangRaw("auto"),
	)
	m = sizedModel(t, m)
	m = openSettings(t, m)
	m.settTab = TabRecipes
	m.settCur = 0
	if got := metaCheatsValue(t, m); !strings.Contains(got, "/patched/c.cht") {
		t.Fatalf("precondition: cheats = %q", got)
	}
	m.dropRecipePatch()
	if got := metaCheatsValue(t, m); !strings.Contains(got, "/base/c.cht") {
		t.Fatalf("meta must follow dropped patch, got %q", got)
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
	if m.status != "" {
		t.Fatalf("language switch must not set bar status, got %q", m.status)
	}
	if strings.Contains(view, "language:") || strings.Contains(view, "язык:") {
		t.Fatalf("bar must not show language status\n%s", view)
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

func TestSettingsLangSelectedStyle(t *testing.T) {
	i18n.Set(i18n.En)
	m := openSettings(t, settingsModel(t))
	m.settTab = TabLang
	m.langRaw = "ru" // active language: Русский
	m.settCur = 0    // cursor on Auto
	lines := m.renderLangTab(40)
	if len(lines) != 3 {
		t.Fatalf("lang tab must list 3 options, got %d", len(lines))
	}
	// Selected language: bold white, no background.
	if want := "  " + langActive.Render("Русский"); lines[2] != want {
		t.Fatalf("active language row = %q, want %q", lines[2], want)
	}
	if strings.Contains(lines[2], "48;") {
		t.Fatalf("active language must have no background: %q", lines[2])
	}
	// The rest: grey italic.
	if want := "  " + langIdle.Render("English"); lines[1] != want {
		t.Fatalf("idle language row = %q, want %q", lines[1], want)
	}
	// Cursor row keeps the navigation highlight.
	if !strings.Contains(lines[0], "> ") {
		t.Fatalf("cursor row must keep the marker: %q", lines[0])
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

func TestSettingsNoHints(t *testing.T) {
	for tab, name := range map[SettingsTab]string{TabLang: "lang", TabRecipes: "recipes", TabPaths: "paths"} {
		m := openSettings(t, settingsModel(t))
		m.settTab = tab
		view := m.View()
		for _, hint := range []string{
			"enter applies at once",
			"patches overlay recipes in memory",
			"enter edits",
			"применяет сразу",
			"накладываются поверх",
			"редактирует",
		} {
			if strings.Contains(strings.ToLower(view), strings.ToLower(hint)) {
				t.Fatalf("%s tab must not show hint %q\n%s", name, hint, view)
			}
		}
	}
}

func TestSettingsPathLabelsTitleCase(t *testing.T) {
	i18n.Set(i18n.En)
	for key, want := range map[string]string{
		"sett.paths.games":   "Games",
		"sett.paths.library": "Library",
		"sett.paths.recipes": "Recipes",
		"sett.paths.patches": "Patches",
		"sett.paths.mods":    "Mods",
		"sett.paths.patched": "Patched",
		"sett.paths.saves":   "Saves",
		"sett.paths.cheats":  "Cheats",
	} {
		if got := i18n.T(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	m := openSettings(t, settingsModel(t))
	m.settTab = TabPaths
	view := m.View()
	for _, want := range []string{"Games:", "Library:", "Recipes:", "Patches:", "Mods:", "Patched:", "Saves:", "Cheats:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("paths tab must show label %q\n%s", want, view)
		}
	}
	// Values keep their own case: the label is title-cased, not the path.
	if strings.Contains(view, "SAVES:") {
		t.Fatalf("labels must not be all caps\n%s", view)
	}
}

func TestSettingsIdlePathsItalicGrey(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	m.settTab = TabPaths
	m.settCur = 0
	// Cursor row is active, the rest are idle (grey + italic).
	if m.isPathIdle(0) {
		t.Fatalf("cursor row must not be idle")
	}
	for _, i := range []int{1, 2, 3, 4, 5} {
		if !m.isPathIdle(i) {
			t.Fatalf("row %d must be idle (grey + italic)", i)
		}
	}
	// The row being edited is never idle.
	m.settEditing = true
	m.settBuf = []rune("/x")
	m.settCur = 2
	if m.isPathIdle(2) {
		t.Fatalf("editing row must not be idle")
	}
	if !m.isPathIdle(1) {
		t.Fatalf("non-edited row must stay idle while editing")
	}
}
