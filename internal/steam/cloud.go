package steam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Cloud namespace files (what current Steam clients actually read):
//
//	<userdata>/<id>/config/cloudstorage/cloud-storage-namespace-1.json
//	<userdata>/<id>/config/cloudstorage/cloud-storage-namespaces.json
//	<userdata>/<id>/config/cloudstorage/cloud-storage-namespace-1.modified.json
//
// namespace-1.json is compact JSON: an array of [key, entry] pairs.
// A live collection entry looks like:
//
//	["user-collections.uc-AbC123",
//	 {"key":"user-collections.uc-AbC123","timestamp":1785986592,
//	  "value":"{\"id\":\"uc-AbC123\",\"name\":\"FPS\",\"added\":[730],\"removed\":[]}",
//	  "version":"4546",...}]
//
// Deleted collections are tombstones: {"key":...,"timestamp":...,
// "version":...,"is_deleted":true} (no value). Tombstones, foreign keys
// and untouched entries are preserved byte-for-byte.
const (
	cloudDirName      = "cloudstorage"
	cloudNamespace    = "cloud-storage-namespace-1.json"
	cloudNamespaces   = "cloud-storage-namespaces.json"
	cloudModifiedTmpl = "cloud-storage-namespace-%d.modified.json"
	cloudNamespaceID  = 1
)

// cloudPaths resolves the cloudstorage files next to shortcuts.vdf.
// cfgDir is the directory holding shortcuts.vdf (…/config).
func cloudPaths(cfgDir string) (ns, namespaces, modified string) {
	dir := filepath.Join(cfgDir, cloudDirName)
	return filepath.Join(dir, cloudNamespace),
		filepath.Join(dir, cloudNamespaces),
		filepath.Join(dir, fmt.Sprintf(cloudModifiedTmpl, cloudNamespaceID))
}

// nsPair is one [key, entry] pair; Raw keeps the entry byte-identical
// unless we deliberately replace it.
type nsPair struct {
	Key string
	Raw json.RawMessage
}

// cloudEntry is the decoded envelope of one pair.
type cloudEntry struct {
	Key       string                     `json:"key"`
	Timestamp int64                      `json:"timestamp"`
	Value     string                     `json:"value,omitempty"`
	Version   cloudVersion               `json:"version,omitempty"`
	Deleted   bool                       `json:"is_deleted,omitempty"`
	Extra     map[string]json.RawMessage `json:"-"`
}

// cloudVersion is a namespace-global counter stored either as a JSON
// number or a JSON string (both occur on real clients). It remembers
// its original encoding so rewrites keep the same style.
type cloudVersion struct {
	N      int64
	Quoted bool
}

// UnmarshalJSON accepts 1473 and "1473".
func (v *cloudVersion) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		v.Quoted = true
		unq, err := strconv.Unquote(s)
		if err != nil {
			return err
		}
		s = unq
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("bad cloud version %s", s)
	}
	v.N = n
	return nil
}

// MarshalJSON keeps the original number-or-string style.
func (v cloudVersion) MarshalJSON() ([]byte, error) {
	if v.Quoted {
		return []byte(strconv.Quote(strconv.FormatInt(v.N, 10))), nil
	}
	return []byte(strconv.FormatInt(v.N, 10)), nil
}

// parseEntry decodes one envelope, tolerating unknown extra fields.
func parseEntry(raw json.RawMessage) (cloudEntry, map[string]json.RawMessage, error) {
	var e cloudEntry
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil {
		return e, nil, err
	}
	get := func(k string, dst any) {
		if r, ok := m[k]; ok {
			_ = json.Unmarshal(r, dst)
			delete(m, k)
		}
	}
	get("key", &e.Key)
	get("timestamp", &e.Timestamp)
	get("value", &e.Value)
	get("version", &e.Version)
	get("is_deleted", &e.Deleted)
	return e, m, nil
}

// encodeEntry rebuilds an envelope preserving unknown fields and the
// original key order as much as JSON allows: known fields first in
// Steam's order, extras appended sorted (only for entries we rewrite).
func encodeEntry(e cloudEntry, extra map[string]json.RawMessage) []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	write := func(first *bool, k string, v []byte) {
		if !*first {
			buf.WriteByte(',')
		}
		*first = false
		buf.WriteString(strconv.Quote(k))
		buf.WriteByte(':')
		buf.Write(v)
	}
	first := true
	if e.Key != "" {
		kb, _ := json.Marshal(e.Key)
		write(&first, "key", kb)
	}
	tb, _ := json.Marshal(e.Timestamp)
	write(&first, "timestamp", tb)
	if e.Value != "" {
		vb, _ := json.Marshal(e.Value)
		write(&first, "value", vb)
	}
	vb, _ := json.Marshal(e.Version)
	write(&first, "version", vb)
	for _, k := range sortedKeys(extra) {
		if k == "is_deleted" {
			// is_deleted goes last, like Steam writes tombstones.
			continue
		}
		write(&first, k, extra[k])
	}
	if e.Deleted {
		write(&first, "is_deleted", []byte("true"))
	} else if r, ok := extra["is_deleted"]; ok {
		write(&first, "is_deleted", r)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// loadNamespace reads the namespace file preserving raw entry bytes.
// A missing file yields an empty doc (caller decides whether to create).
func loadNamespace(path string) ([]nsPair, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(data, &elems); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]nsPair, 0, len(elems))
	for i, el := range elems {
		var kv []json.RawMessage
		if err := json.Unmarshal(el, &kv); err != nil || len(kv) != 2 {
			return nil, fmt.Errorf("parse %s: bad pair #%d", path, i)
		}
		var key string
		if err := json.Unmarshal(kv[0], &key); err != nil {
			return nil, fmt.Errorf("parse %s: bad key #%d", path, i)
		}
		out = append(out, nsPair{Key: key, Raw: kv[1]})
	}
	return out, nil
}

// saveNamespace writes pairs atomically (tmp + rename), compact, one line.
func saveNamespace(path string, pairs []nsPair) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, p := range pairs {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteByte('[')
		kb, _ := json.Marshal(p.Key)
		buf.Write(kb)
		buf.WriteByte(',')
		buf.Write(p.Raw)
		buf.WriteByte(']')
	}
	buf.WriteByte(']')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// backupFile copies path to path+".cartridge.bak" (overwritten each run).
func backupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".cartridge.bak", data, 0o644)
}

// loadVersionMark reads cloud-storage-namespaces.json as raw pairs.
// Pair ids occur both as numbers ([1,"1473"]) and strings (["1","1473"])
// depending on client version; Key keeps the raw bytes verbatim.
func loadVersionMark(path string) ([]nsPair, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(data, &elems); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var out []nsPair
	for _, el := range elems {
		var kv []json.RawMessage
		if err := json.Unmarshal(el, &kv); err != nil || len(kv) != 2 {
			return nil, fmt.Errorf("parse %s: bad pair", path)
		}
		out = append(out, nsPair{Key: string(kv[0]), Raw: kv[1]})
	}
	return out, nil
}

// saveVersionMark writes the namespaces mark back compactly.
func saveVersionMark(path string, pairs []nsPair) error {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, p := range pairs {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString("[")
		buf.WriteString(p.Key)
		buf.WriteByte(',')
		buf.Write(p.Raw)
		buf.WriteString("]")
	}
	buf.WriteByte(']')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadModified reads the *.modified.json key list (usually []).
func loadModified(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return keys, nil
}

// saveModified writes the modified key list compactly.
func saveModified(path string, keys []string) error {
	if keys == nil {
		keys = []string{}
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// nowTS returns current unix time for entry timestamps.
func nowTS() int64 { return time.Now().Unix() }
