package ebpf

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestSummariseHelperStderrIgnoresChatter(t *testing.T) {
	t.Parallel()

	stderr := "nsexec[2531020]: INFO: namespace switch requested\n" +
		"nsexec[2531020]: INFO: setns succeeded: mount namespace changed 4026531832 -> 4026532285\n" +
		"nsexec[2531020]: INFO: returning to Go runtime in new mount namespace\n"
	if got := summariseHelperStderr(stderr); got != "" {
		t.Fatalf("log chatter is not a failure reason: got %q", got)
	}
}

func TestSummariseHelperStderrFindsErrorBeforeTrailingChatter(t *testing.T) {
	t.Parallel()

	stderr := "bpfman-ns: error: specific reason\n" +
		"nsexec[99]: INFO: late chatter\n"
	if got := summariseHelperStderr(stderr); got != "specific reason" {
		t.Fatalf("summary mismatch: got %q", got)
	}
}

func TestHelperExitErrorIncludesHelperReason(t *testing.T) {
	t.Parallel()

	err := helperExitError("malloc", "/bin/bash", 1234, 1, "noise\nbpfman-ns: error: specific reason\n")
	if err == nil {
		t.Fatal("expected error")
	}

	got := err.Error()
	if !strings.Contains(got, "bpfman-ns failed attaching malloc to \"/bin/bash\" in container 1234 (exit 1): specific reason") {
		t.Fatalf("error did not include helper reason: %q", got)
	}
}

func TestSummariseHelperStderrKeepsNsexecLine(t *testing.T) {
	t.Parallel()

	line := "nsexec[2531900]: ERROR: failed to open mount namespace /nonexistent: No such file or directory (errno=2)"
	got := summariseHelperStderr(line + "\n")
	if got != line {
		t.Fatalf("summary mismatch: got %q", got)
	}
}

func TestHelperExitErrorWithoutHelperReason(t *testing.T) {
	t.Parallel()

	err := helperExitError("malloc", "/bin/bash", 1234, 1, "\n")
	if err == nil {
		t.Fatal("expected error")
	}

	got := err.Error()
	if strings.Contains(got, ": ") {
		t.Fatalf("error should not include empty helper reason: %q", got)
	}
}

func TestHelperReceiveErrorIncludesPreSendHelperReason(t *testing.T) {
	t.Parallel()

	waitErr := commandExitError(t, 7)
	recvErr := errors.New("recvmsg: connection reset by peer")
	err := helperReceiveError("malloc", "/bin/bash", 1234, recvErr, waitErr, "bpfman-ns: error: specific reason\n")
	if err == nil {
		t.Fatal("expected error")
	}

	got := err.Error()
	if !strings.Contains(got, "bpfman-ns failed attaching malloc to \"/bin/bash\" in container 1234 (exit 7): specific reason") {
		t.Fatalf("error did not include helper reason: %q", got)
	}
	if strings.Contains(got, "receive link fd from child") {
		t.Fatalf("helper reason should take precedence over receive error: %q", got)
	}
}

func TestHelperReceiveErrorFallsBackToReceiveError(t *testing.T) {
	t.Parallel()

	waitErr := commandExitError(t, 7)
	recvErr := errors.New("recvmsg: connection reset by peer")
	err := helperReceiveError("malloc", "/bin/bash", 1234, recvErr, waitErr, "\n")
	if err == nil {
		t.Fatal("expected error")
	}

	got := err.Error()
	if !strings.Contains(got, "receive link fd from child: recvmsg: connection reset by peer") {
		t.Fatalf("error did not include receive failure: %q", got)
	}
}

func TestHelperReceiveErrorFallsBackWhenStderrIsOnlyChatter(t *testing.T) {
	t.Parallel()

	waitErr := commandExitError(t, 3)
	recvErr := errors.New("recvfd: unexpected oob length: got 0, want 24")
	stderr := "nsexec[2531020]: INFO: returning to Go runtime in new mount namespace\n"
	err := helperReceiveError("malloc", "/bin/bash", 1234, recvErr, waitErr, stderr)
	if err == nil {
		t.Fatal("expected error")
	}

	got := err.Error()
	if !strings.Contains(got, "receive link fd from child: recvfd: unexpected oob length") {
		t.Fatalf("error did not fall back to receive failure: %q", got)
	}
	if strings.Contains(got, "INFO") {
		t.Fatalf("log chatter must not be reported as a failure reason: %q", got)
	}
}

func commandExitError(t *testing.T, code int) error {
	t.Helper()

	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	if err == nil {
		t.Fatal("expected command to fail")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected exec.ExitError, got %T", err)
	}

	if exitErr.ExitCode() != code {
		t.Fatalf("exit code mismatch: got %d, want %d", exitErr.ExitCode(), code)
	}
	return err
}

func TestUprobeOptionsSymbolRelative(t *testing.T) {
	t.Parallel()

	symbol, opts := uprobeOptions("malloc", 8, 4242)
	if symbol != "malloc" {
		t.Fatalf("symbol: got %q, want %q", symbol, "malloc")
	}
	if opts.Offset != 8 || opts.Address != 0 {
		t.Fatalf("with a symbol the offset is symbol-relative: got Offset=%d Address=%d", opts.Offset, opts.Address)
	}
	if opts.PID != 4242 {
		t.Fatalf("pid: got %d, want 4242", opts.PID)
	}
}

func TestUprobeOptionsOffsetOnly(t *testing.T) {
	t.Parallel()

	symbol, opts := uprobeOptions("", 0x1234, 0)
	if symbol != "" {
		t.Fatalf("offset-only attach must not pass a symbol: got %q", symbol)
	}
	if opts.Address != 0x1234 || opts.Offset != 0 {
		t.Fatalf("offset-only means absolute file offset via Address: got Address=%#x Offset=%d", opts.Address, opts.Offset)
	}
	if opts.PID != 0 {
		t.Fatalf("pid: got %d, want 0", opts.PID)
	}
}
