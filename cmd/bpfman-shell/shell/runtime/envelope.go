package runtime

import (
	"encoding/json"
	"strconv"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// Envelope is the result envelope returned alongside every command
// form. It carries execution metadata only:
//
//	ok      derived as exit_code == 0. For an async job that was
//	        killed, ok stays false because
//	        the process did not exit zero; the script
//	        distinguishes "expected termination" from "real
//	        failure" via the killed and signal fields.
//	exit_code
//	        exit code (subprocess) or 0/1 (in-process). For a
//	        signalled process, exit_code is the conventional
//	        128+signum (matching shell convention), so
//	        SIGTERM yields 143, SIGUSR1 yields 138, and so on.
//	        A trap that exits with its own status overrides
//	        the convention.
//	stdout  captured stdout, or in-process renderable
//	stderr  captured stderr, or in-process error message
//	killed  true when the script called 'kill $job' against
//	        this job. Lets the script say "the process exited
//	        non-zero, but I asked for it" without conflating
//	        that case with a real failure.
//	signal  short signal name (TERM, KILL, USR1, ...) when the
//	        process exited via signal, empty otherwise.
//	pid     process id, present only when HasPID is true; the
//	        pid field is omitted from the wrapped Value's
//	        path-walkable shape when HasPID is false.
//
// The provider's typed payload (the primary) lives in its own
// slot, not on the envelope. See BindResult.
type Envelope struct {
	// ExitCode is the subprocess exit code, or 0/1 for an in-process
	// command. A signalled process follows the shell convention of
	// 128+signum (SIGTERM yields 143), unless a trap exits with its
	// own status. OK derives ok from ExitCode == 0.
	ExitCode int

	// Stdout is the captured standard output, or the in-process
	// renderable result.
	Stdout string

	// Stderr is the captured standard error, or the in-process error
	// message.
	Stderr string

	// Killed is true when the script called 'kill $job' against this
	// job, letting the script distinguish a requested termination
	// from a real failure.
	Killed bool

	// Signal is the short name of the signal that ended the process
	// (TERM, KILL, USR1, ...), or empty when the process exited
	// normally.
	Signal string

	// HasPID reports whether PID is meaningful. When false the pid
	// field is omitted from the wrapped Value's path-walkable shape.
	HasPID bool

	// PID is the process id, valid only when HasPID is true.
	PID int
}

// OK reports whether the operation succeeded. ExitCode is the single
// source of truth, so impossible combinations such as ok=true with
// exit_code=5 are not representable in Go.
func (e Envelope) OK() bool {
	return e.ExitCode == 0
}

// ValueFromEnvelope wraps e as a Value. The Value carries e in the
// origin slot (recoverable via Origin()) and a JSON-tree mirror in
// the standard v slot so the path machinery resolves $r.ok,
// $r.exit_code, $r.stdout, $r.stderr, and $r.pid (when HasPID).
func ValueFromEnvelope(e Envelope) Value {
	mirror := map[string]any{
		"ok":        e.OK(),
		"exit_code": numFromInt(e.ExitCode),
		"stdout":    e.Stdout,
		"stderr":    e.Stderr,
		"killed":    e.Killed,
		"signal":    e.Signal,
	}
	if e.HasPID {
		mirror["pid"] = numFromInt(e.PID)
	}
	return Value{v: mirror, origin: e, kind: semantics.OriginEnvelope}
}

// ValueFromOutcome wraps the complete result of a bind-position
// operation. It carries the envelope fields and, when the provider
// produced a distinct primary payload, a value field preserving that
// payload's origin for typed follow-on use.
func ValueFromOutcome(result BindResult) Value {
	fields := envelopeFields(result.Rc)
	var valueOrigin any
	if hasDistinctPrimary(result.Primary) {
		fields["value"] = result.Primary
		valueOrigin = result.Primary.Origin()
	}
	return ValueFromRecord(fields).withOrigin(outcomeOrigin{Value: valueOrigin}, semantics.OriginEnvelope)
}

// ValueFromCollectOutcome builds the aggregate result for
// `let r <- foreach ...`. results is the authoritative
// per-iteration list; values is the successful unwrapped payload
// projection.
func ValueFromCollectOutcome(ok bool, results, values []Value) Value {
	return ValueFromRecord(map[string]Value{
		"ok":      BoolValue(ok),
		"results": valueList(results),
		"values":  valueList(values),
	}).WithKind(semantics.OriginEnvelope)
}

func envelopeFields(e Envelope) map[string]Value {
	fields := map[string]Value{
		"ok":        BoolValue(e.OK()),
		"exit_code": ValueFromAny(numFromInt(e.ExitCode)).WithKind(semantics.OriginScalar),
		"stdout":    StringValue(e.Stdout),
		"stderr":    StringValue(e.Stderr),
		"killed":    BoolValue(e.Killed),
		"signal":    StringValue(e.Signal),
	}
	if e.HasPID {
		fields["pid"] = ValueFromAny(numFromInt(e.PID)).WithKind(semantics.OriginScalar)
	}
	return fields
}

func hasDistinctPrimary(v Value) bool {
	return !v.IsNil() && v.Kind() != semantics.OriginEnvelope
}

func valueList(values []Value) Value {
	raw := make([]any, 0, len(values))
	origins := make([]any, 0, len(values))
	for _, v := range values {
		raw = append(raw, v.Raw())
		origins = append(origins, v.Origin())
	}
	return ValueFromAny(raw).withOrigin(origins, semantics.OriginUnknown)
}

type outcomeOrigin struct {
	Value any `json:"value"`
}

// OkEnvelope returns the canonical "command succeeded with no
// specific payload" envelope: ExitCode=0, no streams.
// Used by dispatch sites that synthesise a successful outcome
// from scratch -- a def body that ran cleanly, a poll
// attempt that satisfied its body without retry. Sites that
// have real outcome data (a subprocess's captured streams,
// an actual exit code from RunExternal) build Envelope{...}
// directly because they have richer information than this
// helper can express.
func OkEnvelope() Envelope {
	return Envelope{}
}

// FailEnvelope returns the canonical "command failed without a
// more specific source" envelope: ExitCode=1, no streams. Sites
// that have a specific failure exit code (a subprocess that exited
// non-zero, a guard-failure envelope from a registered handler)
// build Envelope{...} directly with the real exit code.
func FailEnvelope() Envelope {
	return Envelope{ExitCode: 1}
}

// FailEnvelopeFromError returns a FailEnvelope with err's
// message in Stderr. Used by structural-failure paths where
// the failure has no captured stderr of its own (a defer
// whose dispatch failed without ever launching, a
// builtin-resolution error before the handler ran).
func FailEnvelopeFromError(err error) Envelope {
	e := FailEnvelope()
	if err != nil {
		e.Stderr = err.Error()
	}
	return e
}

// BindResult is what an ExecBind hook returns. Rc is the result
// envelope; Primary is the provider's primary result. For
// providers that produce a typed payload, Primary is the typed
// Value. For providers that produce no separate payload (exec,
// bpftool, wait), Primary is ValueFromEnvelope(Rc) so a
// single-name bind hands the script a uniformly-shaped value to
// inspect. On failure for typed-payload providers, Primary is the
// zero Value.
type BindResult struct {
	// Rc is the result envelope carrying the execution metadata
	// (ok/exit code, captured streams, kill state).
	Rc Envelope

	// Primary is the provider's primary result. For typed-payload
	// providers it is the typed Value (the zero Value on failure);
	// for providers with no separate payload it is
	// ValueFromEnvelope(Rc).
	Primary Value
}

// numFromInt wraps a Go int as a json.Number so the path-walker and
// Scalar() resolve it through the same code path that handles
// numbers parsed from JSON via UseNumber.
func numFromInt(n int) json.Number {
	return json.Number(strconv.Itoa(n))
}
