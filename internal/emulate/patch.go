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
	Recipe       string               `json:"recipe"`
	Emulator     *string              `json:"emulator,omitempty"`
	Open         *bool                `json:"open,omitempty"`
	Args         *[]string            `json:"args,omitempty"`
	WorkDir      *string              `json:"workdir,omitempty"`
	Consoles     *[]string            `json:"consoles,omitempty"`
	Extensions   *[]string            `json:"extensions,omitempty"`
	Games        *[]string            `json:"games,omitempty"`
	Cheats       *Cheats              `json:"cheats,omitempty"`
	Achievements *Achievements        `json:"achievements,omitempty"`
	Mods         *Mods                `json:"mods,omitempty"`
	Note         *string              `json:"note,omitempty"`
	Cores        *map[string][]string `json:"cores,omitempty"`
	CoreNames    *map[string]string   `json:"core_names,omitempty"`
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
		if p.Open != nil {
			r.Open = p.Open
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
		if p.Cores != nil {
			cp := make(map[string][]string, len(*p.Cores))
			for id, cores := range *p.Cores {
				cp[id] = append([]string(nil), cores...)
			}
			r.Cores = cp
		}
		if p.CoreNames != nil {
			np := make(map[string]string, len(*p.CoreNames))
			for id, name := range *p.CoreNames {
				np[id] = name
			}
			r.CoreNames = np
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

// AppliedPatches returns the subset of patches targeting a known base
// recipe name — the ones ApplyPatches actually applies. Patches pointing
// at unknown recipes are skipped and must not be counted.
func AppliedPatches(base []Recipe, patches []RecipePatch) []RecipePatch {
	known := make(map[string]bool, len(base))
	for _, r := range base {
		known[r.Name] = true
	}
	var out []RecipePatch
	for _, p := range patches {
		if known[p.Recipe] {
			out = append(out, p)
		}
	}
	return out
}

// UpsertPatch inserts p into patches or replaces the last patch that
// targets the same recipe and overrides the same fields. Fields are
// compared by non-nil pointers: two patches conflict when they both set
// the same field. A fully disjoint patch is appended, so several patches
// per recipe can coexist (e.g. one for emulator, one for args).
func UpsertPatch(patches []RecipePatch, p RecipePatch) []RecipePatch {
	for i := range patches {
		if patches[i].Recipe == p.Recipe && samePatchFields(patches[i], p) {
			patches[i] = p
			return patches
		}
	}
	return append(patches, p)
}

// samePatchFields reports whether a and b override the same set of fields.
func samePatchFields(a, b RecipePatch) bool {
	return (a.Emulator != nil) == (b.Emulator != nil) &&
		(a.Open != nil) == (b.Open != nil) &&
		(a.Args != nil) == (b.Args != nil) &&
		(a.WorkDir != nil) == (b.WorkDir != nil) &&
		(a.Consoles != nil) == (b.Consoles != nil) &&
		(a.Extensions != nil) == (b.Extensions != nil) &&
		(a.Games != nil) == (b.Games != nil) &&
		(a.Cheats != nil) == (b.Cheats != nil) &&
		(a.Achievements != nil) == (b.Achievements != nil) &&
		(a.Mods != nil) == (b.Mods != nil) &&
		(a.Note != nil) == (b.Note != nil) &&
		(a.Cores != nil) == (b.Cores != nil) &&
		(a.CoreNames != nil) == (b.CoreNames != nil)
}

// RemovePatches drops every patch targeting the named recipe.
func RemovePatches(patches []RecipePatch, name string) []RecipePatch {
	out := patches[:0]
	for _, p := range patches {
		if p.Recipe != name {
			out = append(out, p)
		}
	}
	return out
}

// patchFileName sanitizes a recipe name for use as a file name inside
// a patches directory.
func patchFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		default:
			b.WriteRune('_')
		}
	}
	s := strings.Trim(b.String(), "._")
	if s == "" {
		s = "patch"
	}
	return s + ".json"
}

// StorePatch upserts one patch and persists it without touching recipes.
// When patchesPath is a directory, the patch is stored as
// <dir>/<recipe>.json; otherwise the whole overlay file is rewritten.
// It returns the updated in-memory patch list.
func StorePatch(patchesPath string, patches []RecipePatch, p RecipePatch) ([]RecipePatch, error) {
	if strings.TrimSpace(p.Recipe) == "" {
		return patches, fmt.Errorf("patch has no recipe target")
	}
	if strings.TrimSpace(patchesPath) == "" {
		return patches, fmt.Errorf("empty patches path")
	}
	updated := UpsertPatch(append([]RecipePatch(nil), patches...), p)
	if st, err := os.Stat(patchesPath); err == nil && st.IsDir() {
		if err := os.MkdirAll(patchesPath, 0o755); err != nil {
			return patches, err
		}
		file := filepath.Join(patchesPath, patchFileName(p.Recipe))
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return patches, err
		}
		tmp := file + ".tmp"
		if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
			return patches, err
		}
		if err := os.Rename(tmp, file); err != nil {
			return patches, err
		}
		return updated, nil
	}
	if err := SavePatches(patchesPath, updated); err != nil {
		return patches, err
	}
	return updated, nil
}

// DropPatches removes every patch for the named recipe and persists
// the result. For a directory patches path each <recipe>.json file is
// removed (both plain and {"patches":[...]} shapes); for a file path
// the overlay is rewritten without the recipe.
func DropPatches(patchesPath string, patches []RecipePatch, name string) ([]RecipePatch, error) {
	updated := RemovePatches(append([]RecipePatch(nil), patches...), name)
	if strings.TrimSpace(patchesPath) == "" {
		return updated, nil
	}
	if st, err := os.Stat(patchesPath); err == nil && st.IsDir() {
		target := filepath.Join(patchesPath, patchFileName(name))
		_ = os.Remove(target)
		// Also sweep files that contain the recipe inside a patches array.
		if entries, err := os.ReadDir(patchesPath); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				fp := filepath.Join(patchesPath, e.Name())
				ps, err := loadPatchFile(fp)
				if err != nil || len(ps) == 0 {
					continue
				}
				kept := RemovePatches(ps, name)
				if len(kept) == len(ps) {
					continue
				}
				if len(kept) == 0 {
					_ = os.Remove(fp)
					continue
				}
				data, err := json.MarshalIndent(patchFile{Patches: kept}, "", "  ")
				if err != nil {
					continue
				}
				_ = os.WriteFile(fp, append(data, '\n'), 0o644)
			}
		}
		return updated, nil
	}
	if err := SavePatches(patchesPath, updated); err != nil {
		return patches, err
	}
	return updated, nil
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
