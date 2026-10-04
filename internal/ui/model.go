// Package ui implements the 3-column cartridge browser:
// consoles | games | metadata, separated by grey dividers,
// with bubbles full help above the bottom info bar on "?".
package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	lipglossv2 "charm.land/lipgloss/v2"
	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/charmtone"

	"cartridge/internal/cheats"
	"cartridge/internal/emulate"
	"cartridge/internal/favorites"
	"cartridge/internal/i18n"
	"cartridge/internal/mods"
	"cartridge/internal/overrides"
	"cartridge/internal/playtime"
	"cartridge/internal/rom"
)

// Pane is the focused column.
type Pane int

const (
	PaneConsoles Pane = iota
	PaneGames
	PaneMeta
)

// keyMap wires real keybindings (used both for input and the help view).
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Prev     key.Binding
	Next     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Select   key.Binding
	Rescan   key.Binding
	Launch   key.Binding
	Settings key.Binding
	Copy     key.Binding
	Deploy   key.Binding
	Search   key.Binding
	Favorite key.Binding
	Alt      key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", i18n.T("key.up"))),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", i18n.T("key.down"))),
		Prev:     key.NewBinding(key.WithKeys("shift+tab", "h", "left"), key.WithHelp("←/h", i18n.T("key.pane_left"))),
		Next:     key.NewBinding(key.WithKeys("tab", "l", "right"), key.WithHelp("tab", i18n.T("key.pane_right"))),
		Top:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", i18n.T("key.first"))),
		Bottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", i18n.T("key.last"))),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", i18n.T("key.page_up"))),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", i18n.T("key.page_down"))),
		Select:   key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", i18n.T("key.open"))),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", i18n.T("key.search"))),
		Rescan:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", i18n.T("key.rescan"))),
		Launch:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", i18n.T("key.launch"))),
		Settings: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", i18n.T("key.settings"))),
		Copy:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", i18n.T("key.copy"))),
		Deploy:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", i18n.T("key.deploy"))),
		Favorite: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", i18n.T("key.favorite"))),
		Alt:      key.NewBinding(key.WithKeys("n", "p"), key.WithHelp("n/p", i18n.T("key.alt"))),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", i18n.T("key.help"))),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", i18n.T("key.quit"))),
	}
}

// pluralWord picks the localized noun form for n ("word.rom" → ROM/ROMs…).
func pluralWord(n int, key string) string {
	return i18n.Word(n, i18n.T(key+".one"), i18n.T(key+".few"), i18n.T(key+".many"))
}

// ShortHelp implements help.KeyMap (unused: only full help is shown).
func (k keyMap) ShortHelp() []key.Binding { return []key.Binding{k.Help, k.Quit} }

// FullHelp implements help.KeyMap.
// NB: bubbles renders each inner slice as a COLUMN, so keep exactly 4:
// more columns widen every help line and break narrow screens.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Prev, k.Next, k.Select, k.Launch},
		{k.PageUp, k.PageDown, k.Rescan, k.Settings},
		{k.Copy, k.Deploy, k.Search, k.Favorite, k.Alt, k.Help, k.Quit},
	}
}

// ConsoleRow is one entry of the left column. Besides consoles it holds
// two pinned rows on top: favorites ("favorites") and recent games
// ("recent"); "all" is the full library.
type ConsoleRow struct {
	ID    string
	Name  string
	Short string
	Count int
}

// Pinned row IDs and the recent section size.
const (
	rowFavorites = "favorites"
	rowRecent    = "recent"
	rowAll       = "all"

	recentLimit = 5
)

// metaRow is one navigable metadata row: copy holds the clipboard text,
// kind is "recipe"/"core" on switchable rows, "cheats" on the summary row,
// "cheat" on one expanded cheat (cheat = its index), "mods" on the mods
// summary row, "mod" on one expanded mod file (enter plays it via a
// hard-patched instance, d toggles the softpatch copy), "sep" on separators.
type metaRow struct {
	kind  string
	label string
	lines []string
	copy  string
	sep   bool
	cheat int
}

type rescanMsg struct {
	games []rom.Game
	errs  []error
}

// Model is the bubbletea root model.
type Model struct {
	games    []rom.Game
	rows     []ConsoleRow // rows[0] is always "All"
	consCur  int
	gameCur  int
	filtered []rom.Game
	// Search state: query is the committed filter (session-only),
	// searching + searchBuf is the live input line above the info bar.
	query     string
	searching bool
	searchBuf []rune

	focus    Pane
	meta     viewport.Model
	metaCur  int       // cursor over metaRows (separators skipped)
	metaRows []metaRow // navigable metadata rows for copying/switching
	// Cheat list state: cached file per game+path, expansion flag.
	// cheatPile is a pile source to materialize a missing live path from.
	cheatKey     string
	cheatPath    string
	cheatPile    string
	cheatStat    cheatFileStat
	cheatFile    *cheats.File
	cheatMissing bool
	cheatsOpen   bool
	// Mod list state: cached files per game+recipe+dirs, expansion flag.
	// modTarget maps a pile file to the exact deployed path RetroArch
	// autopatches by (game stem + ext, numbered on collision);
	// modActive marks slots whose bytes already sit in place.
	modKey    string
	modLive   string
	modFiles  []mods.Mod
	modTarget map[string]string
	modActive map[string]bool
	modBuilt  map[string]bool
	modCheck  map[string]mods.Check
	modStat   modCacheStat
	modsOpen  bool
	keys      keyMap
	help      help.Model
	showHelp  bool

	width  int
	height int
	ready  bool

	romDir  string
	libPath string
	status  string

	recipes []emulate.Recipe
	// overrides persists per-game emulator/core choices from metadata (n/p).
	overrides *overrides.Store
	play      *playtime.Store
	// favorites persists favorite game IDs pinned above the consoles list.
	favorites *favorites.Store

	// Settings screen state. Only the bottom bar and help survive
	// from the browser view; tabs sit on top.
	mode        Mode
	settTab     SettingsTab
	settCur     int
	settEditing bool
	settBuf     []rune
	// settPatchField selects which recipe patch field is being edited
	// in the Recipes tab ("emulator", "args", "consoles", ...).
	settPatchField string
	// settField is the detail-field cursor under the selected recipe:
	// -1 means the recipe row itself, 0..N-1 means one of the
	// recipeFields() rows shown directly under the selected recipe.
	settField int

	cfgFile     string
	recipesPath string
	patchesPath string
	modsDir     string
	patchedDir  string
	savesDir    string
	cheatsDir   string
	langRaw     string

	baseRecipes []emulate.Recipe
	patches     []emulate.RecipePatch
}

// launchDoneMsg reports an emulator run back to the model.
type launchDoneMsg struct {
	gameID string
	dur    time.Duration
	err    error
}

// Option configures the model.
type Option func(*Model)

// WithRecipes sets emulator launch recipes.
func WithRecipes(r []emulate.Recipe) Option {
	return func(m *Model) { m.recipes = r }
}

// WithPlaytime sets the play-time store.
func WithPlaytime(s *playtime.Store) Option {
	return func(m *Model) { m.play = s }
}

// WithOverrides sets the per-game emulator/core choice store.
func WithOverrides(s *overrides.Store) Option {
	return func(m *Model) { m.overrides = s }
}

// WithFavorites sets the favorite games store shown on the home page.
func WithFavorites(s *favorites.Store) Option {
	return func(m *Model) { m.favorites = s }
}

// WithBaseRecipes keeps the unpatched base recipes for the settings view.
func WithBaseRecipes(r []emulate.Recipe) Option {
	return func(m *Model) { m.baseRecipes = r }
}

// WithPatches sets recipe overlay patches (never rewrite the base file).
func WithPatches(p []emulate.RecipePatch) Option {
	return func(m *Model) { m.patches = p }
}

// WithPaths sets config file location, recipes/patches paths and data dirs.
func WithPaths(cfgFile, recipesPath, patchesPath, modsDir, savesDir, cheatsDir string) Option {
	return func(m *Model) {
		m.cfgFile = cfgFile
		m.recipesPath = recipesPath
		m.patchesPath = patchesPath
		m.modsDir = modsDir
		m.savesDir = savesDir
		m.cheatsDir = cheatsDir
	}
}

// WithPatchedDir sets where hard-patched ROM instances live.
func WithPatchedDir(dir string) Option {
	return func(m *Model) { m.patchedDir = dir }
}

// WithLangRaw remembers the raw language value from config ("auto"/"en"/"ru").
func WithLangRaw(raw string) Option {
	return func(m *Model) { m.langRaw = raw }
}

// New builds the model from an initial scan.
func New(games []rom.Game, romDir, libPath string, opts ...Option) Model {
	m := Model{
		romDir:  romDir,
		libPath: libPath,
		meta:    viewport.New(0, 0),
		keys:    newKeyMap(),
		help:    help.New(),
	}
	m.help.ShowAll = true
	// NB: help comes from bubbles v2, so its styles use charm.land/lipgloss/v2
	// where Color is a constructor func, not a type.
	m.help.Styles.ShortKey = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Hazy.Hex()))
	m.help.Styles.ShortDesc = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Squid.Hex()))
	m.help.Styles.ShortSeparator = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Iron.Hex()))
	m.help.Styles.FullKey = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Hazy.Hex()))
	m.help.Styles.FullDesc = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Squid.Hex()))
	m.help.Styles.FullSeparator = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Iron.Hex()))
	m.help.Styles.Ellipsis = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Iron.Hex()))
	for _, opt := range opts {
		opt(&m)
	}
	if len(m.baseRecipes) == 0 && len(m.recipes) > 0 {
		m.baseRecipes = append([]emulate.Recipe(nil), m.recipes...)
	}
	if m.langRaw == "" {
		m.langRaw = "auto"
	}
	m.setGames(games)
	// Start on the favorites shelf when it has games, else the library:
	// the pinned rows are always visible, but an empty shelf is a poor
	// landing page. Rescans keep the user's own position (see setGames).
	if m.favCount() == 0 {
		for i, r := range m.rows {
			if r.ID == rowAll {
				m.consCur = i
				break
			}
		}
		m.refilter()
	}
	return m
}

func (m *Model) setGames(games []rom.Game) {
	// Drop stale rows that are not valid games (mod files, archive junk
	// from older scans lingering in the library): a rescan cleans the
	// view at once, without waiting for the next restart save.
	filtered := make([]rom.Game, 0, len(games))
	for _, g := range games {
		if rom.ValidGame(g) {
			filtered = append(filtered, g)
		}
	}
	m.games = filtered
	counts := map[string]int{}
	names := map[string]string{}
	for _, g := range games {
		counts[g.ConsoleID]++
		names[g.ConsoleID] = g.ConsoleName
	}
	// Pinned rows always sit on top: favorites first (the startup shelf),
	// then recent, then the full library.
	m.rows = []ConsoleRow{
		{ID: rowFavorites, Name: "★ " + i18n.T("home.favorites"), Short: "★", Count: m.favCount()},
		{ID: rowRecent, Name: i18n.T("home.recent"), Short: "…", Count: len(m.recentGames())},
		{ID: rowAll, Name: i18n.T("row.all"), Count: len(games)},
	}
	for _, c := range rom.Consoles {
		if n, ok := counts[c.ID]; ok {
			m.rows = append(m.rows, ConsoleRow{ID: c.ID, Name: c.Name, Short: c.Short, Count: n})
		}
	}
	// Keep unknown consoles (if any) at the end instead of dropping them.
	if n, ok := counts["unknown"]; ok {
		m.rows = append(m.rows, ConsoleRow{ID: "unknown", Name: i18n.T("row.unknown"), Short: "???", Count: n})
	}
	if m.consCur >= len(m.rows) {
		m.consCur = 0
	}
	m.gameCur = 0
	m.refilter()
}

// favCount counts favorited games in the current library.
func (m Model) favCount() int {
	n := 0
	for _, g := range m.games {
		if m.favorites.Has(g.ID) {
			n++
		}
	}
	return n
}

// recentGames returns up to recentLimit played games, newest first.
func (m Model) recentGames() []rom.Game {
	type played struct {
		g  rom.Game
		at int64 // unix nano, -1 when unknown
	}
	var list []played
	for _, g := range m.games {
		at := int64(-1)
		if m.play != nil {
			if st, ok := m.play.Get(g.ID); ok {
				at = st.LastPlayed.UnixNano()
			}
		}
		if at >= 0 {
			list = append(list, played{g, at})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].at == list[j].at {
			return list[i].g.ID < list[j].g.ID
		}
		return list[i].at > list[j].at
	})
	if len(list) > recentLimit {
		list = list[:recentLimit]
	}
	out := make([]rom.Game, 0, len(list))
	for _, p := range list {
		out = append(out, p.g)
	}
	return out
}

// refreshRowCounts recomputes every sidebar counter in place without
// touching cursors, so toggling a favorite updates counts instantly.
func (m *Model) refreshRowCounts() {
	counts := map[string]int{}
	for _, g := range m.games {
		counts[g.ConsoleID]++
	}
	for i, r := range m.rows {
		switch r.ID {
		case rowFavorites:
			m.rows[i].Count = m.favCount()
		case rowRecent:
			m.rows[i].Count = len(m.recentGames())
		case rowAll:
			m.rows[i].Count = len(m.games)
		default:
			m.rows[i].Count = counts[r.ID]
		}
	}
}

func (m *Model) refilter() {
	id := rowAll
	if m.consCur < len(m.rows) {
		id = m.rows[m.consCur].ID
	}
	q := m.query
	if m.searching {
		q = string(m.searchBuf)
	}
	q = strings.ToLower(strings.TrimSpace(q))
	// Scope first: favorites, recent (newest first, capped) or a console.
	var scoped []rom.Game
	switch id {
	case rowFavorites:
		for _, g := range m.games {
			if m.favorites.Has(g.ID) {
				scoped = append(scoped, g)
			}
		}
	case rowRecent:
		scoped = m.recentGames()
	default:
		for _, g := range m.games {
			if id == rowAll || g.ConsoleID == id {
				scoped = append(scoped, g)
			}
		}
	}
	m.filtered = m.filtered[:0]
	for _, g := range scoped {
		if q != "" && !strings.Contains(strings.ToLower(g.Title), q) {
			continue
		}
		m.filtered = append(m.filtered, g)
	}
	if m.gameCur >= len(m.filtered) {
		m.gameCur = max(len(m.filtered)-1, 0)
	}
	m.metaCur = 0
	m.cheatsOpen = false
	m.modsOpen = false
	m.refreshMeta()
}

func (m Model) selected() *rom.Game {
	if len(m.filtered) == 0 || m.gameCur >= len(m.filtered) {
		return nil
	}
	return &m.filtered[m.gameCur]
}

// selectGame moves the game cursor and resets the meta pane to the top,
// so the new game's title is always visible instead of a deep-scrolled
// tail that looks identical across games of one console.
func (m *Model) selectGame(i int) {
	m.gameCur = i
	m.metaCur = 0
	m.cheatsOpen = false
	m.modsOpen = false
	m.refreshMeta()
}

// setFocus moves browser focus, re-syncing the meta viewport geometry.
// The visible pane set depends on focus, so without this a meta pane
// shown again after being hidden would keep its stale narrow width
// and render ~10 cells wide until the next resize.
func (m *Model) setFocus(p Pane) {
	if m.focus == p {
		return
	}
	m.focus = p
	m.sizeMeta()
	m.refreshMeta()
}

func (m *Model) refreshMeta() {
	m.syncCheatCache()
	m.syncModCache()
	m.metaRows = m.buildMetaRows()
	m.metaCur = clampMetaCur(m.metaRows, m.metaCur)
	content, starts := m.renderMetaRows()
	m.meta.SetContent(content)
	m.metaFollowCursor(starts, strings.Count(content, "\n"))
}

// clampMetaCur keeps the cursor on a selectable row.
func clampMetaCur(rows []metaRow, cur int) int {
	if len(rows) == 0 {
		return 0
	}
	if cur >= 0 && cur < len(rows) && !rows[cur].sep {
		return cur
	}
	for i := range rows {
		if !rows[i].sep {
			return i
		}
	}
	return 0
}

// metaFollowCursor scrolls the viewport so the cursor row stays visible.
func (m *Model) metaFollowCursor(starts []int, total int) {
	if len(m.metaRows) == 0 || len(starts) == 0 {
		m.meta.SetYOffset(0)
		return
	}
	cur := clamp(m.metaCur, 0, len(starts)-1)
	start := starts[cur]
	end := total - 1
	if cur+1 < len(starts) {
		end = starts[cur+1] - 1
	}
	h := max(m.meta.Height, 1)
	switch {
	case start < m.meta.YOffset:
		m.meta.SetYOffset(start)
	case end >= m.meta.YOffset+h:
		m.meta.SetYOffset(end - h + 1)
	}
}

// gameRecipe returns the effective recipe for g: a manually picked
// alternative when set and still matching, otherwise the hierarchy winner.
func (m Model) gameRecipe(g rom.Game) *emulate.Recipe {
	alts := emulate.MatchAll(m.recipes, g)
	if len(alts) == 0 {
		return nil
	}
	if c, ok := m.overrides.Get(g.ID); ok {
		for _, r := range alts {
			if r.Name == c.Recipe {
				return r
			}
		}
	}
	return alts[0]
}

// gameCore returns the selected core for g under r: a manually picked one
// when set and still listed, otherwise the recipe default.
// The list and the resolved index follow.
func (m Model) gameCore(g rom.Game, r *emulate.Recipe) (string, []string, int) {
	cs := r.CoresFor(g.ConsoleID)
	if len(cs) == 0 {
		return "", nil, 0
	}
	if c, ok := m.overrides.Get(g.ID); ok {
		for i, core := range cs {
			if core == c.Core {
				return core, cs, i
			}
		}
	}
	return cs[0], cs, 0
}

// launchCmd builds the emulator process, honoring a manually picked core.
func (m Model) launchCmd(game rom.Game, r *emulate.Recipe) (*exec.Cmd, error) {
	if c, ok := m.overrides.Get(game.ID); ok && c.Core != "" {
		if cs := r.CoresFor(game.ConsoleID); len(cs) > 0 {
			core := cs[0]
			for _, cand := range cs {
				if cand == c.Core {
					core = cand
					break
				}
			}
			rc := *r
			rc.Cores = map[string][]string{game.ConsoleID: {core}}
			return rc.Command(game)
		}
	}
	return r.Command(game)
}

// metaEdge jumps to the first (last=false) or last selectable row.
func (m *Model) metaEdge(last bool) {
	if len(m.metaRows) == 0 {
		return
	}
	if last {
		m.metaCur = len(m.metaRows) - 1
		for m.metaCur > 0 && m.metaRows[m.metaCur].sep {
			m.metaCur--
		}
	} else {
		m.metaCur = clampMetaCur(m.metaRows, 0)
	}
	m.refreshMeta()
}

// metaMove shifts the metadata cursor, skipping separators.
func (m *Model) metaMove(d int) {
	if len(m.metaRows) == 0 {
		return
	}
	i := m.metaCur
	for {
		i += d
		if i < 0 {
			i = 0
			break
		}
		if i >= len(m.metaRows) {
			i = len(m.metaRows) - 1
			break
		}
		if !m.metaRows[i].sep {
			break
		}
	}
	m.metaCur = i
	m.refreshMeta()
}

// metaCopy copies the cursor row value to the terminal clipboard (OSC52).
func (m Model) metaCopy() (tea.Model, tea.Cmd) {
	if m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return m, nil
	}
	row := m.metaRows[m.metaCur]
	if row.sep || strings.TrimSpace(row.copy) == "" {
		m.status = i18n.T("status.copy_empty")
		return m, nil
	}
	what := row.label
	if what == "" {
		what = row.copy
	}
	m.status = i18n.T("status.copied", what)
	return m, copyCmd(row.copy)
}

// metaSelect activates the cursor row: expands the cheat list, toggles
// a cheat, deploys a mod file beside the ROM, copies anything else.
// It returns a Model value like the other update paths, even though it
// mutates through a pointer.
func (m *Model) metaSelect() (tea.Model, tea.Cmd) {
	if m.metaCur >= 0 && m.metaCur < len(m.metaRows) {
		switch m.metaRows[m.metaCur].kind {
		case "mod":
			return m.launchModSelected()
		case "mods":
			m.modsOpen = !m.modsOpen
			m.refreshMeta()
			return *m, nil
		case "cheats":
			if m.cheatsOpen {
				m.cheatsOpen = false
				m.refreshMeta()
				return *m, nil
			}
			// Opening: materialize a missing file first (pile copy or
			// empty skeleton), then make sure everything is enabled.
			if m.cheatFile == nil {
				if !m.cheatMissing || m.cheatPath == "" {
					break // bad file or nowhere to create: copy instead
				}
				if err := m.materializeCheats(); err != nil {
					m.status = i18n.T("status.save_fail", err)
					return *m, nil
				}
			} else {
				m.ensureCheatsOn()
			}
			m.cheatsOpen = true
			m.refreshMeta()
			return *m, nil
		}
	}
	return m.metaCopy()
}

// launchModSelected builds the hard-patched ROM instance for the cursor
// mod file (reusing a fresh one) and starts the emulator on it, exactly
// like a vanilla launch but with the copied game file. Works with any
// emulator or core: no softpatch support needed on their side.
func (m Model) launchModSelected() (tea.Model, tea.Cmd) {
	if m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return m, nil
	}
	row := m.metaRows[m.metaCur]
	if row.kind != "mod" || strings.TrimSpace(row.copy) == "" {
		return m, nil
	}
	src := row.copy
	g := m.selected()
	if g == nil {
		return m, nil
	}
	game := *g
	r := m.gameRecipe(game)
	if r == nil {
		m.status = i18n.T("status.no_recipe", game.Title)
		return m, nil
	}
	instance, _, err := mods.EnsureInstance(m.patchedDir, game.ID, game.Path, game.Inner, src)
	if err != nil {
		m.status = i18n.T("status.launch_fail", err)
		return m, nil
	}
	game.Path = instance
	game.Inner = ""
	cmd, err := m.launchCmd(game, r)
	if err != nil {
		m.status = i18n.T("status.launch_fail", err)
		return m, nil
	}
	m.status = i18n.T("status.mod.launching", game.Title, filepath.Base(src))
	m.modKey = "" // a fresh instance changes the [built] marks
	m.refreshMeta()
	start := time.Now()
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return launchDoneMsg{gameID: game.ID, dur: time.Since(start), err: err}
	})
}

// toggleSoftpatch places the cursor mod file beside the ROM under the
// exact name RetroArch autopatches by, or removes that copy when it is
// already there. A deployed file with foreign bytes (hand-placed, not
// ours) is reported and kept.
func (m *Model) toggleSoftpatch() {
	if m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return
	}
	row := m.metaRows[m.metaCur]
	if row.kind != "mod" || strings.TrimSpace(row.copy) == "" {
		return
	}
	src := row.copy
	g := m.selected()
	if g == nil {
		return
	}
	// Plan fresh at action time: the cache may predate a hand-placed
	// file beside the ROM, and deploying over it would destroy foreign
	// bytes. PlanDeploy never picks an occupied slot with different
	// content, stacking to .ips1 instead.
	ext := strings.ToLower(filepath.Ext(src))
	dst, active := mods.PlanDeploy(src, g.Path, g.Inner, ext)
	if !active {
		chk := m.modCheck[src]
		if err := mods.Deploy(src, dst); err != nil {
			m.status = i18n.T("status.save_fail", err)
			return
		}
		m.modKey = "" // deployed bytes sit outside the watched dirs: recollect
		m.refreshMeta()
		msg := i18n.T("status.mod.deployed", filepath.Base(dst))
		if d := modCheckDetail(*g, chk); d != "" {
			msg += " - " + d
		}
		m.status = msg
		return
	}
	if err := mods.Undeploy(src, dst); err != nil {
		if errors.Is(err, mods.ErrNotOurs) {
			m.status = i18n.T("status.mod.not_ours", filepath.Base(dst))
		} else {
			m.status = i18n.T("status.save_fail", err)
		}
		return
	}
	m.modKey = ""
	m.refreshMeta()
	m.status = i18n.T("status.mod.removed", filepath.Base(dst))
}

// toggleFavorite flips the favorite mark on the selected game and
// persists it. Row counts refresh in place so the cursor never jumps.
func (m *Model) toggleFavorite() {
	g := m.selected()
	if g == nil || m.favorites == nil {
		return
	}
	if m.favorites.Toggle(g.ID) {
		m.status = i18n.T("status.fav.added", g.Title)
	} else {
		m.status = i18n.T("status.fav.removed", g.Title)
	}
	if err := m.favorites.Save(); err != nil {
		m.status = i18n.T("status.save_fail", err)
		return
	}
	m.refreshRowCounts()
	m.refreshMeta()
}

// copyCmd writes text to the terminal clipboard via OSC52 (works over SSH;
// tmux needs `set-clipboard on` or passthrough).
func copyCmd(s string) tea.Cmd {
	return func() tea.Msg {
		seq := osc52.New(s)
		if os.Getenv("TMUX") != "" {
			seq = seq.Tmux()
		}
		fmt.Fprint(os.Stdout, seq)
		return nil
	}
}

// cycleAlt switches the cursor row option: another matching emulator on
// the recipe row, another core on the core row. n moves forward, p back.
func (m *Model) cycleAlt(next bool) {
	g := m.selected()
	if g == nil || m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return
	}
	// saveChoice persists the per-game choice; on failure the status
	// reports it and the meta view still reflects the in-memory pick.
	save := func(ch overrides.Choice) {
		m.overrides.Set(g.ID, ch)
		if err := m.overrides.Save(); err != nil {
			m.status = i18n.T("status.save_fail", err)
			return
		}
		m.status = ""
		m.refreshMeta()
	}
	switch m.metaRows[m.metaCur].kind {
	case "recipe":
		alts := emulate.MatchAll(m.recipes, *g)
		if len(alts) < 2 {
			return
		}
		cur := 0
		if c, ok := m.overrides.Get(g.ID); ok {
			for i, r := range alts {
				if r.Name == c.Recipe {
					cur = i
					break
				}
			}
		}
		if next {
			cur = (cur + 1) % len(alts)
		} else {
			cur = (cur - 1 + len(alts)) % len(alts)
		}
		ch, _ := m.overrides.Get(g.ID)
		ch.Recipe = alts[cur].Name
		save(ch)
	case "core":
		r := m.gameRecipe(*g)
		if r == nil {
			return
		}
		cs := r.CoresFor(g.ConsoleID)
		if len(cs) < 2 {
			return
		}
		_, _, cur := m.gameCore(*g, r)
		if next {
			cur = (cur + 1) % len(cs)
		} else {
			cur = (cur - 1 + len(cs)) % len(cs)
		}
		ch, _ := m.overrides.Get(g.ID)
		ch.Core = cs[cur]
		save(ch)
	}
}

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
		m.setGames(msg.games)
		if len(msg.errs) > 0 {
			m.status = i18n.T("status.rescan_err",
				len(msg.games), pluralWord(len(msg.games), "word.rom"),
				len(msg.errs), pluralWord(len(msg.errs), "word.error"))
		} else {
			m.status = i18n.T("status.rescan", len(msg.games), pluralWord(len(msg.games), "word.rom"))
		}
		m.sizeMeta()
		m.refreshMeta()
		return m, nil

	case launchDoneMsg:
		if msg.err != nil {
			m.status = i18n.T("status.launch_fail", msg.err)
		} else {
			if m.play != nil {
				m.play.Add(msg.gameID, msg.dur)
				if err := m.play.Save(); err != nil {
					m.status = i18n.T("status.save_fail", err)
					m.refreshMeta()
					return m, nil
				}
			}
			m.status = i18n.T("status.played", i18n.Duration(msg.dur))
		}
		m.refreshMeta()
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

// launchSelected starts the emulator for the selected game via its recipe.
// The TUI is suspended while the emulator runs; the session is timed.
func (m Model) launchSelected() (tea.Model, tea.Cmd) {
	g := m.selected()
	if g == nil {
		return m, nil
	}
	game := *g
	r := m.gameRecipe(game)
	if r == nil {
		m.status = i18n.T("status.no_recipe", game.Title)
		return m, nil
	}
	cmd, err := m.launchCmd(game, r)
	if err != nil {
		m.status = i18n.T("status.launch_fail", err)
		return m, nil
	}
	m.status = i18n.T("status.launching", game.Title)
	start := time.Now()
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return launchDoneMsg{gameID: game.ID, dur: time.Since(start), err: err}
	})
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
	if m.mode == ModeSettings {
		return m.updateSettings(msg)
	}
	if m.searching {
		return m.updateSearch(msg)
	}
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		m.sizeMeta()
		return m, nil
	case key.Matches(msg, k.Launch):
		return m.launchSelected()
	case key.Matches(msg, k.Favorite):
		m.toggleFavorite()
		return m, nil
	case key.Matches(msg, k.Settings):
		m.openSettings()
		return m, nil
	case key.Matches(msg, k.Search):
		// Prefill with the committed filter so it can be refined.
		m.searching = true
		m.searchBuf = []rune(m.query)
		m.sizeMeta()
		m.refilter()
		return m, nil
	}
	if m.showHelp && msg.String() == "esc" {
		m.showHelp = false
		m.help.ShowAll = false
		m.sizeMeta()
		return m, nil
	}
	switch {
	case key.Matches(msg, k.Next):
		m.setFocus((m.focus + 1) % 3)
		return m, nil
	case key.Matches(msg, k.Prev):
		m.setFocus((m.focus + 2) % 3)
		return m, nil
	}
	switch msg.String() {
	case "1":
		m.setFocus(PaneConsoles)
		return m, nil
	case "2":
		m.setFocus(PaneGames)
		return m, nil
	case "3":
		m.setFocus(PaneMeta)
		return m, nil
	case "esc":
		// An expanded cheat/mod list collapses first, like settings details.
		if m.cheatsOpen || m.modsOpen {
			m.cheatsOpen = false
			m.modsOpen = false
			m.refreshMeta()
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
			if m.consCur < len(m.rows)-1 {
				m.consCur++
				m.gameCur = 0
				m.refilter()
			}
		case key.Matches(msg, k.PageUp):
			m.consCur = max(m.consCur-10, 0)
			m.gameCur = 0
			m.refilter()
		case key.Matches(msg, k.PageDown):
			m.consCur = min(m.consCur+10, len(m.rows)-1)
			m.gameCur = 0
			m.refilter()
		case key.Matches(msg, k.Top):
			m.consCur = 0
			m.gameCur = 0
			m.refilter()
		case key.Matches(msg, k.Bottom):
			m.consCur = len(m.rows) - 1
			m.gameCur = 0
			m.refilter()
		case key.Matches(msg, k.Select):
			m.setFocus(PaneGames)
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
			m.status = i18n.T("status.scanning", m.romDir)
			return m, m.rescanCmd()
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
			m.toggleSoftpatch()
			return m, nil
		case key.Matches(msg, k.Select):
			return m.metaSelect()
		case key.Matches(msg, k.Rescan):
			m.status = i18n.T("status.scanning", m.romDir)
			return m, m.rescanCmd()
		}
		switch msg.String() {
		case "n":
			m.cycleAlt(true)
		case "p":
			m.cycleAlt(false)
		}
	}

	// Rescan is global: allow it from the consoles pane too.
	if key.Matches(msg, k.Rescan) {
		m.status = i18n.T("status.scanning", m.romDir)
		return m, m.rescanCmd()
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

func (m Model) helpView() string { return m.help.View(m.keys) }

func (m Model) helpHeight() int {
	if !m.showHelp {
		return 0
	}
	return max(lipgloss.Height(m.helpView()), 1)
}

// searchHeight reserves one row for the live search input line.
func (m Model) searchHeight() int {
	if m.searching {
		return 1
	}
	return 0
}

// renderSearch draws the one-line search input above the info bar:
// "/query▊", or a dim hint while empty.
func (m Model) renderSearch() string {
	w := m.contentW()
	text := "/" + string(m.searchBuf) + "▊"
	if len(m.searchBuf) == 0 {
		text += " " + infoStyle.Render(i18n.T("search.hint"))
	}
	return fit(text, w)
}

// contentW/H is the drawable area inside the outer window gap.
func (m Model) contentW() int {
	w := m.width
	if w <= 0 {
		w = 120
	}
	return max(w-2*appGap, 20)
}

func (m Model) contentH() int {
	h := m.height
	if h <= 0 {
		h = 30
	}
	return max(h-2*appGap, 10)
}

func (m Model) bodyH() int {
	return max(m.contentH()-1-m.helpHeight()-m.searchHeight(), 5) // single info-bar line, no slack
}

// Column breakpoints on content width: three panes fit comfortably,
// two panes share the space, one pane takes it all.
const (
	threeColMin = 90
	twoColMin   = 56
)

// visiblePanes returns the panes to draw for the current width and focus.
// Half screens keep consoles+games on the left side and games+metadata
// once a game is in play (games/meta focus); tiny screens show only the
// focused pane. The focused pane is always visible.
func (m Model) visiblePanes() []Pane {
	w := m.contentW()
	if w >= threeColMin {
		return []Pane{PaneConsoles, PaneGames, PaneMeta}
	}
	if w >= twoColMin {
		if m.focus == PaneConsoles {
			return []Pane{PaneConsoles, PaneGames}
		}
		return []Pane{PaneGames, PaneMeta}
	}
	return []Pane{m.focus}
}

// layout splits the content area into column inner widths (no borders,
// columns are separated by divider lines). Hidden panes get width 0.
// Widths account for column padding, so joined rows never overflow.
func (m Model) layout() (cw, gw, mw int) {
	w := m.contentW()
	vis := m.visiblePanes()
	if len(vis) == 3 {
		cw = clamp(w*30/100, 30, 40)
		mw = clamp(w*30/100, 28, 44)
		gw = max(w-cw-mw-2-4, 20) // 2 dividers + padding
		return cw, gw, mw
	}
	if len(vis) == 2 {
		first := clamp(w*38/100, 24, 40)
		second := max(w-first-4, 18) // 1 divider + column padding
		if vis[0] == PaneConsoles {
			return first, second, 0
		}
		return 0, first, second
	}
	// Single pane: keep room for the column padding around it.
	switch vis[0] {
	case PaneConsoles:
		return max(w-1, 10), 0, 0
	case PaneGames:
		return 0, max(w-2, 10), 0
	default:
		return 0, 0, max(w-1, 10)
	}
}

// View implements tea.Model.
// In settings mode only the bottom bar and help survive from the browser;
// tabs sit on top of the settings body.
func (m Model) View() string {
	if !m.ready {
		return i18n.T("ui.waiting")
	}
	var body string
	if m.mode == ModeSettings {
		body = m.renderSettings()
	} else {
		cw, gw, mw := m.layout()
		sep := sepStyle.Render(strings.TrimSuffix(strings.Repeat("│\n", m.bodyH()), "\n"))
		parts := make([]string, 0, 5)
		if cw > 0 {
			if len(parts) > 0 {
				parts = append(parts, sep)
			}
			parts = append(parts, m.renderConsoles(cw))
		}
		if gw > 0 {
			if len(parts) > 0 {
				parts = append(parts, sep)
			}
			parts = append(parts, m.renderGames(gw))
		}
		if mw > 0 {
			if len(parts) > 0 {
				parts = append(parts, sep)
			}
			parts = append(parts, m.renderMetaPane(mw))
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}
	var content string
	parts := []string{body}
	if m.searching {
		parts = append(parts, m.renderSearch())
	}
	if m.showHelp {
		parts = append(parts, m.renderHelp())
	}
	parts = append(parts, m.renderInfo())
	content = lipgloss.JoinVertical(lipgloss.Left, parts...)
	return appStyle.Render(content)
}

// column pads plain and pre-styled lines to exactly bodyH rows.
// Lines arrive already fitted to width; padL/padR add air around dividers.
func (m Model) column(lines []string, w, padL, padR int) string {
	_ = w
	l, r := strings.Repeat(" ", padL), strings.Repeat(" ", padR)
	for len(lines) < m.bodyH() {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = l + lines[i] + r
	}
	return strings.Join(lines[:m.bodyH()], "\n")
}

// styledRow highlights a pre-fitted plain row and marks it selected.
func styledRow(plain string) string {
	if len(plain) < 2 {
		return plain
	}
	return curStyle.Render("> ") + selStyle.Render(plain[2:])
}

// barRow marks a pre-fitted plain row with a left accent line instead of "> ".
func barRow(plain string) string {
	if len(plain) < 2 {
		return plain
	}
	return curStyle.Render("│ ") + plain[2:]
}

// styledBarRow highlights a pre-fitted plain row and marks it with a left
// accent line instead of "> ".
func styledBarRow(plain string) string {
	if len(plain) < 2 {
		return plain
	}
	return curStyle.Render("│ ") + selStyle.Render(plain[2:])
}

func (m Model) renderConsoles(w int) string {
	lines := []string{titleFor(i18n.T("pane.consoles"), m.focus == PaneConsoles, w)}
	visible := max(m.bodyH()-1, 1)
	start := clamp(m.consCur-visible/2, 0, max(len(m.rows)-visible, 0))
	for i := start; i < min(start+visible, len(m.rows)); i++ {
		r := m.rows[i]
		count := fmt.Sprintf("%d", r.Count)
		nameW := max(w-2-1-len(count), 4)
		name := truncate(r.Name, nameW)
		plain := fit("  "+name+strings.Repeat(" ", max(nameW-lipgloss.Width(name)+1, 1))+count, w)
		if i == m.consCur {
			plain = styledRow(plain)
		}
		lines = append(lines, plain)
	}
	return m.column(lines, w, 0, 1)
}

func (m Model) renderGames(w int) string {
	title := i18n.T("games.title.all", len(m.filtered))
	if m.consCur < len(m.rows) {
		switch m.rows[m.consCur].ID {
		case rowFavorites, rowRecent:
			title = fmt.Sprintf("%s (%d)", m.rows[m.consCur].Name, len(m.filtered))
		case rowAll:
		default:
			title = i18n.T("games.title.filter", m.rows[m.consCur].Short, len(m.filtered))
		}
	}
	if q := strings.TrimSpace(m.activeQuery()); q != "" {
		title = i18n.T("games.title.search", q, len(m.filtered))
	}
	lines := []string{titleFor(truncate(title, w), m.focus == PaneGames, w)}
	if len(m.filtered) == 0 {
		empty := i18n.T("games.empty")
		switch {
		case strings.TrimSpace(m.activeQuery()) != "":
			empty = i18n.T("games.empty.search")
		case m.consCur < len(m.rows) && m.rows[m.consCur].ID == rowFavorites:
			empty = i18n.T("home.empty.favorites")
		case m.consCur < len(m.rows) && m.rows[m.consCur].ID == rowRecent:
			empty = i18n.T("home.empty.recent")
		}
		lines = append(lines, infoStyle.Render(fit(empty, w)))
		return m.column(lines, w, 1, 1)
	}
	visible := max(m.bodyH()-1, 1)
	start := clamp(m.gameCur-visible/2, 0, max(len(m.filtered)-visible, 0))
	for i := start; i < min(start+visible, len(m.filtered)); i++ {
		mark := ""
		if m.favorites.Has(m.filtered[i].ID) {
			mark = " ★"
		}
		plain := fit("  "+truncate(m.filtered[i].Title, w-2-lipgloss.Width(mark))+mark, w)
		if i == m.gameCur {
			plain = styledRow(plain)
		}
		lines = append(lines, plain)
	}
	return m.column(lines, w, 1, 1)
}

func (m Model) renderMetaPane(w int) string {
	lines := []string{titleFor(i18n.T("pane.meta"), m.focus == PaneMeta, w)}
	for _, ln := range strings.Split(m.meta.View(), "\n") {
		lines = append(lines, ln)
	}
	return m.column(lines, w, 1, 0)
}

// syncCheatCache loads the selected game's cheat file, keeping the result
// while the inputs stay put. A cheap stat check notices externally created,
// edited or deleted files (by hand or by the emulator), so hand-made files
// start working without a game switch.
func (m *Model) syncCheatCache() {
	g := m.selected()
	var r *emulate.Recipe
	if g != nil {
		r = m.gameRecipe(*g)
	}
	if g == nil || r == nil || !r.Cheats.Supported {
		m.cheatKey, m.cheatPath, m.cheatPile = "", "", ""
		m.cheatFile, m.cheatMissing = nil, false
		m.cheatStat = cheatFileStat{}
		return
	}
	src := g.ID + "\x00" + r.Name + "\x00" + r.Cheats.Path + "\x00" + m.cheatsDir
	if src == m.cheatKey {
		if m.cheatPath == "" || !cheatChanged(m.cheatPath, m.cheatStat) {
			return
		}
		m.loadCheatFile(m.cheatPath)
		return
	}
	live, pile, searched := m.resolveCheatPath(*g, *r)
	if live == "" {
		// A searched-but-empty pile reads as "no file", not "bad file".
		m.cheatKey, m.cheatPath, m.cheatPile = "", "", ""
		m.cheatFile, m.cheatMissing = nil, searched
		m.cheatStat = cheatFileStat{}
		return
	}
	m.cheatKey, m.cheatPile = src, pile
	m.loadCheatFile(live)
}

// loadCheatFile (re)reads path into the cache; missing files and parse
// errors both cache as nil, told apart by cheatMissing.
func (m *Model) loadCheatFile(path string) {
	m.cheatPath = path
	m.cheatStat = statCheatFile(path)
	if f, err := cheats.Load(path); err == nil {
		m.cheatFile, m.cheatMissing = f, false
	} else if os.IsNotExist(err) {
		m.cheatFile, m.cheatMissing = nil, true
	} else {
		m.cheatFile, m.cheatMissing = nil, false
	}
}

// cheatFileStat is a cheap fingerprint of a cheat file for revalidation.
type cheatFileStat struct {
	exist bool
	mod   time.Time
	size  int64
}

// statCheatFile snapshots a path without reading it.
func statCheatFile(path string) cheatFileStat {
	st, err := os.Stat(path)
	if err != nil {
		return cheatFileStat{}
	}
	return cheatFileStat{exist: true, mod: st.ModTime(), size: st.Size()}
}

// cheatChanged reports whether path differs from a snapshot.
func cheatChanged(path string, prev cheatFileStat) bool {
	return statCheatFile(path) != prev
}

// resolveCheatPath finds the cheat file for the game. It returns the live
// path (explicit recipe path when it exists, else the pile match, else the
// intended path for display) plus a pile source when the live path is
// missing but the pile has content to materialize it from.
func (m Model) resolveCheatPath(g rom.Game, r emulate.Recipe) (live, pile string, searched bool) {
	tmpl, _ := r.Expand(r.Cheats.Path, g)
	tmpl = strings.TrimSpace(tmpl)
	if tmpl != "" {
		if _, err := os.Stat(tmpl); err == nil {
			return tmpl, "", false
		}
		if strings.TrimSpace(m.cheatsDir) == "" {
			return tmpl, "", false // missing, but show the intended path
		}
	}
	if dir := strings.TrimSpace(m.cheatsDir); dir != "" {
		if found, err := cheats.Find(dir, cheatCandidates(g)...); err == nil {
			if tmpl != "" {
				return tmpl, found, true
			}
			return found, "", true
		}
		if tmpl != "" {
			return tmpl, "", true
		}
		return "", "", true
	}
	return tmpl, "", false
}

// cheatCandidates orders file stems to match a game by: inner/ROM base
// name, title, short id, full id.
func cheatCandidates(g rom.Game) []string {
	base := func(p string) string {
		b := filepath.Base(p)
		if e := filepath.Ext(b); e != "" {
			b = strings.TrimSuffix(b, e)
		}
		return b
	}
	var out []string
	if g.Inner != "" {
		out = append(out, base(g.Inner))
	}
	if g.Path != "" {
		out = append(out, base(g.Path))
	}
	return append(out, g.Title, g.ShortID(), g.ID)
}

// buildMetaRows collects the navigable metadata rows for the selected game.
// Every row carries its clipboard text; recipe/core rows are switchable.
func (m Model) buildMetaRows() []metaRow {
	g := m.selected()
	if g == nil {
		return nil
	}
	w := max(m.meta.Width, 20)
	vw := max(w-metaKeyWidth, 10)
	kv := func(label, v string) metaRow {
		return metaRow{kind: "kv", label: label, lines: wrapSoft(v, vw), copy: v}
	}
	var rows []metaRow
	rows = append(rows, metaRow{kind: "title", lines: wrapSoft(g.Title, w), copy: g.Title})
	rows = append(rows, metaRow{sep: true})
	rows = append(rows, metaRow{kind: "id", label: i18n.T("meta.id"), lines: []string{g.ShortID()}, copy: g.ID})
	rows = append(rows, metaRow{kind: "hash", lines: wrapSoft(g.ID, max(w-4, 10)), copy: g.ID})
	con := fmt.Sprintf("%s (%s)", g.ConsoleName, g.ConsoleID)
	rows = append(rows, kv(i18n.T("meta.console"), con))
	if g.Region != "" {
		rows = append(rows, kv(i18n.T("meta.region"), g.Region))
	}
	if len(g.Tags) > 0 {
		rows = append(rows, kv(i18n.T("meta.tags"), strings.Join(g.Tags, ", ")))
	}
	rows = append(rows, kv(i18n.T("meta.size"), i18n.T("meta.size.fmt", g.Size, pluralWord(int(g.Size), "word.byte"), float64(g.Size)/(1<<20))))
	rows = append(rows, metaRow{sep: true})
	rows = append(rows, kv(i18n.T("meta.file"), g.Path))
	if g.Inner != "" {
		rows = append(rows, kv(i18n.T("meta.inner"), g.Inner))
	}
	rows = append(rows, metaRow{sep: true})
	if r := m.gameRecipe(*g); r != nil {
		alts := emulate.MatchAll(m.recipes, *g)
		disp := altLabel(r.Name, alts)
		rows = append(rows, metaRow{kind: "recipe", label: i18n.T("meta.recipe"), lines: wrapSoft(disp, vw), copy: r.Name})
		if core, cs, idx := m.gameCore(*g, r); len(cs) > 0 {
			cval := core
			if len(cs) > 1 {
				cval = fmt.Sprintf("%s (%d/%d)", core, idx+1, len(cs))
			}
			rows = append(rows, metaRow{kind: "core", label: i18n.T("meta.core"), lines: wrapSoft(cval, vw), copy: core})
		}
		rows = append(rows, m.cheatsRows(*g, *r, vw)...)
		rows = append(rows, kv(i18n.T("meta.achievements"), featText(r.Achievements.Supported, r.Achievements.Note, *g, r)))
		rows = append(rows, m.modsRows(*g, *r, vw)...)
	} else {
		rows = append(rows, metaRow{kind: "kv", label: i18n.T("meta.recipe"), lines: wrapSoft(i18n.T("recipe.none"), vw)})
	}
	if m.play != nil {
		if st, ok := m.play.Get(g.ID); ok {
			played := i18n.T("meta.played.fmt",
				i18n.Duration(st.Total()), st.Sessions,
				pluralWord(st.Sessions, "word.session"),
				st.LastPlayed.Format("2006-01-02 15:04"))
			rows = append(rows, kv(i18n.T("meta.played"), played))
		}
	}
	return rows
}

// cheatsRows builds the cheats summary row (with no-file or bad-file mark)
// plus one row per cheat while the list is expanded. Every listed cheat
// is available at once; there are no per-cheat choices.
func (m Model) cheatsRows(g rom.Game, r emulate.Recipe, vw int) []metaRow {
	base := featText(r.Cheats.Supported, r.Cheats.Path, g, &r)
	if !r.Cheats.Supported {
		return []metaRow{{kind: "cheats", label: i18n.T("meta.cheats"), lines: wrapSoft(base, vw), copy: base}}
	}
	switch {
	case m.cheatMissing:
		base += " (" + i18n.T("meta.cheats.nofile") + ")"
	case m.cheatFile == nil:
		base += " (" + i18n.T("meta.cheats.bad") + ")"
	}
	rows := []metaRow{{kind: "cheats", label: i18n.T("meta.cheats"), lines: wrapSoft(base, vw), copy: base}}
	if m.cheatsOpen && m.cheatFile != nil {
		for i, c := range m.cheatFile.Cheats {
			name := strings.TrimSpace(c.Desc)
			if name == "" {
				name = strings.TrimSpace(c.Code)
			}
			rows = append(rows, metaRow{kind: "cheat", lines: wrapSoft(name, vw), copy: c.Code, cheat: i})
		}
	}
	return rows
}

// materializeCheats creates the missing live cheat file: copying the pile
// match when there is one, else an empty skeleton. Saves always write
// every cheat enabled, so whatever lands here is usable at once.
func (m *Model) materializeCheats() error {
	live := m.cheatPath
	if live == "" {
		return fmt.Errorf("no cheat path")
	}
	if m.cheatPile != "" {
		src, err := cheats.Load(m.cheatPile)
		if err != nil {
			return err
		}
		if len(src.Cheats) > 0 {
			src.Path = live
			if err := src.Save(); err != nil {
				return err
			}
			m.cheatFile = src
			m.cheatStat = statCheatFile(live)
			m.cheatMissing = false
			return nil
		}
	}
	f := &cheats.File{Path: live}
	if err := f.Save(); err != nil {
		return err
	}
	m.cheatFile = f
	m.cheatStat = statCheatFile(live)
	m.cheatMissing = false
	return nil
}

// ensureCheatsOn rewrites the file with every cheat enabled when needed,
// so the whole list is available at once without per-cheat choices.
func (m *Model) ensureCheatsOn() {
	if m.cheatFile == nil || len(m.cheatFile.Cheats) == 0 {
		return
	}
	if err := m.cheatFile.Save(); err != nil {
		m.status = i18n.T("status.save_fail", err)
		return
	}
	m.cheatStat = statCheatFile(m.cheatPath)
}

// modCacheStat is a cheap fingerprint of the mod sources for revalidation:
// top-dir stats only, no directory walks. Every cursor move revalidates,
// so this must stay O(1) — walking the trees here froze browsing.
// The ROM dir is watched too, so a deployed (or hand-placed) file
// beside the ROM refreshes the [active] marks without a game switch.
// (A file added in a nested subfolder only changes that subfolder's
// mtime and shows up on the next game switch; a file added at the top
// level is picked up at once.)
type modCacheStat struct {
	liveMod time.Time
	liveOk  bool
	pileMod time.Time
	pileOk  bool
	romMod  time.Time
	romOk   bool
}

// statModSources snapshots the live dir, the shared pile dir and the
// ROM dir holding the deployed copies.
func statModSources(live, pile, romDir string) modCacheStat {
	var s modCacheStat
	if strings.TrimSpace(live) != "" {
		if st, err := os.Stat(live); err == nil {
			s.liveMod, s.liveOk = st.ModTime(), true
		}
	}
	if strings.TrimSpace(pile) != "" {
		if st, err := os.Stat(pile); err == nil {
			s.pileMod, s.pileOk = st.ModTime(), true
		}
	}
	if strings.TrimSpace(romDir) != "" {
		if st, err := os.Stat(romDir); err == nil {
			s.romMod, s.romOk = st.ModTime(), true
		}
	}
	return s
}

// syncModCache collects the selected game's mod files, keeping the result
// while the inputs stay put. Sources are the recipe's mods dir (when it
// exists) plus fuzzy matches from the shared mods dir. For every pile
// file it also plans the exact deployed path RetroArch autopatches by
// and whether those bytes already sit in place.
func (m *Model) syncModCache() {
	g := m.selected()
	var r *emulate.Recipe
	if g != nil {
		r = m.gameRecipe(*g)
	}
	if g == nil || r == nil || !r.Mods.Supported {
		m.modKey, m.modLive = "", ""
		m.modFiles = nil
		m.modTarget, m.modActive, m.modBuilt, m.modCheck = nil, nil, nil, nil
		m.modStat = modCacheStat{}
		return
	}
	src := g.ID + "\x00" + r.Name + "\x00" + r.Mods.Path + "\x00" + m.modsDir
	romDir := filepath.Dir(g.Path)
	if src == m.modKey {
		if statModSources(m.modLive, m.modsDir, romDir) == m.modStat {
			return
		}
	}
	live, _ := m.resolveModPath(*g, *r)
	files := m.collectMods(*g, live)
	target := make(map[string]string, len(files))
	active := make(map[string]bool, len(files))
	built := make(map[string]bool, len(files))
	check := make(map[string]mods.Check, len(files))
	// Verification reads the ROM once per game switch and checks every
	// pile file against it, so a doomed patch is marked before deploy.
	var romBytes []byte
	if len(files) > 0 {
		romBytes, _ = mods.ReadROMBytes(g.Path, g.Inner)
	}
	for _, f := range files {
		dst, ok := mods.PlanDeploy(f.Path, g.Path, g.Inner, f.Ext)
		target[f.Path] = dst
		active[f.Path] = ok
		if romBytes != nil {
			check[f.Path] = mods.VerifyPatch(f.Path, romBytes)
		}
		if dir := strings.TrimSpace(m.patchedDir); dir != "" {
			_, built[f.Path] = mods.InstanceStatus(dir, g.ID, f.Path)
		}
	}
	m.modKey, m.modLive, m.modFiles = src, live, files
	m.modTarget, m.modActive, m.modBuilt, m.modCheck = target, active, built, check
	m.modStat = statModSources(live, m.modsDir, romDir)
}

// resolveModPath expands the recipe's mods template. A template pointing
// at an existing dir is the live dir; anything else (a missing path, a
// file path, an empty template) is kept as the intended display path.
func (m Model) resolveModPath(g rom.Game, r emulate.Recipe) (live string, searched bool) {
	tmpl, _ := r.Expand(r.Mods.Path, g)
	tmpl = strings.TrimSpace(tmpl)
	if tmpl != "" {
		if st, err := os.Stat(tmpl); err == nil && st.IsDir() {
			return tmpl, false
		}
		if strings.TrimSpace(m.modsDir) == "" {
			return tmpl, false
		}
		return tmpl, true
	}
	if dir := strings.TrimSpace(m.modsDir); dir != "" {
		return "", true
	}
	return "", false
}

// collectMods merges the live dir content with fuzzy pile matches,
// deduplicated by path.
func (m Model) collectMods(g rom.Game, live string) []mods.Mod {
	seen := map[string]bool{}
	var out []mods.Mod
	if live != "" {
		if st, err := os.Stat(live); err == nil && st.IsDir() {
			if files, err := mods.ListDir(live); err == nil {
				for _, f := range files {
					if !seen[f.Path] {
						seen[f.Path] = true
						out = append(out, f)
					}
				}
			}
		}
	}
	if dir := strings.TrimSpace(m.modsDir); dir != "" {
		if hits, err := mods.Match(dir, modCandidates(g)...); err == nil {
			for _, f := range hits {
				if !seen[f.Path] {
					seen[f.Path] = true
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// modCandidates orders file stems to match a game by: inner/ROM base
// name, title, short id, full id (same order as cheats).
func modCandidates(g rom.Game) []string {
	return cheatCandidates(g)
}

// modsRows builds the mods summary row plus one row per patch file while
// expanded. Each file shows its pile name and the exact deployed name
// RetroArch autopatches by (game stem + ext), plus an [active] mark when
// those bytes already sit beside the ROM. Enter deploys, d removes.
func (m Model) modsRows(g rom.Game, r emulate.Recipe, vw int) []metaRow {
	base := featText(r.Mods.Supported, r.Mods.Path, g, &r)
	if !r.Mods.Supported {
		return []metaRow{{kind: "mods", label: i18n.T("meta.mods"), lines: wrapSoft(base, vw), copy: base}}
	}
	if len(m.modFiles) == 0 {
		base += " (" + i18n.T("meta.mods.nofile") + ")"
	} else {
		base += fmt.Sprintf(" (%d)", len(m.modFiles))
	}
	rows := []metaRow{{kind: "mods", label: i18n.T("meta.mods"), lines: wrapSoft(base, vw), copy: base}}
	if m.modsOpen {
		for i, f := range m.modFiles {
			text := f.Name + " → " + filepath.Base(m.modTarget[f.Path])
			if m.modActive[f.Path] {
				text += " [" + i18n.T("meta.mods.active") + "]"
			}
			if m.modBuilt[f.Path] {
				text += " [" + i18n.T("meta.mods.built") + "]"
			}
			if chk, ok := m.modCheck[f.Path]; ok {
				switch chk.Kind {
				case "base":
					text += " [" + i18n.T("meta.mods.badbase") + "]"
				case "broken":
					text += " [" + i18n.T("meta.mods.badfile") + "]"
				}
			}
			lines := wrapSoft(text, vw)
			if d := modCheckDetail(g, m.modCheck[f.Path]); d != "" {
				lines = append(lines, wrapSoft(d, vw)...)
			}
			rows = append(rows, metaRow{kind: "mod", lines: lines, copy: f.Path, cheat: i})
		}
	}
	return rows
}

// modCheckDetail renders the second row line for a failed verification:
// the technical mismatch plus an SMC-header hint on SNES ±512 bytes.
func modCheckDetail(g rom.Game, chk mods.Check) string {
	switch chk.Kind {
	case "broken":
		return chk.Note
	case "base":
		s := chk.Note
		if g.ConsoleID == "snes" && chk.HasCRC {
			if d := chk.WantSize - chk.GotSize; d == 512 || d == -512 {
				s += " · " + i18n.T("meta.mods.header")
			}
		}
		return s
	default:
		return ""
	}
}

// altLabel names the active alternative among all matches: plain name when
// alone, otherwise "name (i/n)" so switching stays discoverable.
func altLabel(name string, alts []*emulate.Recipe) string {
	if len(alts) < 2 {
		return name
	}
	for i, r := range alts {
		if r.Name == name {
			return fmt.Sprintf("%s (%d/%d)", name, i+1, len(alts))
		}
	}
	return name
}

// renderMetaRows renders rows within viewport width and reports the visual
// starting line of every row for cursor following. Values are soft-wrapped
// (never hard-truncated with "…"); the cursor row is highlighted instead.
func (m Model) renderMetaRows() (string, []int) {
	rows := m.metaRows
	if len(rows) == 0 {
		return infoStyle.Render(i18n.T("meta.none")), nil
	}
	w := max(m.meta.Width, 20)
	var b strings.Builder
	starts := make([]int, len(rows))
	n := 0
	padLabel := func(label string) string {
		if pad := metaKeyWidth - lipgloss.Width(label); pad > 0 {
			return label + strings.Repeat(" ", pad)
		}
		return label + " "
	}
	for i, r := range rows {
		starts[i] = n
		if r.sep {
			b.WriteString("\n")
			n++
			continue
		}
		if i == m.metaCur && m.focus == PaneMeta {
			for j, ln := range r.lines {
				var plain string
				if j == 0 && r.label != "" {
					plain = fit("  "+padLabel(r.label)+truncate(ln, max(w-2-metaKeyWidth-1, 1)), w)
				} else if j == 0 {
					plain = fit("  "+truncate(ln, w-2), w)
				} else {
					plain = fit("    "+truncate(ln, w-4), w)
				}
				b.WriteString(styledBarRow(plain) + "\n")
				n++
			}
			continue
		}
		switch r.kind {
		case "title":
			for _, ln := range r.lines {
				b.WriteString(metaVal.Render(ln) + "\n")
				n++
			}
		case "id":
			b.WriteString(metaKey.Render(r.label) + metaID.Render(r.lines[0]) + "\n")
			n++
		case "hash":
			for _, ln := range r.lines {
				b.WriteString(dim("    "+ln) + "\n")
				n++
			}
		default:
			for j, ln := range r.lines {
				if j == 0 {
					b.WriteString(metaKey.Render(r.label) + metaVal.Render(ln) + "\n")
				} else {
					b.WriteString(strings.Repeat(" ", metaKeyWidth) + metaVal.Render(ln) + "\n")
				}
				n++
			}
		}
	}
	return b.String(), starts
}

// featText renders a feature row: yes/no plus an expanded path or note.
func featText(supported bool, extra string, g rom.Game, r *emulate.Recipe) string {
	s := i18n.T("feat.no")
	if supported {
		s = i18n.T("feat.yes")
	}
	if extra != "" {
		if expanded, err := r.Expand(extra, g); err == nil {
			extra = expanded
		}
		s += " - " + extra
	}
	return s
}

func dim(s string) string { return infoStyle.Render(s) }

// renderHelp is the bubbles full-help strip right above the bottom bar.
func (m Model) renderHelp() string {
	if !m.showHelp {
		return ""
	}
	return m.help.FullHelpView(m.keys.FullHelp())
}

func (m Model) renderInfo() string {
	w := m.contentW()
	// Full bar with shrinking text budgets while it overflows.
	romB, titleB, statusB := 40, 32, 40
	for {
		left, right := m.barHalves(romB, titleB, statusB)
		if lipgloss.Width(left)+lipgloss.Width(right)+1 <= w {
			return barJoin(w, left, right)
		}
		if romB <= 8 && titleB <= 8 && statusB <= 8 {
			break
		}
		romB, titleB, statusB = max(romB/2, 8), max(titleB/2, 8), max(statusB/2, 8)
	}

	// Compact bar for narrow screens: the middle stats are dropped,
	// the context is truncated to whatever is left.
	brand := brandBadge.Render(" cartridge ")
	helpTxt := i18n.T("bar.help")
	ctxBudget := w - lipgloss.Width(brand) - lipgloss.Width(helpTxt) - 1
	var right string
	if ctxBudget > 4 {
		if m.status != "" {
			right = barStyle.Render(truncate(m.status, ctxBudget))
		} else if g := m.selected(); g != nil {
			sid := g.ShortID()
			if tb := ctxBudget - lipgloss.Width(sid) - 3 - 2; tb >= 4 {
				right = barStyle.Render(sid+" • ") + titleBadge.Render(" "+truncate(g.Title, tb)+" ")
			} else if ctxBudget >= lipgloss.Width(sid) {
				right = barStyle.Render(truncate(sid, ctxBudget))
			}
		}
	}
	help := helpBadge.Render(helpTxt)
	if lipgloss.Width(brand)+1+lipgloss.Width(right)+lipgloss.Width(help) > w {
		// Extreme: even the help badge must shrink to fit.
		help = helpBadge.Render(truncate(helpTxt, max(w-lipgloss.Width(brand)-1-lipgloss.Width(right), 0)))
	}
	return barJoin(w, brand, right+help)
}

// barHalves builds the styled bar halves with shrinking text budgets:
// brand + stats on the left, context + help hint on the right.
func (m Model) barHalves(romB, titleB, statusB int) (string, string) {
	// Весь бар залит серым (barStyle): бейджи остаются цветными модулями,
	// а текст и пустое пространство между ними — на сером фоне.
	left := brandBadge.Render(" cartridge ") +
		barStyle.Render("  "+i18n.T("bar.stats",
			len(m.games), pluralWord(len(m.games), "word.rom"),
			len(m.rows)-1, pluralWord(len(m.rows)-1, "word.console"),
			truncate(m.romDir, romB)))

	var context string
	if m.status != "" {
		context = barStyle.Render(truncate(m.status, statusB))
	} else if g := m.selected(); g != nil {
		context = barStyle.Render(g.ShortID()+" • ") +
			titleBadge.Render(" "+truncate(g.Title, titleB)+" ")
	}
	right := context + helpBadge.Render(i18n.T("bar.help"))
	return left, right
}

// barJoin glues the halves with a grey gap, exactly w cells wide.
func barJoin(w int, left, right string) string {
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	bar := left + barStyle.Render(strings.Repeat(" ", gap)) + right
	// Добиваем бар серым до полной ширины, чтобы не осталось незакрашенных хвостов.
	if pad := w - lipgloss.Width(bar); pad > 0 {
		bar += barStyle.Render(strings.Repeat(" ", pad))
	}
	return bar
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
