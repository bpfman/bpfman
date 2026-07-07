package bpfman

import "slices"

// attachVerb returns, for an attach spec, the user-facing attach verb
// and the set of loaded program types that verb can legitimately drive.
//
// The mapping is one-to-one except for the two probe verbs: the kprobe
// verb serves both kprobe and kretprobe programs, and the uprobe verb
// both uprobe and uretprobe, because the return variant is derived from
// the loaded program type rather than chosen at attach. An unrecognised
// spec type yields the empty set, which makes the membership check below
// fail closed.
func attachVerb(spec AttachSpec) (verb string, accepts []ProgramType) {
	switch spec.(type) {
	case TracepointAttachSpec:
		return "tracepoint", []ProgramType{ProgramTypeTracepoint}
	case KprobeAttachSpec:
		return "kprobe", []ProgramType{ProgramTypeKprobe, ProgramTypeKretprobe}
	case UprobeAttachSpec:
		return "uprobe", []ProgramType{ProgramTypeUprobe, ProgramTypeUretprobe}
	case FentryAttachSpec:
		return "fentry", []ProgramType{ProgramTypeFentry}
	case FexitAttachSpec:
		return "fexit", []ProgramType{ProgramTypeFexit}
	case XDPAttachSpec:
		return "xdp", []ProgramType{ProgramTypeXDP}
	case TCAttachSpec:
		return "tc", []ProgramType{ProgramTypeTC}
	case TCXAttachSpec:
		return "tcx", []ProgramType{ProgramTypeTCX}
	default:
		return "", nil
	}
}

// ValidateAttachProgramType reports whether a program of progType may be
// attached using spec. It returns nil when the type is one the spec's
// attach verb can drive, and ErrAttachKindMismatch otherwise. Callers
// run this before any kernel or store mutation, so a mismatch is a clean
// rejection that leaves no link record and no kernel state behind.
func ValidateAttachProgramType(spec AttachSpec, progType ProgramType) error {
	verb, accepts := attachVerb(spec)
	if slices.Contains(accepts, progType) {
		return nil
	}
	return ErrAttachKindMismatch{
		ProgramID:  spec.ProgramID(),
		ActualType: progType,
		AttachKind: verb,
		Accepts:    accepts,
	}
}
