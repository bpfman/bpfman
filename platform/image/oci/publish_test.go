package oci

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/bpfman/bpfman/internal/imagebuild"
)

func TestPublishBytecodeImagePublishesSingleArchImage(t *testing.T) {
	t.Setenv(sourceDateEpochEnv, "")
	server := newTestRegistry(t)

	bytecode := writeBytecode(t, "xdp_pass.bpf.o", "bytecode")
	ref := registryRef(t, server.URL, "bpfman/xdp-pass:single")
	plan := imagebuild.Plan{
		BuildArgs: []string{"BYTECODE_FILE=" + bytecode},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}

	published, err := PublishBytecodeImage(context.Background(), ref, plan)
	if err != nil {
		t.Fatalf("PublishBytecodeImage returned error: %v", err)
	}

	parsed, err := parseRegistryReference(ref)
	if err != nil {
		t.Fatalf("parseRegistryReference returned error: %v", err)
	}

	if published.Reference != ref {
		t.Fatalf("published Reference = %q, want %q", published.Reference, ref)
	}
	if published.Digest == "" {
		t.Fatal("published Digest is empty")
	}
	if published.PinnedReference != parsed.Context().Name()+"@"+published.Digest {
		t.Fatalf("published PinnedReference = %q, want %q", published.PinnedReference, parsed.Context().Name()+"@"+published.Digest)
	}
	img, err := remote.Image(parsed)
	if err != nil {
		t.Fatalf("remote.Image returned error: %v", err)
	}

	digest, err := img.Digest()
	if err != nil {
		t.Fatalf("Digest returned error: %v", err)
	}

	if published.Digest != digest.String() {
		t.Fatalf("published Digest = %q, want image digest %q", published.Digest, digest.String())
	}
	mt, err := img.MediaType()
	if err != nil {
		t.Fatalf("MediaType returned error: %v", err)
	}

	if mt != types.OCIManifestSchema1 {
		t.Fatalf("image media type = %s, want %s", mt, types.OCIManifestSchema1)
	}

	config, err := img.ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile returned error: %v", err)
	}

	if config.Config.Labels[LabelPrograms] != `{"pass":"xdp"}` {
		t.Fatalf("%s label = %q", LabelPrograms, config.Config.Labels[LabelPrograms])
	}
	if config.Config.Labels[LabelMaps] != `{"xdp_pass_stats_map":"per_cpu_array"}` {
		t.Fatalf("%s label = %q", LabelMaps, config.Config.Labels[LabelMaps])
	}
	if !config.Created.Time.Equal(defaultImageTimestamp) {
		t.Fatalf("config Created = %s, want %s", config.Created.Time, defaultImageTimestamp)
	}

	header, gotContent := readSingleLayerFileAndHeader(t, img)
	gotName := header.Name
	if gotName != "xdp_pass.bpf.o" {
		t.Fatalf("layer file name = %q, want xdp_pass.bpf.o", gotName)
	}
	if gotContent != "bytecode" {
		t.Fatalf("layer file content = %q, want bytecode", gotContent)
	}
	if !header.ModTime.Equal(defaultImageTimestamp) {
		t.Fatalf("layer ModTime = %s, want %s", header.ModTime, defaultImageTimestamp)
	}
}

func TestPublishBytecodeImageHonorsSourceDateEpoch(t *testing.T) {
	t.Setenv(sourceDateEpochEnv, "1700000000")
	wantTimestamp := time.Unix(1700000000, 0).UTC()
	server := newTestRegistry(t)

	bytecode := writeBytecode(t, "xdp_pass.bpf.o", "bytecode")
	ref := registryRef(t, server.URL, "bpfman/xdp-pass:source-date-epoch")
	plan := imagebuild.Plan{
		BuildArgs: []string{"BYTECODE_FILE=" + bytecode},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}

	if _, err := PublishBytecodeImage(context.Background(), ref, plan); err != nil {
		t.Fatalf("PublishBytecodeImage returned error: %v", err)
	}

	parsed, err := parseRegistryReference(ref)
	if err != nil {
		t.Fatalf("parseRegistryReference returned error: %v", err)
	}

	img, err := remote.Image(parsed)
	if err != nil {
		t.Fatalf("remote.Image returned error: %v", err)
	}

	config, err := img.ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile returned error: %v", err)
	}

	if !config.Created.Time.Equal(wantTimestamp) {
		t.Fatalf("config Created = %s, want %s", config.Created.Time, wantTimestamp)
	}
	header, _ := readSingleLayerFileAndHeader(t, img)
	if !header.ModTime.Equal(wantTimestamp) {
		t.Fatalf("layer ModTime = %s, want %s", header.ModTime, wantTimestamp)
	}
}

func TestPublishBytecodeImageRejectsInvalidSourceDateEpoch(t *testing.T) {
	t.Setenv(sourceDateEpochEnv, "not-a-timestamp")
	server := newTestRegistry(t)

	bytecode := writeBytecode(t, "xdp_pass.bpf.o", "bytecode")
	ref := registryRef(t, server.URL, "bpfman/xdp-pass:bad-source-date-epoch")
	plan := imagebuild.Plan{
		BuildArgs: []string{"BYTECODE_FILE=" + bytecode},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}

	_, err := PublishBytecodeImage(context.Background(), ref, plan)
	requireErrorContains(t, err, "invalid SOURCE_DATE_EPOCH")
}

func TestInspectBytecodeImageInspectsSingleArchImage(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)

	bytecode := writeBytecode(t, "xdp_pass.bpf.o", "bytecode")
	ref := registryRef(t, server.URL, "bpfman/xdp-pass:inspect-single")
	plan := imagebuild.Plan{
		BuildArgs: []string{"BYTECODE_FILE=" + bytecode},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}

	if _, err := PublishBytecodeImage(context.Background(), ref, plan); err != nil {
		t.Fatalf("PublishBytecodeImage returned error: %v", err)
	}

	inspection, err := InspectBytecodeImage(context.Background(), ref)
	if err != nil {
		t.Fatalf("InspectBytecodeImage returned error: %v", err)
	}

	if inspection.Reference != ref {
		t.Fatalf("Reference = %q, want %q", inspection.Reference, ref)
	}
	if inspection.MediaType != string(types.OCIManifestSchema1) {
		t.Fatalf("MediaType = %q, want %q", inspection.MediaType, types.OCIManifestSchema1)
	}
	if inspection.Digest == "" {
		t.Fatal("Digest is empty")
	}
	assertStringMapEqual(t, inspection.Programs, map[string]string{"pass": "xdp"})
	assertStringMapEqual(t, inspection.Maps, map[string]string{"xdp_pass_stats_map": "per_cpu_array"})
	if len(inspection.Layers) != 1 {
		t.Fatalf("len(Layers) = %d, want 1", len(inspection.Layers))
	}
	if inspection.Layers[0].MediaType != string(types.OCILayer) {
		t.Fatalf("layer media type = %q, want %q", inspection.Layers[0].MediaType, types.OCILayer)
	}
	if len(inspection.Manifests) != 0 {
		t.Fatalf("len(Manifests) = %d, want 0", len(inspection.Manifests))
	}
}

func TestPublishBytecodeImagePublishesMultiArchIndex(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)

	amd64 := writeBytecode(t, "xdp_pass_amd64.bpf.o", "amd64")
	arm64 := writeBytecode(t, "xdp_pass_arm64.bpf.o", "arm64")
	ref := registryRef(t, server.URL, "bpfman/xdp-pass:multi")
	plan := imagebuild.Plan{
		Platforms: []string{"linux/amd64", "linux/arm64"},
		BuildArgs: []string{
			"BC_AMD64_EL=" + amd64,
			"BC_ARM64_EL=" + arm64,
		},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}

	published, err := PublishBytecodeImage(context.Background(), ref, plan)
	if err != nil {
		t.Fatalf("PublishBytecodeImage returned error: %v", err)
	}

	parsed, err := parseRegistryReference(ref)
	if err != nil {
		t.Fatalf("parseRegistryReference returned error: %v", err)
	}

	if published.Reference != ref {
		t.Fatalf("published Reference = %q, want %q", published.Reference, ref)
	}
	if published.Digest == "" {
		t.Fatal("published Digest is empty")
	}
	if published.PinnedReference != parsed.Context().Name()+"@"+published.Digest {
		t.Fatalf("published PinnedReference = %q, want %q", published.PinnedReference, parsed.Context().Name()+"@"+published.Digest)
	}
	desc, err := remote.Get(parsed)
	if err != nil {
		t.Fatalf("remote.Get returned error: %v", err)
	}

	if published.Digest != desc.Digest.String() {
		t.Fatalf("published Digest = %q, want index digest %q", published.Digest, desc.Digest.String())
	}
	if desc.MediaType != types.OCIImageIndex {
		t.Fatalf("descriptor media type = %s, want %s", desc.MediaType, types.OCIImageIndex)
	}
	idx, err := desc.ImageIndex()
	if err != nil {
		t.Fatalf("ImageIndex returned error: %v", err)
	}

	manifest, err := idx.IndexManifest()
	if err != nil {
		t.Fatalf("IndexManifest returned error: %v", err)
	}

	if len(manifest.Manifests) != 2 {
		t.Fatalf("index manifests = %d, want 2", len(manifest.Manifests))
	}
	if got := manifest.Manifests[0].Platform.String(); got != "linux/amd64" {
		t.Fatalf("manifest[0] platform = %q, want linux/amd64", got)
	}
	if got := manifest.Manifests[1].Platform.String(); got != "linux/arm64" {
		t.Fatalf("manifest[1] platform = %q, want linux/arm64", got)
	}
	for i, wantContent := range []string{"amd64", "arm64"} {
		img, err := idx.Image(manifest.Manifests[i].Digest)
		if err != nil {
			t.Fatalf("index image %d returned error: %v", i, err)
		}

		_, gotContent := readSingleLayerFile(t, img)
		if gotContent != wantContent {
			t.Fatalf("index image %d layer content = %q, want %q", i, gotContent, wantContent)
		}
	}
}

func TestInspectBytecodeImageInspectsMultiArchIndex(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)

	amd64 := writeBytecode(t, "xdp_pass_amd64.bpf.o", "amd64")
	arm64 := writeBytecode(t, "xdp_pass_arm64.bpf.o", "arm64")
	ref := registryRef(t, server.URL, "bpfman/xdp-pass:inspect-multi")
	plan := imagebuild.Plan{
		Platforms: []string{"linux/amd64", "linux/arm64"},
		BuildArgs: []string{
			"BC_AMD64_EL=" + amd64,
			"BC_ARM64_EL=" + arm64,
		},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}

	if _, err := PublishBytecodeImage(context.Background(), ref, plan); err != nil {
		t.Fatalf("PublishBytecodeImage returned error: %v", err)
	}

	inspection, err := InspectBytecodeImage(context.Background(), ref)
	if err != nil {
		t.Fatalf("InspectBytecodeImage returned error: %v", err)
	}

	if inspection.MediaType != string(types.OCIImageIndex) {
		t.Fatalf("MediaType = %q, want %q", inspection.MediaType, types.OCIImageIndex)
	}
	if len(inspection.Manifests) != 2 {
		t.Fatalf("len(Manifests) = %d, want 2", len(inspection.Manifests))
	}
	for i, platform := range []string{"linux/amd64", "linux/arm64"} {
		manifest := inspection.Manifests[i]
		if manifest.Platform != platform {
			t.Fatalf("manifest[%d].Platform = %q, want %q", i, manifest.Platform, platform)
		}
		assertStringMapEqual(t, manifest.Programs, map[string]string{"pass": "xdp"})
		assertStringMapEqual(t, manifest.Maps, map[string]string{"xdp_pass_stats_map": "per_cpu_array"})
		if len(manifest.Layers) != 1 {
			t.Fatalf("len(manifest[%d].Layers) = %d, want 1", i, len(manifest.Layers))
		}
	}
}

func writeBytecode(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func assertStringMapEqual(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("got[%q] = %q, want %q\ngot:  %#v\nwant: %#v", key, got[key], wantValue, got, want)
		}
	}
}

func registryRef(t *testing.T, serverURL, image string) string {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	return u.Host + "/" + image
}

func readSingleLayerFile(t *testing.T, img v1.Image) (string, string) {
	t.Helper()
	header, content := readSingleLayerFileAndHeader(t, img)
	return header.Name, content
}

func readSingleLayerFileAndHeader(t *testing.T, img v1.Image) (*tar.Header, string) {
	t.Helper()
	layers, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers returned error: %v", err)
	}

	if len(layers) != 1 {
		t.Fatalf("layers = %d, want 1", len(layers))
	}
	rc, err := layers[0].Compressed()
	if err != nil {
		t.Fatalf("Compressed returned error: %v", err)
	}

	defer rc.Close()
	gz, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatalf("gzip.NewReader returned error: %v", err)
	}

	defer gz.Close()
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("tar.Next returned error: %v", err)
	}

	bytes, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}

	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("second tar.Next error = %v, want EOF", err)
	}
	return header, string(bytes)
}
