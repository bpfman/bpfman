// External `bpfman` dispatch: invoke the bpfman binary as a
// subprocess, capture stdout, and decode into the typed Value
// shape for the matching (noun, verb). The dispatcher appends -o
// json unless the command is image build or image inspect, or the
// caller already passed an output flag; stdout is therefore JSON
// for the commands that decode a typed payload, whereas image
// build streams build-log text and decodes to an empty Value.
// This keeps command parsing and execution in cmd/bpfman; the
// shell only adapts runtime arguments and decodes the CLI JSON
// contract.

package bpfmanbuiltin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	bpfman "github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/platform"
	"github.com/bpfman/bpfman/platform/image/oci"
)

// dispatchCommandExternal forks the bpfman binary with the
// command's textual args, appends -o json when the command
// supports an output format and the caller did not already pass
// one, captures stdout, and decodes via decodeBpfmanResult.
func dispatchCommandExternal(ctx context.Context, args []runtime.Arg) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("missing command after \"bpfman\"; try \"bpfman program list\"")
	}

	argv := make([]string, 0, len(args))
	for i, a := range args {
		text, err := argToCLIText(a)
		if err != nil {
			return runtime.Value{}, fmt.Errorf("bpfman arg %d: %w", i+1, err)
		}

		argv = append(argv, text)
	}
	if commandSupportsOutput(argv) && !hasOutputFlag(argv) {
		argv = append(argv, "-o", "json")
	}

	var stdout, stderr bytes.Buffer
	cmd, cancellationErr := newBPFManCommand(ctx, argv...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cancelErr := cancellationErr(); cancelErr != nil {
			return runtime.Value{}, cancelErr
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return runtime.Value{}, errors.New(msg)
	}

	if cancelErr := cancellationErr(); cancelErr != nil {
		return runtime.Value{}, cancelErr
	}
	return decodeBpfmanResult(args, stdout.Bytes())
}

func displayName(name string) string {
	if name == "" {
		return "argument"
	}
	return "$" + name
}

// argToCLIText renders a single runtime.Arg into the textual form
// the bpfman CLI expects on argv. WordArg / QuotedArg /
// ScalarValueArg pass straight through; AdapterArg renders its
// value's scalar form; StructuredValueArg dispatches on the
// value's origin capability:
//
//   - HasLinkID          -> the bpfman link ID as decimal text
//   - HasKernelProgramID -> the program ID as decimal text
//
// Dispatching on the capability rather than the concrete origin
// type preserves the `$link` / `$prog` ergonomic that scripts
// already depend on. A StructuredValueArg whose origin satisfies neither
// capability errors out: the CLI takes only IDs (and other
// textual flags) on argv, so other structured kinds (Job, Envelope,
// NetPair) cannot legitimately appear.
func argToCLIText(a runtime.Arg) (string, error) {
	switch v := a.(type) {
	case runtime.WordArg:
		return v.Text, nil
	case runtime.QuotedArg:
		return v.Text, nil
	case runtime.ScalarValueArg:
		return v.Text, nil
	case runtime.AdapterArg:
		s, err := v.Value.Scalar()
		if err != nil {
			return "", fmt.Errorf("adapter %s: %w", v.Adapter, err)
		}
		return s, nil
	case runtime.StructuredValueArg:
		origin := v.Value.Origin()
		if origin != nil {
			if x, ok := origin.(bpfman.HasLinkID); ok {
				return strconv.FormatUint(uint64(x.LinkID()), 10), nil
			}
			if x, ok := origin.(bpfman.HasKernelProgramID); ok {
				return strconv.FormatUint(uint64(x.KernelProgramID()), 10), nil
			}
		}
		display := displayName(v.Name)
		return "", fmt.Errorf("%s is structured but carries no link or program ID capability", display)
	}
	return "", fmt.Errorf("unexpected argument type %T", a)
}

// hasOutputFlag reports whether argv already specifies an output
// format via -o or --output. Used to avoid clobbering an
// explicit user choice when the script invokes a CLI form that
// already passes an output format.
func hasOutputFlag(argv []string) bool {
	for _, a := range argv {
		switch {
		case a == "-o" || a == "--output":
			return true
		case strings.HasPrefix(a, "-o="):
			return true
		case strings.HasPrefix(a, "--output="):
			return true
		}
	}
	return false
}

// commandSupportsOutput reports whether the bpfman subcommand named by
// argv produces structured output worth requesting with -o json, and so
// whether the auto-injected output flag applies. The mutation verbs
// report failures on stderr and print nothing on success (decode
// discards their stdout), and image build/inspect render their own
// formats, so none of them accept or benefit from an output flag.
func commandSupportsOutput(argv []string) bool {
	if len(argv) < 2 {
		return true
	}
	switch argv[0] {
	case "image":
		return argv[1] != "build" && argv[1] != "inspect"
	case "program":
		return argv[1] != "unload" && argv[1] != "delete"
	case "link":
		return argv[1] != "detach" && argv[1] != "delete"
	}
	return true
}

// decodeBpfmanResult unmarshals stdout into the Go domain type
// that matches (noun, verb) and tags the resulting Value with
// the same OriginKind the library backend uses. Commands whose
// primary slot is void (unload, detach, delete) return an empty
// Value; the caller's rc slot still carries the envelope so
// guard / assert observe the right outcome.
//
// The dispatch table mirrors shell/semantics' bpfman bind-shape
// logic: any (noun, verb) that yields a typed Shape there must
// yield a Value with that OriginKind here. Tests that run the
// same script under both modes lock the two paths together.
func decodeBpfmanResult(args []runtime.Arg, stdout []byte) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, nil
	}
	noun := driver.ArgText(args[0])
	verb := driver.ArgText(args[1])
	switch noun {
	case "program":
		switch verb {
		case "get":
			var p bpfman.Program
			if err := json.Unmarshal(stdout, &p); err != nil {
				return runtime.Value{}, fmt.Errorf("decode Program: %w", err)
			}

			v, err := runtime.ValueFromStruct(p)
			if err != nil {
				return runtime.Value{}, err
			}
			return v.WithKind(semantics.OriginProgram), nil
		case "list":
			var result bpfman.ProgramListResult
			if err := json.Unmarshal(stdout, &result); err != nil {
				return runtime.Value{}, fmt.Errorf("decode ProgramListResult: %w", err)
			}
			return runtime.ValueFromStruct(result)
		case "load":
			var result bpfman.LoadResult
			if err := json.Unmarshal(stdout, &result); err != nil {
				return runtime.Value{}, fmt.Errorf("decode LoadResult: %w", err)
			}
			return runtime.ValueFromStruct(result)
		case "unload", "delete":
			return runtime.Value{}, nil
		}
	case "image":
		switch verb {
		case "build":
			return runtime.Value{}, nil
		case "inspect":
			var inspection oci.ImageInspection
			if err := json.Unmarshal(stdout, &inspection); err != nil {
				return runtime.Value{}, fmt.Errorf("decode ImageInspection: %w", err)
			}
			return runtime.ValueFromStruct(inspection)
		}
	case "link":
		switch verb {
		case "attach", "get":
			var l bpfman.Link
			if err := json.Unmarshal(stdout, &l); err != nil {
				return runtime.Value{}, fmt.Errorf("decode Link: %w", err)
			}

			v, err := runtime.ValueFromStruct(l)
			if err != nil {
				return runtime.Value{}, err
			}
			return v.WithKind(semantics.OriginLink), nil
		case "list":
			var result bpfman.LinkListResult
			if err := json.Unmarshal(stdout, &result); err != nil {
				return runtime.Value{}, fmt.Errorf("decode LinkListResult: %w", err)
			}
			return runtime.ValueFromStruct(result)
		case "detach", "delete":
			return runtime.Value{}, nil
		}
	case "dispatcher":
		switch verb {
		case "get":
			var snap platform.DispatcherSnapshot
			if err := json.Unmarshal(stdout, &snap); err != nil {
				return runtime.Value{}, fmt.Errorf("decode DispatcherSnapshot: %w", err)
			}
			return runtime.ValueFromStruct(snap)
		case "list":
			var result platform.DispatcherListResult
			if err := json.Unmarshal(stdout, &result); err != nil {
				return runtime.Value{}, fmt.Errorf("decode DispatcherListResult: %w", err)
			}
			return runtime.ValueFromStruct(result)
		case "delete":
			return runtime.Value{}, nil
		}
	}
	// Anything else (audit, show, ...) has no typed primary slot
	// today. The envelope from the caller's bind already carries
	// stdout/stderr/exit_code.
	return runtime.Value{}, nil
}
