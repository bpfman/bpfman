package runtime

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/check"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/lower"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

func lowerToIR(prog *syntax.Program) (*ir.Program, error) {
	return lower.Lower(prog)
}

func lowerExpr(expr syntax.Expr) ir.Expr {
	return lower.Expr(expr)
}

func lowerExprs(exprs []syntax.Expr) []ir.Expr {
	return lower.Exprs(exprs)
}

func evalLoweredExpr(expr syntax.Expr, env *Env) (Value, error) {
	return EvalExpr(lowerExpr(expr), env)
}

func evalLoweredArgs(exprs []syntax.Expr, env *Env) ([]Arg, error) {
	return EvalArgs(lowerExprs(exprs), env)
}

func execParsedProgram(t *testing.T, prog *syntax.Program, env *Env) error {
	t.Helper()
	lp, err := lowerToIR(prog)
	if err != nil {
		return err
	}

	return Exec(lp, env)
}

func execSourceProgram(t *testing.T, src string, env *Env) error {
	t.Helper()
	prog, err := parseSource(t, src)
	require.NoError(t, err)
	return execParsedProgram(t, prog, env)
}

func parseSource(t *testing.T, src string) (*syntax.Program, error) {
	t.Helper()
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	return syntax.Parse(tokens)
}

func firstStmt(t *testing.T, prog *syntax.Program) syntax.Stmt {
	t.Helper()
	require.Len(t, prog.Stmts, 1)
	return prog.Stmts[0]
}

func checkSource(t *testing.T, src string) []check.Issue {
	t.Helper()
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err)
	return check.Check(prog)
}

// astExprFromIR is the test-only inverse of the syntax->IR
// expression lowering path. Round-trip tests and compatibility
// adapters need an explicit inverse to prove the lowering remains
// structure-preserving.
func astExprFromIR(expr ir.Expr) syntax.Expr {
	switch e := expr.(type) {
	case *ir.LiteralExpr:
		return &syntax.LiteralExpr{Text: e.Text, Quoted: e.Quoted, Span: e.Span}
	case *ir.VarRefExpr:
		return &syntax.VarRefExpr{Name: e.Name, Path: e.Path, Span: e.Span}
	case *ir.AdapterExpr:
		return &syntax.AdapterExpr{Adapter: e.Adapter, Name: e.Name, Path: e.Path, Span: e.Span}
	case *ir.ListExpr:
		elems := make([]syntax.Expr, len(e.Elems))
		for i, elem := range e.Elems {
			elems[i] = astExprFromIR(elem)
		}
		return &syntax.ListExpr{Elems: elems, Span: e.Span}
	case *ir.RecordExpr:
		fields := make([]syntax.RecordField, len(e.Fields))
		for i, field := range e.Fields {
			fields[i] = syntax.RecordField{
				Name: field.Name,
				Expr: astExprFromIR(field.Expr),
				Span: field.Span,
			}
		}
		return &syntax.RecordExpr{Fields: fields, Span: e.Span}
	case *ir.InterpStringExpr:
		segs := make([]syntax.InterpStringSegment, len(e.Segments))
		for i, seg := range e.Segments {
			segs[i].Literal = seg.Literal
			if seg.Expr != nil {
				segs[i].Expr = astExprFromIR(seg.Expr)
			}
		}
		return &syntax.InterpStringExpr{Segments: segs, Span: e.Span}
	case *ir.ThreadExpr:
		return &syntax.ThreadExpr{
			LHS:     astExprFromIR(e.LHS),
			Args:    astExprsFromIR(e.Args),
			PipePos: e.PipePos,
			Span:    e.Span,
		}
	case *ir.BinaryExpr:
		return &syntax.BinaryExpr{Left: astExprFromIR(e.Left), Op: e.Op, Right: astExprFromIR(e.Right), Span: e.Span}
	case *ir.UnaryExpr:
		return &syntax.UnaryExpr{Pred: e.Pred, Operand: astExprFromIR(e.Operand), Span: e.Span}
	case *ir.LogicalExpr:
		return &syntax.LogicalExpr{Op: e.Op, Left: astExprFromIR(e.Left), Right: astExprFromIR(e.Right), Span: e.Span}
	case *ir.NotExpr:
		return &syntax.NotExpr{Operand: astExprFromIR(e.Operand), Span: e.Span}
	case *ir.NegateExpr:
		return &syntax.NegateExpr{Operand: astExprFromIR(e.Operand), Span: e.Span}
	case *ir.PureCallExpr:
		return &syntax.PureCallExpr{Name: e.Name, Args: astExprsFromIR(e.Args), Span: e.Span}
	case *ir.MatchesExpr:
		return &syntax.MatchesExpr{
			Target: astExprFromIR(e.Target),
			Block:  astMatchesBlockFromIR(e.Block),
			Span:   e.Span,
		}
	default:
		panic(fmt.Sprintf("astExprFromIR: unhandled lowered expression type %T", expr))
	}
}

func astExprsFromIR(exprs []ir.Expr) []syntax.Expr {
	out := make([]syntax.Expr, len(exprs))
	for i, expr := range exprs {
		out[i] = astExprFromIR(expr)
	}
	return out
}

func astMatchesBlockFromIR(e *ir.MatchesBlockExpr) *syntax.MatchesBlockExpr {
	out := &syntax.MatchesBlockExpr{
		Entries:    make([]syntax.MatchEntry, len(e.Entries)),
		Exhaustive: e.Exhaustive,
		Span:       e.Span,
	}
	for i, entry := range e.Entries {
		out.Entries[i] = syntax.MatchEntry{
			Path:      entry.Path,
			Predicate: entry.Predicate,
			Span:      entry.Span,
		}
		if entry.Pattern != nil {
			out.Entries[i].Pattern = astExprFromIR(entry.Pattern)
		}
		if entry.SubBlock != nil {
			out.Entries[i].SubBlock = astMatchesBlockFromIR(entry.SubBlock)
		}
	}
	return out
}
