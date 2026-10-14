package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"cartridge/internal/config"
	"cartridge/internal/i18n"
	"cartridge/internal/icons"
)

// writePosterPNG drops a valid gradient PNG at the icon shard path.
// Sized to clear the 512-byte cache floor (smooth gradients compress
// well, so keep it big enough to look like real art).
func writePosterPNG(t *testing.T, dir, gameID string, w, h int) string {
	t.Helper()
	if w < 200 {
		w = 200
	}
	if h < 200 {
		h = 200
	}
	rel := icons.ShardPath(gameID)
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{
				R: uint8(x * 255 / max(w-1, 1)),
				G: uint8(y * 255 / max(h-1, 1)),
				B: 128,
				A: 255,
			})
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	return p
}

func posterTestModel(t *testing.T, dir string) Model {
	t.Helper()
	i18n.Set(i18n.En)
	cfg := config.DefaultSteamConfig()
	cfg.IconsDir = dir
	m := New(sampleGames(), "/x", "/tmp/lib.json", WithSteam(cfg))
	m = sizedModel(t, m)
	m.focus = PaneMeta
	return m
}

func TestPosterRowsAppearUnderTitle(t *testing.T) {
	dir := t.TempDir()
	writePosterPNG(t, dir, "aaa111", 60, 90)
	m := posterTestModel(t, dir)
	if len(m.metaRows) == 0 {
		t.Fatal("no meta rows")
	}
	if m.metaRows[0].kind != "title" {
		t.Fatalf("first row = %q, want title", m.metaRows[0].kind)
	}
	if len(m.metaRows) < 2 || m.metaRows[1].kind != "poster" {
		kinds := []string{}
		for _, r := range m.metaRows {
			if !r.sep {
				kinds = append(kinds, r.kind)
			}
		}
		t.Fatalf("second row must be poster, kinds=%q", kinds)
	}
	row := m.metaRows[1]
	if len(row.lines) == 0 || len(row.lines) > posterMaxRowsHard {
		t.Fatalf("poster lines = %d, want 1..%d", len(row.lines), posterMaxRowsHard)
	}
	for _, ln := range row.lines {
		if got := lipgloss.Width(ln); got == 0 {
			t.Fatal("poster line has zero width")
		}
		if !strings.Contains(ln, "▀") {
			t.Fatalf("half-block preview must contain ▀, got %q", ln)
		}
	}
}

// TestPosterScalesWithWidth renders the same file at two widths: the
// output must follow the requested width (no fixed-size preview), with
// proportionally more rows when wider.
func TestPosterScalesWithWidth(t *testing.T) {
	dir := t.TempDir()
	p := writePosterPNG(t, dir, "aaa111", 200, 200)
	narrow := renderPoster(p, 16, posterMaxRowsHard)
	wide := renderPoster(p, 32, posterMaxRowsHard)
	if len(narrow) == 0 || len(wide) == 0 {
		t.Fatalf("both renders must produce lines: %d vs %d", len(narrow), len(wide))
	}
	for _, ln := range narrow {
		if got := lipgloss.Width(ln); got != 16 {
			t.Fatalf("narrow line width = %d, want 16", got)
		}
	}
	for _, ln := range wide {
		if got := lipgloss.Width(ln); got != 32 {
			t.Fatalf("wide line width = %d, want 32", got)
		}
	}
	if len(wide) <= len(narrow) {
		t.Fatalf("wider pane must give more rows: narrow=%d wide=%d", len(narrow), len(wide))
	}
}

// TestPosterCropsTallImage keeps a pathological banner within the cap
// (center crop, aspect kept) instead of growing without bound.
func TestPosterCropsTallImage(t *testing.T) {
	dir := t.TempDir()
	p := writePosterPNG(t, dir, "aaa111", 200, 1000)
	lines := renderPoster(p, 30, 12)
	if len(lines) != 12 {
		t.Fatalf("tall image must crop to the cap: got %d rows, want 12", len(lines))
	}
	for _, ln := range lines {
		if got := lipgloss.Width(ln); got != 30 {
			t.Fatalf("cropped line width = %d, want 30", got)
		}
		if !strings.Contains(ln, "▀") {
			t.Fatalf("cropped preview must stay half-block, got %q", ln)
		}
	}
}

func TestPosterHiddenWithoutArt(t *testing.T) {
	m := posterTestModel(t, t.TempDir())
	for _, r := range m.metaRows {
		if !r.sep && r.kind == "poster" {
			t.Fatalf("no cache must hide poster, got %+v", r)
		}
	}
}

func TestPosterInvalidImageHidden(t *testing.T) {
	dir := t.TempDir()
	// Fake header-only PNG like preview_test writes: decode must fail.
	rel := icons.ShardPath("aaa111")
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600)...), 0o644); err != nil {
		t.Fatal(err)
	}
	m := posterTestModel(t, dir)
	for _, r := range m.metaRows {
		if !r.sep && r.kind == "poster" {
			t.Fatalf("broken image must hide poster, got %d lines", len(r.lines))
		}
	}
}

func TestPosterRenderFitsPane(t *testing.T) {
	dir := t.TempDir()
	writePosterPNG(t, dir, "aaa111", 60, 90)
	m := posterTestModel(t, dir)
	w := max(m.meta.Width, 20)
	content, _ := m.renderMetaRows()
	for i, ln := range strings.Split(content, "\n") {
		if lipgloss.Width(ln) > w {
			t.Fatalf("poster line %d overflows: %d > %d\n%q", i, lipgloss.Width(ln), w, ln)
		}
	}
	// The image must be visible in the full view.
	if view := m.View(); !strings.Contains(view, "▀") {
		t.Fatalf("view must show poster preview\n%s", view)
	}
}

func TestPosterEnterOpensFile(t *testing.T) {
	dir := t.TempDir()
	writePosterPNG(t, dir, "aaa111", 40, 60)
	m := posterTestModel(t, dir)
	// Cursor starts on title; move to poster.
	found := false
	for i, r := range m.metaRows {
		if !r.sep && r.kind == "poster" {
			m.metaCur = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no poster row")
	}
	m.refreshMeta()
	out, cmd := m.metaSelect()
	m = out.(Model)
	if cmd == nil {
		t.Fatal("enter on poster must return the opener command")
	}
	if !strings.Contains(m.status, "opening") {
		t.Fatalf("status = %q, want opening hint", m.status)
	}
}

// TestPosterRescalesOnResize rebuilds the preview when the terminal
// (and the metadata pane with it) grows: line width must follow.
func TestPosterRescalesOnResize(t *testing.T) {
	dir := t.TempDir()
	writePosterPNG(t, dir, "aaa111", 300, 300)
	m := posterTestModel(t, dir)
	widthOf := func(m Model) int {
		for _, r := range m.metaRows {
			if !r.sep && r.kind == "poster" && len(r.lines) > 0 {
				return lipgloss.Width(r.lines[0])
			}
		}
		return -1
	}
	w1 := widthOf(m)
	m = resizeModel(t, m, 200)
	m.focus = PaneMeta
	w2 := widthOf(m)
	if w1 <= 0 || w2 <= 0 {
		t.Fatalf("poster must exist at both sizes: %d vs %d", w1, w2)
	}
	if w2 <= w1 {
		t.Fatalf("wider terminal must widen the poster: %d vs %d", w1, w2)
	}
}

func TestPosterNoColorASCII(t *testing.T) {
	dir := t.TempDir()
	p := writePosterPNG(t, dir, "aaa111", 40, 60)
	t.Setenv("NO_COLOR", "1")
	// Drop the geometry cache so the ASCII variant regenerates.
	posterCacheMu.Lock()
	posterCache = map[string][]string{}
	posterCacheMu.Unlock()
	lines := renderPoster(p, 20, posterMaxRowsHard)
	if len(lines) == 0 {
		t.Fatal("NO_COLOR must still render ascii")
	}
	if len(lines) > 2*posterMaxRowsHard {
		t.Fatalf("ascii lines = %d, want ≤ %d", len(lines), 2*posterMaxRowsHard)
	}
	for _, ln := range lines {
		if strings.Contains(ln, "▀") || strings.Contains(ln, "\x1b[") {
			t.Fatalf("ascii fallback must be plain, got %q", ln)
		}
	}
}
