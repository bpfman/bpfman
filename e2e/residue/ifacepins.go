package residue

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/vishvananda/netlink"

	bpfnetns "github.com/bpfman/bpfman/ns/netns"
)

// ifaceLinkPinIndex maps bpf_link pins attached to interfaces
// (XDP and TCX) by the program ID they wrap. We index by
// program ID because that's the join key the per-interface
// queries return: netlink IFLA_XDP carries a prog ID, and
// link.QueryPrograms for TCX returns a list of prog IDs.
//
// Built once per scan because the bpf fs is global -- entering
// a netns does not change which pins are visible -- so we walk
// the tree once and then re-use the index inside every netns.
type ifaceLinkPinIndex struct {
	byProgID map[ebpf.ProgramID][]string
}

func newIfaceLinkPinIndex() *ifaceLinkPinIndex {
	return &ifaceLinkPinIndex{byProgID: map[ebpf.ProgramID][]string{}}
}

// buildIfaceLinkPinIndex walks bpffsRoot, loads each candidate
// pin as a bpf_link, and records the XDP and TCX entries by
// their program ID. Non-link pins (programs, maps) and stale
// pin entries that fail to load are skipped silently.
func buildIfaceLinkPinIndex(bpffsRoot string) (*ifaceLinkPinIndex, error) {
	idx := newIfaceLinkPinIndex()
	err := filepath.WalkDir(bpffsRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		lnk, lerr := link.LoadPinnedLink(path, nil)
		if lerr != nil {
			return nil
		}
		info, ierr := lnk.Info()
		lnk.Close()
		if ierr != nil {
			return nil
		}
		switch info.Type {
		case link.XDPType, link.TCXType:
			idx.byProgID[info.Program] = append(idx.byProgID[info.Program], path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return idx, err
	}
	return idx, nil
}

// findLinkPinsOnTestInterfaces returns the pin paths of every
// XDP or TCX bpf_link attached to an interface whose name
// matches StaleTestIfaceRe. The scan visits the root netns and
// every named netns under netnsDir whose name matches the same
// regex (the e2e harness uses the same name for both, so a
// netns we would not have deleted holds no interfaces we would
// have touched either).
//
// For each interface in scope, the XDP attachment is read from
// netlink IFLA_XDP and the TCX ingress/egress attachments are
// queried via bpf_prog_query. Each prog ID returned is looked
// up in the pin index built once at the top of the scan.
func findLinkPinsOnTestInterfaces(bpffsRoot, netnsDir string) ([]string, error) {
	idx, err := buildIfaceLinkPinIndex(bpffsRoot)
	if err != nil {
		return nil, fmt.Errorf("index iface link pins: %w", err)
	}

	var pins []string
	seen := map[string]bool{}

	add := func(progID ebpf.ProgramID) {
		for _, pin := range idx.byProgID[progID] {
			if !seen[pin] {
				seen[pin] = true
				pins = append(pins, pin)
			}
		}
	}

	collect := func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("list interfaces: %w", err)
		}
		for _, l := range links {
			attrs := l.Attrs()
			if !StaleTestIfaceRe.MatchString(attrs.Name) {
				continue
			}
			if attrs.Xdp != nil && attrs.Xdp.Attached {
				add(ebpf.ProgramID(attrs.Xdp.ProgId))
			}
			for _, attach := range []ebpf.AttachType{ebpf.AttachTCXIngress, ebpf.AttachTCXEgress} {
				res, qerr := link.QueryPrograms(link.QueryOptions{
					Target: attrs.Index,
					Attach: attach,
				})
				if qerr != nil {
					// Kernel without TCX support, no
					// attachments, or transient error --
					// none is fatal to the overall scan.
					continue
				}
				for _, ap := range res.Programs {
					add(ap.ID)
				}
			}
		}
		return nil
	}

	if err := collect(); err != nil {
		return pins, fmt.Errorf("scan root netns: %w", err)
	}

	namedNS, err := listDistinctNamedNetns(netnsDir)
	if err != nil {
		return pins, fmt.Errorf("list %s: %w", netnsDir, err)
	}
	for _, ns := range namedNS {
		if !StaleTestIfaceRe.MatchString(ns.Name) {
			continue
		}
		// A failed setns on one netns does not invalidate the
		// others; record nothing for that netns and move on.
		_ = bpfnetns.Run(ns.Path, func() error {
			return collect()
		})
	}
	return pins, nil
}
