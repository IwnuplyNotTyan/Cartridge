package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"cartridge/internal/config"
	"cartridge/internal/icons"
	"cartridge/internal/steam"
)

// newSteamIconsCmd manages the icon cache: download artwork in bulk and
// apply it to Steam shortcuts (Icon field + grid folder).
//
// Default source is Libretro Thumbnails (free CDN, no key, unlimited
// bulk — the sane choice for thousands of ROMs). With a SteamGridDB API
// key configured, `auto` fills Libretro gaps with premium capsules.
func newSteamIconsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "icons",
		Aliases: []string{"icon", "art", "artwork"},
		Short:   "Download game icons and apply them to Steam",
		Long: `Download per-game artwork into a local cache and attach it to Steam.

The cache lives in the XDG cache dir (or steam.icons_dir) sharded by game
id, so even 10k-game libraries stay fast: normal "steam sync" only stats
local files (no network), while "steam icons sync" does the bulk download
through a bounded worker pool with failure backoff.

Default source is Libretro Thumbnails: no API key, predictable CDN URLs,
unlimited bulk. SteamGridDB (optional key via steam.icons_provider +
steam.steamgriddb_key or $CARTRIDGE_STEAMGRIDDB_KEY) adds premium capsules
for games the free CDN misses.`,
		Example: `  cartridge steam icons status
  cartridge steam icons sync
  cartridge steam icons sync --console gba --limit 200
  cartridge steam icons sync --refresh --concurrency 16
  cartridge steam icons provider steamgriddb
  cartridge steam icons prune
  cartridge steam icons clear --yes`,
	}
	cmd.AddCommand(
		newSteamIconsStatusCmd(),
		newSteamIconsSyncCmd(),
		newSteamIconsPruneCmd(),
		newSteamIconsClearCmd(),
		newSteamIconsProviderCmd(),
	)
	return cmd
}

func newSteamIconsStatusCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"st", "info"},
		Short:   "Show icon cache and Steam grid coverage",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			cache := steam.IconsCache(a.cfg.Steam)
			st := cache.Stat()
			withArt, withWides, withHeroes, withLogos := 0, 0, 0, 0
			for _, g := range a.games {
				if _, ok := cache.Hit(g.ID); ok {
					withArt++
				}
				if _, ok := cache.HitWide(g.ID); ok {
					withWides++
				}
				if _, ok := cache.HitHero(g.ID); ok {
					withHeroes++
				}
				if _, ok := cache.HitLogo(g.ID); ok {
					withLogos++
				}
			}
			targets := steam.DiscoverTargets(a.cfg.Steam.Users)
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), map[string]any{
					"provider":      a.cfg.Steam.EffectiveIconsProvider(),
					"dir":           st.Dir,
					"files":         st.Files,
					"wide_files":    st.WideFiles,
					"hero_files":    st.HeroFiles,
					"logo_files":    st.LogoFiles,
					"bytes":         st.Bytes,
					"failed":        st.Failed,
					"wide_failed":   st.WideFailed,
					"hero_failed":   st.HeroFailed,
					"logo_failed":   st.LogoFailed,
					"games":         len(a.games),
					"games_cached":  withArt,
					"wides_cached":  withWides,
					"heroes_cached": withHeroes,
					"logos_cached":  withLogos,
					"targets":       len(targets),
					"has_grid_key":  strings.TrimSpace(a.cfg.Steam.SteamGridDBKey) != "",
				})
			}
			cprintf(cmd, "%s\n\n", banner("Steam icons", a.cfg.Steam.EffectiveIconsProvider()))
			cprintf(cmd, "  %s\n", kv("provider", a.cfg.Steam.EffectiveIconsProvider()))
			cprintf(cmd, "  %s\n", kv("cache", fmt.Sprintf("%s (%d file(s), %.1f MiB)", st.Dir, st.Files, float64(st.Bytes)/(1<<20))))
			cprintf(cmd, "  %s\n", kv("icons cached", fmt.Sprintf("%d / %d", withArt, len(a.games))))
			cprintf(cmd, "  %s\n", kv("wides cached", fmt.Sprintf("%d / %d", withWides, len(a.games))))
			cprintf(cmd, "  %s\n", kv("heroes cached", fmt.Sprintf("%d / %d", withHeroes, len(a.games))))
			cprintf(cmd, "  %s\n", kv("logos cached", fmt.Sprintf("%d / %d", withLogos, len(a.games))))
			if st.Failed > 0 {
				cprintf(cmd, "  %s\n", kv("without art", fmt.Sprintf("%d (backed off 7d, retry with --refresh)", st.Failed)))
			}
			if st.WideFailed > 0 {
				cprintf(cmd, "  %s\n", kv("without wides", fmt.Sprintf("%d (backed off 7d, retry with --refresh)", st.WideFailed)))
			}
			if st.HeroFailed > 0 {
				cprintf(cmd, "  %s\n", kv("without heroes", fmt.Sprintf("%d (backed off 7d, retry with --refresh)", st.HeroFailed)))
			}
			if st.LogoFailed > 0 {
				cprintf(cmd, "  %s\n", kv("without logos", fmt.Sprintf("%d (backed off 7d, retry with --refresh)", st.LogoFailed)))
			}
			if strings.TrimSpace(a.cfg.Steam.SteamGridDBKey) == "" {
				cprintf(cmd, "  %s\n", dimStyle.Render("tip: set a SteamGridDB key for premium capsules (steam icons provider steamgriddb)"))
			}
			if len(targets) == 0 {
				cprintf(cmd, "\n  %s\n", warn("Steam userdata not found — is Steam installed?"))
				return nil
			}
			cprintf(cmd, "\n")
			for _, t := range targets {
				list, err := steam.LoadFile(t.Path)
				if err != nil {
					cprintf(cmd, "  %s %-8s %s (%v)\n", errMark(""), t.UserID, t.Path, err)
					continue
				}
				art := 0
				for _, s := range list {
					if strings.TrimSpace(s.Icon) != "" {
						art++
					}
				}
				cprintf(cmd, "  %s %-8s %s  %s\n", okStyle.Render("●"), t.UserID, dimStyle.Render(t.Path),
					fmt.Sprintf("%d shortcut(s), %d with icons", len(list), art))
			}
			warnSteamRunning(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table | json")
	return cmd
}

func newSteamIconsSyncCmd() *cobra.Command {
	var (
		console     string
		limit       int
		concurrency int
		refresh     bool
		dryRun      bool
		provider    string
		timeoutMin  int
	)
	cmd := &cobra.Command{
		Use:     "sync",
		Aliases: []string{"fetch", "download", "get"},
		Short:   "Download missing icons and apply them to Steam",
		Example: `  cartridge steam icons sync
  cartridge steam icons sync --console gba --limit 200 --dry-run
  cartridge steam icons sync --refresh --provider libretro`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			games := filterGames(a.games, console, "", false, nil)
			if len(games) == 0 {
				return fmt.Errorf("no games match")
			}
			st := a.cfg.Steam
			if strings.TrimSpace(provider) != "" {
				st.IconsProvider = strings.ToLower(strings.TrimSpace(provider))
			}
			cache := steam.IconsCache(st)
			if concurrency > 0 {
				cache.Concurrency = concurrency
			}
			if limit < 0 {
				limit = 0
			}
			cprintf(cmd, "%s\n\n", banner("Steam icons sync", fmt.Sprintf("%d game(s), provider %s", len(games), cache.Provider)))
			if dryRun {
				// Totals always scan the whole filter (cheap local stats);
				// --limit only caps fetching, not counting.
				missing, missingWide, missingHero, missingLogo := 0, 0, 0, 0
				shown := 0
				for _, g := range games {
					_, iconHit := cache.Hit(g.ID)
					_, wideHit := cache.HitWide(g.ID)
					_, heroHit := cache.HitHero(g.ID)
					_, logoHit := cache.HitLogo(g.ID)
					if !iconHit && !cache.FailedRecently(g.ID, icons.ArtIcon, time.Now()) {
						missing++
					}
					if !wideHit && !cache.FailedRecently(g.ID, icons.ArtWide, time.Now()) {
						missingWide++
					}
					if !heroHit && !cache.FailedRecently(g.ID, icons.ArtHero, time.Now()) {
						missingHero++
					}
					if !logoHit && !cache.FailedRecently(g.ID, icons.ArtLogo, time.Now()) {
						missingLogo++
					}
					if shown < 20 && (!iconHit || !wideHit || !heroHit || !logoHit) {
						cprintf(cmd, "  + %-5s %-40s %s\n", consoleShort(g), truncate(g.Title, 40), dimStyle.Render(g.ConsoleID))
						shown++
					}
				}
				cprintf(cmd, "  %s\n", dimStyle.Render(fmt.Sprintf("%d without icons, %d without wides, %d without heroes, %d without logos (showing %d)", missing, missingWide, missingHero, missingLogo, shown)))
				return nil
			}
			if cache.Provider == icons.ProviderOff {
				return fmt.Errorf("icons are off (provider off)")
			}
			if cache.Provider == icons.ProviderSteamGrid && strings.TrimSpace(cache.GridDBKey) == "" {
				return fmt.Errorf("steamgriddb needs a key: set steam.steamgriddb_key or $CARTRIDGE_STEAMGRIDDB_KEY")
			}
			ctx := context.Background()
			if timeoutMin > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMin)*time.Minute)
				defer cancel()
			}
			start := time.Now()
			total := len(games)
			if limit > 0 && limit < total {
				total = limit
			}
			prog := newSyncProgress(cmd, total, start)
			res := cache.Sync(ctx, steam.ToIconGames(games), refresh, limit, prog.each)
			prog.done()
			el := time.Since(start).Round(time.Second)
			cprintf(cmd, "  %s\n", kv("downloaded", fmt.Sprintf("%d icons + %d wides + %d heroes + %d logos fetched, %d+%d+%d+%d skipped, %d+%d+%d+%d failed, %.1f MiB in %s",
				res.Fetched, res.WideFetched, res.HeroFetched, res.LogoFetched, res.Skipped, res.WideSkipped, res.HeroSkipped, res.LogoSkipped, res.Failed, res.WideFailed, res.HeroFailed, res.LogoFailed, float64(res.Bytes)/(1<<20), el)))
			// Apply: re-sync shortcuts so Icon paths + grid art land in Steam.
			targets := steam.DiscoverTargets(a.cfg.Steam.Users)
			if len(targets) == 0 {
				cprintf(cmd, "  %s\n", dimStyle.Render("art cached; Steam userdata not found — apply later with: cartridge steam sync"))
				return nil
			}
			exe, startDir := steamExeDir(cmd, a.cfg)
			results, err := steam.SyncAllTracked(games, a.cfg.Steam, exe, startDir, false, steam.TrackFilePath(a.cfg.Library))
			if err != nil {
				return err
			}
			for _, r := range results {
				cprintf(cmd, "  %s user %s: %d icon(s) + %d wide(s) + %d hero(es) + %d logo(s), %d+%d+%d+%d grid file(s) new, %d+%d+%d+%d with art\n     %s\n",
					ok(""), r.UserID, r.IconsApplied, r.WideWithArt, r.HeroWithArt, r.LogoWithArt, r.GridDeployed, r.WideDeployed, r.HeroDeployed, r.LogoDeployed, r.GridWithArt, r.WideWithArt, r.HeroWithArt, r.LogoWithArt, dimStyle.Render(r.Path))
			}
			warnSteamRunning(cmd)
			cprintf(cmd, "  %s\n", dimStyle.Render("restart Steam to see the new icons"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&console, "console", "c", "", "only this console (id, short or name)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max games to attempt (0 = all)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 0, "parallel downloads (default 8, max 32)")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "re-download even when cached (ignores failure backoff)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "count missing art without downloading")
	cmd.Flags().StringVar(&provider, "provider", "", "override provider for this run: auto | libretro | steamgriddb")
	cmd.Flags().IntVar(&timeoutMin, "timeout-min", 0, "overall timeout in minutes (0 = none)")
	cmd.Flags().String("exe", "", "cartridge binary for shortcuts (default: current binary)")
	cmd.Flags().String("start-dir", "", "shortcut working directory (default: binary dir)")
	return cmd
}

func newSteamIconsPruneCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Drop cached icons for games no longer in the library",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			cache := steam.IconsCache(a.cfg.Steam)
			keep := map[string]bool{}
			for _, g := range a.games {
				keep[g.ID] = true
			}
			if dryRun {
				st := cache.Stat()
				orphans := st.Files - len(a.games)
				if orphans < 0 {
					orphans = 0
				}
				cprintf(cmd, "  %s\n", kv("cache", fmt.Sprintf("%s: %d file(s), ~%d orphan(s)", st.Dir, st.Files, orphans)))
				cprintf(cmd, "  %s\n", dimStyle.Render("(dry run — nothing removed)"))
				return nil
			}
			removed, freed, err := cache.Prune(keep)
			if err != nil {
				return err
			}
			cprintf(cmd, "  %s\n", ok(fmt.Sprintf("pruned %d file(s), freed %.1f MiB", removed, float64(freed)/(1<<20))))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "only count without removing")
	return cmd
}

func newSteamIconsClearCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Wipe the whole icon cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			if !yes {
				return fmt.Errorf("refusing to clear without --yes")
			}
			cache := steam.IconsCache(a.cfg.Steam)
			if err := cache.Clear(); err != nil {
				return err
			}
			cprintf(cmd, "  %s\n", ok("icon cache cleared: "+cache.Dir))
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm wiping the cache")
	return cmd
}

func newSteamIconsProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "provider [auto|libretro|steamgriddb|off]",
		Aliases: []string{"source", "mode"},
		Short:   "Show or set the icon provider",
		Long: `Show or set where icons come from.

  auto        Libretro CDN first, SteamGridDB fills gaps (needs a key) [default]
  libretro    free CDN only, no key, unlimited bulk
  steamgriddb premium capsules only (needs steam.steamgriddb_key)
  off         disable icons entirely (syncs skip Icon + grid)

The SteamGridDB key can also come from $CARTRIDGE_STEAMGRIDDB_KEY.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				cprintf(cmd, "%s\n\n", banner("Steam icons provider", a.cfg.Steam.EffectiveIconsProvider()))
				cprintf(cmd, "  %s\n", kv("provider", a.cfg.Steam.EffectiveIconsProvider()))
				if k := strings.TrimSpace(a.cfg.Steam.SteamGridDBKey); k != "" {
					masked := k[:minInt(4, len(k))] + "…"
					cprintf(cmd, "  %s\n", kv("steamgriddb key", masked+" (set)"))
				} else {
					cprintf(cmd, "  %s\n", kv("steamgriddb key", "(not set — libretro only)"))
				}
				cprintf(cmd, "  %s\n", kv("cache", steam.IconsDir(a.cfg.Steam)))
				return nil
			}
			mode := strings.ToLower(strings.TrimSpace(args[0]))
			switch mode {
			case icons.ProviderAuto, icons.ProviderLibretro, icons.ProviderSteamGrid, icons.ProviderOff:
			default:
				return fmt.Errorf("want auto|libretro|steamgriddb|off, got %q", args[0])
			}
			fileVals := a.cfg.ToFile()
			if fileVals.Steam == nil {
				def := config.DefaultSteamConfig()
				fileVals.Steam = &def
			}
			fileVals.Steam.IconsProvider = mode
			if keyFlag, _ := cmd.Flags().GetString("key"); cmd.Flags().Changed("key") {
				fileVals.Steam.SteamGridDBKey = strings.TrimSpace(keyFlag)
			}
			if dirFlag, _ := cmd.Flags().GetString("dir"); cmd.Flags().Changed("dir") {
				fileVals.Steam.IconsDir = strings.TrimSpace(dirFlag)
			}
			if err := config.SaveFile(a.cfg.SavePath(), fileVals); err != nil {
				return err
			}
			cprintf(cmd, "%s\n", ok("provider "+mode+" saved to "+a.cfg.SavePath()))
			if mode == icons.ProviderSteamGrid && strings.TrimSpace(fileVals.Steam.SteamGridDBKey) == "" {
				cprintf(cmd, "  %s\n", warn("no SteamGridDB key set — add one with --key or $CARTRIDGE_STEAMGRIDDB_KEY"))
			}
			return nil
		},
	}
	cmd.Flags().String("key", "", "SteamGridDB API key (empty clears it)")
	cmd.Flags().String("dir", "", "icon cache directory (empty = XDG default)")
	return cmd
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// syncProgress renders a live one-line progress meter on stderr while the
// bulk download runs, so a 10k-game sync never looks hung. Updates are
// throttled (every 25 games or 500ms) and end with a newline.
type syncProgress struct {
	total int
	start time.Time
	doneN int64
	okN   int64
	failN int64
	last  time.Time
}

func newSyncProgress(cmd *cobra.Command, total int, start time.Time) *syncProgress {
	p := &syncProgress{total: total, start: start}
	_, _ = fmt.Fprintf(os.Stderr, "  fetching icons: 0/%d (0%%)\n", total)
	_ = cmd
	return p
}

func (p *syncProgress) each(_ string, ok bool) {
	d := atomic.AddInt64(&p.doneN, 1)
	if ok {
		atomic.AddInt64(&p.okN, 1)
	} else {
		atomic.AddInt64(&p.failN, 1)
	}
	now := time.Now()
	if d < int64(p.total) && d%25 != 0 && now.Sub(p.last) < 500*time.Millisecond {
		return
	}
	p.last = now
	pct := 0
	if p.total > 0 {
		pct = int(d * 100 / int64(p.total))
	}
	el := now.Sub(p.start).Round(time.Second)
	_, _ = fmt.Fprintf(os.Stderr, "  fetching icons: %d/%d (%d%%) ok %d fail %d • %s elapsed\n",
		d, p.total, pct, atomic.LoadInt64(&p.okN), atomic.LoadInt64(&p.failN), el)
}

func (p *syncProgress) done() {
	_, _ = fmt.Fprintf(os.Stderr, "  fetching icons: %d/%d done (ok %d fail %d) • %s elapsed\n",
		atomic.LoadInt64(&p.doneN), p.total,
		atomic.LoadInt64(&p.okN), atomic.LoadInt64(&p.failN),
		time.Since(p.start).Round(time.Second))
}
