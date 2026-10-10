package steam

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"cartridge/internal/config"
	"cartridge/internal/icons"
	"cartridge/internal/rom"
)

// iconsEnabled reports whether cached art should be attached to shortcuts.
// Default is on: the lookup is a local file stat per game (no network),
// so even 10k-game syncs stay fast. Provider "off" disables Icon + grid.
func iconsEnabled(cfg config.SteamConfig) bool {
	return !strings.EqualFold(strings.TrimSpace(cfg.IconsProvider), icons.ProviderOff)
}

// IconsDir resolves the icon cache directory: explicit config wins,
// else the shared XDG cache.
func IconsDir(cfg config.SteamConfig) string {
	if strings.TrimSpace(cfg.IconsDir) != "" {
		return config.ExpandPath(strings.TrimSpace(cfg.IconsDir))
	}
	return icons.DefaultDir()
}

// Shared manifest-backed caches, one per directory per process.
// BuildShortcut runs per game (10k+ times per sync): re-parsing the whole
// index.json on every lookup would burn minutes, so the manifest parses
// once and Hit/HitHero stay pure disk stats (fresh files from an ongoing
// fetch are visible immediately, manifest staleness is harmless here).
var (
	sharedIconsMu sync.Mutex
	sharedIcons   = map[string]*icons.Cache{}
)

// sharedIconsCache returns the process-wide cache for dir.
func sharedIconsCache(dir string) *icons.Cache {
	sharedIconsMu.Lock()
	defer sharedIconsMu.Unlock()
	if c, ok := sharedIcons[dir]; ok {
		return c
	}
	c := icons.New(dir)
	sharedIcons[dir] = c
	return c
}

// CachedIconPath returns the cached PNG for a game id, or "" on miss.
// Pure local stat: safe to call per game inside bulk sync loops.
func CachedIconPath(cfg config.SteamConfig, gameID string) string {
	if !iconsEnabled(cfg) || strings.TrimSpace(gameID) == "" {
		return ""
	}
	c := sharedIconsCache(IconsDir(cfg))
	if p, ok := c.Hit(gameID); ok {
		return p
	}
	return ""
}

// CachedHeroPath returns the cached wide hero for a game id, or "" on miss.
func CachedHeroPath(cfg config.SteamConfig, gameID string) string {
	if !iconsEnabled(cfg) || strings.TrimSpace(gameID) == "" {
		return ""
	}
	c := sharedIconsCache(IconsDir(cfg))
	if p, ok := c.HitHero(gameID); ok {
		return p
	}
	return ""
}

// ApplyCachedIcons fills Shortcut.Icon from the local cache.
// Keeps a pre-existing Icon when it still points at a real file (e.g. a
// hand-picked image), so re-syncs never wipe manual art. Returns hits.
func ApplyCachedIcons(list []Shortcut, games []rom.Game, cfg config.SteamConfig) int {
	if !iconsEnabled(cfg) {
		return 0
	}
	c := sharedIconsCache(IconsDir(cfg))
	byLaunch := map[string]rom.Game{}
	for _, g := range games {
		byLaunch[strings.TrimSpace(LaunchOptionsFor(g))] = g
	}
	hits := 0
	for i := range list {
		lo := strings.TrimSpace(list[i].Icon)
		g, ok := byLaunch[strings.TrimSpace(list[i].LaunchOptions)]
		if !ok {
			continue
		}
		if p, hit := c.Hit(g.ID); hit {
			if list[i].Icon != p {
				list[i].Icon = p
			}
			hits++
			continue
		}
		// No cache: keep manual art, drop dangling paths.
		if lo != "" {
			if st, err := os.Stat(lo); err != nil || st.IsDir() {
				list[i].Icon = ""
			}
		}
	}
	return hits
}

// GridFileNames returns the grid-folder file names Steam reads for one
// non-Steam shortcut id: the plain icon plus the portrait capsule.
// Steam picks them up on next start (no client API needed).
func GridFileNames(appID uint32) []string {
	base := strconv.FormatUint(uint64(appID), 10)
	return []string{base + ".png", base + "p.png"}
}

// GridHeroName returns the grid-folder wide hero Steam renders on the
// game's library page.
func GridHeroName(appID uint32) string {
	return strconv.FormatUint(uint64(appID), 10) + "_hero.png"
}

// DeployGridArt copies cached icons and heroes into <cfgDir>/grid for every
// shortcut that has local art. Steady state is stats only: a destination
// that exists with the same size and a newer mtime is trusted without
// reading either file, so 10k-game syncs never re-read gigabytes of art.
// Fresh deploys are hardlinks (no data blocks), with a copy fallback.
// Returns deployed (copied now) and total-with-art, icons and heroes split.
func DeployGridArt(cfgDir string, list []Shortcut, games []rom.Game, cfg config.SteamConfig) (deployed, withArt, heroDeployed, heroWithArt int) {
	if !iconsEnabled(cfg) {
		return 0, 0, 0, 0
	}
	grid := filepath.Join(cfgDir, "grid")
	// deploy links src into every dst name that isn't already our link.
	// os.SameFile is the whole freshness check: a link shares the inode,
	// so content is identical by construction and nothing is ever read.
	// Existing plain copies (same content, own inode) are converted to
	// links on the way, freeing their duplicate blocks — link+rename
	// needs only an inode, so it works even on a full disk. Across
	// filesystems it falls back to compare-and-copy.
	deploy := func(names []string, src string, st os.FileInfo) (copied int) {
		var pending []string
		for _, name := range names {
			dst := filepath.Join(grid, name)
			if dstSt, err := os.Stat(dst); err == nil && !dstSt.IsDir() && os.SameFile(dstSt, st) {
				continue
			}
			pending = append(pending, dst)
		}
		if len(pending) == 0 {
			return 0
		}
		for _, dst := range pending {
			tmp := dst + ".tmp"
			_ = os.Remove(tmp)
			if err := os.Link(src, tmp); err == nil {
				if err := os.Rename(tmp, dst); err == nil {
					copied++
					continue
				}
				_ = os.Remove(tmp)
			}
			data, err := os.ReadFile(src)
			if err != nil || sameBytes(dst, data) {
				continue
			}
			if err := atomicCopy(dst, data); err == nil {
				copied++
			}
		}
		return copied
	}
	mkGrid := func() bool {
		if err := os.MkdirAll(grid, 0o755); err != nil {
			return false
		}
		return true
	}
	gridOK := false
	for _, s := range list {
		src := strings.TrimSpace(s.Icon)
		if src == "" {
			continue
		}
		st, err := os.Stat(src)
		if err != nil || st.IsDir() || st.Size() == 0 {
			continue
		}
		withArt++
		if !gridOK {
			if !mkGrid() {
				continue
			}
			gridOK = true
		}
		deployed += deploy(GridFileNames(s.AppID), src, st)
	}
	// Heroes live in the cache, not in shortcuts.vdf: match shortcuts back
	// to games via launch options, then to the cached hero file.
	heroByLaunch := map[string]string{}
	if len(games) > 0 {
		c := sharedIconsCache(IconsDir(cfg))
		for _, g := range games {
			if p, ok := c.HitHero(g.ID); ok {
				heroByLaunch[strings.TrimSpace(LaunchOptionsFor(g))] = p
			}
		}
	}
	for _, s := range list {
		hero, ok := heroByLaunch[strings.TrimSpace(s.LaunchOptions)]
		if !ok || hero == "" {
			continue
		}
		st, err := os.Stat(hero)
		if err != nil || st.IsDir() || st.Size() == 0 {
			continue
		}
		heroWithArt++
		if !gridOK {
			if !mkGrid() {
				continue
			}
			gridOK = true
		}
		heroDeployed += deploy([]string{GridHeroName(s.AppID)}, hero, st)
	}
	return deployed, withArt, heroDeployed, heroWithArt
}

// sameBytes reports whether dst already holds data (size + content).
func sameBytes(dst string, data []byte) bool {
	st, err := os.Stat(dst)
	if err != nil || st.IsDir() || st.Size() != int64(len(data)) {
		return false
	}
	f, err := os.Open(dst)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(data))
	n, err := io.ReadFull(f, buf)
	if err != nil || n != len(data) {
		return false
	}
	for i := range buf {
		if buf[i] != data[i] {
			return false
		}
	}
	return true
}

// atomicCopy writes dst via tmp+rename.
func atomicCopy(dst string, data []byte) error {
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// IconsCache builds the fetch cache for cfg (provider + key + dir).
func IconsCache(cfg config.SteamConfig) *icons.Cache {
	c := icons.New(IconsDir(cfg))
	if p := strings.TrimSpace(cfg.IconsProvider); p != "" {
		c.Provider = strings.ToLower(p)
	} else {
		c.Provider = icons.ProviderAuto
	}
	c.GridDBKey = strings.TrimSpace(cfg.SteamGridDBKey)
	return c
}

// ToIconGames maps rom games to the icons package input.
func ToIconGames(games []rom.Game) []icons.Game {
	out := make([]icons.Game, 0, len(games))
	for _, g := range games {
		out = append(out, icons.Game{
			ID:        g.ID,
			Title:     g.Title,
			ConsoleID: g.ConsoleID,
			Stem:      gameStem(g),
			Region:    g.Region,
			Tags:      g.Tags,
		})
	}
	return out
}

// gameStem returns the filename stem thumbnail repos name art after:
// the zip inner entry when present, else the file itself, minus extension.
func gameStem(g rom.Game) string {
	name := filepath.Base(g.Path)
	if strings.TrimSpace(g.Inner) != "" {
		name = filepath.Base(g.Inner)
	}
	if ext := filepath.Ext(name); ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	return strings.TrimSpace(name)
}

// PruneGridArt removes grid files for appids no longer present.
// keep lists live shortcut ids; returns removed file count.
// Hero files ("<id>_hero.png") belong to their base id; anything else
// non-numeric (real Steam game art) is treated as foreign and kept.
func PruneGridArt(cfgDir string, keep map[uint32]bool) int {
	grid := filepath.Join(cfgDir, "grid")
	entries, err := os.ReadDir(grid)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Stale link-swap leftovers (killed mid-deploy): Steam never
		// reads them, and the next deploy recreates the name anyway.
		if strings.HasSuffix(name, ".tmp") {
			if err := os.Remove(filepath.Join(grid, name)); err == nil {
				removed++
			}
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		// Strip our suffixes for the id lookup: portrait "p" capsules
		// and "_hero" backdrops both belong to their base appid.
		candidates := []string{base}
		if strings.HasSuffix(base, "_hero") {
			candidates = append(candidates, strings.TrimSuffix(base, "_hero"))
		} else if strings.HasSuffix(base, "p") && len(base) > 1 {
			candidates = append(candidates, strings.TrimSuffix(base, "p"))
		}
		alive := false
		parsed := false
		for _, c := range candidates {
			if id, err := strconv.ParseUint(c, 10, 32); err == nil {
				parsed = true
				if keep[uint32(id)] {
					alive = true
					break
				}
			}
		}
		if !parsed {
			alive = true // foreign file (real Steam game art): keep
		}
		if !alive {
			if err := os.Remove(filepath.Join(grid, name)); err == nil {
				removed++
			}
		}
	}
	return removed
}
