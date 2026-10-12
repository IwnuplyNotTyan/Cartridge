package steam

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
)

// CollectionValue is the decoded "value" of a user-collections entry:
// {"id":"uc-…","name":"…","added":[…],"removed":[…],…}.
type CollectionValue struct {
	ID      string                     `json:"id"`
	Name    string                     `json:"name"`
	Added   []uint32                   `json:"added"`
	Removed []uint32                   `json:"removed"`
	Filter  json.RawMessage            `json:"filterSpec,omitempty"`
	Extra   map[string]json.RawMessage `json:"-"`
}

// parseCollectionValue decodes a value string leniently: entries with
// non-integer members are reported as unparsable (caller preserves them).
func parseCollectionValue(s string) (CollectionValue, map[string]json.RawMessage, error) {
	var cv CollectionValue
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil {
		return cv, nil, err
	}
	get := func(k string, dst any) error {
		if r, ok := m[k]; ok {
			if err := json.Unmarshal(r, dst); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			delete(m, k)
		}
		return nil
	}
	if err := get("id", &cv.ID); err != nil {
		return cv, nil, err
	}
	if err := get("name", &cv.Name); err != nil {
		return cv, nil, err
	}
	if err := get("added", &cv.Added); err != nil {
		return cv, nil, err
	}
	if err := get("removed", &cv.Removed); err != nil {
		return cv, nil, err
	}
	if r, ok := m["filterSpec"]; ok {
		cv.Filter = r
		delete(m, "filterSpec")
	}
	if cv.Added == nil {
		cv.Added = []uint32{}
	}
	if cv.Removed == nil {
		cv.Removed = []uint32{}
	}
	return cv, m, nil
}

// encodeCollectionValue marshals in Steam's field order:
// id, name, added, removed, filterSpec, extras.
func encodeCollectionValue(cv CollectionValue, extra map[string]json.RawMessage) string {
	var buf strings.Builder
	buf.WriteByte('{')
	first := true
	write := func(k string, v []byte) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(v)
	}
	ib, _ := json.Marshal(cv.ID)
	write("id", ib)
	nb, _ := json.Marshal(cv.Name)
	write("name", nb)
	ab, _ := json.Marshal(cv.Added)
	write("added", ab)
	rb, _ := json.Marshal(cv.Removed)
	write("removed", rb)
	if len(cv.Filter) > 0 {
		write("filterSpec", cv.Filter)
	}
	for _, k := range sortedKeys(extra) {
		write(k, extra[k])
	}
	buf.WriteByte('}')
	return buf.String()
}

// emptyFilterSpec is the verbatim filterSpec of a manual (static)
// collection, copied from a live Steam client. Non-empty filterGroups
// or strSearchText would mean a dynamic (filtered) collection, which
// we never touch.
const emptyFilterSpec = `{"nFormatVersion":2,"strSearchText":"","filterGroups":[{"rgOptions":[],"bAcceptUnion":true},{"rgOptions":[],"bAcceptUnion":false},{"rgOptions":[],"bAcceptUnion":false},{"rgOptions":[],"bAcceptUnion":false},{"rgOptions":[],"bAcceptUnion":false},{"rgOptions":[],"bAcceptUnion":false},{"rgOptions":[],"bAcceptUnion":false},{"rgOptions":[],"bAcceptUnion":false},{"rgOptions":[],"bAcceptUnion":false}],"setSuggestions":{}}`

// isSteamOwned reports fixed-ID collections we must never modify:
// Favorites, Hidden and tag-derived ones.
func isSteamOwned(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	return k == "user-collections.favorite" ||
		k == "user-collections.hidden" ||
		strings.HasPrefix(k, "user-collections.from-tag-")
}

// isDynamic reports whether a collection value carries a saved filter.
func isDynamic(rawFilter json.RawMessage) bool {
	if len(rawFilter) == 0 {
		return false
	}
	var f struct {
		Search string `json:"strSearchText"`
		Groups []struct {
			Options []json.RawMessage `json:"rgOptions"`
		} `json:"filterGroups"`
	}
	if err := json.Unmarshal(rawFilter, &f); err != nil {
		return true // unknown shape: hands off
	}
	if strings.TrimSpace(f.Search) != "" {
		return true
	}
	for _, g := range f.Groups {
		if len(g.Options) > 0 {
			return true
		}
	}
	return false
}

// CollectionInfo describes one in-Steam collection for status output.
type CollectionInfo struct {
	Key     string
	Name    string
	Members int
	Dynamic bool
}

// ReadCollections lists user collections stored in cfgDir's cloud file.
// Missing cloudstorage yields no collections and no error.
func ReadCollections(cfgDir string) ([]CollectionInfo, error) {
	nsPath, _, _ := cloudPaths(cfgDir)
	pairs, err := loadNamespace(nsPath)
	if err != nil {
		return nil, err
	}
	var out []CollectionInfo
	for _, p := range pairs {
		if !strings.HasPrefix(p.Key, "user-collections.") {
			continue
		}
		e, _, err := parseEntry(p.Raw)
		if err != nil || e.Deleted || e.Value == "" {
			continue
		}
		cv, _, err := parseCollectionValue(e.Value)
		if err != nil {
			continue
		}
		out = append(out, CollectionInfo{
			Key:     p.Key,
			Name:    cv.Name,
			Members: len(cv.Added),
			Dynamic: isDynamic(cv.Filter),
		})
	}
	return out, nil
}

// ucAlphabet mimics Steam's own collection IDs (base64-ish with *+).
const ucAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789*+"

// newCollectionID generates "uc-" + 12 random chars.
func newCollectionID(existing map[string]bool) string {
	for {
		var b strings.Builder
		b.WriteString("uc-")
		for i := 0; i < 12; i++ {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(ucAlphabet))))
			if err != nil {
				return fmt.Sprintf("uc-cartridge-%d", nowTS())
			}
			b.WriteByte(ucAlphabet[n.Int64()])
		}
		key := "user-collections." + b.String()
		if !existing[key] {
			return b.String()
		}
	}
}

// CollectionResult reports what a collections sync changed per target.
type CollectionResult struct {
	UserID  string
	Path    string // cloud namespace file
	Changed int    // entries created, modified or tombstoned
	Total   int    // our collections present afterwards
	Skipped bool   // no cloudstorage (legacy client)
	// Keys maps synced category names to cloud collection keys.
	Keys map[string]string `json:"-"`
}

// collSlot points at one live collection inside the namespace doc.
type collSlot struct {
	idx     int
	entry   cloudEntry
	extra   map[string]json.RawMessage
	cv      CollectionValue
	vextra  map[string]json.RawMessage
	dynamic bool
}

// replacePair swaps (or appends) the encoded entry for key.
func replacePair(pairs []nsPair, s *collSlot, key string, entry cloudEntry, extra map[string]json.RawMessage, found bool) []nsPair {
	raw := encodeEntry(entry, extra)
	if found {
		pairs[s.idx].Raw = raw
		return pairs
	}
	return append(pairs, nsPair{Key: key, Raw: raw})
}

// reconcileCollections is the single merge engine behind sync, prune and
// reset. want maps category name → current appids (nil for pure
// prune/reset passes). drop holds appids to remove from merged entries
// (stale ours for prune, everything ours for reset). ours holds every
// cartridge appid of this target (managed + current) and, with visitAll,
// retires orphaned ours-only collections whose name is no longer wanted:
// a renamed category with a lost track file would otherwise haunt Steam
// forever, because its members are current appids no stale-drop can hit.
// With visitAll, non-want collections are swept for drop appids too.
// sweep lists extra collection keys (previously ours, e.g. renamed)
// cleaned of sweepDrop appids. A collection that ends up empty is
// tombstoned only when every previous member is in the applied drop set —
// i.e. it existed solely because of our games. Already-empty shells (0
// games, name not wanted) are tombstoned on sweep/prune/reset outright:
// no drop set can hit them, so otherwise they linger forever. Foreign
// members, Steam-owned entries (favorite/hidden/from-tag-*) and dynamic
// (filtered) collections are never touched.
func reconcileCollections(cfgDir string, want map[string][]uint32, drop map[uint32]bool, visitAll bool, sweep map[string]bool, sweepDrop map[uint32]bool, ours map[uint32]bool) (CollectionResult, error) {
	var res CollectionResult
	res.Keys = map[string]string{}
	nsPath, namespacesPath, modifiedPath := cloudPaths(cfgDir)
	if st, err := os.Stat(cfgDir + "/" + cloudDirName); err != nil || !st.IsDir() {
		res.Skipped = true
		return res, nil
	}
	pairs, err := loadNamespace(nsPath)
	if err != nil {
		return res, err
	}
	existing := map[string]bool{}
	for _, p := range pairs {
		existing[p.Key] = true
	}
	byName := map[string]*collSlot{}
	maxV := int64(0)
	for i, p := range pairs {
		e, extra, err := parseEntry(p.Raw)
		if err != nil {
			continue
		}
		if e.Version.N > maxV {
			maxV = e.Version.N
		}
		if !strings.HasPrefix(p.Key, "user-collections.") || e.Deleted || e.Value == "" {
			continue
		}
		cv, vextra, err := parseCollectionValue(e.Value)
		if err != nil {
			continue
		}
		if isSteamOwned(p.Key) {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(cv.Name))
		if name == "" {
			continue
		}
		if _, dup := byName[name]; !dup {
			byName[name] = &collSlot{idx: i, entry: e, extra: extra, cv: cv, vextra: vextra, dynamic: isDynamic(cv.Filter)}
		}
	}
	touch := func() int64 { maxV++; return maxV }
	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	changedKeys := map[string]bool{}
	visited := map[int]bool{}
	for _, name := range names {
		add := map[uint32]bool{}
		for _, a := range want[name] {
			add[a] = true
		}
		s, found := byName[strings.ToLower(strings.TrimSpace(name))]
		if found && s.dynamic {
			continue // name taken by a dynamic (filtered) collection: hands off
		}
		var cv CollectionValue
		var vextra map[string]json.RawMessage
		var entry cloudEntry
		var extra map[string]json.RawMessage
		key := ""
		if found {
			cv, vextra, entry, extra = s.cv, s.vextra, s.entry, s.extra
			key = pairs[s.idx].Key
		} else {
			id := newCollectionID(existing)
			key = "user-collections." + id
			existing[key] = true
			cv = CollectionValue{ID: id, Name: name, Filter: json.RawMessage(emptyFilterSpec)}
			entry = cloudEntry{Key: key, Version: cloudVersion{Quoted: true}}
			extra = map[string]json.RawMessage{
				"conflictResolutionMethod": json.RawMessage(`"custom"`),
				"strMethodId":              json.RawMessage(`"union-collections"`),
			}
		}
		keep := map[uint32]bool{}
		for _, a := range cv.Added {
			if drop[a] {
				continue
			}
			keep[a] = true
		}
		for a := range add {
			keep[a] = true
		}
		rem := map[uint32]bool{}
		for _, a := range cv.Removed {
			if !drop[a] {
				rem[a] = true
			}
		}
		merged := sortedU32(keep)
		mergedRem := sortedU32(rem)
		res.Keys[name] = key
		if equalU32(merged, cv.Added) && equalU32(mergedRem, cv.Removed) && found {
			visited[s.idx] = true
			continue // byte-identical: no churn, no version bump
		}
		cv.Added, cv.Removed = merged, mergedRem
		entry.Value = encodeCollectionValue(cv, vextra)
		entry.Timestamp = nowTS()
		entry.Version.N = touch()
		pairs = replacePair(pairs, s, key, entry, extra, found)
		if found {
			visited[s.idx] = true
		} else {
			visited[len(pairs)-1] = true
		}
		changedKeys[key] = true
		res.Changed++
	}
	if visitAll || len(sweep) > 0 {
		wantLower := make(map[string]bool, len(want))
		for n := range want {
			wantLower[strings.ToLower(strings.TrimSpace(n))] = true
		}
		for i, p := range pairs {
			if visited[i] || !strings.HasPrefix(p.Key, "user-collections.") || isSteamOwned(p.Key) {
				continue
			}
			e, extra, err := parseEntry(p.Raw)
			if err != nil || e.Deleted || e.Value == "" {
				continue
			}
			cv, vextra, err := parseCollectionValue(e.Value)
			if err != nil || isDynamic(cv.Filter) {
				continue
			}
			if wantLower[strings.ToLower(strings.TrimSpace(cv.Name))] {
				continue // name still wanted (duplicate or dynamic-guarded): not an orphan
			}
			if len(cv.Added) == 0 {
				// Empty shell (0 games): no members for any drop set to
				// hit, so without this it lingers forever — the exact
				// "Cartridge - NES / 0 games" phantom (Steam itself
				// drops dangling members when shortcuts vanish, keeping
				// the empty shell). Retire it when it's tracked-ours
				// (sweep) or when pruning/resetting (visitAll); plain
				// syncs leave untracked empties alone. Nothing to lose:
				// an empty collection holds no games.
				if !sweep[p.Key] && !visitAll {
					continue
				}
				maxV++
				pairs[i].Raw = encodeEntry(cloudEntry{
					Key:       p.Key,
					Timestamp: nowTS(),
					Version:   cloudVersion{N: maxV, Quoted: true},
					Deleted:   true,
				}, nil)
				changedKeys[p.Key] = true
				res.Changed++
				continue
			}
			// Per-entry drop set: sweep entries (renamed ours) are
			// cleaned of all our appids, others only of stale ones —
			// except orphaned ours-only collections (every member in
			// ours, name no longer wanted): prune retires those
			// outright, otherwise a lost track file leaves a phantom
			// no relink or stale-drop can ever remove. Mixed user
			// collections (with foreign members) still lose stale
			// appids only.
			entryDrop := drop
			if sweep[p.Key] {
				entryDrop = sweepDrop
			} else if !visitAll {
				continue
			} else if len(cv.Added) > 0 && len(ours) > 0 {
				orphaned := true
				for _, a := range cv.Added {
					if !ours[a] {
						orphaned = false
						break
					}
				}
				if orphaned {
					entryDrop = ours
				}
			}
			kept := make([]uint32, 0, len(cv.Added))
			allOurs := true
			hit := false
			for _, a := range cv.Added {
				if entryDrop[a] {
					hit = true
					continue
				}
				allOurs = false
				kept = append(kept, a)
			}
			if !hit {
				continue
			}
			rem := make([]uint32, 0, len(cv.Removed))
			for _, a := range cv.Removed {
				if !entryDrop[a] {
					rem = append(rem, a)
				}
			}
			maxV++
			if len(kept) == 0 && allOurs {
				pairs[i].Raw = encodeEntry(cloudEntry{
					Key:       p.Key,
					Timestamp: nowTS(),
					Version:   cloudVersion{N: maxV, Quoted: true},
					Deleted:   true,
				}, nil)
			} else {
				cv.Added = kept
				cv.Removed = rem
				e.Value = encodeCollectionValue(cv, vextra)
				e.Timestamp = nowTS()
				e.Version.N = maxV
				pairs[i].Raw = encodeEntry(e, extra)
			}
			changedKeys[p.Key] = true
			res.Changed++
		}
	}
	if len(changedKeys) == 0 {
		res.Total = len(names)
		return res, nil
	}
	if err := writeCloudFiles(nsPath, namespacesPath, modifiedPath, pairs, maxV, changedKeys); err != nil {
		return res, err
	}
	res.Path = nsPath
	res.Total = len(names)
	return res, nil
}

// SyncCollections ensures one Steam collection per category in want,
// each holding our current appids merged with foreign members.
// managed holds cartridge appids known from this target's shortcuts.vdf
// (current + stale), current holds appids of games in the library now.
// With prune, managed appids that are no longer current are dropped.
func SyncCollections(cfgDir string, want map[string][]uint32, managed, current map[uint32]bool, prune bool) (CollectionResult, error) {
	drop := map[uint32]bool{}
	ours := map[uint32]bool{}
	for a := range managed {
		ours[a] = true
		if !current[a] && prune {
			drop[a] = true
		}
	}
	for a := range current {
		ours[a] = true
	}
	return reconcileCollections(cfgDir, want, drop, prune, nil, nil, ours)
}

// ResetCollections removes appids from every user collection except
// Steam-owned ones, tombstoning collections that held only our games.
func ResetCollections(cfgDir string, remove, managed map[uint32]bool) (CollectionResult, error) {
	drop := map[uint32]bool{}
	for a := range remove {
		drop[a] = true
	}
	for a := range managed {
		drop[a] = true
	}
	return reconcileCollections(cfgDir, nil, drop, true, nil, nil, nil)
}

// collectionKeysByName indexes live user collections (name → key),
// skipping Steam-owned, dynamic and unparsable entries.
func collectionKeysByName(pairs []nsPair) map[string]string {
	out := map[string]string{}
	for _, p := range pairs {
		if !strings.HasPrefix(p.Key, "user-collections.") || isSteamOwned(p.Key) {
			continue
		}
		e, _, err := parseEntry(p.Raw)
		if err != nil || e.Deleted || e.Value == "" {
			continue
		}
		cv, _, err := parseCollectionValue(e.Value)
		if err != nil || isDynamic(cv.Filter) {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(cv.Name))
		if name == "" {
			continue
		}
		if _, dup := out[name]; !dup {
			out[name] = p.Key
		}
	}
	return out
}

func sortedU32(set map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func equalU32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeCloudFiles persists the namespace (with backup), bumps the
// version mark and records changed keys for upload.
func writeCloudFiles(nsPath, namespacesPath, modifiedPath string, pairs []nsPair, maxV int64, changed map[string]bool) error {
	if err := backupFile(nsPath); err != nil {
		return fmt.Errorf("backup %s: %w", nsPath, err)
	}
	if err := saveNamespace(nsPath, pairs); err != nil {
		return err
	}
	marks, err := loadVersionMark(namespacesPath)
	if err != nil {
		return err
	}
	verRaw := json.RawMessage(`"` + itoa(maxV) + `"`)
	if len(marks) == 0 {
		marks = []nsPair{{Key: `"1"`, Raw: verRaw}}
	} else {
		for i, m := range marks {
			if markID(m.Key) == "1" {
				marks[i].Raw = verRaw
			}
		}
	}
	if err := saveVersionMark(namespacesPath, marks); err != nil {
		return err
	}
	keys, err := loadModified(modifiedPath)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	changedList := make([]string, 0, len(changed))
	for k := range changed {
		changedList = append(changedList, k)
	}
	sort.Strings(changedList)
	for _, k := range changedList {
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	return saveModified(modifiedPath, keys)
}

// markID normalizes a version-mark pair id, which occurs both quoted
// (["1","1473"]) and bare ([1,"1473"]) depending on client version.
func markID(raw string) string {
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err == nil {
		return s
	}
	return strings.TrimSpace(raw)
}

// itoa formats a version counter.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
