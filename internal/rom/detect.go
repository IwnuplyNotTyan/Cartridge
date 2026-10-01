package rom

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Console describes a retro console known to the detector.
type Console struct {
	ID         string
	Name       string
	Short      string
	Extensions []string
}

// Known consoles, ordered for display.
var Consoles = []Console{
	{ID: "nes", Name: "Nintendo Entertainment System", Short: "NES", Extensions: []string{".nes"}},
	{ID: "snes", Name: "Super Nintendo", Short: "SNES", Extensions: []string{".sfc", ".smc"}},
	{ID: "gb", Name: "Game Boy", Short: "GB", Extensions: []string{".gb"}},
	{ID: "gbc", Name: "Game Boy Color", Short: "GBC", Extensions: []string{".gbc"}},
	{ID: "gba", Name: "Game Boy Advance", Short: "GBA", Extensions: []string{".gba"}},
	{ID: "genesis", Name: "Sega Genesis", Short: "MD", Extensions: []string{".gen", ".md", ".smd"}},
	{ID: "gamegear", Name: "Sega Game Gear", Short: "GG", Extensions: []string{".gg"}},
	{ID: "n64", Name: "Nintendo 64", Short: "N64", Extensions: []string{".n64", ".z64"}},
}

// Unknown is used when nothing matches.
var Unknown = Console{ID: "unknown", Name: "Unknown", Short: "???"}

var extIndex map[string]Console

func init() {
	extIndex = make(map[string]Console)
	for _, c := range Consoles {
		for _, e := range c.Extensions {
			extIndex[e] = c
		}
	}
}

// ByID returns the console with the given ID, or Unknown.
func ByID(id string) Console {
	for _, c := range Consoles {
		if c.ID == id {
			return c
		}
	}
	return Unknown
}

// ByExtension maps a file extension (with or without dot) to a console.
func ByExtension(ext string) (Console, bool) {
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	c, ok := extIndex[ext]
	return c, ok
}

// SupportedExtension reports whether the path looks like a scannable file
// (a known ROM extension or a .zip container that may hold one).
func SupportedExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".zip" {
		return true
	}
	_, ok := extIndex[ext]
	return ok
}

// Game is a single detected ROM with a content-based stable ID.
type Game struct {
	ID          string   `json:"id"` // hex sha256 of canonical ROM bytes
	Title       string   `json:"title"`
	ConsoleID   string   `json:"console_id"`
	ConsoleName string   `json:"console_name"`
	Path        string   `json:"path"`            // container on disk (.nes, .zip, ...)
	Inner       string   `json:"inner,omitempty"` // entry inside zip, if any
	Size        int64    `json:"size"`            // canonical ROM bytes
	SHA256      string   `json:"sha256"`          // == ID, kept explicit
	Region      string   `json:"region,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// ShortID returns a 12-char human prefix of the ID.
func (g Game) ShortID() string {
	if len(g.ID) > 12 {
		return g.ID[:12]
	}
	return g.ID
}

// max sniff / hash-handle sizes.
const (
	sniffLen       = 0x400
	maxZipEntryMem = 64 << 20 // 64 MiB safety cap per zip entry
)

// nintendoLogo is the boot logo every GB/GBC ROM must carry at 0x104.
var nintendoLogo = []byte{
	0xCE, 0xED, 0x66, 0x66, 0xCC, 0x0D, 0x00, 0x0B,
	0x03, 0x73, 0x00, 0x83, 0x00, 0x0C, 0x00, 0x0D,
	0x00, 0x08, 0x11, 0x1F, 0x88, 0x89, 0x00, 0x0E,
	0xDC, 0xCC, 0x6E, 0xE6, 0xDD, 0xDD, 0xD9, 0x99,
	0xBB, 0xBB, 0x67, 0x63, 0x6E, 0x0E, 0xEC, 0xCC,
	0xDD, 0xDC, 0x99, 0x9F, 0xBB, 0xB9, 0x33, 0x3E,
}

func isNESMagic(head []byte) bool {
	return len(head) >= 4 && head[0] == 'N' && head[1] == 'E' && head[2] == 'S' && head[3] == 0x1A
}

func isGBBoot(head []byte) bool {
	if len(head) < 0x144 {
		return false
	}
	return bytes.Equal(head[0x104:0x104+len(nintendoLogo)], nintendoLogo)
}

func isGBAHeader(head []byte) bool {
	return len(head) >= 0xC0 && head[0xB2] == 0x96
}

// sniff decides the console from raw ROM bytes, preferring magic over ext.
func sniff(head []byte, extHint string) (Console, bool) {
	if isNESMagic(head) {
		return ByID("nes"), true
	}
	if isGBBoot(head) {
		// CGB flag at 0x143: 0x80/0xC0 => Color-capable.
		if head[0x143] == 0x80 || head[0x143] == 0xC0 {
			return ByID("gbc"), true
		}
		// A plain .gbc file keeps its identity even with a DMG header.
		if strings.EqualFold(extHint, ".gbc") {
			return ByID("gbc"), true
		}
		return ByID("gb"), true
	}
	if isGBAHeader(head) {
		return ByID("gba"), true
	}
	if c, ok := ByExtension(extHint); ok {
		return c, true
	}
	return Unknown, false
}

// Identify opens path, resolves zip containers to the inner ROM,
// detects the console and returns a Game with a content-stable ID.
//
// The ID is sha256 over the *canonical ROM bytes* (the inner file for
// zips, the file itself otherwise), so renames, moves and re-zips of the
// same dump keep the same ID. Nothing situational (path, mtime, order)
// feeds the hash.
func Identify(path string) (Game, error) {
	rawName := filepath.Base(path)
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return identifyZip(path, rawName)
	}
	return identifyPlain(path, rawName)
}

func identifyPlain(path, rawName string) (Game, error) {
	f, err := os.Open(path)
	if err != nil {
		return Game{}, err
	}
	defer f.Close()

	head := make([]byte, sniffLen)
	n, _ := io.ReadFull(f, head)
	head = head[:n]

	h := sha256.New()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Game{}, err
	}
	size, err := io.Copy(h, f)
	if err != nil {
		return Game{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))

	con, _ := sniff(head, filepath.Ext(path))
	title, region, tags := ParseTitle(strings.TrimSuffix(rawName, filepath.Ext(rawName)))

	return Game{
		ID:          sum,
		Title:       title,
		ConsoleID:   con.ID,
		ConsoleName: con.Name,
		Path:        path,
		Size:        size,
		SHA256:      sum,
		Region:      region,
		Tags:        tags,
	}, nil
}

func identifyZip(path, rawName string) (Game, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Game{}, err
	}
	defer zr.Close()

	pick := pickZipEntry(zr.File)
	if pick == nil {
		return Game{}, fmt.Errorf("zip has no ROM entries: %s", path)
	}
	rc, err := pick.Open()
	if err != nil {
		return Game{}, err
	}
	defer rc.Close()

	var buf bytes.Buffer
	size, err := io.Copy(&buf, io.LimitReader(rc, maxZipEntryMem+1))
	if err != nil {
		return Game{}, err
	}
	if size > maxZipEntryMem {
		return Game{}, fmt.Errorf("zip entry too large: %s!%s", path, pick.Name)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	sumHex := hex.EncodeToString(sum[:])

	head := data
	if len(head) > sniffLen {
		head = head[:sniffLen]
	}
	con, _ := sniff(head, filepath.Ext(pick.Name))
	title, region, tags := ParseTitle(strings.TrimSuffix(filepath.Base(pick.Name), filepath.Ext(pick.Name)))
	_ = rawName

	return Game{
		ID:          sumHex,
		Title:       title,
		ConsoleID:   con.ID,
		ConsoleName: con.Name,
		Path:        path,
		Inner:       pick.Name,
		Size:        int64(len(data)),
		SHA256:      sumHex,
		Region:      region,
		Tags:        tags,
	}, nil
}

// pickZipEntry chooses the most plausible ROM inside a zip:
// first known-extension entry (biggest wins), else the biggest file.
func pickZipEntry(files []*zip.File) *zip.File {
	var best *zip.File
	for _, f := range files {
		if f.FileInfo().IsDir() || f.UncompressedSize64 == 0 {
			continue
		}
		if _, ok := ByExtension(filepath.Ext(f.Name)); !ok {
			continue
		}
		if best == nil || f.UncompressedSize64 > best.UncompressedSize64 {
			best = f
		}
	}
	if best != nil {
		return best
	}
	for _, f := range files {
		if f.FileInfo().IsDir() || f.UncompressedSize64 == 0 {
			continue
		}
		if best == nil || f.UncompressedSize64 > best.UncompressedSize64 {
			best = f
		}
	}
	return best
}

// ParseTitle splits "Name (Region) [tags]" into a clean title,
// a region and the remaining tags.
func ParseTitle(base string) (title, region string, tags []string) {
	name := strings.ReplaceAll(base, "_", " ")
	name = strings.TrimSpace(name)

	var regions []string
	rest := name
	for {
		open := strings.LastIndexAny(rest, "([")
		if open < 0 {
			break
		}
		closing := strings.IndexAny(rest[open:], ")]")
		if closing < 0 {
			break
		}
		token := strings.TrimSpace(rest[open+1 : open+closing])
		rest = strings.TrimSpace(rest[:open])
		switch {
		case isRegionToken(token):
			regions = append(regions, token)
		case token != "":
			tags = append([]string{token}, tags...)
		}
		if rest == "" {
			break
		}
	}
	// Regions in original order.
	for i, j := 0, len(regions)-1; i < j; i, j = i+1, j-1 {
		regions[i], regions[j] = regions[j], regions[i]
	}
	return rest, strings.Join(regions, ", "), tags
}

func isRegionToken(t string) bool {
	upper := strings.ToUpper(t)
	for _, r := range []string{"USA", "EUROPE", "JAPAN", "WORLD", "UK", "USA, EUROPE", "EUROPE, USA"} {
		if upper == r || strings.Contains(upper, "USA") && strings.Contains(upper, "EUROPE") {
			return true
		}
	}
	for _, r := range []string{"USA", "EUROPE", "JAPAN", "WORLD", "PAL", "NTSC"} {
		if strings.Contains(upper, r) {
			return true
		}
	}
	return false
}

// ScanDir walks root and identifies every supported file.
// Unknown-console files are kept (console "unknown") so nothing
// silently disappears; callers may filter them out.
func ScanDir(root string) ([]Game, []error) {
	var games []Game
	var errs []error
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !SupportedExtension(path) {
			return nil
		}
		g, err := Identify(path)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		games = append(games, g)
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	sort.Slice(games, func(i, j int) bool {
		if games[i].ConsoleName != games[j].ConsoleName {
			return games[i].ConsoleName < games[j].ConsoleName
		}
		return games[i].Title < games[j].Title
	})
	return games, errs
}
