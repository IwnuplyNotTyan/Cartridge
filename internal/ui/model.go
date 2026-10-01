// Package ui implements the 3-column cartridge browser:
// consoles | games | metadata, plus a bottom bar.
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cartridge/internal/rom"
)

// Pane is the focused column.
type Pane int

const (
	PaneConsoles Pane = iota
	PaneGames
	PaneMeta
)

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

	focus Pane
	meta  viewport.Model

	width  int
	height int
	ready  bool

	romDir  string
	libPath string
	status  string
}

// New builds the model from an initial scan.
func New(games []rom.Game, romDir, libPath string) Model {
	m := Model{romDir: romDir, libPath: libPath, meta: viewport.New(0, 0)}
	m.setGames(games)
	return m
}

func (m *Model) setGames(games []rom.Game) {
	m.games = games
	counts := map[string]int{}
	names := map[string]string{}
	shorts := map[string]string{}
	for _, g := range games {
		counts[g.ConsoleID]++
		names[g.ConsoleID] = g.ConsoleName
	}
	for _, c := range rom.Consoles {
		if _, ok := names[c.ID]; ok {
			shorts[c.ID] = c.Short
		}
	}
	m.rows = []ConsoleRow{{ID: "all", Name: "All consoles", Short: "★", Count: len(games)}}
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
	if m.focus == PaneMeta {
		var cmd tea.Cmd
		m.meta, cmd = m.meta.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab", "l", "right":
		// l/right move focus right… but j/k-style game cursors live
		// on the games pane, so only treat l as focus when not typing.
		if k == "l" && m.focus == PaneGames {
			// still move focus: games -> meta is the natural direction
		}
		m.focus = (m.focus + 1) % 3
		return m, nil
	case "shift+tab", "h", "left":
		m.focus = (m.focus + 2) % 3
		return m, nil
	case "1":
		m.focus = PaneConsoles
		return m, nil
	case "2":
		m.focus = PaneGames
		return m, nil
	case "3":
		m.focus = PaneMeta
		return m, nil
	case "r":
		m.status = "scanning " + m.romDir + " …"
		return m, m.rescanCmd()
	case "g":
		m.jumpTop()
		return m, nil
	case "G":
		m.jumpBottom()
		return m, nil
	}

	switch m.focus {
	case PaneConsoles:
		switch k {
		case "up", "k":
			if m.consCur > 0 {
				m.consCur--
				m.gameCur = 0
				m.refilter()
			}
		case "down", "j":
			if m.consCur < len(m.rows)-1 {
				m.consCur++
				m.gameCur = 0
				m.refilter()
			}
		case "enter", " ":
			m.focus = PaneGames
		}
	case PaneGames:
		switch k {
		case "up", "k":
			if m.gameCur > 0 {
				m.gameCur--
				m.refreshMeta()
			}
		case "down", "j":
			if m.gameCur < len(m.filtered)-1 {
				m.gameCur++
				m.refreshMeta()
			}
		case "pgup":
			m.gameCur = max(m.gameCur-10, 0)
			m.refreshMeta()
		case "pgdown":
			m.gameCur = min(m.gameCur+10, len(m.filtered)-1)
			m.refreshMeta()
		}
	case PaneMeta:
		switch k {
		case "up", "k":
			m.meta.LineUp(1)
		case "down", "j":
			m.meta.LineDown(1)
		case "pgup", " ":
			m.meta.HalfViewUp()
		case "pgdown", "enter":
			m.meta.HalfViewDown()
		}
	}
	return m, nil
}

func (m *Model) jumpTop() {
	switch m.focus {
	case PaneConsoles:
		m.consCur = 0
		m.gameCur = 0
		m.refilter()
	case PaneGames:
		m.gameCur = 0
		m.refreshMeta()
	case PaneMeta:
		m.meta.GotoTop()
	}
}

func (m *Model) jumpBottom() {
	switch m.focus {
	case PaneConsoles:
		m.consCur = len(m.rows) - 1
		m.gameCur = 0
		m.refilter()
	case PaneGames:
		m.gameCur = max(len(m.filtered)-1, 0)
		m.refreshMeta()
	case PaneMeta:
		m.meta.GotoBottom()
	}
}

func (m Model) rescanCmd() tea.Cmd {
	dir := m.romDir
	return func() tea.Msg {
		games, errs := rom.ScanDir(dir)
		return rescanMsg{games: games, errs: errs}
	}
}

func (m *Model) sizeMeta() {
	_, _, mw, mh := m.layout()
	m.meta.Width = max(mw-4, 10)
	m.meta.Height = max(mh-4, 3)
}

// layout splits the screen: header, three columns, bottom bar.
func (m Model) layout() (cw, gw, mw, mh int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 120
	}
	if h <= 0 {
		h = 30
	}
	bodyH := max(h-5, 8) // header(2) + footer(2) + margins
	cw = clamp(w*22/100, 20, 30)
	mw = clamp(w*36/100, 28, 60)
	gw = max(w-cw-mw-6, 20)
	mh = bodyH
	return cw, gw, mw, mh
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "cartridge: waiting for terminal size…"
	}
	cw, gw, mw, mh := m.layout()

	left := paneStyle(m.focus == PaneConsoles, cw, mh).Render(m.renderConsoles(mh))
	mid := paneStyle(m.focus == PaneGames, gw, mh).Render(m.renderGames(mh))
	right := paneStyle(m.focus == PaneMeta, mw, mh).Render(m.renderMetaPane(mh))

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, mid, right)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		body,
		m.renderBar(),
	)
}

func (m Model) renderHeader() string {
	total := len(m.games)
	sel := m.selected()
	right := fmt.Sprintf("%d ROMs • %s", total, truncate(m.romDir, 48))
	if sel != nil {
		right = fmt.Sprintf("%s • %s", sel.ShortID(), right)
	}
	left := headerStyle.Render("▛ CARTRIDGE") + headerDim.Render("  retro rom shelf")
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	return left + strings.Repeat(" ", gap) + headerDim.Render(right)
}

func (m Model) renderConsoles(paneH int) string {
	var b strings.Builder
	b.WriteString(paneTitle.Render(fmt.Sprintf("CONSOLES (%d)", len(m.rows))) + "\n")
	visible := max(paneH-3, 1)
	start := clamp(m.consCur-visible/2, 0, max(len(m.rows)-visible, 0))
	for i := start; i < min(start+visible, len(m.rows)); i++ {
		r := m.rows[i]
		line := fmt.Sprintf("%-4s %s", r.Short, truncate(r.Name, 40))
		line = fmt.Sprintf("%-*s %3d", 24, truncate(line, 24), r.Count)
		if i == m.consCur {
			mark := "> "
			if m.focus == PaneConsoles {
				b.WriteString(curStyle.Render(mark) + selStyle.Render(line) + "\n")
			} else {
				b.WriteString(curStyle.Render(mark) + selStyle.Render(line) + "\n")
			}
		} else {
			b.WriteString("  " + lipgloss.NewStyle().Foreground(brightTx).Render(line) + "\n")
		}
	}
	return b.String()
}

func (m Model) renderGames(paneH int) string {
	var b strings.Builder
	title := "GAMES"
	if m.consCur < len(m.rows) {
		title = fmt.Sprintf("GAMES — %s (%d)", m.rows[m.consCur].Short, len(m.filtered))
	}
	b.WriteString(paneTitle.Render(truncate(title, 60)) + "\n")
	if len(m.filtered) == 0 {
		b.WriteString(headerDim.Render("  (empty — press r to rescan)") + "\n")
		return b.String()
	}
	visible := max(paneH-3, 1)
	innerW := max(m.width-m.width*22/100-m.width*36/100-14, 20)
	start := clamp(m.gameCur-visible/2, 0, max(len(m.filtered)-visible, 0))
	for i := start; i < min(start+visible, len(m.filtered)); i++ {
		g := m.filtered[i]
		line := fmt.Sprintf("%s  %s", truncate(g.Title, innerW-14), headerDim.Render(g.ShortID()))
		if i == m.gameCur {
			b.WriteString(curStyle.Render("> ") + selStyle.Render(truncate(g.Title, innerW-4)) + "\n")
		} else {
			b.WriteString("  " + lipgloss.NewStyle().Foreground(brightTx).Render(line) + "\n")
		}
	}
	return b.String()
}

func (m Model) renderMetaPane(paneH int) string {
	var b strings.Builder
	b.WriteString(paneTitle.Render("METADATA") + "\n")
	b.WriteString(m.meta.View())
	_ = paneH
	return b.String()
}

func (m Model) renderMeta() string {
	g := m.selected()
	if g == nil {
		return headerDim.Render("No game selected.")
	}
	var b strings.Builder
	kv := func(k, v string) {
		b.WriteString(metaKey.Render(k) + metaVal.Render(truncate(v, 120)) + "\n")
	}
	b.WriteString(metaVal.Render(g.Title) + "\n\n")
	b.WriteString(metaKey.Render("id") + metaID.Render(g.ID) + "\n")
	kv("short", g.ShortID())
	kv("console", fmt.Sprintf("%s (%s)", g.ConsoleName, g.ConsoleID))
	if g.Region != "" {
		kv("region", g.Region)
	}
	if len(g.Tags) > 0 {
		kv("tags", strings.Join(g.Tags, ", "))
	}
	kv("size", fmt.Sprintf("%d bytes (%.2f MiB)", g.Size, float64(g.Size)/(1<<20)))
	kv("sha256", g.SHA256)
	b.WriteString("\n")
	kv("file", g.Path)
	if g.Inner != "" {
		kv("inner", g.Inner)
	}
	return b.String()
}

func (m Model) renderBar() string {
	hints := fmt.Sprintf(" %s focus %s consoles/games/meta %s ↑↓/jk move %s r rescan %s q quit ",
		keyStyle.Render("tab"), headerDim.Render("1/2/3"),
		keyStyle.Render(""), keyStyle.Render(""), keyStyle.Render(""))
	status := truncate(m.status, 60)
	if status == "" {
		if g := m.selected(); g != nil {
			status = fmt.Sprintf("%s • %s", g.ConsoleID, g.Title)
		}
	}
	w := max(m.width-2, 10)
	space := max(w-lipgloss.Width(hints)-lipgloss.Width(status)-2, 1)
	line := hints + strings.Repeat(" ", space) + headerDim.Render(status)
	return barStyle.Render(line)
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
