package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"cartridge/internal/config"
	"cartridge/internal/rom"
	"cartridge/internal/steam"
)

func newSteamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "steam",
		Aliases: []string{"st"},
		Short:   "Add games to Steam as non-Steam shortcuts",
		Long: `Add single games or the whole shelf to Steam as non-Steam shortcuts.

Each game launches via "cartridge play <id>" and gets one Steam category
per console ("Cartridge - GBA" by default); the shared "all games" root
category is off by default (steam categories --all-games on).
The same categories are also written as real Steam collections into
the client's cloud storage, so games appear grouped per console with
no manual work (dynamic collections can't do this: their tag filter
only knows store tags, not shortcut tags).
Category names support {prefix} {console} {console_id} {short} {title}
{name} placeholders ({name} = per-console custom name or short tag).
Steam must be restarted (or fully closed while syncing) to pick up changes.`,
		Example: `  cartridge steam status
  cartridge steam add zelda
  cartridge steam add-all --console gba
  cartridge steam sync --prune
  cartridge steam remove sonic
  cartridge steam reset --yes
  cartridge steam categories --set gba="Game Boy Advance"
  cartridge steam auto-add on`,
	}

	cmd.AddCommand(
		newSteamStatusCmd(),
		newSteamAddCmd(),
		newSteamAddAllCmd(),
		newSteamRemoveCmd(),
		newSteamPruneCmd(),
		newSteamResetCmd(),
		newSteamCategoriesCmd(),
		newSteamAutoAddCmd(),
		newSteamIconsCmd(),
	)
	return cmd
}

func steamExeDir(cmd *cobra.Command, cfg config.Config) (string, string) {
	exe, _ := cmd.Flags().GetString("exe")
	startDir, _ := cmd.Flags().GetString("start-dir")
	if strings.TrimSpace(exe) == "" {
		exe = steam.DefaultExe(cfg.Steam)
	}
	if strings.TrimSpace(startDir) == "" {
		startDir = dirOf(exe)
	}
	return exe, startDir
}

func warnSteamRunning(cmd *cobra.Command) {
	if steam.SteamRunning() {
		cprintf(cmd, "  %s\n", warnStyle.Render("Steam is running — close it fully, or changes may be overwritten on exit"))
	}
}

func newSteamStatusCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"st", "info"},
		Short:   "Show Steam targets and auto-add settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			targets := steam.DiscoverTargets(a.cfg.Steam.Users)
			if format == "json" {
				type row struct {
					User  string `json:"user"`
					Path  string `json:"path"`
					Count int    `json:"shortcuts"`
				}
				var rows []row
				for _, t := range targets {
					list, _ := steam.LoadFile(t.Path)
					rows = append(rows, row{t.UserID, t.Path, len(list)})
				}
				if rows == nil {
					rows = []row{}
				}
				colls := map[string]map[string]int{}
				for _, t := range targets {
					infos, _ := steam.ReadCollections(dirOfConfig(t.Path))
					m := map[string]int{}
					for _, in := range infos {
						m[in.Name] = in.Members
					}
					colls[t.UserID] = m
				}
				return printJSON(cmd.OutOrStdout(), map[string]any{
					"auto_add":          a.cfg.Steam.AutoAdd,
					"prefix":            a.cfg.Steam.EffectivePrefix(),
					"all_games":         a.cfg.Steam.AllGames,
					"template":          a.cfg.Steam.EffectiveTemplate(),
					"custom_names":      a.cfg.Steam.Categories,
					"targets":           rows,
					"categories":        steamCategoryCounts(a.games, a.cfg),
					"steam_collections": colls,
				})
			}
			cprintf(cmd, "%s\n\n", banner("Steam", fmt.Sprintf("auto-add: %v", a.cfg.Steam.AutoAdd)))
			cprintf(cmd, "  %s\n", kv("prefix", q(a.cfg.Steam.EffectivePrefix())))
			cprintf(cmd, "  %s\n", kv("all-games", onOff(a.cfg.Steam.AllGames)))
			cprintf(cmd, "  %s\n", kv("template", a.cfg.Steam.EffectiveTemplate()))
			if n := len(a.cfg.Steam.Categories); n > 0 {
				cprintf(cmd, "  %s\n", kv("custom names", fmt.Sprintf("%d", n)))
			}
			cprintf(cmd, "  %s\n", kv("exe", steam.DefaultExe(a.cfg.Steam)))
			cprintf(cmd, "  %s\n", kv("games", fmt.Sprintf("%d", len(a.games))))
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
				cprintf(cmd, "  %s %-8s %s  %s\n", okStyle.Render("●"), t.UserID, dimStyle.Render(t.Path), fmt.Sprintf("%d shortcut(s)", len(list)))
			}
			printSteamCategories(cmd, a)
			printSteamCollections(cmd, a, targets)
			warnSteamRunning(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table | json")
	return cmd
}

// steamCategoryCounts groups library games by the Steam categories
// (tags) they sync with: root prefix + one per-console tag.
func steamCategoryCounts(games []rom.Game, cfg config.Config) map[string]int {
	counts := map[string]int{}
	for _, g := range games {
		for _, t := range steam.TagsFor(g, cfg.Steam) {
			counts[t]++
		}
	}
	return counts
}

// printSteamCategories shows how games spread across Steam categories.
func printSteamCategories(cmd *cobra.Command, a *app) {
	counts := steamCategoryCounts(a.games, a.cfg)
	if len(counts) == 0 {
		return
	}
	cprintf(cmd, "\n%s\n", dimStyle.Render(fmt.Sprintf("Categories (%d game(s) in library):", len(a.games))))
	prefix := strings.TrimSpace(a.cfg.Steam.EffectivePrefix())
	for _, tag := range orderedCategoryTags(counts, prefix) {
		cprintf(cmd, "  %-28s %s\n", consoleStyle.Render(tag), formatGames(counts[tag]))
	}
}

// printSteamCollections shows which of our categories exist as real Steam
// collections per profile (with member counts), so the grouping is visible
// without opening the client.
func printSteamCollections(cmd *cobra.Command, a *app, targets []steam.UserTarget) {
	counts := steamCategoryCounts(a.games, a.cfg)
	if len(counts) == 0 {
		return
	}
	for _, t := range targets {
		infos, err := steam.ReadCollections(dirOfConfig(t.Path))
		if err != nil {
			cprintf(cmd, "\n  %s user %s: collections unreadable (%v)\n", errMark(""), t.UserID, err)
			continue
		}
		if len(infos) == 0 {
			continue
		}
		byName := map[string]int{}
		for _, in := range infos {
			byName[strings.ToLower(strings.TrimSpace(in.Name))] = in.Members
		}
		cprintf(cmd, "\n%s\n", dimStyle.Render(fmt.Sprintf("Collections in Steam (user %s):", t.UserID)))
		wantLower := map[string]bool{}
		for tag := range counts {
			wantLower[strings.ToLower(tag)] = true
		}
		for _, tag := range orderedCategoryTags(counts, strings.TrimSpace(a.cfg.Steam.EffectivePrefix())) {
			if n, ok := byName[strings.ToLower(tag)]; ok {
				cprintf(cmd, "  %-28s %s\n", consoleStyle.Render(tag), formatGames(n))
			} else {
				cprintf(cmd, "  %-28s %s\n", consoleStyle.Render(tag), dimStyle.Render("not synced yet"))
			}
		}
		// Orphaned: live in Steam but no game maps to them anymore
		// (renamed category, stale library row, ...). `steam prune`
		// retires ours-only ones; mixed ones need manual cleanup.
		var orphaned []steam.CollectionInfo
		for _, in := range infos {
			if wantLower[strings.ToLower(strings.TrimSpace(in.Name))] {
				continue
			}
			if isSteamSystemCollection(in) {
				continue
			}
			orphaned = append(orphaned, in)
		}
		if len(orphaned) > 0 {
			cprintf(cmd, "  %s\n", warn("orphaned (not in library config — prune retires ours-only ones):"))
			for _, in := range orphaned {
				cprintf(cmd, "  %-28s %s\n", consoleStyle.Render(in.Name), formatGames(in.Members))
			}
		}
	}
}

// orderedCategoryTags sorts category tags with the prefix first.
func orderedCategoryTags(counts map[string]int, prefix string) []string {
	rest := make([]string, 0, len(counts))
	for tag := range counts {
		if tag == prefix {
			continue
		}
		rest = append(rest, tag)
	}
	sort.Strings(rest)
	if prefix != "" {
		if _, ok := counts[prefix]; ok {
			return append([]string{prefix}, rest...)
		}
	}
	return rest
}

// isSteamSystemCollection reports Steam-owned collections (Favorites,
// Hidden, tag-derived) that are never orphaned by definition.
func isSteamSystemCollection(in steam.CollectionInfo) bool {
	k := strings.ToLower(strings.TrimSpace(in.Key))
	return k == "user-collections.favorite" ||
		k == "user-collections.hidden" ||
		strings.HasPrefix(k, "user-collections.from-tag-")
}

// dirOfConfig returns the config dir holding shortcuts.vdf.
func dirOfConfig(shortcutsPath string) string {
	if i := strings.LastIndexAny(shortcutsPath, `/\`); i >= 0 {
		return shortcutsPath[:i]
	}
	return "."
}

func newSteamAddCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "add <game>",
		Short: "Add one game to Steam",
		Example: `  cartridge steam add zelda
  cartridge steam add a1b2c3 --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			g, err := findOneGame(a.games, args[0])
			if err != nil {
				return err
			}
			exe, startDir := steamExeDir(cmd, a.cfg)
			targets := steam.DiscoverTargets(a.cfg.Steam.Users)
			if len(targets) == 0 {
				return fmt.Errorf("steam userdata not found (is Steam installed?)")
			}
			s := steam.BuildShortcut(g, a.cfg.Steam, exe, startDir)
			cprintf(cmd, "%s\n\n", banner("Steam add", g.Title))
			cprintf(cmd, "  %s\n", kv("shortcut", s.AppName))
			cprintf(cmd, "  %s\n", kv("categories", strings.Join(s.Tags, ", ")))
			cprintf(cmd, "  %s\n", kv("command", fmt.Sprintf("%s %s", s.Exe, s.LaunchOptions)))
			if dryRun {
				for _, t := range targets {
					cprintf(cmd, "  %s\n", kv("would write", fmt.Sprintf("user %s: %s", t.UserID, t.Path)))
				}
				return nil
			}
			for _, t := range targets {
				res, err := steam.SyncFileTracked(t.Path, []rom.Game{g}, a.cfg.Steam, exe, startDir, false, t.UserID, steam.TrackFilePath(a.cfg.Library))
				if err != nil {
					return err
				}
				cprintf(cmd, "  %s user %s: %s, %s\n", ok(""), t.UserID, t.Path, collSummary(res))
			}
			warnSteamRunning(cmd)
			cprintf(cmd, "  %s\n", dimStyle.Render("restart Steam to see the new shortcut"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the shortcut without writing")
	cmd.Flags().String("exe", "", "cartridge binary for the shortcut (default: current binary)")
	cmd.Flags().String("start-dir", "", "shortcut working directory (default: binary dir)")
	return cmd
}

func newSteamAddAllCmd() *cobra.Command {
	var (
		console string
		dryRun  bool
		prune   bool
	)
	cmd := &cobra.Command{
		Use:     "add-all",
		Aliases: []string{"sync", "addall", "bulk"},
		Short:   "Add many games to Steam (optionally one console)",
		Example: `  cartridge steam add-all
  cartridge steam add-all --console gba --dry-run
  cartridge steam sync --prune`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			games := filterGames(a.games, console, "", false, nil)
			if len(games) == 0 {
				return fmt.Errorf("no games match")
			}
			exe, startDir := steamExeDir(cmd, a.cfg)
			targets := steam.DiscoverTargets(a.cfg.Steam.Users)
			if len(targets) == 0 {
				return fmt.Errorf("steam userdata not found (is Steam installed?)")
			}
			cprintf(cmd, "%s\n\n", banner("Steam sync", fmt.Sprintf("%d game(s)", len(games))))
			if dryRun {
				shown := 0
				for _, g := range games {
					s := steam.BuildShortcut(g, a.cfg.Steam, exe, startDir)
					cprintf(cmd, "  + %-5s %-40s %s\n", consoleShort(g), truncate(s.AppName, 40), dimStyle.Render(strings.Join(s.Tags, ", ")))
					if shown++; shown >= 20 {
						cprintf(cmd, "  %s\n", dimStyle.Render(fmt.Sprintf("… and %d more", len(games)-shown)))
						break
					}
				}
				for _, t := range targets {
					cprintf(cmd, "  %s\n", kv("would write", fmt.Sprintf("user %s: %s", t.UserID, t.Path)))
				}
				return nil
			}
			results, err := steam.SyncAllTracked(games, a.cfg.Steam, exe, startDir, prune, steam.TrackFilePath(a.cfg.Library))
			if err != nil {
				return err
			}
			for _, r := range results {
				cprintf(cmd, "  %s user %s: +%d kept %d pruned %d total %d, %s\n     %s\n",
					ok(""), r.UserID, r.Added, r.Kept, r.Removed, r.Total, collSummary(r), dimStyle.Render(r.Path))
				if r.IconsApplied > 0 || r.GridWithArt > 0 || r.HeroWithArt > 0 || r.LogoWithArt > 0 {
					cprintf(cmd, "     %s\n", dimStyle.Render(fmt.Sprintf("icons: %d applied, %d grid new / %d with art; heroes: %d new / %d with art; logos: %d new / %d with art", r.IconsApplied, r.GridDeployed, r.GridWithArt, r.HeroDeployed, r.HeroWithArt, r.LogoDeployed, r.LogoWithArt)))
				}
			}
			warnSteamRunning(cmd)
			cprintf(cmd, "  %s\n", dimStyle.Render("restart Steam to see the new shortcuts"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&console, "console", "c", "", "only this console (id, short or name)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change without writing")
	cmd.Flags().BoolVar(&prune, "prune", false, "also drop cartridge shortcuts whose games are gone")
	cmd.Flags().String("exe", "", "cartridge binary for shortcuts (default: current binary)")
	cmd.Flags().String("start-dir", "", "shortcut working directory (default: binary dir)")
	return cmd
}

func newSteamRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <game>",
		Aliases: []string{"rm", "del"},
		Short:   "Remove one game from Steam",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			g, err := findOneGame(a.games, args[0])
			if err != nil {
				return err
			}
			exe, _ := steamExeDir(cmd, a.cfg)
			results, err := steam.RemoveGames([]rom.Game{g}, a.cfg.Steam, exe)
			if err != nil {
				return err
			}
			for _, r := range results {
				cprintf(cmd, "  %s user %s: removed %d (%s), %s\n", ok(""), r.UserID, r.Removed, dimStyle.Render(r.Path), collSummary(r))
			}
			warnSteamRunning(cmd)
			return nil
		},
	}
	cmd.Flags().String("exe", "", "cartridge binary used in shortcuts (default: current binary)")
	cmd.Flags().String("start-dir", "", "unused, kept for symmetry with add")
	return cmd
}

func newSteamPruneCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Drop Steam shortcuts whose games are gone from the library",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			exe, startDir := steamExeDir(cmd, a.cfg)
			targets := steam.DiscoverTargets(a.cfg.Steam.Users)
			if len(targets) == 0 {
				return fmt.Errorf("steam userdata not found")
			}
			if dryRun {
				for _, t := range targets {
					list, err := steam.LoadFile(t.Path)
					if err != nil {
						return err
					}
					keep := map[string]bool{}
					for _, g := range a.games {
						keep[steam.LaunchOptionsFor(g)] = true
					}
					stale := 0
					for _, e := range list {
						if !keep[e.LaunchOptions] && strings.HasPrefix(strings.TrimSpace(e.LaunchOptions), "play ") {
							stale++
						}
					}
					cprintf(cmd, "  user %s: %d stale (%s)\n", t.UserID, stale, t.Path)
				}
				return nil
			}
			results, err := steam.SyncAllTracked(a.games, a.cfg.Steam, exe, startDir, true, steam.TrackFilePath(a.cfg.Library))
			if err != nil {
				return err
			}
			for _, r := range results {
				cprintf(cmd, "  %s user %s: pruned %d, total %d (%s), %s\n", ok(""), r.UserID, r.Removed, r.Total, dimStyle.Render(r.Path), collSummary(r))
			}
			warnSteamRunning(cmd)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "only count stale shortcuts")
	cmd.Flags().String("exe", "", "cartridge binary used in shortcuts (default: current binary)")
	cmd.Flags().String("start-dir", "", "shortcut working directory (default: binary dir)")
	return cmd
}

func newSteamResetCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:     "reset",
		Aliases: []string{"clear", "drop-all"},
		Short:   "Remove ALL cartridge games from Steam (foreign shortcuts kept)",
		Long: `Drop every cartridge-managed shortcut from every Steam profile.
Games you added to Steam by hand (or via other tools) are never touched.

With auto-add on, the next scan/rescan will add the games back —
turn it off first if the reset should stick.`,
		Example: `  cartridge steam reset --dry-run
  cartridge steam reset --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			exe, _ := steamExeDir(cmd, a.cfg)
			targets := steam.DiscoverTargets(a.cfg.Steam.Users)
			if len(targets) == 0 {
				return fmt.Errorf("steam userdata not found")
			}
			total := 0
			cprintf(cmd, "%s\n\n", banner("Steam reset", "cartridge-managed shortcuts"))
			for _, t := range targets {
				list, err := steam.LoadFile(t.Path)
				if err != nil {
					return err
				}
				n := steam.CountManaged(list, a.cfg.Steam, exe)
				total += n
				cprintf(cmd, "  user %s: %d managed (%s)\n", t.UserID, n, dimStyle.Render(t.Path))
			}
			if dryRun {
				cprintf(cmd, "  %s\n", dimStyle.Render("(dry run — nothing removed)"))
				return nil
			}
			if total == 0 {
				cprintf(cmd, "  %s\n", dimStyle.Render("nothing to remove"))
				return nil
			}
			if !yes {
				return fmt.Errorf("refusing to reset without --yes")
			}
			results, err := steam.ResetAll(a.cfg.Steam, exe)
			if err != nil {
				return err
			}
			for _, r := range results {
				cprintf(cmd, "  %s user %s: removed %d, left %d, %s\n", ok(""), r.UserID, r.Removed, r.Total, collSummary(r))
			}
			if a.cfg.Steam.AutoAdd {
				cprintf(cmd, "  %s\n", warn("auto-add is on: the next scan will add the games back (steam auto-add off)"))
			}
			warnSteamRunning(cmd)
			cprintf(cmd, "  %s\n", dimStyle.Render("restart Steam to apply"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "only count managed shortcuts")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm removal")
	cmd.Flags().String("exe", "", "cartridge binary used in shortcuts (default: current binary)")
	return cmd
}

func newSteamCategoriesCmd() *cobra.Command {
	var (
		sets     []string
		clears   []string
		clearAll bool
		format   string
	)
	cmd := &cobra.Command{
		Use:     "categories",
		Aliases: []string{"cats", "names"},
		Short:   "List and customize Steam category names",
		Long: `Show the effective Steam category per console and customize names.

Custom names are set per console (id or short tag) and apply through the
{name} template placeholder (falls back to the short tag). The shared
"all games" root category (the prefix tag on every game) is off by default.`,
		Example: `  cartridge steam categories
  cartridge steam categories --set gba="Game Boy Advance" --set snes="Super Nintendo"
  cartridge steam categories --clear gba
  cartridge steam categories --all-games on --prefix "Emu"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			fileVals := a.cfg.ToFile()
			if fileVals.Steam == nil {
				def := config.DefaultSteamConfig()
				fileVals.Steam = &def
			}
			st := fileVals.Steam
			changed := false
			if v, present := flagPresent(cmd, "prefix"); present {
				st.Prefix = strings.TrimSpace(v)
				changed = true
			}
			if v, present := flagPresent(cmd, "template"); present {
				st.CategoryTemplate = strings.TrimSpace(v)
				if st.CategoryTemplate == "" {
					st.CategoryTemplate = config.DefaultSteamConfig().CategoryTemplate
				}
				changed = true
			}
			if v, present := flagPresent(cmd, "all-games"); present {
				on, err := parseOnOff(v)
				if err != nil {
					return err
				}
				st.AllGames = on
				changed = true
			}
			if clearAll {
				if len(st.Categories) > 0 {
					st.Categories = nil
					changed = true
				}
			}
			for _, c := range clears {
				id, ok := steam.CanonicalConsoleID(c)
				if !ok {
					return fmt.Errorf("unknown console %q", c)
				}
				if st.Categories != nil {
					if _, has := st.Categories[id]; has {
						delete(st.Categories, id)
						changed = true
					}
				}
			}
			for _, s := range sets {
				id, name, err := parseCategorySet(s)
				if err != nil {
					return err
				}
				if st.Categories == nil {
					st.Categories = map[string]string{}
				}
				if strings.TrimSpace(name) == "" {
					if _, has := st.Categories[id]; has {
						delete(st.Categories, id)
						changed = true
					}
					continue
				}
				if st.Categories[id] != strings.TrimSpace(name) {
					st.Categories[id] = strings.TrimSpace(name)
					changed = true
				}
			}
			if changed {
				path := a.cfg.SavePath()
				if err := config.SaveFile(path, fileVals); err != nil {
					return err
				}
				cprintf(cmd, "%s\n", ok("saved to "+path))
			}
			// Refresh resolved view after mutation.
			a.cfg.Steam = *st
			rows := steamCategoryRows(a)
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), map[string]any{
					"prefix":     st.EffectivePrefix(),
					"all_games":  st.AllGames,
					"template":   st.EffectiveTemplate(),
					"categories": rows,
				})
			}
			cprintf(cmd, "%s\n\n", banner("Steam categories", fmt.Sprintf("template %s, all-games %s", st.EffectiveTemplate(), onOff(st.AllGames))))
			for _, r := range rows {
				mark := dimStyle.Render("·")
				custom := ""
				if r.Custom {
					mark = okStyle.Render("*")
					custom = dimStyle.Render("  custom")
				}
				cprintf(cmd, "  %s %-6s %-28s %s%s\n", mark,
					consoleStyle.Render(r.Short), titleStyle.Render(r.Category),
					dimStyle.Render(formatGames(r.Games)), custom)
			}
			cprintf(cmd, "\n  %s\n", dimStyle.Render("placeholders: {prefix} {console} {console_id} {short} {title} {name}"))
			if len(st.Categories) > 0 && !strings.Contains(st.EffectiveTemplate(), "{name}") {
				cprintf(cmd, "  %s\n", warn("custom names need {name} in the template to take effect"))
			}
			// Renames apply to existing Steam shortcuts/collections at once.
			if changed {
				if noSync, _ := cmd.Flags().GetBool("no-sync"); !noSync {
					exe, startDir := steamExeDir(cmd, a.cfg)
					results, err := steam.SyncAllTracked(a.games, *st, exe, startDir, false, steam.TrackFilePath(a.cfg.Library))
					if err != nil {
						return err
					}
					for _, r := range results {
						cprintf(cmd, "  %s user %s: +%d kept %d total %d, %s\n",
							ok(""), r.UserID, r.Added, r.Kept, r.Total, collSummary(r))
					}
					warnSteamRunning(cmd)
				} else {
					cprintf(cmd, "  %s\n", dimStyle.Render("sync skipped (--no-sync); run: cartridge steam sync"))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&sets, "set", nil, "custom name as id=name (e.g. --set gba=\"Game Boy\")")
	cmd.Flags().StringArrayVar(&clears, "clear", nil, "drop the custom name for a console id")
	cmd.Flags().BoolVar(&clearAll, "clear-all", false, "drop all custom names")
	cmd.Flags().String("all-games", "", "shared root category on every game: on | off")
	cmd.Flags().String("prefix", "", "root category, e.g. \"Cartridge\" (empty disables it)")
	cmd.Flags().String("template", "", "per-console category template (see placeholders above)")
	cmd.Flags().Bool("no-sync", false, "save names without re-syncing Steam")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table | json")
	return cmd
}

// categoryRow is one console's effective Steam category.
type categoryRow struct {
	Console  string `json:"console"`
	Short    string `json:"short"`
	Category string `json:"category"`
	Custom   bool   `json:"custom"`
	Games    int    `json:"games"`
}

// steamCategoryRows lists consoles present in the library plus consoles
// with a custom name but no games, each with its effective category.
func steamCategoryRows(a *app) []categoryRow {
	counts := map[string]int{}
	names := map[string]string{}
	shorts := map[string]string{}
	for _, g := range a.games {
		counts[g.ConsoleID]++
		names[g.ConsoleID] = g.ConsoleName
		shorts[g.ConsoleID] = consoleShort(g)
	}
	for id := range a.cfg.Steam.Categories {
		if _, ok := counts[id]; !ok {
			counts[id] = 0
		}
		if _, ok := names[id]; !ok {
			names[id] = rom.ByID(id).Name
		}
		if _, ok := shorts[id]; !ok {
			shorts[id] = rom.ByID(id).Short
		}
	}
	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var rows []categoryRow
	for _, id := range ids {
		probe := rom.Game{ConsoleID: id, ConsoleName: names[id]}
		custom := strings.TrimSpace(steam.CustomName(a.cfg.Steam, probe)) != ""
		rows = append(rows, categoryRow{
			Console:  names[id],
			Short:    shorts[id],
			Category: singleCategory(a.cfg.Steam, probe),
			Custom:   custom,
			Games:    counts[id],
		})
	}
	return rows
}

// singleCategory renders the per-console category (first tag) for display.
func singleCategory(st config.SteamConfig, g rom.Game) string {
	tags := steam.TagsFor(g, st)
	if len(tags) == 0 {
		return ""
	}
	if st.AllGames && len(tags) > 1 {
		return tags[1]
	}
	return tags[0]
}

// parseCategorySet splits "id=name" and canonicalizes the console id.
func parseCategorySet(s string) (string, string, error) {
	i := strings.Index(s, "=")
	if i < 0 {
		return "", "", fmt.Errorf("want id=name, got %q", s)
	}
	id, ok := steam.CanonicalConsoleID(s[:i])
	if !ok {
		return "", "", fmt.Errorf("unknown console %q", strings.TrimSpace(s[:i]))
	}
	return id, s[i+1:], nil
}

// parseOnOff parses on/off/true/false/1/0.
func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("want on|off, got %q", s)
	}
}

func newSteamAutoAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "auto-add [on|off|status]",
		Aliases: []string{"autoadd", "auto"},
		Short:   "Show or toggle automatic adding of new games to Steam",
		Example: `  cartridge steam auto-add status
  cartridge steam auto-add on
  cartridge steam auto-add off --prefix "Emu" --template "{prefix}/{short}"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			prefix, _ := cmd.Flags().GetString("prefix")
			template, _ := cmd.Flags().GetString("template")
			exeFlag, _ := cmd.Flags().GetString("exe")

			mode := ""
			if len(args) > 0 {
				mode = strings.ToLower(strings.TrimSpace(args[0]))
			}
			changed := false
			fileVals := a.cfg.ToFile()
			if fileVals.Steam == nil {
				def := config.DefaultSteamConfig()
				fileVals.Steam = &def
			}
			switch mode {
			case "", "status", "show":
				// Just print below.
			case "on", "enable", "true", "1":
				fileVals.Steam.AutoAdd = true
				changed = true
			case "off", "disable", "false", "0":
				fileVals.Steam.AutoAdd = false
				changed = true
			default:
				return fmt.Errorf("want on|off|status, got %q", args[0])
			}
			if v, present := flagPresent(cmd, "prefix"); present {
				fileVals.Steam.Prefix = strings.TrimSpace(v)
				changed = true
				_ = prefix
			}
			if v, present := flagPresent(cmd, "template"); present {
				fileVals.Steam.CategoryTemplate = strings.TrimSpace(v)
				if fileVals.Steam.CategoryTemplate == "" {
					fileVals.Steam.CategoryTemplate = config.DefaultSteamConfig().CategoryTemplate
				}
				changed = true
				_ = template
			}
			if v, present := flagPresent(cmd, "exe"); present {
				fileVals.Steam.Exe = strings.TrimSpace(v)
				changed = true
				_ = exeFlag
			}
			if changed {
				path := a.cfg.SavePath()
				if err := config.SaveFile(path, fileVals); err != nil {
					return err
				}
				cprintf(cmd, "%s\n", ok("saved to "+path))
			}
			st := fileVals.Steam
			cprintf(cmd, "%s\n\n", banner("Steam auto-add", onOff(st.AutoAdd)))
			cprintf(cmd, "  %s\n", kv("auto-add", onOff(st.AutoAdd)))
			cprintf(cmd, "  %s\n", kv("prefix", q(strings.TrimSpace(st.Prefix))))
			cprintf(cmd, "  %s\n", kv("template", st.EffectiveTemplate()))
			if strings.TrimSpace(st.Exe) != "" {
				cprintf(cmd, "  %s\n", kv("exe", st.Exe))
			}
			if st.AutoAdd {
				cprintf(cmd, "\n  %s\n", dimStyle.Render("new games from `scan` / TUI rescan are added to Steam automatically"))
			} else {
				cprintf(cmd, "\n  %s\n", dimStyle.Render("enable with: cartridge steam auto-add on"))
			}
			return nil
		},
	}
	cmd.Flags().String("prefix", "", "root category, e.g. \"Cartridge\" (empty disables it)")
	cmd.Flags().String("template", "", "per-console category template ({prefix} {console} {console_id} {short} {title})")
	cmd.Flags().String("exe", "", "override cartridge binary path for shortcuts")
	return cmd
}

func flagPresent(cmd *cobra.Command, name string) (string, bool) {
	if !cmd.Flags().Changed(name) {
		return "", false
	}
	v, _ := cmd.Flags().GetString(name)
	return v, true
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// collSummary renders the collections part of a sync result line.
func collSummary(r steam.SyncResult) string {
	if r.CollSkipped {
		return dimStyle.Render("collections skipped (no cloudstorage — launch Steam once)")
	}
	if r.CollChanged > 0 && r.CollTotal == 0 {
		return fmt.Sprintf("collections %d retired", r.CollChanged)
	}
	if r.CollChanged > 0 {
		return fmt.Sprintf("collections %d changed / %d", r.CollChanged, r.CollTotal)
	}
	if r.CollTotal == 0 {
		return dimStyle.Render("collections: none")
	}
	return fmt.Sprintf("collections %d", r.CollTotal)
}

func q(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func dirOf(exe string) string {
	if i := strings.LastIndexAny(exe, `/\`); i >= 0 {
		if d := exe[:i]; d != "" {
			return d
		}
	}
	return "."
}
