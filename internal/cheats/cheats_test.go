package cheats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleCHT = `# Battletoads cheats
cheats = 3

cheat0_desc = "Infinite Lives"
cheat0_code = "7E0A00:09"
cheat0_enable = false

cheat1_desc = "Infinite Health"
cheat1_code = "7E0B00:FF+7E0C00:FF"
cheat1_enable = true

; trailing comment
cheat2_desc = Unquoted Desc
cheat2_code = 1234
cheat2_enable = 1
`

func TestParseSample(t *testing.T) {
	got := Parse([]byte(sampleCHT))
	if len(got) != 3 {
		t.Fatalf("parsed %d cheats: %+v", len(got), got)
	}
	if got[0].Desc != "Infinite Lives" || got[0].Code != "7E0A00:09" {
		t.Fatalf("cheat0 broken: %+v", got[0])
	}
	if got[1].Desc != "Infinite Health" || got[1].Code != "7E0B00:FF+7E0C00:FF" {
		t.Fatalf("cheat1 broken: %+v", got[1])
	}
	if got[2].Desc != "Unquoted Desc" || got[2].Code != "1234" {
		t.Fatalf("unquoted broken: %+v", got[2])
	}
}

func TestParseGarbageTolerant(t *testing.T) {
	got := Parse([]byte("garbage\ncheat0_desc = \ncheat9_code = \"x\"\ncheat_x_desc = 1\n"))
	if len(got) != 1 || got[0].Code != "x" {
		t.Fatalf("garbage must leave one code-only cheat: %+v", got)
	}
	if len(Parse(nil)) != 0 {
		t.Fatalf("empty must parse to nothing")
	}
}

func TestParseIgnoresEnableValues(t *testing.T) {
	// Enable lines are consumed, never stored: saves normalize to true.
	for _, in := range []string{"true", "false", "1", "0", "yes", "no", "on", "off", "maybe"} {
		got := Parse([]byte("cheat0_desc = \"d\"\ncheat0_code = \"c\"\ncheat0_enable = " + in + "\n"))
		if len(got) != 1 || got[0].Desc != "d" || got[0].Code != "c" {
			t.Fatalf("enable=%q -> %+v", in, got)
		}
	}
}

func TestSaveNormalizesEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "game.cht")
	if err := os.WriteFile(path, []byte(sampleCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Cheats) != 3 {
		t.Fatalf("parsed %d cheats", len(f.Cheats))
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "_enable = true"); got != 3 {
		t.Fatalf("every cheat must save enabled:\n%s", data)
	}
	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Cheats) != 3 || r.Cheats[1].Code != "7E0B00:FF+7E0C00:FF" {
		t.Fatalf("round trip broken: %+v", r.Cheats)
	}
}

func TestFindByStems(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Battletoads.cht", "Kirby Mirror (U).CHT", "other.txt", "zzz.cht"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("cheats = 0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// ROM base with a region tag finds the tagless file.
	if got, err := Find(dir, "Battletoads (U)", "nope"); err != nil || !strings.HasSuffix(got, "Battletoads.cht") {
		t.Fatalf("Find = %q, %v", got, err)
	}
	// Title matches a tagged file once earlier candidates miss.
	if got, err := Find(dir, "zzz-missing", "Kirby Mirror"); err != nil || !strings.Contains(got, "Kirby Mirror") {
		t.Fatalf("Find = %q, %v", got, err)
	}
	// First candidate wins; empty candidates skipped.
	if got, err := Find(dir, "", "zzz"); err != nil || !strings.HasSuffix(got, "zzz.cht") {
		t.Fatalf("Find = %q, %v", got, err)
	}
	if _, err := Find(dir, "missing-game"); err == nil {
		t.Fatalf("no match must error")
	}
	if _, err := Find(filepath.Join(dir, "nope")); err == nil {
		t.Fatalf("missing dir must error")
	}
}

const extrasCHT = `# Kirby pile
cheats = 2

cheat0_desc = "Infinite Health"
cheat0_code = "AAA"
cheat0_enable = false
cheat0_big_endian = false
; boss room only

cheat5_desc = "Lives"
cheat5_code = "BBB"
cheat5_big_endian = true

cheat9_foo = "stray"
trailing = "kept"
`

func TestPreserveExtrasRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "game.cht")
	if err := os.WriteFile(path, []byte(extrasCHT), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Cheats) != 2 {
		t.Fatalf("parsed %d cheats", len(f.Cheats))
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Kirby pile",
		"cheat0_big_endian = false",
		"; boss room only",
		"cheat1_desc = \"Lives\"",
		"cheat1_code = \"BBB\"",
		"cheat1_big_endian = true",
		"cheat0_enable = true",
		"cheat9_foo = \"stray\"",
		"trailing = \"kept\"",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("saved file must keep %q:\n%s", want, data)
		}
	}
	// Renumbered gap keeps extras attached: old cheat5_* became cheat1_*.
	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Cheats) != 2 || r.Cheats[1].Desc != "Lives" {
		t.Fatalf("reload broken: %+v", r.Cheats)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.cht")); err == nil {
		t.Fatalf("missing file must error")
	}
}

func TestSaveEmptyPath(t *testing.T) {
	if err := (&File{}).Save(); err == nil {
		t.Fatalf("empty path must error")
	}
}
