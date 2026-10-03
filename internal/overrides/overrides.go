// Package overrides persists per-game emulator and core choices picked
// in the metadata pane (n/p on the recipe/core rows).
package overrides

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Choice is the saved per-game override: recipe name and core path.
// Empty fields mean "use the default". The core is stored by path (not
// index) so reordered core lists keep resolving.
type Choice struct {
	Recipe string `json:"recipe,omitempty"`
	Core   string `json:"core,omitempty"`
}

// Store persists choices by stable game ID.
type Store struct {
	path    string
	choices map[string]Choice
}

// DefaultPath places overrides.json next to the library database.
func DefaultPath(libraryPath string) string {
	if libraryPath == "" {
		return "overrides.json"
	}
	return filepath.Join(filepath.Dir(libraryPath), "overrides.json")
}

// Load reads path; a missing file yields an empty store.
func Load(path string) (*Store, error) {
	s := &Store{path: path, choices: map[string]Choice{}}
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
	if err := json.Unmarshal(data, &s.choices); err != nil {
		return nil, err
	}
	if s.choices == nil {
		s.choices = map[string]Choice{}
	}
	return s, nil
}

// Get returns the choice for the game, if any. Nil-safe.
func (s *Store) Get(id string) (Choice, bool) {
	if s == nil {
		return Choice{}, false
	}
	c, ok := s.choices[id]
	return c, ok
}

// Set records the choice for the game (in memory; call Save to persist).
// Nil-safe (drops the write).
func (s *Store) Set(id string, c Choice) {
	if s == nil {
		return
	}
	s.choices[id] = c
}

// Save writes the store atomically (tmp + rename). Nil-safe.
func (s *Store) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.choices, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
