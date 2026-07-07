package dispatcher

import (
	"fmt"

	"github.com/bpfman/bpfman"
)

// XDPExtensionAttachSpec contains parameters for attaching an XDP extension
// program to a dispatcher slot. The extension program is loaded from its
// bpffs pin rather than re-read from the original ELF file.
type XDPExtensionAttachSpec struct {
	// DispatcherPinPath is the bpffs pin of the dispatcher program the
	// extension attaches into.
	DispatcherPinPath bpfman.ProgPinPath `json:"dispatcher_pin_path"`

	// ProgPinPath is the bpffs pin of the extension program; it is
	// loaded from this pin rather than re-read from the original ELF.
	ProgPinPath bpfman.ProgPinPath `json:"prog_pin_path"`

	// ProgramName is the extension program's name. The dispatcher slot
	// the extension attaches into is selected by Position (see
	// SlotName), not by this name.
	ProgramName string `json:"program_name"`

	// Position is the dispatcher slot to attach into, in the range
	// [0, MaxPrograms).
	Position int `json:"position"`

	// LinkPinPath empty means the extension link is ephemeral (not pinned); the
	// empty string is the discriminator for ephemeral versus pinned extensions.
	LinkPinPath bpfman.LinkPath `json:"link_pin_path,omitempty"`
}

// validateExtensionAttachSpec checks the fields shared by the XDP and TC
// extension attach specs, prefixing each error with kind ("XDP" or
// "TC"). The two spec types are distinct because they select different
// kernel attach paths at the boundary, but their validation is the same.
func validateExtensionAttachSpec(kind string, dispatcherPin, progPin bpfman.ProgPinPath, programName string, position int) error {
	if dispatcherPin == "" {
		return fmt.Errorf("%s extension: DispatcherPinPath is required", kind)
	}
	if progPin == "" {
		return fmt.Errorf("%s extension: ProgPinPath is required", kind)
	}
	if programName == "" {
		return fmt.Errorf("%s extension: ProgramName is required", kind)
	}
	if position < 0 || position >= MaxPrograms {
		return fmt.Errorf("%s extension: Position %d out of range [0, %d)", kind, position, MaxPrograms)
	}
	return nil
}

// Validate checks the spec for invalid or missing values.
func (s XDPExtensionAttachSpec) Validate() error {
	return validateExtensionAttachSpec("XDP", s.DispatcherPinPath, s.ProgPinPath, s.ProgramName, s.Position)
}

// TCExtensionAttachSpec contains parameters for attaching a TC extension
// program to a dispatcher slot. The extension program is loaded from its
// bpffs pin rather than re-read from the original ELF file.
type TCExtensionAttachSpec struct {
	// DispatcherPinPath is the bpffs pin of the dispatcher program the
	// extension attaches into.
	DispatcherPinPath bpfman.ProgPinPath `json:"dispatcher_pin_path"`

	// ProgPinPath is the bpffs pin of the extension program; it is
	// loaded from this pin rather than re-read from the original ELF.
	ProgPinPath bpfman.ProgPinPath `json:"prog_pin_path"`

	// ProgramName is the extension program's name. The dispatcher slot
	// the extension attaches into is selected by Position (see
	// SlotName), not by this name.
	ProgramName string `json:"program_name"`

	// Position is the dispatcher slot to attach into, in the range
	// [0, MaxPrograms).
	Position int `json:"position"`

	// LinkPinPath empty means the extension link is ephemeral (not pinned); the
	// empty string is the discriminator for ephemeral versus pinned extensions.
	LinkPinPath bpfman.LinkPath `json:"link_pin_path,omitempty"`
}

// Validate checks the spec for invalid or missing values.
func (s TCExtensionAttachSpec) Validate() error {
	return validateExtensionAttachSpec("TC", s.DispatcherPinPath, s.ProgPinPath, s.ProgramName, s.Position)
}
