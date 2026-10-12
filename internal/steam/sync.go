package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cartridge/internal/config"
	"cartridge/internal/rom"
)

// DefaultExe resolves the cartridge binary path for shortcuts:
// explicit config wins, else the current executable, else "cartridge".
func DefaultExe(cfg config.SteamConfig) string {
	if strings.TrimSpace(cfg.Exe) != "" {
		return config.ExpandPath(strings.TrimSpace(cfg.Exe))
	}
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		if p, err := filepath.EvalSymlinks(exe); err == nil && strings.TrimSpace(p) != "" {
			return p
		}
		return exe
	}
	return "cartridge"
}

// BuildShortcut builds the Steam entry for one game.
func BuildShortcut(g rom.Game, cfg config.SteamConfig, exe, startDir string) Shortcut {
	if strings.TrimSpace(exe) == "" {
		exe = DefaultExe(cfg)
	}
	if strings.TrimSpace(startDir) == "" {
		startDir = filepath.Dir(exe)
	}
	name := AppNameFor(g)
	id := AppIDFor(exe, name)
	return Shortcut{
		AppID:              id,
		AppName:            name,
		Exe:                exe,
		StartDir:           startDir,
		Icon:               CachedIconPath(cfg, g.ID),
		ShortcutPath:       "",
		LaunchOptions:      LaunchOptionsFor(g),
		IsHidden:           false,
		AllowDesktopConfig: true,
		AllowOverlay:       true,
		Tags:               TagsFor(g, cfg),
	}
}

// SyncResult counts what a sync changed per file.
type SyncResult struct {
	Path    string
	UserID  string
	Added   int
	Updated int
	Kept    int
	Removed int
	Total   int
	// Collections synced alongside into the sibling cloudstorage file.
	CollChanged int
	CollTotal   int
	CollSkipped bool // no cloudstorage (legacy client)
	// Icons attached from the local cache (no network in this path).
	IconsApplied int
	GridDeployed int
	GridWithArt  int
	// Wide capsules attached from the local cache.
	WideDeployed int
	WideWithArt  int
	// Wide heroes attached from the local cache.
	HeroDeployed int
	HeroWithArt  int
	// Logo overlays attached from the local cache.
	LogoDeployed int
	LogoWithArt  int
	// Games skipped because their ROM file is gone from disk (library
	// ghosts): never (re)created, and pruned like any other stale game.
	SkippedMissing int
}

// targetAppIDs splits this target's shortcuts into current (launching our
// games) and managed (all cartridge shortcuts, current + stale) appid sets.
func targetAppIDs(list []Shortcut, games []rom.Game, cfg config.SteamConfig, exe string) (current, managed map[uint32]bool) {
	want := map[string]bool{}
	for _, g := range games {
		want[strings.TrimSpace(LaunchOptionsFor(g))] = true
	}
	current = map[uint32]bool{}
	managed = map[uint32]bool{}
	for _, e := range list {
		if !isCartridgeShortcut(e, exe, cfg) {
			continue
		}
		managed[e.AppID] = true
		if want[strings.TrimSpace(e.LaunchOptions)] {
			current[e.AppID] = true
		}
	}
	return current, managed
}

// wantCategories groups current appids (as stored) by category name.
func wantCategories(list []Shortcut, games []rom.Game, cfg config.SteamConfig, exe string) map[string][]uint32 {
	byLaunch := map[string]uint32{}
	for _, e := range list {
		if isCartridgeShortcut(e, exe, cfg) {
			lo := strings.TrimSpace(e.LaunchOptions)
			if _, ok := byLaunch[lo]; !ok {
				byLaunch[lo] = e.AppID
			}
		}
	}
	set := map[string]map[uint32]bool{}
	for _, g := range games {
		id, ok := byLaunch[strings.TrimSpace(LaunchOptionsFor(g))]
		if !ok {
			continue
		}
		for _, t := range TagsFor(g, cfg) {
			if set[t] == nil {
				set[t] = map[uint32]bool{}
			}
			set[t][id] = true
		}
	}
	out := map[string][]uint32{}
	for name, ids := range set {
		out[name] = sortedU32(ids)
	}
	return out
}

// SyncFile upserts games into one shortcuts.vdf (no collection tracking).
// prune drops cartridge-managed entries whose game is gone; foreign
// shortcuts are never touched. Managed = launch options "play …" (ours),
// our exe, or our prefix tag.
func SyncFile(path string, games []rom.Game, cfg config.SteamConfig, exe, startDir string, prune bool) (SyncResult, error) {
	return syncFileTracked(path, games, cfg, exe, startDir, prune, "", "")
}

// SyncFileTracked is SyncFile plus collection tracking: collections this
// sync created or merged are remembered per Steam user (trackPath, next
// to the library), so later renames can clean up the old collections.
// userID is the userdata account id; empty userID/trackPath disables it.
func SyncFileTracked(path string, games []rom.Game, cfg config.SteamConfig, exe, startDir string, prune bool, userID, trackPath string) (SyncResult, error) {
	return syncFileTracked(path, games, cfg, exe, startDir, prune, userID, trackPath)
}

func syncFileTracked(path string, games []rom.Game, cfg config.SteamConfig, exe, startDir string, prune bool, userID, trackPath string) (SyncResult, error) {
	res := SyncResult{Path: path}
	list, err := LoadFile(path)
	if err != nil {
		return res, err
	}
	// Ghosts first: library rows whose ROM file is gone from disk are
	// never (re)created — and with prune their shortcuts/collections go
	// like any other stale game. Without this every sync resurrects
	// deleted ROMs in Steam (shortcuts + per-console collections).
	present, missing := rom.PartitionPresent(games)
	res.SkippedMissing = len(missing)
	games = present
	beforeByLaunch := map[string]bool{}
	oldManaged := map[uint32]bool{}
	oldLaunch := map[uint32]string{}
	for _, e := range list {
		beforeByLaunch[strings.TrimSpace(e.LaunchOptions)] = true
		if isCartridgeShortcut(e, exe, cfg) {
			oldManaged[e.AppID] = true
			oldLaunch[e.AppID] = strings.TrimSpace(e.LaunchOptions)
		}
	}
	// Foreign AppIDs we must not steal.
	foreignIDs := map[uint32]bool{}
	for _, e := range list {
		if !isCartridgeShortcut(e, exe, cfg) {
			foreignIDs[e.AppID] = true
		}
	}
	assigned := map[uint32]bool{}
	// launchIdx fast-paths Upsert's first match rule (launch options):
	// steady-state syncs replace in place at O(1) instead of scanning the
	// whole list per game. Misses fall back to full Upsert (new games),
	// then the index learns the result. Semantics equal Upsert exactly.
	launchIdx := make(map[string]int, len(list))
	for i, e := range list {
		if lo := strings.TrimSpace(e.LaunchOptions); lo != "" {
			if _, ok := launchIdx[lo]; !ok {
				launchIdx[lo] = i
			}
		}
	}
	added := 0
	for _, g := range games {
		s := BuildShortcut(g, cfg, exe, startDir)
		// Deterministic bump on collision with foreign or already-assigned ids.
		for foreignIDs[s.AppID] || assigned[s.AppID] {
			s.AppID++
		}
		assigned[s.AppID] = true
		lo := strings.TrimSpace(s.LaunchOptions)
		if !beforeByLaunch[lo] {
			added++
		}
		if i, ok := launchIdx[lo]; ok && lo != "" {
			if s.LastPlayTime == 0 {
				s.LastPlayTime = list[i].LastPlayTime
			}
			list[i] = s
			continue
		}
		list = Upsert(list, s)
		for j, e := range list {
			if strings.TrimSpace(e.LaunchOptions) == lo {
				launchIdx[lo] = j
				break
			}
		}
	}
	byLaunch := map[string]bool{}
	for _, g := range games {
		byLaunch[LaunchOptionsFor(g)] = true
	}
	res.Added = added
	res.Updated = len(games) - added
	res.Kept = len(games)

	if prune {
		keepLaunch := byLaunch
		keepID := assigned
		filtered := list[:0]
		removed := 0
		for _, e := range list {
			if !isCartridgeShortcut(e, exe, cfg) {
				filtered = append(filtered, e)
				continue
			}
			if keepID[e.AppID] || keepLaunch[strings.TrimSpace(e.LaunchOptions)] {
				filtered = append(filtered, e)
				continue
			}
			removed++
		}
		list = filtered
		res.Removed = removed
	}
	res.Total = len(list)
	// Cached icons are local stats only (no network): fill Icon paths,
	// then mirror art into the grid folder Steam actually renders.
	res.IconsApplied = ApplyCachedIcons(list, games, cfg)
	if err := SaveFile(path, list); err != nil {
		return res, fmt.Errorf("save %s: %w", path, err)
	}
	deployed, withArt, wideDeployed, wideWithArt, heroDeployed, heroWithArt, logoDeployed, logoWithArt := DeployGridArt(filepath.Dir(path), list, games, cfg)
	res.GridDeployed, res.GridWithArt = deployed, withArt
	res.WideDeployed, res.WideWithArt = wideDeployed, wideWithArt
	res.HeroDeployed, res.HeroWithArt = heroDeployed, heroWithArt
	res.LogoDeployed, res.LogoWithArt = logoDeployed, logoWithArt
	if prune {
		keep := map[uint32]bool{}
		for _, e := range list {
			keep[e.AppID] = true
		}
		PruneGridArt(filepath.Dir(path), keep)
	}
	// Collections live next to shortcuts.vdf; sync them in the same pass
	// so Steam groups games per console without manual work.
	cfgDir := filepath.Dir(path)
	current, managed := targetAppIDs(list, games, cfg, exe)
	want := wantCategories(list, games, cfg, exe)
	drop := map[uint32]bool{}
	if prune {
		for a := range managed {
			if !current[a] {
				drop[a] = true
			}
		}
		// Shortcuts this prune just deleted are already gone from the
		// post-prune managed set above — but their appids must still be
		// dropped from collections, otherwise prune removes shortcuts
		// while their per-console collections keep the stale members
		// forever. oldManaged is the pre-prune set, so the difference
		// is exactly what this run deleted.
		for a := range oldManaged {
			if !managed[a] {
				drop[a] = true
			}
		}
	}
	// AppIDs orphaned by an exe change (Upsert replaced them in place under
	// the same launch options): drop them from collections even without
	// --prune. Library-removed games are NOT included here — their
	// shortcuts stay until prune, so their collections stay too.
	postLaunches := map[string]bool{}
	for _, e := range list {
		if managed[e.AppID] {
			postLaunches[strings.TrimSpace(e.LaunchOptions)] = true
		}
	}
	for a := range oldManaged {
		if !current[a] && postLaunches[oldLaunch[a]] {
			drop[a] = true
		}
	}
	// Previously-ours collections under a different name (renamed since):
	// clean our appids out of them, tombstoning solely-ours leftovers.
	sweep, trackedOurs := trackedSweep(cfgDir, want, managed, current, userID, trackPath)
	// Every cartridge appid of this target: lets prune retire orphaned
	// ours-only collections even when the track file is gone.
	allOurs := map[uint32]bool{}
	for a := range managed {
		allOurs[a] = true
	}
	for a := range current {
		allOurs[a] = true
	}
	coll, err := reconcileCollections(cfgDir, want, drop, prune, sweep, trackedOurs, allOurs)
	if err != nil {
		return res, err
	}
	saveTracked(trackPath, userID, coll.Keys)
	res.CollChanged, res.CollTotal, res.CollSkipped = coll.Changed, coll.Total, coll.Skipped
	return res, nil
}

// trackedSweep resolves stale tracked collection keys (ours under an old
// name) and the appid set to remove from them. Tracking is skipped when
// userID or trackPath is empty.
func trackedSweep(cfgDir string, want map[string][]uint32, managed, current map[uint32]bool, userID, trackPath string) (sweep map[string]bool, ours map[uint32]bool) {
	sweep = map[string]bool{}
	ours = map[uint32]bool{}
	if userID == "" || trackPath == "" {
		return sweep, ours
	}
	nsPath, _, _ := cloudPaths(cfgDir)
	pairs, err := loadNamespace(nsPath)
	if err != nil {
		return sweep, ours
	}
	byName := collectionKeysByName(pairs)
	wantKeys := map[string]bool{}
	for name := range want {
		if k, ok := byName[strings.ToLower(strings.TrimSpace(name))]; ok {
			wantKeys[k] = true
		}
	}
	doc := loadTrack(trackPath)
	for k := range trackedKeys(doc, userID) {
		if !wantKeys[k] {
			sweep[k] = true
		}
	}
	if len(sweep) == 0 {
		return sweep, ours
	}
	for a := range managed {
		ours[a] = true
	}
	for a := range current {
		ours[a] = true
	}
	return sweep, ours
}

// saveTracked remembers the synced collection keys per Steam user.
func saveTracked(trackPath, userID string, keys map[string]string) {
	if trackPath == "" || userID == "" {
		return
	}
	doc := loadTrack(trackPath)
	got := make([]string, 0, len(keys))
	for _, k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	old := append([]string(nil), doc.Users[userID]...)
	sort.Strings(old)
	if equalStringSlices(old, got) {
		return
	}
	if doc.Users == nil {
		doc.Users = map[string][]string{}
	}
	doc.Users[userID] = got
	_ = saveTrack(trackPath, doc)
}

// isCartridgeShortcut reports whether e was created by cartridge.
func isCartridgeShortcut(e Shortcut, exe string, cfg config.SteamConfig) bool {
	if strings.HasPrefix(strings.TrimSpace(e.LaunchOptions), "play ") {
		return true
	}
	if exe != "" && exeKey(e.Exe) == exeKey(exe) {
		return true
	}
	prefix := strings.TrimSpace(cfg.EffectivePrefix())
	for _, t := range e.Tags {
		if prefix != "" && t == prefix {
			return true
		}
		if strings.HasPrefix(t, prefix+" - ") && prefix != "" {
			return true
		}
	}
	return false
}

// SyncAll syncs games into every discovered userdata target.
func SyncAll(games []rom.Game, cfg config.SteamConfig, exe, startDir string, prune bool) ([]SyncResult, error) {
	return SyncAllTracked(games, cfg, exe, startDir, prune, "")
}

// SyncAllTracked is SyncAll with collection tracking per Steam user.
// trackPath is normally TrackFilePath(libraryPath); empty disables it.
func SyncAllTracked(games []rom.Game, cfg config.SteamConfig, exe, startDir string, prune bool, trackPath string) ([]SyncResult, error) {
	targets := DiscoverTargets(cfg.Users)
	if len(targets) == 0 {
		return nil, fmt.Errorf("steam userdata not found (is Steam installed?)")
	}
	if strings.TrimSpace(exe) == "" {
		exe = DefaultExe(cfg)
	}
	if strings.TrimSpace(startDir) == "" {
		startDir = filepath.Dir(exe)
	}
	var out []SyncResult
	for _, t := range targets {
		r, err := syncFileTracked(t.Path, games, cfg, exe, startDir, prune, t.UserID, trackPath)
		if err != nil {
			return out, err
		}
		r.UserID = t.UserID
		out = append(out, r)
	}
	return out, nil
}

// ResetAll removes every cartridge-managed shortcut from all targets.
// Foreign shortcuts are never touched. Managed = launch options "play …"
// (ours), our exe, or our prefix tag — so entries survive exe/prefix
// changes and are still recognized as ours.
func ResetAll(cfg config.SteamConfig, exe string) ([]SyncResult, error) {
	targets := DiscoverTargets(cfg.Users)
	if len(targets) == 0 {
		return nil, fmt.Errorf("steam userdata not found")
	}
	var out []SyncResult
	for _, t := range targets {
		list, err := LoadFile(t.Path)
		if err != nil {
			return out, err
		}
		managed := map[uint32]bool{}
		for _, e := range list {
			if isCartridgeShortcut(e, exe, cfg) {
				managed[e.AppID] = true
			}
		}
		kept := list[:0]
		removed := 0
		for _, e := range list {
			if isCartridgeShortcut(e, exe, cfg) {
				removed++
				continue
			}
			kept = append(kept, e)
		}
		r := SyncResult{Path: t.Path, UserID: t.UserID, Removed: removed, Total: len(kept)}
		if removed > 0 {
			if err := SaveFile(t.Path, kept); err != nil {
				return out, err
			}
			coll, err := ResetCollections(filepath.Dir(t.Path), managed, managed)
			if err != nil {
				return out, err
			}
			r.CollChanged, r.CollSkipped = coll.Changed, coll.Skipped
			keep := map[uint32]bool{}
			for _, e := range kept {
				keep[e.AppID] = true
			}
			PruneGridArt(filepath.Dir(t.Path), keep)
		}
		out = append(out, r)
	}
	return out, nil
}

// CountManaged counts cartridge-managed shortcuts in list.
func CountManaged(list []Shortcut, cfg config.SteamConfig, exe string) int {
	n := 0
	for _, e := range list {
		if isCartridgeShortcut(e, exe, cfg) {
			n++
		}
	}
	return n
}

// RemoveGames drops the shortcuts for the given games from all targets.
func RemoveGames(games []rom.Game, cfg config.SteamConfig, exe string) ([]SyncResult, error) {
	targets := DiscoverTargets(cfg.Users)
	if len(targets) == 0 {
		return nil, fmt.Errorf("steam userdata not found")
	}
	if strings.TrimSpace(exe) == "" {
		exe = DefaultExe(cfg)
	}
	ids := map[uint32]bool{}
	launch := map[string]bool{}
	for _, g := range games {
		s := BuildShortcut(g, cfg, exe, "")
		ids[s.AppID] = true
		launch[LaunchOptionsFor(g)] = true
	}
	var out []SyncResult
	for _, t := range targets {
		list, err := LoadFile(t.Path)
		if err != nil {
			return out, err
		}
		kept := list[:0]
		removed := 0
		dropped := map[uint32]bool{}
		for _, e := range list {
			if ids[e.AppID] || launch[strings.TrimSpace(e.LaunchOptions)] {
				removed++
				dropped[e.AppID] = true
				continue
			}
			kept = append(kept, e)
		}
		r := SyncResult{Path: t.Path, UserID: t.UserID, Removed: removed, Total: len(kept)}
		if removed > 0 {
			if err := SaveFile(t.Path, kept); err != nil {
				return out, err
			}
			coll, err := ResetCollections(filepath.Dir(t.Path), dropped, nil)
			if err != nil {
				return out, err
			}
			r.CollChanged, r.CollSkipped = coll.Changed, coll.Skipped
			keep := map[uint32]bool{}
			for _, e := range kept {
				keep[e.AppID] = true
			}
			PruneGridArt(filepath.Dir(t.Path), keep)
		}
		out = append(out, r)
	}
	return out, nil
}
