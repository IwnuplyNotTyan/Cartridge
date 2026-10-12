package ui

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/i18n"
	"cartridge/internal/rom"
)

// Background game runs: launching a game no longer suspends the TUI.
// The emulator starts detached with piped output; its logs stream into
// a log screen (esc returns to the browser, the game keeps running in
// the background, playtime is recorded when the process exits).

// maxRunLines caps the kept log tail so a chatty emulator never grows
// memory; maxRunLineBytes caps one scanner line (long JSON dumps, ...).
const (
	maxRunLines     = 500
	maxRunLineBytes = 1 << 20
)

// gameRun tracks one background emulator process and its log tail.
type gameRun struct {
	gameID  string
	title   string
	cmd     *exec.Cmd
	lines   []string
	done    bool
	started time.Time
}

// runLogMsg carries one emulator output line into the model.
type runLogMsg struct{ line string }

// waitRunLog listens for the next log line; nil ends the stream.
func waitRunLog(ch chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return runLogMsg{line: line}
	}
}

// ansiRe strips SGR/color sequences: emulators love colors, but ANSI
// breaks width math (truncate/fit) and smears the log screen.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// cleanRunLine makes one raw output line safe for the log screen:
// no ANSI, no carriage returns (progress bars), tabs expanded, other
// control bytes dropped. Leading spaces survive (log indentation).
func cleanRunLine(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' {
			return -1
		}
		return r
	}, s)
}

// startRun launches cmd in the background with piped output and opens
// the log screen. Everything side-effectful happens inside the returned
// command (never synchronously), so tests stay process-free. Only one
// run at a time: a second launch reports busy instead of stacking
// emulator instances.
func (m Model) startRun(game rom.Game, cmd *exec.Cmd) (tea.Model, tea.Cmd) {
	if m.run != nil && !m.run.done {
		return m, m.setStatus(i18n.T("status.run.busy", m.run.title))
	}
	ch := make(chan string, 256)
	m.runCh = ch
	m.run = &gameRun{gameID: game.ID, title: game.Title, cmd: cmd, started: time.Now()}
	m.runView = true
	m.runScroll = 0
	m.followRun = true
	starter := func() tea.Msg {
		if err := runToChannel(cmd, ch); err != nil {
			return launchDoneMsg{gameID: game.ID, dur: time.Since(m.run.started), err: err}
		}
		return launchDoneMsg{gameID: game.ID, dur: time.Since(m.run.started)}
	}
	return m, tea.Batch(starter, waitRunLog(ch), m.setStatus(i18n.T("status.launching", game.Title)))
}

// runToChannel starts cmd with piped stdout+stderr, forwards every line
// to ch and waits. Stdin is /dev/null: the TUI keeps the real terminal,
// a background emulator must not steal its keystrokes.
func runToChannel(cmd *exec.Cmd, ch chan string) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		close(ch)
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		close(ch)
		return err
	}
	if null, nerr := os.Open(os.DevNull); nerr == nil {
		cmd.Stdin = null
		defer null.Close()
	}
	if err := cmd.Start(); err != nil {
		close(ch)
		return err
	}
	var wg sync.WaitGroup
	pump := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), maxRunLineBytes)
		for sc.Scan() {
			ch <- sc.Text()
		}
	}
	wg.Add(2)
	go pump(stdout)
	go pump(stderr)
	wg.Wait()
	err = cmd.Wait()
	close(ch)
	return err
}

// appendRunLine grows the tail, dropping the head past the cap.
func (m *Model) appendRunLine(line string) {
	if m.run == nil {
		return
	}
	m.run.lines = append(m.run.lines, cleanRunLine(line))
	if len(m.run.lines) > maxRunLines {
		m.run.lines = m.run.lines[len(m.run.lines)-maxRunLines:]
	}
	if m.followRun {
		m.runScroll = m.runMaxScroll()
	}
}

// runMaxScroll is the deepest first-visible-line offset for the current
// terminal height (title + hint occupy two rows of the body).
func (m Model) runMaxScroll() int {
	if m.run == nil {
		return 0
	}
	return max(len(m.run.lines)-max(m.bodyH()-2, 1), 0)
}

// killRun stops the background emulator, best effort (used on quit so
// no orphan keeps running after the TUI is gone).
func (m *Model) killRun() {
	if m.run == nil || m.run.done || m.run.cmd == nil || m.run.cmd.Process == nil {
		return
	}
	_ = m.run.cmd.Process.Kill()
}

// updateRunView handles keys on the log screen: esc returns to the
// browser (the game keeps running), arrows page the tail. Manual
// scrolling leaves follow mode; scrolling back to the bottom resumes
// it. Everything else is ignored so game-list keys never fire here.
func (m Model) updateRunView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.runView = false
		return m, nil
	}
	step := max(m.bodyH()-2, 1)
	switch {
	case key.Matches(msg, m.keys.Up):
		m.followRun = false
		m.runScroll = max(m.runScroll-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.runScroll++
		if m.runScroll >= m.runMaxScroll() {
			m.runScroll = m.runMaxScroll()
			m.followRun = true
		}
	case key.Matches(msg, m.keys.PageUp):
		m.followRun = false
		m.runScroll = max(m.runScroll-step, 0)
	case key.Matches(msg, m.keys.PageDown):
		m.runScroll += step
		if m.runScroll >= m.runMaxScroll() {
			m.runScroll = m.runMaxScroll()
			m.followRun = true
		}
	case key.Matches(msg, m.keys.Top):
		m.followRun = false
		m.runScroll = 0
	case key.Matches(msg, m.keys.Bottom):
		m.runScroll = m.runMaxScroll()
		m.followRun = true
	}
	return m, nil
}

// renderRun draws the log screen: game title + state, the scrollable
// tail, a key hint. Plain lines only (ingest already stripped ANSI),
// every row fitted to the width like everywhere else.
func (m Model) renderRun() string {
	w := m.contentW()
	// Column padding (1 left + 1 right) is added below, so rows are
	// fitted to w-2 like the pane columns do via layout().
	tw := max(w-2, 10)
	title, state := "", i18n.T("run.exited")
	if m.run != nil {
		title = m.run.title
		if !m.run.done {
			state = i18n.T("run.running")
		}
	}
	lines := []string{titleFor(truncate("▶ "+title+" • "+state, tw), true, tw)}
	body := max(m.bodyH()-2, 1)
	start := 0
	if m.run != nil {
		start = clamp(m.runScroll, 0, m.runMaxScroll())
		for _, ln := range m.run.lines[start:min(start+body, len(m.run.lines))] {
			lines = append(lines, fit(truncate(ln, tw), tw))
		}
	}
	lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("run.hint"), tw), tw)))
	return m.column(lines, w, 1, 1)
}
