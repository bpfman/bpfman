package syntax

import "sync"

// pureBuiltinSpec is the parser-facing metadata the syntax phase
// needs for expression-position pure calls. Runtime and checker
// semantics live in shell; syntax only needs to know the name and
// arity so it can commit to a PureCallExpr shape.
type pureBuiltinSpec struct {
	Name  string
	Arity int
}

var (
	pureBuiltinRegistryMu sync.RWMutex
	pureBuiltinRegistry   = map[string]pureBuiltinSpec{}
)

func init() {
	// These names are parse-visible language syntax: the syntax
	// phase needs to know their arity even when it is exercised
	// in isolation from the shell runtime package.
	registerPureBuiltin("jq", 2)
	registerPureBuiltin("u32le", 1)
	registerPureBuiltin("u64le", 1)
	registerPureBuiltin("range", 1)
	registerPureBuiltin("zip", 2)
	registerPureBuiltin("path-exists", 1)
	registerPureBuiltin("contains", 2)
	registerPureBuiltin("null", 1)
	registerPureBuiltin("present", 1)
	registerPureBuiltin("missing", 1)
	registerPureBuiltin("empty", 1)
}

// registerPureBuiltin records name as an expression-position pure
// builtin with the given arity.
func registerPureBuiltin(name string, arity int) {
	pureBuiltinRegistryMu.Lock()
	defer pureBuiltinRegistryMu.Unlock()
	pureBuiltinRegistry[name] = pureBuiltinSpec{Name: name, Arity: arity}
}

// lookupPureBuiltin reports whether name is a parser-visible pure
// builtin and, when so, returns its parse-time metadata.
func lookupPureBuiltin(name string) (pureBuiltinSpec, bool) {
	pureBuiltinRegistryMu.RLock()
	defer pureBuiltinRegistryMu.RUnlock()
	pb, ok := pureBuiltinRegistry[name]
	return pb, ok
}
