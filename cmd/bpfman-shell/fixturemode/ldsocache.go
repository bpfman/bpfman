// Test-fixture mode that fabricates a dynamic-linker cache. When
// BPFMAN_SHELL_MODE=ldso-cache-writer, bpfman-shell writes a minimal
// new-format (glibc-ld.so.cache1.1) cache file from soname=path
// pairs given on the command line:
//
//	bpfman-shell <outfile> <soname>=<path> [<soname>=<path>...]
//
// The e2e scripts use this to place a synthetic /etc/ld.so.cache
// inside a target mount namespace whose entries diverge from the
// host -- the discriminating fixture for container uprobe
// library-name resolution (BPFMAN-42,
// https://redhat.atlassian.net/browse/BPFMAN-42): a name
// that resolves through this cache can only have been resolved inside the namespace.
// ldconfig cannot build such a cache because it takes each key from
// the library's ELF soname, not from the file name.
//
// The byte layout mirrors what glibc writes and what the resolver's
// parseLdSoCache expects: the 20-byte header, a native-endian u32
// entry count, six unused u32s, 24-byte entries of (flags, key
// offset, value offset, 12 unused bytes), then the string pool,
// with offsets absolute from the start of the file.
package fixturemode

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// runLdSoCacheWriter parses <outfile> <soname>=<path>... and writes
// the cache image.
func runLdSoCacheWriter(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ldso-cache-writer <outfile> <soname>=<path>...")
	}

	outfile := args[0]

	type pair struct{ key, value string }
	pairs := make([]pair, 0, len(args)-1)

	for _, arg := range args[1:] {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || key == "" || value == "" {
			return fmt.Errorf("malformed entry %q (want soname=path)", arg)
		}
		pairs = append(pairs, pair{key: key, value: value})
	}

	const header = "glibc-ld.so.cache1.1"
	tableOff := len(header) + 4 + 6*4
	poolOff := tableOff + len(pairs)*24

	var pool []byte
	type entry struct{ key, value uint32 }
	entries := make([]entry, 0, len(pairs))

	for _, p := range pairs {
		key := uint32(poolOff + len(pool))
		pool = append(pool, p.key...)
		pool = append(pool, 0)
		value := uint32(poolOff + len(pool))
		pool = append(pool, p.value...)
		pool = append(pool, 0)
		entries = append(entries, entry{key: key, value: value})
	}

	buf := make([]byte, 0, poolOff+len(pool))
	buf = append(buf, header...)
	buf = binary.NativeEndian.AppendUint32(buf, uint32(len(pairs)))

	for range 6 {
		buf = binary.NativeEndian.AppendUint32(buf, 0)
	}

	for _, e := range entries {
		buf = binary.NativeEndian.AppendUint32(buf, 1) // flags: ELF
		buf = binary.NativeEndian.AppendUint32(buf, e.key)
		buf = binary.NativeEndian.AppendUint32(buf, e.value)
		buf = append(buf, make([]byte, 12)...)
	}

	buf = append(buf, pool...)

	if err := os.WriteFile(outfile, buf, 0o644); err != nil {
		return fmt.Errorf("write cache %s: %w", outfile, err)
	}

	return nil
}
