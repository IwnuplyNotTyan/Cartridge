package steam

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"cartridge/internal/config"
	"cartridge/internal/rom"
)

// seedCloud writes a minimal cloudstorage dir: one foreign pair, a
// favorite collection and a tombstone. Returns the config dir.
func seedCloud(t *testing.T, favoriteAdded []uint32) string {
	t.Helper()
	cfgDir := t.TempDir()
	dir := filepath.Join(cfgDir, cloudDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	favVal, _ := json.Marshal(map[string]any{
		"id": "favorite", "name": "Favorites", "added": favoriteAdded, "removed": []uint32{},
	})
	pairs := []string{
		`["NewContentRollup_730",{"key":"NewContentRollup_730","timestamp":1700000000,"value":"{}","version":100}]`,
		`["user-collections.favorite",{"key":"user-collections.favorite","timestamp":1700000001,"value":` + strconvJSON(string(favVal)) + `,"version":101}]`,
		`["user-collections.uc-deadbeef0001",{"key":"user-collections.uc-deadbeef0001","timestamp":1700000002,"version":102,"is_deleted":true}]`,
	}
	raw := "[" + strings.Join(pairs, ",") + "]"
	if err := os.WriteFile(filepath.Join(dir, cloudNamespace), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cloudNamespaces), []byte(`[[3,"0"],[1,"102"]]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cloud-storage-namespace-1.modified.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgDir
}

func strconvJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func readNS(t *testing.T, cfgDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cfgDir, cloudDirName, cloudNamespace))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func collByName(t *testing.T, cfgDir, name string) (CollectionValue, bool) {
	t.Helper()
	infos, err := ReadCollections(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range infos {
		if strings.EqualFold(in.Name, name) {
			nsPath, _, _ := cloudPaths(cfgDir)
			pairs, err := loadNamespace(nsPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range pairs {
				if p.Key == in.Key {
					e, _, err := parseEntry(p.Raw)
					if err != nil {
						t.Fatal(err)
					}
					cv, _, err := parseCollectionValue(e.Value)
					if err != nil {
						t.Fatal(err)
					}
					return cv, true
				}
			}
		}
	}
	return CollectionValue{}, false
}

func TestCollectionsCreateAndIdempotent(t *testing.T) {
	cfgDir := seedCloud(t, []uint32{730})
	managed := map[uint32]bool{3000000001: true, 3000000002: true}
	current := map[uint32]bool{3000000001: true, 3000000002: true}
	want := map[string][]uint32{
		"Cartridge":       {3000000001, 3000000002},
		"Cartridge - GBA": {3000000001},
		"Cartridge - NES": {3000000002},
	}
	res, err := SyncCollections(cfgDir, want, managed, current, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Changed != 3 || res.Total != 3 {
		t.Fatalf("res = %+v", res)
	}
	for name, ids := range want {
		cv, ok := collByName(t, cfgDir, name)
		if !ok {
			t.Fatalf("collection %q missing", name)
		}
		if !equalU32(cv.Added, ids) {
			t.Fatalf("%q added = %v, want %v", name, cv.Added, ids)
		}
		if cv.Filter == nil {
			t.Fatalf("%q must carry the manual filterSpec", name)
		}
	}
	// Foreign pairs byte-identical.
	after := readNS(t, cfgDir)
	if !strings.Contains(after, `"NewContentRollup_730"`) {
		t.Fatalf("foreign entry lost")
	}
	for _, frag := range []string{
		`"user-collections.uc-deadbeef0001"`,
		`"version":101`,
	} {
		if !strings.Contains(after, frag) {
			t.Fatalf("untouched bytes changed, missing %s", frag)
		}
	}
	// Favorite untouched even though it shares no appids.
	cv, _ := collByName(t, cfgDir, "Favorites")
	if !equalU32(cv.Added, []uint32{730}) {
		t.Fatalf("favorite touched: %v", cv.Added)
	}
	// Second sync: no changes, no version churn.
	snap := readNS(t, cfgDir)
	res2, err := SyncCollections(cfgDir, want, managed, current, false)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed != 0 {
		t.Fatalf("resync changed %d", res2.Changed)
	}
	if readNS(t, cfgDir) != snap {
		t.Fatalf("resync rewrote the file")
	}
}

func TestCollectionsMergeForeignAndPrune(t *testing.T) {
	cfgDir := seedCloud(t, nil)
	managed := map[uint32]bool{3000000001: true, 3000000002: true, 3000000009: true}
	current := map[uint32]bool{3000000001: true, 3000000002: true}
	// Pre-existing user collection with same name + foreign member.
	nsPath, _, _ := cloudPaths(cfgDir)
	pairs, err := loadNamespace(nsPath)
	if err != nil {
		t.Fatal(err)
	}
	val := encodeCollectionValue(CollectionValue{
		ID: "uc-user00000001", Name: "Cartridge - GBA",
		Added: []uint32{999, 3000000009}, Removed: []uint32{},
		Filter: json.RawMessage(emptyFilterSpec),
	}, map[string]json.RawMessage{
		"conflictResolutionMethod": json.RawMessage(`"custom"`),
		"strMethodId":              json.RawMessage(`"union-collections"`),
	})
	pairs = append(pairs, nsPair{Key: "user-collections.uc-user00000001", Raw: encodeEntry(cloudEntry{
		Key: "user-collections.uc-user00000001", Timestamp: 1700000003,
		Value: val, Version: cloudVersion{N: 103, Quoted: true},
	}, map[string]json.RawMessage{
		"conflictResolutionMethod": json.RawMessage(`"custom"`),
		"strMethodId":              json.RawMessage(`"union-collections"`),
	})})
	if err := saveNamespace(nsPath, pairs); err != nil {
		t.Fatal(err)
	}
	want := map[string][]uint32{"Cartridge - GBA": {3000000001}}
	// Plain sync: stale 3000000009 stays (no prune), ours merge, foreign kept.
	if _, err := SyncCollections(cfgDir, want, managed, current, false); err != nil {
		t.Fatal(err)
	}
	cv, _ := collByName(t, cfgDir, "Cartridge - GBA")
	if !equalU32(cv.Added, []uint32{999, 3000000001, 3000000009}) {
		t.Fatalf("merge = %v", cv.Added)
	}
	// Prune: stale 3000000009 dropped, foreign 999 kept.
	if _, err := SyncCollections(cfgDir, want, managed, current, true); err != nil {
		t.Fatal(err)
	}
	cv, _ = collByName(t, cfgDir, "Cartridge - GBA")
	if !equalU32(cv.Added, []uint32{999, 3000000001}) {
		t.Fatalf("prune = %v", cv.Added)
	}
}

func TestCollectionsResetTombstonesOursKeepsForeign(t *testing.T) {
	cfgDir := seedCloud(t, []uint32{730, 3000000001})
	managed := map[uint32]bool{3000000001: true, 3000000002: true}
	current := map[uint32]bool{3000000001: true, 3000000002: true}
	want := map[string][]uint32{
		"Cartridge":       {3000000001, 3000000002},
		"Cartridge - GBA": {3000000001, 3000000002}, // solely ours
	}
	if _, err := SyncCollections(cfgDir, want, managed, current, false); err != nil {
		t.Fatal(err)
	}
	// Favorite holds our appid too: reset must NOT unfavorite.
	res, err := ResetCollections(cfgDir, managed, managed)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed == 0 {
		t.Fatalf("reset changed nothing")
	}
	// Ours-only collection tombstoned.
	nsPath, _, _ := cloudPaths(cfgDir)
	pairs, err := loadNamespace(nsPath)
	if err != nil {
		t.Fatal(err)
	}
	tombed := map[string]bool{}
	for _, p := range pairs {
		if isSteamOwned(p.Key) {
			continue // Steam-owned: never touched, even holding our appids
		}
		e, _, err := parseEntry(p.Raw)
		if err != nil {
			t.Fatal(err)
		}
		if e.Deleted {
			tombed[p.Key] = true
		}
		if e.Value == "" || e.Deleted {
			continue
		}
		cv, _, err := parseCollectionValue(e.Value)
		if err != nil {
			continue
		}
		for _, a := range cv.Added {
			if a == 3000000001 || a == 3000000002 {
				t.Fatalf("appid %d still in %q", a, cv.Name)
			}
		}
	}
	if len(tombed) == 0 {
		t.Fatalf("expected tombstones for emptied ours-only collections")
	}
	// Favorite untouched (still has 730 and our appid: Steam-owned).
	cv, _ := collByName(t, cfgDir, "Favorites")
	if !equalU32(cv.Added, []uint32{730, 3000000001}) {
		t.Fatalf("favorite touched: %v", cv.Added)
	}
}

func TestCollectionsVersionAndModified(t *testing.T) {
	cfgDir := seedCloud(t, nil)
	// Newer clients quote the namespace id (["1","102"]); older ones use
	// a bare number ([1,"102"]). Both must update the mark.
	ns1, ns2, _ := cloudPaths(cfgDir)
	if err := os.WriteFile(ns2, []byte(`[["3","0"],["1","102"]]`), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = ns1
	managed := map[uint32]bool{3000000001: true}
	current := map[uint32]bool{3000000001: true}
	want := map[string][]uint32{"Cartridge": {3000000001}}
	if _, err := SyncCollections(cfgDir, want, managed, current, false); err != nil {
		t.Fatal(err)
	}
	nsPath, namespacesPath, modifiedPath := cloudPaths(cfgDir)
	marks, err := loadVersionMark(namespacesPath)
	if err != nil {
		t.Fatal(err)
	}
	top := int64(0)
	for _, m := range marks {
		if markID(m.Key) == "1" {
			var s string
			if err := json.Unmarshal(m.Raw, &s); err != nil {
				t.Fatal(err)
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				t.Fatalf("mark not numeric: %q", s)
			}
			top = n
		}
	}
	if top <= 102 {
		t.Fatalf("version mark not bumped: %d", top)
	}
	keys, err := loadModified(modifiedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 || !strings.HasPrefix(keys[0], "user-collections.uc-") {
		t.Fatalf("modified.json = %v", keys)
	}
	_ = nsPath
}

func TestCollectionsSkippedWithoutCloudDir(t *testing.T) {
	cfgDir := t.TempDir() // no cloudstorage subdir
	res, err := SyncCollections(cfgDir, map[string][]uint32{"X": {1}}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Fatalf("expected skip, got %+v", res)
	}
}

func TestTrackedRenameSweepsOldCollection(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "u", "config")
	cloud := filepath.Join(cfgDir, cloudDirName)
	if err := os.MkdirAll(cloud, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		cloudNamespace:  "[]",
		cloudNamespaces: `[["1","0"]]`,
		"cloud-storage-namespace-1.modified.json": "[]",
	} {
		if err := os.WriteFile(filepath.Join(cloud, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	shortcuts := filepath.Join(cfgDir, "shortcuts.vdf")
	track := filepath.Join(dir, "lib", "steam-collections.json")
	cfg := config.DefaultSteamConfig()
	exe := filepath.Join(dir, "cartridge")
	games := []rom.Game{testGame("id1", "Zelda", "gba", "Game Boy Advance")}
	if _, err := SyncFileTracked(shortcuts, games, cfg, exe, dir, false, "7", track); err != nil {
		t.Fatal(err)
	}
	old, ok := collByName(t, cfgDir, "Cartridge - GBA")
	if !ok || len(old.Added) != 1 {
		t.Fatalf("initial collection wrong: %+v", old)
	}
	// Rename via custom name + {name} template, then resync.
	cfg.CategoryTemplate = "{prefix} - {name}"
	cfg.Categories = map[string]string{"gba": "ГБА"}
	res, err := SyncFileTracked(shortcuts, games, cfg, exe, dir, false, "7", track)
	if err != nil {
		t.Fatal(err)
	}
	if res.CollChanged == 0 {
		t.Fatalf("rename changed nothing")
	}
	neu, ok := collByName(t, cfgDir, "Cartridge - ГБА")
	if !ok || !equalU32(neu.Added, old.Added) {
		t.Fatalf("renamed collection wrong: %+v", neu)
	}
	// Old collection tombstoned (held only our game).
	nsPath, _, _ := cloudPaths(cfgDir)
	pairs, err := loadNamespace(nsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pairs {
		e, _, err := parseEntry(p.Raw)
		if err != nil {
			t.Fatal(err)
		}
		if e.Deleted || e.Value == "" {
			continue
		}
		cv, _, err := parseCollectionValue(e.Value)
		if err != nil {
			continue
		}
		if strings.EqualFold(cv.Name, "Cartridge - GBA") {
			t.Fatalf("old collection still live: %+v", cv)
		}
	}
}

// A renamed category with a lost track file leaves an ours-only
// collection whose name is no longer wanted but whose members are
// current appids — no stale-drop can hit it. Prune must retire it,
// while a mixed user collection keeps every member.
func TestCollectionsPruneRetiresOrphanedOursOnly(t *testing.T) {
	cfgDir := seedCloud(t, nil)
	nsPath, _, _ := cloudPaths(cfgDir)
	pairs, err := loadNamespace(nsPath)
	if err != nil {
		t.Fatal(err)
	}
	mkExtra := map[string]json.RawMessage{
		"conflictResolutionMethod": json.RawMessage(`"custom"`),
		"strMethodId":              json.RawMessage(`"union-collections"`),
	}
	mkVal := func(id, name string, added []uint32) string {
		return encodeCollectionValue(CollectionValue{
			ID: id, Name: name,
			Added: added, Removed: []uint32{},
			Filter: json.RawMessage(emptyFilterSpec),
		}, mkExtra)
	}
	addColl := func(key, id, name string, added []uint32, ver int64) {
		pairs = append(pairs, nsPair{Key: key, Raw: encodeEntry(cloudEntry{
			Key: key, Timestamp: 1700000003,
			Value:   mkVal(id, name, added),
			Version: cloudVersion{N: ver, Quoted: true},
		}, mkExtra)})
	}
	addColl("user-collections.uc-phantomnes01", "uc-phantomnes01", "Cartridge - NES", []uint32{3000000001}, 103)
	addColl("user-collections.uc-userset00001", "uc-userset00001", "Моя подборка", []uint32{999, 3000000001}, 104)
	if err := saveNamespace(nsPath, pairs); err != nil {
		t.Fatal(err)
	}
	managed := map[uint32]bool{3000000001: true}
	current := map[uint32]bool{3000000001: true}
	res, err := SyncCollections(cfgDir, map[string][]uint32{"Cartridge - GBA": {3000000001}}, managed, current, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed != 2 {
		t.Fatalf("changed = %d, want 2 (phantom retired + GBA created)", res.Changed)
	}
	infos, err := ReadCollections(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range infos {
		if strings.EqualFold(in.Name, "Cartridge - NES") {
			t.Fatalf("phantom collection still live: %+v", in)
		}
		if in.Name == "Моя подборка" && in.Members != 2 {
			t.Fatalf("user collection touched: %+v", in)
		}
	}
	cv, ok := collByName(t, cfgDir, "Cartridge - GBA")
	if !ok || !equalU32(cv.Added, []uint32{3000000001}) {
		t.Fatalf("want collection wrong: %+v %v", cv, ok)
	}
	// Second prune is a no-op.
	snap := readNS(t, cfgDir)
	res2, err := SyncCollections(cfgDir, map[string][]uint32{"Cartridge - GBA": {3000000001}}, managed, current, true)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed != 0 {
		t.Fatalf("second prune changed %d", res2.Changed)
	}
	if readNS(t, cfgDir) != snap {
		t.Fatalf("second prune rewrote the file")
	}
}

func TestCollectionsDynamicGuard(t *testing.T) {
	cfgDir := seedCloud(t, nil)
	nsPath, _, _ := cloudPaths(cfgDir)
	pairs, err := loadNamespace(nsPath)
	if err != nil {
		t.Fatal(err)
	}
	dynFilter := `{"nFormatVersion":2,"strSearchText":"","filterGroups":[{"rgOptions":[{"foo":1}],"bAcceptUnion":true}],"setSuggestions":{}}`
	val := encodeCollectionValue(CollectionValue{
		ID: "uc-dyn000000001", Name: "Cartridge",
		Added: []uint32{555}, Removed: []uint32{},
		Filter: json.RawMessage(dynFilter),
	}, nil)
	pairs = append(pairs, nsPair{Key: "user-collections.uc-dyn000000001", Raw: encodeEntry(cloudEntry{
		Key: "user-collections.uc-dyn000000001", Timestamp: 1700000004,
		Value: val, Version: cloudVersion{N: 103, Quoted: true},
	}, nil)})
	if err := saveNamespace(nsPath, pairs); err != nil {
		t.Fatal(err)
	}
	managed := map[uint32]bool{3000000001: true}
	current := map[uint32]bool{3000000001: true}
	res, err := SyncCollections(cfgDir, map[string][]uint32{"Cartridge": {3000000001}}, managed, current, false)
	if err != nil {
		t.Fatal(err)
	}
	// Dynamic collection with our name must be left alone; a new static
	// collection must NOT be created (name taken by the dynamic one).
	if res.Changed != 0 {
		t.Fatalf("dynamic guard failed, changed %d", res.Changed)
	}
	cv, _ := collByName(t, cfgDir, "Cartridge")
	if !equalU32(cv.Added, []uint32{555}) {
		t.Fatalf("dynamic collection touched: %v", cv.Added)
	}
}
