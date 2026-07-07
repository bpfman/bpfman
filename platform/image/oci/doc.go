// Package oci pulls, inspects, and publishes OCI images that carry BPF
// bytecode.
//
// It is the image-registry boundary of the platform layer. NewPuller
// returns a platform.ImagePuller that fetches an image, verifies its
// signature when a verifier is configured, and extracts the ELF object
// for the running architecture into the local image cache.
// InspectBytecodeImage reports an image's manifests, programs, and maps
// without loading it, reading the io.ebpf.programs and io.ebpf.maps
// labels (LabelPrograms, LabelMaps). PublishBytecodeImage builds and
// pushes a possibly multi-arch bytecode image from an imagebuild.Plan.
//
// Functional options configure the operations: WithVerifier and
// WithLogger on a pull, WithPublishLogger on a publish.
package oci
