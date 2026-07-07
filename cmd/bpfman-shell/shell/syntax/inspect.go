// AST traversal for diagnostics, linters, refactor tools, and
// any other consumer that wants to walk a parsed Program once
// and observe its nodes. The shape mirrors go/ast.Inspect:
// pre-order traversal with a callback that returns false to
// skip the current subtree. The callback signature accepts a
// nil to mark the end of a subtree, again matching Go's
// convention so callers familiar with go/ast can transfer
// directly.
//
// Adding a tool on top is now a five-line closure: the
// dumper, an unwaited-job linter, a 'find every kill targeting
// $p' search, a rename pass -- all the same shape, all
// reusing this one walker rather than each writing their own
// type-switch over every Stmt and Expr variant.

package syntax

// Node is the marker interface for anything Inspect can walk:
// every Stmt, every Expr, plus *Program at the root and the
// IfBranch struct that pairs an elif condition with its body.
// All concrete AST types implement it via the existing
// stmtNode / exprNode markers, so adding a new variant
// surfaces in Inspect automatically.
type Node interface {
	astNode()
}

// astNode is the unifying marker. Stmts get it for free
// because every stmtNode is also astNode-eligible; same for
// Exprs. The explicit method on Program and IfBranch lets the
// walker descend into them as full nodes rather than struct
// peeking, matching go/ast.File and go/ast.CaseClause.
func (*Program) astNode()  {}
func (*IfBranch) astNode() {}

// Stmts are Nodes via their stmtNode method. The blank-field
// trick keeps Stmt's existing sealed-sum-type contract intact:
// any *xxxStmt that has stmtNode() also satisfies astNode()
// because both methods sit on the same set of pointer types
// declared in parse.go.
func (*LetStmt) astNode()            {}
func (*LetDestructureStmt) astNode() {}
func (*BindStmt) astNode()           {}
func (*DeferStmt) astNode()          {}
func (*IfStmt) astNode()             {}
func (*CommandStmt) astNode()        {}
func (*ExprStmt) astNode()           {}
func (*ForEachStmt) astNode()        {}
func (*BreakStmt) astNode()          {}
func (*ContinueStmt) astNode()       {}
func (*PollStmt) astNode()           {}
func (*RetryStmt) astNode()          {}
func (*DefStmt) astNode()            {}
func (*ReturnStmt) astNode()         {}
func (*AssertStmt) astNode()         {}

// Exprs are Nodes via their exprNode method. Same shape as
// Stmts above; one line per concrete type so a future variant
// will produce a clean compile error if the author forgets to
// add it here.
func (*LiteralExpr) astNode()      {}
func (*VarRefExpr) astNode()       {}
func (*AdapterExpr) astNode()      {}
func (*InterpStringExpr) astNode() {}
func (*BinaryExpr) astNode()       {}
func (*UnaryExpr) astNode()        {}
func (*ThreadExpr) astNode()       {}
func (*LogicalExpr) astNode()      {}
func (*NotExpr) astNode()          {}
func (*NegateExpr) astNode()       {}
func (*PureCallExpr) astNode()     {}
func (*MatchesExpr) astNode()      {}
func (*ListExpr) astNode()         {}
func (*RecordExpr) astNode()       {}
func (*MatchesBlockExpr) astNode() {}

// Inspect traverses node in pre-order, calling f on every
// Node it visits. f returns true to descend into the node's
// children, false to skip the subtree. After all children of
// a node have been visited, f is called once more with a nil
// node so consumers that need post-order hooks can detect
// subtree end. This is the same contract as go/ast.Inspect.
//
// Typical usage:
//
//	shell.Inspect(prog, func(n shell.Node) bool {
//	    if v, ok := n.(*shell.VarRefExpr); ok {
//	        fmt.Println(v.Name)
//	    }
//	    return true
//	})
//
// Inspect is safe to call concurrently on the same tree from
// different goroutines because it does not mutate node state.
func Inspect(node Node, f func(Node) bool) {
	walk(node, f)
}

// walk implements the recursive descent. Each case enumerates
// the node's child positions in source order; new variants
// added to parse.go or expr.go must add a case here or the
// traversal will silently stop at the new node. The
// per-variant code is short enough that this is not a real
// burden: most variants have one or two child slots.
func walk(node Node, f func(Node) bool) {
	if node == nil {
		return
	}
	if !f(node) {
		return
	}
	switch n := node.(type) {
	case *Program:
		for _, s := range n.Stmts {
			walk(s, f)
		}
	case *IfBranch:
		walk(n.Cond, f)
		for _, s := range n.Body {
			walk(s, f)
		}

	// --- Statements ---
	case *LetStmt:
		walk(n.RHS, f)
	case *LetDestructureStmt:
		walk(n.RHS, f)
	case *BindStmt:
		if n.Cmd != nil {
			walk(n.Cmd, f)
		}
		if n.Collect != nil {
			walk(n.Collect, f)
		}
	case *DeferStmt:
		if n.Cmd != nil {
			walk(n.Cmd, f)
		}
	case *IfStmt:
		walk(n.Cond, f)
		for _, s := range n.Then {
			walk(s, f)
		}
		for i := range n.Elifs {
			walk(&n.Elifs[i], f)
		}
		for _, s := range n.Else {
			walk(s, f)
		}
	case *CommandStmt:
		for _, a := range n.Args {
			walk(a, f)
		}
	case *ExprStmt:
		walk(n.Expr, f)
	case *ForEachStmt:
		walk(n.List, f)
		for _, s := range n.Body {
			walk(s, f)
		}
	case *BreakStmt, *ContinueStmt:
		// Leaf statements with no children.
	case *PollStmt:
		for _, s := range n.Body {
			walk(s, f)
		}
	case *RetryStmt:
		if n.Message != nil {
			walk(n.Message, f)
		}
		if n.Unless != nil {
			walk(n.Unless, f)
		}
	case *DefStmt:
		for _, s := range n.Body {
			walk(s, f)
		}
	case *ReturnStmt:
		if n.Expr != nil {
			walk(n.Expr, f)
		}
	case *AssertStmt:
		switch clause := n.Clause.(type) {
		case *AssertExprClause:
			walk(clause.Expr, f)
		case *AssertCommandClause:
			for _, a := range clause.Args {
				walk(a, f)
			}
		}

	// --- Expressions ---
	case *LiteralExpr, *VarRefExpr, *AdapterExpr:
		// Leaf expressions: no children to visit.
	case *InterpStringExpr:
		for _, seg := range n.Segments {
			if seg.Expr != nil {
				walk(seg.Expr, f)
			}
		}
	case *BinaryExpr:
		walk(n.Left, f)
		walk(n.Right, f)
	case *UnaryExpr:
		walk(n.Operand, f)
	case *ThreadExpr:
		walk(n.LHS, f)
		for _, a := range n.Args {
			walk(a, f)
		}
	case *LogicalExpr:
		walk(n.Left, f)
		walk(n.Right, f)
	case *NotExpr:
		walk(n.Operand, f)
	case *NegateExpr:
		walk(n.Operand, f)
	case *PureCallExpr:
		for _, a := range n.Args {
			walk(a, f)
		}
	case *MatchesExpr:
		walk(n.Target, f)
		if n.Block != nil {
			walk(n.Block, f)
		}
	case *ListExpr:
		for _, elem := range n.Elems {
			walk(elem, f)
		}
	case *RecordExpr:
		for _, field := range n.Fields {
			walk(field.Expr, f)
		}
	case *MatchesBlockExpr:
		// Each entry's Pattern is a regular Expr and SubBlock
		// is a nested MatchesBlockExpr; both are children that
		// must be visited so generic walkers (notably the
		// checker's undefined-variable pass via Inspect) see
		// references buried inside a matches block. Entries
		// without a Pattern (predicate-only or sub-block-only
		// forms) just skip the corresponding walk call.
		for i := range n.Entries {
			ent := &n.Entries[i]
			if ent.Pattern != nil {
				walk(ent.Pattern, f)
			}
			if ent.SubBlock != nil {
				walk(ent.SubBlock, f)
			}
		}
	}
	// Post-visit signal: nil tells callers the subtree has
	// finished. Mirrors go/ast.Inspect.
	f(nil)
}
