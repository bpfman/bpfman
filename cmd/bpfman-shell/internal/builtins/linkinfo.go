// linkinfo is a native BPF-introspection builtin. It reads a kernel
// bpf_link's metadata by id through the bpf() syscall (via the
// MIT-licensed cilium/ebpf bindings) rather than shelling out to
// bpftool. bpftool's `link show id` is unreliable -- it can exit
// non-zero while still printing the link JSON -- which forced tests to
// ignore the exit code and assert on stdout. Reading the kernel
// directly removes both the parsing and the exit-code quirk, and the
// returned value is statically field-checked under --check.
package builtins

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/cilium/ebpf/link"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func init() {
	Register(driver.Builtin{
		Name:     "linkinfo",
		Handler:  handleLinkInfo,
		Category: driver.CategoryIO,
		Usage:    "linkinfo id N",
		Summary:  "Read a kernel bpf_link's metadata by id (id, prog_id, type).",
		Detail: "linkinfo opens the bpf_link with the given kernel id via the " +
			"bpf() syscall and returns its id, attached program id, and link " +
			"type. It replaces parsing `bpftool link show id`, whose command " +
			"can exit non-zero while still printing the link. The result " +
			"exposes id, prog_id, and type, each statically field-checked. " +
			"Requires CAP_BPF (or root).",
	})
}

// linkTypeNames maps the UAPI BPF_LINK_TYPE_* enum to its short name.
// The kernel does not expose a name table to userspace and the
// cilium/ebpf enum has no stringer, so the names are kept here; they
// are UAPI facts, not derived from any GPL tool. Unknown values fall
// back to their decimal form so a newer kernel never renders blank.
var linkTypeNames = map[uint32]string{
	0:  "unspec",
	1:  "raw_tracepoint",
	2:  "tracing",
	3:  "cgroup",
	4:  "iter",
	5:  "netns",
	6:  "xdp",
	7:  "perf_event",
	8:  "kprobe_multi",
	9:  "struct_ops",
	10: "netfilter",
	11: "tcx",
	12: "uprobe_multi",
	13: "netkit",
}

func handleLinkInfo(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) != 2 || driver.ArgText(c.Args[0]) != "id" {
		return runtime.Value{}, fmt.Errorf("linkinfo: usage: linkinfo id N")
	}
	id, err := strconv.ParseUint(driver.ArgText(c.Args[1]), 10, 32)
	if err != nil {
		return runtime.Value{}, fmt.Errorf("linkinfo: id %q: %w", driver.ArgText(c.Args[1]), err)
	}

	l, err := link.NewFromID(link.ID(id))
	if err != nil {
		return runtime.Value{}, fmt.Errorf("linkinfo: open link id %d: %w", id, err)
	}
	defer l.Close()

	info, err := l.Info()
	if err != nil {
		return runtime.Value{}, fmt.Errorf("linkinfo: info for link id %d: %w", id, err)
	}

	typeName, ok := linkTypeNames[uint32(info.Type)]
	if !ok {
		typeName = strconv.FormatUint(uint64(info.Type), 10)
	}

	mirror := map[string]any{
		"id":      json.Number(strconv.FormatUint(uint64(info.ID), 10)),
		"prog_id": json.Number(strconv.FormatUint(uint64(info.Program), 10)),
		"type":    typeName,
	}
	return runtime.ValueFromMap(mirror).WithKind(semantics.OriginLinkInfo), nil
}
