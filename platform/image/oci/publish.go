package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/bpfman/bpfman/internal/imagebuild"
)

type publisher struct {
	logger *slog.Logger
}

const sourceDateEpochEnv = "SOURCE_DATE_EPOCH"

var defaultImageTimestamp = time.Unix(0, 0).UTC()

// PublishedImage describes the image reference written to the registry.
type PublishedImage struct {
	// Reference is the tag form of the published image, for example
	// registry/repo:tag.
	Reference string

	// Digest is the content digest of the published image or index.
	Digest string

	// PinnedReference is the digest-pinned form of the reference, for
	// example registry/repo@sha256:...
	PinnedReference string
}

// PublishOption configures native OCI image publishing.
type PublishOption func(*publisher) error

// WithPublishLogger sets the publisher logger.
func WithPublishLogger(logger *slog.Logger) PublishOption {
	return func(p *publisher) error {
		if logger != nil {
			p.logger = logger
		}
		return nil
	}
}

// PublishBytecodeImage publishes an OCI image containing BPF bytecode
// directly, without invoking a container runtime.
func PublishBytecodeImage(ctx context.Context, tag string, plan imagebuild.Plan, opts ...PublishOption) (PublishedImage, error) {
	p := &publisher{logger: slog.Default()}
	for _, opt := range opts {
		if err := opt(p); err != nil {
			return PublishedImage{}, err
		}
	}

	ref, err := parseRegistryReference(tag)
	if err != nil {
		return PublishedImage{}, err
	}

	remoteOpts := []remote.Option{
		remote.WithContext(ctx),
	}
	authenticator, _, err := credentialStoreForGoContainerRegistry(ctx, ref, p.logger)
	if err != nil {
		return PublishedImage{}, err
	}

	remoteOpts = append(remoteOpts, remote.WithAuth(authenticator))

	timestamp, err := imageTimestamp()
	if err != nil {
		return PublishedImage{}, err
	}

	p.logger.Debug("creating bytecode image", "tag", ref.Name(), "platforms", plan.Platforms)
	if len(plan.Platforms) == 0 {
		img, err := bytecodeImage(plan, hostPlatform(), timestamp)
		if err != nil {
			return PublishedImage{}, err
		}

		digest, err := img.Digest()
		if err != nil {
			return PublishedImage{}, fmt.Errorf("failed to digest image %s: %w", tag, err)
		}

		if err := remote.Write(ref, img, remoteOpts...); err != nil {
			return PublishedImage{}, fmt.Errorf("failed to publish image %s: %w", tag, err)
		}
		return publishedImage(ref, digest.String()), nil
	}

	idx, err := bytecodeImageIndex(plan, timestamp)
	if err != nil {
		return PublishedImage{}, err
	}

	digest, err := idx.Digest()
	if err != nil {
		return PublishedImage{}, fmt.Errorf("failed to digest image index %s: %w", tag, err)
	}

	if err := remote.WriteIndex(ref, idx, remoteOpts...); err != nil {
		return PublishedImage{}, fmt.Errorf("failed to publish image index %s: %w", tag, err)
	}
	return publishedImage(ref, digest.String()), nil
}

func publishedImage(ref name.Reference, digest string) PublishedImage {
	return PublishedImage{
		Reference:       ref.Name(),
		Digest:          digest,
		PinnedReference: ref.Context().Name() + "@" + digest,
	}
}

func parseRegistryReference(tag string) (name.Reference, error) {
	ref, err := name.ParseReference(tag)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference: %w", err)
	}

	if isLoopbackRegistry(ref.Context().RegistryStr()) {
		ref, err = name.ParseReference(tag, name.Insecure)
		if err != nil {
			return nil, fmt.Errorf("failed to parse image reference: %w", err)
		}
	}
	return ref, nil
}

func imageTimestamp() (time.Time, error) {
	raw := strings.TrimSpace(os.Getenv(sourceDateEpochEnv))
	if raw == "" {
		return defaultImageTimestamp, nil
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s %q: %w", sourceDateEpochEnv, raw, err)
	}

	if seconds < 0 {
		return time.Time{}, fmt.Errorf("invalid %s %q: timestamp must be non-negative", sourceDateEpochEnv, raw)
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func bytecodeImageIndex(plan imagebuild.Plan, timestamp time.Time) (v1.ImageIndex, error) {
	if len(plan.Platforms) != len(plan.BuildArgs) {
		return nil, fmt.Errorf("platform/build-arg mismatch: %d platforms for %d bytecode inputs", len(plan.Platforms), len(plan.BuildArgs))
	}

	adds := make([]mutate.IndexAddendum, 0, len(plan.BuildArgs))
	for i, platformName := range plan.Platforms {
		platform, err := v1.ParsePlatform(platformName)
		if err != nil {
			return nil, fmt.Errorf("invalid platform %q: %w", platformName, err)
		}

		img, err := bytecodeImageForBuildArg(plan, plan.BuildArgs[i], *platform, timestamp)
		if err != nil {
			return nil, err
		}

		adds = append(adds, mutate.IndexAddendum{
			Add: img,
			Descriptor: v1.Descriptor{
				MediaType: types.OCIManifestSchema1,
				Platform:  platform,
			},
		})
	}
	return mutate.IndexMediaType(mutate.AppendManifests(empty.Index, adds...), types.OCIImageIndex), nil
}

func bytecodeImage(plan imagebuild.Plan, platform v1.Platform, timestamp time.Time) (v1.Image, error) {
	if len(plan.BuildArgs) != 1 {
		return nil, fmt.Errorf("single-architecture image requires exactly one bytecode input, got %d", len(plan.BuildArgs))
	}
	return bytecodeImageForBuildArg(plan, plan.BuildArgs[0], platform, timestamp)
}

func bytecodeImageForBuildArg(plan imagebuild.Plan, buildArg string, platform v1.Platform, timestamp time.Time) (v1.Image, error) {
	_, path, ok := strings.Cut(buildArg, "=")
	if !ok || path == "" {
		return nil, fmt.Errorf("invalid build arg %q", buildArg)
	}

	config, err := configFile(plan.Labels, platform)
	if err != nil {
		return nil, err
	}

	img, err := mutate.ConfigFile(empty.Image, config)
	if err != nil {
		return nil, err
	}

	layer, err := bytecodeLayer(path, timestamp)
	if err != nil {
		return nil, err
	}

	img, err = mutate.Append(img, mutate.Addendum{
		Layer:     layer,
		MediaType: types.OCILayer,
	})
	if err != nil {
		return nil, err
	}

	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)
	img, err = mutate.CreatedAt(img, v1.Time{Time: timestamp})
	if err != nil {
		return nil, fmt.Errorf("failed to set image timestamp: %w", err)
	}
	return img, nil
}

func configFile(info imagebuild.Info, platform v1.Platform) (*v1.ConfigFile, error) {
	programs, maps, err := imagebuild.LabelBuildArgValues(info)
	if err != nil {
		return nil, err
	}
	return &v1.ConfigFile{
		Architecture: platform.Architecture,
		OS:           platform.OS,
		Variant:      platform.Variant,
		RootFS: v1.RootFS{
			Type: "layers",
		},
		Config: v1.Config{
			Labels: map[string]string{
				LabelPrograms: programs,
				LabelMaps:     maps,
			},
		},
	}, nil
}

func bytecodeLayer(path string, timestamp time.Time) (v1.Layer, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open bytecode %s: %w", path, err)
	}

	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat bytecode %s: %w", path, err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("bytecode path %s is a directory", path)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:       filepath.Base(path),
		Mode:       0o644,
		Size:       info.Size(),
		ModTime:    timestamp,
		AccessTime: timestamp,
		ChangeTime: timestamp,
	}); err != nil {
		return nil, fmt.Errorf("failed to write bytecode tar header: %w", err)
	}

	if _, err := io.Copy(tw, file); err != nil {
		_ = tw.Close()
		return nil, fmt.Errorf("failed to write bytecode layer: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close bytecode tar layer: %w", err)
	}

	tarBytes := append([]byte(nil), buf.Bytes()...)
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(tarBytes)), nil
	}, tarball.WithMediaType(types.OCILayer), tarball.WithCompressedCaching)
	if err != nil {
		return nil, fmt.Errorf("failed to create bytecode layer: %w", err)
	}
	return layer, nil
}

func hostPlatform() v1.Platform {
	return v1.Platform{
		OS:           "linux",
		Architecture: runtime.GOARCH,
	}
}
