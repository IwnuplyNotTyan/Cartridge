package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Transient status bar: every message fades after statusTTL and the bar
// falls back to the selected game (renderInfo already prefers m.status
// and shows the game when it is empty). setStatus stamps the message and
// returns its expiry timer; timers carry the token captured at set time,
// so a newer message always wins over an older timer.
const statusTTL = 5 * time.Second

// clearStatusMsg clears an expired bar message.
type clearStatusMsg struct{ token int }

// setStatus stamps a transient bar message, returning its expiry timer.
// It fits every site shape:
//
//	m.status = s; return m, nil  →  return m, m.setStatus(s)
//	m.status = s; return m, cmd  →  return m, tea.Batch(cmd, m.setStatus(s))
func (m *Model) setStatus(s string) tea.Cmd {
	m.statusToken++
	m.status = s
	token := m.statusToken
	return tea.Tick(statusTTL, func(time.Time) tea.Msg {
		return clearStatusMsg{token: token}
	})
}

// clearStatus drops the bar message now and invalidates pending timers.
func (m *Model) clearStatus() {
	m.statusToken++
	m.status = ""
}
