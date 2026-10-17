package rom

import (
	"bytes"
	"testing"
)

func TestGBAHeaderTitle(t *testing.T) {
	head := make([]byte, 0x100)
	copy(head[0xA0:], "POKEMON EMER")
	head[0xB2] = 0x96

	got := HeaderTitle(head, "gba")
	if got != "POKEMON EMER" {
		t.Fatalf("gbaHeaderTitle = %q, want %q", got, "POKEMON EMER")
	}

	// Blank header (like moonbeam)
	blank := make([]byte, 0x100)
	if got := HeaderTitle(blank, "gba"); got != "" {
		t.Fatalf("expected empty title for blank GBA header, got %q", got)
	}
}

func TestGBHeaderTitle(t *testing.T) {
	head := make([]byte, 0x200)
	copy(head[0x104:], nintendoLogo)
	copy(head[0x134:], "TETRIS\x00")

	got := HeaderTitle(head, "gb")
	if got != "TETRIS" {
		t.Fatalf("gbHeaderTitle = %q, want %q", got, "TETRIS")
	}

	// CGB title
	copy(head[0x134:], "POKEMON CRYS\x00")
	head[0x143] = 0x80
	got = HeaderTitle(head, "gbc")
	if got != "POKEMON CRYS" {
		t.Fatalf("gbcHeaderTitle = %q, want %q", got, "POKEMON CRYS")
	}
}

func TestGenesisHeaderTitle(t *testing.T) {
	head := make([]byte, 0x200)
	// Overseas title at 0x150
	copy(head[0x150:], "SONIC THE HEDGEHOG                              ")

	got := HeaderTitle(head, "genesis")
	if got != "SONIC THE HEDGEHOG" {
		t.Fatalf("genesisHeaderTitle = %q, want %q", got, "SONIC THE HEDGEHOG")
	}
}

func TestSNESHeaderTitle(t *testing.T) {
	// LoROM at 0x7FC0
	head := make([]byte, 0x10200)
	off := 0x7FC0
	copy(head[off:], "SUPER MARIOWORLD     ")
	// Complement + Checksum = 0xFFFF
	head[off+0x1C] = 0x12
	head[off+0x1D] = 0x34
	head[off+0x1E] = 0xED
	head[off+0x1F] = 0xCB

	got := HeaderTitle(head, "snes")
	if got != "SUPER MARIOWORLD" {
		t.Fatalf("snesHeaderTitle LoROM = %q, want %q", got, "SUPER MARIOWORLD")
	}

	// HiROM at 0xFFC0 + copier (512 bytes)
	hiHead := make([]byte, 0x10200)
	hiOff := 0xFFC0 + 512
	copy(hiHead[hiOff:], "CHRONO TRIGGER       ")
	hiHead[hiOff+0x1C] = 0x01
	hiHead[hiOff+0x1D] = 0x02
	hiHead[hiOff+0x1E] = 0xFE
	hiHead[hiOff+0x1F] = 0xFD

	got = HeaderTitle(hiHead, "snes")
	if got != "CHRONO TRIGGER" {
		t.Fatalf("snesHeaderTitle HiROM+copier = %q, want %q", got, "CHRONO TRIGGER")
	}
}

func TestN64HeaderTitle(t *testing.T) {
	// Native .z64 (big-endian)
	head := make([]byte, 0x100)
	head[0], head[1], head[2], head[3] = 0x80, 0x37, 0x12, 0x40
	copy(head[0x20:], "SUPER MARIO 64      ")
	got := HeaderTitle(head, "n64")
	if got != "SUPER MARIO 64" {
		t.Fatalf("n64HeaderTitle native = %q, want %q", got, "SUPER MARIO 64")
	}

	// Byte-swapped .v64
	vhead := make([]byte, 0x100)
	vhead[0], vhead[1], vhead[2], vhead[3] = 0x37, 0x80, 0x40, 0x12
	raw := []byte("SUPER MARIO 64      ")
	for i := 0; i+1 < len(raw); i += 2 {
		raw[i], raw[i+1] = raw[i+1], raw[i]
	}
	copy(vhead[0x20:], raw)
	got = HeaderTitle(vhead, "n64")
	if got != "SUPER MARIO 64" {
		t.Fatalf("n64HeaderTitle byte-swapped = %q, want %q", got, "SUPER MARIO 64")
	}
}

func TestNDSHeaderTitle(t *testing.T) {
	head := make([]byte, 0x100)
	copy(head[0x000:], "POKEMON D\x00\x00\x00")
	got := HeaderTitle(head, "ds")
	if got != "POKEMON D" {
		t.Fatalf("ndsHeaderTitle = %q, want %q", got, "POKEMON D")
	}
}

func TestIsSlugOrGeneric(t *testing.T) {
	for stem, want := range map[string]bool{
		"moonbeam-v1-0-1": true,
		"smw":             true,
		"zelda_mc":        true,
		"game":            true,
		"mario64":         true,
		"Super Mario":     false,
		"Zelda":           false,
		"Chrono Trigger":  false,
	} {
		if got := isSlugOrGeneric(stem); got != want {
			t.Errorf("isSlugOrGeneric(%q) = %v, want %v", stem, got, want)
		}
	}
}

func TestResolveTitleSlugFallbackToHeader(t *testing.T) {
	head := make([]byte, 0x100)
	copy(head[0xA0:], "METROID ZERO")
	head[0xB2] = 0x96

	title, _, _, hdr := ResolveTitle("metroid_zero", "metroid_zero.gba", head, "gba", 0)
	if title != "METROID ZERO" || hdr != "METROID ZERO" {
		t.Fatalf("slug should fall back to header: title=%q, hdr=%q", title, hdr)
	}

	// Formatted filename keeps its title
	title, region, _, _ := ResolveTitle("Metroid - Zero Mission (USA)", "Metroid - Zero Mission (USA).gba", head, "gba", 0)
	if title != "Metroid - Zero Mission" || region != "USA" {
		t.Fatalf("formatted title should be preserved: title=%q, region=%q", title, region)
	}
}

func TestCleanHeaderString(t *testing.T) {
	if got := cleanHeaderString([]byte("TEST  TITLE  \x00GARBAGE")); got != "TEST TITLE" {
		t.Fatalf("cleanHeaderString = %q", got)
	}
	if got := cleanHeaderString([]byte{0x00, 0x01, 0x02}); got != "" {
		t.Fatalf("cleanHeaderString leading null should be empty, got %q", got)
	}
	if got := cleanHeaderString(bytes.Repeat([]byte(" "), 10)); got != "" {
		t.Fatalf("cleanHeaderString spaces should be empty, got %q", got)
	}
}

func TestLookupNoIntro(t *testing.T) {
	title := LookupNoIntro(654659974)
	t.Logf("LookupNoIntro = %q", title)
	if title != "With the Last Moonbeam (World) (v1.0.1) (Aftermarket) (Unl)" {
		t.Fatalf("LookupNoIntro = %q", title)
	}
}

func TestIdentifyMoonbeam(t *testing.T) {
	g, err := Identify("/Users/q/Documents/Games/moonbeam-v1-0-1.gba")
	if err != nil {
		t.Fatalf("Identify error: %v", err)
	}
	t.Logf("Identify: title=%q, region=%q, tags=%v", g.Title, g.Region, g.Tags)
	if g.Title != "With the Last Moonbeam" {
		t.Fatalf("wanted 'With the Last Moonbeam', got %q", g.Title)
	}
}

func TestScanDirMoonbeam(t *testing.T) {
	stored := []Game{
		{
			ID:          "1087b00ff0276d6681549a1c77ec3608c53c390ef3089d9f402f800625cb1b14",
			Title:       "moonbeam-v1-0-1",
			ConsoleID:   "gba",
			ConsoleName: "Game Boy Advance",
			Path:        "/Users/q/Documents/Games/moonbeam-v1-0-1.gba",
			Size:        1449956,
			SHA256:      "1087b00ff0276d6681549a1c77ec3608c53c390ef3089d9f402f800625cb1b14",
			FileSize:    1449956,
			ModTime:     1792232440401951499,
		},
	}
	games, errs := ScanDir("/Users/q/Documents/Games", stored)
	if len(errs) != 0 {
		t.Fatalf("ScanDir errs: %v", errs)
	}
	if len(games) == 0 {
		t.Fatalf("no games found")
	}
	t.Logf("ScanDir: title=%q", games[0].Title)
	if games[0].Title != "With the Last Moonbeam" {
		t.Fatalf("ScanDir wanted 'With the Last Moonbeam', got %q", games[0].Title)
	}

	// Rescan with already healed cache must preserve "With the Last Moonbeam"
	games2, errs := ScanDir("/Users/q/Documents/Games", games)
	if len(errs) != 0 || len(games2) == 0 {
		t.Fatalf("rescan errs: %v", errs)
	}
	if games2[0].Title != "With the Last Moonbeam" {
		t.Fatalf("rescan with healed cache must not revert to slug: got %q", games2[0].Title)
	}
}
