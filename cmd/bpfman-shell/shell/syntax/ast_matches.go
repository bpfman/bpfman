package syntax

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"

// MatchesBlockExpr is the parsed `matches { path: pattern, ... }`
// block owned by a MatchesExpr, or a nested sub-block reached from a
// MatchEntry.
type MatchesBlockExpr struct {
	// Entries is the ordered set of path/pattern rows, one per
	// source line inside the block.
	Entries []MatchEntry

	// Exhaustive records whether the block was written `matches
	// exhaustive { ... }`, which additionally requires the matched
	// value to have no fields beyond those the block names.
	Exhaustive bool

	source.Span
}

// MatchEntry is one row inside a matches block: a path on the left
// of the colon and exactly one of a pattern, a bareword predicate,
// or a nested sub-block on the right.
type MatchEntry struct {
	// Path is the dotted/indexed navigation selecting the sub-value
	// this row matches against (for example "maps[0].name"). In an
	// exhaustive block it is restricted to a single field name.
	Path string

	// Pattern is the expression whose evaluated value is compared
	// for equality against the value at Path; nil when the row uses
	// Predicate or SubBlock instead.
	Pattern Expr

	// SubBlock is a nested `matches [exhaustive] { ... }` recursing
	// against the sub-value at Path; nil when the row uses Pattern
	// or Predicate.
	SubBlock *MatchesBlockExpr

	// Predicate is a bareword predicate applied to the value at Path
	// (one of "not-empty", "null", "empty"); empty when the row uses
	// Pattern or SubBlock.
	Predicate string

	source.Span
}
