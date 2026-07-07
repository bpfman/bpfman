package residue

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStaleTestIfaceRe(t *testing.T) {
	t.Parallel()

	shouldMatch := []struct {
		name  string
		input string
	}{
		{"base name", "B0123456789abN"},
		{"veth A suffix", "B0123456789abNa"},
		{"veth B suffix", "B0123456789abNb"},
		{"all zeros", "B000000000000N"},
		{"all f", "BffffffffffffN"},
	}
	for _, tt := range shouldMatch {
		t.Run("match/"+tt.name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, StaleTestIfaceRe.MatchString(tt.input),
				"%q should match", tt.input)
		})
	}

	shouldNotMatch := []struct {
		name  string
		input string
	}{
		{"lowercase b prefix", "b0123456789abN"},
		{"lowercase n suffix", "B0123456789abn"},
		{"too short", "B012345678N"},
		{"too long", "B0123456789abcN"},
		{"uppercase hex", "B0123456789ABN"},
		{"non-hex chars", "B01234567ghijN"},
		{"wrong veth suffix", "B0123456789abNc"},
		{"docker bridge", "br-52726189c7bf"},
		{"old bpfman prefix", "bpfman-chain"},
		{"empty string", ""},
		{"random interface", "eth0"},
	}
	for _, tt := range shouldNotMatch {
		t.Run("reject/"+tt.name, func(t *testing.T) {
			t.Parallel()
			assert.False(t, StaleTestIfaceRe.MatchString(tt.input),
				"%q should not match", tt.input)
		})
	}
}
