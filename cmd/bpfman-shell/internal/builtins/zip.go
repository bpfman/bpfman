// The zip pure builtin: 'zip A B' walks two lists in lock-step and
// produces a list of 2-element pair lists. Pairs into multi-var
// foreach destructure the elements back into named bindings so a
// script expresses parallel iteration without index bookkeeping:
//
//	foreach (prio po) in (zip $priorities $proceed_ons) {
//	    bpfman link attach tc ... --priority $prio --proceed-on $po $prog
//	}
//
// Arity is fixed at 2: the pure-builtin registry holds a single
// arity per name.
//
// Length mismatch is a hard error rather than a silent truncation:
// the parallel-list pattern this primitive exists to serve carries
// an implicit "these lists are paired" invariant, so silently
// dropping the tail of the longer list would convert a bug into
// wrong-shape output. Python 3.10's zip(strict=True) made the same
// trade-off for the same reason.
package builtins

import (
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "zip",
		Handler:  HandleZip,
		Category: driver.CategoryIO,
		Usage:    "zip <list> <list>",
		Summary:  "Pair two lists element-wise into a list of 2-element pair lists.",
		Detail: "zip is a pure builtin called from expression position: " +
			"'foreach (a b) in (zip $xs $ys) { ... }' or 'let pairs = zip $xs $ys'. " +
			"Length mismatch is a hard error rather than silent truncation: " +
			"parallel-list patterns carry an implicit \"these are paired\" " +
			"invariant, so dropping the tail of the longer list would convert " +
			"a bug into wrong-shape output. Empty + empty yields an empty list. " +
			"Multi-var foreach destructures each pair back into named bindings; " +
			"a single-var foreach binds the whole pair list and reaches the " +
			"elements via $pair[0] / $pair[1].",
	})
}

// HandleZip walks two lists in lock-step and returns a list of
// 2-element pair lists.
//
//	zip [a b c] [x y z]         -> [[a x] [b y] [c z]]
//	zip [] []                   -> []
//	zip [a b] [x]               -> error (length mismatch)
//	zip "scalar" [x]            -> error (non-list)
func HandleZip(c driver.Ctx) (runtime.Value, error) {
	args := c.Args
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("zip: expected exactly 2 arguments, got %d", len(args))
	}
	a, err := zipArgAsList(args[0], 0)
	if err != nil {
		return runtime.Value{}, err
	}

	b, err := zipArgAsList(args[1], 1)
	if err != nil {
		return runtime.Value{}, err
	}

	if len(a) != len(b) {
		return runtime.Value{}, fmt.Errorf("zip: length mismatch (arg 0 has %d, arg 1 has %d)", len(a), len(b))
	}
	out := make([]any, 0, len(a))
	for i := range a {
		out = append(out, []any{a[i], b[i]})
	}
	return runtime.ValueFromAny(out), nil
}

// zipArgAsList unwraps a runtime.Arg as a []any list, citing the
// argument position when the type is wrong. zip is only ever
// invoked from expression position (the pure-builtin parser
// resolves its operands via valueToArg), so the only shapes the
// args can take are ScalarValueArg (which is never a list) and
// StructuredValueArg (which may or may not wrap a list). The
// error message names the observed kind so the script author can
// spot the offending argument without reaching for trace.
func zipArgAsList(a runtime.Arg, pos int) ([]any, error) {
	sv, ok := a.(runtime.StructuredValueArg)
	if !ok {
		return nil, fmt.Errorf("zip: arg %d must be a list, got %s", pos, argKind(a))
	}
	raw, ok := sv.Value.Raw().([]any)
	if !ok {
		return nil, fmt.Errorf("zip: arg %d must be a list, got %s", pos, sv.Value.Kind())
	}
	return raw, nil
}

// argKind returns a human-readable label for the dynamic type of
// a non-structured runtime.Arg. Used by zip's error path so the
// "must be a list" message names what came in instead.
func argKind(a runtime.Arg) string {
	switch a.(type) {
	case runtime.WordArg:
		return "bareword"
	case runtime.QuotedArg:
		return "quoted string"
	case runtime.ScalarValueArg:
		return "scalar"
	case runtime.AdapterArg:
		return "adapter value"
	default:
		return fmt.Sprintf("%T", a)
	}
}
