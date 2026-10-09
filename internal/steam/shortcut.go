package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Shortcut is one Steam non-Steam game entry.
type Shortcut struct {
	AppID              uint32
	AppName            string
	Exe                string
	StartDir           string
	Icon               string
	ShortcutPath       string
	LaunchOptions      string
	IsHidden           bool
	AllowDesktopConfig bool
	AllowOverlay       bool
	OpenVR             bool
	Devkit             bool
	DevkitGameID       string
	LastPlayTime       int32
	Tags               []string
	// extra preserves unknown fields (FlatpakAppID, …) across rewrites.
	extraStr map[string]string
	extraInt map[string]int32
}

// key variants accepted on read (Steam mixes cases across versions).
func dictStr(d *vdfDict, names ...string) string {
	for _, n := range names {
		if v := d.get(n); v != nil && v.isStr {
			return v.str
		}
	}
	if d == nil {
		return ""
	}
	for _, k := range d.keys {
		for _, n := range names {
			if strings.EqualFold(k, n) {
				if v := d.vals[k]; v != nil && v.isStr {
					return v.str
				}
			}
		}
	}
	return ""
}

func dictInt(d *vdfDict, def int32, names ...string) int32 {
	for _, n := range names {
		if v := d.get(n); v != nil && v.isI32 {
			return v.i32
		}
	}
	if d == nil {
		return def
	}
	for _, k := range d.keys {
		for _, n := range names {
			if strings.EqualFold(k, n) {
				if v := d.vals[k]; v != nil && v.isI32 {
					return v.i32
				}
			}
		}
	}
	return def
}

// shortcutFromDict parses one entry.
func shortcutFromDict(d *vdfDict) Shortcut {
	s := Shortcut{
		AppID:              uint32(dictInt(d, 0, "appid")),
		AppName:            dictStr(d, "appname", "AppName"),
		Exe:                dictStr(d, "exe", "Exe"),
		StartDir:           dictStr(d, "StartDir", "startdir", "startDir"),
		Icon:               dictStr(d, "icon", "Icon"),
		ShortcutPath:       dictStr(d, "ShortcutPath", "shortcutpath"),
		LaunchOptions:      dictStr(d, "LaunchOptions", "launchoptions", "LaunchOptions"),
		IsHidden:           dictInt(d, 0, "IsHidden", "ishidden") != 0,
		AllowDesktopConfig: dictInt(d, 1, "AllowDesktopConfig") != 0,
		AllowOverlay:       dictInt(d, 1, "AllowOverlay") != 0,
		OpenVR:             dictInt(d, 0, "OpenVR", "openvr") != 0,
		Devkit:             dictInt(d, 0, "Devkit", "devkit") != 0,
		DevkitGameID:       dictStr(d, "DevkitGameID"),
		LastPlayTime:       dictInt(d, 0, "LastPlayTime", "lastplaytime"),
		extraStr:           map[string]string{},
		extraInt:           map[string]int32{},
	}
	known := map[string]bool{
		"appid": true, "appname": true, "exe": true, "startdir": true,
		"icon": true, "shortcutpath": true, "launchoptions": true,
		"ishidden": true, "allowdesktopconfig": true, "allowoverlay": true,
		"openvr": true, "devkit": true, "devkitgameid": true,
		"lastplaytime": true, "tags": true,
	}
	for _, k := range d.keys {
		if known[strings.ToLower(k)] {
			continue
		}
		v := d.vals[k]
		if v.dict != nil {
			continue
		}
		if v.isStr {
			s.extraStr[k] = v.str
		} else if v.isI32 {
			s.extraInt[k] = v.i32
		}
	}
	if tv := d.get("tags"); tv != nil && tv.dict != nil {
		idx := []int{}
		byIdx := map[int]string{}
		for _, k := range tv.dict.keys {
			n, err := strconv.Atoi(k)
			if err != nil {
				continue
			}
			if v := tv.dict.vals[k]; v != nil && v.isStr {
				byIdx[n] = v.str
				idx = append(idx, n)
			}
		}
		sort.Ints(idx)
		for _, n := range idx {
			s.Tags = append(s.Tags, byIdx[n])
		}
	}
	return s
}

// toDict serializes with Valve's canonical field casing.
func (s Shortcut) toDict() *vdfDict {
	d := newDict()
	bo := func(b bool) int32 {
		if b {
			return 1
		}
		return 0
	}
	d.set("appid", &vdfValue{i32: int32(s.AppID), isI32: true})
	d.set("AppName", &vdfValue{str: s.AppName, isStr: true})
	d.set("Exe", &vdfValue{str: s.Exe, isStr: true})
	d.set("StartDir", &vdfValue{str: s.StartDir, isStr: true})
	d.set("icon", &vdfValue{str: s.Icon, isStr: true})
	d.set("ShortcutPath", &vdfValue{str: s.ShortcutPath, isStr: true})
	d.set("LaunchOptions", &vdfValue{str: s.LaunchOptions, isStr: true})
	d.set("IsHidden", &vdfValue{i32: bo(s.IsHidden), isI32: true})
	d.set("AllowDesktopConfig", &vdfValue{i32: bo(s.AllowDesktopConfig), isI32: true})
	d.set("AllowOverlay", &vdfValue{i32: bo(s.AllowOverlay), isI32: true})
	d.set("OpenVR", &vdfValue{i32: bo(s.OpenVR), isI32: true})
	d.set("Devkit", &vdfValue{i32: bo(s.Devkit), isI32: true})
	d.set("DevkitGameID", &vdfValue{str: s.DevkitGameID, isStr: true})
	d.set("LastPlayTime", &vdfValue{i32: s.LastPlayTime, isI32: true})
	tags := newDict()
	for i, t := range s.Tags {
		tags.set(strconv.Itoa(i), &vdfValue{str: t, isStr: true})
	}
	d.set("tags", &vdfValue{dict: tags})
	// Preserve unknown fields.
	keys := make([]string, 0, len(s.extraStr)+len(s.extraInt))
	for k := range s.extraStr {
		keys = append(keys, k)
	}
	for k := range s.extraInt {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v, ok := s.extraStr[k]; ok {
			d.set(k, &vdfValue{str: v, isStr: true})
		} else {
			d.set(k, &vdfValue{i32: s.extraInt[k], isI32: true})
		}
	}
	return d
}

// LoadFile reads shortcuts.vdf; missing file yields empty list.
func LoadFile(path string) ([]Shortcut, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	root, err := decodeVDF(data)
	if err != nil {
		return nil, fmt.Errorf("parse shortcuts %s: %w", path, err)
	}
	node := root.get("shortcuts")
	if node == nil || node.dict == nil {
		return nil, nil
	}
	idx := []int{}
	byIdx := map[int]*vdfDict{}
	for _, k := range node.dict.keys {
		n, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		if v := node.dict.vals[k]; v != nil && v.dict != nil {
			byIdx[n] = v.dict
			idx = append(idx, n)
		}
	}
	sort.Ints(idx)
	out := make([]Shortcut, 0, len(idx))
	for _, n := range idx {
		out = append(out, shortcutFromDict(byIdx[n]))
	}
	return out, nil
}

// SaveFile writes shortcuts atomically (tmp + rename), creating dirs.
func SaveFile(path string, shortcuts []Shortcut) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	root := newDict()
	sub := newDict()
	for i, s := range shortcuts {
		sub.set(strconv.Itoa(i), &vdfValue{dict: s.toDict()})
	}
	root.set("shortcuts", &vdfValue{dict: sub})
	data := encodeVDF(root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FindByAppName finds a shortcut by exact AppName (case-insensitive).
func FindByAppName(list []Shortcut, name string) (int, bool) {
	for i, s := range list {
		if strings.EqualFold(s.AppName, name) {
			return i, true
		}
	}
	return -1, false
}

// Upsert inserts or updates a shortcut. Match order:
//  1. LaunchOptions ("play <id>" uniquely identifies our game and survives
//     exe renames, which change the AppID),
//  2. AppID,
//  3. Exe+AppName (keeps Steam's AppID for entries we didn't create).
func Upsert(list []Shortcut, s Shortcut) []Shortcut {
	if lo := strings.TrimSpace(s.LaunchOptions); lo != "" {
		for i, e := range list {
			if strings.TrimSpace(e.LaunchOptions) == lo {
				if s.LastPlayTime == 0 {
					s.LastPlayTime = e.LastPlayTime
				}
				list[i] = s
				return list
			}
		}
	}
	for i, e := range list {
		if e.AppID == s.AppID {
			// Keep LastPlayTime from Steam when we have none.
			if s.LastPlayTime == 0 {
				s.LastPlayTime = e.LastPlayTime
			}
			list[i] = s
			return list
		}
	}
	for i, e := range list {
		if exeKey(e.Exe) == exeKey(s.Exe) && strings.EqualFold(e.AppName, s.AppName) {
			s.AppID = e.AppID
			if s.LastPlayTime == 0 {
				s.LastPlayTime = e.LastPlayTime
			}
			list[i] = s
			return list
		}
	}
	return append(list, s)
}

// RemoveByAppID drops entries with the given ids.
func RemoveByAppID(list []Shortcut, ids map[uint32]bool) []Shortcut {
	out := list[:0]
	for _, s := range list {
		if !ids[s.AppID] {
			out = append(out, s)
		}
	}
	return out
}
