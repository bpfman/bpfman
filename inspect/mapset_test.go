package inspect_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/inspect"
	"github.com/bpfman/bpfman/kernel"
)

func mapSetRecord(id kernel.ProgramID, owner *kernel.ProgramID) bpfman.ProgramRecord {
	return bpfman.ProgramRecord{
		ProgramID: id,
		Handles:   bpfman.ProgramHandles{MapOwnerID: owner},
	}
}

func ownerOf(id kernel.ProgramID) *kernel.ProgramID { return &id }

func TestMapSetMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records map[kernel.ProgramID]bpfman.ProgramRecord
		want    map[kernel.ProgramID][]kernel.ProgramID
	}{
		{
			name: "standalone program is its own sole member",
			records: map[kernel.ProgramID]bpfman.ProgramRecord{
				10: mapSetRecord(10, nil),
			},
			want: map[kernel.ProgramID][]kernel.ProgramID{
				10: {10},
			},
		},
		{
			name: "owner and dependants share one sorted set",
			records: map[kernel.ProgramID]bpfman.ProgramRecord{
				12: mapSetRecord(12, ownerOf(10)),
				10: mapSetRecord(10, nil),
				11: mapSetRecord(11, ownerOf(10)),
			},
			want: map[kernel.ProgramID][]kernel.ProgramID{
				10: {10, 11, 12},
				11: {10, 11, 12},
				12: {10, 11, 12},
			},
		},
		{
			name: "deleted owner leaves dependants grouped without it",
			records: map[kernel.ProgramID]bpfman.ProgramRecord{
				11: mapSetRecord(11, ownerOf(10)),
				12: mapSetRecord(12, ownerOf(10)),
			},
			want: map[kernel.ProgramID][]kernel.ProgramID{
				11: {11, 12},
				12: {11, 12},
			},
		},
		{
			name: "independent sets do not bleed into each other",
			records: map[kernel.ProgramID]bpfman.ProgramRecord{
				20: mapSetRecord(20, nil),
				21: mapSetRecord(21, ownerOf(20)),
				30: mapSetRecord(30, nil),
			},
			want: map[kernel.ProgramID][]kernel.ProgramID{
				20: {20, 21},
				21: {20, 21},
				30: {30},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, inspect.MapSetMembers(tt.records))
		})
	}
}

func TestMapSetMembers_ReturnsDistinctSlicesPerProgram(t *testing.T) {
	t.Parallel()

	// The result is exposed on public ProgramStatus.MapUsedBy, so two
	// programs in the same set must not share a backing array: mutating
	// one program's slice must not disturb another's.
	got := inspect.MapSetMembers(map[kernel.ProgramID]bpfman.ProgramRecord{
		10: mapSetRecord(10, nil),
		11: mapSetRecord(11, ownerOf(10)),
	})
	got[10][0] = 999
	assert.Equal(t, []kernel.ProgramID{10, 11}, got[11])
}

func TestMapSetMembers_KeyedByMapIDNotRecordField(t *testing.T) {
	t.Parallel()

	// The map key is the durable program identity. A record whose
	// ProgramID field disagrees with its key must not change the
	// grouping or the result keys.
	got := inspect.MapSetMembers(map[kernel.ProgramID]bpfman.ProgramRecord{
		10: {ProgramID: 0},
	})
	assert.Equal(t, map[kernel.ProgramID][]kernel.ProgramID{10: {10}}, got)
}
