// Package ui implements the 3-column cartridge browser:
// consoles | games | metadata, separated by grey dividers,
// with bubbles full help above the bottom info bar on "?".
package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	lipglossv2 "charm.land/lipgloss/v2"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/charmtone"

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
	Help     key.Binding
	Quit     key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
		Prev:     key.NewBinding(key.WithKeys("shift+tab", "h", "left"), key.WithHelp("←/h", "pane left")),
		Next:     key.NewBinding(key.WithKeys("tab", "l", "right"), key.WithHelp("tab", "pane right")),
		Top:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "first")),
		Bottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "last")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Select:   key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "open")),
		Rescan:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rescan")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap (unused: only full help is shown).
func (k keyMap) ShortHelp() []key.Binding { return []key.Binding{k.Help, k.Quit} }

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Prev, k.Next, k.Select},
		{k.PageUp, k.PageDown, k.Rescan},
		{k.Help, k.Quit},
	}
}

// ConsoleRow is one entry of the left column.
type ConsoleRow struct {
	ID    string
	Name  string
	Short string
	Count int
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
	keys     keyMap
	help     help.Model
	showHelp bool

	width  int
	height int
	ready  bool

	romDir  string
	libPath string
	status  string
}

// New builds the model from an initial scan.
func New(games []rom.Game, romDir, libPath string) Model {
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
	m.rows = []ConsoleRow{{ID: "all", Name: "All consoles", Count: len(games)}}
	for _, c := range rom.Consoles {
		if n, ok := counts[c.ID]; ok {
			m.rows = append(m.rows, ConsoleRow{ID: c.ID, Name: c.Name, Short: c.Short, Count: n})
		}
	}
	// Keep unknown consoles (if any) at the end instead of dropping them.
	if n, ok := counts["unknown"]; ok {
		m.rows = append(m.rows, ConsoleRow{ID: "unknown", Name: "Unknown", Short: "???", Count: n})
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
	m.refreshMeta()
}

func (m Model) selected() *rom.Game {
	if len(m.filtered) == 0 || m.gameCur >= len(m.filtered) {
		return nil
	}
	return &m.filtered[m.gameCur]
}

func (m *Model) refreshMeta() {
	m.meta.GotoTop()
	m.meta.SetContent(m.renderMeta())
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
			m.status = fmt.Sprintf("%d ROMs, %d errors", len(msg.games), len(msg.errs))
		} else {
			m.status = fmt.Sprintf("rescan: %d ROMs", len(msg.games))
		}
		m.sizeMeta()
		m.refreshMeta()
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		m.sizeMeta()
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
			m.status = "scanning " + m.romDir + " …"
			return m, m.rescanCmd()
		case key.Matches(msg, k.Select):
			m.focus = PaneMeta
		}
	case PaneMeta:
		switch {
		case key.Matches(msg, k.Up):
			m.meta.LineUp(1)
		case key.Matches(msg, k.Down):
			m.meta.LineDown(1)
		case key.Matches(msg, k.PageUp):
			m.meta.HalfViewUp()
		case key.Matches(msg, k.PageDown), key.Matches(msg, k.Select):
			m.meta.HalfViewDown()
		case key.Matches(msg, k.Top):
			m.meta.GotoTop()
		case key.Matches(msg, k.Bottom):
			m.meta.GotoBottom()
		case key.Matches(msg, k.Rescan):
			m.status = "scanning " + m.romDir + " …"
			return m, m.rescanCmd()
		}
	}

	// Rescan is global: allow it from the consoles pane too.
	if key.Matches(msg, k.Rescan) {
		m.status = "scanning " + m.romDir + " …"
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
func (m Model) View() string {
	if !m.ready {
		return "cartridge: waiting for terminal size…"
	}
	cw, gw, mw := m.layout()
	left := m.renderConsoles(cw)
	mid := m.renderGames(gw)
	right := m.renderMetaPane(mw)
	sep := sepStyle.Render(strings.TrimSuffix(strings.Repeat("│\n", m.bodyH()), "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, mid, sep, right)
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

func (m Model) renderConsoles(w int) string {
	lines := []string{titleFor("CONSOLES", m.focus == PaneConsoles, w)}
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
	title := fmt.Sprintf("GAMES (%d)", len(m.filtered))
	if m.consCur < len(m.rows) && m.rows[m.consCur].ID != "all" {
		title = fmt.Sprintf("GAMES — %s (%d)", m.rows[m.consCur].Short, len(m.filtered))
	}
	lines := []string{titleFor(truncate(title, w), m.focus == PaneGames, w)}
	if len(m.filtered) == 0 {
		lines = append(lines, infoStyle.Render(fit("empty — press r", w)))
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
	lines := []string{titleFor("METADATA", m.focus == PaneMeta, w)}
	for _, ln := range strings.Split(m.meta.View(), "\n") {
		lines = append(lines, ln)
	}
	return m.column(lines, w, 1, 0)
}

// renderMeta builds the metadata text, every line within viewport width.
// Values are soft-wrapped (never hard-truncated with "…"), so the viewport
// scroll reveals the full text instead of losing it to truncation.
func (m Model) renderMeta() string {
	g := m.selected()
	if g == nil {
		return infoStyle.Render("No game selected.")
	}
	w := max(m.meta.Width, 20)
	var b strings.Builder
	kv := func(k, v string) {
		for i, ln := range wrapSoft(v, max(w-9, 10)) {
			if i == 0 {
				b.WriteString(metaKey.Render(k) + metaVal.Render(ln) + "\n")
			} else {
				b.WriteString(strings.Repeat(" ", 9) + metaVal.Render(ln) + "\n")
			}
		}
	}
	for _, ln := range wrapSoft(g.Title, w) {
		b.WriteString(metaVal.Render(ln) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(metaKey.Render("id") + metaID.Render(g.ShortID()) + "\n")
	for _, ln := range wrapSoft(g.ID, max(w-4, 10)) {
		b.WriteString(dim("    "+ln) + "\n")
	}
	kv("console", fmt.Sprintf("%s (%s)", g.ConsoleName, g.ConsoleID))
	if g.Region != "" {
		kv("region", g.Region)
	}
	if len(g.Tags) > 0 {
		kv("tags", strings.Join(g.Tags, ", "))
	}
	kv("size", fmt.Sprintf("%d bytes (%.2f MiB)", g.Size, float64(g.Size)/(1<<20)))
	b.WriteString("\n")
	kv("file", g.Path)
	if g.Inner != "" {
		kv("inner", g.Inner)
	}
	return b.String()
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
	left := brandBadge.Render(" cartridge ") +
		infoStyle.Render(fmt.Sprintf("  %d ROMs • %d consoles • %s",
			len(m.games), len(m.rows)-1, truncate(m.romDir, 40)))

	var context string
	if m.status != "" {
		context = infoStyle.Render(truncate(m.status, 40))
	} else if g := m.selected(); g != nil {
		context = infoStyle.Render(g.ShortID()) + infoStyle.Render(" • ") +
			titleBadge.Render(" "+truncate(g.Title, 32)+" ")
	}
	right := context
	if right != "" {
		right += " "
	}
	right += helpBadge.Render(" ? Help ")

	gap := max(m.contentW()-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
	return left + strings.Repeat(" ", gap) + right
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
