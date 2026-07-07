package ir

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"

// Expr is the lowered-expression sum type carried by Eval,
// BuildArgs, and Assert. It mirrors the surface expression
// families the runtime still needs after statement lowering.
// It is distinct from the parser AST.
type Expr interface {
	// irExprNode is the unexported marker that seals the Expr sum;
	// only types in this package can implement it.
	irExprNode()
}

// LiteralExpr is a literal word or quoted string, lowered from a
// syntax.LiteralExpr. In argument position a quoted literal becomes a
// QuotedArg and an unquoted one a WordArg.
type LiteralExpr struct {
	// Text is the literal's text, without surrounding quotes.
	Text string

	// Quoted records whether the literal was written as a quoted
	// string, which the dumper and argument resolution preserve.
	Quoted bool

	source.Span
}

// VarRefExpr is a variable reference "$name" with an optional dotted
// path into a structured value, lowered from a syntax.VarRefExpr.
type VarRefExpr struct {
	// Name is the variable being referenced.
	Name string

	// Path is the dotted path selecting into a structured value;
	// empty for a bare "$name" reference.
	Path string

	source.Span
}

// AdapterExpr is an inline adapter invocation "adapter:$name.path",
// lowered from a syntax.AdapterExpr. It re-interprets the named
// variable's value through the named adapter at argument-resolution
// time; it is valid only in argument position and is rejected when
// evaluated as a plain expression operand.
type AdapterExpr struct {
	// Adapter is the adapter name applied to the resolved value.
	Adapter string

	// Name is the variable whose value the adapter re-interprets.
	Name string

	// Path is the dotted path selecting into the variable's value;
	// empty for a bare reference.
	Path string

	source.Span
}

// ListExpr is a list literal "[a b c]", lowered from a
// syntax.ListExpr. The elements are evaluated left to right into a
// list value with per-element origin preserved.
type ListExpr struct {
	// Elems are the element expressions, evaluated left to right.
	Elems []Expr

	source.Span
}

// RecordField is one "name: expr" field of a record literal.
type RecordField struct {
	// Name is the field name.
	Name string

	// Expr is the field's value expression.
	Expr Expr

	source.Span
}

// RecordExpr is a record literal "record { name: expr ... }", lowered
// from a syntax.RecordExpr. The fields are evaluated in source order
// into a record value.
type RecordExpr struct {
	// Fields are the record's fields in source order.
	Fields []RecordField

	source.Span
}

// InterpStringExpr is an interpolated string "...${expr}...", lowered
// from a syntax.InterpStringExpr. Evaluation concatenates each
// literal segment verbatim and each embedded expression rendered
// compactly.
type InterpStringExpr struct {
	// Segments are the alternating literal-text and embedded-
	// expression pieces, in source order.
	Segments []InterpStringSegment

	source.Span
}

// InterpStringSegment is one piece of an interpolated string: either
// literal text (Expr nil) or an embedded expression (Literal unused).
type InterpStringSegment struct {
	// Literal is the segment's literal text when Expr is nil.
	Literal string

	// Expr is the embedded expression for this segment; nil marks a
	// literal-text segment.
	Expr Expr
}

// ThreadExpr is a thread expression "LHS |> head args...", lowered
// from a syntax.ThreadExpr. LHS is appended as the trailing argument
// to the command formed by Args, dispatched in bind position with the
// rc envelope discarded; the command's primary value is the result.
type ThreadExpr struct {
	// LHS is the left operand, appended as the final argument to the
	// threaded command.
	LHS Expr

	// Args are the command head and any leading arguments the LHS
	// threads into.
	Args []Expr

	// PipePos is the source position of the "|>" operator, used to
	// frame thread diagnostics.
	PipePos source.Pos

	source.Span
}

// BinaryExpr is a binary arithmetic or comparison "Left Op Right",
// lowered from a syntax.BinaryExpr.
type BinaryExpr struct {
	// Left is the left operand.
	Left Expr

	// Op is the operator text; an arithmetic operator coerces both
	// operands to scalars, a comparison operator yields a bool.
	Op string

	// Right is the right operand.
	Right Expr

	source.Span
}

// UnaryExpr is a unary predicate "Pred Operand", lowered from a
// syntax.UnaryExpr. The only recognised predicate is "not-empty".
type UnaryExpr struct {
	// Pred is the predicate name (currently only "not-empty").
	Pred string

	// Operand is the value the predicate tests.
	Operand Expr

	source.Span
}

// LogicalExpr is a short-circuiting logical expression "Left Op
// Right" with Op "and" or "or", lowered from a syntax.LogicalExpr.
type LogicalExpr struct {
	// Op is "and" or "or" and controls the short-circuit rule.
	Op string

	// Left and Right are the operands; Right is evaluated only when
	// Left does not already decide the result.
	Left, Right Expr

	source.Span
}

// NotExpr is a boolean negation "not Operand", lowered from a
// syntax.NotExpr.
type NotExpr struct {
	// Operand is the boolean value to negate.
	Operand Expr

	source.Span
}

// NegateExpr is an arithmetic negation "-Operand", lowered from a
// syntax.NegateExpr.
type NegateExpr struct {
	// Operand is the numeric value to negate.
	Operand Expr

	source.Span
}

// PureCallExpr is a pure-builtin call "name args..." in expression
// position, lowered from a syntax.PureCallExpr. It dispatches through
// the bind handler and yields the call's primary value.
type PureCallExpr struct {
	// Name is the pure builtin being called.
	Name string

	// Args are the call's argument expressions.
	Args []Expr

	source.Span
}

// MatchesExpr is a "TARGET matches { ... }" predicate, lowered from a
// syntax.MatchesExpr. It evaluates Block against the value of Target
// and yields a bool.
type MatchesExpr struct {
	// Target is the value the match block is evaluated against.
	Target Expr

	// Block is the match block describing the rows to satisfy.
	Block *MatchesBlockExpr

	source.Span
}

// MatchesBlockExpr is the "matches { path: pattern, ... }" block owned
// by a MatchesExpr, lowered from a syntax.MatchesBlockExpr.
type MatchesBlockExpr struct {
	// Entries are the block's rows, each matching a path against a
	// pattern, sub-block, or predicate.
	Entries []MatchEntry

	// Exhaustive marks the "exhaustive" keyword, requiring every
	// field of the target to be covered by a row.
	Exhaustive bool

	source.Span
}

// MatchEntry is one "path: ..." row inside a matches block. Exactly
// one of Pattern, SubBlock, and Predicate is meaningful.
type MatchEntry struct {
	// Path is the dotted path into the target selecting the value
	// this row matches.
	Path string

	// Pattern is the value expression compared for equality, used
	// when neither SubBlock nor Predicate is set.
	Pattern Expr

	// SubBlock is a nested matches block applied to the selected
	// value, when the row recurses.
	SubBlock *MatchesBlockExpr

	// Predicate is a named predicate (e.g. "not-empty", "null",
	// "empty") tested against the selected value, when the row tests
	// a predicate rather than a value.
	Predicate string

	source.Span
}

func (*LiteralExpr) irExprNode()      {}
func (*VarRefExpr) irExprNode()       {}
func (*AdapterExpr) irExprNode()      {}
func (*ListExpr) irExprNode()         {}
func (*RecordExpr) irExprNode()       {}
func (*InterpStringExpr) irExprNode() {}
func (*ThreadExpr) irExprNode()       {}
func (*BinaryExpr) irExprNode()       {}
func (*UnaryExpr) irExprNode()        {}
func (*LogicalExpr) irExprNode()      {}
func (*NotExpr) irExprNode()          {}
func (*NegateExpr) irExprNode()       {}
func (*PureCallExpr) irExprNode()     {}
func (*MatchesExpr) irExprNode()      {}
