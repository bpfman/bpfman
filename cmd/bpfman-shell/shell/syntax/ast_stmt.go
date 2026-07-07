package syntax

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strings"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// Program is the root of a parsed source unit: an ordered sequence
// of statements with the source location of the first token.
type Program struct {
	// Stmts is the ordered top-level statement sequence.
	Stmts []Stmt

	source.Span
}

// Stmt is the sealed sum type for statements. Embedding Node
// lets every Stmt be passed to Inspect without an explicit
// type assertion at the call site.
type Stmt interface {
	Node
	stmtNode()
}

// Ident is a parsed identifier exactly as it appeared in source.
// Text is the identifier spelling; Span points at the identifier
// token itself. Binding and definition forms use Ident rather than a
// bare string so the syntax tree preserves both the interpreter-facing
// name and the editor/diagnostic-facing source location.
type Ident struct {
	// Text is the identifier spelling exactly as it appeared in
	// source.
	Text string

	source.Span
}

// LetStmt binds the result of evaluating RHS to Name. Name is
// guaranteed to be a valid identifier by the parser.
type LetStmt struct {
	// Name is the identifier being bound.
	Name Ident

	// RHS is the expression whose evaluated result is bound to Name.
	RHS Expr

	source.Span
}

// LetDestructureStmt binds the positional elements of a list
// expression to a fixed-length name list: `let (a b) = EXPR`,
// `let (a _ c) = EXPR`. RHS must evaluate to a list of length
// len(Names); each non-'_' name binds to its element. The parser
// rejects single-name parenthesised forms, comma separators,
// duplicate real names, and all-underscore name lists; runtime
// errors fire when RHS is not a list or its length does not match.
type LetDestructureStmt struct {
	// Names is the fixed-length list of target identifiers; each
	// non-"_" name binds the element at its position.
	Names []Ident

	// RHS is the expression that must evaluate to a list of length
	// len(Names).
	RHS Expr

	source.Span
}

// BindStmt runs Cmd and binds either its operation outcome (`let`)
// or its unwrapped declared value (`guard`). Two surface forms parse
// here:
//
//	let NAME <- CMD
//	guard NAME <- CMD
//
// Bind-collect sets Collect instead of Cmd:
//
//	let NAME <- foreach X in LIST { BODY }
//	guard NAME <- foreach X in LIST { BODY }
//
// BODY is iterated once per element of LIST; the body's last
// statement must be a CommandStmt and is executed as the bind's
// producer. For guard collect, every producer must succeed and
// Target receives the collected declared values. For let collect,
// Target receives the aggregate outcome with per-iteration
// results and successful values. continue skips a particular
// iteration's accumulation; break terminates iteration and binds
// the partial collection.
//
// Exactly one of Cmd and Collect is non-nil. "_" as a target name
// discards the bind result.
type BindStmt struct {
	// Target is the identifier receiving the bind result; "_"
	// discards it.
	Target Ident

	// Cmd is the command form whose result is bound; nil when
	// Collect is set.
	Cmd *CommandStmt

	// Collect is the bind-collect foreach loop that produces the
	// bound collection; nil when Cmd is set.
	Collect *ForEachStmt

	// Guard is true for the `guard NAME <- ...` form, which binds the
	// unwrapped declared value and halts on failure; false for the
	// `let NAME <- ...` form, which binds the operation outcome.
	Guard bool

	source.Span
}

// IfBranch pairs a condition expression with a block body. Used
// for the primary branch and each elif.
type IfBranch struct {
	// Cond is the condition evaluated to decide whether Body runs.
	Cond Expr

	// Body is the statements executed when Cond is true.
	Body []Stmt

	source.Span
}

// IfStmt is an if-elif-else conditional.
type IfStmt struct {
	// Cond is the primary branch condition.
	Cond Expr

	// Then is the body run when Cond is true.
	Then []Stmt

	// Elifs is the ordered elif branches, tried in turn when Cond
	// and all preceding elif conditions are false.
	Elifs []IfBranch

	// Else is the body run when Cond and every elif condition are
	// false; nil when there is no else.
	Else []Stmt

	source.Span
}

// CommandStmt is a plain command invocation. The first element of
// Args names the command.
type CommandStmt struct {
	// Args is the command and its arguments; the first element names
	// the command to run.
	Args []Expr

	source.Span
}

// ExprStmt is an expression appearing in statement position. The
// parser emits one whenever the leading token of a statement can
// only start an expression -- a quoted string, a list literal, a
// parenthesised group, a negate, a not, a $-reference, an
// interpolated string, or a [EXPR] substitution -- and routes the
// rest of the line through parseExprStmt; the result is wrapped
// here. At runtime an ExprStmt is evaluated and its value
// discarded, which gives the statement form for expressions whose
// only purpose is the side effect of evaluation (a pure call, a
// command substitution that runs for its rc envelope, etc.).
type ExprStmt struct {
	// Expr is the expression evaluated for its side effect; its value
	// is discarded.
	Expr Expr

	source.Span
}

// ForEachStmt iterates a block over the elements of a list. At
// eval time List is evaluated to a Value; it must be a structured
// list, and each element is bound across Names in the Session for
// the duration of its iteration. The bindings are body-scoped:
// any prior binding of a name is restored on exit and a name that
// did not exist before the loop disappears again.
type ForEachStmt struct {
	// Names is the loop variable(s) bound on each iteration; with
	// more than one name each element is destructured positionally
	// across them.
	Names []Ident

	// List is the expression evaluated to the structured list
	// iterated over.
	List Expr

	// Body is the statements run once per element, with Names bound
	// for the duration of each iteration.
	Body []Stmt

	source.Span
}

// BreakStmt terminates the nearest enclosing ForEachStmt. Outside
// a loop it is a runtime error.
type BreakStmt struct{ source.Span }

// ContinueStmt skips the remainder of the current ForEachStmt
// iteration and advances to the next element. Outside a loop it
// is a runtime error.
type ContinueStmt struct{ source.Span }

// PollStmt retries a block until it reaches the end without an
// explicit retry, or until its timeout budget is exhausted.
type PollStmt struct {
	// Timeout is the total budget after which polling gives up.
	Timeout time.Duration

	// Every is the delay between attempts.
	Every time.Duration

	// Body is the statements run on each attempt; reaching the end
	// without an explicit retry stops polling.
	Body []Stmt

	source.Span
}

// RetryStmt requests another poll attempt. It is valid inside a
// poll body and inside helper defs that are executed from a poll.
// Message is optional; Unless is optional and gates the retry so
// `retry unless EXPR` becomes a no-op when EXPR is true.
type RetryStmt struct {
	// Message is the optional diagnostic shown when the poll budget
	// is exhausted; nil when omitted.
	Message Expr

	// Unless is the optional guard expression; when it evaluates to
	// true the retry is a no-op. nil when omitted.
	Unless Expr

	source.Span
}

// AssertStmt is the syntax-owned assertion statement.
type AssertStmt struct {
	// IsRequire distinguishes `require` (true: a failure halts the
	// script) from `assert` (false).
	IsRequire bool

	// Clause is the assertion body: an expression clause or a
	// command-status clause.
	Clause AssertClause

	source.Span
}

// DeferStmt registers a cleanup command for the enclosing defer
// scope.
type DeferStmt struct {
	// Cmd is the cleanup command registered to run when the
	// enclosing defer scope unwinds.
	Cmd *CommandStmt

	source.Span
}

// DefParam is one declared def parameter: a name plus an optional
// type annotation. An annotated parameter parses bare-word
// arguments into the declared type at bind time and requires
// already-typed arguments to match; an empty Type keeps the
// untyped baseline (words bind as strings, variables keep their
// value kinds).
type DefParam struct {
	// Name is the parameter identifier.
	Name Ident

	// Type is the optional annotation, one of DefParamTypes; empty
	// for an untyped parameter.
	Type string
}

// DefParamTypes lists the accepted parameter annotation types in
// the order error messages cite them.
var DefParamTypes = []string{"number", "string", "bool"}

// IsJSONNumber reports whether text is exactly one JSON number.
// This is the validation a `number` parameter annotation applies to
// a bare word, because the shell stores numbers as json.Number:
// anything looser (Go's ParseFloat accepts NaN, Inf, hex floats,
// and a leading +) would smuggle values into json.Number that every
// JSON-oriented path downstream rejects. JSON's grammar also
// forbids leading zeros, which is the right strictness for an
// input boundary.
func IsJSONNumber(text string) bool {
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return false
	}
	if _, ok := v.(json.Number); !ok {
		return false
	}
	// Exactly one value: trailing data means the word was not a
	// single number.
	if dec.Decode(&v) != io.EOF {
		return false
	}
	if IsIntegralJSONNumber(text) {
		return true
	}
	f, err := json.Number(text).Float64()
	return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}

// IsIntegralJSONNumber reports whether text is a JSON number whose
// syntax denotes an integer. It assumes callers have already
// validated the full JSON-number grammar with IsJSONNumber when
// they need to reject malformed input.
func IsIntegralJSONNumber(text string) bool {
	return !strings.ContainsAny(text, ".eE")
}

// DefStmt declares a user-defined command.
type DefStmt struct {
	// Name is the command name being defined.
	Name Ident

	// Params is the ordered parameter list.
	Params []DefParam

	// Body is the statements run when the command is invoked.
	Body []Stmt

	source.Span
}

// ReturnStmt is the value-publishing exit from a def body.
type ReturnStmt struct {
	// Expr is the value published as the def's result. The parser
	// requires it; bare `return` is rejected.
	Expr Expr

	source.Span
}

func (*LetStmt) stmtNode()            {}
func (*LetDestructureStmt) stmtNode() {}
func (*BindStmt) stmtNode()           {}
func (*DeferStmt) stmtNode()          {}
func (*IfStmt) stmtNode()             {}
func (*CommandStmt) stmtNode()        {}
func (*ExprStmt) stmtNode()           {}
func (*ForEachStmt) stmtNode()        {}
func (*BreakStmt) stmtNode()          {}
func (*ContinueStmt) stmtNode()       {}
func (*PollStmt) stmtNode()           {}
func (*RetryStmt) stmtNode()          {}
func (*DefStmt) stmtNode()            {}
func (*ReturnStmt) stmtNode()         {}
func (*AssertStmt) stmtNode()         {}
