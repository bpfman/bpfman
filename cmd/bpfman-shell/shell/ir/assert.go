package ir

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"

// AssertClause is the lowered-runtime mirror of syntax.AssertClause.
// One Assert instruction carries one clause discriminator; the
// runtime keeps a single assertion lane and switches inside it.
type AssertClause interface {
	// assertClauseIRNode is the unexported marker that seals the
	// AssertClause sum; only types in this package can implement it.
	assertClauseIRNode()
}

// AssertExprClause is the expression form of an assertion, lowered
// from a syntax.AssertExprClause: "assert EXPR" / "require EXPR"
// asserts that the boolean expression holds.
type AssertExprClause struct {
	// Expr is the boolean expression the assertion evaluates.
	Expr Expr
}

// AssertCommandClause is the command form of an assertion, lowered
// from a syntax.AssertCommandClause: "assert HEAD ARGS..." runs the
// command and asserts on its outcome.
type AssertCommandClause struct {
	// Head is the command head to run.
	Head string

	// HeadSpan is the source span of the head, used to position
	// diagnostics.
	HeadSpan source.Span

	// Args are the lowered argument expressions passed to the
	// command.
	Args []Expr

	// Negate inverts the success condition (the "not" form): the
	// assertion holds iff the command fails.
	Negate bool
}

func (*AssertExprClause) assertClauseIRNode()    {}
func (*AssertCommandClause) assertClauseIRNode() {}
