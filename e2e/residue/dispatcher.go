package residue

import (
	"fmt"
	"strings"

	"github.com/vishvananda/netlink"

	bpfnetns "github.com/bpfman/bpfman/ns/netns"
)

// DefaultBPFFS is the kernel's bpf filesystem mount point.
// Established by convention -- the kernel mounts bpffs at
// /sys/fs/bpf -- so production callers fall back to this when
// they have no layout-specific path to scan; the bpffsRoot
// parameter on the API exists so callers that know bpfman's
// runtime layout can point at {runtime-dir}/fs instead.
const DefaultBPFFS = "/sys/fs/bpf"

// tcDispatcherProgName is the program name bpfman gives its TC
// dispatcher when it loads it (platform/ebpf/attach_tc.go:100).
// The TC dispatcher attaches via classic netlink, not bpf_link,
// so it does not surface in inspect.Snapshot's Links and we
// have to find it via a per-netns clsact filter walk.
const tcDispatcherProgName = "tc_dispatcher"

// ScanTCDispatcherResidue returns a Plan of DeleteQdisc actions
// for every clsact qdisc whose filters include tc_dispatcher
// and whose interface name matches StaleTestIfaceRe, visiting
// the root netns plus every distinct named netns under
// netnsDir. A DeleteQdisc cascades both ingress and egress
// filters, so the plan emits one action per interface even
// when both directions are attached.
//
// This stays heuristic (program-name match) because classic TC
// attachments are not tracked in bpfman's store-managed link
// table; PlanFromObservation cannot see them. The interface
// name filter is what keeps the scan e2e-scoped: a tc_dispatcher
// on a production netdev was attached by something outside the
// e2e harness and is not ours to remove.
func ScanTCDispatcherResidue(netnsDir string) (Plan, error) {
	var plan Plan

	rootPlan, err := scanTCDispatcherInCurrentNS("", "")
	if err != nil {
		return plan, fmt.Errorf("scan root netns: %w", err)
	}
	plan = append(plan, rootPlan...)

	namedNS, err := listDistinctNamedNetns(netnsDir)
	if err != nil {
		return plan, fmt.Errorf("list %s: %w", netnsDir, err)
	}
	for _, ns := range namedNS {
		var nsPlan Plan
		runErr := bpfnetns.Run(ns.Path, func() error {
			inside, scanErr := scanTCDispatcherInCurrentNS(ns.Path, ns.Name)
			nsPlan = inside
			return scanErr
		})
		if runErr != nil {
			// One bad netns shouldn't sink the rest.
			continue
		}
		plan = append(plan, nsPlan...)
	}
	return plan, nil
}

// scanTCDispatcherInCurrentNS lists every interface in the
// current netns, queries each one's clsact filters on both
// ingress and egress parents, and returns a Plan of
// DeleteQdisc actions for the interfaces carrying a
// tc_dispatcher filter.
func scanTCDispatcherInCurrentNS(netnsPath, netnsName string) (Plan, error) {
	var plan Plan

	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	parents := []uint32{netlink.HANDLE_MIN_INGRESS, netlink.HANDLE_MIN_EGRESS}
	for _, l := range links {
		if !StaleTestIfaceRe.MatchString(l.Attrs().Name) {
			continue
		}
		hasDispatcher := false
		for _, parent := range parents {
			filters, ferr := netlink.FilterList(l, parent)
			if ferr != nil {
				continue
			}
			for _, f := range filters {
				bf, ok := f.(*netlink.BpfFilter)
				if !ok {
					continue
				}
				if bf.Name == tcDispatcherProgName ||
					strings.HasPrefix(bf.Name, tcDispatcherProgName+":") {
					hasDispatcher = true
					break
				}
			}
			if hasDispatcher {
				break
			}
		}
		if hasDispatcher {
			plan = append(plan, DeleteQdisc{
				NetnsPath: netnsPath,
				NetnsName: netnsName,
				IfName:    l.Attrs().Name,
			})
		}
	}
	return plan, nil
}
