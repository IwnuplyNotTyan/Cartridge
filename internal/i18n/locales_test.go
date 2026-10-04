package i18n

// NOTE: the locale registry is process-global, so the tests below only
// ever add unique codes/keys ("de", "xx", "test.only.*") and never alter
// shipped en/ru content. TestParse's unknown-code case uses "zz" for the
// same reason: it must stay unknown no matter the file order.

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLocaleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedLocalesLoaded(t *testing.T) {
	got := Langs()
	if len(got) < 2 || got[0] != En || got[1] != Ru {
		t.Fatalf("shipped order must start [en ru], got %v", got)
	}
	if Name(En) != "English" || Name(Ru) != "Русский" {
		t.Fatalf("names = %q %q", Name(En), Name(Ru))
	}
	if !Valid(En) || !Valid(Ru) || Valid(Auto) || Valid("zz") {
		t.Fatalf("validity broken")
	}
	withLang(t, En)
	if T("key.up") != "move up" {
		t.Fatalf("en key.up = %q", T("key.up"))
	}
	withLang(t, Ru)
	if T("key.up") != "вверх" {
		t.Fatalf("ru key.up = %q", T("key.up"))
	}
}

func TestDynamicLanguageFromDir(t *testing.T) {
	dir := t.TempDir()
	writeLocaleFile(t, dir, "de.json", `{"code": "de", "name": "Deutsch", "plural": "germanic", `+
		`"messages": {"key.up": "nach oben", "meta.mods": "Mods"}}`)
	writeLocaleFile(t, dir, "notes.txt", "not a locale")
	n, err := LoadDir(dir)
	if err != nil || n != 1 {
		t.Fatalf("LoadDir = %d, %v", n, err)
	}
	found := false
	for _, l := range Langs() {
		if l == Lang("de") {
			found = true
		}
	}
	if !found {
		t.Fatalf("de must appear in %v", Langs())
	}
	if Name(Lang("de")) != "Deutsch" {
		t.Fatalf("name = %q", Name(Lang("de")))
	}
	for in, want := range map[string]Lang{"de": "de", "DE": "de", "de-DE": "de", "Deutsch": "de", "deutsch": "de"} {
		if got := Parse(in); got != want {
			t.Fatalf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
	withLang(t, Lang("de"))
	if got := T("key.up"); got != "nach oben" {
		t.Fatalf("de key.up = %q", got)
	}
	// A partial file works: missing keys fall back to English.
	if got := T("key.down"); got != "move down" {
		t.Fatalf("de key.down must fall back, got %q", got)
	}
	if got := Word(1, "ROM", "ROMS", "ROMS"); got != "ROM" {
		t.Fatalf("de plural = %q", got)
	}
}

func TestDetectDynamicLanguage(t *testing.T) {
	dir := t.TempDir()
	writeLocaleFile(t, dir, "de.json", `{"code": "de", "name": "Deutsch", "plural": "germanic", `+
		`"messages": {"key.up": "nach oben"}}`)
	if _, err := LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "de_DE.UTF-8")
	if got := Detect(); got != Lang("de") {
		t.Fatalf("Detect() = %q, want de", got)
	}
	prev := Get()
	defer Set(prev)
	Set(Auto)
	if Get() != Lang("de") {
		t.Fatalf("Set(Auto) with de env = %q", Get())
	}
}

func TestDynamicOverrideAndSlavic(t *testing.T) {
	dir := t.TempDir()
	// Overlaying one key into English leaves everything else intact.
	writeLocaleFile(t, dir, "en-tweak.json", `{"code": "en", "messages": {"test.only.en": "yay"}}`)
	if _, err := LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	withLang(t, En)
	if got := T("test.only.en"); got != "yay" {
		t.Fatalf("overlay = %q", got)
	}
	if got := T("key.up"); got != "move up" {
		t.Fatalf("en must survive overlay, got %q", got)
	}
	// A slavic-rule language gets Russian-style plurals with no code change.
	writeLocaleFile(t, dir, "xx.json", `{"code": "xx", "name": "Xx", "plural": "slavic", `+
		`"messages": {"k": "v"}}`)
	if _, err := LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	withLang(t, Lang("xx"))
	for n, want := range map[int]string{1: "one", 2: "few", 5: "many", 11: "many", 21: "one", 22: "few"} {
		if got := Word(n, "one", "few", "many"); got != want {
			t.Fatalf("xx %d = %q, want %q", n, got, want)
		}
	}
}

func TestLoadDirRejects(t *testing.T) {
	for name, content := range map[string]string{
		"bad.json":    `{"code": "yy", "messages": {broken`,
		"plural.json": `{"code": "yy", "plural": "martian", "messages": {"k": "v"}}`,
		"nocode.json": `{"messages": {"k": "v"}}`,
		"empty.json":  `{"code": "yy", "messages": {}}`,
	} {
		dir := t.TempDir()
		writeLocaleFile(t, dir, name, content)
		if _, err := LoadDir(dir); err == nil {
			t.Fatalf("%s must error", name)
		}
	}
	if n, err := LoadDir(filepath.Join(t.TempDir(), "nope")); n != 0 || err != nil {
		t.Fatalf("missing dir = %d, %v", n, err)
	}
	if n, err := LoadDir(""); n != 0 || err != nil {
		t.Fatalf("empty dir = %d, %v", n, err)
	}
}
