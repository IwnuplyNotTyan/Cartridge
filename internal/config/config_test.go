package config

import (
	"os"
	"path/filepath"
	"runtime"
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
	t.Setenv(EnvRecipes, "")
	t.Setenv(EnvCheats, "")
	t.Setenv(EnvShowArt, "")
	t.Setenv(EnvOpenApps, "")
	t.Setenv("CARTRIDGE_NO_POSTER", "")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_MESSAGES", "")
}

func TestResolveDefaults(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("", "", "", "", "")
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
	c, err := Resolve("", "", "", "", "")
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
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.RomDir != "/env/roms" || c.Lang != i18n.En || c.Library != "/file/lib.json" {
		t.Fatalf("env precedence broken: %+v", c)
	}

	// Flags beat env.
	c, err = Resolve("/flag/roms", "", "ru", "", "")
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
	if _, err := Resolve("", "", "", filepath.Join(home, "nope.json"), ""); err == nil {
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
	if _, err := Resolve("", "", "", bad, ""); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestResolveRecipesPath(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(home, ".config", "cartridge", "recipes.json")
	if c.Recipes != want {
		t.Fatalf("Recipes = %q, want %q", c.Recipes, want)
	}
	t.Setenv(EnvRecipes, "/env/recipes.json")
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.Recipes != "/env/recipes.json" {
		t.Fatalf("env recipes broken: %+v", c)
	}
	c, err = Resolve("", "", "", "", "/flag/recipes.json")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.Recipes != "/flag/recipes.json" {
		t.Fatalf("flag recipes broken: %+v", c)
	}
}

func TestResolveCheatsDir(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "cartridge", "cheats")
	if c.CheatsDir != want {
		t.Fatalf("CheatsDir = %q, want %q", c.CheatsDir, want)
	}
	t.Setenv(EnvCheats, "/env/cheats")
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.CheatsDir != "/env/cheats" {
		t.Fatalf("env cheats broken: %+v", c)
	}
	// File value applies when env is clear; ToFile round-trips it.
	t.Setenv(EnvCheats, "")
	cfgDir := filepath.Join(home, ".config", "cartridge")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"cheats_dir": "/file/cheats"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.CheatsDir != "/file/cheats" {
		t.Fatalf("file cheats broken: %+v", c)
	}
	if f := c.ToFile(); f.CheatsDir != "/file/cheats" {
		t.Fatalf("ToFile cheats = %q", f.CheatsDir)
	}
}

func TestResolveShowArt(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !c.ShowArt {
		t.Fatalf("ShowArt must default to true")
	}
	// File value applies.
	cfgDir := home + "/.config/cartridge"
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgDir+"/config.json", []byte(`{"show_art": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.ShowArt {
		t.Fatalf("file show_art=false must apply: %+v", c)
	}
	// Env beats file.
	t.Setenv(EnvShowArt, "1")
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !c.ShowArt {
		t.Fatalf("env show_art=1 must win: %+v", c)
	}
	// Legacy hard-off switch.
	t.Setenv("CARTRIDGE_NO_POSTER", "1")
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.ShowArt {
		t.Fatalf("CARTRIDGE_NO_POSTER must force art off: %+v", c)
	}
	// ToFile round-trips the toggle.
	t.Setenv("CARTRIDGE_NO_POSTER", "")
	t.Setenv(EnvShowArt, "")
	c.ShowArt = false
	if f := c.ToFile(); f.ShowArt == nil || *f.ShowArt {
		t.Fatalf("ToFile must carry show_art=false: %+v", f.ShowArt)
	}
}

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("~/roms", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(home, "roms"); c.RomDir != want {
		t.Fatalf("RomDir = %q, want %q", c.RomDir, want)
	}
}

func TestResolveOpenApps(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.OpenApps != (runtime.GOOS == "darwin") {
		t.Fatalf("OpenApps default = %v on %s", c.OpenApps, runtime.GOOS)
	}

	cfgDir := filepath.Join(home, ".config", "cartridge")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"open_apps": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.OpenApps {
		t.Fatalf("file open setting broken: %+v", c)
	}
	if f := c.ToFile(); f.OpenApps == nil || *f.OpenApps {
		t.Fatalf("ToFile open setting = %+v", f)
	}

	// Env beats file.
	t.Setenv(EnvOpenApps, "1")
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !c.OpenApps {
		t.Fatalf("env open_apps=1 must enable open mode: %+v", c)
	}
}
