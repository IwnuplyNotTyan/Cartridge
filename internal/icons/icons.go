// Package icons downloads and caches per-game artwork for huge libraries.
//
// Design for 10k+ games:
//
//   - disk cache shards files by game ID (aa/bb…/<id>.png) so no single
//     directory ever holds thousands of entries;
//   - one small index.json manifest tracks what was fetched, from where
//     and when failures happened, so re-runs skip hits and back off
//     from misses instead of hammering the network;
//   - downloads run through a bounded worker pool (default 8) with
//     timeouts, retries and magic-byte validation, so a bulk sync is
//     fast but never opens a thousand connections at once.
//
// Default provider is Libretro Thumbnails: no API key, predictable CDN
// URLs, unlimited bulk — the only sane default for thousands of ROMs.
// SteamGridDB is an optional premium provider (needs an API key) tried
// first for missing art when configured, with Libretro as fallback.
package icons

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Providers.
const (
	ProviderAuto       = "auto"
	ProviderLibretro   = "libretro"
	ProviderSteamGrid  = "steamgriddb"
	ProviderOff        = "off"
	maxCandidatesProbe = 12
)

// Tunables for huge libraries.
const (
	DefaultConcurrency = 8
	MaxConcurrency     = 32
	httpTimeout        = 20 * time.Second
	maxImageBytes      = 8 << 20 // 8 MiB: grid art never exceeds this
	minImageBytes      = 512     // smaller payloads are error pages
	failCooldown       = 7 * 24 * time.Hour
	maxRetries         = 2
	manifestName       = "index.json"
)

// ArtKind selects which artwork flavor to fetch: portrait-ish boxart for
// the shortcut Icon / portrait capsule, a landscape capsule for the wide
// grid slot, a wide backdrop for the Steam hero, or a transparent logo
// overlay for the Steam library page.
type ArtKind string

const (
	ArtIcon ArtKind = "icon"
	ArtWide ArtKind = "wide"
	ArtHero ArtKind = "hero"
	ArtLogo ArtKind = "logo"
)

// heroSuffix distinguishes the hero shard from the icon shard.
const heroSuffix = "_hero"

// logoSuffix distinguishes the logo shard from the icon shard.
const logoSuffix = "_logo"

// wideSuffix distinguishes the wide-capsule shard from the icon shard.
const wideSuffix = "_wide"

// Game is the minimal per-game input providers need.
// Stem is the ROM filename stem ("ActRaiser (USA)" from ".../ActRaiser (USA).sfc"
// or the zip inner entry): thumbnail repos use No-Intro names, so the exact
// stem is the best first candidate. Title/Region/Tags rebuild the name when
// the stem is unavailable or normalized.
type Game struct {
	ID        string
	Title     string
	ConsoleID string
	Stem      string
	Region    string
	Tags      []string
}

// Entry is one manifest row: icon art plus the optional wide capsule,
// wide hero and logo overlay. Per-flavor fields mirror the icon ones so
// misses back off independently.
type Entry struct {
	File      string `json:"file"` // path relative to cache dir
	URL       string `json:"url,omitempty"`
	Provider  string `json:"provider,omitempty"`
	FetchedAt int64  `json:"fetched_at,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	FailCount int    `json:"fail_count,omitempty"`
	FailedAt  int64  `json:"failed_at,omitempty"`

	WideFile      string `json:"wide_file,omitempty"`
	WideURL       string `json:"wide_url,omitempty"`
	WideProvider  string `json:"wide_provider,omitempty"`
	WideFetchedAt int64  `json:"wide_fetched_at,omitempty"`
	WideBytes     int64  `json:"wide_bytes,omitempty"`
	WideFailCount int    `json:"wide_fail_count,omitempty"`
	WideFailedAt  int64  `json:"wide_failed_at,omitempty"`

	HeroFile      string `json:"hero_file,omitempty"`
	HeroURL       string `json:"hero_url,omitempty"`
	HeroProvider  string `json:"hero_provider,omitempty"`
	HeroFetchedAt int64  `json:"hero_fetched_at,omitempty"`
	HeroBytes     int64  `json:"hero_bytes,omitempty"`
	HeroFailCount int    `json:"hero_fail_count,omitempty"`
	HeroFailedAt  int64  `json:"hero_failed_at,omitempty"`

	LogoFile      string `json:"logo_file,omitempty"`
	LogoURL       string `json:"logo_url,omitempty"`
	LogoProvider  string `json:"logo_provider,omitempty"`
	LogoFetchedAt int64  `json:"logo_fetched_at,omitempty"`
	LogoBytes     int64  `json:"logo_bytes,omitempty"`
	LogoFailCount int    `json:"logo_fail_count,omitempty"`
	LogoFailedAt  int64  `json:"logo_failed_at,omitempty"`
}

// DefaultDir returns the standard icon cache location.
func DefaultDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "icons")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".cache", "cartridge", "icons")
	}
	return filepath.Join(home, ".cache", "cartridge", "icons")
}

// ShardPath returns the cache-relative file for a game id.
func ShardPath(gameID string) string {
	return shardPathFor(gameID, ArtIcon)
}

// ShardPathHero returns the cache-relative hero file for a game id.
func ShardPathHero(gameID string) string {
	return shardPathFor(gameID, ArtHero)
}

// ShardPathLogo returns the cache-relative logo file for a game id.
func ShardPathLogo(gameID string) string {
	return shardPathFor(gameID, ArtLogo)
}

// ShardPathWide returns the cache-relative wide-capsule file for a game id.
func ShardPathWide(gameID string) string {
	return shardPathFor(gameID, ArtWide)
}

func shardPathFor(gameID string, kind ArtKind) string {
	id := strings.ToLower(strings.TrimSpace(gameID))
	if len(id) < 4 {
		sum := sha256.Sum256([]byte(id))
		id = hex.EncodeToString(sum[:])
	}
	name := id + ".png"
	if kind == ArtHero {
		name = id + heroSuffix + ".png"
	} else if kind == ArtLogo {
		name = id + logoSuffix + ".png"
	} else if kind == ArtWide {
		name = id + wideSuffix + ".png"
	}
	return filepath.Join(id[:2], id[2:4], name)
}

// gameFileID derives the game id back from a cache file base name,
// stripping the hero/logo/wide suffixes ("<id>_hero" -> "<id>").
func gameFileID(base string) string {
	base = strings.TrimSuffix(base, heroSuffix)
	base = strings.TrimSuffix(base, wideSuffix)
	return strings.TrimSuffix(base, logoSuffix)
}

// AbsPath resolves the absolute cached file for a game id.
func AbsPath(dir, gameID string) string {
	if strings.TrimSpace(dir) == "" {
		dir = DefaultDir()
	}
	return filepath.Join(dir, ShardPath(gameID))
}

// Cache is the on-disk store plus fetch policy.
type Cache struct {
	Dir         string
	Concurrency int
	Client      *http.Client
	Provider    string // auto | libretro | steamgriddb | off
	GridDBKey   string

	mu      sync.Mutex
	entries map[string]Entry
	loaded  bool
	// gridIDs memoizes SteamGridDB title -> game id (0 = miss) within the
	// process, so icon+hero+logo for one game share one search request.
	gridIDs map[string]int
}

// New opens (or creates) the cache at dir.
func New(dir string) *Cache {
	if strings.TrimSpace(dir) == "" {
		dir = DefaultDir()
	}
	return &Cache{
		Dir:         dir,
		Concurrency: DefaultConcurrency,
		Client:      &http.Client{Timeout: httpTimeout},
		Provider:    ProviderAuto,
		entries:     map[string]Entry{},
	}
}

// manifestVersion bumps when candidate generation changes. Pure-failure
// rows from the title-only matcher era (version < 2) are dropped on load
// so fixed matchers get retried; legit misses from fixed matchers carry
// over with their backoff. Successes always carry over.
const manifestVersion = 3

// manifestDoc is the on-disk index.json shape.
type manifestDoc struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries"`
}

// manifestPath is the index file location.
func (c *Cache) manifestPath() string { return filepath.Join(c.Dir, manifestName) }

// load reads index.json once (missing file = empty cache).
func (c *Cache) load() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return
	}
	c.loaded = true
	if c.entries == nil {
		c.entries = map[string]Entry{}
	}
	data, err := os.ReadFile(c.manifestPath())
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return
	}
	// Current shape: {"version": N, "entries": {...}}.
	var doc manifestDoc
	if err := json.Unmarshal(data, &doc); err == nil && doc.Entries != nil {
		for k, v := range doc.Entries {
			// Fixed matchers retry: drop misses recorded by the buggy
			// title-only matcher, keep legit misses from fixed code.
			if doc.Version < 2 && v.Bytes == 0 && v.HeroBytes == 0 && v.LogoBytes == 0 {
				continue
			}
			c.entries[k] = v
		}
		return
	}
	// Legacy shape (v1): bare map. Pure failures are dropped — they were
	// recorded by the title-only matcher that missed region suffixes.
	var legacy map[string]Entry
	if err := json.Unmarshal(data, &legacy); err != nil {
		return
	}
	for k, v := range legacy {
		if v.Bytes == 0 {
			continue
		}
		c.entries[k] = v
	}
}

// save writes index.json atomically (caller must not hold c.mu).
func (c *Cache) save() {
	c.mu.Lock()
	doc := manifestDoc{Version: manifestVersion, Entries: make(map[string]Entry, len(c.entries))}
	for k, v := range c.entries {
		doc.Entries[k] = v
	}
	c.mu.Unlock()
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(c.Dir, 0o755)
	tmp := c.manifestPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, c.manifestPath())
}

// Hit returns the cached icon file when it exists on disk.
func (c *Cache) Hit(gameID string) (string, bool) {
	return c.hitKind(gameID, ArtIcon)
}

// HitHero returns the cached hero file when it exists on disk.
func (c *Cache) HitHero(gameID string) (string, bool) {
	return c.hitKind(gameID, ArtHero)
}

// HitLogo returns the cached logo file when it exists on disk.
func (c *Cache) HitLogo(gameID string) (string, bool) {
	return c.hitKind(gameID, ArtLogo)
}

// HitWide returns the cached wide-capsule file when it exists on disk.
func (c *Cache) HitWide(gameID string) (string, bool) {
	return c.hitKind(gameID, ArtWide)
}

func (c *Cache) hitKind(gameID string, kind ArtKind) (string, bool) {
	c.load()
	p := filepath.Join(c.Dir, shardPathFor(gameID, kind))
	st, err := os.Stat(p)
	if err != nil || st.IsDir() || st.Size() < minImageBytes {
		return "", false
	}
	return p, true
}

// FailedRecently reports whether gameID+kind failed within the cooldown.
func (c *Cache) FailedRecently(gameID string, kind ArtKind, now time.Time) bool {
	c.load()
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[gameID]
	if !ok {
		return false
	}
	at, n := e.FailedAt, e.FailCount
	if kind == ArtHero {
		at, n = e.HeroFailedAt, e.HeroFailCount
	} else if kind == ArtLogo {
		at, n = e.LogoFailedAt, e.LogoFailCount
	} else if kind == ArtWide {
		at, n = e.WideFailedAt, e.WideFailCount
	}
	if n == 0 || at == 0 {
		return false
	}
	return now.Unix()-at < int64(failCooldown.Seconds())
}

// Stats summarizes the cache for status commands.
type Stats struct {
	Dir        string `json:"dir"`
	Files      int    `json:"files"`
	Bytes      int64  `json:"bytes"`
	Indexed    int    `json:"indexed"`
	Failed     int    `json:"failed"`
	WideFiles  int    `json:"wide_files"`
	WideFailed int    `json:"wide_failed"`
	HeroFiles  int    `json:"hero_files"`
	HeroFailed int    `json:"hero_failed"`
	LogoFiles  int    `json:"logo_files"`
	LogoFailed int    `json:"logo_failed"`
}

// Stat walks the cache (sharded, so fast) and counts real files.
func (c *Cache) Stat() Stats {
	c.load()
	st := Stats{Dir: c.Dir}
	c.mu.Lock()
	st.Indexed = len(c.entries)
	for _, e := range c.entries {
		if e.FailedAt != 0 && e.Bytes == 0 {
			st.Failed++
		}
		if e.WideFailedAt != 0 && e.WideBytes == 0 {
			st.WideFailed++
		}
		if e.HeroFailedAt != 0 && e.HeroBytes == 0 {
			st.HeroFailed++
		}
		if e.LogoFailedAt != 0 && e.LogoBytes == 0 {
			st.LogoFailed++
		}
	}
	c.mu.Unlock()
	_ = filepath.WalkDir(c.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(p) == manifestName {
			return nil
		}
		if info, err := d.Info(); err == nil {
			st.Files++
			st.Bytes += info.Size()
			barename := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			if strings.HasSuffix(barename, heroSuffix) {
				st.HeroFiles++
			} else if strings.HasSuffix(barename, logoSuffix) {
				st.LogoFiles++
			} else if strings.HasSuffix(barename, wideSuffix) {
				st.WideFiles++
			}
		}
		return nil
	})
	return st
}

// Prune deletes files whose game id is not in keep; returns removed count.
func (c *Cache) Prune(keep map[string]bool) (removed int, bytesFreed int64, err error) {
	c.load()
	var victims []string
	_ = filepath.WalkDir(c.Dir, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(p) == manifestName {
			return nil
		}
		rel, rerr := filepath.Rel(c.Dir, p)
		if rerr != nil {
			return nil
		}
		id := gameFileID(strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)))
		if !keep[id] {
			victims = append(victims, p)
		}
		return nil
	})
	for _, p := range victims {
		var size int64
		if st, serr := os.Stat(p); serr == nil {
			size = st.Size()
		}
		if rerr := os.Remove(p); rerr == nil {
			removed++
			bytesFreed += size
		} else if !os.IsNotExist(rerr) && err == nil {
			err = rerr
		}
	}
	c.mu.Lock()
	for id := range c.entries {
		if !keep[id] {
			delete(c.entries, id)
		}
	}
	c.mu.Unlock()
	c.save()
	c.removeEmptyShards()
	return removed, bytesFreed, err
}

// Clear wipes the whole cache directory.
func (c *Cache) Clear() error {
	c.mu.Lock()
	c.entries = map[string]Entry{}
	c.loaded = true
	c.mu.Unlock()
	if err := os.RemoveAll(c.Dir); err != nil {
		return err
	}
	return os.MkdirAll(c.Dir, 0o755)
}

// removeEmptyShards drops empty aa/bb dirs left by prune/clear.
func (c *Cache) removeEmptyShards() {
	seen := map[string]bool{}
	_ = filepath.WalkDir(c.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == c.Dir {
			return nil
		}
		rel, rerr := filepath.Rel(c.Dir, p)
		if rerr != nil {
			return nil
		}
		if strings.Count(rel, string(os.PathSeparator)) == 0 {
			seen[p] = true
		}
		return nil
	})
	lv2 := []string{}
	_ = filepath.WalkDir(c.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == c.Dir {
			return nil
		}
		rel, rerr := filepath.Rel(c.Dir, p)
		if rerr != nil {
			return nil
		}
		if strings.Count(rel, string(os.PathSeparator)) == 1 {
			lv2 = append(lv2, p)
		}
		return nil
	})
	for _, p := range lv2 {
		entries, err := os.ReadDir(p)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(p)
		}
	}
	for p := range seen {
		entries, err := os.ReadDir(p)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(p)
		}
	}
}

// Result counts one Sync run (icons + wides + heroes + logos tracked separately).
type Result struct {
	Total       int   `json:"total"`
	Fetched     int   `json:"fetched"`
	Skipped     int   `json:"skipped"`
	Failed      int   `json:"failed"`
	WideFetched int   `json:"wide_fetched"`
	WideSkipped int   `json:"wide_skipped"`
	WideFailed  int   `json:"wide_failed"`
	HeroFetched int   `json:"hero_fetched"`
	HeroSkipped int   `json:"hero_skipped"`
	HeroFailed  int   `json:"hero_failed"`
	LogoFetched int   `json:"logo_fetched"`
	LogoSkipped int   `json:"logo_skipped"`
	LogoFailed  int   `json:"logo_failed"`
	Bytes       int64 `json:"bytes"`
}

// Sync downloads missing art for games with a bounded worker pool.
// All flavors (icon + wide capsule + wide hero + logo overlay) are fetched
// per game; Refresh forces re-download of hits. Limit caps how many games
// are attempted (0 = all). onEach, when non-nil, reports per-game progress
// (ok = any flavor hit).
func (c *Cache) Sync(ctx context.Context, games []Game, refresh bool, limit int, onEach func(id string, ok bool)) Result {
	c.load()
	if c.Concurrency <= 0 {
		c.Concurrency = DefaultConcurrency
	}
	if c.Concurrency > MaxConcurrency {
		c.Concurrency = MaxConcurrency
	}
	now := time.Now()
	type job struct {
		g        Game
		wantIcon bool
		wantWide bool
		wantHero bool
		wantLogo bool
	}
	jobs := make(chan job)
	var fetched, skipped, failed, wideFetched, wideSkipped, wideFailed, heroFetched, heroSkipped, heroFailed, logoFetched, logoSkipped, logoFailed int64
	var nbytes int64

	// Pre-filter cheaply (disk stat only, no network) to keep the queue small.
	queue := make([]Game, 0, len(games))
	wantIcon := map[string]bool{}
	wantWide := map[string]bool{}
	wantHero := map[string]bool{}
	wantLogo := map[string]bool{}
	for _, g := range games {
		if strings.TrimSpace(g.ID) == "" {
			continue
		}
		wi, ww, wh, wl := true, true, true, true
		if p, ok := c.Hit(g.ID); ok && !refresh && p != "" {
			atomic.AddInt64(&skipped, 1)
			wi = false
		} else if !refresh && c.FailedRecently(g.ID, ArtIcon, now) {
			atomic.AddInt64(&skipped, 1)
			wi = false
		}
		if p, ok := c.HitWide(g.ID); ok && !refresh && p != "" {
			atomic.AddInt64(&wideSkipped, 1)
			ww = false
		} else if !refresh && c.FailedRecently(g.ID, ArtWide, now) {
			atomic.AddInt64(&wideSkipped, 1)
			ww = false
		}
		if p, ok := c.HitHero(g.ID); ok && !refresh && p != "" {
			atomic.AddInt64(&heroSkipped, 1)
			wh = false
		} else if !refresh && c.FailedRecently(g.ID, ArtHero, now) {
			atomic.AddInt64(&heroSkipped, 1)
			wh = false
		}
		if p, ok := c.HitLogo(g.ID); ok && !refresh && p != "" {
			atomic.AddInt64(&logoSkipped, 1)
			wl = false
		} else if !refresh && c.FailedRecently(g.ID, ArtLogo, now) {
			atomic.AddInt64(&logoSkipped, 1)
			wl = false
		}
		if !wi && !ww && !wh && !wl {
			continue
		}
		wantIcon[g.ID], wantWide[g.ID], wantHero[g.ID], wantLogo[g.ID] = wi, ww, wh, wl
		queue = append(queue, g)
		if limit > 0 && len(queue) >= limit {
			break
		}
	}
	res := Result{Total: len(games), Skipped: int(skipped), WideSkipped: int(wideSkipped), HeroSkipped: int(heroSkipped), LogoSkipped: int(logoSkipped)}
	if len(queue) == 0 {
		return res
	}

	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	if provider == "" {
		provider = ProviderAuto
	}
	if provider == ProviderOff {
		res.Skipped += len(queue)
		res.HeroSkipped += len(queue)
		res.LogoSkipped += len(queue)
		return res
	}

	var wg sync.WaitGroup
	for w := 0; w < c.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-jobs:
					if !ok {
						return
					}
					okIcon, okWide, okHero, okLogo := true, true, true, true
					if j.wantIcon {
						if n, ok := c.fetchOne(ctx, j.g, provider, ArtIcon); ok {
							atomic.AddInt64(&fetched, 1)
							atomic.AddInt64(&nbytes, n)
						} else {
							atomic.AddInt64(&failed, 1)
							okIcon = false
						}
					}
					if j.wantWide {
						if n, ok := c.fetchOne(ctx, j.g, provider, ArtWide); ok {
							atomic.AddInt64(&wideFetched, 1)
							atomic.AddInt64(&nbytes, n)
						} else {
							atomic.AddInt64(&wideFailed, 1)
							okWide = false
						}
					}
					if j.wantHero {
						if n, ok := c.fetchOne(ctx, j.g, provider, ArtHero); ok {
							atomic.AddInt64(&heroFetched, 1)
							atomic.AddInt64(&nbytes, n)
						} else {
							atomic.AddInt64(&heroFailed, 1)
							okHero = false
						}
					}
					if j.wantLogo {
						if n, ok := c.fetchOne(ctx, j.g, provider, ArtLogo); ok {
							atomic.AddInt64(&logoFetched, 1)
							atomic.AddInt64(&nbytes, n)
						} else {
							atomic.AddInt64(&logoFailed, 1)
							okLogo = false
						}
					}
					if onEach != nil {
						onEach(j.g.ID, okIcon || okWide || okHero || okLogo)
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, g := range queue {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{g: g, wantIcon: wantIcon[g.ID], wantWide: wantWide[g.ID], wantHero: wantHero[g.ID], wantLogo: wantLogo[g.ID]}:
			}
		}
	}()
	wg.Wait()
	res.Fetched = int(fetched)
	res.Failed = int(failed)
	res.WideFetched = int(wideFetched)
	res.WideFailed = int(wideFailed)
	res.HeroFetched = int(heroFetched)
	res.HeroFailed = int(heroFailed)
	res.LogoFetched = int(logoFetched)
	res.LogoFailed = int(logoFailed)
	res.Bytes = nbytes
	c.save()
	return res
}

// chain returns providers in try order for the configured mode.
func (c *Cache) chain(mode string) []string {
	switch mode {
	case ProviderLibretro:
		return []string{ProviderLibretro}
	case ProviderSteamGrid:
		return []string{ProviderSteamGrid}
	default: // auto: free bulk source first, premium fills gaps
		if strings.TrimSpace(c.GridDBKey) != "" {
			return []string{ProviderLibretro, ProviderSteamGrid}
		}
		return []string{ProviderLibretro}
	}
}

// fetchOne tries each provider's URL candidates until one validates.
func (c *Cache) fetchOne(ctx context.Context, g Game, mode string, kind ArtKind) (int64, bool) {
	for _, p := range c.chain(mode) {
		if p == ProviderSteamGrid {
			if n, ok := c.fetchViaGridDB(ctx, g, kind); ok {
				return n, true
			}
			continue
		}
		urls := candidatesFor(p, g, c.GridDBKey, kind)
		if len(urls) > maxCandidatesProbe {
			urls = urls[:maxCandidatesProbe]
		}
		for _, u := range urls {
			n, ok := c.download(ctx, g, p, u, kind)
			if ok {
				return n, true
			}
			select {
			case <-ctx.Done():
				c.markFailed(g.ID, kind)
				return 0, false
			default:
			}
		}
	}
	c.markFailed(g.ID, kind)
	return 0, false
}

// download GETs url with retries and stores it when it looks like an image.
func (c *Cache) download(ctx context.Context, g Game, provider, url string, kind ArtKind) (int64, bool) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, false
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "cartridge-icons/1.0")
		if provider == ProviderSteamGrid && strings.TrimSpace(c.GridDBKey) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.GridDBKey))
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		n, ok := c.storeResponse(g, provider, url, kind, resp)
		_ = resp.Body.Close()
		if ok {
			return n, true
		}
		lastErr = fmt.Errorf("bad response for %s", url)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
			break // probing candidates: 404 means "try next", no retry
		}
	}
	_ = lastErr
	return 0, false
}

// storeResponse validates and writes one HTTP response to the shard path.
// Only the requested flavor's fields are touched, so icon/wide/hero/logo
// rows coexist in one manifest entry.
func (c *Cache) storeResponse(g Game, provider, url string, kind ArtKind, resp *http.Response) (int64, bool) {
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return 0, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil || int64(len(data)) > maxImageBytes || int64(len(data)) < minImageBytes {
		return 0, false
	}
	if !isImage(data) {
		return 0, false
	}
	dest := filepath.Join(c.Dir, shardPathFor(g.ID, kind))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, false
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return 0, false
	}
	if err := os.Rename(tmp, dest); err != nil {
		return 0, false
	}
	rel, _ := filepath.Rel(c.Dir, dest)
	now := time.Now().Unix()
	c.mu.Lock()
	e := c.entries[g.ID]
	if kind == ArtHero {
		e.HeroFile, e.HeroURL, e.HeroProvider = rel, url, provider
		e.HeroFetchedAt, e.HeroBytes = now, int64(len(data))
		e.HeroFailCount, e.HeroFailedAt = 0, 0
	} else if kind == ArtWide {
		e.WideFile, e.WideURL, e.WideProvider = rel, url, provider
		e.WideFetchedAt, e.WideBytes = now, int64(len(data))
		e.WideFailCount, e.WideFailedAt = 0, 0
	} else if kind == ArtLogo {
		e.LogoFile, e.LogoURL, e.LogoProvider = rel, url, provider
		e.LogoFetchedAt, e.LogoBytes = now, int64(len(data))
		e.LogoFailCount, e.LogoFailedAt = 0, 0
	} else {
		e.File, e.URL, e.Provider = rel, url, provider
		e.FetchedAt, e.Bytes = now, int64(len(data))
		e.FailCount, e.FailedAt = 0, 0
	}
	c.entries[g.ID] = e
	c.mu.Unlock()
	return int64(len(data)), true
}

// markFailed records a miss with backoff (checked by FailedRecently).
func (c *Cache) markFailed(gameID string, kind ArtKind) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[gameID]
	now := time.Now().Unix()
	if kind == ArtHero {
		e.HeroFailCount++
		e.HeroFailedAt = now
		if e.HeroBytes == 0 {
			e.HeroFile = ""
		}
	} else if kind == ArtWide {
		e.WideFailCount++
		e.WideFailedAt = now
		if e.WideBytes == 0 {
			e.WideFile = ""
		}
	} else if kind == ArtLogo {
		e.LogoFailCount++
		e.LogoFailedAt = now
		if e.LogoBytes == 0 {
			e.LogoFile = ""
		}
	} else {
		e.FailCount++
		e.FailedAt = now
		// Drop stale success data: a game that 404s everywhere has no art.
		if e.Bytes == 0 {
			e.File = ""
		}
	}
	c.entries[gameID] = e
}

// isImage sniffs PNG/JPEG/WebP magic (rejects HTML error pages).
func isImage(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return true
	}
	if bytes.HasPrefix(data, []byte("\xff\xd8\xff")) {
		return true
	}
	if bytes.HasPrefix(data, []byte("RIFF")) && bytes.HasPrefix(data[8:], []byte("WEBP")) {
		return true
	}
	if bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return true
	}
	return false
}

// SortedIDs returns manifest ids in stable order (for tests/debug).
func (c *Cache) SortedIDs() []string {
	c.load()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.entries))
	for k := range c.entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
