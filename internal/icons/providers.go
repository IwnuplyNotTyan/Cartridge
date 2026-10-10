package icons

import (
	"net/url"
	"strings"
)

// candidatesFor returns download URLs for g under provider, best first.
// Heroes are wide backdrops: SteamGridDB serves real 16:9 heroes, the free
// CDN falls back to in-game snapshots (portrait boxart would look broken
// stretched across the Steam library page).
func candidatesFor(provider string, g Game, gridKey string, kind ArtKind) []string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ProviderSteamGrid:
		return steamGridCandidates(g, gridKey)
	default:
		return libretroCandidates(g, kind)
	}
}

// libretroRepo maps cartridge console ids to libretro-thumbnails repos.
var libretroRepo = map[string]string{
	"nes":          "Nintendo_-_Nintendo_Entertainment_System",
	"snes":         "Nintendo_-_Super_Nintendo_Entertainment_System",
	"gb":           "Nintendo_-_Game_Boy",
	"gbc":          "Nintendo_-_Game_Boy_Color",
	"gba":          "Nintendo_-_Game_Boy_Advance",
	"genesis":      "Sega_-_Mega-Drive_-_Genesis",
	"gamegear":     "Sega_-_Game_Gear",
	"mastersystem": "Sega_-_Master_System_-_Mark_III",
	"sega32x":      "Sega_-_32X",
	"sg-1000":      "Sega_-_SG-1000",
	"n64":          "Nintendo_-_Nintendo_64",
	"fds":          "Nintendo_-_Family_Computer_Disk_System",
	"atari2600":    "Atari_-_2600",
	"atari7800":    "Atari_-_7800",
	"atarilynx":    "Atari_-_Lynx",
	"ngp":          "SNK_-_Neo_Geo_Pocket",
	"ngpc":         "SNK_-_Neo_Geo_Pocket_Color",
	"pcengine":     "NEC_-_PC_Engine_-_TurboGrafx_16",
	"ds":           "Nintendo_-_Nintendo_DS",
	"3ds":          "Nintendo_-_Nintendo_3DS",
	"psx":          "Sony_-_PlayStation",
	"psp":          "Sony_-_PlayStation_Portable",
	"gamecube":     "Nintendo_-_GameCube",
	"wii":          "Nintendo_-_Wii",
	"dreamcast":    "Sega_-_Dreamcast",
	"wonderswan":   "Bandai_-_WonderSwan",
	"virtualboy":   "Nintendo_-_Virtual_Boy",
	"switch":       "Nintendo_-_Nintendo_Switch",
}

// libretroCategories for icons: boxart looks best as a Steam icon,
// then title screen, then in-game snapshot.
var libretroCategories = []string{"Named_Boxarts", "Named_Titles", "Named_Snaps"}

// libretroHeroCategories for heroes: gameplay snapshots double as wide
// backdrops; boxart stays icon-only.
var libretroHeroCategories = []string{"Named_Snaps"}

// libretroCandidates builds CDN URLs for a game.
// Thumbnail repos use No-Intro filenames ("ActRaiser (USA).png"), so the
// ROM filename stem goes first, then Title re-suffixed with Region/Tags,
// then the bare title last. Each name is tried per category in order.
func libretroCandidates(g Game, kind ArtKind) []string {
	repo, ok := libretroRepo[strings.ToLower(strings.TrimSpace(g.ConsoleID))]
	if !ok {
		return nil
	}
	titles := titleVariants(g)
	if len(titles) == 0 {
		return nil
	}
	cats := libretroCategories
	if kind == ArtHero {
		cats = libretroHeroCategories
	}
	var out []string
	for _, t := range titles {
		for _, cat := range cats {
			out = append(out,
				"https://raw.githubusercontent.com/libretro-thumbnails/"+repo+"/master/"+cat+"/"+url.PathEscape(t)+".png")
		}
	}
	return out
}

// titleVariants returns candidate thumbnail names, best first:
// exact ROM stem (and its space/sanitized forms), Title with Region/Tags
// suffixes re-attached, bare title.
func titleVariants(g Game) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		out = append(out, s)
		if san := sanitizeLibretro(s); san != "" && san != s {
			out = append(out, san)
		}
	}
	// 1. Exact ROM stem: "ActRaiser (USA)" hits verbatim.
	// Underscores (common in filenames) become spaces as seen in repos.
	if stem := strings.TrimSpace(g.Stem); stem != "" {
		add(stem)
		if sp := strings.ReplaceAll(stem, "_", " "); sp != stem {
			add(sp)
		}
	}
	// 2. Title + region/tags re-attached: ParseTitle strips "(USA)" and
	// "[!]" into Region/Tags, but the repo keeps them in the filename.
	if t := strings.TrimSpace(g.Title); t != "" {
		if suffixed := withSuffixes(t, g.Region, g.Tags); suffixed != t {
			add(suffixed)
		}
		add(t)
		// 3. De-suffixed leftovers (a stem that itself ends with a group
		// the repo omits) stay a last resort.
		if base := stripTrailingGroup(t); base != "" && base != t {
			add(base)
		}
	}
	seen := map[string]bool{}
	uniq := out[:0]
	for _, s := range out {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		uniq = append(uniq, s)
	}
	return uniq
}

// withSuffixes rebuilds "Title (Region) [tag]" from parsed parts.
func withSuffixes(title, region string, tags []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(title))
	if r := strings.TrimSpace(region); r != "" {
		b.WriteString(" (" + r + ")")
	}
	for _, tg := range tags {
		if tg = strings.TrimSpace(tg); tg != "" {
			b.WriteString(" [" + tg + "]")
		}
	}
	return b.String()
}

// sanitizeLibretro mirrors the thumbnail repo filename rules.
func sanitizeLibretro(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&', '*', '/', '`', '"', '<', '>', ':', '?', '|', '\\':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	return out
}

// stripTrailingGroup drops one trailing " (…)" or " […]" group.
func stripTrailingGroup(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return ""
	}
	last := s[len(s)-1]
	var opener byte
	switch last {
	case ')':
		opener = '('
	case ']':
		opener = '['
	default:
		return s
	}
	i := strings.LastIndexByte(s[:len(s)-1], opener)
	if i < 0 {
		return s
	}
	token := strings.TrimSpace(s[i+1 : len(s)-1])
	if token == "" || strings.ContainsAny(token, "()[]") {
		return s
	}
	return strings.TrimSpace(s[:i])
}

// steamGridCandidates resolves direct image URLs via the SteamGridDB API.
// Without an API key there is nothing to do (no anonymous bulk access),
// so it returns nil and the chain falls back to Libretro.
func steamGridCandidates(g Game, apiKey string) []string {
	// NOTE: resolved lazily at fetch time would need an extra search
	// request per game; instead the steamgriddb provider path is handled
	// by fetchSteamGrid below (search + pick best grid). Returning nil
	// here keeps candidatesFor total, the actual logic lives in fetchOne
	// via provider hook. See steamgriddb.go.
	_ = apiKey
	_ = g
	return nil
}
