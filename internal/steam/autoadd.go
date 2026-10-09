package steam

import (
	"fmt"
	"os"

	"cartridge/internal/config"
	"cartridge/internal/rom"
)

// MaybeAutoAdd syncs games to Steam when AutoAdd is on.
// No Steam installation -> silent no-op (returns nil) so plain CLI usage
// never spams. Sync errors are returned for callers to warn about.
func MaybeAutoAdd(cfg config.Config, games []rom.Game) error {
	if !cfg.Steam.AutoAdd {
		return nil
	}
	if len(games) == 0 {
		return nil
	}
	targets := DiscoverTargets(cfg.Steam.Users)
	if len(targets) == 0 {
		return nil
	}
	exe := DefaultExe(cfg.Steam)
	_, err := SyncAllTracked(games, cfg.Steam, exe, "", false, TrackFilePath(cfg.Library))
	if err != nil {
		return err
	}
	if SteamRunning() {
		fmt.Fprintf(os.Stderr, "cartridge: steam is running — restart it to see new shortcuts\n")
	}
	return nil
}
