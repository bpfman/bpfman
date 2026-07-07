// Package source models positions within bpfman-shell DSL scripts. Pos
// is a single file/line/column point and Span is a half-open range from
// a start position to an exclusive end. Both are embedded in the DSL's
// AST and IR nodes to carry provenance for error reporting.
package source

// Pos is a single point in source: file identity plus 1-based line
// and column. Col is a byte offset within the line, not a rune
// offset. The zero value means "unknown location".
type Pos struct {
	// File identifies the source unit the position refers to,
	// typically a file path or a synthetic label for stdin-driven
	// input.
	File string

	// Line is the 1-based line number. Zero in the zero (unknown)
	// Pos.
	Line int

	// Col is the 1-based column, measured as a byte offset within
	// the line rather than a rune offset, so multi-byte UTF-8
	// characters advance it by more than one. Zero in the zero
	// (unknown) Pos.
	Col int
}

// Span is a half-open source range. Pos is the start (inclusive);
// End is one past the last byte of the spanned region (exclusive).
// End == Pos{} means the End field is unset and only the start is
// meaningful.
type Span struct {
	// Pos is the start of the span (inclusive).
	Pos

	// End is one past the last byte of the span (exclusive). The
	// zero Pos means End is unset and only the start position is
	// meaningful.
	End Pos
}
