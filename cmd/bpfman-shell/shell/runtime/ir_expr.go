package runtime

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

func evalExpr(expr ir.Expr, env *Env) (Value, error) {
	switch e := expr.(type) {
	case *ir.LiteralExpr:
		v, err := literalValueParts(e.Text, e.Quoted)
		if err != nil {
			return Value{}, syntax.SpanErrorf(e.Span, "%v", err)
		}
		return v, nil
	case *ir.VarRefExpr:
		return resolveVarRefValueParts(e.Name, e.Path, e.Span, env)
	case *ir.AdapterExpr:
		return Value{}, syntax.SpanErrorf(e.Span, "adapter %s:$%s cannot be used as an expression operand", e.Adapter, e.Name)
	case *ir.ListExpr:
		out := make([]any, 0, len(e.Elems))
		origins := make([]any, 0, len(e.Elems))
		hasOrigin := false
		for _, elem := range e.Elems {
			v, err := evalExpr(elem, env)
			if err != nil {
				return Value{}, err
			}

			out = append(out, v.Raw())
			o := v.Origin()
			origins = append(origins, o)
			if o != nil {
				hasOrigin = true
			}
		}
		list := ValueFromAny(out)
		if hasOrigin {
			list = list.withOrigin(origins, semantics.OriginUnknown)
		}
		return list, nil
	case *ir.RecordExpr:
		fields := make(map[string]Value, len(e.Fields))
		for _, field := range e.Fields {
			v, err := evalExpr(field.Expr, env)
			if err != nil {
				return Value{}, err
			}

			fields[field.Name] = v
		}
		return ValueFromRecord(fields), nil
	case *ir.InterpStringExpr:
		var b strings.Builder
		for _, seg := range e.Segments {
			if seg.Expr == nil {
				b.WriteString(seg.Literal)
				continue
			}
			v, err := evalExpr(seg.Expr, env)
			if err != nil {
				return Value{}, err
			}

			s, err := RenderCompact(v)
			if err != nil {
				return Value{}, syntax.SpanErrorf(irExprSpan(seg.Expr), "interpolation: %v", err)
			}

			b.WriteString(s)
		}
		return StringValue(b.String()), nil
	case *ir.ThreadExpr:
		threadSpan := irThreadDiagSpan(e)
		if env.ExecBind == nil {
			return Value{}, syntax.SpanErrorf(threadSpan, "'|>' is only valid where commands can run; not available in this context")
		}
		args, err := evalArgs(e.Args, env)
		if err != nil {
			return Value{}, err
		}

		lhsArg, err := evalArg(e.LHS, env)
		if err != nil {
			return Value{}, syntax.SpanErrorf(threadSpan, "thread: %v", err)
		}

		// '|>' is expression-position bind-dispatch with the rc
		// envelope discarded; head resolution follows the same
		// def-first policy as the bind RHS, defer, and bind-
		// collect lanes. Route through dispatchBindByPolicy
		// so the rule lives in one place.
		callLoc := e.PipePos
		if callLoc.Line == 0 {
			callLoc = e.Span.Pos
		}
		result, err := dispatchBindByPolicy(ir.DispatchPolicyDefThenExecBind, append(args, lhsArg), callLoc, e.Span, env)
		if err != nil {
			return Value{}, syntax.FrameAt(threadSpan, err)
		}
		if !result.Rc.OK() {
			if result.Rc.Stderr != "" {
				return Value{}, syntax.SpanErrorf(threadSpan, "thread: command failed (exit %d): %s", result.Rc.ExitCode, result.Rc.Stderr)
			}
			return Value{}, syntax.SpanErrorf(threadSpan, "thread: command failed (exit %d)", result.Rc.ExitCode)
		}
		return result.Primary, nil
	case *ir.BinaryExpr:
		leftV, err := evalExpr(e.Left, env)
		if err != nil {
			return Value{}, err
		}

		rightV, err := evalExpr(e.Right, env)
		if err != nil {
			return Value{}, err
		}

		if isArithmeticOpText(e.Op) {
			left, err := leftV.Scalar()
			if err != nil {
				return Value{}, syntax.SpanErrorf(e.Span, "binary %s: left: %v", e.Op, err)
			}

			right, err := rightV.Scalar()
			if err != nil {
				return Value{}, syntax.SpanErrorf(e.Span, "binary %s: right: %v", e.Op, err)
			}

			v, err := evalArithmetic(e.Op, left, right)
			if err != nil {
				return Value{}, syntax.SpanErrorf(e.Span, "%v", err)
			}
			return v, nil
		}
		return evalCompare(e.Op, leftV, rightV, e.Span)
	case *ir.UnaryExpr:
		operand, err := evalExpr(e.Operand, env)
		if err != nil {
			return Value{}, err
		}

		switch e.Pred {
		case "not-empty":
			return evalNotEmpty(operand, e.Span)
		default:
			return Value{}, syntax.SpanErrorf(e.Span, "unknown unary predicate %q", e.Pred)
		}
	case *ir.LogicalExpr:
		leftV, err := evalExpr(e.Left, env)
		if err != nil {
			return Value{}, err
		}

		leftB, err := AsBool(leftV)
		if err != nil {
			return Value{}, syntax.SpanErrorf(e.Span, "%s: left: %v", e.Op, err)
		}

		switch e.Op {
		case "and":
			if !leftB {
				return BoolValue(false), nil
			}
		case "or":
			if leftB {
				return BoolValue(true), nil
			}
		default:
			return Value{}, syntax.SpanErrorf(e.Span, "unknown logical operator %q", e.Op)
		}
		rightV, err := evalExpr(e.Right, env)
		if err != nil {
			return Value{}, err
		}

		rightB, err := AsBool(rightV)
		if err != nil {
			return Value{}, syntax.SpanErrorf(e.Span, "%s: right: %v", e.Op, err)
		}
		return BoolValue(rightB), nil
	case *ir.NotExpr:
		v, err := evalExpr(e.Operand, env)
		if err != nil {
			return Value{}, err
		}

		b, err := AsBool(v)
		if err != nil {
			return Value{}, syntax.SpanErrorf(e.Span, "not: %v", err)
		}
		return BoolValue(!b), nil
	case *ir.NegateExpr:
		v, err := evalExpr(e.Operand, env)
		if err != nil {
			return Value{}, err
		}

		s, err := v.Scalar()
		if err != nil {
			return Value{}, syntax.SpanErrorf(e.Span, "negate: %v", err)
		}

		x, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return Value{}, syntax.SpanErrorf(e.Span, "negate: operand %q is not numeric", s)
		}
		return numericValue(-x), nil
	case *ir.PureCallExpr:
		if env.ExecBind == nil {
			return Value{}, syntax.SpanErrorf(e.Span, "%s: pure-builtin calls require an active command dispatcher", e.Name)
		}
		args := make([]Arg, 0, len(e.Args)+1)
		args = append(args, WordArg{Text: e.Name, Span: e.Span})
		for _, a := range e.Args {
			arg, err := evalArg(a, env)
			if err != nil {
				return Value{}, syntax.SpanErrorf(irExprSpan(a), "%s: %v", e.Name, err)
			}

			args = append(args, arg)
		}
		result, err := env.ExecBind(args, e.Span)
		if err != nil {
			return Value{}, syntax.FrameAt(e.Span, err)
		}
		if !result.Rc.OK() {
			if result.Rc.Stderr != "" {
				return Value{}, syntax.SpanErrorf(e.Span, "%s: %s", e.Name, result.Rc.Stderr)
			}
			return Value{}, syntax.SpanErrorf(e.Span, "%s: call failed (exit %d)", e.Name, result.Rc.ExitCode)
		}
		return result.Primary, nil
	case *ir.MatchesExpr:
		result, err := evalMatchesExprDetails(e, env)
		if err != nil {
			return Value{}, err
		}
		return BoolValue(result.Matched), nil
	default:
		panic(fmt.Sprintf("evalExpr: unhandled lowered expression type %T", expr))
	}
}

// EvalExpr evaluates one lowered expression against env.
func EvalExpr(expr ir.Expr, env *Env) (Value, error) {
	return evalExpr(expr, env)
}

// EvalArgs evaluates each lowered expression in exprs as a
// command argument and returns the resulting []Arg, suitable for
// dispatch.
func EvalArgs(exprs []ir.Expr, env *Env) ([]Arg, error) {
	return evalArgs(exprs, env)
}

func evalArgs(exprs []ir.Expr, env *Env) ([]Arg, error) {
	out := make([]Arg, 0, len(exprs))
	for _, expr := range exprs {
		a, err := evalArg(expr, env)
		if err != nil {
			return nil, err
		}

		out = append(out, a)
	}
	return out, nil
}

func evalArg(expr ir.Expr, env *Env) (Arg, error) {
	switch e := expr.(type) {
	case *ir.LiteralExpr:
		if e.Quoted {
			return QuotedArg{Text: e.Text, Span: e.Span}, nil
		}
		return WordArg{Text: e.Text, Span: e.Span}, nil
	case *ir.VarRefExpr:
		return resolveVarRefArgParts(e.Name, e.Path, e.Span, env)
	case *ir.AdapterExpr:
		return resolveAdapterArgParts(e.Adapter, e.Name, e.Path, e.Span, env)
	case *ir.ThreadExpr:
		val, err := evalExpr(e, env)
		if err != nil {
			return nil, err
		}
		if val.IsNil() {
			return nil, syntax.SpanErrorf(irThreadDiagSpan(e), "thread produced no value")
		}
		return valueToArg(val, e.Span)
	default:
		v, err := evalExpr(expr, env)
		if err != nil {
			return nil, err
		}
		if v.IsNil() {
			return nil, syntax.SpanErrorf(irExprSpan(expr), "parenthesised expression produced no value")
		}
		return valueToArg(v, irExprSpan(expr))
	}
}

func evalMatchesBlock(e *ir.MatchesBlockExpr, env *Env) (resolvedMatchesBlock, error) {
	out := resolvedMatchesBlock{
		Entries:    make([]resolvedMatchEntry, 0, len(e.Entries)),
		Exhaustive: e.Exhaustive,
		Span:       e.Span,
	}
	for _, entry := range e.Entries {
		ent := resolvedMatchEntry{
			Path:      entry.Path,
			Predicate: entry.Predicate,
			Span:      entry.Span,
		}
		switch {
		case entry.SubBlock != nil:
			sub, err := evalMatchesBlock(entry.SubBlock, env)
			if err != nil {
				return resolvedMatchesBlock{}, err
			}

			ent.SubBlock = &sub
		case entry.Predicate != "":
			// nothing to evaluate
		default:
			v, err := evalExpr(entry.Pattern, env)
			if err != nil {
				return resolvedMatchesBlock{}, syntax.SpanErrorf(entry.Span, "matches entry %q: %v", entry.Path, err)
			}

			ent.Value = v
		}
		out.Entries = append(out.Entries, ent)
	}
	return out, nil
}

func irExprSpan(expr ir.Expr) source.Span {
	switch e := expr.(type) {
	case *ir.LiteralExpr:
		return e.Span
	case *ir.VarRefExpr:
		return e.Span
	case *ir.AdapterExpr:
		return e.Span
	case *ir.ListExpr:
		return e.Span
	case *ir.RecordExpr:
		return e.Span
	case *ir.InterpStringExpr:
		return e.Span
	case *ir.ThreadExpr:
		return e.Span
	case *ir.BinaryExpr:
		return e.Span
	case *ir.UnaryExpr:
		return e.Span
	case *ir.LogicalExpr:
		return e.Span
	case *ir.NotExpr:
		return e.Span
	case *ir.NegateExpr:
		return e.Span
	case *ir.PureCallExpr:
		return e.Span
	case *ir.MatchesExpr:
		return e.Span
	default:
		panic(fmt.Sprintf("irExprSpan: unhandled lowered expression type %T", expr))
	}
}

func irThreadDiagSpan(e *ir.ThreadExpr) source.Span {
	if e.PipePos != (source.Pos{}) {
		return source.Span{Pos: e.PipePos, End: e.PipePos}
	}
	return e.Span
}
