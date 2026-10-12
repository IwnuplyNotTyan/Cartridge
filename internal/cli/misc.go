package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"cartridge/internal/library"
	"cartridge/internal/rom"
)

// printJSON marshals v indented to w.
func printJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(data))
	return nil
}

// reportScan performs a fresh scan and prints a summary.
// loadApp already refreshed+sved, so this re-scans to diff vs stored.
// prune/dryRun control whether missing entries are dropped / saved.
func reportScan(cmd *cobra.Command, dryRun, prune bool) error {
	o := globalOptsFromCmd(cmd)
	cfg, err := resolveConfig(o)
	if err != nil {
		return err
	}
	stored, err := library.Load(cfg.Library)
	if err != nil {
		return err
	}
	before := map[string]rom.Game{}
	for _, g := range stored {
		before[g.ID] = g
	}
	scanned, errs := rom.ScanDir(cfg.RomDir, stored)
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "%s\n", warn(err.Error()))
	}
	after := library.Merge(stored, scanned)
	if prune {
		keep := map[string]bool{}
		for _, g := range scanned {
			keep[g.ID] = true
		}
		var pruned []rom.Game
		for _, g := range after {
			if keep[g.ID] {
				pruned = append(pruned, g)
			}
		}
		after = pruned
	}
	if !dryRun {
		if err := library.Save(cfg.Library, after); err != nil {
			return err
		}
	}
	// Ghosts: library rows whose files are gone (kept by Merge unless
	// --prune). They still sync to Steam, so report them loudly.
	scannedSet := map[string]bool{}
	for _, g := range scanned {
		scannedSet[g.ID] = true
	}
	missing := 0
	for _, g := range after {
		if !scannedSet[g.ID] {
			missing++
		}
	}
	afterSet := map[string]bool{}
	for _, g := range after {
		afterSet[g.ID] = true
	}
	added := 0
	for _, g := range after {
		if _, exists := before[g.ID]; !exists {
			added++
		}
	}
	removed := 0
	for id := range before {
		if !afterSet[id] {
			removed++
		}
	}
	cprintf(cmd, "%s\n\n", banner("Scan", cfg.RomDir))
	cprintf(cmd, "  %s\n", kv("scanned", fmt.Sprintf("%d file(s) -> %d game(s)", len(scanned), len(after))))
	cprintf(cmd, "  %s\n", kv("added", fmt.Sprintf("%d", added)))
	cprintf(cmd, "  %s\n", kv("removed", fmt.Sprintf("%d", removed)))
	if missing > 0 {
		cprintf(cmd, "  %s\n", warn(fmt.Sprintf("%d game(s) in library whose files are gone (scan --prune to drop, steam prune to unlist from Steam)", missing)))
	}
	if dryRun {
		cprintf(cmd, "  %s\n", dimStyle.Render("(dry run — library not written)"))
	} else {
		cprintf(cmd, "  %s\n", kv("library", cfg.Library))
	}
	if added > 0 {
		cprintf(cmd, "\n%s\n", dimStyle.Render("New games:"))
		shown := 0
		for _, g := range after {
			if _, exists := before[g.ID]; !exists {
				cprintf(cmd, "  %s %-5s %s\n", ok("+"), consoleShort(g), g.Title)
				if shown++; shown >= 10 {
					cprintf(cmd, "  %s\n", dimStyle.Render(fmt.Sprintf("… and %d more", added-shown)))
					break
				}
			}
		}
	}
	return nil
}

type consoleRow struct {
	ID         string
	Name       string
	Short      string
	Extensions []string
}

// consolesWithUnknown returns known consoles plus an Unknown row when needed.
func consolesWithUnknown(games []rom.Game) []consoleRow {
	rows := make([]consoleRow, 0, len(rom.Consoles)+1)
	for _, c := range rom.Consoles {
		rows = append(rows, consoleRow{c.ID, c.Name, c.Short, c.Extensions})
	}
	needUnknown := false
	for _, g := range games {
		if rom.ByID(g.ConsoleID).ID == "unknown" || g.ConsoleID == "unknown" {
			needUnknown = true
			break
		}
	}
	if needUnknown {
		rows = append(rows, consoleRow{"unknown", "Unknown", "???", nil})
	}
	return rows
}

func formatCount(n int) string {
	if n == 1 {
		return "1 game"
	}
	return fmt.Sprintf("%d games", n)
}

func formatGames(n int) string {
	if n == 1 {
		return "1 game "
	}
	return fmt.Sprintf("%-7s", fmt.Sprintf("%d games", n))
}

func joinExt(exts []string) string {
	if len(exts) == 0 {
		return "—"
	}
	return strings.Join(exts, " ")
}

// --- small view-model helpers for favorites / stats ---

type favRow struct{ g rom.Game }

type favGame struct{ rom.Game }

type statRow struct {
	game     rom.Game
	sessions int
	total    string
	last     string
}

// resetPlaytime overwrites playtime.json next to the library with {}.
func resetPlaytime(libraryPath string) error {
	dir := filepath.Dir(libraryPath)
	if dir == "" || dir == "." {
		dir = "."
	}
	path := filepath.Join(dir, "playtime.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("{}\n"), 0o644)
}
