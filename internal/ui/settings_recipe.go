package ui

import (
	"fmt"
	"sort"
	"strings"

	"cartridge/internal/config"
	"cartridge/internal/emulate"
	"cartridge/internal/i18n"
)

// Settings recipe editing: patch overlays over base recipes.
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
