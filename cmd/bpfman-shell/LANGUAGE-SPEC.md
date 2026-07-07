# bpfman-shell Language Specification

This document specifies the bpfman-shell language implemented by
`cmd/bpfman-shell`. It is normative for script authors and for
language tooling unless it explicitly says that behaviour is
implementation-defined, reserved, or unsettled. The surface grammar is
documented separately in `GRAMMAR-REFERENCE.md`, and the design rationale
behind it in `GRAMMAR-RATIONALE.md`; this document describes the
semantics of accepted programs.

The key words "must", "must not", "shall", "should", and "may" are to
be interpreted as normative language. Examples are illustrative only.

## 1. Language Model

bpfman-shell is an argv-first command language with expression islands.
Command text is preserved as shell-like words wherever possible so that
ordinary command lines can be pasted into scripts. Expressions are
entered only at syntactic sites that require them, such as `let x =
EXPR`, `if EXPR`, `retry unless EXPR`, assertion expressions,
parenthesised command arguments, list literals, interpolation bodies, and
thread pipelines.

A program is evaluated as a sequence of statements. Top-level
definitions are hoisted before executable statements run. Executable
statements may bind variables, dispatch commands, evaluate expressions,
enter control constructs, register deferred commands, start or manage
jobs, or record assertion failures.

The implementation may lower parsed source into an internal IR before
execution. That lowering must preserve the observable semantics defined
here: source spans in diagnostics, scope lifetimes, deferred cleanup
ordering, command dispatch precedence, and result shapes.

## 2. Lexical Model

The lexer is command-biased. Most contiguous command-shaped text is a
Word token. In shell mode, whitespace, newline, semicolon, `$`, quotes,
comments, and structural delimiter characters split tokens; punctuation
commonly used in command arguments, including `-`, `/`, `.`, `,`, `:`,
`=`, `<`, `>`, `|`, and `!`, remains part of a compound Word unless a
contextual token is recognised at the main lexer dispatch boundary.

The language has no hard lexical keywords. Reserved words are ordinary
Words until the parser tests their text at a specific site. Therefore a
word such as `foreach`, `matches`, or `retry` may be ordinary argv text
outside its grammar position.

The following token classes have semantic significance:

- Word: bare command text, identifiers, literal text, flags, paths, and
  contextual operators.
- VarRef: `$name` or `$name.path[index]`.
- Quoted: a single- or double-quoted string without interpolation.
- InterpString: a double-quoted string with one or more `${EXPR}`
  interpolation segments.
- AdapterRef: currently `file:$var` or `file:$var.path`.
- Assign: a standalone `=`.
- Bind: a standalone `<-`.
- Thread: a standalone `|>`.
- Sep: newline or semicolon.

Comments start with `#` outside quoted strings and run to the end of the
line. A backslash immediately followed by a line ending is a line
continuation outside quoted strings and does not emit a separator.

Operators whose spelling is also common in command arguments are
contextual. Source that relies on `=`, `<-`, `|>`, comparison operators,
`-`, or `/` being operators must write them in a syntactic position where
the parser expects an expression or binding sigil. In particular,
`let x = 1` is a binding, while `let x=1` is not.

## 3. Program Evaluation

Evaluation is ordered and statement-driven. A program-level defer scope
and job scope exist for the duration of one program run. Each def call
creates its own variable frame, defer scope, and job scope. Foreach
iterations and control-flow branches create variable frames as described
below; they do not create independent program-level cleanup scopes unless
specified by the construct.

Runtime errors are fatal unless a construct explicitly converts them
into a non-fatal result. A command's non-zero exit status is not itself
a Go/runtime structural error: it is represented as a command envelope.
This spec calls that value an envelope throughout; its user-facing
origin-kind name, the one diagnostics print, is `result`. `guard` turns
a non-ok envelope into a fatal guard failure. `let <- COMMAND` exposes
the envelope for inspection.

Top-level expression statements evaluate the expression and pass the
value to the driver's print hook. Embedded evaluators may discard that
value.

## 4. Values

The shell value domain is JSON-compatible with origin metadata. A value
is one of:

- object (`map[string]any`);
- list (`[]any`);
- string;
- number (`json.Number`, with finite float results also rendered as
  JSON-number text);
- boolean;
- explicit JSON null;
- absent value, used internally to mean "no result".

Explicit JSON null is distinct from absence. Predicates and path
lookups must preserve the three-way distinction between a missing field,
a present field whose value is null, and a present non-null value.

Values may carry an origin kind. Origin kinds are not user-defined
types; they are authority and shape tags used by builtins and static
checking. The kinds, by the names they render in diagnostics, are:
`scalar`, `boolean`, `null`, `result` (the captured command outcome,
called an envelope elsewhere in this spec), `job`, `program`, `link`,
`dispatcher`, `map`, `net pair`, `kernel function`, `netns-veth-pair`
and its `netns-veth-pair endpoint`, plus `unknown`. A value with
unknown origin remains usable as JSON-shaped data, but handlers that need
a capability must require the appropriate origin.

Path lookup on `$name.path[index]` walks objects and lists. Accessing a
field on a non-object, indexing a non-list, indexing out of range, or
using a malformed path is an error. Scalar variable expansion for
ordinary command arguments must reject structured values unless the
command argument form explicitly passes a structured value.

`file:$var.path` adapts a shell value to a file argument. The adapter
renders scalar values as their scalar text and structured values as
deterministic pretty-printed JSON with a trailing newline. The temporary
file lifecycle is owned by the dispatching command or job.

## 5. Numbers

Integer-shaped JSON numbers are stored and compared exactly at any
width: equality and the integer operators `+`, `-`, `*`, and `%` never
lose precision the way 64-bit floating point would past 2^53. Division,
and any operation with a fractional or exponent-form operand, produces
a floating-point number; a result that is not finite (NaN or infinity)
is an error rather than a value.

Numeric literals must either be valid JSON numbers or fail; they must
not silently become strings merely
because they are malformed or out of range. A digit-leading token, or a
sign followed by a digit, is read as a numeric literal in expression and
structured-value positions; if it is not a valid JSON number, such as
`5s` or `-3s`, parsing/checking must reject it before evaluation. Quote
it (`"5s"`) when string text is intended. A token that does not start
with a digit or a sign followed by a digit, such as `abc`, is an
ordinary string.
Duration positions are separate from expression values: forms such as
`poll timeout 5s every 100ms` use bare duration literals by design.

Comparisons are strict by kind:

- number-to-number comparisons are numeric (exact for integers);
- string-to-string comparisons use lexical text comparison;
- boolean-to-boolean comparisons support only `==` and `!=`;
- null supports only `==` and `!=`;
- cross-kind comparisons are errors.

Arithmetic operands must be numeric. Division and modulo by zero are
errors. The sign of integer remainder follows Go `big.Int.Rem`
semantics.

## 6. Expressions

Expressions include literals, variable references, lists,
parenthesised expressions, interpolation bodies, unary predicates,
logical operators, comparisons, arithmetic, pure builtin calls, thread
pipelines, and `matches` blocks.

Conditions used by `if`, `elif`, `assert`, `require`, and `retry unless`
must evaluate to a boolean. The language has no general truthiness.
Scripts must use comparisons or predicates such as `not-empty`.

Unquoted expression literals are classified as follows:

- `true` and `false` are booleans;
- `null` is explicit JSON null;
- supported JSON numbers are numbers;
- other non-numeric words are strings;
- quoted literals are always strings.

`not-empty X` returns false for null, absent values, empty strings, empty
lists, empty objects, numeric zero, and `false`; it returns true for the
corresponding non-empty or non-zero values.

The `matches` operator compares a value against a matcher block. It is
an expression operator, not a command tail. A failed assertion over a
`matches` expression should report the individual mismatches and their
paths. `matches exhaustive` additionally requires that the checked shape
not contain unspecified fields.

Pure builtins are side-effect-free expression functions registered by
the shell. The current surface includes `jq`, `range`, `zip`, `u32le`,
and `u64le`. Static arity checks may reject incorrect pure builtin
calls before execution.

## 7. Command Forms and Envelopes

A command form is a command head Word followed by zero or more command
arguments. Command arguments may be literal words, quoted strings,
variable references, adapter references, interpolation strings,
parenthesised expressions, or list literals.

Every bind-position command produces a `BindResult`:

- `Rc`: the command envelope;
- `Primary`: the provider's primary payload.

The envelope contains execution metadata only:

- `ok`, derived from `exit_code == 0`;
- `exit_code`;
- `stdout`;
- `stderr`;
- `killed`;
- `signal`;
- `pid`, when a process id exists.

Typed provider payloads, such as bpfman records or job handles, live in
`Primary`, not inside the envelope. Providers without a distinct payload
use the envelope mirror as their primary.

`let r <- COMMAND` binds the complete outcome. The bound value contains
the envelope fields and, when there is a distinct primary, a `value`
field preserving the primary's origin. `guard x <- COMMAND` requires a
successful envelope and binds the primary directly. On failure, `guard`
halts the script with a guard failure.

Command-position dispatch and bind-position dispatch must resolve user
defs before external commands or builtins where the dispatch policy says
`DefThen...`. Thus a visible def shadows a command of the same name at
those language dispatch sites.

Structural dispatch failures, such as an empty argv, an unknown required
handler, malformed adapter input, or command launch failure, are runtime
errors. Ordinary process exit status is represented in the envelope.

## 8. Bind and Guard

`let NAME <- COMMAND` evaluates a command form and binds an inspectable
outcome. `guard NAME <- COMMAND` evaluates a command form, requires
success, and binds the primary payload.

`_` is a discard target at bind sites. Bind after `<-` accepts a
single target only; tuple bind after `<-` is not part of the language.
Use one outcome name and then destructure its primary value, or use
ordinary `let (a b) = EXPR` on a list value. Duplicate real names in
one destructuring target are invalid; `_` is exempt.

In bind-collect form, the right-hand side is a `foreach` whose body ends
with a command statement. For each produced iteration, the trailing
command is run and its bind result is accumulated.

For `guard xs <- foreach ...`, any non-ok iteration is a guard failure.
On success, `xs` is a list of successful primary values. For `let r <-
foreach ...`, `r.ok` is true only if all produced iterations succeeded,
`r.results` contains one outcome per produced iteration, and `r.values`
contains the primary values from successful iterations. `break` returns
the partial collection. `continue` skips producing a value for that
iteration.

## 9. Assertions and Requirements

`assert EXPR` evaluates a boolean expression. On false it records an
assertion failure, reports a diagnostic, and continues execution.
Assertion failures make the script fail overall.

`require EXPR` evaluates a boolean expression. On false it reports a
diagnostic and halts immediately.

The transitional command-status forms are still valid:

- `assert ok COMMAND`;
- `assert fail COMMAND`;
- `require ok COMMAND`;
- `require fail COMMAND`;
- the same forms with leading `not`.

Named predicates are expression-lane pure calls, not command-status
tails. The current assertion predicates include:

- `path-exists FILE`;
- `contains HAYSTACK NEEDLE`;
- `null TARGET`;
- `present TARGET`;
- `missing TARGET`;
- `empty TARGET`.

`present`, `missing`, `null`, and `empty` must distinguish missing,
null, and present value states. `null` without a target is invalid.
`empty` applies only to empty string, list, or object; missing and null do
not satisfy it.

`assert` is invalid while a poll is active, including in helper defs
called from that poll. Scripts must use `retry unless ...` for
recoverable waiting and `require ...` for fatal invariants. `require`
is fatal everywhere, including inside poll.

## 10. Defs, Calls, and Typed Parameters

`def NAME(PARAMS) { BODY }` declares a user command. Defs must be
top-level declarations after import expansion. Nested defs are invalid.
Parameter names are whitespace-separated identifiers. `_` and duplicate
parameter names are invalid.

Parameters may be annotated as `name: number`, `name: string`, or
`name: bool`. An unannotated parameter uses the baseline command
argument rule:

- bare words bind as strings;
- quoted words bind as strings;
- variable-derived scalar values preserve their value kind;
- structured and adapter-derived values preserve their value.

An annotated parameter is an input boundary. Bare Word arguments are
parsed as the declared type. Already-typed inputs must already match the
declared scalar type; annotations do not coerce typed values. In
particular, a quoted numeric-looking argument is a string and must be
rejected by a `number` parameter. Structured, null, or absent values are
not valid scalar arguments for annotated parameters.

Def calls are arity-checked. Recursive calls are permitted but the
implementation must bound def call depth and report a clean recursion
limit error before exhausting the Go stack.

Defs do not close over definition-time variable frames. A def body
resolves variables against the caller's current frame stack plus its own
call frame. Parameters and body-local bindings disappear when the call
returns.

`return EXPR` is valid only inside a def body. The expression is
mandatory. At command position, a returned value is discarded; the early
exit is the observable effect. At bind position, the returned value is
the primary payload. A def that falls through without `return` produces
a successful envelope primary, matching no-payload command binds.

Def-local defers run before the caller receives a bind-position result.
If a def-local defer fails, `let r <- def_call ...` exposes the cleanup
failure through `r.ok == false`; `guard x <- def_call ...` halts.

## 11. Imports and Hoisting

`import PATH` is parsed as a command statement but recognised by the
frontend before execution. The command head must be the literal word
`import`, and it must have exactly one literal file argument. Relative
paths resolve against the importing script's directory, or against the
provided base directory for stdin-backed input.

Imports must appear at top level. Imported files may contain only
top-level def statements. Imported files must not execute top-level side
effects at import time.

The frontend expands direct imports by splicing imported defs into the
program. Imported libraries are checked in the context of defs already
visible from the importing script and earlier imports. Later imports can
therefore call earlier imported defs, and imported helpers can call main
script defs visible through the frontend's def metadata.

After expansion, top-level defs are hoisted before executable statements
run. Forward calls to visible defs are valid. Duplicate top-level def
names across the expanded program are invalid; diagnostics should cite
the later declaration and the previous declaration.

Transitive import execution is not part of the language surface:
imported libraries are def-only, and an `import` statement in an imported
library is rejected as a non-def top-level statement.

## 12. Scope

Variable bindings live in frames. Lookup walks from the innermost frame
outwards. Binding a name in the current frame shadows an outer binding
for the lifetime of that frame. Rebinding a name in the same frame
updates that frame's binding.

The following constructs introduce, or may be lowered into, frames:

- each def call;
- foreach iterations;
- branch bodies as lowered by the implementation;
- poll attempts and other internal control regions where required for
  cleanup and retry unwinding.

The loop variable of a foreach iteration must not leak after the
iteration or after the loop. Variables bound inside a def must not be
visible after the def returns. Imported def declarations are visible as
defs, not as variables.

`_` is a discard binding at bind, destructure, and foreach binding
sites. It does not create a variable.

## 13. Defer and Cancellation

`defer COMMAND` evaluates the command arguments immediately and pushes
the frozen argv onto the active defer stack. Deferred commands run in
last-in, first-out order when their scope unwinds.

Defers run on normal completion, return, guard failure, runtime error,
and poll retry unwinding. Cleanup continues after a deferred command
fails. Each non-ok deferred command is recorded as a defer failure and
contributes to the script's final failure state.

Deferred commands dispatch through the bind-position dispatch path so def
resolution and command authority match other bind sites. Defers capture
the trace state and source location at registration time for diagnostics.

When the run's root context has been cancelled, defer draining must run
under a fresh bounded cleanup context rather than the already-cancelled
root context. A second hard interrupt may still abort the process.
Within the current driver, the graceful drain budget is an implementation
policy, not a language value.

The language does not currently provide an atomic load-and-defer form.
If cancellation occurs after an external side effect completes but before
the script registers its defer, the language cannot guarantee cleanup of
that side effect. Runners may provide residue reports or sweeps as an
operational backstop.

## 14. Foreach

`foreach NAME in EXPR { BODY }` requires `EXPR` to evaluate to a list.
The single-name form binds each element verbatim. The parenthesised
multi-name form requires each element to be a list of matching length and
destructures it positionally.

Each iteration has a fresh iteration frame. `continue` closes frames
opened during the current iteration and advances to the next element.
`break` closes frames opened during the current iteration and exits the
innermost foreach. `break` and `continue` are valid only with an active
foreach and must not target poll attempts.

Loop control inside helper defs called from foreach is not accepted as a
way to transfer control across the def boundary. Helpers should return
values or envelopes that the caller uses to decide whether to break or
continue.

## 15. Poll and Retry

`poll timeout DUR every DUR { BODY }` is a statement-only retrying
construct. Durations must parse as Go `time.ParseDuration` strings.
There is no default timeout, no default cadence, and no bindable poll
result object.

One poll attempt executes the body from top to bottom. `retry` marks the
attempt as not ready. If the deadline has not elapsed, the attempt-local
frames, loops, and defer scopes are unwound, the evaluator sleeps for the
`every` duration, and the body starts again. If the deadline has
elapsed, poll fails with a timeout diagnostic. `retry MSG` records the
last retry reason for that timeout diagnostic. `retry unless EXPR`
retries when `EXPR` evaluates to false.

Helper defs called while a poll is active may execute `retry`; the retry
targets the caller's active poll attempt. Executing `retry` without an
active poll is a runtime error.

Ordinary command, guard, expression, and require failures inside poll are
fatal. They are not converted into retries. Attempt-local defer failure
during retry unwind is fatal to the poll, because retrying after cleanup
failure compounds leaks.

## 16. Jobs

`start COMMAND ARGS` spawns a background process and returns a job
handle as its primary payload. The job runs as a process-group leader.
Its stdout and stderr are captured. The job handle is a mutable
capability, not an immutable JSON record.

The visible job value includes at least `pid`; it may include
`target_binary` when the producer can publish a stable executable image.
The complete lifecycle state is observed through job builtins.

`wait $job` blocks until the job exits and returns an envelope containing
`ok`, `exit_code`, `stdout`, `stderr`, `killed`, and `signal`. `kill
$job` marks the job managed, signals the process group, and returns an
envelope. By default it sends SIGTERM, waits for a grace period, and
escalates to SIGKILL if needed; custom signals are implementation-defined
by the builtin flags. `jobs` lists jobs in the active scope without
marking them managed. `reap` removes completed jobs from the current job
registry and leaves running jobs alone.

A started job must be managed by `wait` or `kill` before its scope exits.
An unmanaged job at scope exit is a script failure. Drivers should kill
or otherwise clean up leaked jobs when reporting them.

`wait` and `kill` do not automatically remove a job from the ledger;
scripts must use `reap` when they want the `jobs` listing trimmed.

## 17. Static Checking and Runtime Authority

Static checking is a preflight error detector, not the source of runtime
authority. It may reject programs before side effects run when the whole
program proves an error. Current static checks include undefined
variables, duplicate or misplaced defs, import placement and import
library shape, break/continue placement, return placement, def arity,
pure builtin arity, command helper arity where known, literal arithmetic
errors, comparison kind mismatches where known, sealed-shape field
access, job leaks visible in the syntax, invalid kill flags, and literal
mismatches for annotated def parameters.

Runtime remains authoritative for:

- dynamic value kind and shape;
- command dispatch and external process authority;
- bpfman and kernel-side capability checks;
- origin-preserving typed payloads;
- guard failures and command envelopes;
- context cancellation;
- deferred cleanup outcomes;
- job lifecycle races and process exit status.

A program that passes static checking may still fail at runtime. A
runtime or driver path that executes unchecked source must still enforce
the same semantic restrictions.

## 18. Unspecified and Reserved Behaviour

The following behaviours are not part of the current language contract:

- nested defs;
- executable statements in imported libraries;
- transitive imports from imported libraries;
- implicit truthiness for conditions;
- comma-separated binding or parameter lists;
- command substitution;
- block-form defer;
- atomic side-effect plus defer registration;
- user-defined value types or user-defined command authority;
- treating poll as an expression or bindable result.

Implementations should reject these forms clearly rather than assign
accidental semantics.
