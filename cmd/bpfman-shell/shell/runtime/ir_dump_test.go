package runtime

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
)

// TestDumpLowered_body covers the simplest possible program: a
// top-level body with one block whose only instruction is ir.Stop.
// The assertion is exact so any change to the header form, the
// block indentation, the trailing newline, or the instruction
// rendering shows up as a diff.
func TestDumpLowered_body(t *testing.T) {
	t.Parallel()
	bb0 := &ir.BasicBlock{Instrs: []ir.Instr{&ir.Stop{}}}
	lp := &ir.Program{
		Body:   bb0,
		Blocks: []*ir.BasicBlock{bb0},
	}

	want := "body entry=bb0\n\nbb0:\n  Stop\n"
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// TestDumpLowered_defWithBindAndReturn covers a representative
// slice of the instruction set: a def with a guard-bind that
// branches to a cleanup block on failure, a register-defer for
// the success path, and a return that stashes its value before
// running def-local defers.
func TestDumpLowered_defWithBindAndReturn(t *testing.T) {
	t.Parallel()
	// bb_fail: defers + exit + propagate.
	bbFail := &ir.BasicBlock{Instrs: []ir.Instr{
		&ir.RunDefers{Policy: ir.RunDefersDefLocal},
		&ir.ExitFrame{},
		&ir.PropagateError{},
	}}

	// bb_return: defers + exit + emit result (primary = t3, rc synthetic).
	t3 := ir.Temp(3)
	bbReturn := &ir.BasicBlock{Instrs: []ir.Instr{
		&ir.RunDefers{Policy: ir.RunDefersDefLocal},
		&ir.ExitFrame{},
		&ir.EmitBindResult{Primary: &t3},
	}}

	// bb_entry: open frame and defer scope, build argv, dispatch bind,
	// apply guard with fail=bb_fail, register cleanup defer, evaluate
	// return expression into t3, then return.
	bbEntry := &ir.BasicBlock{Instrs: []ir.Instr{
		&ir.EnterFrame{Kind: ir.FrameDef},
		&ir.EnterDeferScope{Kind: ir.DeferScopeDef},
		&ir.BuildArgs{Dst: 0, Args: []ir.Expr{&ir.VarRefExpr{Name: "path"}}},
		&ir.DispatchBind{Dst: 1, Argv: 0, Policy: ir.DispatchPolicyDefThenExecBind},
		&ir.ApplyBind{Src: 1, Argv: 0, Target: "prog", Guard: true, OnFail: bbFail},
		&ir.BuildArgs{Dst: 2, Args: []ir.Expr{&ir.VarRefExpr{Name: "prog"}}},
		&ir.RegisterDefer{Argv: 2, Policy: ir.DispatchPolicyDefThenExecBind},
		&ir.Eval{Dst: 3, Expr: &ir.VarRefExpr{Name: "prog"}},
		&ir.ReturnValue{Src: 3, To: bbReturn},
	}}

	def := &ir.Def{
		Name:     "load_prog",
		Params:   []ir.Param{{Name: "path"}},
		Entry:    bbEntry,
		Blocks:   []*ir.BasicBlock{bbEntry, bbFail, bbReturn},
		NumTemps: 4,
	}

	lp := &ir.Program{Defs: []*ir.Def{def}}

	want := strings.Join([]string{
		"def load_prog(path) entry=bb0",
		"",
		"bb0:",
		"  EnterFrame kind=def",
		"  EnterDeferScope kind=def",
		"  BuildArgs t0 = [$path]",
		"  DispatchBind t1 = t0 policy=def-then-exec-bind lane=unresolved-head",
		"  ApplyBind src=t1 argv=t0 target=prog guard=true fail=bb1",
		"  BuildArgs t2 = [$prog]",
		"  RegisterDefer argv=t2 policy=def-then-exec-bind lane=unresolved-head",
		"  Eval t3 = $prog",
		"  ReturnValue t3 to=bb2",
		"",
		"bb1:",
		"  RunDefers policy=def-local",
		"  ExitFrame",
		"  PropagateError",
		"",
		"bb2:",
		"  RunDefers policy=def-local",
		"  ExitFrame",
		"  EmitBindResult rc=synthetic primary=t3",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// TestDumpLowered_controlFlow exercises ir.Jump, ir.Branch, and the
// poll pseudo-terminator plus its ir.RetryPoll partner.
// One fixture per instruction-family group would be more
// granular but this single fixture keeps the snapshot's
// surface visible in one place.
func TestDumpLowered_controlFlow(t *testing.T) {
	t.Parallel()
	bb1 := &ir.BasicBlock{Instrs: []ir.Instr{&ir.Stop{}}}
	bb2 := &ir.BasicBlock{Instrs: []ir.Instr{&ir.Stop{}}}
	bb3 := &ir.BasicBlock{Instrs: []ir.Instr{&ir.Stop{}}}
	bb4 := &ir.BasicBlock{Instrs: []ir.Instr{
		&ir.RetryPoll{},
	}}
	bb0 := &ir.BasicBlock{Instrs: []ir.Instr{
		&ir.Branch{Cond: 0, True: bb1, False: bb2},
		&ir.Jump{Target: bb3},
		&ir.BeginPoll{
			Timeout:   1_000_000_000,
			Every:     50_000_000,
			Attempt:   bb1,
			OnTimeout: bb2,
			OnSuccess: bb3,
		},
	}}

	lp := &ir.Program{
		Body:   bb0,
		Blocks: []*ir.BasicBlock{bb0, bb1, bb2, bb3, bb4},
	}

	want := strings.Join([]string{
		"body entry=bb0",
		"",
		"bb0:",
		"  Branch cond=t0 true=bb1 false=bb2",
		"  Jump bb3",
		"  BeginPoll timeout=1s every=50ms attempt=bb1 timeout=bb2 success=bb3",
		"",
		"bb1:",
		"  Stop",
		"",
		"bb2:",
		"  Stop",
		"",
		"bb3:",
		"  Stop",
		"",
		"bb4:",
		"  RetryPoll",
		"",
	}, "\n")
	if got := dumpLoweredString(t, lp); got != want {
		t.Fatalf("dump mismatch\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestDumpIRExpr_ComplexForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr ir.Expr
		want string
	}{
		{
			name: "interpolation",
			expr: &ir.InterpStringExpr{
				Segments: []ir.InterpStringSegment{
					{Literal: "hello-"},
					{Expr: &ir.VarRefExpr{Name: "name"}},
					{Literal: "-"},
					{Expr: &ir.BinaryExpr{
						Left:  &ir.VarRefExpr{Name: "lhs"},
						Op:    "+",
						Right: &ir.VarRefExpr{Name: "rhs"},
					}},
				},
			},
			want: `"hello-${name}-${$lhs + $rhs}"`,
		},
		{
			name: "thread",
			expr: &ir.ThreadExpr{
				LHS: &ir.VarRefExpr{Name: "src"},
				Args: []ir.Expr{
					&ir.LiteralExpr{Text: "jq"},
					&ir.LiteralExpr{Text: ".id", Quoted: true},
				},
			},
			want: `$src |> jq ".id"`,
		},
		{
			name: "purecall",
			expr: &ir.PureCallExpr{
				Name: "stage",
				Args: []ir.Expr{
					&ir.VarRefExpr{Name: "src"},
					&ir.LiteralExpr{Text: "42"},
				},
			},
			want: `stage $src 42`,
		},
		{
			name: "matches",
			expr: &ir.MatchesExpr{
				Target: &ir.VarRefExpr{Name: "src"},
				Block: &ir.MatchesBlockExpr{
					Exhaustive: true,
					Entries: []ir.MatchEntry{
						{
							Path:    "status.id",
							Pattern: &ir.VarRefExpr{Name: "want"},
						},
						{
							Path: "status.meta",
							SubBlock: &ir.MatchesBlockExpr{
								Entries: []ir.MatchEntry{{
									Path:    "name",
									Pattern: &ir.LiteralExpr{Text: "demo", Quoted: true},
								}},
							},
						},
					},
				},
			},
			want: `$src matches exhaustive { status.id: $want, status.meta: matches { name: "demo" } }`,
		},
	}

	for _, tc := range tests {
		if got := ir.FormatExprSource(tc.expr); got != tc.want {
			t.Fatalf("%s: ir.FormatExprSource() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func dumpLoweredString(t *testing.T, lp *ir.Program) string {
	t.Helper()
	var buf bytes.Buffer
	if err := ir.Dump(&buf, lp); err != nil {
		t.Fatalf("ir.Dump: %v", err)
	}

	return buf.String()
}
