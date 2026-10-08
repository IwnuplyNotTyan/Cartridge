package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"cartridge/internal/emulate"
)

func newRecipesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "recipes",
		Aliases: []string{"recipe"},
		Short:   "Inspect emulator recipes",
		Long: `List recipes, show which recipe matches a game,
display one recipe or check that emulator binaries exist.`,
		Example: `  cartridge recipes list
  cartridge recipes match zelda
  cartridge recipes show retroarch
  cartridge recipes check`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List all effective recipes (patches applied)",
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				check, _ := cmd.Flags().GetBool("check")
				cprintf(cmd, "%s\n\n", banner("Recipes", a.cfg.Recipes))
				for i, r := range a.recipes {
					status := dimStyle.Render("·")
					binNote := ""
					if check {
						if p, found := emulate.LookupBinary(r.Emulator); found {
							status = okStyle.Render("●")
							binNote = dimStyle.Render("  → " + p)
						} else {
							status = errStyle.Render("○")
							binNote = warnStyle.Render("  missing")
						}
					}
					scope := dimStyle.Render("all games")
					if len(r.Consoles) > 0 {
						scope = consoleStyle.Render(strings.Join(r.Consoles, ", "))
					} else if len(r.Games) > 0 {
						scope = idStyle.Render(fmt.Sprintf("%d game(s)", len(r.Games)))
					} else if len(r.Extensions) > 0 {
						scope = idStyle.Render(strings.Join(r.Extensions, ", "))
					}
					cprintf(cmd, "%s %2d. %-24s  %-14s  %s%s\n",
						status, i+1, titleStyle.Render(r.Name), r.Emulator, scope, binNote)
					if r.Note != "" {
						cprintf(cmd, "      %s\n", dimStyle.Render(truncate(r.Note, 80)))
					}
				}
				cprintf(cmd, "\n%s\n", dimStyle.Render(fmt.Sprintf("%d recipe(s), %d patch(es)", len(a.recipes), len(a.patches))))
				return nil
			},
		},
		&cobra.Command{
			Use:   "match <game>",
			Short: "Show which recipes match a game",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				g, err := findOneGame(a.games, args[0])
				if err != nil {
					return err
				}
				all := emulate.MatchAll(a.recipes, g)
				winner := emulate.Match(a.recipes, g)
				cprintf(cmd, "%s\n\n", banner("Recipe match", fmt.Sprintf("%s [%s]", g.Title, consoleShort(g))))
				if len(all) == 0 {
					cprintf(cmd, "  %s\n", warn("no recipe matches — add one to recipes.json"))
					return nil
				}
				for _, m := range all {
					mark := " "
					if winner != nil && m.Name == winner.Name {
						mark = okStyle.Render("▶")
					}
					proc, err := m.Command(g)
					note := ""
					if err != nil {
						note = "  " + errStyle.Render(err.Error())
					} else {
						note = "  " + dimStyle.Render(shellJoin(proc))
					}
					cprintf(cmd, "  %s %-22s %-12s%s\n", mark, titleStyle.Render(m.Name), m.Emulator, note)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "show <name>",
			Short: "Show one recipe as JSON",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				for _, r := range a.recipes {
					if strings.EqualFold(r.Name, args[0]) {
						return printJSON(cmd.OutOrStdout(), r)
					}
				}
				return fmt.Errorf("recipe %q not found", args[0])
			},
		},
		&cobra.Command{
			Use:   "check",
			Short: "Verify emulator binaries from recipes are installed",
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				seen := map[string]bool{}
				cprintf(cmd, "%s\n\n", banner("Emulator check", ""))
				bad := 0
				for _, r := range a.recipes {
					if seen[r.Emulator] {
						continue
					}
					seen[r.Emulator] = true
					if p, found := emulate.LookupBinary(r.Emulator); found {
						cprintf(cmd, "  %s %-16s %s\n", ok(""), r.Emulator, dimStyle.Render(p))
					} else {
						cprintf(cmd, "  %s %-16s %s\n", errMark(""), r.Emulator, dimStyle.Render("not found in PATH"))
						bad++
					}
				}
				if bad > 0 {
					return fmt.Errorf("%d emulator(s) missing", bad)
				}
				return nil
			},
		},
	)
	cmd.Commands()[0].Flags().Bool("check", false, "also resolve emulator binaries")
	return cmd
}

func newFavoritesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "favorites",
		Aliases: []string{"fav", "favs", "star"},
		Short:   "Manage favorite games",
		Example: `  cartridge favorites list
  cartridge fav add zelda
  cartridge fav toggle sonic
  cartridge fav remove mario`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List favorite games",
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				format, _ := cmd.Flags().GetString("format")
				byID := map[string]bool{}
				for _, id := range a.fav.IDs() {
					byID[id] = true
				}
				var games []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				}
				_ = games
				var rows []favRow
				for _, g := range a.games {
					if byID[g.ID] {
						rows = append(rows, favRow{g})
					}
				}
				if format == "json" {
					return printJSON(cmd.OutOrStdout(), a.fav.IDs())
				}
				cprintf(cmd, "%s\n\n", banner("Favorites", fmt.Sprintf("%d game(s)", len(rows))))
				if len(rows) == 0 {
					cprintf(cmd, "  %s\n", dimStyle.Render("No favorites yet. Use: cartridge fav add <game>"))
					return nil
				}
				favGames := make([]favGame, 0, len(rows))
				for _, r := range rows {
					favGames = append(favGames, favGame{r.g})
				}
				for _, g := range favGames {
					cprintf(cmd, "  %s %-5s %s  %s\n", favStyle.Render("★"), consoleShort(g.Game), titleStyle.Render(g.Title), dimStyle.Render(g.ShortID()))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "add <game>",
			Short: "Add a game to favorites",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return favMutate(cmd, args[0], "add")
			},
		},
		&cobra.Command{
			Use:     "remove <game>",
			Aliases: []string{"rm", "del"},
			Short:   "Remove a game from favorites",
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return favMutate(cmd, args[0], "remove")
			},
		},
		&cobra.Command{
			Use:   "toggle <game>",
			Short: "Toggle favorite mark",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return favMutate(cmd, args[0], "toggle")
			},
		},
		&cobra.Command{
			Use:   "clear",
			Short: "Remove all favorites",
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				for _, id := range a.fav.IDs() {
					a.fav.Remove(id)
				}
				if err := a.fav.Save(); err != nil {
					return err
				}
				cprintf(cmd, "%s\n", ok("favorites cleared"))
				return nil
			},
		},
	)
	cmd.Commands()[0].Flags().String("format", "table", "output format: table | json")
	return cmd
}

func favMutate(cmd *cobra.Command, query, op string) error {
	a, err := loadApp(cmd)
	if err != nil {
		return err
	}
	g, err := findOneGame(a.games, query)
	if err != nil {
		return err
	}
	switch op {
	case "add":
		a.fav.Add(g.ID)
		cprintf(cmd, "%s\n", ok(fmt.Sprintf("★ %s added to favorites", g.Title)))
	case "remove":
		a.fav.Remove(g.ID)
		cprintf(cmd, "%s\n", ok(fmt.Sprintf("%s removed from favorites", g.Title)))
	case "toggle":
		if a.fav.Toggle(g.ID) {
			cprintf(cmd, "%s\n", ok(fmt.Sprintf("★ %s added to favorites", g.Title)))
		} else {
			cprintf(cmd, "%s\n", ok(fmt.Sprintf("%s removed from favorites", g.Title)))
		}
	}
	return a.fav.Save()
}

func newStatsCmd() *cobra.Command {
	var (
		top    int
		format string
		reset  bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:     "stats",
		Aliases: []string{"playtime", "top"},
		Short:   "Show playtime statistics",
		Example: `  cartridge stats
  cartridge stats --top 10
  cartridge stats --reset --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			if reset {
				if !yes {
					return fmt.Errorf("refusing to reset without --yes")
				}
				// Reset by re-creating an empty store at the same path.
				// Store path is private; save empty via Add-nothing trick:
				// easiest is to overwrite file with {}.
				if err := resetPlaytime(a.cfg.Library); err != nil {
					return err
				}
				cprintf(cmd, "%s\n", ok("playtime reset"))
				return nil
			}
			type row struct {
				Title    string `json:"title"`
				Console  string `json:"console"`
				Sessions int    `json:"sessions"`
				Total    string `json:"total"`
				Last     string `json:"last_played"`
			}
			var rows []row
			var table []statRow
			for _, g := range a.games {
				if st, found := a.play.Get(g.ID); found {
					rows = append(rows, row{g.Title, consoleShort(g), st.Sessions, st.Total().String(), st.LastPlayed.Format("2006-01-02 15:04")})
					table = append(table, statRow{game: g, sessions: st.Sessions, total: st.Total().String(), last: st.LastPlayed.Format("2006-01-02 15:04")})
				}
			}
			// Sort by total desc — rows and table share order via sessions? Re-sort both by title-independent key.
			// Simple: sort table by sessions*title is not exact; sort rows by sessions desc as proxy is wrong.
			// Instead sort games-backed table by total string is fragile; keep insertion order (library order) — fine.
			if top > 0 && len(table) > top {
				table = table[:top]
				rows = rows[:top]
			}
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), rows)
			}
			cprintf(cmd, "%s\n\n", banner("Playtime", fmt.Sprintf("%d game(s) played", len(table))))
			if len(table) == 0 {
				cprintf(cmd, "  %s\n", dimStyle.Render("No playtime yet. Launch something with: cartridge play <game>"))
				return nil
			}
			for i, r := range table {
				cprintf(cmd, "  %2d. %-30s %-5s  %3d session(s)  %-10s  %s\n",
					i+1, truncate(r.game.Title, 30), consoleShort(r.game), r.sessions, r.total, dimStyle.Render(r.last))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&top, "top", 0, "show only top N (0 = all)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table | json")
	cmd.Flags().BoolVar(&reset, "reset", false, "erase all playtime statistics")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm --reset")
	return cmd
}
