//go:build windows

package steam

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// platformRoots reads Steam install locations from the registry (the
// authoritative source: Steam may live on any drive) and falls back to
// the standard Program Files folders.
func platformRoots() []string {
	var roots []string
	keys := []struct {
		root  registry.Key
		path  string
		value string
	}{
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, "InstallPath"},
	}
	for _, k := range keys {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue(k.value)
		_ = key.Close()
		if err != nil {
			continue
		}
		if v = strings.TrimSpace(v); v != "" {
			// Steam stores forward slashes; Join/Clean want native ones.
			roots = append(roots, filepath.FromSlash(v))
		}
	}
	seen := map[string]bool{}
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir != "" && !seen[strings.ToLower(dir)] {
			seen[strings.ToLower(dir)] = true
			roots = append(roots, dir)
		}
	}
	if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
		add(filepath.Join(pf, "Steam"))
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		add(filepath.Join(pf, "Steam"))
	}
	return roots
}

// steamProcessRunning lists processes with tasklist and looks for
// steam.exe. Unlike /proc this works on Windows without extra privileges.
func steamProcessRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq steam.exe", "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "steam.exe")
}
