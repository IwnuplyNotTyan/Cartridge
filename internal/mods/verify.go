package mods

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxROMBytes caps how much ROM data verification reads into memory.
const maxROMBytes = 64 << 20

// Check is the verdict of VerifyPatch: does this patch file fit this ROM?
// Kind is one of "ok", "base" (the patch targets a different ROM dump —
// wrong revision, headered vs headerless, wrong game), "broken" (the patch
// file itself is corrupt or unreadable) or "unknown" (this format carries
// nothing to check against, e.g. PPF/XDelta).
type Check struct {
	OK   bool
	Kind string
	// Base-mismatch details (Kind == "base"):
	// the sizes/CRCs the patch expects vs the actual ROM.
	WantSize, GotSize int64
	WantCRC, GotCRC   uint32
	HasCRC            bool
	// Note is a short technical hint ("bad magic", "truncated",
	// "writes past ROM end", ...), language-neutral.
	Note string
}

// VerifyPatch checks a pile patch file against raw ROM bytes.
// It never applies anything; BPS/UPS verify by embedded CRC32,
// IPS by record bounds (it carries no checksum).
func VerifyPatch(patchPath string, rom []byte) Check {
	switch strings.ToLower(filepath.Ext(patchPath)) {
	case ".bps":
		return checkBPS(patchPath, rom)
	case ".ups":
		return checkUPS(patchPath, rom)
	case ".ips":
		return checkIPS(patchPath, rom)
	default:
		return Check{Kind: "unknown"}
	}
}

// ReadROMBytes returns the raw bytes verification compares against:
// the file itself, or the inner entry for zipped games.
func ReadROMBytes(romPath, inner string) ([]byte, error) {
	if strings.TrimSpace(inner) == "" {
		return readCapped(romPath)
	}
	zr, err := zip.OpenReader(romPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != inner {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxROMBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxROMBytes {
			return nil, fmt.Errorf("ROM too large: %s!%s", romPath, inner)
		}
		return data, nil
	}
	return nil, fmt.Errorf("no such entry in zip: %s!%s", romPath, inner)
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxROMBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxROMBytes {
		return nil, fmt.Errorf("ROM too large: %s", path)
	}
	return data, nil
}

// decodeVarint decodes a byuu-style (BPS/UPS) variable-length integer,
// advancing *off. It errors on truncation or overflow.
func decodeVarint(data []byte, off *int) (uint64, error) {
	var result, shift uint64
	for {
		if *off >= len(data) {
			return 0, fmt.Errorf("truncated varint")
		}
		b := data[*off]
		*off++
		// Overflow guard: past 64 bits the value is garbage anyway.
		if shift >= 64 {
			return 0, fmt.Errorf("varint overflow")
		}
		result += uint64(b&0x7F) << shift
		if b&0x80 != 0 {
			return result, nil
		}
		shift += 7
		result += 1 << shift
	}
}

// footCRCs splits a BPS/UPS file into body and its 12-byte footer
// (source CRC32, target CRC32, patch CRC32).
func footCRCs(data []byte) (body []byte, srcCRC, dstCRC, patchCRC uint32, err error) {
	if len(data) < 12 {
		return nil, 0, 0, 0, fmt.Errorf("truncated footer")
	}
	foot := data[len(data)-12:]
	return data[:len(data)-12],
		binary.LittleEndian.Uint32(foot[0:4]),
		binary.LittleEndian.Uint32(foot[4:8]),
		binary.LittleEndian.Uint32(foot[8:12]), nil
}

// checkBPS verifies a Beat patch: magic, embedded source size/CRC32
// against the ROM, and the patch's own checksum (catches bad downloads).
func checkBPS(patchPath string, rom []byte) Check {
	data, err := os.ReadFile(patchPath)
	if err != nil {
		return Check{Kind: "broken", Note: err.Error()}
	}
	if len(data) < 4 || string(data[:4]) != "BPS1" {
		return Check{Kind: "broken", Note: "bad magic"}
	}
	off := 4
	srcSize, err := decodeVarint(data, &off)
	if err != nil {
		return Check{Kind: "broken", Note: "bad header"}
	}
	if _, err := decodeVarint(data, &off); err != nil { // target size
		return Check{Kind: "broken", Note: "bad header"}
	}
	metaSize, err := decodeVarint(data, &off) // metadata blob
	if err != nil {
		return Check{Kind: "broken", Note: "bad header"}
	}
	if metaSize > uint64(len(data)-off) {
		return Check{Kind: "broken", Note: "bad header"}
	}
	off += int(metaSize)
	if off+12 > len(data) {
		return Check{Kind: "broken", Note: "truncated"}
	}
	_, wantCRC, _, patchCRC, err := footCRCs(data)
	if err != nil {
		return Check{Kind: "broken", Note: "truncated"}
	}
	if crc32.Checksum(data[:len(data)-4], crc32.IEEETable) != patchCRC {
		return Check{Kind: "broken", Note: "patch checksum mismatch"}
	}
	gotCRC := crc32.Checksum(rom, crc32.IEEETable)
	if int64(srcSize) != int64(len(rom)) || wantCRC != gotCRC {
		return Check{Kind: "base", WantSize: int64(srcSize), GotSize: int64(len(rom)),
			WantCRC: wantCRC, GotCRC: gotCRC, HasCRC: true,
			Note: fmt.Sprintf("bps wants %d bytes crc %08X, rom is %d bytes crc %08X",
				srcSize, wantCRC, len(rom), gotCRC)}
	}
	return Check{OK: true, Kind: "ok"}
}

// checkUPS verifies a UPS patch the same way: embedded source size/CRC32
// plus the patch's own checksum.
func checkUPS(patchPath string, rom []byte) Check {
	data, err := os.ReadFile(patchPath)
	if err != nil {
		return Check{Kind: "broken", Note: err.Error()}
	}
	if len(data) < 4 || string(data[:4]) != "UPS1" {
		return Check{Kind: "broken", Note: "bad magic"}
	}
	off := 4
	srcSize, err := decodeVarint(data, &off)
	if err != nil {
		return Check{Kind: "broken", Note: "bad header"}
	}
	if _, err := decodeVarint(data, &off); err != nil { // target size
		return Check{Kind: "broken", Note: "bad header"}
	}
	if off+12 > len(data) {
		return Check{Kind: "broken", Note: "truncated"}
	}
	_, wantCRC, _, patchCRC, err := footCRCs(data)
	if err != nil {
		return Check{Kind: "broken", Note: "truncated"}
	}
	if crc32.Checksum(data[:len(data)-4], crc32.IEEETable) != patchCRC {
		return Check{Kind: "broken", Note: "patch checksum mismatch"}
	}
	gotCRC := crc32.Checksum(rom, crc32.IEEETable)
	if int64(srcSize) != int64(len(rom)) || wantCRC != gotCRC {
		return Check{Kind: "base", WantSize: int64(srcSize), GotSize: int64(len(rom)),
			WantCRC: wantCRC, GotCRC: gotCRC, HasCRC: true,
			Note: fmt.Sprintf("ups wants %d bytes crc %08X, rom is %d bytes crc %08X",
				srcSize, wantCRC, len(rom), gotCRC)}
	}
	return Check{OK: true, Kind: "ok"}
}

// parseIPSExtent parses an IPS patch and reports the lowest and furthest
// ROM offsets it touches. Shared by verification and the hard-patch
// header heuristic.
func parseIPSExtent(data []byte) (minOff, maxEnd int64, err error) {
	if len(data) < 5 || string(data[:5]) != "PATCH" {
		return 0, 0, fmt.Errorf("bad magic")
	}
	minOff = -1
	off := 5
	for {
		if off+3 > len(data) {
			return 0, 0, fmt.Errorf("truncated")
		}
		if string(data[off:off+3]) == "EOF" {
			if minOff < 0 {
				minOff = 0
			}
			return minOff, maxEnd, nil
		}
		at := int64(data[off])<<16 | int64(data[off+1])<<8 | int64(data[off+2])
		off += 3
		if off+2 > len(data) {
			return 0, 0, fmt.Errorf("truncated")
		}
		size := int64(data[off])<<8 | int64(data[off+1])
		off += 2
		if size == 0 { // RLE record: run length + 1 value byte
			if off+3 > len(data) {
				return 0, 0, fmt.Errorf("truncated")
			}
			size = int64(data[off])<<8 | int64(data[off+1])
			off += 3
		} else {
			if int64(off)+size > int64(len(data)) {
				return 0, 0, fmt.Errorf("truncated")
			}
			off += int(size)
		}
		if minOff < 0 || at < minOff {
			minOff = at
		}
		if end := at + size; end > maxEnd {
			maxEnd = end
		}
	}
}

// checkIPS parses an IPS patch (no checksum in the format) and reports
// whether its records fit the ROM. A patch writing well past the ROM end
// targets a different dump; a small overrun confined past a header-sized
// prefix is auto-adapted at apply time (same rule as ApplyPatch).
func checkIPS(patchPath string, rom []byte) Check {
	data, err := os.ReadFile(patchPath)
	if err != nil {
		return Check{Kind: "broken", Note: err.Error()}
	}
	if len(data) >= 5 && string(data[:5]) == "IPS32" {
		return Check{Kind: "unknown"}
	}
	minOff, maxEnd, err := parseIPSExtent(data)
	if err != nil {
		return Check{Kind: "broken", Note: err.Error()}
	}
	if maxEnd > int64(len(rom)) && (maxEnd-int64(len(rom)) > 512 || minOff < 512) {
		return Check{Kind: "base", WantSize: maxEnd, GotSize: int64(len(rom)),
			Note: fmt.Sprintf("ips writes to offset %d past rom end %d", maxEnd, len(rom))}
	}
	return Check{OK: true, Kind: "ok"}
}
