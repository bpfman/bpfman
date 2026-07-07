// proginfo is a native BPF-introspection builtin, the program-side
// companion to linkinfo. It reads a kernel bpf_prog's metadata either
// by id or from a bpffs pin, through the bpf() syscall (via the
// MIT-licensed cilium/ebpf bindings), rather than parsing
// `bpftool prog show`. The returned value is statically field-checked
// under --check.
package builtins

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/cilium/ebpf"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func init() {
	Register(driver.Builtin{
		Name:     "proginfo",
		Handler:  handleProgInfo,
		Category: driver.CategoryIO,
		Usage:    "proginfo id N  |  proginfo pinned PATH",
		Summary:  "Read a kernel bpf_prog's metadata by id or pin (id, type, name, tag).",
		Detail: "proginfo opens a bpf_prog -- by kernel id or from a bpffs pin " +
			"path -- via the bpf() syscall and returns its id, type, name, and " +
			"tag. It is the program-side companion to linkinfo and replaces " +
			"parsing `bpftool prog show`. The result exposes id, type, name, " +
			"and tag, each statically field-checked. Requires CAP_BPF (or root).",
	})
}

func handleProgInfo(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) != 2 {
		return runtime.Value{}, fmt.Errorf("proginfo: usage: proginfo id N | proginfo pinned PATH")
	}
	sub := driver.ArgText(c.Args[0])
	arg := driver.ArgText(c.Args[1])

	var prog *ebpf.Program
	switch sub {
	case "id":
		id, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			return runtime.Value{}, fmt.Errorf("proginfo: id %q: %w", arg, err)
		}
		prog, err = ebpf.NewProgramFromID(ebpf.ProgramID(id))
		if err != nil {
			return runtime.Value{}, fmt.Errorf("proginfo: open program id %d: %w", id, err)
		}
	case "pinned":
		var err error
		prog, err = ebpf.LoadPinnedProgram(arg, nil)
		if err != nil {
			return runtime.Value{}, fmt.Errorf("proginfo: load pinned program %q: %w", arg, err)
		}
	default:
		return runtime.Value{}, fmt.Errorf("proginfo: unknown subcommand %q (valid: id, pinned)", sub)
	}
	defer prog.Close()

	info, err := prog.Info()
	if err != nil {
		return runtime.Value{}, fmt.Errorf("proginfo: program info: %w", err)
	}

	id, ok := info.ID()
	if !ok {
		return runtime.Value{}, fmt.Errorf("proginfo: kernel did not report a program id (kernel too old)")
	}

	mirror := map[string]any{
		"id":   json.Number(strconv.FormatUint(uint64(id), 10)),
		"type": info.Type.String(),
		"name": info.Name,
		"tag":  info.Tag,
	}
	return runtime.ValueFromMap(mirror).WithKind(semantics.OriginProgInfo), nil
}
