package mods

import (
	"fmt"
	"strings"

	rompatcher "github.com/olsonb97/RomPatcher.go"
)

// ApplyPatch applies a patch file to raw ROM bytes and returns the patched
// image. Format is auto-detected (IPS/BPS/UPS/PPF/xdelta/…) and validation
// is always on: a patch for another dump fails instead of silently
// corrupting the output.
//
// Copier headers are auto-adapted: when the patch declares its source
// (BPS/UPS) and the ROM carries a recognized header the patch was not
// built for, the header is stripped for patching and reattached after;
// when the ROM is headerless but the patch expects headered offsets,
// temporary header-sized data is prepended for the apply and removed
// from the result. IPS carries no source signature, so it applies as-is
// when its records fit the ROM.
func ApplyPatch(rom, patchBytes []byte, sourceName string) ([]byte, error) {
	p, err := rompatcher.Parse(patchBytes)
	if err != nil {
		return nil, err
	}
	base := rompatcher.ApplyOptions{Validate: true, FixChecksum: true, SourceName: sourceName}
	// IPS carries no source signature, so bounds are the only signal and
	// are checked before touching anything:
	// fitting records apply as-is (standard behavior, like Lunar IPS);
	// a small overrun confined past a header-sized prefix means
	// headered offsets on a headerless ROM and is auto-adapted;
	// anything else does not match this ROM.
	if isIPSKind(p) {
		return applyIPSWithHeader(rom, patchBytes, p, base, sourceName)
	}
	// The ROM as-is is the common case and always wins when it validates.
	if p.ValidateSource(rom) {
		return rompatcher.ApplyParsedWithOptions(rom, p, base)
	}
	// A recognized header on the ROM that the patch was not built for:
	// strip it for patching, reattach after.
	if rompatcher.DetectHeader(rom, sourceName) != nil {
		stripped := stripHeader(rom, sourceName)
		if stripped != nil && p.ValidateSource(stripped) {
			opts := base
			opts.RemoveHeader = true
			return rompatcher.ApplyParsedWithOptions(rom, p, opts)
		}
	}
	// A headerless ROM with a patch built for headered offsets: pad
	// temporarily, but only when the padded image actually validates
	// (BPS/UPS carry source checksums; IPS is handled by bounds above).
	if rompatcher.CanAddHeader(rom, sourceName) != nil {
		padded, _ := rompatcher.AddHeader(rom, sourceName)
		if p.ValidateSource(padded) {
			opts := base
			opts.AddHeader = true
			return rompatcher.ApplyParsedWithOptions(rom, p, opts)
		}
	}
	return nil, fmt.Errorf("patch does not match this ROM (%s)", describeWant(p, rom, sourceName))
}

// stripHeader removes a recognized copier header, or nil when absent.
func stripHeader(rom []byte, sourceName string) []byte {
	_, body, info := rompatcher.RemoveHeader(rom, sourceName)
	if info == nil {
		return nil
	}
	return body
}

// isIPSKind reports whether the parsed patch is IPS-family (no source
// signature to probe, bounds-checked instead).
func isIPSKind(p rompatcher.Patch) bool {
	switch p.Format() {
	case rompatcher.FormatIPS, rompatcher.FormatIPS32:
		return true
	default:
		return false
	}
}

// applyIPSWithHeader applies an IPS patch by record bounds: fitting
// records go straight onto the ROM; a small overrun confined past a
// header-sized prefix is patched against temporarily prepended header
// data (headerless ROM, headered offsets) and stripped from the output.
func applyIPSWithHeader(rom, patchBytes []byte, p rompatcher.Patch, base rompatcher.ApplyOptions, sourceName string) ([]byte, error) {
	minOff, maxEnd, err := parseIPSExtent(patchBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid ips: %w", err)
	}
	if maxEnd <= int64(len(rom)) {
		return rompatcher.ApplyParsedWithOptions(rom, p, base)
	}
	if info := rompatcher.CanAddHeader(rom, sourceName); info != nil &&
		maxEnd-int64(len(rom)) <= int64(info.Size) && minOff >= int64(info.Size) {
		opts := base
		opts.AddHeader = true
		return rompatcher.ApplyParsedWithOptions(rom, p, opts)
	}
	return nil, fmt.Errorf("patch does not match this ROM (ips writes to offset %d past rom end %d)",
		maxEnd, len(rom))
}

// describeWant renders why the patch does not fit, for error messages.
func describeWant(p rompatcher.Patch, rom []byte, sourceName string) string {
	if info := p.ValidationInfo(); info != nil && len(info.Values) > 0 {
		return fmt.Sprintf("%s expects %s %s, rom is %d bytes",
			p.Format(), info.Type, strings.Join(info.Values, "/"), len(rom))
	}
	if maxEnd, err := parseIPSExtentFor(p, sourceName); err == nil && maxEnd > int64(len(rom)) {
		return fmt.Sprintf("ips writes to offset %d past rom end %d", maxEnd, len(rom))
	}
	return fmt.Sprintf("%s patch does not match %d-byte rom", p.Format(), len(rom))
}

// parseIPSExtentFor reuses the IPS bounds parser for parsed patches.
func parseIPSExtentFor(p rompatcher.Patch, _ string) (int64, error) {
	data, err := p.MarshalBinary()
	if err != nil {
		return 0, err
	}
	_, maxEnd, err := parseIPSExtent(data)
	return maxEnd, err
}
