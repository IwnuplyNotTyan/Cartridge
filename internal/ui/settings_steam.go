package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cartridge/internal/i18n"
	"cartridge/internal/rom"
	"cartridge/internal/steam"
)

// Steam tab rows: two toggles, three text fields, one row per console
// with a custom name, and the sync action.
const (
	steamRowAuto = iota
	steamRowAll
	steamRowPrefix
	steamRowTemplate
	steamRowExe
	steamRowConsoles // first console row; last row is sync
)

// steamConsoleRows returns consoles present in the library (Home excluded).
func (m Model) steamConsoleRows() []ConsoleRow {
	if len(m.rows) <= 1 {
		return nil
	}
	return m.rows[1:]
}

// steamRows counts the Steam tab rows.
func steamRowsFor(m Model) int { return steamRowConsoles + len(m.steamConsoleRows()) + 1 }

// steamConsoleAt maps a settings cursor row to a console, if it is one.
func (m Model) steamConsoleAt(row int) (ConsoleRow, bool) {
	cons := m.steamConsoleRows()
	i := row - steamRowConsoles
	if i < 0 || i >= len(cons) {
		return ConsoleRow{}, false
	}
	return cons[i], true
}

// steamSyncRow reports whether row is the trailing "sync now" action.
func (m Model) steamSyncRow(row int) bool {
	return row == steamRowConsoles+len(m.steamConsoleRows())
}

// steamFieldValue returns the editable text for Steam text rows.
func (m Model) steamFieldValue(row int) string {
	switch row {
	case steamRowPrefix:
		return m.steam.Prefix
	case steamRowTemplate:
		return m.steam.EffectiveTemplate()
	case steamRowExe:
		return m.steam.Exe
	default:
		if c, ok := m.steamConsoleAt(row); ok {
			if v, has := m.steam.Categories[c.ID]; has {
				return v
			}
		}
		return ""
	}
}

// steamCategoryFor renders the per-console Steam category for display.
func (m Model) steamCategoryFor(id, name string) string {
	tags := steam.TagsFor(rom.Game{ConsoleID: id, ConsoleName: name}, m.steam)
	if len(tags) == 0 {
		return ""
	}
	if m.steam.AllGames && len(tags) > 1 {
		return tags[1]
	}
	return tags[0]
}

// toggleSteamAllGames flips the shared "all games" root category and
// re-syncs so existing shortcuts/collections pick the new names up.
func (m *Model) toggleSteamAllGames() tea.Cmd {
	m.steam.AllGames = !m.steam.AllGames
	if err := m.persistSettings(); err != nil {
		return m.setStatus(i18n.T("status.sett.save_fail", err))
	}
	return m.syncSteamNow(false)
}

// syncSteamNow syncs games to Steam (all or current filter) and reports.
// prune=false here: never delete from the settings screen by surprise.
func (m *Model) syncSteamNow(prune bool) tea.Cmd {
	targets := steam.DiscoverTargets(m.steam.Users)
	if len(targets) == 0 {
		return m.setStatus(i18n.T("status.steam.no_steam"))
	}
	exe := strings.TrimSpace(m.steam.Exe)
	if exe == "" {
		exe = steam.DefaultExe(m.steam)
	}
	results, err := steam.SyncAllTracked(m.games, m.steam, exe, "", prune, steam.TrackFilePath(m.libPath))
	if err != nil {
		return m.setStatus(i18n.T("status.steam.fail", err))
	}
	added, total := 0, 0
	for _, r := range results {
		added += r.Added
		total = r.Total
	}
	note := i18n.T("status.steam.synced", added, total, len(results))
	if steam.SteamRunning() {
		note += " • " + i18n.T("status.steam.running")
	}
	return m.setStatus(note)
}

// addCurrentToSteam adds the selected game to Steam.
func (m *Model) addCurrentToSteam() tea.Cmd {
	g := m.selected()
	if g == nil {
		return m.setStatus(i18n.T("status.copy_empty"))
	}
	game := *g
	targets := steam.DiscoverTargets(m.steam.Users)
	if len(targets) == 0 {
		return m.setStatus(i18n.T("status.steam.no_steam"))
	}
	exe := strings.TrimSpace(m.steam.Exe)
	if exe == "" {
		exe = steam.DefaultExe(m.steam)
	}
	done := 0
	for _, t := range targets {
		if _, err := steam.SyncFileTracked(t.Path, []rom.Game{game}, m.steam, exe, "", false, t.UserID, steam.TrackFilePath(m.libPath)); err != nil {
			return m.setStatus(i18n.T("status.steam.fail", err))
		}
		done++
	}
	note := i18n.T("status.steam.added", game.Title, done)
	if steam.SteamRunning() {
		note += " • " + i18n.T("status.steam.running")
	}
	return m.setStatus(note)
}

// renderSteamTab renders the Steam integration tab: main settings,
// per-console custom names and the sync action, split by divider strips.
func (m Model) renderSteamTab(w int) []string {
	var lines []string
	n := steamRowsFor(m)
	for i := 0; i < n; i++ {
		if i == steamRowConsoles && len(m.steamConsoleRows()) > 0 {
			lines = append(lines, sectionStrip(i18n.T("pane.consoles"), w))
		}
		if m.steamSyncRow(i) {
			lines = append(lines, sectionStrip("", w))
		}
		lines = append(lines, m.renderSteamRow(i, w))
	}
	lines = append(lines, "")
	lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("sett.steam.hint"), w), w)))
	lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("sett.steam.names_hint"), w), w)))
	// Preview for the selected game (if any): name + categories.
	if g := m.selected(); g != nil {
		s := steam.BuildShortcut(*g, m.steam, firstNonEmptySteam(m.steam.Exe, steam.DefaultExe(m.steam)), "")
		preview := fmt.Sprintf("%s → %s", s.AppName, strings.Join(s.Tags, ", "))
		lines = append(lines, infoStyle.Render(fit(truncate(preview, w), w)))
	}
	return lines
}

// renderSteamRow renders one Steam tab row: toggles, text fields with
// idle styling, console rows as "SHORT → category (count)".
func (m Model) renderSteamRow(i, w int) string {
	cur := i == m.settCur
	var row string
	switch {
	case i == steamRowAuto:
		row = i18n.T("sett.steam.auto", onOffWord(m.steam.AutoAdd))
	case i == steamRowAll:
		row = i18n.T("sett.steam.all", onOffWord(m.steam.AllGames))
		if m.steam.AllGames && strings.TrimSpace(m.steam.Prefix) != "" {
			row += fmt.Sprintf(" (%s)", strings.TrimSpace(m.steam.Prefix))
		}
	case i == steamRowPrefix:
		row = i18n.T("sett.steam.prefix") + ": " + displayOrDash(m.steam.Prefix)
	case i == steamRowTemplate:
		row = i18n.T("sett.steam.template") + ": " + m.steam.EffectiveTemplate()
	case i == steamRowExe:
		row = i18n.T("sett.steam.exe") + ": " + displayOrDash(m.steam.Exe)
	case m.steamSyncRow(i):
		row = i18n.T("sett.steam.sync_now")
	default:
		if c, ok := m.steamConsoleAt(i); ok {
			custom := ""
			if _, has := m.steam.Categories[c.ID]; has {
				custom = " *"
			}
			row = fmt.Sprintf("%s → %s (%d)%s", c.Short, m.steamCategoryFor(c.ID, c.Name), c.Count, custom)
		}
	}
	if row == "" {
		return ""
	}
	if m.settEditing && cur && (i == steamRowPrefix || i == steamRowTemplate || i == steamRowExe) {
		return styledRow(editLine("  ", labelOf(row), string(m.settBuf), w))
	}
	if m.settEditing && cur {
		if c, ok := m.steamConsoleAt(i); ok {
			return styledRow(editLine("  ", c.Short+" →", string(m.settBuf), w))
		}
	}
	plain := fit("  "+truncate(row, w-2), w)
	if cur {
		return styledRow(plain)
	}
	return idlePathRow(labelOf(row), valueOf(row), w)
}

func onOffWord(b bool) string {
	if b {
		return i18n.T("sett.steam.on")
	}
	return i18n.T("sett.steam.off")
}

func displayOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func labelOf(row string) string {
	if i := strings.Index(row, ":"); i >= 0 {
		return strings.TrimSpace(row[:i])
	}
	return row
}

func valueOf(row string) string {
	if i := strings.Index(row, ":"); i >= 0 {
		return strings.TrimSpace(row[i+1:])
	}
	return ""
}

func firstNonEmptySteam(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// sectionStrip renders a divider strip between settings groups,
// optionally labeled (e.g. ── Consoles ──).
func sectionStrip(label string, w int) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return sepStyle.Render(fit(strings.Repeat("─", w), w))
	}
	text := "── " + label + " "
	if pad := w - lipgloss.Width(text); pad > 0 {
		text += strings.Repeat("─", pad)
	}
	return sepStyle.Render(fit(text, w))
}
