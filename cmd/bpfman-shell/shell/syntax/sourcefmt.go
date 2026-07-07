package syntax

import (
	"fmt"
	"strings"
)

// indentUnit is one level of indentation in canonical source output.
const indentUnit = "    "

// FormatExprSource renders expr in the shell's compact source-like form.
// Used by diagnostics and traces that need the original expression shape
// without reimplementing the expression formatter.
func FormatExprSource(expr Expr) string {
	return dumpExprSource(expr)
}

// FormatExprSourceIndented renders expr in the shell's compact
// source-like form, laying a matches block out across multiple lines
// at the given indentation depth. Expressions other than a matches
// render the same as FormatExprSource.
func FormatExprSourceIndented(expr Expr, indent int) string {
	if expr == nil {
		return "nil"
	}
	var b strings.Builder
	writeExprSourceIndented(&b, expr, indent)
	return b.String()
}

func dumpExprSource(e Expr) string {
	if e == nil {
		return "nil"
	}
	var b strings.Builder
	writeExprSource(&b, e)
	return b.String()
}

func writeExprSource(b *strings.Builder, e Expr) {
	switch v := e.(type) {
	case *LiteralExpr:
		if v.Quoted {
			writeQuotedLiteralSource(b, v.Text)
			return
		}
		b.WriteString(v.Text)
	case *VarRefExpr:
		b.WriteByte('$')
		b.WriteString(v.Name)
		writeVarPathSource(b, v.Path)
	case *AdapterExpr:
		b.WriteString(v.Adapter)
		b.WriteByte(':')
		b.WriteByte('$')
		b.WriteString(v.Name)
		writeVarPathSource(b, v.Path)
	case *InterpStringExpr:
		b.WriteByte('"')
		for _, seg := range v.Segments {
			if seg.Expr == nil {
				writeDoubleQuotedContent(b, seg.Literal)
				continue
			}
			b.WriteString("${")
			switch sub := seg.Expr.(type) {
			case *VarRefExpr:
				b.WriteString(sub.Name)
				writeVarPathSource(b, sub.Path)
			case *AdapterExpr:
				b.WriteString(sub.Adapter)
				b.WriteByte(':')
				b.WriteString(sub.Name)
				writeVarPathSource(b, sub.Path)
			default:
				writeExprSource(b, seg.Expr)
			}
			b.WriteByte('}')
		}
		b.WriteByte('"')
	case *BinaryExpr:
		writeExprAtomSource(b, v.Left)
		b.WriteByte(' ')
		b.WriteString(v.Op)
		b.WriteByte(' ')
		writeExprAtomSource(b, v.Right)
	case *UnaryExpr:
		b.WriteString(v.Pred)
		b.WriteByte(' ')
		writeExprAtomSource(b, v.Operand)
	case *ThreadExpr:
		writeExprAtomSource(b, v.LHS)
		b.WriteString(" |>")
		for _, a := range v.Args {
			b.WriteByte(' ')
			writeExprSource(b, a)
		}
	case *LogicalExpr:
		writeExprAtomSource(b, v.Left)
		b.WriteByte(' ')
		b.WriteString(v.Op)
		b.WriteByte(' ')
		writeExprAtomSource(b, v.Right)
	case *NotExpr:
		b.WriteString("not ")
		writeExprAtomSource(b, v.Operand)
	case *NegateExpr:
		b.WriteByte('-')
		writeExprAtomSource(b, v.Operand)
	case *PureCallExpr:
		b.WriteString(v.Name)
		for _, a := range v.Args {
			b.WriteByte(' ')
			writePureCallArgSource(b, a)
		}
	case *MatchesExpr:
		writeExprAtomSource(b, v.Target)
		b.WriteByte(' ')
		writeMatchesBlockSource(b, v.Block)
	case *ListExpr:
		b.WriteByte('[')
		for i, elem := range v.Elems {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeListElemSource(b, elem)
		}
		b.WriteByte(']')
	case *RecordExpr:
		b.WriteString("record {")
		for i, field := range v.Fields {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(field.Name)
			b.WriteString(": ")
			writeListElemSource(b, field.Expr)
		}
		b.WriteByte('}')
	default:
		t := fmt.Sprintf("%T", e)
		if i := strings.LastIndex(t, "."); i >= 0 {
			t = t[i+1:]
		}
		fmt.Fprintf(b, "<%s>", t)
	}
}

func writeExprSourceIndented(b *strings.Builder, e Expr, indent int) {
	switch v := e.(type) {
	case *MatchesExpr:
		writeExprAtomSource(b, v.Target)
		b.WriteByte(' ')
		writeMatchesBlockSourceIndented(b, v.Block, indent)
	default:
		writeExprSource(b, e)
	}
}

func writeVarPathSource(b *strings.Builder, path string) {
	if path == "" {
		return
	}
	if strings.HasPrefix(path, "[") {
		b.WriteString(path)
		return
	}
	b.WriteByte('.')
	b.WriteString(path)
}

func writeQuotedLiteralSource(b *strings.Builder, text string) {
	if strings.ContainsAny(text, "\"$") && !strings.Contains(text, "'") &&
		!strings.ContainsAny(text, "\n\t\r") {
		b.WriteByte('\'')
		b.WriteString(text)
		b.WriteByte('\'')
		return
	}
	b.WriteByte('"')
	writeDoubleQuotedContent(b, text)
	b.WriteByte('"')
}

func writeDoubleQuotedContent(b *strings.Builder, text string) {
	for _, r := range text {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '$':
			b.WriteString(`\$`)
		default:
			b.WriteRune(r)
		}
	}
}

func writeExprAtomSource(b *strings.Builder, e Expr) {
	switch e.(type) {
	case *BinaryExpr, *LogicalExpr, *ThreadExpr, *UnaryExpr, *NotExpr, *NegateExpr, *MatchesExpr:
		b.WriteByte('(')
		writeExprSource(b, e)
		b.WriteByte(')')
	default:
		writeExprSource(b, e)
	}
}

func writePureCallArgSource(b *strings.Builder, e Expr) {
	switch e.(type) {
	case *LiteralExpr, *VarRefExpr, *AdapterExpr, *InterpStringExpr, *ListExpr, *RecordExpr:
		writeExprSource(b, e)
	default:
		b.WriteByte('(')
		writeExprSource(b, e)
		b.WriteByte(')')
	}
}

func writeListElemSource(b *strings.Builder, e Expr) {
	switch e.(type) {
	case *LiteralExpr, *VarRefExpr, *AdapterExpr, *InterpStringExpr, *ListExpr, *RecordExpr, *PureCallExpr:
		writeExprSource(b, e)
	default:
		b.WriteByte('(')
		writeExprSource(b, e)
		b.WriteByte(')')
	}
}

func writeMatchesBlockSource(b *strings.Builder, m *MatchesBlockExpr) {
	b.WriteString("matches")
	if m.Exhaustive {
		b.WriteString(" exhaustive")
	}
	b.WriteString(" {")
	for i, ent := range m.Entries {
		// The matches parser separates entries by newlines and
		// explicitly rejects commas; emitting commas here would
		// produce a string that does not round-trip through
		// Parse. Use newlines so the printed form is a valid
		// matches body, falling back to a single space before
		// the first entry so an empty block still renders as
		// "matches { }" rather than "matches {\n}".
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteByte(' ')
		b.WriteString(ent.Path)
		b.WriteString(": ")
		switch {
		case ent.Predicate != "":
			b.WriteString(ent.Predicate)
		case ent.SubBlock != nil:
			writeMatchesBlockSource(b, ent.SubBlock)
		default:
			writeExprSource(b, ent.Pattern)
		}
	}
	if len(m.Entries) > 0 {
		b.WriteByte(' ')
	}
	b.WriteByte('}')
}

func writeMatchesBlockSourceIndented(b *strings.Builder, m *MatchesBlockExpr, indent int) {
	b.WriteString("matches")
	if m.Exhaustive {
		b.WriteString(" exhaustive")
	}
	if len(m.Entries) == 0 {
		b.WriteString(" { }")
		return
	}
	b.WriteString(" {\n")
	width := matchesPathWidth(m)
	for _, ent := range m.Entries {
		writeSourceIndent(b, indent+1)
		b.WriteString(ent.Path)
		b.WriteString(": ")
		switch {
		case ent.Predicate != "":
			writeMatchesValuePadding(b, ent, width)
			b.WriteString(ent.Predicate)
		case ent.SubBlock != nil:
			writeMatchesBlockSourceIndented(b, ent.SubBlock, indent+1)
		default:
			writeMatchesValuePadding(b, ent, width)
			writeExprSource(b, ent.Pattern)
		}
		b.WriteByte('\n')
	}
	writeSourceIndent(b, indent)
	b.WriteByte('}')
}

func writeMatchesValuePadding(b *strings.Builder, ent MatchEntry, width int) {
	for i := len(ent.Path); i < width; i++ {
		b.WriteByte(' ')
	}
}

func matchesPathWidth(m *MatchesBlockExpr) int {
	width := 0
	for _, ent := range m.Entries {
		if len(ent.Path) > width {
			width = len(ent.Path)
		}
	}
	return width
}

func writeSourceIndent(b *strings.Builder, indent int) {
	for range indent {
		b.WriteString(indentUnit)
	}
}
