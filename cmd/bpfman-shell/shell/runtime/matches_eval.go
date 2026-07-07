package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// MatchesResult is the shell-owned outcome of evaluating a
// TARGET matches { ... } expression. The root app decides how to
// label/report the failure, but the matching semantics and the
// mismatch set live in shell.
type MatchesResult struct {
	// Matched reports whether the whole matches block matched the
	// target.
	Matched bool

	// Mismatches lists the failing entries and exhaustive-coverage
	// problems; empty when Matched is true.
	Mismatches []MatchMismatch
}

// MatchMismatch is one failing entry (or exhaustive-coverage
// problem) from a matches block evaluation.
type MatchMismatch struct {
	// Pos is the source position of the failing entry.
	Pos source.Pos

	// Message is the human-readable description of why the entry
	// failed.
	Message string
}

// resolvedMatchesBlock is the eagerly-evaluated runtime payload for
// one `matches { ... }` expression. Predicate entries keep only
// their predicate name; value-pattern entries hold the evaluated
// value; nested matches recurse into another resolved block.
type resolvedMatchesBlock struct {
	Entries    []resolvedMatchEntry
	Exhaustive bool
	source.Span
}

// resolvedMatchEntry is one evaluated row from a matches block.
// Exactly one of Predicate / SubBlock / Value is meaningful.
type resolvedMatchEntry struct {
	Path      string
	Value     Value
	SubBlock  *resolvedMatchesBlock
	Predicate string
	source.Span
}

// evalMatchesExprDetails evaluates one lowered matches expression
// and returns its full mismatch set.
func evalMatchesExprDetails(expr *ir.MatchesExpr, env *Env) (MatchesResult, error) {
	target, err := EvalExpr(expr.Target, env)
	if err != nil {
		return MatchesResult{}, err
	}

	block, err := evalMatchesBlock(expr.Block, env)
	if err != nil {
		return MatchesResult{}, err
	}

	return evalMatchesTarget(target, matchesTargetName(expr.Target), block)
}

// FindFailedMatchesExpr returns the first failed `matches`
// expression that contributed to expr evaluating false under the
// language's boolean semantics. It follows logical short-circuit
// behaviour so it does not report failures from branches the
// original expression would not have evaluated.
func FindFailedMatchesExpr(expr ir.Expr, env *Env) (MatchesResult, bool, error) {
	switch e := expr.(type) {
	case *ir.MatchesExpr:
		result, err := evalMatchesExprDetails(e, env)
		if err != nil {
			return MatchesResult{}, false, err
		}

		return result, !result.Matched, nil
	case *ir.LogicalExpr:
		leftV, err := EvalExpr(e.Left, env)
		if err != nil {
			return MatchesResult{}, false, err
		}

		leftB, err := AsBool(leftV)
		if err != nil {
			return MatchesResult{}, false, err
		}

		switch e.Op {
		case "and":
			if !leftB {
				return FindFailedMatchesExpr(e.Left, env)
			}
			rightV, err := EvalExpr(e.Right, env)
			if err != nil {
				return MatchesResult{}, false, err
			}

			rightB, err := AsBool(rightV)
			if err != nil {
				return MatchesResult{}, false, err
			}

			if !rightB {
				return FindFailedMatchesExpr(e.Right, env)
			}
			return MatchesResult{}, false, nil
		case "or":
			if leftB {
				return MatchesResult{}, false, nil
			}
			rightV, err := EvalExpr(e.Right, env)
			if err != nil {
				return MatchesResult{}, false, err
			}

			rightB, err := AsBool(rightV)
			if err != nil {
				return MatchesResult{}, false, err
			}

			if !rightB {
				if result, ok, err := FindFailedMatchesExpr(e.Left, env); err != nil || ok {
					return result, ok, err
				}

				return FindFailedMatchesExpr(e.Right, env)
			}
			return MatchesResult{}, false, nil
		default:
			return MatchesResult{}, false, nil
		}
	case *ir.NotExpr:
		return MatchesResult{}, false, nil
	default:
		for _, child := range childExprsForFailedMatches(expr) {
			if result, ok, err := FindFailedMatchesExpr(child, env); err != nil || ok {
				return result, ok, err
			}
		}
		return MatchesResult{}, false, nil
	}
}

func childExprsForFailedMatches(expr ir.Expr) []ir.Expr {
	switch e := expr.(type) {
	case *ir.BinaryExpr:
		return []ir.Expr{e.Left, e.Right}
	case *ir.UnaryExpr:
		return []ir.Expr{e.Operand}
	case *ir.NegateExpr:
		return []ir.Expr{e.Operand}
	case *ir.PureCallExpr:
		return e.Args
	case *ir.ThreadExpr:
		children := make([]ir.Expr, 0, len(e.Args)+1)
		children = append(children, e.LHS)
		children = append(children, e.Args...)
		return children
	case *ir.ListExpr:
		return e.Elems
	case *ir.InterpStringExpr:
		children := make([]ir.Expr, 0, len(e.Segments))
		for _, seg := range e.Segments {
			if seg.Expr != nil {
				children = append(children, seg.Expr)
			}
		}
		return children
	default:
		return nil
	}
}

func matchesTargetName(expr ir.Expr) string {
	switch e := expr.(type) {
	case *ir.VarRefExpr:
		if e.Path != "" {
			return e.Name + "." + e.Path
		}
		return e.Name
	default:
		return strings.TrimPrefix(ir.FormatExprSource(expr), "$")
	}
}

func evalMatchesTarget(target Value, targetName string, block resolvedMatchesBlock) (MatchesResult, error) {
	if !target.IsStructured() {
		return MatchesResult{}, fmt.Errorf("matches requires a structured value as the target (got %s)", matchesTargetDisplay(target))
	}
	if len(block.Entries) == 0 && !block.Exhaustive {
		return MatchesResult{}, fmt.Errorf("matches block must contain at least one entry")
	}

	var mismatches []MatchMismatch
	evalMatchesAgainst(target, targetName, "", block, &mismatches)
	return MatchesResult{
		Matched:    len(mismatches) == 0,
		Mismatches: mismatches,
	}, nil
}

func matchesTargetDisplay(v Value) string {
	switch {
	case v.IsNil(), v.IsNull():
		return "null"
	case v.IsStructured():
		return structuredShape(v)
	default:
		s, err := RenderCompact(v)
		if err != nil {
			return v.Kind().String()
		}

		return s
	}
}

func joinMatchesPath(prefix, path string) string {
	switch {
	case prefix == "":
		return path
	case path == "":
		return prefix
	default:
		return prefix + "." + path
	}
}

// evalMatchesAgainst checks each entry of block against the
// target value and appends any mismatches to dst. Recursive over
// sub-blocks; honours exhaustive coverage at every block's level
// independently.
func evalMatchesAgainst(target Value, targetName, displayPrefix string, block resolvedMatchesBlock, dst *[]MatchMismatch) {
	var actualKeys map[string]bool
	if block.Exhaustive {
		m, ok := target.Raw().(map[string]any)
		if !ok {
			if displayPrefix == "" {
				appendMatchMismatch(dst, block.Pos, fmt.Sprintf("matches exhaustive: expected object, got %s", target.Kind()))
			} else {
				appendMatchMismatch(dst, block.Pos, fmt.Sprintf("%s: matches exhaustive: expected object, got %s", displayPrefix, target.Kind()))
			}
			actualKeys = map[string]bool{}
		} else {
			actualKeys = make(map[string]bool, len(m))
			for k := range m {
				actualKeys[k] = true
			}
		}
	}

	claimed := make(map[string]bool, len(block.Entries))
	for _, entry := range block.Entries {
		if block.Exhaustive {
			claimed[entry.Path] = true
		}
		displayPath := joinMatchesPath(displayPrefix, entry.Path)

		actual, err := target.LookupValue(targetName, entry.Path)
		if err != nil {
			appendMatchMismatch(dst, entry.Pos, fmt.Sprintf("%s: %v", displayPath, err))
			continue
		}

		switch {
		case entry.SubBlock != nil:
			subName := targetName + "." + entry.Path
			evalMatchesAgainst(actual, subName, displayPath, *entry.SubBlock, dst)

		case entry.Predicate == "not-empty":
			if isMatchesEmpty(actual) {
				appendMatchMismatch(dst, entry.Pos, fmt.Sprintf("%s: expected non-empty, got %s", displayPath, matchesEmptyDescription(actual)))
			}

		case entry.Predicate == "null":
			if !isNullish(actual) {
				appendMatchMismatch(dst, entry.Pos, fmt.Sprintf("%s: expected null, got %s", displayPath, matchesValueDisplay(actual)))
			}

		case entry.Predicate == "empty":
			if isNullish(actual) {
				appendMatchMismatch(dst, entry.Pos, fmt.Sprintf("%s: expected empty (\"\" / [] / {}), got null", displayPath))
			} else if !isMatchesEmpty(actual) {
				appendMatchMismatch(dst, entry.Pos, fmt.Sprintf("%s: expected empty (\"\" / [] / {}), got %s", displayPath, matchesValueDisplay(actual)))
			}

		default:
			if !matchesValueEqual(actual, entry.Value) {
				appendMatchMismatch(dst, entry.Pos, fmt.Sprintf("%s: expected %s, got %s", displayPath, matchesValueDisplay(entry.Value), matchesValueDisplay(actual)))
			}
		}
	}

	if block.Exhaustive {
		for key := range actualKeys {
			if !claimed[key] {
				appendMatchMismatch(dst, block.Pos, fmt.Sprintf("%s: present in value but unclaimed in exhaustive block", joinMatchesPath(displayPrefix, key)))
			}
		}
	}
}

func appendMatchMismatch(dst *[]MatchMismatch, pos source.Pos, msg string) {
	*dst = append(*dst, MatchMismatch{Pos: pos, Message: msg})
}

// isMatchesEmpty reports whether v is the matches-block notion of
// "empty" -- the Go zero-value convention applied uniformly: null
// (JSON null), empty string, empty list, empty map, numeric zero,
// false. Mirrors the expression-form not-empty predicate's shape
// so the inline `field: not-empty` and the standalone
// `assert not-empty $X.field` read identically.
func isMatchesEmpty(v Value) bool {
	if isNullish(v) {
		return true
	}
	switch x := v.Raw().(type) {
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case json.Number:
		f, err := x.Float64()
		return err == nil && f == 0
	case float64:
		return x == 0
	case bool:
		return !x
	}
	return false
}

// matchesEmptyDescription renders a short human-readable hint
// for the empty kind of v, used in "expected non-empty, got X"
// diagnostics.
func matchesEmptyDescription(v Value) string {
	if isNullish(v) {
		return "null"
	}
	switch v.Raw().(type) {
	case string:
		return `""`
	case []any:
		return "[]"
	case map[string]any:
		return "{}"
	case json.Number, float64:
		return "0"
	case bool:
		return "false"
	}
	return v.Kind().String()
}

// matchesValueEqual compares an actual value at a matches entry's
// path with the entry's evaluated pattern value. Equality is
// kind-aware: scalars compare by their Scalar() text (the legacy
// behaviour), and structured values (lists, maps) compare via
// their raw representation so `field: []` and `field: {}` patterns
// work natively. Null values compare equal only to null.
func matchesValueEqual(actual, expected Value) bool {
	if isNullish(actual) || isNullish(expected) {
		return isNullish(actual) && isNullish(expected)
	}
	if actual.IsStructured() || expected.IsStructured() {
		return reflect.DeepEqual(actual.Raw(), expected.Raw())
	}
	a, errA := actual.Scalar()
	e, errE := expected.Scalar()
	if errA != nil || errE != nil {
		return reflect.DeepEqual(actual.Raw(), expected.Raw())
	}

	return a == e
}

// matchesValueDisplay renders a value for inclusion in a
// mismatch diagnostic. Scalars use their Scalar() text in
// quotes; lists and maps render as compact JSON so the
// "expected X, got Y" line shows the actual contents instead
// of opaque "[...]"/ "{...}" placeholders that hide the drift.
func matchesValueDisplay(v Value) string {
	if isNullish(v) {
		return "null"
	}
	if v.IsStructured() {
		if s, err := RenderCompact(v); err == nil {
			return s
		}

		return v.Kind().String()
	}
	s, err := v.Scalar()
	if err != nil {
		return v.Kind().String()
	}

	return fmt.Sprintf("%q", s)
}

func isNullish(v Value) bool {
	return v.IsNil() || v.IsNull()
}
