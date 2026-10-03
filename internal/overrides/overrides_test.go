package overrides

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	if got := DefaultPath("/x/share/cartridge/library.json"); got != "/x/share/cartridge/overrides.json" {
		t.Fatalf("DefaultPath = %q", got)
	}
	if got := DefaultPath(""); got != "overrides.json" {
		t.Fatalf("DefaultPath empty = %q", got)
	}
}

func TestLoadMissingYieldsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "overrides.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("aaa"); ok {
		t.Fatalf("fresh store must be empty")
	}
}

func TestSetSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overrides.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("aaa", Choice{Recipe: "retroarch-cores", Core: "nestopia_libretro.so"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatalf("store file must not be empty")
	}
	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := r.Get("aaa")
	if !ok || c.Recipe != "retroarch-cores" || c.Core != "nestopia_libretro.so" {
		t.Fatalf("round trip broken: %+v ok=%v", c, ok)
	}
}

func TestNilSafe(t *testing.T) {
	var s *Store
	if _, ok := s.Get("aaa"); ok {
		t.Fatalf("nil Get must miss")
	}
	s.Set("aaa", Choice{Recipe: "x"}) // must not panic
	if err := s.Save(); err != nil {
		t.Fatalf("nil Save must succeed: %v", err)
	}
}
