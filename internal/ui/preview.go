package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/i18n"
	"cartridge/internal/rom"
	"cartridge/internal/steam"
)

// Artwork preview: a collapsible cached icon/hero/logo section in the
// metadata pane (hidden by default, like cheats/mods). Enter on a file
// row hands it to the desktop default app (xdg-open on Linux).

// artOpenedMsg reports the desktop opener back to the model.
type artOpenedMsg struct {
	label string
	err   error
}

// openerBin names the desktop opener for the platform.
func openerBin() string {
	switch runtime.GOOS {
	case "darwin":
		return "open"
	case "windows":
		return "rundll32"
	default: // linux and the rest: freedesktop standard
		return "xdg-open"
	}
}

// openerCmd builds the opener invocation for path.
func openerCmd(path string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	}
	return exec.Command(openerBin(), path)
}

// previewArtCmd hands path to the desktop opener. It goes through
// tea.ExecProcess so bubbletea suspends the fullscreen TUI (restoring
// the terminal) around the spawn and resumes afterwards: a raw Start
// while the alt screen owns the tty never visibly opened anything
// under tmux/kitty. xdg-open exits as soon as the viewer takes over,
// so the resume is immediate; failures land in the status bar instead
// of a swallowed stderr.
func previewArtCmd(path, label string) tea.Cmd {
	if _, err := exec.LookPath(openerBin()); err != nil {
		return func() tea.Msg { return artOpenedMsg{label: label, err: err} }
	}
	return tea.ExecProcess(openerCmd(path), func(err error) tea.Msg {
		return artOpenedMsg{label: label, err: err}
	})
}

// artRows builds the artwork summary row plus one row per cached flavor
// while expanded. With no cached file the section hides entirely, like
// the cheats/mods sections do when there is nothing to show.
func (m Model) artRows(g rom.Game, vw int) []metaRow {
	flavors := []struct {
		key  string
		path string
	}{
		{"icon", steam.CachedIconPath(m.steam, g.ID)},
		{"hero", steam.CachedHeroPath(m.steam, g.ID)},
		{"logo", steam.CachedLogoPath(m.steam, g.ID)},
	}
	var files []metaRow
	for _, f := range flavors {
		if strings.TrimSpace(f.path) == "" {
			continue
		}
		files = append(files, metaRow{
			kind:  "art",
			label: i18n.T("meta.art." + f.key),
			lines: wrapSoft(filepath.Base(f.path), vw),
			copy:  f.path,
		})
	}
	if len(files) == 0 {
		return nil
	}
	base := fmt.Sprintf("%s (%d)", i18n.T("meta.art"), len(files))
	rows := []metaRow{{kind: "arts", label: i18n.T("meta.art"), lines: wrapSoft(base, vw), copy: base}}
	if m.artOpen {
		rows = append(rows, files...)
	}
	return rows
}

// openArtSelected hands the cursor artwork file to the desktop opener.
// A file gone since the rows were built (cache pruned under us) reports
// a miss instead of spawning the opener on a dead path.
func (m Model) openArtSelected() (tea.Model, tea.Cmd) {
	if m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return m, nil
	}
	row := m.metaRows[m.metaCur]
	if row.kind != "art" || strings.TrimSpace(row.copy) == "" {
		return m, nil
	}
	if st, err := os.Stat(row.copy); err != nil || st.IsDir() {
		m.refreshMeta()
		return m, m.setStatus(i18n.T("status.art.missing"))
	}
	return m, tea.Batch(previewArtCmd(row.copy, row.label), m.setStatus(i18n.T("status.art.opening", row.label)))
}
