package ui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea"

	"cartridge/internal/config"
	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
)

// Mode is the top-level screen: browser, home page or settings.
// Settings keeps only the bottom bar and help from the browser view.
type Mode int

const (
	ModeBrowser Mode = iota
	ModeHome
	ModeSettings
)

// SettingsTab is one tab of the settings screen.
type SettingsTab int

const (
	TabLang SettingsTab = iota
	TabRecipes
	TabPaths
)

// NumSettingsTabs is the tab count (Language | Recipes | Paths).
const NumSettingsTabs = 3

// pathEntry binds a label key to a getter/setter over the model.
type pathEntry struct {
	label string // i18n key suffix shown in UI
	get   func(m *Model) string
	set   func(m *Model, v string)
}

func pathEntries() []pathEntry {
	return []pathEntry{
		{"sett.paths.games", func(m *Model) string { return m.romDir }, func(m *Model, v string) { m.romDir = v }},
		{"sett.paths.library", func(m *Model) string { return m.libPath }, func(m *Model, v string) { m.libPath = v }},
		{"sett.paths.recipes", func(m *Model) string { return m.recipesPath }, func(m *Model, v string) { m.recipesPath = v }},
		{"sett.paths.patches", func(m *Model) string { return m.patchesPath }, func(m *Model, v string) { m.patchesPath = v }},
		{"sett.paths.mods", func(m *Model) string { return m.modsDir }, func(m *Model, v string) { m.modsDir = v }},
		{"sett.paths.patched", func(m *Model) string { return m.patchedDir }, func(m *Model, v string) { m.patchedDir = v }},
		{"sett.paths.saves", func(m *Model) string { return m.savesDir }, func(m *Model, v string) { m.savesDir = v }},
		{"sett.paths.cheats", func(m *Model) string { return m.cheatsDir }, func(m *Model, v string) { m.cheatsDir = v }},
	}
}

// tabTitle returns the localized title of tab t.
func tabTitle(t SettingsTab) string {
	switch t {
	case TabLang:
		return i18n.T("sett.tab.lang")
	case TabRecipes:
		return i18n.T("sett.tab.recipes")
	default:
		return i18n.T("sett.tab.paths")
	}
}

// langOptions returns settings language choices as (fileValue, display):
// Auto first, then every loaded language (shipped or dropped into the
// locales dir). Display names come from the locale files themselves.
func langOptions() [][2]string {
	opts := [][2]string{{"auto", i18n.T("sett.lang.auto")}}
	for _, l := range i18n.Langs() {
		opts = append(opts, [2]string{string(l), i18n.Name(l)})
	}
	return opts
}

// langCur returns the cursor index matching the stored raw lang value.
func (m Model) langCur() int {
	opts := langOptions()
	want := string(i18n.Parse(m.langRaw))
	if want == string(i18n.Auto) {
		return 0
	}
	for i, opt := range opts {
		if opt[0] == want {
			return i
		}
	}
	return 0
}

// openSettings enters settings mode, keeping bar+help.
func (m *Model) openSettings() {
	m.mode = ModeSettings
	m.settCur = 0
	m.settEditing = false
	m.settBuf = nil
	m.settPatchField = ""
	m.settField = -1
	m.settCur = clamp(m.settCur, 0, 0)
}

// closeSettings returns to the browser.
func (m *Model) closeSettings() {
	m.mode = ModeBrowser
	m.settEditing = false
	m.settBuf = nil
	m.settPatchField = ""
	m.settField = -1
}

// recipeField is one editable detail row shown under the selected recipe.
type recipeField struct {
	name  string // patch field name ("emulator", "args", ...)
	label string // i18n key for the row label
}

// recipeFields lists every editable recipe field in display order.
func recipeFields() []recipeField {
	return []recipeField{
		{"emulator", "sett.recipes.emulator"},
		{"args", "sett.recipes.args"},
		{"cores", "sett.recipes.cores"},
		{"core_names", "sett.recipes.core_names"},
		{"consoles", "sett.recipes.consoles"},
		{"extensions", "sett.recipes.extensions"},
		{"games", "sett.recipes.games"},
		{"workdir", "sett.recipes.workdir"},
		{"cheats", "meta.cheats"},
		{"achievements", "meta.achievements"},
		{"mods", "meta.mods"},
		{"note", "sett.recipes.note"},
	}
}

// recipeFieldIndex returns the detail-row index for a patch field name.
func recipeFieldIndex(name string) int {
	for i, f := range recipeFields() {
		if f.name == name {
			return i
		}
	}
	return 0
}

// persistSettings writes current paths+language back to the config file.
// Paths are expanded (~/...) before saving; the recipes file itself and
// the patches file are never rewritten here — only config.json changes.
func (m *Model) persistSettings() error {
	path := m.cfgFile
	if path == "" {
		path = config.DefaultFilePath()
	}
	lang := strings.ToLower(strings.TrimSpace(m.langRaw))
	if lang == "" {
		lang = "auto"
	}
	f := config.File{
		RomDir:    m.romDir,
		Library:   m.libPath,
		Lang:      lang,
		Recipes:   m.recipesPath,
		Patches:   m.patchesPath,
		ModsDir:   m.modsDir,
		Patched:   m.patchedDir,
		SavesDir:  m.savesDir,
		CheatsDir: m.cheatsDir,
	}
	if err := config.SaveFile(path, f); err != nil {
		return err
	}
	m.cfgFile = path
	return nil
}

// applyLanguage switches the UI language at once and persists it.
// The stored value is always a code ("auto", "en", "ru", "de", ...),
// whatever spelling the user had before.
func (m *Model) applyLanguage(raw string) {
	l := i18n.Parse(raw)
	if l == i18n.Auto {
		m.langRaw = "auto"
	} else {
		m.langRaw = string(l)
	}
	i18n.Set(l)
	m.keys = newKeyMap()
	m.setGames(m.games)
	m.refreshMeta()
	if err := m.persistSettings(); err != nil {
		m.status = i18n.T("status.sett.save_fail", err)
		return
	}
	m.status = ""
}

// reloadRecipes re-reads recipes+patches from disk and re-applies the overlay.
func (m *Model) reloadRecipes() {
	base, err := emulate.Load(m.recipesPath)
	if err != nil {
		m.status = i18n.T("status.sett.reload_fail", err)
		return
	}
	patches, err := emulate.LoadPatches(m.patchesPath)
	if err != nil {
		m.status = i18n.T("status.sett.reload_fail", err)
		return
	}
	m.baseRecipes = base
	m.patches = patches
	m.recipes = emulate.ApplyPatches(base, patches)
	if m.settCur >= len(m.recipes) {
		m.settCur = max(len(m.recipes)-1, 0)
	}
	m.settField = clamp(m.settField, -1, len(recipeFields())-1)
	m.refreshMeta()
	m.status = i18n.T("status.sett.reloaded", len(base), len(emulate.AppliedPatches(base, patches)))
}

// effectiveRecipe returns the effective (patched) recipe for the selected row.
func (m Model) effectiveRecipe() emulate.Recipe {
	cur := clamp(m.settCur, 0, len(m.baseRecipes)-1)
	if len(m.recipes) == len(m.baseRecipes) {
		return m.recipes[cur]
	}
	return emulate.ApplyPatches(m.baseRecipes, m.patches)[cur]
}

// startRecipeEdit begins inline patch editing for the selected recipe.
// field is any recipeFields() name; the buffer is prefilled with the
// effective value so a patch only overrides what the user changes.
func (m *Model) startRecipeEdit(field string) {
	if len(m.baseRecipes) == 0 || m.settCur >= len(m.baseRecipes) {
		return
	}
	known := false
	for _, f := range recipeFields() {
		if f.name == field {
			known = true
			break
		}
	}
	if !known {
		field = "emulator"
	}
	m.settPatchField = field
	m.settField = recipeFieldIndex(field)
	m.settBuf = []rune(fieldBuffer(field, m.effectiveRecipe()))
	m.settEditing = true
}

// saveRecipeEdit writes the edit buffer as a patch overlay and persists it.
// The base recipes file is never rewritten; only the patches file changes.
func (m *Model) saveRecipeEdit() {
	if len(m.baseRecipes) == 0 || m.settCur >= len(m.baseRecipes) {
		return
	}
	name := m.baseRecipes[m.settCur].Name
	field := m.settPatchField
	known := false
	for _, f := range recipeFields() {
		if f.name == field {
			known = true
			break
		}
	}
	if !known {
		field = "emulator"
	}
	v := strings.TrimSpace(string(m.settBuf))
	var p emulate.RecipePatch
	p.Recipe = name
	switch field {
	case "args":
		args := splitArgs(v)
		p.Args = &args
	case "cores":
		cs := parseCores(v)
		p.Cores = &cs
	case "core_names":
		ns := parseCoreNames(v)
		p.CoreNames = &ns
	case "consoles":
		vs := splitList(v)
		p.Consoles = &vs
	case "extensions":
		vs := splitList(v)
		p.Extensions = &vs
	case "games":
		vs := splitList(v)
		p.Games = &vs
	case "workdir":
		wd := config.ExpandPath(v)
		p.WorkDir = &wd
	case "cheats":
		ok, rest, err := parseFeatureBool(v)
		if err != nil {
			m.status = i18n.T("status.sett.patch_fail", err)
			return
		}
		p.Cheats = &emulate.Cheats{Supported: ok, Path: rest}
	case "achievements":
		ok, rest, err := parseFeatureBool(v)
		if err != nil {
			m.status = i18n.T("status.sett.patch_fail", err)
			return
		}
		p.Achievements = &emulate.Achievements{Supported: ok, Note: rest}
	case "mods":
		ok, rest, err := parseFeatureBool(v)
		if err != nil {
			m.status = i18n.T("status.sett.patch_fail", err)
			return
		}
		p.Mods = &emulate.Mods{Supported: ok, Path: rest}
	case "note":
		note := strings.TrimSpace(string(m.settBuf))
		p.Note = &note
	default:
		if v == "" {
			m.status = i18n.T("status.sett.patch_fail", fmt.Errorf("empty emulator"))
			return
		}
		emu := config.ExpandPath(v)
		p.Emulator = &emu
	}
	path := m.patchesPath
	if strings.TrimSpace(path) == "" {
		path = config.DefaultPatchesPath()
	}
	updated, err := emulate.StorePatch(path, m.patches, p)
	if err != nil {
		m.status = i18n.T("status.sett.patch_fail", err)
		return
	}
	m.patches = updated
	if strings.TrimSpace(m.patchesPath) == "" {
		m.patchesPath = path
		if err := m.persistSettings(); err != nil {
			m.status = i18n.T("status.sett.save_fail", err)
			return
		}
	}
	m.recipes = emulate.ApplyPatches(m.baseRecipes, m.patches)
	m.refreshMeta()
	m.status = i18n.T("status.sett.patch_saved", name)
}

// fieldBuffer renders the effective field value as an editable line.
func fieldBuffer(field string, eff emulate.Recipe) string {
	switch field {
	case "args":
		return joinArgs(eff.Args)
	case "cores":
		return joinCores(eff.Cores)
	case "core_names":
		return joinCoreNames(eff.CoreNames)
	case "consoles":
		return strings.Join(eff.Consoles, ", ")
	case "extensions":
		return strings.Join(eff.Extensions, ", ")
	case "games":
		return strings.Join(eff.Games, ", ")
	case "workdir":
		return eff.WorkDir
	case "cheats":
		return featureBuffer(eff.Cheats.Supported, eff.Cheats.Path)
	case "achievements":
		return featureBuffer(eff.Achievements.Supported, eff.Achievements.Note)
	case "mods":
		return featureBuffer(eff.Mods.Supported, eff.Mods.Path)
	case "note":
		return eff.Note
	default:
		return eff.Emulator
	}
}

// featureBuffer renders a supported+path/note feature as an editable line:
// "true <extra>" or "false".
func featureBuffer(supported bool, extra string) string {
	if strings.TrimSpace(extra) == "" {
		if supported {
			return "true"
		}
		return "false"
	}
	if supported {
		return "true " + extra
	}
	return "false " + extra
}

// parseFeatureBool splits "bool rest" where bool is true/false, yes/no,
// 1/0, on/off (ru: да/нет). The rest (path or note) is returned as-is.
func parseFeatureBool(s string) (bool, string, error) {
	head, rest, _ := strings.Cut(s, " ")
	head = strings.ToLower(strings.TrimSpace(head))
	rest = strings.TrimSpace(rest)
	switch head {
	case "true", "yes", "y", "1", "on", "да", "д", "+":
		return true, rest, nil
	case "false", "no", "n", "0", "off", "нет", "н", "-":
		return false, rest, nil
	}
	return false, "", fmt.Errorf("want true/false first: %q", s)
}

// splitList splits a comma-separated line, dropping empty items.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// joinCores renders a console→cores map as one editable line:
// "nes: a.so, b.so; snes: c.so". Consoles sort for a stable view.
func joinCores(cores map[string][]string) string {
	if len(cores) == 0 {
		return ""
	}
	ids := make([]string, 0, len(cores))
	for id := range cores {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id+": "+strings.Join(cores[id], ", "))
	}
	return strings.Join(parts, "; ")
}

// parseCores parses a "nes: a.so, b.so; snes: c.so" line back into
// a console→cores map. Console ids lowercase; empty items drop out.
func parseCores(s string) map[string][]string {
	out := map[string][]string{}
	for _, group := range strings.Split(s, ";") {
		id, list, ok := strings.Cut(group, ":")
		if !ok {
			continue
		}
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		var cores []string
		for _, c := range strings.Split(list, ",") {
			if v := strings.TrimSpace(c); v != "" {
				cores = append(cores, v)
			}
		}
		if len(cores) > 0 {
			out[id] = cores
		}
	}
	return out
}

// joinCoreNames renders a console→display-name map as one editable line:
// "gba=mGBA; nes=Nestopia". Consoles sort for a stable view.
func joinCoreNames(names map[string]string) string {
	if len(names) == 0 {
		return ""
	}
	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id+"="+names[id])
	}
	return strings.Join(parts, "; ")
}

// parseCoreNames parses a "gba=mGBA; nes=Nestopia" line back into
// a console→display-name map. Console ids lowercase, display names keep
// their case; empty items drop out.
func parseCoreNames(s string) map[string]string {
	out := map[string]string{}
	for _, group := range strings.Split(s, ";") {
		id, name, ok := strings.Cut(group, "=")
		if !ok {
			continue
		}
		id = strings.ToLower(strings.TrimSpace(id))
		if name = strings.TrimSpace(name); id == "" || name == "" {
			continue
		}
		out[id] = name
	}
	return out
}

// moveRecipe shifts the selected recipe up (dir<0) or down (dir>0) in the
// hierarchy and saves the new order to the recipes file. The cursor follows
// the moved recipe; patches keep applying by name. No-op at the edges.
func (m *Model) moveRecipe(dir int) {
	n := len(m.baseRecipes)
	if n < 2 || m.settCur < 0 || m.settCur >= n {
		return
	}
	j := clamp(m.settCur+dir, 0, n-1)
	if j == m.settCur {
		return
	}
	name := m.baseRecipes[m.settCur].Name
	m.baseRecipes[m.settCur], m.baseRecipes[j] = m.baseRecipes[j], m.baseRecipes[m.settCur]
	if err := emulate.SaveRecipes(m.recipesPath, m.baseRecipes); err != nil {
		m.baseRecipes[m.settCur], m.baseRecipes[j] = m.baseRecipes[j], m.baseRecipes[m.settCur]
		m.status = i18n.T("status.sett.recipes_save_fail", err)
		return
	}
	m.settCur = j
	m.recipes = emulate.ApplyPatches(m.baseRecipes, m.patches)
	m.refreshMeta()
	m.status = i18n.T("status.sett.moved", name)
}

// dropRecipePatch removes every patch for the selected recipe and persists.
func (m *Model) dropRecipePatch() {
	if len(m.baseRecipes) == 0 || m.settCur >= len(m.baseRecipes) {
		return
	}
	name := m.baseRecipes[m.settCur].Name
	path := m.patchesPath
	if strings.TrimSpace(path) == "" {
		path = config.DefaultPatchesPath()
	}
	updated, err := emulate.DropPatches(path, m.patches, name)
	if err != nil {
		m.status = i18n.T("status.sett.patch_fail", err)
		return
	}
	m.patches = updated
	m.recipes = emulate.ApplyPatches(m.baseRecipes, m.patches)
	m.refreshMeta()
	m.status = i18n.T("status.sett.patch_dropped", name)
}

// splitArgs splits an args line on spaces, keeping "quoted parts" together.
// Quotes are stripped; an empty input yields an empty (non-nil) slice.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	esc := false
	flush := func() {
		out = append(out, cur.String())
		cur.Reset()
	}
	has := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
			has = true
		case r == '\\' && inQuote:
			esc = true
		case r == '"':
			inQuote = !inQuote
			has = true
		case r == ' ' || r == '\t':
			if inQuote {
				cur.WriteRune(r)
				has = true
			} else if has {
				flush()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		flush()
	}
	if out == nil {
		return []string{}
	}
	return out
}

// joinArgs renders args as a single editable line, quoting parts with spaces.
func joinArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"") {
			parts[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

func (m Model) settingsRows() int {
	switch m.settTab {
	case TabLang:
		return len(langOptions())
	case TabRecipes:
		// Recipe rows plus the detail rows under the selected recipe.
		return max(len(m.baseRecipes)+len(recipeFields()), 1)
	default:
		return len(pathEntries())
	}
}

// updateSettings handles keys while in settings mode.
// Returns (model, cmd, handledQuit) — handledQuit is true when the key
// already produced a quit command.
func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Inline editing: most keys go to the buffer.
	// Recipes tab edits a patch field (emulator/args), Paths tab edits a path.
	if m.settEditing {
		if m.settTab == TabRecipes {
			switch msg.Type {
			case tea.KeyEnter:
				m.saveRecipeEdit()
				m.settEditing = false
				m.settBuf = nil
				m.settPatchField = ""
				return m, nil
			case tea.KeyEsc:
				m.settEditing = false
				m.settBuf = nil
				m.settPatchField = ""
				return m, nil
			case tea.KeyBackspace:
				if len(m.settBuf) > 0 {
					m.settBuf = m.settBuf[:len(m.settBuf)-1]
				}
				return m, nil
			case tea.KeyRunes:
				m.settBuf = append(m.settBuf, msg.Runes...)
				return m, nil
			case tea.KeySpace:
				m.settBuf = append(m.settBuf, ' ')
				return m, nil
			default:
				if len(msg.String()) == 1 {
					m.settBuf = append(m.settBuf, []rune(msg.String())...)
				}
				return m, nil
			}
		}
		switch msg.Type {
		case tea.KeyEnter:
			v := config.ExpandPath(string(m.settBuf))
			if strings.TrimSpace(v) != "" {
				entries := pathEntries()
				if m.settCur < len(entries) {
					entries[m.settCur].set(&m, v)
				}
				// Switching the games dir rescans lazily on next r;
				// switching recipes/patches paths reloads the overlay.
				if m.settCur == 2 || m.settCur == 3 {
					m.reloadRecipes()
				}
				if err := m.persistSettings(); err != nil {
					m.status = i18n.T("status.sett.save_fail", err)
				} else if m.status == "" || !strings.Contains(m.status, "reloaded") {
					m.status = i18n.T("status.sett.saved")
				}
			}
			m.settEditing = false
			m.settBuf = nil
			return m, nil
		case tea.KeyEsc:
			m.settEditing = false
			m.settBuf = nil
			return m, nil
		case tea.KeyBackspace:
			if len(m.settBuf) > 0 {
				m.settBuf = m.settBuf[:len(m.settBuf)-1]
			}
			return m, nil
		case tea.KeyRunes:
			m.settBuf = append(m.settBuf, msg.Runes...)
			return m, nil
		case tea.KeySpace:
			m.settBuf = append(m.settBuf, ' ')
			return m, nil
		default:
			// Ignore navigation keys while editing; type text instead.
			if len(msg.String()) == 1 {
				m.settBuf = append(m.settBuf, []rune(msg.String())...)
			}
			return m, nil
		}
	}

	k := m.keys
	// Global in settings: help, quit, close.
	switch {
	case key.Matches(msg, k.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		return m, nil
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc", "s", "S":
		// Esc on a detail row steps back to the recipe row;
		// s (or esc on the row itself) closes settings.
		if msg.String() == "esc" && m.settTab == TabRecipes && !m.settEditing && m.settField != -1 {
			m.settField = -1
			return m, nil
		}
		m.closeSettings()
		return m, nil
	case "1":
		m.settTab = TabLang
		m.settCur = 0
		m.settField = -1
		return m, nil
	case "2":
		m.settTab = TabRecipes
		m.settCur = 0
		m.settField = -1
		return m, nil
	case "3":
		m.settTab = TabPaths
		m.settCur = 0
		m.settField = -1
		return m, nil
	}
	// Tab switches tabs (like "example | example | etc" navigation).
	if key.Matches(msg, k.Next) || msg.String() == "right" || msg.String() == "l" {
		m.settTab = (m.settTab + 1) % NumSettingsTabs
		m.settCur = 0
		m.settField = -1
		return m, nil
	}
	if key.Matches(msg, k.Prev) || msg.String() == "left" || msg.String() == "h" {
		m.settTab = (m.settTab + NumSettingsTabs - 1) % NumSettingsTabs
		m.settCur = 0
		m.settField = -1
		return m, nil
	}
	// Recipes tab: ↑↓ walk recipe rows only; the detail rows under the
	// selected recipe are reachable solely via enter (settField >= 0),
	// esc steps back out. g/G jump to the first/last recipe.
	if m.settTab == TabRecipes && len(m.baseRecipes) > 0 {
		nf := len(recipeFields())
		switch {
		case key.Matches(msg, k.Up):
			if m.settField >= 0 {
				m.settField = max(m.settField-1, 0)
			} else if m.settCur > 0 {
				m.settCur--
			}
			return m, nil
		case key.Matches(msg, k.Down):
			if m.settField >= 0 {
				m.settField = min(m.settField+1, nf-1)
			} else if m.settCur < len(m.baseRecipes)-1 {
				m.settCur++
			}
			return m, nil
		case key.Matches(msg, k.Top):
			m.settCur = 0
			m.settField = -1
			return m, nil
		case key.Matches(msg, k.Bottom):
			m.settCur = max(len(m.baseRecipes)-1, 0)
			m.settField = -1
			return m, nil
		}
	}
	n := m.settingsRows()
	switch {
	case key.Matches(msg, k.Up):
		if m.settCur > 0 {
			m.settCur--
		}
		return m, nil
	case key.Matches(msg, k.Down):
		if m.settCur < n-1 {
			m.settCur++
		}
		return m, nil
	case key.Matches(msg, k.Top):
		m.settCur = 0
		return m, nil
	case key.Matches(msg, k.Bottom):
		m.settCur = max(n-1, 0)
		return m, nil
	}
	switch m.settTab {
	case TabLang:
		if key.Matches(msg, k.Select) {
			opts := langOptions()
			if m.settCur < len(opts) {
				m.applyLanguage(opts[m.settCur][0])
			}
			return m, nil
		}
	case TabRecipes:
		if key.Matches(msg, k.Select) {
			// Enter on a recipe row opens its detail rows;
			// enter on a detail row edits that field as a patch.
			if m.settField == -1 {
				m.settField = 0
			} else {
				m.startRecipeEdit(recipeFields()[clamp(m.settField, 0, len(recipeFields())-1)].name)
			}
			return m, nil
		}
		switch msg.String() {
		case "e", "E":
			m.startRecipeEdit("emulator")
			return m, nil
		case "a", "A":
			m.startRecipeEdit("args")
			return m, nil
		case "d", "D":
			m.dropRecipePatch()
			return m, nil
		case "J":
			m.moveRecipe(+1)
			return m, nil
		case "K":
			m.moveRecipe(-1)
			return m, nil
		}
		switch msg.Type {
		case tea.KeyDelete, tea.KeyBackspace:
			m.dropRecipePatch()
			return m, nil
		}
		if key.Matches(msg, k.Rescan) {
			m.reloadRecipes()
			return m, nil
		}
	case TabPaths:
		if key.Matches(msg, k.Select) {
			entries := pathEntries()
			if m.settCur < len(entries) {
				m.settEditing = true
				m.settBuf = []rune(entries[m.settCur].get(&m))
			}
			return m, nil
		}
		if key.Matches(msg, k.Rescan) {
			m.reloadRecipes()
			return m, nil
		}
	}
	// Global rescan from any settings tab.
	if key.Matches(msg, k.Rescan) {
		m.reloadRecipes()
		return m, nil
	}
	return m, nil
}

// renderSettingsTabs renders the top tab strip:
// Language | Recipes | Paths (active tab highlighted).
func (m Model) renderSettingsTabs(w int) string {
	parts := make([]string, 0, NumSettingsTabs)
	for t := SettingsTab(0); t < NumSettingsTabs; t++ {
		title := tabTitle(t)
		if t == m.settTab {
			parts = append(parts, titleActive.Render(title))
		} else {
			parts = append(parts, titleDim.Render(title))
		}
	}
	sep := infoStyle.Render(" | ")
	line := strings.Join(parts, sep)
	// Keep the literal " | " separator measurable even with ANSI codes.
	return fit(line, w)
}

// renderSettings builds the settings body: tabs on top, tab content below.
// Only the bottom bar and help survive from the browser view.
func (m Model) renderSettings() string {
	w := m.contentW()
	var lines []string
	lines = append(lines, m.renderSettingsTabs(w))
	lines = append(lines, "")
	switch m.settTab {
	case TabLang:
		lines = append(lines, m.renderLangTab(w)...)
	case TabRecipes:
		lines = append(lines, m.renderRecipesTab(w)...)
	default:
		lines = append(lines, m.renderPathsTab(w)...)
	}
	for len(lines) < m.bodyH() {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.bodyH()], "\n")
}

func (m Model) renderLangTab(w int) []string {
	var lines []string
	cur := m.langCur()
	for i, opt := range langOptions() {
		label := opt[1]
		var plain string
		switch {
		case i == m.settCur:
			plain = styledRow(fit("  "+truncate(label, w-2), w))
		case i == cur:
			// The active language: bold white, no background.
			plain = "  " + langActive.Render(truncate(label, w-2))
		default:
			// Everything else: grey italic.
			plain = "  " + langIdle.Render(truncate(label, w-2))
		}
		lines = append(lines, plain)
	}
	return lines
}

func (m Model) renderRecipesTab(w int) []string {
	var lines []string
	dot := infoStyle.Render(" • ")
	head := i18n.T("sett.recipes.base", len(m.baseRecipes)) + dot +
		i18n.T("sett.recipes.patches", len(emulate.AppliedPatches(m.baseRecipes, m.patches)))
	lines = append(lines, fit(head, w))
	lines = append(lines, infoStyle.Render(fit(truncate(i18n.T("sett.recipes.keys"), w), w)))
	lines = append(lines, "")
	if len(m.baseRecipes) == 0 {
		lines = append(lines, infoStyle.Render(fit(i18n.T("sett.recipes.none"), w)))
		return lines
	}
	marks := emulate.PatchedNames(m.baseRecipes, m.patches)
	effList := m.recipes
	if len(effList) != len(m.baseRecipes) {
		effList = emulate.ApplyPatches(m.baseRecipes, m.patches)
	}
	fields := recipeFields()
	for i, r := range m.baseRecipes {
		tag := ""
		if marks[r.Name] {
			tag = " [" + i18n.T("sett.recipes.patched") + "]"
		}
		// Effective (patched) emulator shown; base file stays untouched.
		effEmu := r.Emulator
		if i < len(effList) {
			effEmu = effList[i].Emulator
		} else {
			for _, p := range emulate.PatchesFor(m.patches, r.Name) {
				if p.Emulator != nil {
					effEmu = *p.Emulator
				}
			}
		}
		if _, ok := emulate.LookupBinary(effEmu); !ok {
			tag += " [" + i18n.T("sett.recipes.missing") + "]"
		}
		row := fmt.Sprintf("%s → %s%s", r.Name, effEmu, tag)
		plain := fit("  "+truncate(row, w-2), w)
		if i == m.settCur {
			plain = styledBarRow(plain)
		}
		lines = append(lines, plain)
		// Detail rows live directly under the selected recipe with a left
		// bar; values stay grey, the cursor row is highlighted instead.
		if i != m.settCur {
			continue
		}
		var eff emulate.Recipe
		if i < len(effList) {
			eff = effList[i]
		} else {
			eff = r
		}
		for fi, f := range fields {
			label := i18n.T(f.label)
			if m.settEditing && m.settPatchField == f.name {
				lines = append(lines, styledBarRow(editLine("    ", label, string(m.settBuf), w)))
				continue
			}
			val := fieldDisplay(f.name, r, eff)
			if fi == m.settField {
				row := label + ": " + val
				lines = append(lines, styledBarRow(fit("    "+truncate(row, w-4), w)))
				continue
			}
			lines = append(lines, barRow(idleDetailRow(label, val, w)))
		}
	}
	return lines
}

// fieldDisplay renders the effective field value for a detail row.
// The emulator row also carries the binary check and the base →
// effective transition when a patch overrides it.
func fieldDisplay(name string, base, eff emulate.Recipe) string {
	var val string
	switch name {
	case "emulator":
		val = eff.Emulator
		if base.Emulator != eff.Emulator {
			val = base.Emulator + " → " + eff.Emulator
		}
		if bin, ok := emulate.LookupBinary(eff.Emulator); ok {
			val += " • " + i18n.T("sett.recipes.binary_ok", bin)
		} else {
			val += " • " + i18n.T("sett.recipes.binary_missing", eff.Emulator)
		}
	case "args":
		val = joinArgs(eff.Args)
	case "cores":
		val = joinCores(eff.Cores)
	case "core_names":
		val = joinCoreNames(eff.CoreNames)
	case "consoles":
		val = strings.Join(eff.Consoles, ", ")
	case "extensions":
		val = strings.Join(eff.Extensions, ", ")
	case "games":
		val = strings.Join(eff.Games, ", ")
	case "workdir":
		val = eff.WorkDir
	case "cheats":
		val = featShort(eff.Cheats.Supported, eff.Cheats.Path)
	case "achievements":
		val = featShort(eff.Achievements.Supported, eff.Achievements.Note)
	case "mods":
		val = featShort(eff.Mods.Supported, eff.Mods.Path)
	case "note":
		val = eff.Note
	default:
		val = ""
	}
	if strings.TrimSpace(val) == "" {
		val = "-"
	}
	return val
}

// featShort renders a feature as yes/no plus a raw template or note.
func featShort(supported bool, extra string) string {
	s := i18n.T("feat.no")
	if supported {
		s = i18n.T("feat.yes")
	}
	if strings.TrimSpace(extra) != "" {
		s += " - " + extra
	}
	return s
}

// isPathIdle reports whether path row i renders as idle (grey + italic).
// The row being edited and the cursor row are never idle.
func (m Model) isPathIdle(i int) bool {
	if m.settEditing && i == m.settCur {
		return false
	}
	return i != m.settCur
}

func (m Model) renderPathsTab(w int) []string {
	var lines []string
	entries := pathEntries()
	for i, e := range entries {
		label := i18n.T(e.label)
		val := e.get(&m)
		if m.settEditing && i == m.settCur {
			lines = append(lines, styledRow(editLine("  ", label, string(m.settBuf), w)))
			continue
		}
		row := label + ": " + val
		plain := fit("  "+truncate(row, w-2), w)
		if m.isPathIdle(i) {
			// Idle: only the path value is grey + italic, the name stays plain.
			plain = idlePathRow(label, val, w)
		} else {
			plain = styledRow(plain)
		}
		lines = append(lines, plain)
	}
	return lines
}
