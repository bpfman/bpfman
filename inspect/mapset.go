package inspect

import (
	"slices"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// MapSetMembers derives, for every program in records, the sorted ids
// of all programs sharing its map set. A program's map set is its
// MapOwnerID when it is a shared-map consumer, otherwise its own id,
// mirroring the map_set_id column the store groups on.
//
// records is keyed by durable program id, and that key is the identity
// used throughout: the grouping, the member ids, and the result keys
// all come from it, not from the record's own ProgramID field, so the
// derivation cannot drift if the two ever disagree. The result is keyed
// the same way, so a caller reads its set with the id it already holds.
//
// Every record is ranged over, so a program whose kernel object is
// absent still counts as a member, matching the store's
// ListMapSetUsers, which queries managed_programs regardless of kernel
// presence. Each program receives its own slice: the result is exposed
// on public ProgramStatus.MapUsedBy, so members must not share a
// backing array.
func MapSetMembers(records map[kernel.ProgramID]bpfman.ProgramRecord) map[kernel.ProgramID][]kernel.ProgramID {
	bySet := make(map[kernel.ProgramID][]kernel.ProgramID)
	for id, r := range records {
		set := mapSetID(id, r)
		bySet[set] = append(bySet[set], id)
	}
	for _, members := range bySet {
		slices.Sort(members)
	}

	out := make(map[kernel.ProgramID][]kernel.ProgramID, len(records))
	for id, r := range records {
		out[id] = slices.Clone(bySet[mapSetID(id, r)])
	}
	return out
}

// mapSetID returns the id of the map set a record belongs to: its
// MapOwnerID when set, else id. id is the record's durable map key, so
// a non-consumer's set id does not depend on its own ProgramID field.
func mapSetID(id kernel.ProgramID, r bpfman.ProgramRecord) kernel.ProgramID {
	if r.Handles.MapOwnerID != nil {
		return *r.Handles.MapOwnerID
	}
	return id
}
