package ir

import "testing"

func TestCommandBuiltinNames_ContainsCanonicalHeads(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"print",
		"trace",
		"net",
		"kfunc",
		"bpfman",
		"wait",
		"import",
	} {
		if !isCommandBuiltinName(name) {
			t.Fatalf("isCommandBuiltinName(%q) = false, want true", name)
		}
	}
	if isCommandBuiltinName("totally_unknown_command") {
		t.Fatalf("isCommandBuiltinName(%q) = true, want false", "totally_unknown_command")
	}
}
