package steam

import "strings"

// unquoteSteamPath removes the double quotes Steam stores around Windows
// shortcut paths, so callers can work with the raw path.
func unquoteSteamPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		return p[1 : len(p)-1]
	}
	return p
}

// quoteSteamPath wraps a path in quotes when quote is true (Windows).
// Already-quoted input is normalized, never double-quoted.
func quoteSteamPath(p string, quote bool) string {
	p = unquoteSteamPath(p)
	if !quote || p == "" {
		return p
	}
	return `"` + p + `"`
}
