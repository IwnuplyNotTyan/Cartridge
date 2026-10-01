// Package i18n provides minimal UI localization (English + Russian)
// with language detection from the environment and no external deps.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Lang is a UI language code.
type Lang string

const (
	// En is English, the default and fallback language.
	En Lang = "en"
	// Ru is Russian.
	Ru Lang = "ru"
	// Auto selects the language from the environment.
	Auto Lang = "auto"
)

var current = En

// Set switches the current UI language (Auto resolves via Detect).
func Set(l Lang) {
	if l == Auto || l == "" {
		l = Detect()
	}
	if l != Ru {
		l = En
	}
	current = l
}

// Get returns the current UI language.
func Get() Lang { return current }

// Parse maps a language name/code to Lang; unknown values fall back to En.
// Empty and "auto" map to Auto.
func Parse(s string) Lang {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return Auto
	case "ru", "rus", "russian", "ru_ru", "ru-ru":
		return Ru
	default:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "ru") {
			return Ru
		}
		return En
	}
}

// Detect picks a language from LC_ALL/LANG/LANGUAGE ("ru_*" → Ru).
func Detect() Lang {
	for _, key := range []string{"LC_ALL", "LANG", "LANGUAGE", "LC_MESSAGES"} {
		if v := os.Getenv(key); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "ru") {
				return Ru
			}
			if strings.TrimSpace(v) != "" {
				return En
			}
		}
	}
	return En
}

// T returns the localized template for key formatted with args.
// Missing keys fall back to English, then to the key itself.
func T(key string, args ...any) string {
	tmpl, ok := catalog[current][key]
	if !ok {
		tmpl, ok = catalog[En][key]
	}
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
// rules (en: one/many; ru: one/few/many).
func Word(n int, one, few, many string) string {
	if current == Ru {
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
