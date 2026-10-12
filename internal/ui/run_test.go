package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
	"cartridge/internal/playtime"
)

// runTestModel builds a browser model with a working nes recipe.
// Nothing is ever executed: startRun defers the spawn into the
// returned command, which tests never run.
func runTestModel(t *testing.T) Model {
	t.Helper()
	i18n.Set(i18n.En)
	recipes := []emulate.Recipe{{
		Name: "test-nes", Consoles: []string{"nes"},
		Emulator: "true", Args: []string{"{rom}"},
	}}
	play, err := playtime.Load(filepath.Join(t.TempDir(), "playtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(sampleGames(), "/x", "/tmp/lib.json",
		WithRecipes(recipes), WithPlaytime(play))
	return sizedModel(t, m)
}

func TestLaunchOpensLogView(t *testing.T) {
	m := runTestModel(t)
	out, cmd := m.Update(keyMsg("x"))
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("launch must return a command")
	}
	if !m.runView || m.run == nil || m.runCh == nil {
		t.Fatalf("launch must open the log view: %+v", m.run)
	}
	if m.run.done {
		t.Fatalf("fresh run must not be done")
	}
	if !strings.Contains(m.status, "starting Battletoads") {
		t.Fatalf("status = %q", m.status)
	}
	if view := m.View(); !strings.Contains(view, "Battletoads") || !strings.Contains(view, "running") {
		t.Fatalf("log screen must show title + state\n%s", view)
	}
}

func TestLaunchSecondRunBusy(t *testing.T) {
	m := runTestModel(t)
	out, _ := m.Update(keyMsg("x"))
	m = out.(Model)
	first := m.run
	out, _ = m.Update(keyMsg("x"))
	m = out.(Model)
	if m.run != first {
		t.Fatalf("second launch must not replace the run")
	}
	if !strings.Contains(m.status, "already running") {
		t.Fatalf("status = %q, want busy hint", m.status)
	}
}

func TestRunLogStreamsAndCaps(t *testing.T) {
	m := runTestModel(t)
	out, _ := m.Update(keyMsg("x"))
	m = out.(Model)
	feed := func(n int, s string) Model {
		for i := 0; i < n; i++ {
			out, _ := m.Update(runLogMsg{line: s})
			m = out.(Model)
		}
		return m
	}
	m = feed(3, "hello")
	if len(m.run.lines) != 3 {
		t.Fatalf("lines = %d", len(m.run.lines))
	}
	// ANSI colors and carriage returns never reach the screen.
	m = feed(1, "\x1b[32mgreen\x1b[0m\rprogress")
	if got := m.run.lines[len(m.run.lines)-1]; got != "greenprogress" {
		t.Fatalf("line not cleaned: %q", got)
	}
	// The tail is capped, follow sticks to the bottom.
	m = feed(maxRunLines+10, "filler")
	if len(m.run.lines) != maxRunLines {
		t.Fatalf("tail must cap at %d, got %d", maxRunLines, len(m.run.lines))
	}
	if m.runScroll != m.runMaxScroll() {
		t.Fatalf("follow must stick to bottom: %d vs %d", m.runScroll, m.runMaxScroll())
	}
	if view := m.View(); !strings.Contains(view, "filler") {
		t.Fatalf("bottom must be visible\n%s", view)
	}
}

func TestRunLogDoneSealsTail(t *testing.T) {
	m := runTestModel(t)
	out, _ := m.Update(keyMsg("x"))
	m = out.(Model)
	out, _ = m.Update(runLogMsg{line: "libretro: ready"})
	m = out.(Model)
	out, _ = m.Update(launchDoneMsg{gameID: "aaa111", dur: 90 * time.Second})
	m = out.(Model)
	if m.run == nil || !m.run.done {
		t.Fatalf("run must be marked done")
	}
	if m.runCh != nil {
		t.Fatalf("line waiter must stop")
	}
	if got := m.run.lines[len(m.run.lines)-1]; !strings.Contains(got, "exited after 1 min 30 s") {
		t.Fatalf("tail must seal with exit line, got %q", got)
	}
	if !strings.Contains(m.status, "played 1 min 30 s") {
		t.Fatalf("playtime status = %q", m.status)
	}
	// Esc leaves the log screen, the sealed tail stays for later.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if out.(Model).runView {
		t.Fatalf("esc must leave the log screen")
	}
}

func TestRunViewScroll(t *testing.T) {
	m := runTestModel(t)
	out, _ := m.Update(keyMsg("x"))
	m = out.(Model)
	for i := 0; i < 60; i++ {
		out, _ = m.Update(runLogMsg{line: "line"})
		m = out.(Model)
	}
	if !m.followRun {
		t.Fatalf("must follow while untouched")
	}
	out, _ = m.Update(keyMsg("pgup"))
	m = out.(Model)
	if m.followRun || m.runScroll >= m.runMaxScroll() {
		t.Fatalf("pgup must leave follow: scroll=%d max=%d", m.runScroll, m.runMaxScroll())
	}
	out, _ = m.Update(keyMsg("G"))
	m = out.(Model)
	if !m.followRun || m.runScroll != m.runMaxScroll() {
		t.Fatalf("G must resume follow: scroll=%d max=%d", m.runScroll, m.runMaxScroll())
	}
	// Game-list keys must not fire on the log screen.
	cur := m.gameCur
	out, _ = m.Update(keyMsg("down"))
	m = out.(Model)
	if m.gameCur != cur {
		t.Fatalf("down must scroll logs, not games")
	}
}

func TestRunViewOverflow(t *testing.T) {
	m := runTestModel(t)
	out, _ := m.Update(keyMsg("x"))
	m = out.(Model)
	out, _ = m.Update(runLogMsg{line: strings.Repeat("abcdef", 40) + "\x1b[31mred\x1b[0m"})
	m = out.(Model)
	for _, w := range []int{120, 70, 40, 24} {
		mm := resizeModel(t, m, w)
		for i, ln := range strings.Split(mm.View(), "\n") {
			if got := lipgloss.Width(ln); got > w {
				t.Fatalf("w=%d line %d overflows: %d\n%s", w, i, got, mm.View())
			}
		}
	}
}

func TestCleanRunLine(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[32mgreen\x1b[0m": "green",
		"a\rb":                 "ab",
		"a\tb":                 "a    b",
		"a\x07b":               "ab",
		"  indented":           "  indented",
	} {
		if got := cleanRunLine(in); got != want {
			t.Fatalf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKillRunSafe(t *testing.T) {
	m := runTestModel(t)
	m.killRun() // no run: must not panic
	m.run = &gameRun{gameID: "x", done: true}
	m.killRun() // finished run: must not panic
}

// TestRunToChannelEndToEnd exercises the real spawn machinery: the test
// binary re-execs itself as the child (portable, no shell), prints to
// both streams and exits 3. The parent must see both lines merged and
// the non-zero exit.
func TestRunToChannelEndToEnd(t *testing.T) {
	if os.Getenv("CARTRIDGE_TEST_CHILD") == "1" {
		fmt.Fprintln(os.Stdout, "child-out")
		fmt.Fprintln(os.Stderr, "child-err")
		os.Exit(3)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("no executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestRunToChannelEndToEnd$")
	cmd.Env = append(os.Environ(), "CARTRIDGE_TEST_CHILD=1")
	ch := make(chan string, 256)
	runErr := runToChannel(cmd, ch)
	var lines []string
	for l := range ch {
		lines = append(lines, l)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"child-out", "child-err"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("child output missing %q: %q", want, joined)
		}
	}
	exitErr, ok := runErr.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 3 {
		t.Fatalf("exit must propagate as code 3, got %v", runErr)
	}
}
