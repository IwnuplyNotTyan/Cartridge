// Package emulate maps ROMs onto emulator launch recipes.
//
// A recipe scopes an emulator to specific games, consoles or file formats
// and carries feature placeholders (cheats path, achievements and mods
// support) shown in the UI. Recipes live in a JSON file, e.g.
// ~/.config/cartridge/recipes.json; run with --recipe-example for a sample.
//
// Matching follows the list hierarchy: the first recipe from the top whose
// scope matches the game wins. A scopeless recipe matches everything, so
// defaults go last (move recipes with J/K in the settings tab).
package emulate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"cartridge/internal/rom"
)

// Placeholders supported in args, workdir and feature paths.
const (
	phRom     = "rom"     // ROM path on disk (archive when zipped)
	phInner   = "inner"   // entry inside the archive, if any
	phDir     = "dir"     // ROM directory
	phTitle   = "title"   // game title
	phID      = "id"      // stable full ID
	phShortID = "shortid" // 12-char ID prefix
	phConsole = "console" // console id (nes, gba, …)
	phExt     = "ext"     // ROM extension with dot (.nes)
	phEmuDir  = "emu_dir" // emulator binary directory
	phCore    = "core"    // selected core for the game console (see Cores)
)

// Cheats describes cheat-file support (path is a placeholder template).
type Cheats struct {
	Supported bool   `json:"supported"`
	Path      string `json:"path,omitempty"`
}

// Achievements describes achievements support (placeholder for now).
type Achievements struct {
	Supported bool   `json:"supported"`
	Note      string `json:"note,omitempty"`
}

// Mods describes mod support (path is a placeholder template).
type Mods struct {
	Supported bool   `json:"supported"`
	Path      string `json:"path,omitempty"`
}

// Recipe is one emulator launch template.
type Recipe struct {
	Name         string       `json:"name"`
	Consoles     []string     `json:"consoles,omitempty"`
	Extensions   []string     `json:"extensions,omitempty"`
	Games        []string     `json:"games,omitempty"`
	Emulator     string       `json:"emulator"`
	Args         []string     `json:"args,omitempty"`
	WorkDir      string       `json:"workdir,omitempty"`
	Cheats       Cheats       `json:"cheats,omitempty"`
	Achievements Achievements `json:"achievements,omitempty"`
	Mods         Mods         `json:"mods,omitempty"`
	Note         string       `json:"note,omitempty"`
	// Cores maps a console id to its core paths (libretro .so files, …).
	// The first entry is the default and expands as {core} in args,
	// workdir and feature paths. Needed by multi-system frontends
	// like RetroArch where each console needs its own core.
	Cores map[string][]string `json:"cores,omitempty"`
}

// CoresFor returns the core list for a console, lookup case-insensitive.
// An empty result means the recipe covers no core for this console.
func (r Recipe) CoresFor(console string) []string {
	for id, cores := range r.Cores {
		if strings.EqualFold(id, console) {
			return cores
		}
	}
	return nil
}

// CoreFor returns the default core for a console (the first listed).
// Lookup is case-insensitive; an error means the recipe covers no core
// for this console.
func (r Recipe) CoreFor(console string) (string, error) {
	for id, cores := range r.Cores {
		if !strings.EqualFold(id, console) {
			continue
		}
		for _, c := range cores {
			if strings.TrimSpace(c) != "" {
				return c, nil
			}
		}
		return "", fmt.Errorf("recipe %q has no cores for console %q", r.Name, console)
	}
	return "", fmt.Errorf("recipe %q has no cores for console %q", r.Name, console)
}

// file is the on-disk recipes document.
type file struct {
	Recipes []Recipe `json:"recipes"`
}

// Load reads path; a missing file yields no recipes and no error.
// Unknown fields are rejected to catch authoring typos early.
func Load(path string) ([]Recipe, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse recipes %s: %w", path, err)
	}
	for i := range f.Recipes {
		r := &f.Recipes[i]
		if strings.TrimSpace(r.Emulator) == "" {
			return nil, fmt.Errorf("recipes %s: recipe #%d has no emulator", path, i+1)
		}
		if strings.TrimSpace(r.Name) == "" {
			r.Name = r.Emulator
		}
	}
	return f.Recipes, nil
}

// Match picks the recipe for g by list hierarchy: the first recipe from
// the top whose scope (game ID/ShortID, console or extension) matches wins.
// A scopeless recipe matches everything, so defaults go last.
// Returns nil when nothing applies.
func Match(recipes []Recipe, g rom.Game) *Recipe {
	ext := gameExt(g)
	for i := range recipes {
		if recipeMatches(&recipes[i], g, ext) {
			return &recipes[i]
		}
	}
	return nil
}

// MatchAll returns every recipe matching g in list order.
// Useful to offer alternatives to the hierarchy winner.
func MatchAll(recipes []Recipe, g rom.Game) []*Recipe {
	var out []*Recipe
	ext := gameExt(g)
	for i := range recipes {
		if recipeMatches(&recipes[i], g, ext) {
			out = append(out, &recipes[i])
		}
	}
	return out
}

// recipeMatches reports whether r applies to g: a scopeless recipe matches
// everything, otherwise game ID/ShortID, console or extension must hit.
func recipeMatches(r *Recipe, g rom.Game, ext string) bool {
	if len(r.Games) == 0 && len(r.Consoles) == 0 && len(r.Extensions) == 0 {
		return true
	}
	for _, id := range r.Games {
		if id == g.ID || id == g.ShortID() {
			return true
		}
	}
	for _, c := range r.Consoles {
		if strings.EqualFold(c, g.ConsoleID) {
			return true
		}
	}
	for _, e := range r.Extensions {
		if normExt(e) == ext {
			return true
		}
	}
	return false
}

// Command builds the emulator process for g, expanding placeholders.
// Unknown {placeholders} are an error to help recipe authors.
func (r Recipe) Command(g rom.Game) (*exec.Cmd, error) {
	if strings.TrimSpace(r.Emulator) == "" {
		return nil, fmt.Errorf("recipe %q has no emulator", r.Name)
	}
	vars := map[string]string{
		phRom:     g.Path,
		phInner:   g.Inner,
		phDir:     filepath.Dir(g.Path),
		phTitle:   g.Title,
		phID:      g.ID,
		phShortID: g.ShortID(),
		phConsole: g.ConsoleID,
		phExt:     gameExt(g),
		phEmuDir:  emuDir(r.Emulator),
	}
	if len(r.Cores) > 0 {
		core, err := r.CoreFor(g.ConsoleID)
		if err != nil {
			return nil, err
		}
		vars[phCore] = core
	}
	args := make([]string, len(r.Args))
	for i, a := range r.Args {
		s, err := expand(a, vars)
		if err != nil {
			return nil, fmt.Errorf("recipe %q arg %d: %w", r.Name, i+1, err)
		}
		args[i] = s
	}
	cmd := exec.Command(r.Emulator, args...)
	if strings.TrimSpace(r.WorkDir) != "" {
		wd, err := expand(r.WorkDir, vars)
		if err != nil {
			return nil, fmt.Errorf("recipe %q workdir: %w", r.Name, err)
		}
		cmd.Dir = wd
	}
	return cmd, nil
}

// Expand resolves a placeholder template (cheats/mods paths) for display.
func (r Recipe) Expand(tmpl string, g rom.Game) (string, error) {
	vars := map[string]string{
		phRom:     g.Path,
		phInner:   g.Inner,
		phDir:     filepath.Dir(g.Path),
		phTitle:   g.Title,
		phID:      g.ID,
		phShortID: g.ShortID(),
		phConsole: g.ConsoleID,
		phExt:     gameExt(g),
		phEmuDir:  emuDir(r.Emulator),
	}
	if len(r.Cores) > 0 {
		core, err := r.CoreFor(g.ConsoleID)
		if err != nil {
			return "", err
		}
		vars[phCore] = core
	}
	return expand(tmpl, vars)
}

// gameExt is the ROM extension with dot, lowercase; the inner entry wins
// for archives (a .zip of .nes matches ".nes").
func gameExt(g rom.Game) string {
	p := g.Path
	if g.Inner != "" {
		p = g.Inner
	}
	ext := strings.ToLower(filepath.Ext(p))
	if ext == "" {
		return ""
	}
	return ext
}

// normExt lowercases an extension and ensures the leading dot.
func normExt(e string) string {
	e = strings.ToLower(strings.TrimSpace(e))
	if e == "" {
		return ""
	}
	return "." + strings.TrimPrefix(e, ".")
}

var phRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expand(tmpl string, vars map[string]string) (string, error) {
	var missing string
	out := phRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := phRe.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok {
			missing = name
			return m
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("unknown placeholder {%s}", missing)
	}
	return out, nil
}

// LookupBinary resolves the emulator binary to a full path.
// It reports false when the binary is not found in PATH and is not
// an existing file (for paths containing a slash or backslash).
func LookupBinary(emulator string) (string, bool) {
	emulator = strings.TrimSpace(emulator)
	if emulator == "" {
		return "", false
	}
	if p, err := exec.LookPath(emulator); err == nil {
		return p, true
	}
	if strings.ContainsAny(emulator, `/\`) {
		if st, err := os.Stat(emulator); err == nil && !st.IsDir() {
			return emulator, true
		}
	}
	return "", false
}

func emuDir(emulator string) string {
	if p, ok := LookupBinary(emulator); ok {
		return filepath.Dir(p)
	}
	if dir := filepath.Dir(emulator); dir != "." && dir != "" {
		return dir
	}
	return "."
}

// SaveRecipes writes base recipes to path atomically, preserving list order
// (score ties in matching resolve to the first recipe).
// Patches are never touched; use StorePatch/DropPatches for those.
func SaveRecipes(path string, recipes []Recipe) error {
	if path == "" {
		return fmt.Errorf("empty recipes path")
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return fmt.Errorf("recipes path is a directory: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file{Recipes: recipes}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Example returns a commented-shape sample document (notes describe fields).
func Example() []Recipe {
	return []Recipe{
		{
			Name:       "mesen-nes",
			Consoles:   []string{"nes"},
			Extensions: []string{".nes", ".fds"},
			Emulator:   "mesen",
			Args:       []string{"{rom}"},
			Cheats:     Cheats{Supported: true, Path: "{emu_dir}/cheats/{title}.cht"},
			Mods:       Mods{Supported: false},
			Note:       "standalone NES emulator",
		},
		{
			Name:         "retroarch-gba",
			Consoles:     []string{"gba"},
			Emulator:     "retroarch",
			Args:         []string{"-L", "mgba_libretro.so", "{rom}"},
			Cheats:       Cheats{Supported: true, Path: "{emu_dir}/cheats/{shortid}.cht"},
			Achievements: Achievements{Supported: true, Note: "via RetroAchievements login"},
			Mods:         Mods{Supported: true, Path: "{dir}/mods/{title}"},
		},
		{
			// Multi-system frontend: one core per console, several where
			// there is a choice (the first listed is the default {core}).
			Name:     "retroarch-cores",
			Consoles: []string{"nes", "snes", "gb", "gbc", "gba", "genesis", "gamegear", "n64"},
			Emulator: "retroarch",
			Args:     []string{"-L", "{core}", "{rom}"},
			Cores: map[string][]string{
				"nes":      {"nestopia_libretro.so", "mesen_libretro.so"},
				"snes":     {"snes9x_libretro.so"},
				"gb":       {"gambatte_libretro.so", "sameboy_libretro.so"},
				"gbc":      {"gambatte_libretro.so", "sameboy_libretro.so"},
				"gba":      {"mgba_libretro.so"},
				"genesis":  {"genesis_plus_gx_libretro.so"},
				"gamegear": {"genesis_plus_gx_libretro.so"},
				"n64":      {"mupen64plus_next_libretro.so", "parallel_n64_libretro.so"},
			},
			Cheats:       Cheats{Supported: true, Path: "{emu_dir}/cheats/{shortid}.cht"},
			Achievements: Achievements{Supported: true, Note: "via RetroAchievements login"},
			Note:         "multi-system frontend, per-console cores",
		},
	}
}

// ExampleJSON renders Example as a pretty recipes document.
func ExampleJSON() []byte {
	data, _ := json.MarshalIndent(file{Recipes: Example()}, "", "  ")
	return append(data, '\n')
}
