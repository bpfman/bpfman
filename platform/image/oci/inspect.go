package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// ImageInspection is the registry-visible metadata for an OCI bytecode image.
// Programs/Maps and Layers describe a single-platform image; Manifests is
// populated instead when the reference resolves to an image index.
type ImageInspection struct {
	// Reference is the fully qualified image reference that was
	// inspected.
	Reference string `json:"reference"`

	// Digest is the resolved content digest of the image or index.
	Digest string `json:"digest"`

	// MediaType is the OCI media type of the resolved descriptor (an
	// image manifest or an image index).
	MediaType string `json:"media_type"`

	// Programs maps program name to type, decoded from the
	// io.ebpf.programs label. It is populated only for a
	// single-platform image; nil for an image index.
	Programs map[string]string `json:"programs,omitempty"`

	// Maps maps map name to type, decoded from the io.ebpf.maps label.
	// It is populated only for a single-platform image; nil for an
	// image index.
	Maps map[string]string `json:"maps,omitempty"`

	// Layers summarises the image's layer descriptors. It is populated
	// only for a single-platform image; nil for an image index.
	Layers []DescriptorSummary `json:"layers,omitempty"`

	// Manifests summarises the child manifests when the reference
	// resolves to an image index; nil for a single-platform image.
	Manifests []ManifestSummary `json:"manifests,omitempty"`
}

// ManifestSummary describes one child manifest in an image index.
type ManifestSummary struct {
	// Digest is the content digest of the child manifest.
	Digest string `json:"digest"`

	// MediaType is the OCI media type of the child manifest.
	MediaType string `json:"media_type"`

	// Size is the size in bytes of the child manifest.
	Size int64 `json:"size"`

	// Platform is the OS/architecture string of the child manifest
	// (for example "linux/amd64"); empty when the index entry carries
	// no platform.
	Platform string `json:"platform,omitempty"`

	// Programs maps program name to type, decoded from the
	// io.ebpf.programs label of the child image.
	Programs map[string]string `json:"programs,omitempty"`

	// Maps maps map name to type, decoded from the io.ebpf.maps label
	// of the child image.
	Maps map[string]string `json:"maps,omitempty"`

	// Layers summarises the child image's layer descriptors.
	Layers []DescriptorSummary `json:"layers,omitempty"`
}

// DescriptorSummary describes one OCI descriptor relevant to image inspection.
type DescriptorSummary struct {
	// Digest is the content digest of the descriptor.
	Digest string `json:"digest"`

	// MediaType is the OCI media type of the descriptor.
	MediaType string `json:"media_type"`

	// Size is the size in bytes of the descriptor's content.
	Size int64 `json:"size"`
}

// InspectBytecodeImage reads image metadata from a registry without pulling
// bytecode into bpfman's image cache.
func InspectBytecodeImage(ctx context.Context, imageRef string) (ImageInspection, error) {
	ref, err := parseRegistryReference(imageRef)
	if err != nil {
		return ImageInspection{}, err
	}

	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuth(authn.Anonymous),
	}
	desc, err := remote.Get(ref, opts...)
	if err != nil {
		authenticator, found, authErr := credentialStoreForGoContainerRegistry(ctx, ref, slog.Default())
		if authErr != nil {
			return ImageInspection{}, authErr
		}

		if !found {
			return ImageInspection{}, missingCredentialError(
				ref.Context().RegistryStr(),
				fmt.Errorf("failed to inspect image: %w", err),
			)
		}
		opts = []remote.Option{
			remote.WithContext(ctx),
			remote.WithAuth(authenticator),
		}
		desc, err = remote.Get(ref, opts...)
		if err != nil {
			return ImageInspection{}, fmt.Errorf("failed to inspect image: %w", err)
		}
	}

	out := ImageInspection{
		Reference: ref.Name(),
		Digest:    desc.Digest.String(),
		MediaType: string(desc.MediaType),
	}
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		idx, err := desc.ImageIndex()
		if err != nil {
			return ImageInspection{}, err
		}

		manifest, err := idx.IndexManifest()
		if err != nil {
			return ImageInspection{}, fmt.Errorf("failed to read image index manifest: %w", err)
		}

		out.Manifests = make([]ManifestSummary, 0, len(manifest.Manifests))
		for _, child := range manifest.Manifests {
			summary, err := inspectChildManifest(idx, child)
			if err != nil {
				return ImageInspection{}, err
			}

			out.Manifests = append(out.Manifests, summary)
		}
	case types.OCIManifestSchema1, types.DockerManifestSchema2:
		img, err := desc.Image()
		if err != nil {
			return ImageInspection{}, err
		}

		programs, maps, layers, err := inspectImage(img)
		if err != nil {
			return ImageInspection{}, err
		}

		out.Programs = programs
		out.Maps = maps
		out.Layers = layers
	default:
		return ImageInspection{}, fmt.Errorf("unsupported image media type %s", desc.MediaType)
	}
	return out, nil
}

func inspectChildManifest(idx v1.ImageIndex, child v1.Descriptor) (ManifestSummary, error) {
	summary := ManifestSummary{
		Digest:    child.Digest.String(),
		MediaType: string(child.MediaType),
		Size:      child.Size,
	}
	if child.Platform != nil {
		summary.Platform = child.Platform.String()
	}
	img, err := idx.Image(child.Digest)
	if err != nil {
		return ManifestSummary{}, fmt.Errorf("failed to fetch child manifest %s: %w", child.Digest, err)
	}

	programs, maps, layers, err := inspectImage(img)
	if err != nil {
		return ManifestSummary{}, err
	}

	summary.Programs = programs
	summary.Maps = maps
	summary.Layers = layers
	return summary, nil
}

func inspectImage(img v1.Image) (map[string]string, map[string]string, []DescriptorSummary, error) {
	config, err := img.ConfigFile()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read image config: %w", err)
	}

	programs, maps, err := decodeBPFLabels(config.Config.Labels)
	if err != nil {
		return nil, nil, nil, err
	}

	manifest, err := img.Manifest()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read image manifest: %w", err)
	}

	layers := make([]DescriptorSummary, 0, len(manifest.Layers))
	for _, layer := range manifest.Layers {
		layers = append(layers, descriptorSummary(layer))
	}
	return programs, maps, layers, nil
}

func decodeBPFLabels(labels map[string]string) (map[string]string, map[string]string, error) {
	var programs map[string]string
	if progJSON := labels[LabelPrograms]; progJSON != "" {
		programs = make(map[string]string)
		if err := json.Unmarshal([]byte(progJSON), &programs); err != nil {
			return nil, nil, fmt.Errorf("failed to parse %s label: %w", LabelPrograms, err)
		}
	}

	var maps map[string]string
	if mapJSON := labels[LabelMaps]; mapJSON != "" {
		maps = make(map[string]string)
		if err := json.Unmarshal([]byte(mapJSON), &maps); err != nil {
			return nil, nil, fmt.Errorf("failed to parse %s label: %w", LabelMaps, err)
		}
	}
	return programs, maps, nil
}

func descriptorSummary(desc v1.Descriptor) DescriptorSummary {
	return DescriptorSummary{
		Digest:    desc.Digest.String(),
		MediaType: string(desc.MediaType),
		Size:      desc.Size,
	}
}
