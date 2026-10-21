package steam

import (
	"testing"

	"cartridge/internal/config"
	"cartridge/internal/rom"
)

func TestQuoteSteamPath(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		quote bool
		want  string
	}{
		{"windows plain", `C:\Games\cartridge.exe`, true, `"C:\Games\cartridge.exe"`},
		{"windows already quoted", `"C:\Games\cartridge.exe"`, true, `"C:\Games\cartridge.exe"`},
		{"unix plain", `/usr/bin/cartridge`, false, `/usr/bin/cartridge`},
		{"empty", "", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := quoteSteamPath(tc.in, tc.quote); got != tc.want {
				t.Fatalf("quoteSteamPath(%q, %v) = %q, want %q", tc.in, tc.quote, got, tc.want)
			}
		})
	}
}

func TestBuildShortcutQuotesWindowsPaths(t *testing.T) {
	g := rom.Game{ID: "id1", Title: "Zelda", ConsoleID: "gba"}
	cfg := config.DefaultSteamConfig()
	sc := buildShortcut(g, cfg, `C:\Games\cartridge.exe`, `C:\Games`, true)
	if sc.Exe != `"C:\Games\cartridge.exe"` {
		t.Fatalf("Exe = %q, want quoted windows path", sc.Exe)
	}
	if sc.StartDir != `"C:\Games"` {
		t.Fatalf("StartDir = %q, want quoted windows path", sc.StartDir)
	}
	if sc.AppID != AppIDFor(sc.Exe, sc.AppName) {
		t.Fatalf("AppID must hash the stored (quoted) exe")
	}
}

func TestExeKeyIgnoresQuotes(t *testing.T) {
	if exeKey(`"C:\Games\cartridge.exe"`) != exeKey(`C:\Games\cartridge.exe`) {
		t.Fatalf("quoted and unquoted exe must match")
	}
}
