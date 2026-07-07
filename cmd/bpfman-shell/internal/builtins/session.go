package builtins

import (
	"fmt"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "defs",
		Handler:  handleDefs,
		Category: driver.CategorySession,
		Usage:    "defs",
		Summary:  "List user-defined commands.",
	})
	Register(driver.Builtin{
		Name:     "print",
		Handler:  handlePrint,
		Category: driver.CategoryIO,
		Usage:    "print [value]...",
		Summary:  "Print zero or more values (none emits a blank line; one pretty; many compact space-joined).",
	})
	Register(driver.Builtin{
		Name:     "trace",
		Handler:  handleTrace,
		Category: driver.CategorySession,
		Usage:    "trace on  |  trace off",
		Summary:  "Toggle execution tracing on or off.",
		Detail: "With tracing on, each statement prints to stderr just before " +
			"it runs, prefixed with '+ file:line:' and rendered with " +
			"interpolations resolved -- argv with $name substituted by the " +
			"value (compact JSON for structured values), 'let' with the bound " +
			"value, 'defer' both at registration and at fire, foreach with the " +
			"loop variable per iteration. Useful for understanding what a " +
			"script actually saw at the moment a call was made. 'trace off' " +
			"disables it again. The CLI flag -x / --trace turns tracing on at " +
			"script startup.",
	})
}

func handleDefs(c driver.Ctx) (runtime.Value, error) {
	session := c.Env.Session
	defs := session.DefSignatures()
	var b strings.Builder
	for _, d := range defs {
		fmt.Fprintf(&b, "  %s(%s)\n", d.Name, strings.Join(d.Params, ", "))
	}
	return runtime.Value{}, c.CLI.PrintOut(b.String())
}

func handleTrace(c driver.Ctx) (runtime.Value, error) {
	args := driver.ArgTexts(c.Args)
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("trace requires exactly one argument: on or off")
	}
	switch args[0] {
	case "on":
		c.Env.Session.SetTrace(true)
	case "off":
		c.Env.Session.SetTrace(false)
	default:
		return runtime.Value{}, fmt.Errorf("trace: unknown argument %q (expected on or off)", args[0])
	}
	return runtime.Value{}, nil
}

func handlePrint(c driver.Ctx) (runtime.Value, error) {
	args := c.Args
	if len(args) == 0 {
		return runtime.Value{}, c.CLI.PrintOut("\n")
	}
	if len(args) == 1 {
		v, err := PrintValue(args[0])
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.Value{}, driver.WriteValue(c.CLI, v)
	}
	parts := make([]string, len(args))
	for i, a := range args {
		v, err := PrintValue(a)
		if err != nil {
			return runtime.Value{}, err
		}

		s, err := runtime.RenderCompact(v)
		if err != nil {
			return runtime.Value{}, fmt.Errorf("print: argument %d: %v", i+1, err)
		}
		parts[i] = s
	}
	return runtime.Value{}, c.CLI.PrintOut(strings.Join(parts, " ") + "\n")
}

// PrintValue resolves a single print/file argument into a
// runtime.Value. Every arg kind is treated as a value: WordArg and
// QuotedArg are literal strings; ScalarValueArg and
// StructuredValueArg are already-resolved values from variable
// expansion or command substitution; AdapterArg carries its
// resolved Value. Bare identifiers are not looked up in the
// session -- callers must write $name to dereference a variable.
func PrintValue(arg runtime.Arg) (runtime.Value, error) {
	switch a := arg.(type) {
	case runtime.WordArg:
		return runtime.StringValue(a.Text), nil
	case runtime.QuotedArg:
		return runtime.StringValue(a.Text), nil
	case runtime.ScalarValueArg:
		return runtime.StringValue(a.Text), nil
	case runtime.StructuredValueArg:
		return a.Value, nil
	case runtime.AdapterArg:
		return a.Value, nil
	case runtime.NilArg:
		return runtime.Value{}, nil
	default:
		return runtime.Value{}, fmt.Errorf("print: unsupported argument kind %T", arg)
	}
}
