package cliformat

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// programWithID is a minimal helper for building a Program value that
// round-trips through json.Marshal with a recognisable program_id.
// The CLI format tests do not exercise the rest of the Program shape
// (that is the manager's job), so all other fields stay zero.
func programWithID(id kernel.ProgramID, name string) bpfman.Program {
	return bpfman.Program{
		Record: bpfman.ProgramRecord{
			ProgramID: id,
			Meta:      bpfman.ProgramMeta{Name: name},
		},
	}
}

// TestRenderLoadedProgramsJSON_WrapsWithProgramsKey asserts that
// RenderLoadedPrograms emits a top-level object whose `programs`
// key carries the slice in slice order.
func TestRenderLoadedProgramsJSON_WrapsWithProgramsKey(t *testing.T) {
	t.Parallel()

	programs := []bpfman.Program{
		programWithID(7, "tp_c"),
		programWithID(3, "tp_a"),
		programWithID(5, "tp_b"),
	}

	var buf bytes.Buffer
	require.NoError(t, RenderLoadedPrograms(&buf, programs, OutputFormatJSON))
	got := buf.String()

	// Decode into a generic shape so we test the wire format
	// without depending on bpfman.Program's strict unmarshaller
	// (which rejects empty program type, irrelevant to the
	// wrapper-key contract).
	var raw struct {
		Programs []map[string]any `json:"programs"`
	}
	require.NoError(t, json.NewDecoder(strings.NewReader(got)).Decode(&raw))
	require.Len(t, raw.Programs, len(programs), "programs count")
	for i := range programs {
		record, ok := raw.Programs[i]["record"].(map[string]any)
		require.True(t, ok, "programs[%d].record missing", i)
		require.EqualValues(t, programs[i].Record.ProgramID, record["program_id"], "slice order preserved at index %d", i)
	}
}

// TestRenderLoadedProgramsJSON_EmptySlice asserts that an empty
// load result still produces a valid object with `programs: []`,
// not `programs: null`.
func TestRenderLoadedProgramsJSON_EmptySlice(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"nil", "empty"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var input []bpfman.Program
			if name == "empty" {
				input = []bpfman.Program{}
			}
			var buf bytes.Buffer
			require.NoError(t, RenderLoadedPrograms(&buf, input, OutputFormatJSON))
			got := buf.String()

			var raw map[string]json.RawMessage
			require.NoError(t, json.NewDecoder(strings.NewReader(got)).Decode(&raw))
			require.Contains(t, raw, "programs", "top-level key 'programs' must always be present")
			require.Equal(t, "[]", string(raw["programs"]), "empty load result must marshal as `programs: []`, not null")
		})
	}
}
