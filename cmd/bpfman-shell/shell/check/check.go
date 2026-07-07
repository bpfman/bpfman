// Static checks that run between parse and evaluation. The
// goal is to catch bugs that would otherwise surface at run
// time (and thus only after some side effects have fired) one
// pass earlier, when the whole program is still in front of
// us. The current set covers undefined variables, uncaptured
// background jobs, arithmetic on non-numeric literals or
// non-numeric variables, comparison-kind mismatches,
// break/continue outside foreach, builtin arity, def arity,
// kill-flag validation, and field-access typos against sealed kinds
// (Job, the captured-command-result kind, syntax.Program, Link).
//
// Like go/types, this is a separate pass over the AST that
// produces a list of issues; it stays much smaller because the
// DSL has a fixed kind enum and no user-extensible types. Each
// check uses syntax.Inspect for expression-level work; scope-bearing
// constructs (let, bind, foreach, def) drive the structural
// part by hand because pre-order traversal cannot express
// "define this name after processing the RHS, before walking
// the next statement".

package check

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/jobsig"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
	"github.com/bpfman/bpfman/internal/strdist"
)

// Issue is one finding from a Check pass: a source location
// and a human-readable message. Multiple issues can be
// reported in a single Check invocation; severity is
// implicit: every Issue is an error.
type Issue struct {
	// Span is the source extent the issue is reported against; its
	// start position drives the "line:col:" prefix Error renders.
	source.Span

	// Msg is the human-readable description of the finding.
	Msg string
}

// DefStaticInfo is the static def metadata the checker needs
// when a caller wants to seed visibility from an outer context
// (for example, imported libraries checked in the context of
// defs already made visible by the importing script).
type DefStaticInfo struct {
	// Arity is the def's declared parameter count, used to validate
	// call sites against the seeded definition.
	Arity int

	// DeclPos is the def's declaration site, cited by arity and
	// duplicate-def diagnostics.
	DeclPos source.Pos

	// HasReturn reports whether the def body contains a return
	// anywhere in its tree. When true the def publishes a value, so
	// a bind RHS whose head is this def infers ReturnShape rather
	// than the default envelope shape.
	HasReturn bool

	// ReturnShape is the inferred shape the def produces, propagated
	// into downstream bindings. Meaningful only when HasReturn is
	// true.
	ReturnShape semantics.Shape
}

// Error renders the issue as 'line:col: message' so the
// driver layer can prepend a file path and emit the same
// shape parser/evaluator errors already use.
func (i Issue) Error() string {
	return fmt.Sprintf("%d:%d: %s", i.Pos.Line, i.Pos.Col, i.Msg)
}

// Check runs static analysis over prog and returns every
// issue it finds. Returning a slice rather than the first
// error lets callers report all problems at once instead of
// the user having to re-run after fixing each. An empty
// return slice means the program is clean by every check
// implemented.
func Check(prog *syntax.Program) []Issue {
	c := newChecker()
	c.prescanTopLevelDefs(prog.Stmts)
	return runCheckPass(c, prog)
}

// CheckImportLibraryWithDefs is the contextual form of
// CheckImportLibrary: visibleDefs seeds the top-level def set
// before this library's own defs are prescanned, so imported
// helpers can reference defs already visible from the importing
// script or earlier imports.
func CheckImportLibraryWithDefs(prog *syntax.Program, visibleDefs map[string]DefStaticInfo) []Issue {
	if prog == nil {
		return nil
	}
	defsOnly := &syntax.Program{Span: prog.Span}
	var issues []Issue
	for _, st := range prog.Stmts {
		if _, ok := st.(*syntax.DefStmt); ok {
			defsOnly.Stmts = append(defsOnly.Stmts, st)
			continue
		}
		issues = append(issues, Issue{
			Span: stmtSpan(st),
			Msg:  "imported files may contain only top-level def statements",
		})
	}
	c := newChecker()
	seedVisibleDefs(c, visibleDefs)
	c.prescanTopLevelDefs(defsOnly.Stmts)
	return append(issues, runCheckPass(c, defsOnly)...)
}

// checker carries the rolling state for one Check pass. Variable
// state lives on a stack of frames that mirrors the runtime
// Session frame stack: defines write innermost, lookups walk
// outward, and a block-shaped construct pushes a frame for the
// duration of its body. Each checkFrame holds the per-variable
// shape and the verbatim RHS syntax.LiteralExpr for variables bound to a
// single literal, so type checks (notably arithmetic-operand
// validation) can inspect the original expression.
type checker struct {
	frames []checkFrame
	issues []Issue
	// defDepth counts the def bodies currently being walked. A
	// syntax.ReturnStmt is only valid when defDepth > 0. The depth is
	// non-zero inside nested blocks (if, foreach, poll)
	// once we are inside a def, so a return tucked inside an
	// `if` branch of a def body is fine; a return at script top
	// level or inside an if-at-top-level is rejected.
	defDepth int
	// defs records the unique top-level def names visible from the
	// program body. Top-level defs are hoisted before execution, so
	// a bind RHS whose head is a known def routes through the def's
	// open-shape inference even when the call textually precedes the
	// declaration. Duplicate top-level def names are rejected during
	// the prescan; non-top-level defs are rejected at the declaration
	// site, so only top-level script/module defs enter this map.
	defs map[string]bool
	// defArity[name] is the parameter count of the unique visible
	// top-level def named name.
	defArity map[string]int
	// defDeclPos[name] records the declaration site of the unique
	// visible top-level def named name. Static def-arity errors and
	// duplicate-def diagnostics cite it so the user sees the
	// declaration that won admission to the visible def set.
	defDeclPos map[string]source.Pos
	// defDecls[name] records the actual top-level def node for
	// return-shape inference. Imported/visible defs from an outer
	// context may have metadata without a local declaration; those
	// are represented in defReturnShape instead.
	defDecls map[string]*syntax.DefStmt

	// nonTopLevelDepth tracks how many enclosing constructs put
	// us below module top level. Non-zero means a syntax.DefStmt is
	// not at module top level and must be rejected. The frames
	// around if/elif/else branches, foreach bodies, poll
	// bodies, and def bodies push this counter; DefStmts at the
	// top level keep it at zero.
	nonTopLevelDepth int
	// pollDepth counts syntactic nesting inside poll bodies.
	// retry is accepted there, and also inside def bodies so
	// polling logic can be factored into helpers. assert is
	// rejected in textual poll bodies so polling keeps explicit
	// retry semantics.
	pollDepth int
	// defHasReturn[name] is true when the unique visible def's body
	// contains at least one syntax.ReturnStmt anywhere in its tree.
	// The bind site uses the flag to keep the original sealed
	// envelope shape for no-return defs (so accessing a non-envelope
	// field surfaces at preflight) and to trigger monomorphic
	// return-shape inference for defs that actually publish a value. The flag is
	// not flow-sensitive -- a `return` in one branch is enough --
	// because the runtime contract is "primary is the returned value
	// when a return fires, otherwise the envelope mirror", and the
	// open shape is the conservative choice when either is possible.
	defHasReturn map[string]bool
	// defReturnShape caches the monomorphic return shape inferred
	// for each top-level def. Plain open OriginUnknown means "shape
	// not provable"; no-return defs are still handled through
	// defHasReturn and bind to the result-envelope shape.
	defReturnShape map[string]semantics.Shape
	// defReturnState tracks on-demand inference so composed helpers
	// can ask for each other's shapes without becoming declaration
	// order sensitive. Encountering a visiting def is recursion and
	// conservatively returns open.
	defReturnState map[string]defReturnInferState
}

type defReturnInferState int

const (
	defReturnUnseen defReturnInferState = iota
	defReturnVisiting
	defReturnDone
)

// checkFrame is one entry on the checker's frame stack. defined
// carries the names introduced in this frame; shapes carries
// their inferred semantics.Shape (or semantics.KindShape(semantics.OriginUnknown) when not
// inferable); literals carries the bound RHS syntax.LiteralExpr for the
// arithmetic-on-literal validation, or nil when the binding did
// not come from a single literal.
type checkFrame struct {
	defined  map[string]bool
	shapes   map[string]semantics.Shape
	literals map[string]*syntax.LiteralExpr
}

func newChecker() *checker {
	return &checker{
		frames:         []checkFrame{newCheckFrame()},
		defs:           map[string]bool{},
		defArity:       map[string]int{},
		defDeclPos:     map[string]source.Pos{},
		defDecls:       map[string]*syntax.DefStmt{},
		defHasReturn:   map[string]bool{},
		defReturnShape: map[string]semantics.Shape{},
		defReturnState: map[string]defReturnInferState{},
	}
}

func seedVisibleDefs(c *checker, visibleDefs map[string]DefStaticInfo) {
	for name, info := range visibleDefs {
		c.defs[name] = true
		c.defArity[name] = info.Arity
		c.defDeclPos[name] = info.DeclPos
		c.defHasReturn[name] = info.HasReturn
		if info.HasReturn {
			c.defReturnShape[name] = semantics.CloneShape(info.ReturnShape)
			c.defReturnState[name] = defReturnDone
		}
	}
}

func runCheckPass(c *checker, prog *syntax.Program) []Issue {
	c.walkStmts(prog.Stmts)
	c.checkJobLeaks(prog)
	c.checkArithmeticOperands(prog)
	c.checkComparisonOperands(prog)
	c.checkLoopExits(prog.Stmts, 0)
	c.checkBuiltinArity(prog)
	c.checkKillFlags(prog)
	c.checkThreadDefArity(prog)
	return c.issues
}

// prescanTopLevelDefs records the unique top-level def set visible
// from the program body before any statement runs. Forward
// references rely on this hoisted pass; duplicate names are
// rejected here so the visible set stays one-name-to-one-def.
func (c *checker) prescanTopLevelDefs(stmts []syntax.Stmt) {
	for _, st := range stmts {
		def, ok := st.(*syntax.DefStmt)
		if !ok {
			continue
		}
		name := def.Name.Text
		if prev, dup := c.defDeclPos[name]; dup {
			c.addIssue(def.Name.Span, "duplicate top-level def %q; previous declaration at %s", name, formatPos(prev))
			continue
		}
		c.defs[name] = true
		c.defArity[name] = len(def.Params)
		c.defDeclPos[name] = def.Name.Pos
		c.defDecls[name] = def
		c.defHasReturn[name] = bodyHasReturn(def.Body)
	}
}

func newCheckFrame() checkFrame {
	return checkFrame{
		defined:  map[string]bool{},
		shapes:   map[string]semantics.Shape{},
		literals: map[string]*syntax.LiteralExpr{},
	}
}

// define binds name into the innermost frame, shadowing any
// outer binding for the duration of the frame. Crossing a frame
// boundary creates a new shadowing binding rather than mutating
// an outer one -- the inner binding disappears when the frame is
// popped and the outer one becomes visible again. `_` is a
// discard slot at every binding site and contributes no binding.
func (c *checker) define(name string, shape semantics.Shape, lit *syntax.LiteralExpr) {
	if name == "_" {
		return
	}
	f := c.frames[len(c.frames)-1]
	f.defined[name] = true
	f.shapes[name] = shape
	if lit != nil {
		f.literals[name] = lit
	} else {
		delete(f.literals, name)
	}
}

// lookupDefined reports whether name resolves in any frame.
func (c *checker) lookupDefined(name string) bool {
	for i := len(c.frames) - 1; i >= 0; i-- {
		if c.frames[i].defined[name] {
			return true
		}
	}
	return false
}

// lookupShape returns the semantics.Shape for name found in the innermost
// frame that holds a binding. The second return value reports
// whether any frame had a shape entry; callers that find no
// entry treat the variable as an unsealed wildcard.
func (c *checker) lookupShape(name string) (semantics.Shape, bool) {
	for i := len(c.frames) - 1; i >= 0; i-- {
		if s, ok := c.frames[i].shapes[name]; ok {
			return s, true
		}
	}
	return semantics.Shape{}, false
}

// lookupLiteral returns the RHS syntax.LiteralExpr for name from the
// innermost frame that recorded one. Absence means the binding
// did not come from a single literal in any visible frame.
func (c *checker) lookupLiteral(name string) (*syntax.LiteralExpr, bool) {
	for i := len(c.frames) - 1; i >= 0; i-- {
		if lit, ok := c.frames[i].literals[name]; ok {
			return lit, true
		}
	}
	return nil, false
}

// withFrame pushes a fresh frame, runs fn, and pops in a defer
// so the pop runs on every exit path. The checker pushes frames
// only through withFrame so block scope is symmetric with the
// body's lexical extent.
func (c *checker) withFrame(fn func()) {
	c.frames = append(c.frames, newCheckFrame())
	defer func() {
		c.frames = c.frames[:len(c.frames)-1]
	}()
	fn()
}

// addIssue records an issue at span with the given message.
// Pulled out so the formatter is in one place if the message
// shape changes. Callers pass the offending node's full source.Span
// so the renderer can underline the relevant region rather than
// caret a single column.
func (c *checker) addIssue(span source.Span, format string, args ...any) {
	c.issues = append(c.issues, Issue{Span: span, Msg: fmt.Sprintf(format, args...)})
}

func formatPos(pos source.Pos) string {
	if pos.File != "" {
		return fmt.Sprintf("%s:%d:%d", pos.File, pos.Line, pos.Col)
	}
	return fmt.Sprintf("%d:%d", pos.Line, pos.Col)
}

// inferExprShape returns the semantics.Shape a let RHS expression produces
// at static-check time. The semantics.Shape carries the semantics.OriginKind tag
// (so scalar / bool / known-record cases resolve directly) and
// any nested structure: a path-walk through a sealed parent
// returns the leaf field's semantics.Shape, a list-bound variable
// preserves its element shape, and unknown expressions return
// an unsealed wildcard.
func (c *checker) inferExprShape(e syntax.Expr) semantics.Shape {
	switch v := e.(type) {
	case *syntax.LiteralExpr:
		if v.Quoted {
			return semantics.KindShape(semantics.OriginScalar)
		}
		switch v.Text {
		case "true", "false":
			return semantics.KindShape(semantics.OriginBool)
		case "null":
			// literalValueParts returns NullValue() for the
			// unquoted "null" token at runtime; the static
			// shape must agree so a `let r = null` carries
			// OriginNull through chained inferences and the
			// comparison classifier sees the same kind the
			// runtime sees.
			return semantics.KindShape(semantics.OriginNull)
		}
		return semantics.KindShape(semantics.OriginScalar)
	case *syntax.VarRefExpr:
		shape, ok := c.lookupShape(v.Name)
		if !ok {
			return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
		}
		if v.Path == "" {
			return shape
		}
		// Walk the semantics.Shape tree to find the leaf so a chained
		// let inherits the right shape. 'let q = $r.exit_code'
		// gives q a Scalar shape; 'let head = $progs[0]'
		// gives head a syntax.Program shape (via the list's Elem).
		// An unsealed step returns Unknown, which still
		// propagates as a wildcard but disables nested
		// validation further down.
		for _, seg := range splitPathSegments(v.Path) {
			if seg.index {
				if shape.Elem != nil {
					shape = *shape.Elem
					continue
				}
				return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
			}
			if !shape.Sealed {
				return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
			}
			child, ok := shape.Fields[seg.name]
			if !ok {
				return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
			}
			shape = child
		}
		return shape
	case *syntax.ListExpr:
		if len(v.Elems) == 0 {
			return openShape()
		}
		elem := c.inferExprShape(v.Elems[0])
		if isOpenShape(elem) {
			return openShape()
		}
		for _, e := range v.Elems[1:] {
			next := c.inferExprShape(e)
			if isOpenShape(next) || !sameShape(elem, next) {
				return openShape()
			}
		}
		elem = semantics.CloneShape(elem)
		return semantics.Shape{
			Sealed: false,
			Kind:   semantics.OriginUnknown,
			Elem:   &elem,
		}
	case *syntax.RecordExpr:
		fields := make(map[string]semantics.Shape, len(v.Fields))
		for _, field := range v.Fields {
			fields[field.Name] = c.inferExprShape(field.Expr)
		}
		return semantics.Shape{
			Sealed: true,
			Kind:   semantics.OriginUnknown,
			Fields: fields,
		}
	case *syntax.BinaryExpr:
		switch v.Op {
		case "+", "-", "*", "/", "%":
			return semantics.KindShape(semantics.OriginScalar)
		}
		return semantics.KindShape(semantics.OriginBool)
	case *syntax.LogicalExpr, *syntax.NotExpr, *syntax.UnaryExpr:
		return semantics.KindShape(semantics.OriginBool)
	case *syntax.NegateExpr:
		return semantics.KindShape(semantics.OriginScalar)
	case *syntax.InterpStringExpr:
		return semantics.KindShape(semantics.OriginScalar)
	case *syntax.PureCallExpr:
		if shape, ok := semantics.PureBuiltinReturnShape(v.Name); ok {
			return shape
		}
		return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
	}
	return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
}

// inferExprKind is sugar for inferExprShape(e).Kind. Existing
// kind-based checks (arithmetic, comparison) consult it without
// caring about the surrounding semantics.Shape; richer queries call
// inferExprShape directly.
func (c *checker) inferExprKind(e syntax.Expr) semantics.OriginKind {
	return c.inferExprShape(e).Kind
}

// inferBindShape returns the semantics.Shape a bind RHS syntax.CommandStmt
// produces in its primary slot.
//
// Resolution order:
//
//  1. The semantics-owned bind-shape table. Effectful command heads
//     with typed primaries (start, fire, exec, wait, kill, file,
//     net, tempdir, bpfman, ...) are described there. The
//     semantics.InferBindShape receives the args after the command
//     name so subcommand-aware shapes (net veth-pair -> NetPair,
//     net release -> result, net start -> Job; bpfman program get ->
//     syntax.Program, bpfman link list -> [Link], ...) live in the
//     shared semantic substrate rather than in cmd-side init glue.
//
//  2. The semantics-owned pure-builtin table. Pure entries declare
//     their return Shape there; the `<-` form remains a
//     compatibility spelling but `=` and `${...}` invoke the same
//     handler in expression position.
//
//  3. Everything else falls through to an external-subprocess
//     result envelope.
//
// A non-guard bind wraps this primary shape in an outcome shape;
// a guard bind exposes the primary shape directly.
func (c *checker) inferBindShape(cmd *syntax.CommandStmt) semantics.Shape {
	if cmd == nil || len(cmd.Args) == 0 {
		return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
	}
	first, ok := cmd.Args[0].(*syntax.LiteralExpr)
	if !ok || first.Quoted {
		return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
	}
	headText := first.Text
	if shape, ok := semantics.InferBindShape(headText, cmd.Args[1:]); ok {
		return shape
	}
	if shape, ok := semantics.PureBuiltinReturnShape(headText); ok {
		return shape
	}
	// Default: unknown first word runs as an external
	// subprocess via runExternalAsBind, which always returns
	// a result.
	return semantics.KindShape(semantics.OriginEnvelope)
}

func openShape() semantics.Shape {
	return semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
}

func isOpenShape(s semantics.Shape) bool {
	return !s.Sealed && s.Kind == semantics.OriginUnknown && s.Elem == nil && len(s.Fields) == 0
}

func sameShape(a, b semantics.Shape) bool {
	if a.Sealed != b.Sealed || a.Kind != b.Kind {
		return false
	}
	if (a.Elem == nil) != (b.Elem == nil) {
		return false
	}
	if a.Elem != nil && !sameShape(*a.Elem, *b.Elem) {
		return false
	}
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	for name, aChild := range a.Fields {
		bChild, ok := b.Fields[name]
		if !ok || !sameShape(aChild, bChild) {
			return false
		}
	}
	return true
}

func joinReturnShapes(a, b semantics.Shape) semantics.Shape {
	if isOpenShape(a) || isOpenShape(b) {
		return openShape()
	}
	if !sameShape(a, b) {
		return openShape()
	}
	return semantics.CloneShape(a)
}

func (c *checker) defPrimaryShape(name string) semantics.Shape {
	if !c.defHasReturn[name] {
		return semantics.KindShape(semantics.OriginEnvelope)
	}
	return c.inferDefReturnShape(name)
}

func (c *checker) bindPrimaryShape(cmd *syntax.CommandStmt, headIsDef bool) semantics.Shape {
	if headIsDef {
		return c.defPrimaryShape(bindHeadDefName(cmd))
	}
	return c.inferBindShape(cmd)
}

func (c *checker) bindOutcomeShape(cmd *syntax.CommandStmt, headIsDef bool) semantics.Shape {
	return outcomeShape(c.bindPrimaryShape(cmd, headIsDef))
}

func (c *checker) bindCollectTargetShape(n *syntax.BindStmt) semantics.Shape {
	producerShape := openShape()
	if n.Collect != nil && len(n.Collect.Body) > 0 {
		if last, ok := n.Collect.Body[len(n.Collect.Body)-1].(*syntax.CommandStmt); ok {
			producerShape = c.bindPrimaryShape(last, c.bindHeadDef(last))
		}
	}
	elem := semantics.CloneShape(producerShape)
	values := semantics.Shape{
		Sealed: false,
		Kind:   semantics.OriginUnknown,
		Elem:   &elem,
	}
	if n.Guard {
		return values
	}
	return collectOutcomeShape(producerShape)
}

func outcomeShape(primary semantics.Shape) semantics.Shape {
	out := semantics.CloneShape(semantics.KindShape(semantics.OriginEnvelope))
	if primary.Kind != semantics.OriginEnvelope {
		if out.Fields == nil {
			out.Fields = map[string]semantics.Shape{}
		}
		out.Fields["value"] = semantics.CloneShape(primary)
	}
	return out
}

func collectOutcomeShape(value semantics.Shape) semantics.Shape {
	result := outcomeShape(value)
	values := semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
	if value.Kind != semantics.OriginEnvelope {
		elem := semantics.CloneShape(value)
		values.Elem = &elem
	} else {
		elem := semantics.CloneShape(semantics.KindShape(semantics.OriginEnvelope))
		values.Elem = &elem
	}
	return semantics.Shape{
		Sealed: true,
		Kind:   semantics.OriginEnvelope,
		Fields: map[string]semantics.Shape{
			"ok":      semantics.KindShape(semantics.OriginBool),
			"results": {Sealed: false, Kind: semantics.OriginUnknown, Elem: &result},
			"values":  values,
		},
	}
}

type returnSummary struct {
	has    bool
	always bool
	shape  semantics.Shape
}

func noReturnSummary() returnSummary {
	return returnSummary{}
}

func possibleReturn(shape semantics.Shape, always bool) returnSummary {
	return returnSummary{has: true, always: always, shape: semantics.CloneShape(shape)}
}

// mergeReturnSummaries joins the shapes of two possible return
// sites. Control-flow exhaustiveness is owned by the caller
// (inferBlockReturn / inferIfReturn), so the merged summary is
// deliberately not marked always-returning here.
func mergeReturnSummaries(a, b returnSummary) returnSummary {
	if !a.has {
		return b
	}
	if !b.has {
		return a
	}
	return returnSummary{
		has:    true,
		always: false,
		shape:  joinReturnShapes(a.shape, b.shape),
	}
}

func (c *checker) inferDefReturnShape(name string) semantics.Shape {
	switch c.defReturnState[name] {
	case defReturnDone:
		return semantics.CloneShape(c.defReturnShape[name])
	case defReturnVisiting:
		return openShape()
	}

	def, ok := c.defDecls[name]
	if !ok {
		if shape, ok := c.defReturnShape[name]; ok {
			return semantics.CloneShape(shape)
		}
		return openShape()
	}
	if !c.defHasReturn[name] {
		return semantics.KindShape(semantics.OriginEnvelope)
	}

	c.defReturnState[name] = defReturnVisiting
	var summary returnSummary
	outerFrames := c.frames
	c.frames = []checkFrame{newCheckFrame()}
	defer func() {
		c.frames = outerFrames
	}()
	c.withFrame(func() {
		for _, p := range def.Params {
			c.define(p.Name.Text, openShape(), nil)
		}
		summary = c.inferBlockReturn(def.Body)
	})

	shape := openShape()
	if summary.has && summary.always {
		shape = semantics.CloneShape(summary.shape)
	}
	c.defReturnShape[name] = shape
	c.defReturnState[name] = defReturnDone
	return semantics.CloneShape(shape)
}

func (c *checker) inferBlockReturn(stmts []syntax.Stmt) returnSummary {
	out := noReturnSummary()
	for _, st := range stmts {
		summary := c.inferStmtReturn(st)
		if summary.has {
			out = mergeReturnSummaries(out, summary)
		}
		if summary.always {
			out.always = true
			return out
		}
	}
	return out
}

func (c *checker) inferStmtReturn(st syntax.Stmt) returnSummary {
	switch n := st.(type) {
	case *syntax.LetStmt:
		var lit *syntax.LiteralExpr
		if l, ok := n.RHS.(*syntax.LiteralExpr); ok {
			lit = l
		}
		c.define(n.Name.Text, c.inferExprShape(n.RHS), lit)
		return noReturnSummary()

	case *syntax.LetDestructureStmt:
		for _, name := range n.Names {
			c.define(name.Text, openShape(), nil)
		}
		return noReturnSummary()

	case *syntax.BindStmt:
		if n.Collect != nil {
			return c.inferBindCollectReturn(n)
		}
		headIsDef := c.bindHeadDef(n.Cmd)
		if n.Guard {
			c.define(n.Target.Text, c.bindPrimaryShape(n.Cmd, headIsDef), nil)
		} else {
			c.define(n.Target.Text, c.bindOutcomeShape(n.Cmd, headIsDef), nil)
		}
		return noReturnSummary()

	case *syntax.ReturnStmt:
		if n.Expr == nil {
			return possibleReturn(openShape(), true)
		}
		return possibleReturn(c.inferExprShape(n.Expr), true)

	case *syntax.IfStmt:
		return c.inferIfReturn(n)

	case *syntax.ForEachStmt:
		return c.inferLoopBodyReturn(n.Names, n.Body)

	case *syntax.PollStmt:
		return c.inferLoopBodyReturn(nil, n.Body)

	case *syntax.DefStmt:
		// Nested defs are rejected elsewhere and their returns
		// belong to that def, not this body.
		return noReturnSummary()

	default:
		return noReturnSummary()
	}
}

func (c *checker) inferBindCollectReturn(n *syntax.BindStmt) returnSummary {
	return c.inferLoopBodyReturn(n.Collect.Names, n.Collect.Body)
}

func (c *checker) inferLoopBodyReturn(names []syntax.Ident, body []syntax.Stmt) returnSummary {
	var summary returnSummary
	c.withFrame(func() {
		for _, name := range names {
			c.define(name.Text, openShape(), nil)
		}
		summary = c.inferBlockReturn(body)
	})
	if summary.has {
		summary.always = false
	}
	return summary
}

func (c *checker) inferIfReturn(n *syntax.IfStmt) returnSummary {
	out := noReturnSummary()
	allBranchesAlways := true
	branchCount := 0

	mergeBranch := func(stmts []syntax.Stmt) {
		branchCount++
		var summary returnSummary
		c.withFrame(func() {
			summary = c.inferBlockReturn(stmts)
		})
		if summary.has {
			out = mergeReturnSummaries(out, summary)
		}
		if !summary.always {
			allBranchesAlways = false
		}
	}

	mergeBranch(n.Then)
	for _, br := range n.Elifs {
		mergeBranch(br.Body)
	}
	if len(n.Else) > 0 {
		mergeBranch(n.Else)
	} else {
		allBranchesAlways = false
	}
	out.always = branchCount > 0 && allBranchesAlways && out.has
	return out
}

// bindHeadPureBuiltin reports whether cmd's first word is a
// registered pure builtin. The hint emitted by walkStmt cites
// the resolved name so a user reading the diagnostic sees the
// same spelling the registry would.
func bindHeadPureBuiltin(cmd *syntax.CommandStmt) (string, bool) {
	if cmd == nil || len(cmd.Args) == 0 {
		return "", false
	}
	first, ok := cmd.Args[0].(*syntax.LiteralExpr)
	if !ok || first.Quoted {
		return "", false
	}
	if semantics.IsPureBuiltin(first.Text) {
		return first.Text, true
	}
	return "", false
}

// bodyHasReturn walks stmts looking for at least one
// syntax.ReturnStmt that belongs to this body. A nested syntax.DefStmt's
// body is NOT descended into: an inner def's `return` is the
// inner def's contract, not the outer's. The walk is purely
// structural; it does not need to know about reachability or
// flow because a single return anywhere is enough to flip the
// outer def's bind primary from "always envelope" to "may be
// the returned value".
func bodyHasReturn(stmts []syntax.Stmt) bool {
	for _, s := range stmts {
		switch n := s.(type) {
		case *syntax.ReturnStmt:
			return true
		case *syntax.DefStmt:
			// Inner def's returns are its own.
			continue
		case *syntax.IfStmt:
			if bodyHasReturn(n.Then) {
				return true
			}
			for _, b := range n.Elifs {
				if bodyHasReturn(b.Body) {
					return true
				}
			}
			if bodyHasReturn(n.Else) {
				return true
			}
		case *syntax.ForEachStmt:
			if bodyHasReturn(n.Body) {
				return true
			}
		case *syntax.PollStmt:
			if bodyHasReturn(n.Body) {
				return true
			}
		case *syntax.BindStmt:
			if n.Collect != nil && bodyHasReturn(n.Collect.Body) {
				return true
			}
		}
	}
	return false
}

// suggestDefForUnknownBindHead emits the typo-suggest hint at
// a bind-RHS site. The bind RHS is a
// narrow surface -- the user is explicitly saying "give me a
// value from this dispatch" -- so a near-miss def name is
// almost always the right suggestion. The typo-suggest hint
// fires only there; broader command-shaped surfaces stay
// runtime-resolved because unknown heads may be external.
func (c *checker) suggestDefForUnknownBindHead(cmd *syntax.CommandStmt) {
	c.diagnoseUnknownHead(cmd, "bind head", true /*suggestTypos*/)
}

// diagnoseUnknownHead implements the shared typo-hint logic
// for unknown bind heads. One case produces a diagnostic:
//
//   - When suggestTypos is true and the head matches no def
//     at all but a top-level def name is a short edit distance
//     away. Likely typo; emit the "did you mean ..." hint.
//
// The role string is interpolated into the diagnostic so the
// user reads "bind head", "command", "defer command", or
// "bind-collect producer" depending on where the call came
// from. The suggestTypos gate exists because the typo-suggest
// hint is appropriate at narrow surfaces (bind RHS) but noisy
// at broad ones (command position) where the universe of
// valid commands extends far beyond the shell package's defs
// map.
func (c *checker) diagnoseUnknownHead(cmd *syntax.CommandStmt, role string, suggestTypos bool) {
	if cmd == nil || len(cmd.Args) == 0 {
		return
	}
	first, ok := cmd.Args[0].(*syntax.LiteralExpr)
	if !ok || first.Quoted {
		return
	}
	// Skip if the head already resolves through a known
	// dispatch path: a top-level def, a bind-shape registry
	// entry (bpfman, start, exec, etc.), or a pure builtin.
	head := first.Text
	if c.defs[head] {
		return
	}
	if semantics.HasBindShape(head) {
		return
	}
	if semantics.IsPureBuiltin(head) {
		return
	}
	if !suggestTypos || len(c.defs) == 0 {
		return
	}
	candidates := make([]string, 0, len(c.defs))
	for name := range c.defs {
		candidates = append(candidates, name)
	}
	matches := strdist.Nearest(head, candidates, 1)
	if len(matches) == 0 {
		return
	}
	c.addIssue(first.Span, "unknown %s %q; did you mean %q?", role, first.Text, matches[0])
}

// bindHeadDef reports whether cmd's first word names a def
// registered earlier in the walk. A matching def name takes
// precedence over both the pure-builtin rejection and the
// bind-shape registry: the def's `return EXPR` value is
// dynamic at preflight, so the primary slot binds with an
// open shape rather than the envelope fallback that
// mis-shapes def-bound primaries as commands' result
// envelopes.
func (c *checker) bindHeadDef(cmd *syntax.CommandStmt) bool {
	name := bindHeadDefName(cmd)
	if name == "" {
		return false
	}
	return c.defs[name]
}

// bindHeadDefName is the name-returning sibling of bindHeadDef.
// An empty string means "not a known def head"; a non-empty
// string is the head used for follow-on lookups (e.g.
// defHasReturn).
func bindHeadDefName(cmd *syntax.CommandStmt) string {
	if cmd == nil || len(cmd.Args) == 0 {
		return ""
	}
	first, ok := cmd.Args[0].(*syntax.LiteralExpr)
	if !ok || first.Quoted {
		return ""
	}
	head := first.Text
	return head
}

// checkDefArity mirrors callDef's exact-arity runtime rule at
// static time for any command-shaped surface whose head resolves
// to a known top-level def. Unknown heads stay runtime-resolved:
// they may be external commands.
func (c *checker) checkDefArity(cmd *syntax.CommandStmt) {
	head := bindHeadDefName(cmd)
	if head == "" || !c.defs[head] {
		return
	}
	want := c.defArity[head]
	got := len(cmd.Args) - 1
	if got != want {
		decl := c.defDeclPos[head]
		c.addIssue(cmd.Span, "%s: expected %d argument(s), got %d (def declared at %d:%d)",
			head, want, got, decl.Line, decl.Col)
		return
	}
	c.checkDefLiteralArgs(head, cmd.Args[1:], 0)
}

// checkDefLiteralArgs mirrors the runtime's annotated-parameter
// policy at static time for the inputs the checker can fully judge:
// literal arguments. A bare word that cannot parse as the declared
// type, or a quoted literal where a non-string type is declared,
// can never bind successfully, so the mismatch is a check-time
// issue with the same message the runtime would produce. Variable
// and projection arguments stay runtime-checked: value shapes do
// not carry scalar kinds. args[i] binds the def's parameter
// paramOffset+i, so thread-position calls (whose piped value
// occupies parameter zero) pass offset one.
func (c *checker) checkDefLiteralArgs(head string, args []syntax.Expr, paramOffset int) {
	def, ok := c.defDecls[head]
	if !ok {
		// Pre-expansion visible-def metadata (DefStaticInfo)
		// carries no parameter types; the expanded-program
		// check sees the declarations directly.
		return
	}
	for i, arg := range args {
		pi := paramOffset + i
		if pi >= len(def.Params) {
			return
		}
		p := def.Params[pi]
		if p.Type == "" {
			continue
		}
		lit, ok := arg.(*syntax.LiteralExpr)
		if !ok {
			continue
		}
		if lit.Quoted {
			if p.Type != "string" {
				c.addIssue(lit.Span, "def %s: parameter %q: expected %s, got the quoted string %q (quoting asserts string; drop the quotes to parse it)",
					head, p.Name.Text, p.Type, lit.Text)
			}
			continue
		}
		switch p.Type {
		case "number":
			if !syntax.IsJSONNumber(lit.Text) {
				c.addIssue(lit.Span, "def %s: parameter %q: expected number, got %q", head, p.Name.Text, lit.Text)
			}
		case "bool":
			if lit.Text != "true" && lit.Text != "false" {
				c.addIssue(lit.Span, "def %s: parameter %q: expected bool, got %q", head, p.Name.Text, lit.Text)
			}
		}
	}
}

// primaryNameForHint picks a placeholder for the bind-target
// slot in the diagnostic suggestion. The bind target's name is
// reused when it names a real binding (non-empty and not the
// "_" discard); otherwise a generic "x" reads cleanly in the
// rewritten form the hint suggests.
func primaryNameForHint(n *syntax.BindStmt) string {
	if n.Target.Text != "" && n.Target.Text != "_" {
		return n.Target.Text
	}
	return "x"
}

// walkStmts walks a statement list in source order. Defining
// statements (let, bind, foreach, def) call c.define as a side
// effect of being walked; expression statements run their
// VarRef-usage check via checkExpr.
func (c *checker) walkStmts(stmts []syntax.Stmt) {
	for _, s := range stmts {
		c.walkStmt(s)
	}
}

// walkStmt dispatches on statement kind. The order of work
// inside each case matters: RHS expressions are checked before
// the binding name is added to defined, so 'let x = $x' on a
// previously-undefined x correctly reports $x undefined rather
// than silently letting the new binding shadow the lookup.
func (c *checker) walkStmt(s syntax.Stmt) {
	switch n := s.(type) {
	case *syntax.LetStmt:
		c.checkExpr(n.RHS)
		// Record the RHS literal when the binding is a single
		// syntax.LiteralExpr, quoted or not. The arithmetic check
		// consults the .Text and .Quoted fields so a quoted
		// string ('let s = "world"') and an unquoted
		// non-numeric token ('let s = bogus') both fire,
		// while a numeric literal stays clean.
		var lit *syntax.LiteralExpr
		if l, ok := n.RHS.(*syntax.LiteralExpr); ok {
			lit = l
		}
		// `let v = my_def` silently binds the literal string
		// "my_def" when my_def is a registered def. The
		// operator distinction between `=` (expression) and
		// `<-` (bind) is intentional but easy to walk off; the
		// hint points at the corrective shape without
		// restricting the bareword-literal case.
		if lit != nil && !lit.Quoted && c.defs[lit.Text] {
			c.addIssue(lit.Span, "let %s = %s binds the literal string %q; %q is a def -- did you mean `let %s <- %s`?", n.Name.Text, lit.Text, lit.Text, lit.Text, n.Name.Text, lit.Text)
		}
		c.define(n.Name.Text, c.inferExprShape(n.RHS), lit)

	case *syntax.LetDestructureStmt:
		c.checkExpr(n.RHS)
		// Each non-'_' name becomes defined. Element shapes are
		// not inferred individually because the RHS could be any
		// list expression; only the binding existence matters
		// for downstream name-resolution.
		for _, name := range n.Names {
			c.define(name.Text, semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}, nil)
		}

	case *syntax.BindStmt:
		if n.Cmd != nil {
			for _, a := range n.Cmd.Args {
				c.checkExpr(a)
			}
			c.checkDefArity(n.Cmd)
			c.checkNetArgShape(n.Cmd)
		}
		// Def dispatch precedes both the pure-builtin
		// rejection and the bind-shape lookup. A def name on
		// the bind RHS routes through callDefAsBind at runtime
		// no matter what other handler the same word might
		// match against, so the static checker must do the
		// same -- otherwise a def shadowing a pure-builtin
		// name would get rejected at preflight even though it
		// runs cleanly, and a def-bound primary would be
		// mis-shaped as the fallback envelope and reject any
		// field access the def's actual return shape supports.
		headIsDef := c.bindHeadDef(n.Cmd)
		if !headIsDef {
			// A '<-' bind on a pure builtin is rejected: pure
			// builtins produce no result envelope, so the rc
			// slot of '<-' (and the synthetic one a single-name
			// bind would discard) has nothing to carry. The '='
			// form is the only correct call shape in binding
			// position.
			if name, ok := bindHeadPureBuiltin(n.Cmd); ok {
				c.addIssue(n.Cmd.Span, "%s is a pure builtin; use 'let %s = %s ...' rather than '<-' (no result envelope is produced)", name, primaryNameForHint(n), name)
			} else {
				// Typo-against-defs hint. An unknown bind head
				// is a valid shape -- the user may genuinely
				// want a subprocess -- but when the head is a
				// short edit distance away from a known def
				// the most likely intent is the def. The
				// strdist threshold already drops candidates
				// beyond a relative ratio, so an arbitrary
				// external command name does not pick up
				// false-positive suggestions; only close-miss
				// typos surface a hint.
				c.suggestDefForUnknownBindHead(n.Cmd)
			}
		}
		// Walk the bind-collect's foreach list expression and
		// body the same way a free-standing ForEachStmt does: the
		// list is a regular expression that may reference
		// undefined variables, and the body is a statement list
		// that must be checked with the loop variable(s) in
		// scope. Without this walk a typo in the source list, an
		// undefined reference in the body, or a misplaced def /
		// import / break / continue inside the body would all
		// pass preflight unnoticed. The body runs inside a fresh
		// frame so loop-var bindings disappear at the bind
		// boundary, and nonTopLevelDepth bumps so a syntax.DefStmt
		// inside the body is rejected as non-top-level.
		if n.Collect != nil {
			c.checkExpr(n.Collect.List)
			c.nonTopLevelDepth++
			c.withFrame(func() {
				for _, name := range n.Collect.Names {
					c.define(name.Text, semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}, nil)
				}
				c.walkStmts(n.Collect.Body)
			})
			c.nonTopLevelDepth--
		}
		// A def-bound primary is shaped from the def's
		// monomorphic return shape when one can be inferred.
		// Recursive, fall-through, parameter-dependent, or
		// disagreeing returns stay open; no-return defs keep the
		// sealed result-envelope shape they publish at runtime.
		if n.Collect != nil {
			shape := c.bindCollectTargetShape(n)
			c.define(n.Target.Text, shape, nil)
			return
		}
		if n.Guard {
			c.define(n.Target.Text, c.bindPrimaryShape(n.Cmd, headIsDef), nil)
		} else {
			c.define(n.Target.Text, c.bindOutcomeShape(n.Cmd, headIsDef), nil)
		}

	case *syntax.ForEachStmt:
		c.checkExpr(n.List)
		// Loop variables are in scope inside the body only.
		// The body runs inside a fresh frame so loop-var
		// bindings and any body-level `let` disappear at the
		// end of the loop. The checker has no notion of
		// iteration; one frame for the body is enough -- the
		// runtime allocates one frame per iteration but a
		// static walk cannot make use of the distinction.
		// A def inside the body would not be top-level, so
		// the body counts as a rejecting context for syntax.DefStmt.
		c.nonTopLevelDepth++
		c.withFrame(func() {
			for _, name := range n.Names {
				c.define(name.Text, semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}, nil)
			}
			c.walkStmts(n.Body)
		})
		c.nonTopLevelDepth--

	case *syntax.DefStmt:
		// Top-level defs were pre-scanned before the walk started,
		// so recursive calls and forward references already see the
		// def set the runtime will have before body execution
		// begins. Def declarations are module-top-level only. A def in
		// a branch, loop, poll body, or another def body
		// reads like a lexical helper but mutates the session-
		// global def table at runtime; reject that shape at the
		// declaration site rather than asking readers to reason
		// about conditional/global registration.
		if c.nonTopLevelDepth != 0 {
			c.addIssue(n.Name.Span, "def %q must be declared at top level", n.Name.Text)
		}
		c.defHasReturn[n.Name.Text] = bodyHasReturn(n.Body)
		// Parameters are visible inside the body and disappear
		// at end-of-def. The runtime allocates a fresh frame
		// per call; the checker walks the def body once with
		// its parameter frame in place. defDepth tracks whether
		// the walk is currently inside any def so the syntax.ReturnStmt
		// case can reject the keyword at the wrong nesting.
		// Any nested syntax.DefStmt inside the body is therefore not
		// top-level and is rejected.
		c.defDepth++
		c.nonTopLevelDepth++
		c.withFrame(func() {
			for _, p := range n.Params {
				c.define(p.Name.Text, semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}, nil)
			}
			c.walkStmts(n.Body)
		})
		c.nonTopLevelDepth--
		c.defDepth--

	case *syntax.ReturnStmt:
		// `return EXPR` is only valid inside a def body. The
		// runtime carries a safety-net check at evalProgramBody
		// for paths the checker does not see (a return reached
		// only through a dynamic source, say), but the visible
		// shapes are caught here so the diagnostic lands at
		// check time, before any side effect fires. The
		// expression itself is checked even when the position
		// is wrong, so a script with both errors reports both.
		if c.defDepth == 0 {
			c.addIssue(n.Span, "return outside a def body")
		}
		if n.Expr != nil {
			c.checkExpr(n.Expr)
		}

	case *syntax.IfStmt:
		// Each branch body checks in its own frame: a `let`
		// inside one branch is invisible to subsequent
		// sibling branches and to the post-if scope. The
		// checker does not know which branch will run, so it
		// walks every branch independently; none contributes
		// bindings to the surrounding scope.
		//
		// nonTopLevelDepth bumps for each branch so a syntax.DefStmt
		// inside the branch body is rejected as non-top-level.
		c.checkExpr(n.Cond)
		c.nonTopLevelDepth++
		c.withFrame(func() {
			c.walkStmts(n.Then)
		})
		c.nonTopLevelDepth--
		for _, b := range n.Elifs {
			c.checkExpr(b.Cond)
			c.nonTopLevelDepth++
			c.withFrame(func() {
				c.walkStmts(b.Body)
			})
			c.nonTopLevelDepth--
		}
		if len(n.Else) > 0 {
			c.nonTopLevelDepth++
			c.withFrame(func() {
				c.walkStmts(n.Else)
			})
			c.nonTopLevelDepth--
		}

	case *syntax.DeferStmt:
		if n.Cmd != nil {
			for _, a := range n.Cmd.Args {
				c.checkExpr(a)
			}
			c.checkDefArity(n.Cmd)
			c.checkJobArgShape(n.Cmd)
			c.checkNetArgShape(n.Cmd)
		}

	case *syntax.PollStmt:
		// The body checks in its own frame: body-level `let`
		// stays inside the polling attempt and is invisible to
		// the post-construct scope. A def inside the body would
		// not be top-level, so the body counts as a rejecting
		// context for syntax.DefStmt too.
		c.nonTopLevelDepth++
		c.pollDepth++
		c.withFrame(func() {
			c.walkStmts(n.Body)
		})
		c.pollDepth--
		c.nonTopLevelDepth--

	case *syntax.AssertStmt:
		// `require` shares the AssertStmt node with `assert` but
		// is fatal-immediately in every context, including
		// inside a poll body; the diagnostic itself names
		// `require ...` as one of the two replacements for an
		// `assert` here, so rejecting `require` as well would
		// leave the suggestion unreachable from its source. The
		// gate fires only for the non-require spelling.
		if c.pollDepth > 0 && !n.IsRequire {
			c.addIssue(n.Span, "assert is not valid inside poll; use retry unless ... or require ...")
		}
		c.checkAssertClause(n.Clause)

	case *syntax.RetryStmt:
		if c.pollDepth == 0 && c.defDepth == 0 {
			c.addIssue(n.Span, "retry is only valid inside poll")
		}
		if n.Message != nil {
			c.checkExpr(n.Message)
		}
		if n.Unless != nil {
			c.checkExpr(n.Unless)
		}

	case *syntax.ExprStmt:
		c.checkExpr(n.Expr)

	case *syntax.CommandStmt:
		for _, a := range n.Args {
			c.checkExpr(a)
		}
		c.checkDefArity(n)
		c.checkJobArgShape(n)
		c.checkNetArgShape(n)
		if head, ok := commandHeadLiteral(n); ok && head == "import" && c.nonTopLevelDepth != 0 {
			c.addIssue(n.Span, "import must be declared at top level")
		}

	case *syntax.BreakStmt, *syntax.ContinueStmt:
		// Leaves; nothing to check today.
	}
}

func (c *checker) checkJobArgShape(cmd *syntax.CommandStmt) {
	target := c.jobReferenceExpr(cmd)
	if target == nil {
		return
	}
	shape, ok := c.lookupShape(target.Name)
	if !ok || shape.Kind == semantics.OriginUnknown || shape.Kind == semantics.OriginJob {
		return
	}
	c.addIssue(target.Span, "expected a $job argument, got a %s value", shape.Kind)
}

// checkNetArgShape mirrors the runtime's net exec / net start /
// net release argument rules statically, message for message. The
// isolated netns-veth-pair has no natural default side, so the
// bare pair is rejected for exec/start in favour of an explicit
// endpoint, and an endpoint is rejected for release because the
// release unit is the pair. Unknown shapes stay silent (wildcard),
// matching every other kind check.
func (c *checker) checkNetArgShape(cmd *syntax.CommandStmt) {
	if cmd == nil || len(cmd.Args) < 3 {
		return
	}
	head, ok := commandHeadLiteral(cmd)
	if !ok || head != "net" || c.defs[head] {
		return
	}
	sub, ok := cmd.Args[1].(*syntax.LiteralExpr)
	if !ok || sub.Quoted {
		return
	}
	target, ok := cmd.Args[2].(*syntax.VarRefExpr)
	if !ok {
		return
	}
	kind := c.inferExprShape(target).Kind
	switch sub.Text {
	case "exec", "start":
		switch kind {
		case semantics.OriginNetnsVethPair:
			c.addIssue(target.Span, "net %s: netns-veth-pair has two endpoints; use $pair.a or $pair.b", sub.Text)
		case semantics.OriginUnknown, semantics.OriginNetPair, semantics.OriginNetnsVethEndpoint:
		default:
			c.addIssue(target.Span, "net %s: expected a $pair or endpoint argument, got a %s value", sub.Text, kind)
		}
	case "release":
		switch kind {
		case semantics.OriginNetnsVethEndpoint:
			c.addIssue(target.Span, "net release: endpoint belongs to a netns-veth-pair; release the pair")
		case semantics.OriginUnknown, semantics.OriginNetPair, semantics.OriginNetnsVethPair:
		default:
			c.addIssue(target.Span, "net release: expected a $pair argument, got a %s value", kind)
		}
	}
}

func (c *checker) checkAssertClause(clause syntax.AssertClause) {
	switch v := clause.(type) {
	case *syntax.AssertExprClause:
		c.checkExpr(v.Expr)
	case *syntax.AssertCommandClause:
		for _, a := range v.Args {
			c.checkExpr(a)
		}
	}
}

// checkExpr scans an expression subtree for VarRef usages
// against the current defined-set, plus path-validity against
// any sealed kinds the checker has inferred for the variable.
// syntax.Inspect is the right instrument here: an expression has no
// scoping of its own, so generic pre-order is exactly what we
// want.
func (c *checker) checkExpr(e syntax.Expr) {
	if e == nil {
		return
	}
	syntax.Inspect(e, func(n syntax.Node) bool {
		if v, ok := n.(*syntax.VarRefExpr); ok {
			if !c.lookupDefined(v.Name) {
				c.addIssue(v.Span, "undefined variable: %s", v.Name)
				return true
			}
			c.checkVarRefPath(v)
		}
		if v, ok := n.(*syntax.PureCallExpr); ok {
			if span, msg, ok := semantics.PureBuiltinLiteralIssue(v); ok {
				c.addIssue(span, "%s", msg)
			}
		}
		return true
	})
}

func commandHeadLiteral(n *syntax.CommandStmt) (string, bool) {
	if n == nil || len(n.Args) == 0 {
		return "", false
	}
	head, ok := n.Args[0].(*syntax.LiteralExpr)
	if !ok || head.Quoted {
		return "", false
	}
	return head.Text, true
}

func stmtSpan(st syntax.Stmt) source.Span {
	switch n := st.(type) {
	case *syntax.LetStmt:
		return n.Span
	case *syntax.LetDestructureStmt:
		return n.Span
	case *syntax.BindStmt:
		return n.Span
	case *syntax.ForEachStmt:
		return n.Span
	case *syntax.DefStmt:
		return n.Span
	case *syntax.ReturnStmt:
		return n.Span
	case *syntax.IfStmt:
		return n.Span
	case *syntax.DeferStmt:
		return n.Span
	case *syntax.PollStmt:
		return n.Span
	case *syntax.RetryStmt:
		return n.Span
	case *syntax.AssertStmt:
		return n.Span
	case *syntax.ExprStmt:
		return n.Span
	case *syntax.CommandStmt:
		return n.Span
	case *syntax.BreakStmt:
		return n.Span
	case *syntax.ContinueStmt:
		return n.Span
	default:
		return source.Span{}
	}
}

// checkVarRefPath validates v's path against the variable's
// inferred semantics.Shape, descending field by field. The walker stops at
// the first segment that is not in a sealed parent's field set
// and emits a frame underlining the whole varref with a "did you
// mean ..." suggestion derived via internal/strdist.
//
// Index segments ([N]) descend through semantics.Shape.Elem when the
// semantics.Shape is a list; lists with no Elem (or non-list parents)
// permit indexing without comment because we cannot disprove the
// shape. Once the walk lands on an unsealed semantics.Shape every
// remaining segment is accepted -- the checker has lost
// visibility into nested structure and refusing to walk further
// would produce false positives.
func (c *checker) checkVarRefPath(v *syntax.VarRefExpr) {
	if v.Path == "" {
		return
	}
	current, ok := c.lookupShape(v.Name)
	if !ok {
		return
	}
	currentName := v.Name
	currentKind := current.Kind

	for _, seg := range splitPathSegments(v.Path) {
		if seg.index {
			if current.Elem != nil {
				current = *current.Elem
				currentKind = current.Kind
				continue
			}
			// Either this semantics.Shape isn't a list, or its Elem
			// is not registered. Descend into Unknown so we
			// stop trying to validate further fields.
			current = semantics.Shape{Sealed: false, Kind: semantics.OriginUnknown}
			currentKind = semantics.OriginUnknown
			continue
		}
		if !current.Sealed {
			return
		}
		if len(current.Fields) == 0 {
			c.addIssue(v.Span, "%s has kind %s; field access is not valid", currentName, currentKind)
			return
		}
		child, ok := current.Fields[seg.name]
		if !ok {
			c.addIssue(v.Span, "%s",
				unknownFieldMsg(currentName, currentKind, seg.name, current.Fields))
			return
		}
		current = child
		currentName = currentName + "." + seg.name
		currentKind = child.Kind
	}
}

// pathSegment is a single step inside a varref path: either a
// dotted field name or a "[N]" index step.
type pathSegment struct {
	name  string
	index bool
}

// splitPathSegments parses a varref path into its component
// steps. The grammar is the same one the lexer accepts inside
// "${name.path}" / "$name[0].field": dotted names alternate with
// "[N]" index steps, and the leading dot (if any) is implicit
// because syntax.VarRefExpr.Path is stored without it.
func splitPathSegments(path string) []pathSegment {
	var out []pathSegment
	i := 0
	for i < len(path) {
		if path[i] == '.' {
			i++
			continue
		}
		if path[i] == '[' {
			j := i + 1
			for j < len(path) && path[j] != ']' {
				j++
			}
			out = append(out, pathSegment{index: true})
			if j < len(path) {
				j++
			}
			i = j
			continue
		}
		j := i
		for j < len(path) && path[j] != '.' && path[j] != '[' {
			j++
		}
		out = append(out, pathSegment{name: path[i:j]})
		i = j
	}
	return out
}

// unknownFieldMsg renders the "no field X (valid: ...; did you
// mean Y?)" message used when a path segment misses the parent
// semantics.Shape's field set. The kind label is included only when it is
// informative -- nested record types (syntax.Program.Record, Link.Record,
// etc.) carry semantics.OriginUnknown for their kind tag because the
// reflector does not cross-link Go types to OriginKinds, and
// "X has kind unknown" reads as noise; the field set is the
// useful information. The valid list is sorted so error
// rendering is stable; the suggestion list comes from
// internal/strdist.
func unknownFieldMsg(name string, kind semantics.OriginKind, seg string, fields map[string]semantics.Shape) string {
	valid := slices.Sorted(maps.Keys(fields))
	suggestions := strdist.Nearest(seg, valid, 3)
	var msg string
	if kind == semantics.OriginUnknown {
		msg = fmt.Sprintf("%s has no field %q (valid: %s)",
			name, seg, strings.Join(valid, ", "))
	} else {
		msg = fmt.Sprintf("%s has kind %s; field %q does not exist (valid: %s)",
			name, kind, seg, strings.Join(valid, ", "))
	}
	if len(suggestions) > 0 {
		msg += "; did you mean " + strings.Join(quoteAll(suggestions), ", ") + "?"
	}
	return msg
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// checkJobLeaks reports started-but-never-managed jobs. A
// 'guard X <- start ...' creates a job named X; a later 'kill
// $X', 'wait $X', or 'defer kill $X' marks it managed. Only the
// guard form is tracked: a plain 'let X <- start ...' binds an
// outcome envelope rather than the job handle, so $X cannot be
// killed or waited and is outside the leak rule. An unmanaged
// job at script end is the static analogue of the runtime leak
// walk: same rule, caught one pass earlier so the user sees it
// before any side effects fire.
//
// The check is intentionally conservative: a 'kill $X' or
// 'wait $X' anywhere in the program counts, even inside a
// conditional branch the runtime might never enter. We
// prefer false-negatives (missed leaks the user sees at run
// time anyway) to false-positives (warning about scripts
// that work fine in practice). Sourced files are not
// analysed cross-file; each script is checked in isolation.
func (c *checker) checkJobLeaks(prog *syntax.Program) {
	c.checkJobLeaksInBody(prog.Stmts)
}

func (c *checker) checkJobLeaksInBody(stmts []syntax.Stmt) {
	type jobBinding struct {
		Name string
		source.Span
	}

	var started []jobBinding
	managed := map[string]bool{}
	returned := map[string]bool{}

	var walkBody func([]syntax.Stmt)
	walkBody = func(stmts []syntax.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *syntax.BindStmt:
				if s.Guard && c.isStartCommand(s.Cmd) && s.Target.Text != "" && s.Target.Text != "_" {
					started = append(started, jobBinding{Name: s.Target.Text, Span: s.Target.Span})
				}
				if name := c.jobReferenceTarget(s.Cmd); name != "" {
					managed[name] = true
				}
			case *syntax.CommandStmt:
				if name := c.jobReferenceTarget(s); name != "" {
					managed[name] = true
				}
			case *syntax.DeferStmt:
				if s.Cmd != nil {
					if name := c.jobReferenceTarget(s.Cmd); name != "" {
						managed[name] = true
					}
				}
			case *syntax.ReturnStmt:
				// Returning the started job hands lifecycle ownership
				// to the caller. We intentionally treat the direct
				// `return $job` case as managed here and accept false
				// negatives for more complex escape paths: the runtime
				// leak walk still catches missed waits/kills, while the
				// checker must avoid rejecting valid helper patterns.
				if ref, ok := s.Expr.(*syntax.VarRefExpr); ok && ref.Path == "" {
					returned[ref.Name] = true
				}
			case *syntax.IfStmt:
				walkBody(s.Then)
				for _, branch := range s.Elifs {
					walkBody(branch.Body)
				}
				walkBody(s.Else)
			case *syntax.ForEachStmt:
				walkBody(s.Body)
			case *syntax.PollStmt:
				walkBody(s.Body)
			case *syntax.DefStmt:
				c.checkJobLeaksInBody(s.Body)
			}
		}
	}
	walkBody(stmts)

	for _, j := range started {
		if !managed[j.Name] && !returned[j.Name] {
			c.addIssue(j.Span, "started job %q has no matching wait or kill", j.Name)
		}
	}
}

// isStartCommand reports whether cmd is a 'start ...' invocation
// against the builtin. A user `def start(...)` resolves first at
// runtime, so the bind it produces is not a job and is not
// subject to leak analysis; the checker honours the same
// precedence by consulting c.defs.
func (c *checker) isStartCommand(cmd *syntax.CommandStmt) bool {
	if cmd == nil || len(cmd.Args) == 0 {
		return false
	}
	lit, ok := cmd.Args[0].(*syntax.LiteralExpr)
	if !ok || lit.Text != "start" {
		return false
	}
	return !c.defs[lit.Text]
}

// checkArithmeticOperands flags literal operands of arithmetic
// operators (+, -, *, /, %) that cannot parse as numeric. The
// runtime evaluator already produces 'left operand "Z" is not
// numeric' for these; the static check pulls the diagnostic
// one pass earlier so an arithmetic typo in '${4 * Z}' or
// 'let r = A / B' surfaces before any side effect runs.
//
// Variable-reference operands are trusted (we cannot know
// their value at static time); only syntax.LiteralExpr operands are
// inspected. The numeric-vs-not test is syntax.IsJSONNumber,
// which accepts the same finite JSON-number shapes the runtime
// arithmetic evaluator accepts. Hex, non-finite values, leading
// plus signs, and leading-zero forms are rejected by both sides;
// the check reports them at preflight rather than letting them
// surface later from the runtime's "operand is not numeric" path.
func (c *checker) checkArithmeticOperands(prog *syntax.Program) {
	syntax.Inspect(prog, func(n syntax.Node) bool {
		be, ok := n.(*syntax.BinaryExpr)
		if !ok || !isArithmeticOpText(be.Op) {
			return true
		}
		c.flagNonNumericOperand(be.Left, be.Op)
		c.flagNonNumericOperand(be.Right, be.Op)
		return true
	})
}

// flagNonNumericOperand emits an issue when e is statically
// known to be non-numeric. Three sources of evidence are
// consulted in turn: a literal whose text fails the numeric
// parser; a varref whose kind cannot represent a number
// (Bool, Job, the captured-result kind, the bpfman record
// kinds, Map, Null); and a varref bound to a literal whose
// text is non-numeric (Scalar kind alone is ambiguous because
// "world" and "5" are both Scalar -- the recorded RHS text
// resolves the ambiguity). Variables of semantics.OriginUnknown or
// path-walked Scalars are trusted at static time because the
// runtime value is genuinely opaque.
func (c *checker) flagNonNumericOperand(e syntax.Expr, op string) {
	if lit, ok := e.(*syntax.LiteralExpr); ok {
		if isNumericLiteral(lit.Text) {
			return
		}
		// When the offending literal happens to name a def,
		// the most likely user intent is to call it. Point at
		// the bind form so the corrective shape is obvious;
		// the same hint shape works for comparisons via the
		// shared classifier below.
		if !lit.Quoted && c.defs[lit.Text] {
			c.addIssue(lit.Span, "arithmetic %s: operand %q is not numeric; %q is a def -- call it via `let v <- %s` and use $v in the expression", op, lit.Text, lit.Text, lit.Text)
			return
		}
		c.addIssue(lit.Span, "arithmetic %s: operand %q is not numeric", op, lit.Text)
		return
	}
	v, ok := e.(*syntax.VarRefExpr)
	if !ok {
		return
	}
	kind := c.inferExprKind(v)
	switch kind {
	case semantics.OriginScalar, semantics.OriginUnknown:
		// Scalars may be numeric or not; consult the recorded
		// literal RHS when the varref is a bare name with no
		// path walk and was bound to a single syntax.LiteralExpr.
		// A quoted string ('let s = "world"') is always
		// non-numeric; an unquoted token gets the same
		// numeric-parser test isNumericLiteral applies to
		// arithmetic-on-literal operands.
		if v.Path == "" {
			if lit, ok := c.lookupLiteral(v.Name); ok {
				if lit.Quoted || !isNumericLiteral(lit.Text) {
					c.addIssue(v.Span, "arithmetic %s: %s is %q, not a number", op, v.Name, lit.Text)
				}
			}
		}
	case semantics.OriginBool:
		c.addIssue(v.Span, "arithmetic %s: %s is a boolean, not a number", op, v.Name)
	case semantics.OriginNull:
		c.addIssue(v.Span, "arithmetic %s: %s is null, not a number", op, v.Name)
	default:
		c.addIssue(v.Span, "arithmetic %s: %s has kind %s, not a number", op, v.Name, kind)
	}
}

// checkComparisonOperands reports comparisons whose operand
// kinds are known and incompatible. The runtime's evalCompare
// already errors on a Bool-vs-Scalar comparison or on any
// non-scalar operand; this static check catches the same
// shapes earlier so the user does not have to run the script
// to find them. Only sealed kinds with a clear mismatch are
// flagged; one operand of semantics.OriginUnknown or semantics.OriginScalar
// (without a known literal text) silences the check because
// the runtime types are genuinely ambiguous.
func (c *checker) checkComparisonOperands(prog *syntax.Program) {
	syntax.Inspect(prog, func(n syntax.Node) bool {
		be, ok := n.(*syntax.BinaryExpr)
		if !ok || isArithmeticOpText(be.Op) {
			return true
		}
		if !isComparisonOp(be.Op) {
			return true
		}
		// Def-name-as-bareword hint. A literal operand whose
		// text matches a known def is almost certainly a missing
		// `<- ` bind: the user wrote `if two == 2` meaning to
		// compare the result of calling `two`, not the literal
		// string "two". The arithmetic check has the parallel
		// hint at flagNonNumericOperand; this is its comparison
		// sibling. Emit and return -- the kind-based mismatch
		// below would only re-describe the same problem in
		// less helpful terms.
		if c.flagDefNameOperand(be.Left, be.Op) || c.flagDefNameOperand(be.Right, be.Op) {
			return true
		}
		l := c.inferExprKind(be.Left)
		r := c.inferExprKind(be.Right)
		if l == semantics.OriginUnknown || r == semantics.OriginUnknown {
			return true
		}
		if comparable, mismatch := classifyComparison(l, r, be.Op); !comparable {
			c.addIssue(be.Span, "binary %s: %s", be.Op, mismatch)
		}
		return true
	})
}

// flagDefNameOperand emits a "did you mean `<-`?" hint when e
// is an unquoted literal whose text matches a known def name.
// Returns true when the hint fired so the caller can suppress
// subsequent operand-kind diagnostics that would duplicate the
// same root cause in less actionable terms.
func (c *checker) flagDefNameOperand(e syntax.Expr, op string) bool {
	lit, ok := e.(*syntax.LiteralExpr)
	if !ok || lit.Quoted {
		return false
	}
	if !c.defs[lit.Text] {
		return false
	}
	c.addIssue(lit.Span, "binary %s: operand %q is a def -- call it via `let v <- %s` and use $v in the comparison", op, lit.Text, lit.Text)
	return true
}

// isComparisonOp reports whether op is one of the binary
// comparison operators evalCompare handles: equality, ordering,
// or their textual aliases. Logical operators (and, or) and
// arithmetic operators are handled by their own checks.
func isComparisonOp(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

func isArithmeticOpText(op string) bool {
	switch op {
	case "+", "-", "*", "/", "%":
		return true
	}
	return false
}

// classifyComparison returns whether two kinds can be compared
// under op, and a human-readable explanation when they cannot.
// The rules mirror evalCompare: non-scalar operands cannot be
// compared; Bool supports only == and !=; Scalar-vs-Bool is a
// kind mismatch; otherwise the operands are comparable. The
// caller has already filtered out semantics.OriginUnknown so this
// classifier never sees a wildcard.
func classifyComparison(l, r semantics.OriginKind, op string) (ok bool, msg string) {
	if !isScalarLikeKind(l) {
		return false, fmt.Sprintf("left side has kind %s; only scalars (numbers, strings, booleans) can be compared with %s", l, op)
	}
	if !isScalarLikeKind(r) {
		return false, fmt.Sprintf("right side has kind %s; only scalars (numbers, strings, booleans) can be compared with %s", r, op)
	}
	// Null is a first-class comparable value: `null == null`
	// is true, `null == X` (X non-null) is false, and the
	// cross-kind case is well-defined for == / != rather than
	// a kind-mismatch error. Ordering (<, <=, >, >=) is not
	// defined for null and surfaces explicitly. The same rule
	// lives in the runtime's evalCompare; mirror it here so
	// the static checker does not reject a comparison the
	// runtime would happily evaluate.
	if l == semantics.OriginNull || r == semantics.OriginNull {
		if op != "==" && op != "!=" {
			return false, fmt.Sprintf("null supports only == and !=, not %s", op)
		}
		return true, ""
	}
	if l != r {
		return false, fmt.Sprintf("cannot compare %s to %s; coerce explicitly", l, r)
	}
	if l == semantics.OriginBool && op != "==" && op != "!=" {
		return false, fmt.Sprintf("booleans support only == and !=, not %s", op)
	}
	return true, ""
}

// isScalarLikeKind reports whether kind names a value that the
// comparison evaluator accepts. Scalars (numbers, strings),
// Booleans, and Null are scalar-like; record kinds, jobs,
// command results, lists, and maps are not.
func isScalarLikeKind(k semantics.OriginKind) bool {
	switch k {
	case semantics.OriginScalar, semantics.OriginBool, semantics.OriginNull:
		return true
	}
	return false
}

// isNumericLiteral reports whether text is a literal the
// arithmetic evaluator will accept. The runtime accepts finite
// JSON numbers, keeping integer-shaped values exact and rejecting
// non-finite or non-JSON spellings. The static check uses the same
// predicate to keep both sides aligned.
func isNumericLiteral(text string) bool {
	return syntax.IsJSONNumber(text)
}

// checkLoopExits walks stmts with a foreach-depth counter,
// flagging 'break' or 'continue' that appear at depth 0
// (outside any enclosing foreach). Poll blocks do not count
// as foreach for this purpose, so their bodies inherit the
// caller's depth. Def bodies reset
// the depth: a def is a callable unit, and break/continue
// inside the body but not inside a foreach within the def
// body is wrong even if the def itself is later called from
// inside a foreach.
func (c *checker) checkLoopExits(stmts []syntax.Stmt, depth int) {
	for _, s := range stmts {
		switch n := s.(type) {
		case *syntax.ForEachStmt:
			c.checkLoopExits(n.Body, depth+1)
		case *syntax.PollStmt:
			// Poll is not an iteration construct: break and
			// continue are only valid inside a foreach, nested
			// or otherwise. Pass through depth so a nested
			// foreach inside the body still admits
			// break/continue against itself.
			c.checkLoopExits(n.Body, depth)
		case *syntax.IfStmt:
			c.checkLoopExits(n.Then, depth)
			for _, b := range n.Elifs {
				c.checkLoopExits(b.Body, depth)
			}
			c.checkLoopExits(n.Else, depth)
		case *syntax.DefStmt:
			c.checkLoopExits(n.Body, 0)
		case *syntax.BreakStmt:
			if depth == 0 {
				c.addIssue(n.Span, "'break' outside any foreach loop")
			}
		case *syntax.ContinueStmt:
			if depth == 0 {
				c.addIssue(n.Span, "'continue' outside any foreach loop")
			}
		}
	}
}

// checkThreadDefArity mirrors checkDefArity for thread
// expressions. `$value |> head arg1 arg2` routes through
// def-first bind dispatch at runtime with the LHS appended
// last, so a def whose head matches the thread's first arg
// receives len(ThreadExpr.Args) arguments (head excluded,
// LHS appended). The walker dispatches on every ThreadExpr
// rather than CommandStmt because thread args live in an
// expression position the command-shape checks never see.
func (c *checker) checkThreadDefArity(prog *syntax.Program) {
	syntax.Inspect(prog, func(n syntax.Node) bool {
		te, ok := n.(*syntax.ThreadExpr)
		if !ok || len(te.Args) == 0 {
			return true
		}
		headLit, ok := te.Args[0].(*syntax.LiteralExpr)
		if !ok || headLit.Quoted {
			return true
		}
		name := headLit.Text
		if !c.defs[name] {
			return true
		}
		want := c.defArity[name]
		got := len(te.Args)
		if got != want {
			decl := c.defDeclPos[name]
			c.addIssue(te.Span, "%s: expected %d argument(s), got %d (def declared at %d:%d)",
				name, want, got, decl.Line, decl.Col)
			return true
		}
		// The piped value occupies parameter zero, so literal
		// arguments start at parameter one.
		c.checkDefLiteralArgs(name, te.Args[1:], 1)
		return true
	})
}

// checkBuiltinArity flags shape errors on the async-job
// builtins the runtime documents as taking specific argument
// counts. Static catches typos like 'kill --signa=USR1 $p'
// (--signa is not a flag, so the kill ends up with two
// non-flag args) one pass before the runtime does. Flag args
// (anything starting with '--') are skipped so the count
// reflects only the positional args the runtime cares about.
//
// Coupling: the builtin names and their arities are duplicated
// here from the driver-side dispatch in cmd/bpfman-shell. The
// set is small and stable; if a new lifecycle verb lands, the
// entry adds in one place. Driver-side dispatch and static
// check stay in step via convention rather than a shared
// registry.
func (c *checker) checkBuiltinArity(prog *syntax.Program) {
	type aritySpec struct {
		min, max int // -1 max means unbounded
	}
	specs := map[string]aritySpec{
		"start": {min: 1, max: -1}, // command and optional args
		"wait":  {min: 1, max: 1},  // exactly one $job
		"kill":  {min: 1, max: 1},  // exactly one $job (after flags)
		"jobs":  {min: 0, max: 0},
		"reap":  {min: 0, max: 0},
		// fire's positional shape is (KIND, SENTINEL, ACK) but its
		// flags (--count=N, --waves=K) frequently interpolate (e.g.
		// --count=$n), which makes them non-syntax.LiteralExpr args that
		// nonFlagArgCount cannot recognise as flags. Strict static
		// counting would reject every interpolated invocation. The
		// runtime handler enforces the exact positional/flag shape.
		"fire": {min: 1, max: -1},
	}
	syntax.Inspect(prog, func(n syntax.Node) bool {
		cmd, ok := n.(*syntax.CommandStmt)
		if !ok || len(cmd.Args) == 0 {
			return true
		}
		head, ok := cmd.Args[0].(*syntax.LiteralExpr)
		if !ok {
			return true
		}
		spec, known := specs[head.Text]
		if !known {
			return true
		}
		// Def-first dispatch is the language rule: a user `def
		// start(...)` (or kill, wait, fire, jobs, reap) wins over
		// the builtin at runtime via dispatchCommandByPolicy /
		// dispatchBindByPolicy. The static check must honour the
		// same precedence and skip the builtin arity spec when
		// the name is locally redefined; otherwise a perfectly
		// good def call gets a false-positive "expected N
		// argument(s)" diagnostic against a rule the runtime
		// never reaches.
		if c.defs[head.Text] {
			return true
		}
		got := nonFlagArgCount(cmd.Args[1:])
		switch {
		case got < spec.min:
			c.addIssue(cmd.Span, "%s: expected at least %d argument(s), got %d", head.Text, spec.min, got)
		case spec.max >= 0 && got > spec.max:
			c.addIssue(cmd.Span, "%s: expected at most %d argument(s), got %d", head.Text, spec.max, got)
		}
		return true
	})
}

// checkKillFlags validates --signal=NAME and --grace=DUR
// values on kill invocations. The runtime catches the same
// errors when the kill builtin actually runs; static checking
// surfaces the typo before any side effect. NAME is matched
// against the same fixed signal set the runtime accepts
// (TERM, KILL, INT, QUIT, HUP, USR1, USR2, STOP, CONT) with
// optional 'SIG' prefix and case-insensitive lookup. DUR is
// fed to time.ParseDuration which matches the runtime's
// acceptance.
func (c *checker) checkKillFlags(prog *syntax.Program) {
	syntax.Inspect(prog, func(n syntax.Node) bool {
		cmd, ok := n.(*syntax.CommandStmt)
		if !ok || len(cmd.Args) == 0 {
			return true
		}
		head, ok := cmd.Args[0].(*syntax.LiteralExpr)
		if !ok || head.Text != "kill" {
			return true
		}
		// A user `def kill(...)` resolves first at runtime, so
		// its arguments are positionals to the def, not flags
		// to the builtin. Skip the flag validator when the name
		// is locally redefined; otherwise a literal that happens
		// to look like a --signal / --grace flag triggers a
		// false-positive diagnostic against a rule the runtime
		// never applies.
		if c.defs["kill"] {
			return true
		}
		for _, arg := range cmd.Args[1:] {
			lit, ok := arg.(*syntax.LiteralExpr)
			if !ok {
				continue
			}
			switch {
			case strings.HasPrefix(lit.Text, "--signal="):
				name := strings.TrimPrefix(lit.Text, "--signal=")
				if !jobsig.KnownName(name) {
					c.addIssue(lit.Span, "kill --signal: unknown signal %q", name)
				}
			case strings.HasPrefix(lit.Text, "--grace="):
				dur := strings.TrimPrefix(lit.Text, "--grace=")
				if _, err := time.ParseDuration(dur); err != nil {
					c.addIssue(lit.Span, "kill --grace: %v", err)
				}
			}
		}
		return true
	})
}

// nonFlagArgCount returns the number of args that are not
// '--'-prefixed flag literals. Flag args (--signal=NAME,
// --grace=DUR, ...) are skipped so an arity check counts
// only the positional args the runtime cares about.
func nonFlagArgCount(args []syntax.Expr) int {
	n := 0
	for _, a := range args {
		if lit, ok := a.(*syntax.LiteralExpr); ok && len(lit.Text) >= 2 && lit.Text[:2] == "--" {
			continue
		}
		n++
	}
	return n
}

// jobReferenceTarget returns the variable name of a 'kill $X'
// or 'wait $X' command (the X), or "" if the command is not a
// kill or wait, or its target is not a simple syntax.VarRefExpr.
// Flag args (--signal=NAME, --grace=DUR) are skipped so 'kill
// --signal=USR1 $job' still picks up $job as the target.
func (c *checker) jobReferenceTarget(cmd *syntax.CommandStmt) string {
	target := c.jobReferenceExpr(cmd)
	if target == nil {
		return ""
	}
	return target.Name
}

func (c *checker) jobReferenceExpr(cmd *syntax.CommandStmt) *syntax.VarRefExpr {
	if cmd == nil || len(cmd.Args) == 0 {
		return nil
	}
	lit, ok := cmd.Args[0].(*syntax.LiteralExpr)
	if !ok || (lit.Text != "kill" && lit.Text != "wait") {
		return nil
	}
	// A user `def kill(...)` or `def wait(...)` resolves first
	// at runtime and does not consume a job. Returning a managed
	// target here would silently mask a real leak when the user
	// intended a job operation but actually called a shadowing
	// def; defer to the def's own semantics by skipping the
	// builtin-shape recognition.
	if c.defs[lit.Text] {
		return nil
	}
	for _, arg := range cmd.Args[1:] {
		// Skip flag args; the target is the first non-flag.
		if l, ok := arg.(*syntax.LiteralExpr); ok && len(l.Text) >= 2 && l.Text[:2] == "--" {
			continue
		}
		if v, ok := arg.(*syntax.VarRefExpr); ok {
			return v
		}
	}
	return nil
}
