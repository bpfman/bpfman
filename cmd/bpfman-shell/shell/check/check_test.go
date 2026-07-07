package check

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// checkSource tokenises and parses src, runs Check, returns
// the issues. Tests use it as a one-liner so the source
// stays readable.
func checkSource(t *testing.T, src string) []Issue {
	t.Helper()
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err)
	return Check(prog)
}

func checkImportLibrary(t *testing.T, src string) []Issue {
	t.Helper()
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err)
	return CheckImportLibrary(prog)
}

func parseProgram(t *testing.T, src string) *syntax.Program {
	t.Helper()
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err, "tokenise failed for %q", src)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err, "parse failed for %q", src)
	return prog
}

// TestCheckerFrames_* mirror the runtime Session-frame tests in
// session_test.go: same shape, same names, exercised against the
// checker's frame stack so the two halves of the language stay
// in step.

func TestCheckerFrames_DefineWritesInnermost(t *testing.T) {
	t.Parallel()

	c := newChecker()
	c.define("x", semantics.KindShape(semantics.OriginScalar), nil)
	c.withFrame(func() {
		c.define("x", semantics.KindShape(semantics.OriginBool), nil)
		sh, ok := c.lookupShape("x")
		require.True(t, ok)
		assert.Equal(t, semantics.OriginBool, sh.Kind)
	})
	sh, ok := c.lookupShape("x")
	require.True(t, ok)
	assert.Equal(t, semantics.OriginScalar, sh.Kind)
}

func TestCheckerFrames_LookupWalksOutward(t *testing.T) {
	t.Parallel()

	c := newChecker()
	c.define("a", semantics.KindShape(semantics.OriginScalar), nil)
	c.withFrame(func() {
		c.define("b", semantics.KindShape(semantics.OriginBool), nil)
		// Inner sees both: b directly, a through the walk.
		assert.True(t, c.lookupDefined("a"))
		assert.True(t, c.lookupDefined("b"))
	})
	// After pop, only the outer remains visible.
	assert.True(t, c.lookupDefined("a"))
	assert.False(t, c.lookupDefined("b"))
}

func TestCheckerFrames_InnerShadowsOuter(t *testing.T) {
	t.Parallel()

	c := newChecker()
	c.define("x", semantics.KindShape(semantics.OriginScalar), nil)
	c.withFrame(func() {
		// Inner frame initially sees outer x.
		sh, ok := c.lookupShape("x")
		require.True(t, ok)
		assert.Equal(t, semantics.OriginScalar, sh.Kind)
		// Shadowing the name does not touch the outer binding.
		c.define("x", semantics.KindShape(semantics.OriginBool), nil)
		sh, ok = c.lookupShape("x")
		require.True(t, ok)
		assert.Equal(t, semantics.OriginBool, sh.Kind)
	})
	sh, ok := c.lookupShape("x")
	require.True(t, ok)
	assert.Equal(t, semantics.OriginScalar, sh.Kind)
}

func TestCheckerFrames_LiteralRecordingScopesToFrame(t *testing.T) {
	t.Parallel()

	// The arithmetic check inspects the recorded RHS literal
	// for non-numeric tokens. The lookup must walk frames so a
	// let inside an inner frame finds its own literal, and
	// popping the frame restores any outer literal.
	c := newChecker()
	outerLit := &syntax.LiteralExpr{Text: "outer", Quoted: true}
	c.define("x", semantics.KindShape(semantics.OriginScalar), outerLit)
	c.withFrame(func() {
		innerLit := &syntax.LiteralExpr{Text: "inner", Quoted: true}
		c.define("x", semantics.KindShape(semantics.OriginScalar), innerLit)
		got, ok := c.lookupLiteral("x")
		require.True(t, ok)
		assert.Equal(t, "inner", got.Text)
	})
	got, ok := c.lookupLiteral("x")
	require.True(t, ok)
	assert.Equal(t, "outer", got.Text)
}

func TestCheckerFrames_DiscardSlotNotBound(t *testing.T) {
	t.Parallel()

	c := newChecker()
	c.define("_", semantics.KindShape(semantics.OriginScalar), nil)
	assert.False(t, c.lookupDefined("_"))
}

func TestCheckerFrames_WithFramePopsOnPanic(t *testing.T) {
	t.Parallel()

	c := newChecker()
	c.define("x", semantics.KindShape(semantics.OriginScalar), nil)

	func() {
		defer func() {
			_ = recover()
		}()
		c.withFrame(func() {
			c.define("x", semantics.KindShape(semantics.OriginBool), nil)
			panic("boom")
		})
	}()

	// Frame popped on panic; outer binding restored.
	sh, ok := c.lookupShape("x")
	require.True(t, ok)
	assert.Equal(t, semantics.OriginScalar, sh.Kind)
}

func TestCheck_DefinedThenUsed_Clean(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "let p = \"hello\"\nprint $p")
	assert.Empty(t, issues)
}

func TestCheck_UseBeforeDefIsReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "print $porg")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: porg")
}

func TestCheck_LetRHSCheckedBeforeBinding(t *testing.T) {
	t.Parallel()

	// 'let x = $x' on a previously-undefined x must report
	// the RHS reference rather than letting the new binding
	// silently shadow the lookup.
	issues := checkSource(t, "let x = $x")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: x")
}

func TestCheck_BindCollect_LetOutcomeOnPureBuiltinAccepted(t *testing.T) {
	t.Parallel()

	// A non-guard bind-collect binds an aggregate outcome. The
	// successful per-iteration payloads are available through
	// .values.
	src := "let xs = [10 20 30]\nlet doubled <- foreach n in $xs { jq \". * 2\" $n }\nprint $doubled.values"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BindCollect_SingleBindOnPureBuiltinAccepted(t *testing.T) {
	t.Parallel()

	// Guard unwraps the bind-collect result, so the target is the
	// successful value list directly.
	src := "let xs = [10 20 30]\nguard doubled <- foreach n in $xs { jq \". * 2\" $n }\nprint $doubled[0]"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_LetBindTypedProviderBindsOutcomeShape(t *testing.T) {
	t.Parallel()

	src := "let r <- bpfman program get 1\n" +
		"print $r.ok\n" +
		"print $r.stderr\n" +
		"print $r.value.record.program_id"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_LetBindTypedProviderRejectsDirectPayloadAccess(t *testing.T) {
	t.Parallel()

	src := "let r <- bpfman program get 1\nprint $r.record.program_id"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "r has kind result")
	assert.Contains(t, issues[0].Msg, "field \"record\" does not exist")
	assert.Contains(t, issues[0].Msg, "value")
}

func TestCheck_LetBindPlainCommandRejectsValueField(t *testing.T) {
	t.Parallel()

	src := "let r <- echo hello\nprint $r.value"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "r has kind result")
	assert.Contains(t, issues[0].Msg, "field \"value\" does not exist")
}

func TestCheck_ThreadDefArityTooFew(t *testing.T) {
	t.Parallel()

	// `$value |> takes_two` resolves takes_two via the def-first
	// bind dispatch policy at runtime, appending the LHS as the
	// final positional. The def expects two arguments, the
	// thread supplies one (the LHS), so the arity is wrong --
	// runtime fails with "expected 2 argument(s), got 1". The
	// static checker must reach the same diagnostic because
	// thread is just expression-position bind-dispatch.
	src := "def takes_two(a b) { return $a }\nlet r = \"hello\" |> takes_two"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "takes_two: expected 2 argument(s), got 1")
}

func TestCheck_ThreadDefArityTooMany(t *testing.T) {
	t.Parallel()

	// Symmetric over-supply: `|> takes_one extra1 extra2` puts
	// three arguments through to a one-param def (LHS plus the
	// two explicit args after the head). The static checker
	// pulls this diagnostic forward from the runtime.
	src := "def takes_one(x) { return $x }\nlet r = \"hello\" |> takes_one extra1 extra2"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "takes_one: expected 1 argument(s), got 3")
}

func TestCheck_ThreadDefArityExact(t *testing.T) {
	t.Parallel()

	// Boundary case: arity matches exactly. The LHS becomes the
	// last positional, so a single-param def with no extra
	// thread args is a clean call.
	src := "def id(x) { return $x }\nlet r = \"hello\" |> id"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BuiltinArity_DefShadowSkipped(t *testing.T) {
	t.Parallel()

	// `start` is a known builtin with min-arity 1, but the user
	// has declared a def with the same name. Runtime dispatch
	// resolves the head to the def, so the builtin arity rule
	// no longer applies; the static checker must honour the
	// same precedence and skip the builtin spec.
	src := "def start() { print ok }\nstart"
	issues := checkSource(t, src)
	assert.Empty(t, issues, "shadowed start: builtin arity check must skip")
}

func TestCheck_KillFlags_DefShadowSkipped(t *testing.T) {
	t.Parallel()

	// `kill` shadowed by a def takes its own positional
	// arguments and has no --signal / --grace semantics. The
	// checker's kill-flag validator must not flag a literal
	// that happens to look like a builtin flag when the head
	// is actually a user def; the runtime never reaches the
	// builtin path.
	src := "def kill(arg) { print $arg }\nkill --signal=BOGUS"
	issues := checkSource(t, src)
	assert.Empty(t, issues, "shadowed kill: --signal flag check must skip")
}

func TestCheck_JobLeak_StartShadowedNoFalsePositive(t *testing.T) {
	t.Parallel()

	// A user `def start` does not create a job, so the bind
	// target it produces is not subject to the job-leak rule
	// and the checker must not demand a matching wait or kill.
	src := "def start(name) { print $name }\nlet j <- start foo"
	issues := checkSource(t, src)
	assert.Empty(t, issues, "shadowed start: no false job-leak report")
}

func TestCheck_JobLeak_KillShadowedSurfacesRealLeak(t *testing.T) {
	t.Parallel()

	// Conversely, when the real `start` does create a job and
	// the call that looks like `kill $j` is actually a user
	// def, the def does not consume the job, so the leak must
	// surface.
	src := "def kill(arg) { print $arg }\nguard j <- start foo\nkill $j"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "started job \"j\" has no matching wait or kill")
}

func TestCheck_BindCollect_ListExprIsChecked(t *testing.T) {
	t.Parallel()

	// The foreach list on the RHS of a bind-collect is an
	// expression like any other; an undefined variable there
	// must be reported, otherwise a typo in the source list
	// silently turns into a runtime "undefined variable"
	// surprise long after the checker has cleared the script.
	src := "let xs <- foreach n in $missing { jq \". * 2\" $n }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: missing")
}

func TestCheck_BindCollect_BodyVariablesAreChecked(t *testing.T) {
	t.Parallel()

	// References inside the bind-collect body need the same
	// undefined-variable check as any other statement context.
	// Without walking the body the checker rubber-stamps a
	// reference to a name no producer ever published.
	src := "let xs <- foreach n in [1 2 3] { print $n $undef }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: undef")
}

func TestCheck_BindCollect_LoopVarVisibleInBody(t *testing.T) {
	t.Parallel()

	// The flip side of walking the body: the loop variable(s)
	// must be defined inside the body's frame so a legitimate
	// reference does not flag a false-positive undefined.
	src := "let doubled <- foreach n in [1 2 3] { jq \". * 2\" $n }"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BindStmtDefinesOutcome(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "let r <- bpfman program list\nprint $r.ok $r.value.programs")
	assert.Empty(t, issues)
}

func TestCheck_BindStmtDiscardSlotDoesNotDefine(t *testing.T) {
	t.Parallel()

	// '_' as a target name discards. Subsequent '$_'
	// reference is undefined.
	issues := checkSource(t, "let _ <- bpfman program list\nprint $_")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: _")
}

func TestCheck_ForEachVarVisibleInBody(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "let xs = \"a\"\nforeach x in $xs { print $x }")
	assert.Empty(t, issues)
}

func TestCheck_ForEachVarNotVisibleAfterBody(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"let xs = \"a\"",
		"foreach x in $xs { print $x }",
		"print $x",
	}, "\n")
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: x")
}

func TestCheck_DefParamsVisibleInBody(t *testing.T) {
	t.Parallel()

	src := "def greet(name) { print $name }"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DefParamsNotVisibleAfterBody(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"def greet(name) { print $name }",
		"print $name",
	}, "\n")
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: name")
}

func TestCheck_SameScopeReletIsAllowed(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"let label = outer",
		"let label = inner",
		"print $label",
	}, "\n")
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DuplicateTopLevelDefRejected(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"def helper() { print one }",
		"def helper() { print two }",
	}, "\n")
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `duplicate top-level def "helper"`)
	assert.Contains(t, issues[0].Msg, "1:5")
}

func TestCheckImportLibrary_RejectsTopLevelLet(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"let x = 1",
		"def show() { print $x }",
	}, "\n")
	issues := checkImportLibrary(t, src)
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Msg, "imported files may contain only top-level def statements")
}

func TestCheckImportLibrary_RejectsTopLevelCommand(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"def show() { print ok }",
		"print nope",
	}, "\n")
	issues := checkImportLibrary(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "imported files may contain only top-level def statements")
}

func TestCheckImportLibraryWithDefs_VisibleDefsResolveBindHead(t *testing.T) {
	t.Parallel()

	prog := parseProgram(t, `
def outer(x) {
  guard v <- inner $x
  return $v
}
`)
	issues := CheckImportLibraryWithDefs(prog, map[string]DefStaticInfo{
		"inner": {
			Arity:     1,
			DeclPos:   source.Pos{Line: 1, Col: 1},
			HasReturn: true,
		},
	})
	assert.Empty(t, issues, "visible imported defs should resolve bind heads during library checking")
}

func TestCheckImportLibraryWithDefs_VisibleDefReturnShapeIsUsed(t *testing.T) {
	t.Parallel()

	prog := parseProgram(t, `
def outer() {
  guard p <- inner
  print $p.record.program_idd
}
`)
	issues := CheckImportLibraryWithDefs(prog, map[string]DefStaticInfo{
		"inner": {
			Arity:       0,
			DeclPos:     source.Pos{Line: 1, Col: 1},
			HasReturn:   true,
			ReturnShape: semantics.KindShape(semantics.OriginProgram),
		},
	})
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "p.record has no field")
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
}

func TestCheckImportLibraryWithDefs_RejectsDuplicateVisibleDef(t *testing.T) {
	t.Parallel()

	prog := parseProgram(t, `
def inner() {
  print shadow
}
`)
	issues := CheckImportLibraryWithDefs(prog, map[string]DefStaticInfo{
		"inner": {
			Arity:     0,
			DeclPos:   source.Pos{File: "main.bpfman", Line: 1, Col: 1},
			HasReturn: false,
		},
	})
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `duplicate top-level def "inner"`)
	assert.Contains(t, issues[0].Msg, "main.bpfman:1:1")
}

func TestCheck_ImportMustBeTopLevel(t *testing.T) {
	t.Parallel()

	src := "if true { import ./lib.bpfman }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "import must be declared at top level")
}

func TestCheck_MultipleIssuesAccumulate(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"print $a",
		"print $b",
	}, "\n")
	issues := checkSource(t, src)
	require.Len(t, issues, 2)
	assert.Contains(t, issues[0].Msg, "a")
	assert.Contains(t, issues[1].Msg, "b")
}

func TestCheck_DotPathOnDefinedNameIsClean(t *testing.T) {
	t.Parallel()

	// Field access on a typed defined name is clean when the
	// path matches the declared JSON shape.
	src := "guard p <- bpfman program get 1\nprint $p.record.program_id"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_InsideInterpolation(t *testing.T) {
	t.Parallel()

	// '${$missing}' refers to an undefined variable inside
	// the interpolation. The check descends through
	// InterpStringExpr's Segments via Inspect.
	src := "print \"${$missing}\""
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: missing")
}

func TestCheck_LeakedJobIsReported(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 60"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `started job "p" has no matching wait or kill`)
}

func TestCheck_GuardLeakedJobIsReported(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 60"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `started job "p" has no matching wait or kill`)
}

func TestCheck_WaitedJobIsClean(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 1\nwait $p"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_WaitedJobViaBindIsClean(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 1\nlet rc <- wait $p"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_KilledJobIsClean(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 60\nkill $p"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DeferKilledJobIsClean(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 60\ndefer kill $p"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_HelperReturnedJobManagedByCallerIsClean(t *testing.T) {
	t.Parallel()

	src := "def spawn() {\n" +
		"  guard p <- start sleep 60\n" +
		"  return $p\n" +
		"}\n" +
		"guard p <- spawn\n" +
		"wait $p\n"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_HelperLeakedJobWithoutReturnStillReported(t *testing.T) {
	t.Parallel()

	src := "def spawn() {\n" +
		"  guard p <- start sleep 60\n" +
		"  return 7\n" +
		"}\n" +
		"spawn\n"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `started job "p" has no matching wait or kill`)
}

func TestCheck_KillWithSignalFlagIsClean(t *testing.T) {
	t.Parallel()

	// 'kill --signal=USR1 $p' should still match $p as the
	// target. Flag args (starting with '--') are skipped.
	src := "guard p <- start sleep 60\nkill --signal=USR1 $p"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DiscardedJobIsNotChecked(t *testing.T) {
	t.Parallel()

	// 'let _ <- start ...' discards the handle; the start
	// itself is fire-and-forget, no managed lifecycle to
	// expect. We treat that as user-acknowledged and do not
	// report a leak.
	src := "let _ <- start sleep 60"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_LeakReportedAtStartSite(t *testing.T) {
	t.Parallel()

	src := "let x = 1\n\nguard p <- start sleep 60"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Equal(t, 3, issues[0].Pos.Line, "leak should be cited at the start site, not elsewhere")
}

func TestCheck_LetBindOnStartBindsOutcomeNotJob(t *testing.T) {
	t.Parallel()

	src := "let r <- start sleep 60\nkill $r"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "expected a $job argument")
	assert.Contains(t, issues[0].Msg, "result value")
}

func TestCheck_ArithmeticOnNumericLiteralsClean(t *testing.T) {
	t.Parallel()

	src := "let r = 4 * 2 + 1"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_ArithmeticOnFloatLiteralsClean(t *testing.T) {
	t.Parallel()

	src := "let r = 1.5 * 2"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_ArithmeticOnNonNumericLiteralFlagged(t *testing.T) {
	t.Parallel()

	src := "let r = 4 * bogus"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `arithmetic *: operand "bogus" is not numeric`)
}

func TestCheck_ArithmeticBothNonNumericReported(t *testing.T) {
	t.Parallel()

	src := "let r = A / B"
	issues := checkSource(t, src)
	require.Len(t, issues, 2)
	assert.Contains(t, issues[0].Msg, `operand "A"`)
	assert.Contains(t, issues[1].Msg, `operand "B"`)
}

func TestCheck_ArithmeticVarRefIsTrusted(t *testing.T) {
	t.Parallel()

	// Variable-reference operands are not flagged; we cannot
	// know their value at static time. The undefined-variable
	// check still catches the case where the name is unbound
	// (covered by TestCheck_UseBeforeDefIsReported).
	src := "let n = 4\nlet r = $n * 2"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_ArithmeticInsideInterpolationFlagged(t *testing.T) {
	t.Parallel()

	// The interpolation case the user surfaced: '${4 * Z}'
	// reaches the arithmetic check via Inspect descending
	// through the InterpStringExpr's segments.
	src := `print "${4 * Z}"`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `operand "Z"`)
}

func TestCheck_ArithmeticHexLiteralRejectedBeforeCheck(t *testing.T) {
	t.Parallel()

	// Digit-leading words in expression position must be valid
	// JSON numbers before the arithmetic checker gets involved.
	// This keeps source-text-decidable literal errors in
	// preflight rather than deferring them to runtime.
	src := "let r = 0x1a + 1"
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	_, err = syntax.Parse(tokens)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid numeric literal "0x1a"`)
}

func TestCheck_BreakInsideForeachIsClean(t *testing.T) {
	t.Parallel()

	src := "let xs = \"a\"\nforeach x in $xs { break }"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BreakOutsideForeachReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "break")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "'break' outside any foreach loop")
}

func TestCheck_ContinueOutsideForeachReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "continue")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "'continue' outside any foreach loop")
}

func TestCheck_UndefinedVarInMatchesEntryReported(t *testing.T) {
	t.Parallel()

	// Patterns on the right-hand side of a matches entry are
	// regular expressions; an undefined reference there must
	// surface from preflight the same way it does in any
	// other position, otherwise a typo in a fixture-style
	// assert silently survives the check pass.
	src := "let got = \"x\"\nassert $got matches { id: $expected_id }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "undefined variable: expected_id")
}

func TestCheck_AssertInsidePollRejected(t *testing.T) {
	t.Parallel()

	// `assert` inside a poll body is the fatal form that cannot
	// retry, so the checker steers the author towards
	// `retry unless ...` or `require ...`.
	src := "poll timeout 1s every 1ms { assert ok ls }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "assert is not valid inside poll")
}

func TestCheck_RequireInsidePollAccepted(t *testing.T) {
	t.Parallel()

	// `require` shares the AssertStmt node with `assert` (the
	// distinguishing field is IsRequire). The grammar deems
	// `require` fatal-immediately at every context including
	// inside a poll: the diagnostic that steers `assert`
	// towards `retry unless` or `require ...` must not also
	// reject `require` itself, otherwise the suggestion in the
	// diagnostic is unreachable from the source it points at.
	src := "poll timeout 1s every 1ms { require ok ls }"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BreakInsidePollReported(t *testing.T) {
	t.Parallel()

	// Poll is not a foreach loop for break/continue purposes.
	// A bare `break` inside the body is caught here so the user
	// does not need to reach runtime to discover the mismatch.
	src := "poll timeout 1s every 1ms { break }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "'break'")
}

func TestCheck_BreakInsideDefBodyResetsDepth(t *testing.T) {
	t.Parallel()

	// A def body resets the loop depth: a 'break' inside
	// the body but not inside a foreach within the body is
	// flagged even if the def is later called from inside a
	// foreach in the caller.
	src := "def f() { break }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "'break'")
}

func TestCheck_DefArity_CommandReported(t *testing.T) {
	t.Parallel()

	src := "hello one\n\ndef hello(a b) { print ok }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "hello: expected 2 argument(s), got 1")
	assert.Contains(t, issues[0].Msg, "def declared at 3:5")
}

func TestCheck_DefArity_BindReported(t *testing.T) {
	t.Parallel()

	src := "let x <- hello one\n\ndef hello(a b) { return 7 }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "hello: expected 2 argument(s), got 1")
}

func TestCheck_DefArity_DeferReported(t *testing.T) {
	t.Parallel()

	src := "defer hello one\n\ndef hello(a b) { print ok }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "hello: expected 2 argument(s), got 1")
}

func TestCheck_DefArity_BindCollectProducerReported(t *testing.T) {
	t.Parallel()

	src := "let xs <- foreach x in [1] { hello $x }\n\ndef hello(a b) { return 7 }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "hello: expected 2 argument(s), got 1")
}

func TestCheck_StartWithoutCommandReported(t *testing.T) {
	t.Parallel()

	// 'let p <- start' triggers both the arity check and
	// the job-leak check (the bound 'p' has no matching
	// wait or kill); assert the arity message is present
	// without constraining the total count, since both are
	// legitimate findings.
	issues := checkSource(t, "let p <- start")
	var msgs []string
	for _, i := range issues {
		msgs = append(msgs, i.Msg)
	}
	assert.Contains(t, strings.Join(msgs, " | "), "start: expected at least 1")
}

func TestCheck_UnknownShortBindHeadDoesNotSuggestDistantDef(t *testing.T) {
	t.Parallel()

	src := `def main() {
    return "value"
}
let route <- ip route get 198.51.100.2`
	issues := checkSource(t, src)
	assert.Empty(t, issues, "short external command names must not pick up distant def suggestions")
}

func TestCheck_WaitWithoutJobReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "wait")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "wait: expected at least 1")
}

func TestCheck_KillWithoutJobReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "kill")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "kill: expected at least 1")
}

func TestCheck_KillFlagsAreNotCountedAsArgs(t *testing.T) {
	t.Parallel()

	// 'kill --signal=USR1' has one arg textually but zero
	// non-flag args; the arity check must report it as
	// missing the $job target.
	issues := checkSource(t, "kill --signal=USR1")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "kill: expected at least 1")
}

func TestCheck_JobsTakesNoArgs(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "jobs extra")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "jobs: expected at most 0")
}

func TestCheck_ReapTakesNoArgs(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "reap extra")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "reap: expected at most 0")
}

func TestCheck_PureBuiltinU32LENegativeLiteralReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "let x = u32le (-1)")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `u32le: negative values are not representable`)
}

func TestCheck_PureBuiltinU32LEOverflowLiteralReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "let x = u32le 4294967296")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `u32le: value 4294967296 does not fit in 32 bits`)
}

func TestCheck_PureBuiltinU64LENegativeLiteralReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "let x = u64le (-1)")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `u64le: negative values are not representable`)
}

func TestCheck_PureBuiltinRangeNegativeLiteralReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "let xs = range (-1)")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `range: negative bound is not allowed`)
}

func TestCheck_PureBuiltinRangeTooLargeLiteralReported(t *testing.T) {
	t.Parallel()

	issues := checkSource(t, "let xs = range 2147483648")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `range: bound 2147483648 exceeds the maximum of 2147483647`)
}

func TestCheck_KillSignalKnownNamesClean(t *testing.T) {
	t.Parallel()

	// Each accepted spelling: bare, SIG-prefixed, and
	// lowercase. The static check mirrors the runtime's
	// acceptance.
	cases := []string{
		"guard p <- start sleep 60\nkill --signal=USR1 $p\nwait $p",
		"guard p <- start sleep 60\nkill --signal=SIGUSR1 $p\nwait $p",
		"guard p <- start sleep 60\nkill --signal=usr1 $p\nwait $p",
		"guard p <- start sleep 60\nkill --signal=TERM $p\nwait $p",
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			issues := checkSource(t, src)
			assert.Empty(t, issues, "src=%q", src)
		})
	}
}

func TestCheck_KillSignalUnknownReported(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 60\nkill --signal=BLAH $p\nwait $p"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `unknown signal "BLAH"`)
}

func TestCheck_KillGraceValidDurationsClean(t *testing.T) {
	t.Parallel()

	cases := []string{
		"guard p <- start sleep 60\nkill --grace=2s $p\nwait $p",
		"guard p <- start sleep 60\nkill --grace=500ms $p\nwait $p",
		"guard p <- start sleep 60\nkill --grace=0 $p\nwait $p",
		"guard p <- start sleep 60\nkill --grace=1m30s $p\nwait $p",
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			issues := checkSource(t, src)
			assert.Empty(t, issues, "src=%q", src)
		})
	}
}

func TestCheck_KillGraceMalformedReported(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 60\nkill --grace=banana $p\nwait $p"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "kill --grace:")
}

func TestCheck_KindFieldAccess_JobBadField(t *testing.T) {
	t.Parallel()

	// 'start' produces a Job whose only sealed field is 'pid'.
	// Any other field name on $p is statically detectable.
	src := "guard p <- start sleep 60\nprint $p.pidd\nkill $p"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "p has kind job")
	assert.Contains(t, issues[0].Msg, `"pidd"`)
	assert.Contains(t, issues[0].Msg, "valid: pid")
}

func TestCheck_KindFieldAccess_JobValidField(t *testing.T) {
	t.Parallel()

	src := "guard p <- start sleep 60\nprint $p.pid\nkill $p"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_KindFieldAccess_EnvelopeBadField(t *testing.T) {
	t.Parallel()

	// An external command (here 'which') binds through the
	// envelope path, so $bpftool.code is statically
	// detectable as a typo for $bpftool.exit_code.
	src := `let bpftool <- which bpftool
if $bpftool.code == 0 {
    print "found"
}`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "bpftool has kind result")
	assert.Contains(t, issues[0].Msg, `"code"`)
	assert.Contains(t, issues[0].Msg, "exit_code")
}

func TestCheck_KindFieldAccess_ScalarRejectsAnyField(t *testing.T) {
	t.Parallel()

	src := "let n = 42\nprint $n.value"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "n has kind scalar")
	assert.Contains(t, issues[0].Msg, "field access is not valid")
}

func TestCheck_KindFieldAccess_BoolRejectsAnyField(t *testing.T) {
	t.Parallel()

	src := "let flag = true\nprint $flag.value"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "flag has kind boolean")
}

func TestCheck_KindFieldAccess_UnknownKindIsPermissive(t *testing.T) {
	t.Parallel()

	// 'jq' returns semantics.OriginUnknown so any field access is
	// allowed. Same for 'bpfman' subcommands and 'file'
	// whose shapes are not yet enumerated. jq is a pure
	// builtin so the expression-position '=' form is the
	// only legal call shape.
	src := `let data = jq "." '{"x":1}'
print $data.anything.we.want`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_KindFieldAccess_LetCopyKindFromVarRef(t *testing.T) {
	t.Parallel()

	// 'let q = $p' copies p's inferred kind onto q, so
	// $q.field is checked the same way $p.field would be.
	src := "guard p <- start sleep 60\nlet q = $p\nprint $q.pidd\nkill $p"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "q has kind job")
}

func TestCheck_KindFieldAccess_LetBindOutcomeIsEnvelope(t *testing.T) {
	t.Parallel()

	src := `let r <- bpfman program get 42
if $r.exit_code == 0 {
    print $r.value
}`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_KindFieldAccess_DidYouMeanSuggestion(t *testing.T) {
	t.Parallel()

	// A near-miss field name produces a suggestion derived
	// through internal/strdist's nearest-string ranker.
	src := "guard p <- start sleep 60\nprint $p.pidd\nkill $p"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "did you mean")
	assert.Contains(t, issues[0].Msg, `"pid"`)
}

func TestCheck_KindFieldAccess_NestedKindPropagation(t *testing.T) {
	t.Parallel()

	// 'let q = $r.exit_code' inherits Scalar from the result's
	// exit_code field, so $q.field on q reports the scalar
	// constraint rather than silently passing.
	src := `let r <- exec ls
let q = $r.exit_code
print $q.field`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "q has kind scalar")
}

func TestCheck_ListExprShape_HomogeneousScalarElementsAreChecked(t *testing.T) {
	t.Parallel()

	src := `let xs = [1 2]
print $xs[0].field`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "xs has kind scalar")
}

func TestCheck_ListExprShape_HomogeneousProgramElementsAreChecked(t *testing.T) {
	t.Parallel()

	src := `guard p1 <- bpfman program get 1
guard p2 <- bpfman program get 2
let xs = [$p1 $p2]
print $xs[0].record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
	assert.Contains(t, issues[0].Msg, `"program_id"`)
}

func TestCheck_ListExprShape_DefReturnedHomogeneousListIsChecked(t *testing.T) {
	t.Parallel()

	src := `def progs() {
    guard p1 <- bpfman program get 1
    guard p2 <- bpfman program get 2
    return [$p1 $p2]
}
guard xs <- progs
print $xs[0].record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
	assert.Contains(t, issues[0].Msg, `"program_id"`)
}

func TestCheck_ListExprShape_MixedElementsStayOpen(t *testing.T) {
	t.Parallel()

	src := `guard p <- bpfman program get 1
let r <- exec true
let xs = [$p $r]
print $xs[0].record.program_idd`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_RecordExprShape_FieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `guard p <- bpfman program get 42
let r = record {
    prog: $p
}
print $r.prgo`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "r has no field")
	assert.Contains(t, issues[0].Msg, `"prgo"`)
	assert.Contains(t, issues[0].Msg, `"prog"`)
}

func TestCheck_RecordExprShape_NestedTypedFieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `guard p <- bpfman program get 42
let r = record {
    prog: $p
}
print $r.prog.record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
	assert.Contains(t, issues[0].Msg, `"program_id"`)
}

func TestCheck_RecordExprShape_DefReturnedRecordIsChecked(t *testing.T) {
	t.Parallel()

	src := `def loaded() {
    guard p <- bpfman program get 42
    return record {
        prog: $p
    }
}
guard r <- loaded
print $r.prog.record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
	assert.Contains(t, issues[0].Msg, `"program_id"`)
}

func TestCheck_BPFManProgramListShape_TopLevelFieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `guard listed <- bpfman program list -o json
print $listed.progams[0].record.program_id`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "listed has no field")
	assert.Contains(t, issues[0].Msg, `"progams"`)
	assert.Contains(t, issues[0].Msg, `"programs"`)
}

func TestCheck_BPFManProgramListShape_ValidProgramFieldIsClean(t *testing.T) {
	t.Parallel()

	src := `guard listed <- bpfman program list -o json
print $listed.programs[0].record.program_id`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BPFManProgramListShape_ProgramFieldsAreChecked(t *testing.T) {
	t.Parallel()

	src := `guard listed <- bpfman program list -o json
print $listed.programs[0].record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
	assert.Contains(t, issues[0].Msg, `"program_id"`)
}

func TestCheck_BPFManProgramListEntryShape_TopLevelFieldsAreClean(t *testing.T) {
	t.Parallel()

	// The list entry exposes the summary columns as top-level fields,
	// plus the nullable kernel observation. All are part of the sealed
	// entry shape.
	src := `guard listed <- bpfman program list -o json
print $listed.programs[0].managed
print $listed.programs[0].type
print $listed.programs[0].function_name
print $listed.programs[0].application
print $listed.programs[0].kernel.id`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BPFManProgramListEntryShape_TopLevelFieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `guard listed <- bpfman program list -o json
print $listed.programs[0].managd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"managd"`)
	assert.Contains(t, issues[0].Msg, `"managed"`)
}

func TestCheck_BPFManProgramLoadResultShape_ValidProgramFieldIsClean(t *testing.T) {
	t.Parallel()

	src := `guard loaded <- bpfman program load xdp file /tmp/x.o
print $loaded.programs[0].record.program_id`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BPFManProgramLoadResultShape_TopLevelFieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `guard loaded <- bpfman program load xdp file /tmp/x.o
print $loaded.progams[0].record.program_id`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "loaded has no field")
	assert.Contains(t, issues[0].Msg, `"progams"`)
	assert.Contains(t, issues[0].Msg, `"programs"`)
}

func TestCheck_BPFManLinkListResultShape_TopLevelFieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `guard listed <- bpfman link list -o json
print $listed.lniks[0].id`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "listed has no field")
	assert.Contains(t, issues[0].Msg, `"lniks"`)
	assert.Contains(t, issues[0].Msg, `"links"`)
}

func TestCheck_BPFManLinkListResultShape_ValidLinkFieldIsClean(t *testing.T) {
	t.Parallel()

	src := `guard listed <- bpfman link list -o json
print $listed.links[0].id
print $listed.links[0].kind`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_BPFManLinkListResultShape_LinkFieldsAreChecked(t *testing.T) {
	t.Parallel()

	src := `guard listed <- bpfman link list -o json
print $listed.links[0].kindd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"kindd"`)
	assert.Contains(t, issues[0].Msg, `"kind"`)
}

func TestCheck_RecordExprShape_ParamFieldStaysOpen(t *testing.T) {
	t.Parallel()

	src := `def box(x) {
    return record {
        item: $x
    }
}
guard p <- bpfman program get 42
guard r <- box $p
print $r.item.record.program_idd`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DefReturnShape_ProgramFieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `def get_prog() {
    guard p <- bpfman program get 42
    return $p
}
guard p <- get_prog
print $p.record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "p.record has no field")
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
	assert.Contains(t, issues[0].Msg, `"program_id"`)
}

func TestCheck_DefReturnShape_ComposedHelperUsesCalleeShape(t *testing.T) {
	t.Parallel()

	src := `def inner() {
    guard p <- bpfman program get 42
    return $p
}
def outer() {
    guard p <- inner
    return $p
}
guard p <- outer
print $p.record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "p.record has no field")
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
}

func TestCheck_DefReturnShape_ForwardHelperUsesCalleeShape(t *testing.T) {
	t.Parallel()

	src := `def outer() {
    guard p <- inner
    return $p
}
def inner() {
    guard p <- bpfman program get 42
    return $p
}
guard p <- outer
print $p.record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "p.record has no field")
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
}

func TestCheck_DefReturnShape_ParamPassthroughStaysOpen(t *testing.T) {
	t.Parallel()

	src := `def id(x) {
    return $x
}
guard p <- bpfman program get 42
guard q <- id $p
print $q.record.program_idd`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DefReturnShape_DoesNotCaptureCallerFrame(t *testing.T) {
	t.Parallel()

	src := `guard x <- bpfman program get 42
def get_x() {
    return $x
}
guard q <- get_x
print $q.record.program_idd`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DefReturnShape_ScalarFieldAccessRejected(t *testing.T) {
	t.Parallel()

	src := `def seven() {
    return 7
}
guard n <- seven
print $n.field`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "n has kind scalar")
}

func TestCheck_DefReturnShape_FullIfElseSameShapeRejected(t *testing.T) {
	t.Parallel()

	src := `def choose(flag) {
    if $flag {
        guard p <- bpfman program get 1
        return $p
    } else {
        guard p <- bpfman program get 2
        return $p
    }
}
guard p <- choose true
print $p.record.program_idd`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "p.record has no field")
	assert.Contains(t, issues[0].Msg, `"program_idd"`)
}

func TestCheck_DefReturnShape_PartialIfReturnStaysOpen(t *testing.T) {
	t.Parallel()

	src := `def choose(flag) {
    if $flag {
        guard p <- bpfman program get 1
        return $p
    }
}
guard p <- choose true
print $p.record.program_idd`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DefReturnShape_MixedReturnShapesStayOpen(t *testing.T) {
	t.Parallel()

	src := `def choose(flag) {
    if $flag {
        guard p <- bpfman program get 1
        return $p
    } else {
        let r <- exec true
        return $r
    }
}
guard p <- choose true
print $p.record.program_idd`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DefReturnShape_RecursiveReturnStaysOpen(t *testing.T) {
	t.Parallel()

	src := `def self() {
    guard p <- self
    return $p
}
guard p <- self
print $p.record.program_idd`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_DefReturnShape_NoReturnDefStillEnvelope(t *testing.T) {
	t.Parallel()

	src := `def warmup() {
    exec true
}
let r <- warmup
print $r.exit_code`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_KindFieldAccess_UnknownBindIsPermissive(t *testing.T) {
	t.Parallel()

	// jq returns a value the static checker has no shape for,
	// so $data.anything.deep is permitted: the alternative is
	// false positives on every dynamic structure. jq is a pure
	// builtin invoked in expression position.
	src := `let data = jq "." '{"x":{"y":1}}'
print $data.x.y.z`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_ForEachScopeRestoresShape(t *testing.T) {
	t.Parallel()

	// 'let x = 5' defines x as a numeric scalar. A foreach
	// that reuses 'x' as the loop variable must not leak the
	// loop's per-iteration shape past the body. After the
	// loop, the original 'let' shape stays in effect.
	src := `let x = 5
foreach x in $list {
    print $x
}
print "${4 * $x}"`
	issues := checkSource(t, src)
	// $list is undefined -> one issue. The trailing "${4 * $x}"
	// arithmetic must not flag $x as non-numeric: x's outer
	// shape (Scalar with literal RHS "5") was restored.
	for _, iss := range issues {
		assert.NotContains(t, iss.Msg, "x is", "foreach must restore x's outer shape on exit")
	}
}

// TestCheck_DefParamAnnotationLiterals pins the static half of the
// annotation policy: a literal argument to an annotated parameter
// that can never bind successfully is a check-time issue, in both
// command position and thread position. Variables stay
// runtime-checked, so they produce no issue here even when their
// value would mismatch.
func TestCheck_DefParamAnnotationLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		src       string
		wantIssue string
	}{
		{
			name:      "bad number literal in command position",
			src:       "def c(want: number) {\n    assert $want == 5\n}\nc abc",
			wantIssue: `def c: parameter "want": expected number, got "abc"`,
		},
		{
			name:      "quoted literal where number declared",
			src:       "def c(want: number) {\n    assert $want == 5\n}\nc \"5\"",
			wantIssue: "quoting asserts string",
		},
		{
			name:      "bad bool literal",
			src:       "def b(flag: bool) {\n    print $flag\n}\nb yes",
			wantIssue: `def b: parameter "flag": expected bool, got "yes"`,
		},
		{
			name:      "NaN rejected for number",
			src:       "def c(want: number) {\n    print $want\n}\nc NaN",
			wantIssue: `def c: parameter "want": expected number, got "NaN"`,
		},
		{
			name:      "Inf rejected for number",
			src:       "def c(want: number) {\n    print $want\n}\nc Inf",
			wantIssue: `def c: parameter "want": expected number, got "Inf"`,
		},
		{
			name: "scientific notation accepted",
			src:  "def c(want: number) {\n    print $want\n}\nc 1e3",
		},
		{
			name: "valid literals produce no issue",
			src:  "def c(want: number ok: bool name: string) {\n    print $want\n}\nc 5 true hello",
		},
		{
			name: "variables stay runtime-checked",
			src:  "def c(want: number) {\n    print $want\n}\nlet s = \"5\"\nc $s",
		},
		{
			name: "unannotated params accept any literal",
			src:  "def c(want) {\n    print $want\n}\nc abc",
		},
		{
			name:      "bad literal in thread position",
			src:       "def f(x y: number) {\n    print $y\n}\nlet v = 1\nprint ($v |> f abc)",
			wantIssue: `def f: parameter "y": expected number, got "abc"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			issues := checkSource(t, tt.src)
			if tt.wantIssue == "" {
				assert.Empty(t, issues)
				return
			}
			require.NotEmpty(t, issues, "expected an issue containing %q", tt.wantIssue)
			found := false
			for _, is := range issues {
				if strings.Contains(is.Msg, tt.wantIssue) {
					found = true
				}
			}
			assert.True(t, found, "no issue contained %q; got %v", tt.wantIssue, issues)
		})
	}
}

// TestCheck_NetnsVethPair_* pin the static half of the isolated
// netns-veth-pair topology: the bind shape, the nested endpoint
// record shapes, and the net exec / net release argument rules,
// mirroring the runtime messages exactly.

func TestCheck_NetnsVethPairShape_ValidPathsAreClean(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
print $pair.a.ns
print $pair.a.link
print $pair.a.addr
print $pair.a.ifindex
print $pair.a.nsid
print $pair.b.ns
print $pair.b.link
print $pair.b.addr
print $pair.b.ifindex
print $pair.b.nsid`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_NetnsVethPairShape_TopLevelFieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
print $pair.host_link`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "pair has kind netns-veth-pair")
	assert.Contains(t, issues[0].Msg, `"host_link"`)
	assert.Contains(t, issues[0].Msg, "valid: a, b")
}

func TestCheck_NetnsVethPairShape_EndpointFieldTypoRejected(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
print $pair.a.nss`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"nss"`)
	assert.Contains(t, issues[0].Msg, `"ns"`)
}

func TestCheck_NetExec_BareIsolatedPairRejected(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
net exec $pair ping -c 1 198.51.100.2`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "net exec: netns-veth-pair has two endpoints; use $pair.a or $pair.b")
}

func TestCheck_NetExec_BareIsolatedPairRejectedInBindPosition(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
guard e <- net exec $pair ping -c 1 198.51.100.2`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "net exec: netns-veth-pair has two endpoints; use $pair.a or $pair.b")
}

func TestCheck_NetStart_BareIsolatedPairRejected(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
guard j <- net start $pair sleep 60`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "net start: netns-veth-pair has two endpoints; use $pair.a or $pair.b")
}

func TestCheck_NetExec_EndpointAccepted(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
net exec $pair.a ping -c 1 $pair.b.addr
net exec $pair.b ping -c 1 $pair.a.addr`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_NetExec_HostEndPairStillAccepted(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net veth-pair
net exec $pair ping -c 1 $pair.host_addr`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_NetRelease_EndpointRejected(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
net release $pair.a`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "net release: endpoint belongs to a netns-veth-pair; release the pair")
}

func TestCheck_NetRelease_IsolatedPairAccepted(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
net release $pair`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_NetRelease_InDeferAccepted(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
defer net release $pair`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_NetRelease_EndpointInDeferRejected(t *testing.T) {
	t.Parallel()

	src := `guard pair <- net netns-veth-pair
defer net release $pair.a`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "net release: endpoint belongs to a netns-veth-pair; release the pair")
}

func TestCheck_NetExec_WrongKindRejected(t *testing.T) {
	t.Parallel()

	src := `guard j <- start sleep 60
net exec $j ping -c 1 198.51.100.2
kill $j`
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "expected a $pair or endpoint argument, got a job value")
}

func TestCheck_NetExec_UnknownKindStaysSilent(t *testing.T) {
	t.Parallel()

	src := `def probe(x) {
    net exec $x ping -c 1 198.51.100.2
}
probe abc`
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}
