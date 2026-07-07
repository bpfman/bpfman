// Assertion policy for the shell DSL. The shell package owns the
// syntax, AST, IR, and clause discriminator; this file owns the
// app-layer behaviour when an assertion fires: how failures are
// rendered, how the remaining command-status forms dispatch, and
// how assert vs require affect counters and control flow.
//
// Assert failures mean "the script failed" everywhere.
// Polling is expressed explicitly through `poll` + `retry`.
// Require is fatal everywhere.
package main

import (
	"fmt"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/internal/builtins"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// assertResult holds the outcome of evaluating an assertion verb.
type assertResult struct {
	pass    bool
	message string
}

func completeAssertResult(cli *cli.CLI, session *runtime.Session, isRequire bool, failureSpan source.Span, loc driver.SourceLoc, label string, result assertResult) error {
	if result.pass {
		return nil
	}
	_ = cli.PrintErrf("%s[%s] FAIL: %s\n", loc, label, result.message)
	if isRequire {
		return &runtime.RequireFailure{Span: failureSpan, Expr: result.message}
	}
	session.RecordAssertFailure()
	return nil
}

// makeExecAssert returns the Env.ExecAssert callback used by
// the lowered runtime.
func makeExecAssert(cli *cli.CLI, session *runtime.Session) func(*ir.Assert, *runtime.Env) error {
	return func(a *ir.Assert, env *runtime.Env) error {
		return runAssertClause(cli, session, a.IsRequire, a.Span, a.Clause, env)
	}
}

func runAssertClause(cli *cli.CLI, session *runtime.Session, isRequire bool, span source.Span, clause ir.AssertClause, env *runtime.Env) error {
	// Lowered helper calls preserve the original assert span, so a
	// dynamic rejection under poll still points at the assert itself.
	if !isRequire && env.InPoll() {
		return syntax.SpanErrorf(span, "assert is not valid inside poll; use retry unless ... or require ...")
	}
	switch v := clause.(type) {
	case *ir.AssertExprClause:
		if isBareNullExpr(v.Expr) {
			return assertNullUsageError(assertLabel(isRequire), span)
		}
		return runAssertExprCallback(
			cli,
			session,
			isRequire,
			span,
			env,
			ir.FormatAssertClauseSource(clause),
			func(env *runtime.Env) (runtime.Value, error) {
				return runtime.EvalExpr(v.Expr, env)
			},
			func(env *runtime.Env, loc driver.SourceLoc) (string, error) {
				return formatAssertExprFailure(v.Expr, env, session, loc)
			},
		)
	case *ir.AssertCommandClause:
		return runAssertCommandClause(cli, session, isRequire, span, v, env)
	default:
		return fmt.Errorf("assert: unsupported lowered clause %T", clause)
	}
}

func runAssertCommandClause(cli *cli.CLI, session *runtime.Session, isRequire bool, span source.Span, clause *ir.AssertCommandClause, env *runtime.Env) error {
	args, err := runtime.EvalArgs(clause.Args, env)
	if err != nil {
		return err
	}

	return finishAssertVerbClause(cli, session, isRequire, span, clause.Head, clause.HeadSpan, clause.Negate, args, env)
}

func assertLabel(isRequire bool) string {
	if isRequire {
		return "require"
	}
	return "assert"
}

func assertNullUsageError(label string, span source.Span) error {
	return syntax.SpanErrorf(span, "%s null requires a target; did you mean %s null $x or %s $x == null?", label, label, label)
}

func isBareNullExpr(e ir.Expr) bool {
	lit, ok := e.(*ir.LiteralExpr)
	return ok && !lit.Quoted && lit.Text == "null"
}

func finishAssertVerbClause(cli *cli.CLI, session *runtime.Session, isRequire bool, span source.Span, head string, headSpan source.Span, negate bool, args []runtime.Arg, env *runtime.Env) error {
	verbArg := runtime.WordArg{Text: head, Span: headSpan}
	result, err := evalAssertVerb(env, verbArg, head, args)
	if err != nil {
		return err
	}

	if negate {
		result.pass = !result.pass
		result.message = negateMessage(result.message)
	}
	loc := driver.SourceLoc{}.WithSpan(span)
	return completeAssertResult(cli, session, isRequire, headSpan, loc, assertLabel(isRequire), result)
}

func runAssertExprCallback(
	cli *cli.CLI,
	session *runtime.Session,
	isRequire bool,
	span source.Span,
	env *runtime.Env,
	traceSource string,
	eval func(*runtime.Env) (runtime.Value, error),
	failureMessage func(*runtime.Env, driver.SourceLoc) (string, error),
) error {
	loc := driver.SourceLoc{}.WithSpan(span)
	v, err := eval(env)
	if err != nil {
		return err
	}

	pass, err := runtime.AsBool(v)
	if err != nil {
		return err
	}

	label := "assert"
	if isRequire {
		label = "require"
	}
	if env.Trace != nil {
		env.Trace(span.Pos, fmt.Sprintf("%s %s", label, traceSource))
	}
	if pass {
		return nil
	}
	message, err := failureMessage(env, loc)
	if err != nil {
		return err
	}

	return completeAssertResult(cli, session, isRequire, span, loc, label, assertResult{
		pass:    false,
		message: message,
	})
}

func formatAssertExprFailure(expr ir.Expr, env *runtime.Env, session *runtime.Session, loc driver.SourceLoc) (string, error) {
	if result, ok, err := runtime.FindFailedMatchesExpr(expr, env); err != nil {
		return "", err
	} else if ok {
		return formatMatchesFailureMessage(result, matchesLocator(loc)), nil
	}
	return formatExprFailure(expr, session), nil
}

func formatMatchesFailureMessage(result runtime.MatchesResult, locate func(source.Pos, string) string) string {
	if len(result.Mismatches) == 0 {
		return "matches block held"
	}
	lines := make([]string, 0, len(result.Mismatches))
	for _, mm := range result.Mismatches {
		if locate != nil {
			lines = append(lines, locate(mm.Pos, mm.Message))
			continue
		}
		lines = append(lines, mm.Message)
	}
	noun := "mismatch"
	if len(lines) > 1 {
		noun = "mismatches"
	}
	return fmt.Sprintf("matches: %d %s\n  %s", len(lines), noun, strings.Join(lines, "\n  "))
}

func formatExprFailure(e ir.Expr, session *runtime.Session) string {
	switch x := e.(type) {
	case *ir.BinaryExpr:
		left := exprScalar(x.Left, session)
		right := exprScalar(x.Right, session)
		switch x.Op {
		case "==":
			return fmt.Sprintf("expected %q to equal %q", left, right)
		case "!=":
			return fmt.Sprintf("expected %q to not equal %q", left, right)
		default:
			return fmt.Sprintf("expected %s %s %s", left, x.Op, right)
		}
	case *ir.UnaryExpr:
		operand := exprScalar(x.Operand, session)
		switch x.Pred {
		case "not-empty":
			return fmt.Sprintf("expected non-empty string, got %q", operand)
		default:
			return fmt.Sprintf("expected predicate %s to hold on %s", x.Pred, operand)
		}
	case *ir.NotExpr:
		if msg, ok := formatPurePredicateExprFailure(x.Operand, session); ok {
			return negateMessage(msg)
		}
	case *ir.PureCallExpr:
		if msg, ok := formatPurePredicateExprFailure(x, session); ok {
			return msg
		}
	}
	return fmt.Sprintf("expected %s to be true", exprScalar(e, session))
}

func formatPurePredicateExprFailure(e ir.Expr, session *runtime.Session) (string, bool) {
	call, ok := e.(*ir.PureCallExpr)
	if !ok {
		return "", false
	}
	switch call.Name {
	case "path-exists":
		if len(call.Args) == 1 {
			return fmt.Sprintf("expected path %q to exist", exprScalar(call.Args[0], session)), true
		}
	case "contains":
		if len(call.Args) == 2 {
			return fmt.Sprintf("expected %q to contain %q", exprScalar(call.Args[0], session), exprScalar(call.Args[1], session)), true
		}
	case "null":
		if len(call.Args) == 1 {
			return fmt.Sprintf("expected %s to be null", ir.FormatExprSource(call.Args[0])), true
		}
	case "present":
		if len(call.Args) == 1 {
			return fmt.Sprintf("expected %s to be present", ir.FormatExprSource(call.Args[0])), true
		}
	case "missing":
		if len(call.Args) == 1 {
			return fmt.Sprintf("expected %s to be missing from the shape", ir.FormatExprSource(call.Args[0])), true
		}
	case "empty":
		if len(call.Args) == 1 {
			return fmt.Sprintf("expected %s to be empty (\"\" / [] / {})", ir.FormatExprSource(call.Args[0])), true
		}
	}
	return "", false
}

func exprScalar(e ir.Expr, session *runtime.Session) string {
	v, err := runtime.EvalExpr(e, &runtime.Env{Session: session})
	if err != nil {
		return "<err>"
	}
	s, err := v.Scalar()
	if err != nil {
		return "<" + v.Kind().String() + ">"
	}
	return s
}

// evalAssertVerb dispatches the command-form assertion heads.
// `ok` / `fail` remain command-shaped; the named predicates route
// through the shared expression/builtin predicate mechanism for
// compatibility.
func evalAssertVerb(env *runtime.Env, verbArg runtime.Arg, verb string, args []runtime.Arg) (assertResult, error) {
	verbSpan := runtime.ArgSpan(verbArg)
	switch verb {
	case "ok":
		return assertOk(env, verbSpan, args)
	case "fail":
		return assertFail(env, verbSpan, args)
	case "path-exists", "contains", "null", "present", "missing", "empty":
		result, err := builtins.EvalAssertionPredicate(verb, verbSpan, args, env)
		if err != nil {
			return assertResult{}, err
		}
		return assertResult{pass: result.Pass, message: result.Message}, nil
	case "==", "!=", "<", "<=", ">", ">=":
		return assertResult{}, syntax.SpanErrorf(verbSpan, "%q goes between two values: try 'assert <left> %s <right>'", verb, verb)
	case "not-empty":
		return assertResult{}, syntax.SpanErrorf(verbSpan, "%q takes one value: try 'assert %s $name'", verb, verb)
	default:
		return assertResult{}, syntax.SpanErrorf(verbSpan, "unknown assertion verb %q", verb)
	}
}

func assertCommandBind(env *runtime.Env, verbSpan source.Span, args []runtime.Arg) (runtime.BindResult, error) {
	if env == nil || env.ExecBind == nil {
		return runtime.BindResult{}, syntax.SpanErrorf(verbSpan, "assert %q requires bind dispatch in the runtime environment", driver.ArgText(args[0]))
	}
	return env.ExecBind(args, verbSpan)
}

func assertOk(env *runtime.Env, verbSpan source.Span, args []runtime.Arg) (assertResult, error) {
	if len(args) == 0 {
		return assertResult{}, syntax.SpanErrorf(verbSpan, "ok requires a command")
	}
	br, err := assertCommandBind(env, verbSpan, args)
	if err != nil {
		return assertResult{
			pass:    false,
			message: fmt.Sprintf("expected command to succeed, but got: %v", err),
		}, nil
	}

	if !br.Rc.OK() {
		msg := br.Rc.Stderr
		if msg == "" {
			msg = fmt.Sprintf("exit code %d", br.Rc.ExitCode)
		}
		return assertResult{
			pass:    false,
			message: fmt.Sprintf("expected command to succeed, but got: %s", msg),
		}, nil
	}
	return assertResult{
		pass:    true,
		message: "expected command to succeed",
	}, nil
}

func assertFail(env *runtime.Env, verbSpan source.Span, args []runtime.Arg) (assertResult, error) {
	if len(args) == 0 {
		return assertResult{}, syntax.SpanErrorf(verbSpan, "fail requires a command")
	}
	br, err := assertCommandBind(env, verbSpan, args)
	if err != nil {
		return assertResult{
			pass:    true,
			message: "expected command to fail",
		}, nil
	}

	if !br.Rc.OK() {
		return assertResult{
			pass:    true,
			message: "expected command to fail",
		}, nil
	}
	return assertResult{
		pass:    false,
		message: "expected command to fail, but it succeeded",
	}, nil
}

// matchesLocator returns a closure that prefixes a path message
// with the entry's source location so multi-mismatch failures
// point at the specific offending line inside the block. Entry
// positions already carry absolute file/line/col when the source
// had a file name; the base location only fills the file field for
// fileless stdin-style parses.
func matchesLocator(base driver.SourceLoc) func(source.Pos, string) string {
	return func(loc source.Pos, msg string) string {
		if loc.Line == 0 {
			return msg
		}
		file := loc.File
		if file == "" {
			file = base.File
		}
		if file != "" {
			return fmt.Sprintf("%s:%d:%d: %s", file, loc.Line, loc.Col, msg)
		}
		return fmt.Sprintf("%d:%d: %s", loc.Line, loc.Col, msg)
	}
}

// negateMessage transforms an assertion message for negated assertions.
// It inserts "not" into the message: "expected X to equal Y" becomes
// "expected X not to equal Y", "expected X to be Y" becomes
// "expected X not to be Y".
func negateMessage(msg string) string {
	// Try "to equal", "to not equal", "to be", "to contain", "to exist", "to succeed", "to fail".
	if before, after, ok := strings.Cut(msg, " to "); ok {
		return before + " not to " + after
	}
	// Try "expected command to" patterns.
	if strings.HasPrefix(msg, "expected") {
		return "expected not: " + msg[9:]
	}
	return "not: " + msg
}
