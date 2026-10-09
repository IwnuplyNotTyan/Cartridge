package steam

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// TrackFilePath returns the collections tracking file next to the library
// database (same directory scheme as playtime/favorites stores).
func TrackFilePath(libraryPath string) string {
	dir := filepath.Dir(libraryPath)
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "steam-collections.json")
}

// trackDoc remembers which cloud collection keys cartridge created or
// merged per Steam user, so renames can clean up the old collections.
// Only keys are stored — never game titles or appids.
type trackDoc struct {
	Users map[string][]string `json:"users,omitempty"`
}

// loadTrack reads the tracking file; missing file yields empty doc.
func loadTrack(path string) trackDoc {
	var d trackDoc
	if path == "" {
		return d
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return d
	}
	_ = json.Unmarshal(data, &d)
	if d.Users == nil {
		d.Users = map[string][]string{}
	}
	return d
}

// saveTrack writes the tracking file atomically when it changed.
func saveTrack(path string, d trackDoc) error {
	if path == "" {
		return nil
	}
	if d.Users == nil {
		d.Users = map[string][]string{}
	}
	for u, keys := range d.Users {
		cp := append([]string(nil), keys...)
		sort.Strings(cp)
		d.Users[u] = cp
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// trackedKeys returns the tracked collection keys for a user.
func trackedKeys(d trackDoc, userID string) map[string]bool {
	out := map[string]bool{}
	for _, k := range d.Users[userID] {
		out[k] = true
	}
	return out
}

// equalStringSlices reports ordered equality (both sorted).
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
