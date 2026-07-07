// Package imagebuild assembles and describes OCI images that carry BPF
// bytecode.
//
// A bytecode image is an OCI artefact whose layers hold compiled ELF
// objects -- one per target architecture -- annotated with metadata
// that lets a puller select the right object and learn what it
// contains. This package prepares the inputs for that artefact: the
// Arch constants enumerate the supported target architectures,
// BytecodeInput pairs an architecture with its ELF object, and
// MultiArchSource gathers several inputs into one multi-arch
// BytecodeSource.
//
// It also derives the descriptive metadata recorded on the image. From
// a compiled object it extracts the program and map names and types
// (Info) and renders them, via LabelBuildArgValues, into the
// io.ebpf.programs and io.ebpf.maps image labels, so an image can be
// inspected without being loaded. The Format functions render a build
// Plan as text or JSON for the CLI.
package imagebuild
