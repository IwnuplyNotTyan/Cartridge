package steam

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SteamRoot is one Steam installation data dir candidate.
func candidateRoots() []string {
	home, _ := os.UserHomeDir()
	roots := []string{}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		roots = append(roots, filepath.Join(xdg, "Steam"))
	}
	if home != "" {
		roots = append(roots,
			// Steam for macOS stores userdata and shortcuts under the
			// user's Application Support directory.
			filepath.Join(home, "Library", "Application Support", "Steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".steam", "root"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		)
	}
	// Dedupe, keep order.
	seen := map[string]bool{}
	out := roots[:0]
	for _, r := range roots {
		r = filepath.Clean(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// UserTarget is one userdata shortcuts.vdf to sync.
type UserTarget struct {
	UserID string // userdata dir name
	Path   string // full shortcuts.vdf path
	Root   string // steam root it was found under
}

// DiscoverTargets finds all shortcuts.vdf locations (existing or not).
// When cfgUsers is non-empty only those userdata ids are returned.
func DiscoverTargets(cfgUsers []string) []UserTarget {
	want := map[string]bool{}
	for _, u := range cfgUsers {
		u = strings.TrimSpace(u)
		if u != "" {
			want[u] = true
		}
	}
	var out []UserTarget
	seen := map[string]bool{}
	for _, root := range candidateRoots() {
		ud := filepath.Join(root, "userdata")
		entries, err := os.ReadDir(ud)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			id := e.Name()
			if len(want) > 0 && !want[id] {
				continue
			}
			p := filepath.Join(ud, id, "config", "shortcuts.vdf")
			// Distinct roots often symlink to one Steam install
			// (~/.steam/steam, ~/.steam/root, ...): compare the
			// resolved path so each userdata file syncs once.
			key := p
			if rp, err := filepath.EvalSymlinks(p); err == nil {
				key = rp
			} else if rd, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
				key = filepath.Join(rd, "shortcuts.vdf")
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			seen[p] = true
			out = append(out, UserTarget{UserID: id, Path: p, Root: root})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// SteamRunning reports whether a steam process seems to be alive.
// Writing while running is allowed but lossy; callers should warn.
func SteamRunning() bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		isNum := true
		for _, c := range name {
			if c < '0' || c > '9' {
				isNum = false
				break
			}
		}
		if !isNum {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", name, "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == "steam" {
			return true
		}
	}
	return false
}
