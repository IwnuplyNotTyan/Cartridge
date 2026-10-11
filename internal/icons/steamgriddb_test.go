package icons

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Valid artwork dimensions accepted by the SteamGridDB API. Any other
// value 400s the whole request, so the requested dimensions must stay
// within these sets or the flavor silently fetches nothing.
var (
	validGridDims = map[string]bool{
		"600x900": true, "460x215": true, "920x430": true,
		"512x512": true, "1024x1024": true,
	}
	validHeroDims = map[string]bool{
		"3840x1240": true, "1920x620": true, "1600x650": true,
	}
)

// TestGridBestArtHeroEndpoint ensures heroes are asked from /heroes/ with
// only valid hero dimensions (regression: /grids/ with hero dimensions
// matched nothing, then an invalid 960x310 dimension 400s the request —
// both left HeroFetched at 0 for every game under provider steamgriddb).
func TestGridBestArtHeroEndpoint(t *testing.T) {
	var gotPath, gotDims string
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(append(png1x1, make([]byte, 600)...))
	}))
	defer img.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotDims = r.URL.Path, r.URL.Query().Get("dimensions")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"url":%q}]}`, img.URL+"/hero.png")
	}))
	defer api.Close()

	c := New(t.TempDir())
	got, ok := c.gridBestArtFrom(context.Background(), api.Client(), "key", api.URL, 123, ArtHero)
	if !ok || !strings.HasPrefix(got, img.URL) {
		t.Fatalf("hero art = %q %v", got, ok)
	}
	if !strings.HasPrefix(gotPath, "/heroes/game/123") {
		t.Fatalf("hero must hit /heroes/, got path %q dims %q", gotPath, gotDims)
	}
	// Every requested dimension must be API-valid, otherwise the whole
	// request 400s and heroes stay empty for all games.
	for _, d := range strings.Split(gotDims, ",") {
		if !validHeroDims[d] {
			t.Fatalf("invalid hero dimension %q in %q", d, gotDims)
		}
	}
	if strings.Contains(gotDims, "960x310") {
		t.Fatalf("960x310 is not a hero dimension: %q", gotDims)
	}
}

// TestGridBestArtIconEndpoint guards the icon flavor the same way.
func TestGridBestArtIconEndpoint(t *testing.T) {
	var gotPath, gotDims string
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(append(png1x1, make([]byte, 600)...))
	}))
	defer img.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotDims = r.URL.Path, r.URL.Query().Get("dimensions")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"url":%q}]}`, img.URL+"/grid.png")
	}))
	defer api.Close()

	c := New(t.TempDir())
	if _, ok := c.gridBestArtFrom(context.Background(), api.Client(), "key", api.URL, 7, ArtIcon); !ok {
		t.Fatalf("icon art must fetch")
	}
	if !strings.HasPrefix(gotPath, "/grids/game/7") {
		t.Fatalf("icon must hit /grids/, got path %q dims %q", gotPath, gotDims)
	}
	for _, d := range strings.Split(gotDims, ",") {
		if !validGridDims[d] {
			t.Fatalf("invalid grid dimension %q in %q", d, gotDims)
		}
	}
}
