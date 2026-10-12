package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/i18n"
)

func iconsModel(t *testing.T) Model {
	t.Helper()
	m := openSettings(t, settingsModel(t))
	m.settTab = TabIcons
	m.settCur = 0
	// Isolate the icon cache: temp dir, no network in these tests.
	m.steam.IconsDir = t.TempDir()
	m.iconsStatFresh = false
	return m
}

func TestIconsTabVisible(t *testing.T) {
	i18n.Set(i18n.En)
	m := iconsModel(t)
	view := m.View()
	for _, want := range []string{"Icons", "Source", "SteamGridDB key", "Cache dir", "Fetch missing icons", "Prune orphaned"} {
		if !strings.Contains(view, want) {
			t.Fatalf("icons tab must show %q\n%s", want, view)
		}
	}
}
func TestIconsStatsShowHeroes(t *testing.T) {
	m := iconsModel(t)
	if len(m.games) == 0 {
		t.Fatalf("need sample games")
	}
	g := m.games[0]
	mk := func(rel string) {
		p := filepath.Join(m.steam.IconsDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600)...)
		if err := os.WriteFile(p, png, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk(g.ID[:2] + "/" + g.ID[2:4] + "/" + g.ID + ".png")
	mk(g.ID[:2] + "/" + g.ID[2:4] + "/" + g.ID + "_hero.png")
	m.refreshIconsStat()
	if m.iconsCached != 1 || m.iconsHeroes != 1 {
		t.Fatalf("cached=%d heroes=%d", m.iconsCached, m.iconsHeroes)
	}
	if view := m.View(); !strings.Contains(view, "1 icons + 1 heroes") {
		t.Fatalf("stats must show heroes\n%s", view)
	}
}

func TestIconsTabSwitchKeys(t *testing.T) {
	m := openSettings(t, settingsModel(t))
	m.settTab = TabSteam
	out, _ := m.Update(keyMsg("5"))
	mm := out.(Model)
	if mm.settTab != TabIcons {
		t.Fatalf("5 must switch to icons, got %d", mm.settTab)
	}
	if !mm.iconsStatFresh {
		t.Fatalf("entering icons tab must refresh stats")
	}
	// tab from Steam lands on Icons too.
	m.settTab = TabSteam
	out, _ = m.Update(keyMsg("tab"))
	mm = out.(Model)
	if mm.settTab != TabIcons {
		t.Fatalf("tab from steam must land on icons, got %d", mm.settTab)
	}
}

func TestIconsProviderCycles(t *testing.T) {
	m := iconsModel(t)
	m.steam.IconsProvider = "auto"
	m.settCur = iconsRowProvider
	want := []string{"libretro", "steamgriddb", "off", "auto"}
	for _, w := range want {
		out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = out.(Model)
		if m.steam.IconsProvider != w {
			t.Fatalf("provider = %q, want %q", m.steam.IconsProvider, w)
		}
	}
	// Persisted to the config file.
	data, err := os.ReadFile(m.cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"icons_provider": "auto"`) {
		t.Fatalf("config must persist provider: %s", data)
	}
	// Space cycles too.
	out, _ := m.Update(keyMsg(" "))
	m = out.(Model)
	if m.steam.IconsProvider != "libretro" {
		t.Fatalf("space must cycle provider, got %q", m.steam.IconsProvider)
	}
}

func TestIconsKeyEditMasked(t *testing.T) {
	m := iconsModel(t)
	m.settCur = iconsRowKey
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if !m.settEditing {
		t.Fatalf("enter on key row must start editing")
	}
	m.settBuf = []rune("secret-key-123")
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.steam.SteamGridDBKey != "secret-key-123" {
		t.Fatalf("key = %q", m.steam.SteamGridDBKey)
	}
	view := m.View()
	if strings.Contains(view, "secret-key-123") {
		t.Fatalf("key must be masked on screen\n%s", view)
	}
	if !strings.Contains(view, "secr…") {
		t.Fatalf("masked key must show prefix\n%s", view)
	}
}

func TestIconsPruneRemovesOrphans(t *testing.T) {	m := iconsModel(t)
	// Seed one orphaned file.
	orphan := filepath.Join(m.steam.IconsDir, "aa", "aa", "deadbeef0001.png")
	mk := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600)...)
		if err := os.WriteFile(p, png, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk(orphan)
	m.settCur = iconsRowPrune
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan must be pruned")
	}
	if !strings.Contains(m.status, "pruned") {
		t.Fatalf("status must report prune, got %q", m.status)
	}
}

func TestIconsProgressMsgs(t *testing.T) {
	m := iconsModel(t)
	m.iconsActive = true
	m.iconsCh = make(chan iconsProgMsg)
	out, _ := m.Update(iconsProgMsg{done: 7, total: 20})
	mm := out.(Model)
	if mm.iconsDone != 7 || mm.iconsTotal != 20 {
		t.Fatalf("progress not tracked: %+v", mm)
	}
	if !strings.Contains(mm.status, "7/20") {
		t.Fatalf("status must show progress, got %q", mm.status)
	}
	// Listener re-armed: feed it and finish (channel captured locally:
	// closing over mm races with the reassignment below).
	ch := mm.iconsCh
	go func() { ch <- iconsProgMsg{done: 20, total: 20} }()
	_ = mm
	out, _ = m.Update(iconsDoneMsg{fetched: 12, failed: 1, applied: 10, profiles: 2})
	mm = out.(Model)
	if mm.iconsActive {
		t.Fatalf("done must clear active flag")
	}
	if !strings.Contains(mm.status, "12") {
		t.Fatalf("status must summarize, got %q", mm.status)
	}
}

func TestIconsSyncGuards(t *testing.T) {
	// Provider off refuses to start.
	m := iconsModel(t)
	m.steam.IconsProvider = "off"
	m.settCur = iconsRowSync
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := out.(Model)
	if mm.iconsActive {
		t.Fatalf("provider off must not start sync")
	}
	// SteamGridDB without a key refuses too.
	m = iconsModel(t)
	m.steam.IconsProvider = "steamgriddb"
	m.steam.SteamGridDBKey = ""
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm = out.(Model)
	if mm.iconsActive {
		t.Fatalf("steamgriddb without key must not start sync")
	}
}
