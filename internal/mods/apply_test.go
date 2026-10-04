package mods

import (
	"bytes"
	"testing"

	rompatcher "github.com/olsonb97/RomPatcher.go"
)

func patternROM(t *testing.T, size int) []byte {
	t.Helper()
	rom := make([]byte, size)
	for i := range rom {
		rom[i] = byte(i*7 + 3)
	}
	return rom
}

// mutate returns a copy of rom with payload written at off.
func mutate(rom []byte, off int, payload []byte) []byte {
	out := append([]byte(nil), rom...)
	copy(out[off:], payload)
	return out
}

func mustCreate(t *testing.T, original, modified []byte, format rompatcher.Format) []byte {
	t.Helper()
	p, err := rompatcher.Create(original, modified, format, nil)
	if err != nil {
		t.Fatalf("create %s: %v", format, err)
	}
	data, err := p.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal %s: %v", format, err)
	}
	return data
}

func TestApplyPatchRoundTrip(t *testing.T) {
	rom := patternROM(t, 4096)
	want := mutate(rom, 100, []byte("HELLO-MOD"))
	for _, format := range []rompatcher.Format{
		rompatcher.FormatIPS, rompatcher.FormatBPS, rompatcher.FormatUPS,
	} {
		patch := mustCreate(t, rom, want, format)
		got, err := ApplyPatch(rom, patch, "game.sfc")
		if err != nil {
			t.Fatalf("apply %s: %v", format, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("apply %s mismatch", format)
		}
	}
}

func TestApplyPatchWrongROM(t *testing.T) {
	rom := patternROM(t, 4096)
	want := mutate(rom, 100, []byte("HELLO-MOD"))
	other := patternROM(t, 4096)
	other[200] ^= 0xFF
	patch := mustCreate(t, rom, want, rompatcher.FormatBPS)
	if _, err := ApplyPatch(other, patch, "game.sfc"); err == nil {
		t.Fatalf("bps for another dump must fail, not corrupt")
	}
}

func TestApplyPatchGarbage(t *testing.T) {
	if _, err := ApplyPatch([]byte("0123456789ABCDEF"), []byte("<html>nope</html>"), "game.sfc"); err == nil {
		t.Fatalf("garbage patch must fail")
	}
}

// TestApplyPatchStripsHeader covers the auto-adapt win: a BPS built for
// the headerless dump applied to the headered dump (real 512-byte copier
// header present) — the header is stripped for patching, reattached after.
func TestApplyPatchStripsHeader(t *testing.T) {
	const body = 262144 // SNES size multiple the lib recognizes
	header := bytes.Repeat([]byte("H"), 512)
	headless := patternROM(t, body)
	headed := append(append([]byte(nil), header...), headless...)
	wantHeadless := mutate(headless, 1000, []byte("KAIZO!!"))
	patch := mustCreate(t, headless, wantHeadless, rompatcher.FormatBPS)

	got, err := ApplyPatch(headed, patch, "game.sfc")
	if err != nil {
		t.Fatalf("headered rom + headerless bps: %v", err)
	}
	want := append(append([]byte(nil), header...), wantHeadless...)
	if !bytes.Equal(got, want) {
		t.Fatalf("header must survive, body patched")
	}
}

// TestApplyPatchAddsHeader covers the IPS mirror: headerless ROM with a
// patch written for headered offsets (records past the 512-byte mark,
// overrunning the ROM end). Temporary header data aligns the offsets,
// then leaves the output.
func TestApplyPatchAddsHeader(t *testing.T) {
	const body = 262144
	headless := patternROM(t, body)
	// Hand-built headered IPS: record near the end, overrunning the
	// headerless ROM but fitting with a 512-byte header.
	var patch []byte
	patch = append(patch, "PATCH"...)
	off := body - 144 + 512 // headered offset of body point body-144
	payload := bytes.Repeat([]byte("K"), 100)
	patch = append(patch, byte(off>>16), byte(off>>8), byte(off))
	patch = append(patch, byte(len(payload)>>8), byte(len(payload)))
	patch = append(patch, payload...)
	patch = append(patch, "EOF"...)

	got, err := ApplyPatch(headless, patch, "game.sfc")
	if err != nil {
		t.Fatalf("headerless rom + headered ips: %v", err)
	}
	if len(got) != body {
		t.Fatalf("output must stay headerless: %d", len(got))
	}
	if !bytes.Equal(got[body-144:body-144+len(payload)], payload) {
		t.Fatalf("payload landed at the wrong offset")
	}
}

// TestApplyPatchCannotInventHeader: a BPS built for the headered dump can
// never apply to the headerless one — 512 unknown bytes cannot be guessed.
// It must fail loudly instead of corrupting.
func TestApplyPatchCannotInventHeader(t *testing.T) {
	const body = 262144
	header := bytes.Repeat([]byte("H"), 512)
	headless := patternROM(t, body)
	headed := append(append([]byte(nil), header...), headless...)
	wantHeaded := mutate(headed, 2000, []byte("KAIZO!!"))
	patch := mustCreate(t, headed, wantHeaded, rompatcher.FormatBPS)

	if _, err := ApplyPatch(headless, patch, "game.sfc"); err == nil {
		t.Fatalf("headered bps on headerless rom must fail, not corrupt")
	}
}
