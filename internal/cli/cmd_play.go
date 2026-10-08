package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"cartridge/internal/emulate"
	"cartridge/internal/rom"
)

func newPlayCmd() *cobra.Command {
	var (
		recipeName string
		core       string
		dryRun     bool
		noStats    bool
		saveChoice bool
	)
	cmd := &cobra.Command{
		Use:     "play <game>",
		Aliases: []string{"run", "launch", "start"},
		Short:   "Launch a game in its emulator",
		Long: `Find a game by title, short ID or full ID and launch it
with the matching emulator recipe.

Respects per-game overrides (set with --save or in the TUI).
Play time is recorded unless --no-stats is given.`,
		Example: `  cartridge play "super mario"
  cartridge play a1b2c3d4e5f6
  cartridge play zelda --recipe retroarch --dry-run
  cartridge play sonic --save   # remember recipe choice for this game`,
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
			r, err := pickRecipe(a, g.ID, recipeName)
			if err != nil {
				return err
			}
			// Core override: flag > saved choice > recipe default.
			corePath := strings.TrimSpace(core)
			if corePath == "" {
				if ch, ok := a.over.Get(g.ID); ok && strings.TrimSpace(ch.Core) != "" {
					corePath = ch.Core
				}
			}
			// Build the command. Core placeholder substitution happens
			// inside Recipe.Command via Cores map; an explicit --core is
			// honored by cloning the recipe with a single-core list.
			eff := *r
			if corePath != "" {
				if eff.Cores == nil {
					eff.Cores = map[string][]string{}
				}
				eff.Cores[g.ConsoleID] = []string{corePath}
			}
			proc, err := eff.Command(g)
			if err != nil {
				return fmt.Errorf("build command: %w", err)
			}
			if saveChoice && recipeName != "" {
				ch, _ := a.over.Get(g.ID)
				ch.Recipe = r.Name
				if corePath != "" {
					ch.Core = corePath
				}
				a.over.Set(g.ID, ch)
				if err := a.over.Save(); err != nil {
					return fmt.Errorf("save override: %w", err)
				}
				cprintf(cmd, "%s\n", ok(fmt.Sprintf("saved default: %s for %q", r.Name, g.Title)))
			}
			if dryRun {
				cprintf(cmd, "%s\n\n", banner("Dry run", g.Title))
				cprintf(cmd, "  %s\n", kv("recipe", r.Name))
				cprintf(cmd, "  %s\n", kv("emulator", eff.Emulator))
				if corePath != "" {
					cprintf(cmd, "  %s\n", kv("core", corePath))
				}
				cprintf(cmd, "  %s\n", kv("command", shellJoin(proc)))
				return nil
			}
			if _, ok := emulate.LookupBinary(proc.Path); !ok && !isPath(proc.Path) {
				fmt.Fprintf(os.Stderr, "%s\n", warn(fmt.Sprintf("emulator %q not found in PATH", proc.Path)))
			}
			cprintf(cmd, "▶ %s  %s\n", titleStyle.Render(g.Title), dimStyle.Render("["+r.Name+"]"))
			start := time.Now()
			proc.Stdin = os.Stdin
			proc.Stdout = os.Stdout
			proc.Stderr = os.Stderr
			runErr := proc.Run()
			elapsed := time.Since(start)
			if !noStats {
				a.play.Add(g.ID, elapsed)
				_ = a.play.Save()
			}
			if runErr != nil {
				return fmt.Errorf("emulator exited: %w", runErr)
			}
			cprintf(cmd, "%s\n", dimStyle.Render(fmt.Sprintf("played for %s", elapsed.Round(time.Second))))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&recipeName, "recipe", "r", "", "emulator recipe name (default: first match / override)")
	f.StringVar(&core, "core", "", "libretro core path (overrides recipe default)")
	f.BoolVar(&dryRun, "dry-run", false, "print the emulator command without launching")
	f.BoolVar(&noStats, "no-stats", false, "do not record playtime")
	f.BoolVar(&saveChoice, "save", false, "remember --recipe/--core as default for this game")
	return cmd
}

// pickRecipe resolves the recipe for a game: explicit name > override > match.
func pickRecipe(a *app, gameID, name string) (*emulate.Recipe, error) {
	if strings.TrimSpace(name) != "" {
		for i := range a.recipes {
			if strings.EqualFold(a.recipes[i].Name, strings.TrimSpace(name)) {
				return &a.recipes[i], nil
			}
		}
		return nil, fmt.Errorf("recipe %q not found", name)
	}
	game := rom.Game{}
	for _, g := range a.games {
		if g.ID == gameID {
			game = g
			break
		}
	}
	if ch, ok := a.over.Get(gameID); ok && strings.TrimSpace(ch.Recipe) != "" {
		for i := range a.recipes {
			if strings.EqualFold(a.recipes[i].Name, ch.Recipe) {
				return &a.recipes[i], nil
			}
		}
	}
	if r := emulate.Match(a.recipes, game); r != nil {
		return r, nil
	}
	return nil, fmt.Errorf("no recipe matches %q", game.Title)
}

func shellJoin(c *exec.Cmd) string {
	parts := append([]string{c.Path}, c.Args[1:]...)
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.ContainsAny(p, " \t\"'") {
			quoted = append(quoted, fmt.Sprintf("%q", p))
		} else {
			quoted = append(quoted, p)
		}
	}
	return strings.Join(quoted, " ")
}

func isPath(s string) bool { return strings.ContainsAny(s, `/\`) }

func newInfoCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:     "info <game>",
		Aliases: []string{"show"},
		Short:   "Show detailed info about a game",
		Example: `  cartridge info zelda
  cartridge info a1b2c3d4e5f6 --format json`,
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
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), g)
			}
			favMark := "no"
			if a.fav.Has(g.ID) {
				favMark = favStyle.Render("★ yes")
			}
			matches := emulate.MatchAll(a.recipes, g)
			names := make([]string, 0, len(matches))
			for _, m := range matches {
				names = append(names, m.Name)
			}
			winner := "—"
			if w := emulate.Match(a.recipes, g); w != nil {
				winner = w.Name
			}
			cprintf(cmd, "%s\n\n", banner(g.Title, g.ConsoleName))
			cprintf(cmd, "  %s\n", kv("id", g.ID))
			cprintf(cmd, "  %s\n", kv("short", g.ShortID()))
			cprintf(cmd, "  %s\n", kv("console", fmt.Sprintf("%s (%s)", g.ConsoleName, g.ConsoleID)))
			cprintf(cmd, "  %s\n", kv("path", g.Path))
			if g.Inner != "" {
				cprintf(cmd, "  %s\n", kv("inner", g.Inner))
			}
			cprintf(cmd, "  %s\n", kv("size", formatSize(g.Size)))
			if g.Region != "" {
				cprintf(cmd, "  %s\n", kv("region", g.Region))
			}
			if len(g.Tags) > 0 {
				cprintf(cmd, "  %s\n", kv("tags", strings.Join(g.Tags, ", ")))
			}
			cprintf(cmd, "  %s\n", kv("favorite", favMark))
			cprintf(cmd, "  %s\n", kv("recipe", winner))
			if len(names) > 1 {
				cprintf(cmd, "  %s\n", kv("also match", strings.Join(names[1:], ", ")))
			}
			if ch, ok := a.over.Get(g.ID); ok {
				cprintf(cmd, "  %s\n", kv("override", fmt.Sprintf("recipe=%s core=%s", ch.Recipe, ch.Core)))
			}
			if st, ok := a.play.Get(g.ID); ok {
				cprintf(cmd, "  %s\n", kv("playtime", fmt.Sprintf("%d session(s), %s, last %s", st.Sessions, st.Total(), st.LastPlayed.Format("2006-01-02 15:04"))))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table | json")
	return cmd
}
