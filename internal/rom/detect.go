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

	"cartridge/internal/mods"
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
	{ID: "snes", Name: "Super Nintendo", Short: "SNES", Extensions: []string{".sfc", ".smc", ".fig"}},
	{ID: "gb", Name: "Game Boy", Short: "GB", Extensions: []string{".gb"}},
	{ID: "gbc", Name: "Game Boy Color", Short: "GBC", Extensions: []string{".gbc"}},
	{ID: "gba", Name: "Game Boy Advance", Short: "GBA", Extensions: []string{".gba"}},
	{ID: "genesis", Name: "Sega Genesis", Short: "MD", Extensions: []string{".gen", ".md", ".smd"}},
	{ID: "gamegear", Name: "Sega Game Gear", Short: "GG", Extensions: []string{".gg"}},
	{ID: "mastersystem", Name: "Sega Master System", Short: "SMS", Extensions: []string{".sms"}},
	{ID: "sega32x", Name: "Sega 32X", Short: "32X", Extensions: []string{".32x"}},
	{ID: "sg-1000", Name: "Sega SG-1000", Short: "SG1K", Extensions: []string{".sg", ".sc"}},
	{ID: "n64", Name: "Nintendo 64", Short: "N64", Extensions: []string{".n64", ".z64", ".v64"}},
	{ID: "fds", Name: "Famicom Disk System", Short: "FDS", Extensions: []string{".fds"}},
	{ID: "atari2600", Name: "Atari 2600", Short: "A26", Extensions: []string{".a26"}},
	{ID: "atari7800", Name: "Atari 7800", Short: "A78", Extensions: []string{".a78"}},
	{ID: "atarilynx", Name: "Atari Lynx", Short: "LYNX", Extensions: []string{".lnx", ".lyx"}},
	{ID: "ngp", Name: "Neo Geo Pocket", Short: "NGP", Extensions: []string{".ngp"}},
	{ID: "ngpc", Name: "Neo Geo Pocket Color", Short: "NGPC", Extensions: []string{".ngc", ".ngpc"}},
	{ID: "pcengine", Name: "PC Engine / TurboGrafx-16", Short: "PCE", Extensions: []string{".pce", ".sgx"}},
	{ID: "switch", Name: "Nintendo Switch", Short: "SW", Extensions: []string{".xci", ".nsp", ".nsz"}},
	{ID: "3ds", Name: "Nintendo 3DS", Short: "3DS", Extensions: []string{".3ds", ".3dsx", ".cci", ".cxi"}},
	{ID: "ds", Name: "Nintendo DS", Short: "DS", Extensions: []string{".nds", ".dsi"}},
	{ID: "psx", Name: "Sony PlayStation", Short: "PS1", Extensions: []string{".pbp"}},
	{ID: "psp", Name: "Sony PlayStation Portable", Short: "PSP", Extensions: []string{".cso", ".ppbp"}},
	{ID: "gamecube", Name: "Nintendo GameCube", Short: "GC", Extensions: []string{".gcm", ".gcz"}},
	{ID: "wii", Name: "Nintendo Wii", Short: "WII", Extensions: []string{".wbfs", ".rvz"}},
	{ID: "dreamcast", Name: "Sega Dreamcast", Short: "DC", Extensions: []string{".gdi", ".cdi"}},
	{ID: "wonderswan", Name: "Bandai WonderSwan", Short: "WS", Extensions: []string{".ws", ".wsc"}},
	{ID: "virtualboy", Name: "Nintendo Virtual Boy", Short: "VB", Extensions: []string{".vb"}},
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
// Mod/patch files (.ips, .bps, ...) are never ROMs, even when they sit
// next to games or inside a zip.
func SupportedExtension(path string) bool {
	if mods.IsModPath(path) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".zip" {
		return true
	}
	_, ok := extIndex[ext]
	return ok
}

// junkZipExts are never ROMs inside archives: docs, images, music,
// cheat/save files and the like. The zip fallback below (meant for ROMs
// with unlisted extensions like .bin) must skip them, otherwise a
// lone readme.txt becomes an "unknown console" game.
var junkZipExts = map[string]bool{
	".txt": true, ".nfo": true, ".diz": true, ".md": true, ".markdown": true,
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
	".tiff": true, ".webp": true, ".ico": true,
	".pdf": true, ".html": true, ".htm": true, ".xml": true, ".json": true,
	".yml": true, ".yaml": true,
	".cht": true, ".sav": true, ".srm": true,
	".mp3": true, ".ogg": true, ".wav": true, ".flac": true,
	".mp4": true, ".avi": true, ".mkv": true,
	".exe": true, ".bat": true, ".sh": true, ".dll": true, ".so": true,
}

// isJunkExt reports whether ext (with or without dot) is archive junk
// that must never become a game.
func isJunkExt(ext string) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return false
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return junkZipExts[ext]
}

// ValidGame reports whether g looks like a real ROM entry.
// Mod/patch files (.ips, ...), archive junk (.txt inside a zip, ...) and
// plain files with unsupported extensions are never valid games, so stale
// library rows from older scans get pruned instead of haunting the shelf.
func ValidGame(g Game) bool {
	if mods.IsModPath(g.Path) || isJunkExt(filepath.Ext(g.Path)) {
		return false
	}
	if g.Inner != "" {
		if mods.IsModPath(g.Inner) || isJunkExt(filepath.Ext(g.Inner)) {
			return false
		}
		return true // zip container with a non-junk inner entry
	}
	if strings.EqualFold(filepath.Ext(g.Path), ".zip") {
		return true // container; inner unknown (legacy row), keep
	}
	return SupportedExtension(g.Path)
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
	// FileSize and ModTime describe the container on disk at scan time.
	// Together with Path they let the next ScanDir reuse the stored Game
	// without re-hashing untouched files (see cacheFresh).
	FileSize int64 `json:"file_size"`
	ModTime  int64 `json:"mod_time"` // UnixNano of the container file
}

// cacheFresh reports whether the cached Game still describes the file at
// path: same container size and mtime, so the content (and the ID) cannot
// have changed through any normal write. Same-size+same-mtime rewrites
// with different bytes are pathological and intentionally not detected —
// trading that for skipping gigabytes of re-hashing on every start.
func cacheFresh(c Game, info os.FileInfo) bool {
	return c.ID != "" && c.Path != "" &&
		c.FileSize == info.Size() && c.ModTime == info.ModTime().UnixNano()
}

// ShortID returns a 12-char human prefix of the ID.
func (g Game) ShortID() string {
	if len(g.ID) > 12 {
		return g.ID[:12]
	}
	return g.ID
}

// ExistsOnDisk reports whether the game's container file is present.
// Library rows outlive deleted ROMs (Merge keeps them), so callers that
// act on games — Steam sync above all — must skip the missing ones
// instead of resurrecting ghosts.
func (g Game) ExistsOnDisk() bool {
	if strings.TrimSpace(g.Path) == "" {
		return false
	}
	st, err := os.Stat(g.Path)
	return err == nil && !st.IsDir()
}

// PartitionPresent splits games into present-on-disk and missing (ghosts:
// library rows whose container file is gone). Order-stable.
func PartitionPresent(games []Game) (present, missing []Game) {
	for _, g := range games {
		if g.ExistsOnDisk() {
			present = append(present, g)
		} else {
			missing = append(missing, g)
		}
	}
	return present, missing
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
// Mod/patch files (.ips, .bps, ...) are rejected with an error, so they
// never show up as games.
//
// The ID is sha256 over the *canonical ROM bytes* (the inner file for
// zips, the file itself otherwise), so renames, moves and re-zips of the
// same dump keep the same ID. Nothing situational (path, mtime, order)
// feeds the hash.
func Identify(path string) (Game, error) {
	if mods.IsModPath(path) {
		return Game{}, fmt.Errorf("not a ROM (mod file): %s", path)
	}
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
	if title == "" {
		title = rawName // never a blank row (e.g. a file named ".nes")
	}

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
	if title == "" {
		title = filepath.Base(pick.Name) // never a blank row
	}
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
// first known-extension entry (biggest wins), else the biggest file with
// an unlisted extension (for ROMs like .bin).
// Mod files (.ips, ...) and archive junk (readme.txt, art, ...) never win:
// a zip holding only those reports no ROM entries instead of becoming
// an "unknown console" game.
func pickZipEntry(files []*zip.File) *zip.File {
	var best *zip.File
	for _, f := range files {
		if f.FileInfo().IsDir() || f.UncompressedSize64 == 0 {
			continue
		}
		if mods.IsModPath(f.Name) {
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
		if mods.IsModPath(f.Name) || isJunkExt(filepath.Ext(f.Name)) {
			continue
		}
		if best == nil || f.UncompressedSize64 > best.UncompressedSize64 {
			best = f
		}
	}
	return best
}

// ParseTitle splits "Name (Region) [tags]" into a clean title,
// a region and the remaining tags. Only trailing groups are stripped,
// so a leading publisher prefix like "[Aladdin] Micro Machines" stays
// in the title instead of being eaten as a tag (which left it empty).
func ParseTitle(base string) (title, region string, tags []string) {
	name := strings.ReplaceAll(base, "_", " ")
	rest := strings.TrimSpace(name)

	var regions []string
	for {
		token, head, ok := cutTrailingGroup(rest)
		if !ok || head == "" {
			break // no clean suffix group, or nothing would remain
		}
		rest = head
		switch {
		case isRegionToken(token):
			regions = append(regions, token)
		case token != "":
			tags = append([]string{token}, tags...)
		}
	}
	// Regions in original order.
	for i, j := 0, len(regions)-1; i < j; i, j = i+1, j-1 {
		regions[i], regions[j] = regions[j], regions[i]
	}
	return rest, strings.Join(regions, ", "), tags
}

// displayStem returns the filename stem a title is parsed from:
// the inner entry for zips, else the file itself. Mirrors Identify.
func displayStem(path, inner string) string {
	name := filepath.Base(path)
	if strings.TrimSpace(inner) != "" {
		name = filepath.Base(inner)
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// cutTrailingGroup splits "head (token)" or "head [token]" at its last
// group. It reports false when the string does not end with a clean,
// non-nested group.
func cutTrailingGroup(s string) (token, head string, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return "", s, false
	}
	var opener byte
	switch s[len(s)-1] {
	case ')':
		opener = '('
	case ']':
		opener = '['
	default:
		return "", s, false
	}
	open := strings.LastIndexByte(s[:len(s)-1], opener)
	if open < 0 {
		return "", s, false
	}
	token = strings.TrimSpace(s[open+1 : len(s)-1])
	if token == "" || strings.ContainsAny(token, "()[]") {
		return "", s, false
	}
	return token, strings.TrimSpace(s[:open]), true
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
//
// cache (usually the stored library) lets untouched files skip
// re-hashing: an entry whose container has the same size and mtime is
// reused as-is, so rescans cost a walk plus stats instead of gigabytes
// of reads. Pass nil for a cold scan.
func ScanDir(root string, cache []Game) ([]Game, []error) {
	byPath := make(map[string]Game, len(cache))
	for _, g := range cache {
		if g.Path != "" {
			byPath[g.Path] = g
		}
	}
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
		if mods.IsModPath(path) {
			return nil
		}
		if !SupportedExtension(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		if c, ok := byPath[path]; ok && cacheFresh(c, info) {
			// Same bytes, so the ID/size/console stand — but cheap
			// filename-derived fields are re-parsed with the current
			// parser. Otherwise a parser fix would never heal rows
			// stored while the old one blanked them (e.g. titles).
			title, region, tags := ParseTitle(displayStem(path, c.Inner))
			if title == "" {
				title = filepath.Base(path)
			}
			c.Title, c.Region, c.Tags = title, region, tags
			c.ConsoleName = ByID(c.ConsoleID).Name
			games = append(games, c)
			return nil
		}
		g, err := Identify(path)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		g.FileSize = info.Size()
		g.ModTime = info.ModTime().UnixNano()
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
