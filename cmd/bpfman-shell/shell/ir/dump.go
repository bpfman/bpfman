// Deterministic text dump of a Program. The dump is the
// canonical inspection format for the IR: snapshot tests compare
// against it, the --lowered CLI mode emits it, and reviewers read
// it to understand the semantic shape of a script.
//
// Determinism comes from three rules. Blocks are numbered bb0,
// bb1, ... in the order the lowerer placed them in the unit's
// Blocks slice. Temps render as t0, t1, ... per their Temp index
// (the lowerer is responsible for allocating Temp values in
// emission order). Lowered expressions render as compact
// source-shaped text via writeExpr: $var.path, $a + $b, jq
// "...", matches exhaustive { ... }, and so on. The shape is
// chosen so a let, Eval, or BuildArgs line reads as the
// surface form a reviewer would expect; parens are added on
// subexpressions to preserve operator grouping but quoting
// rules are best-effort and the result is not guaranteed to
// round-trip through the parser.
//
// Spans live on every instruction but are not rendered.
// Including them would tie every snapshot to every source line
// and column, so a one-line edit to a script would diff its
// entire lowered form. The IR keeps the data either way.

package ir

import (
	"fmt"
	"io"
	"strings"
)

// Dump writes a deterministic text rendering of lp to w.
// Each Def prints first, in source order, followed by the
// top-level program body. Blocks within one unit share a single
// label namespace (bb0, bb1, ...); temps similarly share their
// containing unit's namespace.
func Dump(w io.Writer, lp *Program) error {
	var b strings.Builder
	defs := loweredDefNameSet(lp.Defs)
	for i, d := range lp.Defs {
		if i > 0 {
			b.WriteByte('\n')
		}
		dumpDef(&b, d, defs)
	}
	if lp.Body != nil {
		if len(lp.Defs) > 0 {
			b.WriteByte('\n')
		}
		dumpBody(&b, lp, defs)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// dumpDef renders one Def. The header line names the def,
// its parameters, and its entry block; each block then follows
// in emission order separated by a blank line.
func dumpDef(b *strings.Builder, d *Def, defs map[string]bool) {
	labels := assignLabels(d.Blocks)
	ctx := newDumpContext(d.Blocks, defs)
	fmt.Fprintf(b, "def %s(%s)", d.Name, ParamList(d.Params))
	fmt.Fprintf(b, " entry=%s\n", labels[d.Entry])
	for _, blk := range d.Blocks {
		b.WriteByte('\n')
		dumpBlock(b, blk, labels[blk], labels, ctx)
	}
}

// dumpBody renders the top-level program body. The header line
// names the entry block; each block then follows in emission
// order separated by a blank line.
func dumpBody(b *strings.Builder, lp *Program, defs map[string]bool) {
	labels := assignLabels(lp.Blocks)
	ctx := newDumpContext(lp.Blocks, defs)
	fmt.Fprintf(b, "body entry=%s\n", labels[lp.Body])
	for _, blk := range lp.Blocks {
		b.WriteByte('\n')
		dumpBlock(b, blk, labels[blk], labels, ctx)
	}
}

// assignLabels builds the *BasicBlock -> "bb<N>" table for one
// unit. The Blocks slice IS the emission order; the dumper does
// not reorder.
func assignLabels(blocks []*BasicBlock) map[*BasicBlock]string {
	m := make(map[*BasicBlock]string, len(blocks))
	for i, blk := range blocks {
		m[blk] = fmt.Sprintf("bb%d", i)
	}
	return m
}

func loweredDefNameSet(defs []*Def) map[string]bool {
	m := make(map[string]bool, len(defs))
	for _, d := range defs {
		m[d.Name] = true
	}
	return m
}

type dumpContext struct {
	defs map[string]bool
	argv map[Temp][]Expr
}

func newDumpContext(blocks []*BasicBlock, defs map[string]bool) dumpContext {
	argv := map[Temp][]Expr{}
	for _, blk := range blocks {
		for _, ins := range blk.Instrs {
			if v, ok := ins.(*BuildArgs); ok {
				argv[v.Dst] = v.Args
			}
		}
	}
	return dumpContext{defs: defs, argv: argv}
}

// dumpBlock prints a single block: its label flush left, then
// each instruction indented two spaces on its own line.
func dumpBlock(b *strings.Builder, blk *BasicBlock, label string, labels map[*BasicBlock]string, ctx dumpContext) {
	fmt.Fprintf(b, "%s:\n", label)
	for _, ins := range blk.Instrs {
		if _, ok := ins.(*TraceNote); ok {
			continue
		}
		b.WriteString("  ")
		dumpInstr(b, ins, labels, ctx)
		b.WriteByte('\n')
	}
}

// dumpInstr writes one instruction in the format the snapshot
// contract relies on. The verb is the Go type name (CamelCase);
// operands use named-field form to keep the dump self-describing.
func dumpInstr(b *strings.Builder, i Instr, labels map[*BasicBlock]string, ctx dumpContext) {
	switch v := i.(type) {
	case *EnterFrame:
		fmt.Fprintf(b, "EnterFrame kind=%s", frameKindName(v.Kind))
	case *ExitFrame:
		b.WriteString("ExitFrame")
	case *EnterDeferScope:
		fmt.Fprintf(b, "EnterDeferScope kind=%s", deferScopeKindName(v.Kind))
	case *RegisterDefer:
		fmt.Fprintf(b, "RegisterDefer argv=t%d policy=%s lane=%s", v.Argv, dispatchPolicyName(v.Policy), resolveDispatchLane(v.Argv, ctx))
	case *RunDefers:
		fmt.Fprintf(b, "RunDefers policy=%s", runDefersPolicyName(v.Policy))
	case *Eval:
		fmt.Fprintf(b, "Eval t%d = %s", v.Dst, dumpExpr(v.Expr))
	case *BuildArgs:
		args := make([]string, len(v.Args))
		for j, a := range v.Args {
			args[j] = dumpExpr(a)
		}
		fmt.Fprintf(b, "BuildArgs t%d = [%s]", v.Dst, strings.Join(args, " "))
	case *DispatchBind:
		fmt.Fprintf(b, "DispatchBind t%d = t%d policy=%s lane=%s", v.Dst, v.Argv, dispatchPolicyName(v.Policy), resolveDispatchLane(v.Argv, ctx))
	case *DispatchCommand:
		fmt.Fprintf(b, "DispatchCommand argv=t%d policy=%s lane=%s", v.Argv, dispatchPolicyName(v.Policy), resolveDispatchLane(v.Argv, ctx))
	case *ApplyBind:
		target := v.Target
		if target == "" {
			target = "_"
		}
		fmt.Fprintf(b, "ApplyBind src=t%d argv=t%d target=%s guard=%t", v.Src, v.Argv, target, v.Guard)
		if v.OnFail != nil {
			fmt.Fprintf(b, " fail=%s", labels[v.OnFail])
		}
	case *BindName:
		fmt.Fprintf(b, "BindName %s = t%d", v.Name, v.Src)
	case *BindDestructure:
		fmt.Fprintf(b, "BindDestructure [%s] = t%d", strings.Join(v.Names, " "), v.Src)
	case *BuildEnvelope:
		fmt.Fprintf(b, "BuildEnvelope t%d = { ok=%t exit_code=%d err=%q }", v.Dst, v.ExitCode == 0, v.ExitCode, v.Err)
	case *EmitBindResult:
		rc := "synthetic"
		if v.Rc != nil {
			rc = fmt.Sprintf("t%d", *v.Rc)
		}
		primary := "nil"
		if v.Primary != nil {
			primary = fmt.Sprintf("t%d", *v.Primary)
		}
		fmt.Fprintf(b, "EmitBindResult rc=%s primary=%s", rc, primary)
	case *EmitResult:
		fmt.Fprintf(b, "EmitResult t%d", v.Src)
	case *Stop:
		b.WriteString("Stop")
	case *Jump:
		fmt.Fprintf(b, "Jump %s", labels[v.Target])
	case *Branch:
		fmt.Fprintf(b, "Branch cond=t%d true=%s false=%s", v.Cond, labels[v.True], labels[v.False])
	case *ReturnValue:
		fmt.Fprintf(b, "ReturnValue t%d to=%s", v.Src, labels[v.To])
	case *PropagateError:
		b.WriteString("PropagateError")
	case *PropagateGuardFailure:
		fmt.Fprintf(b, "PropagateGuardFailure primary=%s head=%s exit_code=%d stderr=%q", v.Primary, v.Head, v.ExitCode, v.Stderr)
	case *Fail:
		fmt.Fprintf(b, "Fail msg=%q", v.Msg)
	case *BeginPoll:
		fmt.Fprintf(b, "BeginPoll timeout=%s every=%s attempt=%s timeout=%s success=%s", v.Timeout, v.Every, labels[v.Attempt], labels[v.OnTimeout], labels[v.OnSuccess])
	case *RetryPoll:
		if v.Message != nil {
			fmt.Fprintf(b, "RetryPoll message=t%d", *v.Message)
		} else {
			b.WriteString("RetryPoll")
		}
	case *Assert:
		verb := "Assert"
		if v.IsRequire {
			verb = "Require"
		}
		fmt.Fprintf(b, "%s %s", verb, dumpAssertClause(v.Clause))
	case *ForEach:
		fmt.Fprintf(b, "ForEach list=t%d names=[%s] body=%s exit=%s", v.List, strings.Join(v.Names, " "), labels[v.Body], labels[v.Exit])
	case *ForEachContinue:
		b.WriteString("ForEachContinue")
	case *ForEachCollect:
		target := v.Target
		if target == "" {
			target = "_"
		}
		fmt.Fprintf(b, "ForEachCollect list=t%d names=[%s] target=%s guard=%t body=%s exit=%s", v.List, strings.Join(v.Names, " "), target, v.Guard, labels[v.Body], labels[v.Exit])
	case *CollectProduce:
		fmt.Fprintf(b, "CollectProduce t%d", v.Result)
	case *ExitLoop:
		b.WriteString("ExitLoop")
	case *RegisterDef:
		fmt.Fprintf(b, "RegisterDef name=%s params=[%s]", v.Def.Name, ParamList(v.Def.Params))
	default:
		fmt.Fprintf(b, "<unknown %T>", i)
	}
}

func resolveDispatchLane(argv Temp, ctx dumpContext) string {
	args, ok := ctx.argv[argv]
	if !ok || len(args) == 0 {
		return "unresolved-head"
	}
	head, ok := literalHeadText(args[0])
	if !ok {
		return "unresolved-head"
	}
	switch {
	case ctx.defs[head]:
		return fmt.Sprintf("def(%s)", head)
	case isCommandBuiltinName(head):
		return fmt.Sprintf("builtin(%s)", head)
	default:
		return "exec"
	}
}

// frameKindName, deferScopeKindName, and runDefersPolicyName
// render the small enum kinds in the same vocabulary the design
// documents use. Unknown values fall back to a numeric form so
// the dump always renders something rather than panicking; the
// test suite will catch a missing case.

func frameKindName(k FrameKind) string {
	switch k {
	case FrameDef:
		return "def"
	case FrameIfBranch:
		return "if-branch"
	case FrameForEachIter:
		return "foreach-iter"
	case FramePollAttempt:
		return "poll-attempt"
	default:
		return fmt.Sprintf("<unknown:%d>", int(k))
	}
}

func deferScopeKindName(k DeferScopeKind) string {
	switch k {
	case DeferScopeProgram:
		return "program"
	case DeferScopeDef:
		return "def"
	case DeferScopePollAttempt:
		return "poll-attempt"
	default:
		return fmt.Sprintf("<unknown:%d>", int(k))
	}
}

func runDefersPolicyName(p RunDefersPolicy) string {
	switch p {
	case RunDefersProgram:
		return "program"
	case RunDefersDefLocal:
		return "def-local"
	case RunDefersAttemptFatal:
		return "attempt-fatal"
	default:
		return fmt.Sprintf("<unknown:%d>", int(p))
	}
}

func dispatchPolicyName(p DispatchPolicy) string {
	switch p {
	case DispatchPolicyDefThenExecBind:
		return "def-then-exec-bind"
	case DispatchPolicyDefThenExecCommand:
		return "def-then-exec-command"
	default:
		return fmt.Sprintf("<unknown:%d>", int(p))
	}
}

func literalHeadText(expr Expr) (string, bool) {
	lit, ok := expr.(*LiteralExpr)
	if !ok {
		return "", false
	}
	return lit.Text, true
}

// FormatExprSource renders a lowered expression as compact
// source-shaped text.
func FormatExprSource(e Expr) string {
	return dumpExpr(e)
}

// FormatAssertClauseSource renders a lowered assertion clause in the
// source-like form used by dumps, traces, and diagnostics.
func FormatAssertClauseSource(c AssertClause) string {
	return dumpAssertClause(c)
}

// dumpExpr renders a lowered expression as compact
// source-shaped text.
func dumpExpr(e Expr) string {
	if e == nil {
		return "nil"
	}
	var b strings.Builder
	writeExpr(&b, e)
	return b.String()
}

func dumpAssertClause(c AssertClause) string {
	if c == nil {
		return "<nil-assert-clause>"
	}
	var b strings.Builder
	writeAssertClause(&b, c)
	return b.String()
}

// writeExpr is the IR-native counterpart to writeExpr. It keeps
// the textual dump stable without reconstructing AST expression
// nodes just for formatting.
func writeExpr(b *strings.Builder, e Expr) {
	switch v := e.(type) {
	case *LiteralExpr:
		if v.Quoted {
			fmt.Fprintf(b, "%q", v.Text)
			return
		}
		b.WriteString(v.Text)
	case *VarRefExpr:
		b.WriteByte('$')
		b.WriteString(v.Name)
		if v.Path != "" {
			b.WriteByte('.')
			b.WriteString(v.Path)
		}
	case *AdapterExpr:
		b.WriteString(v.Adapter)
		b.WriteByte(':')
		b.WriteByte('$')
		b.WriteString(v.Name)
		if v.Path != "" {
			b.WriteByte('.')
			b.WriteString(v.Path)
		}
	case *InterpStringExpr:
		b.WriteByte('"')
		for _, seg := range v.Segments {
			if seg.Expr == nil {
				b.WriteString(seg.Literal)
				continue
			}
			b.WriteString("${")
			switch sub := seg.Expr.(type) {
			case *VarRefExpr:
				b.WriteString(sub.Name)
				if sub.Path != "" {
					b.WriteByte('.')
					b.WriteString(sub.Path)
				}
			case *AdapterExpr:
				b.WriteString(sub.Adapter)
				b.WriteByte(':')
				b.WriteString(sub.Name)
				if sub.Path != "" {
					b.WriteByte('.')
					b.WriteString(sub.Path)
				}
			default:
				writeExpr(b, seg.Expr)
			}
			b.WriteByte('}')
		}
		b.WriteByte('"')
	case *BinaryExpr:
		writeExprAtom(b, v.Left)
		b.WriteByte(' ')
		b.WriteString(v.Op)
		b.WriteByte(' ')
		writeExprAtom(b, v.Right)
	case *UnaryExpr:
		b.WriteString(v.Pred)
		b.WriteByte(' ')
		writeExprAtom(b, v.Operand)
	case *ThreadExpr:
		writeExprAtom(b, v.LHS)
		b.WriteString(" |>")
		for _, a := range v.Args {
			b.WriteByte(' ')
			writeExpr(b, a)
		}
	case *LogicalExpr:
		writeExprAtom(b, v.Left)
		b.WriteByte(' ')
		b.WriteString(v.Op)
		b.WriteByte(' ')
		writeExprAtom(b, v.Right)
	case *NotExpr:
		b.WriteString("not ")
		writeExprAtom(b, v.Operand)
	case *NegateExpr:
		b.WriteByte('-')
		writeExprAtom(b, v.Operand)
	case *PureCallExpr:
		b.WriteString(v.Name)
		for _, a := range v.Args {
			b.WriteByte(' ')
			writeExpr(b, a)
		}
	case *MatchesExpr:
		writeExprAtom(b, v.Target)
		b.WriteByte(' ')
		writeIRMatchesBlock(b, v.Block)
	case *ListExpr:
		b.WriteByte('[')
		for i, elem := range v.Elems {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeExpr(b, elem)
		}
		b.WriteByte(']')
	case *RecordExpr:
		b.WriteString("record {")
		for i, field := range v.Fields {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(field.Name)
			b.WriteString(": ")
			writeExpr(b, field.Expr)
		}
		b.WriteByte('}')
	default:
		t := fmt.Sprintf("%T", e)
		if i := strings.LastIndex(t, "."); i >= 0 {
			t = t[i+1:]
		}
		fmt.Fprintf(b, "<%s>", t)
	}
}

func writeAssertClause(b *strings.Builder, c AssertClause) {
	switch v := c.(type) {
	case *AssertExprClause:
		writeExpr(b, v.Expr)
	case *AssertCommandClause:
		if v.Negate {
			b.WriteString("not ")
		}
		b.WriteString(v.Head)
		for _, a := range v.Args {
			b.WriteByte(' ')
			writeExpr(b, a)
		}
	default:
		t := fmt.Sprintf("%T", c)
		if i := strings.LastIndex(t, "."); i >= 0 {
			t = t[i+1:]
		}
		fmt.Fprintf(b, "<%s>", t)
	}
}

func writeExprAtom(b *strings.Builder, e Expr) {
	switch e.(type) {
	case *BinaryExpr, *LogicalExpr, *ThreadExpr, *UnaryExpr, *NotExpr, *NegateExpr, *MatchesExpr:
		b.WriteByte('(')
		writeExpr(b, e)
		b.WriteByte(')')
	default:
		writeExpr(b, e)
	}
}

func writeIRMatchesBlock(b *strings.Builder, m *MatchesBlockExpr) {
	b.WriteString("matches")
	if m.Exhaustive {
		b.WriteString(" exhaustive")
	}
	b.WriteString(" {")
	for i, ent := range m.Entries {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte(' ')
		b.WriteString(ent.Path)
		b.WriteString(": ")
		switch {
		case ent.Predicate != "":
			b.WriteString(ent.Predicate)
		case ent.SubBlock != nil:
			writeIRMatchesBlock(b, ent.SubBlock)
		default:
			writeExpr(b, ent.Pattern)
		}
	}
	if len(m.Entries) > 0 {
		b.WriteByte(' ')
	}
	b.WriteByte('}')
}
