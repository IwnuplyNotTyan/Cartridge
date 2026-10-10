package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cartridge/internal/i18n"
	"cartridge/internal/library"
	"cartridge/internal/rom"
)

// TUI rescan (r) is authoritative: the shelf reflects exactly what is
// on disk — deleted ROMs are pruned — and the result is persisted, so
// the view and library.json never diverge until the next restart.
func TestRescanPrunesAndSaves(t *testing.T) {
	i18n.Set(i18n.En)
	dir := t.TempDir()
	lib := filepath.Join(dir, "library.json")

	deleted := rom.Game{ID: "gone", Title: "Gone", ConsoleID: "nes",
		ConsoleName: "Nintendo Entertainment System", Path: "/gone/gone.nes", SHA256: "gone"}
	scanned := rom.Game{ID: "new", Title: "New", ConsoleID: "nes",
		ConsoleName: "Nintendo Entertainment System", Path: "/disk/new.nes", SHA256: "new"}

	m := New([]rom.Game{deleted}, "/disk", lib)
	out, _ := m.Update(rescanMsg{games: []rom.Game{scanned}})
	m = out.(Model)

	// The deleted console must disappear from the view at once.
	if len(m.games) != 1 || m.games[0].ID != "new" {
		t.Fatalf("rescan must prune deleted games, got %+v", m.games)
	}
	for _, r := range m.rows {
		if r.ID == "nes" && r.Count != 1 {
			t.Fatalf("console count must follow the prune: %+v", m.rows)
		}
	}

	// The library file must be rewritten with the same pruned set.
	saved, err := library.Load(lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].ID != "new" {
		t.Fatalf("rescan must save the pruned library, got %+v", saved)
	}
}

// A failed scan (errors plus an empty result while we had games, e.g.
// an unmounted drive) must not wipe the stored collection.
func TestRescanFailureKeepsStored(t *testing.T) {
	i18n.Set(i18n.En)
	dir := t.TempDir()
	lib := filepath.Join(dir, "library.json")

	stored := rom.Game{ID: "keep", Title: "Kept", ConsoleID: "nes",
		ConsoleName: "Nintendo Entertainment System", Path: "/disk/kept.nes", SHA256: "keep"}
	if err := library.Save(lib, []rom.Game{stored}); err != nil {
		t.Fatal(err)
	}

	m := New([]rom.Game{stored}, "/disk", lib)
	out, _ := m.Update(rescanMsg{errs: []error{errors.New("disk gone")}})
	m = out.(Model)

	if len(m.games) != 1 || m.games[0].ID != "keep" {
		t.Fatalf("failed rescan must keep stored games, got %+v", m.games)
	}
	saved, err := library.Load(lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].ID != "keep" {
		t.Fatalf("failed rescan must not rewrite the library, got %+v", saved)
	}
}

// End-to-end on a real dir: deleting a ROM file and rescanning must
// drop its console from the shelf and persist the prune.
func TestRescanRealDirPrunesDeleted(t *testing.T) {
	i18n.Set(i18n.En)
	romDir := t.TempDir()
	lib := filepath.Join(t.TempDir(), "library.json")
	payload := append([]byte{'N', 'E', 'S', 0x1A}, make([]byte, 256)...)

	nesPath := filepath.Join(romDir, "game.nes")
	if err := os.WriteFile(nesPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	cold, errs := rom.ScanDir(romDir, nil)
	if len(errs) != 0 || len(cold) != 1 {
		t.Fatalf("cold scan = %+v, %v", cold, errs)
	}
	if err := library.Save(lib, cold); err != nil {
		t.Fatal(err)
	}
	m := New(cold, romDir, lib)

	// Delete the ROM from disk, rescan through the real rescanCmd path.
	if err := os.Remove(nesPath); err != nil {
		t.Fatal(err)
	}
	cmd := m.rescanCmd()
	out, _ := m.Update(cmd())
	m = out.(Model)

	if len(m.games) != 0 {
		t.Fatalf("deleted ROM must vanish from the shelf, got %+v", m.games)
	}
	for _, r := range m.rows {
		if r.ID == "nes" {
			t.Fatalf("deleted console must vanish from the rows: %+v", m.rows)
		}
	}
	saved, err := library.Load(lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 0 {
		t.Fatalf("deleted ROM must be pruned from the file, got %+v", saved)
	}
}

// Console counts must describe the visible (valid) games only:
// stale mod/junk rows are filtered from the shelf, so they must not
// inflate the All counter or add phantom console rows.
func TestSetGamesCountsFiltered(t *testing.T) {
	i18n.Set(i18n.En)
	games := append(sampleGames(),
		rom.Game{ID: "ghost", Title: "hack", ConsoleID: "unknown", ConsoleName: "Unknown", Path: "/x/hack.ips"},
	)
	m := New(games, "/x", filepath.Join(t.TempDir(), "lib.json"))
	if len(m.games) != len(sampleGames()) {
		t.Fatalf("invalid rows must be filtered, got %+v", m.games)
	}
	if m.rows[0].Count != len(sampleGames()) {
		t.Fatalf("All count = %d, want %d (no phantoms)", m.rows[0].Count, len(sampleGames()))
	}
	for _, r := range m.rows {
		if r.ID == "unknown" {
			t.Fatalf("filtered junk must not add a console row: %+v", m.rows)
		}
	}
}
