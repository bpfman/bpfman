package ebpf

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestResolveXDPFrags(t *testing.T) {
	t.Parallel()

	const frags = uint32(unix.BPF_F_XDP_HAS_FRAGS)
	const other = uint32(1 << 4) // an unrelated prog_flags bit, must be preserved

	tests := []struct {
		name           string
		specFlags      uint32
		kernelHasFrags bool
		wantFlags      uint32
		wantHasFrags   bool
	}{
		{"non-frags program, kernel supports frags", other, true, other, false},
		{"non-frags program, kernel lacks frags", other, false, other, false},
		{"frags program, kernel supports frags", frags | other, true, frags | other, true},
		{"frags program, kernel lacks frags -> stripped", frags | other, false, other, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotFlags, gotHasFrags := resolveXDPFrags(tt.specFlags, tt.kernelHasFrags)
			if gotFlags != tt.wantFlags {
				t.Errorf("flags = %#x, want %#x", gotFlags, tt.wantFlags)
			}
			if gotHasFrags != tt.wantHasFrags {
				t.Errorf("hasFrags = %v, want %v", gotHasFrags, tt.wantHasFrags)
			}
		})
	}
}
