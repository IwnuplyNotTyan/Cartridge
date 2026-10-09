package steam

import (
	"hash/crc32"
	"strings"
)

// AppIDFor generates the stable 32-bit non-Steam shortcut id for
// (exe, appname): crc32(exe+appname) with the top bit forced, the same
// recipe SteamGridDB / steam-rom-manager use for grid art ("legacy id").
//
// Valve's client "sanitizes" tiny ids (0,1,2…) into random ones, but ids
// with the high bit set survive a Steam restart, so generated shortcuts
// keep their id (and artwork/compat settings) across re-syncs.
func AppIDFor(exe, appName string) uint32 {
	key := exe + appName
	return crc32.ChecksumIEEE([]byte(key)) | 0x80000000
}

// SignedAppID converts the uint32 id to the signed int32 stored in
// shortcuts.vdf (same bits, little-endian).
func SignedAppID(id uint32) int32 { return int32(id) }

// ShortcutID64 returns the 64-bit id used for grid art / launch URLs:
// (appid << 32) | 0x02000000.
func ShortcutID64(appID uint32) uint64 {
	return (uint64(appID) << 32) | 0x02000000
}

// exeKey normalizes exe for stable matching across syncs.
func exeKey(exe string) string { return strings.TrimSpace(exe) }
