package ebpf

import (
	"syscall"
	"testing"

	"github.com/cilium/ebpf/link"
)

// TestShouldFallbackToGeneric covers the BPFMAN-43 decision: a
// native-mode XDP attach rejected with ERANGE retries in generic
// mode, and nothing else does -- so a generic-mode attach that also
// returns ERANGE cannot loop.
func TestShouldFallbackToGeneric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		err   error
		flags link.XDPAttachFlags
		want  bool
	}{
		{"ERANGE in native mode falls back", syscall.ERANGE, 0, true},
		{"success does not fall back", nil, 0, false},
		{"non-ERANGE error does not fall back", syscall.EPERM, 0, false},
		{"ERANGE already in generic mode does not loop", syscall.ERANGE, link.XDPGenericMode, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldFallbackToGeneric(tt.err, tt.flags); got != tt.want {
				t.Errorf("shouldFallbackToGeneric(%v, %d) = %v, want %v", tt.err, tt.flags, got, tt.want)
			}
		})
	}
}
