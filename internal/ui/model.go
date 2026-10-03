// Package ui implements the 3-column cartridge browser:
// consoles | games | metadata, separated by grey dividers,
// with bubbles full help above the bottom info bar on "?".
package ui

import (
	"fmt"
	"os"
	"os/exec"
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

	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
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
		Rescan:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", i18n.T("key.rescan"))),
		Launch:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", i18n.T("key.launch"))),
		Settings: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", i18n.T("key.settings"))),
		Copy:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", i18n.T("key.copy"))),
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
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Prev, k.Next, k.Select, k.Launch},
		{k.PageUp, k.PageDown, k.Rescan, k.Settings},
		{k.Copy, k.Alt, k.Help, k.Quit},
	}
}

// ConsoleRow is one entry of the left column.
type ConsoleRow struct {
	ID    string
	Name  string
	Short string
	Count int
}

// metaRow is one navigable metadata row: copy holds the clipboard text,
// kind is "recipe" or "core" on switchable rows, "sep" on blank separators.
type metaRow struct {
	kind  string
	label string
	lines []string
	copy  string
	sep   bool
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

	focus    Pane
	meta     viewport.Model
	metaCur  int       // cursor over metaRows (separators skipped)
	metaRows []metaRow // navigable metadata rows for copying/switching
	keys     keyMap
	help     help.Model
	showHelp bool

	width  int
	height int
	ready  bool

	romDir  string
	libPath string
	status  string

	recipes []emulate.Recipe
	// overrides persists per-game emulator/core picks from metadata (n/p).
	overrides *overrides.Store
	play      *playtime.Store

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
	savesDir    string
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

// WithBaseRecipes keeps the unpatched base recipes for the settings view.
func WithBaseRecipes(r []emulate.Recipe) Option {
	return func(m *Model) { m.baseRecipes = r }
}

// WithPatches sets recipe overlay patches (never rewrite the base file).
func WithPatches(p []emulate.RecipePatch) Option {
	return func(m *Model) { m.patches = p }
}

// WithPaths sets config file location, recipes/patches paths and data dirs.
func WithPaths(cfgFile, recipesPath, patchesPath, modsDir, savesDir string) Option {
	return func(m *Model) {
		m.cfgFile = cfgFile
		m.recipesPath = recipesPath
		m.patchesPath = patchesPath
		m.modsDir = modsDir
		m.savesDir = savesDir
	}
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
	return m
}

func (m *Model) setGames(games []rom.Game) {
	m.games = games
	counts := map[string]int{}
	names := map[string]string{}
	for _, g := range games {
		counts[g.ConsoleID]++
		names[g.ConsoleID] = g.ConsoleName
	}
	m.rows = []ConsoleRow{{ID: "all", Name: i18n.T("row.all"), Count: len(games)}}
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

func (m *Model) refilter() {
	id := "all"
	if m.consCur < len(m.rows) {
		id = m.rows[m.consCur].ID
	}
	m.filtered = m.filtered[:0]
	for _, g := range m.games {
		if id == "all" || g.ConsoleID == id {
			m.filtered = append(m.filtered, g)
		}
	}
	if m.gameCur >= len(m.filtered) {
		m.gameCur = max(len(m.filtered)-1, 0)
	}
	m.metaCur = 0
	m.refreshMeta()
}

func (m Model) selected() *rom.Game {
	if len(m.filtered) == 0 || m.gameCur >= len(m.filtered) {
		return nil
	}
	return &m.filtered[m.gameCur]
}

func (m *Model) refreshMeta() {
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

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if m.mode == ModeSettings {
		return m.updateSettings(msg)
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
	case key.Matches(msg, k.Settings):
		m.openSettings()
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
		m.focus = (m.focus + 1) % 3
		return m, nil
	case key.Matches(msg, k.Prev):
		m.focus = (m.focus + 2) % 3
		return m, nil
	}
	switch msg.String() {
	case "1":
		m.focus = PaneConsoles
		return m, nil
	case "2":
		m.focus = PaneGames
		return m, nil
	case "3":
		m.focus = PaneMeta
		return m, nil
	case "esc":
		m.focus = PaneGames
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
			m.focus = PaneGames
		}
	case PaneGames:
		switch {
		case key.Matches(msg, k.Up):
			if m.gameCur > 0 {
				m.gameCur--
				m.refreshMeta()
			}
		case key.Matches(msg, k.Down):
			if m.gameCur < len(m.filtered)-1 {
				m.gameCur++
				m.refreshMeta()
			}
		case key.Matches(msg, k.PageUp):
			m.gameCur = max(m.gameCur-10, 0)
			m.refreshMeta()
		case key.Matches(msg, k.PageDown):
			m.gameCur = min(m.gameCur+10, max(len(m.filtered)-1, 0))
			m.refreshMeta()
		case key.Matches(msg, k.Top):
			m.gameCur = 0
			m.refreshMeta()
		case key.Matches(msg, k.Bottom):
			m.gameCur = max(len(m.filtered)-1, 0)
			m.refreshMeta()
		case key.Matches(msg, k.Rescan):
			m.status = i18n.T("status.scanning", m.romDir)
			return m, m.rescanCmd()
		case key.Matches(msg, k.Select):
			m.focus = PaneMeta
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
		case key.Matches(msg, k.Copy), key.Matches(msg, k.Select):
			return m.metaCopy()
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
	return func() tea.Msg {
		games, errs := rom.ScanDir(dir)
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
	return max(m.contentH()-1-m.helpHeight(), 5) // single info-bar line, no slack
}

// layout splits the content area into three column inner widths (no borders,
// columns are separated by divider lines).
func (m Model) layout() (cw, gw, mw int) {
	w := m.contentW()
	cw = clamp(w*30/100, 30, 40)
	mw = clamp(w*30/100, 28, 44)
	gw = max(w-cw-mw-2-4, 20) // 2 dividers + padding
	return cw, gw, mw
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
		left := m.renderConsoles(cw)
		mid := m.renderGames(gw)
		right := m.renderMetaPane(mw)
		sep := sepStyle.Render(strings.TrimSuffix(strings.Repeat("│\n", m.bodyH()), "\n"))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, sep, mid, sep, right)
	}
	var content string
	if m.showHelp {
		content = lipgloss.JoinVertical(lipgloss.Left,
			body,
			m.renderHelp(),
			m.renderInfo(),
		)
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left,
			body,
			m.renderInfo(),
		)
	}
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
	if m.consCur < len(m.rows) && m.rows[m.consCur].ID != "all" {
		title = i18n.T("games.title.filter", m.rows[m.consCur].Short, len(m.filtered))
	}
	lines := []string{titleFor(truncate(title, w), m.focus == PaneGames, w)}
	if len(m.filtered) == 0 {
		lines = append(lines, infoStyle.Render(fit(i18n.T("games.empty"), w)))
		return m.column(lines, w, 1, 1)
	}
	visible := max(m.bodyH()-1, 1)
	start := clamp(m.gameCur-visible/2, 0, max(len(m.filtered)-visible, 0))
	for i := start; i < min(start+visible, len(m.filtered)); i++ {
		plain := fit("  "+truncate(m.filtered[i].Title, w-2), w)
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
		rows = append(rows, kv(i18n.T("meta.cheats"), featText(r.Cheats.Supported, r.Cheats.Path, *g, r)))
		rows = append(rows, kv(i18n.T("meta.achievements"), featText(r.Achievements.Supported, r.Achievements.Note, *g, r)))
		rows = append(rows, kv(i18n.T("meta.mods"), featText(r.Mods.Supported, r.Mods.Path, *g, r)))
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
		s += " — " + extra
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
	// Весь бар залит серым (barStyle): бейджи остаются цветными модулями,
	// а текст и пустое пространство между ними — на сером фоне.
	left := brandBadge.Render(" cartridge ") +
		barStyle.Render("  "+i18n.T("bar.stats",
			len(m.games), pluralWord(len(m.games), "word.rom"),
			len(m.rows)-1, pluralWord(len(m.rows)-1, "word.console"),
			truncate(m.romDir, 40)))

	var context string
	if m.status != "" {
		context = barStyle.Render(truncate(m.status, 40))
	} else if g := m.selected(); g != nil {
		context = barStyle.Render(g.ShortID()+" • ") +
			titleBadge.Render(" "+truncate(g.Title, 32)+" ")
	}
	right := context
	right += helpBadge.Render(i18n.T("bar.help"))

	gap := max(m.contentW()-lipgloss.Width(left)-lipgloss.Width(right), 1)
	gapStr := barStyle.Render(strings.Repeat(" ", gap))
	bar := left + gapStr + right
	// Добиваем бар серым до полной ширины, чтобы не осталось незакрашенных хвостов.
	if pad := m.contentW() - lipgloss.Width(bar); pad > 0 {
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
