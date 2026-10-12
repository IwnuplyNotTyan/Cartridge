package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/config"
	"cartridge/internal/i18n"
	"cartridge/internal/icons"
)

// artTestModel builds a browser model whose Steam icons dir is dir (empty:
// no cached art). The first sample game (aaa111) is selected.
func artTestModel(t *testing.T, dir string) Model {
	t.Helper()
	i18n.Set(i18n.En)
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = dir
	m := New(sampleGames(), "/x", "/tmp/lib.json", WithSteam(cfg))
	m = sizedModel(t, m)
	m.focus = PaneMeta
	return m
}

// writeArtFile drops a fake cached PNG at the shard path for gameID+kind.
func writeArtFile(t *testing.T, dir, gameID string, kind icons.ArtKind) string {
	t.Helper()
	var rel string
	switch kind {
	case icons.ArtHero:
		rel = icons.ShardPathHero(gameID)
	case icons.ArtLogo:
		rel = icons.ShardPathLogo(gameID)
	default:
		rel = icons.ShardPath(gameID)
	}
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600)...)
	if err := os.WriteFile(p, png, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// openArtsList expands the artwork section like a user would.
func openArtsList(t *testing.T, m Model) Model {
	t.Helper()
	m = metaCursorTo(t, m, "arts")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if !m.artOpen {
		t.Fatalf("enter must expand artwork")
	}
	return m
}

func TestMetaArtRowsHiddenWithoutCache(t *testing.T) {
	m := artTestModel(t, t.TempDir())
	for _, r := range m.metaRows {
		if !r.sep && (r.kind == "arts" || r.kind == "art") {
			t.Fatalf("no cache must hide art rows, got %+v", r)
		}
	}
}

func TestMetaArtSummaryCollapsedByDefault(t *testing.T) {
	dir := t.TempDir()
	writeArtFile(t, dir, "aaa111", icons.ArtIcon)
	writeArtFile(t, dir, "aaa111", icons.ArtHero)
	writeArtFile(t, dir, "aaa111", icons.ArtLogo)
	m := artTestModel(t, dir)
	if got := metaRowText(t, m, "arts"); !strings.Contains(got, "(3)") {
		t.Fatalf("summary must show count, got %q", got)
	}
	for _, r := range m.metaRows {
		if !r.sep && r.kind == "art" {
			t.Fatalf("files must stay hidden until expanded, got %+v", r)
		}
	}
	m = openArtsList(t, m)
	var labels []string
	for _, r := range m.metaRows {
		if !r.sep && r.kind == "art" {
			labels = append(labels, r.label)
		}
	}
	for _, want := range []string{"icon", "hero", "logo"} {
		found := false
		for _, l := range labels {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expanded list must show %q, got %q", want, labels)
		}
	}
	// Esc collapses the list.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.artOpen {
		t.Fatalf("esc must collapse artwork")
	}
}

func TestMetaArtEnterHandsToOpener(t *testing.T) {
	dir := t.TempDir()
	hero := writeArtFile(t, dir, "aaa111", icons.ArtHero)
	m := artTestModel(t, dir)
	m = openArtsList(t, m)
	m = metaCursorTo(t, m, "art")
	// Enter must not spawn anything itself: it returns the opener
	// command (tea.ExecProcess suspends the TUI around xdg-open), which
	// tests never execute headless.
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("enter must return the opener command")
	}
	if !strings.Contains(m.status, "opening") {
		t.Fatalf("status = %q, want opening hint", m.status)
	}
	_ = hero
}

func TestMetaArtOpenedMsg(t *testing.T) {
	m := artTestModel(t, t.TempDir())
	out, _ := m.Update(artOpenedMsg{label: "hero"})
	if got := out.(Model).status; !strings.Contains(got, "opened hero") {
		t.Fatalf("status = %q, want opened hint", got)
	}
	out, _ = m.Update(artOpenedMsg{label: "hero", err: errTestBoom})
	if got := out.(Model).status; !strings.Contains(got, "open failed") {
		t.Fatalf("status = %q, want failure hint", got)
	}
}

func TestMetaArtEnterMissingFile(t *testing.T) {
	dir := t.TempDir()
	writeArtFile(t, dir, "aaa111", icons.ArtHero)
	m := artTestModel(t, dir)
	m = openArtsList(t, m)
	m = metaCursorTo(t, m, "art")
	// The cache is pruned under us after the rows were built.
	for _, r := range m.metaRows {
		if !r.sep && r.kind == "art" {
			if err := os.Remove(r.copy); err != nil {
				t.Fatal(err)
			}
		}
	}
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.status == "" || !strings.Contains(m.status, "gone") {
		t.Fatalf("status = %q, want missing hint", m.status)
	}
}

func TestMetaArtHiddenWhenOff(t *testing.T) {
	dir := t.TempDir()
	writeArtFile(t, dir, "aaa111", icons.ArtIcon)
	i18n.Set(i18n.En)
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = dir
	cfg.IconsProvider = "off"
	m := sizedModel(t, New(sampleGames(), "/x", "/tmp/lib.json", WithSteam(cfg)))
	for _, r := range m.metaRows {
		if !r.sep && (r.kind == "arts" || r.kind == "art") {
			t.Fatalf("provider off must hide art rows, got %+v", r)
		}
	}
}

func TestMetaArtClosesOnGameSwitch(t *testing.T) {
	dir := t.TempDir()
	writeArtFile(t, dir, "aaa111", icons.ArtIcon)
	writeArtFile(t, dir, "bbb222", icons.ArtIcon)
	m := artTestModel(t, dir)
	m = openArtsList(t, m)
	m.focus = PaneGames
	out, _ := m.Update(keyMsg("down")) // Battletoads -> Kirby
	m = out.(Model)
	if m.artOpen {
		t.Fatalf("game switch must close the list")
	}
}
