package emulate

import (
	"os"
	"path/filepath"
	"strings"
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

func TestMatchAllOrder(t *testing.T) {
	byExt := Recipe{Name: "by-ext", Extensions: []string{"nes"}, Emulator: "e1"}
	byConsole := Recipe{Name: "by-console", Consoles: []string{"nes"}, Emulator: "e2"}
	byGame := Recipe{Name: "by-game", Games: []string{"aaa111bbb222"}, Emulator: "e3"}
	def := Recipe{Name: "default", Emulator: "e0"}

	got := MatchAll([]Recipe{byExt, byConsole, byGame, def}, nesGame())
	want := []string{"by-ext", "by-console", "by-game", "default"}
	if len(got) != len(want) {
		t.Fatalf("MatchAll = %d recipes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("MatchAll[%d] = %q, want %q", i, got[i].Name, want[i])
		}
	}
	other := nesGame()
	other.ConsoleID = "snes"
	other.Path = "/x/b.sfc"
	if got := MatchAll([]Recipe{byExt, byConsole}, other); len(got) != 0 {
		t.Fatalf("MatchAll must be empty, got %v", got)
	}
}

func TestCoresFor(t *testing.T) {
	r := Recipe{Name: "ra", Cores: map[string][]string{
		"nes": {"a.so", "b.so"},
	}}
	if got := r.CoresFor("NES"); len(got) != 2 || got[1] != "b.so" {
		t.Fatalf("CoresFor must ignore case: %+v", got)
	}
	if got := r.CoresFor("gba"); len(got) != 0 {
		t.Fatalf("CoresFor(gba) must be empty: %+v", got)
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
	cmd, err := r.Command(nesGame(), false)
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

func TestCommandOpenLauncher(t *testing.T) {
	on, off := true, false
	// A recipe with no explicit setting inherits the global default.
	r := Recipe{Name: "openemu", Emulator: "OpenEmu", Args: []string{"{rom}"}}
	cmd, err := r.Command(nesGame(), true)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := []string{"open", "-W", "-a", "OpenEmu", "/x/battle.nes"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("args = %q, want %q", cmd.Args, want)
		}
	}
	cmd, err = r.Command(nesGame(), false)
	if err != nil {
		t.Fatalf("Command direct: %v", err)
	}
	if cmd.Args[0] != "OpenEmu" {
		t.Fatalf("direct args = %q", cmd.Args)
	}

	// Forced on/off wins over the global default.
	r.Open = &on
	cmd, err = r.Command(nesGame(), false)
	if err != nil {
		t.Fatalf("Command forced open: %v", err)
	}
	if cmd.Args[0] != "open" {
		t.Fatalf("forced open args = %q", cmd.Args)
	}
	r.Open = &off
	cmd, err = r.Command(nesGame(), true)
	if err != nil {
		t.Fatalf("Command forced direct: %v", err)
	}
	if cmd.Args[0] != "OpenEmu" {
		t.Fatalf("forced direct args = %q", cmd.Args)
	}
}

func TestLoadOpenRecipe(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "open.json")
	if err := os.WriteFile(p, []byte(`{"name":"OpenEmu","emulator":"OpenEmu","open":true,"args":["{rom}"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Open == nil || !*rs[0].Open || rs[0].Emulator != "OpenEmu" {
		t.Fatalf("loaded = %+v", rs)
	}
	if err := os.WriteFile(p, []byte(`{"name":"broken"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatalf("recipe without emulator must fail")
	}
}

func TestCommandUnknownPlaceholder(t *testing.T) {
	r := Recipe{Name: "r", Emulator: "emu", Args: []string{"{nope}"}}
	if _, err := r.Command(nesGame(), false); err == nil {
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

func TestLoadSingleRecipeShapes(t *testing.T) {
	dir := t.TempDir()
	// One recipe object: the downloadable single-file shape.
	single := filepath.Join(dir, "mesen-nes.json")
	if err := os.WriteFile(single, []byte(`{"name": "mesen-nes", "emulator": "mesen", "args": ["{rom}"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(single)
	if err != nil || len(got) != 1 || got[0].Name != "mesen-nes" {
		t.Fatalf("single object load = %+v, %v", got, err)
	}
	// Bare array shape.
	arr := filepath.Join(dir, "arr.json")
	if err := os.WriteFile(arr, []byte(`[{"name": "a", "emulator": "e1"}, {"name": "b", "emulator": "e2"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = Load(arr)
	if err != nil || len(got) != 2 || got[1].Name != "b" {
		t.Fatalf("array load = %+v, %v", got, err)
	}
	// Empty name defaults to the emulator binary.
	noname := filepath.Join(dir, "noname.json")
	if err := os.WriteFile(noname, []byte(`{"emulator": "e1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = Load(noname)
	if err != nil || len(got) != 1 || got[0].Name != "e1" {
		t.Fatalf("default name = %+v, %v", got, err)
	}
}

func TestLoadRecipesDir(t *testing.T) {
	dir := t.TempDir()
	recipes := filepath.Join(dir, "recipes")
	if err := ExportRecipes(recipes, []Recipe{
		{Name: "b-second", Emulator: "e2"},
		{Name: "a-first", Emulator: "e1"},
	}); err != nil {
		t.Fatal(err)
	}
	// A stray non-JSON file must be ignored.
	if err := os.WriteFile(filepath.Join(recipes, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(recipes)
	if err != nil {
		t.Fatal(err)
	}
	// Directory order is filename-sorted: a-first.json wins.
	if len(got) != 2 || got[0].Name != "a-first" || got[1].Name != "b-second" {
		t.Fatalf("dir load order broken: %+v", got)
	}
	// Mixed shapes in one dir: document + single object.
	mixed := t.TempDir()
	if err := SaveRecipes(filepath.Join(mixed, "00-doc.json"), []Recipe{{Name: "doc", Emulator: "e0"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecipe(mixed, Recipe{Name: "solo", Emulator: "e1"}); err != nil {
		t.Fatal(err)
	}
	got, err = Load(mixed)
	if err != nil || len(got) != 2 {
		t.Fatalf("mixed dir load = %+v, %v", got, err)
	}
	if got[0].Name != "doc" || got[1].Name != "solo" {
		t.Fatalf("mixed dir order broken: %+v", got)
	}
}

func TestSaveRecipeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := Recipe{Name: "mesen-nes", Consoles: []string{"nes"}, Emulator: "mesen", Args: []string{"{rom}"}}
	// Into a directory: lands as <dir>/mesen-nes.json.
	if err := SaveRecipe(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := Load(filepath.Join(dir, "mesen-nes.json"))
	if err != nil || len(got) != 1 || got[0].Emulator != "mesen" {
		t.Fatalf("dir save/load = %+v, %v", got, err)
	}
	// Explicit file path: single object shape, still loadable.
	fp := filepath.Join(dir, "custom.json")
	if err := SaveRecipe(fp, r); err != nil {
		t.Fatal(err)
	}
	got, err = Load(fp)
	if err != nil || len(got) != 1 || got[0].Name != "mesen-nes" {
		t.Fatalf("file save/load = %+v, %v", got, err)
	}
	if got := RecipeFileName("retroarch cores!"); got != "retroarch-cores.json" {
		t.Fatalf("RecipeFileName = %q", got)
	}
	if err := SaveRecipe("", r); err == nil {
		t.Fatalf("expected error for empty path")
	}
	if err := SaveRecipe(dir, Recipe{Name: "noemu"}); err == nil {
		t.Fatalf("expected error for missing emulator")
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
	cmd, err := r.Command(nesGame(), false)
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
	if _, err := r.Command(gba, false); err == nil {
		t.Fatalf("Command must fail without a core for gba")
	}
	// {core} without any Cores map stays an unknown placeholder.
	plain := Recipe{Name: "plain", Emulator: "retroarch", Args: []string{"-L", "{core}", "{rom}"}}
	if _, err := plain.Command(nesGame(), false); err == nil {
		t.Fatalf("Command must fail on {core} without cores")
	}
}

func TestCoreNamePlaceholder(t *testing.T) {
	r := Recipe{Name: "ra", Emulator: "retroarch",
		Args:  []string{"-L", "{core}", "--cfg", "{core_name}.cfg"},
		Cores: map[string][]string{"nes": {"/usr/lib/nestopia_libretro.so"}}}
	cmd, err := r.Command(nesGame(), false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"retroarch", "-L", "/usr/lib/nestopia_libretro.so", "--cfg", "nestopia_libretro.cfg"}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("args = %q, want %q", cmd.Args, want)
		}
	}
	// {core_name} without any Cores map stays an unknown placeholder.
	plain := Recipe{Name: "plain", Emulator: "retroarch", Args: []string{"{core_name}"}}
	if _, err := plain.Command(nesGame(), false); err == nil {
		t.Fatalf("Command must fail on {core_name} without cores")
	}
}

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	r := Recipe{Name: "ra", Emulator: "retroarch", Args: []string{"{rom}"}}
	got, err := r.Expand("~/.config/retroarch/cheats/{title}.cht", nesGame())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config/retroarch/cheats/Battletoads.cht")
	if got != want {
		t.Fatalf("tilde = %q, want %q", got, want)
	}
	// Non-tilde templates pass through untouched.
	got, err = r.Expand("{emu_dir}/cheats/{title}.cht", nesGame())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "/cheats/Battletoads.cht") {
		t.Fatalf("plain template broken: %q", got)
	}
}

func TestCoreNameFor(t *testing.T) {
	r := Recipe{Name: "ra",
		Cores: map[string][]string{
			"nes":  {"/usr/lib/nestopia_libretro.so"},
			"snes": {"/usr/lib/snes9x_libretro.so"},
		},
		CoreNames: map[string]string{"gba": "mGBA", "snes": " "}}
	// Explicit display name wins, case-insensitively.
	if got, ok := r.CoreNameFor("GBA"); !ok || got != "mGBA" {
		t.Fatalf("CoreNameFor(gba) = %q, %v", got, ok)
	}
	// Blank entry falls back to the core file stem.
	if got, ok := r.CoreNameFor("snes"); !ok || got != "snes9x_libretro" {
		t.Fatalf("CoreNameFor(snes) = %q, %v", got, ok)
	}
	// Neither source covers the console.
	if _, ok := r.CoreNameFor("n64"); ok {
		t.Fatalf("CoreNameFor(n64) must miss")
	}
	// Display names work without any Cores map.
	bare := Recipe{Name: "bare", CoreNames: map[string]string{"nes": "Nestopia"}}
	if got, ok := bare.CoreNameFor("nes"); !ok || got != "Nestopia" {
		t.Fatalf("CoreNameFor without cores = %q, %v", got, ok)
	}
	// ...but {core} still needs real cores.
	bareCmd := Recipe{Name: "bare", Emulator: "retroarch",
		Args:      []string{"-L", "{core}", "{rom}"},
		CoreNames: map[string]string{"nes": "Nestopia"}}
	if _, err := bareCmd.Command(nesGame(), false); err == nil {
		t.Fatalf("Command must fail on {core} without cores")
	}
	cmd, err := (Recipe{Name: "bare", Emulator: "retroarch",
		Args:      []string{"{core_name}"},
		CoreNames: map[string]string{"nes": "Nestopia"}}).Command(nesGame(), false)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[1] != "Nestopia" {
		t.Fatalf("{core_name} without cores = %q", cmd.Args)
	}
}

func TestPatchCoreNamesApply(t *testing.T) {
	base := []Recipe{{Name: "ra", Emulator: "retroarch",
		Cores: map[string][]string{"nes": {"a.so"}}}}
	names := map[string]string{"nes": "Nestopia"}
	patched := ApplyPatches(base, []RecipePatch{{Recipe: "ra", CoreNames: &names}})
	if got := patched[0].CoreNames["nes"]; got != "Nestopia" {
		t.Fatalf("core_names patch broken: %+v", patched[0].CoreNames)
	}
	if len(base[0].CoreNames) != 0 {
		t.Fatalf("base must stay untouched: %+v", base[0].CoreNames)
	}
	if got, ok := patched[0].CoreNameFor("nes"); !ok || got != "Nestopia" {
		t.Fatalf("patched CoreNameFor = %q, %v", got, ok)
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
	cmd, err := patched[0].Command(nesGame(), false)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[2] != "b.so" {
		t.Fatalf("patched core not used: %q", cmd.Args)
	}
}
