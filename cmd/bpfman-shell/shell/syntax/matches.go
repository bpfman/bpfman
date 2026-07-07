package syntax

import (
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// isMatchesPredicate reports whether s is one of the bareword
// predicates the matches block recognises in pattern position.
// Mirrors the expression-form predicate set so `field: null`
// reads the same as `assert null $X.field`.
func isMatchesPredicate(s string) bool {
	switch s {
	case "not-empty", "null", "empty":
		return true
	}
	return false
}

// parseMatchesBlock parses the body of a `matches { ... }` block.
// The opening `{` token must be the next token in the stream.
//
// A matches block is line-oriented: each entry occupies its own
// line, and the only entry separator is the newline between them.
// `,` and `;` are both rejected with a dedicated diagnostic --
// the block is a table of path-pattern relations, not a sequence
// of statements, so the comma-list and semicolon-statement
// punctuations the rest of the language uses elsewhere do not
// apply here. See the def parameter list for the contrasting
// case where commas *are* required: parameters are a value list,
// not a table.
//
// exhaustive is set when the caller saw `matches exhaustive {`
// (vs the unadorned `matches {`). It propagates onto the
// resulting MatchesBlockExpr so the evaluator can additionally
// enforce structural coverage of the actual value at this level.
// Dotted paths inside an exhaustive block are rejected here at
// parse time: exhaustive mode requires structural nesting via
// `matches [exhaustive] { ... }` sub-blocks rather than dotted
// reach-across.
func (p *parser) parseMatchesBlock(matchesLoc source.Pos, exhaustive bool) (*MatchesBlockExpr, error) {
	if p.atEOF() || !(p.peek().Kind == TokenWord && p.peek().Text == "{") {
		return nil, LocErrorf(matchesLoc, "expected '{' after matches")
	}
	openTok := p.advance()
	expr := &MatchesBlockExpr{Exhaustive: exhaustive, Span: source.Span{Pos: openTok.Pos, End: openTok.End}}
	for {
		// Skip newline separators between entries. Multiple
		// consecutive newlines (blank lines inside the block) are
		// allowed.
		for !p.atEOF() && isMatchesSep(p.peek()) {
			p.pos++
		}
		if p.atEOF() {
			return nil, spanErrorf(openTok.Span, "unterminated matches block: missing '}'")
		}
		if p.peek().Kind == TokenSep && p.peek().Text == ";" {
			return nil, LocErrorf(p.peek().Pos, "matches: ';' is not a valid entry separator; entries are separated by newlines")
		}
		if p.peek().Kind == TokenWord && p.peek().Text == "," {
			return nil, LocErrorf(p.peek().Pos, "matches: ',' is not a valid entry separator; entries are separated by newlines")
		}
		if p.peek().Kind == TokenWord && p.peek().Text == "}" {
			closeTok := p.advance()
			expr.End = closeTok.End
			return expr, nil
		}
		entry, err := p.parseMatchEntry(exhaustive)
		if err != nil {
			return nil, err
		}
		expr.Entries = append(expr.Entries, entry)
	}
}

// isMatchesSep reports whether t is a newline separator inside a
// matches block. `;` is not -- a matches block is a table of
// path-pattern relations, not a sequence of statements.
func isMatchesSep(t Token) bool {
	return t.Kind == TokenSep && t.Text == "\n"
}

// parseMatchEntry parses one entry inside a matches block: a
// path, a `:` separator, and a pattern. The colon may be glued
// to the path word ("path:"), to the pattern's leading word
// (":pattern"), or stand alone as its own token. The pattern is
// one of:
//
//   - the bare `not-empty` keyword (lifted unary predicate)
//   - a nested `matches [exhaustive] { ... }` sub-block, which
//     recurses against the sub-value at the entry's path
//   - any other expression, whose evaluated value is compared
//     for equality against the value at the entry's path
//
// inExhaustive carries the surrounding block's exhaustive flag;
// when set, dotted paths are rejected at this entry with a hint
// pointing at the nested-sub-block form.
func (p *parser) parseMatchEntry(inExhaustive bool) (MatchEntry, error) {
	startPos := p.pos
	pathTok := p.peek()
	if p.atEOF() || pathTok.Kind != TokenWord {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: path must be a word, got %q", pathTok.Text)
	}
	if pathTok.Text == "{" {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: missing path before '{'")
	}

	var pathText string
	if strings.HasSuffix(pathTok.Text, ":") && pathTok.Text != ":" {
		pathText = pathTok.Text[:len(pathTok.Text)-1]
		p.pos++
	} else if pathTok.Text == ":" {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: missing path before ':'")
	} else {
		pathText = pathTok.Text
		p.pos++
		colonSeen := false
		for !p.atEOF() {
			next := p.peek()
			switch {
			case next.Kind == TokenWord && next.Text == ":":
				p.pos++
				colonSeen = true
			case next.Kind == TokenWord && strings.HasPrefix(next.Text, ":"):
				// Strip the leading ':' from next.Text in place by
				// rewriting the token. The cleanest way is to swap
				// the underlying token's text; since p.tokens is the
				// shared slice, mutate via index.
				tok := next
				tok.Text = next.Text[1:]
				if tok.Text == "" {
					p.pos++
				} else {
					p.tokens[p.pos] = tok
				}
				colonSeen = true
			case next.Kind == TokenWord && next.Text == "[":
				pathText += next.Text
				p.pos++
				if p.atEOF() || p.peek().Kind != TokenWord {
					return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: invalid path %q", pathText)
				}
				pathText += p.peek().Text
				p.pos++
				if p.atEOF() || p.peek().Kind != TokenWord || p.peek().Text != "]" {
					return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: invalid path %q", pathText)
				}
				pathText += p.peek().Text
				p.pos++
			case next.Kind == TokenWord && strings.HasPrefix(next.Text, ".") && strings.HasSuffix(next.Text, ":") && next.Text != ":":
				pathText += next.Text[:len(next.Text)-1]
				p.pos++
				colonSeen = true
			case next.Kind == TokenWord && strings.HasPrefix(next.Text, "."):
				pathText += next.Text
				p.pos++
			default:
				return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: missing ':' between path and pattern")
			}
			if colonSeen {
				break
			}
		}
		if !colonSeen {
			return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: missing ':' after path %q", pathText)
		}
	}

	if pathText == "" {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: empty path")
	}
	if !isValidPath(pathText) {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry: invalid path %q", pathText)
	}
	if inExhaustive && strings.ContainsAny(pathText, ".[") {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches exhaustive: dotted or indexed path %q is not allowed; use a nested 'matches [exhaustive] { ... }' sub-block instead", pathText)
	}

	// Pattern position. Three shapes are recognised:
	//   - `not-empty` alone
	//   - `matches [exhaustive] { ... }` sub-block
	//   - any other expression (line-oriented token collection)
	if p.atEOF() {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry %q: missing pattern after ':'", pathText)
	}

	// Sub-block: `matches { ... }` or `matches exhaustive { ... }`.
	if p.peek().Kind == TokenWord && p.peek().Text == "matches" {
		matchesTok := p.advance()
		subExhaustive := false
		if !p.atEOF() && p.peek().Kind == TokenWord && p.peek().Text == "exhaustive" {
			subExhaustive = true
			p.advance()
		}
		sub, err := p.parseMatchesBlock(matchesTok.Pos, subExhaustive)
		if err != nil {
			return MatchEntry{}, err
		}

		endTok := p.tokens[p.pos-1]
		return MatchEntry{
			Path:     pathText,
			SubBlock: sub,
			Span:     source.Span{Pos: p.tokens[startPos].Pos, End: endTok.End},
		}, nil
	}

	// Expression or `not-empty` pattern: collect tokens up to the
	// next newline (the entry separator) or the outer block's
	// closing `}`. Reject `,` and `;` with their dedicated
	// diagnostics so the user is pointed at the actual rule.
	patternStart := p.pos
	for !p.atEOF() {
		t := p.peek()
		if isMatchesSep(t) {
			break
		}
		if t.Kind == TokenSep && t.Text == ";" {
			return MatchEntry{}, spanErrorf(t.Span, "matches: ';' is not a valid entry separator; entries are separated by newlines")
		}
		if t.Kind == TokenWord && t.Text == "}" {
			break
		}
		if t.Kind == TokenWord && t.Text == "," {
			return MatchEntry{}, spanErrorf(t.Span, "matches: ',' is not a valid entry separator; entries are separated by newlines")
		}
		if t.Kind == TokenWord && len(t.Text) > 1 && strings.HasSuffix(t.Text, ",") {
			return MatchEntry{}, spanErrorf(t.Span, "matches: trailing ',' on %q is not allowed; entries are separated by newlines", t.Text)
		}
		p.pos++
	}
	rest := p.tokens[patternStart:p.pos]
	if len(rest) == 0 {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry %q: missing pattern after ':'", pathText)
	}

	entrySpan := source.Span{Pos: p.tokens[startPos].Pos, End: rest[len(rest)-1].End}
	if len(rest) == 1 && rest[0].Kind == TokenWord && isMatchesPredicate(rest[0].Text) {
		return MatchEntry{Path: pathText, Predicate: rest[0].Text, Span: entrySpan}, nil
	}
	pattern, err := parseExpression(rest)
	if err != nil {
		return MatchEntry{}, spanErrorf(pathTok.Span, "matches entry %q: %v", pathText, err)
	}

	return MatchEntry{Path: pathText, Pattern: pattern, Span: entrySpan}, nil
}

// evalMatchesBlock resolves one parsed `matches { ... }` block into
// the runtime payload the matches evaluator consumes. Predicate
// rows keep only their predicate name; value-pattern rows evaluate
// their expression now; nested blocks recurse under the same env.
