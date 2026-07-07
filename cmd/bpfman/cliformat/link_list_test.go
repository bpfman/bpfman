package cliformat

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// tableGap matches the run of padding tabwriter inserts between columns
// (always two or more spaces), so a header like "LINK ID" stays one cell.
var tableGap = regexp.MustCompile(`\s{2,}`)

// linkListCell renders one link and returns the value under the named
// column, so a test asserts a cell by what it means rather than by a bare
// substring. It requires a header and exactly one data row, and that the
// fixture leaves no empty cells (an empty cell would collapse under the
// gap split and shift the columns).
func linkListCell(t *testing.T, link bpfman.LinkRecord, column string) string {
	t.Helper()

	var buf bytes.Buffer
	if err := RenderLinkList(&buf, LinkListView{Links: []bpfman.LinkRecord{link}}, OutputFormatText); err != nil {
		t.Fatalf("RenderLinkList() error = %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want a header and one row, got %d lines:\n%s", len(lines), buf.String())
	}

	headers := tableGap.Split(strings.TrimRight(lines[0], " "), -1)
	cells := tableGap.Split(strings.TrimRight(lines[1], " "), -1)
	if len(headers) != len(cells) {
		t.Fatalf("header/row column mismatch (%d vs %d):\n%s", len(headers), len(cells), buf.String())
	}

	for i, h := range headers {
		if h == column {
			return cells[i]
		}
	}
	t.Fatalf("no column %q in header %q", column, lines[0])
	return ""
}

// The list surfaces the bpfman-managed link ID and the captured kernel
// link ID as separate columns, so a user can correlate the two; a
// regression that dropped or conflated either would land the wrong value
// under the column.
func TestRenderLinkList_ManagedAndKernelIDsInTheirColumns(t *testing.T) {
	t.Parallel()

	kernelLinkID := kernel.LinkID(17)
	link := bpfman.LinkRecord{
		ID:           8,
		ProgramID:    42,
		KernelLinkID: &kernelLinkID,
		Kind:         bpfman.LinkKindTracepoint,
	}

	if got := linkListCell(t, link, "LINK ID"); got != "8" {
		t.Errorf("LINK ID column = %q, want %q", got, "8")
	}
	if got := linkListCell(t, link, "KERNEL LINK ID"); got != "17" {
		t.Errorf("KERNEL LINK ID column = %q, want %q", got, "17")
	}
}

// A link bpfman never captured a kernel ID for shows the sentinel in the
// kernel-ID column, not a blank that reads as missing data nor a zero
// that reads as a real kernel ID.
func TestRenderLinkList_NilKernelIDShowsSentinel(t *testing.T) {
	t.Parallel()

	link := bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindXDP}
	if got := linkListCell(t, link, "KERNEL LINK ID"); got != "<none>" {
		t.Errorf("KERNEL LINK ID column with no captured kernel ID = %q, want %q", got, "<none>")
	}
}

// The link list resolves each link's owning program to an APPLICATION
// and FUNCTION NAME column, so the listing reads which program (by name)
// and application a link belongs to without cross-referencing the
// program list.
func TestRenderLinkList_ProgramColumns(t *testing.T) {
	t.Parallel()

	links := []bpfman.LinkRecord{
		{ID: 1, ProgramID: 42, Kind: bpfman.LinkKindXDP, Details: bpfman.XDPDetails{Interface: "eth0", Position: 0}},
		{ID: 2, ProgramID: 43, Kind: bpfman.LinkKindTC, Details: bpfman.TCDetails{Interface: "eth0", Direction: bpfman.TCDirectionIngress, Position: 0}},
	}
	refs := map[kernel.ProgramID]LinkProgramRef{
		42: {Application: "demo-app", FunctionName: "pass"},
		43: {FunctionName: "stats"},
	}

	var buf bytes.Buffer
	if err := RenderLinkList(&buf, LinkListView{Links: links, Programs: refs}, OutputFormatText); err != nil {
		t.Fatalf("RenderLinkList() error = %v", err)
	}

	out := buf.String()
	for _, want := range []string{"APPLICATION", "FUNCTION NAME", "demo-app", "pass", "stats"} {
		if !strings.Contains(out, want) {
			t.Errorf("link list missing %q:\n%s", want, out)
		}
	}
}

// When no listed link's program carries an application label, the
// APPLICATION column is omitted rather than rendered as a blank stripe,
// mirroring the program list; FUNCTION NAME is always shown.
func TestRenderLinkList_ApplicationElidedWhenUnlabelled(t *testing.T) {
	t.Parallel()

	links := []bpfman.LinkRecord{
		{ID: 1, ProgramID: 43, Kind: bpfman.LinkKindTC, Details: bpfman.TCDetails{Interface: "eth0", Direction: bpfman.TCDirectionIngress, Position: 0}},
	}
	refs := map[kernel.ProgramID]LinkProgramRef{43: {FunctionName: "stats"}}

	var buf bytes.Buffer
	if err := RenderLinkList(&buf, LinkListView{Links: links, Programs: refs}, OutputFormatText); err != nil {
		t.Fatalf("RenderLinkList() error = %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "APPLICATION") {
		t.Errorf("APPLICATION column should be elided when unlabelled:\n%s", out)
	}
	if !strings.Contains(out, "FUNCTION NAME") || !strings.Contains(out, "stats") {
		t.Errorf("FUNCTION NAME column should always show:\n%s", out)
	}
}

// The ATTACHMENT column summarises where each link is attached from its
// typed details, so the listing answers "attached to what?" without
// decoding the pin path or running link get per row.
func TestRenderLinkList_AttachmentSummaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		link bpfman.LinkRecord
		want string
	}{
		{
			name: "xdp",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindXDP, Details: bpfman.XDPDetails{Interface: "eth0", Position: 2}},
			want: "eth0 pos-2",
		},
		{
			name: "xdp in a network namespace",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindXDP, Details: bpfman.XDPDetails{Interface: "eth0", Position: 0, Netns: "/var/run/netns/blue"}},
			want: "eth0 pos-0 netns=/var/run/netns/blue",
		},
		{
			name: "tc",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindTC, Details: bpfman.TCDetails{Interface: "eth0", Direction: bpfman.TCDirectionIngress, Position: 0}},
			want: "eth0 ingress pos-0",
		},
		{
			name: "tcx",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindTCX, Details: bpfman.TCXDetails{Interface: "eth0", Direction: bpfman.TCDirectionEgress, Position: 1}},
			want: "eth0 egress pos-1",
		},
		{
			name: "tracepoint",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindTracepoint, Details: bpfman.TracepointDetails{Group: "syscalls", Name: "sys_enter_kill"}},
			want: "syscalls/sys_enter_kill",
		},
		{
			name: "kprobe",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindKprobe, Details: bpfman.KprobeDetails{FnName: "do_unlinkat"}},
			want: "do_unlinkat",
		},
		{
			name: "kprobe with offset",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindKprobe, Details: bpfman.KprobeDetails{FnName: "do_unlinkat", Offset: 8}},
			want: "do_unlinkat+8",
		},
		{
			name: "uprobe",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindUprobe, Details: bpfman.UprobeDetails{Target: "/usr/lib/libc.so.6", FnName: "malloc"}},
			want: "/usr/lib/libc.so.6 malloc",
		},
		{
			name: "uprobe by offset",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindUprobe, Details: bpfman.UprobeDetails{Target: "/usr/lib/libc.so.6", Offset: 4096}},
			want: "/usr/lib/libc.so.6 +4096",
		},
		{
			name: "fentry",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindFentry, Details: bpfman.FentryDetails{FnName: "do_unlinkat"}},
			want: "do_unlinkat",
		},
		{
			name: "no details",
			link: bpfman.LinkRecord{ID: 1, Kind: bpfman.LinkKindXDP},
			want: "<none>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := linkListCell(t, tt.link, "ATTACHMENT"); got != tt.want {
				t.Errorf("ATTACHMENT column = %q, want %q", got, tt.want)
			}
		})
	}
}
