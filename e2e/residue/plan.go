package residue

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/cilium/ebpf/link"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/bpfman/bpfman/internal/testnetroute"
	bpfnetns "github.com/bpfman/bpfman/ns/netns"
)

// Action is one cleanup step. Describe returns the shell-shaped
// line shown in dry-run output (so a reader can audit it and, in
// principle, run it by hand); Apply executes the same step
// against the live system. Action values are pure data --
// describing them allocates no resources -- so Plan can be built,
// dedup'd, and reordered without touching the kernel.
type Action interface {
	// Describe returns the shell-shaped line shown in dry-run
	// output: an audit-friendly rendering a reader could, in
	// principle, run by hand. Pure; allocates no resources.
	Describe() string

	// Apply executes the step against the live system and returns
	// any error it produced.
	Apply() error
}

// Plan is an ordered list of Actions. Order is load-bearing: pin
// removal must precede netdev deletion (otherwise the kernel
// leaves a detached link object pinned), and netdev deletion
// must precede netns deletion (otherwise we cascade twice).
// Scanners produce a Plan already in the right order; callers
// assembling a composite plan append in order and rely on each
// scanner's own ordering.
type Plan []Action

// Describe writes one line per action to w. Pure.
func (p Plan) Describe(w io.Writer) {
	for _, a := range p {
		fmt.Fprintln(w, a.Describe())
	}
}

// ActionFailure pairs an Action with the error its Apply
// returned. Apply collects one of these per failing action and
// hands them all back to the caller, which is the right place
// to decide how to render them -- callers running interactively
// want one line per failure with the action's Describe() as the
// preamble.
type ActionFailure struct {
	// Action is the cleanup step whose Apply returned an error.
	Action Action

	// Err is the error that Action's Apply returned.
	Err error
}

// Apply executes every action. Per-entry errors are accumulated
// so a single bad step does not block the rest -- the cleanup
// tool's purpose is to drain as much as possible on each run.
// Returns the per-action failures in execution order; an empty
// slice means every action succeeded.
func (p Plan) Apply() []ActionFailure {
	var failures []ActionFailure
	for _, a := range p {
		if err := a.Apply(); err != nil {
			failures = append(failures, ActionFailure{Action: a, Err: err})
		}
	}
	return failures
}

// Empty reports whether the plan carries no actions.
func (p Plan) Empty() bool { return len(p) == 0 }

// Dedup removes duplicate RemovePin actions (same Path), keeping
// the first occurrence and preserving order. Other action kinds
// are passed through unchanged; their identity is implicit in
// the kernel object they target and the scanners that produce
// them do not generate duplicates in practice.
func (p Plan) Dedup() Plan {
	if len(p) <= 1 {
		return p
	}
	seenPin := map[string]bool{}
	out := make(Plan, 0, len(p))
	for _, a := range p {
		if rp, ok := a.(RemovePin); ok {
			if seenPin[rp.Path] {
				continue
			}
			seenPin[rp.Path] = true
		}
		out = append(out, a)
	}
	return out
}

// RemoveTree recursively removes a directory tree. Used by the
// --wipe path to clear bpfman's runtime FS subtree wholesale,
// ignoring DB ownership. Missing trees are not an error.
type RemoveTree struct {
	// Path is the root of the directory tree to remove
	// recursively; a missing tree is not an error.
	Path string
}

// Describe implements Action.
func (a RemoveTree) Describe() string { return fmt.Sprintf("rm -rf -- %s", a.Path) }

// Apply implements Action.
func (a RemoveTree) Apply() error {
	if err := os.RemoveAll(a.Path); err != nil {
		return fmt.Errorf("remove tree %s: %w", a.Path, err)
	}
	return nil
}

// UnmountBPFFS unmounts the bpffs at Path. Used by the --wipe
// path so the subsequent RemoveTree on the runtime root can also
// drain the mount point directory. Already-unmounted paths
// surface as an Apply failure rather than a silent success
// because a stale plan entry is worth flagging.
type UnmountBPFFS struct {
	// Path is the bpffs mount point to unmount.
	Path string
}

// Describe implements Action.
func (a UnmountBPFFS) Describe() string { return fmt.Sprintf("umount -- %s", a.Path) }

// Apply implements Action.
func (a UnmountBPFFS) Apply() error {
	if err := syscall.Unmount(a.Path, 0); err != nil {
		return fmt.Errorf("unmount %s: %w", a.Path, err)
	}
	return nil
}

// RemovePin unlinks a pin file under the bpf fs. Removing the
// last reference to a bpf_link makes the kernel detach the link
// and GC the program; for program / map pins the kernel GCs the
// object once no FD or other pin keeps it alive.
type RemovePin struct {
	// Path is the pin file under the bpf fs to unlink.
	Path string
}

// Describe implements Action.
func (a RemovePin) Describe() string { return fmt.Sprintf("rm -f -- %s", a.Path) }

// Apply implements Action.
func (a RemovePin) Apply() error {
	if err := os.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", a.Path, err)
	}
	return nil
}

// DetachLink closes a bpf_link by ID. Used for kernel-orphan
// links not backed by any pin -- closing the only FD makes the
// kernel detach. If the link is already gone (someone else
// closed it between scan and apply), Apply succeeds quietly.
type DetachLink struct {
	// ID identifies the bpf_link to close by ID.
	ID link.ID
}

// Describe implements Action. The shell-shaped output uses
// bpftool because that is the closest hand-runnable equivalent;
// Apply does the same work via the bpf_link API directly.
func (a DetachLink) Describe() string { return fmt.Sprintf("bpftool link detach id %d", a.ID) }

// Apply implements Action.
func (a DetachLink) Apply() error {
	lnk, err := link.NewFromID(a.ID)
	if err != nil {
		// Already gone, not an error worth surfacing.
		return nil
	}
	if err := lnk.Close(); err != nil {
		return fmt.Errorf("detach link %d: %w", a.ID, err)
	}
	return nil
}

// DeleteQdisc removes a clsact qdisc from an interface,
// cascading the ingress and egress filters it carries. NetnsPath
// empty selects the current netns; non-empty enters via setns.
// NetnsName is only used by Describe.
type DeleteQdisc struct {
	// NetnsPath is the netns to enter before deleting the qdisc;
	// empty selects the current netns, non-empty enters via setns.
	NetnsPath string

	// NetnsName is the netns name used only by Describe's output.
	NetnsName string

	// IfName is the interface whose clsact qdisc is removed.
	IfName string
}

// Describe implements Action.
func (a DeleteQdisc) Describe() string {
	if a.NetnsName == "" {
		return fmt.Sprintf("tc qdisc del dev %s clsact", a.IfName)
	}
	return fmt.Sprintf("ip netns exec %s tc qdisc del dev %s clsact", a.NetnsName, a.IfName)
}

// Apply implements Action.
func (a DeleteQdisc) Apply() error {
	work := func() error {
		l, err := netlink.LinkByName(a.IfName)
		if err != nil {
			return fmt.Errorf("look up %s: %w", a.IfName, err)
		}
		qdiscs, err := netlink.QdiscList(l)
		if err != nil {
			return fmt.Errorf("list qdiscs on %s: %w", a.IfName, err)
		}
		for _, q := range qdiscs {
			if q.Type() != "clsact" {
				continue
			}
			if err := netlink.QdiscDel(q); err != nil {
				return fmt.Errorf("delete clsact on %s: %w", a.IfName, err)
			}
			return nil
		}
		return nil // already gone
	}
	if a.NetnsPath == "" {
		return work()
	}
	if err := bpfnetns.Run(a.NetnsPath, work); err != nil {
		return fmt.Errorf("netns %s: %w", a.NetnsName, err)
	}
	return nil
}

// DeleteIface removes a network interface from the current
// netns. Veth peers cascade automatically: deleting the host
// end of an `Na` / `Nb` pair tears down the peer wherever it
// lives.
type DeleteIface struct {
	// Name is the interface to delete from the current netns;
	// deleting one end of a veth pair cascades to its peer.
	Name string
}

// Describe implements Action.
func (a DeleteIface) Describe() string { return fmt.Sprintf("ip link del %s", a.Name) }

// Apply implements Action.
func (a DeleteIface) Apply() error {
	l, err := netlink.LinkByName(a.Name)
	if err != nil {
		// Already gone counts as done: the scan emits both ends
		// of a veth pair and deleting either end cascades to the
		// other, so the second delete routinely finds nothing.
		var notFound netlink.LinkNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("look up %s: %w", a.Name, err)
	}
	if err := netlink.LinkDel(l); err != nil {
		return fmt.Errorf("delete %s: %w", a.Name, err)
	}
	return nil
}

// DeleteNetns removes a named netns marker file. Dir defaults
// to DefaultNetnsDir when empty so production callers can keep
// using the iproute2 convention without naming it; the scanner
// fills Dir in explicitly so tests can point at a temp tree.
type DeleteNetns struct {
	// Name is the named netns marker file to remove.
	Name string

	// Dir is the directory holding the marker file; empty
	// defaults to DefaultNetnsDir.
	Dir string
}

// Describe implements Action.
func (a DeleteNetns) Describe() string { return fmt.Sprintf("ip netns del %s", a.Name) }

// Apply implements Action.
//
// The vendored vishvananda/netns DeleteNamed helper runs
// unix.Unmount(path, MNT_DETACH) and then os.Remove(path). When
// /run/netns/X exists as a plain file rather than a live bind-
// mount (reboot, OOM kill, manual umount that left the marker
// behind), the unmount step returns EINVAL ("not a mount
// point") and the helper bails before the unlink, so the
// marker file lingers forever and every subsequent --apply
// fails on the same name. Run the two steps inline here with
// the unmount treated as best-effort: EINVAL covers the
// non-mount case under sudo, EPERM covers the same case under
// an unprivileged caller (the capability check fires before
// the mount-status check). The unlink is the load-bearing
// operation that turns the entry from "discoverable by the
// scanner" into "gone" and is the authoritative signal for
// real permission failures.
func (a DeleteNetns) Apply() error {
	dir := a.Dir
	if dir == "" {
		dir = DefaultNetnsDir
	}
	path := filepath.Join(dir, a.Name)
	if err := unix.Unmount(path, unix.MNT_DETACH); err != nil &&
		!errors.Is(err, unix.EINVAL) &&
		!errors.Is(err, unix.EPERM) {
		return fmt.Errorf("unmount netns %s: %w", a.Name, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete netns %s: %w", a.Name, err)
	}
	return nil
}

// DeleteTestNetRule removes one harness policy-routing rule that
// directs TEST-NET-2 lookups to the main table. The scan matches by
// destination and table at any preference, so rules installed with
// a custom BPFMAN_E2E_POLICY_RULE_PREF are swept too.
type DeleteTestNetRule struct {
	// Pref is the preference of the policy-routing rule to remove.
	Pref int
}

// Describe implements Action.
func (a DeleteTestNetRule) Describe() string {
	return fmt.Sprintf("ip rule del pref %d to %s lookup main", a.Pref, testnetroute.CIDR)
}

// Apply implements Action. Absence counts as done: a parallel
// sweep or manual removal may have raced this plan.
func (a DeleteTestNetRule) Apply() error {
	installed, err := testnetroute.Installed()
	if err != nil {
		return err
	}
	for _, r := range installed {
		if r.Priority != a.Pref {
			continue
		}
		del := r
		if err := netlink.RuleDel(&del); err != nil {
			return fmt.Errorf("delete test-net rule (pref %d): %w", a.Pref, err)
		}
	}
	return nil
}
