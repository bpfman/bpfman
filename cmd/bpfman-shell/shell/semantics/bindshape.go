package semantics

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"

// Bind-shape lookup: the static checker queries this table to
// learn what primary Shape a `<-` bind RHS produces. The table is
// declarative language semantics, not a cmd-side registration
// surface: start/fire return jobs, exec/wait/kill return result
// envelopes, net and bpfman inspect their subcommand grammar, and
// everything else falls through to the external-result default in
// the checker.

// bindShapeFn computes the primary Shape a bind RHS produces given
// the arguments after the command name. Most builtins ignore the
// args and return a fixed Shape; subcommand-aware builtins inspect
// the first arg to decide.
type bindShapeFn func(args []syntax.Expr) Shape

func staticBindShape(s Shape) bindShapeFn {
	return func([]syntax.Expr) Shape { return s }
}

var bindShapeRegistry = map[string]bindShapeFn{
	"bpfman":        inferBpfmanBindShape,
	"exec":          staticBindShape(KindShape(OriginEnvelope)),
	"file":          staticBindShape(Shape{Sealed: false, Kind: OriginUnknown}),
	"fire":          staticBindShape(KindShape(OriginJob)),
	"kfunc":         inferKfuncBindShape,
	"kill":          staticBindShape(KindShape(OriginEnvelope)),
	"linkinfo":      staticBindShape(KindShape(OriginLinkInfo)),
	"net":           inferNetBindShape,
	"process":       staticBindShape(Shape{Sealed: false, Kind: OriginUnknown}),
	"proginfo":      staticBindShape(KindShape(OriginProgInfo)),
	"registry":      staticBindShape(KindShape(OriginScalar)),
	"start":         staticBindShape(KindShape(OriginJob)),
	"tempdir":       staticBindShape(Shape{Sealed: false, Kind: OriginUnknown}),
	"uprobe":        inferUprobeBindShape,
	"uprobe-target": staticBindShape(Shape{Sealed: false, Kind: OriginUnknown}),
	"wait":          staticBindShape(KindShape(OriginEnvelope)),
}

// HasBindShape reports whether name has a semantics-owned bind-shape
// rule. Callers use this for head classification without depending
// on the registry's internal function table.
func HasBindShape(name string) bool {
	_, ok := bindShapeRegistry[name]
	return ok
}

// InferBindShape reports the semantics-owned primary shape for a bind
// RHS head and its args. Callers get the language answer directly,
// rather than a function handle into the registry's storage shape.
func InferBindShape(name string, args []syntax.Expr) (Shape, bool) {
	fn, ok := bindShapeRegistry[name]
	if !ok {
		return Shape{}, false
	}
	return fn(args), true
}

func inferNetBindShape(args []syntax.Expr) Shape {
	if len(args) < 1 {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	sub, ok := args[0].(*syntax.LiteralExpr)
	if !ok || sub.Quoted {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	switch sub.Text {
	case "veth-pair":
		return KindShape(OriginNetPair)
	case "netns-veth-pair":
		return KindShape(OriginNetnsVethPair)
	case "release", "exec":
		return KindShape(OriginEnvelope)
	case "start":
		return KindShape(OriginJob)
	}
	return Shape{Sealed: false, Kind: OriginUnknown}
}

func inferKfuncBindShape(args []syntax.Expr) Shape {
	if len(args) < 1 {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	sub, ok := args[0].(*syntax.LiteralExpr)
	if !ok || sub.Quoted {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	switch sub.Text {
	case "acquire":
		return KindShape(OriginKfunc)
	case "release", "fire":
		return KindShape(OriginEnvelope)
	}
	return Shape{Sealed: false, Kind: OriginUnknown}
}

func inferUprobeBindShape(args []syntax.Expr) Shape {
	if len(args) < 1 {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	sub, ok := args[0].(*syntax.LiteralExpr)
	if !ok || sub.Quoted {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	switch sub.Text {
	case "target":
		return Shape{Sealed: false, Kind: OriginUnknown}
	case "fire":
		return KindShape(OriginEnvelope)
	}
	return Shape{Sealed: false, Kind: OriginUnknown}
}
