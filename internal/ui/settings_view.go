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

// Settings view: rows, key handling and tab rendering.
func (m Model) settingsRows() int {
	switch m.settTab {
	case TabLang:
		return len(langOptions())
	case TabRecipes:
		// Recipe rows plus the detail rows under the selected recipe.
		return max(len(m.baseRecipes)+len(recipeFields()), 1)
	case TabSteam:
		return steamRowsFor(m)
	case TabIcons:
		return iconsRowsFor()
	default:
		return len(pathEntries())
	}
}

// updateSettings handles keys while in settings mode.
// Returns (model, cmd, handledQuit) — handledQuit is true when the key
// already produced a quit command.
func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Inline editing: most keys go to the buffer.
	// Recipes tab edits a patch field (emulator/args), Paths tab edits a path,
	// Steam tab edits prefix/template/exe.
	if m.settEditing {
		if m.settTab == TabRecipes {
			switch msg.Type {
			case tea.KeyEnter:
				m.saveRecipeEdit()
				m.settEditing = false
				m.settBuf = nil
				m.settPatchField = ""
				return m, nil
			case tea.KeyEsc:
				m.settEditing = false
				m.settBuf = nil
				m.settPatchField = ""
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
				if len(msg.String()) == 1 {
					m.settBuf = append(m.settBuf, []rune(msg.String())...)
				}
				return m, nil
			}
		}
		if m.settTab == TabSteam {
			switch msg.Type {
			case tea.KeyEnter:
				v := strings.TrimSpace(string(m.settBuf))
				switch m.settCur {
				case steamRowPrefix:
					m.steam.Prefix = v
				case steamRowTemplate:
					if v == "" {
						v = config.DefaultSteamConfig().CategoryTemplate
					}
					m.steam.CategoryTemplate = v
				case steamRowExe:
					m.steam.Exe = config.ExpandPath(v)
				default:
					if c, ok := m.steamConsoleAt(m.settCur); ok {
						if m.steam.Categories == nil && v != "" {
							m.steam.Categories = map[string]string{}
						}
						if v == "" {
							delete(m.steam.Categories, c.ID)
						} else {
							m.steam.Categories[c.ID] = v
						}
					}
				}
				if err := m.persistSettings(); err != nil {
					m.status = i18n.T("status.sett.save_fail", err)
				} else {
					// Naming edits re-sync at once so existing Steam
					// shortcuts/collections pick the new names up.
					m.syncSteamNow(false)
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
				if len(msg.String()) == 1 {
					m.settBuf = append(m.settBuf, []rune(msg.String())...)
				}
				return m, nil
			}
		}
		if m.settTab == TabIcons {
			switch msg.Type {
			case tea.KeyEnter:
				m.saveIconsEdit(strings.TrimSpace(string(m.settBuf)))
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
				if len(msg.String()) == 1 {
					m.settBuf = append(m.settBuf, []rune(msg.String())...)
				}
				return m, nil
			}
		}
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
	case "esc":
		// Esc on a detail row steps back to the recipe row;
		// esc on the row itself steps back out to the consoles.
		if msg.String() == "esc" && m.settTab == TabRecipes && !m.settEditing && m.settField != -1 {
			m.settField = -1
			return m, nil
		}
		m.closeSettings()
		return m, nil
	case "1":
		m.settTab = TabLang
		m.settCur = 0
		m.settField = -1
		return m, nil
	case "2":
		m.settTab = TabRecipes
		m.settCur = 0
		m.settField = -1
		return m, nil
	case "3":
		m.settTab = TabPaths
		m.settCur = 0
		m.settField = -1
		return m, nil
	case "4":
		m.settTab = TabSteam
		m.settCur = 0
		m.settField = -1
		return m, nil
	case "5":
		m.settTab = TabIcons
		m.settCur = 0
		m.settField = -1
		m.refreshIconsStat()
		return m, nil
	}
	// Tab switches tabs (like "example | example | etc" navigation).
	if key.Matches(msg, k.Next) || msg.String() == "right" || msg.String() == "l" {
		m.settTab = (m.settTab + 1) % NumSettingsTabs
		m.settCur = 0
		m.settField = -1
		if m.settTab == TabIcons {
			m.refreshIconsStat()
		}
		return m, nil
	}
	if key.Matches(msg, k.Prev) || msg.String() == "left" || msg.String() == "h" {
		// No previous page on the first tab: h/Left step out to the
		// consoles list (closeSettings also leaves the settings row,
		// so one press restores games+metadata). Other tabs flip to
		// the previous one; Tab is untouched (next tab).
		if m.settTab == TabLang {
			m.closeSettings()
			return m, nil
		}
		m.settTab = (m.settTab + NumSettingsTabs - 1) % NumSettingsTabs
		m.settCur = 0
		m.settField = -1
		if m.settTab == TabIcons {
			m.refreshIconsStat()
		}
		return m, nil
	}
	// Recipes tab: ↑↓ walk recipe rows only; the detail rows under the
	// selected recipe are reachable solely via enter (settField >= 0),
	// esc steps back out. g/G jump to the first/last recipe.
	if m.settTab == TabRecipes && len(m.baseRecipes) > 0 {
		nf := len(recipeFields())
		switch {
		case key.Matches(msg, k.Up):
			if m.settField >= 0 {
				m.settField = max(m.settField-1, 0)
			} else if m.settCur > 0 {
				m.settCur--
			}
			return m, nil
		case key.Matches(msg, k.Down):
			if m.settField >= 0 {
				m.settField = min(m.settField+1, nf-1)
			} else if m.settCur < len(m.baseRecipes)-1 {
				m.settCur++
			}
			return m, nil
		case key.Matches(msg, k.Top):
			m.settCur = 0
			m.settField = -1
			return m, nil
		case key.Matches(msg, k.Bottom):
			m.settCur = max(len(m.baseRecipes)-1, 0)
			m.settField = -1
			return m, nil
		}
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
		if key.Matches(msg, k.Select) {
			// Enter on a recipe row opens its detail rows;
			// enter on a detail row edits that field as a patch.
			if m.settField == -1 {
				m.settField = 0
			} else {
				m.startRecipeEdit(recipeFields()[clamp(m.settField, 0, len(recipeFields())-1)].name)
			}
			return m, nil
		}
		switch msg.String() {
		case "e", "E":
			m.startRecipeEdit("emulator")
			return m, nil
		case "a", "A":
			m.startRecipeEdit("args")
			return m, nil
		case "d", "D":
			m.dropRecipePatch()
			return m, nil
		case "J":
			m.moveRecipe(+1)
			return m, nil
		case "K":
			m.moveRecipe(-1)
			return m, nil
		}
		switch msg.Type {
		case tea.KeyDelete, tea.KeyBackspace:
			m.dropRecipePatch()
			return m, nil
		}
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
	case TabSteam:
		if key.Matches(msg, k.Select) {
			switch m.settCur {
			case steamRowAuto:
				m.toggleSteamAutoAdd()
			case steamRowAll:
				m.toggleSteamAllGames()
			case steamRowPrefix, steamRowTemplate, steamRowExe:
				m.settEditing = true
				m.settBuf = []rune(m.steamFieldValue(m.settCur))
			default:
				if m.steamSyncRow(m.settCur) {
					m.syncSteamNow(false)
				} else if _, ok := m.steamConsoleAt(m.settCur); ok {
					m.settEditing = true
					m.settBuf = []rune(m.steamFieldValue(m.settCur))
				}
			}
			return m, nil
		}
		// Space also toggles for discoverability.
		if msg.String() == " " && (m.settCur == steamRowAuto || m.settCur == steamRowAll) {
			if m.settCur == steamRowAuto {
				m.toggleSteamAutoAdd()
			} else {
				m.toggleSteamAllGames()
			}
			return m, nil
		}
		if key.Matches(msg, k.Rescan) {
			m.syncSteamNow(false)
			return m, nil
		}
	case TabIcons:
		if key.Matches(msg, k.Select) {
			switch m.settCur {
			case iconsRowProvider:
				m.cycleIconsProvider()
			case iconsRowKey, iconsRowDir:
				m.settEditing = true
				m.settBuf = []rune(m.iconsFieldValue(m.settCur))
			case iconsRowSync:
				return m, m.startIconsSync()
			case iconsRowPrune:
				m.pruneIconsCache()
			}
			return m, nil
		}
		// Space also cycles for discoverability.
		if msg.String() == " " && m.settCur == iconsRowProvider {
			m.cycleIconsProvider()
			return m, nil
		}
		if key.Matches(msg, k.Rescan) {
			return m, m.startIconsSync()
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
		// Bright active tab only while the focus is inside the panel;
		// on hover the whole strip stays dim.
		if t == m.settTab && m.settActive {
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

// renderSettingsLines builds the settings panel content for width w:
// tabs on top, tab content below, padded to bodyH rows. The panel swaps
// in for the games+metadata panes while the settings row is hovered.
func (m Model) renderSettingsLines(w int) []string {
	var lines []string
	lines = append(lines, m.renderSettingsTabs(w))
	lines = append(lines, "")
	switch m.settTab {
	case TabLang:
		lines = append(lines, m.renderLangTab(w)...)
	case TabRecipes:
		lines = append(lines, m.renderRecipesTab(w)...)
	case TabSteam:
		lines = append(lines, m.renderSteamTab(w)...)
	case TabIcons:
		lines = append(lines, m.renderIconsTab(w)...)
	default:
		lines = append(lines, m.renderPathsTab(w)...)
	}
	for len(lines) < m.bodyH() {
		lines = append(lines, "")
	}
	return lines[:m.bodyH()]
}

func (m Model) renderLangTab(w int) []string {
	var lines []string
	cur := m.langCur()
	for i, opt := range langOptions() {
		label := opt[1]
		var plain string
		switch i {
		case m.settCur:
			plain = styledRow(fit("  "+truncate(label, w-2), w))
		case cur:
			// The active language: bold white, no background.
			plain = "  " + langActive.Render(truncate(label, w-2))
		default:
			// Everything else: grey italic.
			plain = "  " + langIdle.Render(truncate(label, w-2))
		}
		lines = append(lines, plain)
	}
	return lines
}

func (m Model) renderRecipesTab(w int) []string {
	var lines []string
	dot := infoStyle.Render(" • ")
	head := i18n.T("sett.recipes.base", len(m.baseRecipes)) + dot +
		i18n.T("sett.recipes.patches", len(emulate.AppliedPatches(m.baseRecipes, m.patches)))
	lines = append(lines, fit(head, w))
	lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("sett.recipes.keys"), w), w)))
	lines = append(lines, "")
	if len(m.baseRecipes) == 0 {
		lines = append(lines, infoStyle.Render(fit(i18n.T("sett.recipes.none"), w)))
		return lines
	}
	marks := emulate.PatchedNames(m.baseRecipes, m.patches)
	effList := m.recipes
	if len(effList) != len(m.baseRecipes) {
		effList = emulate.ApplyPatches(m.baseRecipes, m.patches)
	}
	fields := recipeFields()
	for i, r := range m.baseRecipes {
		tag := ""
		if marks[r.Name] {
			tag = " [" + i18n.T("sett.recipes.patched") + "]"
		}
		// Effective (patched) emulator shown; base file stays untouched.
		effEmu := r.Emulator
		if i < len(effList) {
			effEmu = effList[i].Emulator
		} else {
			for _, p := range emulate.PatchesFor(m.patches, r.Name) {
				if p.Emulator != nil {
					effEmu = *p.Emulator
				}
			}
		}
		if _, ok := emulate.LookupBinary(effEmu); !ok {
			tag += " [" + i18n.T("sett.recipes.missing") + "]"
		}
		row := fmt.Sprintf("%s → %s%s", r.Name, effEmu, tag)
		plain := fit("  "+truncate(row, w-2), w)
		if i == m.settCur {
			plain = styledBarRow(plain)
		}
		lines = append(lines, plain)
		// Detail rows live directly under the selected recipe with a left
		// bar; values stay grey, the cursor row is highlighted instead.
		if i != m.settCur {
			continue
		}
		var eff emulate.Recipe
		if i < len(effList) {
			eff = effList[i]
		} else {
			eff = r
		}
		for fi, f := range fields {
			label := i18n.T(f.label)
			if m.settEditing && m.settPatchField == f.name {
				lines = append(lines, styledBarRow(editLine("    ", label, string(m.settBuf), w)))
				continue
			}
			val := fieldDisplay(f.name, r, eff)
			if fi == m.settField {
				row := label + ": " + val
				lines = append(lines, styledBarRow(fit("    "+truncate(row, w-4), w)))
				continue
			}
			lines = append(lines, barRow(idleDetailRow(label, val, w)))
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
			lines = append(lines, styledRow(editLine("  ", label, string(m.settBuf), w)))
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
