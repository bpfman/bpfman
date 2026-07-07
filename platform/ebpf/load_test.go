package ebpf

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cilium/ebpf"

	"github.com/bpfman/bpfman"
)

// xdpPassGlobalsObject holds the same xdp_pass object the external
// discover_test uses; this copy lives in the internal test package
// so the global-data tests can reach the unexported applyGlobalData.
// Read off disk at package-init time -- `go test` runs with the
// package directory as cwd, where the Makefile emits the object.
var xdpPassGlobalsObject = mustReadXDPPass()

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

// xdpPassSpec parses the xdp_pass object into a fresh
// CollectionSpec. xdp_pass.bpf.c declares two globals, config_u8
// (1 byte) and config_u32 (4 bytes), which the global-data tests
// target. Each call returns a new spec so t.Parallel callers do
// not share mutable state.
func xdpPassSpec(t *testing.T) *ebpf.CollectionSpec {
	t.Helper()
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(xdpPassGlobalsObject))
	if err != nil {
		t.Fatalf("load collection spec: %v", err)
	}
	return spec
}

// TestDeclaredTypeMatchesSection pins the load-time policy that a
// declared program type must agree with the ELF section, up to the
// equivalence classes that share a kernel program type. It is the unit
// for the section-vs-declared-type guard in loadProgram.
func TestDeclaredTypeMatchesSection(t *testing.T) {
	t.Parallel()

	match := []struct{ declared, inferred bpfman.ProgramType }{
		// Exact agreement.
		{bpfman.ProgramTypeXDP, bpfman.ProgramTypeXDP},
		{bpfman.ProgramTypeTracepoint, bpfman.ProgramTypeTracepoint},
		{bpfman.ProgramTypeFentry, bpfman.ProgramTypeFentry},
		{bpfman.ProgramTypeFexit, bpfman.ProgramTypeFexit},
		// Probe entry/return interchange within a family.
		{bpfman.ProgramTypeKretprobe, bpfman.ProgramTypeKprobe},
		{bpfman.ProgramTypeKprobe, bpfman.ProgramTypeKretprobe},
		{bpfman.ProgramTypeUretprobe, bpfman.ProgramTypeUprobe},
		// tcx objects compile with the classifier SEC (infers tc).
		{bpfman.ProgramTypeTCX, bpfman.ProgramTypeTC},
		{bpfman.ProgramTypeTC, bpfman.ProgramTypeTCX},
	}
	for _, tc := range match {
		if !declaredTypeMatchesSection(tc.declared, tc.inferred) {
			t.Errorf("declared %s from %s section should be allowed", tc.declared, tc.inferred)
		}
	}

	mismatch := []struct{ declared, inferred bpfman.ProgramType }{
		{bpfman.ProgramTypeKprobe, bpfman.ProgramTypeXDP},    // case B
		{bpfman.ProgramTypeXDP, bpfman.ProgramTypeKprobe},    // case A
		{bpfman.ProgramTypeKprobe, bpfman.ProgramTypeUprobe}, // cross probe family
		{bpfman.ProgramTypeUprobe, bpfman.ProgramTypeKprobe}, // cross probe family
		{bpfman.ProgramTypeKprobe, bpfman.ProgramTypeTracepoint},
		{bpfman.ProgramTypeFentry, bpfman.ProgramTypeFexit},
		{bpfman.ProgramTypeXDP, bpfman.ProgramTypeTC},
	}
	for _, tc := range mismatch {
		if declaredTypeMatchesSection(tc.declared, tc.inferred) {
			t.Errorf("declared %s from %s section should be rejected", tc.declared, tc.inferred)
		}
	}
}

// TestApplyGlobalData_UnknownKeyRejected requires that a global-data
// key that names no variable in the object fails the load, mirroring
// Rust's must_exist=true (aya's ParseError::SymbolNotFound).
func TestApplyGlobalData_UnknownKeyRejected(t *testing.T) {
	t.Parallel()

	err := applyGlobalData(xdpPassSpec(t), map[string][]byte{
		"config_u32":       {1, 0, 0, 0},
		"definitely_bogus": {0, 0, 0, 0},
	})
	if err == nil {
		t.Fatal("unknown global-data key must fail the load")
	}
	if !strings.Contains(err.Error(), "definitely_bogus") {
		t.Fatalf("error must name the unknown key, got: %v", err)
	}
}

// TestApplyGlobalData_KnownKeySucceeds is the happy-path
// regression: a key that names a real variable, sized to match, is
// applied without error.
func TestApplyGlobalData_KnownKeySucceeds(t *testing.T) {
	t.Parallel()

	if err := applyGlobalData(xdpPassSpec(t), map[string][]byte{
		"config_u8":  {7},
		"config_u32": {9, 0, 0, 0},
	}); err != nil {
		t.Fatalf("valid global data must apply cleanly: %v", err)
	}
}

// TestApplyGlobalData_WrongSizeRejected pins the size check Rust
// also enforces (aya's ParseError::InvalidGlobalData): a value
// whose length does not match the variable's size fails. cilium's
// VariableSpec.Set already enforces this; the test guards against a
// future refactor dropping the check.
func TestApplyGlobalData_WrongSizeRejected(t *testing.T) {
	t.Parallel()

	err := applyGlobalData(xdpPassSpec(t), map[string][]byte{
		"config_u32": {1}, // 1 byte for a 4-byte variable
	})
	if err == nil {
		t.Fatal("wrong-sized global data must fail the load")
	}
	if !strings.Contains(err.Error(), "config_u32") {
		t.Fatalf("error must name the variable, got: %v", err)
	}
}

// TestApplyGlobalData_EmptyIsNoOp confirms no globals is not an
// error.
func TestApplyGlobalData_EmptyIsNoOp(t *testing.T) {
	t.Parallel()

	if err := applyGlobalData(xdpPassSpec(t), nil); err != nil {
		t.Fatalf("empty global data must be a no-op, got: %v", err)
	}
}
