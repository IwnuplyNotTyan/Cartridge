package ui

import (
	"context"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/config"
	"cartridge/internal/i18n"
	"cartridge/internal/icons"
	"cartridge/internal/steam"
)

// Icons tab rows: provider cycle, two text fields, two actions.
// Cache stats render as dim info lines, not selectable rows.
const (
	iconsRowProvider = iota
	iconsRowKey
	iconsRowDir
	iconsRowSync
	iconsRowPrune
)

const iconsRowCount = 5

// iconsProviders cycles on enter/space: free CDN, free only, premium, off.
var iconsProviders = []string{icons.ProviderAuto, icons.ProviderLibretro, icons.ProviderSteamGrid, icons.ProviderOff}

// iconsRowsFor counts the Icons tab rows.
func iconsRowsFor() int { return iconsRowCount }

// iconsFieldValue returns the editable text for Icons text rows.
func (m Model) iconsFieldValue(row int) string {
	switch row {
	case iconsRowKey:
		return m.steam.SteamGridDBKey
	case iconsRowDir:
		return m.steam.IconsDir
	default:
		return ""
	}
}

// maskedKey shows the SteamGridDB key without leaking it on screen.
func maskedKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return "—"
	}
	if len(k) <= 4 {
		return "••••"
	}
	return k[:4] + "…"
}

// cycleIconsProvider steps the art source and persists it.
func (m *Model) cycleIconsProvider() {
	cur := strings.ToLower(strings.TrimSpace(m.steam.IconsProvider))
	if cur == "" {
		cur = icons.ProviderAuto
	}
	next := iconsProviders[0]
	for i, p := range iconsProviders {
		if p == cur {
			next = iconsProviders[(i+1)%len(iconsProviders)]
			break
		}
	}
	m.steam.IconsProvider = next
	if err := m.persistSettings(); err != nil {
		m.status = i18n.T("status.sett.save_fail", err)
		return
	}
	m.status = i18n.T("status.sett.saved")
}

// refreshIconsStat recomputes cache numbers for the tab: file walk plus
// one stat per library game. Runs on demand (tab enter, after actions),
// never per render, so 10k-game libraries stay snappy.
func (m *Model) refreshIconsStat() {
	cache := steam.IconsCache(m.steam)
	st := cache.Stat()
	cached, heroes, logos := 0, 0, 0
	for _, g := range m.games {
		if _, ok := cache.Hit(g.ID); ok {
			cached++
		}
		if _, ok := cache.HitHero(g.ID); ok {
			heroes++
		}
		if _, ok := cache.HitLogo(g.ID); ok {
			logos++
		}
	}
	m.iconsFiles = st.Files
	m.iconsBytes = st.Bytes
	m.iconsMiss = st.Failed
	m.iconsCached = cached
	m.iconsHeroes = heroes
	m.iconsLogos = logos
	m.iconsStatFresh = true
}

// pruneIconsCache drops cached art for games gone from the library.
func (m *Model) pruneIconsCache() {
	cache := steam.IconsCache(m.steam)
	keep := make(map[string]bool, len(m.games))
	for _, g := range m.games {
		keep[g.ID] = true
	}
	removed, freed, err := cache.Prune(keep)
	if err != nil {
		m.status = i18n.T("status.sett.save_fail", err)
		return
	}
	m.refreshIconsStat()
	m.status = i18n.T("status.icons.pruned", removed, float64(freed)/(1<<20))
}

// iconsProgMsg streams bulk-fetch progress (throttled by the sender).
type iconsProgMsg struct {
	done, total int
}

// iconsDoneMsg reports the finished fetch plus the Steam apply pass.
type iconsDoneMsg struct {
	fetched, skipped, failed int
	heroFetched, heroFailed   int
	logoFetched, logoFailed   int
	bytes                    int64
	applied, gridNew, art    int
	heroes                   int
	logos                    int
	profiles                 int
	errMsg                   string
	runningNote              bool
}

// waitIconsProgress listens for the next progress message.
func waitIconsProgress(ch chan iconsProgMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// startIconsSync fetches missing art in the background and applies it to
// Steam, streaming progress into the status bar. Never blocks the TUI:
// a 10k-game fetch runs for minutes while the shelf stays usable.
func (m *Model) startIconsSync() tea.Cmd {
	if m.iconsActive {
		m.status = i18n.T("status.icons.busy")
		return nil
	}
	st := m.steam
	cache := steam.IconsCache(st)
	if cache.Provider == icons.ProviderOff {
		m.status = i18n.T("status.icons.off")
		return nil
	}
	if cache.Provider == icons.ProviderSteamGrid && strings.TrimSpace(cache.GridDBKey) == "" {
		m.status = i18n.T("status.icons.no_key")
		return nil
	}
	if len(m.games) == 0 {
		m.status = i18n.T("status.copy_empty")
		return nil
	}
	roms := m.games
	libPath := m.libPath
	ch := make(chan iconsProgMsg, 128)
	m.iconsCh = ch
	m.iconsActive = true
	m.iconsTotal = len(roms)
	m.iconsDone, m.iconsOk, m.iconsFail = 0, 0, 0
	m.status = i18n.T("status.icons.fetching", 0, len(roms))
	fetch := func() tea.Msg {
		var n int64
		res := cache.Sync(context.Background(), steam.ToIconGames(roms), false, 0, func(_ string, ok bool) {
			v := atomic.AddInt64(&n, 1)
			if v%10 == 0 || int(v) == len(roms) {
				select {
				case ch <- iconsProgMsg{done: int(v), total: len(roms)}:
				default:
				}
			}
		})
		close(ch)
		// Apply: re-sync shortcuts so Icon paths + grid art land in Steam.
		// Local stats/copies only, no network — safe inside the worker.
		done := iconsDoneMsg{
			fetched: res.Fetched, skipped: res.Skipped, failed: res.Failed,
			heroFetched: res.HeroFetched, heroFailed: res.HeroFailed,
			logoFetched: res.LogoFetched, logoFailed: res.LogoFailed,
			bytes: res.Bytes,
		}
		exe := strings.TrimSpace(st.Exe)
		if exe == "" {
			exe = steam.DefaultExe(st)
		}
		results, err := steam.SyncAllTracked(roms, st, exe, "", false, steam.TrackFilePath(libPath))
		if err != nil {
			done.errMsg = err.Error()
			return done
		}
		done.profiles = len(results)
		for _, r := range results {
			done.applied += r.IconsApplied
			done.gridNew += r.GridDeployed
			done.art = r.GridWithArt
			done.heroes += r.HeroWithArt
			done.logos += r.LogoWithArt
		}
		done.runningNote = steam.SteamRunning()
		return done
	}
	return tea.Batch(fetch, waitIconsProgress(ch))
}

// renderIconsTab renders the Icons integration tab: art source, key and
// cache dir, cached-art stats, and the fetch/prune actions.
func (m Model) renderIconsTab(w int) []string {
	var lines []string
	for i := 0; i < iconsRowsFor(); i++ {
		lines = append(lines, m.renderIconsRow(i, w))
	}
	lines = append(lines, "")
	if !m.iconsStatFresh {
		lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("sett.icons.stats_hint"), w), w)))
	} else {
		stats := i18n.T("sett.icons.cached", m.iconsFiles, float64(m.iconsBytes)/(1<<20), m.iconsCached, m.iconsHeroes, m.iconsLogos, len(m.games))
		lines = append(lines, infoStyle.Render(fit(truncate(stats, w), w)))
		if m.iconsMiss > 0 {
			lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("sett.icons.missing", m.iconsMiss), w), w)))
		}
	}
	if m.iconsActive {
		lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("status.icons.fetching", m.iconsDone, m.iconsTotal), w), w)))
	} else {
		lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("sett.icons.hint"), w), w)))
	}
	if m.steam.EffectiveIconsProvider() == icons.ProviderSteamGrid && strings.TrimSpace(m.steam.SteamGridDBKey) == "" {
		lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("sett.icons.no_key"), w), w)))
	}
	return lines
}

// renderIconsRow renders one Icons tab row.
func (m Model) renderIconsRow(i, w int) string {
	cur := i == m.settCur
	var row string
	switch i {
	case iconsRowProvider:
		row = i18n.T("sett.icons.provider") + ": " + m.steam.EffectiveIconsProvider()
	case iconsRowKey:
		row = i18n.T("sett.icons.key") + ": " + maskedKey(m.steam.SteamGridDBKey)
	case iconsRowDir:
		row = i18n.T("sett.icons.dir") + ": " + displayOrDash(steam.IconsDir(m.steam))
	case iconsRowSync:
		row = i18n.T("sett.icons.sync_now")
	case iconsRowPrune:
		row = i18n.T("sett.icons.prune")
	}
	if row == "" {
		return ""
	}
	if m.settEditing && cur && (i == iconsRowKey || i == iconsRowDir) {
		return styledRow(editLine("  ", labelOf(row), string(m.settBuf), w))
	}
	plain := fit("  "+truncate(row, w-2), w)
	if cur {
		return styledRow(plain)
	}
	return idlePathRow(labelOf(row), valueOf(row), w)
}

// saveIconsEdit persists the key/dir text rows.
func (m *Model) saveIconsEdit(v string) {
	switch m.settCur {
	case iconsRowKey:
		m.steam.SteamGridDBKey = strings.TrimSpace(v)
	case iconsRowDir:
		m.steam.IconsDir = config.ExpandPath(strings.TrimSpace(v))
	}
	if err := m.persistSettings(); err != nil {
		m.status = i18n.T("status.sett.save_fail", err)
		return
	}
	// A moved cache dir changes where art attaches from: re-apply at once.
	if m.settCur == iconsRowDir {
		m.syncSteamNow(false)
	} else {
		m.status = i18n.T("status.sett.saved")
	}
	m.refreshIconsStat()
}
