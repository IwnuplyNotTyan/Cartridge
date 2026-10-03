package emulate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RecipePatch is an overlay for one base recipe matched by name.
// Only non-nil fields are applied; the base recipes file is never rewritten.
// This guarantees patches never replace a recipe, they only overlay it.
type RecipePatch struct {
	Recipe       string        `json:"recipe"`
	Emulator     *string       `json:"emulator,omitempty"`
	Args         *[]string     `json:"args,omitempty"`
	WorkDir      *string       `json:"workdir,omitempty"`
	Consoles     *[]string     `json:"consoles,omitempty"`
	Extensions   *[]string     `json:"extensions,omitempty"`
	Games        *[]string     `json:"games,omitempty"`
	Cheats       *Cheats       `json:"cheats,omitempty"`
	Achievements *Achievements `json:"achievements,omitempty"`
	Mods         *Mods         `json:"mods,omitempty"`
	Note         *string       `json:"note,omitempty"`
}

type patchFile struct {
	Patches []RecipePatch `json:"patches"`
}

// LoadPatches reads path which may be a JSON file or a directory of JSON files.
// A missing path yields no patches and no error. Each file accepts either
// {"patches": [...]} or a single patch object or a bare [...] array.
// Unknown fields are rejected to catch authoring typos early.
func LoadPatches(path string) ([]RecipePatch, error) {
	if path == "" {
		return nil, nil
	}
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		var out []RecipePatch
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := strings.ToLower(e.Name())
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			ps, err := loadPatchFile(filepath.Join(path, e.Name()))
			if err != nil {
				return nil, err
			}
			out = append(out, ps...)
		}
		return out, nil
	}
	return loadPatchFile(path)
}

func loadPatchFile(path string) ([]RecipePatch, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f patchFile
	if err := dec.Decode(&f); err == nil {
		return checkPatches(f.Patches, path)
	}
	dec2 := json.NewDecoder(bytes.NewReader(data))
	dec2.DisallowUnknownFields()
	var arr []RecipePatch
	if err := dec2.Decode(&arr); err == nil {
		return checkPatches(arr, path)
	}
	dec3 := json.NewDecoder(bytes.NewReader(data))
	dec3.DisallowUnknownFields()
	var single RecipePatch
	if err := dec3.Decode(&single); err != nil {
		return nil, fmt.Errorf("parse patches %s: %w", path, err)
	}
	return checkPatches([]RecipePatch{single}, path)
}

func checkPatches(ps []RecipePatch, path string) ([]RecipePatch, error) {
	for i := range ps {
		if strings.TrimSpace(ps[i].Recipe) == "" {
			return nil, fmt.Errorf("patches %s: patch #%d has no recipe target", path, i+1)
		}
	}
	return ps, nil
}

// ApplyPatches overlays patches onto a copy of base recipes and returns
// the effective list. Base recipes are never mutated. Patches targeting
// an unknown recipe name are skipped. When several patches target the same
// recipe they apply in order.
func ApplyPatches(base []Recipe, patches []RecipePatch) []Recipe {
	out := make([]Recipe, len(base))
	copy(out, base)
	byName := map[string]int{}
	for i := range out {
		if _, ok := byName[out[i].Name]; !ok {
			byName[out[i].Name] = i
		}
	}
	for _, p := range patches {
		idx, ok := byName[p.Recipe]
		if !ok {
			continue
		}
		r := &out[idx]
		if p.Emulator != nil {
			r.Emulator = *p.Emulator
		}
		if p.Args != nil {
			r.Args = append([]string(nil), (*p.Args)...)
		}
		if p.WorkDir != nil {
			r.WorkDir = *p.WorkDir
		}
		if p.Consoles != nil {
			r.Consoles = append([]string(nil), (*p.Consoles)...)
		}
		if p.Extensions != nil {
			r.Extensions = append([]string(nil), (*p.Extensions)...)
		}
		if p.Games != nil {
			r.Games = append([]string(nil), (*p.Games)...)
		}
		if p.Cheats != nil {
			r.Cheats = *p.Cheats
		}
		if p.Achievements != nil {
			r.Achievements = *p.Achievements
		}
		if p.Mods != nil {
			r.Mods = *p.Mods
		}
		if p.Note != nil {
			r.Note = *p.Note
		}
	}
	return out
}

// PatchedNames reports which base recipe names have at least one patch.
func PatchedNames(base []Recipe, patches []RecipePatch) map[string]bool {
	known := map[string]bool{}
	for _, r := range base {
		known[r.Name] = false
	}
	for _, p := range patches {
		if _, ok := known[p.Recipe]; ok {
			known[p.Recipe] = true
		}
	}
	return known
}

// PatchesFor returns patches targeting the named recipe, in order.
func PatchesFor(patches []RecipePatch, name string) []RecipePatch {
	var out []RecipePatch
	for _, p := range patches {
		if p.Recipe == name {
			out = append(out, p)
		}
	}
	return out
}

// SavePatches writes patches to path atomically without touching recipes.
// The directory form is not writable: path must be a file path.
func SavePatches(path string, patches []RecipePatch) error {
	if path == "" {
		return fmt.Errorf("empty patches path")
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return fmt.Errorf("patches path is a directory: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(patchFile{Patches: patches}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ExamplePatches renders a sample patches document.
func ExamplePatches() []byte {
	emu := "mesen"
	args := []string{"{rom}", "--patch", "{dir}/patches/{title}.ips"}
	data, _ := json.MarshalIndent(patchFile{Patches: []RecipePatch{
		{Recipe: "mesen-nes", Emulator: &emu, Args: &args},
	}}, "", "  ")
	return append(data, '\n')
}
