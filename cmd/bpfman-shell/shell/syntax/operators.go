package syntax

// isBinaryOp reports whether s is a recognised binary operator.
func isBinaryOp(s string) bool {
	switch s {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// isUnaryPred reports whether s is a recognised unary predicate in
// the expression grammar.
func isUnaryPred(s string) bool {
	return s == "not-empty"
}
