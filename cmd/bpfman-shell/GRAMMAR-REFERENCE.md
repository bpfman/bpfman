# bpfman-shell Grammar Reference

This is a syntax-only reference for the language accepted by
`cmd/bpfman-shell`. It is intentionally separate from
`GRAMMAR-RATIONALE.md`, which records the design rationale, the
tokenisation trade-offs, and the parser gotchas behind this grammar.

The parser and tests remain the authority. This document describes the
current surface grammar, including typed `def` parameters, and calls out
tokenisation-dependent behaviours that should not be treated as stable
language guarantees.

## Notation

Productions use an EBNF-style notation:

```ebnf
X        = expansion .
'word'   literal source text
[ X ]    optional
{ X }    zero or more
X | Y     alternative
```

`Word`, `Quoted`, `VarRef`, `AdapterRef`, `InterpString`, `Assign`,
`Bind`, `Thread`, and `Sep` name token kinds emitted by the lexer.
`Ident` means a word matching the identifier predicate. Names in
binding positions are whitespace-separated unless a production says
otherwise.

## Lexical Model

bpfman-shell is argv-first. In command positions, most CLI-shaped text
is preserved as `Word` tokens: flags, paths, `key=value`,
colon-qualified arguments, and comma-bearing arguments normally stay
opaque. Expression syntax appears only at expression sites.

```ebnf
Sep          = newline | ';' .
Assign       = '=' .
Bind         = '<-' .
Thread       = '|>' .
Ident        = letter-or-underscore { letter | digit | '_' } .
VarRef       = '$' Ident [ Path ] | '${' Ident [ Path ] '}' .
AdapterRef   = Adapter ':' VarRef .
Quoted       = single-quoted-string | double-quoted-string-without-interp .
InterpString = double-quoted-string-with-${ ExpressionText } .
Path         = PathSegment { PathSegment } .
PathSegment  = '.' Ident | '[' Integer ']' | '[' '$' Ident ']' .
```

The lexer strips comments beginning with `#` outside quotes. A backslash
immediately followed by a newline is a line continuation and does not
emit `Sep`.

Several operator spellings are contextual:

- `+`, `*`, and `%` split as single-character `Word` tokens.
- `-` and `/` are word characters in normal lexing, so binary `-` and
  `/` require separation from neighbouring operands.
- `=`, `<-`, and `|>` are standalone tokens only at token boundaries;
  glued spellings such as `x=1`, `x<-cmd`, and `a|>b` are plain words.

## Program

```ebnf
Program      = { Sep } [ Statement { SepSeq Statement } { Sep } ] EOF .
Block        = '{' { Sep } [ Statement { SepSeq Statement } { Sep } ] '}' .
SepSeq       = Sep { Sep } .
Statement    = IfStmt
             | LetStmt
             | GuardStmt
             | DeferStmt
             | DefStmt
             | ReturnStmt
             | ForEachStmt
             | PollStmt
             | RetryStmt
             | BreakStmt
             | ContinueStmt
             | AssertStmt
             | RequireStmt
             | ExprStmt
             | CommandStmt .
```

Statement parsing is line-oriented at top level: a `Sep` ends most
statements, while balanced parentheses, brackets, record literals, and
matches blocks keep their contents together.

## Commands

```ebnf
CommandStmt  = CommandHead { CommandArg } .
CommandHead  = Word .
CommandArg   = Word
             | Quoted
             | VarRef
             | AdapterRef
             | InterpString
             | '(' Expression ')'
             | ListLiteral .
ExprStmt     = Expression .
```

A command argument is normally a primary value. Parenthesised arguments
are expression islands, so `print ($snap |> jq ".id")` is one computed
argument. A list literal in argument position is likewise one argument.

An expression statement is used only when the first token cannot be a
plain command head, such as `$value`, a quoted string, `(`, `not`, or an
interpolated string. Bare word-led statements are commands unless a
statement keyword routes them elsewhere.

`Assign` is not a command argument; use `let name = expr` for
assignment.

## Bind, Guard, And Defer

```ebnf
LetStmt      = 'let' Ident Assign Expression
             | 'let' '(' NameList ')' Assign Expression
             | 'let' Ident Bind BindRHS .

GuardStmt    = 'guard' Ident Bind BindRHS .
DeferStmt    = 'defer' CommandStmt .

BindRHS      = CommandStmt
             | ForEachCollect .
ForEachCollect = 'foreach' ForEachNames 'in' Expression Block .
NameList     = Name Name { Name } .
Name         = Ident | '_' .
```

`let (a b) = expr` destructures a list value. The list must contain at
least two slots, duplicate real names are rejected, and an all-`_` list
is rejected.

Tuple bind after `<-` is not part of the current grammar. Bind a single
outcome name and refer to named fields instead.

`guard` has no assignment form; it always runs a command form and gates
the script on success.

## Definitions

```ebnf
DefStmt      = 'def' Ident '(' [ DefParams ] ')' Block .
DefParams    = DefParam { [ Sep ] DefParam } .
DefParam     = Ident [ ':' DefParamType ] .
DefParamType = 'number' | 'string' | 'bool' .
ReturnStmt   = 'return' Expression .
```

Parameter separators are whitespace and optional newlines/semicolons
inside the parentheses. Commas are rejected.

Typed parameters use a spaced annotation form:

```bpfman
def f(a: number b c: string d: bool) {
  print $a $b $c $d
}
```

The glued spelling `a:number` is rejected. Parameter names must be real
identifiers: `_`, duplicates, invalid identifiers, and reserved
statement/operator words are not valid `def` parameter or definition
names.

## Control Statements

```ebnf
IfStmt       = 'if' Condition Block { Sep 'elif' Condition Block }
               [ Sep 'else' Block ] .
Condition    = Expression .

ForEachStmt  = 'foreach' ForEachNames 'in' Expression Block .
ForEachNames = Ident | '_' | '(' NameList ')' .

PollStmt     = 'poll' 'timeout' Duration 'every' Duration Block .
RetryStmt    = 'retry'
             | 'retry' Expression
             | 'retry' 'unless' Expression
             | 'retry' Expression 'unless' Expression .
BreakStmt    = 'break' .
ContinueStmt = 'continue' .
```

`foreach` destructuring uses parentheses for multiple names and
whitespace between names. A single parenthesised name, commas, duplicate
real names, and all-`_` destructuring lists are rejected.

`poll` requires both `timeout` and `every`, each followed by a
Go-style duration literal accepted by `time.ParseDuration`, such as
`5s` or `100ms`.

## Assertions

```ebnf
AssertStmt   = 'assert' Assertion .
RequireStmt  = 'require' Assertion .
Assertion    = Expression
             | [ 'not' ] AssertCommand .
AssertCommand = ( 'ok' | 'fail' ) { CommandArg } .
```

The expression form is the steady-state assertion syntax. The
command-shaped `ok` and `fail` forms are retained as command-status
assertions.

## Expressions

```ebnf
Expression       = OrExpr .
OrExpr           = AndExpr { 'or' AndExpr } .
AndExpr          = NotExpr { 'and' NotExpr } .
NotExpr          = 'not' NotExpr | ComparisonExpr .
ComparisonExpr   = AdditiveExpr
                   [ CompareOp AdditiveExpr | MatchesExpr ] .
CompareOp        = '==' | '!=' | '<' | '<=' | '>' | '>=' .
MatchesExpr      = 'matches' [ 'exhaustive' ] MatchesBlock .
AdditiveExpr     = MultiplicativeExpr { ( '+' | '-' ) MultiplicativeExpr } .
MultiplicativeExpr =
                   PredicateExpr { ( '*' | '/' | '%' ) PredicateExpr } .
PredicateExpr    = 'not-empty' Primary | NegateExpr .
NegateExpr       = '-' NegateExpr | ThreadExpr .
ThreadExpr       = Primary { Thread ThreadCommand } .
ThreadCommand    = CommandArg { CommandArg } .
Primary          = Literal
                 | Quoted
                 | VarRef
                 | AdapterRef
                 | InterpString
                 | '(' Expression ')'
                 | ListLiteral
                 | RecordLiteral
                 | PureCall .
```

`and`, `or`, and `not` are keyword operators in expression positions.
`not-empty` is the current expression unary predicate. `true` and
`false` are literal words with value semantics at evaluation time.
`null` is dual: bare `null` is a literal, while `null EXPR` is the
arity-1 present-and-null predicate registered as a pure builtin (see
below), so `assert null $x.field` parses as a predicate call rather
than a literal followed by a stray argument. It is true only when the
path resolves to a JSON null; a missing path is false (use the
separate `missing` predicate for that), and `present` is its
non-missing complement.

Comparison accepts one comparison or one `matches` operator at its
precedence level. Arithmetic chains are left-associative. Threading is
left-associative and puts the left-hand value into the right-hand
command call at evaluation time.

Registered pure builtins parse as fixed-arity calls with primary
arguments. Compound arguments to pure builtins should be parenthesised.
The registered set is `jq`, `u32le`, `u64le`, `range`, `zip`, and the
predicates `path-exists`, `contains`, `present`, `missing`, `null`, and
`empty`. The value-shaping builtins (`jq`/`u32le`/`u64le`/`range`/`zip`)
return a value; the predicates return a boolean and are the expression
forms used in `assert`, `require`, and `if` conditions:

```ebnf
PureCall   = PureName { Primary } .
PureName   = 'jq' | 'u32le' | 'u64le' | 'range' | 'zip'
           | 'path-exists' | 'contains' | 'present' | 'missing'
           | 'null' | 'empty' .
```

## Literals And Structured Values

```ebnf
ListLiteral   = '[' { Sep | ExpressionElement } ']' .
ExpressionElement = Primary | '(' Expression ')' .

RecordLiteral = 'record' '{' { Sep | RecordField } '}' .
RecordField   = Ident ':' Primary .
```

List elements are whitespace-separated; commas are rejected. Compound
list elements should be parenthesised, for example `[($x + 1) $y]`.

Record fields use `name:` followed by one primary value. Fields are
whitespace-separated and field names must be unique identifiers. A
whitespace-separated comma (`a: 1 , b: 2`) is rejected outright. A comma
glued to a value also rejects in record value position (`a: 1,` and
`a: foo,` are both errors), because records are structured syntax rather
than command argv. Write fields whitespace-separated with no commas.

## Matches Blocks

```ebnf
MatchesBlock = '{' { MatchesSep | MatchEntry } '}' .
MatchesSep   = newline .
MatchEntry   = MatchPath ':' MatchPattern .
MatchPattern = MatchPredicate
             | Expression
             | 'matches' [ 'exhaustive' ] MatchesBlock .
MatchPredicate = 'not-empty' | 'null' | 'empty' .
MatchPath    = path-word-with-optional-dot-or-index-segments .
```

Matches blocks are newline-separated tables. Semicolons and commas are
not valid entry separators. `matches exhaustive` requires nested
sub-blocks for nested structure; dotted or indexed entry paths are
rejected inside an exhaustive block.

The colon may appear as its own token or attached to the path word
(`field:`). Parser support for a colon attached to the following pattern
is an implementation accommodation, not recommended style.

## Expression Islands

Expression parsing is entered at these sites:

- right-hand side of `let name = expr`;
- destructuring RHS in `let (a b) = expr`;
- `return expr`;
- `if` and `elif` conditions;
- `foreach ... in expr`;
- `retry` message and `retry unless expr`;
- `assert` and `require` expression clauses;
- command arguments written as `(expr)`;
- list literals and record literal values;
- `matches` patterns;
- interpolation bodies inside `"${...}"`;
- thread left-hand sides and right-hand command arguments.

Outside these sites, command text remains argv-shaped and words are not
split as expression syntax merely because they contain punctuation.

## Parser Accidents And Non-Guarantees

The following behaviours are consequences of the current lexer/parser
shape. They should not be relied on as language guarantees:

- Glued command-ish spellings such as `x=1`, `x<-cmd`, `a|>b`, `1/2`,
  and `a:number` are generally single `Word` tokens. The accepted
  language uses separated sigils and, for typed parameters,
  `name: type`.
- The parser sometimes accepts punctuation glued to a neighbouring word
  to improve diagnostics or preserve compatibility, especially around
  matches paths. Prefer the documented separated or trailing-colon forms.
- Keyword-looking words can still appear as opaque command arguments in
  command positions. Their operator meaning is guaranteed only in
  expression islands.
- Newlines inside balanced forms are normally transparent, but newlines
  in matches blocks separate entries. Do not generalise one rule to the
  other.
- Transitional command-status assertions, `assert ok ...` and
  `assert fail ...`, are parsed explicitly but should not be read as a
  general command grammar under `assert`.
- Error-recovery and diagnostic-specific rejections, such as loud comma
  errors at binding sites, document current parser behaviour rather than
  alternate accepted syntax.
