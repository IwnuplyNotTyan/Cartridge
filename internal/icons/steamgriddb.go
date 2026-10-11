package icons

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SteamGridDB: optional premium source. Needs a personal API key
// (https://www.steamgriddb.com/profile/preferences/api, free).
// Quality is higher than Libretro (hand-made capsules/logos), but the
// API is rate-limited, so for huge libraries it only fills gaps the
// free CDN missed. Set provider=steamgriddb to prefer it, auto (with a
// key) to use it as fallback.

const steamGridBase = "https://www.steamgriddb.com/api/v2"

type gridSearchHit struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type gridSearchResp struct {
	Data []gridSearchHit `json:"data"`
}

type gridArt struct {
	URL string `json:"url"`
}

type gridArtResp struct {
	Data []gridArt `json:"data"`
}

// fetchViaGridDB searches the game then downloads the best image:
// portrait capsules for icons, /heroes/ for backdrops, /logos/ for logo
// overlays. Returns bytes written and true on success.
func (c *Cache) fetchViaGridDB(ctx context.Context, g Game, kind ArtKind) (int64, bool) {
	key := strings.TrimSpace(c.GridDBKey)
	if key == "" {
		return 0, false
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	id, ok := c.gridSearchCached(ctx, client, key, g)
	if !ok {
		return 0, false
	}
	imgURL, ok := c.gridBestArt(ctx, client, key, id, kind)
	if !ok {
		return 0, false
	}
	// Download the CDN image (no auth header needed there).
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return 0, false
	}
	req.Header.Set("User-Agent", "cartridge-icons/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	return c.storeResponse(g, ProviderSteamGrid, imgURL, kind, resp)
}

// gridSearchCached memoizes title -> game id so one Sync (icon+hero+logo
// per game) costs a single autocomplete request, not three. This matters:
// the API is rate-limited and bulk libraries already stress it.
func (c *Cache) gridSearchCached(ctx context.Context, client *http.Client, key string, g Game) (int, bool) {
	q := strings.ToLower(strings.TrimSpace(g.Title))
	if q != "" {
		c.mu.Lock()
		if c.gridIDs == nil {
			c.gridIDs = map[string]int{}
		}
		if id, ok := c.gridIDs[q]; ok {
			c.mu.Unlock()
			if id == 0 {
				return 0, false
			}
			return id, true
		}
		c.mu.Unlock()
		id, ok := c.gridSearch(ctx, client, key, g)
		c.mu.Lock()
		if ok {
			c.gridIDs[q] = id
		} else {
			c.gridIDs[q] = 0
		}
		c.mu.Unlock()
		return id, ok
	}
	return c.gridSearch(ctx, client, key, g)
}

// gridSearch finds the SteamGridDB game id for a title.
func (c *Cache) gridSearch(ctx context.Context, client *http.Client, key string, g Game) (int, bool) {
	q := strings.TrimSpace(g.Title)
	if q == "" {
		return 0, false
	}
	u := steamGridBase + "/search/autocomplete/" + url.PathEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", "cartridge-icons/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return 0, false
	}
	var sr gridSearchResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&sr); err != nil {
		return 0, false
	}
	if len(sr.Data) == 0 {
		return 0, false
	}
	// Prefer exact case-insensitive match, else first hit.
	for _, h := range sr.Data {
		if strings.EqualFold(strings.TrimSpace(h.Name), q) {
			return h.ID, true
		}
	}
	return sr.Data[0].ID, true
}

// gridArtURL builds the per-flavor art request URL (extracted so tests
// can assert endpoint + dimensions without network).
func gridArtURL(base, endpoint string, gameID int, query string) string {
	return fmt.Sprintf("%s/%s/game/%d?%s", strings.TrimSuffix(base, "/"), endpoint, gameID, query)
}

// gridBestArt picks one static image per flavor: /grids/ capsules for
// icons, /heroes/ backdrops for heroes, /logos/ overlays for logos.
// (The old code asked /grids/ with hero dimensions, which matches nothing
// — heroes never came from SteamGridDB. Each flavor has its own endpoint.)
func (c *Cache) gridBestArt(ctx context.Context, client *http.Client, key string, gameID int, kind ArtKind) (string, bool) {
	return c.gridBestArtFrom(ctx, client, key, steamGridBase, gameID, kind)
}

// gridBestArtFrom is gridBestArt against an arbitrary API base (tests
// point it at an httptest server; production passes steamGridBase).
func (c *Cache) gridBestArtFrom(ctx context.Context, client *http.Client, key, base string, gameID int, kind ArtKind) (string, bool) {
	endpoint := "grids"
	query := "dimensions=600x900,460x215,920x430&types=static&limit=1"
	if kind == ArtHero {
		endpoint = "heroes"
		// Only these three are valid hero dimensions per the API
		// (1920x620, 3840x1240, 1600x650): any other value 400s the
		// whole request, so heroes came back empty for every game.
		query = "dimensions=3840x1240,1920x620,1600x650&types=static&limit=1"
	} else if kind == ArtLogo {
		endpoint = "logos"
		query = "types=static&limit=1"
	}
	u := gridArtURL(base, endpoint, gameID, query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", "cartridge-icons/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return "", false
	}
	var ar gridArtResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ar); err != nil {
		return "", false
	}
	if len(ar.Data) == 0 || strings.TrimSpace(ar.Data[0].URL) == "" {
		return "", false
	}
	return ar.Data[0].URL, true
}

// gridCooldown keeps SteamGridDB 429s from stalling bulk syncs.
var _ = time.Second
