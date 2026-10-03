// Package cheats reads and writes game cheat files.
//
// The supported format is the RetroArch .cht file: an indexed list of
// cheatN_desc / cheatN_code entries (enable flags are normalized to true
// on save — every listed cheat is available at once, with no per-cheat
// choices). The launcher resolves the file path from the recipe's cheats
// template or the shared cheats dir, materializes missing files and shows
// the list in the metadata pane, so the emulator picks it up on launch.
package cheats

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Cheat is one cheat entry: description and code. Every listed cheat is
// available at once; there are no per-cheat enable choices (saves always
// write them enabled).
type Cheat struct {
	Desc string
	Code string
}

// File is a parsed cheat file bound to its path for saving.
// Unknown lines (extra cheatN_* fields like big_endian, comments) are
// preserved verbatim across saves, so toggling never mangles entries.
type File struct {
	Path   string
	Cheats []Cheat

	pre   []string         // unknown lines before the first cheat entry
	extra map[int][]string // raw extra lines per original cheat index
	tail  []string         // unknown lines after the last cheat entry
	src   []int            // original cheat index per entry (for extras)
}

// Load reads and parses path.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cheats, extra, tail, src := parse(data)
	return &File{Path: path, Cheats: cheats, extra: extra, tail: tail, src: src}, nil
}

// Save writes the file back in canonical form (tmp + rename), keeping
// preserved lines in place. Entry order stays as parsed. Every cheat is
// written enabled: the launcher offers no per-cheat choices, everything
// listed is available at once (missing parent dirs are created).
func (f *File) Save() error {
	if f == nil || strings.TrimSpace(f.Path) == "" {
		return fmt.Errorf("empty cheat file path")
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "cheats = %d\n", len(f.Cheats))
	for i, c := range f.Cheats {
		b.WriteString("\n")
		fmt.Fprintf(&b, "cheat%d_desc = %s\n", i, quote(c.Desc))
		fmt.Fprintf(&b, "cheat%d_code = %s\n", i, quote(c.Code))
		fmt.Fprintf(&b, "cheat%d_enable = true\n", i)
		oidx := i
		if i < len(f.src) {
			oidx = f.src[i]
		}
		for _, ln := range f.extra[oidx] {
			b.WriteString(renumberExtra(ln, oidx, i) + "\n")
		}
	}
	for _, ln := range f.tail {
		b.WriteString(ln + "\n")
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

// Find locates a cheat file inside dir by candidate stems: the first
// candidate matching a *.cht file stem wins. Stems compare normalized
// (lowercase, tags like (U) dropped, alnum only), so "Battletoads (U).nes"
// finds "Battletoads.cht". Only the top level of dir is scanned.
func Find(dir string, candidates ...string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	type hit struct{ path, stem string }
	var files []hit
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".cht") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		files = append(files, hit{filepath.Join(dir, e.Name()), normStem(stem)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	for _, c := range candidates {
		if c = normStem(c); c == "" {
			continue
		}
		for _, f := range files {
			if f.stem == c {
				return f.path, nil
			}
		}
	}
	return "", fmt.Errorf("no cheat file in %s", dir)
}

// normStem lowercases s, drops (parenthesized) and [bracketed] tags and
// keeps alphanumerics only, for fuzzy cheat file matching.
func normStem(s string) string {
	s = strings.ToLower(s)
	for {
		i := strings.IndexAny(s, "([")
		if i < 0 {
			break
		}
		closer := ")"
		if s[i] == '[' {
			closer = "]"
		}
		j := strings.Index(s[i:], closer)
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + " " + s[i+j+1:]
	}
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Parse decodes RetroArch .cht content. It tolerates comments (# or ;),
// blank lines, missing quotes and unknown keys; entries carrying neither
// desc nor code are skipped. Order follows cheat indexes.
func Parse(data []byte) []Cheat {
	out, _, _, _ := parse(data)
	return out
}

// parse decodes the content like Parse and additionally collects preserved
// lines: raw extra cheatN_* lines per original index (extra), trailing
// unknown lines (tail), and the original index per returned entry (src,
// so extras survive renumbering after gaps).
func parse(data []byte) (out []Cheat, extra map[int][]string, tail []string, src []int) {
	type raw struct {
		desc, code string
		has        bool
	}
	byIdx := map[int]*raw{}
	extras := map[int][]string{}
	var pending []string
	flush := func(idx int) {
		if len(pending) > 0 {
			extras[idx] = append(extras[idx], pending...)
			pending = nil
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			pending = append(pending, line)
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			pending = append(pending, line)
			continue
		}
		key, val = strings.TrimSpace(key), unquote(strings.TrimSpace(val))
		name := strings.ToLower(key)
		if name == "cheats" {
			continue // count is regenerated on save
		}
		idx, field, ok := cheatKey(key)
		if !ok {
			pending = append(pending, line)
			continue
		}
		flush(idx)
		r := byIdx[idx]
		if r == nil {
			r = &raw{}
			byIdx[idx] = r
		}
		switch field {
		case "desc":
			r.desc, r.has = val, true
		case "code":
			r.code, r.has = val, true
		case "enable":
			// Consumed, not stored: saves always write enabled.
		default:
			extras[idx] = append(extras[idx], line)
		}
	}
	idxs := make([]int, 0, len(byIdx))
	for i, r := range byIdx {
		if r.has && (r.desc != "" || r.code != "") {
			idxs = append(idxs, i)
		}
	}
	sort.Ints(idxs)
	out = make([]Cheat, 0, len(idxs))
	src = make([]int, 0, len(idxs))
	for _, i := range idxs {
		r := byIdx[i]
		out = append(out, Cheat{Desc: r.desc, Code: r.code})
		src = append(src, i)
	}
	// Extras of dropped indexes (no desc/code) rejoin the tail so no
	// line is ever lost.
	kept := make(map[int]bool, len(idxs))
	for _, i := range idxs {
		kept[i] = true
	}
	var orphan []int
	for i := range extras {
		if !kept[i] {
			orphan = append(orphan, i)
		}
	}
	sort.Ints(orphan)
	tail = pending
	for _, i := range orphan {
		tail = append(tail, extras[i]...)
		delete(extras, i)
	}
	return out, extras, tail, src
}

// renumberExtra rewrites a "cheat<old>_" key prefix to the new index so
// extras stay attached to their entry after gap renumbering. Other lines
// (comments, unknown keys) pass through verbatim.
func renumberExtra(line string, oldIdx, newIdx int) string {
	if oldIdx == newIdx {
		return line
	}
	key, rest, ok := strings.Cut(line, "=")
	if !ok {
		return line
	}
	k := strings.TrimSpace(key)
	if len(k) < 6 || !strings.EqualFold(k[:5], "cheat") {
		return line
	}
	j := 5
	for j < len(k) && k[j] >= '0' && k[j] <= '9' {
		j++
	}
	n, err := strconv.Atoi(k[5:j])
	if err != nil || n != oldIdx || j >= len(k) || k[j] != '_' {
		return line
	}
	return k[:5] + strconv.Itoa(newIdx) + k[j:] + " = " + strings.TrimSpace(rest)
}

// cheatKey splits a "cheatN_field" key into index and lowercased field.
func cheatKey(key string) (int, string, bool) {
	name := strings.ToLower(key)
	if !strings.HasPrefix(name, "cheat") {
		return 0, "", false
	}
	rest := key[len("cheat"):]
	us := strings.Index(rest, "_")
	if us < 0 {
		return 0, "", false
	}
	idx, err := strconv.Atoi(rest[:us])
	if err != nil || idx < 0 {
		return 0, "", false
	}
	return idx, strings.ToLower(rest[us+1:]), true
}

// quote wraps s in double quotes, escaping backslashes and quotes.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '\\' || r == '"' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// unquote strips one pair of surrounding double quotes and unescapes.
func unquote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		var b strings.Builder
		esc := false
		for _, r := range s[1 : len(s)-1] {
			if esc {
				b.WriteRune(r)
				esc = false
				continue
			}
			if r == '\\' {
				esc = true
				continue
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	return s
}
