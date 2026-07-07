package bpfmanbuiltin

import (
	"context"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	parent "github.com/bpfman/bpfman/cmd/bpfman-shell/internal/builtins"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

var topLevelNouns = map[string]bool{
	"program":    true,
	"show":       true,
	"image":      true,
	"link":       true,
	"dispatcher": true,
	"audit":      true,
}

// HelpDetail is the multi-paragraph long help listing every bpfman
// subcommand, registered as the bpfman builtin's Detail text.
const HelpDetail = `Subcommands:

  Program management:
    bpfman program list [flags]                         List BPF programs (--all includes unmanaged)
    bpfman program get <id>                             Get program details (assignable)
    bpfman program load file <path> [flags]             Load from a local object file (assignable)
    bpfman program load image <image> [flags]           Load from an OCI image (assignable)
    bpfman program unload <ids>                         Unload programs
    bpfman program delete (<ids> | --all) [-r]          Delete with cascading cleanup
    bpfman show program <id> [view] [-o]                Inspect (views: links, maps, paths)

  Image management:
    bpfman image build <image> <bytecode> [flags]       Build and publish a bytecode image
    bpfman image inspect <image>                        Inspect bytecode image metadata

  Link management:
    bpfman link attach xdp <id> <iface> --priority <prio> [flags]         Attach an XDP program (assignable)
    bpfman link attach tc <id> <iface> <dir> --priority <prio> [flags]    Attach a TC program (assignable)
    bpfman link attach tcx <id> <iface> <dir> --priority <prio> [flags]   Attach a TCX program (assignable)
    bpfman link attach tracepoint <id> <group/name>     Attach a tracepoint program (assignable)
    bpfman link attach kprobe <id> <fn> [flags]         Attach a kprobe program (assignable)
    bpfman link attach uprobe <id> <target> [flags]     Attach a uprobe program (assignable)
    bpfman link detach <link-ids>                       Detach links
    bpfman link get <link-id>                           Get link details (assignable)
    bpfman link list [flags]                            List managed links
    bpfman link delete <link-ids> [-r]                  Delete with cascading cleanup

  Dispatcher management:
    bpfman dispatcher list [--type <type>] [--nsid <nsid>] [--ifindex <ifindex>]  List dispatchers
    bpfman dispatcher get <type> <nsid> <ifindex>    Get dispatcher details
    bpfman dispatcher delete <type> <nsid> <ifindex> Delete a dispatcher

  Diagnostics:
    bpfman audit [rules]                            Audit coherency (read-only)
    bpfman audit explain [rule]                     Explain a coherency rule`

func init() {
	parent.Register(driver.Builtin{
		Name:     "bpfman",
		Handler:  Handle,
		Category: driver.CategoryIO,
		Usage:    "bpfman <subcommand> ...",
		Summary:  "Run bpfman program, link, dispatcher, and audit subcommands.",
		Detail:   HelpDetail,
	})
}

// IsTopLevelNoun reports whether name is a bpfman domain noun
// (program, show, image, link, dispatcher, audit) that must follow a
// "bpfman" prefix. The statement and bind fallbacks use it to emit the
// "domain commands require a bpfman prefix" diagnostic.
func IsTopLevelNoun(name string) bool {
	return topLevelNouns[name]
}

// Handle runs a "bpfman <subcommand> ..." invocation and returns the
// subcommand's assignable value. A dispatch failure is wrapped as a
// *driver.RuntimeError carrying the call site's span so the renderer
// cites the source line.
func Handle(c driver.Ctx) (runtime.Value, error) {
	val, err := dispatch(c.Ctx, c.Args)
	if err != nil {
		return runtime.Value{}, &driver.RuntimeError{Msg: err.Error(), Span: c.Span}
	}
	return val, nil
}

func dispatch(ctx context.Context, args []runtime.Arg) (runtime.Value, error) {
	var err error
	args, err = maybeBrokerLoadFileArgs(ctx, args)
	if err != nil {
		return runtime.Value{}, err
	}

	args, err = resolveE2EImageRefsInArgs(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return dispatchCommandExternal(ctx, args)
}
