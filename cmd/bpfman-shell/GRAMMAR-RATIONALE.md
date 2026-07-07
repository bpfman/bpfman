# bpfman-shell Grammar Rationale

This document records *why* the bpfman-shell grammar is shaped the way
it is: the design principles behind the lexer, the tokenisation
trade-offs and the gotchas they produce, the places where the current
parser is more permissive than the language rule, and guidance for
anyone deriving a separate grammar (for example a Tree-sitter grammar)
from this code.

It is the companion to two normative documents and adds no new rules of
its own:

- `GRAMMAR-REFERENCE.md` -- the syntax-only surface grammar (productions,
  tokens, precedence).
- `LANGUAGE-SPEC.md` -- the normative semantics of accepted programs.

The parser and AST in `shell/syntax` remain the load-bearing ground
truth. Where this document and that code disagree, the code wins.

## Design principles

### Argv-first, with expression islands

bpfman-shell is an argv-first shell language with expression islands,
not an expression language with command escape hatches. The lexer is
command-biased: it preserves most CLI-shaped text as single Word tokens
so paths, flags, `key=value` pairs, comma-separated program specs, and
colon-qualified arguments can be written without quoting. Expression
syntax is opt-in at specific parser sites -- `let X = EXPR`,
`assert`/`require` clauses, parenthesised command arguments,
interpolation bodies, list literals, `if`/`elif` conditions,
`foreach ... in` operands, and thread pipelines. Everywhere else, Word
text stays opaque.

### Paste-from-shell

A `bpfman ...` command copied from ordinary shell history can usually be
pasted after `let NAME <-`, `guard NAME <-`, `defer`, or inside a block
with little or no rewriting. For example:

    guard loaded <- bpfman program load file \
        testdata/bpf/multi_prog_tcx_counter.bpf.o \
        --programs tcx:mtcx_a,tcx:mtcx_b,tcx:mtcx_c \
        -g "weight_a=0x${u64le $weight_a}" \
        -g "weight_b=0x${u64le $weight_b}"

At command position these arguments lex as boring Word tokens: the
bytecode path, `--programs`, the comma-separated program-spec list,
`-g`, then an InterpString. A "clean" lexer that split `:`, `,`, `/`,
`.`, `=`, `-` everywhere would force every CLI-shaped argument to be
quoted -- the wrong direction for an argv-first language, and it would
break the paste-from-shell property the script corpus relies on.

### Whitespace-significant operators are deliberate

Where the language needs an operator that is also common in command
arguments (`-`, `/`, `<`, `>`, `=`, the comparison operators), the
operator is recognised only at documented parser sites, and usually only
when it appears as a standalone Word. This keeps copied command lines
stable and pushes the small amount of extra ceremony onto
expression-heavy code rather than onto command invocations.

## Tokenisation consequences and gotchas

These follow directly from the command-biased lexer. They are the cases
where the surface you write and the tokens the parser sees diverge.

- **Commas are never a separator.** Binding sites use whitespace between
  names (`def f(a b)`, `let (a b) =`, `foreach (a b) in ...`). Because
  the lexer does not split on `,`, a spelling like `def f(a, b)` lexes as
  `Word("a,") Word("b")`. Every binding-site parser
  (`parseBindTargetName`, `parseForEachNameToken`, `parseDefParams`)
  rejects any token whose text contains a comma, with an explicit "comma
  is not a separator" diagnostic, so a comma-separated spelling fails
  loudly rather than silently mis-binding.

- **`1+2` parses, `1/2` does not.** `+`, `*`, and `%` are delimiter
  Words emitted as single-character tokens regardless of surrounding
  whitespace, so `1+2` and `1 * 2` parse as arithmetic. `-` and `/` are
  compound-Word constituents, so `1/2` lexes as a single Word and is not
  arithmetic. Operators that need surrounding whitespace to emit
  standalone are called out at the relevant production in
  `GRAMMAR-REFERENCE.md`.

- **Sigil gluing is asymmetric.** Assignment, bind, and thread sigils
  (`=`, `<-`, `|>`) only emit as their own tokens at top-level dispatch.
  A bareword left-hand side glues: `let x=1` and `let x<-cmd` lex `x=1`
  and `x<-cmd` as single compound Words and do not parse as their
  intended forms. A VarRef left-hand side does not have this problem:
  `$x|>jq` splits correctly because the VarRef lexer stops at the sigil
  character.

- **Comment stripping preserves source offsets.** `#` outside a quoted
  string starts a comment to end of line, stripped before tokenisation.
  The strip preserves source offsets so error spans stay accurate.

## Line continuation: two models

The primary continuation model is **structural**: a separator token does
not terminate a statement while the parser is inside an open syntactic
form. The statement-token collectors treat newlines and `{` as inert at
positive paren/bracket depth, so an expression spread across lines reads
naturally inside `(...)`, `[...]`, a matches block, a `def` parameter
list, an `if`/`elif` condition before its block, or any other balanced
pair. Inside `{ ... }` block bodies, newlines are ordinary statement
separators.

A backslash immediately followed by a newline (or `\r\n`) is also
accepted as a lexer-level line continuation, recognised only at top-level
positions outside quoted strings. This **backslash** form exists for
paste-from-shell compatibility on a plain argv command line that has no
surrounding syntactic form; reach for it only when the structural
continuation does not apply.

## Language rule versus parser accident

A few constructs behave a certain way only because the recursive-descent
parser feeds isolated tokens to lower layers. These are accidents of
token-at-a-time parsing, not language rules. Anyone deriving a separate
grammar should model the *language rule* and surface the constraints as
syntax errors, not reproduce the accident.

- **Thread RHS accepts only simple atoms** (ThreadWord, Quoted, VarRef,
  AdapterRef, InterpString). Grouped forms like `$x |> foo ($a + 1)` and
  `$x |> foo [1 2 3]` are not supported, even though `(EXPR)` and
  `[ ... ]` are valid command arguments elsewhere. The accident: because
  `(` and `[` lex as Word tokens, the RHS parser sees a solitary
  word-like token with no view of the matching closer; `(` becomes a
  `LiteralExpr("(")` and whether the pipeline continues depends on the
  next token. Model `ThreadRHS` as a sequence of thread atoms with
  delimiter Words excluded; do not reproduce the "lonely `(`" behaviour.

- **`matches {` does not terminate a command-RHS buffer.** When a
  command right-hand-side buffer's tail is the bare word `matches`
  (optionally followed by `exhaustive`), an immediately following `{` is
  absorbed into the buffer rather than treated as a block terminator.
  This is a buffer-collection convenience in the parser, not a feature of
  the command form.

- **A leading `[` at statement position is rejected** with "list literal
  at statement position is not allowed", to keep a bare list from being
  read as a command.

The general guidance: the productions in `GRAMMAR-REFERENCE.md` describe
the intended surface syntax; a derived grammar should model that surface
and surface the "needs whitespace" constraints to the user via
syntax-error highlighting, rather than mirror the parser's
post-tokenisation normalisation.

## Semantic rationale

These explain *why* a few semantic rules in `LANGUAGE-SPEC.md` are the
way they are.

### Cleanup belongs at the call site, not inside a def

A def opens its own defer scope on entry, so anything `defer`ed inside
the body unwinds when the def returns -- before the caller binds the
result. So cleanup for a resource a def returns must be registered by the
caller:

    guard pair <- load_xdp ./xdp.o eth0
    let (prog link) = $pair
    defer bpfman program unload $prog
    defer bpfman link detach $link

Putting `defer bpfman program unload $prog` inside `load_xdp` would
unload the program at function return and leave the caller's `$prog`
naming a freed resource. Registration order matters: unwind is LIFO, so
the example registers unload first and detach second, so detach runs
before unload at scope exit.

### `poll` has no `continue`-means-next-attempt

`poll` deliberately does not let `continue` mean "next attempt": the
attempt boundary is the explicit `retry`, not a user-driven control
transfer. This keeps the attempt loop legible -- you can see every point
that starts a new attempt.

### Interpolation stamps absolute source positions

An interpolation body (`${ ... }`) is parsed as a small expression, and
position information from that inner parse is stamped with the
surrounding source file and absolute line/column. Error spans therefore
point at the offending byte inside the original source without later
rebasing.

## Reserved design intent

`LANGUAGE-SPEC.md` reserves a block-form `defer { CMD ... }`. If it
lands, treat it as sugar over multiple ordinary deferred commands:
lowering should reverse registration so ordinary LIFO unwind still fires
the block's commands in written order at scope exit. Explicit
caller-side `defer` remains the baseline.
