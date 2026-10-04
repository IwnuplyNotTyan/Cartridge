package mods

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// DefaultPatchedDir returns the standard directory for hard-patched ROM
// instances. It lives outside the games dir on purpose: instances carry
// real ROM extensions and must never be rescanned as library games.
func DefaultPatchedDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "patched")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "patched"
	}
	return filepath.Join(home, ".local", "share", "cartridge", "patched")
}

// sanitizeStem keeps a filename stem readable: letters, digits and a few
// safe symbols survive, the rest becomes "_".
func sanitizeStem(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case strings.ContainsRune(" -_.[]()'", r):
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s = strings.TrimSpace(strings.Trim(b.String(), "._"))
	if s == "" {
		s = "mod"
	}
	return s
}

// romStemExt splits the game into its display stem and ROM extension:
// the inner entry wins for zipped games (the instance is always a plain
// extracted, patched file).
func romStemExt(romPath, inner string) (stem, ext string) {
	name := filepath.Base(romPath)
	if strings.TrimSpace(inner) != "" {
		name = filepath.Base(inner)
	}
	ext = strings.ToLower(filepath.Ext(name))
	stem = strings.TrimSuffix(name, filepath.Ext(name))
	if strings.TrimSpace(stem) == "" {
		stem = name
	}
	return sanitizeStem(stem), ext
}

// sourceName returns the filename RomPatcher.go uses for ROM-specific
// handling (header sizes, checksum repair): the inner entry for zips,
// else the ROM file itself.
func sourceName(romPath, inner string) string {
	if strings.TrimSpace(inner) != "" {
		return filepath.Base(inner)
	}
	return filepath.Base(romPath)
}

// InstanceName derives the deterministic instance filename for a
// (game, mod) pair: "<rom stem> [<mod stem>].<rom ext>".
// Readable on purpose — this is what players see in emulators' recent
// lists. Collisions across pile subfolders are resolved by the index
// with a " (2)" suffix at build time.
func InstanceName(romPath, inner, modPath string) string {
	stem, ext := romStemExt(romPath, inner)
	modStem := strings.TrimSuffix(filepath.Base(modPath), filepath.Ext(modPath))
	return fmt.Sprintf("%s [%s]%s", stem, sanitizeStem(modStem), ext)
}

// instanceEntry tracks one built instance for staleness checks.
type instanceEntry struct {
	Instance string `json:"instance"` // file name inside the patched dir
	RomSHA   string `json:"rom_sha"`  // game.ID: sha256 of canonical ROM bytes
	ModSHA   string `json:"mod_sha"`  // sha256 of the patch file
	ModSize  int64  `json:"mod_size"`
}

func instanceIndexPath(dir string) string {
	return filepath.Join(dir, "instances.json")
}

func loadInstanceIndex(dir string) map[string]instanceEntry {
	idx := map[string]instanceEntry{}
	if strings.TrimSpace(dir) == "" {
		return idx
	}
	data, err := os.ReadFile(instanceIndexPath(dir))
	if err != nil {
		return idx
	}
	_ = json.Unmarshal(data, &idx)
	if idx == nil {
		idx = map[string]instanceEntry{}
	}
	return idx
}

func saveInstanceIndex(dir string, idx map[string]instanceEntry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmp := instanceIndexPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, instanceIndexPath(dir))
}

func instanceKey(gameID, modPath string) string {
	abs, err := filepath.Abs(modPath)
	if err != nil {
		abs = modPath
	}
	return gameID + "\x00" + abs
}

func hashFile(path string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), int64(len(data)), nil
}

// InstanceStatus reports the built instance for a (game, mod) pair
// without building anything: the path and whether it is fresh
// (source hashes match and the file exists).
func InstanceStatus(dir, gameID, modPath string) (instance string, fresh bool) {
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	idx := loadInstanceIndex(dir)
	e, ok := idx[instanceKey(gameID, modPath)]
	if !ok || strings.TrimSpace(e.Instance) == "" {
		return "", false
	}
	modSHA, modSize, err := hashFile(modPath)
	if err != nil || modSHA != e.ModSHA || modSize != e.ModSize || e.RomSHA != gameID {
		return "", false
	}
	full := filepath.Join(dir, e.Instance)
	if st, err := os.Stat(full); err != nil || st.IsDir() {
		return "", false
	}
	return full, true
}

// EnsureInstance returns the hard-patched ROM for a (game, mod) pair,
// building it on the first run and whenever the ROM or the patch file
// changed. built reports whether work happened. The original ROM and
// the pile file are never modified.
func EnsureInstance(dir, gameID, romPath, inner, modPath string) (instance string, built bool, err error) {
	if strings.TrimSpace(dir) == "" {
		return "", false, fmt.Errorf("empty patched dir")
	}
	if strings.TrimSpace(modPath) == "" {
		return "", false, fmt.Errorf("empty mod path")
	}
	if full, fresh := InstanceStatus(dir, gameID, modPath); fresh {
		return full, false, nil
	}
	romBytes, err := ReadROMBytes(romPath, inner)
	if err != nil {
		return "", false, err
	}
	patchBytes, err := os.ReadFile(modPath)
	if err != nil {
		return "", false, err
	}
	out, err := ApplyPatch(romBytes, patchBytes, sourceName(romPath, inner))
	if err != nil {
		return "", false, err
	}
	modSHA, modSize, err := hashFile(modPath)
	if err != nil {
		return "", false, err
	}
	idx := loadInstanceIndex(dir)
	key := instanceKey(gameID, modPath)
	name := InstanceName(romPath, inner, modPath)
	// Keep readable names collision-free across pile subfolders.
	if e, taken := idx[key]; !taken || e.Instance != name {
		used := map[string]bool{}
		for k, v := range idx {
			if k != key && v.Instance != "" {
				used[v.Instance] = true
			}
		}
		if used[name] {
			stem, ext := romStemExt(romPath, inner)
			modStem := strings.TrimSuffix(filepath.Base(modPath), filepath.Ext(modPath))
			modStem = sanitizeStem(modStem)
			for i := 2; ; i++ {
				cand := fmt.Sprintf("%s [%s] (%d)%s", stem, modStem, i, ext)
				if !used[cand] {
					name = cand
					break
				}
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	full := filepath.Join(dir, name)
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return "", false, err
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return "", false, err
	}
	idx[key] = instanceEntry{Instance: name, RomSHA: gameID, ModSHA: modSHA, ModSize: modSize}
	if err := saveInstanceIndex(dir, idx); err != nil {
		return "", false, err
	}
	return full, true, nil
}
