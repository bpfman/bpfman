package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Format represents the log output format.
type Format string

const (
	// FormatText outputs logs in human-readable text format.
	FormatText Format = "text"
	// FormatJSON outputs logs in JSON format.
	FormatJSON Format = "json"
)

// ParseFormat parses a format string into a Format.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "text", "":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	default:
		return FormatText, fmt.Errorf("unknown log format: %q", s)
	}
}

// Options configures the logger factory.
type Options struct {
	// EnvSpec is the log spec from environment variable (highest precedence).
	EnvSpec string

	// CLISpec is the log spec from command line flag.
	CLISpec string

	// ConfigSpec is the log spec from config file (lowest precedence).
	ConfigSpec string

	// Format is the output format (text or json).
	Format Format

	// Output is the writer for log output. Defaults to os.Stdout.
	Output io.Writer
}

// New creates a new slog.Logger with component-level filtering.
// Precedence: CLISpec > EnvSpec > ConfigSpec > defaults.
func New(opts Options) (*slog.Logger, error) {
	// Determine which spec to use based on precedence
	// CLI flags override env vars (Unix convention)
	specStr := ""
	switch {
	case opts.CLISpec != "":
		specStr = opts.CLISpec
	case opts.EnvSpec != "":
		specStr = opts.EnvSpec
	case opts.ConfigSpec != "":
		specStr = opts.ConfigSpec
	}

	spec, err := ParseSpec(specStr)
	if err != nil {
		return nil, fmt.Errorf("invalid log spec: %w", err)
	}

	output := opts.Output
	if output == nil {
		output = os.Stdout
	}

	// Create the inner handler based on format
	var innerHandler slog.Handler
	handlerOpts := &slog.HandlerOptions{
		// Set to the lowest possible level so our FilteringHandler controls filtering
		Level: LevelTrace.ToSlog(),
	}

	switch opts.Format {
	case FormatJSON:
		innerHandler = slog.NewJSONHandler(output, handlerOpts)
	default:
		innerHandler = slog.NewTextHandler(output, handlerOpts)
	}

	// Wrap with filtering handler
	filteringHandler := NewFilteringHandler(innerHandler, &spec)

	return slog.New(filteringHandler), nil
}
