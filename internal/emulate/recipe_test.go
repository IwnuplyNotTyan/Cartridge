package emulate

import (
	"os"
	"path/filepath"
	"testing"

	"cartridge/internal/rom"
)

func nesGame() rom.Game {
	return rom.Game{
		ID: "aaa111bbb222", Title: "Battletoads",
		ConsoleID: "nes", ConsoleName: "Nintendo Entertainment System",
		Path: "/x/battle.nes",
	}
}

func TestMatchPrecedence(t *testing.T) {
	byExt := Recipe{Name: "by-ext", Extensions: []string{"nes"}, Emulator: "e1"}
	byConsole := Recipe{Name: "by-console", Consoles: []string{"nes"}, Emulator: "e2"}
	byGame := Recipe{Name: "by-game", Games: []string{"aaa111bbb222"}, Emulator: "e3"}
	def := Recipe{Name: "default", Emulator: "e0"}

	if got := Match([]Recipe{byExt, byConsole, byGame, def}, nesGame()); got.Name != "by-game" {
		t.Fatalf("game must win, got %v", got)
	}
	if got := Match([]Recipe{byExt, byConsole, def}, nesGame()); got.Name != "by-console" {
		t.Fatalf("console must beat ext, got %v", got)
	}
	if got := Match([]Recipe{byExt, def}, nesGame()); got.Name != "by-ext" {
		t.Fatalf("ext must beat default, got %v", got)
	}
	other := nesGame()
	other.ConsoleID = "snes"
	other.Path = "/x/b.sfc"
	if got := Match([]Recipe{byExt, byConsole, def}, other); got.Name != "default" {
		t.Fatalf("default fallback broken, got %v", got)
	}
	if got := Match([]Recipe{byExt}, other); got != nil {
		t.Fatalf("expected nil without default, got %v", got)
	}
}

func TestMatchShortIDAndInnerExt(t *testing.T) {
	g := nesGame()
	g.ID = "aaa111bbb222ccc333"
	g.Path = "/x/b.zip"
	g.Inner = "battle.NES"
	r := Recipe{Name: "zip", Games: []string{"aaa111bbb222"}, Extensions: []string{".nes"}, Emulator: "e"}
	if got := Match([]Recipe{r}, g); got == nil || got.Name != "zip" {
		t.Fatalf("shortid/inner-ext match broken, got %v", got)
	}
}

func TestCommandPlaceholders(t *testing.T) {
	r := Recipe{Name: "r", Emulator: "emu", Args: []string{"{rom}", "{title}", "{console}{ext}", "{shortid}"}}
	cmd, err := r.Command(nesGame())
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := []string{"emu", "/x/battle.nes", "Battletoads", "nes.nes", "aaa111bbb222"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("args = %q, want %q", cmd.Args, want)
		}
	}
}

func TestCommandUnknownPlaceholder(t *testing.T) {
	r := Recipe{Name: "r", Emulator: "emu", Args: []string{"{nope}"}}
	if _, err := r.Command(nesGame()); err == nil {
		t.Fatalf("expected unknown placeholder error")
	}
}

func TestLoadStrict(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"recipes": [{"name": "x", "bogus": 1}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Fatalf("expected unknown-field error")
	}
	noemu := filepath.Join(dir, "noemu.json")
	if err := os.WriteFile(noemu, []byte(`{"recipes": [{"name": "x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(noemu); err == nil {
		t.Fatalf("expected missing-emulator error")
	}
	missing := filepath.Join(dir, "missing.json")
	if got, err := Load(missing); err != nil || got != nil {
		t.Fatalf("missing file must yield nil,nil: %v %v", got, err)
	}
}

func TestExampleParses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "recipes.json")
	if err := os.WriteFile(p, ExampleJSON(), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := Load(p)
	if err != nil {
		t.Fatalf("example must load: %v", err)
	}
	if len(rs) != 2 {
		t.Fatalf("example has %d recipes", len(rs))
	}
}
