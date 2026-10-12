package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/i18n"
	"cartridge/internal/library"
	"cartridge/internal/rom"
	"cartridge/internal/steam"
)

// Update: bubbletea messages, search and launch.
// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.help.SetWidth(m.contentW())
		m.sizeMeta()
		m.refreshMeta()
		return m, nil

	case rescanMsg:
		// Explicit rescan (r) is authoritative: the shelf reflects
		// exactly what is on disk, so entries for deleted ROMs are
		// pruned here (unlike the conservative startup merge, which
		// keeps them). The result is persisted, so the shelf and
		// library.json never diverge until the next restart.
		// Guard: a failed scan (errors plus an empty result while we
		// had games, e.g. an unmounted drive) keeps the stored
		// collection instead of wiping the library.
		if len(msg.games) == 0 && len(m.games) > 0 && len(msg.errs) > 0 {
			return m, m.setStatus(i18n.T("status.rescan_err",
				0, pluralWord(0, "word.rom"),
				len(msg.errs), pluralWord(len(msg.errs), "word.error")))
		}
		m.setGames(msg.games)
		if m.libPath != "" {
			if err := library.Save(m.libPath, m.games); err != nil {
				m.sizeMeta()
				m.refreshMeta()
				return m, m.setStatus(i18n.T("status.save_fail", err))
			}
		}
		var note string
		if len(msg.errs) > 0 {
			note = i18n.T("status.rescan_err",
				len(m.games), pluralWord(len(m.games), "word.rom"),
				len(msg.errs), pluralWord(len(msg.errs), "word.error"))
		} else {
			note = i18n.T("status.rescan", len(m.games), pluralWord(len(m.games), "word.rom"))
		}
		// Steam auto-add on TUI rescan (same rule as CLI startup).
		if m.steam.AutoAdd && len(m.games) > 0 && len(steam.DiscoverTargets(m.steam.Users)) > 0 {
			steamTimer := m.syncSteamNow(false)
			// Keep both facts visible: rescan count + steam result.
			if m.status != "" {
				note = note + " • " + m.status
			}
			m.sizeMeta()
			m.refreshMeta()
			return m, tea.Batch(steamTimer, m.setStatus(note))
		}
		m.sizeMeta()
		m.refreshMeta()
		return m, m.setStatus(note)

	case launchDoneMsg:
		var note string
		if msg.err != nil {
			note = i18n.T("status.launch_fail", msg.err)
		} else {
			if m.play != nil {
				m.play.Add(msg.gameID, msg.dur)
				if err := m.play.Save(); err != nil {
					m.refreshMeta()
					return m, m.setStatus(i18n.T("status.save_fail", err))
				}
			}
			note = i18n.T("status.played", i18n.Duration(msg.dur))
		}
		// A background run finished: mark it, seal the log tail with
		// an exit line, stop the line waiter. Playtime above is kept.
		if m.run != nil && msg.gameID == m.run.gameID {
			m.run.done = true
			m.runCh = nil
			m.run.lines = append(m.run.lines, i18n.T("run.exitline", i18n.Duration(msg.dur)))
			if len(m.run.lines) > maxRunLines {
				m.run.lines = m.run.lines[len(m.run.lines)-maxRunLines:]
			}
			if m.followRun {
				m.runScroll = m.runMaxScroll()
			}
		}
		m.refreshMeta()
		return m, m.setStatus(note)

	case runLogMsg:
		m.appendRunLine(msg.line)
		if m.runCh != nil {
			return m, waitRunLog(m.runCh)
		}
		return m, nil

	case iconsProgMsg:
		m.iconsDone = msg.done
		m.iconsTotal = msg.total
		progTimer := m.setStatus(i18n.T("status.icons.fetching", msg.done, msg.total))
		if m.iconsCh != nil {
			return m, tea.Batch(waitIconsProgress(m.iconsCh), progTimer)
		}
		return m, progTimer

	case iconsDoneMsg:
		m.iconsActive = false
		m.iconsCh = nil
		m.iconsOk = msg.fetched
		m.iconsFail = msg.failed
		m.refreshIconsStat()
		if msg.errMsg != "" {
			return m, m.setStatus(i18n.T("status.steam.fail", msg.errMsg))
		}
		note := i18n.T("status.icons.synced",
			msg.fetched, msg.heroFetched, msg.logoFetched, msg.failed, msg.applied, msg.heroes, msg.logos, msg.profiles)
		if msg.runningNote {
			note += " • " + i18n.T("status.steam.running")
		}
		return m, m.setStatus(note)

	case artOpenedMsg:
		var note string
		if msg.err != nil {
			note = i18n.T("status.art.fail", msg.err)
		} else {
			note = i18n.T("status.art.opened", msg.label)
		}
		return m, m.setStatus(note)

	case clearStatusMsg:
		if msg.token == m.statusToken {
			m.clearStatus()
		}
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

// launchSelected starts the emulator for the selected game in the
// background: the TUI stays up on the log screen instead of handing
// the whole terminal over. Playtime is recorded when it exits.
func (m Model) launchSelected() (tea.Model, tea.Cmd) {
	g := m.selected()
	if g == nil {
		return m, nil
	}
	game := *g
	r := m.gameRecipe(game)
	if r == nil {
		return m, m.setStatus(i18n.T("status.no_recipe", game.Title))
	}
	cmd, err := m.launchCmd(game, r)
	if err != nil {
		return m, m.setStatus(i18n.T("status.launch_fail", err))
	}
	return m.startRun(game, cmd)
}

// activeQuery returns the visible games filter: the live draft while
// typing, the committed query otherwise.
func (m Model) activeQuery() string {
	if m.searching {
		return string(m.searchBuf)
	}
	return m.query
}

// updateSearch handles keys while the search input line is open:
// printable input filters the games list live, enter keeps the filter
// and jumps to it, esc drops it. Arrows keep browsing the live results.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.searching = false
		m.searchBuf = nil
		m.query = ""
		m.sizeMeta()
		m.refilter()
		return m, nil
	case tea.KeyEnter:
		m.searching = false
		m.query = strings.TrimSpace(string(m.searchBuf))
		m.searchBuf = nil
		m.sizeMeta()
		m.setFocus(PaneGames)
		m.refilter()
		return m, nil
	case tea.KeyBackspace:
		if len(m.searchBuf) > 0 {
			m.searchBuf = m.searchBuf[:len(m.searchBuf)-1]
		}
		m.refilter()
		return m, nil
	case tea.KeyUp:
		if m.gameCur > 0 {
			m.selectGame(m.gameCur - 1)
		}
		return m, nil
	case tea.KeyDown:
		if m.gameCur < len(m.filtered)-1 {
			m.selectGame(m.gameCur + 1)
		}
		return m, nil
	case tea.KeyRunes, tea.KeySpace:
		m.searchBuf = append(m.searchBuf, msg.Runes...)
		m.refilter()
		return m, nil
	}
	if key.Matches(msg, m.keys.Quit) { // ctrl+c still quits mid-input
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if m.settActive {
		return m.updateSettings(msg)
	}
	if m.searching {
		return m.updateSearch(msg)
	}
	switch {
	case key.Matches(msg, k.Quit):
		m.killRun() // no orphan emulator keeps running after quit
		return m, tea.Quit
	case key.Matches(msg, k.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		m.sizeMeta()
		return m, nil
	case key.Matches(msg, k.Launch):
		return m.launchSelected()
	case key.Matches(msg, k.Favorite):
		return m, m.toggleFavorite()
	case key.Matches(msg, k.Steam):
		return m, m.addCurrentToSteam()
	case key.Matches(msg, k.Search):
		// Prefill with the committed filter so it can be refined.
		m.searching = true
		m.searchBuf = []rune(m.query)
		m.sizeMeta()
		m.refilter()
		return m, nil
	}
	if m.runView {
		return m.updateRunView(msg)
	}
	if m.showHelp && msg.String() == "esc" {
		m.showHelp = false
		m.help.ShowAll = false
		m.sizeMeta()
		return m, nil
	}
	switch {
	case key.Matches(msg, k.Next):
		if m.onSettingsRow() {
			m.openSettings()
			return m, nil
		}
		m.setFocus((m.focus + 1) % 3)
		return m, nil
	case key.Matches(msg, k.Prev):
		if m.onSettingsRow() {
			m.leaveSettingsRow()
			return m, nil
		}
		m.setFocus((m.focus + 2) % 3)
		return m, nil
	}
	switch msg.String() {
	case "1":
		m.setFocus(PaneConsoles)
		return m, nil
	case "2":
		if !m.onSettingsRow() {
			m.setFocus(PaneGames)
		}
		return m, nil
	case "3":
		if !m.onSettingsRow() {
			m.setFocus(PaneMeta)
		}
		return m, nil
	case "esc":
		// An expanded cheat/mod/art list collapses first, like settings details.
		if m.cheatsOpen || m.modsOpen || m.artOpen {
			m.cheatsOpen = false
			m.modsOpen = false
			m.artOpen = false
			m.refreshMeta()
			return m, nil
		}
		if m.onSettingsRow() && m.focus == PaneConsoles {
			// The games pane is hidden behind the settings panel:
			// step back to the consoles instead of focusing it.
			m.leaveSettingsRow()
			return m, nil
		}
		m.setFocus(PaneGames)
		return m, nil
	}

	switch m.focus {
	case PaneConsoles:
		switch {
		case key.Matches(msg, k.Up):
			if m.consCur > 0 {
				m.consCur--
				m.gameCur = 0
				m.refilter()
			}
		case key.Matches(msg, k.Down):
			if m.consCur < len(m.rows) {
				m.consCur++
				m.gameCur = 0
				m.refilter()
			}
		case key.Matches(msg, k.PageUp):
			m.consCur = max(m.consCur-10, 0)
			m.gameCur = 0
			m.refilter()
		case key.Matches(msg, k.PageDown):
			m.consCur = min(m.consCur+10, len(m.rows))
			m.gameCur = 0
			m.refilter()
		case key.Matches(msg, k.Top):
			m.consCur = 0
			m.gameCur = 0
			m.refilter()
		case key.Matches(msg, k.Bottom):
			m.consCur = len(m.rows)
			m.gameCur = 0
			m.refilter()
		case key.Matches(msg, k.Select):
			if m.onSettingsRow() {
				m.openSettings()
			} else {
				m.setFocus(PaneGames)
			}
		}
	case PaneGames:
		switch {
		case key.Matches(msg, k.Up):
			if m.gameCur > 0 {
				m.selectGame(m.gameCur - 1)
			}
		case key.Matches(msg, k.Down):
			if m.gameCur < len(m.filtered)-1 {
				m.selectGame(m.gameCur + 1)
			}
		case key.Matches(msg, k.PageUp):
			m.selectGame(max(m.gameCur-10, 0))
		case key.Matches(msg, k.PageDown):
			m.selectGame(min(m.gameCur+10, max(len(m.filtered)-1, 0)))
		case key.Matches(msg, k.Top):
			m.selectGame(0)
		case key.Matches(msg, k.Bottom):
			m.selectGame(max(len(m.filtered)-1, 0))
		case key.Matches(msg, k.Rescan):
			return m, tea.Batch(m.rescanCmd(), m.setStatus(i18n.T("status.scanning", m.romDir)))
		case key.Matches(msg, k.Select):
			m.setFocus(PaneMeta)
		}
	case PaneMeta:
		switch {
		case key.Matches(msg, k.Up):
			m.metaMove(-1)
		case key.Matches(msg, k.Down):
			m.metaMove(1)
		case key.Matches(msg, k.PageUp):
			m.metaMove(-max(m.meta.Height-1, 1))
		case key.Matches(msg, k.PageDown):
			m.metaMove(max(m.meta.Height-1, 1))
		case key.Matches(msg, k.Top):
			m.metaEdge(false)
		case key.Matches(msg, k.Bottom):
			m.metaEdge(true)
		case key.Matches(msg, k.Copy):
			return m.metaCopy()
		case key.Matches(msg, k.Deploy):
			return m, m.toggleSoftpatch()
		case key.Matches(msg, k.Select):
			return m.metaSelect()
		case key.Matches(msg, k.Rescan):
			return m, tea.Batch(m.rescanCmd(), m.setStatus(i18n.T("status.scanning", m.romDir)))
		}
		switch msg.String() {
		case "n":
			return m, m.cycleAlt(true)
		case "p":
			return m, m.cycleAlt(false)
		}
	}

	// Rescan is global: allow it from the consoles pane too.
	if key.Matches(msg, k.Rescan) {
		return m, tea.Batch(m.rescanCmd(), m.setStatus(i18n.T("status.scanning", m.romDir)))
	}
	return m, nil
}

func (m Model) rescanCmd() tea.Cmd {
	dir := m.romDir
	cache := m.games
	return func() tea.Msg {
		games, errs := rom.ScanDir(dir, cache)
		return rescanMsg{games: games, errs: errs}
	}
}

func (m *Model) sizeMeta() {
	_, _, mw := m.layout()
	m.meta.Width = max(mw, 10)
	m.meta.Height = max(m.bodyH()-1, 3) // one line for the METADATA title
}
