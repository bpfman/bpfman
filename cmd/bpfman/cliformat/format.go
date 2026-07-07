package cliformat

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/platform"
)

func writeOutput(w io.Writer, output string) error {
	n, err := io.WriteString(w, output)
	if err != nil {
		return err
	}
	if n != len(output) {
		return io.ErrShortWrite
	}
	return nil
}

// CLI output trailing-newline contract.
//
// Every formatter that returns a string for CLI emission MUST end
// its output with exactly one "\n", matching the Unix convention
// for text streams and what every comparable CLI does (kubectl,
// aws, gcloud, jq, ...). Marshaller-driven formatters
// (formatProgramJSON, formatLoadedProgramsJSON, etc.) lean on the
// encoding/json contract. encoding/json.Marshal and MarshalIndent
// never emit a trailing newline (see Marshal / MarshalIndent godoc),
// so `string(output) + "\n"` produces exactly one. No trim needed;
// the producer-side guarantee is checked by
// TestStdlibJSONMarshal_NoTrailingNewline so a future Go upgrade that
// changes the stdlib behaviour is caught.
//
// Code that emits CLI strings should not reinvent either path.
// Marshaller paths use `string(jsonBytes) + "\n"`. Anything else risks
// breaking the shape that consumers, integration tests, and downstream
// scripts rely on.

func unsupportedOutputFormat(format OutputFormat) error {
	return fmt.Errorf("unsupported output format %q", format)
}

// renderOutput dispatches CLI output by format. The JSON branch marshals
// jsonValue indented, with the single trailing newline the CLI contract
// requires; the text branch runs textFn. Per-resource shaping -- envelope
// wrappers, nil-to-empty coercion, presentation joins -- stays in the
// caller; renderOutput owns only the format switch, the JSON encoding and
// trailing newline, and the unsupported-format error.
func renderOutput(w io.Writer, format OutputFormat, jsonValue any, textFn func(io.Writer) error) error {
	switch format {
	case OutputFormatJSON:
		output, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal %T: %w", jsonValue, err)
		}
		return writeOutput(w, string(output)+"\n")
	case OutputFormatText:
		return textFn(w)
	default:
		return unsupportedOutputFormat(format)
	}
}

// A detail (get) view renders as a tree of rows. A row is one of three
// shapes: a field (label and scalar value), a section (label and nested
// children), or a note (a bare line standing in for an empty section,
// such as "(no kernel info available)"). renderRows turns the tree into
// text, computing indentation from depth so a level can be added or moved
// without re-counting spaces.
type row struct {
	label    string
	value    string
	note     string
	children []row
}

// fieldRow is a "label: value" line.
func fieldRow(label, value string) row { return row{label: label, value: value} }

// sectionRow is a header with nested children.
func sectionRow(label string, children ...row) row { return row{label: label, children: children} }

// noteRow is a bare indented line that stands in for an empty section.
func noteRow(text string) row { return row{note: text} }

// sortRowsByLabel orders rows by label, not by their rendered line, so a
// label that is a prefix of another (Target vs Target Function vs Target
// Offset) sorts by the name rather than by the punctuation that follows
// it. Section rows are appended after the sorted fields by the caller and
// keep their given order.
func sortRowsByLabel(rows []row) {
	slices.SortFunc(rows, func(a, b row) int { return strings.Compare(a.label, b.label) })
}

// renderRows writes rows at the given depth, two spaces of indent per
// level. Field values align within a sibling group: the label column is
// padded to the widest field label in the group so the values line up,
// and each subtree aligns independently -- a deep label never pushes a
// shallow value to the right. Section rows render a header and recurse one
// level deeper; note rows render verbatim.
func renderRows(b *strings.Builder, rows []row, depth int) {
	indent := strings.Repeat("  ", depth)
	width := 0
	for _, r := range rows {
		if r.note == "" && len(r.children) == 0 && len(r.label) > width {
			width = len(r.label)
		}
	}
	for _, r := range rows {
		switch {
		case r.note != "":
			fmt.Fprintf(b, "%s%s\n", indent, r.note)
		case len(r.children) > 0:
			fmt.Fprintf(b, "%s%s:\n", indent, r.label)
			renderRows(b, r.children, depth+1)
		default:
			line := fmt.Sprintf("%s%-*s %s", indent, width+1, r.label+":", r.value)
			b.WriteString(strings.TrimRight(line, " "))
			b.WriteByte('\n')
		}
	}
}

// RenderProgram writes a program get result in the specified output format.
func RenderProgram(w io.Writer, prog bpfman.Program, format OutputFormat) error {
	return renderOutput(w, format, prog, func(w io.Writer) error {
		return writeOutput(w, formatProgramTable(prog))
	})
}

func formatProgramTable(prog bpfman.Program) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Program ID: %d\n", prog.Record.ProgramID)
	renderRows(&b, programDetailRows(prog), 1)
	return b.String()
}

// programDetailRows builds the Spec, Status, and Stats sections of a
// program get view.
func programDetailRows(prog bpfman.Program) []row {
	return []row{
		sectionRow("Spec", programSpecRows(prog)...),
		sectionRow("Status", programStatusRows(prog)...),
		sectionRow("Stats", programStatsRows(prog)...),
	}
}

// programSpecRows builds the Spec fields, ordered by label.
func programSpecRows(prog bpfman.Program) []row {
	p := &prog.Record

	var spec []row
	if len(p.Load.GlobalData()) > 0 {
		spec = append(spec, fieldRow("Global", formatGlobalData(p.Load.GlobalData())))
	} else {
		spec = append(spec, fieldRow("Global", "None"))
	}
	spec = append(spec, fieldRow("GPL Compatible", fmt.Sprintf("%t", p.GPLCompatible)))
	if p.License != "" {
		spec = append(spec, fieldRow("License", p.License))
	} else {
		spec = append(spec, fieldRow("License", "None"))
	}
	if p.Handles.MapOwnerID != nil {
		spec = append(spec, fieldRow("Map Owner ID", fmt.Sprintf("%d", *p.Handles.MapOwnerID)))
	} else {
		spec = append(spec, fieldRow("Map Owner ID", "None"))
	}
	spec = append(spec, fieldRow("Map Pin Path", p.Handles.MapsDir.String()))
	if len(p.Meta.Metadata) > 0 {
		spec = append(spec, fieldRow("Metadata", formatMetadata(p.Meta.Metadata)))
	} else {
		spec = append(spec, fieldRow("Metadata", "None"))
	}
	spec = append(spec, fieldRow("Name", p.Meta.Name))
	// The load source is one concept with variant-specific rows: an
	// image load shows its provenance, a file load shows the caller's
	// path operand. The stored copy is reported as Bytecode in Status
	// either way.
	if p.Load.HasImageSource() {
		spec = append(spec, fieldRow("Image URL", p.Load.ImageURL()))
		spec = append(spec, fieldRow("Pull Policy", p.Load.ImagePullPolicy().String()))
	} else {
		spec = append(spec, fieldRow("Path", p.Load.SourcePath()))
	}
	spec = append(spec, fieldRow("Type", p.Load.ProgramType().String()))
	sortRowsByLabel(spec)
	return spec
}

// programStatusRows builds the Status fields, ordered by label, followed
// by the Links and Maps sub-sections.
func programStatusRows(prog bpfman.Program) []row {
	if prog.Status.Kernel == nil {
		return []row{noteRow("(no kernel info available)")}
	}
	kp := prog.Status.Kernel

	var st []row
	if kp.BTFId != 0 {
		st = append(st, fieldRow("BTF ID", fmt.Sprintf("%d", kp.BTFId)))
	}
	// The kernel's own program-type taxonomy (schedcls, probe, tracing,
	// ...), which can differ from the bpfman Type in the Spec: a tcx
	// program is schedcls to the kernel, fentry is tracing. Elided when
	// the kernel did not report a type.
	if kp.ProgramType != "" {
		st = append(st, fieldRow("Kernel Type", kp.ProgramType.String()))
	}
	if prog.Status.ProgPin != "" {
		st = append(st, fieldRow("Bytecode", prog.Status.Bytecode))
	}
	st = append(st, fieldRow("Instructions", fmt.Sprintf("%d", kp.VerifiedInstructions)))
	if !kp.LoadedAt.IsZero() {
		st = append(st, fieldRow("Loaded At", kp.LoadedAt.Format(time.RFC3339)))
	}
	if prog.Status.ProgPin != "" {
		st = append(st, fieldRow("Map Dir", prog.Status.MapDir.String()))
	}
	// Map-sharing membership: every managed program whose records
	// point at this program's map set, i.e. whose data disappears if
	// this program's maps go away. Space-separated so each ID is a
	// word to a terminal's double-click selection.
	if len(prog.Status.MapUsedBy) > 0 {
		ids := make([]string, len(prog.Status.MapUsedBy))
		for i, id := range prog.Status.MapUsedBy {
			ids[i] = fmt.Sprintf("%d", id)
		}
		st = append(st, fieldRow("Maps Used By", strings.Join(ids, " ")))
	}
	if kp.Memlock != 0 {
		st = append(st, fieldRow("Memory", fmt.Sprintf("%d bytes", kp.Memlock)))
	}
	if prog.Status.ProgPin != "" {
		st = append(st, fieldRow("Prog Pin", prog.Status.ProgPin.String()))
	}
	st = append(st, fieldRow("Size JITted", fmt.Sprintf("%d bytes", kp.JitedSize)))
	// The kernel withholds the translated-instruction size under
	// kptr_restrict and/or bpf_jit_harden, leaving XlatedSize zero by
	// omission rather than because the program is empty. Say so instead of
	// printing an authoritative "0 bytes".
	if kp.Restricted {
		st = append(st, fieldRow("Size Translated", "(restricted)"))
	} else {
		st = append(st, fieldRow("Size Translated", fmt.Sprintf("%d bytes", kp.XlatedSize)))
	}
	st = append(st, fieldRow("Tag", kp.Tag))
	sortRowsByLabel(st)

	return append(st, programLinksRow(prog), programMapsRow(prog))
}

// programLinksRow builds the Links sub-section: one section per link with
// its attach details, kind, and pin, or a "None" field when there are no
// links.
func programLinksRow(prog bpfman.Program) row {
	if len(prog.Status.Links) == 0 {
		return fieldRow("Links", "None")
	}
	var entries []row
	for _, l := range prog.Status.Links {
		var props []row
		if l.Record.Details != nil {
			props = append(props, fieldRow("Attach", formatAttachDetails(l.Record.Details)))
		}
		props = append(props, fieldRow("Kind", l.Record.Kind.String()))
		if l.Record.PinPath != nil {
			props = append(props, fieldRow("Pin", fmt.Sprintf("%s%s", l.Record.PinPath.String(), presenceSuffix(l.Status.PinPresent))))
		}
		entries = append(entries, sectionRow(fmt.Sprintf("%d", l.Record.ID), props...))
	}
	return sectionRow("Links", entries...)
}

// programMapsRow builds the Maps sub-section: one section per map with its
// sizes, name, pin, and type; the bare kernel map ids when only those are
// known; or a "None" field when there are none.
func programMapsRow(prog bpfman.Program) row {
	if len(prog.Status.Maps) > 0 {
		var entries []row
		for _, m := range prog.Status.Maps {
			props := []row{
				fieldRow("Key Size", fmt.Sprintf("%dB", m.KeySize)),
				fieldRow("Max Entries", fmt.Sprintf("%d", m.MaxEntries)),
				fieldRow("Name", m.Name),
			}
			if m.PinPath != "" {
				props = append(props, fieldRow("Pin", fmt.Sprintf("%s%s", m.PinPath, presenceSuffix(m.Present))))
			}
			props = append(props,
				fieldRow("Type", m.MapType.String()),
				fieldRow("Value Size", fmt.Sprintf("%dB", m.ValueSize)),
			)
			entries = append(entries, sectionRow(fmt.Sprintf("%d", m.ID), props...))
		}
		return sectionRow("Maps", entries...)
	}
	if len(prog.Status.Kernel.MapIDs) > 0 {
		return fieldRow("Maps", fmt.Sprintf("%v", prog.Status.Kernel.MapIDs))
	}
	return fieldRow("Maps", "None")
}

// programStatsRows builds the Stats fields, ordered by label, or the
// not-enabled note when stats are unavailable.
func programStatsRows(prog bpfman.Program) []row {
	if prog.Status.Stats == nil {
		return []row{noteRow("(not enabled, see sysctl kernel.bpf_stats_enabled)")}
	}
	var stats []row
	if prog.Status.Stats.RecursionMisses > 0 {
		stats = append(stats, fieldRow("Recursion Misses", fmt.Sprintf("%d", prog.Status.Stats.RecursionMisses)))
	}
	stats = append(stats, fieldRow("Run Count", fmt.Sprintf("%d", prog.Status.Stats.RunCount)))
	stats = append(stats, fieldRow("Runtime", prog.Status.Stats.Runtime.String()))
	sortRowsByLabel(stats)
	return stats
}

// formatGlobalData formats global data map for display.
func formatGlobalData(data map[string][]byte) string {
	if len(data) == 0 {
		return "None"
	}
	parts := make([]string, 0, len(data))
	for _, k := range slices.Sorted(maps.Keys(data)) {
		parts = append(parts, fmt.Sprintf("%s=%x", k, data[k]))
	}
	return strings.Join(parts, ", ")
}

// formatMetadata formats metadata map for display.
func formatMetadata(meta map[string]string) string {
	if len(meta) == 0 {
		return "None"
	}
	parts := make([]string, 0, len(meta))
	for _, k := range slices.Sorted(maps.Keys(meta)) {
		parts = append(parts, fmt.Sprintf("%s=%s", k, meta[k]))
	}
	return strings.Join(parts, ", ")
}

// formatAttachDetails formats type-specific link details for display.
func formatAttachDetails(details bpfman.LinkDetails) string {
	if details == nil {
		return ""
	}
	switch d := details.(type) {
	case bpfman.TracepointDetails:
		return d.Group + "/" + d.Name
	case bpfman.KprobeDetails:
		if d.Retprobe {
			return "kretprobe:" + d.FnName
		}
		return d.FnName
	case bpfman.UprobeDetails:
		if d.Retprobe {
			return fmt.Sprintf("uretprobe:%s:%s", d.Target, d.FnName)
		}
		return fmt.Sprintf("%s:%s", d.Target, d.FnName)
	case bpfman.FentryDetails:
		return d.FnName
	case bpfman.FexitDetails:
		return d.FnName
	case bpfman.XDPDetails:
		return fmt.Sprintf("%s (ifindex=%d, pos=%d)", d.Interface, d.Ifindex, d.Position)
	case bpfman.TCDetails:
		return fmt.Sprintf("%s/%s (ifindex=%d, pos=%d)", d.Interface, d.Direction, d.Ifindex, d.Position)
	case bpfman.TCXDetails:
		return fmt.Sprintf("%s/%s (ifindex=%d, pos=%d)", d.Interface, d.Direction, d.Ifindex, d.Position)
	default:
		return ""
	}
}

// presenceSuffix annotates a pin path inline: empty when the pin is
// present, " (missing)" when the path does not exist on the filesystem.
func presenceSuffix(present bool) string {
	if present {
		return ""
	}
	return " (missing)"
}

// RenderLoadedPrograms writes the result of a load command.
func RenderLoadedPrograms(w io.Writer, programs []bpfman.Program, format OutputFormat) error {
	if programs == nil {
		programs = []bpfman.Program{}
	}
	return renderOutput(w, format, bpfman.LoadResult{Programs: programs}, func(w io.Writer) error {
		return writeOutput(w, formatLoadedProgramsTable(programs))
	})
}

func formatLoadedProgramsTable(programs []bpfman.Program) string {
	// Sort programs by program ID for consistent, scannable output.
	sorted := slices.Clone(programs)
	slices.SortFunc(sorted, func(a, b bpfman.Program) int {
		if a.Record.ProgramID < b.Record.ProgramID {
			return -1
		}
		if a.Record.ProgramID > b.Record.ProgramID {
			return 1
		}
		return 0
	})

	// Each program renders exactly like `program get`; a load reporting
	// several programs separates them with a blank line.
	parts := make([]string, len(sorted))
	for i, prog := range sorted {
		parts[i] = formatProgramTable(prog)
	}
	return strings.Join(parts, "\n")
}

// RenderProgramList writes a program list result.
func RenderProgramList(w io.Writer, result bpfman.ProgramListResult, format OutputFormat) error {
	if result.Programs == nil {
		result.Programs = []bpfman.ProgramListEntry{}
	}
	return renderOutput(w, format, result, func(w io.Writer) error {
		return writeOutput(w, formatProgramsCompositeTable(result))
	})
}

// formatProgramsCompositeTable renders the default program-list table.
// The columns -- Program ID, Application, Type, Function Name, and the
// bpfman link IDs -- let the listing answer "which application?" and
// "attached by what?" without a second command: each link ID is the
// handle `link get` and `link list` accept, so a row leads straight to
// its links. Every ID renders, space-separated and untruncated -- single
// spaces keep each ID selectable as a word in a terminal while the
// table's wider column gaps keep the cell one column -- and a count
// would only restate the list's length.
//
// The APPLICATION column is elided when no listed program carries an
// application label, so an unlabelled result set does not render a
// blank stripe down the table; one labelled entry brings it back for
// every row. The per-entry fields are precomputed by the manager, so
// kernel-only rows render with their kernel type and name, the
// no-links sentinel, and (when the column renders at all) an empty
// application cell.
func formatProgramsCompositeTable(result bpfman.ProgramListResult) string {
	withApplication := false
	for _, e := range result.Programs {
		if e.Application != "" {
			withApplication = true
			break
		}
	}

	headers := []string{"PROGRAM ID", "TYPE", "FUNCTION NAME", "LINK IDS"}
	if withApplication {
		headers = []string{"PROGRAM ID", "APPLICATION", "TYPE", "FUNCTION NAME", "LINK IDS"}
	}
	rows := make([][]string, len(result.Programs))
	for i, e := range result.Programs {
		row := []string{fmt.Sprintf("%d", e.ProgramID)}
		if withApplication {
			row = append(row, e.Application)
		}
		rows[i] = append(row, e.Type, e.FunctionName, formatLinkIDs(e.Links))
	}
	return renderTable("", headers, rows)
}

// formatLinkIDs joins a program's bpfman link IDs for the list table,
// with a sentinel when the program has no links so "unattached" never
// renders as a blank cell.
func formatLinkIDs(ids []bpfman.LinkID) string {
	if len(ids) == 0 {
		return "<none>"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return strings.Join(parts, " ")
}

// RenderDispatcherList writes a dispatcher list result.
func RenderDispatcherList(w io.Writer, summaries []platform.DispatcherSummary, format OutputFormat) error {
	if summaries == nil {
		summaries = []platform.DispatcherSummary{}
	}
	return renderOutput(w, format, platform.DispatcherListResult{Dispatchers: summaries}, func(w io.Writer) error {
		return writeOutput(w, formatDispatcherListTable(summaries))
	})
}

func formatDispatcherListTable(summaries []platform.DispatcherSummary) string {
	headers := []string{"TYPE", "NSID", "IFINDEX", "REVISION", "PROGRAM ID", "KERNEL LINK ID", "PRIORITY", "HANDLE", "MEMBERS", "NETNS"}
	rows := make([][]string, len(summaries))
	for i, s := range summaries {
		linkID := "-"
		if s.Runtime.KernelLinkID != nil {
			linkID = fmt.Sprintf("%d", *s.Runtime.KernelLinkID)
		}
		priority := "-"
		if s.Runtime.FilterPriority != nil {
			priority = fmt.Sprintf("%d", *s.Runtime.FilterPriority)
		}
		handle := "-"
		if s.Runtime.FilterHandle != nil {
			handle = fmt.Sprintf("%#x", *s.Runtime.FilterHandle)
		}
		netns := s.Runtime.NetnsPath
		if netns == "" {
			netns = "-"
		}
		rows[i] = []string{
			s.Key.Type.String(),
			fmt.Sprintf("%d", s.Key.Nsid),
			fmt.Sprintf("%d", s.Key.Ifindex),
			fmt.Sprintf("%d", s.Revision),
			fmt.Sprintf("%d", s.Runtime.ProgramID),
			linkID, priority, handle,
			fmt.Sprintf("%d", s.MemberCount),
			netns,
		}
	}
	return renderTable("", headers, rows)
}

// RenderDispatcherSnapshot writes a single dispatcher snapshot.
func RenderDispatcherSnapshot(w io.Writer, snap platform.DispatcherSnapshot, format OutputFormat) error {
	// The snapshot's member order follows the store query, which the
	// rebuild path relies on for equal-priority tie-breaks; it is not
	// the dispatcher's execution order. Present members in POS order
	// (the slot the kernel actually runs) for both JSON and text.
	// Sort a copy so the caller's slice is untouched.
	snap.Members = slices.Clone(snap.Members)
	slices.SortFunc(snap.Members, func(a, b platform.DispatcherMember) int {
		return a.Position - b.Position
	})

	return renderOutput(w, format, snap, func(w io.Writer) error {
		return writeOutput(w, formatDispatcherSnapshotTable(snap))
	})
}

func formatDispatcherSnapshotTable(snap platform.DispatcherSnapshot) string {
	var b strings.Builder

	// Header section: identity line, then the runtime fields as aligned
	// label/value rows.
	fmt.Fprintf(&b, "Dispatcher: %s nsid=%d ifindex=%d\n", snap.Key.Type, snap.Key.Nsid, snap.Key.Ifindex)

	header := []row{
		fieldRow("Revision", fmt.Sprintf("%d", snap.Revision)),
		fieldRow("Program ID", fmt.Sprintf("%d", snap.Runtime.ProgramID)),
	}
	if snap.Runtime.KernelLinkID != nil {
		header = append(header, fieldRow("Kernel Link ID", fmt.Sprintf("%d", *snap.Runtime.KernelLinkID)))
	}
	if snap.Runtime.FilterPriority != nil {
		header = append(header, fieldRow("Priority", fmt.Sprintf("%d", *snap.Runtime.FilterPriority)))
	}
	if snap.Runtime.FilterHandle != nil {
		header = append(header, fieldRow("Filter Handle", fmt.Sprintf("%#x", *snap.Runtime.FilterHandle)))
	}
	renderRows(&b, header, 1)

	// Members table
	fmt.Fprintf(&b, "\nMembers (%d/%d):\n", len(snap.Members), dispatcher.MaxPrograms)

	if len(snap.Members) == 0 {
		b.WriteString("  (none)\n")
		return b.String()
	}

	headers := []string{"POS", "PRIORITY", "PROGRAM ID", "FUNCTION NAME", "LINK ID", "KERNEL LINK ID", "PROCEED ON"}
	rows := make([][]string, len(snap.Members))
	for i, m := range snap.Members {
		kernelLinkID := "<none>"
		if m.KernelLinkID != nil {
			kernelLinkID = fmt.Sprintf("%d", *m.KernelLinkID)
		}
		rows[i] = []string{
			fmt.Sprintf("%d", m.Position),
			fmt.Sprintf("%d", m.Priority),
			fmt.Sprintf("%d", m.ProgramID),
			m.ProgramName,
			fmt.Sprintf("%d", m.LinkID),
			kernelLinkID,
			formatProceedOnMask(m.ProceedOn, snap.Key.Type),
		}
	}
	b.WriteString(renderTable("  ", headers, rows))

	return b.String()
}

// formatProceedOnMask decodes a dispatcher ABI proceed-on bitmask into
// named actions.
func formatProceedOnMask(mask uint32, dispType dispatcher.DispatcherType) string {
	if mask == 0 {
		return "None"
	}

	actions, err := dispatcher.ProceedOnActions(dispType, mask)
	if err != nil {
		return fmt.Sprintf("invalid(%v)", err)
	}
	if dispType == dispatcher.DispatcherTypeXDP {
		return bpfman.XDPActionsToString(actions)
	}
	return bpfman.TCActionsToString(actions)
}
