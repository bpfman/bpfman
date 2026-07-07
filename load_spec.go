package bpfman

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bpfman/bpfman/kernel"
)

// LoadSpec describes how to load a BPF program.
// LoadSpec is immutable after construction and can only be created via
// the NewLoadSpec or NewAttachLoadSpec constructors, which enforce that
// all required fields are present and valid.
//
// LoadSpec represents user intent (what to load), not runtime wiring
// (where to pin). The bpffs root is provided separately by the Manager
// when calling the kernel layer.
type LoadSpec struct {
	objectPath      string
	sourcePath      string
	programName     string
	programType     ProgramType
	globalData      map[string][]byte
	imageURL        string
	imageDigest     string
	imagePullPolicy ImagePullPolicy
	imageUsername   string // for registry auth
	imagePassword   string // for registry auth
	attachFunc      string
	mapOwnerID      kernel.ProgramID
}

// RequiresAttachFunc returns true if this program type requires an attach
// function (fentry and fexit).
func (t ProgramType) RequiresAttachFunc() bool {
	return t == ProgramTypeFentry || t == ProgramTypeFexit
}

// Valid reports whether t is one of the known program types. It is the
// strict membership predicate the constructors gate on: an empty or
// otherwise unrecognised value is not valid. ParseProgramType is the
// single source of truth for the known set.
func (t ProgramType) Valid() bool {
	_, err := ParseProgramType(string(t))
	return err == nil
}

// NewLoadSpec creates a LoadSpec for program types that do not require
// an attach function. For fentry/fexit, use NewAttachLoadSpec instead.
//
// Returns an error if:
//   - objectPath is empty
//   - programName is empty
//   - programType is invalid or unspecified
//   - programType requires an attach function (use NewAttachLoadSpec)
func NewLoadSpec(objectPath, programName string, programType ProgramType) (LoadSpec, error) {
	if objectPath == "" {
		return LoadSpec{}, errors.New("objectPath is required")
	}
	if programName == "" {
		return LoadSpec{}, errors.New("programName is required")
	}
	if !programType.Valid() {
		return LoadSpec{}, fmt.Errorf("invalid program type: %s", programType)
	}
	if programType.RequiresAttachFunc() {
		return LoadSpec{}, fmt.Errorf("%s requires NewAttachLoadSpec with attachFunc", programType)
	}
	return LoadSpec{
		objectPath:  objectPath,
		programName: programName,
		programType: programType,
	}, nil
}

// NewAttachLoadSpec creates a LoadSpec for program types that require an
// attach function (fentry/fexit).
//
// Returns an error if:
//   - objectPath is empty
//   - programName is empty
//   - programType is invalid or does not require an attach function
//   - attachFunc is empty
func NewAttachLoadSpec(objectPath, programName string, programType ProgramType, attachFunc string) (LoadSpec, error) {
	if objectPath == "" {
		return LoadSpec{}, errors.New("objectPath is required")
	}
	if programName == "" {
		return LoadSpec{}, errors.New("programName is required")
	}
	if !programType.Valid() {
		return LoadSpec{}, fmt.Errorf("invalid program type: %s", programType)
	}
	if !programType.RequiresAttachFunc() {
		return LoadSpec{}, fmt.Errorf("%s does not require attachFunc, use NewLoadSpec", programType)
	}
	if attachFunc == "" {
		return LoadSpec{}, fmt.Errorf("attachFunc is required for %s", programType)
	}
	return LoadSpec{
		objectPath:  objectPath,
		programName: programName,
		programType: programType,
		attachFunc:  attachFunc,
	}, nil
}

// Getters for LoadSpec fields

// ObjectPath returns the bytecode object path the spec was
// constructed with. For a spec that has flowed through a load,
// this is bpfman's stored copy under
// <runtime-dir>/programs/<id>/bytecode.o -- the canonical location
// bpfman reads from afterwards. The path the caller originally
// supplied to a file load is preserved separately; see SourcePath.
func (s LoadSpec) ObjectPath() string { return s.objectPath }

// SourcePath returns the bytecode path the caller supplied to a file
// load, verbatim. It is empty for image loads; image provenance lives
// in the image source fields.
func (s LoadSpec) SourcePath() string { return s.sourcePath }

// ProgramName returns the program (function) name selected within the object.
func (s LoadSpec) ProgramName() string { return s.programName }

// ProgramType returns the program type.
func (s LoadSpec) ProgramType() ProgramType { return s.programType }

// GlobalData returns the global-data overrides applied at load, keyed
// by variable name; nil when none were set.
func (s LoadSpec) GlobalData() map[string][]byte { return s.globalData }

// ImageURL returns the OCI image reference the program was loaded from,
// or empty for a file load.
func (s LoadSpec) ImageURL() string { return s.imageURL }

// ImageDigest returns the resolved image digest, or empty when unknown
// or file-loaded.
func (s LoadSpec) ImageDigest() string { return s.imageDigest }

// ImagePullPolicy returns the image pull policy.
func (s LoadSpec) ImagePullPolicy() ImagePullPolicy { return s.imagePullPolicy }

// ImageUsername returns the registry username for image authentication,
// or empty when none was set.
func (s LoadSpec) ImageUsername() string { return s.imageUsername }

// ImagePassword returns the registry password for image authentication,
// or empty when none was set.
func (s LoadSpec) ImagePassword() string { return s.imagePassword }

// AttachFunc returns the attach function for fentry/fexit programs, or
// empty for program types that do not use one.
func (s LoadSpec) AttachFunc() string { return s.attachFunc }

// MapOwnerID returns the kernel ID of the program whose maps this
// program shares, or 0 when it owns its maps.
func (s LoadSpec) MapOwnerID() kernel.ProgramID { return s.mapOwnerID }

// HasImageAuth returns true if this LoadSpec has registry authentication configured.
func (s LoadSpec) HasImageAuth() bool { return s.imageUsername != "" }

// HasImageSource returns true if this LoadSpec specifies an OCI image source.
// This is true both when the image URL was set as input (for pulling) and
// when it was set as provenance (after loading from an image).
func (s LoadSpec) HasImageSource() bool { return s.imageURL != "" }

// WithGlobalData returns a new LoadSpec with global data set.
func (s LoadSpec) WithGlobalData(data map[string][]byte) LoadSpec {
	s.globalData = data
	return s
}

// WithImageProvenance returns a new LoadSpec with image provenance set.
// Used when loading from an OCI image.
func (s LoadSpec) WithImageProvenance(url, digest string, policy ImagePullPolicy) LoadSpec {
	s.imageURL = url
	s.imageDigest = digest
	s.imagePullPolicy = policy
	return s
}

// WithMapOwnerID returns a new LoadSpec with map owner ID set.
func (s LoadSpec) WithMapOwnerID(id kernel.ProgramID) LoadSpec {
	s.mapOwnerID = id
	return s
}

// WithImageAuth returns a new LoadSpec with registry authentication set.
func (s LoadSpec) WithImageAuth(username, password string) LoadSpec {
	s.imageUsername = username
	s.imagePassword = password
	return s
}

// Builder methods for reconstructing LoadSpec from stored data.
// These bypass constructor validation since the data was validated at creation time.

// WithObjectPath returns a new LoadSpec with object path set.
// Used when reconstructing from stored data.
func (s LoadSpec) WithObjectPath(path string) LoadSpec {
	s.objectPath = path
	return s
}

// WithSourcePath returns a new LoadSpec with the caller-supplied
// file-load path set. Used when building the persisted record at load
// time and when reconstructing from stored data.
func (s LoadSpec) WithSourcePath(path string) LoadSpec {
	s.sourcePath = path
	return s
}

// WithProgramName returns a new LoadSpec with program name set.
// Used when reconstructing from stored data.
func (s LoadSpec) WithProgramName(name string) LoadSpec {
	s.programName = name
	return s
}

// WithProgramType returns a new LoadSpec with program type set.
// Used when reconstructing from stored data.
func (s LoadSpec) WithProgramType(pt ProgramType) LoadSpec {
	s.programType = pt
	return s
}

// WithAttachFunc returns a new LoadSpec with attach function set.
// Used when reconstructing from stored data.
func (s LoadSpec) WithAttachFunc(fn string) LoadSpec {
	s.attachFunc = fn
	return s
}

// imageSourceJSON is the JSON representation of image provenance fields.
// Kept as a nested object for backwards compatibility with existing DB rows.
type imageSourceJSON struct {
	URL        string          `json:"url"`
	Digest     string          `json:"digest"` // empty when the registry returned no digest
	PullPolicy ImagePullPolicy `json:"pull_policy"`
}

// loadSpecJSON is the JSON representation of LoadSpec.
// This allows LoadSpec to have private fields while still being serialisable.
//
// Every field is always emitted: nullable concepts (ImageSource,
// MapOwnerID) marshal as JSON null when unset; collections
// (GlobalData) marshal as {} when empty. The contract is a stable
// schema for consumers; absence is never used to encode meaning.
type loadSpecJSON struct {
	ObjectPath  string            `json:"object_path"` // bpfman's stored bytecode path post-load (<runtime-dir>/programs/<id>/bytecode.o)
	SourcePath  *string           `json:"source_path"` // the caller's file-load path operand, verbatim; null for image loads
	ProgramName string            `json:"program_name"`
	ProgramType ProgramType       `json:"program_type"`
	GlobalData  map[string][]byte `json:"global_data"`  // always emit; {} when no globals
	ImageSource *imageSourceJSON  `json:"image_source"` // always emit; null when file-loaded
	AttachFunc  *string           `json:"attach_func"`  // null for program types that do not use it; the attach symbol name (e.g. fentry/fexit) when applicable
	MapOwnerID  *kernel.ProgramID `json:"map_owner_id"` // null when this program does not share another's maps; mirrors ProgramHandles.MapOwnerID
}

// MarshalJSON implements json.Marshaler.
func (s LoadSpec) MarshalJSON() ([]byte, error) {
	var imgSrc *imageSourceJSON
	if s.imageURL != "" {
		imgSrc = &imageSourceJSON{
			URL:        s.imageURL,
			Digest:     s.imageDigest,
			PullPolicy: s.imagePullPolicy,
		}
	}
	gd := s.globalData
	if gd == nil {
		gd = map[string][]byte{}
	}
	var mapOwnerID *kernel.ProgramID
	if s.mapOwnerID != 0 {
		moid := s.mapOwnerID
		mapOwnerID = &moid
	}
	var attachFunc *string
	if s.attachFunc != "" {
		af := s.attachFunc
		attachFunc = &af
	}
	var sourcePath *string
	if s.sourcePath != "" {
		src := s.sourcePath
		sourcePath = &src
	}
	return json.Marshal(loadSpecJSON{
		ObjectPath:  s.objectPath,
		SourcePath:  sourcePath,
		ProgramName: s.programName,
		ProgramType: s.programType,
		GlobalData:  gd,
		ImageSource: imgSrc,
		AttachFunc:  attachFunc,
		MapOwnerID:  mapOwnerID,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
// Note: This bypasses the constructor validation to support deserialising
// stored data. The assumption is that data was validated at creation time.
func (s *LoadSpec) UnmarshalJSON(data []byte) error {
	var js loadSpecJSON
	if err := json.Unmarshal(data, &js); err != nil {
		return err
	}

	s.objectPath = js.ObjectPath
	if js.SourcePath != nil {
		s.sourcePath = *js.SourcePath
	}
	s.programName = js.ProgramName
	s.programType = js.ProgramType
	s.globalData = js.GlobalData
	if js.ImageSource != nil {
		s.imageURL = js.ImageSource.URL
		s.imageDigest = js.ImageSource.Digest
		s.imagePullPolicy = js.ImageSource.PullPolicy
	}
	if js.AttachFunc != nil {
		s.attachFunc = *js.AttachFunc
	}
	if js.MapOwnerID != nil {
		s.mapOwnerID = *js.MapOwnerID
	}
	return nil
}
