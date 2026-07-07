package runtime

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"

// AssertFailure is the typed-error form of an assertion whose condition
// did not hold. It is test-only: the runtime tests construct it directly
// when they need a concrete assertion-failure value. Production assert
// handling counts failures via Session.RecordAssertFailure.
type AssertFailure struct {
	source.Span

	// Expr is the rendered assertion expression, or empty when the
	// caller supplied no expression text.
	Expr string
}

// Error renders the failure as "assert failed", appending the
// expression text when Expr is non-empty.
func (e *AssertFailure) Error() string {
	if e.Expr == "" {
		return "assert failed"
	}
	return "assert failed: " + e.Expr
}
