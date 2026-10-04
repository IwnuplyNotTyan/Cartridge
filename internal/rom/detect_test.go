package rom

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseTitle(t *testing.T) {
	title, region, tags := ParseTitle("Legend of Zelda, The - Link's Awakening DX (USA, Europe)")
	if title != "Legend of Zelda, The - Link's Awakening DX" {
		t.Fatalf("bad title: %q", title)
	}
	if region == "" {
		t.Fatalf("expected region, got empty")
	}
	_ = tags

	title, _, _ = ParseTitle("Legend_of_Zelda,_The_A_Link_to_the_Past_&_Four_Swords_USA")
	if title != "Legend of Zelda, The A Link to the Past & Four Swords USA" {
		t.Fatalf("underscore title not cleaned: %q", title)
	}
}

func TestParseTitleKeepsLeadingPublisher(t *testing.T) {
	for _, tc := range []struct{ in, title, region string }{
		{"[Aladdin] Micro Machines (USA) (Unl)", "[Aladdin] Micro Machines", "USA"},
		{"[Aladdin] Fantastic Adventures of Dizzy, The (USA) (Unl)", "[Aladdin] Fantastic Adventures of Dizzy, The", "USA"},
	} {
		title, region, _ := ParseTitle(tc.in)
		if title != tc.title {
			t.Fatalf("ParseTitle(%q) title = %q, want %q", tc.in, title, tc.title)
		}
		if region != tc.region {
			t.Fatalf("ParseTitle(%q) region = %q, want %q", tc.in, region, tc.region)
		}
	}
}

func TestSniffNES(t *testing.T) {
	head := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0}, 64)...)
	con, ok := sniff(head, ".bin")
	if !ok || con.ID != "nes" {
		t.Fatalf("NES magic not detected: %+v %v", con, ok)
	}
}

func TestSniffGBC(t *testing.T) {
	head := bytes.Repeat([]byte{0}, 0x200)
	copy(head[0x104:], nintendoLogo)
	head[0x143] = 0x80
	con, ok := sniff(head, ".gbc")
	if !ok || con.ID != "gbc" {
		t.Fatalf("GBC not detected: %+v %v", con, ok)
	}
	head[0x143] = 0x00
	con, ok = sniff(head, ".gb")
	if !ok || con.ID != "gb" {
		t.Fatalf("GB not detected: %+v %v", con, ok)
	}
}

func TestIdentifyIDStableAcrossRename(t *testing.T) {
	dir := t.TempDir()
	payload := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 256)...)
	p1 := filepath.Join(dir, "a.nes")
	p2 := filepath.Join(dir, "b.nes")
	if err := os.WriteFile(p1, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	g1, err := Identify(p1)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := Identify(p2)
	if err != nil {
		t.Fatal(err)
	}
	if g1.ID != g2.ID {
		t.Fatalf("ID must not depend on path: %q vs %q", g1.ID, g2.ID)
	}
	sum := sha256.Sum256(payload)
	if g1.ID != hex.EncodeToString(sum[:]) {
		t.Fatalf("ID must be sha256 of content")
	}
	if g1.ConsoleID != "nes" {
		t.Fatalf("expected nes, got %q", g1.ConsoleID)
	}
}

func TestIpsNotDetectedAsGame(t *testing.T) {
	dir := t.TempDir()
	ips := filepath.Join(dir, "hack.ips")
	if err := os.WriteFile(ips, []byte("PATCHdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if SupportedExtension(ips) {
		t.Fatalf(".ips must not be a supported ROM extension")
	}
	if _, err := Identify(ips); err == nil {
		t.Fatalf(".ips Identify must error")
	}
	nes := filepath.Join(dir, "game.nes")
	payload := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 256)...)
	if err := os.WriteFile(nes, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	games, _ := ScanDir(dir, nil)
	if len(games) != 1 || games[0].Path != nes {
		t.Fatalf("ScanDir must skip .ips, got %+v", games)
	}
}

func TestZipWithOnlyModsHasNoRom(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "mods.zip")
	zw, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(zw)
	f, err := w.Create("hack.ips")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("PATCH"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	if _, err := Identify(zp); err == nil {
		t.Fatalf("zip with only .ips must not identify as a game")
	}
	games, _ := ScanDir(dir, nil)
	if len(games) != 0 {
		t.Fatalf("ScanDir must skip mod-only zip, got %+v", games)
	}
}

func TestZipPrefersRomOverMod(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "mixed.zip")
	zw, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(zw)
	payload := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 256)...)
	f, _ := w.Create("game.nes")
	_, _ = f.Write(payload)
	f2, _ := w.Create("hack.ips")
	_, _ = f2.Write(bytes.Repeat([]byte{0xCD}, 512))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	g, err := Identify(zp)
	if err != nil {
		t.Fatal(err)
	}
	if g.ConsoleID != "nes" {
		t.Fatalf("zip must resolve to the ROM, got %+v", g)
	}
}

func makeZip(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	zw, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(zw)
	for name, data := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestZipWithOnlyJunkHasNoRom(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "docs.zip")
	makeZip(t, zp, map[string][]byte{
		"readme.txt": []byte("hello world"),
		"art.nfo":    []byte("nfo"),
	})
	if _, err := Identify(zp); err == nil {
		t.Fatalf("zip with only .txt/.nfo must not identify as a game")
	}
	games, _ := ScanDir(dir, nil)
	if len(games) != 0 {
		t.Fatalf("ScanDir must skip junk-only zip, got %+v", games)
	}
}

func TestZipFallbackKeepsUnknownRomExt(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "homebrew.zip")
	makeZip(t, zp, map[string][]byte{
		"game.bin":   bytes.Repeat([]byte{0xAB}, 512),
		"readme.txt": bytes.Repeat([]byte{0xCD}, 64),
	})
	g, err := Identify(zp)
	if err != nil {
		t.Fatal(err)
	}
	if g.Inner != "game.bin" {
		t.Fatalf("fallback must pick the .bin ROM over readme, got %+v", g)
	}
}

func TestValidGame(t *testing.T) {
	nes := Game{ID: "a", Title: "g", ConsoleID: "nes", ConsoleName: "N", Path: "/x/g.nes"}
	if !ValidGame(nes) {
		t.Fatalf(".nes must be valid")
	}
	zipRom := Game{ID: "b", Path: "/x/b.zip", Inner: "g.nes"}
	if !ValidGame(zipRom) {
		t.Fatalf("zip with ROM inner must be valid")
	}
	for i, g := range []Game{
		{ID: "c", Path: "/x/hack.ips"},
		{ID: "d", Path: "/x/notes.txt"},
		{ID: "e", Path: "/x/b.zip", Inner: "hack.ips"},
		{ID: "f", Path: "/x/b.zip", Inner: "readme.txt"},
		{ID: "g", Path: "/x/game.cht"},
	} {
		if ValidGame(g) {
			t.Fatalf("case %d must be invalid: %+v", i, g)
		}
	}
}

func TestCacheFresh(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.nes")
	payload := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 256)...)
	if err := os.WriteFile(p, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	full := Game{ID: "x", Path: p, FileSize: st.Size(), ModTime: st.ModTime().UnixNano()}
	if !cacheFresh(full, st) {
		t.Fatalf("untouched file must be fresh")
	}
	if cacheFresh(Game{ID: "x", Path: p}, st) {
		t.Fatalf("zero file stats must miss (first scan after upgrade)")
	}
	if cacheFresh(Game{ID: "", Path: p, FileSize: st.Size(), ModTime: st.ModTime().UnixNano()}, st) {
		t.Fatalf("empty ID must miss")
	}
}

func TestScanDirUsesCache(t *testing.T) {
	dir := t.TempDir()
	pa := filepath.Join(dir, "a.nes")
	pb := filepath.Join(dir, "b.sfc")
	payloadA := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 256)...)
	payloadB := bytes.Repeat([]byte{0xCD}, 512)
	if err := os.WriteFile(pa, payloadA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pb, payloadB, 0o644); err != nil {
		t.Fatal(err)
	}

	cold, errs := ScanDir(dir, nil)
	if len(errs) != 0 || len(cold) != 2 {
		t.Fatalf("cold scan = %+v, %v", cold, errs)
	}
	for _, g := range cold {
		if g.FileSize == 0 || g.ModTime == 0 {
			t.Fatalf("cold scan must record file stats: %+v", g)
		}
	}

	// Untouched files reuse the cached entries wholesale, no re-hashing.
	warm, errs := ScanDir(dir, cold)
	if len(errs) != 0 {
		t.Fatalf("warm scan errs: %v", errs)
	}
	if !reflect.DeepEqual(cold, warm) {
		t.Fatalf("warm scan must reuse cache:\ncold=%+v\nwarm=%+v", cold, warm)
	}

	// A changed file is re-hashed (new ID); the other is still reused.
	f, err := os.OpenFile(pa, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0xFF}); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	// A brand-new file appears too.
	pc := filepath.Join(dir, "c.gb")
	if err := os.WriteFile(pc, bytes.Repeat([]byte{0xEE}, 128), 0o644); err != nil {
		t.Fatal(err)
	}

	third, errs := ScanDir(dir, cold)
	if len(errs) != 0 || len(third) != 3 {
		t.Fatalf("third scan = %+v, %v", third, errs)
	}
	byPath := map[string]Game{}
	for _, g := range third {
		byPath[g.Path] = g
	}
	byCold := map[string]Game{}
	for _, g := range cold {
		byCold[g.Path] = g
	}
	if byPath[pa].ID == byCold[pa].ID {
		t.Fatalf("changed file must get a new ID")
	}
	if byPath[pb].ID != byCold[pb].ID {
		t.Fatalf("untouched file must keep its ID")
	}
}

func TestScanDirCacheZip(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "game.zip")
	payload := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 256)...)
	makeZip(t, zp, map[string][]byte{"game.nes": payload})
	cold, errs := ScanDir(dir, nil)
	if len(errs) != 0 || len(cold) != 1 {
		t.Fatalf("cold scan = %+v, %v", cold, errs)
	}
	warm, errs := ScanDir(dir, cold)
	if len(errs) != 0 || !reflect.DeepEqual(cold, warm) {
		t.Fatalf("zipped rom must reuse cache: %+v, %v", cold, warm)
	}
}

func TestScanDirHealsBlankCachedTitles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "[Aladdin] Micro Machines (USA) (Unl).nes")
	payload := append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 256)...)
	if err := os.WriteFile(p, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	cold, errs := ScanDir(dir, nil)
	if len(errs) != 0 || len(cold) != 1 {
		t.Fatalf("cold scan = %+v, %v", cold, errs)
	}
	// Simulate a library.json written while the parser blanked titles:
	// same bytes (ID/size/mtime all match), but Title lost.
	stale := append([]Game(nil), cold...)
	stale[0].Title, stale[0].Region, stale[0].Tags = "", "", nil
	healed, errs := ScanDir(dir, stale)
	if len(errs) != 0 || len(healed) != 1 {
		t.Fatalf("heal scan = %+v, %v", healed, errs)
	}
	if healed[0].ID != cold[0].ID {
		t.Fatalf("healed entry must keep its content ID")
	}
	if healed[0].Title != "[Aladdin] Micro Machines" || healed[0].Region != "USA" {
		t.Fatalf("blank title must heal, got %+v", healed[0])
	}
}

func TestIdentifyNeverBlankTitle(t *testing.T) {
	dir := t.TempDir()
	// A file named exactly ".nes": the stem is empty, the row must not be.
	p := filepath.Join(dir, ".nes")
	if err := os.WriteFile(p, append([]byte{'N', 'E', 'S', 0x1A}, bytes.Repeat([]byte{0xAB}, 64)...), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := Identify(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title == "" {
		t.Fatalf("title must never be blank: %+v", g)
	}
	if games, _ := ScanDir(dir, nil); len(games) != 1 || games[0].Title == "" {
		t.Fatalf("scan must not produce blank rows: %+v", games)
	}
}
