package ui

import (
	"strings"

	"cartridge/internal/config"
	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
)

// Settings screen: modes, tabs, paths and language.
// Mode is the top-level screen: browser or settings.
// Settings keeps only the bottom bar and help from the browser view.
type Mode int

const (
	ModeBrowser Mode = iota
	ModeSettings
)

// SettingsTab is one tab of the settings screen.
type SettingsTab int

const (
	TabLang SettingsTab = iota
	TabRecipes
	TabPaths
)

// NumSettingsTabs is the tab count (Language | Recipes | Paths).
const NumSettingsTabs = 3

// pathEntry binds a label key to a getter/setter over the model.
type pathEntry struct {
	label string // i18n key suffix shown in UI
	get   func(m *Model) string
	set   func(m *Model, v string)
}

func pathEntries() []pathEntry {
	return []pathEntry{
		{"sett.paths.games", func(m *Model) string { return m.romDir }, func(m *Model, v string) { m.romDir = v }},
		{"sett.paths.library", func(m *Model) string { return m.libPath }, func(m *Model, v string) { m.libPath = v }},
		{"sett.paths.recipes", func(m *Model) string { return m.recipesPath }, func(m *Model, v string) { m.recipesPath = v }},
		{"sett.paths.patches", func(m *Model) string { return m.patchesPath }, func(m *Model, v string) { m.patchesPath = v }},
		{"sett.paths.mods", func(m *Model) string { return m.modsDir }, func(m *Model, v string) { m.modsDir = v }},
		{"sett.paths.patched", func(m *Model) string { return m.patchedDir }, func(m *Model, v string) { m.patchedDir = v }},
		{"sett.paths.saves", func(m *Model) string { return m.savesDir }, func(m *Model, v string) { m.savesDir = v }},
		{"sett.paths.cheats", func(m *Model) string { return m.cheatsDir }, func(m *Model, v string) { m.cheatsDir = v }},
	}
}

// tabTitle returns the localized title of tab t.
func tabTitle(t SettingsTab) string {
	switch t {
	case TabLang:
		return i18n.T("sett.tab.lang")
	case TabRecipes:
		return i18n.T("sett.tab.recipes")
	default:
		return i18n.T("sett.tab.paths")
	}
}

// langOptions returns settings language choices as (fileValue, display):
// Auto first, then every loaded language (shipped or dropped into the
// locales dir). Display names come from the locale files themselves.
func langOptions() [][2]string {
	opts := [][2]string{{"auto", i18n.T("sett.lang.auto")}}
	for _, l := range i18n.Langs() {
		opts = append(opts, [2]string{string(l), i18n.Name(l)})
	}
	return opts
}

// langCur returns the cursor index matching the stored raw lang value.
func (m Model) langCur() int {
	opts := langOptions()
	want := string(i18n.Parse(m.langRaw))
	if want == string(i18n.Auto) {
		return 0
	}
	for i, opt := range opts {
		if opt[0] == want {
			return i
		}
	}
	return 0
}

// openSettings enters settings mode, keeping bar+help.
func (m *Model) openSettings() {
	m.mode = ModeSettings
	m.settCur = 0
	m.settEditing = false
	m.settBuf = nil
	m.settPatchField = ""
	m.settField = -1
	m.settCur = clamp(m.settCur, 0, 0)
}

// closeSettings returns to the browser.
func (m *Model) closeSettings() {
	m.mode = ModeBrowser
	m.settEditing = false
	m.settBuf = nil
	m.settPatchField = ""
	m.settField = -1
}

// persistSettings writes current paths+language back to the config file.
// Paths are expanded (~/...) before saving; the recipes file itself and
// the patches file are never rewritten here — only config.json changes.
func (m *Model) persistSettings() error {
	path := m.cfgFile
	if path == "" {
		path = config.DefaultFilePath()
	}
	lang := strings.ToLower(strings.TrimSpace(m.langRaw))
	if lang == "" {
		lang = "auto"
	}
	f := config.File{
		RomDir:    m.romDir,
		Library:   m.libPath,
		Lang:      lang,
		Recipes:   m.recipesPath,
		Patches:   m.patchesPath,
		ModsDir:   m.modsDir,
		Patched:   m.patchedDir,
		SavesDir:  m.savesDir,
		CheatsDir: m.cheatsDir,
	}
	if err := config.SaveFile(path, f); err != nil {
		return err
	}
	m.cfgFile = path
	return nil
}

// applyLanguage switches the UI language at once and persists it.
// The stored value is always a code ("auto", "en", "ru", "de", ...),
// whatever spelling the user had before.
func (m *Model) applyLanguage(raw string) {
	l := i18n.Parse(raw)
	if l == i18n.Auto {
		m.langRaw = "auto"
	} else {
		m.langRaw = string(l)
	}
	i18n.Set(l)
	m.keys = newKeyMap()
	m.setGames(m.games)
	m.refreshMeta()
	if err := m.persistSettings(); err != nil {
		m.status = i18n.T("status.sett.save_fail", err)
		return
	}
	m.status = ""
}

// reloadRecipes re-reads recipes+patches from disk and re-applies the overlay.
func (m *Model) reloadRecipes() {
	base, err := emulate.Load(m.recipesPath)
	if err != nil {
		m.status = i18n.T("status.sett.reload_fail", err)
		return
	}
	patches, err := emulate.LoadPatches(m.patchesPath)
	if err != nil {
		m.status = i18n.T("status.sett.reload_fail", err)
		return
	}
	m.baseRecipes = base
	m.patches = patches
	m.recipes = emulate.ApplyPatches(base, patches)
	if m.settCur >= len(m.recipes) {
		m.settCur = max(len(m.recipes)-1, 0)
	}
	m.settField = clamp(m.settField, -1, len(recipeFields())-1)
	m.refreshMeta()
	m.status = i18n.T("status.sett.reloaded", len(base), len(emulate.AppliedPatches(base, patches)))
}

// effectiveRecipe returns the effective (patched) recipe for the selected row.
func (m Model) effectiveRecipe() emulate.Recipe {
	cur := clamp(m.settCur, 0, len(m.baseRecipes)-1)
	if len(m.recipes) == len(m.baseRecipes) {
		return m.recipes[cur]
	}
	return emulate.ApplyPatches(m.baseRecipes, m.patches)[cur]
}
