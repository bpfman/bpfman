package ebpf_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/platform/ebpf"
)

// xdpPassObject is the compiled xdp_pass BPF object, read off disk
// at package-init time. The Makefile rule
// `platform/ebpf/xdp_pass.bpf.o: e2e/testdata/bpf/xdp_pass.bpf.c`
// emits the object next to this test file, and `go test` runs with
// the package directory as cwd, so the relative path resolves.
var xdpPassObject = mustReadXDPPass()

// mustReadXDPPass reads the compiled xdp_pass object, panicking if
// it is absent -- a missing build artefact is a setup failure, not
// a test condition.
func mustReadXDPPass() []byte {
	b, err := os.ReadFile("xdp_pass.bpf.o")
	if err != nil {
		panic(fmt.Sprintf("read xdp_pass.bpf.o (run `make platform/ebpf/xdp_pass.bpf.o`): %v", err))
	}
	return b
}

// xdpPassReader returns a fresh io.ReaderAt over the xdp_pass BPF
// object. Each call hands back a new bytes.Reader so concurrent
// (t.Parallel) callers don't share read state.
func xdpPassReader() *bytes.Reader {
	return bytes.NewReader(xdpPassObject)
}

func TestValidatePrograms(t *testing.T) {
	t.Parallel()

	// The compiled xdp_pass object holds exactly one program, "pass".
	t.Run("valid programs", func(t *testing.T) {
		t.Parallel()
		err := ebpf.ValidateProgramsFromReader(xdpPassReader(), []string{"pass"})
		if err != nil {
			t.Errorf("ValidatePrograms failed for valid programs: %v", err)
		}
	})

	t.Run("missing program", func(t *testing.T) {
		t.Parallel()
		err := ebpf.ValidateProgramsFromReader(xdpPassReader(), []string{"nonexistent_program_xyz"})
		if err == nil {
			t.Error("expected error for missing program")
		}
	})

	t.Run("mix of valid and invalid", func(t *testing.T) {
		t.Parallel()
		names := []string{"pass", "nonexistent_program_xyz"}
		err := ebpf.ValidateProgramsFromReader(xdpPassReader(), names)
		if err == nil {
			t.Error("expected error for mixed valid/invalid programs")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()
		err := ebpf.ValidateProgramsFromReader(xdpPassReader(), []string{})
		if err != nil {
			t.Errorf("expected no error for empty list: %v", err)
		}
	})

	t.Run("nil list", func(t *testing.T) {
		t.Parallel()
		err := ebpf.ValidateProgramsFromReader(xdpPassReader(), nil)
		if err != nil {
			t.Errorf("expected no error for nil list: %v", err)
		}
	})
}

func TestInferProgramType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		section  string
		expected bpfman.ProgramType
	}{
		{"kprobe/sys_open", bpfman.ProgramTypeKprobe},
		{"kprobe.multi/foo", bpfman.ProgramTypeKprobe},
		{"kretprobe/sys_open", bpfman.ProgramTypeKretprobe},
		{"uprobe/func", bpfman.ProgramTypeUprobe},
		{"uretprobe/func", bpfman.ProgramTypeUretprobe},
		{"tracepoint/syscalls/sys_enter_open", bpfman.ProgramTypeTracepoint},
		{"xdp", bpfman.ProgramTypeXDP},
		{"xdp.frags", bpfman.ProgramTypeXDP},
		{"tc", bpfman.ProgramTypeTC},
		{"classifier/ingress", bpfman.ProgramTypeTC},
		{"tcx/ingress", bpfman.ProgramTypeTCX},
		{"fentry/vfs_read", bpfman.ProgramTypeFentry},
		{"fexit/vfs_read", bpfman.ProgramTypeFexit},
		{"?kprobe/sys_open", bpfman.ProgramTypeKprobe}, // optional prefix
		{"unknown_section", ""},
	}

	for _, tc := range tests {
		t.Run(tc.section, func(t *testing.T) {
			t.Parallel()
			got := ebpf.InferProgramType(tc.section)
			if got != tc.expected {
				t.Errorf("InferProgramType(%q) = %v, want %v", tc.section, got, tc.expected)
			}
		})
	}
}
