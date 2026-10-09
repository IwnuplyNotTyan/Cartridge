package steam

import (
	"os"
	"path/filepath"
	"testing"

	"cartridge/internal/config"
	"cartridge/internal/rom"
)

func testGame(id, title, consoleID, consoleName string) rom.Game {
	return rom.Game{ID: id, Title: title, ConsoleID: consoleID, ConsoleName: consoleName, Path: "/games/" + title + ".gba"}
}

func TestAppIDStable(t *testing.T) {
	a := AppIDFor("/usr/bin/cartridge", "Zelda (GBA)")
	b := AppIDFor("/usr/bin/cartridge", "Zelda (GBA)")
	if a != b {
		t.Fatalf("appid not stable: %d vs %d", a, b)
	}
	if a&0x80000000 == 0 {
		t.Fatalf("high bit not set: %x", a)
	}
	c := AppIDFor("/usr/bin/cartridge", "Mario (GBA)")
	if a == c {
		t.Fatalf("different names collide")
	}
}

func TestTagsDefault(t *testing.T) {
	cfg := config.DefaultSteamConfig()
	g := testGame("abc", "Zelda", "gba", "Game Boy Advance")
	tags := TagsFor(g, cfg)
	// Root "all games" category is off by default: only per-console.
	if len(tags) != 1 || tags[0] != "Cartridge - GBA" {
		t.Fatalf("tags = %q", tags)
	}
}

func TestTagsAllGames(t *testing.T) {
	cfg := config.DefaultSteamConfig()
	cfg.AllGames = true
	g := testGame("abc", "Zelda", "gba", "Game Boy Advance")
	tags := TagsFor(g, cfg)
	if len(tags) != 2 || tags[0] != "Cartridge" || tags[1] != "Cartridge - GBA" {
		t.Fatalf("tags = %q", tags)
	}
}

func TestTagsNoPrefix(t *testing.T) {
	cfg := config.SteamConfig{Prefix: "", CategoryTemplate: "{prefix} - {short}"}
	g := testGame("abc", "Zelda", "gba", "Game Boy Advance")
	tags := TagsFor(g, cfg)
	if len(tags) != 1 || tags[0] != "GBA" {
		t.Fatalf("tags = %q", tags)
	}
}

func TestTagsCustomName(t *testing.T) {
	cfg := config.DefaultSteamConfig()
	cfg.CategoryTemplate = "{prefix} - {name}"
	cfg.Categories = map[string]string{"GBA": "ГБА"}
	g := testGame("abc", "Zelda", "gba", "Game Boy Advance")
	tags := TagsFor(g, cfg)
	if len(tags) != 1 || tags[0] != "Cartridge - ГБА" {
		t.Fatalf("tags = %q", tags)
	}
	// Consoles without an override fall back to the short tag.
	g2 := testGame("def", "Mario", "nes", "Nintendo Entertainment System")
	tags2 := TagsFor(g2, cfg)
	if len(tags2) != 1 || tags2[0] != "Cartridge - NES" {
		t.Fatalf("tags2 = %q", tags2)
	}
	// Lookup is case-insensitive on id and short.
	if id, ok := CanonicalConsoleID("GBA"); !ok || id != "gba" {
		t.Fatalf("canonical GBA = %q %v", id, ok)
	}
	if _, ok := CanonicalConsoleID("nope"); ok {
		t.Fatalf("unknown console accepted")
	}
}

func TestAppName(t *testing.T) {
	g := testGame("abc", "Zelda", "gba", "Game Boy Advance")
	if got := AppNameFor(g); got != "Zelda (GBA)" {
		t.Fatalf("appname = %q", got)
	}
	u := testGame("abc", "Mystery", "unknown", "")
	u.ConsoleName = ""
	if got := AppNameFor(u); got != "Mystery" {
		t.Fatalf("unknown appname = %q", got)
	}
}

func TestVDFRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shortcuts.vdf")
	cfg := config.DefaultSteamConfig()
	exe := "/usr/bin/cartridge"
	games := []rom.Game{
		testGame("id1", "Zelda", "gba", "Game Boy Advance"),
		testGame("id2", "Mario", "nes", "Nintendo Entertainment System"),
	}
	if _, err := SyncFile(path, games, cfg, exe, "/usr/bin", false); err != nil {
		t.Fatal(err)
	}
	list, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("shortcuts = %d, want 2", len(list))
	}
	// Second sync must be idempotent (no dupes).
	if _, err := SyncFile(path, games, cfg, exe, "/usr/bin", false); err != nil {
		t.Fatal(err)
	}
	list2, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 2 {
		t.Fatalf("after resync shortcuts = %d, want 2", len(list2))
	}
	if list2[0].LaunchOptions != "play id1" {
		t.Fatalf("launch = %q", list2[0].LaunchOptions)
	}
	if len(list2[0].Tags) != 1 || list2[0].Tags[0] != "Cartridge - GBA" {
		t.Fatalf("tags = %q", list2[0].Tags)
	}
	// Prune removes gone games but keeps foreign shortcuts.
	foreign := Shortcut{AppID: 123, AppName: "Firefox", Exe: "/usr/bin/firefox", LaunchOptions: "", Tags: []string{"Internet"}}
	list2 = append(list2, foreign)
	if err := SaveFile(path, list2); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncFile(path, games[:1], cfg, exe, "/usr/bin", true); err != nil {
		t.Fatal(err)
	}
	list3, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(list3) != 2 {
		t.Fatalf("after prune = %d, want 2 (1 game + firefox)", len(list3))
	}
	found := false
	for _, s := range list3 {
		if s.AppName == "Firefox" {
			found = true
		}
	}
	if !found {
		t.Fatalf("foreign shortcut dropped")
	}
}

func TestResetAllKeepsForeign(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	ud := filepath.Join(home, ".local", "share", "Steam", "userdata", "7", "config")
	if err := os.MkdirAll(ud, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ud, "shortcuts.vdf")
	cfg := config.DefaultSteamConfig()
	exe := filepath.Join(home, "cartridge")
	games := []rom.Game{
		testGame("id1", "Zelda", "gba", "Game Boy Advance"),
		testGame("id2", "Mario", "nes", "Nintendo Entertainment System"),
	}
	if _, err := SyncFile(path, games, cfg, exe, home, false); err != nil {
		t.Fatal(err)
	}
	list, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	list = append(list, Shortcut{AppID: 42, AppName: "Firefox", Exe: "/usr/bin/firefox"})
	if err := SaveFile(path, list); err != nil {
		t.Fatal(err)
	}
	res, err := ResetAll(cfg, exe)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Removed != 2 || res[0].Total != 1 {
		t.Fatalf("reset = %+v", res)
	}
	rest, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || rest[0].AppName != "Firefox" {
		t.Fatalf("after reset = %+v", rest)
	}
	// Second reset is a no-op.
	res, err = ResetAll(cfg, exe)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Removed != 0 {
		t.Fatalf("second reset removed %d", res[0].Removed)
	}
}

func TestLoadMissing(t *testing.T) {
	list, err := LoadFile(filepath.Join(t.TempDir(), "nope.vdf"))
	if err != nil || len(list) != 0 {
		t.Fatalf("missing = %v %v", list, err)
	}
}

func TestPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.vdf")
	s := Shortcut{AppID: 99, AppName: "X", Exe: "/bin/x", Tags: []string{"A"}, extraStr: map[string]string{"FlatpakAppID": "org.test"}, extraInt: map[string]int32{}}
	if err := SaveFile(path, []Shortcut{s}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	_ = data
	list, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].extraStr["FlatpakAppID"] != "org.test" {
		t.Fatalf("extra lost: %+v", list[0])
	}
}
