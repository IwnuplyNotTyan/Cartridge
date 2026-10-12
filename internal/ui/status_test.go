package ui

import (
	"strings"
	"testing"

	"cartridge/internal/i18n"
)

// TestStatusExpiry pins the transient bar: a newer message wins over an
// older timer, the matching timer clears back to the selected game.
func TestStatusExpiry(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.setStatus("first")
	m.setStatus("second")
	if m.status != "second" {
		t.Fatalf("status = %q", m.status)
	}
	// Stale timer (token 1) must not wipe the newer message.
	out, _ := m.Update(clearStatusMsg{token: 1})
	m = out.(Model)
	if m.status != "second" {
		t.Fatalf("stale timer cleared the message: %q", m.status)
	}
	// Matching timer clears; the bar falls back to the hovered game.
	out, _ = m.Update(clearStatusMsg{token: 2})
	m = out.(Model)
	if m.status != "" {
		t.Fatalf("status = %q, want empty", m.status)
	}
	if view := m.View(); !strings.Contains(view, "Battletoads") {
		t.Fatalf("cleared bar must show the game\n%s", view)
	}
}

// TestStatusCopyExpiresEndToEnd checks a real action round-trips through
// the timer: copy sets the message, its own token clears it.
func TestStatusCopyExpiresEndToEnd(t *testing.T) {
	i18n.Set(i18n.En)
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json"))
	m.focus = PaneMeta
	m = metaCursorTo(t, m, "id")
	out, _ := m.Update(keyMsg("c"))
	m = out.(Model)
	if !strings.Contains(m.status, "copied id") {
		t.Fatalf("status = %q", m.status)
	}
	token := m.statusToken
	out, _ = m.Update(clearStatusMsg{token: token})
	if out.(Model).status != "" {
		t.Fatalf("copy message must expire")
	}
}
