package cliformat

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// renderTable lays out a header row and data rows as space-aligned
// columns. It is the single tabular renderer every list view shares:
// each view builds its headers and string cells and calls this, rather
// than wiring up its own tabwriter. Cells are tab-separated on input and
// padded to aligned columns on output.
//
// indent prefixes every line. It is empty for a top-level listing and a
// couple of spaces for a table nested under a detail-view header (the
// dispatcher members grid under "Members:"). Multi-word headers are
// written with spaces, not underscores.
func renderTable(indent string, headers []string, rows [][]string) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, indent+strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, indent+strings.Join(row, "\t"))
	}

	w.Flush()
	return b.String()
}
