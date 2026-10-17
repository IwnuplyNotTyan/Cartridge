package rom

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HeaderTitle attempts to extract the internal title from ROM header bytes.
func HeaderTitle(head []byte, consoleID string) string {
	switch consoleID {
	case "gba":
		return gbaHeaderTitle(head)
	case "gb", "gbc":
		return gbHeaderTitle(head)
	case "genesis", "sega32x":
		return genesisHeaderTitle(head)
	case "snes":
		return snesHeaderTitle(head)
	case "n64":
		return n64HeaderTitle(head)
	case "ds":
		return ndsHeaderTitle(head)
	}
	return ""
}

func gbaHeaderTitle(head []byte) string {
	// GBA title is at 0xA0..0xAC (12 bytes).
	if len(head) < 0xAC {
		return ""
	}
	return cleanHeaderString(head[0xA0:0xAC])
}

func gbHeaderTitle(head []byte) string {
	// GB/GBC title is at 0x134..0x144. On CGB, 0x143 is CGB flag.
	if len(head) < 0x144 {
		return ""
	}
	end := 0x144
	if head[0x143] == 0x80 || head[0x143] == 0xC0 {
		end = 0x143
	}
	return cleanHeaderString(head[0x134:end])
}

func genesisHeaderTitle(head []byte) string {
	// Sega Genesis title at 0x150..0x180 (Overseas) or 0x120..0x150 (Domestic).
	if len(head) < 0x180 {
		return ""
	}
	intl := cleanHeaderString(head[0x150:0x180])
	if intl != "" {
		return intl
	}
	return cleanHeaderString(head[0x120:0x150])
}

func snesHeaderTitle(head []byte) string {
	// SNES header is at 0x7FC0 (LoROM) or 0xFFC0 (HiROM).
	// If ROM has copier header (len % 1024 == 512), offsets shift by 512.
	checkHeader := func(offset int) (string, bool) {
		if len(head) < offset+0x20 {
			return "", false
		}
		comp := uint16(head[offset+0x1C]) | uint16(head[offset+0x1D])<<8
		csum := uint16(head[offset+0x1E]) | uint16(head[offset+0x1F])<<8
		if comp+csum == 0xFFFF && comp != 0 && csum != 0 {
			return cleanHeaderString(head[offset : offset+21]), true
		}
		return "", false
	}

	for _, base := range []int{0, 512} {
		for _, off := range []int{0x7FC0, 0xFFC0} {
			if title, ok := checkHeader(base + off); ok && title != "" {
				return title
			}
		}
	}
	return ""
}

func n64HeaderTitle(head []byte) string {
	// N64 title is 20 bytes at 0x20..0x34.
	if len(head) < 0x34 {
		return ""
	}
	buf := make([]byte, 20)
	copy(buf, head[0x20:0x34])
	if len(head) >= 4 {
		switch {
		case head[0] == 0x37 && head[1] == 0x80: // Byte-swapped (.v64)
			for i := 0; i+1 < len(buf); i += 2 {
				buf[i], buf[i+1] = buf[i+1], buf[i]
			}
		case head[0] == 0x40 && head[1] == 0x12: // Little-endian (.n64)
			for i := 0; i+3 < len(buf); i += 4 {
				buf[i], buf[i+1], buf[i+2], buf[i+3] = buf[i+3], buf[i+2], buf[i+1], buf[i]
			}
		}
	}
	return cleanHeaderString(buf)
}

func ndsHeaderTitle(head []byte) string {
	// NDS title is 12 bytes at 0x000..0x00C.
	if len(head) < 0x00C {
		return ""
	}
	return cleanHeaderString(head[0x000:0x00C])
}

// cleanHeaderString strips trailing spaces/nulls and ensures printable ASCII.
func cleanHeaderString(b []byte) string {
	var buf strings.Builder
	for _, c := range b {
		if c == 0 {
			break
		}
		if c >= 32 && c <= 126 {
			buf.WriteByte(c)
		} else {
			return ""
		}
	}
	s := strings.TrimSpace(buf.String())
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// isSlugOrGeneric reports whether a name looks like an unformatted filename slug
// (all lowercase, or containing hyphens/underscores without spaces).
func isSlugOrGeneric(stem string) bool {
	stem = strings.TrimSpace(stem)
	if stem == "" {
		return true
	}
	if strings.Contains(stem, " ") {
		return false
	}
	if strings.ContainsAny(stem, "-_") {
		return true
	}
	if strings.ToLower(stem) == stem {
		return true
	}
	return false
}

var (
	cachedNoIntroDB     string
	cachedNoIntroDBOnce bool
)

func findNoIntroDB() string {
	if cachedNoIntroDBOnce {
		return cachedNoIntroDB
	}
	cachedNoIntroDBOnce = true

	home, _ := os.UserHomeDir()
	var candidates []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "mgba", "nointro.sqlite3"))
	}
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".config", "mgba", "nointro.sqlite3"),
			filepath.Join(home, "Library", "Application Support", "mGBA", "nointro.sqlite3"),
		)
	}
	candidates = append(candidates, "/Applications/mGBA.app/Contents/Resources/nointro.sqlite3")

	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			cachedNoIntroDB = p
			return p
		}
	}
	return ""
}

// LookupNoIntro queries local mGBA No-Intro database if present.
func LookupNoIntro(crc uint32) string {
	if crc == 0 {
		return ""
	}
	db := findNoIntroDB()
	if db == "" {
		return ""
	}
	sqliteBin, err := exec.LookPath("sqlite3")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	query := fmt.Sprintf("SELECT games.name FROM roms JOIN games ON roms.gid = games.gid WHERE crc32 = %d LIMIT 1;", crc)
	out, err := exec.CommandContext(ctx, sqliteBin, db, query).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ResolveTitle determines the best title for a ROM using No-Intro lookup,
// ROM header title, and the filename stem.
func ResolveTitle(stem, rawName string, head []byte, consoleID string, crc uint32) (title, region string, tags []string, headerTitle string) {
	headerTitle = HeaderTitle(head, consoleID)

	// 1. Try No-Intro lookup
	if crc != 0 {
		if noIntroName := LookupNoIntro(crc); noIntroName != "" {
			t, r, tg := ParseTitle(noIntroName)
			if t != "" {
				return t, r, tg, headerTitle
			}
		}
	}

	// 2. Parse filename stem
	fTitle, fRegion, fTags := ParseTitle(stem)

	// 3. Fall back to header title if filename is a slug or empty
	if headerTitle != "" && (isSlugOrGeneric(stem) || fTitle == "") {
		return headerTitle, fRegion, fTags, headerTitle
	}

	if fTitle == "" {
		if stem != "" {
			fTitle = stem
		} else {
			fTitle = rawName
		}
	}
	return fTitle, fRegion, fTags, headerTitle
}
