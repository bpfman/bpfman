package oci

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/internal/imagebuild"
	"github.com/bpfman/bpfman/platform"
)

func TestIsLoopbackRegistry(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"localhost":         true,
		"localhost:5000":    true,
		"127.0.0.1:5000":    true,
		"[::1]:5000":        true,
		"quay.io":           false,
		"example.test:5000": false,
	}
	for registry, want := range tests {
		if got := isLoopbackRegistry(registry); got != want {
			t.Fatalf("isLoopbackRegistry(%q) = %v, want %v", registry, got, want)
		}
	}
}

func TestInspectBytecodeImageMissingTagReturnsError(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/missing:tag")

	_, err := InspectBytecodeImage(context.Background(), ref)
	requireErrorContains(t, err, "failed to inspect image")
}

func TestInspectBytecodeImageRejectsMalformedProgramLabel(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/bad-label:latest")
	writeTestImage(t, ref, map[string]string{
		LabelPrograms: "not-json",
		LabelMaps:     `{"xdp_pass_stats_map":"per_cpu_array"}`,
	}, testBytecodeObject(t))

	_, err := InspectBytecodeImage(context.Background(), ref)
	requireErrorContains(t, err, "failed to parse io.ebpf.programs label")
}

func TestPullerPullBareReferenceDefaultsToLatest(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	tagged := registryRef(t, server.URL, "bpfman/xdp-pass:latest")
	bare := registryRef(t, server.URL, "bpfman/xdp-pass")
	plan := imagebuild.Plan{
		BuildArgs: []string{"BYTECODE_FILE=" + testBytecodeObject(t)},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}
	if _, err := PublishBytecodeImage(context.Background(), tagged, plan); err != nil {
		t.Fatalf("PublishBytecodeImage returned error: %v", err)
	}

	puller := newTestPuller(t)
	pulled, err := puller.Pull(context.Background(), platform.ImageRef{
		URL:        bare,
		PullPolicy: bpfman.PullAlways,
	})
	if err != nil {
		t.Fatalf("Pull returned error: %v", err)
	}

	if pulled.URL != bare {
		t.Fatalf("URL = %q, want %q", pulled.URL, bare)
	}
	if pulled.Digest == "" {
		t.Fatal("Digest is empty")
	}
	if pulled.PullPolicy != bpfman.PullAlways {
		t.Fatalf("PullPolicy = %s, want %s", pulled.PullPolicy, bpfman.PullAlways)
	}
	assertStringMapEqual(t, pulled.Programs, map[string]string{"pass": "xdp"})
	assertStringMapEqual(t, pulled.Maps, map[string]string{"xdp_pass_stats_map": "per_cpu_array"})
	if _, err := os.Stat(pulled.ObjectPath); err != nil {
		t.Fatalf("pulled object does not exist at %s: %v", pulled.ObjectPath, err)
	}
}

func TestPullerPullMissingTagReturnsResolveError(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	puller := newTestPuller(t)
	ref := registryRef(t, server.URL, "bpfman/missing:tag")

	_, err := puller.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	})
	requireErrorContains(t, err, "failed to resolve image")
}

func TestPullerPullIndexWithoutHostPlatformReturnsError(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/arm64-only:latest")
	plan := imagebuild.Plan{
		Platforms: []string{"linux/arm64"},
		BuildArgs: []string{"BC_ARM64_EL=" + writeBytecode(t, "arm64.bpf.o", "bytecode")},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}
	if _, err := PublishBytecodeImage(context.Background(), ref, plan); err != nil {
		t.Fatalf("PublishBytecodeImage returned error: %v", err)
	}

	puller := newTestPuller(t)
	_, err := puller.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	})
	requireErrorContains(t, err, "no linux/amd64 manifest found in image index")
}

func TestPullerPullIndexSelectsForcedHostPlatform(t *testing.T) {
	t.Setenv("GOARCH", "arm64")
	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/multi-arch:latest")
	bytecode := testBytecodeObject(t)
	plan := imagebuild.Plan{
		Platforms: []string{"linux/amd64", "linux/arm64"},
		BuildArgs: []string{
			"BC_AMD64_EL=" + bytecode,
			"BC_ARM64_EL=" + bytecode,
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

	arm64Digest := ""
	for _, manifest := range inspection.Manifests {
		if manifest.Platform == "linux/arm64" {
			arm64Digest = manifest.Digest
			break
		}
	}
	if arm64Digest == "" {
		t.Fatalf("inspection did not include a linux/arm64 child: %#v", inspection.Manifests)
	}

	puller := newTestPuller(t)
	pulled, err := puller.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	})
	if err != nil {
		t.Fatalf("Pull returned error: %v", err)
	}

	if pulled.Digest != arm64Digest {
		t.Fatalf("Digest = %q, want arm64 child digest %q", pulled.Digest, arm64Digest)
	}
	assertStringMapEqual(t, pulled.Programs, map[string]string{"pass": "xdp"})
	assertStringMapEqual(t, pulled.Maps, map[string]string{"xdp_pass_stats_map": "per_cpu_array"})
	if _, err := os.Stat(pulled.ObjectPath); err != nil {
		t.Fatalf("pulled object does not exist at %s: %v", pulled.ObjectPath, err)
	}
}

func TestPullerPullImageWithNoLayersReturnsError(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/no-layers:latest")
	writeTestImage(t, ref, map[string]string{
		LabelPrograms: `{"pass":"xdp"}`,
		LabelMaps:     `{"xdp_pass_stats_map":"per_cpu_array"}`,
	}, "")

	puller := newTestPuller(t)
	_, err := puller.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	})
	requireErrorContains(t, err, "image has no layers")
}

func TestPullerPullRejectsMalformedProgramLabel(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/bad-label:latest")
	writeTestImage(t, ref, map[string]string{
		LabelPrograms: "not-json",
		LabelMaps:     `{"xdp_pass_stats_map":"per_cpu_array"}`,
	}, testBytecodeObject(t))

	puller := newTestPuller(t)
	_, err := puller.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	})
	requireErrorContains(t, err, "failed to extract labels", "failed to parse io.ebpf.programs label")
}

func TestPullerPassesExplicitAuthToVerifier(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/authenticated:latest")
	writeTestImage(t, ref, map[string]string{
		LabelPrograms: `{"pass":"xdp"}`,
		LabelMaps:     `{"xdp_pass_stats_map":"per_cpu_array"}`,
	}, testBytecodeObject(t))

	cache, err := fs.NewImageCache(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatalf("NewImageCache returned error: %v", err)
	}

	ensured, err := fs.EnsureCache(cache)
	if err != nil {
		t.Fatalf("EnsureCache returned error: %v", err)
	}

	verifier := &capturingVerifier{policy: "auth"}
	puller, err := NewPuller(ensured, WithVerifier(verifier))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}

	auth := &platform.ImageAuth{
		Username: "user",
		Password: "pass",
	}
	pulled, err := puller.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
		Auth:       auth,
	})
	if err != nil {
		t.Fatalf("Pull returned error: %v", err)
	}

	if verifier.request.ImageRef != ref+"@"+pulled.Digest {
		t.Fatalf("verifier image ref = %q, want %q", verifier.request.ImageRef, ref+"@"+pulled.Digest)
	}
	if verifier.request.Auth == nil {
		t.Fatal("verifier auth is nil")
	}
	if verifier.request.Auth.Username != auth.Username {
		t.Fatalf("verifier auth username = %q, want %q", verifier.request.Auth.Username, auth.Username)
	}
	if verifier.request.Auth.Password != auth.Password {
		t.Fatalf("verifier auth password = %q, want %q", verifier.request.Auth.Password, auth.Password)
	}
}

// TestPullerVerifiesCachedImageAgainstCurrentPolicy pulls an image
// under a permissive verifier, then pulls it again through a puller
// whose verifier refuses everything. Tightening the signing policy
// must reject the cached image; serving it from cache without asking
// the verifier leaves the old policy in force forever.
func TestPullerVerifiesCachedImageAgainstCurrentPolicy(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/unsigned:latest")
	writeTestImage(t, ref, map[string]string{
		LabelPrograms: `{"pass":"xdp"}`,
		LabelMaps:     `{"xdp_pass_stats_map":"per_cpu_array"}`,
	}, testBytecodeObject(t))

	cacheDir := filepath.Join(t.TempDir(), "cache")

	// First pull: unsigned images are allowed, so the image is
	// admitted and cached.
	permissive, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(&capturingVerifier{policy: "permissive"}))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}
	if _, err := permissive.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	}); err != nil {
		t.Fatalf("first Pull returned error: %v", err)
	}

	// Policy tightened: unsigned images are now refused.
	strict := &refusingVerifier{}
	tightened, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(strict))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}

	_, err = tightened.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullIfNotPresent,
	})
	if strict.calls == 0 {
		t.Error("verifier was never consulted for the cached image")
	}
	if err == nil {
		t.Fatal("Pull returned no error, want the cached image refused by the tightened policy")
	}
}

// TestPullerServesCachedImageWithoutRepeatingVerification pulls twice
// under one unchanged policy. The second load must come from cache
// without asking the verifier again, which is what keeps sigstore off
// the load path.
func TestPullerServesCachedImageWithoutRepeatingVerification(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/steady:latest")
	writeTestImage(t, ref, map[string]string{
		LabelPrograms: `{"pass":"xdp"}`,
		LabelMaps:     `{"xdp_pass_stats_map":"per_cpu_array"}`,
	}, testBytecodeObject(t))

	cacheDir := filepath.Join(t.TempDir(), "cache")
	verifier := &capturingVerifier{policy: "steady"}

	for i := range 2 {
		puller, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(verifier))
		if err != nil {
			t.Fatalf("NewPuller returned error: %v", err)
		}

		policy := bpfman.PullIfNotPresent
		if i == 0 {
			policy = bpfman.PullAlways
		}
		if _, err := puller.Pull(context.Background(), platform.ImageRef{
			URL:        ref,
			PullPolicy: policy,
		}); err != nil {
			t.Fatalf("Pull %d returned error: %v", i, err)
		}
	}

	if verifier.calls != 1 {
		t.Fatalf("verifier calls = %d, want 1 (the initial pull)", verifier.calls)
	}
}

// TestPullerReverifiesTheDigestThatWasSigned checks a cached
// multi-platform image against the index digest, which is what cosign
// signs. The cache also holds the selected platform child digest, and
// verifying that instead would reject a properly signed image.
func TestPullerReverifiesTheDigestThatWasSigned(t *testing.T) {
	t.Setenv("GOARCH", "arm64")

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/multi-arch-policy:latest")
	bytecode := testBytecodeObject(t)
	plan := imagebuild.Plan{
		Platforms: []string{"linux/amd64", "linux/arm64"},
		BuildArgs: []string{
			"BC_AMD64_EL=" + bytecode,
			"BC_ARM64_EL=" + bytecode,
		},
		Labels: imagebuild.Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}
	if _, err := PublishBytecodeImage(context.Background(), ref, plan); err != nil {
		t.Fatalf("PublishBytecodeImage returned error: %v", err)
	}

	cacheDir := filepath.Join(t.TempDir(), "cache")

	initial := &capturingVerifier{policy: "initial"}
	first, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(initial))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}
	pulled, err := first.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	})
	if err != nil {
		t.Fatalf("first Pull returned error: %v", err)
	}
	if initial.request.ImageRef == ref+"@"+pulled.Digest {
		t.Fatalf("verifier was asked about the platform child digest %q, so this test proves nothing", pulled.Digest)
	}

	tightened := &capturingVerifier{policy: "tightened"}
	second, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(tightened))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}
	if _, err := second.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullIfNotPresent,
	}); err != nil {
		t.Fatalf("second Pull returned error: %v", err)
	}

	if tightened.request.ImageRef != initial.request.ImageRef {
		t.Fatalf("re-verified %q, want the originally verified %q",
			tightened.request.ImageRef, initial.request.ImageRef)
	}
}

// TestNewPullerRejectsVerifierWithoutPolicyID refuses a verifier that
// cannot name its policy. The cache reserves the empty policy for
// images admitted by no verifier, so such a verifier could never
// record an entry it would later recognise as its own.
func TestNewPullerRejectsVerifierWithoutPolicyID(t *testing.T) {
	t.Parallel()

	cacheDir := filepath.Join(t.TempDir(), "cache")
	_, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(&capturingVerifier{policy: ""}))
	requireErrorContains(t, err, "empty policy id")
}

// TestPullerReverifiedEntryBecomesReusable checks that the record
// written after a policy change is one the same policy recognises: the
// load following the re-verification must come from cache untouched,
// including under PullNever, which cannot reach a registry at all.
func TestPullerReverifiedEntryBecomesReusable(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/reusable:latest")
	writeTestImage(t, ref, map[string]string{
		LabelPrograms: `{"pass":"xdp"}`,
		LabelMaps:     `{"xdp_pass_stats_map":"per_cpu_array"}`,
	}, testBytecodeObject(t))

	cacheDir := filepath.Join(t.TempDir(), "cache")

	initial, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(&capturingVerifier{policy: "initial"}))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}
	if _, err := initial.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	}); err != nil {
		t.Fatalf("first Pull returned error: %v", err)
	}

	// The policy changes, so this load verifies the cached image
	// again and records the new decision against it.
	tightened := &capturingVerifier{policy: "tightened"}
	for i := range 2 {
		puller, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(tightened))
		if err != nil {
			t.Fatalf("NewPuller returned error: %v", err)
		}
		if _, err := puller.Pull(context.Background(), platform.ImageRef{
			URL:        ref,
			PullPolicy: bpfman.PullIfNotPresent,
		}); err != nil {
			t.Fatalf("Pull %d under the new policy returned error: %v", i, err)
		}
	}

	if tightened.calls != 1 {
		t.Fatalf("verifier calls = %d, want 1 (the load that saw the policy change)", tightened.calls)
	}

	// PullNever refuses an entry it cannot judge, so serving one
	// proves the entry counts as admitted under this policy.
	offline, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(tightened))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}
	if _, err := offline.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullNever,
	}); err != nil {
		t.Fatalf("PullNever returned error: %v", err)
	}
	if tightened.calls != 1 {
		t.Fatalf("verifier calls = %d after PullNever, want 1", tightened.calls)
	}
}

// TestPullerDoesNotAdmitEntryCachedWithoutAVerifier loads an image
// cached with no verifier at all through a verifier that has one, and
// checks the image is put through the policy rather than served.
func TestPullerDoesNotAdmitEntryCachedWithoutAVerifier(t *testing.T) {
	t.Parallel()

	server := newTestRegistry(t)
	ref := registryRef(t, server.URL, "bpfman/unrecorded:latest")
	writeTestImage(t, ref, map[string]string{
		LabelPrograms: `{"pass":"xdp"}`,
		LabelMaps:     `{"xdp_pass_stats_map":"per_cpu_array"}`,
	}, testBytecodeObject(t))

	cacheDir := filepath.Join(t.TempDir(), "cache")

	unverified, err := NewPuller(ensuredCache(t, cacheDir))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}
	if _, err := unverified.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullAlways,
	}); err != nil {
		t.Fatalf("first Pull returned error: %v", err)
	}

	guarded := &capturingVerifier{policy: "guarded"}
	puller, err := NewPuller(ensuredCache(t, cacheDir), WithVerifier(guarded))
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}
	if _, err := puller.Pull(context.Background(), platform.ImageRef{
		URL:        ref,
		PullPolicy: bpfman.PullIfNotPresent,
	}); err != nil {
		t.Fatalf("second Pull returned error: %v", err)
	}

	if guarded.calls == 0 {
		t.Fatal("verifier was never consulted for an entry it had not admitted")
	}
}

func ensuredCache(t *testing.T, dir string) fs.EnsuredImageCache {
	t.Helper()

	cache, err := fs.NewImageCache(dir)
	if err != nil {
		t.Fatalf("NewImageCache returned error: %v", err)
	}

	ensured, err := fs.EnsureCache(cache)
	if err != nil {
		t.Fatalf("EnsureCache returned error: %v", err)
	}
	return ensured
}

// refusingVerifier stands in for a signing policy that rejects every
// image, and counts how often it is asked.
type refusingVerifier struct {
	calls int
}

func (v *refusingVerifier) Verify(_ context.Context, req platform.SignatureVerificationRequest) (platform.SignatureVerification, error) {
	v.calls++
	return platform.SignatureVerification{}, fmt.Errorf("image %s has no signatures and unsigned images are not allowed", req.ImageRef)
}

func (v *refusingVerifier) PolicyID() string {
	return "refusing"
}

type capturingVerifier struct {
	request platform.SignatureVerificationRequest
	calls   int
	policy  string
}

func (v *capturingVerifier) Verify(_ context.Context, req platform.SignatureVerificationRequest) (platform.SignatureVerification, error) {
	v.request = req
	v.calls++
	return platform.SignatureVerification{
		Status: platform.SignatureVerificationDisabled,
	}, nil
}

// PolicyID returns the configured policy verbatim, empty string
// included, so tests can exercise a verifier that does not name its
// policy.
func (v *capturingVerifier) PolicyID() string {
	return v.policy
}

func newTestRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(registry.New(
		registry.Logger(log.New(io.Discard, "", 0)),
	))
	t.Cleanup(server.Close)
	return server
}

func newTestPuller(t *testing.T) platform.ImagePuller {
	t.Helper()
	cache, err := fs.NewImageCache(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatalf("NewImageCache returned error: %v", err)
	}

	ensured, err := fs.EnsureCache(cache)
	if err != nil {
		t.Fatalf("EnsureCache returned error: %v", err)
	}

	puller, err := NewPuller(ensured)
	if err != nil {
		t.Fatalf("NewPuller returned error: %v", err)
	}
	return puller
}

func testBytecodeObject(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "e2e", "testdata", "bpf", "xdp_pass.bpf.o")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("test bytecode object is unavailable at %s: %v", path, err)
	}
	return path
}

func writeTestImage(t *testing.T, ref string, labels map[string]string, bytecodePath string) {
	t.Helper()
	img, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{
		Architecture: "amd64",
		OS:           "linux",
		RootFS: v1.RootFS{
			Type: "layers",
		},
		Config: v1.Config{
			Labels: labels,
		},
	})
	if err != nil {
		t.Fatalf("ConfigFile returned error: %v", err)
	}

	if bytecodePath != "" {
		layer, err := bytecodeLayer(bytecodePath, defaultImageTimestamp)
		if err != nil {
			t.Fatalf("bytecodeLayer returned error: %v", err)
		}
		img, err = mutate.Append(img, mutate.Addendum{
			Layer:     layer,
			MediaType: types.OCILayer,
		})
		if err != nil {
			t.Fatalf("Append returned error: %v", err)
		}
	}

	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)
	parsed, err := parseRegistryReference(ref)
	if err != nil {
		t.Fatalf("parseRegistryReference returned error: %v", err)
	}

	if err := remote.Write(parsed, img); err != nil {
		t.Fatalf("remote.Write returned error: %v", err)
	}
}

func requireErrorContains(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error is nil, want message containing %q", strings.Join(parts, ", "))
	}
	message := err.Error()
	for _, part := range parts {
		if !strings.Contains(message, part) {
			t.Fatalf("error = %q, want substring %q", message, part)
		}
	}
}
