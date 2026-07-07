package cliformat

import (
	"fmt"
)

// OutputFormat represents the output format type.
type OutputFormat string

// The supported output formats.
const (
	OutputFormatText OutputFormat = "text"
	OutputFormatJSON OutputFormat = "json"
)

// OutputFlags provides output formatting flags.
type OutputFlags struct {
	// Output selects the output format (text or json) via the -o/--output flag; it defaults to text.
	Output string `short:"o" enum:"text,json" default:"text" help:"Output format: text, json."`
}

// Format returns the base format type, or an error if the format is unrecognised.
func (f *OutputFlags) Format() (OutputFormat, error) {
	switch f.Output {
	case string(OutputFormatText):
		return OutputFormatText, nil
	case string(OutputFormatJSON):
		return OutputFormatJSON, nil
	default:
		return "", fmt.Errorf("unknown output format %q; valid formats: text, json", f.Output)
	}
}

// IsStructured reports whether the output format should produce valid
// output even when the result set is empty.
func (f OutputFormat) IsStructured() bool {
	return f == OutputFormatJSON
}

// NeedsLinkGetProgramName reports whether get-link output renders the
// presentation-only BPF Function row.
func (f OutputFormat) NeedsLinkGetProgramName() bool {
	return f == OutputFormatText
}
