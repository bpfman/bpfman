package runtime

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"

// Arg is the post-expansion representation of a command argument. It
// is a sealed sum type that preserves distinctions lost in []string:
// whether an argument was literal command syntax, a quoted literal,
// an eagerly resolved scalar value, or a structured shell value
// passed directly to a command. Every Arg variant embeds source.Span so
// command-handler parsers can frame argument-position errors at
// the source token (or, for variable-resolved args, at the
// originating $name reference). ArgSpan extracts the source.Span via a
// type switch since Go interfaces cannot share embedded fields.
type Arg interface {
	// isArg is an unexported marker; only types in this package can
	// implement Arg.
	isArg()
}

// ArgSpan returns the source extent of a, or a zero source.Span when a is
// nil or a variant the helper does not recognise. Use from
// command-handler parsers to attach a source.Span to an argument-position
// diagnostic (missing flag, unknown view, unparseable ID).
func ArgSpan(a Arg) source.Span {
	switch v := a.(type) {
	case WordArg:
		return v.Span
	case NilArg:
		return v.Span
	case MissingArg:
		return v.Span
	case QuotedArg:
		return v.Span
	case ScalarValueArg:
		return v.Span
	case StructuredValueArg:
		return v.Span
	case AdapterArg:
		return v.Span
	}
	return source.Span{}
}

// WordArg is literal command text supplied by the user: command
// names, flags, paths, numeric IDs. It was never a variable
// reference.
type WordArg struct {
	// Text is the literal command text exactly as the user typed
	// it.
	Text string

	// Span is the source extent of the word token.
	source.Span
}

// NilArg is the null value at an arg boundary. Produced by
// valueToArg when a variable expression resolves to a nil Value:
// `$got.status.links` where the JSON value is `null`,
// `$prog.status.stats` where stats is `null`, and similar shape-
// test inputs. Command handlers that meaningfully accept null
// (jq, print, the strict-null / present predicates) inspect this
// variant; other handlers can either reject NilArg explicitly or
// fall through to their default unsupported-type diagnostic.
//
// source.Span is the originating $name reference's source extent so a
// downstream "this command can't take null" diagnostic frames at
// the right token.
type NilArg struct {
	// Span is the originating $name reference's source extent, so a
	// downstream "this command can't take null" diagnostic frames
	// at the right token.
	source.Span
}

// MissingArg is the "field absent from the value tree" outcome at
// an arg boundary. Produced when a variable expression's path
// names a field that does not exist (typically because an
// omitempty-elided producer chose not to emit it). Distinct from
// NilArg, which is the explicit-null outcome.
//
// The two variants exist so the shape-test predicates can
// distinguish "the field is missing from the shape" (a contract
// regression) from "the field is present and null" (the producer's
// way of saying the concept does not apply). Command handlers
// that do not meaningfully accept missing fields surface their
// own diagnostic when they encounter MissingArg.
type MissingArg struct {
	// Name is the bare variable name without the leading "$".
	Name string

	// Path is the dotted/indexed path expression after the name.
	Path string

	// Span is the originating $name reference's source extent.
	source.Span
}

// QuotedArg preserves user quoting as a distinct syntactic form.
// A quoted path with spaces is distinct from an unquoted flag.
type QuotedArg struct {
	// Text is the quoted literal's contents with the surrounding
	// quotes removed.
	Text string

	// Span is the source extent of the quoted token.
	source.Span
}

// ScalarValueArg is a value produced by variable expansion. The
// original variable reference has been resolved to a string in
// Text (for consumers that need argv-style text), and the source
// Value is preserved in Value with HasValue set true so consumers
// that care about the originating type (jq, future typed
// adapters) can recover it without re-parsing the rendered text.
// It is semantically distinct from WordArg because it came from a
// variable, not from user-typed literal text. source.Span is the
// originating $name reference's source extent.
//
// Boundary invariant for adapters that re-interpret scalars:
//
//	// User-written input is decoded from source text.
//	// Shell-resolved input is passed as its original Value.
//
// jq is the canonical example: `jq "." 42` decodes the literal
// 42 from text, but `let p = $prog.x.y; $p |> jq "."` passes the
// resolved string Value through untouched even if its text form
// is not valid JSON. Adapters check HasValue first.
type ScalarValueArg struct {
	// Text is the argv-style string the originating variable
	// reference resolved to.
	Text string

	// Value is the originating shell Value, preserved so consumers
	// that care about its type (jq, typed adapters) can recover it
	// without re-parsing Text. Valid only when HasValue is true.
	Value Value

	// HasValue reports whether Value carries the resolved Value;
	// adapters that re-interpret scalars check it before reaching
	// for Value.
	HasValue bool

	// Span is the originating $name reference's source extent.
	source.Span
}

// StructuredValueArg is a resolved structured variable value passed
// directly to a command. The command parser decides how to use it
// (e.g. extract .record.program_id). Name holds the variable name
// without the $ prefix. source.Span is the originating $name reference.
type StructuredValueArg struct {
	// Name is the originating variable name without the "$" prefix.
	Name string

	// Value is the resolved structured shell value handed to the
	// command; the command parser decides how to use it.
	Value Value

	// Span is the originating $name reference's source extent.
	source.Span
}

// AdapterArg is a resolved adapter invocation from inline adapter
// syntax (e.g. file:$var.path in exec argument position). Adapter
// is the adapter name, Value is the resolved shell value (scalar or
// structured), and Name/Path are retained for display. source.Span covers
// the adapter:$var.path expression.
type AdapterArg struct {
	// Adapter is the adapter name (the part before the colon in
	// adapter:$var.path).
	Adapter string

	// Name is the resolved variable's name, retained for display.
	Name string

	// Path is the dotted path after the variable name, retained for
	// display.
	Path string

	// Value is the resolved shell value, scalar or structured.
	Value Value

	// Span covers the whole adapter:$var.path expression.
	source.Span
}

func (WordArg) isArg()            {}
func (NilArg) isArg()             {}
func (MissingArg) isArg()         {}
func (QuotedArg) isArg()          {}
func (ScalarValueArg) isArg()     {}
func (StructuredValueArg) isArg() {}
func (AdapterArg) isArg()         {}
