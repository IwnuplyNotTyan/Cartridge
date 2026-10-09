// Package steam manages Steam non-Steam shortcuts (shortcuts.vdf):
// single/bulk add with per-console categories and optional auto-add.
//
// Shortcuts live per Steam user in
// <steam>/userdata/<id>/config/shortcuts.vdf (binary VDF). Each cartridge
// game becomes one shortcut launching `cartridge play <id>`, tagged with
// a per-console category like "Cartridge - GBA" so Steam collections
// group the shelf by console automatically.
//
// Writing while Steam is running is lossy (Steam rewrites the file on
// exit), so callers should warn and write anyway; Steam picks the file up
// on next start.
package steam

import (
	"strings"

	"cartridge/internal/config"
	"cartridge/internal/rom"
)

// AppNameFor returns the Steam shortcut name for a game.
// Titles collide across consoles, so the console short tag disambiguates:
// "Super Mario (GBA)". Unknown consoles keep the plain title.
func AppNameFor(g rom.Game) string {
	short := ConsoleShort(g)
	if short == "" || short == "???" {
		return g.Title
	}
	// Already suffixed? Keep idempotent for re-syncs.
	if strings.HasSuffix(g.Title, " ("+short+")") {
		return g.Title
	}
	return g.Title + " (" + short + ")"
}

// ConsoleShort returns the short console tag (NES, GBA, …) or "".
func ConsoleShort(g rom.Game) string {
	for _, c := range rom.Consoles {
		if c.ID == g.ConsoleID {
			return c.Short
		}
	}
	if strings.TrimSpace(g.ConsoleName) != "" {
		return g.ConsoleName
	}
	return ""
}

// ConsoleName returns the full console name for templates.
func ConsoleName(g rom.Game) string {
	if g.ConsoleName != "" {
		return g.ConsoleName
	}
	return string(rom.ByID(g.ConsoleID).Name)
}

// CustomName returns the per-console custom category name for a game.
// Keys match the console id or short tag case-insensitively
// ("gba" and "GBA" both hit). Empty string means "no override".
func CustomName(cfg config.SteamConfig, g rom.Game) string {
	if len(cfg.Categories) == 0 {
		return ""
	}
	wantID := strings.ToLower(strings.TrimSpace(g.ConsoleID))
	wantShort := strings.ToLower(strings.TrimSpace(ConsoleShort(g)))
	for k, v := range cfg.Categories {
		kk := strings.ToLower(strings.TrimSpace(k))
		if kk == "" {
			continue
		}
		if kk == wantID || (wantShort != "" && kk == wantShort) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// renderTemplate expands {prefix} {console} {console_id} {short} {title}
// {name}, where {name} is the custom per-console name or the short tag.
func renderTemplate(tmpl string, g rom.Game, cfg config.SteamConfig) string {
	prefix := strings.TrimSpace(cfg.EffectivePrefix())
	out := tmpl
	out = strings.ReplaceAll(out, "{prefix}", prefix)
	out = strings.ReplaceAll(out, "{console}", ConsoleName(g))
	out = strings.ReplaceAll(out, "{console_id}", g.ConsoleID)
	out = strings.ReplaceAll(out, "{short}", ConsoleShort(g))
	out = strings.ReplaceAll(out, "{title}", g.Title)
	if name := CustomName(cfg, g); name != "" {
		out = strings.ReplaceAll(out, "{name}", name)
	} else {
		out = strings.ReplaceAll(out, "{name}", ConsoleShort(g))
	}
	// Collapse leftover separators when prefix/short are empty:
	// " - GBA" -> "GBA", "Cartridge - " -> "Cartridge".
	out = strings.TrimSpace(out)
	out = strings.Trim(out, "-")
	out = strings.TrimSpace(out)
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	return out
}

// TagsFor builds Steam categories for a game from SteamConfig.
// Default: just ["Cartridge - GBA"] — one category per console.
// The shared root tag (Prefix) is added only when AllGames is on.
// Results are deduped, order-stable, never empty.
func TagsFor(g rom.Game, cfg config.SteamConfig) []string {
	prefix := strings.TrimSpace(cfg.EffectivePrefix())
	tmpl := cfg.EffectiveTemplate()
	perConsole := renderTemplate(tmpl, g, cfg)
	var tags []string
	seen := map[string]bool{}
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		tags = append(tags, t)
	}
	if cfg.AllGames && prefix != "" {
		add(prefix)
	}
	add(perConsole)
	if len(tags) == 0 {
		// Absolute fallback, should be unreachable.
		add("Cartridge")
	}
	return tags
}

// LaunchOptionsFor returns the cartridge CLI args for a Steam shortcut.
func LaunchOptionsFor(g rom.Game) string {
	// Full content ID: stable across renames/moves, unambiguous for `play`.
	return "play " + g.ID
}

// CanonicalConsoleID resolves a user-supplied console key (id or short
// tag, any case) to the canonical console id. Unknown consoles report false.
func CanonicalConsoleID(key string) (string, bool) {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return "", false
	}
	for _, c := range rom.Consoles {
		if strings.ToLower(c.ID) == k || strings.ToLower(c.Short) == k {
			return c.ID, true
		}
	}
	if strings.EqualFold("unknown", k) {
		return "unknown", true
	}
	return "", false
}
