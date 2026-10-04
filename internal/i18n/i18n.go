// Package i18n provides UI localization with no external deps.
//
// Messages live in JSON files: internal/i18n/locales/*.json is embedded
// into the binary, and LoadDir overlays more files from a directory like
// ~/.config/cartridge/locales. Dropping de.json there is enough for
// Deutsch to appear in the language tab — no code changes, no rebuild —
// and even a partial file works, missing keys falling back to English.
// Each file carries its code, display name and plural rule:
//
//	{"code": "de", "name": "Deutsch", "plural": "germanic",
//	 "messages": {"key.up": "nach oben", ...}}
//
// plural is "germanic" (one/many) or "slavic" (one/few/many, the Russian
// noun rule). Missing keys fall back to English, then to the key itself.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed locales/*.json
var embedded embed.FS

// Lang is a UI language code ("en", "ru", "de", ...) or "auto".
type Lang string

const (
	// En is English, the default and fallback language.
	En Lang = "en"
	// Ru is Russian.
	Ru Lang = "ru"
	// Auto selects the language from the environment.
	Auto Lang = "auto"
)

// locale is one loaded language.
type locale struct {
	code     string
	name     string
	plural   string // "germanic" or "slavic"
	messages map[string]string
}

var (
	mu       sync.RWMutex
	locales  = map[string]*locale{}
	order    []string
	current  = string(En)
	fallback = string(En)
)

func init() {
	entries, err := embedded.ReadDir("locales")
	if err != nil {
		panic(fmt.Sprintf("i18n: embedded locales unreadable: %v", err))
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		data, err := embedded.ReadFile("locales/" + e.Name())
		if err != nil {
			panic(fmt.Sprintf("i18n: embedded %s unreadable: %v", e.Name(), err))
		}
		if err := addLocale(data, e.Name()); err != nil {
			panic(fmt.Sprintf("i18n: embedded %s: %v", e.Name(), err))
		}
	}
	if _, ok := locales[fallback]; !ok {
		panic("i18n: embedded English locale missing")
	}
}

// localeFile is the on-disk JSON shape.
type localeFile struct {
	Code     string            `json:"code"`
	Name     string            `json:"name"`
	Plural   string            `json:"plural"`
	Messages map[string]string `json:"messages"`
}

// addLocale parses one JSON document and overlays it onto the registry:
// new codes are appended, known codes merge per key (so a partial file
// already yields a working language), and a non-empty name/plural wins.
func addLocale(data []byte, source string) error {
	var f localeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", source, err)
	}
	code := strings.ToLower(strings.TrimSpace(f.Code))
	if code == "" {
		return fmt.Errorf("parse %s: empty code", source)
	}
	if len(f.Messages) == 0 {
		return fmt.Errorf("parse %s: no messages", source)
	}
	plural := strings.ToLower(strings.TrimSpace(f.Plural))
	if plural == "" {
		plural = "germanic"
	}
	if plural != "germanic" && plural != "slavic" {
		return fmt.Errorf("parse %s: unknown plural %q (want germanic/slavic)", source, f.Plural)
	}
	mu.Lock()
	defer mu.Unlock()
	loc, ok := locales[code]
	if !ok {
		loc = &locale{code: code, messages: map[string]string{}}
		locales[code] = loc
		order = append(order, code)
	}
	for k, v := range f.Messages {
		loc.messages[k] = v
	}
	if name := strings.TrimSpace(f.Name); name != "" {
		loc.name = name
	}
	loc.plural = plural
	return nil
}

// LoadDir reads *.json locale files from dir: new languages appear and
// shipped ones are overlaid per key. A missing dir is fine (zero
// languages loaded); malformed files abort with an error. It returns
// how many files were accepted.
func LoadDir(dir string) (int, error) {
	if strings.TrimSpace(dir) == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	loaded := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return loaded, err
		}
		if err := addLocale(data, e.Name()); err != nil {
			return loaded, err
		}
		loaded++
	}
	return loaded, nil
}

// ExternalDir is where user locale files live: $CARTRIDGE_LOCALES wins,
// then $XDG_CONFIG_HOME/cartridge/locales, then ~/.config/cartridge/locales.
func ExternalDir() string {
	if v := strings.TrimSpace(os.Getenv("CARTRIDGE_LOCALES")); v != "" {
		return v
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartridge", "locales")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "cartridge", "locales")
	}
	return ""
}

// Langs returns the available language codes in load order
// (shipped first, then external).
func Langs() []Lang {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Lang, 0, len(order))
	for _, c := range order {
		out = append(out, Lang(c))
	}
	return out
}

// Name returns the display name of a language ("Deutsch"), or the code
// itself when unknown.
func Name(l Lang) string {
	mu.RLock()
	defer mu.RUnlock()
	if loc, ok := locales[strings.ToLower(string(l))]; ok {
		return loc.name
	}
	return string(l)
}

// Valid reports whether code is a loaded language (not "auto").
func Valid(l Lang) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := locales[strings.ToLower(string(l))]
	return ok
}

// Set switches the current UI language (Auto resolves via Detect).
// Unknown codes fall back to English.
func Set(l Lang) {
	code := strings.ToLower(strings.TrimSpace(string(l)))
	if code == "" || code == string(Auto) {
		code = string(Detect())
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := locales[code]; !ok {
		code = fallback
	}
	if _, ok := locales[code]; !ok && len(order) > 0 {
		code = order[0]
	}
	current = code
}

// Get returns the current UI language.
func Get() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return Lang(current)
}

// Parse maps a language name/code to Lang; unknown values fall back
// to En. Empty and "auto" map to Auto. It accepts codes ("de"),
// codes with region ("de-DE", "ru_RU"), display names ("Deutsch")
// and unambiguous name prefixes ("rus" → ru).
func Parse(s string) Lang {
	norm := strings.ToLower(strings.TrimSpace(s))
	if norm == "" || norm == string(Auto) {
		return Auto
	}
	mu.RLock()
	defer mu.RUnlock()
	if _, ok := locales[norm]; ok {
		return Lang(norm)
	}
	if base := strings.FieldsFunc(norm, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	}); len(base) > 0 {
		if _, ok := locales[base[0]]; ok {
			return Lang(base[0])
		}
	}
	var prefixHit string
	for code, loc := range locales {
		name := strings.ToLower(loc.name)
		if norm == name {
			return Lang(code)
		}
		if prefixHit == "" && strings.HasPrefix(name, norm) {
			prefixHit = code
		}
	}
	if prefixHit != "" {
		return Lang(prefixHit)
	}
	// Legacy leniency: anything starting with "ru" meant Russian.
	if strings.HasPrefix(norm, "ru") {
		if _, ok := locales[string(Ru)]; ok {
			return Ru
		}
	}
	return En
}

// langSubtag extracts the language part of a locale value:
// "ru_RU.UTF-8" → "ru", "de-DE" → "de", "C"/"POSIX" → "".
func langSubtag(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if i := strings.IndexAny(v, ".@"); i >= 0 {
		v = v[:i]
	}
	if v == "" || v == "c" || v == "posix" {
		return ""
	}
	if i := strings.IndexAny(v, "_-"); i >= 0 {
		v = v[:i]
	}
	return v
}

// Detect picks a language from LC_ALL/LANG/LANGUAGE/LC_MESSAGES:
// the first variable naming a loaded language wins, otherwise English.
func Detect() Lang {
	mu.RLock()
	defer mu.RUnlock()
	for _, key := range []string{"LC_ALL", "LANG", "LANGUAGE", "LC_MESSAGES"} {
		sub := langSubtag(os.Getenv(key))
		if sub == "" {
			continue
		}
		if _, ok := locales[sub]; ok {
			return Lang(sub)
		}
		return En
	}
	return En
}

// T returns the localized template for key formatted with args.
// Missing keys fall back to English, then to the key itself.
func T(key string, args ...any) string {
	mu.RLock()
	var tmpl string
	var ok bool
	if loc := locales[current]; loc != nil {
		tmpl, ok = loc.messages[key]
	}
	if !ok {
		if loc := locales[fallback]; loc != nil {
			tmpl, ok = loc.messages[key]
		}
	}
	mu.RUnlock()
	if !ok {
		if len(args) == 0 {
			return key
		}
		return key + ": " + fmt.Sprint(args...)
	}
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}

// Duration formats d compactly using localized units ("2 h 15 min").
func Duration(d time.Duration) string {
	s := int(d.Seconds())
	if s < 0 {
		s = 0
	}
	h, m, sec := s/3600, (s%3600)/60, s%60
	switch {
	case h > 0:
		return fmt.Sprintf("%d %s %d %s", h, T("dur.h"), m, T("dur.min"))
	case m > 0:
		return fmt.Sprintf("%d %s %d %s", m, T("dur.min"), sec, T("dur.sec"))
	default:
		return fmt.Sprintf("%d %s", sec, T("dur.sec"))
	}
}

// Word picks the plural form of a noun for n using the current language
// rules (germanic: one/many; slavic: one/few/many).
func Word(n int, one, few, many string) string {
	mu.RLock()
	loc := locales[current]
	slavic := loc != nil && loc.plural == "slavic"
	mu.RUnlock()
	if slavic {
		m10, m100 := n%10, n%100
		switch {
		case m10 == 1 && m100 != 11:
			return one
		case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
			return few
		default:
			return many
		}
	}
	if n == 1 {
		return one
	}
	return many
}
