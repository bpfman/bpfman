package cliformat

import "testing"

// The map formatters must emit entries in sorted key order so that
// -o text output is deterministic and matches encoding/json, which
// sorts map keys. With the old range-over-map implementation these
// assertions would pass or fail at random.

func TestFormatMetadata_SortsByKey(t *testing.T) {
	t.Parallel()
	got := formatMetadata(map[string]string{"zeta": "1", "alpha": "2", "mid": "3"})
	want := "alpha=2, mid=3, zeta=1"
	if got != want {
		t.Errorf("formatMetadata() = %q, want %q", got, want)
	}
}

func TestFormatGlobalData_SortsByKey(t *testing.T) {
	t.Parallel()
	got := formatGlobalData(map[string][]byte{"zeta": {0x01}, "alpha": {0x02}})
	want := "alpha=02, zeta=01"
	if got != want {
		t.Errorf("formatGlobalData() = %q, want %q", got, want)
	}
}
