package ir

// Command-builtin name set for dump-time lane resolution. This is the
// canonical command-head vocabulary of the DSL, not a mutable runtime
// registry: the IR dump owns the classification it prints.
var commandBuiltinNames = map[string]bool{}

func init() {
	for _, name := range []string{
		"defs",
		"exec",
		"file",
		"import",
		"jobs",
		"jq",
		"kill",
		"print",
		"registry",
		"trace",
		"reap",
		"tempdir",
		"start",
		"fire",
		"net",
		"kfunc",
		"bpfman",
		"range",
		"zip",
		"u32le",
		"u64le",
		"uprobe",
		"uprobe-target",
		"wait",
	} {
		commandBuiltinNames[name] = true
	}
}

func isCommandBuiltinName(name string) bool {
	return commandBuiltinNames[name]
}
