// Package mods finds ROM patch files (mods) for a game.
//
// Supported files are soft-patches the emulator applies on top of the
// base ROM: .ips, .bps, .ups and friends. They live in a shared mods dir
// (or in the recipe's mods path) and match a game by file stem, the same
// fuzzy way cheats do: "Battletoads (U).nes" finds "Battletoads.ips".
// A per-game subfolder also matches: mods/Battletoads/*.ips.
//
// A pile match alone is invisible to emulators: RetroArch autopatches
// only a file sitting beside the ROM and named exactly like it
// (game.sfc + game.ips), so the UI deploys pile files there via
// PlanDeploy/Deploy. Several mods stack through numbered slots
// (game.ips, game.ips1, …), which RetroArch applies in order.
//
// Hard-patched instances (one ROM copy per game+mod) are built by
// ApplyPatch/EnsureInstance on top of github.com/olsonb97/RomPatcher.go
// (MIT): patching a copy instead of relying on emulator softpatching,
// so mods play on any emulator or core.
//
// Mod files are never ROMs: rom.ScanDir skips them via IsModPath, so a
// downloaded .ips never shows up as a game.
package mods

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Extensions is the canonical list of mod/patch file extensions: everything
// RomPatcher.go applies (IPS/IPS32/EBP, UPS, BPS, APS, RUP, PPF, BSDIFF,
// PMSR, VCDIFF) plus the generic .patch. Files with these extensions are
// listed as mods and never detected as ROMs.
var Extensions = []string{
	".ips", ".ips32", ".ebp", ".ups", ".bps", ".aps",
	".rup", ".ppf", ".bdf", ".bspatch", ".mod",
	".xdelta", ".vcdiff", ".dps", ".patch",
}

var extSet map[string]bool

func init() {
	extSet = make(map[string]bool, len(Extensions))
	for _, e := range Extensions {
		extSet[strings.ToLower(e)] = true
	}
}

// IsModExtension reports whether ext (with or without dot) is a mod file.
func IsModExtension(ext string) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return false
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return extSet[ext]
}

// IsModPath reports whether path looks like a mod file.
func IsModPath(path string) bool {
	return IsModExtension(filepath.Ext(path))
}

// Mod is one patch file on disk.
type Mod struct {
	Name string `json:"name"` // file name with extension
	Path string `json:"path"` // full path
	Size int64  `json:"size"`
	Ext  string `json:"ext"` // lowercase extension with dot
}

// ListDir returns every mod file under dir, recursively, sorted by path.
// A missing dir yields an empty list and no error.
func ListDir(dir string) ([]Mod, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	var out []Mod
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsModPath(path) {
			return nil
		}
		var size int64
		if st, err := d.Info(); err == nil {
			size = st.Size()
		}
		out = append(out, Mod{
			Name: filepath.Base(path),
			Path: path,
			Size: size,
			Ext:  strings.ToLower(filepath.Ext(path)),
		})
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// minFuzzyLen is the shortest stem allowed into fuzzy tiers: without it a
// one-letter ROM base like "a" would claim every patch in the pile.
const minFuzzyLen = 4

// Match returns the mods for a game from dir, best tier wins: exact stem
// (or exact parent folder, for mods/Game/*.ips layouts) first, then
// prefix ("Super Mario World Kaizo.ips" for "Super Mario World"), then a
// significant title word ("mario-kaizo.ips"). Within a tier the first
// candidate with any hit wins; an empty result means "no file".
func Match(dir string, candidates ...string) ([]Mod, error) {
	all, err := ListDir(dir)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	keys := fullKeys(candidates)
	for _, c := range keys {
		if c == "" {
			continue
		}
		var hits []Mod
		for _, m := range all {
			stem := strings.TrimSuffix(m.Name, filepath.Ext(m.Name))
			if normStem(stem) == c {
				hits = append(hits, m)
				continue
			}
			if parent := filepath.Base(filepath.Dir(m.Path)); parent != "." && parent != "" {
				if normStem(parent) == c {
					hits = append(hits, m)
				}
			}
		}
		if len(hits) > 0 {
			return hits, nil
		}
	}
	for _, c := range keys {
		if len(c) < minFuzzyLen {
			continue
		}
		var hits []Mod
		for _, m := range all {
			stem := normStem(strings.TrimSuffix(m.Name, filepath.Ext(m.Name)))
			if len(stem) < minFuzzyLen {
				continue
			}
			if strings.HasPrefix(stem, c) || strings.HasPrefix(c, stem) {
				hits = append(hits, m)
			}
		}
		if len(hits) > 0 {
			return hits, nil
		}
	}
	for _, w := range wordKeys(candidates) {
		var hits []Mod
		for _, m := range all {
			stem := normStem(strings.TrimSuffix(m.Name, filepath.Ext(m.Name)))
			if stem == w || strings.HasPrefix(stem, w) {
				hits = append(hits, m)
			}
		}
		if len(hits) > 0 {
			return hits, nil
		}
	}
	return nil, nil
}

// fullKeys normalizes every candidate stem, in order.
func fullKeys(candidates []string) []string {
	keys := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if nc := normStem(c); nc != "" {
			keys = append(keys, nc)
		}
	}
	return keys
}

// wordKeys splits candidates into significant words (lowercase,
// tags dropped, length 4+), deduplicated in order: "Super Mario World
// (USA)" gives super, mario, world.
func wordKeys(candidates []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range candidates {
		for _, w := range stemWords(c) {
			if !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	return out
}

// stemWords lowercases s, drops (parenthesized) and [bracketed] tags,
// and returns alphanumeric words of length 4+.
func stemWords(s string) []string {
	s = strings.ToLower(s)
	for {
		i := strings.IndexAny(s, "([")
		if i < 0 {
			break
		}
		closer := ")"
		if s[i] == '[' {
			closer = "]"
		}
		j := strings.Index(s[i:], closer)
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + " " + s[i+j+1:]
	}
	var out []string
	for _, w := range strings.FieldsFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(w) >= minFuzzyLen {
			out = append(out, w)
		}
	}
	return out
}

// SoftExts are patch extensions RetroArch autopatches by basename:
// loading rom.bin applies rom.ips / rom.bps / rom.ups / rom.xdelta when
// present. Only cores that load ROMs from memory support this.
var SoftExts = []string{".ips", ".bps", ".ups", ".xdelta"}

// SoftBase returns the directory and stem RetroArch derives the patch
// name from. For zipped ROMs it is the archive's dir plus the inner
// entry's stem (comp.zip#inner.j64 wants inner.ips beside the zip),
// otherwise the ROM's own dir plus its stem: game.sfc wants game.ips
// in the same folder.
func SoftBase(romPath, inner string) (dir, stem string) {
	dir = filepath.Dir(romPath)
	name := filepath.Base(romPath)
	if strings.TrimSpace(inner) != "" {
		name = filepath.Base(inner)
	}
	stem = strings.TrimSuffix(name, filepath.Ext(name))
	if strings.TrimSpace(stem) == "" {
		stem = name
	}
	return dir, stem
}

// DeployTargets lists where a pile file with the given extension must be
// copied to become active for the game: stem.ext first, then numbered
// slots stem.ext1, stem.ext2, … (RetroArch applies .ips, .ips1, … in
// order, so several mods can stack).
func DeployTargets(romPath, inner, ext string) []string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	dir, stem := SoftBase(romPath, inner)
	targets := make([]string, 0, 4)
	targets = append(targets, filepath.Join(dir, stem+ext))
	for i := 1; i < 100; i++ {
		targets = append(targets, filepath.Join(dir, fmt.Sprintf("%s%s%d", stem, ext, i)))
	}
	return targets
}

// sameBytes reports whether both paths exist and hold identical bytes.
// Missing files read as "different", never as equal.
func sameBytes(a, b string) bool {
	da, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(da, db)
}

// PlanDeploy picks the deployed path for a pile file: the first slot
// holding identical bytes (already active, nothing to do) wins, else the
// first free slot. ext is the pile file's own extension.
func PlanDeploy(src, romPath, inner, ext string) (dst string, active bool) {
	targets := DeployTargets(romPath, inner, ext)
	for _, t := range targets {
		if sameBytes(src, t) {
			return t, true
		}
		if _, err := os.Stat(t); os.IsNotExist(err) {
			return t, false
		}
	}
	return targets[0], false
}

// Deploy copies the pile file to dst, creating parent dirs.
// dst normally comes from PlanDeploy, so the name is exactly what
// RetroArch autopatches by.
func Deploy(src, dst string) error {
	if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
		return fmt.Errorf("empty mod path")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dst)
}

// ErrNotOurs aborts Undeploy when the deployed file differs from the pile
// source: it was hand-placed by the user, not by cartridge, so removing
// it would destroy foreign data.
var ErrNotOurs = errors.New("deployed file differs from the pile source")

// Undeploy removes a deployed copy, but only when its bytes still match
// the pile source. A missing dst is a no-op success.
func Undeploy(src, dst string) error {
	if _, err := os.Stat(dst); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !sameBytes(src, dst) {
		return fmt.Errorf("%w: %s", ErrNotOurs, dst)
	}
	return os.Remove(dst)
}

// normStem lowercases s, drops (parenthesized) and [bracketed] tags and
// keeps alphanumerics only, for fuzzy mod file matching.
func normStem(s string) string {
	s = strings.ToLower(s)
	for {
		i := strings.IndexAny(s, "([")
		if i < 0 {
			break
		}
		closer := ")"
		if s[i] == '[' {
			closer = "]"
		}
		j := strings.Index(s[i:], closer)
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + " " + s[i+j+1:]
	}
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
