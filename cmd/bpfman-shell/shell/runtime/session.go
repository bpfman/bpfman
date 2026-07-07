package runtime

import (
	"maps"
	"slices"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// Session holds variable bindings and user-defined commands (defs)
// for the shell runtime. It is the runtime state that persists across
// commands within a session.
//
// Variables live on a stack of frames. There is always at least one
// frame: the root frame, established by NewSession and never popped
// while the session is alive. Block-shaped constructs (def calls, if
// branches, foreach iterations, and poll attempts) push a fresh
// frame for the duration of the body and pop it on exit. let writes
// to the innermost frame; reads walk outward; deleting from the
// innermost frame leaves outer bindings intact.
//
// Defs are session-level and are not part of the frame stack.
type Session struct {
	frames         []map[string]Value
	defs           map[string]*defValue
	assertFailures int
	deferFailures  int
	jobLeaks       int
	traceEnabled   bool
}

// defValue is a user-defined command registered via the `def NAME(P1
// P2 ...) { BODY }` form. It holds the parameter list, whether the
// body contains a return, and the source location of the declaration
// for diagnostics.
type defValue struct {
	Name      string
	Params    []ir.Param
	HasReturn bool
	source.Span

	// Entry / NumTemps are populated when the def
	// was installed from a lowered program's hoisted top-level
	// def set (or, in hand-built IR, by a RegisterDef
	// instruction). If Entry is non-nil, runDefCall runs
	// the def via the lowered interpreter. The remaining
	// semantic metadata the shell still needs from the original
	// body is HasReturn.
	Entry    *ir.BasicBlock
	NumTemps int
}

// DefSignature is the read-only external view of a registered def.
// Callers that need to display or enumerate defs depend on this
// language-facing shape rather than on the runtime's internal def
// storage.
type DefSignature struct {
	// Name is the registered def's name.
	Name string

	// Params lists the parameter declarations in order, each
	// rendered as it appeared in the def header.
	Params []string
}

// RecordAssertFailure records one failed assertion.
func (s *Session) RecordAssertFailure() {
	s.assertFailures++
}

// AssertFailures returns the number of recorded assertion failures.
func (s *Session) AssertFailures() int {
	return s.assertFailures
}

// RecordDeferFailure increments the defer-failure counter.
func (s *Session) RecordDeferFailure() {
	s.deferFailures++
}

// DeferFailures returns the number of recorded defer failures.
// Drivers consult this after script completion to set the
// process exit code: any non-zero count means at least one
// defer reported a non-ok rc.
func (s *Session) DeferFailures() int {
	return s.deferFailures
}

// RecordJobLeak increments the unmanaged-job counter. The
// scope-exit leak check calls it for each started job that the
// script never waited or killed; drivers consult JobLeaks after
// script completion to fail the exit code.
func (s *Session) RecordJobLeak() {
	s.jobLeaks++
}

// JobLeaks returns the number of unmanaged jobs reported at
// scope exit. A non-zero count means at least one 'start' had
// no matching wait or kill before its enclosing defer scope
// unwound, and the script should fail.
func (s *Session) JobLeaks() int {
	return s.jobLeaks
}

// SetTrace enables or disables execution tracing. Drivers usually
// install an Env.Trace callback whose body consults this so the
// `trace on` / `trace off` builtin (and a startup CLI flag) can
// flip the state mid-session without swapping the Env hook itself.
func (s *Session) SetTrace(on bool) {
	s.traceEnabled = on
}

// TraceEnabled reports whether tracing is currently enabled.
func (s *Session) TraceEnabled() bool {
	return s.traceEnabled
}

// NewSession returns an empty session with a single root frame.
func NewSession() *Session {
	return &Session{
		frames: []map[string]Value{make(map[string]Value)},
		defs:   make(map[string]*defValue),
	}
}

// setDef registers (or replaces) a user-defined command. The caller
// is responsible for validating the name and parameter list.
func (s *Session) setDef(d *defValue) {
	s.defs[d.Name] = d
}

// getDef retrieves a user-defined command. The second return value
// indicates whether a def with that name exists.
func (s *Session) getDef(name string) (*defValue, bool) {
	d, ok := s.defs[name]
	return d, ok
}

// DefSignatures returns the registered defs as sorted read-only
// signatures. The results are stable for display and tests without
// exposing the runtime's internal def records.
func (s *Session) DefSignatures() []DefSignature {
	names := slices.Sorted(maps.Keys(s.defs))
	out := make([]DefSignature, 0, len(names))
	for _, name := range names {
		d := s.defs[name]
		params := make([]string, 0, len(d.Params))
		for _, p := range d.Params {
			params = append(params, p.String())
		}
		out = append(out, DefSignature{
			Name:   d.Name,
			Params: params,
		})
	}
	return out
}

// Set binds a value to a variable name in the innermost frame,
// shadowing any same-named binding in an outer frame. Crossing a
// frame boundary creates a new shadowing binding rather than
// mutating an outer one.
func (s *Session) Set(name string, v Value) {
	s.frames[len(s.frames)-1][name] = v
}

// Get retrieves a variable's value. The lookup walks the frame
// stack from innermost to outermost and returns the first hit, so
// an inner binding shadows an outer one. The second return value
// indicates whether a binding exists in any frame.
func (s *Session) Get(name string) (Value, bool) {
	for i := len(s.frames) - 1; i >= 0; i-- {
		if v, ok := s.frames[i][name]; ok {
			return v, true
		}
	}
	return Value{}, false
}

// FrameDepth returns the current size of the frame stack. The
// root frame counts: an unmodified Session reports depth 1.
// Used by the IR interpreter to remember how deep frames were
// at a loop's start so ForEachContinue and ExitLoop can pop
// every frame opened during one iteration in one shot.
func (s *Session) FrameDepth() int {
	return len(s.frames)
}

// PushFrame appends an empty frame to the stack. Subsequent Set
// calls bind into this frame; Get continues to walk outward and
// can see bindings that the new frame does not shadow.
func (s *Session) PushFrame() {
	s.frames = append(s.frames, make(map[string]Value))
}

// PopFrame removes the innermost frame. PopFrame panics if asked
// to pop the root frame: every Push must be paired with exactly
// one Pop, and an unbalanced Pop is an invariant violation in the
// evaluator. Callers that need exception-safe push/pop should use
// WithFrame.
func (s *Session) PopFrame() {
	if len(s.frames) <= 1 {
		panic("shell.Session.PopFrame: cannot pop root frame")
	}
	s.frames = s.frames[:len(s.frames)-1]
}
