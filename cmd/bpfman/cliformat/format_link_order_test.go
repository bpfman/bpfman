package cliformat

import (
	"strings"
	"testing"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// Detail rows are ordered by label, not by the rendered line, so a label
// that is a prefix of another sorts by name. The old code sorted the
// formatted "    Target Function:\t..." strings, which put Target Function
// above Target because a space sorts before a colon.
func TestFormatLinkTable_OrdersByLabelNotFormattedLine(t *testing.T) {
	t.Parallel()

	kid := kernel.LinkID(17)
	link := bpfman.Link{Record: bpfman.LinkRecord{
		ID:           8,
		ProgramID:    42,
		KernelLinkID: &kid,
		Kind:         bpfman.LinkKindUprobe,
		Details:      bpfman.UprobeDetails{Target: "/usr/lib/libc.so.6", FnName: "malloc", Offset: 16},
	}}

	out := formatLinkTable(LinkGetView{Link: link})
	target := strings.Index(out, "Target:")
	targetFn := strings.Index(out, "Target Function:")
	targetOff := strings.Index(out, "Target Offset:")
	if target < 0 || targetFn < 0 || targetOff < 0 {
		t.Fatalf("missing Target rows in:\n%s", out)
	}
	if !(target < targetFn && targetFn < targetOff) {
		t.Errorf("want Target < Target Function < Target Offset; got offsets %d, %d, %d:\n%s", target, targetFn, targetOff, out)
	}
}
