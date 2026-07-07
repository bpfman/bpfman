// bpfman-shell command-line interface using Kong for argument parsing.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/kong"

	k8slabels "k8s.io/apimachinery/pkg/labels"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/internal/builtins"
	bpfmanbuiltin "github.com/bpfman/bpfman/cmd/bpfman-shell/internal/builtins/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/scriptmeta"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
	"github.com/bpfman/bpfman/cmd/internal/cli"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/internal/registryfixture"
	"github.com/bpfman/bpfman/version"
)

// CLI is the root command structure for bpfman-shell. The binary is
// a script runner and inspection tool: with a positional Script it
// runs the named file; with no positional argument (or Script="-")
// it reads one whole program from stdin; with --check it parses
// without evaluating.
type CLI struct {
	cli.CLI

	kctx *kong.Context `kong:"-"`

	// Scripts names the script file to run. A single "-", or an
	// empty list, reads one whole program from stdin. With
	// ListScripts it accepts files or directories to enumerate.
	Scripts []string `arg:"" optional:"" name:"script" help:"Script file to run; '-' reads a whole program from stdin; omit to read a whole program from stdin. With --list-scripts, accepts files or directories."`

	// Directory changes to this directory before anything else
	// runs, like make -C dir, so the script path, imported
	// libraries, spawned subprocesses, and external commands all
	// see the new working directory.
	Directory string `name:"directory" short:"C" help:"Change to this directory before doing anything else, like make -C dir. The script path, imported libraries, spawned subprocesses, and external commands all see the new working directory."`

	// Check parses the input without evaluating it, reporting
	// syntax errors and exiting.
	Check bool `name:"check" short:"c" help:"Parse input without evaluating; report syntax errors and exit."`

	// NoCheck skips the static-analysis pre-flight before script
	// evaluation; the default runs Check first and refuses on
	// errors.
	NoCheck bool `name:"no-check" help:"Skip the static-analysis pre-flight before script evaluation. Default is to run Check first and refuse on errors."`

	// AST parses the input and prints the AST tree of the whole
	// program to stdout without evaluating.
	AST bool `name:"ast" help:"Parse input and print the AST tree of the whole program to stdout; do not evaluate."`

	// Fmt formats the input as canonical bpfman-shell source and
	// prints it to stdout without evaluating.
	Fmt bool `name:"fmt" help:"Format input as canonical bpfman-shell source and print it to stdout; do not evaluate."`

	// FmtWrite writes the formatted output back to the script file;
	// valid only together with Fmt.
	FmtWrite bool `name:"write" short:"w" help:"Write formatted output back to the script file when used with fmt."`

	// Lowered parses the input, lowers it to the canonical IR, and
	// prints the lowered form to stdout without evaluating.
	Lowered bool `name:"lowered" help:"Parse input, lower it to the canonical IR, and print the lowered form to stdout; do not evaluate."`

	// Symbols parses the input and prints a JSON symbol table for
	// editor tooling without evaluating.
	Symbols bool `name:"symbols" help:"Parse input and print a JSON symbol table for editor tooling; do not evaluate."`

	// ListScripts prints the script paths whose header labels match
	// Selector; it runs no scripts and opens no bpfman state.
	ListScripts bool `name:"list-scripts" help:"Print script paths whose header labels match --selector; does not run scripts or open bpfman state."`

	// Selector is the Kubernetes-style label selector applied by
	// ListScripts, for example 'program in (tc,xdp),external'.
	Selector string `name:"selector" help:"Kubernetes-style label selector for --list-scripts, for example 'program in (tc,xdp),external'."`

	// Trace traces each statement to stderr with interpolations
	// resolved, like bash -x. It is equivalent to running
	// 'trace on' at script start; toggle within a session with
	// 'trace on' / 'trace off'.
	Trace bool `name:"trace" short:"x" help:"Trace each statement to stderr with interpolations resolved, like bash -x. Equivalent to running 'trace on' at script start; toggle with 'trace on' / 'trace off' from within a session."`

	// Version prints version information and exits.
	Version bool `name:"version" short:"V" help:"Print version information and exit."`
}

// NewCLI creates and initialises a CLI instance by parsing
// command-line arguments.
func NewCLI() (*CLI, error) {
	rewriteFmtSubcommand()

	var c CLI
	c.kctx = kong.Parse(&c, KongOptions()...)
	c.DefaultWriters()

	// Initialise logger eagerly. Skip parse-only modes and
	// --version, which should be runnable without access to the
	// system config file.
	if !c.Check && !c.AST && !c.Fmt && !c.FmtWrite && !c.Lowered && !c.Symbols && !c.ListScripts && !c.Version {
		if err := c.InitLogger(); err != nil {
			return nil, fmt.Errorf("create logger: %w", err)
		}
	}

	return &c, nil
}

func rewriteFmtSubcommand() {
	if len(os.Args) >= 2 && os.Args[1] == "fmt" {
		os.Args = append([]string{os.Args[0], "--fmt"}, os.Args[2:]...)
	}
}

// Execute runs the parsed command.
func (c *CLI) Execute(ctx context.Context) error {
	if c.Version {
		return c.PrintOut(version.Get().Long())
	}
	// Apply -C / --directory before anything path-relative runs:
	// opening the script file, the static checker, every
	// subprocess spawned at runtime. Matches make -C / git -C
	// semantics: change cwd once, then proceed as if the user had
	// cd'd there manually.
	if c.Directory != "" {
		if err := os.Chdir(c.Directory); err != nil {
			_ = c.PrintErrf("bpfman-shell: error: chdir %q: %v\n", c.Directory, err)
			return err
		}
	}
	if err := c.Run(ctx); err != nil {
		if !errors.Is(err, driver.ErrSilent) {
			_ = c.PrintErrf("bpfman-shell: error: %v\n", err)
		}
		return err
	}
	return nil
}

// KongOptions returns the Kong configuration options for the CLI.
func KongOptions() []kong.Option {
	return []kong.Option{
		kong.Name("bpfman-shell"),
		kong.Description("Development / test / ops companion to bpfman: DSL runner, inspection tool, test scaffolding."),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
		kong.ShortUsageOnError(),
		kong.Vars{
			"default_runtime_dir":     fs.DefaultRoot,
			"default_image_cache_dir": "/var/cache/bpfman",
		},
	}
}

// openInputReader chooses the input source for the CLI's script and
// parse-only modes: the positional script file, stdin via "-", or
// stdin when no positional argument was supplied.
func (c *CLI) openInputReader() (driver.LineReader, error) {
	script := c.script()
	if script == "" {
		return driver.OpenScriptReader("-")
	}
	return driver.OpenScriptReader(script)
}

// runCheck drives the --check pipeline: open the input source and
// hand it to driver.CheckInput. Returns driver.ErrSilent when any
// issue was emitted so the process exits non-zero without an extra
// message from Kong.
func (c *CLI) runCheck() error {
	reader, err := c.openInputReader()
	if err != nil {
		return err
	}

	defer reader.Close()

	file := c.inputFileLabel()
	if driver.CheckInput(reader, c.Err, file) {
		return driver.ErrSilent
	}
	return nil
}

// runAST drives the --ast pipeline: slurp the whole input, parse it
// as one program, dump the resulting AST to stdout, and exit.
func (c *CLI) runAST() error {
	reader, err := c.openInputReader()
	if err != nil {
		return err
	}

	defer reader.Close()

	file := c.inputFileLabel()
	if driver.ASTInput(reader, c.Out, c.Err, file) {
		return driver.ErrSilent
	}
	return nil
}

// runLowered drives the --lowered pipeline: slurp the whole input,
// parse it as one program, lower it to the canonical IR, dump the
// lowered form to stdout, and exit.
func (c *CLI) runLowered() error {
	reader, err := c.openInputReader()
	if err != nil {
		return err
	}

	defer reader.Close()

	file := c.inputFileLabel()
	if driver.LoweredInput(reader, c.Out, c.Err, file) {
		return driver.ErrSilent
	}
	return nil
}

// runSymbols drives the --symbols pipeline: slurp the whole input,
// render the visible symbol table as JSON, and exit.
func (c *CLI) runSymbols() error {
	reader, err := c.openInputReader()
	if err != nil {
		return err
	}

	defer reader.Close()

	file := c.inputFileLabel()
	if driver.SymbolsInput(reader, c.Out, c.Err, file) {
		return driver.ErrSilent
	}
	return nil
}

// runFmt drives the fmt pipeline: parse one input file (or stdin),
// render the canonical source form, and either print it or write it
// back to the named script.
func (c *CLI) runFmt() error {
	if c.FmtWrite {
		script := c.script()
		if script == "" || script == "-" {
			return fmt.Errorf("fmt -w requires a script file")
		}
		reader, err := driver.OpenScriptReader(script)
		if err != nil {
			return err
		}
		defer reader.Close()

		formatted, hadIssue := driver.FormatInputString(reader, c.Err, c.inputFileLabel())
		if hadIssue {
			return driver.ErrSilent
		}

		if err := os.WriteFile(script, []byte(formatted), 0o644); err != nil {
			return fmt.Errorf("write formatted script: %w", err)
		}
		return nil
	}

	reader, err := c.openInputReader()
	if err != nil {
		return err
	}

	defer reader.Close()

	file := c.inputFileLabel()
	if driver.FormatInput(reader, c.Out, c.Err, file) {
		return driver.ErrSilent
	}
	return nil
}

// Run is the CLI's top-level entry. With --check / --ast /
// --lowered / --symbols it short-circuits to those parse-only
// pipelines; otherwise it builds a script-runner config and delegates
// to driver.Run.
func (c *CLI) Run(ctx context.Context) error {
	if c.ListScripts {
		return c.runListScripts()
	}
	if len(c.Scripts) > 1 {
		return fmt.Errorf("expected at most one script to run; use --list-scripts to inspect multiple scripts")
	}
	if c.FmtWrite && !c.Fmt {
		return fmt.Errorf("-w/--write is only valid with fmt")
	}
	if c.Check {
		return c.runCheck()
	}
	if c.AST {
		return c.runAST()
	}
	if c.Fmt {
		return c.runFmt()
	}
	if c.Lowered {
		return c.runLowered()
	}
	if c.Symbols {
		return c.runSymbols()
	}
	defer registryfixture.Close()

	session := runtime.NewSession()
	if c.Trace {
		session.SetTrace(true)
	}

	lr, err := c.openInputReader()
	if err != nil {
		return err
	}

	defer lr.Close()

	return driver.Run(ctx, driver.Config{
		CLI:          &c.CLI,
		LineReader:   lr,
		Session:      session,
		File:         c.inputFileLabel(),
		NoCheck:      c.NoCheck,
		Fallback:     commandFallback,
		BindFallback: bindCommandFallback,
		MakeAssert:   makeExecAssert,
	})
}

// commandFallback is the statement-position fallback the runner
// loop calls when no registered builtin matches the first
// token. It owns the "forgot the bpfman prefix" diagnostic;
// unknown first words fall through to external execution.
func commandFallback(ctx context.Context, cli *cli.CLI, args []runtime.Arg, loc driver.SourceLoc, span source.Span) (bool, runtime.Value, error) {
	if len(args) == 0 {
		return false, runtime.Value{}, nil
	}
	first := driver.ArgText(args[0])
	if bpfmanbuiltin.IsTopLevelNoun(first) {
		return true, runtime.Value{}, syntax.SpanErrorf(span, "domain commands require a \"bpfman\" prefix: try %q", "bpfman "+strings.Join(driver.ArgTexts(args), " "))
	}
	return false, runtime.Value{}, nil
}

// bindCommandFallback is the bind-position fallback the runner
// loop calls when no registered builtin matches. It owns the
// wait and net-exec fast paths (which need the bind's Rc to
// reflect the captured inner outcome) and the "forgot the
// bpfman prefix" diagnostic. Unknown first words fall through
// to external execution.
func bindCommandFallback(ctx context.Context, cli *cli.CLI, session *runtime.Session, env *runtime.Env, args []runtime.Arg, loc driver.SourceLoc, span source.Span) (bool, runtime.BindResult, error) {
	if len(args) == 0 {
		return false, runtime.BindResult{}, nil
	}
	first := driver.ArgText(args[0])

	// 'wait $job' is special-cased so the bind's Rc reflects
	// the JOB's outcome, not merely "wait succeeded".
	if first == "wait" {
		envEnv, err := builtins.WaitEnvelope(ctx, args[1:])
		if err != nil {
			return true, runtime.BindResult{}, err
		}
		return true, runtime.BindResult{Rc: envEnv, Primary: runtime.ValueFromEnvelope(envEnv)}, nil
	}

	// 'net exec $pair CMD...' captures into a real envelope
	// so the bind's Rc reflects the netns command's actual
	// outcome.
	if first == "net" && len(args) >= 2 && driver.ArgText(args[1]) == "exec" {
		envEnv, err := builtins.NetExecEnvelope(ctx, args[2:])
		if err != nil {
			return true, runtime.BindResult{}, err
		}
		return true, runtime.BindResult{Rc: envEnv, Primary: runtime.ValueFromEnvelope(envEnv)}, nil
	}

	if bpfmanbuiltin.IsTopLevelNoun(first) {
		rc := runtime.FailEnvelope()
		rc.Stderr = fmt.Sprintf("domain commands require a \"bpfman\" prefix: try %q", "bpfman "+strings.Join(driver.ArgTexts(args), " "))
		return true, runtime.BindResult{Rc: rc, Primary: runtime.ValueFromEnvelope(rc)}, nil
	}
	return false, runtime.BindResult{}, nil
}

// runScript is a thin test seam wrapping driver.Loop with the
// bpfman-side fallbacks pre-wired. The full bpfman-shell binary
// goes through Run; tests that want to drive one whole program
// directly over a string source call this wrapper.
func runScript(ctx context.Context, cli *cli.CLI, lr driver.LineReader, session *runtime.Session, file string, _ bool, noCheck bool) error {
	if file == "" || file == "-" {
		file = "<stdin>"
	}
	return driver.Loop(ctx, driver.Config{
		CLI:          cli,
		LineReader:   lr,
		Session:      session,
		File:         file,
		NoCheck:      noCheck,
		Fallback:     commandFallback,
		BindFallback: bindCommandFallback,
		MakeAssert:   makeExecAssert,
	})
}

func (c *CLI) inputFileLabel() string {
	script := c.script()
	if script == "" || script == "-" {
		return "<stdin>"
	}
	return script
}

func (c *CLI) script() string {
	if len(c.Scripts) == 0 {
		return ""
	}
	return c.Scripts[0]
}

func (c *CLI) runListScripts() error {
	selector := k8slabels.Everything()
	if c.Selector != "" {
		parsed, err := k8slabels.Parse(c.Selector)
		if err != nil {
			return fmt.Errorf("parse --selector=%q: %w", c.Selector, err)
		}

		selector = parsed
	}

	paths, err := expandScriptPaths(c.Scripts)
	if err != nil {
		return err
	}

	if len(paths) == 0 {
		return fmt.Errorf("--list-scripts requires at least one script file or directory")
	}

	for _, path := range paths {
		mode, err := scriptmeta.Read(path)
		if err != nil {
			return err
		}

		if selector.Matches(mode.Labels) {
			if err := c.PrintOutf("%s\n", path); err != nil {
				return err
			}
		}
	}
	return nil
}

func expandScriptPaths(inputs []string) ([]string, error) {
	var paths []string
	for _, input := range inputs {
		info, err := os.Stat(input)
		if err != nil {
			return nil, err
		}

		if !info.IsDir() {
			paths = append(paths, input)
			continue
		}
		if err := filepath.WalkDir(input, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() || filepath.Ext(path) != ".bpfman" {
				return nil
			}
			paths = append(paths, path)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	return paths, nil
}
