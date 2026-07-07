package syntax

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// adapterPrefixes is the fixed set of known adapter names recognised
// by the tokeniser. Only these names followed by :$ trigger adapter
// token recognition.
var adapterPrefixes = []string{"file"}

// TokenKind classifies a lexed token.
type TokenKind int

const (
	// TokenWord is an unquoted word: command name, flag, path, etc.
	TokenWord TokenKind = iota
	// TokenAssign is a standalone "=" token at a token boundary.
	TokenAssign
	// TokenVarRef is a variable reference such as $prog.id or
	// ${prog.maps[0].name}.
	TokenVarRef
	// TokenQuoted is a single- or double-quoted string. The
	// delimiters are stripped; $ is literal inside quotes.
	TokenQuoted
	// TokenAdapterRef is an adapter invocation such as
	// file:$var.path. It carries the adapter name, the variable
	// name, and the optional field path.
	TokenAdapterRef
	// TokenSep is a statement separator: a newline or a semicolon.
	// Consecutive separators are collapsed at parse time.
	TokenSep
	// TokenThread is the '|>' operator at a token boundary -- the
	// value-threading composition operator that feeds the LHS
	// Value into the RHS command's last argument slot. Matches
	// the '|>' sigil used by F#, OCaml, Elixir, Julia, and R;
	// semantically equivalent to Clojure's `->>` thread-last
	// macro. Inside a bare word or quoted string, '|>' stays
	// part of the surrounding literal.
	TokenThread
	// TokenBind is the '<-' sigil at a token boundary. It binds
	// the result of running a command form on its right to the
	// name on its left: "let r <- bpfman program get $pid" or
	// "guard r <- bpfman program load file foo.o". Inside
	// a bare word the bytes '<-' stay part of the surrounding
	// literal; '<-' only emits as TokenBind when it sits at a
	// token boundary (whitespace or start of input on the left).
	TokenBind
	// TokenInterpString is a double-quoted string containing one
	// or more ${...} interpolation points. Segments carries the
	// alternation of literal text and raw expression text; the
	// parser retokenises each expression inner, parses it as a
	// single expression, and the evaluator concatenates the
	// resolved scalars at run time. Double-quoted strings with
	// no interpolation stay as TokenQuoted so the common case
	// pays no extra cost.
	TokenInterpString
)

// InterpSegment is one piece of an interpolated double-quoted
// string. IsLit true means Literal carries the text verbatim and
// Inner is unused; IsLit false means Inner carries the raw source
// of an "${expr}" interpolation (without the '${' and '}'
// delimiters) and the parser tokenises and parses it at parse
// time. source.Pos points at the segment's first byte in the enclosing
// input so diagnostics cite the right column.
type InterpSegment struct {
	// Literal is the verbatim text of a literal segment; it is
	// unused when IsLit is false.
	Literal string

	// Inner is the raw source of an "${...}" interpolation, without
	// the '${' and '}' delimiters; it is unused when IsLit is true.
	Inner string

	source.Span

	// IsLit is true for a literal-text segment and false for an
	// interpolation segment.
	IsLit bool
}

// Token is a single lexical element produced by Tokenise. The
// embedded source.Span covers the token's full extent: source.Span.Pos is the
// first byte, source.Span.End is one past the last byte. Reads via
// promoted fields -- tok.Pos for the start, tok.Line / tok.Col,
// tok.End for the end -- match the AST-node pattern.
type Token struct {
	// Kind is the lexical class of the token.
	Kind TokenKind

	// Text is the token's content, with surrounding quotes stripped
	// for TokenQuoted.
	Text string

	// VarName is the variable name for TokenVarRef and
	// TokenAdapterRef, empty for other kinds.
	VarName string

	// VarPath is the field path for TokenVarRef and TokenAdapterRef,
	// empty when the reference is bare.
	VarPath string

	// Adapter is the adapter name for TokenAdapterRef (for example
	// "file"), empty for other kinds.
	Adapter string

	// Segments is the alternation of literal and interpolation
	// pieces for TokenInterpString, nil for other kinds.
	Segments []InterpSegment

	source.Span
}

// Tokenise lexes input in shell mode: '-' and '/' are valid
// word-interior characters so paths like /sys/fs/bpf and flags
// like -x and --long stay whole. Arithmetic operators '+', '*',
// and '%' still split without whitespace.
func Tokenise(input string) ([]Token, error) {
	return tokeniseAt(source.Pos{Line: 1, Col: 1}, input)
}

// TokeniseAt is Tokenise plus an explicit starting source
// position. The first byte of input is stamped with start; later
// positions stay in the same file and advance across lines from
// there.
func TokeniseAt(start source.Pos, input string) ([]Token, error) {
	return tokeniseAt(start, input)
}

func tokeniseAt(start source.Pos, input string) ([]Token, error) {
	// stripComment preserves offsets by replacing stripped bytes
	// with spaces, so positions into the returned string still map
	// back to the original input's line/column.
	input = stripComment(input)
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}

	lineStarts := buildLineStarts(input)
	base := normalizeStartPos(start)

	emit := func(tokens []Token, tokStart, tokEnd int, tok Token) []Token {
		tok.Span = source.Span{
			Pos: locAt(tokStart, lineStarts, base),
			End: locAt(tokEnd, lineStarts, base),
		}
		return append(tokens, tok)
	}

	var tokens []Token
	i := 0
	for i < len(input) {
		ch := input[i]

		// Skip whitespace (but not newlines, which are separators).
		if ch == ' ' || ch == '\t' || ch == '\r' {
			i++
			continue
		}

		// Backslash at end of line is a line continuation: '\' and
		// the following '\n' together count as whitespace. '\r\n'
		// is handled the same way for CRLF inputs. A lone '\' not
		// followed by a newline falls through to the regular word
		// tokeniser below.
		if ch == '\\' && i+1 < len(input) {
			if input[i+1] == '\n' {
				i += 2
				continue
			}
			if input[i+1] == '\r' && i+2 < len(input) && input[i+2] == '\n' {
				i += 3
				continue
			}
		}

		start := i
		switch {
		case ch == '\n' || ch == ';':
			tokens = emit(tokens, start, start+1, Token{Kind: TokenSep, Text: string(ch)})
			i++

		case ch == '{' || ch == '}' || ch == '(' || ch == ')':
			tokens = emit(tokens, start, start+1, Token{Kind: TokenWord, Text: string(ch)})
			i++

		case ch == '+' || ch == '*' || ch == '%':
			// Arithmetic operators that cannot appear inside a
			// bare word (unlike '-' and '/', which are valid
			// word-interior characters because of negative
			// literals, flags, and paths). Emitting them as
			// single-char tokens lets "1+1", "$x*2", "7%3" split
			// cleanly without requiring surrounding whitespace.
			tokens = emit(tokens, start, start+1, Token{Kind: TokenWord, Text: string(ch)})
			i++

		case ch == '=' && isTokenStart(tokens):
			// Distinguish == (comparison) from = (assignment).
			if i+1 < len(input) && input[i+1] == '=' {
				tokens = emit(tokens, start, start+2, Token{Kind: TokenWord, Text: "=="})
				i += 2
			} else {
				tokens = emit(tokens, start, start+1, Token{Kind: TokenAssign, Text: "="})
				i++
			}

		case ch == '$':
			tok, n, err := lexVarRef(input, i)
			if err != nil {
				end := i + 1
				if n > 0 {
					end = i + n
				}
				return nil, spanErrorf(source.Span{
					Pos: locAt(start, lineStarts, base),
					End: locAt(end, lineStarts, base),
				}, "%v", err)
			}

			tokens = emit(tokens, start, start+n, tok)
			i += n

		case ch == '"' || ch == '\'':
			tok, n, err := lexQuoted(input, i, base)
			if err != nil {
				// The lex helpers' quote/escape/interpolation
				// errors do not themselves carry a source.Span; the
				// opening quote's position is what the user
				// needs to find the string when the source
				// has many. End collapses to the same point
				// so renderers cite without a misleading
				// multi-byte caret over an unknown region.
				openPos := locAt(start, lineStarts, base)
				return nil, spanErrorf(source.Span{Pos: openPos, End: openPos}, "%v", err)
			}

			tokens = emit(tokens, start, start+n, tok)
			i += n

		case ch == '[' || ch == ']':
			tokens = emit(tokens, start, start+1, Token{Kind: TokenWord, Text: string(ch)})
			i++

		case ch == '|' && i+1 < len(input) && input[i+1] == '>':
			// Reaching this case means the previous byte was
			// whitespace or absent, so '|>' sits at a token
			// boundary. The lexWord path keeps '|' as an
			// interior word character, so 'a|>b' stays a word.
			tokens = emit(tokens, start, start+2, Token{Kind: TokenThread, Text: "|>"})
			i += 2

		case ch == '<' && i+1 < len(input) && input[i+1] == '-':
			// Reaching this case means the previous byte was
			// whitespace or absent, so '<-' sits at a token
			// boundary. The lexWord path keeps '<' and '-' as
			// interior word characters, so 'x<-y' stays a word.
			tokens = emit(tokens, start, start+2, Token{Kind: TokenBind, Text: "<-"})
			i += 2

		default:
			if tok, n, ok := lexAdapterRef(input, i); ok {
				tokens = emit(tokens, start, start+n, tok)
				i += n
			} else {
				tok, n := lexWord(input, i)
				tokens = emit(tokens, start, start+n, tok)
				i += n
			}
		}
	}

	return tokens, nil
}

// buildLineStarts returns the byte offset at which each line begins.
// Line 1 starts at offset 0; line k+1 starts at the byte following
// the (k)th newline.
func buildLineStarts(input string) []int {
	starts := []int{0}
	for i := 0; i < len(input); i++ {
		if input[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// locAt returns the 1-based line/column for a byte offset. The
// column is a byte offset within the line, counting from 1.
func locAt(pos int, lineStarts []int, base source.Pos) source.Pos {
	// Binary search for the largest k with lineStarts[k] <= pos.
	lo, hi := 0, len(lineStarts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if lineStarts[mid] <= pos {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	line := base.Line + lo
	col := pos - lineStarts[lo] + 1
	if lo == 0 {
		col = base.Col + pos - lineStarts[lo]
	}
	return source.Pos{File: base.File, Line: line, Col: col}
}

func normalizeStartPos(p source.Pos) source.Pos {
	if p.Line <= 0 {
		p.Line = 1
	}
	if p.Col <= 0 {
		p.Col = 1
	}
	return p
}

// isTokenStart returns true when the current position is at a token
// boundary where = should be treated as a standalone TokenAssign.
// This is true when = appears as the entire next token (preceded by
// whitespace or start of input) rather than embedded in a word like
// KEY=VALUE.
func isTokenStart(tokens []Token) bool {
	// = is only standalone when it follows at least one token
	// (the LHS identifier). The caller already skips whitespace
	// before reaching =, so if we get here the = is at a token
	// boundary.
	return len(tokens) > 0
}

// stripComment replaces inline comments with spaces while preserving
// byte offsets so downstream line/column tracking still matches the
// original input. A comment starts at an unquoted '#' and extends to
// (but does not include) the next newline, which is left intact so
// accumulated multi-line input (e.g. an if block spanning lines) still
// sees the separator.
func stripComment(input string) string {
	b := make([]byte, 0, len(input))
	inSingle := false
	inDouble := false
	for i := 0; i < len(input); {
		ch := input[i]
		switch {
		case ch == '\\' && inDouble && i+1 < len(input):
			// The double-quoted string lexer recognises backslash
			// escapes (\", \n, \t, \r, \\, \$). The comment-
			// stripping pass runs before the string lexer, so it
			// has to walk the same escape vocabulary to keep quote
			// state correct: a bare \" would otherwise look like a
			// closing quote, flipping inDouble false, and any '#'
			// later in the same string would be mistaken for an
			// inline comment. Copy both bytes verbatim and skip
			// the escape's payload without touching quote state.
			b = append(b, ch, input[i+1])
			i += 2
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
			b = append(b, ch)
			i++
		case ch == '"' && !inSingle:
			inDouble = !inDouble
			b = append(b, ch)
			i++
		case ch == '#' && !inSingle && !inDouble:
			for i < len(input) && input[i] != '\n' {
				b = append(b, ' ')
				i++
			}
		default:
			b = append(b, ch)
			i++
		}
	}
	return string(b)
}

// lexVarRef lexes a variable reference starting at input[pos] which
// must be '$'. It handles both bare ($name.path) and braced
// (${name.path[0]}) forms.
func lexVarRef(input string, pos int) (Token, int, error) {
	i := pos + 1 // skip $
	if i >= len(input) {
		return Token{}, 0, fmt.Errorf("unexpected end of input after $")
	}

	if input[i] == '{' {
		return lexBracedVarRef(input, pos)
	}
	return lexBareVarRef(input, pos)
}

// lexBareVarRef lexes $name or $name.path.
func lexBareVarRef(input string, pos int) (Token, int, error) {
	i := pos + 1 // skip $

	// The variable name must start with a letter or underscore.
	if i >= len(input) || !isIdentStart(input[i]) {
		return Token{}, 0, fmt.Errorf("invalid variable reference: expected identifier after $")
	}

	// Consume the identifier part of the name.
	nameStart := i
	for i < len(input) && isIdentContinue(input[i]) {
		i++
	}
	name := input[nameStart:i]

	// Consume optional path: dots, identifiers, and [n] / [$ident]
	// indexing. The path grammar is: segment+ where segment =
	// '.' ident | '[' digits ']' | '[' '$' ident ']'.
	pathStart := i
	for i < len(input) {
		if input[i] == '.' {
			i++
			if i >= len(input) || !isIdentStart(input[i]) {
				return Token{}, 0, fmt.Errorf("invalid variable reference %q: expected identifier after '.'", input[pos:i])
			}
			for i < len(input) && isIdentContinue(input[i]) {
				i++
			}
		} else if input[i] == '[' {
			next, err := lexPathIndex(input, i, "")
			if err != nil {
				return Token{}, 0, fmt.Errorf("invalid variable reference %q: %w", input[pos:next], err)
			}

			i = next
		} else {
			break
		}
	}
	path := input[pathStart:i]

	// Strip leading dot from path.
	if len(path) > 0 && path[0] == '.' {
		path = path[1:]
	}

	tok := Token{
		Kind:    TokenVarRef,
		Text:    input[pos:i],
		VarName: name,
		VarPath: path,
	}
	return tok, i - pos, nil
}

// lexBracedVarRef lexes ${name.path[0]}.
func lexBracedVarRef(input string, pos int) (Token, int, error) {
	i := pos + 2 // skip ${

	// Must not be empty.
	if i >= len(input) || input[i] == '}' {
		return Token{}, 0, fmt.Errorf("empty variable reference: ${}")
	}

	// The variable name must start with a letter or underscore.
	if !isIdentStart(input[i]) {
		return Token{}, 0, fmt.Errorf("invalid variable reference: expected identifier after ${")
	}

	nameStart := i
	for i < len(input) && isIdentContinue(input[i]) {
		i++
	}
	name := input[nameStart:i]

	// Validate optional path inside braces using the same grammar
	// as bare refs: segment = '.' ident | '[' digits ']' |
	// '[' '$' ident ']'.
	pathStart := i
	for i < len(input) && input[i] != '}' {
		if input[i] == '.' {
			i++
			if i >= len(input) || input[i] == '}' || !isIdentStart(input[i]) {
				return Token{}, 0, fmt.Errorf("invalid variable reference: expected identifier after '.' in ${...}")
			}
			for i < len(input) && input[i] != '}' && isIdentContinue(input[i]) {
				i++
			}
		} else if input[i] == '[' {
			next, err := lexPathIndex(input, i, " in ${...}")
			if err != nil {
				return Token{}, 0, fmt.Errorf("invalid variable reference: %w", err)
			}

			i = next
		} else {
			return Token{}, 0, fmt.Errorf("invalid variable reference: unexpected character %q in ${...} path", input[i])
		}
	}
	if i >= len(input) {
		return Token{}, 0, fmt.Errorf("unterminated variable reference: missing }")
	}
	path := input[pathStart:i]
	i++ // skip }

	// Strip leading dot from path.
	if len(path) > 0 && path[0] == '.' {
		path = path[1:]
	}

	tok := Token{
		Kind:    TokenVarRef,
		Text:    input[pos:i],
		VarName: name,
		VarPath: path,
	}
	return tok, i - pos, nil
}

// lexPathIndex consumes a "[N]" or "[$ident]" segment starting at
// input[pos], which must be '['. Returns the position one past the
// closing ']'. The literal-digit form yields the same shape parsePath
// understands today; the "[$ident]" form is resolved at the use site
// against the active session bindings (see resolveDynamicPath). The
// where suffix lets callers tag errors with " in ${...}" for braced
// refs so braced and bare diagnostics stay distinguishable.
func lexPathIndex(input string, pos int, where string) (int, error) {
	j := pos + 1
	if j < len(input) && input[j] == '$' {
		k := j + 1
		if k >= len(input) || !isIdentStart(input[k]) {
			return min(k+1, len(input)), fmt.Errorf("expected identifier after '[$'%s", where)
		}
		for k < len(input) && isIdentContinue(input[k]) {
			k++
		}
		if k >= len(input) || input[k] != ']' {
			return min(k+1, len(input)), fmt.Errorf("expected ']' after '[$%s'%s", input[j+1:k], where)
		}
		return k + 1, nil
	}
	digitStart := j
	for j < len(input) && input[j] >= '0' && input[j] <= '9' {
		j++
	}
	if j == digitStart {
		return min(j+1, len(input)), fmt.Errorf("expected digits or '$ident' inside '[]'%s", where)
	}
	if j >= len(input) || input[j] != ']' {
		return min(j+1, len(input)), fmt.Errorf("expected ']' after index%s", where)
	}
	return j + 1, nil
}

// lexQuoted lexes a single- or double-quoted string. $ is literal
// inside quotes; no backslash escapes.
func lexQuoted(input string, pos int, base source.Pos) (Token, int, error) {
	quote := input[pos]
	if quote == '\'' {
		return lexSingleQuoted(input, pos)
	}
	return lexDoubleQuoted(input, pos, base)
}

// lexSingleQuoted lexes a single-quoted string. Single quotes
// are fully literal: no '$' recognition, no escapes. The result
// is always a plain TokenQuoted.
func lexSingleQuoted(input string, pos int) (Token, int, error) {
	i := pos + 1
	for i < len(input) && input[i] != '\'' {
		i++
	}
	if i >= len(input) {
		return Token{}, 0, fmt.Errorf("unterminated single-quoted string (no matching ' before end of input)")
	}
	text := input[pos+1 : i]
	i++ // skip closing quote
	return Token{Kind: TokenQuoted, Text: text}, i - pos, nil
}

// lexDoubleQuoted lexes a double-quoted string, splitting on
// "${...}" interpolation points. A string with no interpolation
// emits TokenQuoted so downstream code paths for plain literals
// do not need to know about segments. A string with at least one
// "${...}" emits TokenInterpString whose Segments alternate
// literal and interp pieces; a bare '$' inside a double-quoted
// string that is not followed by '{' is a lex-time error so the
// "$ here is literal" bash habit fails loudly rather than
// silently mis-parsing.
func lexDoubleQuoted(input string, pos int, base source.Pos) (Token, int, error) {
	lineStarts := buildLineStarts(input)
	i := pos + 1
	var segments []InterpSegment
	var lit strings.Builder
	var litStart int
	litOpen := false

	startLiteral := func(at int) {
		if !litOpen {
			litStart = at
			litOpen = true
		}
	}
	flushLiteral := func(at int) {
		if !litOpen {
			return
		}
		segments = append(segments, InterpSegment{
			Literal: lit.String(),
			Span: source.Span{
				Pos: locAt(litStart, lineStarts, base),
				End: locAt(at, lineStarts, base),
			},
			IsLit: true,
		})
		lit.Reset()
		litOpen = false
	}

	for i < len(input) {
		ch := input[i]
		switch ch {
		case '"':
			flushLiteral(i)
			i++ // skip closing quote
			if len(segments) == 0 {
				return Token{Kind: TokenQuoted, Text: ""}, i - pos, nil
			}
			if len(segments) == 1 && segments[0].IsLit {
				return Token{
					Kind: TokenQuoted,
					Text: segments[0].Literal,
				}, i - pos, nil
			}
			return Token{
				Kind:     TokenInterpString,
				Text:     input[pos:i],
				Segments: segments,
			}, i - pos, nil
		case '\\':
			if i+1 >= len(input) {
				return Token{}, 0, fmt.Errorf("unterminated escape sequence at end of string")
			}
			var decoded byte
			switch input[i+1] {
			case 'n':
				decoded = '\n'
			case 't':
				decoded = '\t'
			case 'r':
				decoded = '\r'
			case '\\':
				decoded = '\\'
			case '"':
				decoded = '"'
			case '$':
				decoded = '$'
			default:
				return Token{}, 0, fmt.Errorf("unknown escape sequence '\\%c' in double-quoted string; recognised escapes are \\n, \\t, \\r, \\\\, \\\", \\$", input[i+1])
			}
			startLiteral(i)
			lit.WriteByte(decoded)
			i += 2
		case '$':
			if i+1 >= len(input) || input[i+1] != '{' {
				return Token{}, 0, fmt.Errorf("'$' in double-quoted string must be followed by '{...}' (use single quotes or '\\$' for a literal '$')")
			}
			flushLiteral(i)
			start := i
			innerEnd, err := scanInterpBody(input, i+2)
			if err != nil {
				return Token{}, 0, err
			}

			inner := input[start+2 : innerEnd]
			if strings.TrimSpace(inner) == "" {
				return Token{}, 0, fmt.Errorf("empty interpolation '${}' in string")
			}
			segments = append(segments, InterpSegment{
				Inner: inner,
				Span: source.Span{
					Pos: locAt(start, lineStarts, base),
					End: locAt(innerEnd+1, lineStarts, base),
				},
				IsLit: false,
			})
			i = innerEnd + 1 // skip past closing '}'
		default:
			startLiteral(i)
			lit.WriteByte(ch)
			i++
		}
	}

	return Token{}, 0, fmt.Errorf("unterminated double-quoted string (no matching \" before end of input)")
}

// scanInterpBody returns the offset of the '}' that closes an
// interpolation whose contents start at pos. Single-quoted
// strings inside the body are skipped so a stray '}' inside a
// literal does not close the interpolation prematurely; nested
// braces are counted so the matching close brace is found. A bare
// '"' inside the body is rejected -- the simple rule "use single
// quotes inside ${...}" keeps the lexer linear and avoids an
// escape mechanism.
func scanInterpBody(input string, pos int) (int, error) {
	depth := 1
	j := pos
	for j < len(input) {
		switch input[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j, nil
			}
		case '\'':
			k := j + 1
			for k < len(input) && input[k] != '\'' {
				k++
			}
			if k >= len(input) {
				return 0, fmt.Errorf("unterminated single-quoted string inside ${...}")
			}
			j = k
		case '"':
			// A '"' inside the interp body is either a
			// missing '}' (user forgot to close the
			// interpolation and the next '"' is the outer
			// string's close) or a nested double-quoted
			// string (unsupported -- use single quotes).
			// Either way the user needs to fix it; the
			// more actionable diagnostic is "unterminated
			// interpolation" since that is the common case.
			return 0, fmt.Errorf("unterminated interpolation in string: missing '}' (use single quotes for strings inside ${...})")
		}
		j++
	}
	return 0, fmt.Errorf("unterminated interpolation in string: missing '}'")
}

// lexWord consumes a word token: everything until whitespace, a
// separator (newline or semicolon), $, ", ', #, [, ], {, }, (,
// ), or one of the arithmetic operators '+' / '*' / '%'.
// Brackets and braces are terminators because they introduce or
// close command substitution and block syntax respectively.
// '+', '*', and '%' are terminators so "1+1" and "$x*2" split
// without requiring whitespace around the operator.
//
// In shell mode '-' and '/' stay as word-interior characters: '-'
// is part of negative literals ("-3") and flags ("-x", "--long");
// '/' is part of file paths ("/sys/fs/bpf").
func lexWord(input string, pos int) (Token, int) {
	i := pos
	for i < len(input) {
		ch := input[i]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == ';' ||
			ch == '$' || ch == '"' || ch == '\'' || ch == '#' ||
			ch == '[' || ch == ']' || ch == '{' || ch == '}' ||
			ch == '(' || ch == ')' ||
			ch == '+' || ch == '*' || ch == '%' {
			break
		}
		i++
	}
	tok := Token{Kind: TokenWord, Text: input[pos:i]}
	return tok, i - pos
}

// lexAdapterRef tries to lex an adapter reference at input[pos].
// Known adapter prefixes (e.g. "file") immediately followed by :$
// trigger recognition. The variable reference after : is lexed by
// lexVarRef. Returns (token, consumed, true) on success, or
// (Token{}, 0, false) if this position is not an adapter reference.
func lexAdapterRef(input string, pos int) (Token, int, bool) {
	for _, prefix := range adapterPrefixes {
		full := prefix + ":"
		if !strings.HasPrefix(input[pos:], full) {
			continue
		}
		afterColon := pos + len(full)
		if afterColon >= len(input) || input[afterColon] != '$' {
			continue
		}
		tok, n, err := lexVarRef(input, afterColon)
		if err != nil {
			// Let normal flow handle the error via lexWord + lexVarRef.
			return Token{}, 0, false
		}

		adapterTok := Token{
			Kind:    TokenAdapterRef,
			Text:    input[pos : afterColon+n],
			Adapter: prefix,
			VarName: tok.VarName,
			VarPath: tok.VarPath,
		}
		return adapterTok, afterColon + n - pos, true
	}
	return Token{}, 0, false
}

func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isIdentContinue(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// isIdent reports whether s is a valid identifier: [a-zA-Z_][a-zA-Z0-9_]*.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}
