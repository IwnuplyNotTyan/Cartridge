package playtime

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAccumulateAndPersist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "playtime.json")
	s, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Add("abc", 90*time.Second)
	s.Add("abc", 30*time.Second)
	st, ok := s.Get("abc")
	if !ok {
		t.Fatalf("missing stats")
	}
	if st.Sessions != 2 || st.TotalSeconds != 120 {
		t.Fatalf("stats = %+v", st)
	}
	if st.LastPlayed.IsZero() {
		t.Fatalf("LastPlayed not set")
	}
	if _, ok := s.Get("nope"); ok {
		t.Fatalf("unexpected stats for unknown id")
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	st, ok = again.Get("abc")
	if !ok || st.Sessions != 2 || st.TotalSeconds != 120 {
		t.Fatalf("persisted stats = %+v ok=%v", st, ok)
	}
}

func TestDefaultPath(t *testing.T) {
	if got := DefaultPath("/x/share/cartridge/library.json"); got != "/x/share/cartridge/playtime.json" {
		t.Fatalf("DefaultPath = %q", got)
	}
}
