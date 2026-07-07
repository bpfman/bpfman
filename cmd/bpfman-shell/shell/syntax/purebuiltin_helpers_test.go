package syntax

// unregisterPureBuiltin removes name from the parser-facing pure builtin
// registry. Test-only cleanup helper: tests register a pure builtin and
// unregister it afterwards, while production only ever registers (at
// init).
func unregisterPureBuiltin(name string) {
	pureBuiltinRegistryMu.Lock()
	defer pureBuiltinRegistryMu.Unlock()
	delete(pureBuiltinRegistry, name)
}
