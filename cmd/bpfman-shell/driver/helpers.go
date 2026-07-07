// Small mechanism helpers that don't fit one of the other driver
// files: the silent-error sentinel and the value writer.

package driver

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// ErrSilent is returned when the error has already been
// communicated (typically via a printed diagnostic) and the
// embedding binary should exit non-zero without another message
// from Kong.
var ErrSilent = errors.New("silent error")

// WriteValue renders a runtime.Value onto cli: nil as "null",
// scalars as plain text, structured values as indented JSON.
// Used by the loop's PrintResult callback and by the `print`
// session builtin.
func WriteValue(cli *cli.CLI, v runtime.Value) error {
	if v.IsNil() {
		return cli.PrintOut("null\n")
	}
	if v.IsScalar() {
		s, err := v.Scalar()
		if err != nil {
			return err
		}

		return cli.PrintOut(s + "\n")
	}
	b, err := json.MarshalIndent(v.Raw(), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}

	return cli.PrintOut(string(b) + "\n")
}
