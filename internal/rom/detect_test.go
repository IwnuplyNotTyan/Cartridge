package rom

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestParseTitle(t *testing.T) {
	title, region, tags := ParseTitle("Legend of Zelda, The - Link's Awakening DX (USA, Europe)")
	if title != "Legend of Zelda, The - Link's Awakening DX" {
		t.Fatalf("bad title: %q", title)
	}
	if region == "" {
		t.Fatalf("expected region, got empty")
	}
	_ = tags

	title, _, _ = ParseTitle("Legend_of_Zelda,_The_A_Link_to_the_Past_&_Four_Swords_USA")
	if title != "Legend of Zelda, The A Link to the Past & Four Swords USA" {
		t.Fatalf("underscore title not cleaned: %q", title)
	}
}

func TestSniffNES(t *testing.T) {
	head := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0}, 64)...)
	con, ok := sniff(head, ".bin")
	if !ok || con.ID != "nes" {
		t.Fatalf("NES magic not detected: %+v %v", con, ok)
	}
}

func TestSniffGBC(t *testing.T) {
	head := bytes.Repeat([]byte{0}, 0x200)
	copy(head[0x104:], nintendoLogo)
	head[0x143] = 0x80
	con, ok := sniff(head, ".gbc")
	if !ok || con.ID != "gbc" {
		t.Fatalf("GBC not detected: %+v %v", con, ok)
	}
	head[0x143] = 0x00
	con, ok = sniff(head, ".gb")
	if !ok || con.ID != "gb" {
		t.Fatalf("GB not detected: %+v %v", con, ok)
	}
}

func TestIdentifyIDStableAcrossRename(t *testing.T) {
	dir := t.TempDir()
	payload := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 256)...)
	p1 := filepath.Join(dir, "a.nes")
	p2 := filepath.Join(dir, "b.nes")
	if err := os.WriteFile(p1, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	g1, err := Identify(p1)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := Identify(p2)
	if err != nil {
		t.Fatal(err)
	}
	if g1.ID != g2.ID {
		t.Fatalf("ID must not depend on path: %q vs %q", g1.ID, g2.ID)
	}
	sum := sha256.Sum256(payload)
	if g1.ID != hex.EncodeToString(sum[:]) {
		t.Fatalf("ID must be sha256 of content")
	}
	if g1.ConsoleID != "nes" {
		t.Fatalf("expected nes, got %q", g1.ConsoleID)
	}
}
