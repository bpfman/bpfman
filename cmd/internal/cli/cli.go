// Package cli holds the small command-line helper shared by the
// command binaries: global output writers, captured-output helpers,
// config loading, and logger setup.
package cli

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/bpfman/bpfman/config"
	"github.com/bpfman/bpfman/logging"
)

// CLI carries the common global flags, output writers, and cached
// logger / config. It is meant to be embedded in each command
// binary's Kong root, or in a binary-specific wrapper that adds
// more global flags.
type CLI struct {
	// Config is the path to the bpfman TOML config file, set via --config or the BPFMAN_CONFIG environment variable. An empty value means the default /etc/bpfman/bpfman.toml.
	Config string `name:"config" placeholder:"FILE" group:"global" help:"Config file path (default: /etc/bpfman/bpfman.toml)." env:"BPFMAN_CONFIG"`

	// Log is the logging spec, set via --log or the BPFMAN_LOG environment variable, e.g. "info,manager=debug". An empty value defaults to warn for CLI invocations.
	Log string `name:"log" placeholder:"SPEC" group:"global" help:"Log spec (e.g., 'info,manager=debug')." env:"BPFMAN_LOG"`

	// Out is the writer for command output. Defaults to os.Stdout
	// when DefaultWriters is called. Injected for testability.
	Out io.Writer `kong:"-"`

	// Err is the writer for error output. Defaults to os.Stderr
	// when DefaultWriters is called. Injected for testability.
	Err io.Writer `kong:"-"`

	configOnce   sync.Once     `kong:"-"`
	cachedConfig config.Config `kong:"-"`
	configErr    error         `kong:"-"`

	logger *slog.Logger `kong:"-"`
}

// DefaultWriters fills Out / Err with os.Stdout / os.Stderr when the
// caller has not injected something else.
func (c *CLI) DefaultWriters() {
	if c.Out == nil {
		c.Out = os.Stdout
	}
	if c.Err == nil {
		c.Err = os.Stderr
	}
}

// CapturedOutput is a CLI variant backed by in-memory buffers. The
// embedded CLI is wired to write Out / Err into the buffers; callers
// read the captured bytes after dispatching a command.
type CapturedOutput struct {
	*CLI
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

// WithCaptureOutput returns a CapturedOutput whose CLI mirrors the
// receiver's execution settings but whose Out / Err write into
// private buffers.
func (c *CLI) WithCaptureOutput() *CapturedOutput {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return &CapturedOutput{
		CLI: &CLI{
			Config: c.Config,
			Log:    c.Log,
			Out:    stdout,
			Err:    stderr,
			logger: c.logger,
		},
		stdout: stdout,
		stderr: stderr,
	}
}

// Stdout returns the captured stdout as a string.
func (c *CapturedOutput) Stdout() string {
	return c.stdout.String()
}

// Stderr returns the captured stderr as a string.
func (c *CapturedOutput) Stderr() string {
	return c.stderr.String()
}

// WriteOut writes bytes to Out, returning an error if the write
// fails or is short.
func (c *CLI) WriteOut(p []byte) error {
	n, err := c.Out.Write(p)
	if err != nil {
		return err
	}
	if n != len(p) {
		return io.ErrShortWrite
	}
	return nil
}

// PrintOut writes a string to Out, returning an error on failure.
func (c *CLI) PrintOut(s string) error {
	return c.WriteOut([]byte(s))
}

// PrintOutf formats and writes to Out, returning an error on failure.
func (c *CLI) PrintOutf(format string, args ...any) error {
	return c.PrintOut(fmt.Sprintf(format, args...))
}

// WriteErr writes bytes to Err, returning an error if the write
// fails or is short.
func (c *CLI) WriteErr(p []byte) error {
	n, err := c.Err.Write(p)
	if err != nil {
		return err
	}
	if n != len(p) {
		return io.ErrShortWrite
	}
	return nil
}

// PrintErr writes a string to Err, returning an error on failure.
func (c *CLI) PrintErr(s string) error {
	return c.WriteErr([]byte(s))
}

// PrintErrf formats and writes to Err, returning an error on failure.
func (c *CLI) PrintErrf(format string, args ...any) error {
	return c.PrintErr(fmt.Sprintf(format, args...))
}

// LoadConfig loads the configuration from the config file path.
// Results are cached for the lifetime of the CLI.
func (c *CLI) LoadConfig() (config.Config, error) {
	c.configOnce.Do(func() {
		c.cachedConfig, c.configErr = config.Load(c.Config)
	})
	return c.cachedConfig, c.configErr
}

// InitLogger initialises the CLI logger to stderr at the
// configured log level. CLI invocations default to warn unless
// --log is set.
func (c *CLI) InitLogger() error {
	cfg, err := c.LoadConfig()
	if err != nil {
		return err
	}

	format, err := logging.ParseFormat(cfg.Logging.Format)
	if err != nil {
		return err
	}

	spec := c.Log
	if spec == "" {
		spec = "warn"
	}

	opts := logging.Options{
		CLISpec:    spec,
		ConfigSpec: cfg.Logging.ToSpec(),
		Format:     format,
		Output:     os.Stderr,
	}

	c.logger, err = logging.New(opts)
	return err
}

// Logger returns the CLI logger.
func (c *CLI) Logger() *slog.Logger {
	return c.logger
}

// LoggerFromConfig creates a logger using config file settings, with
// stdout output. Used by long-running services like serve where INFO
// is appropriate and the daemon collects stdout.
func (c *CLI) LoggerFromConfig() (*slog.Logger, error) {
	cfg, err := c.LoadConfig()
	if err != nil {
		return nil, err
	}

	format, err := logging.ParseFormat(cfg.Logging.Format)
	if err != nil {
		return nil, err
	}

	opts := logging.Options{
		CLISpec:    c.Log,
		ConfigSpec: cfg.Logging.ToSpec(),
		Format:     format,
		Output:     os.Stdout,
	}

	return logging.New(opts)
}
