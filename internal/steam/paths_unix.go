//go:build !windows

package steam

import (
	"os"
	"path/filepath"
	"strings"
)

// platformRoots has no extra install locations outside Windows: the
// Unix candidates live in candidateRoots.
func platformRoots() []string { return nil }

// steamProcessRunning scans /proc for the Steam client.
func steamProcessRunning() bool {
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
