package builtins

import (
	"fmt"
	"os"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// AssertPredicateResult is the boolean/message pair the app-layer
// assertion policy needs when routing a predicate through
// assert/require rather than through expression evaluation.
type AssertPredicateResult struct {
	// Pass reports whether the predicate held.
	Pass bool

	// Message is the human-readable explanation reported when the
	// predicate is used as an assertion and Pass is false.
	Message string
}

func init() {
	registerAssertPredicate("path-exists", handlePathExistsPredicate)
	registerAssertPredicate("contains", handleContainsPredicate)
	registerAssertPredicate("null", handleNullPredicate)
	registerAssertPredicate("present", handlePresentPredicate)
	registerAssertPredicate("missing", handleMissingPredicate)
	registerAssertPredicate("empty", handleEmptyPredicate)
}

func registerAssertPredicate(name string, handler func(driver.Ctx) (runtime.Value, error)) {
	driver.RegisterBuiltin(driver.Builtin{
		Name:    name,
		Handler: handler,
	})
}

// EvalAssertionPredicate evaluates one side-effect-free predicate
// that both the expression lane and the assert/require policy can
// share.
func EvalAssertionPredicate(name string, span source.Span, args []runtime.Arg, env *runtime.Env) (AssertPredicateResult, error) {
	switch name {
	case "path-exists":
		return evalPathExistsPredicate(span, args)
	case "contains":
		return evalContainsPredicate(span, args)
	case "null":
		return evalNullPredicate(sessionFromEnv(env), span, args)
	case "present":
		return evalPresentPredicate(sessionFromEnv(env), span, args)
	case "missing":
		return evalMissingPredicate(sessionFromEnv(env), span, args)
	case "empty":
		return evalEmptyPredicate(sessionFromEnv(env), span, args)
	default:
		return AssertPredicateResult{}, syntax.SpanErrorf(span, "unknown assertion predicate %q", name)
	}
}

func sessionFromEnv(env *runtime.Env) *runtime.Session {
	if env == nil {
		return nil
	}
	return env.Session
}

func handlePathExistsPredicate(c driver.Ctx) (runtime.Value, error) {
	return predicateBoolResult(EvalAssertionPredicate("path-exists", c.Span, c.Args, c.Env))
}

func handleContainsPredicate(c driver.Ctx) (runtime.Value, error) {
	return predicateBoolResult(EvalAssertionPredicate("contains", c.Span, c.Args, c.Env))
}

func handleNullPredicate(c driver.Ctx) (runtime.Value, error) {
	return predicateBoolResult(EvalAssertionPredicate("null", c.Span, c.Args, c.Env))
}

func handlePresentPredicate(c driver.Ctx) (runtime.Value, error) {
	return predicateBoolResult(EvalAssertionPredicate("present", c.Span, c.Args, c.Env))
}

func handleMissingPredicate(c driver.Ctx) (runtime.Value, error) {
	return predicateBoolResult(EvalAssertionPredicate("missing", c.Span, c.Args, c.Env))
}

func handleEmptyPredicate(c driver.Ctx) (runtime.Value, error) {
	return predicateBoolResult(EvalAssertionPredicate("empty", c.Span, c.Args, c.Env))
}

func predicateBoolResult(result AssertPredicateResult, err error) (runtime.Value, error) {
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.BoolValue(result.Pass), nil
}

func evalPathExistsPredicate(span source.Span, args []runtime.Arg) (AssertPredicateResult, error) {
	if len(args) != 1 {
		return AssertPredicateResult{}, syntax.SpanErrorf(span, "path-exists requires exactly 1 argument: <filepath>")
	}
	path := driver.ArgText(args[0])
	_, err := os.Stat(path)
	return AssertPredicateResult{
		Pass:    err == nil,
		Message: fmt.Sprintf("expected path %q to exist", path),
	}, nil
}

func evalContainsPredicate(span source.Span, args []runtime.Arg) (AssertPredicateResult, error) {
	if len(args) != 2 {
		return AssertPredicateResult{}, syntax.SpanErrorf(span, "contains requires exactly 2 arguments: <haystack> <needle>")
	}
	texts := driver.ArgTexts(args)
	return AssertPredicateResult{
		Pass:    strings.Contains(texts[0], texts[1]),
		Message: fmt.Sprintf("expected %q to contain %q", texts[0], texts[1]),
	}, nil
}

func evalNullPredicate(session *runtime.Session, span source.Span, args []runtime.Arg) (AssertPredicateResult, error) {
	if len(args) != 1 {
		return AssertPredicateResult{}, syntax.SpanErrorf(span, "null requires exactly 1 argument (a value expression or bare variable name)")
	}
	val, missing, null, display, err := classifyPredicateOperand(session, args[0])
	if err != nil {
		return AssertPredicateResult{}, err
	}
	return AssertPredicateResult{
		Pass:    !missing && (null || val.IsNull() || val.IsNil()),
		Message: fmt.Sprintf("expected %s to be null", display),
	}, nil
}

func evalPresentPredicate(session *runtime.Session, span source.Span, args []runtime.Arg) (AssertPredicateResult, error) {
	if len(args) != 1 {
		return AssertPredicateResult{}, syntax.SpanErrorf(span, "present requires exactly 1 argument (a value expression or bare variable name)")
	}
	_, missing, _, display, err := classifyPredicateOperand(session, args[0])
	if err != nil {
		return AssertPredicateResult{}, err
	}
	return AssertPredicateResult{
		Pass:    !missing,
		Message: fmt.Sprintf("expected %s to be present", display),
	}, nil
}

func evalMissingPredicate(session *runtime.Session, span source.Span, args []runtime.Arg) (AssertPredicateResult, error) {
	if len(args) != 1 {
		return AssertPredicateResult{}, syntax.SpanErrorf(span, "missing requires exactly 1 argument (a value expression or bare variable name)")
	}
	_, missing, _, display, err := classifyPredicateOperand(session, args[0])
	if err != nil {
		return AssertPredicateResult{}, err
	}
	return AssertPredicateResult{
		Pass:    missing,
		Message: fmt.Sprintf("expected %s to be missing from the shape", display),
	}, nil
}

func evalEmptyPredicate(session *runtime.Session, span source.Span, args []runtime.Arg) (AssertPredicateResult, error) {
	if len(args) != 1 {
		return AssertPredicateResult{}, syntax.SpanErrorf(span, "empty requires exactly 1 argument (a value expression or bare variable name)")
	}
	val, missing, null, display, err := classifyPredicateOperand(session, args[0])
	if err != nil {
		return AssertPredicateResult{}, err
	}
	if missing || null {
		return AssertPredicateResult{
			Pass:    false,
			Message: fmt.Sprintf("expected %s to be empty (\"\" / [] / {})", display),
		}, nil
	}
	pass := false
	switch x := val.Raw().(type) {
	case string:
		pass = x == ""
	case []any:
		pass = len(x) == 0
	case map[string]any:
		pass = len(x) == 0
	}
	return AssertPredicateResult{
		Pass:    pass,
		Message: fmt.Sprintf("expected %s to be empty (\"\" / [] / {})", display),
	}, nil
}

func classifyPredicateOperand(session *runtime.Session, a runtime.Arg) (runtime.Value, bool, bool, string, error) {
	switch v := a.(type) {
	case runtime.WordArg:
		val, missing, null, err := lookupBareVarSoft(session, v.Text)
		if err != nil {
			return runtime.Value{}, false, false, v.Text, err
		}
		return val, missing, null, v.Text, nil
	case runtime.NilArg:
		return runtime.Value{}, false, true, "<null>", nil
	case runtime.MissingArg:
		display := "$" + v.Name
		if v.Path != "" {
			display += "." + v.Path
		}
		return runtime.Value{}, true, false, display, nil
	case runtime.ScalarValueArg:
		if v.HasValue && (v.Value.IsNull() || v.Value.IsNil()) {
			return v.Value, false, true, v.Text, nil
		}
		return runtime.StringValue(v.Text), false, false, v.Text, nil
	case runtime.StructuredValueArg:
		if v.Value.IsNull() || v.Value.IsNil() {
			display := "$" + v.Name
			return v.Value, false, true, display, nil
		}
		display := "$" + v.Name
		return v.Value, false, false, display, nil
	case runtime.QuotedArg:
		return runtime.StringValue(v.Text), false, false, "\"" + v.Text + "\"", nil
	case runtime.AdapterArg:
		display := v.Adapter + ":$" + v.Name
		if v.Path != "" {
			display += "." + v.Path
		}
		return v.Value, false, false, display, nil
	default:
		return runtime.Value{}, false, false, "", fmt.Errorf("unsupported argument %T", a)
	}
}

func lookupBareVarSoft(session *runtime.Session, arg string) (runtime.Value, bool, bool, error) {
	if session == nil {
		return runtime.Value{}, false, false, fmt.Errorf("no active session to resolve %q", arg)
	}
	varName := arg
	path := ""
	if i := strings.IndexAny(arg, ".["); i >= 0 {
		varName = arg[:i]
		path = strings.TrimPrefix(arg[i:], ".")
	}
	v, ok := session.Get(varName)
	if !ok {
		return runtime.Value{}, true, false, nil
	}
	presence, err := v.LookupPresence(varName, path)
	if err != nil {
		return runtime.Value{}, false, false, err
	}
	return presence.Value(), presence.IsMissing(), presence.IsNull(), nil
}
