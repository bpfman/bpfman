package cliformat

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// LinkGetView is the output view for get-link commands. ProgramName is
// a presentation-only join resolved by the caller from Link.Record.ProgramID.
type LinkGetView struct {
	// Link is the attachment being displayed.
	Link bpfman.Link

	// ProgramName is the presentation-only program name, resolved by the caller from Link.Record.ProgramID and shown only in table output.
	ProgramName string
}

// LinkListView is the output view for link list commands.
type LinkListView struct {
	// Links are the attachment records to display, one row per link.
	Links []bpfman.LinkRecord

	// Programs resolves each link's owning program, keyed by
	// LinkRecord.ProgramID, to the presentation-only application and
	// function name shown in the APPLICATION and FUNCTION NAME columns of
	// the text table. It is unused by structured output, which carries
	// the link records alone.
	Programs map[kernel.ProgramID]LinkProgramRef
}

// LinkProgramRef carries the owning-program fields shown per link in the
// link list: the application grouping label and the BPF function name.
type LinkProgramRef struct {
	// Application is the program's application metadata label; empty when unset.
	Application string

	// FunctionName is the program's BPF function name.
	FunctionName string
}

// RenderLinkAttach writes the result of a link attach command.
func RenderLinkAttach(w io.Writer, link bpfman.Link, format OutputFormat) error {
	return renderOutput(w, format, link, func(w io.Writer) error {
		return writeOutput(w, formatLinkTable(LinkGetView{Link: link}))
	})
}

// RenderLinkGet writes the result of a get-link command.
func RenderLinkGet(w io.Writer, view LinkGetView, format OutputFormat) error {
	return renderOutput(w, format, view.Link, func(w io.Writer) error {
		return writeOutput(w, formatLinkTable(view))
	})
}

// RenderLinkList writes the result of a link list command.
func RenderLinkList(w io.Writer, view LinkListView, format OutputFormat) error {
	links := view.Links
	if links == nil {
		links = []bpfman.LinkRecord{}
	}
	return renderOutput(w, format, bpfman.LinkListResult{Links: links}, func(w io.Writer) error {
		return renderLinkListTable(w, view)
	})
}

// renderLinkListTable renders the link-list overview. The pin path is
// deliberately absent: the bpffs layout is internal naming, not
// interface, and the ATTACHMENT summary carries the "attached to what?"
// answer in domain terms. Pin detail stays on `link get` and in the
// JSON output.
func renderLinkListTable(w io.Writer, view LinkListView) error {
	withApplication := false
	for _, l := range view.Links {
		if view.Programs[l.ProgramID].Application != "" {
			withApplication = true
			break
		}
	}

	headers := []string{"LINK ID", "KERNEL LINK ID", "KIND", "PROGRAM ID", "FUNCTION NAME", "ATTACHMENT"}
	if withApplication {
		headers = []string{"LINK ID", "KERNEL LINK ID", "KIND", "PROGRAM ID", "APPLICATION", "FUNCTION NAME", "ATTACHMENT"}
	}

	rows := make([][]string, len(view.Links))
	for i, l := range view.Links {
		kernelLinkID := "<none>"
		if l.KernelLinkID != nil {
			kernelLinkID = fmt.Sprintf("%d", *l.KernelLinkID)
		}

		ref := view.Programs[l.ProgramID]
		functionName := ref.FunctionName
		if functionName == "" {
			functionName = "<none>"
		}

		row := []string{
			fmt.Sprintf("%d", l.ID),
			kernelLinkID,
			l.Kind.String(),
			fmt.Sprintf("%d", l.ProgramID),
		}
		if withApplication {
			row = append(row, ref.Application)
		}
		rows[i] = append(row, functionName, attachmentSummary(l.Details))
	}
	return writeOutput(w, renderTable("", headers, rows))
}

// attachmentSummary renders a one-cell summary of where a link is
// attached, from its typed details: interface, direction, and chain
// position for the network kinds, the traced function or tracepoint for
// the probe kinds. The KIND column already names the link type, so the
// summary carries only the target.
func attachmentSummary(details bpfman.LinkDetails) string {
	switch d := details.(type) {
	case bpfman.XDPDetails:
		s := fmt.Sprintf("%s pos-%d", d.Interface, d.Position)
		if d.Netns != "" {
			s += " netns=" + d.Netns
		}
		return s
	case bpfman.TCDetails:
		s := fmt.Sprintf("%s %s pos-%d", d.Interface, d.Direction, d.Position)
		if d.Netns != "" {
			s += " netns=" + d.Netns
		}
		return s
	case bpfman.TCXDetails:
		s := fmt.Sprintf("%s %s pos-%d", d.Interface, d.Direction, d.Position)
		if d.Netns != "" {
			s += " netns=" + d.Netns
		}
		return s
	case bpfman.TracepointDetails:
		return d.Group + "/" + d.Name
	case bpfman.KprobeDetails:
		if d.Offset != 0 {
			return fmt.Sprintf("%s+%d", d.FnName, d.Offset)
		}
		return d.FnName
	case bpfman.UprobeDetails:
		if d.FnName == "" {
			return fmt.Sprintf("%s +%d", d.Target, d.Offset)
		}
		return d.Target + " " + d.FnName
	case bpfman.FentryDetails:
		return d.FnName
	case bpfman.FexitDetails:
		return d.FnName
	default:
		return "<none>"
	}
}

func formatLinkTable(view LinkGetView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Link ID: %d\n", view.Link.Record.ID)
	renderRows(&b, linkDetailRows(view), 1)
	return b.String()
}

// linkDetailRows builds the Spec and Status sections of a link get view.
// Spec fields are ordered by label, not by their rendered line, so a label
// that is a prefix of another (Target vs Target Function vs Target Offset)
// sorts by the name rather than by the punctuation that follows it.
func linkDetailRows(view LinkGetView) []row {
	link := view.Link

	var spec []row
	add := func(label, value string) { spec = append(spec, fieldRow(label, value)) }

	if view.ProgramName != "" {
		add("BPF Function", view.ProgramName)
	}
	if !link.Record.CreatedAt.IsZero() {
		add("Created At", link.Record.CreatedAt.Format(time.RFC3339))
	}
	if link.Record.KernelLinkID != nil {
		add("Kernel Link ID", fmt.Sprintf("%d", *link.Record.KernelLinkID))
	} else {
		add("Kernel Link ID", "None")
	}
	add("Metadata", formatMetadata(link.Record.Metadata))
	if link.Record.PinPath != nil {
		add("Pin Path", link.Record.PinPath.String())
	} else {
		add("Pin Path", "None")
	}
	add("Program ID", fmt.Sprintf("%d", link.Record.ProgramID))
	add("Type", link.Record.Kind.String())

	// Type-specific fields from LinkDetails.
	switch d := link.Record.Details.(type) {
	case bpfman.FentryDetails:
		add("Target Function", d.FnName)
	case bpfman.FexitDetails:
		add("Target Function", d.FnName)
	case bpfman.KprobeDetails:
		if d.Retprobe {
			add("Attach Type", "kretprobe")
		} else {
			add("Attach Type", "kprobe")
		}
		add("Target Function", d.FnName)
		if d.Offset != 0 {
			add("Target Offset", fmt.Sprintf("%d", d.Offset))
		}
	case bpfman.TCDetails:
		add("Direction", d.Direction.String())
		add("Interface", d.Interface)
		add("Network Namespace", netnsOrNone(d.Netns))
		add("Position", fmt.Sprintf("%d", d.Position))
		add("Priority", fmt.Sprintf("%d", d.Priority))
		add("Proceed On", bpfman.TCActionsToString(d.ProceedOn))
	case bpfman.TCXDetails:
		add("Direction", d.Direction.String())
		add("Interface", d.Interface)
		add("Network Namespace", netnsOrNone(d.Netns))
		add("Position", fmt.Sprintf("%d", d.Position))
		add("Priority", fmt.Sprintf("%d", d.Priority))
	case bpfman.TracepointDetails:
		add("Tracepoint", d.Group+"/"+d.Name)
	case bpfman.UprobeDetails:
		if d.Retprobe {
			add("Attach Type", "uretprobe")
		} else {
			add("Attach Type", "uprobe")
		}
		if d.PID != 0 {
			add("PID", fmt.Sprintf("%d", d.PID))
		}
		add("Target", d.Target)
		add("Target Function", d.FnName)
		if d.Offset != 0 {
			add("Target Offset", fmt.Sprintf("%d", d.Offset))
		}
	case bpfman.XDPDetails:
		add("Interface", d.Interface)
		add("Network Namespace", netnsOrNone(d.Netns))
		add("Position", fmt.Sprintf("%d", d.Position))
		add("Priority", fmt.Sprintf("%d", d.Priority))
		add("Proceed On", bpfman.XDPActionsToString(d.ProceedOn))
	}

	sortRowsByLabel(spec)

	status := []row{
		fieldRow("Kernel Seen", fmt.Sprintf("%t", link.Status.KernelSeen)),
		fieldRow("Pin Present", fmt.Sprintf("%t", link.Status.PinPresent)),
	}
	sortRowsByLabel(status)

	return []row{
		sectionRow("Spec", spec...),
		sectionRow("Status", status...),
	}
}

// netnsOrNone renders a link's network namespace path, falling back to
// "None" for host (empty) attaches.
func netnsOrNone(netns string) string {
	if netns == "" {
		return "None"
	}
	return netns
}
