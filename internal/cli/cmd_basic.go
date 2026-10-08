package cli

import (
	"github.com/spf13/cobra"
)

func newTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "tui",
		Aliases: []string{"open", "shelf", "ui"},
		Short:   "Open the interactive 3-column shelf (default)",
		Long: `Open the full-screen TUI: consoles | games | metadata plus a bottom bar.

This is the default when cartridge is run without a subcommand.`,
		Example: `  cartridge tui
  cartridge open`,
		RunE: runTUI,
	}
}

func newListCmd() *cobra.Command {
	var (
		console  string
		search   string
		favOnly  bool
		limit    int
		format   string
		sortKey  string
		noRescan bool
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "games"},
		Short:   "List games in the library",
		Long: `List games with console, favorite and search filters.

Refreshes the library from disk first (unless --no-rescan),
so the output always matches the ROM directory.`,
		Example: `  cartridge list
  cartridge list --console gba --sort title
  cartridge ls -c snes -s zelda --format json
  cartridge ls --favorites --limit 20`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			if noRescan {
				// loadApp already rescanned; flag kept for API stability.
				_ = noRescan
			}
			games := filterGames(a.games, console, search, favOnly, a.fav)
			sortGames(games, sortKey)
			if limit > 0 && len(games) > limit {
				games = games[:limit]
			}
			switch format {
			case "json":
				return printJSON(cmd.OutOrStdout(), games)
			case "plain":
				for _, g := range games {
					cprintf(cmd, "%s  %-4s  %-50s  %s\n", g.ShortID(), consoleShort(g), g.Title, g.Path)
				}
				return nil
			default: // table
				cprintf(cmd, "%s\n\n", banner("Library", a.cfg.RomDir))
				printGameTable(cmd.OutOrStdout(), games, a.fav, a.cfg.RomDir)
				return nil
			}
		},
	}
	f := cmd.Flags()
	f.StringVarP(&console, "console", "c", "", "filter by console id, short tag or name (e.g. gba, SNES)")
	f.StringVarP(&search, "search", "s", "", "fuzzy search in title / path / console")
	f.BoolVar(&favOnly, "favorites", false, "show favorites only")
	f.IntVarP(&limit, "limit", "n", 0, "max games to show (0 = all)")
	f.StringVar(&format, "format", "table", "output format: table | json | plain")
	f.StringVar(&sortKey, "sort", "title", "sort by: title | console | size | path")
	f.BoolVar(&noRescan, "no-rescan", false, "skip library refresh (reserved, refresh is cheap)")
	return cmd
}

func newScanCmd() *cobra.Command {
	var (
		prune  bool
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:     "scan",
		Aliases: []string{"rescan", "refresh"},
		Short:   "Rescan the ROM directory and update the library",
		Long: `Walk the ROM directory, identify new games, merge them into the
library database and report what changed.

By default entries for ROMs that disappeared from disk are kept
(same as the TUI startup). Use --prune to drop them.`,
		Example: `  cartridge scan
  cartridge scan --prune
  cartridge scan --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			_ = a // loadApp already scanned+saved; recompute diff for report
			return reportScan(cmd, dryRun, prune)
		},
	}
	cmd.Flags().BoolVar(&prune, "prune", false, "drop library entries whose files are gone")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "scan without writing the library")
	return cmd
}

func newConsolesCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:     "consoles",
		Aliases: []string{"systems"},
		Short:   "List known consoles with game counts",
		Example: `  cartridge consoles
  cartridge consoles --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			type row struct {
				ID         string   `json:"id"`
				Name       string   `json:"name"`
				Short      string   `json:"short"`
				Count      int      `json:"count"`
				Extensions []string `json:"extensions"`
			}
			counts := map[string]int{}
			for _, g := range a.games {
				counts[g.ConsoleID]++
			}
			var rows []row
			for _, c := range consolesWithUnknown(a.games) {
				rows = append(rows, row{c.ID, c.Name, c.Short, counts[c.ID], c.Extensions})
			}
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), rows)
			}
			cprintf(cmd, "%s\n\n", banner("Consoles", formatCount(len(a.games))))
			for _, r := range rows {
				mark := dimStyle.Render("·")
				if r.Count > 0 {
					mark = okStyle.Render("●")
				}
				cprintf(cmd, "%s %-28s %s  %s  %s\n",
					mark,
					consoleStyle.Render(r.Short+"  "+r.Name),
					keyStyle.Render(formatGames(r.Count)),
					idStyle.Render(r.ID),
					dimStyle.Render(joinExt(r.Extensions)),
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table | json")
	return cmd
}
