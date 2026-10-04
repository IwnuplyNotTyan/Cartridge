package favorites

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestToggleRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Has("aaa") {
		t.Fatalf("fresh store must be empty")
	}
	if !s.Toggle("aaa") {
		t.Fatalf("toggle must report the new state")
	}
	if !s.Has("aaa") {
		t.Fatalf("toggle must mark")
	}
	if s.Toggle("aaa") {
		t.Fatalf("second toggle must unmark")
	}
	if s.Has("aaa") {
		t.Fatalf("toggle must unmark")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Has("aaa") {
		t.Fatalf("unmarked game must stay unmarked across save/load")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "favorites.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Add("bbb")
	s.Add("aaa")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.IDs(), []string{"aaa", "bbb"}) {
		t.Fatalf("ids = %v", r.IDs())
	}
}

func TestLoadListShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	if err := os.WriteFile(path, []byte(`["aaa", "bbb", ""]`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Has("aaa") || !s.Has("bbb") {
		t.Fatalf("list shape must load: %v", s.IDs())
	}
}

func TestLoadBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	if err := os.WriteFile(path, []byte(`{broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatalf("garbage must error")
	}
}

func TestNilSafe(t *testing.T) {
	var s *Store
	if s.Has("aaa") || s.Toggle("aaa") {
		t.Fatalf("nil store must report false")
	}
	s.Add("aaa")
	s.Remove("aaa")
	if err := s.Save(); err != nil {
		t.Fatalf("nil save must be a no-op: %v", err)
	}
	if s.IDs() != nil {
		t.Fatalf("nil ids must be nil")
	}
	if got := DefaultPath(""); got != "favorites.json" {
		t.Fatalf("empty library path = %q", got)
	}
	if got := DefaultPath("/x/lib.json"); got != filepath.Join("/x", "favorites.json") {
		t.Fatalf("path = %q", got)
	}
}
