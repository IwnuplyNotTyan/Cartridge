package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSteamDefaults(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	c, err := Resolve("", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Steam.Prefix != "Cartridge" {
		t.Fatalf("Steam.Prefix = %q", c.Steam.Prefix)
	}
	if c.Steam.EffectiveTemplate() != "{prefix} - {name}" {
		t.Fatalf("template = %q", c.Steam.EffectiveTemplate())
	}
	if c.Steam.AutoAdd {
		t.Fatalf("AutoAdd must default to false")
	}
	if c.Steam.AllGames {
		t.Fatalf("AllGames must default to false")
	}
}

func TestSteamFileAndEnv(t *testing.T) {
	home := t.TempDir()
	cleanEnv(t, home)
	cfgDir := filepath.Join(home, ".config", "cartridge")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	js := `{"steam": {"auto_add": true, "prefix": "Emu", "all_games": true, "category_template": "{short}", "exe": "/bin/cart", "categories": {"gba": "GBA custom"}}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Resolve("", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Steam.AutoAdd || c.Steam.Prefix != "Emu" || c.Steam.Exe != "/bin/cart" {
		t.Fatalf("steam file not applied: %+v", c.Steam)
	}
	if !c.Steam.AllGames || c.Steam.Categories["gba"] != "GBA custom" {
		t.Fatalf("steam naming not applied: %+v", c.Steam)
	}
	t.Setenv(EnvSteamAutoAdd, "0")
	t.Setenv(EnvSteamPrefix, "Retro")
	c, err = Resolve("", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Steam.AutoAdd || c.Steam.Prefix != "Retro" {
		t.Fatalf("steam env not applied: %+v", c.Steam)
	}
	// Round-trip through ToFile/SaveFile.
	f := c.ToFile()
	if f.Steam == nil || f.Steam.Prefix != "Retro" {
		t.Fatalf("ToFile steam = %+v", f.Steam)
	}
}
