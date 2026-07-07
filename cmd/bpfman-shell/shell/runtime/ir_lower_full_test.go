package runtime

import (
	"strings"
	"testing"
)

// TestLower_defer covers DeferStmt: a RegisterDefer with an
// argv built from the defer command's arguments.
func TestLower_defer(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "defer echo bye")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	want := strings.Join([]string{
		"body entry=bb0",
		"",
		"bb0:",
		"  EnterDeferScope kind=program",
		"  BuildArgs t0 = [echo bye]",
		"  RegisterDefer argv=t0 policy=def-then-exec-bind lane=exec",
		"  RunDefers policy=program",
		"  Stop",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// TestLower_assertAndRequire covers AssertStmt in both forms:
// the verb-form path stays in the parser as CommandStmt,
// but the expression form (assert EXPR / require EXPR) lowers
// to Assert or Require carrying the embedded expression.
func TestLower_assertAndRequire(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "assert $x > 0\nrequire $y > 0")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	got := dumpLoweredString(t, lp)
	if !strings.Contains(got, "Assert $x > 0") {
		t.Errorf("expected Assert $x > 0 in dump:\n%s", got)
	}

	if !strings.Contains(got, "Require $y > 0") {
		t.Errorf("expected Require $y > 0 in dump:\n%s", got)
	}
}

// TestLower_letDestructure covers LetDestructureStmt: Eval the
// RHS into a Temp, then BindDestructure against the name list.
func TestLower_letDestructure(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "let (a b _) = [1 2 3]")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	want := strings.Join([]string{
		"body entry=bb0",
		"",
		"bb0:",
		"  EnterDeferScope kind=program",
		"  Eval t0 = [1 2 3]",
		"  BindDestructure [a b _] = t0",
		"  RunDefers policy=program",
		"  Stop",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// TestLower_ifElse covers IfStmt with a then and else branch:
// one Branch terminator, two if-branch frames, and a merge
// block emitted after both branches (bb3 here) so the dump
// reads top-to-bottom.
func TestLower_ifElse(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "if $x { echo yes } else { echo no }")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	got := dumpLoweredString(t, lp)
	mustContain(t, got, "Branch cond=t0 true=bb1 false=bb2")
	mustContain(t, got, "EnterFrame kind=if-branch")
	mustContain(t, got, "Jump bb3")
}

// TestLower_foreach covers a non-collecting foreach: Eval the
// list, ForEach terminator with body and exit blocks, body
// ending in ForEachContinue. The iter frame is owned by the
// interpreter (pushed/popped around each iteration) so no
// explicit EnterFrame/ExitFrame appears in the body.
func TestLower_foreach(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "foreach x in $xs { echo $x }")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	got := dumpLoweredString(t, lp)
	mustContain(t, got, "ForEach list=t0 names=[x] body=bb1 exit=bb2")
	mustContain(t, got, "ForEachContinue")
	if strings.Contains(got, "EnterFrame kind=foreach-iter") {
		t.Errorf("foreach body should not emit EnterFrame; interpreter owns it:\n%s", got)
	}
}

// TestLower_pollStmt covers the statement form of poll: a
// BeginPoll terminator with attempt and success blocks, plus a
// timeout block ending in PropagateError.
func TestLower_pollStmt(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "poll timeout 1s every 10ms { require $ok }")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	got := dumpLoweredString(t, lp)
	mustContain(t, got, "BeginPoll timeout=1s every=10ms attempt=bb1 timeout=bb2 success=bb3")
	mustContain(t, got, "EnterFrame kind=poll-attempt")
	mustContain(t, got, "EnterDeferScope kind=poll-attempt")
	mustContain(t, got, "PropagateError")
}

// TestLower_bindCollect covers the bind-collect path: ForEachCollect
// terminator with body and exit blocks, body ends in
// CollectProduce after the trailing CommandStmt has been
// dispatched in bind position.
func TestLower_bindCollect(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "let xs <- foreach p in $ps { echo $p }")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	got := dumpLoweredString(t, lp)
	mustContain(t, got, "ForEachCollect list=t0 names=[p] target=xs guard=false")
	mustContain(t, got, "CollectProduce")
}

// TestLower_defAndReturn covers a def with a parameter, a
// return value, and an epilogue that runs def-local defers,
// exits the def frame, and emits the bind result.
func TestLower_defAndReturn(t *testing.T) {
	t.Parallel()
	src := "def f(x) { return $x }"
	prog := parseProgram(t, src)
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	if len(lp.Defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(lp.Defs))
	}

	got := dumpLoweredString(t, lp)
	mustContain(t, got, "def f(x) entry=bb0")
	mustContain(t, got, "BindName x = t0")
	mustContain(t, got, "ReturnValue t")
	mustContain(t, got, "RunDefers policy=def-local")
	mustContain(t, got, "EmitBindResult rc=synthetic primary=nil")
}

// TestLower_breakContinue verifies that break lowers to an
// ExitLoop terminator and continue lowers to a ForEachContinue
// terminator. ExitLoop closes the iter frame and any
// intermediate ones before transferring to the loop's exit;
// ForEachContinue does the same and re-enters the body for the
// next iteration.
func TestLower_breakContinue(t *testing.T) {
	t.Parallel()
	src := "foreach x in $xs { if $x { break } else { continue } }"
	prog := parseProgram(t, src)
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	got := dumpLoweredString(t, lp)
	mustContain(t, got, "ExitLoop")
	mustContain(t, got, "ForEachContinue")
}

func mustContain(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("missing %q in dump:\n%s", want, body)
	}
}
