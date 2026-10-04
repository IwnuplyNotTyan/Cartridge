package mods

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rompatcher "github.com/olsonb97/RomPatcher.go"
)

func makeZipBytes(t *testing.T, zp, name string, data []byte) {
	t.Helper()
	zw, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(zw)
	f, err := w.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceName(t *testing.T) {
	if got := InstanceName("/games/Super Mario World (USA).sfc", "", "/pile/kaizo.ips"); got != "Super Mario World (USA) [kaizo].sfc" {
		t.Fatalf("name = %q", got)
	}
	// Zipped games take the inner stem; the instance is a plain file.
	if got := InstanceName("/games/comp.zip", "inner.smc", "/pile/fix.bps"); got != "inner [fix].smc" {
		t.Fatalf("zip name = %q", got)
	}
}

func TestEnsureInstanceBuildReuseRebuild(t *testing.T) {
	dir := t.TempDir()
	patched := filepath.Join(dir, "patched")
	romPath := filepath.Join(dir, "games", "game.sfc")
	rom := patternROM(t, 4096)
	writeFile(t, romPath, rom)
	modPath := filepath.Join(dir, "pile", "kaizo.ips")
	want := mutate(rom, 500, []byte("KAIZO!!"))
	writeFile(t, modPath, mustCreate(t, rom, want, rompatcher.FormatIPS))

	const gameID = "rom-sha-placeholder"
	inst, built, err := EnsureInstance(patched, gameID, romPath, "", modPath)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !built {
		t.Fatalf("first ensure must build")
	}
	if base := filepath.Base(inst); base != "game [kaizo].sfc" {
		t.Fatalf("instance name = %q", base)
	}
	if _, fresh := InstanceStatus(patched, gameID, modPath); !fresh {
		t.Fatalf("must be fresh right after build")
	}
	data, err := os.ReadFile(inst)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(want) {
		t.Fatalf("instance size %d, want %d", len(data), len(want))
	}
	for i := range want {
		if data[i] != want[i] {
			t.Fatalf("instance byte %d differs", i)
		}
	}

	// Second ensure reuses without touching the disk.
	st1, _ := os.Stat(inst)
	inst2, built, err := EnsureInstance(patched, gameID, romPath, "", modPath)
	if err != nil || built || inst2 != inst {
		t.Fatalf("reuse = %q %v %v", inst2, built, err)
	}
	st2, _ := os.Stat(inst)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatalf("reuse must not rewrite")
	}

	// Changed patch file rebuilds in place.
	want2 := mutate(rom, 600, []byte("V2!!"))
	writeFile(t, modPath, mustCreate(t, rom, want2, rompatcher.FormatIPS))
	inst3, built, err := EnsureInstance(patched, gameID, romPath, "", modPath)
	if err != nil || !built || inst3 != inst {
		t.Fatalf("rebuild = %q %v %v", inst3, built, err)
	}
	data, _ = os.ReadFile(inst)
	for i := range want2 {
		if data[i] != want2[i] {
			t.Fatalf("rebuilt byte %d differs", i)
		}
	}
}

func TestEnsureInstanceWrongBase(t *testing.T) {
	dir := t.TempDir()
	romPath := filepath.Join(dir, "game.sfc")
	rom := patternROM(t, 4096)
	other := patternROM(t, 4096)
	other[0] ^= 0xFF
	writeFile(t, romPath, rom)
	modPath := filepath.Join(dir, "kaizo.bps")
	writeFile(t, modPath, mustCreate(t, other, mutate(other, 10, []byte("X")), rompatcher.FormatBPS))
	_, _, err := EnsureInstance(filepath.Join(dir, "patched"), "id", romPath, "", modPath)
	if err == nil {
		t.Fatalf("bps for another dump must fail loudly")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error must explain mismatch: %v", err)
	}
}

func TestEnsureInstanceFromZip(t *testing.T) {
	dir := t.TempDir()
	rom := patternROM(t, 4096)
	zp := filepath.Join(dir, "comp.zip")
	makeZipBytes(t, zp, "inner.sfc", rom)
	modPath := filepath.Join(dir, "fix.ips")
	want := mutate(rom, 300, []byte("FIX"))
	writeFile(t, modPath, mustCreate(t, rom, want, rompatcher.FormatIPS))

	inst, built, err := EnsureInstance(filepath.Join(dir, "patched"), "inner-sha", zp, "inner.sfc", modPath)
	if err != nil || !built {
		t.Fatalf("zip build = %v %v", built, err)
	}
	if base := filepath.Base(inst); base != "inner [fix].sfc" {
		t.Fatalf("zip instance name = %q", base)
	}
	data, err := os.ReadFile(inst)
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if data[i] != want[i] {
			t.Fatalf("zip instance byte %d differs", i)
		}
	}
}
