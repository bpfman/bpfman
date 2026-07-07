package syntax

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// Parse turns a token stream into a *Program. Every parse error
// carries a source location derived from the offending token.
func Parse(tokens []Token) (*Program, error) {
	p := &parser{tokens: tokens}
	stmts, err := p.parseStmts(p.atEOF)
	if err != nil {
		return nil, err
	}

	var start source.Pos
	if len(tokens) > 0 {
		start = tokens[0].Pos
	}
	prog := &Program{Stmts: stmts, Span: p.spanFrom(start)}
	if err := validateLocs(prog); err != nil {
		return nil, err
	}

	return prog, nil
}

// validateLocs walks every node of prog and asserts that each
// has a non-zero source.Pos (both line and column populated). The
// position infrastructure is end-to-end -- the tokeniser
// builds source.Pos{Line, Col} from a lineStarts table, every AST
// node copies its source.Pos from a token, and the renderers print
// 'file:line:col:' for diagnostics. A regression that adds a
// new AST variant without copying its source position would
// silently land an empty source.Pos on user-facing error messages;
// validateLocs catches that at parse time so the next
// developer to introduce the gap sees a loud failure rather
// than a quiet column drop.
//
// Program nodes are skipped when they have no Stmts: an empty
// input is a valid parse with an empty source.Pos, and we do not
// want to reject empty programs. Every other node, including
// the Program of a non-empty input, must have Line > 0 and
// Col > 0.
func validateLocs(prog *Program) error {
	var missing []string
	Inspect(prog, func(n Node) bool {
		if n == nil {
			return true
		}
		if p, ok := n.(*Program); ok && len(p.Stmts) == 0 {
			return true
		}
		sp := NodeSpan(n)
		switch {
		case sp.Pos.Line == 0 || sp.Pos.Col == 0:
			missing = append(missing, fmt.Sprintf("%T missing start (line=%d col=%d)",
				n, sp.Pos.Line, sp.Pos.Col))
		case sp.End.Line == 0 || sp.End.Col == 0:
			missing = append(missing, fmt.Sprintf("%T missing end (start=%d:%d, end=%d:%d)",
				n, sp.Pos.Line, sp.Pos.Col, sp.End.Line, sp.End.Col))
		}
		return true
	})
	if len(missing) > 0 {
		return fmt.Errorf("internal: AST node(s) with incomplete source spans: %s",
			strings.Join(missing, ", "))
	}
	return nil
}

// NodeSpan returns the source.Span value embedded in n. Every AST type
// embeds source.Span as an anonymous struct field; reflect over
// n to find that field. Used by validateLocs to enforce the
// position-completeness invariant; the rest of the code reaches
// source.Span through concrete-type access.
func NodeSpan(n Node) source.Span {
	v := reflect.ValueOf(n)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return source.Span{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return source.Span{}
	}
	for i := 0; i < v.NumField(); i++ {
		if sp, ok := v.Field(i).Interface().(source.Span); ok {
			return sp
		}
	}
	return source.Span{}
}

func identFromToken(tok Token) Ident {
	return Ident{Text: tok.Text, Span: tok.Span}
}

// parser is the recursive-descent state: a token stream and a
// cursor. All navigation goes through peek/advance so the cursor
// stays consistent with what has been consumed.
type parser struct {
	tokens []Token
	pos    int
}

func (p *parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() Token {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *parser) atEOF() bool {
	return p.pos >= len(p.tokens)
}

// spanFrom builds a source.Span starting at start and ending at the End of
// the most recently consumed token. Use at AST construction sites
// so every node carries its full source extent: callers capture the
// first token's source.Pos before parsing the construct, then call
// spanFrom once construction is complete. When no tokens have been
// consumed (start of input) the End collapses to start so the source.Span
// is well-formed but empty-width.
func (p *parser) spanFrom(start source.Pos) source.Span {
	if p.pos == 0 {
		return source.Span{Pos: start, End: start}
	}
	return source.Span{Pos: start, End: p.tokens[p.pos-1].End}
}

func (p *parser) atBlockClose() bool {
	t := p.peek()
	return t.Kind == TokenWord && t.Text == "}"
}

// parseStmts consumes statements until isEnd returns true or the
// token stream is exhausted. Separators between statements are
// skipped. Used for both the program root and block bodies.
func (p *parser) parseStmts(isEnd func() bool) ([]Stmt, error) {
	var stmts []Stmt
	for {
		for !p.atEOF() && p.peek().Kind == TokenSep {
			p.pos++
		}
		if p.atEOF() || isEnd() {
			break
		}
		before := p.pos
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}

		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		// Forward-progress guard: every parseStmt call must either
		// return an error or consume at least one token. Without
		// this guard a parser branch that silently returns (nil,
		// nil) without advancing causes an infinite loop here. The
		// guard converts that class of bug into an actionable parse
		// error at the offending token rather than a hang.
		if stmt == nil && p.pos == before {
			t := p.peek()
			return nil, spanErrorf(t.Span, "unexpected %q", t.Text)
		}
	}
	return stmts, nil
}

func (p *parser) parseStmt() (Stmt, error) {
	t := p.peek()
	if t.Kind == TokenWord {
		switch t.Text {
		case "if":
			return p.parseIfStmt()
		case "let":
			return p.parseLetStmt()
		case "foreach":
			return p.parseForEachStmt()
		case "retry":
			return p.parseRetryStmt()
		case "poll":
			return p.parsePollStmt()
		case "return":
			return p.parseReturnStmt()
		case "break":
			return p.parseBreakStmt()
		case "continue":
			return p.parseContinueStmt()
		case "guard":
			return p.parseGuardStmt()
		case "defer":
			return p.parseDeferStmt()
		case "def":
			return p.parseDefStmt()
		case "assert", "require":
			return p.parseAssertStmt(t.Text == "require")
		}
	}
	if leadsExpression(t) {
		return p.parseExprStmt()
	}
	if t.Kind == TokenWord && t.Text == "[" {
		return nil, spanErrorf(t.Span, "list literal at statement position is not allowed")
	}
	return p.parseCommandStmt()
}

func isAssertCommandHead(text string) bool {
	switch text {
	case "ok", "fail":
		return true
	}
	return false
}

func (p *parser) parseAssertClause(keywordTok Token) (AssertClause, error) {
	var buf []Token
	depth := 0
	for !p.atEOF() {
		t := p.peek()
		if t.Kind == TokenSep {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			break
		}
		if t.Kind == TokenWord && (t.Text == "{" || t.Text == "}") {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			if t.Text == "{" && recordLiteralTail(buf) {
				var err error
				buf, err = p.appendRecordBlockTokens(buf)
				if err != nil {
					return nil, err
				}
				continue
			}
			if t.Text == "{" && matchesBlockTail(buf) {
				var err error
				buf, err = p.appendMatchesBlockTokens(buf)
				if err != nil {
					return nil, err
				}
				continue
			}
			break
		}
		if t.Kind == TokenAssign {
			return nil, spanErrorf(t.Span, "unexpected '='; use \"let <name> = <value...>\" for assignment")
		}
		if t.Kind == TokenWord {
			switch t.Text {
			case "(", "[":
				depth++
			case ")", "]":
				if depth > 0 {
					depth--
				}
			}
		}
		buf = append(buf, t)
		p.pos++
	}
	if len(buf) == 0 {
		return nil, spanErrorf(keywordTok.Span, "%s requires an assertion target", keywordTok.Text)
	}

	negate := false
	bodyStart := 0
	if buf[0].Kind == TokenWord && buf[0].Text == "not" {
		negate = true
		bodyStart = 1
		if len(buf) == 1 {
			return nil, spanErrorf(buf[0].Span, "expected a form after \"not\"")
		}
	}

	if bodyStart < len(buf) && buf[bodyStart].Kind == TokenWord && isAssertCommandHead(buf[bodyStart].Text) {
		args, err := parseCommandArgs(buf[bodyStart+1:])
		if err != nil {
			return nil, err
		}

		return &AssertCommandClause{
			Head:     buf[bodyStart].Text,
			HeadSpan: buf[bodyStart].Span,
			Args:     args,
			Negate:   negate,
		}, nil
	}

	expr, err := parseExpression(buf)
	if err != nil {
		return nil, WrapError(keywordTok.Text, err)
	}

	return &AssertExprClause{Expr: expr}, nil
}

// parseAssertStmt consumes "assert"/"require" followed by one
// assertion clause, returning an AssertStmt. The parser always
// commits to the assertion lane at the keyword; clause parsing
// then decides between the expression and transitional
// command-shaped forms.
func (p *parser) parseAssertStmt(isRequire bool) (Stmt, error) {
	keywordTok := p.advance()
	clause, err := p.parseAssertClause(keywordTok)
	if err != nil {
		return nil, err
	}

	return &AssertStmt{
		IsRequire: isRequire,
		Clause:    clause,
		Span:      p.spanFrom(keywordTok.Pos),
	}, nil
}

// leadsExpression reports whether a token can only start an
// expression at statement position. These tokens would otherwise
// be mis-routed into the command-statement grammar and produce
// unhelpful errors (unknown command names that are actually
// variable references, quoted literals, bracketed expressions,
// etc.). Bare WORDs are excluded because they are the normal
// command-name form; the few WORD texts that can only appear in
// expression position ("(", "not", and the unary predicates) are
// listed explicitly.
func leadsExpression(t Token) bool {
	switch t.Kind {
	case TokenVarRef, TokenQuoted, TokenInterpString, TokenAdapterRef:
		return true
	case TokenWord:
		switch t.Text {
		case "(", "not", "not-empty", "true", "false":
			return true
		}
	}
	return false
}

// parseExprStmt consumes the current statement as an expression
// and wraps it in an ExprStmt. The tokens between the current
// cursor and the next separator (or end-of-input) are collected
// and handed to parseExpression verbatim, so every construct the
// expression grammar understands -- comparisons, logical
// combinators, threading, unary predicates, parens -- works
// unchanged at the top level.
func (p *parser) parseExprStmt() (Stmt, error) {
	startLoc := p.peek().Pos
	tokens, err := p.takeStmtTokens(false)
	if err != nil {
		return nil, err
	}

	if len(tokens) == 0 {
		return nil, LocErrorf(startLoc, "empty expression statement")
	}

	expr, err := parseExpression(tokens)
	if err != nil {
		return nil, err
	}

	return &ExprStmt{Expr: expr, Span: p.spanFrom(startLoc)}, nil
}

func (p *parser) parseBreakStmt() (Stmt, error) {
	t := p.advance()
	if err := p.rejectTrailingArgs("break"); err != nil {
		return nil, err
	}

	return &BreakStmt{Span: p.spanFrom(t.Pos)}, nil
}

func (p *parser) parseContinueStmt() (Stmt, error) {
	t := p.advance()
	if err := p.rejectTrailingArgs("continue"); err != nil {
		return nil, err
	}

	return &ContinueStmt{Span: p.spanFrom(t.Pos)}, nil
}

// rejectTrailingArgs errors when a bare-keyword statement
// (break, continue) has extra tokens on the same statement
// before the next separator or block marker. Silent tolerance
// would let "break 2" tokenise as if "break" were a command,
// which is not what the user wrote.
func (p *parser) rejectTrailingArgs(name string) error {
	if p.atEOF() {
		return nil
	}
	t := p.peek()
	if t.Kind == TokenSep {
		return nil
	}
	if t.Kind == TokenWord && (t.Text == "{" || t.Text == "}") {
		return nil
	}
	return spanErrorf(t.Span, "%s takes no arguments; got %q", name, t.Text)
}

func (p *parser) parseLetStmt() (Stmt, error) {
	letTok := p.advance() // "let"
	if p.atEOF() {
		return nil, spanErrorf(letTok.Span, "let requires: let <name> = <expr> or let <name> <- <command...> or let (<a> <b> ...) = <expr>")
	}
	if t := p.peek(); t.Kind == TokenWord && t.Text == "(" {
		// Parenthesised name list. Two surface forms share this
		// shape: the supported let-destructure `let (a b ...) =
		// EXPR` and the removed tuple-bind spelling after '<-'.
		// The sigil after the closing ')' selects the outcome.
		names, openTok, err := p.parseParenNameList("let")
		if err != nil {
			return nil, err
		}

		if p.atEOF() {
			return nil, spanErrorf(openTok.Span, "let: expected '=' or '<-' after name list")
		}
		switch p.peek().Kind {
		case TokenAssign:
			if allUnderscore(names) {
				return nil, spanErrorf(openTok.Span, "let: all destructure slots are '_'; at least one must bind")
			}
			p.advance() // "="
			rhsTokens, err := p.takeStmtTokens(true)
			if err != nil {
				return nil, err
			}

			if len(rhsTokens) == 0 {
				return nil, spanErrorf(letTok.Span, "let requires: let (<names>) = <value...>")
			}

			rhs, err := parseExpression(rhsTokens)
			if err != nil {
				return nil, err
			}

			return &LetDestructureStmt{Names: names, RHS: rhs, Span: p.spanFrom(letTok.Pos)}, nil
		case TokenBind:
			return nil, spanErrorf(openTok.Span, "tuple bind after '<-' is no longer supported; bind a single outcome name and use named fields")
		default:
			return nil, spanErrorf(openTok.Span, "let: expected '=' or '<-' after name list, got %q", p.peek().Text)
		}
	}
	if p.peek().Kind != TokenWord {
		return nil, spanErrorf(letTok.Span, "let requires an identifier, got %q", p.peek().Text)
	}
	nameTok := p.advance()
	name := identFromToken(nameTok)
	if !isIdent(name.Text) {
		return nil, spanErrorf(nameTok.Span, "invalid variable name: %q", name.Text)
	}
	if p.atEOF() {
		return nil, spanErrorf(letTok.Span, "let requires '=' or '<-' after the name")
	}
	switch p.peek().Kind {
	case TokenAssign:
		p.advance() // "="
		// `_` is consistently a discard slot at every binding
		// site (bind, destructure, foreach); single-name
		// `let _ = ...` is rejected: force-evaluation for side
		// effects belongs in bind / guard / a bare command, not
		// `let _ = ...`.
		if name.Text == "_" {
			return nil, spanErrorf(nameTok.Span, "single-name let cannot bind '_'; use a real name")
		}
		rhsTokens, err := p.takeStmtTokens(true)
		if err != nil {
			return nil, err
		}

		if len(rhsTokens) == 0 {
			return nil, spanErrorf(letTok.Span, "let requires: let <name> = <value...>")
		}

		rhs, err := parseExpression(rhsTokens)
		if err != nil {
			return nil, err
		}

		return &LetStmt{Name: name, RHS: rhs, Span: p.spanFrom(letTok.Pos)}, nil
	case TokenBind:
		return p.parseBindRHS(letTok.Pos, name, false)
	default:
		return nil, spanErrorf(letTok.Span, "let requires '=' or '<-' after the name, got %q", p.peek().Text)
	}
}

// parseGuardStmt parses "guard NAME <- COMMAND". The form is fixed:
// the keyword, a single identifier, the bind sigil '<-', then a
// non-empty command form. There is no "guard NAME = EXPR" spelling.
// parseDeferStmt parses "defer COMMAND". The RHS is a command
// form; argument evaluation happens at run time when the defer
// statement executes (registering the captured invocation), and
// the command itself dispatches at scope exit. There is no
// 'defer { ... }' block form in v1.
func (p *parser) parseDeferStmt() (Stmt, error) {
	deferTok := p.advance() // "defer"
	cmdTokens, err := p.takeStmtTokens(false)
	if err != nil {
		return nil, err
	}

	if len(cmdTokens) == 0 {
		return nil, spanErrorf(deferTok.Span, "defer requires a command form")
	}

	args, err := parseCommandArgs(cmdTokens)
	if err != nil {
		return nil, err
	}

	cmd := &CommandStmt{Args: args, Span: p.spanFrom(cmdTokens[0].Pos)}
	return &DeferStmt{Cmd: cmd, Span: p.spanFrom(deferTok.Pos)}, nil
}

func (p *parser) parseGuardStmt() (Stmt, error) {
	guardTok := p.advance() // "guard"
	if p.atEOF() {
		return nil, spanErrorf(guardTok.Span, "guard requires: guard <name> <- <command...>")
	}
	if t := p.peek(); t.Kind == TokenWord && t.Text == "(" {
		return nil, spanErrorf(t.Span, "tuple bind after '<-' is no longer supported; bind a single outcome name and use named fields")
	}
	if p.peek().Kind != TokenWord {
		return nil, spanErrorf(guardTok.Span, "guard requires an identifier, got %q", p.peek().Text)
	}
	nameTok := p.advance()
	name := identFromToken(nameTok)
	if !isIdent(name.Text) {
		return nil, spanErrorf(nameTok.Span, "invalid variable name: %q", name.Text)
	}
	if p.atEOF() || p.peek().Kind != TokenBind {
		return nil, spanErrorf(guardTok.Span, "guard requires: guard <name> <- <command...> (missing '<-')")
	}
	return p.parseBindRHS(guardTok.Pos, name, true)
}

// parseParenNameList consumes a parenthesised whitespace-separated
// name list shared by let-destructure, let-tuple, and guard-tuple
// sites. The opening '(' is at the cursor on entry; the closing
// ')' is consumed before returning.
//
// Rules:
//   - whitespace separator only; tokens containing ',' are rejected
//     so the previous comma-separated spelling fails loudly
//   - identifiers and '_' allowed; '_' is exempt from duplicate
//     checking because it does not bind
//   - duplicate real names are rejected
//   - empty list and single-name list are rejected because the
//     binding design refuses implicit single-name parens at
//     non-def sites; the single-name spelling drops the parens
//
// Per-site constraints beyond "at least two names" (e.g. the bind
// tuple's exactly-two rule, the let-destructure's
// all-underscore rejection) are applied by the caller after this
// helper returns. The returned openTok carries the source span of
// the opening '(' so call-site diagnostics point at the right token.
// site prefixes error messages (`let` or `guard`).
func (p *parser) parseParenNameList(site string) ([]Ident, Token, error) {
	openTok := p.advance() // "("
	var names []Ident
	seen := make(map[string]bool)
	for {
		for !p.atEOF() && p.peek().Kind == TokenSep {
			p.pos++
		}
		if p.atEOF() {
			return nil, openTok, spanErrorf(openTok.Span, "%s: unterminated name list (missing ')')", site)
		}
		t := p.peek()
		if t.Kind == TokenWord && t.Text == ")" {
			p.advance()
			break
		}
		if t.Kind != TokenWord {
			return nil, openTok, spanErrorf(t.Span, "%s: expected name, got %q", site, t.Text)
		}
		if strings.ContainsRune(t.Text, ',') {
			return nil, openTok, spanErrorf(t.Span, "%s: comma is not a separator; use whitespace (got %q)", site, t.Text)
		}
		if t.Text != "_" && !isIdent(t.Text) {
			return nil, openTok, spanErrorf(t.Span, "%s: invalid name %q", site, t.Text)
		}
		if t.Text != "_" {
			if seen[t.Text] {
				return nil, openTok, spanErrorf(t.Span, "%s: duplicate name %q", site, t.Text)
			}
			seen[t.Text] = true
		}
		names = append(names, identFromToken(t))
		p.advance()
	}
	if len(names) < 2 {
		return nil, openTok, spanErrorf(openTok.Span, "%s: parenthesised name list requires at least two names; the single-name spelling drops the parens", site)
	}
	return names, openTok, nil
}

// allUnderscore reports whether every element of names is "_".
// Callers use this to enforce the binding-design rule that a
// multi-name group must establish at least one real binding.
func allUnderscore(names []Ident) bool {
	for _, n := range names {
		if n.Text != "_" {
			return false
		}
	}
	return true
}

// parseBindRHS consumes the '<-' sigil and the command form that
// follows, returning a BindStmt. The RHS extends to the next
// statement separator or block marker; a stray '=' or '<-' inside
// the RHS is rejected. parseCommandArgs handles the command-form
// tokens so every primary expression the command-statement grammar
// accepts works on the right of a bind.
func (p *parser) parseBindRHS(stmtLoc source.Pos, target Ident, guard bool) (Stmt, error) {
	bindTok := p.advance() // "<-"
	// Bind-collect form: 'let X <- foreach NAME in LIST { BODY }'.
	// The 'foreach' keyword in RHS position triggers a separate
	// parse path because the body is a block and the existing
	// takeBindRHSTokens stops at '{'. Tuple bind targets and the
	// guard prefix carry through unchanged.
	if !p.atEOF() && p.peek().Kind == TokenWord && p.peek().Text == "foreach" {
		feStmt, err := p.parseForEachStmt()
		if err != nil {
			return nil, err
		}

		fe := feStmt.(*ForEachStmt)
		if len(fe.Body) == 0 {
			return nil, spanErrorf(bindTok.Span, "bind-collect: foreach body must produce a command at its last statement")
		}
		last := fe.Body[len(fe.Body)-1]
		if _, ok := last.(*CommandStmt); !ok {
			return nil, spanErrorf(NodeSpan(last), "bind-collect: foreach body's last statement must be a command (got %s); the last statement is the iteration's producer", describeStmt(last))
		}
		return &BindStmt{
			Target:  target,
			Collect: fe,
			Guard:   guard,
			Span:    p.spanFrom(stmtLoc),
		}, nil
	}
	cmdTokens, err := p.takeBindRHSTokens(bindTok)
	if err != nil {
		return nil, err
	}

	args, err := parseCommandArgs(cmdTokens)
	if err != nil {
		return nil, err
	}

	cmd := &CommandStmt{Args: args, Span: p.spanFrom(cmdTokens[0].Pos)}
	return &BindStmt{Target: target, Cmd: cmd, Guard: guard, Span: p.spanFrom(stmtLoc)}, nil
}

// describeStmt returns a short human-readable name for a
// statement kind, used in error messages so the user sees "got
// let" rather than "got *shell.LetStmt".
func describeStmt(s Stmt) string {
	switch s.(type) {
	case *LetStmt:
		return "let"
	case *BindStmt:
		return "bind"
	case *DeferStmt:
		return "defer"
	case *IfStmt:
		return "if"
	case *ForEachStmt:
		return "foreach"
	case *PollStmt:
		return "poll"
	case *RetryStmt:
		return "retry"
	case *BreakStmt:
		return "break"
	case *ContinueStmt:
		return "continue"
	case *DefStmt:
		return "def"
	case *AssertStmt:
		return "assert"
	case *ExprStmt:
		return "expression"
	}
	return fmt.Sprintf("%T", s)
}

// takeBindRHSTokens collects the tokens that form the command on the
// right of a '<-' bind. The run terminates at the next separator or
// block marker; nested '=' or '<-' on the RHS are rejected at the
// offending token. Newlines inside a '[...]' list literal are
// transparent: while bracket depth is positive the collector treats
// them as part of the buffer so a long list can wrap across lines
// (parseListLiteral already skips TokenSep tokens between elements).
func (p *parser) takeBindRHSTokens(bindTok Token) ([]Token, error) {
	var buf []Token
	var depth int
	for !p.atEOF() {
		t := p.peek()
		if t.Kind == TokenSep {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			break
		}
		if t.Kind == TokenWord && (t.Text == "{" || t.Text == "}") {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			break
		}
		if t.Kind == TokenAssign {
			return nil, spanErrorf(t.Span, "unexpected '=' on bind RHS; the right of '<-' must be a command form")
		}
		if t.Kind == TokenBind {
			return nil, spanErrorf(t.Span, "unexpected '<-' on bind RHS; chain via separate let/guard statements")
		}
		if t.Kind == TokenWord {
			switch t.Text {
			case "[", "(":
				depth++
			case "]", ")":
				if depth > 0 {
					depth--
				}
			}
		}
		buf = append(buf, t)
		p.pos++
	}
	if len(buf) == 0 {
		return nil, spanErrorf(bindTok.Span, "bind requires a command after '<-'")
	}
	return buf, nil
}

// takeStmtTokens collects tokens belonging to the current statement
// up to the next separator or block marker. When rejectAssign is
// true a stray TokenAssign inside the collected range is an error,
// used on a let RHS to catch "let x = a = b". Newlines inside a
// '[...]' list literal are transparent: while bracket depth is
// positive the collector treats them as part of the buffer so a
// long list can wrap across lines.
func (p *parser) takeStmtTokens(rejectAssign bool) ([]Token, error) {
	var buf []Token
	var depth int
	for !p.atEOF() {
		t := p.peek()
		if t.Kind == TokenSep {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			break
		}
		if t.Kind == TokenWord && (t.Text == "{" || t.Text == "}") {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			if t.Text == "{" && recordLiteralTail(buf) {
				var err error
				buf, err = p.appendRecordBlockTokens(buf)
				if err != nil {
					return nil, err
				}
				continue
			}
			if t.Text == "{" && matchesBlockTail(buf) {
				var err error
				buf, err = p.appendMatchesBlockTokens(buf)
				if err != nil {
					return nil, err
				}
				continue
			}
			break
		}
		if rejectAssign && t.Kind == TokenAssign {
			return nil, spanErrorf(t.Span, "unexpected '=' in let RHS")
		}
		if t.Kind == TokenWord {
			switch t.Text {
			case "[", "(":
				depth++
			case "]", ")":
				if depth > 0 {
					depth--
				}
			}
		}
		buf = append(buf, t)
		p.pos++
	}
	return buf, nil
}

func (p *parser) parseCommandStmt() (Stmt, error) {
	first := p.peek()
	startLoc := first.Pos
	// A bare `{` or `}` at statement position is not the start of a
	// command (parseStmt has already routed if/foreach/retry/...
	// keywords away from here), so reject it explicitly. Returning
	// (nil, nil) without consuming the token would let parseStmts
	// loop forever on the same token; this surfaces a real parse
	// error at the offending location instead.
	if first.Kind == TokenWord && (first.Text == "{" || first.Text == "}") {
		return nil, spanErrorf(first.Span, "unexpected %q at statement start", first.Text)
	}
	var buf []Token
	depth := 0
	for !p.atEOF() {
		t := p.peek()
		// Statement terminators (Sep, `{`, `}`) only end the
		// command at top-level paren/bracket depth. Inside a
		// CommandParenArg `(EXPR)` or CommandListArg `[EXPR]`
		// the inner expression grammar owns the contents,
		// including any `matches { ... }` tail it folds in via
		// the ComparisonExpr operator.
		if t.Kind == TokenSep {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			break
		}
		if t.Kind == TokenWord && (t.Text == "{" || t.Text == "}") {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			break
		}
		if t.Kind == TokenAssign {
			return nil, spanErrorf(t.Span, "unexpected '='; use \"let <name> = <value...>\" for assignment")
		}
		if t.Kind == TokenWord {
			switch t.Text {
			case "(", "[":
				depth++
			case ")", "]":
				if depth > 0 {
					depth--
				}
			}
		}
		buf = append(buf, t)
		p.pos++
	}
	if len(buf) == 0 {
		return nil, nil
	}

	args, err := parseCommandArgs(buf)
	if err != nil {
		return nil, err
	}
	return &CommandStmt{Args: args, Span: p.spanFrom(startLoc)}, nil
}

// reservedDefNames lists identifiers that cannot be used as a def
// name because the parser routes them away from the command-statement
// grammar. Allowing a def to shadow these would either be unreachable
// (the keyword wins at parseStmt) or break statement parsing.
var reservedDefNames = map[string]bool{
	"def":      true,
	"defer":    true,
	"let":      true,
	"guard":    true,
	"if":       true,
	"elif":     true,
	"else":     true,
	"foreach":  true,
	"in":       true,
	"retry":    true,
	"poll":     true,
	"return":   true,
	"break":    true,
	"continue": true,
	"assert":   true,
	"require":  true,
	"and":      true,
	"or":       true,
	"not":      true,
	"matches":  true,
	"true":     true,
	"false":    true,
}

// parseDefStmt parses a `def NAME(P1 P2 ...) { BODY }` declaration.
// The body is parsed eagerly via parseBlock so a syntactically broken
// body fails at declaration time and the def is never installed.
// Parameter names must be identifiers other than "_" and must be
// unique within the declaration.
func (p *parser) parseDefStmt() (Stmt, error) {
	defTok := p.advance() // "def"
	if p.atEOF() || p.peek().Kind != TokenWord {
		return nil, spanErrorf(defTok.Span, "def requires: def <name>(<params>) { ... }")
	}
	if p.peek().Text == "(" {
		return nil, spanErrorf(defTok.Span, "def requires a name before '('")
	}
	nameTok := p.advance()
	name := identFromToken(nameTok)
	if !isIdent(name.Text) {
		return nil, spanErrorf(nameTok.Span, "invalid def name: %q", name.Text)
	}
	if reservedDefNames[name.Text] {
		return nil, spanErrorf(nameTok.Span, "cannot use reserved word %q as a def name", name.Text)
	}
	if p.atEOF() || !(p.peek().Kind == TokenWord && p.peek().Text == "(") {
		return nil, spanErrorf(defTok.Span, "def requires '(' after the name")
	}
	p.advance() // "("
	params, err := p.parseDefParams(defTok.Pos)
	if err != nil {
		return nil, err
	}

	// Skip separators between ')' and '{'.
	for !p.atEOF() && p.peek().Kind == TokenSep {
		p.pos++
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, WrapError(fmt.Sprintf("def %s", name.Text), err)
	}

	return &DefStmt{Name: name, Params: params, Body: body, Span: p.spanFrom(defTok.Pos)}, nil
}

// parseReturnStmt consumes "return EXPR". The expression is
// mandatory: bare `return` is rejected to keep the construct
// uniformly value-publishing; if a value-less early-exit form is
// ever wanted, it earns its place separately. The parser does not
// know whether it is inside a def body; "return outside a def" is
// caught by the static checker and again at runtime.
func (p *parser) parseReturnStmt() (Stmt, error) {
	retTok := p.advance() // "return"
	tokens, err := p.takeStmtTokens(false)
	if err != nil {
		return nil, err
	}

	if len(tokens) == 0 {
		return nil, spanErrorf(retTok.Span, "return requires an expression: return EXPR")
	}

	expr, err := parseExpression(tokens)
	if err != nil {
		return nil, WrapError("return", err)
	}

	return &ReturnStmt{Expr: expr, Span: p.spanFrom(retTok.Pos)}, nil
}

// parseDefParams consumes the parameter list up to and including the
// closing ')'. Parameters are whitespace-separated identifiers; an
// empty list (immediately closing ')') is permitted. Duplicate
// parameter names are rejected, and "_" is rejected explicitly so
// def params do not remain the one binding site where underscore is
// treated as an ordinary name. Commas are not a separator at any
// binding site, including this one: a token whose text contains ','
// (which can happen because the tokeniser does not split on ',') is
// rejected with a clear error so the migration from the previous
// comma-separated spelling fails loudly rather than silently
// accepting `def f(a, b)` as `def f(a, b)` with a glued-comma name.
func (p *parser) parseDefParams(defLoc source.Pos) ([]DefParam, error) {
	var params []DefParam
	seen := make(map[string]bool)
	for {
		// Allow newlines/semis inside the parameter list so a long
		// def signature can wrap.
		for !p.atEOF() && p.peek().Kind == TokenSep {
			p.pos++
		}
		if p.atEOF() {
			return nil, LocErrorf(defLoc, "def: unterminated parameter list (missing ')')")
		}
		t := p.peek()
		if t.Kind == TokenWord && t.Text == ")" {
			p.advance()
			return params, nil
		}
		if t.Kind != TokenWord {
			return nil, spanErrorf(t.Span, "def: expected parameter name, got %q", t.Text)
		}
		if strings.ContainsRune(t.Text, ',') {
			return nil, spanErrorf(t.Span, "def: comma is not a parameter separator; use whitespace (got %q)", t.Text)
		}
		// An annotated parameter follows the record-field
		// convention: the name token carries a trailing colon
		// and the type is the next word. A colon anywhere else
		// in the token is the glued spelling, rejected so
		// "a:number" fails loudly rather than parsing as a
		// strange name.
		name := t.Text
		annotated := false
		if strings.ContainsRune(name, ':') {
			if !strings.HasSuffix(name, ":") || strings.Count(name, ":") != 1 {
				return nil, spanErrorf(t.Span, "def: parameter annotation needs a space after the colon; write it as %q", strings.Replace(name, ":", ": ", 1))
			}
			name = strings.TrimSuffix(name, ":")
			annotated = true
		}
		if !isIdent(name) {
			return nil, spanErrorf(t.Span, "def: invalid parameter name %q", name)
		}
		if name == "_" {
			return nil, spanErrorf(t.Span, "def parameters cannot bind '_'; use a real name or drop the slot")
		}
		if seen[name] {
			return nil, spanErrorf(t.Span, "def: duplicate parameter name %q", name)
		}
		seen[name] = true
		nameTok := t
		p.advance()

		param := DefParam{Name: Ident{Text: name, Span: nameTok.Span}}
		if annotated {
			for !p.atEOF() && p.peek().Kind == TokenSep {
				p.pos++
			}
			if p.atEOF() || p.peek().Kind != TokenWord || p.peek().Text == ")" {
				return nil, spanErrorf(nameTok.Span, "def: parameter %q: annotation requires a type (number, string, bool)", name)
			}
			typeTok := p.peek()
			validType := slices.Contains(DefParamTypes, typeTok.Text)
			if !validType {
				return nil, spanErrorf(typeTok.Span, "def: parameter %q: unknown parameter type %q (expected one of: number, string, bool)", name, typeTok.Text)
			}
			param.Type = typeTok.Text
			p.advance()
		}
		params = append(params, param)
	}
}

// parsePollStmt parses
//
//	poll timeout DUR every DUR { BODY }
//
// timeout and every are mandatory to keep polling cadence
// explicit in the source.
func (p *parser) parsePollStmt() (Stmt, error) {
	pollTok := p.advance() // "poll"
	timeout, every, err := p.parsePollClauses(pollTok)
	if err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, WrapError("poll", err)
	}

	return &PollStmt{
		Timeout: timeout,
		Every:   every,
		Body:    body,
		Span:    p.spanFrom(pollTok.Pos),
	}, nil
}

func (p *parser) parsePollClauses(pollTok Token) (time.Duration, time.Duration, error) {
	if p.atEOF() || p.peek().Kind != TokenWord || p.peek().Text != "timeout" {
		return 0, 0, spanErrorf(pollTok.Span, "poll requires 'timeout DUR every DUR' (e.g. 'poll timeout 5s every 100ms { ... }')")
	}
	p.advance() // "timeout"
	timeout, err := p.parseDurationWord(pollTok, "poll", "timeout")
	if err != nil {
		return 0, 0, err
	}

	if p.atEOF() || p.peek().Kind != TokenWord || p.peek().Text != "every" {
		return 0, 0, spanErrorf(pollTok.Span, "poll requires 'every DUR' after timeout")
	}
	p.advance() // "every"
	every, err := p.parseDurationWord(pollTok, "poll", "every")
	if err != nil {
		return 0, 0, err
	}

	return timeout, every, nil
}

// parseDurationWord reads the next Word token as a Go duration
// literal. owner/clause name the surrounding construct for
// diagnostics.
func (p *parser) parseDurationWord(ownerTok Token, owner, clause string) (time.Duration, error) {
	if p.atEOF() || p.peek().Kind != TokenWord {
		return 0, spanErrorf(ownerTok.Span, "%s %s requires a duration literal (e.g. 5s, 100ms)", owner, clause)
	}
	durTok := p.advance()
	d, err := time.ParseDuration(durTok.Text)
	if err != nil {
		return 0, spanErrorf(durTok.Span, "%s %s: %v", owner, clause, err)
	}

	if d <= 0 {
		return 0, spanErrorf(durTok.Span, "%s %s: %q is not a positive duration", owner, clause, durTok.Text)
	}

	return d, nil
}

// parseRetryStmt parses:
//
//	retry
//	retry MSG
//	retry unless EXPR
//	retry MSG unless EXPR
func (p *parser) parseRetryStmt() (Stmt, error) {
	retryTok := p.advance() // "retry"
	tokens, err := p.takeStmtTokens(false)
	if err != nil {
		return nil, err
	}

	var msgTokens, unlessTokens []Token
	for i, t := range tokens {
		if t.Kind == TokenWord && t.Text == "unless" {
			msgTokens = tokens[:i]
			unlessTokens = tokens[i+1:]
			goto build
		}
	}
	msgTokens = tokens

build:
	var message Expr
	if len(msgTokens) > 0 {
		expr, err := parseExpression(msgTokens)
		if err != nil {
			return nil, WrapError("retry", err)
		}

		message = expr
	}
	var unless Expr
	if unlessTokens != nil {
		if len(unlessTokens) == 0 {
			return nil, spanErrorf(retryTok.Span, "retry unless requires a condition expression")
		}
		expr, err := parseExpression(unlessTokens)
		if err != nil {
			return nil, WrapError("retry", err)
		}

		unless = expr
	}
	return &RetryStmt{
		Message: message,
		Unless:  unless,
		Span:    p.spanFrom(retryTok.Pos),
	}, nil
}

func (p *parser) parseForEachStmt() (Stmt, error) {
	feTok := p.advance() // "foreach"
	names, err := p.parseForEachNames(feTok)
	if err != nil {
		return nil, err
	}

	if p.atEOF() || p.peek().Kind != TokenWord || p.peek().Text != "in" {
		return nil, spanErrorf(feTok.Span, "foreach requires 'in' after the loop variable")
	}
	p.advance() // "in"
	listTokens, err := p.takeUntilOpenBrace()
	if err != nil {
		return nil, err
	}

	if len(listTokens) == 0 {
		return nil, spanErrorf(feTok.Span, "foreach requires: foreach <name> in <expr> { ... }")
	}

	list, err := parseExpression(listTokens)
	if err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &ForEachStmt{Names: names, List: list, Body: body, Span: p.spanFrom(feTok.Pos)}, nil
}

// parseForEachNames reads the loop-variable name list that follows
// the 'foreach' keyword. Two surface forms:
//
//   - single-var: 'foreach NAME in LIST { ... }'. NAME is a bare
//     identifier or '_' (the iterate-for-side-effects idiom).
//   - destructure: 'foreach (NAME NAME { NAME }) in LIST { ... }'.
//     At least two names, whitespace-separated; parens are
//     required because a bare 'foreach a b in xs' would read as a
//     command-shaped name list rather than a binding.
//
// Commas are not a separator at this site. A token whose text
// contains ',' is rejected with an explicit error so a stray
// 'foreach a, b in xs' fails loudly rather than silently mis-parsing.
func (p *parser) parseForEachNames(feTok Token) ([]Ident, error) {
	if p.atEOF() || p.peek().Kind != TokenWord {
		return nil, spanErrorf(feTok.Span, "foreach requires: foreach <name> in <expr> { ... }  |  foreach (<name1> <name2> ...) in <expr> { ... }")
	}
	if p.peek().Text == "(" {
		return p.parseForEachDestructureNames(feTok)
	}
	name, err := p.parseForEachNameToken(feTok)
	if err != nil {
		return nil, err
	}

	return []Ident{name}, nil
}

// parseForEachDestructureNames consumes a parenthesised name list:
// '(' Name Name { Name } ')'. Single-name parens and the empty
// shape are rejected because the design refuses implicit
// single-name parens at non-def binding sites; the single-var
// 'foreach x in xs' is the canonical spelling for one loop
// variable. Newlines and semicolons inside the parens are
// transparent so a long list can wrap.
func (p *parser) parseForEachDestructureNames(feTok Token) ([]Ident, error) {
	openTok := p.advance() // "("
	var names []Ident
	seen := make(map[string]bool)
	for {
		for !p.atEOF() && p.peek().Kind == TokenSep {
			p.pos++
		}
		if p.atEOF() {
			return nil, spanErrorf(openTok.Span, "foreach: unterminated name list (missing ')')")
		}
		t := p.peek()
		if t.Kind == TokenWord && t.Text == ")" {
			p.advance()
			break
		}
		nameTok := t
		name, err := p.parseForEachNameToken(feTok)
		if err != nil {
			return nil, err
		}

		if name.Text != "_" {
			if seen[name.Text] {
				return nil, spanErrorf(nameTok.Span, "foreach: duplicate name %q", name.Text)
			}
			seen[name.Text] = true
		}
		names = append(names, name)
	}
	if len(names) < 2 {
		return nil, spanErrorf(openTok.Span, "foreach: parenthesised name list requires at least two names; use 'foreach x in ...' for single-var iteration")
	}
	allDiscard := true
	for _, n := range names {
		if n.Text != "_" {
			allDiscard = false
			break
		}
	}
	if allDiscard {
		return nil, spanErrorf(feTok.Span, "foreach: all loop variables are '_'; at least one must bind")
	}
	return names, nil
}

// parseForEachNameToken consumes one loop-variable name. The 'in'
// keyword is rejected here so it is reachable as the terminator at
// the single-var call site. Tokens whose text contains ',' are
// rejected explicitly so the old 'foreach a, b in xs' spelling
// fails with a clear diagnostic.
func (p *parser) parseForEachNameToken(feTok Token) (Ident, error) {
	if p.atEOF() || p.peek().Kind != TokenWord {
		return Ident{}, spanErrorf(feTok.Span, "foreach: expected variable name, got end of input")
	}
	t := p.advance()
	if strings.ContainsRune(t.Text, ',') {
		return Ident{}, spanErrorf(t.Span, "foreach: comma is not a separator; use whitespace and wrap multi-var lists in parens (got %q)", t.Text)
	}
	if t.Text == "in" {
		return Ident{}, spanErrorf(t.Span, "foreach requires a variable name before 'in'")
	}
	if t.Text == "_" {
		return identFromToken(t), nil
	}
	if !isIdent(t.Text) {
		return Ident{}, spanErrorf(t.Span, "invalid variable name: %q", t.Text)
	}
	return identFromToken(t), nil
}

// takeUntilOpenBrace collects tokens up to (but not including) the
// next '{'. Separator tokens inside the range are skipped so
// multi-line list expressions work. Returns an error if no '{'
// appears before EOF.
func (p *parser) takeUntilOpenBrace() ([]Token, error) {
	var buf []Token
	depth := 0
	for !p.atEOF() {
		t := p.peek()
		if t.Kind == TokenSep {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			p.pos++
			continue
		}
		if t.Kind == TokenWord && t.Text == "{" {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			if recordLiteralTail(buf) {
				var err error
				buf, err = p.appendRecordBlockTokens(buf)
				if err != nil {
					return nil, err
				}
				continue
			}
			if matchesBlockTail(buf) {
				var err error
				buf, err = p.appendMatchesBlockTokens(buf)
				if err != nil {
					return nil, err
				}
				continue
			}
			return buf, nil
		}
		if t.Kind == TokenWord && t.Text == "}" {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			return nil, spanErrorf(t.Span, "unexpected '}' before '{'")
		}
		if t.Kind == TokenWord {
			switch t.Text {
			case "(", "[":
				depth++
			case ")", "]":
				if depth > 0 {
					depth--
				}
			}
		}
		buf = append(buf, t)
		p.pos++
	}
	return nil, fmt.Errorf("expected '{' after expression")
}

func (p *parser) parseIfStmt() (Stmt, error) {
	ifTok := p.advance() // "if"
	cond, err := p.parseCondition()
	if err != nil {
		return nil, WrapError("if", err)
	}

	then, err := p.parseBlock()
	if err != nil {
		return nil, WrapError("if", err)
	}

	// Capture the end at each block's closing brace before the
	// elif/else lookahead below advances past trailing separators, so
	// the statement span ends at `}` and a comment following the block
	// is not pulled inside it.
	end := p.spanFrom(ifTok.Pos).End
	var elifs []IfBranch
	var els []Stmt
	for {
		for !p.atEOF() && p.peek().Kind == TokenSep {
			p.pos++
		}
		if p.atEOF() {
			break
		}
		t := p.peek()
		if t.Kind != TokenWord {
			break
		}
		switch t.Text {
		case "elif":
			elifTok := p.advance()
			ec, err := p.parseCondition()
			if err != nil {
				return nil, WrapError("elif", err)
			}

			eb, err := p.parseBlock()
			if err != nil {
				return nil, WrapError("elif", err)
			}

			branch := p.spanFrom(elifTok.Pos)
			elifs = append(elifs, IfBranch{Cond: ec, Body: eb, Span: branch})
			end = branch.End
		case "else":
			p.advance()
			eb, err := p.parseBlock()
			if err != nil {
				return nil, WrapError("else", err)
			}

			els = eb
			end = p.spanFrom(ifTok.Pos).End
			return &IfStmt{Cond: cond, Then: then, Elifs: elifs, Else: els, Span: source.Span{Pos: ifTok.Pos, End: end}}, nil
		default:
			return &IfStmt{Cond: cond, Then: then, Elifs: elifs, Else: els, Span: source.Span{Pos: ifTok.Pos, End: end}}, nil
		}
	}
	return &IfStmt{Cond: cond, Then: then, Elifs: elifs, Else: els, Span: source.Span{Pos: ifTok.Pos, End: end}}, nil
}

// parseCondition collects tokens up to the next `{` and parses them
// as an expression. The `{` is not consumed.
func (p *parser) parseCondition() (Expr, error) {
	var buf []Token
	depth := 0
	for !p.atEOF() {
		t := p.peek()
		if t.Kind == TokenSep {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			p.pos++
			continue
		}
		if t.Kind == TokenWord && t.Text == "{" {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			if recordLiteralTail(buf) {
				var err error
				buf, err = p.appendRecordBlockTokens(buf)
				if err != nil {
					return nil, err
				}
				continue
			}
			if matchesBlockTail(buf) {
				var err error
				buf, err = p.appendMatchesBlockTokens(buf)
				if err != nil {
					return nil, err
				}
				continue
			}
			break
		}
		if t.Kind == TokenWord && t.Text == "}" {
			if depth > 0 {
				buf = append(buf, t)
				p.pos++
				continue
			}
			break
		}
		if t.Kind == TokenWord {
			switch t.Text {
			case "(", "[":
				depth++
			case ")", "]":
				if depth > 0 {
					depth--
				}
			}
		}
		buf = append(buf, t)
		p.pos++
	}
	if depth > 0 {
		return nil, fmt.Errorf("unmatched '(' in condition; expected matching ')' before '{'")
	}
	if p.atEOF() || !(p.peek().Kind == TokenWord && p.peek().Text == "{") {
		return nil, fmt.Errorf("expected '{' after condition")
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("expected condition before '{'")
	}
	return parseExpression(buf)
}

// parseBlock consumes a `{` ... `}` block and returns its parsed
// statements. Nested blocks balance naturally via parseStmts.
func (p *parser) parseBlock() ([]Stmt, error) {
	if p.atEOF() || !(p.peek().Kind == TokenWord && p.peek().Text == "{") {
		return nil, fmt.Errorf("expected '{'")
	}
	p.advance()
	stmts, err := p.parseStmts(p.atBlockClose)
	if err != nil {
		return nil, err
	}

	if p.atEOF() || !(p.peek().Kind == TokenWord && p.peek().Text == "}") {
		return nil, fmt.Errorf("unterminated block: missing '}'")
	}
	p.advance()
	return stmts, nil
}

// parseExpression parses an expression via a cursor-based
// recursive-descent parser. Each precedence level has its own
// method, loosest to tightest:
//
//	parseComparison     -- binary comparison (==, !=, <, <=, >, >=)
//	                       and the matches operator
//	parseAdditive       -- '+' / '-' left-associative
//	parseMultiplicative -- '*' / '/' / '%' left-associative
//	parsePredicate      -- unary predicate (not-empty, true, false)
//	parseNegate         -- unary '-' right-associative
//	parseThread         -- threading chain (|>)
//	parseTerm           -- primary token (literal, varref, adapter,
//	                                      cmdsub)
//
// Each level calls the next-tighter level for its operands and
// loops for any left-associative operator of its own. The shape
// makes errors self-locating: a mismatched token triggers an
// error from the level that was expecting something else, and
// trailing tokens after a complete expression get a single
// "unexpected token" message at the outer call.
func parseExpression(tokens []Token) (Expr, error) {
	tokens = stripSeps(tokens)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	ep := &exprParser{tokens: tokens}
	e, err := ep.parseOr()
	if err != nil {
		return nil, err
	}

	if !ep.eof() {
		t := ep.peek()
		if hint, ok := smushedArithmeticHint(t); ok {
			return nil, spanErrorf(t.Span, "unexpected %q after expression; %s", t.Text, hint)
		}
		return nil, spanErrorf(t.Span, "unexpected %q after expression", t.Text)
	}
	return e, nil
}

// smushedArithmeticHint returns a user-facing hint when the
// trailing token looks like a binary '-' or '/' fused to its
// right operand (e.g. "-1", "/2"). The tokeniser keeps '-' and
// '/' as word-constituents because they appear inside negative
// literals, flags, and paths, so the common "$x -1" / "$x /2"
// shapes tokenise as two adjacent primaries rather than as
// binary arithmetic. When that shape is the reason parsing
// failed, point at whitespace explicitly.
func smushedArithmeticHint(t Token) (string, bool) {
	if t.Kind != TokenWord || len(t.Text) < 2 {
		return "", false
	}
	c := t.Text[0]
	if c != '-' && c != '/' {
		return "", false
	}
	next := t.Text[1]
	isOperand := (next >= '0' && next <= '9') || next == '.' || next == '$' ||
		(next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || next == '_'
	if !isOperand {
		return "", false
	}
	return fmt.Sprintf("arithmetic '%c' requires whitespace (e.g. \"%c %s\" not %q)", c, c, t.Text[1:], t.Text), true
}

// parseInterpBody turns the raw contents of a "${...}"
// interpolation into the Expr that will be evaluated at run time.
// Three accepted shapes:
//
//   - bare name with optional path: "name", "name.path",
//     "name[0]". Treated as a variable reference; the user
//     does not write "$name" inside the braces.
//   - sigil-led expression: "$n * 2", "$count + 1",
//     "$x |> jq .y".
//   - literal-led expression: "4 * 2", "1 + $count",
//     "(3 + 4) * 2", "true and $flag".
//
// The literal-led form is the bash $((...))-equivalent for
// inline arithmetic in command args: 'print "${4 * 2}"'
// reaches it. Without this branch, command args could only
// reach the expression evaluator via a named intermediate
// ('let x = 4 * 2; print $x'), which is correct but verbose.
// Inside braces is the right place for the relaxation because
// the surrounding double-quoted string already disambiguates
// the syntactic context.
func parseInterpBody(inner string, span source.Span) (Expr, error) {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return nil, spanErrorf(span, "empty interpolation")
	}
	// Compute the original-source position of trimmed[0]. The
	// outer span starts at '$', so the body's first byte is
	// two columns past source.Pos (skip "${"). Walk any leading
	// whitespace inside the body to land trimmedStart on the
	// first non-whitespace byte. The walk handles multi-line
	// bodies (rare but legal) by tracking newlines.
	bodyStart := source.Pos{File: span.Pos.File, Line: span.Pos.Line, Col: span.Pos.Col + 2}
	leadingWS := inner[:len(inner)-len(strings.TrimLeft(inner, " \t\n\r"))]
	trimmedStart := advancePos(bodyStart, leadingWS)

	// Bare-name shortcut: "${name}" / "${name.path}" /
	// "${name[0]}" tokenise (after a synthesised "$") to a
	// single TokenVarRef. Use that fast path so the common
	// case stays a simple VarRefExpr; the source.Span covers the
	// trimmed body in the original source -- excluding the
	// "${" / "}" wrappers -- so the caret underlines just the
	// name.
	if trimmed[0] != '$' {
		synthStart := trimmedStart
		synthStart.Col--
		if tokens, err := TokeniseAt(synthStart, "$"+trimmed); err == nil &&
			len(tokens) == 1 && tokens[0].Kind == TokenVarRef {
			t := tokens[0]
			return &VarRefExpr{
				Name: t.VarName,
				Path: t.VarPath,
				Span: source.Span{
					Pos: trimmedStart,
					End: advancePos(trimmedStart, trimmed),
				},
			}, nil
		}
		// Not a bare-name reference: fall through to the
		// general expression path below, which parses the
		// original (un-prefixed) body. This is what makes
		// literal-led expressions like "${4 * 2}" work.
	}
	tokens, err := TokeniseAt(trimmedStart, trimmed)
	if err != nil {
		return nil, spanErrorf(span, "string interpolation ${%s}: %v", inner, err)
	}

	expr, ok := tryParseExpression(tokens)
	if !ok {
		return nil, spanErrorf(span, "string interpolation ${%s}: not a valid expression", inner)
	}

	return expr, nil
}

// advancePos walks s as if appended after start, returning the
// position after the last byte. Newlines reset the column to 1
// and bump the line; everything else advances the column by one.
// Used by parseInterpBody to convert byte counts (leading
// whitespace, full trimmed-body length) into source.Pos coordinates so
// inner-parsed spans translate back to the original source.
func advancePos(start source.Pos, s string) source.Pos {
	pos := normalizeStartPos(start)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			pos.Line++
			pos.Col = 1
		} else {
			pos.Col++
		}
	}
	return pos
}

// tryParseExpression attempts to interpret tokens as a single
// expression. It returns (expr, true) only when the expression
// grammar matches and every non-separator token is consumed; any
// parse error or trailing token returns (nil, false). Used by
// string-interpolation parsing to decide whether a ${...} body is a
// valid expression.
func tryParseExpression(tokens []Token) (Expr, bool) {
	e, err := parseExpression(tokens)
	if err != nil {
		return nil, false
	}

	return e, true
}

// exprParser is a cursor over a pre-collected token slice used by
// parseExpression's recursive-descent methods. Each level calls
// the next-tighter level and loops for any left-associative
// operator of its own.
type exprParser struct {
	tokens []Token
	pos    int
}

func (p *exprParser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{}
	}
	return p.tokens[p.pos]
}

func (p *exprParser) advance() Token {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *exprParser) eof() bool {
	return p.pos >= len(p.tokens)
}

// spanFrom returns source.Span{start, end-of-last-consumed-token} so every
// expression node carries its full source extent. Mirrors the
// statement parser's helper.
func (p *exprParser) spanFrom(start source.Pos) source.Span {
	if p.pos == 0 {
		return source.Span{Pos: start, End: start}
	}
	return source.Span{Pos: start, End: p.tokens[p.pos-1].End}
}

// spanFromNodeStart builds a source.Span that starts at n's own source
// start and ends at the most recently consumed token. Used for
// left-associative infix expressions so the resulting node covers
// the full expression text, not just the operator token onward.
func (p *exprParser) spanFromNodeStart(n Node) source.Span {
	return p.spanFrom(NodeSpan(n).Pos)
}

// parseOr recognises left-associative 'or' chains. 'or' is the
// loosest logical connective; it binds looser than 'and' and
// looser than the comparison level. Short-circuit evaluation is
// handled at eval time.
func (p *exprParser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	for !p.eof() && isKeywordWord(p.peek(), "or") {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}

		left = &LogicalExpr{Op: "or", Left: left, Right: right, Span: p.spanFromNodeStart(left)}
	}
	return left, nil
}

// parseAnd recognises left-associative 'and' chains. 'and' is
// tighter than 'or' and looser than 'not'.
func (p *exprParser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}

	for !p.eof() && isKeywordWord(p.peek(), "and") {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}

		left = &LogicalExpr{Op: "and", Left: left, Right: right, Span: p.spanFromNodeStart(left)}
	}
	return left, nil
}

// parseNot recognises the 'not' prefix. It binds tighter than
// 'and' / 'or' but looser than the comparison level, matching
// SQL and Python conventions (so "not $a == $b" parses as
// "not ($a == $b)", not "(not $a) == $b"). Multiple 'not's are
// accepted via right-associative recursion.
func (p *exprParser) parseNot() (Expr, error) {
	if isKeywordWord(p.peek(), "not") {
		notTok := p.advance()
		operand, err := p.parseNot()
		if err != nil {
			return nil, err
		}

		return &NotExpr{Operand: operand, Span: p.spanFrom(notTok.Pos)}, nil
	}
	return p.parseComparison()
}

// isKeywordWord reports whether t is a plain word token whose
// text equals kw. Used at precedence levels to recognise
// keyword operators (and / or / not) without colliding with
// tokens that happen to have the same text inside other positions.
func isKeywordWord(t Token, kw string) bool {
	return t.Kind == TokenWord && t.Text == kw
}

// parseComparison recognises the optional binary-comparison infix
// around a tighter sub-expression. At most one binary operator
// per expression matches the current grammar; anything else the
// caller flags via the "unexpected trailing token" check in
// parseExpression.
func (p *exprParser) parseComparison() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}

	if p.eof() {
		return left, nil
	}
	if isKeywordWord(p.peek(), "matches") {
		matchesTok := p.advance()
		exhaustive := false
		if isKeywordWord(p.peek(), "exhaustive") {
			exhaustive = true
			p.advance()
		}
		if p.eof() || !(p.peek().Kind == TokenWord && p.peek().Text == "{") {
			return nil, spanErrorf(matchesTok.Span, "matches requires a block: %s { ... }", "matches")
		}
		block, err := p.parseMatchesBlockExpr(matchesTok.Pos, exhaustive)
		if err != nil {
			return nil, err
		}

		return &MatchesExpr{Target: left, Block: block, Span: p.spanFromNodeStart(left)}, nil
	}
	op, ok := binaryOpFromToken(p.peek())
	if !ok {
		return left, nil
	}
	p.advance()
	right, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}

	return &BinaryExpr{Left: left, Op: op, Right: right, Span: p.spanFromNodeStart(left)}, nil
}

// parseAdditive recognises left-associative '+' and '-' chains.
// The operands live at the multiplicative level so that
// "1 + 2 * 3" parses as "1 + (2 * 3)". The '-' here is always
// binary subtraction; unary negation is handled at the negate
// level, below the predicate rung.
func (p *exprParser) parseAdditive() (Expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}

	for !p.eof() {
		t := p.peek()
		if t.Kind != TokenWord || (t.Text != "+" && t.Text != "-") {
			break
		}
		opTok := p.advance()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}

		left = &BinaryExpr{Left: left, Op: opTok.Text, Right: right, Span: p.spanFromNodeStart(left)}
	}
	return left, nil
}

// parseMultiplicative recognises left-associative '*', '/', and
// '%' chains. Operands live at the predicate level. Division
// by zero and non-numeric operands are caught at evaluation
// time, not here.
func (p *exprParser) parseMultiplicative() (Expr, error) {
	left, err := p.parsePredicate()
	if err != nil {
		return nil, err
	}

	for !p.eof() {
		t := p.peek()
		if t.Kind != TokenWord || (t.Text != "*" && t.Text != "/" && t.Text != "%") {
			break
		}
		opTok := p.advance()
		right, err := p.parsePredicate()
		if err != nil {
			return nil, err
		}

		left = &BinaryExpr{Left: left, Op: opTok.Text, Right: right, Span: p.spanFromNodeStart(left)}
	}
	return left, nil
}

// parsePredicate recognises a unary-predicate prefix applied to a
// primary operand. The only predicate is "not-empty";
// "true" and "false" are plain boolean literals. The rule is
// still context-sensitive in shape because the predicate word
// must actually have an operand to its right -- "not-empty" alone
// at end of input falls through to the tighter negate level
// where it is parsed as a literal.
func (p *exprParser) parsePredicate() (Expr, error) {
	if pred, ok := unaryPredFromToken(p.peek()); ok && p.operandFollowsPred() {
		predTok := p.advance()
		operand, err := p.parseTerm()
		if err != nil {
			return nil, err
		}

		return &UnaryExpr{Pred: pred, Operand: operand, Span: p.spanFrom(predTok.Pos)}, nil
	}
	return p.parseNegate()
}

// parseNegate recognises a unary '-' prefix. Right-associative
// recursion supports stacked negations ("- -$x"). The bare '-'
// WORD token is produced only when whitespace surrounds it;
// "-3" tokenises as a single WORD (a negative literal) and
// never reaches this rule.
func (p *exprParser) parseNegate() (Expr, error) {
	t := p.peek()
	if t.Kind == TokenWord && t.Text == "-" {
		negTok := p.advance()
		operand, err := p.parseNegate()
		if err != nil {
			return nil, err
		}

		return &NegateExpr{Operand: operand, Span: p.spanFrom(negTok.Pos)}, nil
	}
	return p.parseThread()
}

// operandFollowsPred reports whether the token immediately after
// the current one could syntactically be a unary predicate's
// operand. It rejects anything that belongs to a higher
// precedence level or ends the current expression: binary-
// comparison words, arithmetic operators, logical operators
// (and / or), '|>', a closing ')' that would terminate a
// parenthesised sub-expression, and end of input. That lets a
// pred word sitting at a comparison-RHS, arithmetic-RHS, or
// logical-RHS position parse as a literal instead of greedily
// swallowing the next token.
func (p *exprParser) operandFollowsPred() bool {
	if p.pos+1 >= len(p.tokens) {
		return false
	}
	next := p.tokens[p.pos+1]
	if next.Kind == TokenThread {
		return false
	}
	if _, isBinOp := binaryOpFromToken(next); isBinOp {
		return false
	}
	if isArithmeticOp(next) {
		return false
	}
	if isKeywordWord(next, "and") || isKeywordWord(next, "or") {
		return false
	}
	if next.Kind == TokenWord && next.Text == ")" {
		return false
	}
	return true
}

// isArithmeticOp reports whether t is a bare WORD carrying one
// of the five arithmetic operators. The tokeniser does not
// give these tokens a dedicated kind, so recognition is by
// text. Used at precedence boundaries to keep arithmetic
// operators from being absorbed as operands at a tighter level.
func isArithmeticOp(t Token) bool {
	if t.Kind != TokenWord {
		return false
	}
	switch t.Text {
	case "+", "-", "*", "/", "%":
		return true
	}
	return false
}

// parseThread consumes a primary then zero or more '|>
// command-call' segments, folding left-associatively into a
// chain of ThreadExprs. The RHS is read by parseThreadRHS,
// which stops at the next '|>' or a binary-op word so the
// comparison level can pick up operators at its own precedence.
func (p *exprParser) parseThread() (Expr, error) {
	lhs, err := p.parseTerm()
	if err != nil {
		return nil, err
	}

	for !p.eof() && p.peek().Kind == TokenThread {
		threadTok := p.advance()
		args, err := p.parseThreadRHS(threadTok.Pos)
		if err != nil {
			return nil, err
		}

		lhs = &ThreadExpr{LHS: lhs, Args: args, PipePos: threadTok.Pos, Span: p.spanFromNodeStart(lhs)}
	}
	return lhs, nil
}

// parseThreadRHS consumes the command-call tokens that follow a
// '|>'. The general rule is that the RHS extends to the end of
// the current expression, not blindly to end-of-input, so any
// token that begins a higher-precedence construct or closes the
// surrounding form terminates the command. Concretely it stops
// at: the next '|>' (so a chain of threads composes); a
// binary-comparison word; an arithmetic operator; a logical
// operator 'and' or 'or' (so a thread can sit inside a
// LogicalExpr); a closing bracket ')', ']', or '}' (so a thread
// nested inside a parenthesised expression, command
// substitution, or interpolation lets the enclosing form close);
// or end of input. A literal binary-op, arithmetic, logical, or
// bracket word used as a command argument must be quoted.
func (p *exprParser) parseThreadRHS(threadLoc source.Pos) ([]Expr, error) {
	var args []Expr
	for !p.eof() {
		t := p.peek()
		if t.Kind == TokenThread {
			break
		}
		if _, isBinOp := binaryOpFromToken(t); isBinOp {
			break
		}
		if isArithmeticOp(t) {
			break
		}
		if t.Kind == TokenWord && (t.Text == "and" || t.Text == "or") {
			break
		}
		if t.Kind == TokenWord && (t.Text == ")" || t.Text == "]" || t.Text == "}") {
			break
		}
		p.advance()
		e, err := parsePrimary(t)
		if err != nil {
			return nil, err
		}

		args = append(args, e)
	}
	if len(args) == 0 {
		return nil, LocErrorf(threadLoc, "thread requires a command on the right-hand side")
	}

	return args, nil
}

// parseTerm consumes one primary expression -- a single literal,
// varref, adapter, or command-substitution token, a
// parenthesised sub-expression that recurses back into the full
// expression grammar at the 'or' level, or a 'timeout DURATION'
// primary that evaluates to a boolean against the enclosing
// retry's elapsed-time clock.
func (p *exprParser) parseTerm() (Expr, error) {
	if p.eof() {
		return nil, fmt.Errorf("expected expression, got end of input")
	}
	t := p.peek()
	if t.Kind == TokenWord && t.Text == "(" {
		openTok := p.advance()
		if !p.eof() && p.peek().Kind == TokenWord && p.peek().Text == ")" {
			return nil, spanErrorf(openTok.Span, "empty parenthesised expression")
		}
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}

		if p.eof() || !(p.peek().Kind == TokenWord && p.peek().Text == ")") {
			return nil, spanErrorf(openTok.Span, "missing ')' to close parenthesised expression")
		}
		p.advance() // consume ')'
		return inner, nil
	}
	if t.Kind == TokenWord && t.Text == "[" {
		return p.parseListLiteral()
	}
	if t.Kind == TokenWord && t.Text == "record" {
		return p.parseRecordLiteral()
	}
	if t.Kind == TokenWord {
		if pb, ok := lookupPureBuiltin(t.Text); ok {
			// `null` is both a value literal and a unary predicate.
			// Parse it as a call only when a valid primary follows;
			// otherwise the bare word stays the literal null value.
			if pb.Name == "null" {
				nextPos := p.pos + 1
				if nextPos >= len(p.tokens) || !canStartPureCallArgToken(p.tokens[nextPos]) {
					p.advance()
					return parsePrimary(t)
				}
			}
			return p.parsePureCall(pb)
		}
	}
	if err := validateExpressionWordLiteral(t); err != nil {
		return nil, err
	}

	p.advance()
	return parsePrimary(t)
}

func validateExpressionWordLiteral(t Token) error {
	if t.Kind != TokenWord {
		return nil
	}
	if strings.ContainsRune(t.Text, ',') {
		return spanErrorf(t.Span, "unquoted comma in expression literal %q; quote it for string text or remove the comma", t.Text)
	}
	if startsNumericLiteral(t.Text) && !IsJSONNumber(t.Text) {
		if json.Valid([]byte(t.Text)) {
			return spanErrorf(t.Span, "numeric literal %q exceeds the representable range", t.Text)
		}
		return spanErrorf(t.Span, "invalid numeric literal %q; quote it as %q for string text", t.Text, t.Text)
	}
	return nil
}

func startsNumericLiteral(text string) bool {
	if text == "" {
		return false
	}
	if text[0] >= '0' && text[0] <= '9' {
		return true
	}
	if (text[0] == '-' || text[0] == '+') && len(text) > 1 {
		return text[1] >= '0' && text[1] <= '9'
	}
	return false
}

func canStartPureCallArgToken(t Token) bool {
	if t.Kind == TokenWord && t.Text == "(" {
		return true
	}
	if t.Kind == TokenWord && t.Text == "[" {
		return true
	}
	switch t.Kind {
	case TokenQuoted, TokenVarRef, TokenAdapterRef, TokenInterpString:
		return true
	case TokenWord:
		if _, isBinOp := binaryOpFromToken(t); isBinOp {
			return false
		}
		if isArithmeticOp(t) {
			return false
		}
		if isKeywordWord(t, "and") || isKeywordWord(t, "or") || isKeywordWord(t, "not") {
			return false
		}
		return true
	default:
		return false
	}
}

// parseRecordLiteral consumes a 'record' '{' NAME: EXPR ... '}'
// literal. Fields are whitespace-separated and each value parses as
// one primary expression via parseTerm; compound values use
// parentheses, matching list literal elements.
func (p *exprParser) parseRecordLiteral() (Expr, error) {
	recordTok := p.advance() // record
	for !p.eof() && p.peek().Kind == TokenSep {
		p.advance()
	}
	if p.eof() || !(p.peek().Kind == TokenWord && p.peek().Text == "{") {
		return nil, spanErrorf(recordTok.Span, "record literal requires '{'")
	}
	openTok := p.advance() // '{'
	var fields []RecordField
	seen := make(map[string]bool)
	for {
		for !p.eof() && p.peek().Kind == TokenSep {
			p.advance()
		}
		if p.eof() {
			return nil, spanErrorf(openTok.Span, "missing '}' to close record literal")
		}
		t := p.peek()
		if t.Kind == TokenWord && t.Text == "}" {
			p.advance()
			return &RecordExpr{Fields: fields, Span: p.spanFrom(recordTok.Pos)}, nil
		}
		if t.Kind == TokenWord && strings.ContainsRune(t.Text, ',') {
			return nil, spanErrorf(t.Span, "record fields are whitespace-separated; commas are not record separators")
		}
		if t.Kind != TokenWord || !strings.HasSuffix(t.Text, ":") {
			return nil, spanErrorf(t.Span, "record field must be written as name:")
		}
		name := strings.TrimSuffix(t.Text, ":")
		if !isIdent(name) {
			return nil, spanErrorf(t.Span, "invalid record field name %q", name)
		}
		if seen[name] {
			return nil, spanErrorf(t.Span, "duplicate record field %q", name)
		}
		seen[name] = true
		nameTok := p.advance()
		if p.eof() {
			return nil, spanErrorf(nameTok.Span, "record field %q requires a value", name)
		}
		value, err := p.parseTerm()
		if err != nil {
			return nil, err
		}

		fields = append(fields, RecordField{
			Name: name,
			Expr: value,
			Span: p.spanFrom(nameTok.Pos),
		})
	}
}

// parseListLiteral consumes a '[' EXPR EXPR ... ']' list literal.
// Elements are whitespace-separated and each element parses as a
// primary expression via parseTerm; compound expressions wrap in
// parens, matching every other expression context. An operator at
// element-boundary position (binary, thread, arithmetic, logical)
// is rejected with a hint to parenthesise the compound. Newlines
// inside the brackets are transparent so a long list can wrap
// across lines.
//
// Empty lists `[]` are accepted; they evaluate to a list Value of
// length zero. Used in shape-test contexts to compare against a
// known-empty collection (`assert $got.status.links == []`) where
// otherwise an alternative spelling via jq length would be needed.
func (p *exprParser) parseListLiteral() (Expr, error) {
	openTok := p.advance() // '['
	var elems []Expr
	for {
		// Newlines inside the brackets are transparent.
		for !p.eof() && p.peek().Kind == TokenSep {
			p.advance()
		}
		if p.eof() {
			return nil, spanErrorf(openTok.Span, "missing ']' to close list literal")
		}
		t := p.peek()
		if t.Kind == TokenWord && t.Text == "]" {
			p.advance() // ']'
			return &ListExpr{Elems: elems, Span: p.spanFrom(openTok.Pos)}, nil
		}
		if _, isBinOp := binaryOpFromToken(t); isBinOp {
			return nil, spanErrorf(t.Span, "unexpected %q between list elements; wrap a compound element in parens, e.g. [($x + 1) $y]", t.Text)
		}
		if isArithmeticOp(t) {
			return nil, spanErrorf(t.Span, "unexpected %q between list elements; wrap a compound element in parens, e.g. [($x + 1) $y]", t.Text)
		}
		if t.Kind == TokenThread {
			return nil, spanErrorf(t.Span, "unexpected '|>' between list elements; wrap a threaded element in parens, e.g. [($x |> jq \".id\") $y]")
		}
		if t.Kind == TokenWord && (t.Text == "and" || t.Text == "or") {
			return nil, spanErrorf(t.Span, "unexpected %q between list elements; wrap a compound element in parens", t.Text)
		}
		// Comma is not a tokeniser terminator (CLI arg payloads
		// like '--proceed-on ok,pipe,dispatcher_return' rely on
		// '-bearing barewords staying whole). That means a user
		// who writes [1, 2, 3] out of muscle memory would parse
		// silently as the bareword strings "1,", "2,", "3".
		// Catch any unquoted comma in element position and reject
		// it loudly; quoted strings ("a,b" as an element) escape
		// the check because TokenQuoted is not TokenWord.
		if t.Kind == TokenWord && strings.ContainsRune(t.Text, ',') {
			return nil, spanErrorf(t.Span, "unquoted comma in list literal; elements are whitespace-separated (try [1 2 3] not [1, 2, 3])")
		}
		elem, err := p.parseTerm()
		if err != nil {
			return nil, err
		}

		elems = append(elems, elem)
	}
}

// parsePureCall consumes a registered pure-builtin name followed
// by exactly pb.Arity primary arguments. Arguments are parsed as
// primaries (parsePrimary tokens or parenthesised sub-expressions),
// not as full expressions, so trailing operators bind to the
// surrounding expression rather than to the call. The rule keeps
// "range 5 + 1" parsing as "(range 5) + 1" and forces nested calls
// to be explicit via parens: "u32le (jq '.x' $v)".
func (p *exprParser) parsePureCall(pb pureBuiltinSpec) (Expr, error) {
	nameTok := p.advance()
	args := make([]Expr, 0, pb.Arity)
	for i := 0; i < pb.Arity; i++ {
		if p.eof() {
			return nil, spanErrorf(nameTok.Span, "%s: expected %d argument(s), got %d", pb.Name, pb.Arity, i)
		}
		arg, err := p.parsePureCallArg(pb.Name)
		if err != nil {
			return nil, err
		}

		args = append(args, arg)
	}
	return &PureCallExpr{Name: pb.Name, Args: args, Span: p.spanFrom(nameTok.Pos)}, nil
}

// parsePureCallArg accepts one primary argument for a pure-builtin
// call: a parenthesised sub-expression (full expression grammar
// inside), a list literal '[...]', a single literal / varref /
// adapter / interp-string token, or a sigil-led varref. Operators
// (and / or / not / + / - / * / / / % / |> / comparison) are not
// primaries and stop the argument list, leaving the outer
// expression to pick them up.
func (p *exprParser) parsePureCallArg(name string) (Expr, error) {
	t := p.peek()
	if t.Kind == TokenWord && t.Text == "(" {
		openTok := p.advance()
		if !p.eof() && p.peek().Kind == TokenWord && p.peek().Text == ")" {
			return nil, spanErrorf(openTok.Span, "empty parenthesised expression")
		}
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}

		if p.eof() || !(p.peek().Kind == TokenWord && p.peek().Text == ")") {
			return nil, spanErrorf(openTok.Span, "missing ')' to close parenthesised expression")
		}
		p.advance()
		return inner, nil
	}
	if t.Kind == TokenWord && t.Text == "[" {
		return p.parseListLiteral()
	}
	switch t.Kind {
	case TokenWord:
		if _, isBinOp := binaryOpFromToken(t); isBinOp {
			return nil, spanErrorf(t.Span, "%s: unexpected %q in argument position", name, t.Text)
		}
		if isArithmeticOp(t) {
			return nil, spanErrorf(t.Span, "%s: unexpected %q in argument position", name, t.Text)
		}
		if isKeywordWord(t, "and") || isKeywordWord(t, "or") || isKeywordWord(t, "not") {
			return nil, spanErrorf(t.Span, "%s: unexpected %q in argument position", name, t.Text)
		}
		if err := validateExpressionWordLiteral(t); err != nil {
			return nil, err
		}
	case TokenQuoted, TokenVarRef, TokenAdapterRef, TokenInterpString:
		// Recognised primary tokens; fall through to consume.
	default:
		return nil, spanErrorf(t.Span, "%s: unexpected %q in argument position", name, t.Text)
	}
	p.advance()
	return parsePrimary(t)
}

func (p *exprParser) parseMatchesBlockExpr(matchesLoc source.Pos, exhaustive bool) (*MatchesBlockExpr, error) {
	sub := &parser{tokens: p.tokens, pos: p.pos}
	block, err := sub.parseMatchesBlock(matchesLoc, exhaustive)
	if err != nil {
		return nil, err
	}

	p.pos = sub.pos
	return block, nil
}

// parseCommandArgs turns a command's token run into argument
// expressions. Each token becomes a primary expression; a stray
// TokenAssign is rejected with a "use let" hint because no command
// form expects a literal '=' as an argument.
//
// A leading '(' starts a parenthesised expression that runs through
// the same parser used by let RHSes and assert operands. The whole
// '(EXPR)' group becomes one argument whose value is computed at
// command-eval time; downstream evalArg evaluates the expression
// and wraps the resulting Value as a Scalar / StructuredValueArg.
// '|>' is recognised in argument position: 'print ($snap |> jq
// ".x")' parses the same way 'let v = $snap |> jq ".x"' does.
//
// A leading '[' starts a list literal arg the same way, dispatched
// to parseListLiteral via parseExpression. 'print [1 2 3]' produces
// one ListExpr argument rather than five separate literal tokens.
// A bare ')' or ']' outside any opening paren / bracket is a parse
// error; the tokeniser keeps them as their own tokens and the
// resulting "unmatched" diagnostic mirrors the opening-side check.
func parseCommandArgs(tokens []Token) ([]Expr, error) {
	exprs := make([]Expr, 0, len(tokens))
	i := 0
	for i < len(tokens) {
		t := tokens[i]
		if t.Kind == TokenAssign {
			return nil, spanErrorf(t.Span, "unexpected '='; use \"let <name> = <value...>\" for assignment")
		}
		if t.Kind == TokenWord && t.Text == "(" {
			end, err := findMatchingParen(tokens, i)
			if err != nil {
				return nil, err
			}

			inner := tokens[i+1 : end]
			if len(inner) == 0 || onlySeparators(inner) {
				return nil, spanErrorf(t.Span, "empty parenthesised expression in argument position")
			}

			e, err := parseExpression(inner)
			if err != nil {
				return nil, err
			}

			exprs = append(exprs, e)
			i = end + 1
			continue
		}
		if t.Kind == TokenWord && t.Text == ")" {
			return nil, spanErrorf(t.Span, "unmatched ')' in command argument")
		}
		if t.Kind == TokenWord && t.Text == "[" {
			end, err := findMatchingBracket(tokens, i)
			if err != nil {
				return nil, err
			}

			// parseExpression routes a run starting with '['
			// through parseTerm to parseListLiteral, so handing
			// it the whole '[ ... ]' slice (brackets included)
			// produces a ListExpr argument.
			e, err := parseExpression(tokens[i : end+1])
			if err != nil {
				return nil, err
			}

			exprs = append(exprs, e)
			i = end + 1
			continue
		}
		if t.Kind == TokenWord && t.Text == "]" {
			return nil, spanErrorf(t.Span, "unmatched ']' in command argument")
		}
		e, err := parsePrimary(t)
		if err != nil {
			return nil, err
		}

		exprs = append(exprs, e)
		i++
	}
	return exprs, nil
}

// findMatchingParen returns the index of the ')' that closes the
// '(' at openIdx. Tracks nested parens so '(zip $a (range 3))' is
// returned whole. An unmatched '(' is a parse error cited at the
// opening paren.
func findMatchingParen(tokens []Token, openIdx int) (int, error) {
	depth := 1
	for i := openIdx + 1; i < len(tokens); i++ {
		t := tokens[i]
		if t.Kind != TokenWord {
			continue
		}
		switch t.Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, spanErrorf(tokens[openIdx].Span, "unmatched '(' in command argument")
}

// findMatchingBracket returns the index of the ']' that closes
// the '[' at openIdx. Mirror of findMatchingParen: tracks nested
// '[' / ']' so '[[1] [2 3]]' is returned whole. An unmatched '['
// is a parse error cited at the opening bracket.
func findMatchingBracket(tokens []Token, openIdx int) (int, error) {
	depth := 1
	for i := openIdx + 1; i < len(tokens); i++ {
		t := tokens[i]
		if t.Kind != TokenWord {
			continue
		}
		switch t.Text {
		case "[":
			depth++
		case "]":
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, spanErrorf(tokens[openIdx].Span, "unmatched '[' in command argument")
}

// onlySeparators reports whether tokens is non-empty but contains
// only TokenSep entries (newlines, semicolons). Used by the
// argument-position '(EXPR)' check so '(\n)' is rejected as empty
// the same way '()' is, rather than reaching parseExpression's
// stripSeps and erroring with "empty expression" further from the
// user's source.
func onlySeparators(tokens []Token) bool {
	for _, t := range tokens {
		if t.Kind != TokenSep {
			return false
		}
	}
	return true
}

// parsePrimary converts a single token into a primary expression.
// Command substitutions are recursively parsed so their inner
// syntax is checked eagerly; errors inside the brackets surface at
// parse time rather than at eval time.
func parsePrimary(t Token) (Expr, error) {
	switch t.Kind {
	case TokenWord:
		if t.Text == "nil" {
			return nil, spanErrorf(t.Span, "nil has been removed; use null")
		}
		return &LiteralExpr{Text: t.Text, Span: t.Span}, nil
	case TokenQuoted:
		return &LiteralExpr{Text: t.Text, Quoted: true, Span: t.Span}, nil
	case TokenVarRef:
		return &VarRefExpr{Name: t.VarName, Path: t.VarPath, Span: t.Span}, nil
	case TokenAdapterRef:
		return &AdapterExpr{Adapter: t.Adapter, Name: t.VarName, Path: t.VarPath, Span: t.Span}, nil
	case TokenInterpString:
		segs := make([]InterpStringSegment, 0, len(t.Segments))
		for _, s := range t.Segments {
			if s.IsLit {
				segs = append(segs, InterpStringSegment{Literal: s.Literal})
				continue
			}
			expr, err := parseInterpBody(s.Inner, s.Span)
			if err != nil {
				return nil, err
			}

			segs = append(segs, InterpStringSegment{Expr: expr})
		}
		return &InterpStringExpr{Segments: segs, Span: t.Span}, nil
	default:
		return nil, spanErrorf(t.Span, "unexpected %q", t.Text)
	}
}

func unaryPredFromToken(t Token) (string, bool) {
	if t.Kind != TokenWord || !isUnaryPred(t.Text) {
		return "", false
	}
	return t.Text, true
}

func binaryOpFromToken(t Token) (string, bool) {
	if t.Kind != TokenWord || !isBinaryOp(t.Text) {
		return "", false
	}
	return t.Text, true
}

// stripSeps removes separator tokens from a flat slice. Used when
// folding multi-line condition expressions into a flat operand list.
func stripSeps(tokens []Token) []Token {
	out := make([]Token, 0, len(tokens))
	braceDepth := 0
	for _, t := range tokens {
		if t.Kind == TokenSep && braceDepth == 0 {
			continue
		}
		out = append(out, t)
		if t.Kind != TokenWord {
			continue
		}
		switch t.Text {
		case "{":
			braceDepth++
		case "}":
			if braceDepth > 0 {
				braceDepth--
			}
		}
	}
	return out
}

func matchesBlockTail(buf []Token) bool {
	switch {
	case len(buf) >= 2 &&
		buf[len(buf)-2].Kind == TokenWord && buf[len(buf)-2].Text == "matches" &&
		buf[len(buf)-1].Kind == TokenWord && buf[len(buf)-1].Text == "exhaustive":
		return true
	case len(buf) >= 1 &&
		buf[len(buf)-1].Kind == TokenWord && buf[len(buf)-1].Text == "matches":
		return true
	default:
		return false
	}
}

func recordLiteralTail(buf []Token) bool {
	return len(buf) >= 1 &&
		buf[len(buf)-1].Kind == TokenWord &&
		buf[len(buf)-1].Text == "record"
}

func (p *parser) appendRecordBlockTokens(buf []Token) ([]Token, error) {
	openTok := p.peek()
	depth := 0
	for !p.atEOF() {
		t := p.advance()
		buf = append(buf, t)
		if t.Kind != TokenWord {
			continue
		}
		switch t.Text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return buf, nil
			}
		}
	}
	return nil, spanErrorf(openTok.Span, "missing '}' to close record literal")
}

func (p *parser) appendMatchesBlockTokens(buf []Token) ([]Token, error) {
	openTok := p.peek()
	depth := 0
	for !p.atEOF() {
		t := p.advance()
		buf = append(buf, t)
		if t.Kind != TokenWord {
			continue
		}
		switch t.Text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return buf, nil
			}
		}
	}
	return nil, spanErrorf(openTok.Span, "unterminated matches block: missing '}'")
}

// SyntaxError is the typed error shape returned by Tokenise,
// Parse, and the runtime evaluators for every diagnosable
// problem. source.Span carries the offending region; Msg is the
// human-readable message; Cause optionally holds an underlying
// error so errors.Is and errors.As traversals walk through to
// any sentinel a command handler emitted. Error() renders the
// plain "line:col: message" form for string-only callers; the
// renderer-aware paths in cmd/bpfman-shell type-assert via
// errors.As and pull the source.Span directly so the rust-frame caret
// underlines the actual region.
type SyntaxError struct {
	// Span is the source region the diagnostic points at.
	Span source.Span

	// Msg is the human-readable message.
	Msg string

	// Cause is the optional underlying error, exposed via Unwrap so
	// errors.Is and errors.As reach any sentinel beneath the
	// wrapper.
	Cause error
}

// Error renders the diagnostic as "line:col: message", or just the
// message when no source position is set.
func (e *SyntaxError) Error() string {
	if e.Span.Pos.Line == 0 {
		return e.Msg
	}
	return fmt.Sprintf("%d:%d: %s", e.Span.Pos.Line, e.Span.Pos.Col, e.Msg)
}

// Unwrap exposes the underlying error so errors.Is and errors.As
// see through the SyntaxError wrapper. Runtime sentinels emitted
// from command handlers stay reachable via errors.Is(err, sentinel)
// after the safety-net wrap at statement boundaries; without
// Unwrap the wrap would erase pointer identity.
func (e *SyntaxError) Unwrap() error { return e.Cause }

// spanErrorf builds a *SyntaxError covering span. Use whenever
// the offending region is known as a source.Span (the common parser
// case: capture the first token's source.Span, parse the construct, then
// build the source.Span from first.Pos through the last consumed token's
// End).
func spanErrorf(span source.Span, format string, args ...any) error {
	return &SyntaxError{Span: span, Msg: fmt.Sprintf(format, args...)}
}

// SpanErrorf is the exported form of spanErrorf so cmd-side
// handlers (assert policy, builtins) can build *SyntaxError
// values with a source.Span and a formatted message without spelling
// out the literal. The internal package-local helper retains
// the lowercase name; this shim is API surface for the rest of
// the bpfman-shell program.
func SpanErrorf(span source.Span, format string, args ...any) error {
	return spanErrorf(span, format, args...)
}

// WrapError preserves a child *SyntaxError's source.Span while
// prepending a parent context prefix to its message. Without
// this, fmt.Errorf("%s: %w", prefix, err) builds a wrapper
// whose Error() string carries the prefix but whose underlying
// *SyntaxError.Msg does not, so renderer paths that pull se.Msg
// via errors.As silently lose the prefix. Falls through to a
// plain fmt.Errorf for non-SyntaxError children.
func WrapError(prefix string, err error) error {
	var se *SyntaxError
	if errors.As(err, &se) {
		return &SyntaxError{Span: se.Span, Msg: prefix + ": " + se.Msg, Cause: se.Cause}
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// spanCarrier marks errors that already carry their own source.Span
// and so should pass through frameAtSpan unchanged. cmd-side
// runtime-outcome errors (a subprocess exiting non-zero, future
// launch-failure variants) implement this so they reach the renderer
// as their concrete type instead of being re-wrapped as
// *SyntaxError. The renderer routes them to a citation shape; the
// rust-style frame stays reserved for parser/checker diagnostics and
// runtime errors that identify a wrong construct.
type spanCarrier interface {
	error
	SourceSpan() source.Span
}

// frameAtSpan attaches span to err so the renderer can frame the
// diagnostic at a known region. err is preserved as Cause so
// errors.Is/errors.As reach any sentinel underneath; if err is
// already a *SyntaxError the original source.Span is kept (the inner
// site knew better), and spanCarrier errors pass through untouched
// so they can be rendered as their own concrete type. Use at every
// point where a runtime error crosses a source.Span-bearing boundary --
// the command and bind statement evaluators, the program-level
// safety net, future builtin/assert dispatchers.
func frameAtSpan(span source.Span, err error) error {
	if err == nil {
		return nil
	}
	var se *SyntaxError
	if errors.As(err, &se) {
		return err
	}
	var sc spanCarrier
	if errors.As(err, &sc) {
		return err
	}
	return &SyntaxError{Span: span, Msg: err.Error(), Cause: err}
}

// FrameAt is the exported form of frameAtSpan. Use from cmd-side
// dispatchers (assert policy, builtins) that catch a non-typed
// error from a sub-call and want to frame it at the originating
// statement's source.Span.
func FrameAt(span source.Span, err error) error { return frameAtSpan(span, err) }

// LocErrorf builds a SyntaxError at a single source.Pos. The source.Span it
// produces is collapsed at loc; renderers will draw a one-column
// caret. Prefer spanErrorf where the full source.Span is reachable so
// frames cover the offending run.
func LocErrorf(loc source.Pos, format string, args ...any) error {
	return &SyntaxError{
		Span: source.Span{Pos: loc, End: loc},
		Msg:  fmt.Sprintf(format, args...),
	}
}
