// Package library persists the scanned ROM collection as JSON.
package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"cartridge/internal/rom"
)

// DefaultPath returns the standard location of the library database.
func DefaultPath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "library.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "library.json"
	}
	return filepath.Join(home, ".local", "share", "cartridge", "library.json")
}

// Load reads a library file. A missing file yields an empty collection.
func Load(path string) ([]rom.Game, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var games []rom.Game
	if err := json.Unmarshal(data, &games); err != nil {
		return nil, fmt.Errorf("parse library %s: %w", path, err)
	}
	return games, nil
}

// Save writes the collection atomically (tmp + rename).
func Save(path string, games []rom.Game) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(games, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Merge upserts scanned games into the stored collection by stable ID.
// A re-discovered ID updates its path (ROM moved); stored metadata for
// IDs no longer on disk is kept. Entries that are not valid games
// (mod files, archive junk, unsupported extensions — e.g. rows written
// by older scans) are pruned from both sides, so the library file heals
// itself on the next save.
func Merge(stored, scanned []rom.Game) []rom.Game {
	byID := make(map[string]rom.Game, len(stored)+len(scanned))
	for _, g := range stored {
		if !rom.ValidGame(g) {
			continue
		}
		byID[g.ID] = g
	}
	for _, g := range scanned {
		if !rom.ValidGame(g) {
			continue
		}
		byID[g.ID] = g
	}
	out := make([]rom.Game, 0, len(byID))
	for _, g := range byID {
		out = append(out, g)
	}
	// Keep deterministic order: console, then title.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			less := a.ConsoleName < b.ConsoleName ||
				(a.ConsoleName == b.ConsoleName && a.Title < b.Title)
			if less {
				break
			}
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
