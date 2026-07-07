package cliformat

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
)

func TestRenderLinkGetTable_ExposesManagedAndKernelIDs(t *testing.T) {
	t.Parallel()

	kernelLinkID := kernel.LinkID(17)
	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:           8,
			ProgramID:    42,
			KernelLinkID: &kernelLinkID,
			Kind:         bpfman.LinkKindTracepoint,
			Details:      bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
		},
	}

	var buf bytes.Buffer
	if err := RenderLinkGet(&buf, LinkGetView{Link: link}, OutputFormatText); err != nil {
		t.Fatalf("RenderLinkGet() error = %v", err)
	}
	output := buf.String()
	for _, want := range []string{"Link ID: 8", "Kernel Link ID:", "17"} {
		if !strings.Contains(output, want) {
			t.Errorf("link result table missing %q: %s", want, output)
		}
	}
}

func TestRenderLinkGetTable_ShowsMetadata(t *testing.T) {
	t.Parallel()

	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:        8,
			ProgramID: 42,
			Kind:      bpfman.LinkKindTracepoint,
			Details:   bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
			Metadata:  map[string]string{"owner": "acme"},
		},
	}

	var buf bytes.Buffer
	if err := RenderLinkGet(&buf, LinkGetView{Link: link}, OutputFormatText); err != nil {
		t.Fatalf("RenderLinkGet() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "owner=acme") {
		t.Errorf("link result table should show metadata owner=acme:\n%s", output)
	}
}

func TestRenderLinkAttachTable_PrintsLinkDetails(t *testing.T) {
	t.Parallel()

	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:        8,
			ProgramID: 42,
			Kind:      bpfman.LinkKindTC,
			Details: bpfman.TCDetails{
				Interface: "eth0",
				Direction: bpfman.TCDirectionIngress,
				Priority:  50,
			},
		},
	}

	var buf bytes.Buffer
	if err := RenderLinkAttach(&buf, link, OutputFormatText); err != nil {
		t.Fatalf("RenderLinkAttach() error = %v", err)
	}
	output := buf.String()
	for _, want := range []string{
		"Link ID: 8",
		"Spec:",
		"Status:",
		"Type:",
		"tc",
		"Interface:",
		"eth0",
		"Priority:",
		"50",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("link attach table missing %q:\n%s", want, output)
		}
	}
}

func TestRenderLinkGetTable_RendersPresentationFields(t *testing.T) {
	t.Parallel()

	link := bpfman.Link{
		Record: bpfman.LinkRecord{
			ID:        8,
			ProgramID: 42,
			Kind:      bpfman.LinkKindTC,
			Details: bpfman.TCDetails{
				Interface: "eth0",
				Direction: bpfman.TCDirectionIngress,
				Priority:  50,
			},
		},
	}

	var buf bytes.Buffer
	if err := RenderLinkGet(&buf, LinkGetView{Link: link, ProgramName: "stats"}, OutputFormatText); err != nil {
		t.Fatalf("RenderLinkGet() error = %v", err)
	}
	output := buf.String()
	for _, want := range []string{"BPF Function:", "stats", "Network Namespace:", "None"} {
		if !strings.Contains(output, want) {
			t.Errorf("link get table missing %q:\n%s", want, output)
		}
	}
}

func TestRenderDispatcherSnapshotTable_ExposesMemberManagedAndKernelIDs(t *testing.T) {
	t.Parallel()

	dispatcherKernelLinkID := kernel.LinkID(19)
	memberKernelLinkID := kernel.LinkID(23)
	snap := platform.DispatcherSnapshot{
		Key: dispatcher.Key{
			Type:    dispatcher.DispatcherTypeXDP,
			Nsid:    1,
			Ifindex: 2,
		},
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    100,
			KernelLinkID: &dispatcherKernelLinkID,
		},
		Members: []platform.DispatcherMember{
			{
				ProgramID:    42,
				ProgramName:  "xdp_pass",
				LinkID:       8,
				KernelLinkID: &memberKernelLinkID,
				Position:     0,
				Priority:     50,
				ProceedOn:    1 << 2,
			},
		},
	}

	var buf bytes.Buffer
	if err := RenderDispatcherSnapshot(&buf, snap, OutputFormatText); err != nil {
		t.Fatalf("RenderDispatcherSnapshot() error = %v", err)
	}
	output := buf.String()
	for _, want := range []string{"FUNCTION NAME", "xdp_pass", "KERNEL LINK ID", "8", "23"} {
		if !strings.Contains(output, want) {
			t.Errorf("dispatcher snapshot table missing %q: %s", want, output)
		}
	}
}

func TestRenderDispatcherSnapshot_OrdersMembersByPosition(t *testing.T) {
	t.Parallel()

	// Members supplied out of position order, as the store query may
	// return them (it orders by priority then program name). Slice
	// order here is position 2, 0, 1.
	snap := platform.DispatcherSnapshot{
		Key:      dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: 1, Ifindex: 2},
		Revision: 1,
		Runtime:  platform.DispatcherRuntime{ProgramID: 999},
		Members: []platform.DispatcherMember{
			{ProgramID: 222, ProgramName: "c", LinkID: 73, Position: 2, Priority: 80, ProceedOn: 1 << 2},
			{ProgramID: 220, ProgramName: "a", LinkID: 71, Position: 0, Priority: 50, ProceedOn: 1 << 2},
			{ProgramID: 221, ProgramName: "b", LinkID: 72, Position: 1, Priority: 55, ProceedOn: 1 << 2},
		},
	}

	// JSON: members must be emitted in ascending position order, with
	// program IDs following position rather than the input slice order.
	var jbuf bytes.Buffer
	if err := RenderDispatcherSnapshot(&jbuf, snap, OutputFormatJSON); err != nil {
		t.Fatalf("RenderDispatcherSnapshot(json) error = %v", err)
	}

	var got struct {
		Members []struct {
			Position  int `json:"position"`
			ProgramID int `json:"program_id"`
		} `json:"members"`
	}
	if err := json.Unmarshal(jbuf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}

	wantProgramIDs := []int{220, 221, 222}
	for i, m := range got.Members {
		if m.Position != i {
			t.Errorf("json member %d position = %d, want %d", i, m.Position, i)
		}
		if m.ProgramID != wantProgramIDs[i] {
			t.Errorf("json member %d program_id = %d, want %d", i, m.ProgramID, wantProgramIDs[i])
		}
	}

	// Text: program IDs appear top-to-bottom in position order.
	var tbuf bytes.Buffer
	if err := RenderDispatcherSnapshot(&tbuf, snap, OutputFormatText); err != nil {
		t.Fatalf("RenderDispatcherSnapshot(text) error = %v", err)
	}

	text := tbuf.String()
	if i220, i221, i222 := strings.Index(text, "220"), strings.Index(text, "221"), strings.Index(text, "222"); !(i220 < i221 && i221 < i222) {
		t.Errorf("text rows not in position order (220@%d, 221@%d, 222@%d):\n%s", i220, i221, i222, text)
	}

	// The caller's slice is left in its original order.
	if snap.Members[0].ProgramID != 222 {
		t.Errorf("caller's Members slice was reordered: first program_id = %d, want 222", snap.Members[0].ProgramID)
	}
}

func TestRenderDispatcherSnapshotTable_MissingMemberKernelIDUsesColumnSentinel(t *testing.T) {
	t.Parallel()

	snap := platform.DispatcherSnapshot{
		Key: dispatcher.Key{
			Type:    dispatcher.DispatcherTypeTCIngress,
			Nsid:    1,
			Ifindex: 2,
		},
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID: 100,
		},
		Members: []platform.DispatcherMember{
			{
				ProgramID:   42,
				ProgramName: "tc_pass",
				LinkID:      8,
				Position:    0,
				Priority:    50,
				ProceedOn:   1 << 0,
			},
		},
	}

	var buf bytes.Buffer
	if err := RenderDispatcherSnapshot(&buf, snap, OutputFormatText); err != nil {
		t.Fatalf("RenderDispatcherSnapshot() error = %v", err)
	}
	output := buf.String()
	for _, want := range []string{"KERNEL LINK ID", "<none>"} {
		if !strings.Contains(output, want) {
			t.Errorf("dispatcher snapshot table missing %q: %s", want, output)
		}
	}
}
