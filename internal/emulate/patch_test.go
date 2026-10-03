package emulate

import (
	"os"
	"path/filepath"
	"testing"
)

func strp(s string) *string { return &s }

func TestApplyPatchesOverlay(t *testing.T) {
	base := []Recipe{{Name: "mesen-nes", Emulator: "mesen", Args: []string{"{rom}"}}}
	emu := "mesen-new"
	patches := []RecipePatch{{Recipe: "mesen-nes", Emulator: &emu}}
	got := ApplyPatches(base, patches)
	if got[0].Emulator != "mesen-new" {
		t.Fatalf("patch not applied: %+v", got[0])
	}
	if base[0].Emulator != "mesen" {
		t.Fatalf("base must not be mutated: %+v", base[0])
	}
	// Unknown target is skipped.
	got = ApplyPatches(base, []RecipePatch{{Recipe: "nope", Emulator: &emu}})
	if got[0].Emulator != "mesen" {
		t.Fatalf("unknown patch must be skipped: %+v", got[0])
	}
}

func TestLoadPatchesRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "patches.json")
	emu := "retroarch"
	if err := SavePatches(p, []RecipePatch{{Recipe: "r", Emulator: &emu}}); err != nil {
		t.Fatal(err)
	}
	// Base recipes file must stay untouched: only patches file exists.
	loaded, err := LoadPatches(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Recipe != "r" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if loaded[0].Emulator == nil || *loaded[0].Emulator != "retroarch" {
		t.Fatalf("emulator lost: %+v", loaded[0])
	}
}

func TestLoadPatchesDir(t *testing.T) {
	dir := t.TempDir()
	if err := SavePatches(filepath.Join(dir, "a.json"), []RecipePatch{{Recipe: "x"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPatches(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("dir load = %+v", got)
	}
	missing, err := LoadPatches(filepath.Join(dir, "nope.json"))
	if err != nil || missing != nil {
		t.Fatalf("missing must yield nil,nil: %v %v", missing, err)
	}
}
