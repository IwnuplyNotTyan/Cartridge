package ui

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"
	"sync"

	"cartridge/internal/rom"
	"cartridge/internal/steam"
)

// Poster preview in the metadata pane.
//
// The cached artwork (icon/wide/hero/logo PNGs) is rendered as colored
// half-block cells ("▀", top pixel = foreground, bottom pixel =
// background). One terminal cell shows two image pixels, so even a
// ~30-cell metadata column shows a recognizable poster. This works
// everywhere ASCII/block art works: ssh, tmux, kitty, foot, xterm —
// unlike the Kitty graphics protocol, it never corrupts the viewport
// on scroll and needs no terminal queries or image cleanup.
//
// Geometry: the preview always fills the metadata pane width exactly
// (cols = viewport width minus the 2-cell cursor gutter), height is
// proportional to the source aspect, center-cropped to a viewport
// height driven cap. Resizing the terminal rebuilds the rows, so the
// poster scales with the pane instead of sitting at a fixed size.
// Navigation is per metaRow (the poster is one row), so a tall preview
// costs a single down-press and the viewport scrolls past it.
//
// Placement: a single "poster" metaRow right under the game title
// (see buildMetaRows). Enter on it opens the source file with the
// desktop opener, like the art file rows; `c` copies its path.

const (
	// posterMaxRowsHard is the absolute preview height cap in cells.
	// The effective cap also scales with the viewport height (see
	// posterRows), so short terminals get a shorter preview.
	posterMaxRowsHard = 24
	// posterMinRows floors the viewport-driven cap.
	posterMinRows = 6
	// posterMaxCols caps the width; the real width is pane-driven.
	posterMaxCols = 40
	// posterMinCols skips the preview when the pane is too narrow.
	posterMinCols = 12
)

var (
	posterCacheMu sync.Mutex
	posterCache   = map[string][]string{}
)

// posterBestPath picks the source image for the preview. The metadata
// column is narrow and tall, so the portrait capsule (icon slot,
// 600x900) fits best; wide/hero backdrops follow, logo overlay last.
func posterBestPath(m Model, g rom.Game) string {
	if p := steam.CachedIconPath(m.steam, g.ID); p != "" {
		return p
	}
	if p := steam.CachedWidePath(m.steam, g.ID); p != "" {
		return p
	}
	if p := steam.CachedHeroPath(m.steam, g.ID); p != "" {
		return p
	}
	if p := steam.CachedLogoPath(m.steam, g.ID); p != "" {
		return p
	}
	return ""
}

// posterRows builds the preview row for g, or nil when there is no
// cached art, decoding fails, or previews are disabled via
// CARTRIDGE_NO_POSTER. w is the full metadata viewport width; the
// image itself uses w-2 cells (room for the 2-cell cursor gutter the
// renderer adds, like every other row). maxRows follows the viewport
// height so the preview scales with the terminal, not just the width.
func (m Model) posterRows(g rom.Game, w int) []metaRow {
	if strings.TrimSpace(os.Getenv("CARTRIDGE_NO_POSTER")) != "" {
		return nil
	}
	path := posterBestPath(m, g)
	if path == "" {
		return nil
	}
	cols := w - 2
	if cols < posterMinCols {
		return nil
	}
	if cols > posterMaxCols {
		cols = posterMaxCols
	}
	maxRows := clamp(m.meta.Height, posterMinRows, posterMaxRowsHard)
	lines := cachedPoster(path, cols, maxRows)
	if len(lines) == 0 {
		return nil
	}
	return []metaRow{{kind: "poster", lines: lines, copy: path}}
}

// cachedPoster memoizes renderPoster by path+size+mtime+geometry: the
// metadata rebuilds on every cursor move and resize, and re-decoding
// a PNG each time would stutter on large libraries.
func cachedPoster(path string, cols, maxRows int) []string {
	st, err := os.Stat(path)
	var key string
	if err != nil {
		key = fmt.Sprintf("%s|bad|%dx%d", path, cols, maxRows)
	} else {
		key = fmt.Sprintf("%s|%d|%d|%dx%d", path, st.Size(), st.ModTime().UnixNano(), cols, maxRows)
	}
	posterCacheMu.Lock()
	if lines, ok := posterCache[key]; ok {
		out := append([]string(nil), lines...)
		posterCacheMu.Unlock()
		return out
	}
	posterCacheMu.Unlock()

	lines := renderPoster(path, cols, maxRows)

	posterCacheMu.Lock()
	if len(posterCache) > 128 {
		posterCache = map[string][]string{}
	}
	posterCache[key] = append([]string(nil), lines...)
	posterCacheMu.Unlock()
	return lines
}

// renderPoster decodes path and renders it cols cells wide with the
// source aspect kept; taller pictures are center-cropped to maxRows.
// Each returned string is exactly cols cells wide (lipgloss.Width);
// colors are truecolor ANSI. With NO_COLOR set it falls back to plain
// grayscale ASCII of the same geometry (2 lines per cell row).
func renderPoster(path string, cols, maxRows int) []string {
	if cols <= 0 || maxRows <= 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return renderPosterASCII(img, b, cols, maxRows)
	}
	return renderPosterHalfBlock(img, b, cols, maxRows)
}

// posterGeom scales src to exactly cols cells wide (1 cell = 1 scaled
// pixel horizontally) and returns the row count after center-cropping
// to maxRows, plus the vertical crop offset in scaled pixels.
func posterGeom(b image.Rectangle, cols, maxRows int) (scale, yOff float64, rows int) {
	scale = float64(cols) / float64(b.Dx())
	scaledH := float64(b.Dy()) * scale
	wanted := int(scaledH+1) / 2 // ceil(scaledH/2), ≥1
	if wanted < 1 {
		wanted = 1
	}
	rows = wanted
	if rows > maxRows {
		rows = maxRows
	}
	yOff = (scaledH - float64(rows*2)) / 2
	if yOff < 0 {
		yOff = 0
	}
	return scale, yOff, rows
}

// posterSample maps an output pixel (cx, py) onto src with center
// sampling, clamping to the image bounds.
func posterSample(img image.Image, b image.Rectangle, scale, yOff float64, cx, py int) (uint8, uint8, uint8) {
	ix := b.Min.X + int((float64(cx)+0.5)/scale)
	iy := b.Min.Y + int((yOff+float64(py)+0.5)/scale)
	if ix < b.Min.X {
		ix = b.Min.X
	}
	if iy < b.Min.Y {
		iy = b.Min.Y
	}
	if ix >= b.Max.X {
		ix = b.Max.X - 1
	}
	if iy >= b.Max.Y {
		iy = b.Max.Y - 1
	}
	return pxRGB(img.At(ix, iy))
}

// renderPosterHalfBlock renders cols x rows half-block cells (2 image
// pixels per cell, no distortion, no side bars).
func renderPosterHalfBlock(img image.Image, b image.Rectangle, cols, maxRows int) []string {
	scale, yOff, rows := posterGeom(b, cols, maxRows)
	var out []string
	for cy := 0; cy < rows; cy++ {
		var sb strings.Builder
		for cx := 0; cx < cols; cx++ {
			r1, g1, b1 := posterSample(img, b, scale, yOff, cx, cy*2)
			r2, g2, b2 := posterSample(img, b, scale, yOff, cx, cy*2+1)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀\x1b[0m", r1, g1, b1, r2, g2, b2)
		}
		out = append(out, sb.String())
	}
	return out
}

var asciiRamp = []rune(" .:-=+*#%@")

// renderPosterASCII is the NO_COLOR fallback: plain grayscale chars at
// the same geometry (2 lines per half-block row).
func renderPosterASCII(img image.Image, b image.Rectangle, cols, maxRows int) []string {
	scale, yOff, rows := posterGeom(b, cols, maxRows)
	var out []string
	for cy := 0; cy < rows; cy++ {
		for py := 0; py < 2; py++ {
			var sb strings.Builder
			for cx := 0; cx < cols; cx++ {
				r, g, bl := posterSample(img, b, scale, yOff, cx, cy*2+py)
				lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)
				idx := int(lum / 255 * float64(len(asciiRamp)-1))
				sb.WriteRune(asciiRamp[idx])
			}
			out = append(out, sb.String())
		}
	}
	return out
}

func pxRGB(c color.Color) (uint8, uint8, uint8) {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return n.R, n.G, n.B
}
