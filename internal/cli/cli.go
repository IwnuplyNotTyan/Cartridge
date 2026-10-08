// Package cli implements the cartridge command line interface built on
// cobra (commands) and fang (pretty help / errors / version).
package cli

import (
	"context"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

var (
	// Version is the CLI version shown in --version and fang header.
	// Override with: go build -ldflags "-X cartridge/internal/cli.Version=x.y.z"
	Version = "dev"
	// Commit is the short git sha shown next to the version.
	Commit = "none"
)

// Execute builds the root command and runs it through fang.
func Execute() error {
	root := NewRootCmd()
	return fang.Execute(
		context.Background(),
		root,
		fang.WithVersion(Version),
		fang.WithCommit(Commit),
	)
}

// NewRootCmd builds the full command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cartridge",
		Short: "Retro-ROM shelf: library, recipes and emulator launcher",
		Long: `cartridge — retro-ROM shelf with a 3-column TUI and a full CLI.

Scans ROM directories, matches games to emulator recipes,
tracks favorites and playtime, manages mods and cheats.`,
		Example: `  cartridge                    # open the TUI shelf
  cartridge list --console gba    # list Game Boy Advance games
  cartridge play "zelda"          # fuzzy-find and launch a game
  cartridge scan --prune          # rescan ROMs, drop missing entries
  cartridge doctor                 # check emulators, paths and recipes`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if legacy, _ := cmd.Flags().GetBool("print"); legacy {
				return runPrint(cmd)
			}
			return runTUI(cmd, args)
		},
	}

	AddGlobalFlags(root)
	root.Flags().Bool("print", false, "print the library (SHORTID SYS TITLE PATH) and exit, without TUI")
	root.AddCommand(
		newTuiCmd(),
		newListCmd(),
		newScanCmd(),
		newPlayCmd(),
		newInfoCmd(),
		newConsolesCmd(),
		newRecipesCmd(),
		newFavoritesCmd(),
		newStatsCmd(),
		newConfigCmd(),
		newModsCmd(),
		newCheatsCmd(),
		newDoctorCmd(),
	)

	return root
}
