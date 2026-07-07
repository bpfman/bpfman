package runtime

import (
	"testing"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

func TestLower_EvalLowersLiteralVarListAndOperators(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, `
let lit = 1
let ref = $src.path
let xs = [1 $src]
let ok = not false and (1 < 2)
`)
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	body := lp.Body.Instrs
	if len(body) < 9 {
		t.Fatalf("expected enough body instructions, got %d", len(body))
	}

	litEval, ok := body[1].(*ir.Eval)
	if !ok {
		t.Fatalf("body[1] = %T, want *ir.Eval", body[1])
	}
	if _, ok := litEval.Expr.(*ir.LiteralExpr); !ok {
		t.Fatalf("lit eval expr = %T, want *ir.LiteralExpr", litEval.Expr)
	}

	refEval, ok := body[3].(*ir.Eval)
	if !ok {
		t.Fatalf("body[3] = %T, want *ir.Eval", body[3])
	}
	ref, ok := refEval.Expr.(*ir.VarRefExpr)
	if !ok {
		t.Fatalf("ref eval expr = %T, want *ir.VarRefExpr", refEval.Expr)
	}
	if ref.Name != "src" || ref.Path != "path" {
		t.Fatalf("ref eval = %+v, want name=src path=path", ref)
	}

	listEval, ok := body[5].(*ir.Eval)
	if !ok {
		t.Fatalf("body[5] = %T, want *ir.Eval", body[5])
	}
	list, ok := listEval.Expr.(*ir.ListExpr)
	if !ok {
		t.Fatalf("list eval expr = %T, want *ir.ListExpr", listEval.Expr)
	}
	if len(list.Elems) != 2 {
		t.Fatalf("list elems = %d, want 2", len(list.Elems))
	}
	if _, ok := list.Elems[0].(*ir.LiteralExpr); !ok {
		t.Fatalf("list elem[0] = %T, want *ir.LiteralExpr", list.Elems[0])
	}
	if _, ok := list.Elems[1].(*ir.VarRefExpr); !ok {
		t.Fatalf("list elem[1] = %T, want *ir.VarRefExpr", list.Elems[1])
	}

	boolEval, ok := body[7].(*ir.Eval)
	if !ok {
		t.Fatalf("body[7] = %T, want *ir.Eval", body[7])
	}
	logical, ok := boolEval.Expr.(*ir.LogicalExpr)
	if !ok {
		t.Fatalf("bool eval expr = %T, want *ir.LogicalExpr", boolEval.Expr)
	}
	if _, ok := logical.Left.(*ir.NotExpr); !ok {
		t.Fatalf("logical left = %T, want *ir.NotExpr", logical.Left)
	}
	if _, ok := logical.Right.(*ir.BinaryExpr); !ok {
		t.Fatalf("logical right = %T, want *ir.BinaryExpr", logical.Right)
	}
}

func TestLower_BuildArgsLowersLiteralAndVarRefs(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "echo hello $path")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	build, ok := lp.Body.Instrs[1].(*ir.BuildArgs)
	if !ok {
		t.Fatalf("body[1] = %T, want *ir.BuildArgs", lp.Body.Instrs[1])
	}
	if len(build.Args) != 3 {
		t.Fatalf("build args = %d, want 3", len(build.Args))
	}
	if _, ok := build.Args[0].(*ir.LiteralExpr); !ok {
		t.Fatalf("build arg[0] = %T, want *ir.LiteralExpr", build.Args[0])
	}
	if _, ok := build.Args[1].(*ir.LiteralExpr); !ok {
		t.Fatalf("build arg[1] = %T, want *ir.LiteralExpr", build.Args[1])
	}
	if _, ok := build.Args[2].(*ir.VarRefExpr); !ok {
		t.Fatalf("build arg[2] = %T, want *ir.VarRefExpr", build.Args[2])
	}
}

func TestLower_BuildArgsLowersAdapterExpr(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "echo file:$path.value")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	build, ok := lp.Body.Instrs[1].(*ir.BuildArgs)
	if !ok {
		t.Fatalf("body[1] = %T, want *ir.BuildArgs", lp.Body.Instrs[1])
	}
	if len(build.Args) != 2 {
		t.Fatalf("build args = %d, want 2", len(build.Args))
	}
	adapter, ok := build.Args[1].(*ir.AdapterExpr)
	if !ok {
		t.Fatalf("build arg[1] = %T, want *ir.AdapterExpr", build.Args[1])
	}
	if adapter.Adapter != "file" || adapter.Name != "path" || adapter.Path != "value" {
		t.Fatalf("adapter = %+v, want adapter=file name=path path=value", adapter)
	}
}

func TestLower_InterpStringLowersSegments(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, `
let msg = "prog-${$id}-${$lhs + $rhs}"
echo "hello-${$name}"
`)
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	letEval, ok := lp.Body.Instrs[1].(*ir.Eval)
	if !ok {
		t.Fatalf("body[1] = %T, want *ir.Eval", lp.Body.Instrs[1])
	}
	interp, ok := letEval.Expr.(*ir.InterpStringExpr)
	if !ok {
		t.Fatalf("let interp = %T, want *ir.InterpStringExpr", letEval.Expr)
	}
	if len(interp.Segments) != 4 {
		t.Fatalf("interp segments = %d, want 4", len(interp.Segments))
	}
	if interp.Segments[0].Literal != "prog-" {
		t.Fatalf("segment[0] literal = %q, want %q", interp.Segments[0].Literal, "prog-")
	}
	if _, ok := interp.Segments[1].Expr.(*ir.VarRefExpr); !ok {
		t.Fatalf("segment[1] expr = %T, want *ir.VarRefExpr", interp.Segments[1].Expr)
	}
	if interp.Segments[2].Literal != "-" {
		t.Fatalf("segment[2] literal = %q, want %q", interp.Segments[2].Literal, "-")
	}
	bin, ok := interp.Segments[3].Expr.(*ir.BinaryExpr)
	if !ok {
		t.Fatalf("segment[3] expr = %T, want *ir.BinaryExpr", interp.Segments[3].Expr)
	}
	if _, ok := bin.Left.(*ir.VarRefExpr); !ok {
		t.Fatalf("segment[3] left = %T, want *ir.VarRefExpr", bin.Left)
	}
	if _, ok := bin.Right.(*ir.VarRefExpr); !ok {
		t.Fatalf("segment[3] right = %T, want *ir.VarRefExpr", bin.Right)
	}

	build, ok := lp.Body.Instrs[3].(*ir.BuildArgs)
	if !ok {
		t.Fatalf("body[3] = %T, want *ir.BuildArgs", lp.Body.Instrs[3])
	}
	argInterp, ok := build.Args[1].(*ir.InterpStringExpr)
	if !ok {
		t.Fatalf("build arg[1] = %T, want *ir.InterpStringExpr", build.Args[1])
	}
	if len(argInterp.Segments) != 2 {
		t.Fatalf("arg interp segments = %d, want 2", len(argInterp.Segments))
	}
	if _, ok := argInterp.Segments[1].Expr.(*ir.VarRefExpr); !ok {
		t.Fatalf("arg interp segment[1] expr = %T, want *ir.VarRefExpr", argInterp.Segments[1].Expr)
	}
}

func TestLower_ThreadAndPureCallLower(t *testing.T) {
	t.Parallel()
	name := "u32le"
	prog := parseProgram(t, `
let piped = $src |> jq ".id"
let called = `+name+` $src
echo ($src |> jq ".id") (`+name+` $src)
`)
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	var evals []*ir.Eval
	var builds []*ir.BuildArgs
	for _, ins := range lp.Body.Instrs {
		switch v := ins.(type) {
		case *ir.Eval:
			evals = append(evals, v)
		case *ir.BuildArgs:
			builds = append(builds, v)
		}
	}
	if len(evals) < 2 {
		t.Fatalf("evals = %d, want >= 2", len(evals))
	}
	thread, ok := evals[0].Expr.(*ir.ThreadExpr)
	if !ok {
		t.Fatalf("eval[0] expr = %T, want *ir.ThreadExpr", evals[0].Expr)
	}
	if _, ok := thread.LHS.(*ir.VarRefExpr); !ok {
		t.Fatalf("thread lhs = %T, want *ir.VarRefExpr", thread.LHS)
	}
	if len(thread.Args) != 2 {
		t.Fatalf("thread args = %d, want 2", len(thread.Args))
	}
	if _, ok := thread.Args[0].(*ir.LiteralExpr); !ok {
		t.Fatalf("thread arg[0] = %T, want *ir.LiteralExpr", thread.Args[0])
	}

	call, ok := evals[1].Expr.(*ir.PureCallExpr)
	if !ok {
		t.Fatalf("eval[1] expr = %T, want *ir.PureCallExpr", evals[1].Expr)
	}
	if call.Name != name {
		t.Fatalf("pure call name = %q, want %q", call.Name, name)
	}
	if len(call.Args) != 1 {
		t.Fatalf("pure call args = %d, want 1", len(call.Args))
	}
	if _, ok := call.Args[0].(*ir.VarRefExpr); !ok {
		t.Fatalf("pure call arg[0] = %T, want *ir.VarRefExpr", call.Args[0])
	}

	if len(builds) == 0 {
		t.Fatalf("expected at least one ir.BuildArgs")
	}
	build := builds[len(builds)-1]
	if len(build.Args) != 3 {
		t.Fatalf("build args = %d, want 3", len(build.Args))
	}
	if _, ok := build.Args[1].(*ir.ThreadExpr); !ok {
		t.Fatalf("build arg[1] = %T, want *ir.ThreadExpr", build.Args[1])
	}
	if _, ok := build.Args[2].(*ir.PureCallExpr); !ok {
		t.Fatalf("build arg[2] = %T, want *ir.PureCallExpr", build.Args[2])
	}
}

func TestIRExpr_ASTRoundTripPreservesInterpStructure(t *testing.T) {
	t.Parallel()

	span := source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 20}}
	original := &syntax.InterpStringExpr{
		Segments: []syntax.InterpStringSegment{
			{Literal: "prefix-"},
			{Expr: &syntax.BinaryExpr{
				Left:  &syntax.VarRefExpr{Name: "lhs", Span: span},
				Op:    "+",
				Right: &syntax.VarRefExpr{Name: "rhs", Span: span},
				Span:  span,
			}},
			{Literal: "-tail"},
		},
		Span: span,
	}

	roundTrip := lowerTestExpr(t, astExprFromIR(lowerTestExpr(t, original)))
	interp, ok := roundTrip.(*ir.InterpStringExpr)
	if !ok {
		t.Fatalf("roundTrip = %T, want *ir.InterpStringExpr", roundTrip)
	}
	if len(interp.Segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(interp.Segments))
	}
	if interp.Segments[0].Literal != "prefix-" {
		t.Fatalf("segment[0] literal = %q, want %q", interp.Segments[0].Literal, "prefix-")
	}
	if interp.Segments[2].Literal != "-tail" {
		t.Fatalf("segment[2] literal = %q, want %q", interp.Segments[2].Literal, "-tail")
	}
	bin, ok := interp.Segments[1].Expr.(*ir.BinaryExpr)
	if !ok {
		t.Fatalf("segment[1] expr = %T, want *ir.BinaryExpr", interp.Segments[1].Expr)
	}
	if _, ok := bin.Left.(*ir.VarRefExpr); !ok {
		t.Fatalf("segment[1] left = %T, want *ir.VarRefExpr", bin.Left)
	}
	if _, ok := bin.Right.(*ir.VarRefExpr); !ok {
		t.Fatalf("segment[1] right = %T, want *ir.VarRefExpr", bin.Right)
	}
}

func TestIRExpr_ASTRoundTripPreservesMatchesStructure(t *testing.T) {
	t.Parallel()

	span := source.Span{Pos: source.Pos{File: "main.bpfman", Line: 1, Col: 1}, End: source.Pos{File: "main.bpfman", Line: 4, Col: 1}}
	original := &syntax.MatchesBlockExpr{
		Entries: []syntax.MatchEntry{
			{
				Path: "status.kernel.id",
				Pattern: &syntax.BinaryExpr{
					Left:  &syntax.VarRefExpr{Name: "lhs", Span: span},
					Op:    "+",
					Right: &syntax.VarRefExpr{Name: "rhs", Span: span},
					Span:  span,
				},
				Span: span,
			},
			{
				Path:      "status.kernel.tag",
				Predicate: "not-empty",
				Span:      span,
			},
			{
				Path: "record",
				SubBlock: &syntax.MatchesBlockExpr{
					Entries: []syntax.MatchEntry{{
						Path:    "name",
						Pattern: &syntax.LiteralExpr{Text: "demo", Quoted: true, Span: span},
						Span:    span,
					}},
					Span: span,
				},
				Span: span,
			},
		},
		Exhaustive: true,
		Span:       span,
	}

	block := lowerTestMatchesBlock(t, astMatchesBlockFromIR(lowerTestMatchesBlock(t, original)))
	if !block.Exhaustive {
		t.Fatalf("roundTrip exhaustive = false, want true")
	}
	if len(block.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(block.Entries))
	}
	pattern, ok := block.Entries[0].Pattern.(*ir.BinaryExpr)
	if !ok {
		t.Fatalf("entry[0] pattern = %T, want *ir.BinaryExpr", block.Entries[0].Pattern)
	}
	if _, ok := pattern.Left.(*ir.VarRefExpr); !ok {
		t.Fatalf("entry[0] left = %T, want *ir.VarRefExpr", pattern.Left)
	}
	if _, ok := pattern.Right.(*ir.VarRefExpr); !ok {
		t.Fatalf("entry[0] right = %T, want *ir.VarRefExpr", pattern.Right)
	}
	if block.Entries[1].Predicate != "not-empty" {
		t.Fatalf("entry[1] predicate = %q, want %q", block.Entries[1].Predicate, "not-empty")
	}
	if block.Entries[2].SubBlock == nil {
		t.Fatalf("entry[2] sub-block = nil, want nested block")
	}
	if _, ok := block.Entries[2].SubBlock.Entries[0].Pattern.(*ir.LiteralExpr); !ok {
		t.Fatalf("nested entry pattern = %T, want *ir.LiteralExpr", block.Entries[2].SubBlock.Entries[0].Pattern)
	}
}

func TestEvalExpr_ThreadAndPureCallDispatch(t *testing.T) {
	t.Parallel()

	name := "u32le"
	s := NewSession()
	s.Set("x", StringValue("42"))

	var threadArgs []Arg
	threadEnv := &Env{
		Session: s,
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			threadArgs = args
			return StringValue("thread-ok"), nil
		}),
	}
	threadExpr := &ir.ThreadExpr{
		LHS:  &ir.VarRefExpr{Name: "x"},
		Args: []ir.Expr{&ir.LiteralExpr{Text: "jq"}, &ir.LiteralExpr{Text: ".", Quoted: true}},
	}
	got, err := evalExpr(threadExpr, threadEnv)
	if err != nil {
		t.Fatalf("evalExpr(thread): %v", err)
	}
	if text, err := got.Scalar(); err != nil || text != "thread-ok" {
		t.Fatalf("thread result = (%q, %v), want (%q, nil)", text, err, "thread-ok")
	}
	if len(threadArgs) != 3 {
		t.Fatalf("thread dispatch argc = %d, want 3", len(threadArgs))
	}
	if _, ok := threadArgs[2].(ScalarValueArg); !ok {
		t.Fatalf("thread dispatch last arg = %T, want ScalarValueArg", threadArgs[2])
	}

	var callArgs []Arg
	callEnv := &Env{
		Session: s,
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			callArgs = args
			return StringValue("pure-ok"), nil
		}),
	}
	callExpr := &ir.PureCallExpr{
		Name: name,
		Args: []ir.Expr{&ir.VarRefExpr{Name: "x"}},
	}
	got, err = evalExpr(callExpr, callEnv)
	if err != nil {
		t.Fatalf("evalExpr(pure): %v", err)
	}
	if text, err := got.Scalar(); err != nil || text != "pure-ok" {
		t.Fatalf("pure result = (%q, %v), want (%q, nil)", text, err, "pure-ok")
	}
	if len(callArgs) != 2 {
		t.Fatalf("pure dispatch argc = %d, want 2", len(callArgs))
	}
	if head, ok := callArgs[0].(WordArg); !ok || head.Text != name {
		t.Fatalf("pure dispatch head = %#v, want WordArg(%q)", callArgs[0], name)
	}
	if _, ok := callArgs[1].(ScalarValueArg); !ok {
		t.Fatalf("pure dispatch arg[1] = %T, want ScalarValueArg", callArgs[1])
	}
}

func TestEvalArg_ThreadWrapsThreadResultAsArg(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("42"))
	env := &Env{
		Session: s,
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			return StringValue("piped"), nil
		}),
	}
	arg, err := evalArg(&ir.ThreadExpr{
		LHS:  &ir.VarRefExpr{Name: "x"},
		Args: []ir.Expr{&ir.LiteralExpr{Text: "stage"}},
	}, env)
	if err != nil {
		t.Fatalf("evalArg(thread): %v", err)
	}
	scalar, ok := arg.(ScalarValueArg)
	if !ok {
		t.Fatalf("arg = %T, want ScalarValueArg", arg)
	}
	if scalar.Text != "piped" {
		t.Fatalf("arg text = %q, want %q", scalar.Text, "piped")
	}
}

func TestEvalArg_AdapterResolvesValue(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("path", StringValue("/tmp/pin"))
	arg, err := evalArg(&ir.AdapterExpr{
		Adapter: "file",
		Name:    "path",
		Path:    "",
	}, &Env{Session: s})
	if err != nil {
		t.Fatalf("evalArg(adapter): %v", err)
	}
	adapter, ok := arg.(AdapterArg)
	if !ok {
		t.Fatalf("arg = %T, want AdapterArg", arg)
	}
	if adapter.Adapter != "file" || adapter.Name != "path" {
		t.Fatalf("adapter arg = %+v, want adapter=file name=path", adapter)
	}
}

func TestLower_AssertMatchesLowersToMatchesExpr(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, `assert $prog matches {
    status.kernel.id: $id
    status.kernel.tag: not-empty
    record: matches {
        name: "demo"
    }
}`)
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	assertInstr, ok := lp.Body.Instrs[1].(*ir.Assert)
	if !ok {
		t.Fatalf("body[1] = %T, want *ir.Assert", lp.Body.Instrs[1])
	}
	clause, ok := assertInstr.Clause.(*ir.AssertExprClause)
	if !ok {
		t.Fatalf("assert clause = %T, want *ir.AssertExprClause", assertInstr.Clause)
	}
	matches, ok := clause.Expr.(*ir.MatchesExpr)
	if !ok {
		t.Fatalf("assert expr = %T, want *ir.MatchesExpr", clause.Expr)
	}
	if _, ok := matches.Target.(*ir.VarRefExpr); !ok {
		t.Fatalf("target = %T, want *ir.VarRefExpr", matches.Target)
	}
	block := matches.Block
	if block == nil {
		t.Fatalf("matches block = nil, want *ir.MatchesBlockExpr")
	}
	if len(block.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(block.Entries))
	}
	if _, ok := block.Entries[0].Pattern.(*ir.VarRefExpr); !ok {
		t.Fatalf("entry[0] pattern = %T, want *ir.VarRefExpr", block.Entries[0].Pattern)
	}
	if block.Entries[1].Predicate != "not-empty" {
		t.Fatalf("entry[1] predicate = %q, want %q", block.Entries[1].Predicate, "not-empty")
	}
	if block.Entries[2].SubBlock == nil {
		t.Fatalf("entry[2] sub-block = nil, want nested block")
	}
	if len(block.Entries[2].SubBlock.Entries) != 1 {
		t.Fatalf("nested entries = %d, want 1", len(block.Entries[2].SubBlock.Entries))
	}
	if _, ok := block.Entries[2].SubBlock.Entries[0].Pattern.(*ir.LiteralExpr); !ok {
		t.Fatalf("nested entry pattern = %T, want *ir.LiteralExpr", block.Entries[2].SubBlock.Entries[0].Pattern)
	}
}

func TestEvalMatchesBlock_ResolvesPatterns(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("id", StringValue("42"))
	block, err := evalMatchesBlock(&ir.MatchesBlockExpr{
		Entries: []ir.MatchEntry{
			{
				Path:    "status.kernel.id",
				Pattern: &ir.VarRefExpr{Name: "id"},
			},
			{
				Path:      "status.kernel.tag",
				Predicate: "not-empty",
			},
			{
				Path: "record",
				SubBlock: &ir.MatchesBlockExpr{
					Entries: []ir.MatchEntry{{
						Path:    "name",
						Pattern: &ir.LiteralExpr{Text: "demo", Quoted: true},
					}},
				},
			},
		},
	}, &Env{Session: s})
	if err != nil {
		t.Fatalf("evalMatchesBlock: %v", err)
	}
	if len(block.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(block.Entries))
	}
	if text, err := block.Entries[0].Value.Scalar(); err != nil || text != "42" {
		t.Fatalf("entry[0] value = (%q, %v), want (%q, nil)", text, err, "42")
	}
	if block.Entries[1].Predicate != "not-empty" {
		t.Fatalf("entry[1] predicate = %q, want %q", block.Entries[1].Predicate, "not-empty")
	}
	if block.Entries[2].SubBlock == nil || len(block.Entries[2].SubBlock.Entries) != 1 {
		t.Fatalf("entry[2] sub-block = %#v, want one nested entry", block.Entries[2].SubBlock)
	}
	if text, err := block.Entries[2].SubBlock.Entries[0].Value.Scalar(); err != nil || text != "demo" {
		t.Fatalf("nested value = (%q, %v), want (%q, nil)", text, err, "demo")
	}
}

func TestLower_AssertLowersBooleanExprTree(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, "assert not false and (1 < 2)")
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	assertInstr, ok := lp.Body.Instrs[1].(*ir.Assert)
	if !ok {
		t.Fatalf("body[1] = %T, want *ir.Assert", lp.Body.Instrs[1])
	}
	clause, ok := assertInstr.Clause.(*ir.AssertExprClause)
	if !ok {
		t.Fatalf("assert clause = %T, want *ir.AssertExprClause", assertInstr.Clause)
	}
	logical, ok := clause.Expr.(*ir.LogicalExpr)
	if !ok {
		t.Fatalf("assert expr = %T, want *ir.LogicalExpr", clause.Expr)
	}
	if _, ok := logical.Left.(*ir.NotExpr); !ok {
		t.Fatalf("assert left = %T, want *ir.NotExpr", logical.Left)
	}
	if _, ok := logical.Right.(*ir.BinaryExpr); !ok {
		t.Fatalf("assert right = %T, want *ir.BinaryExpr", logical.Right)
	}
}

func lowerTestExpr(t *testing.T, expr syntax.Expr) ir.Expr {
	t.Helper()

	span := syntax.NodeSpan(expr)
	prog := &syntax.Program{
		Stmts: []syntax.Stmt{&syntax.AssertStmt{
			Clause: &syntax.AssertExprClause{Expr: expr},
			Span:   span,
		}},
		Span: span,
	}
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	assertInstr, ok := lp.Body.Instrs[1].(*ir.Assert)
	if !ok {
		t.Fatalf("body[1] = %T, want *ir.Assert", lp.Body.Instrs[1])
	}
	clause, ok := assertInstr.Clause.(*ir.AssertExprClause)
	if !ok {
		t.Fatalf("assert clause = %T, want *ir.AssertExprClause", assertInstr.Clause)
	}
	return clause.Expr
}

func lowerTestMatchesBlock(t *testing.T, block *syntax.MatchesBlockExpr) *ir.MatchesBlockExpr {
	t.Helper()

	expr := &syntax.MatchesExpr{
		Target: &syntax.VarRefExpr{Name: "subject", Span: block.Span},
		Block:  block,
		Span:   block.Span,
	}
	lowered := lowerTestExpr(t, expr)
	matches, ok := lowered.(*ir.MatchesExpr)
	if !ok {
		t.Fatalf("lowered expr = %T, want *ir.MatchesExpr", lowered)
	}
	return matches.Block
}
