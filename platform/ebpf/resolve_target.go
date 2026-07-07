// Uprobe target resolution: turn the user's target operand into a
// path the kernel can probe. Rust (via aya) accepts three forms and
// so do we, tried in this order: a library mapped into a given
// process (read from /proc/<pid>/maps), an absolute path used
// as-is, and a bare library name resolved through /etc/ld.so.cache
// (so a bare `libc` target works like the documented Rust CLI usage).
//
// The parsers are pure functions over bytes -- the same SANS-IO
// discipline as the process builtin's procFS, at even smaller
// scale: the effect set here is two static file reads with no
// mid-sequence policy, so plain values replace the effect
// interface. targetResolver is the thin imperative shell that
// performs the reads and sequences the tiers; every decision lives
// in the pure functions below it.
//
// Resolution deliberately never loads the library (no dlopen):
// running an attacker-supplied library's constructors inside the
// daemon would be code execution. aya parses the cache for the
// same reason.
package ebpf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// resolutionSource says which tier resolved a target. Carried on
// targetResolution so logs and errors can name the decision, not
// just its outcome.
type resolutionSource int

const (
	sourceProcMaps resolutionSource = iota
	sourceAbsolutePath
	sourceLdSoCache
)

// String returns a human-readable name for the resolution tier.
func (s resolutionSource) String() string {
	switch s {
	case sourceProcMaps:
		return "process maps"
	case sourceAbsolutePath:
		return "absolute path"
	case sourceLdSoCache:
		return "ld.so.cache"
	default:
		return fmt.Sprintf("resolutionSource(%d)", int(s))
	}
}

// targetResolution is the resolver's decision as a value: the path
// to open and the tier that chose it. The caller performs the open.
type targetResolution struct {
	Path   string
	Source resolutionSource
}

// targetResolver is the imperative shell: it owns the two file
// reads and the tier order, nothing else. The zero-value paths are
// only for tests; production uses defaultTargetResolver.
type targetResolver struct {
	procRoot  string
	cachePath string
}

var defaultTargetResolver = targetResolver{procRoot: "/proc", cachePath: "/etc/ld.so.cache"}

// resolve maps target to a probeable path using aya's tier order:
// a pid's mapped libraries first (a library name names the copy
// that process actually loaded), then an absolute path as-is, then
// the dynamic linker's cache. A maps read failure for a requested
// pid is an error rather than a fallthrough -- the caller asked
// about that process, and resolving the name some other way could
// silently probe a different library than the one mapped.
func (r targetResolver) resolve(target string, pid int32) (targetResolution, error) {
	if pid > 0 {
		mapsPath := filepath.Join(r.procRoot, strconv.Itoa(int(pid)), "maps")
		data, err := os.ReadFile(mapsPath)
		if err != nil {
			return targetResolution{}, fmt.Errorf("resolve target %q: read %s: %w", target, mapsPath, err)
		}

		if path, ok := libFromMaps(data, target); ok {
			return targetResolution{Path: path, Source: sourceProcMaps}, nil
		}
	}
	if filepath.IsAbs(target) {
		return targetResolution{Path: target, Source: sourceAbsolutePath}, nil
	}
	data, err := os.ReadFile(r.cachePath)
	if err != nil {
		return targetResolution{}, fmt.Errorf("resolve target %q: read %s: %w", target, r.cachePath, err)
	}

	entries, err := parseLdSoCache(data)
	if err != nil {
		return targetResolution{}, fmt.Errorf("resolve target %q: parse %s: %w", target, r.cachePath, err)
	}

	path, ok := resolveLibName(entries, target)
	if !ok {
		return targetResolution{}, fmt.Errorf("resolve target %q: not found in %s (pass an absolute path)", target, r.cachePath)
	}
	return targetResolution{Path: path, Source: sourceLdSoCache}, nil
}

// libFromMaps finds the first mapped file in /proc/<pid>/maps text
// whose basename names the queried library. The match rule is
// aya's: strip a trailing ".so" from the query, then accept a
// basename that continues with ".so" (libc -> libc.so.6) or "-"
// (libold -> libold-2.31.so, the pre-2.34 glibc layout) -- plus an
// exact basename match. An absolute query never matches a
// basename, so absolute targets fall through to the as-is tier.
func libFromMaps(maps []byte, query string) (string, bool) {
	for line := range bytes.Lines(maps) {
		// Fields: address perms offset dev inode [pathname].
		// The pathname is absent for anonymous mappings.
		fields := strings.Fields(string(line))
		if len(fields) < 6 {
			continue
		}
		path := fields[5]
		if !strings.HasPrefix(path, "/") {
			continue // [vdso], [heap], ...
		}
		if libNameMatches(filepath.Base(path), query) {
			return path, true
		}
	}
	return "", false
}

// ldCacheEntry is one parsed cache record: key is the soname
// (libc.so.6), value the path the dynamic linker would load.
type ldCacheEntry struct {
	key   string
	value string
}

const (
	ldCacheHeaderNew = "glibc-ld.so.cache1.1"
	ldCacheHeaderOld = "ld.so-1.7.0\x00"
)

// parseLdSoCache decodes the new-format (glibc 2.2+) ld.so.cache:
// the 20-byte header, a native-endian u32 entry count, six unused
// u32s, then 24-byte entries of (flags i32, key u32, value u32,
// 12 unused bytes) whose string offsets are absolute from the
// start of the file. The old standalone format predates 2001 and
// is rejected by name rather than misparsed. The file is trusted
// root-owned state but still an external format: offsets are
// bounds-checked so a corrupt cache is an error, never a panic.
func parseLdSoCache(data []byte) ([]ldCacheEntry, error) {
	if len(data) >= len(ldCacheHeaderOld) && string(data[:len(ldCacheHeaderOld)]) == ldCacheHeaderOld {
		return nil, fmt.Errorf("old-format ld.so.cache is not supported")
	}
	if len(data) < len(ldCacheHeaderNew)+4 || string(data[:len(ldCacheHeaderNew)]) != ldCacheHeaderNew {
		return nil, fmt.Errorf("not a new-format ld.so.cache (no %q header)", ldCacheHeaderNew)
	}
	pos := len(ldCacheHeaderNew)
	nlibs := int(binary.NativeEndian.Uint32(data[pos:]))
	pos += 4 + 6*4 // entry count, then six unused u32s

	const entrySize = 24
	if nlibs < 0 || pos+nlibs*entrySize > len(data) {
		return nil, fmt.Errorf("entry table for %d entries exceeds cache size %d", nlibs, len(data))
	}

	readString := func(off uint32) (string, error) {
		if int(off) >= len(data) {
			return "", fmt.Errorf("string offset %d beyond cache size %d", off, len(data))
		}
		end := bytes.IndexByte(data[off:], 0)
		if end < 0 {
			return "", fmt.Errorf("unterminated string at offset %d", off)
		}
		return string(data[off : int(off)+end]), nil
	}

	entries := make([]ldCacheEntry, 0, nlibs)
	for i := range nlibs {
		entry := data[pos+i*entrySize:]
		key, err := readString(binary.NativeEndian.Uint32(entry[4:]))
		if err != nil {
			return nil, fmt.Errorf("entry %d key: %w", i, err)
		}

		value, err := readString(binary.NativeEndian.Uint32(entry[8:]))
		if err != nil {
			return nil, fmt.Errorf("entry %d value: %w", i, err)
		}

		entries = append(entries, ldCacheEntry{key: key, value: value})
	}
	return entries, nil
}

// resolveLibName resolves a bare library name against parsed cache
// entries, first match wins (the cache is ordered best-first by
// ldconfig). Matching is aya's rule -- strip a trailing ".so" from
// the query, accept keys continuing with ".so" -- plus an exact
// key match, which aya rejects (its suffix check fails on the
// empty remainder) but is the obvious reading of a `libc.so.6`
// target.
func resolveLibName(entries []ldCacheEntry, query string) (string, bool) {
	for _, e := range entries {
		if libNameMatches(e.key, query) {
			return e.value, true
		}
	}
	return "", false
}

// libNameMatches reports whether a library file name (a cache key
// or a maps basename) names the queried library: an exact match on
// a library-shaped name (one containing ".so", so a query naming a
// plain mapped binary does not resolve through the library tier),
// or the name continues the ".so"-stripped query with ".so" or
// "-" (libold -> libold-2.31.so, the pre-2.34 glibc layout).
func libNameMatches(name, query string) bool {
	if name == query {
		return strings.Contains(name, ".so")
	}
	stem := strings.TrimSuffix(query, ".so")
	rest, ok := strings.CutPrefix(name, stem)
	if !ok {
		return false
	}
	return strings.HasPrefix(rest, ".so") || strings.HasPrefix(rest, "-")
}
