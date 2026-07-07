package residue

import (
	"github.com/cilium/ebpf/link"

	"github.com/bpfman/bpfman/inspect"
	"github.com/bpfman/bpfman/kernel"
)

// PlanFromObservation translates an inspect.Observation into a
// Plan that removes bpfman residue: bpf fs pins and kernel
// bpf_links that bpfman created but no longer tracks in its
// store.
//
// A kernel bpf_link not in the store is not bpfman residue
// just because it isn't in the store -- it might belong to
// systemd, a Cilium agent, an ad-hoc bpftool session, or any
// other tenant of the kernel's bpf_link table. We classify a
// kernel-only link as bpfman residue only when it wraps a
// program bpfman has a record of (either in the store or
// pinned in bpfman's runtime FS). Anything else is left alone.
//
// FS-only program pins are different: bpfman's scanner only
// walks bpfman's own runtime FS subtree, so an FS-present
// program is definitively bpfman's. An orphan there is safe to
// remove without further checks.
//
// Store-managed objects are left untouched even when the kernel
// or filesystem view is partial -- partial visibility is an
// inconsistency for the next bpfman invocation to reconcile,
// not residue for us to clean.
func PlanFromObservation(obs *inspect.Observation) Plan {
	var plan Plan

	// Build the set of program IDs bpfman is associated with:
	// anything it has either a store record for or a pin under
	// its runtime FS. Used to qualify the kernel-only link
	// scan below.
	bpfmanProgs := map[kernel.ProgramID]bool{}
	for _, p := range obs.Programs {
		if p.Presence.InStore || p.Presence.InFS {
			bpfmanProgs[p.ProgramID] = true
		}
	}

	// FS-only program pins: a pin file in bpfman's runtime FS
	// with no DB record and no live kernel program. Drop the
	// pin; nothing else holds the program alive so the kernel
	// has already GC'd it.
	for _, p := range obs.Programs {
		if p.Presence.OrphanFS() && p.FSPinPath != "" {
			plan = append(plan, RemovePin{Path: p.FSPinPath})
		}
	}

	// Kernel-only links wrapping a bpfman-associated program:
	// bpfman knows the program but lost the link record. The
	// link is bpfman's to detach. Other kernel-only links --
	// belonging to systemd, host BPF tools, or any other
	// tenant of the kernel's bpf_link table -- are out of
	// scope and not touched.
	//
	// When the link has a known pin (FSPinPath set by the
	// snapshot's bpf fs walk), removing the pin drops the last
	// reference and the kernel detaches the link. When no pin
	// is known the link is held only by FDs, and DetachLink's
	// close-by-id is the right move. The pin path branch
	// matters because close-FD on a pinned link does nothing:
	// the pin still holds a reference, the link survives, and
	// the next dry-run lists the same id again.
	for _, l := range obs.Links {
		if l.Presence.InStore {
			continue
		}
		if !l.Presence.InKernel || l.Kernel == nil {
			continue
		}
		if !bpfmanProgs[l.Kernel.ProgramID] {
			continue
		}
		if l.FSPinPath != "" {
			plan = append(plan, RemovePin{Path: l.FSPinPath})
			continue
		}
		plan = append(plan, DetachLink{ID: link.ID(l.Kernel.ID)})
	}

	return plan
}
