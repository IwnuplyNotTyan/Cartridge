package i18n

import (
	"testing"
)

func withLang(t *testing.T, l Lang) {
	t.Helper()
	prev := Get()
	Set(l)
	t.Cleanup(func() { Set(prev) })
}

func TestFallbackToEnglish(t *testing.T) {
	withLang(t, Ru)
	if got := T("key.up"); got != "вверх" {
		t.Fatalf("ru key.up = %q", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Fatalf("missing key should echo, got %q", got)
	}
}

func TestParse(t *testing.T) {
	for in, want := range map[string]Lang{
		"":        Auto,
		"auto":    Auto,
		"en":      En,
		"EN":      En,
		"ru":      Ru,
		"ru_RU":   Ru,
		"russian": Ru,
		"de":      En,
	} {
		if got := Parse(in); got != want {
			t.Fatalf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectRussian(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "ru_RU.UTF-8")
	if got := Detect(); got != Ru {
		t.Fatalf("Detect() = %q, want ru", got)
	}
}

func TestDetectEnglish(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := Detect(); got != En {
		t.Fatalf("Detect() = %q, want en", got)
	}
}

func TestWordPlurals(t *testing.T) {
	withLang(t, En)
	if got := Word(1, "ROM", "ROMs", "ROMs"); got != "ROM" {
		t.Fatalf("en 1 = %q", got)
	}
	if got := Word(5, "ROM", "ROMs", "ROMs"); got != "ROMs" {
		t.Fatalf("en 5 = %q", got)
	}

	withLang(t, Ru)
	for n, want := range map[int]string{1: "байт", 2: "байта", 5: "байтов", 11: "байтов", 21: "байт", 22: "байта", 25: "байтов"} {
		if got := Word(n, "байт", "байта", "байтов"); got != want {
			t.Fatalf("ru %d = %q, want %q", n, got, want)
		}
	}
}

func TestSetAutoResolves(t *testing.T) {
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	t.Setenv("LANG", "")
	prev := Get()
	defer Set(prev)
	Set(Auto)
	if Get() != Ru {
		t.Fatalf("Set(Auto) with ru env = %q", Get())
	}
}
