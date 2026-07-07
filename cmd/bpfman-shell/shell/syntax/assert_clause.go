package syntax

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"

// AssertClause is the syntax-owned body that follows an assert or
// require keyword. The parser always produces one AssertStmt; the
// clause discriminator records which assertion shape the user
// wrote without routing some forms through CommandStmt.
type AssertClause interface {
	assertClauseNode()
}

// AssertExprClause is the steady-state assertion form: any
// expression whose boolean value decides pass/fail.
type AssertExprClause struct {
	// Expr is the expression whose boolean value decides whether the
	// assertion passes.
	Expr Expr
}

// AssertCommandClause holds the command-status assertion forms:
// `assert ok CMD...` and `assert fail CMD...`. The clause keeps the
// command-style argument payload in expression form rather than
// embedding a statement node.
type AssertCommandClause struct {
	// Head is the verb that selected this form, "ok" (pass when the
	// command succeeds) or "fail" (pass when it fails).
	Head string

	// HeadSpan is the source extent of the Head token, used to frame
	// diagnostics at the verb.
	HeadSpan source.Span

	// Args is the command and its arguments in expression form; the
	// first element names the command to run.
	Args []Expr

	// Negate records a leading `not` (for example `assert not ok
	// CMD`) and inverts the verb's pass condition.
	Negate bool
}

func (*AssertExprClause) assertClauseNode()    {}
func (*AssertCommandClause) assertClauseNode() {}
