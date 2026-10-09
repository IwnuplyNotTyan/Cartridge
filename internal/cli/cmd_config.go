package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"cartridge/internal/config"
	"cartridge/internal/i18n"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"cfg"},
		Short:   "Show and manage configuration",
		Example: `  cartridge config show
  cartridge config paths
  cartridge config save
  cartridge config init`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show resolved configuration",
			RunE: func(cmd *cobra.Command, args []string) error {
				format, _ := cmd.Flags().GetString("format")
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				if format == "json" {
					return printJSON(cmd.OutOrStdout(), a.cfg.ToFile())
				}
				c := a.cfg
				cprintf(cmd, "%s\n\n", banner("Config", c.SavePath()))
				cprintf(cmd, "  %s\n", kv("rom dir", c.RomDir))
				cprintf(cmd, "  %s\n", kv("library", c.Library))
				cprintf(cmd, "  %s\n", kv("language", fmt.Sprintf("%s (%s)", i18n.Name(c.Lang), string(c.Lang))))
				cprintf(cmd, "  %s\n", kv("recipes", c.Recipes))
				cprintf(cmd, "  %s\n", kv("patches", c.Patches))
				cprintf(cmd, "  %s\n", kv("mods", c.ModsDir))
				cprintf(cmd, "  %s\n", kv("patched", c.Patched))
				cprintf(cmd, "  %s\n", kv("saves", c.SavesDir))
				cprintf(cmd, "  %s\n", kv("cheats", c.CheatsDir))
				cprintf(cmd, "  %s\n", kv("steam auto-add", onOff(c.Steam.AutoAdd)))
				cprintf(cmd, "  %s\n", kv("steam prefix", q(c.Steam.EffectivePrefix())))
				cprintf(cmd, "  %s\n", kv("steam all-games", onOff(c.Steam.AllGames)))
				cprintf(cmd, "  %s\n", kv("steam template", c.Steam.EffectiveTemplate()))
				if n := len(c.Steam.Categories); n > 0 {
					cprintf(cmd, "  %s\n", kv("steam custom names", fmt.Sprintf("%d", n)))
				}
				if c.ConfigFile != "" {
					cprintf(cmd, "  %s\n", kv("file", c.ConfigFile))
				} else {
					cprintf(cmd, "  %s\n", dimStyle.Render("(no config file — using defaults)"))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "paths",
			Short: "Show only resolved paths",
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				c := a.cfg
				for _, p := range [][2]string{
					{"ROM", c.RomDir}, {"library", c.Library}, {"recipes", c.Recipes},
					{"patches", c.Patches}, {"mods", c.ModsDir}, {"patched", c.Patched},
					{"saves", c.SavesDir}, {"cheats", c.CheatsDir},
				} {
					cprintf(cmd, "%-8s %s\n", p[0], p[1])
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "save",
			Short: "Persist resolved configuration to the config file",
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := loadApp(cmd)
				if err != nil {
					return err
				}
				if err := a.cfg.Save(); err != nil {
					return err
				}
				cprintf(cmd, "%s\n", ok("saved to "+a.cfg.SavePath()))
				return nil
			},
		},
		&cobra.Command{
			Use:   "init",
			Short: "Create a default config file with current paths",
			RunE: func(cmd *cobra.Command, args []string) error {
				force, _ := cmd.Flags().GetBool("force")
				o := globalOptsFromCmd(cmd)
				cfg, err := resolveConfig(o)
				if err != nil {
					return err
				}
				path := cfg.SavePath()
				if _, err := os.Stat(path); err == nil && !force {
					return fmt.Errorf("config already exists: %s (use --force to overwrite)", path)
				}
				if err := config.SaveFile(path, cfg.ToFile()); err != nil {
					return err
				}
				cprintf(cmd, "%s\n", ok("initialized "+path))
				return nil
			},
		},
	)
	cmd.Commands()[0].Flags().String("format", "table", "output format: table | json")
	cmd.Commands()[3].Flags().Bool("force", false, "overwrite existing config file")
	return cmd
}
