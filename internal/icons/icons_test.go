package icons

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShardPathStable(t *testing.T) {
	a := ShardPath("abc123")
	b := ShardPath("abc123")
	if a != b {
		t.Fatalf("shard not stable: %q vs %q", a, b)
	}
	if !strings.HasSuffix(a, "abc123.png") {
		t.Fatalf("shard = %q", a)
	}
	if got := ShardPath("ABC123"); got != ShardPath("abc123") {
		t.Fatalf("shard must be case-insensitive")
	}
}

func TestDefaultDirRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache-test")
	if got := DefaultDir(); got != "/tmp/xdg-cache-test/cartridge/icons" {
		t.Fatalf("dir = %q", got)
	}
}

func TestLibretroCandidates(t *testing.T) {
	urls := libretroCandidates(Game{ID: "x", Title: "Super Mario World", ConsoleID: "snes"}, ArtIcon)
	if len(urls) == 0 {
		t.Fatalf("no candidates")
	}
	if !strings.Contains(urls[0], "Nintendo_-_Super_Nintendo_Entertainment_System") {
		t.Fatalf("repo wrong: %q", urls[0])
	}
	if !strings.Contains(urls[0], "Named_Boxarts") {
		t.Fatalf("first category must be boxart: %q", urls[0])
	}
	if !strings.Contains(urls[0], "Super%20Mario%20World") {
		t.Fatalf("bare title missing: %q", urls[0])
	}
}

func TestLibretroStemFirstWithRegion(t *testing.T) {
	// No-Intro stems keep the region: the exact stem must lead.
	g := Game{ID: "x", Title: "ActRaiser", ConsoleID: "snes", Stem: "ActRaiser (USA)", Region: "USA"}
	urls := libretroCandidates(g, ArtIcon)
	if len(urls) == 0 {
		t.Fatalf("no candidates")
	}
	first := urls[0]
	if !strings.Contains(first, "ActRaiser%20%28USA%29") && !strings.Contains(first, "ActRaiser (USA)") {
		t.Fatalf("stem+region must lead: %q", first)
	}
}

func TestLibretroRegionReattached(t *testing.T) {
	// Title was parsed ("Zelda" + Region "USA"): repo name has the suffix.
	g := Game{ID: "x", Title: "Zelda", ConsoleID: "nes", Region: "USA"}
	found := false
	for _, u := range libretroCandidates(g, ArtIcon) {
		if strings.Contains(u, "Zelda%20%28USA%29") || strings.Contains(u, "Zelda (USA)") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("region-suffixed candidate missing")
	}
}

func TestLibretroUnknownConsole(t *testing.T) {
	if urls := libretroCandidates(Game{ID: "x", Title: "Foo", ConsoleID: "wat"}, ArtIcon); len(urls) != 0 {
		t.Fatalf("unknown console must yield no urls, got %d", len(urls))
	}
}

func TestLibretroHeroCandidatesUseSnaps(t *testing.T) {
	g := Game{ID: "x", Title: "ActRaiser", ConsoleID: "snes", Stem: "ActRaiser (USA)", Region: "USA"}
	urls := libretroCandidates(g, ArtHero)
	if len(urls) == 0 {
		t.Fatalf("no hero candidates")
	}
	for _, u := range urls {
		if strings.Contains(u, "Named_Boxarts") {
			t.Fatalf("portrait boxart must never be a hero: %q", u)
		}
		if !strings.Contains(u, "Named_Snaps") {
			t.Fatalf("hero must come from snaps: %q", u)
		}
	}
	if !strings.Contains(urls[0], "ActRaiser%20%28USA%29") {
		t.Fatalf("stem+region must lead heroes: %q", urls[0])
	}
}

func TestShardPathHero(t *testing.T) {
	if got := ShardPathHero("abc123"); got != filepath.Join("ab", "c1", "abc123_hero.png") {
		t.Fatalf("hero shard = %q", got)
	}
	if gameFileID("abc123_hero") != "abc123" {
		t.Fatalf("hero suffix not stripped")
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitizeLibretro("A/B:C*D"); got != "A_B_C_D" {
		t.Fatalf("sanitize = %q", got)
	}
}

// png1x1 is a minimal valid PNG.
var png1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xd7, 0x63, 0xf8, 0xff, 0xff, 0x3f,
	0x00, 0x05, 0xfe, 0x02, 0xfe, 0xdc, 0xcc, 0x59,
	0xe7, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestIsImageRejectsHTML(t *testing.T) {
	if !isImage(append(png1x1, make([]byte, 600)...)) {
		t.Fatalf("png not recognized")
	}
	if isImage([]byte("<html><body>not found" + strings.Repeat("x", 600))) {
		t.Fatalf("html accepted as image")
	}
}

func TestSyncSkipsHitsAndCachesMisses(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	c.Concurrency = 2
	// Seed a hit on disk.
	hitID := "hitgame1234"
	hitPath := filepath.Join(dir, ShardPath(hitID))
	if err := os.MkdirAll(filepath.Dir(hitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hitPath, append(png1x1, make([]byte, 600)...), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	games := []Game{
		{ID: hitID, Title: "Hit", ConsoleID: "snes"},
		// Unknown console: no candidates, no network, instant miss.
		{ID: "missgame5678", Title: "Miss", ConsoleID: "no-such-console"},
	}
	_ = srv
	// Point libretro at nothing by using unknown console for miss: no urls.
	res := c.Sync(context.Background(), games, false, 0, nil)
	if res.Skipped < 1 {
		t.Fatalf("hit must be skipped: %+v", res)
	}
	if calls != 0 {
		t.Fatalf("hits must not touch network, calls=%d", calls)
	}
	if !c.FailedRecently("missgame5678", ArtIcon, time.Now()) {
		t.Fatalf("miss must be recorded for backoff")
	}
	// Second run: miss is in cooldown, no more work.
	res2 := c.Sync(context.Background(), games, false, 0, nil)
	if res2.Fetched != 0 || res2.Failed != 0 {
		t.Fatalf("cooldown must skip failures: %+v", res2)
	}
}

func TestSyncTracksHeroSeparately(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	c.Concurrency = 2
	// Unknown console: no candidates for either flavor, instant misses.
	games := []Game{{ID: "heromiss01", Title: "Miss", ConsoleID: "no-such-console"}}
	res := c.Sync(context.Background(), games, false, 0, nil)
	if res.Failed != 1 || res.HeroFailed != 1 {
		t.Fatalf("both flavors must miss: %+v", res)
	}
	now := time.Now()
	if !c.FailedRecently("heromiss01", ArtIcon, now) || !c.FailedRecently("heromiss01", ArtHero, now) {
		t.Fatalf("both backoffs must be recorded")
	}
	// Icon hit must not imply hero hit: hero is still attempted.
	hitPath := filepath.Join(dir, ShardPath("heromiss01"))
	if err := os.MkdirAll(filepath.Dir(hitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hitPath, append(png1x1, make([]byte, 600)...), 0o644); err != nil {
		t.Fatal(err)
	}
	// Clear only the hero backoff by hand: hero retries, icon skips.
	c.mu.Lock()
	e := c.entries["heromiss01"]
	e.HeroFailedAt = 0
	e.HeroFailCount = 0
	c.entries["heromiss01"] = e
	c.mu.Unlock()
	res2 := c.Sync(context.Background(), games, false, 0, nil)
	if res2.Skipped != 1 || res2.HeroFailed != 1 {
		t.Fatalf("icon skips, hero retries: %+v", res2)
	}
}

func TestHeroHitAndPrune(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	heroPath := filepath.Join(dir, ShardPathHero("hero0001"))
	if err := os.MkdirAll(filepath.Dir(heroPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(heroPath, append(png1x1, make([]byte, 600)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.HitHero("hero0001"); !ok {
		t.Fatalf("hero must hit")
	}
	if _, ok := c.Hit("hero0001"); ok {
		t.Fatalf("hero file must not count as icon")
	}
	if st := c.Stat(); st.HeroFiles != 1 || st.Files != 1 {
		t.Fatalf("stat = %+v", st)
	}
	// Prune by game id keeps the hero (suffix stripped for the lookup).
	if removed, _, err := c.Prune(map[string]bool{"hero0001": true}); err != nil || removed != 0 {
		t.Fatalf("live hero pruned: removed=%d err=%v", removed, err)
	}
	if removed, _, err := c.Prune(map[string]bool{}); err != nil || removed != 1 {
		t.Fatalf("dead hero kept: removed=%d err=%v", removed, err)
	}
}

func TestWideHitAndPrune(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	widePath := filepath.Join(dir, ShardPathWide("wide0001"))
	if err := os.MkdirAll(filepath.Dir(widePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(widePath, append(png1x1, make([]byte, 600)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.HitWide("wide0001"); !ok {
		t.Fatalf("wide must hit")
	}
	if _, ok := c.Hit("wide0001"); ok {
		t.Fatalf("wide file must not count as icon")
	}
	if st := c.Stat(); st.WideFiles != 1 || st.Files != 1 {
		t.Fatalf("stat = %+v", st)
	}
	// Failure backoff is per-flavor: a wide miss must not imply an icon miss.
	if c.FailedRecently("wide0001", ArtWide, time.Now()) {
		t.Fatalf("hit must not back off")
	}
	// Prune by game id keeps the wide (suffix stripped for the lookup).
	if removed, _, err := c.Prune(map[string]bool{"wide0001": true}); err != nil || removed != 0 {
		t.Fatalf("live wide pruned: removed=%d err=%v", removed, err)
	}
	if removed, _, err := c.Prune(map[string]bool{}); err != nil || removed != 1 {
		t.Fatalf("dead wide kept: removed=%d err=%v", removed, err)
	}
}

func TestSyncTracksWideSeparately(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	c.Concurrency = 2
	// Unknown console: no candidates for any flavor, instant misses.
	games := []Game{{ID: "widemiss01", Title: "Miss", ConsoleID: "no-such-console"}}
	res := c.Sync(context.Background(), games, false, 0, nil)
	if res.Failed != 1 || res.WideFailed != 1 || res.HeroFailed != 1 || res.LogoFailed != 1 {
		t.Fatalf("all flavors must miss: %+v", res)
	}
	now := time.Now()
	if !c.FailedRecently("widemiss01", ArtWide, now) {
		t.Fatalf("wide backoff must be recorded")
	}
	// Second run: all misses are in cooldown, no more work.
	res2 := c.Sync(context.Background(), games, false, 0, nil)
	if res2.Fetched != 0 || res2.WideFetched != 0 || res2.Failed != 0 || res2.WideFailed != 0 {
		t.Fatalf("cooldown must skip failures: %+v", res2)
	}
}

func TestPruneKeepsOnlyWanted(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	for _, id := range []string{"keep1111", "drop2222"} {
		p := filepath.Join(dir, ShardPath(id))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append(png1x1, make([]byte, 600)...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	removed, _, err := c.Prune(map[string]bool{"keep1111": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
	if _, ok := c.Hit("keep1111"); !ok {
		t.Fatalf("kept file gone")
	}
	if _, ok := c.Hit("drop2222"); ok {
		t.Fatalf("dropped file kept")
	}
}

func TestDownloadStoresValidImage(t *testing.T) {	payload := append(append([]byte{}, png1x1...), make([]byte, 600)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := New(dir)
	n, ok := c.download(context.Background(), Game{ID: "dl123456"}, ProviderLibretro, srv.URL+"/x.png", ArtIcon)
	if !ok || n != int64(len(payload)) {
		t.Fatalf("download = %d %v", n, ok)
	}
	if _, ok := c.Hit("dl123456"); !ok {
		t.Fatalf("stored file not a hit")
	}
}

func TestDownloadRejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "<html>"+strings.Repeat("x", 800))
	}))
	defer srv.Close()
	c := New(t.TempDir())
	if _, ok := c.download(context.Background(), Game{ID: "bad9999"}, ProviderLibretro, srv.URL, ArtIcon); ok {
		t.Fatalf("html must be rejected")
	}
}

func TestLegacyManifestDropsPureFailures(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  "goodgame0001": {"file": "go/od/goodgame0001.png", "bytes": 1234},
  "badgame000002": {"file": "", "fail_count": 1, "failed_at": 1791654026}
}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(dir)
	c.load()
	if len(c.SortedIDs()) != 1 || c.SortedIDs()[0] != "goodgame0001" {
		t.Fatalf("legacy misses must be dropped, kept=%q", c.SortedIDs())
	}
	if c.FailedRecently("badgame000002", ArtIcon, time.Now()) {
		t.Fatalf("dropped miss must not back off")
	}
}

func TestManifestRoundTripKeepsVersion(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	c.mu.Lock()
	c.entries["a"] = Entry{File: "x", Bytes: 10}
	c.mu.Unlock()
	c.save()
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version"`) {
		t.Fatalf("manifest must carry version: %s", raw)
	}
	c2 := New(dir)
	c2.load()
	if len(c2.SortedIDs()) != 1 {
		t.Fatalf("round trip lost entries: %q", c2.SortedIDs())
	}
}
