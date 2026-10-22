package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/config"
	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
)

// Settings screen: tabs, paths and language.
// Settings live in the left column under the consoles list: hovering the
// settings row swaps the games+metadata panes for the settings panel,
// Enter moves the focus inside, Esc steps back to the consoles.
type SettingsTab int

const (
	TabGeneral SettingsTab = iota
	TabRecipes
	TabPaths
	TabSteam
	TabIcons
)

// TabLang is the deprecated alias for the General tab (language used to
// be a tab of its own; now it lives under General below the art toggle).
const TabLang = TabGeneral

// NumSettingsTabs is the tab count (General | Recipes | Paths | Steam | Icons).
const NumSettingsTabs = 5

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
		{"sett.paths.cheats", func(m *Model) string { return m.cheatsDir }, func(m *Model, v string) { m.cheatsDir = v }},
	}
}

// tabTitle returns the localized title of tab t.
func tabTitle(t SettingsTab) string {
	switch t {
	case TabGeneral:
		return i18n.T("sett.tab.general")
	case TabRecipes:
		return i18n.T("sett.tab.recipes")
	case TabSteam:
		return i18n.T("sett.tab.steam")
	case TabIcons:
		return i18n.T("sett.tab.icons")
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

// langRowCount is the number of language choices (Auto first).
func langRowCount() int { return len(langOptions()) }

// openAppsRow is the General tab cursor row for the open -a switch.
func openAppsRow() int { return 1 + langRowCount() }

// generalRows is the General tab row count: art, languages, then the
// Advanced section with the open -a switch.
func (m Model) generalRows() int { return openAppsRow() + 1 }

// artOnOff returns the localized on/off mark for the art toggle.
func artOnOff(on bool) string {
	if on {
		return i18n.T("sett.steam.on")
	}
	return i18n.T("sett.steam.off")
}

// artLabel returns the General tab art toggle row text.
func (m Model) artLabel() string {
	return i18n.T("sett.general.art", artOnOff(m.showArt))
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

// onSettingsRow reports whether the consoles cursor sits on the settings
// entry below the consoles list (index len(rows): a pseudo-row, not a
// console filter).
func (m Model) onSettingsRow() bool {
	return m.consCur >= len(m.rows)
}

// leaveSettingsRow steps the consoles cursor off the settings entry back
// to Home, restoring the full shelf. The settings entry belongs to the
// Home view (hovering it shows the unfiltered shelf), so exiting returns
// there instead of landing on a console filter.
func (m *Model) leaveSettingsRow() {
	m.consCur = 0
	m.gameCur = 0
	m.refilter()
}

// openSettings moves the focus inside the inline settings panel,
// keeping the consoles column on the left.
func (m *Model) openSettings() {
	m.settActive = true
	m.settCur = 0
	m.settEditing = false
	m.settBuf = nil
	m.settPatchField = ""
	m.settField = -1
}

// closeSettings steps back out to the settings row (hover): the panel
// stays visible, a second step (Esc/arrows on the row) returns Home.
func (m *Model) closeSettings() {
	m.settActive = false
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
	steam := m.steam
	showArt := m.showArt
	openApps := m.openApps
	f := config.File{
		RomDir:    m.romDir,
		Library:   m.libPath,
		Lang:      lang,
		ShowArt:   &showArt,
		Recipes:   m.recipesPath,
		Patches:   m.patchesPath,
		ModsDir:   m.modsDir,
		Patched:   m.patchedDir,
		CheatsDir: m.cheatsDir,
		OpenApps:  &openApps,
		Steam:     &steam,
	}
	if err := config.SaveFile(path, f); err != nil {
		return err
	}
	m.cfgFile = path
	return nil
}

// toggleArt flips the poster preview (ascii/half-block art in the
// metadata pane) and persists it. The preview rebuilds at once so the
// change is visible without a rescan.
func (m *Model) toggleArt() tea.Cmd {
	m.showArt = !m.showArt
	m.refreshMeta()
	if err := m.persistSettings(); err != nil {
		return m.setStatus(i18n.T("status.sett.save_fail", err))
	}
	return m.setStatus(i18n.T("status.sett.saved"))
}

// toggleOpenApps flips the global open -a launch switch and persists.
func (m *Model) toggleOpenApps() tea.Cmd {
	m.openApps = !m.openApps
	if err := m.persistSettings(); err != nil {
		return m.setStatus(i18n.T("status.sett.save_fail", err))
	}
	return m.setStatus(i18n.T("status.sett.saved"))
}

// toggleSteamAutoAdd flips auto-add and persists.
func (m *Model) toggleSteamAutoAdd() tea.Cmd {
	m.steam.AutoAdd = !m.steam.AutoAdd
	if err := m.persistSettings(); err != nil {
		return m.setStatus(i18n.T("status.sett.save_fail", err))
	}
	return m.setStatus(i18n.T("status.sett.saved"))
}

// applyLanguage switches the UI language at once and persists it.
// The stored value is always a code ("auto", "en", "ru", "de", ...),
// whatever spelling the user had before.
func (m *Model) applyLanguage(raw string) tea.Cmd {
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
		return m.setStatus(i18n.T("status.sett.save_fail", err))
	}
	m.clearStatus()
	return nil
}

// reloadRecipes re-reads recipes+patches from disk and re-applies the overlay.
func (m *Model) reloadRecipes() tea.Cmd {
	base, err := emulate.Load(m.recipesPath)
	if err != nil {
		return m.setStatus(i18n.T("status.sett.reload_fail", err))
	}
	patches, err := emulate.LoadPatches(m.patchesPath)
	if err != nil {
		return m.setStatus(i18n.T("status.sett.reload_fail", err))
	}
	m.baseRecipes = base
	m.patches = patches
	m.recipes = emulate.ApplyPatches(base, patches)
	if m.settCur >= len(m.recipes) {
		m.settCur = max(len(m.recipes)-1, 0)
	}
	m.settField = clamp(m.settField, -1, len(recipeFields())-1)
	m.refreshMeta()
	return m.setStatus(i18n.T("status.sett.reloaded", len(base), len(emulate.AppliedPatches(base, patches))))
}

// effectiveRecipe returns the effective (patched) recipe for the selected row.
func (m Model) effectiveRecipe() emulate.Recipe {
	cur := clamp(m.settCur, 0, len(m.baseRecipes)-1)
	if len(m.recipes) == len(m.baseRecipes) {
		return m.recipes[cur]
	}
	return emulate.ApplyPatches(m.baseRecipes, m.patches)[cur]
}
