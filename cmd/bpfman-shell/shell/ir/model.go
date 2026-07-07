// IR types for the bpfman-shell lowered intermediate
// representation. This file defines only the data model. The
// dumper, the lowerer, and the interpreter sit in sibling files
// within the same package.
//
// The IR is block-structured. A Program has an entry
// block for the top-level script body, a list of every block
// reachable from that entry in emission order, and a list of
// user-defined commands. Each block holds a sequence of Instr
// values; the last instruction in a block is its terminator and
// names the next block(s) explicitly via *BasicBlock pointers.
//
// Instructions form a sealed sum via the unexported instrNode()
// marker, mirroring the existing Stmt and Expr discipline in
// ast_stmt.go and ast_expr.go. Every instruction embeds a source.Span so the
// dump and the interpreter can point back at source coordinates
// without an out-of-band table.
//
// Eval, BuildArgs, and Assert carry lowered expression operands,
// so the runtime never reconstructs syntax.Expr trees to execute
// program behaviour.

package ir

import (
	"strings"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// Program is the lowered form of a parsed *syntax.Program. It
// owns the top-level entry block, every block reachable from
// that entry in emission order, and every user-defined command
// declared in source order. The temp counter for the body
// records how many Temp values were allocated so the dumper
// and the interpreter can size their tables once.
type Program struct {
	// Defs are the lowered top-level defs, one per source DefStmt,
	// in declaration order. The interpreter hoists and pre-registers
	// them into the session before any body instruction runs.
	Defs []*Def

	// Body is the entry block of the top-level script body; control
	// starts here once the defs are registered.
	Body *BasicBlock

	// Blocks holds every block reachable from Body in emission order
	// (the order the lowerer placed them). The dumper labels them
	// bb0, bb1, ... by this order.
	Blocks []*BasicBlock

	// NumTemps is how many Temp slots the body allocated; the
	// interpreter and dumper size their temp tables to this count.
	NumTemps int

	source.Span
}

// Param is one declared def parameter. Type is the optional
// annotation ("number", "string", "bool"); empty means untyped,
// keeping the baseline binding rule (words bind as strings,
// variables keep their value kinds).
type Param struct {
	// Name is the parameter identifier as written in the def header.
	Name string

	// Type is the optional type annotation ("number", "string",
	// "bool"); empty means untyped, so a word argument binds as a
	// string and a variable keeps its own value kind.
	Type string
}

// String renders the parameter as it appears in source.
func (p Param) String() string {
	if p.Type != "" {
		return p.Name + ": " + p.Type
	}
	return p.Name
}

// ParamList renders a parameter list as it appears in source.
func ParamList(params []Param) string {
	texts := make([]string, 0, len(params))
	for _, p := range params {
		texts = append(texts, p.String())
	}
	return strings.Join(texts, " ")
}

// Def is the lowered form of a DefStmt: a named callable
// with an ordered parameter list, its own entry block, and its
// own block table. Params keeps the original parameter names
// for diagnostics; binding happens inside the def's entry block
// via the usual EnterFrame / BindName sequence.
type Def struct {
	// Name is the def's invocation keyword.
	Name string

	// Params are the declared parameters in order; parameter i binds
	// against temp slot i, which the interpreter populates with the
	// call's i-th argument.
	Params []Param

	// HasReturn is true when the body contains a reachable return
	// statement, so a caller knows the def publishes a primary value.
	HasReturn bool

	// Entry is the def body's entry block.
	Entry *BasicBlock

	// Blocks holds every block in the def body in emission order; a
	// def has its own label and temp namespace, separate from the
	// program body and from other defs.
	Blocks []*BasicBlock

	// NumTemps is how many Temp slots the def body allocated,
	// including the leading parameter slots.
	NumTemps int

	source.Span
}

// BasicBlock is a straight-line sequence of instructions ending
// in a terminator that names its successors via *BasicBlock
// pointers. The dumper assigns deterministic labels (bb0, bb1,
// ...) in emission order; blocks have no inherent identity
// beyond their pointer.
type BasicBlock struct {
	// Instrs is the block's instruction sequence in execution order.
	// The final element is the terminator and names the successor
	// block(s); the preceding instructions are non-terminators that
	// fall through to the next.
	Instrs []Instr

	source.Span
}

// Temp identifies a temporary value produced inside a single
// Program body or Def body. Temp values are
// allocated in strict program order so the dumper can render
// them as t0, t1, ... without a separate name table. The
// containing unit's NumTemps field is the upper bound.
type Temp uint32

// Instr is the sealed sum of lowered instructions. Every
// concrete instruction is a pointer type so interpreters can
// share state via pointer identity if they ever need to, and
// so the dumper can pattern-match without value-copying source.Span
// fields.
type Instr interface {
	// instrNode is the unexported marker that seals the Instr sum;
	// only types in this package can implement it.
	instrNode()
}

// FrameKind classifies an EnterFrame instruction. The kind
// records why the frame opened so the dumper, the interpreter,
// and any future stack-walker can speak about frames in the
// same vocabulary as the design documents.
type FrameKind int

const (
	// FrameDef is the frame opened for a def-call body.
	FrameDef FrameKind = iota + 1

	// FrameIfBranch is the frame opened for a taken if/elif/else
	// branch so bindings introduced in the branch do not leak.
	FrameIfBranch

	// FrameForEachIter is the frame opened for one foreach iteration
	// so iter-bound names are local to that iteration.
	FrameForEachIter

	// FramePollAttempt is the frame opened for one poll attempt body.
	FramePollAttempt
)

// EnterFrame opens a new runtime frame. Frames nest LIFO;
// ExitFrame closes the innermost one. The Kind field is
// observational, not semantic: the interpreter behaves the
// same regardless, but diagnostics and the dump rely on it.
type EnterFrame struct {
	// Kind records why the frame opened (def, if-branch,
	// foreach iteration, poll attempt); it drives diagnostics and
	// the dump, not the interpreter's behaviour.
	Kind FrameKind

	source.Span
}

// ExitFrame closes the innermost frame. The lowerer is
// responsible for pairing EnterFrame and ExitFrame across
// every control path that can leave a frame, including the
// cleanup path that runs defers on the way out.
type ExitFrame struct {
	source.Span
}

// DeferScopeKind classifies an EnterDeferScope. It feeds the dump
// and one interpreter decision: in skipProgramScope mode a
// DeferScopeProgram open is a no-op, so the body appends to the
// driver's outer program scope rather than shadowing it. How a
// scope drains is decided by the matching RunDefers (see
// RunDefersPolicy), not by this kind.
type DeferScopeKind int

const (
	// DeferScopeProgram is the top-level program defer scope,
	// drained at script exit.
	DeferScopeProgram DeferScopeKind = iota + 1

	// DeferScopeDef is a def-body defer scope, drained when the def
	// returns.
	DeferScopeDef

	// DeferScopePollAttempt is a poll-attempt defer scope, drained at
	// the end of each attempt.
	DeferScopePollAttempt
)

// EnterDeferScope opens a new defer scope. Subsequent
// RegisterDefer instructions attach to this scope until the
// matching RunDefers fires.
type EnterDeferScope struct {
	// Kind records which defer scope is opening; the matching
	// RunDefers carries the policy that drains the scope.
	Kind DeferScopeKind

	source.Span
}

// RegisterDefer queues an argv, built earlier into a Temp via
// BuildArgs, onto the innermost open defer scope. Argument
// values are frozen at registration time; the dispatch itself
// runs at scope exit, in LIFO order, using Policy to resolve
// the deferred head.
type RegisterDefer struct {
	// Argv is the temp holding the argv built by an earlier
	// BuildArgs; its argument values are frozen now and the command
	// runs only when the scope unwinds.
	Argv Temp

	// Policy is the head-resolution rule used to resolve the deferred
	// command when it finally dispatches.
	Policy DispatchPolicy

	// Trace requests a "defer ..." trace line when the driver's trace
	// hook is enabled.
	Trace bool

	source.Span
}

// RunDefersPolicy names how the scope unwinds. Program-level
// unwind runs at script exit; def-local unwind runs at
// function return; attempt-fatal unwind runs at the end of one
// poll attempt regardless of success or retry.
type RunDefersPolicy int

const (
	// RunDefersProgram unwinds the program-level scope at script
	// exit.
	RunDefersProgram RunDefersPolicy = iota + 1

	// RunDefersDefLocal unwinds a def-body scope at function return;
	// the interpreter counts its cleanup failures so a failed def
	// cleanup can mark the def's bind result failed.
	RunDefersDefLocal

	// RunDefersAttemptFatal unwinds a poll-attempt scope at the end
	// of one attempt; a cleanup failure here is fatal to the
	// enclosing poll rather than retried.
	RunDefersAttemptFatal
)

// RunDefers unwinds the innermost defer scope under the named
// policy. The instruction does not pop the frame; ExitFrame
// does that separately so the dump can show return-stash
// before cleanup ordering explicitly.
type RunDefers struct {
	// Policy selects which scope is being drained and how a cleanup
	// failure is treated.
	Policy RunDefersPolicy

	source.Span
}

// Eval evaluates a lowered expression and stores the result in
// Dst. The instruction is named Eval because the IR context already
// makes expression lowering explicit; the dumper emits the design
// doc's illustrative spelling, eval_expr.
type Eval struct {
	// Dst is the temp that receives the evaluated value.
	Dst Temp

	// Expr is the lowered expression to evaluate.
	Expr Expr

	source.Span
}

// BuildArgs evaluates an ordered list of lowered argument
// expressions and packages them into a runtime argv stored in
// Dst.
type BuildArgs struct {
	// Dst is the temp that receives the packaged argv ([]Arg).
	Dst Temp

	// Args are the lowered argument expressions, evaluated left to
	// right into the argv.
	Args []Expr

	source.Span
}

// DispatchPolicy names the shell-level head-resolution policy an
// instruction uses. The policy is intentionally narrower than the
// full cmd-side runtime lane story: the IR records what the shell
// package itself guarantees (for example, "defs first, then
// ExecBind"), while the concrete builtin / driver / external split
// behind ExecBind or ExecCommand remains outside the IR boundary.
type DispatchPolicy int

const (
	// DispatchPolicyDefThenExecBind resolves the head as a user def
	// first and otherwise dispatches in bind position (ExecBind).
	DispatchPolicyDefThenExecBind DispatchPolicy = iota + 1

	// DispatchPolicyDefThenExecCommand resolves the head as a user
	// def first and otherwise dispatches in command position
	// (ExecCommand).
	DispatchPolicyDefThenExecCommand
)

// DispatchBind invokes the command named by Argv in bind
// position, producing a bind-result Temp that an ApplyBind can
// then consume. Bind dispatch may route to a builtin, a user
// def, or an external command; Policy names the shell-level
// resolution rule the interpreter must follow.
type DispatchBind struct {
	// Dst is the temp that receives the BindResult (rc envelope plus
	// primary value) for a later ApplyBind to consume.
	Dst Temp

	// Argv is the temp holding the argv to invoke.
	Argv Temp

	// CallPos is the source position of the command head, used to
	// frame diagnostics at the call site; the interpreter falls back
	// to the instruction span when it is unset.
	CallPos source.Pos

	// Policy is the head-resolution rule the interpreter must follow.
	Policy DispatchPolicy

	// TraceHeader is the "let X <- " / "guard X <- " prefix rendered
	// before the argv when tracing; empty disables the trace line.
	TraceHeader string

	source.Span
}

// DispatchCommand invokes the command named by Argv in
// statement position; no bind result is consumed. Failures
// flow through the program-level error policy rather than
// through an ApplyBind. Policy names the shell-level
// resolution rule the interpreter must follow.
type DispatchCommand struct {
	// Argv is the temp holding the argv to invoke.
	Argv Temp

	// Policy is the head-resolution rule the interpreter must follow.
	Policy DispatchPolicy

	// Trace requests an argv trace line when the driver's trace hook
	// is enabled.
	Trace bool

	source.Span
}

// ApplyBind consumes the result of a DispatchBind and binds it into
// the current frame. An empty Target means discard. When Guard is
// true the instruction branches to OnFail on a non-ok envelope and
// otherwise binds the unwrapped primary value. Non-guard binds always
// bind the operation outcome so failure can be inspected as data.
//
// Argv references the same temp DispatchBind read so a guard
// failure raised in OnFail can carry the original argv to the
// diagnostic site. Without it the resulting *GuardFailure has
// no Args slot to populate.
type ApplyBind struct {
	// Src is the temp holding the BindResult produced by the matching
	// DispatchBind.
	Src Temp

	// Argv is the same argv temp the DispatchBind read; re-reading it
	// lets a guard failure carry the original arguments to the
	// diagnostic site.
	Argv Temp

	// Target is the identifier to bind; empty (or "_") discards the
	// result.
	Target string

	// Guard selects guard semantics: branch to OnFail on a non-ok
	// envelope and bind the unwrapped primary on success. When false,
	// bind the whole outcome so failure is inspectable as data.
	Guard bool

	// OnFail is the block entered on guard failure; nil for non-guard
	// binds.
	OnFail *BasicBlock

	source.Span
}

// BindName binds an identifier in the current frame to the
// value held in Src. Used for let, foreach iteration
// variables, and def parameter binding -- anywhere a name is
// introduced without a bind-position dispatch.
type BindName struct {
	// Name is the identifier introduced in the current frame.
	Name string

	// Src is the temp holding the value to bind.
	Src Temp

	// TracePrefix is rendered before the value when tracing (e.g.
	// "let x = "); empty disables the trace line.
	TracePrefix string

	source.Span
}

// BindDestructure binds a positional name list against a list
// value held in Src. Each element of Names binds to the
// corresponding element of the list; an entry of "_" discards
// that slot. The interpreter validates list shape and length;
// the IR carries only the binding intent.
type BindDestructure struct {
	// Names are the positional names to bind against the list
	// elements; "_" discards that slot.
	Names []string

	// Src is the temp holding the list value to destructure.
	Src Temp

	// Trace requests a destructure trace line when the driver's trace
	// hook is enabled.
	Trace bool

	source.Span
}

// BuildEnvelope synthesises a result envelope in Dst from a literal
// exit code and diagnostic string, ready for an EmitBindResult to
// publish as its rc.
type BuildEnvelope struct {
	// Dst is the temp that receives the synthesised envelope value.
	Dst Temp

	// ExitCode is the envelope's exit code; zero renders as ok.
	ExitCode int

	// Err is a diagnostic string; it lands in the envelope's Stderr,
	// which holds the human-readable failure reason.
	Err string

	source.Span
}

// EmitBindResult publishes a bind result from a callee frame:
// the rc envelope and the primary value. A nil Rc means the
// interpreter should synthesise an ok envelope from program
// state; a nil Primary means no primary value is being
// published.
type EmitBindResult struct {
	// Rc is the optional temp holding the rc envelope; nil means
	// synthesise an ok envelope from program state.
	Rc *Temp

	// Primary is the optional temp holding the primary value; nil
	// means publish no primary.
	Primary *Temp

	source.Span
}

// EmitResult forwards a value to the driver's PrintResult hook.
// Used for top-level expression statements in shell programs
// where a bare expression should auto-print the evaluated value. A
// nil PrintResult hook makes the instruction a no-op.
type EmitResult struct {
	// Src is the temp holding the value to forward to PrintResult.
	Src Temp

	// Trace requests an "expr ..." trace line when the driver's trace
	// hook is enabled.
	Trace bool

	source.Span
}

// TraceNote emits a one-line execution trace message when the
// driver's trace hook is enabled. It is execution metadata, not
// part of the canonical lowered dump: lowering uses it for
// control-flow constructs whose trace coverage is about branch or
// lifecycle choice rather than about a produced value.
type TraceNote struct {
	// Msg is the one-line trace message (for example "if then",
	// "break", "poll attempt").
	Msg string

	source.Span
}

// Stop halts the current execution unit.
type Stop struct {
	source.Span
}

// Jump transfers control unconditionally to Target. The
// terminator at the end of any block that does not need to
// branch, return, propagate, or stop.
type Jump struct {
	// Target is the block control transfers to.
	Target *BasicBlock

	source.Span
}

// Branch transfers control based on a boolean Temp.
type Branch struct {
	// Cond is the temp holding the boolean condition.
	Cond Temp

	// True is the block entered when Cond is true.
	True *BasicBlock

	// False is the block entered when Cond is false.
	False *BasicBlock

	source.Span
}

// ReturnValue is the terminator that exits the enclosing def
// with Src as the published primary value. To names the def's
// shared epilogue block; the interpreter copies Src into a
// return slot and jumps to To, where def-local defers run
// before the frame closes. This preserves the documented
// return-stash-before-cleanup order while keeping every
// ReturnStmt in the source routed to a single cleanup block.
type ReturnValue struct {
	// Src is the temp holding the value to publish as the def's
	// primary.
	Src Temp

	// To is the def's shared epilogue block, where def-local defers
	// run before the frame closes.
	To *BasicBlock

	// Trace requests a "return ..." trace line when the driver's
	// trace hook is enabled.
	Trace bool

	source.Span
}

// PropagateError exits the enclosing unit by re-raising the
// pending error. Used by guard failure paths after defers
// have run, when the caller's frame is the one that decides
// what happens next.
type PropagateError struct {
	source.Span
}

// PropagateGuardFailure raises a synthetic GuardFailure without
// going through DispatchBind / ApplyBind.
type PropagateGuardFailure struct {
	// Primary is the bound name the failure is attributed to (the
	// guard's primary slot).
	Primary string

	// Head is the command head, recorded as the failure's single
	// argument.
	Head string

	// ArgSpan is the source span of the head argument, used to
	// position the synthetic argument in diagnostics.
	ArgSpan source.Span

	// OK records the intended success flag. The interpreter rebuilds
	// the envelope from ExitCode (Envelope.OK derives ok from
	// exit_code == 0), so this field currently has no reader.
	OK bool

	// ExitCode is the synthetic envelope's exit code.
	ExitCode int

	// Stdout is the synthetic envelope's captured stdout.
	Stdout string

	// Stderr is the synthetic envelope's captured stderr (the
	// human-readable failure reason).
	Stderr string

	// Killed reports whether the synthetic envelope's command was
	// killed by a signal.
	Killed bool

	// Signal names the signal that killed the command, when Killed.
	Signal string

	// HasPID reports whether PID carries a meaningful process id.
	HasPID bool

	// PID is the process id recorded on the synthetic envelope, when
	// HasPID.
	PID int

	source.Span
}

// BeginPoll opens a polling region. The interpreter enters Attempt
// first; explicit RetryPoll terminators loop back here until timeout,
// and ordinary failures remain fatal.
type BeginPoll struct {
	// Timeout is the overall deadline for the poll region; once it
	// elapses, the next retry transfers to OnTimeout.
	Timeout time.Duration

	// Every is the sleep inserted before each retry attempt; zero
	// means retry without delay.
	Every time.Duration

	// Attempt is the block entered for each poll attempt, including
	// the first.
	Attempt *BasicBlock

	// OnTimeout is the block entered when the deadline elapses before
	// an attempt succeeds.
	OnTimeout *BasicBlock

	// OnSuccess is the block entered once an attempt completes without
	// retrying.
	OnSuccess *BasicBlock

	source.Span
}

// RetryPoll requests another poll attempt. Message names an optional
// Temp whose value is rendered on timeout as the last retry reason.
type RetryPoll struct {
	// Message is an optional temp whose value is rendered on timeout
	// as the last retry reason; nil means no reason.
	Message *Temp

	source.Span
}

// Fail is a structural lowering-time surface that becomes an
// explicit control-flow exit: a condition the lowerer can
// prove at compile time should never reach. Msg is the
// diagnostic the interpreter raises if it does.
type Fail struct {
	// Msg is the diagnostic the interpreter raises if control ever
	// reaches this instruction.
	Msg string

	source.Span
}

// Assert evaluates one assertion clause at runtime and reports
// the outcome to the session's assertion policy via the Env. The
// IsRequire flag distinguishes 'require' (halt on failure) from
// 'assert' (record and continue). Lowered execution dispatches
// this directly through Env.ExecAssert.
type Assert struct {
	// IsRequire selects 'require' semantics (halt on failure) over
	// 'assert' semantics (record the outcome and continue).
	IsRequire bool

	// Clause is the lowered assertion clause to evaluate (an
	// expression or a command form).
	Clause AssertClause

	source.Span
}

// ForEach is the structured pseudo-terminator that opens a
// foreach loop. List names the Temp holding the evaluated list
// (it is the iterator's input). Names is the destructure shape
// applied to each element; len(Names) == 1 binds the element
// itself, len(Names) > 1 destructures it. Body is the block
// entered once per element and must end in a ForEachContinue
// terminator; Exit is the block control enters after the loop
// finishes naturally or via a break Jump.
type ForEach struct {
	// List is the temp holding the evaluated list to iterate over.
	List Temp

	// Names is the destructure shape applied to each element: a
	// single name binds the element itself, multiple names
	// destructure it.
	Names []string

	// Body is the block entered once per element; it must end in a
	// ForEachContinue terminator.
	Body *BasicBlock

	// Exit is the block control enters after the loop finishes
	// naturally or via a break.
	Exit *BasicBlock

	source.Span
}

// ForEachContinue is the terminator at the end of a foreach
// body block. The interpreter pops every frame opened during
// this iteration -- including the iter frame itself --
// advances the iterator, and either re-enters the loop body
// (re-opening the iter frame and re-binding Names) or transfers
// to the loop's Exit block when exhausted. Continue statements
// in the source lower to a ForEachContinue terminator on a
// separate block.
type ForEachContinue struct {
	source.Span
}

// ExitLoop is the terminator a break statement lowers to. The
// interpreter pops every frame opened during the iteration --
// including the innermost loop's iter frame -- and transfers
// to that loop's Exit block. Break inside nested loops always
// targets the innermost loop; outer loops are unaffected.
type ExitLoop struct {
	source.Span
}

// RegisterDef installs a Def into the session. Canonical
// lowering hoists top-level defs into Program.Defs and
// pre-registers them before body execution, so lowered output
// does not emit RegisterDef; the instruction is an escape hatch
// for hand-built IR and tests that want an explicit
// def-publication step.
type RegisterDef struct {
	// Def is the def to install into the session.
	Def *Def

	source.Span
}

// ForEachCollect is the structured pseudo-terminator for a
// bind-collect: `let X <- foreach Y in $list { ... }`. The
// body executes once per element; its trailing CommandStmt
// dispatches in bind position and the result is collected.
// Guard collect binds the collected declared values after all
// iterations succeed. Let collect binds an aggregate outcome with
// per-iteration results and successful values.
type ForEachCollect struct {
	// List is the temp holding the evaluated list to iterate over.
	List Temp

	// Names is the destructure shape applied to each element, as in
	// ForEach.
	Names []string

	// Target is the identifier the collected result binds to; empty
	// (or "_") discards it.
	Target string

	// Guard selects guard-collect semantics (bind the collected
	// values, halting on the first failing iteration) over
	// let-collect semantics (bind an aggregate outcome).
	Guard bool

	// Body is the block entered once per element; it must end in a
	// CollectProduce terminator.
	Body *BasicBlock

	// Exit is the block control enters once the collection finishes.
	Exit *BasicBlock

	source.Span
}

// CollectProduce is the terminator at the end of a
// ForEachCollect body block. Result names the Temp produced
// by the trailing DispatchBind; the interpreter splits the
// envelope from the primary value and routes each into its
// accumulator before advancing the iterator.
type CollectProduce struct {
	// Result is the temp holding the BindResult produced by the
	// body's trailing DispatchBind, split into envelope and primary
	// accumulators.
	Result Temp

	// FrameSpan is the span used to frame a guard-collect failure at
	// the bind-collect statement; the interpreter falls back to the
	// instruction span when it is unset.
	FrameSpan source.Span

	source.Span
}

func (*EnterFrame) instrNode()            {}
func (*ExitFrame) instrNode()             {}
func (*EnterDeferScope) instrNode()       {}
func (*RegisterDefer) instrNode()         {}
func (*RunDefers) instrNode()             {}
func (*Eval) instrNode()                  {}
func (*BuildArgs) instrNode()             {}
func (*DispatchBind) instrNode()          {}
func (*DispatchCommand) instrNode()       {}
func (*ApplyBind) instrNode()             {}
func (*BindName) instrNode()              {}
func (*BuildEnvelope) instrNode()         {}
func (*EmitBindResult) instrNode()        {}
func (*EmitResult) instrNode()            {}
func (*TraceNote) instrNode()             {}
func (*Stop) instrNode()                  {}
func (*Jump) instrNode()                  {}
func (*Branch) instrNode()                {}
func (*ReturnValue) instrNode()           {}
func (*PropagateError) instrNode()        {}
func (*PropagateGuardFailure) instrNode() {}
func (*BeginPoll) instrNode()             {}
func (*RetryPoll) instrNode()             {}
func (*Fail) instrNode()                  {}
func (*Assert) instrNode()                {}
func (*ForEach) instrNode()               {}
func (*ForEachContinue) instrNode()       {}
func (*BindDestructure) instrNode()       {}
func (*ForEachCollect) instrNode()        {}
func (*CollectProduce) instrNode()        {}
func (*ExitLoop) instrNode()              {}
func (*RegisterDef) instrNode()           {}
