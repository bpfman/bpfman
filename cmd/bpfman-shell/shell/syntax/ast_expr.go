package syntax

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"

// Expr is the sealed sum type for shell expressions.
type Expr interface {
	Node
	exprNode()
}

// LiteralExpr is a bare word or quoted-string literal: a command
// argument, a flag, a path, a numeric word, or a quoted string.
type LiteralExpr struct {
	// Text is the literal content with any surrounding quotes
	// removed.
	Text string

	// Quoted reports whether the literal was written in quoted form
	// (single or double quotes). A quoted literal always evaluates
	// to a string; an unquoted word may instead resolve to a
	// boolean, null, or JSON number before falling back to a
	// string.
	Quoted bool

	source.Span
}

// VarRefExpr is a variable reference such as $prog or
// ${prog.maps[0].name}: a name with an optional dotted/indexed path
// into the bound value.
type VarRefExpr struct {
	// Name is the variable being read, without the leading '$'.
	Name string

	// Path is the dotted/indexed navigation applied to the bound
	// value (for example "maps[0].name"), empty for a bare $name.
	Path string

	source.Span
}

// AdapterExpr is an adapter invocation such as file:$var.path. The
// adapter transforms the referenced variable's value at evaluation
// time (the "file" adapter resolves a path against a fetched image).
type AdapterExpr struct {
	// Adapter is the adapter name preceding the colon (for example
	// "file").
	Adapter string

	// Name is the referenced variable, without the leading '$'.
	Name string

	// Path is the dotted/indexed navigation into the variable's
	// value, empty when the reference is bare.
	Path string

	source.Span
}

// InterpStringExpr is a double-quoted string carrying at least one
// ${...} interpolation. A double-quoted string with no
// interpolation parses as a quoted LiteralExpr instead.
type InterpStringExpr struct {
	// Segments is the ordered alternation of literal text and
	// interpolated sub-expressions; the evaluator concatenates
	// their resolved scalars left to right.
	Segments []InterpStringSegment

	source.Span
}

// InterpStringSegment is one piece of an InterpStringExpr: either a
// run of literal text or a single interpolated expression, never
// both.
type InterpStringSegment struct {
	// Literal is the verbatim text of a literal segment; it is
	// empty for an interpolation segment.
	Literal string

	// Expr is the parsed expression of an interpolation segment; it
	// is nil for a literal segment.
	Expr Expr
}

// BinaryExpr is a comparison between two operands using one of ==,
// !=, <, <=, >, or >=.
type BinaryExpr struct {
	// Left is the left-hand operand.
	Left Expr

	// Op is the comparison operator spelling (for example "==").
	Op string

	// Right is the right-hand operand.
	Right Expr

	source.Span
}

// UnaryExpr is a unary predicate applied to one operand, such as
// `not-empty $xs`.
type UnaryExpr struct {
	// Pred is the predicate keyword (currently only "not-empty").
	Pred string

	// Operand is the expression the predicate is tested against.
	Operand Expr

	source.Span
}

// ThreadExpr is a value-threading pipeline written with '|>'. The
// evaluated LHS is fed into the RHS command as its final argument,
// matching the thread-last semantics of '|>' in F#, Elixir, and R.
type ThreadExpr struct {
	// LHS is the value-producing expression on the left of '|>'.
	LHS Expr

	// Args is the RHS command and its arguments; the threaded LHS
	// value is supplied as the command's last argument.
	Args []Expr

	// PipePos is the source position of the '|>' operator, retained
	// for diagnostics.
	PipePos source.Pos

	source.Span
}

// LogicalExpr is a short-circuiting boolean combination written
// with the `and` or `or` keyword.
type LogicalExpr struct {
	// Op is the operator keyword, "and" or "or".
	Op string

	// Left and Right are the boolean operands; Right is only
	// evaluated when Left does not already decide the result.
	Left, Right Expr

	source.Span
}

// NotExpr is a boolean negation written `not EXPR`.
type NotExpr struct {
	// Operand is the expression whose boolean value is negated.
	Operand Expr

	source.Span
}

// NegateExpr is an arithmetic negation written `-EXPR`.
type NegateExpr struct {
	// Operand is the numeric expression being negated.
	Operand Expr

	source.Span
}

// PureCallExpr is a call to a pure builtin in expression position,
// such as `jq ".id" $out` or `range 5`. The recognised builtins and
// their arities are fixed by the parser (see registerPureBuiltin).
type PureCallExpr struct {
	// Name is the builtin being invoked (for example "jq").
	Name string

	// Args is the ordered argument expressions; their count matches
	// the builtin's declared arity.
	Args []Expr

	source.Span
}

// MatchesExpr is a structural match written `EXPR matches { ... }`
// (optionally `matches exhaustive`). It tests the target value
// against the path/pattern rows of its block and yields a boolean.
type MatchesExpr struct {
	// Target is the value being matched against the block's rows.
	Target Expr

	// Block is the parsed `{ path: pattern, ... }` body.
	Block *MatchesBlockExpr

	source.Span
}

// ListExpr is a bracketed list literal such as `[a b c]`. Elements
// are whitespace-separated; commas are not separators.
type ListExpr struct {
	// Elems is the ordered element expressions.
	Elems []Expr

	source.Span
}

// RecordField is one `name: value` entry of a RecordExpr.
type RecordField struct {
	// Name is the field name preceding the colon.
	Name string

	// Expr is the field's value expression.
	Expr Expr

	source.Span
}

// RecordExpr is a record literal written `record { name: value ... }`.
// Fields are whitespace-separated; commas are not separators and
// duplicate field names are rejected at parse time.
type RecordExpr struct {
	// Fields is the ordered set of named fields in source order.
	Fields []RecordField

	source.Span
}

func (*LiteralExpr) exprNode()      {}
func (*VarRefExpr) exprNode()       {}
func (*AdapterExpr) exprNode()      {}
func (*InterpStringExpr) exprNode() {}
func (*BinaryExpr) exprNode()       {}
func (*UnaryExpr) exprNode()        {}
func (*ThreadExpr) exprNode()       {}
func (*LogicalExpr) exprNode()      {}
func (*NotExpr) exprNode()          {}
func (*NegateExpr) exprNode()       {}
func (*PureCallExpr) exprNode()     {}
func (*MatchesExpr) exprNode()      {}
func (*ListExpr) exprNode()         {}
func (*RecordExpr) exprNode()       {}
