package syntax

import (
	"fmt"
	"strings"
)

// Test-only AST-based source renderers. Production formats through
// FormatSource, which preserves comments and source layout from the
// original text; the renderers below work from the AST alone and are used
// by the formatter and assert-clause tests.

// FormatProgramSource renders prog as canonical bpfman-shell source from
// the AST alone, so comments and redundant grouping parens are not
// preserved.
func FormatProgramSource(prog *Program) string {
	if prog == nil || len(prog.Stmts) == 0 {
		return ""
	}
	f := sourceFormatter{}
	f.writeStmts(prog.Stmts, 0)
	return f.b.String()
}

// FormatAssertClauseSource renders one assertion clause in the shell's
// compact source-like form.
func FormatAssertClauseSource(clause AssertClause) string {
	return dumpAssertClauseSource(clause)
}

func dumpAssertClauseSource(c AssertClause) string {
	if c == nil {
		return "<nil-assert-clause>"
	}
	var b strings.Builder
	writeAssertClauseSource(&b, c)
	return b.String()
}

func writeAssertClauseSource(b *strings.Builder, c AssertClause) {
	switch v := c.(type) {
	case *AssertExprClause:
		writeExprSource(b, v.Expr)
	case *AssertCommandClause:
		if v.Negate {
			b.WriteString("not ")
		}
		b.WriteString(v.Head)
		for _, a := range v.Args {
			b.WriteByte(' ')
			writeExprSource(b, a)
		}
	default:
		t := fmt.Sprintf("%T", c)
		if i := strings.LastIndex(t, "."); i >= 0 {
			t = t[i+1:]
		}
		fmt.Fprintf(b, "<%s>", t)
	}
}
