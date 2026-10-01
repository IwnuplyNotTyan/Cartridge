package config

import (
	"os"
	"path/filepath"
	"testing"

	"cartridge/internal/i18n"
)

func cleanEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv(EnvDir, "")
	t.Setenv(EnvLibrary, "")
	t.Setenv(EnvLang, "")
	t.Setenv(EnvConfig, "")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_MESSAGES", "")
}

func TestResolveDefaults(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.RomDir != "." {
		t.Fatalf("RomDir = %q, want .", c.RomDir)
	}
	wantLib := filepath.Join(home, ".local", "share", "cartridge", "library.json")
	if c.Library != wantLib {
		t.Fatalf("Library = %q, want %q", c.Library, wantLib)
	}
	if c.Lang != i18n.En {
		t.Fatalf("Lang = %q, want en", c.Lang)
	}
	if c.ConfigFile != "" {
		t.Fatalf("ConfigFile = %q, want empty", c.ConfigFile)
	}
}

func TestResolvePrecedence(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	cfgDir := filepath.Join(home, ".config", "cartridge")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.json")
	fileJSON := `{"rom_dir": "/file/roms", "library": "/file/lib.json", "lang": "ru"}`
	if err := os.WriteFile(cfgPath, []byte(fileJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// File values apply when nothing else is set.
	c, err := Resolve("", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.RomDir != "/file/roms" || c.Library != "/file/lib.json" || c.Lang != i18n.Ru {
		t.Fatalf("file fallback broken: %+v", c)
	}
	if c.ConfigFile != cfgPath {
		t.Fatalf("ConfigFile = %q, want %q", c.ConfigFile, cfgPath)
	}

	// Env beats file.
	t.Setenv(EnvDir, "/env/roms")
	t.Setenv(EnvLang, "en")
	c, err = Resolve("", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.RomDir != "/env/roms" || c.Lang != i18n.En || c.Library != "/file/lib.json" {
		t.Fatalf("env precedence broken: %+v", c)
	}

	// Flags beat env.
	c, err = Resolve("/flag/roms", "", "ru", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.RomDir != "/flag/roms" || c.Lang != i18n.Ru {
		t.Fatalf("flag precedence broken: %+v", c)
	}
}

func TestResolveMissingExplicitConfig(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	if _, err := Resolve("", "", "", filepath.Join(home, "nope.json")); err == nil {
		t.Fatalf("expected error for missing explicit config")
	}
}

func TestResolveBadConfig(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	bad := filepath.Join(home, "bad.json")
	if err := os.WriteFile(bad, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("", "", "", bad); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("~/roms", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(home, "roms"); c.RomDir != want {
		t.Fatalf("RomDir = %q, want %q", c.RomDir, want)
	}
}
