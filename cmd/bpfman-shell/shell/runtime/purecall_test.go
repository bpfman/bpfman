package runtime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/check"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

func TestParse_PureCall_BindsAsExprAtLet(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let x = path-exists 42")
	require.NoError(t, err)
	let, ok := firstStmt(t, prog).(*syntax.LetStmt)
	require.True(t, ok)
	call, ok := let.RHS.(*syntax.PureCallExpr)
	require.True(t, ok, "expected PureCallExpr, got %T", let.RHS)
	assert.Equal(t, "path-exists", call.Name)
	require.Len(t, call.Args, 1)
	lit, ok := call.Args[0].(*syntax.LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "42", lit.Text)
}

func TestParse_PureCall_TrailingArithmeticBindsOutside(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let x = path-exists 5 + 1")
	require.NoError(t, err)
	let, _ := firstStmt(t, prog).(*syntax.LetStmt)
	bin, ok := let.RHS.(*syntax.BinaryExpr)
	require.True(t, ok, "trailing + should bind outside the call, got %T", let.RHS)
	assert.Equal(t, "+", bin.Op)
	_, ok = bin.Left.(*syntax.PureCallExpr)
	require.True(t, ok, "left of + should be the pure call")
}

func TestParse_PureCall_ParenthesisedArgConsumesFullExpression(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let x = path-exists (5 + 1)")
	require.NoError(t, err)
	let, _ := firstStmt(t, prog).(*syntax.LetStmt)
	call, ok := let.RHS.(*syntax.PureCallExpr)
	require.True(t, ok)
	require.Len(t, call.Args, 1)
	bin, ok := call.Args[0].(*syntax.BinaryExpr)
	require.True(t, ok, "parenthesised arg should be the binary expression")
	assert.Equal(t, "+", bin.Op)
}

func TestParse_PureCall_MissingArgsIsParseError(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "let x = path-exists")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path-exists")
	assert.Contains(t, err.Error(), "expected 1")
}

func TestParse_PureCall_UnregisteredNameStaysLiteral(t *testing.T) {
	t.Parallel()

	// 'definitely-not-registered' is not in the registry, so it
	// falls through to parsePrimary and lands as a literal.
	prog, err := parseSource(t, "let x = definitely-not-registered")
	require.NoError(t, err)
	let, _ := firstStmt(t, prog).(*syntax.LetStmt)
	lit, ok := let.RHS.(*syntax.LiteralExpr)
	require.True(t, ok, "unregistered name should fall through to literal, got %T", let.RHS)
	assert.Equal(t, "definitely-not-registered", lit.Text)
}

func TestParse_PureCall_VarRefArg(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let x = path-exists $n")
	require.NoError(t, err)
	let, _ := firstStmt(t, prog).(*syntax.LetStmt)
	call, _ := let.RHS.(*syntax.PureCallExpr)
	require.NotNil(t, call)
	assert.Equal(t, "path-exists", call.Name)
	require.Len(t, call.Args, 1)
	vr, ok := call.Args[0].(*syntax.VarRefExpr)
	require.True(t, ok)
	assert.Equal(t, "n", vr.Name)
}

func TestEvalExpr_PureCall_DispatchesThroughExecBind(t *testing.T) {
	t.Parallel()

	name := "u32le"
	var captured []Arg
	env := &Env{
		Session: NewSession(),
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			captured = args
			return StringValue("primary-result"), nil
		}),
	}
	call := &syntax.PureCallExpr{
		Name: name,
		Args: []syntax.Expr{&syntax.LiteralExpr{Text: "42"}},
	}
	v, err := evalLoweredExpr(call, env)
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "primary-result", s)
	require.Len(t, captured, 2, "name prepended as first arg")
	assert.Equal(t, name, captured[0].(WordArg).Text)
	// Bareword literal args reach the pure-builtin handler as
	// WordArg (preserving the user-typed distinction) rather than
	// ScalarValueArg. Handlers that need the rendered text use
	// driver.ArgText which is variant-agnostic; handlers that care
	// about provenance (jq's JSON decoder) get the right
	// distinction at the WordArg / QuotedArg / ScalarValueArg
	// boundary.
	word, ok := captured[1].(WordArg)
	require.True(t, ok)
	assert.Equal(t, "42", word.Text)
}

func TestEvalExpr_PureCall_NoExecBindIsError(t *testing.T) {
	t.Parallel()

	name := "u32le"
	env := &Env{Session: NewSession()}
	call := &syntax.PureCallExpr{Name: name, Args: []syntax.Expr{&syntax.LiteralExpr{Text: "1"}}}
	_, err := evalLoweredExpr(call, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), name)
}

func TestEvalExpr_PureCall_FailingHandlerIsExpressionError(t *testing.T) {
	t.Parallel()

	name := "u32le"
	env := &Env{
		Session: NewSession(),
		ExecBind: func(_ []Arg, _ source.Span) (BindResult, error) {
			return BindResult{Rc: Envelope{ExitCode: 7, Stderr: "bad"}}, nil
		},
	}
	call := &syntax.PureCallExpr{Name: name, Args: []syntax.Expr{&syntax.LiteralExpr{Text: "1"}}}
	_, err := evalLoweredExpr(call, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad")
}

func TestEvalExpr_PureCall_InsideInterpString(t *testing.T) {
	t.Parallel()

	name := "u32le"
	src := `let s = "value=${` + name + ` 255}"`
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err)
	let := prog.Stmts[0].(*syntax.LetStmt)
	is, ok := let.RHS.(*syntax.InterpStringExpr)
	require.True(t, ok)
	require.Len(t, is.Segments, 2)
	require.NotNil(t, is.Segments[1].Expr)
	_, ok = is.Segments[1].Expr.(*syntax.PureCallExpr)
	require.True(t, ok, "${hex 255} inside an interp string should parse as a PureCallExpr")

	env := &Env{
		Session: NewSession(),
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			require.Len(t, args, 2)
			return StringValue("ff"), nil
		}),
	}
	v, err := evalLoweredExpr(let.RHS, env)
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "value=ff", s)
}

func TestCheck_PureCall_ReturnShapeFlowsThroughLet(t *testing.T) {
	t.Parallel()

	src := "let x = u32le 42\nprint $x.field"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "x has kind scalar")
}

func TestCheck_PureCall_UnknownReturnShapeIsPermissive(t *testing.T) {
	t.Parallel()

	src := "let x = jq \"{}\" \".\"\nprint $x.deep.path[0]"
	issues := checkSource(t, src)
	assert.Empty(t, issues, "OriginUnknown return propagates as a wildcard")
}

func TestCheck_PureCall_ArgsAreCheckedForUndefinedVars(t *testing.T) {
	t.Parallel()

	src := "let x = path-exists $undef"
	issues := checkSource(t, src)
	require.NotEmpty(t, issues)
	combined := joinIssues(issues)
	assert.Contains(t, combined, "undefined variable")
	assert.Contains(t, combined, "undef")
}

func TestCheck_PureBuiltin_RejectedInBindForm(t *testing.T) {
	t.Parallel()

	src := "let x <- path-exists 1"
	issues := checkSource(t, src)
	require.NotEmpty(t, issues)
	combined := joinIssues(issues)
	assert.Contains(t, combined, "path-exists")
	assert.Contains(t, combined, "pure builtin")
	assert.Contains(t, combined, "let x = path-exists")
}

func TestCheck_PureBuiltin_RejectedInGuardForm(t *testing.T) {
	t.Parallel()

	src := "guard x <- path-exists 1"
	issues := checkSource(t, src)
	require.NotEmpty(t, issues)
	combined := joinIssues(issues)
	assert.Contains(t, combined, "pure builtin")
}

// joinIssues collapses a slice of issues into one string so a
// single assertion can probe for substrings that may appear in
// any entry.
func joinIssues(issues []check.Issue) string {
	var sb strings.Builder
	for _, i := range issues {
		sb.WriteString(i.Msg)
		sb.WriteString("\n")
	}
	return sb.String()
}
