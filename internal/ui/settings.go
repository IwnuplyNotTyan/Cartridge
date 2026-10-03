package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/config"
	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
)

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
		{"sett.paths.saves", func(m *Model) string { return m.savesDir }, func(m *Model, v string) { m.savesDir = v }},
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

// langOptions returns settings language choices as (fileValue, labelKey).
func langOptions() [][2]string {
	return [][2]string{
		{"auto", "sett.lang.auto"},
		{"en", "sett.lang.en"},
		{"ru", "sett.lang.ru"},
	}
}

// langCur returns the cursor index matching the stored raw lang value.
func (m Model) langCur() int {
	switch strings.ToLower(strings.TrimSpace(m.langRaw)) {
	case "ru", "rus", "russian", "ru_ru", "ru-ru":
		return 2
	case "en", "eng", "english":
		return 1
	default:
		// Empty or "auto": show Auto, unless the file had no lang at all
		// and detection resolved to en/ru — still show Auto as the source.
		return 0
	}
}

// openSettings enters settings mode, keeping bar+help.
func (m *Model) openSettings() {
	m.mode = ModeSettings
	m.settCur = 0
	m.settEditing = false
	m.settBuf = nil
	m.settCur = clamp(m.settCur, 0, 0)
}

// closeSettings returns to the browser.
func (m *Model) closeSettings() {
	m.mode = ModeBrowser
	m.settEditing = false
	m.settBuf = nil
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
		RomDir:   m.romDir,
		Library:  m.libPath,
		Lang:     lang,
		Recipes:  m.recipesPath,
		Patches:  m.patchesPath,
		ModsDir:  m.modsDir,
		SavesDir: m.savesDir,
	}
	if err := config.SaveFile(path, f); err != nil {
		return err
	}
	m.cfgFile = path
	return nil
}

// applyLanguage switches the UI language at once and persists it.
func (m *Model) applyLanguage(raw string) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		raw = "auto"
	}
	m.langRaw = raw
	switch raw {
	case "ru", "rus", "russian":
		i18n.Set(i18n.Ru)
	case "en", "eng", "english":
		i18n.Set(i18n.En)
	default:
		m.langRaw = "auto"
		i18n.Set(i18n.Auto)
	}
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
	m.refreshMeta()
	m.status = i18n.T("status.sett.reloaded", len(base), len(patches))
}

func (m Model) settingsRows() int {
	switch m.settTab {
	case TabLang:
		return len(langOptions())
	case TabRecipes:
		return max(len(m.baseRecipes), 1)
	default:
		return len(pathEntries())
	}
}

// updateSettings handles keys while in settings mode.
// Returns (model, cmd, handledQuit) — handledQuit is true when the key
// already produced a quit command.
func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Inline path editing: most keys go to the buffer.
	if m.settEditing {
		switch msg.Type {
		case tea.KeyEnter:
			v := config.ExpandPath(string(m.settBuf))
			if strings.TrimSpace(v) != "" {
				entries := pathEntries()
				if m.settCur < len(entries) {
					entries[m.settCur].set(&m, v)
				}
				// Switching the games dir rescans lazily on next r;
				// switching recipes/patches paths reloads the overlay.
				if m.settCur == 2 || m.settCur == 3 {
					m.reloadRecipes()
				}
				if err := m.persistSettings(); err != nil {
					m.status = i18n.T("status.sett.save_fail", err)
				} else if m.status == "" || !strings.Contains(m.status, "reloaded") {
					m.status = i18n.T("status.sett.saved")
				}
			}
			m.settEditing = false
			m.settBuf = nil
			return m, nil
		case tea.KeyEsc:
			m.settEditing = false
			m.settBuf = nil
			return m, nil
		case tea.KeyBackspace:
			if len(m.settBuf) > 0 {
				m.settBuf = m.settBuf[:len(m.settBuf)-1]
			}
			return m, nil
		case tea.KeyRunes:
			m.settBuf = append(m.settBuf, msg.Runes...)
			return m, nil
		case tea.KeySpace:
			m.settBuf = append(m.settBuf, ' ')
			return m, nil
		default:
			// Ignore navigation keys while editing; type text instead.
			if len(msg.String()) == 1 {
				m.settBuf = append(m.settBuf, []rune(msg.String())...)
			}
			return m, nil
		}
	}

	k := m.keys
	// Global in settings: help, quit, close.
	switch {
	case key.Matches(msg, k.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		return m, nil
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc", "s", "S":
		m.closeSettings()
		return m, nil
	case "1":
		m.settTab = TabLang
		m.settCur = 0
		return m, nil
	case "2":
		m.settTab = TabRecipes
		m.settCur = 0
		return m, nil
	case "3":
		m.settTab = TabPaths
		m.settCur = 0
		return m, nil
	}
	// Tab switches tabs (like "example | example | etc" navigation).
	if key.Matches(msg, k.Next) || msg.String() == "right" || msg.String() == "l" {
		m.settTab = (m.settTab + 1) % NumSettingsTabs
		m.settCur = 0
		return m, nil
	}
	if key.Matches(msg, k.Prev) || msg.String() == "left" || msg.String() == "h" {
		m.settTab = (m.settTab + NumSettingsTabs - 1) % NumSettingsTabs
		m.settCur = 0
		return m, nil
	}
	n := m.settingsRows()
	switch {
	case key.Matches(msg, k.Up):
		if m.settCur > 0 {
			m.settCur--
		}
		return m, nil
	case key.Matches(msg, k.Down):
		if m.settCur < n-1 {
			m.settCur++
		}
		return m, nil
	case key.Matches(msg, k.Top):
		m.settCur = 0
		return m, nil
	case key.Matches(msg, k.Bottom):
		m.settCur = max(n-1, 0)
		return m, nil
	}
	switch m.settTab {
	case TabLang:
		if key.Matches(msg, k.Select) {
			opts := langOptions()
			if m.settCur < len(opts) {
				m.applyLanguage(opts[m.settCur][0])
			}
			return m, nil
		}
	case TabRecipes:
		if key.Matches(msg, k.Rescan) {
			m.reloadRecipes()
			return m, nil
		}
	case TabPaths:
		if key.Matches(msg, k.Select) {
			entries := pathEntries()
			if m.settCur < len(entries) {
				m.settEditing = true
				m.settBuf = []rune(entries[m.settCur].get(&m))
			}
			return m, nil
		}
		if key.Matches(msg, k.Rescan) {
			m.reloadRecipes()
			return m, nil
		}
	}
	// Global rescan from any settings tab.
	if key.Matches(msg, k.Rescan) {
		m.reloadRecipes()
		return m, nil
	}
	return m, nil
}

// renderSettingsTabs renders the top tab strip:
// Language | Recipes | Paths (active tab highlighted).
func (m Model) renderSettingsTabs(w int) string {
	parts := make([]string, 0, NumSettingsTabs)
	for t := SettingsTab(0); t < NumSettingsTabs; t++ {
		title := tabTitle(t)
		if t == m.settTab {
			parts = append(parts, titleActive.Render(title))
		} else {
			parts = append(parts, titleDim.Render(title))
		}
	}
	sep := infoStyle.Render(" | ")
	line := strings.Join(parts, sep)
	// Keep the literal " | " separator measurable even with ANSI codes.
	return fit(line, w)
}

// renderSettings builds the settings body: tabs on top, tab content below.
// Only the bottom bar and help survive from the browser view.
func (m Model) renderSettings() string {
	w := m.contentW()
	var lines []string
	lines = append(lines, m.renderSettingsTabs(w))
	lines = append(lines, "")
	switch m.settTab {
	case TabLang:
		lines = append(lines, m.renderLangTab(w)...)
	case TabRecipes:
		lines = append(lines, m.renderRecipesTab(w)...)
	default:
		lines = append(lines, m.renderPathsTab(w)...)
	}
	for len(lines) < m.bodyH() {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.bodyH()], "\n")
}

func (m Model) renderLangTab(w int) []string {
	var lines []string
	cur := m.langCur()
	for i, opt := range langOptions() {
		label := i18n.T(opt[1])
		plain := fit("  "+truncate(label, w-2), w)
		if i == m.settCur {
			plain = styledRow(plain)
		} else if i == cur {
			plain = "  " + selStyle.Render(truncate(label, w-2))
		}
		_ = opt
		lines = append(lines, plain)
	}
	return lines
}

func (m Model) renderRecipesTab(w int) []string {
	var lines []string
	lines = append(lines, fit(truncate(i18n.T("sett.recipes.base", m.recipesPath, len(m.baseRecipes)), w), w))
	lines = append(lines, fit(truncate(i18n.T("sett.recipes.patches", m.patchesPath, len(m.patches)), w), w))
	lines = append(lines, "")
	if len(m.baseRecipes) == 0 {
		lines = append(lines, infoStyle.Render(fit(i18n.T("sett.recipes.none"), w)))
	} else {
		marks := emulate.PatchedNames(m.baseRecipes, m.patches)
		for i, r := range m.baseRecipes {
			tag := ""
			if marks[r.Name] {
				tag = " [" + i18n.T("sett.recipes.patched") + "]"
			}
			// Effective (patched) emulator shown; base file stays untouched.
			eff := r.Emulator
			for _, p := range emulate.PatchesFor(m.patches, r.Name) {
				if p.Emulator != nil {
					eff = *p.Emulator
				}
			}
			row := fmt.Sprintf("%s → %s%s", r.Name, eff, tag)
			plain := fit("  "+truncate(row, w-2), w)
			if i == m.settCur {
				plain = styledRow(plain)
			}
			lines = append(lines, plain)
		}
	}
	return lines
}

// isPathIdle reports whether path row i renders as idle (grey + italic).
// The row being edited and the cursor row are never idle.
func (m Model) isPathIdle(i int) bool {
	if m.settEditing && i == m.settCur {
		return false
	}
	return i != m.settCur
}

func (m Model) renderPathsTab(w int) []string {
	var lines []string
	entries := pathEntries()
	for i, e := range entries {
		label := i18n.T(e.label)
		val := e.get(&m)
		if m.settEditing && i == m.settCur {
			val = string(m.settBuf) + "▊"
			row := label + ": " + val
			plain := fit("  "+truncate(row, w-2), w)
			lines = append(lines, styledRow(plain))
			continue
		}
		row := label + ": " + val
		plain := fit("  "+truncate(row, w-2), w)
		if m.isPathIdle(i) {
			// Idle: only the path value is grey + italic, the name stays plain.
			plain = idlePathRow(label, val, w)
		} else {
			plain = styledRow(plain)
		}
		lines = append(lines, plain)
	}
	return lines
}
