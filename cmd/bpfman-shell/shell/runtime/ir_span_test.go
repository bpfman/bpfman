package runtime

import (
	"testing"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

func lowerSpanProgram(t *testing.T, src string) (*syntax.Program, *ir.Program) {
	t.Helper()
	prog := parseProgram(t, src)
	lp, err := lowerToIR(prog)
	require.NoError(t, err)
	return prog, lp
}

func blockInstrs(blocks []*ir.BasicBlock) []ir.Instr {
	var out []ir.Instr
	for _, blk := range blocks {
		out = append(out, blk.Instrs...)
	}
	return out
}

func traceNoteByMsg(t *testing.T, blocks []*ir.BasicBlock, msg string) *ir.TraceNote {
	t.Helper()
	for _, ins := range blockInstrs(blocks) {
		if v, ok := ins.(*ir.TraceNote); ok && v.Msg == msg {
			return v
		}
	}
	t.Fatalf("trace note %q not found", msg)
	return nil
}

func TestLowerSpans_LetBindAndDefer(t *testing.T) {
	t.Parallel()

	prog, lp := lowerSpanProgram(t, "let x = 1\nlet r <- echo hello\ndefer echo bye")

	letStmt := prog.Stmts[0].(*syntax.LetStmt)
	bindStmt := prog.Stmts[1].(*syntax.BindStmt)
	deferStmt := prog.Stmts[2].(*syntax.DeferStmt)

	var eval *ir.Eval
	var bindName *ir.BindName
	var buildArgs []*ir.BuildArgs
	var dispatchBind *ir.DispatchBind
	var applyBind *ir.ApplyBind
	var registerDefer *ir.RegisterDefer

	for _, ins := range blockInstrs(lp.Blocks) {
		switch v := ins.(type) {
		case *ir.Eval:
			if eval == nil {
				eval = v
			}
		case *ir.BindName:
			if v.Name == "x" {
				bindName = v
			}
		case *ir.BuildArgs:
			buildArgs = append(buildArgs, v)
		case *ir.DispatchBind:
			dispatchBind = v
		case *ir.ApplyBind:
			applyBind = v
		case *ir.RegisterDefer:
			registerDefer = v
		}
	}

	require.NotNil(t, eval)
	require.NotNil(t, bindName)
	require.Len(t, buildArgs, 2)
	require.NotNil(t, dispatchBind)
	require.NotNil(t, applyBind)
	require.NotNil(t, registerDefer)

	assert.Equal(t, letStmt.Span, eval.Span)
	assert.Equal(t, letStmt.Span, bindName.Span)
	assert.Equal(t, bindStmt.Cmd.Span, buildArgs[0].Span)
	assert.Equal(t, bindStmt.Span, dispatchBind.Span)
	assert.Equal(t, bindStmt.Cmd.Span.Pos, dispatchBind.CallPos)
	assert.Equal(t, bindStmt.Span, applyBind.Span)
	assert.Equal(t, deferStmt.Cmd.Span, buildArgs[1].Span)
	assert.Equal(t, deferStmt.Span, registerDefer.Span)
}

func TestLowerSpans_IfBranchCommands(t *testing.T) {
	t.Parallel()

	src := "if $x {\n  echo yes\n} else {\n  echo no\n}"
	prog, lp := lowerSpanProgram(t, src)

	ifStmt := prog.Stmts[0].(*syntax.IfStmt)
	thenCmd := ifStmt.Then[0].(*syntax.CommandStmt)
	elseCmd := ifStmt.Else[0].(*syntax.CommandStmt)

	var branch *ir.Branch
	var dispatches []source.Span

	for _, ins := range blockInstrs(lp.Blocks) {
		switch v := ins.(type) {
		case *ir.Branch:
			branch = v
		case *ir.DispatchCommand:
			dispatches = append(dispatches, v.Span)
		}
	}

	require.NotNil(t, branch)
	require.Len(t, dispatches, 2)

	assert.Equal(t, ifStmt.Span, branch.Span)
	assert.ElementsMatch(t, []source.Span{thenCmd.Span, elseCmd.Span}, dispatches)
}

func TestLowerSpans_ForEachContinue(t *testing.T) {
	t.Parallel()

	src := "foreach x in $xs {\n  continue\n}"
	prog, lp := lowerSpanProgram(t, src)

	forEachStmt := prog.Stmts[0].(*syntax.ForEachStmt)
	continueStmt := forEachStmt.Body[0].(*syntax.ContinueStmt)

	var loop *ir.ForEach
	var cont *ir.ForEachContinue

	for _, ins := range blockInstrs(lp.Blocks) {
		switch v := ins.(type) {
		case *ir.ForEach:
			loop = v
		case *ir.ForEachContinue:
			if v.Span == continueStmt.Span {
				cont = v
			}
		}
	}

	require.NotNil(t, loop)
	require.NotNil(t, cont)

	assert.Equal(t, forEachStmt.Span, loop.Span)
	assert.Equal(t, continueStmt.Span, traceNoteByMsg(t, lp.Blocks, "continue").Span)
	assert.Equal(t, continueStmt.Span, cont.Span)
}

func TestLowerSpans_DefReturn(t *testing.T) {
	t.Parallel()

	src := "def f(x) {\n  return $x\n}"
	prog, lp := lowerSpanProgram(t, src)

	defStmt := prog.Stmts[0].(*syntax.DefStmt)
	retStmt := defStmt.Body[0].(*syntax.ReturnStmt)

	require.Len(t, lp.Defs, 1)
	def := lp.Defs[0]

	var ret *ir.ReturnValue
	for _, ins := range blockInstrs(def.Blocks) {
		if v, ok := ins.(*ir.ReturnValue); ok {
			ret = v
			break
		}
	}

	require.NotNil(t, ret)

	assert.Equal(t, defStmt.Span, def.Span)
	assert.Equal(t, retStmt.Span, ret.Span)
}

func TestLowerSpans_PollAttemptAndRequire(t *testing.T) {
	t.Parallel()

	src := "poll timeout 1s every 10ms {\n  require $ok\n}"
	prog, lp := lowerSpanProgram(t, src)

	poll := prog.Stmts[0].(*syntax.PollStmt)
	req := poll.Body[0].(*syntax.AssertStmt)

	var begin *ir.BeginPoll
	var runtimeAssert *ir.Assert

	for _, ins := range blockInstrs(lp.Blocks) {
		switch v := ins.(type) {
		case *ir.BeginPoll:
			begin = v
		case *ir.Assert:
			runtimeAssert = v
		}
	}

	require.NotNil(t, begin)
	require.NotNil(t, runtimeAssert)

	assert.Equal(t, poll.Span, begin.Span)
	assert.Equal(t, poll.Span, traceNoteByMsg(t, lp.Blocks, "poll attempt").Span)
	assert.Equal(t, poll.Span, traceNoteByMsg(t, lp.Blocks, "poll timeout").Span)
	assert.Equal(t, req.Span, runtimeAssert.Span)
}
