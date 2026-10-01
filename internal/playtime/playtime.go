// Package playtime records per-game play sessions and totals.
package playtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Stats aggregates sessions of one game.
type Stats struct {
	Sessions     int       `json:"sessions"`
	TotalSeconds int64     `json:"total_seconds"`
	LastPlayed   time.Time `json:"last_played"`
}

// Total returns the accumulated play time.
func (s Stats) Total() time.Duration { return time.Duration(s.TotalSeconds) * time.Second }

// Store persists stats by stable game ID.
type Store struct {
	path  string
	stats map[string]*Stats
}

// DefaultPath places playtime.json next to the library database.
func DefaultPath(libraryPath string) string {
	if libraryPath == "" {
		return "playtime.json"
	}
	return filepath.Join(filepath.Dir(libraryPath), "playtime.json")
}

// Load reads path; a missing file yields an empty store.
func Load(path string) (*Store, error) {
	s := &Store{path: path, stats: map[string]*Stats{}}
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
	if err := json.Unmarshal(data, &s.stats); err != nil {
		return nil, err
	}
	if s.stats == nil {
		s.stats = map[string]*Stats{}
	}
	return s, nil
}

// Add records one session of d for the game.
func (s *Store) Add(id string, d time.Duration) {
	st := s.stats[id]
	if st == nil {
		st = &Stats{}
		s.stats[id] = st
	}
	st.Sessions++
	st.TotalSeconds += int64(d / time.Second)
	st.LastPlayed = time.Now()
}

// Get returns stats for the game, if any.
func (s *Store) Get(id string) (Stats, bool) {
	st, ok := s.stats[id]
	if !ok || st == nil {
		return Stats{}, false
	}
	return *st, true
}

// Save writes the store atomically (tmp + rename).
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.stats, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
