package ebpf

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildLdSoCache constructs a minimal new-format
// (glibc-ld.so.cache1.1) cache image: header, nlibs, six unused
// u32s, nlibs 24-byte entries, then the string pool. Key/value
// offsets are absolute from the start of the file, native endian,
// exactly the shape glibc writes and aya parses.
func buildLdSoCache(t *testing.T, pairs [][2]string) []byte {
	t.Helper()
	const header = "glibc-ld.so.cache1.1"
	headerLen := len(header)
	tableOff := headerLen + 4 + 6*4
	poolOff := tableOff + len(pairs)*24

	var pool []byte
	type ent struct{ k, v uint32 }
	ents := make([]ent, 0, len(pairs))
	for _, p := range pairs {
		k := uint32(poolOff + len(pool))
		pool = append(pool, p[0]...)
		pool = append(pool, 0)
		v := uint32(poolOff + len(pool))
		pool = append(pool, p[1]...)
		pool = append(pool, 0)
		ents = append(ents, ent{k, v})
	}

	buf := make([]byte, 0, poolOff+len(pool))
	buf = append(buf, header...)
	buf = binary.NativeEndian.AppendUint32(buf, uint32(len(pairs)))
	for range 6 {
		buf = binary.NativeEndian.AppendUint32(buf, 0)
	}
	for _, e := range ents {
		buf = binary.NativeEndian.AppendUint32(buf, 1) // flags
		buf = binary.NativeEndian.AppendUint32(buf, e.k)
		buf = binary.NativeEndian.AppendUint32(buf, e.v)
		buf = append(buf, make([]byte, 12)...)
	}
	buf = append(buf, pool...)
	return buf
}

func TestParseLdSoCache(t *testing.T) {
	t.Parallel()

	data := buildLdSoCache(t, [][2]string{
		{"libc.so.6", "/lib64/libc.so.6"},
		{"libcrypto.so.3", "/usr/lib64/libcrypto.so.3"},
	})
	entries, err := parseLdSoCache(data)
	if err != nil {
		t.Fatalf("parseLdSoCache: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("entries: got %d, want 2", len(entries))
	}
	if entries[0].key != "libc.so.6" || entries[0].value != "/lib64/libc.so.6" {
		t.Fatalf("entry 0: got %+v", entries[0])
	}
	if entries[1].key != "libcrypto.so.3" || entries[1].value != "/usr/lib64/libcrypto.so.3" {
		t.Fatalf("entry 1: got %+v", entries[1])
	}
}

func TestParseLdSoCache_RealHostCacheIfPresent(t *testing.T) {
	t.Parallel()

	// Not a fixture: an opportunistic check against the real
	// glibc-written file when the host has one. Skipped, never
	// failed, when absent or old-format.
	data, err := os.ReadFile("/etc/ld.so.cache")
	if err != nil {
		t.Skipf("no host cache: %v", err)
	}

	entries, err := parseLdSoCache(data)
	if err != nil {
		t.Skipf("host cache not new-format: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("host cache parsed to zero entries")
	}
	for _, e := range entries {
		if e.key == "" || !strings.HasPrefix(e.value, "/") {
			t.Fatalf("implausible entry %+v", e)
		}
	}
}

func TestParseLdSoCache_RejectsOldFormat(t *testing.T) {
	t.Parallel()

	data := append([]byte("ld.so-1.7.0\x00"), make([]byte, 64)...)
	if _, err := parseLdSoCache(data); err == nil {
		t.Fatal("old-format cache must be rejected, not misparsed")
	}
}

func TestParseLdSoCache_RejectsGarbage(t *testing.T) {
	t.Parallel()

	for name, data := range map[string][]byte{
		"empty":     {},
		"short":     []byte("glibc-ld"),
		"not-cache": []byte("this is not a cache file at all........"),
	} {
		if _, err := parseLdSoCache(data); err == nil {
			t.Fatalf("%s: want error, got nil", name)
		}
	}
}

func TestParseLdSoCache_RejectsOutOfRangeOffsets(t *testing.T) {
	t.Parallel()

	data := buildLdSoCache(t, [][2]string{{"libc.so.6", "/lib64/libc.so.6"}})
	// Corrupt the key offset (first entry field after flags) to
	// point past the end of the file.
	keyOffPos := len("glibc-ld.so.cache1.1") + 4 + 6*4 + 4
	binary.NativeEndian.PutUint32(data[keyOffPos:], uint32(len(data)+100))
	if _, err := parseLdSoCache(data); err == nil {
		t.Fatal("out-of-range string offset must be an error, not a panic or empty string")
	}
}

func TestResolveLibName(t *testing.T) {
	t.Parallel()

	entries := []ldCacheEntry{
		{key: "libcrypto.so.3", value: "/usr/lib64/libcrypto.so.3"},
		{key: "libc.so.6", value: "/lib64/libc.so.6"},
		{key: "libc.so.6", value: "/other/libc.so.6"}, // later duplicate: first match wins
	}
	cases := []struct {
		name  string
		query string
		want  string
		ok    bool
	}{
		{"bare name", "libc", "/lib64/libc.so.6", true},
		{"name with .so", "libc.so", "/lib64/libc.so.6", true},
		{"exact soname", "libc.so.6", "/lib64/libc.so.6", true},
		{"prefix must not bleed", "libcr", "", false},
		{"other lib", "libcrypto", "/usr/lib64/libcrypto.so.3", true},
		{"unknown", "libnope", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resolveLibName(entries, tc.query)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("resolveLibName(%q): got (%q, %v), want (%q, %v)", tc.query, got, ok, tc.want, tc.ok)
			}
		})
	}
}

const fixtureMaps = `5641a0000000-5641a0020000 r--p 00000000 00:21 1000 /usr/bin/some-binary
7f2b40000000-7f2b40030000 r-xp 00000000 00:21 2000 /usr/lib64/libc.so.6
7f2b40030000-7f2b40031000 rw-p 00000000 00:00 0
7f2b40040000-7f2b40050000 r-xp 00000000 00:21 3000 /usr/lib64/libcrypto.so.3
7f2b40050000-7f2b40060000 r-xp 00000000 00:21 4000 /usr/lib64/libssl-3.so (deleted)
7f2b40060000-7f2b40070000 r-xp 00000000 00:21 5000 [vdso]
7f2b40070000-7f2b40080000 r-xp 00000000 00:21 6000 /opt/old/libold-2.31.so
`

func TestLibFromMaps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		query string
		want  string
		ok    bool
	}{
		{"bare name", "libc", "/usr/lib64/libc.so.6", true},
		{"name with .so", "libc.so", "/usr/lib64/libc.so.6", true},
		{"exact soname", "libc.so.6", "/usr/lib64/libc.so.6", true},
		{"versioned dash form", "libold", "/opt/old/libold-2.31.so", true},
		{"prefix must not bleed", "libcr", "", false},
		{"non-library binary basename", "some-binary", "", false},
		{"absolute path never matches", "/usr/lib64/libc.so.6", "", false},
		{"unknown", "libnope", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := libFromMaps([]byte(fixtureMaps), tc.query)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("libFromMaps(%q): got (%q, %v), want (%q, %v)", tc.query, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// writeResolverFixture lays out a fake procRoot and cache file for
// shell-level tier tests.
func writeResolverFixture(t *testing.T, pid string, maps string, cache []byte) targetResolver {
	t.Helper()
	dir := t.TempDir()
	procRoot := filepath.Join(dir, "proc")
	if maps != "" {
		if err := os.MkdirAll(filepath.Join(procRoot, pid), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(procRoot, pid, "maps"), []byte(maps), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cachePath := filepath.Join(dir, "ld.so.cache")
	if cache != nil {
		if err := os.WriteFile(cachePath, cache, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return targetResolver{procRoot: procRoot, cachePath: cachePath}
}

func TestResolveUprobeTarget_TierOrder(t *testing.T) {
	t.Parallel()

	cache := buildLdSoCache(t, [][2]string{{"libc.so.6", "/from/cache/libc.so.6"}})

	t.Run("pid present and lib mapped wins over cache", func(t *testing.T) {
		t.Parallel()
		r := writeResolverFixture(t, "1234", fixtureMaps, cache)
		res, err := r.resolve("libc", 1234)
		if err != nil {
			t.Fatal(err)
		}

		if res.Path != "/usr/lib64/libc.so.6" || res.Source != sourceProcMaps {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("absolute path used as-is without reads", func(t *testing.T) {
		t.Parallel()
		r := targetResolver{procRoot: "/nonexistent", cachePath: "/nonexistent"}
		res, err := r.resolve("/usr/bin/thing", 0)
		if err != nil {
			t.Fatal(err)
		}

		if res.Path != "/usr/bin/thing" || res.Source != sourceAbsolutePath {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("bare name with no pid resolves via cache", func(t *testing.T) {
		t.Parallel()
		r := writeResolverFixture(t, "0", "", cache)
		res, err := r.resolve("libc", 0)
		if err != nil {
			t.Fatal(err)
		}

		if res.Path != "/from/cache/libc.so.6" || res.Source != sourceLdSoCache {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("pid present but lib unmapped falls through to cache", func(t *testing.T) {
		t.Parallel()
		r := writeResolverFixture(t, "1234", "7f00-7f01 r-xp 00000000 00:21 1 /usr/lib64/libssl.so.3\n", cache)
		res, err := r.resolve("libc", 1234)
		if err != nil {
			t.Fatal(err)
		}

		if res.Path != "/from/cache/libc.so.6" || res.Source != sourceLdSoCache {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("pid present but maps unreadable is an error", func(t *testing.T) {
		t.Parallel()
		r := writeResolverFixture(t, "9999", "", cache) // no maps file for 1234
		_, err := r.resolve("libc", 1234)
		if err == nil {
			t.Fatal("an unreadable maps file for a requested pid must not silently fall through")
		}
	})

	t.Run("name missing from cache is a designed error naming the target", func(t *testing.T) {
		t.Parallel()
		r := writeResolverFixture(t, "0", "", cache)
		_, err := r.resolve("libnope", 0)
		if err == nil {
			t.Fatal("want error")
		}
		if !strings.Contains(err.Error(), "libnope") {
			t.Fatalf("error must name the unresolvable target: %v", err)
		}
	})

	t.Run("unreadable cache surfaces as an error", func(t *testing.T) {
		t.Parallel()
		r := writeResolverFixture(t, "0", "", nil) // no cache file
		_, err := r.resolve("libc", 0)
		if err == nil {
			t.Fatal("want error")
		}
	})
}
