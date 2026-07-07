package ebpf

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"github.com/cilium/ebpf"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/platform"
)

// ProgramValidator implements platform.ProgramValidator using cilium/ebpf.
type ProgramValidator struct{}

// NewProgramValidator creates a new program validator.
func NewProgramValidator() *ProgramValidator {
	return &ProgramValidator{}
}

// ValidatePrograms implements platform.ProgramValidator.
func (d *ProgramValidator) ValidatePrograms(objectPath string, programNames []string) error {
	return ValidatePrograms(objectPath, programNames)
}

// Ensure ProgramValidator implements the interface.
var _ platform.ProgramValidator = (*ProgramValidator)(nil)

// InferProgramType returns the program type based on the ELF section name.
// This follows the Rust bpfman approach of deriving the type from bytecode
// metadata rather than relying on user-specified types.
//
// Section name patterns (from cilium/ebpf elf_sections.go):
//   - kprobe/*, kprobe.multi/* -> kprobe
//   - kretprobe/*, kretprobe.multi/* -> kretprobe
//   - uprobe/*, uprobe.multi/* -> uprobe
//   - uretprobe/*, uretprobe.multi/* -> uretprobe
//   - tracepoint/* -> tracepoint
//   - xdp*, xdp.frags* -> xdp
//   - tc, classifier/* -> tc
//   - tcx/* -> tcx
//   - fentry/* -> fentry
//   - fexit/* -> fexit
func InferProgramType(sectionName string) bpfman.ProgramType {
	return inferProgramType(sectionName)
}

// ValidatePrograms checks that all specified program names exist in the
// given object file. Returns an error listing any missing programs along
// with the available programs in the object file.
func ValidatePrograms(objectPath string, programNames []string) error {
	if len(programNames) == 0 {
		return nil
	}

	f, err := os.Open(objectPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return ValidateProgramsFromReader(f, programNames)
}

// ValidateProgramsFromReader is the io.ReaderAt-based form of
// ValidatePrograms.
func ValidateProgramsFromReader(rd io.ReaderAt, programNames []string) error {
	if len(programNames) == 0 {
		return nil
	}

	collSpec, err := ebpf.LoadCollectionSpecFromReader(rd)
	if err != nil {
		return fmt.Errorf("load collection spec: %w", err)
	}

	// Build set of available program names
	available := make(map[string]bool)
	for name := range collSpec.Programs {
		available[name] = true
	}

	// Check each requested program
	var missing []string
	for _, name := range programNames {
		if !available[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		slices.Sort(missing)
		availableList := slices.Sorted(maps.Keys(available))
		return fmt.Errorf("program(s) not found in object file: %v; available programs: %v", missing, availableList)
	}

	return nil
}
