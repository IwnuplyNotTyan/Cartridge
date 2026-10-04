package library

import (
	"testing"

	"cartridge/internal/rom"
)

func TestMergePrunesInvalid(t *testing.T) {
	stored := []rom.Game{
		{ID: "keep", Title: "B", ConsoleID: "nes", ConsoleName: "N", Path: "/x/b.nes"},
		{ID: "ghost-ips", Title: "hack", ConsoleID: "unknown", ConsoleName: "Unknown", Path: "/x/hack.ips"},
		{ID: "ghost-txt", Title: "readme", ConsoleID: "unknown", ConsoleName: "Unknown", Path: "/x/b.zip", Inner: "readme.txt"},
	}
	scanned := []rom.Game{
		{ID: "keep", Title: "B", ConsoleID: "nes", ConsoleName: "N", Path: "/y/b.nes"},
		{ID: "new", Title: "A", ConsoleID: "nes", ConsoleName: "N", Path: "/y/a.nes"},
	}
	got := Merge(stored, scanned)
	if len(got) != 2 {
		t.Fatalf("Merge must drop ghosts, got %+v", got)
	}
	byID := map[string]rom.Game{}
	for _, g := range got {
		byID[g.ID] = g
	}
	if byID["keep"].Path != "/y/b.nes" {
		t.Fatalf("re-discovered ID must update path: %+v", byID["keep"])
	}
	if _, ok := byID["new"]; !ok {
		t.Fatalf("scanned game missing: %+v", got)
	}
}
