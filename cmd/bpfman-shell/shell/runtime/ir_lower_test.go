package runtime

import (
	"strings"
	"testing"
)

// TestLower_empty covers the degenerate case: an empty body
// still opens the program defer scope, closes it, and stops.
// Verifying the wrapper instructions on the empty case prevents
// later changes from silently dropping the program-level
// epilogue.
func TestLower_empty(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	want := strings.Join([]string{
		"body entry=bb0",
		"",
		"bb0:",
		"  EnterDeferScope kind=program",
		"  RunDefers policy=program",
		"  Stop",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// TestLower_command lowers a single command statement and
// asserts both the argv build and the statement-position
// dispatch. The Expr placeholders confirm the AST is reachable
// through the IR rather than discarded.
func TestLower_command(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "echo hello")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	want := strings.Join([]string{
		"body entry=bb0",
		"",
		"bb0:",
		"  EnterDeferScope kind=program",
		"  BuildArgs t0 = [echo hello]",
		"  DispatchCommand argv=t0 policy=def-then-exec-command lane=exec",
		"  RunDefers policy=program",
		"  Stop",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// TestLower_let lowers a plain let assignment: Eval into a
// Temp, then BindName.
func TestLower_let(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "let x = 1")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	want := strings.Join([]string{
		"body entry=bb0",
		"",
		"bb0:",
		"  EnterDeferScope kind=program",
		"  Eval t0 = 1",
		"  BindName x = t0",
		"  RunDefers policy=program",
		"  Stop",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// TestLower_bindNoGuard lowers a non-guard bind: BuildArgs ->
// DispatchBind -> ApplyBind without a fail block. The absence
// of a fail target distinguishes this from the guard form.
func TestLower_bindNoGuard(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "let r <- echo hello")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	want := strings.Join([]string{
		"body entry=bb0",
		"",
		"bb0:",
		"  EnterDeferScope kind=program",
		"  BuildArgs t0 = [echo hello]",
		"  DispatchBind t1 = t0 policy=def-then-exec-bind lane=exec",
		"  ApplyBind src=t1 argv=t0 target=r guard=false",
		"  RunDefers policy=program",
		"  Stop",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// TestLower_bindGuard lowers the guard form. The ApplyBind
// names a fail block; the fail block holds the program-level
// defer unwind and a PropagateError terminator.
func TestLower_bindGuard(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "guard r <- echo hello")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	want := strings.Join([]string{
		"body entry=bb0",
		"",
		"bb0:",
		"  EnterDeferScope kind=program",
		"  BuildArgs t0 = [echo hello]",
		"  DispatchBind t1 = t0 policy=def-then-exec-bind lane=exec",
		"  ApplyBind src=t1 argv=t0 target=r guard=true fail=bb1",
		"  RunDefers policy=program",
		"  Stop",
		"",
		"bb1:",
		"  RunDefers policy=program",
		"  PropagateError",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}
