package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"cartridge/internal/emulate"
	"cartridge/internal/mods"
	"cartridge/internal/rom"
)

// Mod source cache: live/pile dirs and deployed copies.
// modCacheStat is a cheap fingerprint of the mod sources for revalidation:
// top-dir stats only, no directory walks. Every cursor move revalidates,
// so this must stay O(1) — walking the trees here froze browsing.
// The ROM dir is watched too, so a deployed (or hand-placed) file
// beside the ROM refreshes the [active] marks without a game switch.
// (A file added in a nested subfolder only changes that subfolder's
// mtime and shows up on the next game switch; a file added at the top
// level is picked up at once.)
type modCacheStat struct {
	liveMod time.Time
	liveOk  bool
	pileMod time.Time
	pileOk  bool
	romMod  time.Time
	romOk   bool
}

// statModSources snapshots the live dir, the shared pile dir and the
// ROM dir holding the deployed copies.
func statModSources(live, pile, romDir string) modCacheStat {
	var s modCacheStat
	if strings.TrimSpace(live) != "" {
		if st, err := os.Stat(live); err == nil {
			s.liveMod, s.liveOk = st.ModTime(), true
		}
	}
	if strings.TrimSpace(pile) != "" {
		if st, err := os.Stat(pile); err == nil {
			s.pileMod, s.pileOk = st.ModTime(), true
		}
	}
	if strings.TrimSpace(romDir) != "" {
		if st, err := os.Stat(romDir); err == nil {
			s.romMod, s.romOk = st.ModTime(), true
		}
	}
	return s
}

// syncModCache collects the selected game's mod files, keeping the result
// while the inputs stay put. Sources are the recipe's mods dir (when it
// exists) plus fuzzy matches from the shared mods dir. For every pile
// file it also plans the exact deployed path RetroArch autopatches by
// and whether those bytes already sit in place.
func (m *Model) syncModCache() {
	g := m.selected()
	var r *emulate.Recipe
	if g != nil {
		r = m.gameRecipe(*g)
	}
	if g == nil || r == nil || !r.Mods.Supported {
		m.modKey, m.modLive = "", ""
		m.modFiles = nil
		m.modTarget, m.modActive, m.modBuilt, m.modCheck = nil, nil, nil, nil
		m.modStat = modCacheStat{}
		return
	}
	src := g.ID + "\x00" + r.Name + "\x00" + r.Mods.Path + "\x00" + m.modsDir
	romDir := filepath.Dir(g.Path)
	if src == m.modKey {
		if statModSources(m.modLive, m.modsDir, romDir) == m.modStat {
			return
		}
	}
	live, _ := m.resolveModPath(*g, *r)
	files := m.collectMods(*g, live)
	target := make(map[string]string, len(files))
	active := make(map[string]bool, len(files))
	built := make(map[string]bool, len(files))
	check := make(map[string]mods.Check, len(files))
	// Verification reads the ROM once per game switch and checks every
	// pile file against it, so a doomed patch is marked before deploy.
	var romBytes []byte
	if len(files) > 0 {
		romBytes, _ = mods.ReadROMBytes(g.Path, g.Inner)
	}
	for _, f := range files {
		dst, ok := mods.PlanDeploy(f.Path, g.Path, g.Inner, f.Ext)
		target[f.Path] = dst
		active[f.Path] = ok
		if romBytes != nil {
			check[f.Path] = mods.VerifyPatch(f.Path, romBytes)
		}
		if dir := strings.TrimSpace(m.patchedDir); dir != "" {
			_, built[f.Path] = mods.InstanceStatus(dir, g.ID, f.Path)
		}
	}
	m.modKey, m.modLive, m.modFiles = src, live, files
	m.modTarget, m.modActive, m.modBuilt, m.modCheck = target, active, built, check
	m.modStat = statModSources(live, m.modsDir, romDir)
}

// resolveModPath expands the recipe's mods template. A template pointing
// at an existing dir is the live dir; anything else (a missing path, a
// file path, an empty template) is kept as the intended display path.
func (m Model) resolveModPath(g rom.Game, r emulate.Recipe) (live string, searched bool) {
	tmpl, _ := r.Expand(r.Mods.Path, g)
	tmpl = strings.TrimSpace(tmpl)
	if tmpl != "" {
		if st, err := os.Stat(tmpl); err == nil && st.IsDir() {
			return tmpl, false
		}
		if strings.TrimSpace(m.modsDir) == "" {
			return tmpl, false
		}
		return tmpl, true
	}
	if dir := strings.TrimSpace(m.modsDir); dir != "" {
		return "", true
	}
	return "", false
}

// collectMods merges the live dir content with fuzzy pile matches,
// deduplicated by path.
func (m Model) collectMods(g rom.Game, live string) []mods.Mod {
	seen := map[string]bool{}
	var out []mods.Mod
	if live != "" {
		if st, err := os.Stat(live); err == nil && st.IsDir() {
			if files, err := mods.ListDir(live); err == nil {
				for _, f := range files {
					if !seen[f.Path] {
						seen[f.Path] = true
						out = append(out, f)
					}
				}
			}
		}
	}
	if dir := strings.TrimSpace(m.modsDir); dir != "" {
		if hits, err := mods.Match(dir, modCandidates(g)...); err == nil {
			for _, f := range hits {
				if !seen[f.Path] {
					seen[f.Path] = true
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// modCandidates orders file stems to match a game by: inner/ROM base
// name, title, short id, full id (same order as cheats).
func modCandidates(g rom.Game) []string {
	return cheatCandidates(g)
}
