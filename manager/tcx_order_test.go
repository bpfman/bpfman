package manager

import (
	"testing"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

func TestComputeTCXAttachOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		existingLinks []bpfman.TCXLinkInfo
		newPriority   int32
		wantFirst     bool
		wantBefore    kernel.ProgramID
		wantAfter     kernel.ProgramID
	}{
		{
			name:          "empty chain - attach at head",
			existingLinks: nil,
			newPriority:   50,
			wantFirst:     true,
		},
		{
			name: "lowest priority - attach at head (before all)",
			existingLinks: []bpfman.TCXLinkInfo{
				{KernelLinkID: 1, KernelProgramID: 100, Priority: 100},
				{KernelLinkID: 2, KernelProgramID: 200, Priority: 200},
			},
			newPriority: 50,
			wantBefore:  100, // before program with priority 100
		},
		{
			name: "highest priority - attach at tail (after all)",
			existingLinks: []bpfman.TCXLinkInfo{
				{KernelLinkID: 1, KernelProgramID: 100, Priority: 100},
				{KernelLinkID: 2, KernelProgramID: 200, Priority: 200},
			},
			newPriority: 300,
			wantAfter:   200, // after program with priority 200
		},
		{
			name: "middle priority - attach before higher priority",
			existingLinks: []bpfman.TCXLinkInfo{
				{KernelLinkID: 1, KernelProgramID: 100, Priority: 100},
				{KernelLinkID: 2, KernelProgramID: 300, Priority: 300},
			},
			newPriority: 200,
			wantBefore:  300, // before program with priority 300
		},
		{
			name: "equal priority - attach after existing (FIFO for ties)",
			existingLinks: []bpfman.TCXLinkInfo{
				{KernelLinkID: 1, KernelProgramID: 100, Priority: 100},
				{KernelLinkID: 2, KernelProgramID: 200, Priority: 200},
			},
			newPriority: 200,
			wantAfter:   200, // after existing program with same priority
		},
		{
			name: "single existing link - lower priority inserts before",
			existingLinks: []bpfman.TCXLinkInfo{
				{KernelLinkID: 1, KernelProgramID: 100, Priority: 100},
			},
			newPriority: 50,
			wantBefore:  100,
		},
		{
			name: "single existing link - higher priority inserts after",
			existingLinks: []bpfman.TCXLinkInfo{
				{KernelLinkID: 1, KernelProgramID: 100, Priority: 100},
			},
			newPriority: 200,
			wantAfter:   100,
		},
		{
			name: "TC dispatcher scenario - TCX with priority 500 after dispatcher with priority 50",
			existingLinks: []bpfman.TCXLinkInfo{
				{KernelLinkID: 1, KernelProgramID: 1000, Priority: 50}, // TC dispatcher
			},
			newPriority: 500, // TCX user program
			wantAfter:   1000,
		},
		{
			name: "TCX before TC dispatcher - lower priority runs first",
			existingLinks: []bpfman.TCXLinkInfo{
				{KernelLinkID: 1, KernelProgramID: 1000, Priority: 50}, // TC dispatcher
			},
			newPriority: 25, // TCX user program with lower priority
			wantBefore:  1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := computeTCXAttachOrder(tt.existingLinks, tt.newPriority)

			if tt.wantFirst && !got.First {
				t.Errorf("expected First=true, got %+v", got)
			}
			if tt.wantBefore != 0 && got.BeforeProgID != tt.wantBefore {
				t.Errorf("expected BeforeProgID=%d, got %+v", tt.wantBefore, got)
			}
			if tt.wantAfter != 0 && got.AfterProgID != tt.wantAfter {
				t.Errorf("expected AfterProgID=%d, got %+v", tt.wantAfter, got)
			}

			// Verify mutual exclusivity
			setFields := 0
			if got.First {
				setFields++
			}
			if got.BeforeProgID != 0 {
				setFields++
			}
			if got.AfterProgID != 0 {
				setFields++
			}
			if setFields != 1 {
				t.Errorf("expected exactly one field set, got %d: %+v", setFields, got)
			}
		})
	}
}

func TestFilterLiveTCXLinks(t *testing.T) {
	t.Parallel()

	links := []bpfman.TCXLinkInfo{
		{KernelLinkID: 1, KernelProgramID: 100, Priority: 100},
		{KernelLinkID: 2, KernelProgramID: 200, Priority: 200},
		{KernelLinkID: 3, KernelProgramID: 200, Priority: 200}, // duplicate program
	}

	t.Run("all live are kept", func(t *testing.T) {
		t.Parallel()
		got := filterLiveTCXLinks(links, func(kernel.ProgramID) bool { return true })
		if len(got) != len(links) {
			t.Fatalf("kept %d links, want %d", len(got), len(links))
		}
	})

	t.Run("links to a dead program are dropped", func(t *testing.T) {
		t.Parallel()
		got := filterLiveTCXLinks(links, func(id kernel.ProgramID) bool { return id != 200 })
		if len(got) != 1 || got[0].KernelProgramID != 100 {
			t.Fatalf("got %+v, want only program 100", got)
		}
	})

	t.Run("all dead yields empty, which orders at head not ENOENT", func(t *testing.T) {
		t.Parallel()
		got := filterLiveTCXLinks(links, func(kernel.ProgramID) bool { return false })
		if len(got) != 0 {
			t.Fatalf("kept %d links, want 0", len(got))
		}
		// The whole point: a dead anchor must degrade to Head rather
		// than emit AttachAfter(<dead id>) and fail with ENOENT.
		if order := computeTCXAttachOrder(got, 100); !order.First {
			t.Fatalf("empty chain did not order at head: %+v", order)
		}
	})

	t.Run("liveness is queried once per distinct program", func(t *testing.T) {
		t.Parallel()
		calls := map[kernel.ProgramID]int{}
		filterLiveTCXLinks(links, func(id kernel.ProgramID) bool {
			calls[id]++
			return true
		})
		if calls[100] != 1 || calls[200] != 1 {
			t.Fatalf("expected one liveness query per distinct program, got %v", calls)
		}
	})
}
