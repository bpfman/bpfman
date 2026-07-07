// Rust-compiler-style multi-line diagnostic rendering. Given a
// source string and a source.Span describing the offending region, emit
// a header citation, a numbered source frame, and a caret span
// underlining the region with the message attached. Mirrors the
// shape rustc and clang produce so the format is familiar to
// anyone who has touched a typed language with positional errors.
//
// One helper: renderDiagnostic for the full diagnostic shape
// (with optional Help text).

package driver

import (
	"fmt"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// diagnostic is the structured form of one rendered error: where
// it points, what it says, and any inline help. source.Span carries the
// source range; Msg is the headline; Help is optional advice
// rendered as a trailing "= help: ..." line (rustc shape).
type diagnostic struct {
	Span source.Span
	Msg  string
	Help string
}

// renderDiagnostic formats d against src with a rust-compiler
// frame: a leading "error: ..." line, a " --> file:line:col"
// citation, the numbered source line(s) covered by the span, a
// caret span underlining the region, and an optional help
// footer. file may be empty, in which case the citation omits
// the path. src is the raw program text; positions are 1-based.
//
// When the span ends on the same line as it starts, the caret
// run covers exactly d.Span.End.Col - d.Span.Pos.Col bytes (with
// a minimum width of 1 so single-point spans still draw a caret).
// Multi-line spans render the start line with carets running to
// the end of that line and an ellipsis indicator pointing the
// reader to the trailing lines; the cost of full multi-line
// painting is not justified by the current callers.
func renderDiagnostic(src, file string, d diagnostic) string {
	var b strings.Builder
	fmt.Fprintf(&b, "error: %s\n", d.Msg)

	startLine := d.Span.Pos.Line
	startCol := d.Span.Pos.Col
	endLine := d.Span.End.Line
	endCol := d.Span.End.Col
	if endLine == 0 {
		endLine = startLine
		endCol = startCol + 1
	}

	if file != "" {
		fmt.Fprintf(&b, " --> %s:%d:%d\n", file, startLine, startCol)
	} else {
		fmt.Fprintf(&b, " --> %d:%d\n", startLine, startCol)
	}

	lines := splitSourceLines(src)
	gutter := lineNumberWidth(startLine)
	pad := strings.Repeat(" ", gutter)

	fmt.Fprintf(&b, "%s |\n", pad)

	if startLine >= 1 && startLine <= len(lines) {
		text := lines[startLine-1]
		fmt.Fprintf(&b, "%*d | %s\n", gutter, startLine, text)

		caretStart := max(startCol-1, 0)
		caretEnd := endCol - 1
		if endLine != startLine || caretEnd <= caretStart {
			caretEnd = len(text)
		}
		if caretEnd > len(text) {
			caretEnd = len(text)
		}
		width := max(caretEnd-caretStart, 1)
		fmt.Fprintf(&b, "%s | %s%s %s\n", pad, strings.Repeat(" ", caretStart), strings.Repeat("^", width), d.Msg)
	}

	if endLine > startLine {
		fmt.Fprintf(&b, "%s | ... continues to line %d:%d\n", pad, endLine, endCol)
	}

	if d.Help != "" {
		fmt.Fprintf(&b, "%s |\n", pad)
		fmt.Fprintf(&b, "%s = help: %s\n", pad, d.Help)
	}

	return b.String()
}

// splitSourceLines splits src on '\n' without dropping a trailing
// empty line. Tabs and other whitespace are preserved so column
// arithmetic against the original byte offsets stays accurate.
func splitSourceLines(src string) []string {
	if src == "" {
		return nil
	}
	return strings.Split(src, "\n")
}

// lineNumberWidth returns the printable width of the largest line
// number that may appear in the frame's gutter. The renderer
// prints only one source line today so the answer is always the
// width of n.
func lineNumberWidth(n int) int {
	if n < 10 {
		return 1
	}
	if n < 100 {
		return 2
	}
	if n < 1000 {
		return 3
	}
	w := 0
	for n > 0 {
		w++
		n /= 10
	}
	return w
}
