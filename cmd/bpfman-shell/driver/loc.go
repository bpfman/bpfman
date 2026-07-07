// SourceLoc is the file/line/col triple the shell runner threads
// through every diagnostic. Lives in the driver package because the loop,
// the dispatcher, the renderer, and every handler that emits a
// failure path share it.

package driver

import (
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// SourceLoc identifies a position in a script file. The zero
// value means "no location" and formats as the empty string, so
// stdin-driven modes are unaffected.
type SourceLoc struct {
	// File is the script file name, or empty for the zero value.
	File string

	// Line is the 1-based line number.
	Line int

	// Col is the 1-based column, or 0 when no column is known.
	Col int
}

// String renders the location as `file:line: ` (or
// `file:line:col: `), suitable as a prefix for error messages.
// Returns the empty string for the zero value.
func (l SourceLoc) String() string {
	if l.File == "" {
		return ""
	}
	if l.Col > 0 {
		return fmt.Sprintf("%s:%d:%d: ", l.File, l.Line, l.Col)
	}
	return fmt.Sprintf("%s:%d: ", l.File, l.Line)
}

// Cite returns the bare `file:line[:col]` citation without the
// trailing `: ` separator that String adds for inline error
// prefixes. Used when the location is rendered as a value in
// its own right (Job.Origin, for example, so the scope-exit
// leak diagnostic can show where the start lived).
func (l SourceLoc) Cite() string {
	if l.File == "" {
		return ""
	}
	if l.Col > 0 {
		return fmt.Sprintf("%s:%d:%d", l.File, l.Line, l.Col)
	}
	return fmt.Sprintf("%s:%d", l.File, l.Line)
}

// WithSpan returns a SourceLoc pointing at span's start. When the
// span carries no file, l.File is used as a fallback so stdin/local
// callers still get a stable citation.
func (l SourceLoc) WithSpan(span source.Span) SourceLoc {
	file := span.Pos.File
	if file == "" {
		file = l.File
	}
	if file == "" {
		return SourceLoc{}
	}
	return SourceLoc{File: file, Line: span.Pos.Line, Col: span.Pos.Col}
}
