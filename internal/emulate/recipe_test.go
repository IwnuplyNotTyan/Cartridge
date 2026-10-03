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

func TestMatchHierarchy(t *testing.T) {
	byExt := Recipe{Name: "by-ext", Extensions: []string{"nes"}, Emulator: "e1"}
	byConsole := Recipe{Name: "by-console", Consoles: []string{"nes"}, Emulator: "e2"}
	byGame := Recipe{Name: "by-game", Games: []string{"aaa111bbb222"}, Emulator: "e3"}
	def := Recipe{Name: "default", Emulator: "e0"}

	// List order beats scope weight: the first match from the top wins.
	if got := Match([]Recipe{byExt, byConsole, byGame, def}, nesGame()); got.Name != "by-ext" {
		t.Fatalf("top recipe must win, got %v", got)
	}
	if got := Match([]Recipe{byGame, byConsole, byExt}, nesGame()); got.Name != "by-game" {
		t.Fatalf("top recipe must win, got %v", got)
	}
	// A scopeless recipe matches everything, so on top it shadows all.
	if got := Match([]Recipe{def, byGame}, nesGame()); got.Name != "default" {
		t.Fatalf("top default must win, got %v", got)
	}
	// Default placed last still works as a fallback.
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

func TestSaveRecipesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "recipes.json")
	want := []Recipe{
		{Name: "r1", Consoles: []string{"nes"}, Emulator: "e1"},
		{Name: "r2", Consoles: []string{"snes"}, Emulator: "e2"},
	}
	if err := SaveRecipes(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "r1" || got[1].Name != "r2" {
		t.Fatalf("order not preserved: %+v", got)
	}
	if err := SaveRecipes("", want); err == nil {
		t.Fatalf("expected error for empty path")
	}
	if err := SaveRecipes(dir, want); err == nil {
		t.Fatalf("expected error for directory path")
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

func TestCoreFor(t *testing.T) {
	r := Recipe{Name: "ra", Cores: map[string][]string{
		"nes":  {"nestopia_libretro.so", "mesen_libretro.so"},
		"snes": {""},
	}}
	if got, err := r.CoreFor("nes"); err != nil || got != "nestopia_libretro.so" {
		t.Fatalf("CoreFor(nes) = %q, %v", got, err)
	}
	if got, err := r.CoreFor("NES"); err != nil || got != "nestopia_libretro.so" {
		t.Fatalf("CoreFor must ignore case: %q, %v", got, err)
	}
	if _, err := r.CoreFor("gba"); err == nil {
		t.Fatalf("CoreFor(gba) must fail")
	}
	if _, err := r.CoreFor("snes"); err == nil {
		t.Fatalf("CoreFor(snes) must fail on empty list")
	}
}

func TestCommandCorePlaceholder(t *testing.T) {
	r := Recipe{Name: "ra", Emulator: "retroarch",
		Args:  []string{"-L", "{core}", "{rom}"},
		Cores: map[string][]string{"nes": {"nestopia_libretro.so", "mesen_libretro.so"}}}
	cmd, err := r.Command(nesGame())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"retroarch", "-L", "nestopia_libretro.so", "/x/battle.nes"}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("args = %q, want %q", cmd.Args, want)
		}
	}
	// Console without a core fails loudly instead of launching bare.
	gba := nesGame()
	gba.ConsoleID = "gba"
	if _, err := r.Command(gba); err == nil {
		t.Fatalf("Command must fail without a core for gba")
	}
	// {core} without any Cores map stays an unknown placeholder.
	plain := Recipe{Name: "plain", Emulator: "retroarch", Args: []string{"-L", "{core}", "{rom}"}}
	if _, err := plain.Command(nesGame()); err == nil {
		t.Fatalf("Command must fail on {core} without cores")
	}
}

func TestPatchCoresApply(t *testing.T) {
	base := []Recipe{{Name: "ra", Emulator: "retroarch",
		Args:  []string{"-L", "{core}", "{rom}"},
		Cores: map[string][]string{"nes": {"a.so"}}}}
	cores := map[string][]string{"nes": {"b.so", "c.so"}, "snes": {"d.so"}}
	patched := ApplyPatches(base, []RecipePatch{{Recipe: "ra", Cores: &cores}})
	if got := patched[0].Cores["nes"]; len(got) != 2 || got[0] != "b.so" {
		t.Fatalf("cores patch broken: %+v", patched[0].Cores)
	}
	if got := patched[0].Cores["snes"]; len(got) != 1 {
		t.Fatalf("cores patch broken: %+v", patched[0].Cores)
	}
	if len(base[0].Cores["nes"]) != 1 || base[0].Cores["nes"][0] != "a.so" {
		t.Fatalf("base must stay untouched: %+v", base[0].Cores)
	}
	cmd, err := patched[0].Command(nesGame())
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[2] != "b.so" {
		t.Fatalf("patched core not used: %q", cmd.Args)
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
	if len(rs) != 3 {
		t.Fatalf("example has %d recipes", len(rs))
	}
}
