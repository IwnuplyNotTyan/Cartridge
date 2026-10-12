package steam

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"cartridge/internal/config"
	"cartridge/internal/rom"
)

func testRomGame(t *testing.T, id string) rom.Game {
	t.Helper()
	// Real file: sync skips library ghosts missing from disk.
	f, err := os.CreateTemp(t.TempDir(), "rom-*.gba")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return rom.Game{ID: id, Title: "Zelda", ConsoleID: "gba", ConsoleName: "Game Boy Advance", Path: f.Name()}
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Minimal PNG header + padding so Hit() size checks pass.
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600)...)
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildShortcutUsesCachedIcon(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = dir
	g := rom.Game{ID: "abc123", Title: "Zelda", ConsoleID: "gba", ConsoleName: "Game Boy Advance"}
	s := BuildShortcut(g, cfg, "/usr/bin/cartridge", "/usr/bin")
	if s.Icon != "" {
		t.Fatalf("no cache yet, icon = %q", s.Icon)
	}
	writePNG(t, filepath.Join(dir, "ab", "c1", "abc123.png"))
	s = BuildShortcut(g, cfg, "/usr/bin/cartridge", "/usr/bin")
	if s.Icon == "" {
		t.Fatalf("cached icon not attached")
	}
}

func TestSyncDeploysGridArt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	ud := filepath.Join(home, ".local", "share", "Steam", "userdata", "9", "config")
	if err := os.MkdirAll(ud, 0o755); err != nil {
		t.Fatal(err)
	}
	iconsDir := t.TempDir()
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = iconsDir
	games := []rom.Game{
		testRomGame(t, "gameid0001"),
	}
	writePNG(t, filepath.Join(iconsDir, "ga", "me", "gameid0001.png"))
	res, err := SyncFile(filepath.Join(ud, "shortcuts.vdf"), games, cfg, "/usr/bin/cartridge", "/usr/bin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.IconsApplied != 1 {
		t.Fatalf("icons applied = %d", res.IconsApplied)
	}
	list, err := LoadFile(filepath.Join(ud, "shortcuts.vdf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Icon == "" {
		t.Fatalf("shortcut icon not saved: %+v", list)
	}
	// Grid files for the appid must exist.
	for _, n := range GridFileNames(list[0].AppID) {
		if _, err := os.Stat(filepath.Join(ud, "grid", n)); err != nil {
			t.Fatalf("grid %s missing: %v", n, err)
		}
	}
}

func TestGameStemPrefersInner(t *testing.T) {
	g := rom.Game{ID: "x", Path: "/games/pack.zip", Inner: "roms/ActRaiser (USA).sfc"}
	if got := gameStem(g); got != "ActRaiser (USA)" {
		t.Fatalf("stem = %q", got)
	}
	g2 := rom.Game{ID: "y", Path: "/games/Zelda (USA).nes"}
	if got := gameStem(g2); got != "Zelda (USA)" {
		t.Fatalf("stem = %q", got)
	}
}

func TestToIconGamesCarriesNaming(t *testing.T) {
	games := []rom.Game{{ID: "x", Title: "Zelda", ConsoleID: "nes", Region: "USA", Tags: []string{"!"}, Path: "/g/Zelda (USA).nes"}}
	ig := ToIconGames(games)
	if len(ig) != 1 || ig[0].Stem != "Zelda (USA)" || ig[0].Region != "USA" {
		t.Fatalf("icon game = %+v", ig)
	}
}

func TestProviderOffDisablesIcons(t *testing.T) {	dir := t.TempDir()
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = dir
	cfg.IconsProvider = "off"
	writePNG(t, filepath.Join(dir, "ab", "c1", "abc123.png"))
	g := rom.Game{ID: "abc123", Title: "Zelda", ConsoleID: "gba", ConsoleName: "Game Boy Advance"}
	if s := BuildShortcut(g, cfg, "/usr/bin/cartridge", "/usr/bin"); s.Icon != "" {
		t.Fatalf("provider off must skip icons, got %q", s.Icon)
	}
}

func TestSyncDeploysHero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	ud := filepath.Join(home, ".local", "share", "Steam", "userdata", "9", "config")
	if err := os.MkdirAll(ud, 0o755); err != nil {
		t.Fatal(err)
	}
	iconsDir := t.TempDir()
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = iconsDir
	games := []rom.Game{
		testRomGame(t, "gameid0001"),
	}
	writePNG(t, filepath.Join(iconsDir, "ga", "me", "gameid0001.png"))
	writePNG(t, filepath.Join(iconsDir, "ga", "me", "gameid0001_hero.png"))
	res, err := SyncFile(filepath.Join(ud, "shortcuts.vdf"), games, cfg, "/usr/bin/cartridge", "/usr/bin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.HeroWithArt != 1 {
		t.Fatalf("hero with art = %d", res.HeroWithArt)
	}
	if res.HeroDeployed != 1 {
		t.Fatalf("hero deployed = %d", res.HeroDeployed)
	}
	list, err := LoadFile(filepath.Join(ud, "shortcuts.vdf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("shortcuts = %d", len(list))
	}
	if _, err := os.Stat(filepath.Join(ud, "grid", GridHeroName(list[0].AppID))); err != nil {
		t.Fatalf("hero grid missing: %v", err)
	}
	// Steady state: re-sync copies nothing (mtime+size trust, no re-reads).
	res2, err := SyncFile(filepath.Join(ud, "shortcuts.vdf"), games, cfg, "/usr/bin/cartridge", "/usr/bin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res2.GridDeployed != 0 || res2.HeroDeployed != 0 {
		t.Fatalf("resync must copy nothing: %+v", res2)
	}
	if res2.IconsApplied != 1 || res2.HeroWithArt != 1 {
		t.Fatalf("resync must keep art counts: %+v", res2)
	}
}

func TestSyncDeploysWideCapsule(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	ud := filepath.Join(home, ".local", "share", "Steam", "userdata", "9", "config")
	if err := os.MkdirAll(ud, 0o755); err != nil {
		t.Fatal(err)
	}
	iconsDir := t.TempDir()
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = iconsDir
	games := []rom.Game{
		testRomGame(t, "gameid0001"),
	}
	// Distinct contents so slots are distinguishable.
	icon := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x11}, 600)...)
	wide := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x22}, 600)...)
	if err := os.MkdirAll(filepath.Join(iconsDir, "ga", "me"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(iconsDir, "ga", "me", "gameid0001.png"), icon, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(iconsDir, "ga", "me", "gameid0001_wide.png"), wide, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := SyncFile(filepath.Join(ud, "shortcuts.vdf"), games, cfg, "/usr/bin/cartridge", "/usr/bin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.WideWithArt != 1 || res.WideDeployed != 1 {
		t.Fatalf("wide = %+v", res)
	}
	list, err := LoadFile(filepath.Join(ud, "shortcuts.vdf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("shortcuts = %d", len(list))
	}
	// Wide slot holds the landscape capsule, portrait slot the icon.
	if got, err := os.ReadFile(filepath.Join(ud, "grid", GridWideName(list[0].AppID))); err != nil || !bytes.Equal(got, wide) {
		t.Fatalf("wide grid must hold the wide capsule: err=%v", err)
	}
	if got, err := os.ReadFile(filepath.Join(ud, "grid", GridPortraitName(list[0].AppID))); err != nil || !bytes.Equal(got, icon) {
		t.Fatalf("portrait grid must hold the icon: err=%v", err)
	}
	// Steady state: re-sync copies nothing.
	res2, err := SyncFile(filepath.Join(ud, "shortcuts.vdf"), games, cfg, "/usr/bin/cartridge", "/usr/bin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res2.WideDeployed != 0 || res2.GridDeployed != 0 {
		t.Fatalf("resync must copy nothing: %+v", res2)
	}
	if res2.WideWithArt != 1 {
		t.Fatalf("resync must keep wide counts: %+v", res2)
	}
}

func TestSyncWideFallsBackToIcon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	ud := filepath.Join(home, ".local", "share", "Steam", "userdata", "9", "config")
	if err := os.MkdirAll(ud, 0o755); err != nil {
		t.Fatal(err)
	}
	iconsDir := t.TempDir()
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = iconsDir
	games := []rom.Game{
		testRomGame(t, "gameid0001"),
	}
	// Icon only, no wide capsule cached.
	writePNG(t, filepath.Join(iconsDir, "ga", "me", "gameid0001.png"))
	res, err := SyncFile(filepath.Join(ud, "shortcuts.vdf"), games, cfg, "/usr/bin/cartridge", "/usr/bin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.WideWithArt != 1 {
		t.Fatalf("icon must back the wide slot: %+v", res)
	}
	list, err := LoadFile(filepath.Join(ud, "shortcuts.vdf"))
	if err != nil {
		t.Fatal(err)
	}
	icon, err := os.ReadFile(filepath.Join(ud, "grid", GridPortraitName(list[0].AppID)))
	if err != nil {
		t.Fatal(err)
	}
	wide, err := os.ReadFile(filepath.Join(ud, "grid", GridWideName(list[0].AppID)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(icon, wide) {
		t.Fatalf("without a cached wide the icon must fill both slots")
	}
}

func TestPruneGridArtCleansHeroes(t *testing.T) {
	dir := t.TempDir()
	grid := filepath.Join(dir, "grid")
	if err := os.MkdirAll(grid, 0o755); err != nil {
		t.Fatal(err)
	}
	touch := func(name string) {
		if err := os.WriteFile(filepath.Join(grid, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch("123.png")
	touch("123p.png")
	touch("123_hero.png")
	touch("999_hero.png")   // orphan hero: removed
	touch("999p.png")       // orphan capsule: removed (suffix parse fix)
	touch("real_game.png")  // foreign: kept
	if n := PruneGridArt(dir, map[uint32]bool{123: true}); n != 2 {
		t.Fatalf("pruned = %d, want 2", n)
	}
	for _, want := range []string{"123.png", "123p.png", "123_hero.png", "real_game.png"} {
		if _, err := os.Stat(filepath.Join(grid, want)); err != nil {
			t.Fatalf("%s must survive: %v", want, err)
		}
	}
}
