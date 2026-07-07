package cliformat

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
)

// programListCell renders one entry and returns the value under the named
// column, so a test asserts a cell by meaning. It relies on the fixture
// leaving no empty cells (an empty cell collapses under the gap split).
func programListCell(t *testing.T, entry bpfman.ProgramListEntry, column string) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, RenderProgramList(&buf, bpfman.ProgramListResult{Programs: []bpfman.ProgramListEntry{entry}}, OutputFormatText))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 2, "header plus one row")

	headers := tableGap.Split(strings.TrimRight(lines[0], " "), -1)
	cells := tableGap.Split(strings.TrimRight(lines[1], " "), -1)
	require.Len(t, cells, len(headers), "row column count matches header")

	for i, h := range headers {
		if h == column {
			return cells[i]
		}
	}
	t.Fatalf("no column %q in header %q", column, lines[0])
	return ""
}

// The default program-list table carries exactly these columns, with the
// multi-word headers spelled with spaces.
func TestRenderProgramList_Columns(t *testing.T) {
	t.Parallel()

	entry := bpfman.ProgramListEntry{ProgramID: 42, Application: "demo", Type: "xdp", FunctionName: "xdp_stats"}
	var buf bytes.Buffer
	require.NoError(t, RenderProgramList(&buf, bpfman.ProgramListResult{Programs: []bpfman.ProgramListEntry{entry}}, OutputFormatText))

	header := tableGap.Split(strings.TrimRight(strings.SplitN(buf.String(), "\n", 2)[0], " "), -1)
	assert.Equal(t, []string{"PROGRAM ID", "APPLICATION", "TYPE", "FUNCTION NAME", "LINK IDS"}, header)
}

// When no listed program carries an application label the APPLICATION
// column is elided rather than rendered as a blank stripe down the
// table. One labelled entry anywhere in the result brings it back for
// every row.
func TestRenderProgramList_ApplicationColumnElidedWhenEmpty(t *testing.T) {
	t.Parallel()

	unlabelled := bpfman.ProgramListEntry{ProgramID: 7, Type: "tc", FunctionName: "fn"}
	labelled := bpfman.ProgramListEntry{ProgramID: 8, Application: "demo", Type: "xdp", FunctionName: "pass"}

	headerFor := func(entries ...bpfman.ProgramListEntry) []string {
		var buf bytes.Buffer
		require.NoError(t, RenderProgramList(&buf, bpfman.ProgramListResult{Programs: entries}, OutputFormatText))
		return tableGap.Split(strings.TrimRight(strings.SplitN(buf.String(), "\n", 2)[0], " "), -1)
	}

	assert.Equal(t, []string{"PROGRAM ID", "TYPE", "FUNCTION NAME", "LINK IDS"}, headerFor(unlabelled))
	assert.Equal(t, []string{"PROGRAM ID", "APPLICATION", "TYPE", "FUNCTION NAME", "LINK IDS"}, headerFor(unlabelled, labelled))
}

// The link column carries the bpfman link IDs -- the handles link get and
// link list accept -- so a listing row leads straight to its links without
// another query. All IDs render; there is no truncation. Single spaces
// separate the IDs so each one is a word to a terminal's double-click
// selection; the table's column gaps are two or more spaces, so the cell
// still reads as one column.
func TestRenderProgramList_LinkColumnCarriesLinkIDs(t *testing.T) {
	t.Parallel()

	entry := bpfman.ProgramListEntry{ProgramID: 42, Application: "demo", Type: "xdp", FunctionName: "xdp_stats", Links: []bpfman.LinkID{100, 101, 102, 103}}
	if got := programListCell(t, entry, "LINK IDS"); got != "100 101 102 103" {
		t.Errorf("LINK IDS = %q, want %q (every bpfman link ID, space-separated)", got, "100 101 102 103")
	}
}

// A program with no links shows the sentinel, not a blank cell, so
// "unattached" reads unambiguously.
func TestRenderProgramList_NoLinksShowsSentinel(t *testing.T) {
	t.Parallel()

	entry := bpfman.ProgramListEntry{ProgramID: 7, Application: "app", Type: "tc", FunctionName: "fn"}
	if got := programListCell(t, entry, "LINK IDS"); got != "<none>" {
		t.Errorf("LINK IDS with no links = %q, want %q", got, "<none>")
	}
}
