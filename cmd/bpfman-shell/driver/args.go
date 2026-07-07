// Argument and value helpers shared by the exec primitives and
// the loop dispatcher. ArgText / ArgTexts flatten runtime.Arg values
// into plain strings for argv construction; WriteValueToTemp
// renders a runtime.Value to a private temp file for the file:$var
// adapter.

package driver

import (
	"fmt"
	"os"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

// ArgText extracts the text from a single Arg. For text-bearing
// variants (WordArg, QuotedArg, ScalarValueArg) this returns the
// text directly. For StructuredValueArg this returns "$name", and
// for AdapterArg "adapter:$name" (or "adapter:$name.path" when a
// path is present), both as display forms suitable for error
// messages.
func ArgText(a runtime.Arg) string {
	switch v := a.(type) {
	case runtime.WordArg:
		return v.Text
	case runtime.QuotedArg:
		return v.Text
	case runtime.ScalarValueArg:
		return v.Text
	case runtime.StructuredValueArg:
		return "$" + v.Name
	case runtime.AdapterArg:
		if v.Path != "" {
			return fmt.Sprintf("%s:$%s.%s", v.Adapter, v.Name, v.Path)
		}
		return fmt.Sprintf("%s:$%s", v.Adapter, v.Name)
	default:
		return ""
	}
}

// ArgTexts extracts plain strings from all Args. This is the
// conversion boundary for passing expanded arguments to Kong
// parsers and handlers that operate on resolved string values.
// Structured values should already have been extracted by typed
// helpers before this point; any remaining StructuredValueArg is
// rendered as "$name" for display.
func ArgTexts(args []runtime.Arg) []string {
	ss := make([]string, len(args))
	for i, a := range args {
		ss[i] = ArgText(a)
	}
	return ss
}

// WriteValueToTemp renders a runtime.Value to a private temporary
// file and returns the absolute path. The file is created with
// mode 0600 in the OS default temp directory with a recognisable
// prefix. Used by the `file temp` builtin and by exec / start
// when resolving file:$var adapter args before spawning a
// subprocess.
func WriteValueToTemp(v runtime.Value) (string, error) {
	data, err := runtime.RenderValue(v)
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "bpfman-driver-")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	defer f.Close()
	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("write temp file: %w", err)
	}

	return f.Name(), nil
}
