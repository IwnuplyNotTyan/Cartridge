package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"cartridge/internal/cheats"
	"cartridge/internal/emulate"
	"cartridge/internal/mods"
)

func newModsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mods",
		Aliases: []string{"mod", "patches"},
		Short:   "Manage ROM mods / patch files",
		Example: `  cartridge mods list
  cartridge mods match zelda
  cartridge mods verify fix.ips --rom game.sfc`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List all mod files in the mods directory",
			RunE: func(cmd *cobra.Command, args []string) error {
				format, _ := cmd.Flags().GetString("format")
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				list, err := mods.ListDir(a.cfg.ModsDir)
				if err != nil {
					return err
				}
				if format == "json" {
					return printJSON(cmd.OutOrStdout(), list)
				}
				cprintf(cmd, "%s\n\n", banner("Mods", a.cfg.ModsDir))
				if len(list) == 0 {
					cprintf(cmd, "  %s\n", dimStyle.Render("No mods found. Drop .ips/.bps/.ups files into the mods dir."))
					return nil
				}
				for _, m := range list {
					cprintf(cmd, "  %-34s %-8s %s\n",
						titleStyle.Render(truncate(m.Name, 34)), idStyle.Render(m.Ext), dimStyle.Render(formatSize(m.Size)))
				}
				cprintf(cmd, "\n%s\n", dimStyle.Render(fmt.Sprintf("%d mod(s)", len(list))))
				return nil
			},
		},
		&cobra.Command{
			Use:   "match <game>",
			Short: "Find mods matching a game",
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
				// Candidate stems: filename stem + title.
				stem := g.Title
				matched, err := mods.Match(a.cfg.ModsDir, g.Path, stem)
				if err != nil {
					return err
				}
				cprintf(cmd, "%s\n\n", banner("Mods for "+g.Title, a.cfg.ModsDir))
				if len(matched) == 0 {
					cprintf(cmd, "  %s\n", dimStyle.Render("No matching mods."))
					return nil
				}
				for _, m := range matched {
					cprintf(cmd, "  %s %-8s %s\n", ok(""), m.Name, dimStyle.Render(formatSize(m.Size)))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "verify <patch>",
			Short: "Verify a patch file against a ROM",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				romPath, _ := cmd.Flags().GetString("rom")
				if strings.TrimSpace(romPath) == "" {
					return fmt.Errorf("flag --rom is required")
				}
				inner, _ := cmd.Flags().GetString("inner")
				romBytes, err := mods.ReadROMBytes(romPath, inner)
				if err != nil {
					return fmt.Errorf("read ROM: %w", err)
				}
				check := mods.VerifyPatch(args[0], romBytes)
				cprintf(cmd, "%s\n\n", banner("Patch verify", args[0]))
				if check.OK {
					cprintf(cmd, "  %s (%s)\n", ok("patch looks good"), check.Kind)
				} else {
					cprintf(cmd, "  %s (%s)\n", errMark("patch problem"), check.Kind)
				}
				if check.Note != "" {
					cprintf(cmd, "  %s\n", kv("note", check.Note))
				}
				if check.HasCRC {
					cprintf(cmd, "  %s\n", kv("crc", fmt.Sprintf("want %08x got %08x", check.WantCRC, check.GotCRC)))
				}
				if check.WantSize != 0 {
					cprintf(cmd, "  %s\n", kv("size", fmt.Sprintf("want %d got %d", check.WantSize, check.GotSize)))
				}
				if !check.OK {
					return fmt.Errorf("verification failed")
				}
				return nil
			},
		},
	)
	cmd.Commands()[0].Flags().String("format", "table", "output format: table | json")
	cmd.Commands()[2].Flags().String("rom", "", "ROM file to verify the patch against (required)")
	cmd.Commands()[2].Flags().String("inner", "", "entry inside zip, if the ROM is an archive")
	return cmd
}

func newCheatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cheats",
		Aliases: []string{"cheat", "cht"},
		Short:   "Find and inspect RetroArch cheat files",
		Example: `  cartridge cheats find zelda
  cartridge cheats show game.cht`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "find <game>",
			Short: "Locate the cheat file for a game",
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
				// Resolve via recipe template first, then shared dir.
				var dirs []string
				if r := emulate.Match(a.recipes, g); r != nil && r.Cheats.Supported && r.Cheats.Path != "" {
					if p, err := r.Expand(r.Cheats.Path, g); err == nil {
						dirs = append(dirs, p)
					}
				}
				dirs = append(dirs, a.cfg.CheatsDir)
				var found string
				var findErr error
				for _, d := range dirs {
					if f, err := cheats.Find(d, g.Path, g.Title); err == nil {
						found = f
						break
					} else {
						findErr = err
					}
				}
				if found == "" {
					if findErr != nil {
						return findErr
					}
					return fmt.Errorf("no cheat file for %q", g.Title)
				}
				cprintf(cmd, "%s\n", ok(found))
				if n, _ := cmd.Flags().GetBool("show"); n {
					return showCheatFile(cmd, found)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "show <file>",
			Short: "Print cheats from a .cht file",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return showCheatFile(cmd, args[0])
			},
		},
	)
	cmd.Commands()[0].Flags().Bool("show", false, "also print cheat entries")
	return cmd
}

func showCheatFile(cmd *cobra.Command, path string) error {
	f, err := cheats.Load(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cprintf(cmd, "%s\n\n", banner("Cheats", path))
	if len(f.Cheats) == 0 {
		cprintf(cmd, "  %s\n", dimStyle.Render("No cheats in file."))
		return nil
	}
	for i, c := range f.Cheats {
		cprintf(cmd, "  %2d. %-30s  %s\n", i+1, titleStyle.Render(truncate(c.Desc, 30)), idStyle.Render(truncate(c.Code, 40)))
	}
	return nil
}

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check environment, paths, recipes and emulators",
		Long: `Run a self-check: ROM and data directories, library file,
recipes/patches load, emulator binaries and cheat/mod dirs.`,
		Example: `  cartridge doctor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp(cmd)
			if err != nil {
				return err
			}
			cprintf(cmd, "%s\n\n", banner("Doctor", ""))
			fail := 0
			check := func(name, detail string, good bool) {
				if good {
					cprintf(cmd, "  %s %-12s %s\n", ok(""), name, dimStyle.Render(detail))
				} else {
					cprintf(cmd, "  %s %-12s %s\n", errMark(""), name, dimStyle.Render(detail))
					fail++
				}
			}
			st, err := os.Stat(a.cfg.RomDir)
			check("rom dir", a.cfg.RomDir, err == nil && st != nil && st.IsDir())
			check("library", fmt.Sprintf("%s (%d games)", a.cfg.Library, len(a.games)), true)
			check("recipes", fmt.Sprintf("%s (%d)", a.cfg.Recipes, len(a.recipes)), len(a.recipes) > 0 || a.cfg.Recipes == "")
			check("patches", fmt.Sprintf("%s (%d)", a.cfg.Patches, len(a.patches)), true)
			for _, d := range []struct{ name, path string }{
				{"mods", a.cfg.ModsDir}, {"cheats", a.cfg.CheatsDir}, {"patched", a.cfg.Patched},
			} {
				if err := os.MkdirAll(d.path, 0o755); err != nil {
					check(d.name, d.path+" ("+err.Error()+")", false)
				} else {
					check(d.name, d.path, true)
				}
			}
			seen := map[string]bool{}
			for _, r := range a.recipes {
				target := r.LaunchLabel(a.cfg.OpenApps)
				if seen[target] {
					continue
				}
				seen[target] = true
				_, found := r.LookupTarget(a.cfg.OpenApps)
				check("emulator", target, found)
			}
			if fail > 0 {
				return fmt.Errorf("doctor found %d problem(s)", fail)
			}
			cprintf(cmd, "\n  %s\n", okStyle.Render("All good ✓"))
			return nil
		},
	}
	return cmd
}
