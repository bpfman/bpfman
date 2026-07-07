package semantics

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// Pure builtins are expression-position value producers: no
// captured-result envelope and no statement-only control-flow
// semantics. Most are deterministic transforms (`u32le`,
// `u64le`, `jq`, `range`); some are side-effect-free predicates
// over ambient state such as `path-exists`.
//
// The language needs to know which names are pure for two
// reasons:
//
//  1. Static-check shape inference (shell/check.go). A bind RHS
//     whose first word is a pure builtin produces the builtin's
//     declared return Shape, not the default captured-envelope
//     shape.
//
//  2. Expression-position invocation (shell/parse.go,
//     shell/expr.go). The parser dispatches a bare identifier in
//     expression position to a PureCallExpr when the name is
//     registered, consuming the registered arity of primary
//     expressions as arguments.
//
// The handler itself lives in cmd/bpfman-shell (the dispatcher
// wires it through ExecBind). The shell package only needs to
// know the name, the arity, the return Shape, and any narrow
// literal-only static contract the checker should enforce; that
// is what the registry stores.

type pureLiteralContractKind uint8

const (
	pureLiteralContractNone pureLiteralContractKind = iota
	pureLiteralContractUnsigned
	pureLiteralContractRangeBound
)

// pureLiteralContract describes an optional literal-only checker
// rule for a pure builtin. The zero value means "no extra static
// contract"; nonzero values stay intentionally narrow so the
// registry can carry targeted, low-false-positive contracts.
type pureLiteralContract struct {
	kind pureLiteralContractKind
	bits int
	max  uint64
}

// unsignedPureLiteralContract declares that literal arguments
// must parse as non-negative unsigned integers that fit in the
// given width. Used by builtins like u32le and u64le.
func unsignedPureLiteralContract(bits int, max uint64) pureLiteralContract {
	return pureLiteralContract{
		kind: pureLiteralContractUnsigned,
		bits: bits,
		max:  max,
	}
}

// rangePureLiteralContract declares that a literal bound must be
// a non-negative integer not exceeding max. Used by the range
// pure builtin, whose runtime iterates over int32-sized bounds.
func rangePureLiteralContract(max uint64) pureLiteralContract {
	return pureLiteralContract{
		kind: pureLiteralContractRangeBound,
		max:  max,
	}
}

// pureBuiltin describes one entry in the pure-builtin registry.
//
//	Arity       number of positional primary arguments the call
//	            consumes in expression position. The parser
//	            takes exactly this many primaries; the static
//	            checker validates the same count at the bind path.
//	ReturnShape the Shape the call produces, used by the static
//	            checker to propagate types into downstream let
//	            bindings ('let x = u32le N' -> x is OriginScalar).
//	LiteralContract optional literal-only checker validation,
//	            such as unsigned-width or range-bound checks.
type pureBuiltin struct {
	Name            string
	Arity           int
	ReturnShape     Shape
	LiteralContract pureLiteralContract
}

// pureBuiltinRegistry is the source of truth for the language's
// pure-builtin set. These names, arities, return shapes, and
// literal-only contracts are part of the DSL itself, so the
// shared semantic substrate owns them declaratively rather than
// accepting init-time mutation from cmd-side packages.
var pureBuiltinRegistry = func() map[string]pureBuiltin {
	rangeElem := KindShape(OriginScalar)
	zipPair := Shape{Sealed: false, Kind: OriginUnknown}
	return map[string]pureBuiltin{
		"jq": {
			Name:        "jq",
			Arity:       2,
			ReturnShape: Shape{Sealed: false, Kind: OriginUnknown},
		},
		"u32le": {
			Name:            "u32le",
			Arity:           1,
			ReturnShape:     KindShape(OriginScalar),
			LiteralContract: unsignedPureLiteralContract(32, math.MaxUint32),
		},
		"u64le": {
			Name:            "u64le",
			Arity:           1,
			ReturnShape:     KindShape(OriginScalar),
			LiteralContract: unsignedPureLiteralContract(64, math.MaxUint64),
		},
		"range": {
			Name:            "range",
			Arity:           1,
			ReturnShape:     Shape{Sealed: false, Kind: OriginUnknown, Elem: &rangeElem},
			LiteralContract: rangePureLiteralContract(math.MaxInt32),
		},
		"zip": {
			Name:        "zip",
			Arity:       2,
			ReturnShape: Shape{Sealed: false, Kind: OriginUnknown, Elem: &zipPair},
		},
		"path-exists": {
			Name:        "path-exists",
			Arity:       1,
			ReturnShape: KindShape(OriginBool),
		},
		"contains": {
			Name:        "contains",
			Arity:       2,
			ReturnShape: KindShape(OriginBool),
		},
		"null": {
			Name:        "null",
			Arity:       1,
			ReturnShape: KindShape(OriginBool),
		},
		"present": {
			Name:        "present",
			Arity:       1,
			ReturnShape: KindShape(OriginBool),
		},
		"missing": {
			Name:        "missing",
			Arity:       1,
			ReturnShape: KindShape(OriginBool),
		},
		"empty": {
			Name:        "empty",
			Arity:       1,
			ReturnShape: KindShape(OriginBool),
		},
	}
}()

func lookupPureBuiltin(name string) (pureBuiltin, bool) {
	pb, ok := pureBuiltinRegistry[name]
	return pb, ok
}

// IsPureBuiltin reports whether name is part of the language's
// pure-builtin set.
func IsPureBuiltin(name string) bool {
	_, ok := pureBuiltinRegistry[name]
	return ok
}

// PureBuiltinReturnShape reports the declared shape of a pure builtin,
// if name resolves to one.
func PureBuiltinReturnShape(name string) (Shape, bool) {
	pb, ok := pureBuiltinRegistry[name]
	if !ok {
		return Shape{}, false
	}
	return pb.ReturnShape, true
}

// PureBuiltinLiteralIssue applies any narrow literal-only static
// contract attached to a pure builtin call. It returns the precise
// offending span plus the checker diagnostic text when the call
// violates its builtin's contract; callers that do not care about
// static validation can ignore it entirely.
func PureBuiltinLiteralIssue(call *syntax.PureCallExpr) (source.Span, string, bool) {
	if call == nil || len(call.Args) != 1 {
		return source.Span{}, "", false
	}
	pb, ok := lookupPureBuiltin(call.Name)
	if !ok {
		return source.Span{}, "", false
	}
	switch pb.LiteralContract.kind {
	case pureLiteralContractUnsigned:
		return unsignedPureLiteralIssue(call, pb.LiteralContract.max, pb.LiteralContract.bits)
	case pureLiteralContractRangeBound:
		return rangePureLiteralIssue(call, pb.LiteralContract.max)
	default:
		return source.Span{}, "", false
	}
}

func pureLiteralText(e syntax.Expr) (string, bool) {
	switch v := e.(type) {
	case *syntax.LiteralExpr:
		return v.Text, true
	case *syntax.NegateExpr:
		lit, ok := v.Operand.(*syntax.LiteralExpr)
		if !ok {
			return "", false
		}
		return "-" + lit.Text, true
	default:
		return "", false
	}
}

func unsignedPureLiteralIssue(call *syntax.PureCallExpr, max uint64, bits int) (source.Span, string, bool) {
	text, ok := pureLiteralText(call.Args[0])
	if !ok {
		return source.Span{}, "", false
	}
	text = strings.TrimSpace(text)
	span := syntax.NodeSpan(call.Args[0])
	if text == "" {
		return span, fmt.Sprintf("%s: empty argument", call.Name), true
	}
	if strings.HasPrefix(text, "-") {
		return span, fmt.Sprintf("%s: negative values are not representable (got %q)", call.Name, text), true
	}
	n, err := strconv.ParseUint(text, 0, 64)
	if err != nil {
		return span, fmt.Sprintf("%s: invalid integer %q: %v", call.Name, text, err), true
	}
	if n > max {
		return span, fmt.Sprintf("%s: value %d does not fit in %d bits", call.Name, n, bits), true
	}
	return source.Span{}, "", false
}

func rangePureLiteralIssue(call *syntax.PureCallExpr, max uint64) (source.Span, string, bool) {
	text, ok := pureLiteralText(call.Args[0])
	if !ok {
		return source.Span{}, "", false
	}
	text = strings.TrimSpace(text)
	span := syntax.NodeSpan(call.Args[0])
	if text == "" {
		return span, fmt.Sprintf("%s: empty argument", call.Name), true
	}
	if strings.HasPrefix(text, "-") {
		return span, fmt.Sprintf("%s: negative bound is not allowed (got %q)", call.Name, text), true
	}
	n, err := strconv.ParseUint(text, 0, 64)
	if err != nil {
		return span, fmt.Sprintf("%s: invalid integer %q: %v", call.Name, text, err), true
	}
	if n > max {
		return span, fmt.Sprintf("%s: bound %d exceeds the maximum of %d", call.Name, n, max), true
	}
	return source.Span{}, "", false
}
