// Package favorites persists the user's favorite games by stable game ID.
package favorites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Store persists favorite game IDs.
type Store struct {
	path string
	ids  map[string]bool
}

// DefaultPath places favorites.json next to the library database.
func DefaultPath(libraryPath string) string {
	if libraryPath == "" {
		return "favorites.json"
	}
	return filepath.Join(filepath.Dir(libraryPath), "favorites.json")
}

// Load reads path; a missing file yields an empty store. Both the map
// shape ({"id": true}) and a plain ID list (["id"]) are accepted so the
// file stays hand-editable.
func Load(path string) (*Store, error) {
	s := &Store{path: path, ids: map[string]bool{}}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.ids); err != nil {
		var list []string
		if err2 := json.Unmarshal(data, &list); err2 != nil {
			return nil, err
		}
		for _, id := range list {
			if id != "" {
				s.ids[id] = true
			}
		}
		return s, nil
	}
	if s.ids == nil {
		s.ids = map[string]bool{}
	}
	for id := range s.ids {
		if id == "" {
			delete(s.ids, id)
		}
	}
	return s, nil
}

// Has reports whether the game is favorited. Nil-safe.
func (s *Store) Has(id string) bool {
	if s == nil || id == "" {
		return false
	}
	return s.ids[id]
}

// Add marks the game as a favorite (in memory; call Save to persist).
// Nil-safe (drops the write).
func (s *Store) Add(id string) {
	if s == nil || id == "" {
		return
	}
	s.ids[id] = true
}

// Remove unmarks the game. Nil-safe.
func (s *Store) Remove(id string) {
	if s == nil {
		return
	}
	delete(s.ids, id)
}

// Toggle flips the favorite mark and reports the new state.
// Nil-safe (reports false, changes nothing).
func (s *Store) Toggle(id string) bool {
	if s == nil || id == "" {
		return false
	}
	if s.ids[id] {
		delete(s.ids, id)
		return false
	}
	s.ids[id] = true
	return true
}

// IDs returns the favorited IDs in stable sorted order.
func (s *Store) IDs() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.ids))
	for id := range s.ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Save writes the store atomically (tmp + rename). Nil-safe.
func (s *Store) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.ids, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
