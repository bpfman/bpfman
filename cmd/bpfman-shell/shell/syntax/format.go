package syntax

import (
	"fmt"
	"strings"
)

// FormatSource renders prog as canonical bpfman-shell source while
// preserving comments and blank lines from src. This mirrors Go's
// formatter architecture conceptually: the AST supplies semantic
// structure, and the original source supplies trivia that execution
// intentionally ignores. Backslash-continued command line grouping is
// also treated as source layout: the formatter preserves the author's
// physical line breaks while canonicalising indentation and token
// spelling within each line.
func FormatSource(src string, prog *Program) string {
	f := newSourceFormatter(src)
	if prog == nil || len(prog.Stmts) == 0 {
		f.writeTrivia(1, len(f.lines))
		return f.b.String()
	}
	f.writeStmtsSource(prog.Stmts, 0, 1, len(f.lines))
	return f.b.String()
}

type sourceFormatter struct {
	b     strings.Builder
	lines []string
}

func newSourceFormatter(src string) sourceFormatter {
	return sourceFormatter{lines: splitSourceLines(src)}
}

func (f *sourceFormatter) writeStmts(stmts []Stmt, indent int) {
	for _, st := range stmts {
		f.writeIndent(indent)
		f.writeStmt(st, indent)
		f.b.WriteByte('\n')
	}
}

func (f *sourceFormatter) writeStmtsSource(stmts []Stmt, indent, startLine, endLine int) {
	nextLine := startLine
	for _, st := range stmts {
		sp := NodeSpan(st)
		f.writeTrivia(nextLine, sp.Pos.Line-1)
		if !isBlockStmt(st) && !f.hasContinuation(sp.Pos.Line, sp.End.Line) {
			f.writeInteriorComments(sp.Pos.Line+1, sp.End.Line-1, indent)
		}
		f.writeIndent(indent)
		f.writeStmtSource(st, indent)
		f.writeLineComment(sp.End.Line)
		f.b.WriteByte('\n')
		nextLine = sp.End.Line + 1
	}
	f.writeTrivia(nextLine, endLine)
}

func (f *sourceFormatter) writeStmtSource(st Stmt, indent int) {
	switch v := st.(type) {
	case *IfStmt:
		f.writeIfStmtSource(v, indent)
	case *ForEachStmt:
		f.writeForEachStmtSource(v, indent)
	case *PollStmt:
		f.writePollStmtSource(v, indent)
	case *DefStmt:
		f.writeDefStmtSource(v, indent)
	case *BindStmt:
		if v.Collect != nil {
			f.writeBindCollectStmtSource(v, indent)
			return
		}
		if f.hasContinuation(v.Pos.Line, v.End.Line) && v.Cmd != nil {
			f.writeBindStmtContinued(v, indent)
			return
		}
		f.writeStmt(st, indent)
	case *DeferStmt:
		if f.hasContinuation(v.Pos.Line, v.End.Line) && v.Cmd != nil {
			f.writeContinuedCommand("defer ", v.Cmd.Args, indent)
			return
		}
		f.writeStmt(st, indent)
	case *CommandStmt:
		if f.hasContinuation(v.Pos.Line, v.End.Line) {
			f.writeContinuedCommand("", v.Args, indent)
			return
		}
		f.writeStmt(st, indent)
	case *AssertStmt:
		if f.hasContinuation(v.Pos.Line, v.End.Line) {
			if f.writeAssertStmtContinued(v, indent) {
				return
			}
		}
		f.writeStmt(st, indent)
	default:
		f.writeStmt(st, indent)
	}
}

func isBlockStmt(st Stmt) bool {
	switch v := st.(type) {
	case *IfStmt, *ForEachStmt, *PollStmt, *DefStmt:
		return true
	case *BindStmt:
		return v.Collect != nil
	default:
		return false
	}
}

func (f *sourceFormatter) writeStmt(st Stmt, indent int) {
	switch v := st.(type) {
	case *LetStmt:
		fmt.Fprintf(&f.b, "let %s = ", v.Name.Text)
		f.writeExprIndented(v.RHS, indent)
	case *LetDestructureStmt:
		f.b.WriteString("let (")
		f.b.WriteString(joinIdentTexts(v.Names))
		f.b.WriteString(") = ")
		f.writeExprIndented(v.RHS, indent)
	case *BindStmt:
		f.writeBindStmt(v, indent)
	case *DeferStmt:
		f.b.WriteString("defer")
		if v.Cmd != nil {
			f.writeCommandArgs(v.Cmd.Args)
		}
	case *IfStmt:
		f.writeIfStmt(v, indent)
	case *CommandStmt:
		f.writeCommandStmt(v)
	case *ExprStmt:
		f.b.WriteByte('(')
		f.writeExpr(v.Expr)
		f.b.WriteByte(')')
	case *ForEachStmt:
		f.writeForEachStmt(v, indent)
	case *BreakStmt:
		f.b.WriteString("break")
	case *ContinueStmt:
		f.b.WriteString("continue")
	case *PollStmt:
		fmt.Fprintf(&f.b, "poll timeout %s every %s {\n", v.Timeout, v.Every)
		f.writeStmts(v.Body, indent+1)
		f.writeIndent(indent)
		f.b.WriteByte('}')
	case *RetryStmt:
		f.b.WriteString("retry")
		if v.Message != nil {
			f.b.WriteByte(' ')
			f.writeExpr(v.Message)
		}
		if v.Unless != nil {
			f.b.WriteString(" unless ")
			f.writeExpr(v.Unless)
		}
	case *DefStmt:
		fmt.Fprintf(&f.b, "def %s(", v.Name.Text)
		f.b.WriteString(joinDefParams(v.Params))
		f.b.WriteString(") {\n")
		f.writeStmts(v.Body, indent+1)
		f.writeIndent(indent)
		f.b.WriteByte('}')
	case *ReturnStmt:
		f.b.WriteString("return ")
		f.writeExprIndented(v.Expr, indent)
	case *AssertStmt:
		if v.IsRequire {
			f.b.WriteString("require ")
		} else {
			f.b.WriteString("assert ")
		}
		f.writeAssertClause(v.Clause, indent)
	default:
		fmt.Fprintf(&f.b, "<%T>", st)
	}
}

func (f *sourceFormatter) writeIfStmtSource(v *IfStmt, indent int) {
	if v.Pos.Line == v.End.Line {
		f.writeIfStmt(v, indent)
		return
	}
	f.b.WriteString("if ")
	f.writeExpr(v.Cond)
	f.b.WriteString(" {")
	f.writeLineComment(v.Pos.Line)
	f.b.WriteByte('\n')
	thenEnd := v.End.Line - 1
	if len(v.Elifs) > 0 {
		thenEnd = v.Elifs[0].Pos.Line - 1
	} else if len(v.Else) > 0 {
		thenEnd = f.elseLine(v) - 1
	}
	f.writeStmtsSource(v.Then, indent+1, v.Pos.Line+1, thenEnd)
	f.writeIndent(indent)
	f.b.WriteByte('}')

	for i, br := range v.Elifs {
		f.b.WriteString(" elif ")
		f.writeExpr(br.Cond)
		f.b.WriteString(" {")
		f.writeLineComment(br.Pos.Line)
		f.b.WriteByte('\n')
		bodyEnd := br.End.Line - 1
		if i+1 < len(v.Elifs) {
			bodyEnd = v.Elifs[i+1].Pos.Line - 1
		} else if len(v.Else) > 0 {
			bodyEnd = f.elseLine(v) - 1
		}
		f.writeStmtsSource(br.Body, indent+1, br.Pos.Line+1, bodyEnd)
		f.writeIndent(indent)
		f.b.WriteByte('}')
	}

	if len(v.Else) > 0 {
		elseLine := f.elseLine(v)
		f.b.WriteString(" else {")
		f.writeLineComment(elseLine)
		f.b.WriteByte('\n')
		f.writeStmtsSource(v.Else, indent+1, elseLine+1, v.End.Line-1)
		f.writeIndent(indent)
		f.b.WriteByte('}')
	}
}

func (f *sourceFormatter) writeForEachStmtSource(v *ForEachStmt, indent int) {
	if v.Pos.Line == v.End.Line {
		f.writeForEachStmt(v, indent)
		return
	}
	f.writeForEachHeader(v)
	f.b.WriteString(" {")
	f.writeLineComment(v.Pos.Line)
	f.b.WriteByte('\n')
	f.writeStmtsSource(v.Body, indent+1, v.Pos.Line+1, v.End.Line-1)
	f.writeIndent(indent)
	f.b.WriteByte('}')
}

func (f *sourceFormatter) writePollStmtSource(v *PollStmt, indent int) {
	if v.Pos.Line == v.End.Line {
		fmt.Fprintf(&f.b, "poll timeout %s every %s {\n", v.Timeout, v.Every)
		f.writeStmts(v.Body, indent+1)
		f.writeIndent(indent)
		f.b.WriteByte('}')
		return
	}
	fmt.Fprintf(&f.b, "poll timeout %s every %s {", v.Timeout, v.Every)
	f.writeLineComment(v.Pos.Line)
	f.b.WriteByte('\n')
	f.writeStmtsSource(v.Body, indent+1, v.Pos.Line+1, v.End.Line-1)
	f.writeIndent(indent)
	f.b.WriteByte('}')
}

func (f *sourceFormatter) writeDefStmtSource(v *DefStmt, indent int) {
	if v.Pos.Line == v.End.Line {
		fmt.Fprintf(&f.b, "def %s(", v.Name.Text)
		f.b.WriteString(joinDefParams(v.Params))
		f.b.WriteString(") {\n")
		f.writeStmts(v.Body, indent+1)
		f.writeIndent(indent)
		f.b.WriteByte('}')
		return
	}
	fmt.Fprintf(&f.b, "def %s(", v.Name.Text)
	f.b.WriteString(joinDefParams(v.Params))
	f.b.WriteString(") {")
	f.writeLineComment(v.Pos.Line)
	f.b.WriteByte('\n')
	f.writeStmtsSource(v.Body, indent+1, v.Pos.Line+1, v.End.Line-1)
	f.writeIndent(indent)
	f.b.WriteByte('}')
}

func (f *sourceFormatter) writeBindCollectStmtSource(v *BindStmt, indent int) {
	if v.Pos.Line == v.End.Line {
		f.writeBindStmt(v, indent)
		return
	}
	if v.Guard {
		f.b.WriteString("guard ")
	} else {
		f.b.WriteString("let ")
	}
	f.b.WriteString(v.Target.Text)
	f.b.WriteString(" <- ")
	f.writeForEachStmtSource(v.Collect, indent)
}

func (f *sourceFormatter) writeBindStmt(v *BindStmt, indent int) {
	if v.Guard {
		f.b.WriteString("guard ")
	} else {
		f.b.WriteString("let ")
	}
	f.b.WriteString(v.Target.Text)
	f.b.WriteString(" <- ")
	if v.Collect != nil {
		f.writeForEachStmt(v.Collect, indent)
		return
	}
	if v.Cmd != nil {
		f.writeCommandStmt(v.Cmd)
	}
}

func (f *sourceFormatter) writeBindStmtContinued(v *BindStmt, indent int) {
	var prefix strings.Builder
	if v.Guard {
		prefix.WriteString("guard ")
	} else {
		prefix.WriteString("let ")
	}
	prefix.WriteString(v.Target.Text)
	prefix.WriteString(" <- ")
	f.writeContinuedCommand(prefix.String(), v.Cmd.Args, indent)
}

func (f *sourceFormatter) writeIfStmt(v *IfStmt, indent int) {
	f.b.WriteString("if ")
	f.writeExpr(v.Cond)
	f.b.WriteString(" {\n")
	f.writeStmts(v.Then, indent+1)
	f.writeIndent(indent)
	f.b.WriteByte('}')
	for _, br := range v.Elifs {
		f.b.WriteString(" elif ")
		f.writeExpr(br.Cond)
		f.b.WriteString(" {\n")
		f.writeStmts(br.Body, indent+1)
		f.writeIndent(indent)
		f.b.WriteByte('}')
	}
	if len(v.Else) > 0 {
		f.b.WriteString(" else {\n")
		f.writeStmts(v.Else, indent+1)
		f.writeIndent(indent)
		f.b.WriteByte('}')
	}
}

func (f *sourceFormatter) writeForEachStmt(v *ForEachStmt, indent int) {
	f.writeForEachHeader(v)
	f.b.WriteString(" {\n")
	f.writeStmts(v.Body, indent+1)
	f.writeIndent(indent)
	f.b.WriteByte('}')
}

func (f *sourceFormatter) writeForEachHeader(v *ForEachStmt) {
	f.b.WriteString("foreach ")
	if len(v.Names) == 1 {
		f.b.WriteString(v.Names[0].Text)
	} else {
		f.b.WriteByte('(')
		f.b.WriteString(joinIdentTexts(v.Names))
		f.b.WriteByte(')')
	}
	f.b.WriteString(" in ")
	f.writeExpr(v.List)
}

func joinIdentTexts(idents []Ident) string {
	texts := make([]string, 0, len(idents))
	for _, ident := range idents {
		texts = append(texts, ident.Text)
	}
	return strings.Join(texts, " ")
}

// joinDefParams renders a def parameter list back to source form,
// preserving annotations: "a: number b".
func joinDefParams(params []DefParam) string {
	texts := make([]string, 0, len(params))
	for _, p := range params {
		if p.Type != "" {
			texts = append(texts, p.Name.Text+": "+p.Type)
			continue
		}
		texts = append(texts, p.Name.Text)
	}
	return strings.Join(texts, " ")
}

func (f *sourceFormatter) writeCommandStmt(v *CommandStmt) {
	for i, arg := range v.Args {
		if i > 0 {
			f.b.WriteByte(' ')
		}
		f.writeCommandArg(arg)
	}
}

func (f *sourceFormatter) writeContinuedCommand(prefix string, args []Expr, indent int) {
	groups := commandArgLineGroups(args)
	if len(groups) == 0 {
		f.b.WriteString(strings.TrimRight(prefix, " "))
		return
	}
	f.b.WriteString(prefix)
	f.writeCommandArgGroup(groups[0])
	if len(groups) == 1 {
		return
	}
	f.b.WriteString(" \\")
	for i, group := range groups[1:] {
		f.b.WriteByte('\n')
		f.writeIndent(indent + 1)
		f.writeCommandArgGroup(group)
		if i+1 < len(groups[1:]) {
			f.b.WriteString(" \\")
		}
	}
}

func (f *sourceFormatter) writeCommandArgGroup(args []Expr) {
	for i, arg := range args {
		if i > 0 {
			f.b.WriteByte(' ')
		}
		f.b.WriteString(formatCommandArg(arg))
	}
}

func (f *sourceFormatter) writeCommandArgs(args []Expr) {
	for _, arg := range args {
		f.b.WriteByte(' ')
		f.writeCommandArg(arg)
	}
}

func (f *sourceFormatter) writeAssertStmtContinued(v *AssertStmt, indent int) bool {
	clause, ok := v.Clause.(*AssertCommandClause)
	if !ok {
		return false
	}
	if v.IsRequire {
		f.b.WriteString("require ")
	} else {
		f.b.WriteString("assert ")
	}
	if clause.Negate {
		f.b.WriteString("not ")
	}
	f.b.WriteString(clause.Head)
	f.b.WriteByte(' ')
	f.writeContinuedCommand("", clause.Args, indent)
	return true
}

func (f *sourceFormatter) writeAssertClause(clause AssertClause, indent int) {
	switch v := clause.(type) {
	case *AssertExprClause:
		f.writeExprIndented(v.Expr, indent)
	case *AssertCommandClause:
		if v.Negate {
			f.b.WriteString("not ")
		}
		f.b.WriteString(v.Head)
		for _, arg := range v.Args {
			f.b.WriteByte(' ')
			f.writeCommandArg(arg)
		}
	default:
		fmt.Fprintf(&f.b, "<%T>", clause)
	}
}

func (f *sourceFormatter) writeExpr(expr Expr) {
	f.b.WriteString(FormatExprSource(expr))
}

func (f *sourceFormatter) writeExprIndented(expr Expr, indent int) {
	f.b.WriteString(FormatExprSourceIndented(expr, indent))
}

func (f *sourceFormatter) writeCommandArg(expr Expr) {
	f.b.WriteString(formatCommandArg(expr))
}

func (f *sourceFormatter) writeIndent(indent int) {
	for range indent {
		f.b.WriteString(indentUnit)
	}
}

func (f *sourceFormatter) writeTrivia(startLine, endLine int) {
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(f.lines) {
		endLine = len(f.lines)
	}
	for line := startLine; line <= endLine; line++ {
		text := f.lines[line-1]
		if strings.TrimSpace(text) == "" || isCommentOnlyLine(text) {
			f.b.WriteString(text)
			f.b.WriteByte('\n')
		}
	}
}

func (f *sourceFormatter) writeInteriorComments(startLine, endLine, indent int) {
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(f.lines) {
		endLine = len(f.lines)
	}
	for line := startLine; line <= endLine; line++ {
		text := f.lines[line-1]
		if comment := lineComment(text); comment != "" {
			f.writeIndent(indent)
			f.b.WriteString(comment)
			f.b.WriteByte('\n')
		}
	}
}

func (f *sourceFormatter) writeLineComment(line int) {
	if line < 1 || line > len(f.lines) {
		return
	}
	text := f.lines[line-1]
	if isCommentOnlyLine(text) {
		return
	}
	if comment := lineComment(text); comment != "" {
		f.b.WriteByte(' ')
		f.b.WriteString(comment)
	}
}

func (f *sourceFormatter) hasContinuation(startLine, endLine int) bool {
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(f.lines) {
		endLine = len(f.lines)
	}
	for line := startLine; line < endLine; line++ {
		if strings.HasSuffix(strings.TrimRight(f.lines[line-1], " \t"), "\\") {
			return true
		}
	}
	return false
}

func (f *sourceFormatter) elseLine(v *IfStmt) int {
	if len(v.Else) == 0 {
		return v.End.Line
	}
	first := NodeSpan(v.Else[0])
	if first.Pos.Line > 1 {
		return first.Pos.Line - 1
	}
	return v.End.Line
}

func splitSourceLines(src string) []string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	lines := strings.Split(src, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func isCommentOnlyLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

func lineComment(line string) string {
	stripped := stripComment(line)
	if stripped == line {
		return ""
	}
	for i := range line {
		if line[i] == '#' && stripped[i] == ' ' {
			return strings.TrimSpace(line[i:])
		}
	}
	return ""
}

func commandArgLineGroups(args []Expr) [][]Expr {
	if len(args) == 0 {
		return nil
	}
	var groups [][]Expr
	currentLine := NodeSpan(args[0]).Pos.Line
	groups = append(groups, []Expr{args[0]})
	for _, arg := range args[1:] {
		line := NodeSpan(arg).Pos.Line
		if line != currentLine {
			groups = append(groups, []Expr{arg})
			currentLine = line
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], arg)
	}
	return groups
}

func formatCommandArg(expr Expr) string {
	switch expr.(type) {
	case *LiteralExpr, *VarRefExpr, *AdapterExpr, *InterpStringExpr, *ListExpr:
		return FormatExprSource(expr)
	default:
		return "(" + FormatExprSource(expr) + ")"
	}
}
