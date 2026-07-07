package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

func TestTokenise(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    []Token
		wantErr string
	}{
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: "   \t  ",
			want:  nil,
		},
		{
			name:  "single word",
			input: "help",
			want:  []Token{{Kind: TokenWord, Text: "help"}},
		},
		{
			name:  "multiple words",
			input: "show program 123",
			want: []Token{
				{Kind: TokenWord, Text: "show"},
				{Kind: TokenWord, Text: "program"},
				{Kind: TokenWord, Text: "123"},
			},
		},
		{
			name:  "flags",
			input: "load file foo.o -m app=test",
			want: []Token{
				{Kind: TokenWord, Text: "load"},
				{Kind: TokenWord, Text: "file"},
				{Kind: TokenWord, Text: "foo.o"},
				{Kind: TokenWord, Text: "-m"},
				{Kind: TokenWord, Text: "app=test"},
			},
		},
		{
			name:  "equals embedded in word stays part of word",
			input: "load KEY=VALUE",
			want: []Token{
				{Kind: TokenWord, Text: "load"},
				{Kind: TokenWord, Text: "KEY=VALUE"},
			},
		},
		{
			name:  "standalone equals after identifier",
			input: "prog = load file",
			want: []Token{
				{Kind: TokenWord, Text: "prog"},
				{Kind: TokenAssign, Text: "="},
				{Kind: TokenWord, Text: "load"},
				{Kind: TokenWord, Text: "file"},
			},
		},
		{
			name:  "bare varref simple",
			input: "show $prog",
			want: []Token{
				{Kind: TokenWord, Text: "show"},
				{Kind: TokenVarRef, Text: "$prog", VarName: "prog"},
			},
		},
		{
			name:  "bare varref with dotted path",
			input: "show $prog.id",
			want: []Token{
				{Kind: TokenWord, Text: "show"},
				{Kind: TokenVarRef, Text: "$prog.id", VarName: "prog", VarPath: "id"},
			},
		},
		{
			name:  "bare varref with nested dotted path",
			input: "--program-id $prog.details.kernel_id",
			want: []Token{
				{Kind: TokenWord, Text: "--program-id"},
				{Kind: TokenVarRef, Text: "$prog.details.kernel_id", VarName: "prog", VarPath: "details.kernel_id"},
			},
		},
		{
			name:  "bare varref with array index",
			input: "$prog.maps[0].name",
			want: []Token{
				{Kind: TokenVarRef, Text: "$prog.maps[0].name", VarName: "prog", VarPath: "maps[0].name"},
			},
		},
		{
			name:  "braced varref simple",
			input: "${prog}",
			want: []Token{
				{Kind: TokenVarRef, Text: "${prog}", VarName: "prog"},
			},
		},
		{
			name:  "braced varref with path",
			input: "${prog.id}",
			want: []Token{
				{Kind: TokenVarRef, Text: "${prog.id}", VarName: "prog", VarPath: "id"},
			},
		},
		{
			name:  "braced varref with index",
			input: "${prog.maps[0].name}",
			want: []Token{
				{Kind: TokenVarRef, Text: "${prog.maps[0].name}", VarName: "prog", VarPath: "maps[0].name"},
			},
		},
		{
			name:  "bare varref with $ident index",
			input: "$xs[$i]",
			want: []Token{
				{Kind: TokenVarRef, Text: "$xs[$i]", VarName: "xs", VarPath: "[$i]"},
			},
		},
		{
			name:  "bare varref with $ident index inside path",
			input: "$xs.field[$i].name",
			want: []Token{
				{Kind: TokenVarRef, Text: "$xs.field[$i].name", VarName: "xs", VarPath: "field[$i].name"},
			},
		},
		{
			name:  "braced varref with $ident index",
			input: "${xs[$i]}",
			want: []Token{
				{Kind: TokenVarRef, Text: "${xs[$i]}", VarName: "xs", VarPath: "[$i]"},
			},
		},
		{
			name:  "bare varref chained $ident indices",
			input: "$xs[$i][$j]",
			want: []Token{
				{Kind: TokenVarRef, Text: "$xs[$i][$j]", VarName: "xs", VarPath: "[$i][$j]"},
			},
		},
		{
			name:  "double-quoted string",
			input: `load "hello world"`,
			want: []Token{
				{Kind: TokenWord, Text: "load"},
				{Kind: TokenQuoted, Text: "hello world"},
			},
		},
		{
			name:  "single-quoted string",
			input: "load 'hello world'",
			want: []Token{
				{Kind: TokenWord, Text: "load"},
				{Kind: TokenQuoted, Text: "hello world"},
			},
		},
		{
			name:  "dollar is literal inside single quotes",
			input: `'$prog.id'`,
			want: []Token{
				{Kind: TokenQuoted, Text: "$prog.id"},
			},
		},
		{
			name:    "bare dollar in double quotes errors",
			input:   `"$prog.id"`,
			wantErr: "'$' in double-quoted string must be followed by '{...}'",
		},
		{
			name:  "comment strips trailing text",
			input: "show program 123 # this is a comment",
			want: []Token{
				{Kind: TokenWord, Text: "show"},
				{Kind: TokenWord, Text: "program"},
				{Kind: TokenWord, Text: "123"},
			},
		},
		{
			name:  "hash inside quotes is not a comment",
			input: `load "path#with#hash"`,
			want: []Token{
				{Kind: TokenWord, Text: "load"},
				{Kind: TokenQuoted, Text: "path#with#hash"},
			},
		},
		{
			name:  "comment only",
			input: "# just a comment",
			want:  nil,
		},
		{
			name:  "mixed line with assignment and varrefs",
			input: "link = link attach --program-id $prog.id",
			want: []Token{
				{Kind: TokenWord, Text: "link"},
				{Kind: TokenAssign, Text: "="},
				{Kind: TokenWord, Text: "link"},
				{Kind: TokenWord, Text: "attach"},
				{Kind: TokenWord, Text: "--program-id"},
				{Kind: TokenVarRef, Text: "$prog.id", VarName: "prog", VarPath: "id"},
			},
		},
		{
			name:  "varref adjacent to word",
			input: "prefix$var",
			want: []Token{
				{Kind: TokenWord, Text: "prefix"},
				{Kind: TokenVarRef, Text: "$var", VarName: "var"},
			},
		},
		{
			name:    "unterminated double quote",
			input:   `"hello`,
			wantErr: `unterminated double-quoted string`,
		},
		{
			name:    "unterminated single quote",
			input:   `'hello`,
			wantErr: `unterminated single-quoted string`,
		},
		{
			name:    "unterminated braced varref",
			input:   "${prog.id",
			wantErr: "unterminated variable reference: missing }",
		},
		{
			name:    "bare dollar at end of input",
			input:   "$",
			wantErr: "unexpected end of input after $",
		},
		{
			name:    "dollar followed by whitespace",
			input:   "$ ",
			wantErr: "expected identifier after $",
		},
		{
			name:    "empty braced varref",
			input:   "${}",
			wantErr: "empty variable reference: ${}",
		},
		{
			name:    "dollar followed by digit",
			input:   "$123",
			wantErr: "invalid variable reference: expected identifier after $",
		},
		{
			name:  "varref with underscore",
			input: "$my_var.field_name",
			want: []Token{
				{Kind: TokenVarRef, Text: "$my_var.field_name", VarName: "my_var", VarPath: "field_name"},
			},
		},

		// Malformed variable reference tests.

		// Bare form: trailing dot.
		{
			name:    "bare varref trailing dot at end of input",
			input:   "$prog.",
			wantErr: "expected identifier after '.'",
		},
		{
			name:    "bare varref trailing dot before space",
			input:   "$prog. foo",
			wantErr: "expected identifier after '.'",
		},
		{
			name:    "bare varref dot followed by digit",
			input:   "$prog.123",
			wantErr: "expected identifier after '.'",
		},
		{
			name:    "bare varref trailing dot after path",
			input:   "$prog.maps[0].",
			wantErr: "expected identifier after '.'",
		},

		// Bare form: malformed index.
		{
			name:    "bare varref empty index",
			input:   "$prog[]",
			wantErr: "expected digits or '$ident' inside '[]'",
		},
		{
			name:    "bare varref non-numeric index",
			input:   "$prog[abc]",
			wantErr: "expected digits or '$ident' inside '[]'",
		},
		{
			name:    "bare varref unclosed index",
			input:   "$prog[0",
			wantErr: "expected ']' after index",
		},
		{
			name:    "bare varref unclosed index no digits",
			input:   "$prog[",
			wantErr: "expected digits or '$ident' inside '[]'",
		},
		{
			name:    "bare varref $ident index empty after $",
			input:   "$prog[$]",
			wantErr: "expected identifier after '[$'",
		},
		{
			name:    "bare varref $ident index non-letter start",
			input:   "$prog[$1]",
			wantErr: "expected identifier after '[$'",
		},
		{
			name:    "bare varref $ident index unclosed",
			input:   "$prog[$i",
			wantErr: "expected ']' after '[$i'",
		},
		{
			name:    "braced varref $ident index empty after $",
			input:   "${prog[$]}",
			wantErr: "expected identifier after '[$' in ${...}",
		},
		{
			name:    "braced varref $ident index unclosed",
			input:   "${prog[$i}",
			wantErr: "expected ']' after '[$i' in ${...}",
		},

		// Braced form: trailing dot.
		{
			name:    "braced varref trailing dot",
			input:   "${prog.}",
			wantErr: "expected identifier after '.' in ${...}",
		},
		{
			name:    "braced varref empty segment (double dot)",
			input:   "${prog..id}",
			wantErr: "expected identifier after '.' in ${...}",
		},

		// Braced form: malformed index.
		{
			name:    "braced varref non-numeric index",
			input:   "${prog[abc]}",
			wantErr: "expected digits or '$ident' inside '[]' in ${...}",
		},
		{
			name:    "braced varref empty index",
			input:   "${prog[]}",
			wantErr: "expected digits or '$ident' inside '[]' in ${...}",
		},
		{
			name:    "braced varref unclosed index",
			input:   "${prog[0}",
			wantErr: "expected ']' after index in ${...}",
		},

		// Braced form: unexpected characters.
		{
			name:    "braced varref unexpected character in path",
			input:   "${prog!id}",
			wantErr: "unexpected character",
		},
		{
			name:    "braced varref space in path",
			input:   "${prog id}",
			wantErr: "unexpected character",
		},

		// Adapter reference tests.

		{
			name:  "adapter ref bare",
			input: "exec diff file:$x file:$y",
			want: []Token{
				{Kind: TokenWord, Text: "exec"},
				{Kind: TokenWord, Text: "diff"},
				{Kind: TokenAdapterRef, Text: "file:$x", Adapter: "file", VarName: "x"},
				{Kind: TokenAdapterRef, Text: "file:$y", Adapter: "file", VarName: "y"},
			},
		},
		{
			name:  "adapter ref with dotted path",
			input: "file:$raw.stdout",
			want: []Token{
				{Kind: TokenAdapterRef, Text: "file:$raw.stdout", Adapter: "file", VarName: "raw", VarPath: "stdout"},
			},
		},
		{
			name:  "adapter ref with index",
			input: "file:$snap[2]",
			want: []Token{
				{Kind: TokenAdapterRef, Text: "file:$snap[2]", Adapter: "file", VarName: "snap", VarPath: "[2]"},
			},
		},
		{
			name:  "adapter ref braced form",
			input: "file:${data.items[0]}",
			want: []Token{
				{Kind: TokenAdapterRef, Text: "file:${data.items[0]}", Adapter: "file", VarName: "data", VarPath: "items[0]"},
			},
		},
		{
			name:  "file colon without dollar is plain word",
			input: "file:something",
			want: []Token{
				{Kind: TokenWord, Text: "file:something"},
			},
		},
		{
			name:  "file colon with space before dollar is two tokens",
			input: "file: $var",
			want: []Token{
				{Kind: TokenWord, Text: "file:"},
				{Kind: TokenVarRef, Text: "$var", VarName: "var"},
			},
		},
		// Comparison operator tokenisation.
		{
			name:  "== is a single word token, not two assigns",
			input: "assert $a == 1",
			want: []Token{
				{Kind: TokenWord, Text: "assert"},
				{Kind: TokenVarRef, Text: "$a", VarName: "a"},
				{Kind: TokenWord, Text: "=="},
				{Kind: TokenWord, Text: "1"},
			},
		},
		{
			name:  "!= is a single word token",
			input: "assert $a != 1",
			want: []Token{
				{Kind: TokenWord, Text: "assert"},
				{Kind: TokenVarRef, Text: "$a", VarName: "a"},
				{Kind: TokenWord, Text: "!="},
				{Kind: TokenWord, Text: "1"},
			},
		},
		{
			name:  "< and > are word tokens",
			input: "assert $a < $b",
			want: []Token{
				{Kind: TokenWord, Text: "assert"},
				{Kind: TokenVarRef, Text: "$a", VarName: "a"},
				{Kind: TokenWord, Text: "<"},
				{Kind: TokenVarRef, Text: "$b", VarName: "b"},
			},
		},
		{
			name:  "<= is a single word token",
			input: "assert $a <= $b",
			want: []Token{
				{Kind: TokenWord, Text: "assert"},
				{Kind: TokenVarRef, Text: "$a", VarName: "a"},
				{Kind: TokenWord, Text: "<="},
				{Kind: TokenVarRef, Text: "$b", VarName: "b"},
			},
		},
		{
			name:  ">= is a single word token",
			input: "assert $a >= $b",
			want: []Token{
				{Kind: TokenWord, Text: "assert"},
				{Kind: TokenVarRef, Text: "$a", VarName: "a"},
				{Kind: TokenWord, Text: ">="},
				{Kind: TokenVarRef, Text: "$b", VarName: "b"},
			},
		},
		{
			name:  "= remains assignment when not followed by =",
			input: "x = 42",
			want: []Token{
				{Kind: TokenWord, Text: "x"},
				{Kind: TokenAssign, Text: "="},
				{Kind: TokenWord, Text: "42"},
			},
		},

		{
			name:  "unknown adapter prefix is word plus varref",
			input: "notanadapter:$var",
			want: []Token{
				{Kind: TokenWord, Text: "notanadapter:"},
				{Kind: TokenVarRef, Text: "$var", VarName: "var"},
			},
		},
		{
			name:  "adapter ref mixed with normal args",
			input: "exec wc -l file:$raw.stdout",
			want: []Token{
				{Kind: TokenWord, Text: "exec"},
				{Kind: TokenWord, Text: "wc"},
				{Kind: TokenWord, Text: "-l"},
				{Kind: TokenAdapterRef, Text: "file:$raw.stdout", Adapter: "file", VarName: "raw", VarPath: "stdout"},
			},
		},

		// Thread operator. A `|>` at a token boundary is a
		// standalone TokenThread; inside a bare word or quoted
		// string the characters stay part of the word/string.
		// A lone `|` is ordinary word content.

		{
			name:  "thread between tokens",
			input: "$x |> jq .a",
			want: []Token{
				{Kind: TokenVarRef, Text: "$x", VarName: "x"},
				{Kind: TokenThread, Text: "|>"},
				{Kind: TokenWord, Text: "jq"},
				{Kind: TokenWord, Text: ".a"},
			},
		},
		{
			name:  "thread chain",
			input: "$x |> jq .a |> jq add",
			want: []Token{
				{Kind: TokenVarRef, Text: "$x", VarName: "x"},
				{Kind: TokenThread, Text: "|>"},
				{Kind: TokenWord, Text: "jq"},
				{Kind: TokenWord, Text: ".a"},
				{Kind: TokenThread, Text: "|>"},
				{Kind: TokenWord, Text: "jq"},
				{Kind: TokenWord, Text: "add"},
			},
		},
		{
			name:  "thread inside bare word stays part of word",
			input: "a|>b",
			want: []Token{
				{Kind: TokenWord, Text: "a|>b"},
			},
		},
		{
			name:  "thread inside quoted string is literal",
			input: `"a |> b"`,
			want: []Token{
				{Kind: TokenQuoted, Text: "a |> b"},
			},
		},
		{
			name:  "bare pipe without arrow is word content",
			input: "a | b",
			want: []Token{
				{Kind: TokenWord, Text: "a"},
				{Kind: TokenWord, Text: "|"},
				{Kind: TokenWord, Text: "b"},
			},
		},

		// Bind sigil. A `<-` at a token boundary is a standalone
		// TokenBind; inside a bare word the characters stay part
		// of the surrounding literal.

		{
			name:  "bind between tokens",
			input: "let r <- bpfman version",
			want: []Token{
				{Kind: TokenWord, Text: "let"},
				{Kind: TokenWord, Text: "r"},
				{Kind: TokenBind, Text: "<-"},
				{Kind: TokenWord, Text: "bpfman"},
				{Kind: TokenWord, Text: "version"},
			},
		},
		{
			name:  "guard with bind",
			input: "guard r <- bpfman program get $pid",
			want: []Token{
				{Kind: TokenWord, Text: "guard"},
				{Kind: TokenWord, Text: "r"},
				{Kind: TokenBind, Text: "<-"},
				{Kind: TokenWord, Text: "bpfman"},
				{Kind: TokenWord, Text: "program"},
				{Kind: TokenWord, Text: "get"},
				{Kind: TokenVarRef, Text: "$pid", VarName: "pid"},
			},
		},
		{
			name:  "bind inside bare word stays part of word",
			input: "a<-b",
			want: []Token{
				{Kind: TokenWord, Text: "a<-b"},
			},
		},
		{
			name:  "bind inside quoted string is literal",
			input: `"a <- b"`,
			want: []Token{
				{Kind: TokenQuoted, Text: "a <- b"},
			},
		},
		{
			name:  "bare less-than without dash is word content",
			input: "$a < $b",
			want: []Token{
				{Kind: TokenVarRef, Text: "$a", VarName: "a"},
				{Kind: TokenWord, Text: "<"},
				{Kind: TokenVarRef, Text: "$b", VarName: "b"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Tokenise(tt.input)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, stripLocs(got))
		})
	}
}

func TestTokeniseLineContinuation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "backslash newline folds into whitespace",
			input: "foo \\\nbar",
			want: []Token{
				{Kind: TokenWord, Text: "foo"},
				{Kind: TokenWord, Text: "bar"},
			},
		},
		{
			name:  "multiple continuations in one command",
			input: "bpfman load file \\\nfoo.o \\\n--programs xdp:pass",
			want: []Token{
				{Kind: TokenWord, Text: "bpfman"},
				{Kind: TokenWord, Text: "load"},
				{Kind: TokenWord, Text: "file"},
				{Kind: TokenWord, Text: "foo.o"},
				{Kind: TokenWord, Text: "--programs"},
				{Kind: TokenWord, Text: "xdp:pass"},
			},
		},
		{
			name:  "CRLF continuation",
			input: "foo \\\r\nbar",
			want: []Token{
				{Kind: TokenWord, Text: "foo"},
				{Kind: TokenWord, Text: "bar"},
			},
		},
		{
			name:  "continuation does not cross a real separator after a space backslash space",
			input: "foo \\ \nbar",
			want: []Token{
				{Kind: TokenWord, Text: "foo"},
				{Kind: TokenWord, Text: "\\"},
				{Kind: TokenSep, Text: "\n"},
				{Kind: TokenWord, Text: "bar"},
			},
		},
		{
			name:  "continuation preserves following line separator when consumed",
			input: "a \\\nb\nc",
			want: []Token{
				{Kind: TokenWord, Text: "a"},
				{Kind: TokenWord, Text: "b"},
				{Kind: TokenSep, Text: "\n"},
				{Kind: TokenWord, Text: "c"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Tokenise(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, stripLocs(got))
		})
	}
}

func TestTokeniseInterpString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantKind TokenKind
		wantText string
		wantSegs []InterpSegment
		wantErr  string
	}{
		{
			name:     "plain double-quoted string stays TokenQuoted",
			input:    `"hello"`,
			wantKind: TokenQuoted,
			wantText: "hello",
		},
		{
			name:     "empty double-quoted string stays TokenQuoted",
			input:    `""`,
			wantKind: TokenQuoted,
			wantText: "",
		},
		{
			name:     "single interpolation",
			input:    `"${x}"`,
			wantKind: TokenInterpString,
			wantSegs: []InterpSegment{
				{Inner: "x"},
			},
		},
		{
			name:     "literal before interpolation",
			input:    `"pre-${x}"`,
			wantKind: TokenInterpString,
			wantSegs: []InterpSegment{
				{Literal: "pre-", IsLit: true},
				{Inner: "x"},
			},
		},
		{
			name:     "literal after interpolation",
			input:    `"${x}-suf"`,
			wantKind: TokenInterpString,
			wantSegs: []InterpSegment{
				{Inner: "x"},
				{Literal: "-suf", IsLit: true},
			},
		},
		{
			name:     "adjacent interpolations",
			input:    `"${a}${b}"`,
			wantKind: TokenInterpString,
			wantSegs: []InterpSegment{
				{Inner: "a"},
				{Inner: "b"},
			},
		},
		{
			name:     "mixed literal and interpolations",
			input:    `"pre-${a}-mid-${b}-suf"`,
			wantKind: TokenInterpString,
			wantSegs: []InterpSegment{
				{Literal: "pre-", IsLit: true},
				{Inner: "a"},
				{Literal: "-mid-", IsLit: true},
				{Inner: "b"},
				{Literal: "-suf", IsLit: true},
			},
		},
		{
			name:     "expression inside interpolation",
			input:    `"${$x + 1}"`,
			wantKind: TokenInterpString,
			wantSegs: []InterpSegment{
				{Inner: "$x + 1"},
			},
		},
		{
			name:     "single-quoted interp body keeps '}' literal",
			input:    `"${$x eq 'a}b'}"`,
			wantKind: TokenInterpString,
			wantSegs: []InterpSegment{
				{Inner: "$x eq 'a}b'"},
			},
		},
		{
			name:     "single-quoted string is never interpolated",
			input:    `'${x}'`,
			wantKind: TokenQuoted,
			wantText: "${x}",
		},
		{
			name:    "bare $ in double quotes is rejected",
			input:   `"hello $world"`,
			wantErr: "'$' in double-quoted string must be followed by '{...}'",
		},
		{
			name:    "empty interpolation is rejected",
			input:   `"${}"`,
			wantErr: "empty interpolation",
		},
		{
			name:    "unterminated interpolation",
			input:   `"${x"`,
			wantErr: "unterminated",
		},
		{
			name:    "nested double-quote inside interpolation rejected",
			input:   `"${$x eq "b"}"`,
			wantErr: "unterminated interpolation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Tokenise(tt.input)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tt.wantKind, got[0].Kind)
			if tt.wantKind == TokenQuoted {
				assert.Equal(t, tt.wantText, got[0].Text)
				return
			}
			require.Len(t, got[0].Segments, len(tt.wantSegs))
			for i, want := range tt.wantSegs {
				gotSeg := got[0].Segments[i]
				assert.Equal(t, want.IsLit, gotSeg.IsLit, "segment %d IsLit", i)
				assert.Equal(t, want.Literal, gotSeg.Literal, "segment %d Literal", i)
				assert.Equal(t, want.Inner, gotSeg.Inner, "segment %d Inner", i)
			}
		})
	}
}

func TestTokeniseDoubleQuotedEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantKind TokenKind
		wantText string   // for TokenQuoted
		wantSegs []string // for TokenInterpString: literal segments only (non-lit shown as "")
		wantErr  string
	}{
		{
			name:     "newline escape",
			input:    `"a\nb"`,
			wantKind: TokenQuoted,
			wantText: "a\nb",
		},
		{
			name:     "tab escape",
			input:    `"a\tb"`,
			wantKind: TokenQuoted,
			wantText: "a\tb",
		},
		{
			name:     "carriage return escape",
			input:    `"a\rb"`,
			wantKind: TokenQuoted,
			wantText: "a\rb",
		},
		{
			name:     "backslash escape",
			input:    `"a\\b"`,
			wantKind: TokenQuoted,
			wantText: `a\b`,
		},
		{
			name:     "double-quote escape",
			input:    `"he said \"hi\""`,
			wantKind: TokenQuoted,
			wantText: `he said "hi"`,
		},
		{
			name:     "dollar escape keeps literal dollar",
			input:    `"price=\$5"`,
			wantKind: TokenQuoted,
			wantText: `price=$5`,
		},
		{
			name:     "escape inside interpolated string",
			input:    `"line1\n${n}\n"`,
			wantKind: TokenInterpString,
			wantSegs: []string{"line1\n", "", "\n"},
		},
		{
			name:    "unknown escape rejected",
			input:   `"\q"`,
			wantErr: `unknown escape sequence '\q'`,
		},
		{
			name:    "trailing backslash rejected",
			input:   `"\\`,
			wantErr: "unterminated",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Tokenise(tt.input)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tt.wantKind, got[0].Kind)
			if tt.wantKind == TokenQuoted {
				assert.Equal(t, tt.wantText, got[0].Text)
				return
			}
			require.Len(t, got[0].Segments, len(tt.wantSegs))
			for i, want := range tt.wantSegs {
				got := got[0].Segments[i]
				if want == "" {
					assert.False(t, got.IsLit, "segment %d should be interp", i)
				} else {
					assert.True(t, got.IsLit, "segment %d should be literal", i)
					assert.Equal(t, want, got.Literal)
				}
			}
		})
	}
}

func TestTokeniseDoubleQuotedEscape_HashInsideStringIsNotAComment(t *testing.T) {
	t.Parallel()

	// stripComment runs before the string lexer and walks
	// quote state by hand. It must understand backslash
	// escapes so that an escaped double quote does not flip
	// the in-string flag, otherwise a '#' later in the same
	// string is mistaken for a real inline comment and the
	// rest of the line is stripped. The double-quote escape
	// is a documented part of the string syntax, so this is
	// the load-bearing seam between the two passes.
	input := `let x = "he said \"hi#nope\""` + "\n" + `print done`
	tokens, err := Tokenise(input)
	require.NoError(t, err)
	var quoted *Token
	for i := range tokens {
		if tokens[i].Kind == TokenQuoted {
			quoted = &tokens[i]
			break
		}
	}
	require.NotNil(t, quoted, "expected a TokenQuoted; tokens=%v", tokens)
	assert.Equal(t, `he said "hi#nope"`, quoted.Text,
		"the '#' inside the escaped-quote string is part of the string, not a comment")
}

func TestTokeniseSingleQuotedIsLiteral(t *testing.T) {
	t.Parallel()

	// Single quotes are fully literal: no escape processing.
	// "\n" inside single quotes stays two characters.
	got, err := Tokenise(`'a\nb'`)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, TokenQuoted, got[0].Kind)
	assert.Equal(t, `a\nb`, got[0].Text)
}

// stripLocs zeroes source.Pos fields on a slice of tokens so tests that
// care about kind/text/etc. can compare against literals without
// having to spell out every token's position. Dedicated source.Pos
// assertions belong in TestTokeniseLoc.
func stripLocs(tokens []Token) []Token {
	if tokens == nil {
		return nil
	}
	out := make([]Token, len(tokens))
	for i, t := range tokens {
		t.Span = source.Span{}
		out[i] = t
	}
	return out
}

func TestTokeniseLoc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		// wantLocs maps a token's 0-based index to its expected
		// source.Pos. Only the listed tokens are checked.
		wantLocs map[int]source.Pos
	}{
		{
			name:     "single word starts at 1:1",
			input:    "help",
			wantLocs: map[int]source.Pos{0: {Line: 1, Col: 1}},
		},
		{
			name:  "second word on same line",
			input: "show program 123",
			wantLocs: map[int]source.Pos{
				0: {Line: 1, Col: 1},  // show
				1: {Line: 1, Col: 6},  // program
				2: {Line: 1, Col: 14}, // 123
			},
		},
		{
			name:  "tokens on later lines",
			input: "first\nsecond\nthird",
			wantLocs: map[int]source.Pos{
				0: {Line: 1, Col: 1}, // first
				1: {Line: 1, Col: 6}, // \n
				2: {Line: 2, Col: 1}, // second
				3: {Line: 2, Col: 7}, // \n
				4: {Line: 3, Col: 1}, // third
			},
		},
		{
			name:  "leading whitespace shifts column",
			input: "   help",
			wantLocs: map[int]source.Pos{
				0: {Line: 1, Col: 4},
			},
		},
		{
			name:  "varref and bind sigil carry start column",
			input: "let r <- show $prog",
			wantLocs: map[int]source.Pos{
				0: {Line: 1, Col: 1},  // let
				1: {Line: 1, Col: 5},  // r
				2: {Line: 1, Col: 7},  // <-
				3: {Line: 1, Col: 10}, // show
				4: {Line: 1, Col: 15}, // $prog
			},
		},
		{
			name:  "token after comment preserves column",
			input: "show # c\nnext",
			wantLocs: map[int]source.Pos{
				0: {Line: 1, Col: 1}, // show
				1: {Line: 1, Col: 9}, // \n (comment body replaced with spaces)
				2: {Line: 2, Col: 1}, // next
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Tokenise(tt.input)
			require.NoError(t, err)
			for idx, want := range tt.wantLocs {
				require.Greater(t, len(got), idx, "token index %d out of range; got %d tokens", idx, len(got))
				assert.Equal(t, want, got[idx].Pos, "token %d (%q)", idx, got[idx].Text)
			}
		})
	}
}

func TestTokeniseAt_UsesAbsoluteSourcePosition(t *testing.T) {
	t.Parallel()

	got, err := TokeniseAt(source.Pos{File: "main.bpfman", Line: 40, Col: 3}, "let x = 1\nprint $x")
	require.NoError(t, err)
	require.Len(t, got, 7)

	assert.Equal(t, source.Pos{File: "main.bpfman", Line: 40, Col: 3}, got[0].Pos)
	assert.Equal(t, source.Pos{File: "main.bpfman", Line: 40, Col: 7}, got[1].Pos)
	assert.Equal(t, source.Pos{File: "main.bpfman", Line: 40, Col: 9}, got[2].Pos)
	assert.Equal(t, source.Pos{File: "main.bpfman", Line: 40, Col: 11}, got[3].Pos)
	assert.Equal(t, source.Pos{File: "main.bpfman", Line: 40, Col: 12}, got[4].Pos)
	assert.Equal(t, source.Pos{File: "main.bpfman", Line: 41, Col: 1}, got[5].Pos)
	assert.Equal(t, source.Pos{File: "main.bpfman", Line: 41, Col: 7}, got[6].Pos)
}

func TestIsIdent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"prog", true},
		{"_private", true},
		{"myVar2", true},
		{"MY_CONST", true},
		{"a", true},
		{"", false},
		{"123", false},
		{"1abc", false},
		{"my-var", false},
		{"my.var", false},
		{"my var", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isIdent(tt.input))
		})
	}
}
