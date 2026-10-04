package mods

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsModPath(t *testing.T) {
	for _, p := range []string{
		"a.ips", "b.BPS", "c.ups", "d.ppf", "e.xdelta", "f.patch",
		"g.ebp", "h.ips32", "i.bdf", "j.bspatch", "k.mod", "l.aps", "m.rup",
	} {
		if !IsModPath(p) {
			t.Fatalf("%s must be a mod", p)
		}
	}
	for _, p := range []string{"a.nes", "b.smc", "c.gba", "c.zip", "c.cht", "noext"} {
		if IsModPath(p) {
			t.Fatalf("%s must not be a mod", p)
		}
	}
}

func TestMatchByStem(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"Battletoads.ips",
		"Kirby Mirror (U).bps",
		"other.txt",
		"zzz.ups",
	}
	for _, n := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Match(dir, "Battletoads (U)", "nope")
	if err != nil || len(got) != 1 {
		t.Fatalf("Match = %+v, %v", got, err)
	}
	got, err = Match(dir, "zzz-missing", "Kirby Mirror")
	if err != nil || len(got) != 1 {
		t.Fatalf("Match = %+v, %v", got, err)
	}
	got, err = Match(dir, "missing-game")
	if err != nil || len(got) != 0 {
		t.Fatalf("miss must be empty: %+v, %v", got, err)
	}
}

func TestMatchSubfolder(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "Battletoads")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "hard.ips"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "hard2.bps"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Match(dir, "Battletoads (U)")
	if err != nil || len(got) != 2 {
		t.Fatalf("folder match = %+v, %v", got, err)
	}
}

func TestListDirMissing(t *testing.T) {
	got, err := ListDir(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(got) != 0 {
		t.Fatalf("missing dir must be empty: %+v, %v", got, err)
	}
}

func writeModFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMatchSuffixedName(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Super Mario World Kaizo.ips", "other.bps"} {
		writeModFile(t, dir, n)
	}
	// A suffix after the game name must still match.
	got, err := Match(dir, "Super Mario World (USA)")
	if err != nil || len(got) != 1 || got[0].Name != "Super Mario World Kaizo.ips" {
		t.Fatalf("suffixed match = %+v, %v", got, err)
	}
}

func TestMatchTitleWord(t *testing.T) {
	dir := t.TempDir()
	writeModFile(t, dir, "mario-kaizo.ips")
	got, err := Match(dir, "Super Mario World (USA)")
	if err != nil || len(got) != 1 {
		t.Fatalf("word match = %+v, %v", got, err)
	}
}

func TestMatchExactBeatsFuzzy(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"game.ips", "gamekaizo.ips"} {
		writeModFile(t, dir, n)
	}
	// Exact stem wins alone and does not drag the fuzzy one along.
	got, err := Match(dir, "game")
	if err != nil || len(got) != 1 || got[0].Name != "game.ips" {
		t.Fatalf("exact must win: %+v, %v", got, err)
	}
}

func TestMatchShortStemNoClaim(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"abc.ips", "xyz.ups"} {
		writeModFile(t, dir, n)
	}
	// A one-letter ROM base must not claim every patch in the pile.
	if got, err := Match(dir, "a"); err != nil || len(got) != 0 {
		t.Fatalf("short stem must not match: %+v, %v", got, err)
	}
}

func TestSoftBase(t *testing.T) {
	dir, stem := SoftBase("/games/game.sfc", "")
	if dir != "/games" || stem != "game" {
		t.Fatalf("plain ROM -> %q %q", dir, stem)
	}
	// Zipped ROM: patch takes the inner stem, in the archive's dir.
	dir, stem = SoftBase("/games/comp.zip", "inner.smc")
	if dir != "/games" || stem != "inner" {
		t.Fatalf("zipped ROM -> %q %q", dir, stem)
	}
}

func TestPlanDeployFirstSlot(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "pile", "hack.ips")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("PATCH"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst, active := PlanDeploy(src, filepath.Join(dir, "game.sfc"), "", ".ips")
	if active || dst != filepath.Join(dir, "game.ips") {
		t.Fatalf("fresh deploy must target game.ips inactive, got %q %v", dst, active)
	}
}

func TestPlanDeployStacksNumberedSlots(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "pile", "hack2.ips")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("OTHER"), 0o644); err != nil {
		t.Fatal(err)
	}
	// game.ips is occupied by foreign bytes: take the next slot.
	if err := os.WriteFile(filepath.Join(dir, "game.ips"), []byte("FOREIGN"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst, active := PlanDeploy(src, filepath.Join(dir, "game.sfc"), "", ".ips")
	if active || dst != filepath.Join(dir, "game.ips1") {
		t.Fatalf("occupied slot must stack to game.ips1, got %q %v", dst, active)
	}
}

func TestDeployRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "pile", "hack.ips")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("PATCH"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst, active := PlanDeploy(src, filepath.Join(dir, "game.sfc"), "", ".ips")
	if active {
		t.Fatalf("must not be active before deploy")
	}
	if err := Deploy(src, dst); err != nil {
		t.Fatal(err)
	}
	if dst2, active := PlanDeploy(src, filepath.Join(dir, "game.sfc"), "", ".ips"); !active || dst2 != dst {
		t.Fatalf("must be active after deploy, got %q %v", dst2, active)
	}
	if err := Undeploy(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("undeploy must remove the file")
	}
}

func TestUndeployKeepsForeignFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "pile", "hack.ips")
	dst := filepath.Join(dir, "game.ips")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("PATCH"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("HAND-PLACED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Undeploy(src, dst); err == nil {
		t.Fatalf("foreign file must not be removed")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("foreign file must survive: %v", err)
	}
}
