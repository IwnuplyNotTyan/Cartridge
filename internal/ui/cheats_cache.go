package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cartridge/internal/cheats"
	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
	"cartridge/internal/rom"
)

// Cheat-file cache: resolve, materialize and normalize .cht files.
// syncCheatCache loads the selected game's cheat file, keeping the result
// while the inputs stay put. A cheap stat check notices externally created,
// edited or deleted files (by hand or by the emulator), so hand-made files
// start working without a game switch.
func (m *Model) syncCheatCache() {
	g := m.selected()
	var r *emulate.Recipe
	if g != nil {
		r = m.gameRecipe(*g)
	}
	if g == nil || r == nil || !r.Cheats.Supported {
		m.cheatKey, m.cheatPath, m.cheatPile = "", "", ""
		m.cheatFile, m.cheatMissing = nil, false
		m.cheatStat = cheatFileStat{}
		return
	}
	src := g.ID + "\x00" + r.Name + "\x00" + r.Cheats.Path + "\x00" + m.cheatsDir
	if src == m.cheatKey {
		if m.cheatPath == "" || !cheatChanged(m.cheatPath, m.cheatStat) {
			return
		}
		m.loadCheatFile(m.cheatPath)
		return
	}
	live, pile, searched := m.resolveCheatPath(*g, *r)
	if live == "" {
		// A searched-but-empty pile reads as "no file", not "bad file".
		m.cheatKey, m.cheatPath, m.cheatPile = "", "", ""
		m.cheatFile, m.cheatMissing = nil, searched
		m.cheatStat = cheatFileStat{}
		return
	}
	m.cheatKey, m.cheatPile = src, pile
	m.loadCheatFile(live)
}

// loadCheatFile (re)reads path into the cache; missing files and parse
// errors both cache as nil, told apart by cheatMissing.
func (m *Model) loadCheatFile(path string) {
	m.cheatPath = path
	m.cheatStat = statCheatFile(path)
	if f, err := cheats.Load(path); err == nil {
		m.cheatFile, m.cheatMissing = f, false
	} else if os.IsNotExist(err) {
		m.cheatFile, m.cheatMissing = nil, true
	} else {
		m.cheatFile, m.cheatMissing = nil, false
	}
}

// cheatFileStat is a cheap fingerprint of a cheat file for revalidation.
type cheatFileStat struct {
	exist bool
	mod   time.Time
	size  int64
}

// statCheatFile snapshots a path without reading it.
func statCheatFile(path string) cheatFileStat {
	st, err := os.Stat(path)
	if err != nil {
		return cheatFileStat{}
	}
	return cheatFileStat{exist: true, mod: st.ModTime(), size: st.Size()}
}

// cheatChanged reports whether path differs from a snapshot.
func cheatChanged(path string, prev cheatFileStat) bool {
	return statCheatFile(path) != prev
}

// resolveCheatPath finds the cheat file for the game. It returns the live
// path (explicit recipe path when it exists, else the pile match, else the
// intended path for display) plus a pile source when the live path is
// missing but the pile has content to materialize it from.
func (m Model) resolveCheatPath(g rom.Game, r emulate.Recipe) (live, pile string, searched bool) {
	tmpl, _ := r.Expand(r.Cheats.Path, g)
	tmpl = strings.TrimSpace(tmpl)
	if tmpl != "" {
		if _, err := os.Stat(tmpl); err == nil {
			return tmpl, "", false
		}
		if strings.TrimSpace(m.cheatsDir) == "" {
			return tmpl, "", false // missing, but show the intended path
		}
	}
	if dir := strings.TrimSpace(m.cheatsDir); dir != "" {
		if found, err := cheats.Find(dir, cheatCandidates(g)...); err == nil {
			if tmpl != "" {
				return tmpl, found, true
			}
			return found, "", true
		}
		if tmpl != "" {
			return tmpl, "", true
		}
		return "", "", true
	}
	return tmpl, "", false
}

// cheatCandidates orders file stems to match a game by: inner/ROM base
// name, title, short id, full id.
func cheatCandidates(g rom.Game) []string {
	base := func(p string) string {
		b := filepath.Base(p)
		if e := filepath.Ext(b); e != "" {
			b = strings.TrimSuffix(b, e)
		}
		return b
	}
	var out []string
	if g.Inner != "" {
		out = append(out, base(g.Inner))
	}
	if g.Path != "" {
		out = append(out, base(g.Path))
	}
	return append(out, g.Title, g.ShortID(), g.ID)
}

// materializeCheats creates the missing live cheat file: copying the pile
// match when there is one, else an empty skeleton. Saves always write
// every cheat enabled, so whatever lands here is usable at once.
func (m *Model) materializeCheats() error {
	live := m.cheatPath
	if live == "" {
		return fmt.Errorf("no cheat path")
	}
	if m.cheatPile != "" {
		src, err := cheats.Load(m.cheatPile)
		if err != nil {
			return err
		}
		if len(src.Cheats) > 0 {
			src.Path = live
			if err := src.Save(); err != nil {
				return err
			}
			m.cheatFile = src
			m.cheatStat = statCheatFile(live)
			m.cheatMissing = false
			return nil
		}
	}
	f := &cheats.File{Path: live}
	if err := f.Save(); err != nil {
		return err
	}
	m.cheatFile = f
	m.cheatStat = statCheatFile(live)
	m.cheatMissing = false
	return nil
}

// ensureCheatsOn rewrites the file with every cheat enabled when needed,
// so the whole list is available at once without per-cheat choices.
func (m *Model) ensureCheatsOn() {
	if m.cheatFile == nil || len(m.cheatFile.Cheats) == 0 {
		return
	}
	if err := m.cheatFile.Save(); err != nil {
		m.status = i18n.T("status.save_fail", err)
		return
	}
	m.cheatStat = statCheatFile(m.cheatPath)
}
