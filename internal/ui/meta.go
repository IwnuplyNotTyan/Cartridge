package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
	"cartridge/internal/mods"
	"cartridge/internal/overrides"
	"cartridge/internal/rom"
)

// Metadata pane: recipe/core/cheats/mods rows and actions.
func (m *Model) refreshMeta() {
	m.syncCheatCache()
	m.syncModCache()
	m.metaRows = m.buildMetaRows()
	m.metaCur = clampMetaCur(m.metaRows, m.metaCur)
	content, starts := m.renderMetaRows()
	m.meta.SetContent(content)
	m.metaFollowCursor(starts, strings.Count(content, "\n"))
}

// clampMetaCur keeps the cursor on a selectable row.
func clampMetaCur(rows []metaRow, cur int) int {
	if len(rows) == 0 {
		return 0
	}
	if cur >= 0 && cur < len(rows) && !rows[cur].sep {
		return cur
	}
	for i := range rows {
		if !rows[i].sep {
			return i
		}
	}
	return 0
}

// metaFollowCursor scrolls the viewport so the cursor row stays visible.
func (m *Model) metaFollowCursor(starts []int, total int) {
	if len(m.metaRows) == 0 || len(starts) == 0 {
		m.meta.SetYOffset(0)
		return
	}
	cur := clamp(m.metaCur, 0, len(starts)-1)
	start := starts[cur]
	end := total - 1
	if cur+1 < len(starts) {
		end = starts[cur+1] - 1
	}
	h := max(m.meta.Height, 1)
	switch {
	case start < m.meta.YOffset:
		m.meta.SetYOffset(start)
	case end >= m.meta.YOffset+h:
		m.meta.SetYOffset(end - h + 1)
	}
}

// gameRecipe returns the effective recipe for g: a manually picked
// alternative when set and still matching, otherwise the hierarchy winner.
func (m Model) gameRecipe(g rom.Game) *emulate.Recipe {
	alts := emulate.MatchAll(m.recipes, g)
	if len(alts) == 0 {
		return nil
	}
	if c, ok := m.overrides.Get(g.ID); ok {
		for _, r := range alts {
			if r.Name == c.Recipe {
				return r
			}
		}
	}
	return alts[0]
}

// gameCore returns the selected core for g under r: a manually picked one
// when set and still listed, otherwise the recipe default.
// The list and the resolved index follow.
func (m Model) gameCore(g rom.Game, r *emulate.Recipe) (string, []string, int) {
	cs := r.CoresFor(g.ConsoleID)
	if len(cs) == 0 {
		return "", nil, 0
	}
	if c, ok := m.overrides.Get(g.ID); ok {
		for i, core := range cs {
			if core == c.Core {
				return core, cs, i
			}
		}
	}
	return cs[0], cs, 0
}

// launchCmd builds the emulator process, honoring a manually picked core.
func (m Model) launchCmd(game rom.Game, r *emulate.Recipe) (*exec.Cmd, error) {
	if c, ok := m.overrides.Get(game.ID); ok && c.Core != "" {
		if cs := r.CoresFor(game.ConsoleID); len(cs) > 0 {
			core := cs[0]
			for _, cand := range cs {
				if cand == c.Core {
					core = cand
					break
				}
			}
			rc := *r
			rc.Cores = map[string][]string{game.ConsoleID: {core}}
			return rc.Command(game)
		}
	}
	return r.Command(game)
}

// metaEdge jumps to the first (last=false) or last selectable row.
func (m *Model) metaEdge(last bool) {
	if len(m.metaRows) == 0 {
		return
	}
	if last {
		m.metaCur = len(m.metaRows) - 1
		for m.metaCur > 0 && m.metaRows[m.metaCur].sep {
			m.metaCur--
		}
	} else {
		m.metaCur = clampMetaCur(m.metaRows, 0)
	}
	m.refreshMeta()
}

// metaMove shifts the metadata cursor, skipping separators.
func (m *Model) metaMove(d int) {
	if len(m.metaRows) == 0 {
		return
	}
	i := m.metaCur
	for {
		i += d
		if i < 0 {
			i = 0
			break
		}
		if i >= len(m.metaRows) {
			i = len(m.metaRows) - 1
			break
		}
		if !m.metaRows[i].sep {
			break
		}
	}
	m.metaCur = i
	m.refreshMeta()
}

// metaCopy copies the cursor row value to the terminal clipboard (OSC52).
func (m Model) metaCopy() (tea.Model, tea.Cmd) {
	if m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return m, nil
	}
	row := m.metaRows[m.metaCur]
	if row.sep || strings.TrimSpace(row.copy) == "" {
		return m, m.setStatus(i18n.T("status.copy_empty"))
	}
	what := row.label
	if what == "" {
		what = row.copy
	}
	return m, tea.Batch(copyCmd(row.copy), m.setStatus(i18n.T("status.copied", what)))
}

// metaSelect activates the cursor row: expands the cheat list, toggles
// a cheat, deploys a mod file beside the ROM, copies anything else.
// It returns a Model value like the other update paths, even though it
// mutates through a pointer.
func (m *Model) metaSelect() (tea.Model, tea.Cmd) {
	if m.metaCur >= 0 && m.metaCur < len(m.metaRows) {
		switch m.metaRows[m.metaCur].kind {
		case "arts":
			m.artOpen = !m.artOpen
			m.refreshMeta()
			return *m, nil
		case "art", "poster":
			return m.openArtSelected()
		case "mod":
			return m.launchModSelected()
		case "mods":
			m.modsOpen = !m.modsOpen
			m.refreshMeta()
			return *m, nil
		case "cheats":
			if m.cheatsOpen {
				m.cheatsOpen = false
				m.refreshMeta()
				return *m, nil
			}
			// Opening: materialize a missing file first (pile copy or
			// empty skeleton), then make sure everything is enabled.
			if m.cheatFile == nil {
				if !m.cheatMissing || m.cheatPath == "" {
					break // bad file or nowhere to create: copy instead
				}
				if err := m.materializeCheats(); err != nil {
					return *m, m.setStatus(i18n.T("status.save_fail", err))
				}
			} else {
				if cmd := m.ensureCheatsOn(); cmd != nil {
					m.cheatsOpen = true
					m.refreshMeta()
					return *m, cmd
				}
			}
			m.cheatsOpen = true
			m.refreshMeta()
			return *m, nil
		}
	}
	return m.metaCopy()
}

// launchModSelected builds the hard-patched ROM instance for the cursor
// mod file (reusing a fresh one) and starts the emulator on it, exactly
// like a vanilla launch but with the copied game file. Works with any
// emulator or core: no softpatch support needed on their side.
func (m Model) launchModSelected() (tea.Model, tea.Cmd) {
	if m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return m, nil
	}
	row := m.metaRows[m.metaCur]
	if row.kind != "mod" || strings.TrimSpace(row.copy) == "" {
		return m, nil
	}
	src := row.copy
	g := m.selected()
	if g == nil {
		return m, nil
	}
	game := *g
	r := m.gameRecipe(game)
	if r == nil {
		return m, m.setStatus(i18n.T("status.no_recipe", game.Title))
	}
	instance, _, err := mods.EnsureInstance(m.patchedDir, game.ID, game.Path, game.Inner, src)
	if err != nil {
		return m, m.setStatus(i18n.T("status.launch_fail", err))
	}
	game.Path = instance
	game.Inner = ""
	cmd, err := m.launchCmd(game, r)
	if err != nil {
		return m, m.setStatus(i18n.T("status.launch_fail", err))
	}
	res, runCmd := m.startRun(game, cmd)
	if mm, ok := res.(Model); ok && mm.run != nil && mm.run.gameID == game.ID && !mm.run.done {
		mm.modKey = "" // a fresh instance changes the [built] marks
		mm.refreshMeta()
		return mm, tea.Batch(runCmd, mm.setStatus(i18n.T("status.mod.launching", game.Title, filepath.Base(src))))
	}
	return res, runCmd
}

// toggleSoftpatch places the cursor mod file beside the ROM under the
// exact name RetroArch autopatches by, or removes that copy when it is
// already there. A deployed file with foreign bytes (hand-placed, not
// ours) is reported and kept.
func (m *Model) toggleSoftpatch() tea.Cmd {
	if m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return nil
	}
	row := m.metaRows[m.metaCur]
	if row.kind != "mod" || strings.TrimSpace(row.copy) == "" {
		return nil
	}
	src := row.copy
	g := m.selected()
	if g == nil {
		return nil
	}
	// Plan fresh at action time: the cache may predate a hand-placed
	// file beside the ROM, and deploying over it would destroy foreign
	// bytes. PlanDeploy never picks an occupied slot with different
	// content, stacking to .ips1 instead.
	ext := strings.ToLower(filepath.Ext(src))
	dst, active := mods.PlanDeploy(src, g.Path, g.Inner, ext)
	if !active {
		chk := m.modCheck[src]
		if err := mods.Deploy(src, dst); err != nil {
			return m.setStatus(i18n.T("status.save_fail", err))
		}
		m.modKey = "" // deployed bytes sit outside the watched dirs: recollect
		m.refreshMeta()
		msg := i18n.T("status.mod.deployed", filepath.Base(dst))
		if d := modCheckDetail(*g, chk); d != "" {
			msg += " - " + d
		}
		return m.setStatus(msg)
	}
	if err := mods.Undeploy(src, dst); err != nil {
		if errors.Is(err, mods.ErrNotOurs) {
			return m.setStatus(i18n.T("status.mod.not_ours", filepath.Base(dst)))
		}
		return m.setStatus(i18n.T("status.save_fail", err))
	}
	m.modKey = ""
	m.refreshMeta()
	return m.setStatus(i18n.T("status.mod.removed", filepath.Base(dst)))
}

// toggleFavorite flips the favorite mark on the selected game and
// persists it. The shelf rebuilds in place with the cursor kept on the
// same game, which just moves sections.
func (m *Model) toggleFavorite() tea.Cmd {
	g := m.selected()
	if g == nil || m.favorites == nil {
		return nil
	}
	id := g.ID
	var note string
	if m.favorites.Toggle(id) {
		note = i18n.T("status.fav.added", g.Title)
	} else {
		note = i18n.T("status.fav.removed", g.Title)
	}
	if err := m.favorites.Save(); err != nil {
		return m.setStatus(i18n.T("status.save_fail", err))
	}
	m.refilter()
	for i := range m.filtered {
		if m.filtered[i].ID == id {
			m.gameCur = i
			break
		}
	}
	m.refreshMeta()
	return m.setStatus(note)
}

// copyCmd writes text to the terminal clipboard via OSC52 (works over SSH;
// tmux needs `set-clipboard on` or passthrough).
func copyCmd(s string) tea.Cmd {
	return func() tea.Msg {
		seq := osc52.New(s)
		if os.Getenv("TMUX") != "" {
			seq = seq.Tmux()
		}
		fmt.Fprint(os.Stdout, seq)
		return nil
	}
}

// cycleAlt switches the cursor row option: another matching emulator on
// the recipe row, another core on the core row. n moves forward, p back.
func (m *Model) cycleAlt(next bool) tea.Cmd {
	g := m.selected()
	if g == nil || m.metaCur < 0 || m.metaCur >= len(m.metaRows) {
		return nil
	}
	// saveChoice persists the per-game choice; on failure the status
	// reports it and the meta view still reflects the in-memory pick.
	save := func(ch overrides.Choice) tea.Cmd {
		m.overrides.Set(g.ID, ch)
		if err := m.overrides.Save(); err != nil {
			return m.setStatus(i18n.T("status.save_fail", err))
		}
		m.clearStatus()
		m.refreshMeta()
		return nil
	}
	switch m.metaRows[m.metaCur].kind {
	case "recipe":
		alts := emulate.MatchAll(m.recipes, *g)
		if len(alts) < 2 {
			return nil
		}
		cur := 0
		if c, ok := m.overrides.Get(g.ID); ok {
			for i, r := range alts {
				if r.Name == c.Recipe {
					cur = i
					break
				}
			}
		}
		if next {
			cur = (cur + 1) % len(alts)
		} else {
			cur = (cur - 1 + len(alts)) % len(alts)
		}
		ch, _ := m.overrides.Get(g.ID)
		ch.Recipe = alts[cur].Name
		return save(ch)
	case "core":
		r := m.gameRecipe(*g)
		if r == nil {
			return nil
		}
		cs := r.CoresFor(g.ConsoleID)
		if len(cs) < 2 {
			return nil
		}
		_, _, cur := m.gameCore(*g, r)
		if next {
			cur = (cur + 1) % len(cs)
		} else {
			cur = (cur - 1 + len(cs)) % len(cs)
		}
		ch, _ := m.overrides.Get(g.ID)
		ch.Core = cs[cur]
		return save(ch)
	}
	return nil
}

// buildMetaRows collects the navigable metadata rows for the selected game.
// Every row carries its clipboard text; recipe/core rows are switchable.
func (m Model) buildMetaRows() []metaRow {
	g := m.selected()
	if g == nil {
		return nil
	}
	w := max(m.meta.Width, 20)
	vw := max(w-metaKeyWidth, 10)
	kv := func(label, v string) metaRow {
		return metaRow{kind: "kv", label: label, lines: wrapSoft(v, vw), copy: v}
	}
	var rows []metaRow
	rows = append(rows, metaRow{kind: "title", lines: wrapSoft(g.Title, w), copy: g.Title})
	rows = append(rows, m.posterRows(*g, w)...)
	rows = append(rows, metaRow{sep: true})
	rows = append(rows, metaRow{kind: "id", label: i18n.T("meta.id"), lines: []string{g.ShortID()}, copy: g.ID})
	rows = append(rows, metaRow{kind: "hash", lines: wrapSoft(g.ID, max(w-4, 10)), copy: g.ID})
	con := fmt.Sprintf("%s (%s)", g.ConsoleName, g.ConsoleID)
	rows = append(rows, kv(i18n.T("meta.console"), con))
	if g.Region != "" {
		rows = append(rows, kv(i18n.T("meta.region"), g.Region))
	}
	if len(g.Tags) > 0 {
		rows = append(rows, kv(i18n.T("meta.tags"), strings.Join(g.Tags, ", ")))
	}
	rows = append(rows, kv(i18n.T("meta.size"), i18n.T("meta.size.fmt", g.Size, pluralWord(int(g.Size), "word.byte"), float64(g.Size)/(1<<20))))
	rows = append(rows, metaRow{sep: true})
	rows = append(rows, kv(i18n.T("meta.file"), g.Path))
	if g.Inner != "" {
		rows = append(rows, kv(i18n.T("meta.inner"), g.Inner))
	}
	rows = append(rows, m.artRows(*g, vw)...)
	rows = append(rows, metaRow{sep: true})
	if r := m.gameRecipe(*g); r != nil {
		alts := emulate.MatchAll(m.recipes, *g)
		disp := altLabel(r.Name, alts)
		rows = append(rows, metaRow{kind: "recipe", label: i18n.T("meta.recipe"), lines: wrapSoft(disp, vw), copy: r.Name})
		if core, cs, idx := m.gameCore(*g, r); len(cs) > 0 {
			cval := core
			if len(cs) > 1 {
				cval = fmt.Sprintf("%s (%d/%d)", core, idx+1, len(cs))
			}
			rows = append(rows, metaRow{kind: "core", label: i18n.T("meta.core"), lines: wrapSoft(cval, vw), copy: core})
		}
		rows = append(rows, m.cheatsRows(*g, *r, vw)...)
		rows = append(rows, m.modsRows(*g, *r, vw)...)
	} else {
		rows = append(rows, metaRow{kind: "kv", label: i18n.T("meta.recipe"), lines: wrapSoft(i18n.T("recipe.none"), vw)})
	}
	if m.play != nil {
		if st, ok := m.play.Get(g.ID); ok {
			played := i18n.T("meta.played.fmt",
				i18n.Duration(st.Total()), st.Sessions,
				pluralWord(st.Sessions, "word.session"),
				st.LastPlayed.Format("2006-01-02 15:04"))
			rows = append(rows, kv(i18n.T("meta.played"), played))
		}
	}
	return rows
}

// cheatsRows builds the cheats summary row (with a bad-file mark)
// plus one row per cheat while the list is expanded. Every listed cheat
// is available at once; there are no per-cheat choices. When no cheat
// file is detected the section is hidden entirely.
func (m Model) cheatsRows(g rom.Game, r emulate.Recipe, vw int) []metaRow {
	if !r.Cheats.Supported || m.cheatMissing || m.cheatPath == "" {
		return nil
	}
	base := featText(r.Cheats.Supported, r.Cheats.Path, g, &r)
	if m.cheatFile == nil {
		base += " (" + i18n.T("meta.cheats.bad") + ")"
	}
	rows := []metaRow{{kind: "cheats", label: i18n.T("meta.cheats"), lines: wrapSoft(base, vw), copy: base}}
	if m.cheatsOpen && m.cheatFile != nil {
		for i, c := range m.cheatFile.Cheats {
			name := strings.TrimSpace(c.Desc)
			if name == "" {
				name = strings.TrimSpace(c.Code)
			}
			rows = append(rows, metaRow{kind: "cheat", lines: wrapSoft(name, vw), copy: c.Code, cheat: i})
		}
	}
	return rows
}

// modsRows builds the mods summary row plus one row per patch file while
// expanded. Each file shows its pile name and the exact deployed name
// RetroArch autopatches by (game stem + ext), plus an [active] mark when
// those bytes already sit beside the ROM. Enter deploys, d removes.
// When no mod files are found the section is hidden entirely.
func (m Model) modsRows(g rom.Game, r emulate.Recipe, vw int) []metaRow {
	if !r.Mods.Supported || len(m.modFiles) == 0 {
		return nil
	}
	base := featText(r.Mods.Supported, r.Mods.Path, g, &r)
	base += fmt.Sprintf(" (%d)", len(m.modFiles))
	rows := []metaRow{{kind: "mods", label: i18n.T("meta.mods"), lines: wrapSoft(base, vw), copy: base}}
	if m.modsOpen {
		for i, f := range m.modFiles {
			text := f.Name + " → " + filepath.Base(m.modTarget[f.Path])
			if m.modActive[f.Path] {
				text += " [" + i18n.T("meta.mods.active") + "]"
			}
			if m.modBuilt[f.Path] {
				text += " [" + i18n.T("meta.mods.built") + "]"
			}
			if chk, ok := m.modCheck[f.Path]; ok {
				switch chk.Kind {
				case "base":
					text += " [" + i18n.T("meta.mods.badbase") + "]"
				case "broken":
					text += " [" + i18n.T("meta.mods.badfile") + "]"
				}
			}
			lines := wrapSoft(text, vw)
			if d := modCheckDetail(g, m.modCheck[f.Path]); d != "" {
				lines = append(lines, wrapSoft(d, vw)...)
			}
			rows = append(rows, metaRow{kind: "mod", lines: lines, copy: f.Path, cheat: i})
		}
	}
	return rows
}

// modCheckDetail renders the second row line for a failed verification:
// the technical mismatch plus an SMC-header hint on SNES ±512 bytes.
func modCheckDetail(g rom.Game, chk mods.Check) string {
	switch chk.Kind {
	case "broken":
		return chk.Note
	case "base":
		s := chk.Note
		if g.ConsoleID == "snes" && chk.HasCRC {
			if d := chk.WantSize - chk.GotSize; d == 512 || d == -512 {
				s += " · " + i18n.T("meta.mods.header")
			}
		}
		return s
	default:
		return ""
	}
}

// altLabel names the active alternative among all matches: plain name when
// alone, otherwise "name (i/n)" so switching stays discoverable.
func altLabel(name string, alts []*emulate.Recipe) string {
	if len(alts) < 2 {
		return name
	}
	for i, r := range alts {
		if r.Name == name {
			return fmt.Sprintf("%s (%d/%d)", name, i+1, len(alts))
		}
	}
	return name
}

// renderMetaRows renders rows within viewport width and reports the visual
// starting line of every row for cursor following. Values are soft-wrapped
// (never hard-truncated with "…"); the cursor row is highlighted instead.
func (m Model) renderMetaRows() (string, []int) {
	rows := m.metaRows
	if len(rows) == 0 {
		return infoStyle.Render(i18n.T("meta.none")), nil
	}
	w := max(m.meta.Width, 20)
	var b strings.Builder
	starts := make([]int, len(rows))
	n := 0
	padLabel := func(label string) string {
		if pad := metaKeyWidth - lipgloss.Width(label); pad > 0 {
			return label + strings.Repeat(" ", pad)
		}
		return label + " "
	}
	for i, r := range rows {
		starts[i] = n
		if r.sep {
			b.WriteString("\n")
			n++
			continue
		}
		// Poster preview: raw half-block lines (ANSI truecolor) need
		// no wrapping or truncation — truncate() would cut escape
		// sequences. Selection shows only the left accent bar so the
		// image colors stay intact (no selStyle background over art).
		if r.kind == "poster" {
			if i == m.metaCur && m.focus == PaneMeta {
				for _, ln := range r.lines {
					b.WriteString(curStyle.Render("│ ") + ln + "\n")
					n++
				}
			} else {
				for _, ln := range r.lines {
					b.WriteString("  " + ln + "\n")
					n++
				}
			}
			continue
		}
		if i == m.metaCur && m.focus == PaneMeta {
			for j, ln := range r.lines {
				var plain string
				if j == 0 && r.label != "" {
					plain = fit("  "+padLabel(r.label)+truncate(ln, max(w-2-metaKeyWidth-1, 1)), w)
				} else if j == 0 {
					plain = fit("  "+truncate(ln, w-2), w)
				} else {
					plain = fit("    "+truncate(ln, w-4), w)
				}
				b.WriteString(styledBarRow(plain) + "\n")
				n++
			}
			continue
		}
		switch r.kind {
		case "title":
			for _, ln := range r.lines {
				b.WriteString(metaVal.Render(ln) + "\n")
				n++
			}
		case "id":
			b.WriteString(metaKey.Render(r.label) + metaID.Render(r.lines[0]) + "\n")
			n++
		case "hash":
			for _, ln := range r.lines {
				b.WriteString(dim("    "+ln) + "\n")
				n++
			}
		default:
			for j, ln := range r.lines {
				if j == 0 {
					b.WriteString(metaKey.Render(r.label) + metaVal.Render(ln) + "\n")
				} else {
					b.WriteString(strings.Repeat(" ", metaKeyWidth) + metaVal.Render(ln) + "\n")
				}
				n++
			}
		}
	}
	return b.String(), starts
}

// featText renders a feature row: yes/no plus an expanded path or note.
func featText(supported bool, extra string, g rom.Game, r *emulate.Recipe) string {
	s := i18n.T("feat.no")
	if supported {
		s = i18n.T("feat.yes")
	}
	if extra != "" {
		if expanded, err := r.Expand(extra, g); err == nil {
			extra = expanded
		}
		s += " - " + extra
	}
	return s
}
