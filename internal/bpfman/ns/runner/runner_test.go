package runner

import (
	"errors"
	"reflect"
	"testing"

	"github.com/bpfman/bpfman/internal/bpfman/ns"
)

func TestDetectNamespaceHelperInvocation_NotHelper(t *testing.T) {
	t.Parallel()

	inv, ok, err := DetectNamespaceHelperInvocation(
		[]string{"bpfman", "serve"},
		"", // no BPFMAN_MODE
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatalf("expected ok=false, got true (inv=%+v)", inv)
	}
}

func TestDetectNamespaceHelperInvocation_ModeEnvVar(t *testing.T) {
	t.Parallel()

	inv, ok, err := DetectNamespaceHelperInvocation(
		[]string{"bpfman", "link", "attach", "uprobe", "42", "/bin/bash"},
		ns.ModeBPFManNS, // BPFMAN_MODE=bpfman-ns
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ok {
		t.Fatalf("expected ok=true, got false")
	}

	// For env-triggered invocation, strip argv[0] and pass through the rest.
	wantArgs := []string{"link", "attach", "uprobe", "42", "/bin/bash"}
	if !reflect.DeepEqual(inv.Args, wantArgs) {
		t.Fatalf("args mismatch:\nwant: %#v\ngot:  %#v", wantArgs, inv.Args)
	}
}

func TestDetectNamespaceHelperInvocation_ModeEnvVar_BpfmanRpc(t *testing.T) {
	t.Parallel()

	// BPFMAN_MODE=bpfman-rpc should NOT trigger helper mode (valid, but not helper)
	inv, ok, err := DetectNamespaceHelperInvocation(
		[]string{"bpfman", "serve"},
		ns.ModeBPFManRPC,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatalf("expected ok=false for BPFMAN_MODE=bpfman-rpc, got true (inv=%+v)", inv)
	}
}

func TestDetectNamespaceHelperInvocation_ModeEnvVar_UnknownValue(t *testing.T) {
	t.Parallel()

	// BPFMAN_MODE=something-else should return an error
	_, _, err := DetectNamespaceHelperInvocation(
		[]string{"bpfman", "serve"},
		"something-else",
	)
	if err == nil {
		t.Fatalf("expected error for unknown BPFMAN_MODE, got nil")
	}
}

func TestDetectNamespaceHelperInvocation_EmptyArgv(t *testing.T) {
	t.Parallel()

	inv, ok, err := DetectNamespaceHelperInvocation(
		[]string{},
		"",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatalf("expected ok=false for empty argv, got true (inv=%+v)", inv)
	}
}

func TestHandleNamespaceHelperInvocation_NotHelper_DoesNotRun(t *testing.T) {
	t.Parallel()

	var ran bool
	runner := func(_ NamespaceHelperInvocation) error {
		ran = true
		return nil
	}

	handled, err := HandleNamespaceHelperInvocation(
		[]string{"bpfman", "serve"},
		"", // no BPFMAN_MODE
		runner,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if handled {
		t.Fatalf("expected handled=false, got true")
	}

	if ran {
		t.Fatalf("runner should not have been called")
	}
}

func TestHandleNamespaceHelperInvocation_Helper_RunsAndReturnsHandled(t *testing.T) {
	t.Parallel()

	var got NamespaceHelperInvocation
	runner := func(inv NamespaceHelperInvocation) error {
		got = inv
		return nil
	}

	argv := []string{"bpfman", "link", "attach", "uprobe", "42", "/bin/bash"}
	handled, err := HandleNamespaceHelperInvocation(
		argv,
		ns.ModeBPFManNS,
		runner,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !handled {
		t.Fatalf("expected handled=true, got false")
	}

	wantArgs := []string{"link", "attach", "uprobe", "42", "/bin/bash"}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("invocation args mismatch:\nwant: %#v\ngot:  %#v", wantArgs, got.Args)
	}
}

func TestHandleNamespaceHelperInvocation_Helper_PropagatesRunnerError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	runner := func(_ NamespaceHelperInvocation) error {
		return wantErr
	}

	handled, err := HandleNamespaceHelperInvocation(
		[]string{"bpfman", "uprobe"},
		ns.ModeBPFManNS,
		runner,
	)
	if !handled {
		t.Fatalf("expected handled=true, got false")
	}

	if !errors.Is(err, wantErr) {
		t.Fatalf("error mismatch:\nwant: %v\ngot:  %v", wantErr, err)
	}
}

func TestHandleNamespaceHelperInvocation_UnknownMode_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := func(_ NamespaceHelperInvocation) error {
		t.Fatal("runner should not be called for unknown mode")
		return nil
	}

	handled, err := HandleNamespaceHelperInvocation(
		[]string{"bpfman", "serve"},
		"unknown-mode",
		runner,
	)
	if handled {
		t.Fatalf("expected handled=false for unknown mode")
	}

	if err == nil {
		t.Fatalf("expected error for unknown BPFMAN_MODE")
	}
}
