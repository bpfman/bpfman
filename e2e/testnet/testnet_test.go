//go:build e2e

package testnet

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUniqueTestName(t *testing.T) {
	t.Parallel()

	t.Run("format", func(t *testing.T) {
		name := uniqueTestName()
		assert.Len(t, name, 14, "name should be 14 characters")
		assert.Equal(t, byte('B'), name[0], "should start with B")
		assert.Equal(t, byte('N'), name[13], "should end with N")

		// With veth suffix must fit IFNAMSIZ (15).
		assert.LessOrEqual(t, len(name+"a"), 15, "with veth suffix should fit IFNAMSIZ")
	})

	t.Run("hex middle", func(t *testing.T) {
		name := uniqueTestName()
		middle := name[1:13]
		for _, c := range middle {
			assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'), "character %c should be hex", c)
		}
	})

	t.Run("uniqueness", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := range 1000 {
			name := uniqueTestName()
			assert.False(t, seen[name], "name %s should be unique (collision at call %d)", name, i)
			seen[name] = true
		}
	})
}

func TestVethAddrsForIndex(t *testing.T) {
	t.Parallel()

	t.Run("first pair", func(t *testing.T) {
		addrA, addrB, target := vethAddrsForIndex(1)
		assert.Equal(t, "198.51.100.3/32", addrA)
		assert.Equal(t, "198.51.100.2/32", addrB)
		assert.Equal(t, "198.51.100.3", target)
	})

	t.Run("second pair", func(t *testing.T) {
		addrA, addrB, target := vethAddrsForIndex(2)
		assert.Equal(t, "198.51.100.5/32", addrA)
		assert.Equal(t, "198.51.100.4/32", addrB)
		assert.Equal(t, "198.51.100.5", target)
	})

	t.Run("all pairs unique", func(t *testing.T) {
		seenA := make(map[string]bool)
		seenB := make(map[string]bool)
		for i := uint32(1); i <= 127; i++ {
			addrA, addrB, _ := vethAddrsForIndex(i)
			require.False(t, seenA[addrA], "addrA %s duplicated at index %d", addrA, i)
			require.False(t, seenB[addrB], "addrB %s duplicated at index %d", addrB, i)
			seenA[addrA] = true
			seenB[addrB] = true
		}
	})

	t.Run("no overlap between A and B", func(t *testing.T) {
		all := make(map[string]bool)
		for i := uint32(1); i <= 127; i++ {
			addrA, addrB, _ := vethAddrsForIndex(i)
			require.False(t, all[addrA], "addrA %s overlaps at index %d", addrA, i)
			require.False(t, all[addrB], "addrB %s overlaps at index %d", addrB, i)
			all[addrA] = true
			all[addrB] = true
		}
	})

	t.Run("boundary addresses", func(t *testing.T) {
		addrA, addrB, _ := vethAddrsForIndex(127)
		assert.Equal(t, "198.51.100.255/32", addrA, "last pair A address")
		assert.Equal(t, "198.51.100.254/32", addrB, "last pair B address")
	})

	t.Run("panics at zero", func(t *testing.T) {
		assert.Panics(t, func() { vethAddrsForIndex(0) })
	})

	t.Run("panics beyond 127", func(t *testing.T) {
		assert.Panics(t, func() { vethAddrsForIndex(128) })
	})
}
