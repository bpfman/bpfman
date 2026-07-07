package lower

import (
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

func lowerIRExpr(expr syntax.Expr) ir.Expr {
	switch e := expr.(type) {
	case *syntax.LiteralExpr:
		return &ir.LiteralExpr{Text: e.Text, Quoted: e.Quoted, Span: e.Span}
	case *syntax.VarRefExpr:
		return &ir.VarRefExpr{Name: e.Name, Path: e.Path, Span: e.Span}
	case *syntax.AdapterExpr:
		return &ir.AdapterExpr{Adapter: e.Adapter, Name: e.Name, Path: e.Path, Span: e.Span}
	case *syntax.ListExpr:
		elems := make([]ir.Expr, len(e.Elems))
		for i, elem := range e.Elems {
			elems[i] = lowerIRExpr(elem)
		}
		return &ir.ListExpr{Elems: elems, Span: e.Span}
	case *syntax.RecordExpr:
		fields := make([]ir.RecordField, len(e.Fields))
		for i, field := range e.Fields {
			fields[i] = ir.RecordField{
				Name: field.Name,
				Expr: lowerIRExpr(field.Expr),
				Span: field.Span,
			}
		}
		return &ir.RecordExpr{Fields: fields, Span: e.Span}
	case *syntax.InterpStringExpr:
		segs := make([]ir.InterpStringSegment, len(e.Segments))
		for i, seg := range e.Segments {
			segs[i].Literal = seg.Literal
			if seg.Expr != nil {
				segs[i].Expr = lowerIRExpr(seg.Expr)
			}
		}
		return &ir.InterpStringExpr{Segments: segs, Span: e.Span}
	case *syntax.ThreadExpr:
		return &ir.ThreadExpr{
			LHS:     lowerIRExpr(e.LHS),
			Args:    lowerIRExprs(e.Args),
			PipePos: e.PipePos,
			Span:    e.Span,
		}
	case *syntax.BinaryExpr:
		return &ir.BinaryExpr{
			Left:  lowerIRExpr(e.Left),
			Op:    e.Op,
			Right: lowerIRExpr(e.Right),
			Span:  e.Span,
		}
	case *syntax.UnaryExpr:
		return &ir.UnaryExpr{Pred: e.Pred, Operand: lowerIRExpr(e.Operand), Span: e.Span}
	case *syntax.LogicalExpr:
		return &ir.LogicalExpr{
			Op:    e.Op,
			Left:  lowerIRExpr(e.Left),
			Right: lowerIRExpr(e.Right),
			Span:  e.Span,
		}
	case *syntax.NotExpr:
		return &ir.NotExpr{Operand: lowerIRExpr(e.Operand), Span: e.Span}
	case *syntax.NegateExpr:
		return &ir.NegateExpr{Operand: lowerIRExpr(e.Operand), Span: e.Span}
	case *syntax.PureCallExpr:
		return &ir.PureCallExpr{Name: e.Name, Args: lowerIRExprs(e.Args), Span: e.Span}
	case *syntax.MatchesExpr:
		return &ir.MatchesExpr{
			Target: lowerIRExpr(e.Target),
			Block:  lowerIRMatchesBlock(e.Block),
			Span:   e.Span,
		}
	default:
		panic(fmt.Sprintf("lowerIRExpr: unhandled expression type %T", expr))
	}
}

// Expr lowers one parsed expression into the expression node used by
// the IR runtime. It is the public expression-only counterpart to
// Lower for callers that need to evaluate or inspect a standalone
// expression without manufacturing a whole statement.
func Expr(expr syntax.Expr) ir.Expr {
	return lowerIRExpr(expr)
}

// Exprs lowers an ordered expression list into IR expression nodes.
func Exprs(exprs []syntax.Expr) []ir.Expr {
	return lowerIRExprs(exprs)
}

func lowerIRMatchesBlock(e *syntax.MatchesBlockExpr) *ir.MatchesBlockExpr {
	out := &ir.MatchesBlockExpr{
		Entries:    make([]ir.MatchEntry, len(e.Entries)),
		Exhaustive: e.Exhaustive,
		Span:       e.Span,
	}
	for i, entry := range e.Entries {
		out.Entries[i] = ir.MatchEntry{
			Path:      entry.Path,
			Predicate: entry.Predicate,
			Span:      entry.Span,
		}
		if entry.Pattern != nil {
			out.Entries[i].Pattern = lowerIRExpr(entry.Pattern)
		}
		if entry.SubBlock != nil {
			out.Entries[i].SubBlock = lowerIRMatchesBlock(entry.SubBlock)
		}
	}
	return out
}

func lowerIRExprs(exprs []syntax.Expr) []ir.Expr {
	out := make([]ir.Expr, len(exprs))
	for i, expr := range exprs {
		out[i] = lowerIRExpr(expr)
	}
	return out
}
