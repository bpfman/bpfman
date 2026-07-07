package driver

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/check"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/lower"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// runCheckInput wraps CheckInput over a string source so tests
// can focus on which errors are reported and on which line.
func runCheckInput(t *testing.T, src string) (bool, string) {
	t.Helper()
	r := NewScannerReader(strings.NewReader(src), nil)
	var buf bytes.Buffer
	hadErrors := CheckInput(r, &buf, "test.bpfman")
	return hadErrors, buf.String()
}

func TestShellCheck_CleanInput(t *testing.T) {
	t.Parallel()

	// --check runs static analysis after parsing; every
	// $-reference must resolve to a previously-defined name,
	// matching how go vet and pylint catch undefined-name
	// typos. The 'if $x > 0' case defines $x first.
	clean := []string{
		"print ok",
		"let x = 1\nshow program",
		"let x = 1\nif $x > 0 {\n  bpfman program list\n}",
		"let y <- bpfman program list",
		"# a comment only",
		"",
	}
	for _, src := range clean {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			hadErrors, errOut := runCheckInput(t, src)
			assert.False(t, hadErrors, "unexpected errors: %s", errOut)
			assert.Empty(t, errOut)
		})
	}
}

func TestShellCheck_BrokenSnippets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		input       string
		wantContain string
	}{
		{
			name:        "second assign in let RHS",
			input:       "let x = 1 = 2",
			wantContain: "unexpected '='",
		},
		{
			name:        "bare ident-equals",
			input:       "prog = load",
			wantContain: "unexpected '='",
		},
		{
			name:        "if missing brace",
			input:       "if $x > 0 bpfman",
			wantContain: "expected '{'",
		},
		{
			name:        "unterminated quote",
			input:       `echo "hello`,
			wantContain: "unterminated",
		},
		{
			name:        "malformed varref",
			input:       "echo $prog.",
			wantContain: "expected identifier",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hadErrors, errOut := runCheckInput(t, tc.input)
			assert.True(t, hadErrors, "expected errors; got clean output")
			assert.Contains(t, errOut, tc.wantContain)
			assert.Contains(t, errOut, "test.bpfman:")
		})
	}
}

func TestShellCheck_UnterminatedBlockAtEOF(t *testing.T) {
	t.Parallel()

	hadErrors, errOut := runCheckInput(t, "if $x > 0 {\n  let y = 1")
	assert.True(t, hadErrors)
	assert.Contains(t, errOut, "unterminated block")
}

func TestShellCheck_ReportsMultipleStaticIssues(t *testing.T) {
	t.Parallel()

	// Static analysis (Check) accumulates issues and reports
	// every undefined reference, not just the first. Parse
	// errors still bail on the first because the parser
	// cannot meaningfully continue past a syntax error.
	src := "print $a\nprint $b\n"
	hadErrors, errOut := runCheckInput(t, src)
	assert.True(t, hadErrors)
	assert.Contains(t, errOut, "undefined variable: a")
	assert.Contains(t, errOut, "undefined variable: b")
}

func TestShellCheck_LinePrefixTracksParserPosition(t *testing.T) {
	t.Parallel()

	// Parser errors are typed *shell.SyntaxError carrying a
	// Span; the renderer cites the Span's start position via
	// the "--> file:line:col" header. A 'print ok' on line 1, a
	// blank line 2, then the offending 'let x = 1 = 2' on
	// line 3 should produce a frame whose header cites line
	// 3, not line 1.
	src := "print ok\n\nlet x = 1 = 2\n"
	hadErrors, errOut := runCheckInput(t, src)
	assert.True(t, hadErrors)
	assert.Contains(t, errOut, "test.bpfman:3:")
}

// TestShellCheck_SyntaxGallery is a smoke test that the shipped
// contrib/emacs/syntax-gallery.bpfman example parses cleanly under
// CheckInput. The gallery is the reference source for the shell's
// surface syntax; if this regresses the refactor has lost
// coverage somewhere.
func TestShellCheck_SyntaxGallery(t *testing.T) {
	t.Parallel()

	path, err := filepath.Abs("../../../contrib/emacs/syntax-gallery.bpfman")
	require.NoError(t, err)
	f, err := OpenScriptReader(path)
	require.NoError(t, err)
	defer f.Close()
	var buf bytes.Buffer
	hadErrors := CheckInput(f, &buf, path)
	assert.False(t, hadErrors, "syntax gallery reports errors:\n%s", buf.String())
}

func TestShellCheck_ImportLibraryRejectsTopLevelLet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.bpfman")
	err := os.WriteFile(lib, []byte(strings.Join([]string{
		"let x = 1",
		"def show() { print $x }",
	}, "\n")), 0o644)
	require.NoError(t, err)

	src := "import " + lib + "\nshow\n"
	hadErrors, errOut := runCheckInput(t, src)
	assert.True(t, hadErrors)
	assert.Contains(t, errOut, lib+":1:")
	assert.Contains(t, errOut, "imported files may contain only top-level def statements")
}

func TestCheckInput_ImportRelativeToScriptDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	lib := filepath.Join(dir, "lib.bpfman")
	main := filepath.Join(sub, "main.bpfman")
	require.NoError(t, os.WriteFile(lib, []byte("def loaded() { print loaded-lib }\n"), 0o644))
	require.NoError(t, os.WriteFile(main, []byte("import ../lib.bpfman\nloaded\n"), 0o644))

	f, err := OpenScriptReader(main)
	require.NoError(t, err)
	defer f.Close()

	var buf bytes.Buffer
	hadErrors := CheckInput(f, &buf, main)
	assert.False(t, hadErrors, "unexpected errors: %s", buf.String())
	assert.Empty(t, buf.String())
}

func TestParseAndExpand_ImportSplicesDefsIntoLoweredProgram(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	lib := filepath.Join(dir, "lib.bpfman")
	main := filepath.Join(sub, "main.bpfman")
	require.NoError(t, os.WriteFile(lib, []byte("def loaded() { print loaded-lib }\n"), 0o644))

	prog, err := ParseAndExpand(main, "import ../lib.bpfman\nloaded\n")
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 2)
	_, ok := prog.Stmts[0].(*syntax.DefStmt)
	require.True(t, ok, "first statement should be imported def")

	lp, err := lower.Lower(prog)
	require.NoError(t, err)
	var buf strings.Builder
	require.NoError(t, ir.Dump(&buf, lp))
	out := buf.String()
	assert.Contains(t, out, "def loaded() entry=")
	assert.NotContains(t, out, "RegisterDef name=loaded")
	assert.NotContains(t, out, "import")
}

func TestParseImportProgram_SeesEarlierImportedDefs(t *testing.T) {
	t.Parallel()

	mainProg, err := parseProgram("main.bpfman", "import ./lib.bpfman\nimport ./helpers.bpfman\n")
	require.NoError(t, err)
	visibleDefs := topLevelDefInfo(mainProg.Stmts)

	libProg, err := parseImportProgram("lib.bpfman", "def inner(x) { return $x }\n", visibleDefs)
	require.NoError(t, err)
	recordTopLevelDefInfo(visibleDefs, libProg.Stmts)

	_, err = parseImportProgram("helpers.bpfman", "def outer(x) { let v <- inner $x\n return $v }\n", visibleDefs)
	require.NoError(t, err, "later imported helper should see defs from earlier imports")
}

func TestParseImportProgram_RejectsDuplicateVisibleDef(t *testing.T) {
	t.Parallel()

	_, err := parseImportProgram("lib.bpfman", "def inner() { print shadow }\n", map[string]check.DefStaticInfo{
		"inner": {
			Arity:     0,
			DeclPos:   source.Pos{File: "main.bpfman", Line: 1, Col: 1},
			HasReturn: false,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate top-level def "inner"`)
	assert.Contains(t, err.Error(), "main.bpfman:1:1")
}

func TestParseAndExpand_ImportedHelperSeesEarlierImportedDef(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.bpfman")
	helpers := filepath.Join(dir, "helpers.bpfman")
	main := filepath.Join(dir, "main.bpfman")
	require.NoError(t, os.WriteFile(lib, []byte("def inner(x) { return $x }\n"), 0o644))
	require.NoError(t, os.WriteFile(helpers, []byte("def outer(x) {\n  let v <- inner $x\n  return $v\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(main, []byte("import ./lib.bpfman\nimport ./helpers.bpfman\nlet v <- outer hi\n"), 0o644))

	prog, err := ParseAndExpand(main, "import ./lib.bpfman\nimport ./helpers.bpfman\nlet v <- outer hi\n")
	require.NoError(t, err, "ParseAndExpand should preserve earlier imported defs for later imports")

	issues := check.Check(prog)
	assert.Empty(t, issues, "expanded program should not report unknown imported def references")
}

func TestShellCheck_RenderedDiagnostic_ArithmeticOperandSpan(t *testing.T) {
	t.Parallel()

	hadErrors, errOut := runCheckInput(t, `let x = "abc" + 1`)
	assert.True(t, hadErrors)
	assert.Contains(t, errOut, "test.bpfman:1:9")
	assert.Contains(t, errOut, `let x = "abc" + 1`)
	assert.Contains(t, errOut, `operand "abc" is not numeric`)
	assert.Contains(t, errOut, "^^^^^")
}

func TestShellCheck_RenderedDiagnostic_ComparisonOperandSpan(t *testing.T) {
	t.Parallel()

	hadErrors, errOut := runCheckInput(t, "let ok = true == 1")
	assert.True(t, hadErrors)
	assert.Contains(t, errOut, "test.bpfman:1:10")
	assert.Contains(t, errOut, "let ok = true == 1")
	assert.Contains(t, errOut, "cannot compare boolean to scalar")
	assert.Contains(t, errOut, "^^^^^^^^^")
}
