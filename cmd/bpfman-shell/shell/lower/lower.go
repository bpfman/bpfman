// AST -> IR lowering.
//
// The lowerer is a recursive walker that emits one block per
// region of control flow. Per-unit state lives on a *lowerer
// (a fresh one is created for the program body and for each
// def); a shared *lowerState collects every def regardless of
// nesting. Lexical context that survives across statements --
// the current defer-unwind policy, the enclosing def's return
// target, the enclosing poll attempt's retry/fatal
// targets, the stack of foreach loops for break/continue --
// sits on the lowerer too.

package lower

import (
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// Lower converts a parsed *syntax.Program into its canonical lowered
// form. The returned ir.Program holds the body's entry
// block, every block reachable from it in emission order, the
// temp count, and every def encountered at any nesting depth.
func Lower(prog *syntax.Program) (*ir.Program, error) {
	state := &lowerState{}
	l := newLowerer(state, lowerCtx{deferPolicy: ir.RunDefersProgram})
	body, err := l.lowerBody(prog.Stmts, prog.Span)
	if err != nil {
		return nil, err
	}
	return &ir.Program{
		Defs:     state.defs,
		Body:     body,
		Blocks:   l.blocks,
		NumTemps: int(l.nextTemp),
		Span:     prog.Span,
	}, nil
}

// lowerState is shared across every lowerer running over one
// Lower() call. Defs collected from any nesting depth land in
// the same slice so the dumper can render them in source order
// at the ir.Program level.
type lowerState struct {
	defs []*ir.Def
}

// lowerCtx captures per-unit policy that flows across the
// statement walk: which defer policy a guard fail should use,
// where return statements jump, and -- inside a poll
// attempt -- how explicit retry should unwind. Saving and
// restoring the whole struct lets the
// statement helpers nest cleanly.
type lowerCtx struct {
	deferPolicy ir.RunDefersPolicy
	returnTo    *ir.BasicBlock
	attempt     *ir.BasicBlock
	fatal       *ir.BasicBlock
	// nonTopLevel marks that the current statement walk is
	// inside an if/elif/else branch, a foreach body, or a poll
	// body. lowerDefStmt consults it (alongside returnTo, which
	// marks "inside a def body") to refuse any def declared
	// outside the program's top level. The static checker
	// applies the same gate via nonTopLevelDepth; pinning it in
	// the lowerer keeps Lower defensive when Check is not in
	// the path. Save and restore via the same ctx swap as
	// returnTo / attempt.
	nonTopLevel bool
}

// loopFrame is one entry on the foreach stack. Exit is the
// block control transfers to on a break statement; continue
// emits a ir.ForEachContinue terminator without needing an entry
// here, but keeping the frame makes the "in any loop" check
// trivial.
type loopFrame struct {
	Exit *ir.BasicBlock
}

// lowerer holds per-unit lowering state.
type lowerer struct {
	state    *lowerState
	ctx      lowerCtx
	blocks   []*ir.BasicBlock
	cur      *ir.BasicBlock
	nextTemp ir.Temp
	loops    []loopFrame
}

func newLowerer(state *lowerState, ctx lowerCtx) *lowerer {
	return &lowerer{state: state, ctx: ctx}
}

func (l *lowerer) newBlock(sp source.Span) *ir.BasicBlock {
	b := &ir.BasicBlock{Span: sp}
	l.blocks = append(l.blocks, b)
	return b
}

// reserveBlock creates a ir.BasicBlock without adding it to the
// emission list. Callers use this to allocate a block whose
// position should come later in the dump (for example, the
// merge block of an if-chain that should sit after its
// branches). publishBlock adds the reserved block to the
// emission list at the point the caller wants it to appear.
func (l *lowerer) reserveBlock(sp source.Span) *ir.BasicBlock {
	return &ir.BasicBlock{Span: sp}
}

func (l *lowerer) publishBlock(b *ir.BasicBlock) {
	l.blocks = append(l.blocks, b)
}

func (l *lowerer) emit(i ir.Instr) {
	l.cur.Instrs = append(l.cur.Instrs, i)
}

func (l *lowerer) alloc() ir.Temp {
	t := l.nextTemp
	l.nextTemp++
	return t
}

// lowerBody lowers a top-level statement sequence into the
// program-level entry block: open the program defer scope,
// walk each statement, then close with a program defer unwind
// and ir.Stop. Any blocks created by guard fails, control flow,
// or nested constructs are appended to l.blocks during the
// walk and may interleave with the entry block in emission
// order.
func (l *lowerer) lowerBody(stmts []syntax.Stmt, span source.Span) (*ir.BasicBlock, error) {
	entry := l.newBlock(span)
	l.cur = entry
	l.emit(&ir.EnterDeferScope{Kind: ir.DeferScopeProgram, Span: span})
	for _, s := range stmts {
		if err := l.lowerStmt(s); err != nil {
			return nil, err
		}
	}
	l.emit(&ir.RunDefers{Policy: ir.RunDefersProgram, Span: span})
	l.emit(&ir.Stop{Span: span})
	return entry, nil
}

// lowerStmt dispatches on syntax.Stmt type to the right per-statement
// helper. Unsupported constructs produce an explicit error so
// gaps stay visible.
func (l *lowerer) lowerStmt(s syntax.Stmt) error {
	switch v := s.(type) {
	case *syntax.CommandStmt:
		return l.lowerCommandStmt(v)
	case *syntax.ExprStmt:
		return l.lowerExprStmt(v)
	case *syntax.LetStmt:
		return l.lowerLetStmt(v)
	case *syntax.LetDestructureStmt:
		return l.lowerLetDestructureStmt(v)
	case *syntax.BindStmt:
		return l.lowerBindStmt(v)
	case *syntax.DeferStmt:
		return l.lowerDeferStmt(v)
	case *syntax.AssertStmt:
		return l.lowerAssertStmt(v)
	case *syntax.IfStmt:
		return l.lowerIfStmt(v)
	case *syntax.ForEachStmt:
		return l.lowerForEachStmt(v)
	case *syntax.BreakStmt:
		return l.lowerBreakStmt(v)
	case *syntax.ContinueStmt:
		return l.lowerContinueStmt(v)
	case *syntax.PollStmt:
		return l.lowerPollStmt(v)
	case *syntax.RetryStmt:
		return l.lowerRetryStmt(v)
	case *syntax.DefStmt:
		return l.lowerDefStmt(v)
	case *syntax.ReturnStmt:
		return l.lowerReturnStmt(v)
	default:
		sp := syntax.NodeSpan(s)
		return fmt.Errorf("lower: statement %T at %d:%d not yet supported", s, sp.Pos.Line, sp.Pos.Col)
	}
}

// lowerCommandStmt is the command-position dispatch path: pack
// the arguments into an argv ir.Temp, then ir.DispatchCommand.
func (l *lowerer) lowerCommandStmt(s *syntax.CommandStmt) error {
	argv := l.alloc()
	l.emit(&ir.BuildArgs{Dst: argv, Args: lowerIRExprs(s.Args), Span: s.Span})
	l.emit(&ir.DispatchCommand{
		Argv:   argv,
		Policy: ir.DispatchPolicyDefThenExecCommand,
		Trace:  true,
		Span:   s.Span,
	})
	return nil
}

// lowerExprStmt evaluates a top-level expression statement and
// forwards the result to the driver's PrintResult hook.
func (l *lowerer) lowerExprStmt(s *syntax.ExprStmt) error {
	v := l.alloc()
	l.emit(&ir.Eval{Dst: v, Expr: lowerIRExpr(s.Expr), Span: s.Span})
	l.emit(&ir.EmitResult{Src: v, Trace: true, Span: s.Span})
	return nil
}

// lowerLetStmt is the value-bind path: evaluate the RHS into a
// ir.Temp, then bind the named identifier to that ir.Temp.
func (l *lowerer) lowerLetStmt(s *syntax.LetStmt) error {
	v := l.alloc()
	l.emit(&ir.Eval{Dst: v, Expr: lowerIRExpr(s.RHS), Span: s.Span})
	l.emit(&ir.BindName{
		Name:        s.Name.Text,
		Src:         v,
		TracePrefix: fmt.Sprintf("let %s = ", s.Name.Text),
		Span:        s.Span,
	})
	return nil
}

// lowerLetDestructureStmt is the positional destructure form:
// evaluate the RHS into a ir.Temp, then ir.BindDestructure against
// the name list. The interpreter validates list length and
// shape at runtime.
func (l *lowerer) lowerLetDestructureStmt(s *syntax.LetDestructureStmt) error {
	src := l.alloc()
	l.emit(&ir.Eval{Dst: src, Expr: lowerIRExpr(s.RHS), Span: s.Span})
	l.emit(&ir.BindDestructure{Names: identTexts(s.Names), Src: src, Trace: true, Span: s.Span})
	return nil
}

// lowerDeferStmt evaluates the defer command's arguments into
// an argv ir.Temp at registration time and queues that argv on
// the innermost open defer scope.
func (l *lowerer) lowerDeferStmt(s *syntax.DeferStmt) error {
	argv := l.alloc()
	l.emit(&ir.BuildArgs{Dst: argv, Args: lowerIRExprs(s.Cmd.Args), Span: s.Cmd.Span})
	l.emit(&ir.RegisterDefer{
		Argv:   argv,
		Policy: ir.DispatchPolicyDefThenExecBind,
		Trace:  true,
		Span:   s.Span,
	})
	return nil
}

// lowerAssertStmt emits an ir.Assert instruction carrying the
// expression to evaluate and the require/assert distinction.
// Failure-policy routing happens in the interpreter via the
// Env.
func (l *lowerer) lowerAssertStmt(s *syntax.AssertStmt) error {
	l.emit(&ir.Assert{IsRequire: s.IsRequire, Clause: lowerAssertClause(s.Clause), Span: s.Span})
	return nil
}

func lowerAssertClause(c syntax.AssertClause) ir.AssertClause {
	switch v := c.(type) {
	case *syntax.AssertExprClause:
		return &ir.AssertExprClause{Expr: lowerIRExpr(v.Expr)}
	case *syntax.AssertCommandClause:
		return &ir.AssertCommandClause{
			Head:     v.Head,
			HeadSpan: v.HeadSpan,
			Args:     lowerIRExprs(v.Args),
			Negate:   v.Negate,
		}
	default:
		panic(fmt.Sprintf("lowerAssertClause: unsupported %T", c))
	}
}

// lowerIfStmt lowers an if / elif / else chain into a sequence
// of conditional branches that all rejoin at a single merge
// block. Each taken branch runs inside its own if-branch frame
// so bindings introduced in the branch do not leak.
func (l *lowerer) lowerIfStmt(s *syntax.IfStmt) error {
	merge := l.reserveBlock(s.Span)
	if err := l.lowerIfChain(s.Span, s.Span, s.Cond, s.Then, s.Elifs, s.Else, merge, "if then"); err != nil {
		return err
	}
	l.publishBlock(merge)
	l.cur = merge
	return nil
}

// lowerIfChain recursively lowers one conditional and its
// false-side continuation. The false side is the next elif,
// the else body, or the merge block when nothing follows.
// Recursion keeps the elif chain in source order in the
// emission list.
func (l *lowerer) lowerIfChain(span, traceSpan source.Span, cond syntax.Expr, then []syntax.Stmt, elifs []syntax.IfBranch, elseBody []syntax.Stmt, merge *ir.BasicBlock, takenTrace string) error {
	condTemp := l.alloc()
	l.emit(&ir.Eval{Dst: condTemp, Expr: lowerIRExpr(cond), Span: span})

	thenBlock := l.newBlock(span)
	var nextBlock *ir.BasicBlock
	switch {
	case len(elifs) > 0:
		nextBlock = l.newBlock(elifs[0].Span)
	case elseBody != nil:
		nextBlock = l.newBlock(span)
	default:
		nextBlock = l.newBlock(span)
	}
	l.emit(&ir.Branch{Cond: condTemp, True: thenBlock, False: nextBlock, Span: span})

	if err := l.lowerIfBranchBody(thenBlock, then, span, traceSpan, merge, takenTrace); err != nil {
		return err
	}

	if len(elifs) > 0 {
		l.cur = nextBlock
		return l.lowerIfChain(elifs[0].Span, traceSpan, elifs[0].Cond, elifs[0].Body, elifs[1:], elseBody, merge, "if elif")
	}
	if elseBody != nil {
		if err := l.lowerIfBranchBody(nextBlock, elseBody, span, traceSpan, merge, "if else"); err != nil {
			return err
		}
		return nil
	}
	l.cur = nextBlock
	l.emit(&ir.TraceNote{Msg: "if skip", Span: traceSpan})
	l.emit(&ir.Jump{Target: merge, Span: traceSpan})
	return nil
}

// lowerIfBranchBody fills one if-branch block with its frame
// pair and body, then jumps to the merge.
func (l *lowerer) lowerIfBranchBody(block *ir.BasicBlock, body []syntax.Stmt, span, traceSpan source.Span, merge *ir.BasicBlock, traceMsg string) error {
	l.cur = block
	l.emit(&ir.TraceNote{Msg: traceMsg, Span: traceSpan})
	l.emit(&ir.EnterFrame{Kind: ir.FrameIfBranch, Span: span})
	saved := l.ctx
	l.ctx.nonTopLevel = true
	for _, st := range body {
		if err := l.lowerStmt(st); err != nil {
			l.ctx = saved
			return err
		}
	}
	l.ctx = saved
	l.emit(&ir.ExitFrame{Span: span})
	l.emit(&ir.Jump{Target: merge, Span: span})
	return nil
}

// lowerForEachStmt lowers a non-collecting foreach: evaluate
// the list, emit the structured ir.ForEach terminator with body
// and exit blocks, then lower the body in its own iteration
// frame. The body ends with a ir.ForEachContinue terminator that
// drives the iterator forward (or to the exit when exhausted).
func (l *lowerer) lowerForEachStmt(s *syntax.ForEachStmt) error {
	listTemp := l.alloc()
	l.emit(&ir.Eval{Dst: listTemp, Expr: lowerIRExpr(s.List), Span: s.Span})

	body := l.newBlock(s.Span)
	exit := l.newBlock(s.Span)
	l.emit(&ir.ForEach{
		List:  listTemp,
		Names: identTexts(s.Names),
		Body:  body,
		Exit:  exit,
		Span:  s.Span,
	})

	l.loops = append(l.loops, loopFrame{Exit: exit})
	defer func() { l.loops = l.loops[:len(l.loops)-1] }()

	l.cur = body
	// The iter frame is owned by the ir.ForEach instruction at
	// runtime: ir.ForEach pushes it and binds Names before transferring
	// to the body, ir.ForEachContinue pops it back off when the
	// iteration ends. Lowering does not emit explicit frame
	// markers because they would otherwise pair against intermediate
	// frames opened mid-body and obscure the loop boundary.
	saved := l.ctx
	l.ctx.nonTopLevel = true
	for _, st := range s.Body {
		if err := l.lowerStmt(st); err != nil {
			l.ctx = saved
			return err
		}
	}
	l.ctx = saved
	l.emit(&ir.ForEachContinue{Span: s.Span})

	l.cur = exit
	return nil
}

// lowerBreakStmt emits an ir.ExitLoop terminator that the
// interpreter resolves against the innermost foreach. Unlike a
// plain ir.Jump, ir.ExitLoop also closes every frame opened during
// the current iteration so cleanup is symmetric with the
// natural body end.
// Subsequent statements at the source position are
// unreachable; the lowerer creates an unreachable block so
// emission can continue without crashing.
func (l *lowerer) lowerBreakStmt(s *syntax.BreakStmt) error {
	if len(l.loops) == 0 {
		return fmt.Errorf("lower: break at %d:%d outside any loop", s.Pos.Line, s.Pos.Col)
	}
	l.emit(&ir.TraceNote{Msg: "break", Span: s.Span})
	l.emit(&ir.ExitLoop{Span: s.Span})
	l.cur = l.newBlock(s.Span)
	return nil
}

// lowerContinueStmt terminates the current block with a
// ir.ForEachContinue and starts an unreachable continuation
// block; reach-aware passes can ignore the trailing block.
func (l *lowerer) lowerContinueStmt(s *syntax.ContinueStmt) error {
	if len(l.loops) == 0 {
		return fmt.Errorf("lower: continue at %d:%d outside any loop", s.Pos.Line, s.Pos.Col)
	}
	l.emit(&ir.TraceNote{Msg: "continue", Span: s.Span})
	l.emit(&ir.ForEachContinue{Span: s.Span})
	l.cur = l.newBlock(s.Span)
	return nil
}

// lowerPollStmt lowers the statement-only polling construct. Its
// body runs in an attempt-local frame and defer scope; explicit
// retry unwinds the attempt and loops, while ordinary failures
// remain fatal.
func (l *lowerer) lowerPollStmt(s *syntax.PollStmt) error {
	attempt := l.newBlock(s.Span)
	timeout := l.newBlock(s.Span)
	done := l.newBlock(s.Span)

	l.emit(&ir.BeginPoll{
		Timeout:   s.Timeout,
		Every:     s.Every,
		Attempt:   attempt,
		OnTimeout: timeout,
		OnSuccess: done,
		Span:      s.Span,
	})

	saved := l.ctx
	l.ctx.attempt = attempt
	l.ctx.fatal = timeout
	l.ctx.deferPolicy = ir.RunDefersAttemptFatal
	l.ctx.nonTopLevel = true

	l.cur = attempt
	l.emit(&ir.TraceNote{Msg: "poll attempt", Span: s.Span})
	l.emit(&ir.EnterFrame{Kind: ir.FramePollAttempt, Span: s.Span})
	l.emit(&ir.EnterDeferScope{Kind: ir.DeferScopePollAttempt, Span: s.Span})
	for _, st := range s.Body {
		if err := l.lowerStmt(st); err != nil {
			l.ctx = saved
			return err
		}
	}
	l.emit(&ir.RunDefers{Policy: ir.RunDefersAttemptFatal, Span: s.Span})
	l.emit(&ir.ExitFrame{Span: s.Span})
	l.emit(&ir.Jump{Target: done, Span: s.Span})

	l.ctx = saved

	l.cur = timeout
	l.emit(&ir.TraceNote{Msg: "poll timeout", Span: s.Span})
	l.emit(&ir.PropagateError{Span: s.Span})

	l.cur = done
	return nil
}

func (l *lowerer) lowerRetryStmt(s *syntax.RetryStmt) error {
	// retry only makes sense inside a poll attempt's lexical
	// body (l.ctx.attempt set) or inside a helper def callable
	// from a poll attempt (l.ctx.returnTo set). The emitted
	// sequence drains attempt-local defers and pops the
	// attempt frame; without an enclosing attempt or def
	// frame it would dismantle the program-level frame and
	// the runtime cannot recover. The static checker catches
	// the same shape, but Lower runs without check as a
	// precondition, so refuse the misplaced form here rather
	// than emit IR that corrupts the executor's frame stack.
	if l.ctx.attempt == nil && l.ctx.returnTo == nil {
		return syntax.SpanErrorf(s.Span, "retry outside any poll or helper def")
	}
	if s.Unless == nil {
		l.emitRetryStmt(s)
		return nil
	}
	cond := l.alloc()
	l.emit(&ir.Eval{Dst: cond, Expr: lowerIRExpr(s.Unless), Span: s.Span})
	continueBlock := l.newBlock(s.Span)
	retryBlock := l.newBlock(s.Span)
	l.emit(&ir.Branch{Cond: cond, True: continueBlock, False: retryBlock, Span: s.Span})
	l.cur = retryBlock
	l.emitRetryStmt(s)
	l.cur = continueBlock
	return nil
}

func (l *lowerer) emitRetryStmt(s *syntax.RetryStmt) {
	var msg *ir.Temp
	if s.Message != nil {
		temp := l.alloc()
		l.emit(&ir.Eval{Dst: temp, Expr: lowerIRExpr(s.Message), Span: s.Span})
		msg = &temp
	}
	l.emit(&ir.RunDefers{Policy: ir.RunDefersAttemptFatal, Span: s.Span})
	l.emit(&ir.ExitFrame{Span: s.Span})
	l.emit(&ir.RetryPoll{Message: msg, Span: s.Span})
	l.cur = l.newBlock(s.Span)
}

// lowerDefStmt creates a fresh lowerer for the def body so the
// outer unit's blocks, temps, and loop stack stay separate.
// The def's epilogue block is allocated first so any
// syntax.ReturnStmt encountered during the body walk can target it;
// parameter names bind against the first N temp slots, which
// the interpreter populates with call arguments. The lowered
// def is recorded on the program's Defs list; canonical
// lowering does not emit a body-time ir.RegisterDef instruction
// because top-level defs are hoisted before body execution.
func (l *lowerer) lowerDefStmt(s *syntax.DefStmt) error {
	// Top-level defs are the only shape the IR Defs list is meant
	// to carry: registerLoweredDefs hoists every entry into the
	// session at the start of Exec, so a def nested in an
	// if/elif/else branch, a foreach body, a poll body, or
	// another def body would become globally visible regardless
	// of the conditional that lexically surrounds it. The static
	// checker catches this via nonTopLevelDepth, but Lower is
	// callable without Check as a precondition; refuse the
	// shape here so the IR shape stays well-formed no matter
	// how the lowerer is reached. returnTo covers the
	// def-inside-def case (set on entry to the outer def body);
	// nonTopLevel covers the if / foreach / poll branches that
	// bump it around their bodies.
	if l.ctx.returnTo != nil || l.ctx.nonTopLevel {
		return syntax.SpanErrorf(s.Span, "def %q must be declared at top level", s.Name.Text)
	}
	defL := newLowerer(l.state, lowerCtx{deferPolicy: ir.RunDefersDefLocal})
	defL.nextTemp = ir.Temp(len(s.Params))

	epilogue := defL.reserveBlock(s.Span)
	defL.ctx.returnTo = epilogue

	entry := defL.newBlock(s.Span)
	defL.cur = entry
	defL.emit(&ir.EnterFrame{Kind: ir.FrameDef, Span: s.Span})
	defL.emit(&ir.EnterDeferScope{Kind: ir.DeferScopeDef, Span: s.Span})
	for i, p := range s.Params {
		defL.emit(&ir.BindName{Name: p.Name.Text, Src: ir.Temp(i), Span: s.Span})
	}
	for _, st := range s.Body {
		if err := defL.lowerStmt(st); err != nil {
			return err
		}
	}
	defL.emit(&ir.Jump{Target: epilogue, Span: s.Span})

	defL.publishBlock(epilogue)
	defL.cur = epilogue
	defL.emit(&ir.RunDefers{Policy: ir.RunDefersDefLocal, Span: s.Span})
	defL.emit(&ir.ExitFrame{Span: s.Span})
	defL.emit(&ir.EmitBindResult{Span: s.Span})

	def := &ir.Def{
		Name:      s.Name.Text,
		Params:    irDefParams(s.Params),
		HasReturn: bodyHasReturn(s.Body),
		Entry:     entry,
		Blocks:    defL.blocks,
		NumTemps:  int(defL.nextTemp),
		Span:      s.Span,
	}
	l.state.defs = append(l.state.defs, def)
	return nil
}

// lowerReturnStmt evaluates the return value into a ir.Temp and
// emits a ir.ReturnValue terminator targeting the def's epilogue.
// A return outside any def is an error -- the checker should
// catch it earlier, but the lowerer reports it too in case
// the AST reaches us with that mistake.
func (l *lowerer) lowerReturnStmt(s *syntax.ReturnStmt) error {
	if l.ctx.returnTo == nil {
		return fmt.Errorf("lower: return at %d:%d outside any def", s.Pos.Line, s.Pos.Col)
	}
	src := l.alloc()
	l.emit(&ir.Eval{Dst: src, Expr: lowerIRExpr(s.Expr), Span: s.Span})
	l.emit(&ir.ReturnValue{Src: src, To: l.ctx.returnTo, Trace: true, Span: s.Span})
	l.cur = l.newBlock(s.Span)
	return nil
}

// lowerBindStmt dispatches on the bind kind: command, collect,
// or bind-collect. The legacy command path keeps its own helper;
// the collect path is bigger and lives next to lowerPoll for
// symmetry.
func (l *lowerer) lowerBindStmt(s *syntax.BindStmt) error {
	switch {
	case s.Collect != nil:
		return l.lowerBindCollect(s)
	case s.Cmd == nil:
		return fmt.Errorf("lower: bind statement at %d:%d has no dispatch target", s.Pos.Line, s.Pos.Col)
	}
	return l.lowerBindCmd(s)
}

// lowerBindCmd lowers the command-form bind. Guard binds get a
// fail block whose shape depends on context: inside a poll
// attempt the fail block runs attempt-fatal cleanup; otherwise
// it unwinds the current defer scope and PropagateErrors.
func (l *lowerer) lowerBindCmd(s *syntax.BindStmt) error {
	argv := l.alloc()
	l.emit(&ir.BuildArgs{Dst: argv, Args: lowerIRExprs(s.Cmd.Args), Span: s.Cmd.Span})
	result := l.alloc()
	l.emit(&ir.DispatchBind{
		Dst:         result,
		Argv:        argv,
		CallPos:     s.Cmd.Span.Pos,
		Policy:      ir.DispatchPolicyDefThenExecBind,
		TraceHeader: bindTraceHeader(s),
		Span:        s.Span,
	})

	apply := &ir.ApplyBind{
		Src:    result,
		Argv:   argv,
		Target: s.Target.Text,
		Guard:  s.Guard,
		Span:   s.Span,
	}
	if s.Guard {
		apply.OnFail = l.buildGuardFailBlock(s.Span)
	}
	l.emit(apply)
	return nil
}

// buildGuardFailBlock allocates a fail-cleanup block tailored
// to the current unit. Inside a poll attempt the block still
// runs attempt-local cleanup, but ordinary guard failure stays
// fatal; polling retries only when an explicit retry statement
// runs.
func (l *lowerer) buildGuardFailBlock(sp source.Span) *ir.BasicBlock {
	saved := l.cur
	fb := l.newBlock(sp)
	l.cur = fb
	if l.ctx.attempt != nil {
		l.emit(&ir.RunDefers{Policy: ir.RunDefersAttemptFatal, Span: sp})
		l.emit(&ir.ExitFrame{Span: sp})
		l.emit(&ir.PropagateError{Span: sp})
	} else {
		l.emit(&ir.RunDefers{Policy: l.ctx.deferPolicy, Span: sp})
		if l.ctx.returnTo != nil {
			l.emit(&ir.ExitFrame{Span: sp})
		}
		l.emit(&ir.PropagateError{Span: sp})
	}
	l.cur = saved
	return fb
}

// lowerBindCollect lowers bind-collect: the body iterates the
// list, the body's final syntax.CommandStmt dispatches in bind
// position, and the per-iteration result is collected via a
// ir.CollectProduce terminator. The interpreter binds either
// collected values (guard) or an aggregate outcome (let).
func (l *lowerer) lowerBindCollect(s *syntax.BindStmt) error {
	fe := s.Collect
	if len(fe.Body) == 0 {
		return fmt.Errorf("lower: bind-collect at %d:%d has empty body", s.Pos.Line, s.Pos.Col)
	}
	finalCmd, ok := fe.Body[len(fe.Body)-1].(*syntax.CommandStmt)
	if !ok {
		return fmt.Errorf("lower: bind-collect at %d:%d body does not end in a command", s.Pos.Line, s.Pos.Col)
	}

	listTemp := l.alloc()
	l.emit(&ir.Eval{Dst: listTemp, Expr: lowerIRExpr(fe.List), Span: fe.Span})

	body := l.newBlock(fe.Span)
	exit := l.newBlock(fe.Span)
	l.emit(&ir.ForEachCollect{
		List:   listTemp,
		Names:  identTexts(fe.Names),
		Target: s.Target.Text,
		Guard:  s.Guard,
		Body:   body,
		Exit:   exit,
		// Use the inner foreach's span so a "bind-collect: list
		// expression is null" error frames at the same column
		// the tree walker's evalBindCollect uses (fe.Span,
		// matching the `foreach` keyword), not the surrounding
		// syntax.BindStmt's leading column.
		Span: fe.Span,
	})

	l.loops = append(l.loops, loopFrame{Exit: exit})
	defer func() { l.loops = l.loops[:len(l.loops)-1] }()

	l.cur = body
	// As with the non-collecting ir.ForEach, the iter frame is owned
	// by the structured terminator at runtime; lowering does not
	// emit explicit frame markers for it.
	for _, st := range fe.Body[:len(fe.Body)-1] {
		if err := l.lowerStmt(st); err != nil {
			return err
		}
	}
	argv := l.alloc()
	l.emit(&ir.BuildArgs{Dst: argv, Args: lowerIRExprs(finalCmd.Args), Span: finalCmd.Span})
	res := l.alloc()
	l.emit(&ir.DispatchBind{
		Dst:         res,
		Argv:        argv,
		CallPos:     finalCmd.Span.Pos,
		Policy:      ir.DispatchPolicyDefThenExecBind,
		TraceHeader: bindTraceHeader(s),
		Span:        finalCmd.Span,
	})
	l.emit(&ir.CollectProduce{Result: res, FrameSpan: s.Span, Span: finalCmd.Span})

	l.cur = exit
	return nil
}

func bodyHasReturn(stmts []syntax.Stmt) bool {
	for _, st := range stmts {
		switch n := st.(type) {
		case *syntax.ReturnStmt:
			return true
		case *syntax.IfStmt:
			if bodyHasReturn(n.Then) {
				return true
			}
			for _, br := range n.Elifs {
				if bodyHasReturn(br.Body) {
					return true
				}
			}
			if bodyHasReturn(n.Else) {
				return true
			}
		case *syntax.ForEachStmt:
			if bodyHasReturn(n.Body) {
				return true
			}
		case *syntax.PollStmt:
			if bodyHasReturn(n.Body) {
				return true
			}
		case *syntax.BindStmt:
			if n.Collect != nil && bodyHasReturn(n.Collect.Body) {
				return true
			}
		}
	}
	return false
}

func bindTraceHeader(s *syntax.BindStmt) string {
	verb := "let"
	if s.Guard {
		verb = "guard"
	}
	return fmt.Sprintf("%s %s", verb, named(s.Target.Text))
}

func named(s string) string {
	if s == "" {
		return "_"
	}
	return s
}

func identTexts(idents []syntax.Ident) []string {
	texts := make([]string, 0, len(idents))
	for _, ident := range idents {
		texts = append(texts, ident.Text)
	}
	return texts
}

// irDefParams converts the syntax-level def parameters into their
// IR form, carrying the optional type annotation through.
func irDefParams(params []syntax.DefParam) []ir.Param {
	out := make([]ir.Param, 0, len(params))
	for _, p := range params {
		out = append(out, ir.Param{Name: p.Name.Text, Type: p.Type})
	}
	return out
}
