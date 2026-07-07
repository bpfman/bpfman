package kernel

import "strings"

// ProgramType represents a kernel BPF program type.
// Always lowercase. Use NewProgramType to construct.
type ProgramType string

// NewProgramType creates a ProgramType from a string, normalising to lowercase.
func NewProgramType(s string) ProgramType {
	return ProgramType(strings.ToLower(s))
}

// String returns the program type as a string.
func (t ProgramType) String() string { return string(t) }

// MapType represents a kernel BPF map type.
// Always lowercase. Use NewMapType to construct.
type MapType string

// NewMapType creates a MapType from a string, normalising to lowercase.
func NewMapType(s string) MapType {
	return MapType(strings.ToLower(s))
}

// String returns the map type as a string.
func (t MapType) String() string { return string(t) }
