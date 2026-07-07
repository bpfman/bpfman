package bpfman

import (
	"fmt"
	"strings"

	"github.com/bpfman/bpfman/kernel"
)

// ErrLinkNotFound is returned when attempting to operate on a link
// that does not exist in either the kernel or bpfman's store.
type ErrLinkNotFound struct {
	// LinkID is the bpfman link handle that was not found.
	LinkID LinkID `json:"link_id"`
}

// Error implements the error interface.
func (e ErrLinkNotFound) Error() string {
	return fmt.Sprintf("link %d does not exist", e.LinkID)
}

// ErrProgramNotManaged is returned when attempting to operate on a program
// that exists in the kernel but is not managed by bpfman.
type ErrProgramNotManaged struct {
	// ID is the kernel program ID that bpfman does not manage.
	ID kernel.ProgramID `json:"id"`
}

// Error implements the error interface.
func (e ErrProgramNotManaged) Error() string {
	return fmt.Sprintf("program %d exists in kernel but is not managed by bpfman", e.ID)
}

// ErrProgramNotFound is returned when attempting to operate on a program
// that does not exist in either the kernel or bpfman's store.
type ErrProgramNotFound struct {
	// ID is the kernel program ID that was not found.
	ID kernel.ProgramID `json:"id"`
}

// Error implements the error interface.
func (e ErrProgramNotFound) Error() string {
	return fmt.Sprintf("program %d does not exist", e.ID)
}

// ErrAttachKindMismatch is returned when an attach request targets a
// program whose loaded type the chosen attach verb cannot drive -- for
// example attaching a uprobe program via the kprobe verb. The check
// runs before any kernel or store side effect, so a mismatch leaves no
// link record and no kernel state. AttachKind is the user-facing verb
// (kprobe, uprobe, fentry, ...); Accepts is the set of program types
// that verb legitimately serves (two for the probe verbs, which cover
// both the entry and return variants, one for every other verb).
type ErrAttachKindMismatch struct {
	// ProgramID is the kernel program ID of the targeted program.
	ProgramID kernel.ProgramID `json:"program_id"`

	// ActualType is the loaded program's bpfman type.
	ActualType ProgramType `json:"actual_type"`

	// AttachKind is the user-facing attach verb (kprobe, uprobe, fentry, ...).
	AttachKind string `json:"attach_kind"`

	// Accepts is the set of program types the attach verb legitimately serves.
	Accepts []ProgramType `json:"accepts"`
}

// Error implements the error interface.
func (e ErrAttachKindMismatch) Error() string {
	accepts := make([]string, len(e.Accepts))
	for i, t := range e.Accepts {
		accepts[i] = t.String()
	}
	var which string
	switch len(accepts) {
	case 0:
		which = "no program type"
	case 1:
		which = accepts[0]
	default:
		which = strings.Join(accepts[:len(accepts)-1], ", ") + " or " + accepts[len(accepts)-1]
	}
	return fmt.Sprintf("program %d is a %s program; the %s attach accepts %s", e.ProgramID, e.ActualType, e.AttachKind, which)
}

// ErrTracepointNotFound is returned when an attach targets a kernel
// tracepoint that is not present in /sys/kernel/tracing/events/.
// Suggestions holds up to a few nearest-match tracepoints computed by
// the manager; empty when nothing close enough was found or when the
// kernel could not be consulted.
type ErrTracepointNotFound struct {
	// Group is the tracepoint group (the directory under events/).
	Group string `json:"group"`

	// Name is the tracepoint name within the group.
	Name string `json:"name"`

	// Suggestions holds up to a few nearest-match tracepoints; empty when
	// nothing close enough was found or the kernel could not be consulted.
	Suggestions []string `json:"suggestions,omitempty"`
}

// Error implements the error interface.
func (e ErrTracepointNotFound) Error() string {
	msg := fmt.Sprintf("tracepoint %q not found", e.Group+"/"+e.Name)
	if len(e.Suggestions) == 0 {
		return msg
	}
	return msg + "; did you mean: " + strings.Join(e.Suggestions, ", ") + "?"
}
