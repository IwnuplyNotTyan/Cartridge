package mods

import (
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// encodeVarint is the test-side encoder for byuu-style varints.
func encodeVarint(n uint64) []byte {
	var out []byte
	for {
		b := byte(n & 0x7F)
		n >>= 7
		if n == 0 {
			return append(out, 0x80|b)
		}
		out = append(out, b)
		n--
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeBPS builds a minimal valid BPS for rom: header sizes + empty
// metadata/actions + footer CRCs.
func makeBPS(t *testing.T, path string, rom []byte) {
	t.Helper()
	var data []byte
	data = append(data, "BPS1"...)
	data = append(data, encodeVarint(uint64(len(rom)))...)
	data = append(data, encodeVarint(uint64(len(rom)))...)
	data = append(data, encodeVarint(0)...) // no metadata, no actions
	data = appendU32(data, crc32.Checksum(rom, crc32.IEEETable))
	data = appendU32(data, crc32.Checksum(rom, crc32.IEEETable))
	data = appendU32(data, crc32.Checksum(data, crc32.IEEETable))
	writeFile(t, path, data)
}

func appendU32(data []byte, v uint32) []byte {
	return append(data, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// makeUPS builds a minimal valid UPS the same way.
func makeUPS(t *testing.T, path string, rom []byte) {
	t.Helper()
	var data []byte
	data = append(data, "UPS1"...)
	data = append(data, encodeVarint(uint64(len(rom)))...)
	data = append(data, encodeVarint(uint64(len(rom)))...)
	data = appendU32(data, crc32.Checksum(rom, crc32.IEEETable))
	data = appendU32(data, crc32.Checksum(rom, crc32.IEEETable))
	data = appendU32(data, crc32.Checksum(data, crc32.IEEETable))
	writeFile(t, path, data)
}

// makeIPS builds PATCH + one record at off + EOF.
func makeIPS(t *testing.T, path string, off int, payload []byte) {
	t.Helper()
	data := []byte("PATCH")
	data = append(data, byte(off>>16), byte(off>>8), byte(off))
	data = append(data, byte(len(payload)>>8), byte(len(payload)))
	data = append(data, payload...)
	data = append(data, "EOF"...)
	writeFile(t, path, data)
}

func TestVerifyBPSOK(t *testing.T) {
	rom := []byte("0123456789ABCDEF")
	p := filepath.Join(t.TempDir(), "hack.bps")
	makeBPS(t, p, rom)
	if got := VerifyPatch(p, rom); !got.OK || got.Kind != "ok" {
		t.Fatalf("valid bps must pass: %+v", got)
	}
}

func TestVerifyBPSWrongROM(t *testing.T) {
	rom := []byte("0123456789ABCDEF")
	other := []byte("0123456789ABCDE!")
	p := filepath.Join(t.TempDir(), "hack.bps")
	makeBPS(t, p, rom)
	got := VerifyPatch(p, other)
	if got.OK || got.Kind != "base" || !got.HasCRC {
		t.Fatalf("bps for another dump must report base: %+v", got)
	}
	if got.WantCRC == got.GotCRC {
		t.Fatalf("CRCs must differ: %+v", got)
	}
}

func TestVerifyBPSHeaderedMismatch(t *testing.T) {
	// Headerless ROM vs patch built for the headered dump (+512 bytes):
	// the classic SNES miss.
	headerless := make([]byte, 1024)
	headered := make([]byte, 1024+512)
	p := filepath.Join(t.TempDir(), "hack.bps")
	makeBPS(t, p, headered)
	got := VerifyPatch(p, headerless)
	if got.OK || got.Kind != "base" {
		t.Fatalf("must report base: %+v", got)
	}
	if got.WantSize-got.GotSize != 512 {
		t.Fatalf("size delta must be 512: %+v", got)
	}
}

func TestVerifyBPSCorrupt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.bps"), []byte("BPS1\x82junk"))
	if got := VerifyPatch(filepath.Join(dir, "a.bps"), []byte("x")); got.Kind != "broken" {
		t.Fatalf("garbage must be broken: %+v", got)
	}
	writeFile(t, filepath.Join(dir, "b.bps"), []byte("<html>not a patch</html>"))
	if got := VerifyPatch(filepath.Join(dir, "b.bps"), []byte("x")); got.Kind != "broken" {
		t.Fatalf("html download must be broken: %+v", got)
	}
}

func TestVerifyUPS(t *testing.T) {
	rom := []byte("0123456789ABCDEF")
	p := filepath.Join(t.TempDir(), "hack.ups")
	makeUPS(t, p, rom)
	if got := VerifyPatch(p, rom); !got.OK {
		t.Fatalf("valid ups must pass: %+v", got)
	}
	if got := VerifyPatch(p, []byte("different!different")); got.Kind != "base" {
		t.Fatalf("ups for another dump must report base: %+v", got)
	}
}

func TestVerifyIPSFitAndOverrun(t *testing.T) {
	rom := make([]byte, 64)
	p := filepath.Join(t.TempDir(), "hack.ips")
	makeIPS(t, p, 10, []byte("hello"))
	if got := VerifyPatch(p, rom); !got.OK {
		t.Fatalf("fitting ips must pass: %+v", got)
	}
	makeIPS(t, p, 60, []byte("0123456789")) // ends at 70 > 64
	if got := VerifyPatch(p, rom); got.Kind != "base" {
		t.Fatalf("overrunning ips must report base: %+v", got)
	}
	writeFile(t, p, []byte("PATCH-truncated"))
	if got := VerifyPatch(p, rom); got.Kind != "broken" {
		t.Fatalf("truncated ips must be broken: %+v", got)
	}
}

func TestVerifyUnknownFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hack.ppf")
	writeFile(t, p, []byte("PPF3junk"))
	if got := VerifyPatch(p, make([]byte, 8)); got.Kind != "unknown" {
		t.Fatalf("ppf must be unknown: %+v", got)
	}
}
