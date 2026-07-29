package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/platform"
)

const (
	// LabelPrograms is the OCI label containing program metadata.
	LabelPrograms = "io.ebpf.programs"

	// LabelMaps is the OCI label containing map metadata.
	LabelMaps = "io.ebpf.maps"
)

// cachedMetadata stores image metadata in the cache directory.
type cachedMetadata struct {
	Digest   string            `json:"digest"`
	Programs map[string]string `json:"programs,omitempty"`
	Maps     map[string]string `json:"maps,omitempty"`
	PulledAt time.Time         `json:"pulled_at"`

	// Policy is the SignatureVerifier.PolicyID that admitted this
	// entry. Empty on entries cached before the policy was
	// recorded, or with no verifier configured, which no policy
	// matches, so those entries are pulled again on the next hit.
	Policy string `json:"policy,omitempty"`

	// VerifiedDigest is the image the verifier was asked about. For
	// a multi-platform image that is the index digest, which cosign
	// signs, and so differs from Digest above once a platform child
	// has been selected.
	VerifiedDigest string `json:"verified_digest,omitempty"`
}

// puller implements ImagePuller using ORAS for OCI registry access.
type puller struct {
	cache    fs.EnsuredImageCache
	logger   *slog.Logger
	verifier platform.SignatureVerifier
}

// Option configures a puller.
type Option func(*puller) error

// WithLogger sets the logger.
func WithLogger(logger *slog.Logger) Option {
	return func(p *puller) error {
		p.logger = logger
		return nil
	}
}

// WithVerifier sets the signature verifier.
// If not set, no signature verification is performed.
func WithVerifier(v platform.SignatureVerifier) Option {
	return func(p *puller) error {
		p.verifier = v
		return nil
	}
}

// NewPuller creates a new OCI image puller.
// The cache parameter must be an EnsuredImageCache obtained via EnsureCache(),
// which proves the cache directory exists.
func NewPuller(cache fs.EnsuredImageCache, opts ...Option) (platform.ImagePuller, error) {
	if !cache.Valid() {
		return nil, fmt.Errorf("invalid image cache")
	}

	p := &puller{
		cache:  cache,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, err
		}
	}

	// The cache reserves the empty policy for images admitted by no
	// verifier, so a verifier answering with it could never record a
	// reusable entry. Refuse here rather than degrade quietly later.
	if p.verifier != nil && p.verifier.PolicyID() == "" {
		return nil, fmt.Errorf("signature verifier reports an empty policy id")
	}

	p.logger.Debug("initialising OCI puller", "cache_dir", p.cache.Root())

	return p, nil
}

// Pull downloads an image and returns the extracted bytecode.
func (p *puller) Pull(ctx context.Context, ref platform.ImageRef) (platform.PulledImage, error) {
	logger := p.logger.With("url", ref.URL, "policy", ref.PullPolicy.String())
	logger.Info("pulling OCI image")

	// Compute cache key from URL
	cacheKey := p.cache.CacheKey(ref.URL)
	logger = logger.With("cache_key", cacheKey)

	// Check cache based on pull policy
	if ref.PullPolicy != bpfman.PullAlways {
		if cached, meta, ok := p.checkCache(cacheKey, ref, logger); ok {
			serve, err := p.cachedEntryAdmitted(ctx, cacheKey, ref, meta, logger)
			if err != nil {
				return platform.PulledImage{}, err
			}
			if serve {
				logger.Info("using cached image", "digest", cached.Digest)
				return cached, nil
			}
			// The entry cannot be judged against the current
			// policy without asking the registry, so fall
			// through and pull it again.
		}

		if ref.PullPolicy == bpfman.PullNever {
			logger.Error("image not in cache and pull policy is Never")
			return platform.PulledImage{}, fmt.Errorf("image %s not in cache and pull policy is Never", ref.URL)
		}
	}

	logger.Debug("pulling image from registry")

	// Parse the reference
	repo, err := remote.NewRepository(ref.URL)
	if err != nil {
		logger.Error("failed to parse image reference", "error", err)
		return platform.PulledImage{}, fmt.Errorf("failed to parse image reference: %w", err)
	}

	if isLoopbackRegistry(repo.Reference.Registry) {
		repo.PlainHTTP = true
	}

	// Set up authentication
	if err := p.configureAuth(repo, ref.Auth, logger); err != nil {
		return platform.PulledImage{}, err
	}

	logger.Debug("resolving image manifest")

	// Resolve the manifest descriptor
	desc, err := repo.Resolve(ctx, repo.Reference.ReferenceOrDefault())
	if err != nil {
		logger.Error("failed to resolve image", "error", err)
		resolveErr := fmt.Errorf("failed to resolve image: %w", err)
		if !ref.Auth.Complete() && !registryCredentialsFound(ctx, repo.Reference.Registry, logger) {
			resolveErr = missingCredentialError(repo.Reference.Registry, resolveErr)
		}
		return platform.PulledImage{}, resolveErr
	}

	logger.Info("image resolved", "digest", desc.Digest.String(), "media_type", desc.MediaType)

	// Verify image signature if a verifier is configured. Both of
	// these go into the cache entry below, so a later policy change
	// can be detected on a cache hit.

	// admittedBy is the policy that accepted the image.
	var admittedBy string

	// verifiedDigest is the image the verifier was asked about.
	var verifiedDigest string
	if p.verifier != nil {
		// Use the resolved digest for verification to ensure we verify what we pull
		verifyRef := ref.URL
		if desc.Digest != "" {
			// Append digest to ensure we verify the exact image
			verifyRef = ref.URL + "@" + desc.Digest.String()
		}
		verification, err := p.verifier.Verify(ctx, platform.SignatureVerificationRequest{
			ImageRef: verifyRef,
			Auth:     ref.Auth,
		})
		if err != nil {
			logger.Error("image signature verification failed", "error", err)
			return platform.PulledImage{}, fmt.Errorf("signature verification failed: %w", err)
		}

		admittedBy = p.verifier.PolicyID()
		verifiedDigest = desc.Digest.String()

		switch verification.Status {
		case platform.SignatureVerificationVerified:
			logger.Info("image signature verified")
		case platform.SignatureVerificationUnsignedAccepted:
			logger.Info("unsigned image accepted by policy")
		case platform.SignatureVerificationDisabled:
			logger.Debug("image signature verification disabled")
		default:
			logger.Info("image signature policy accepted", "status", verification.Status)
		}
	}

	// Handle OCI image index (multi-platform manifest list)
	manifestDesc := desc
	if desc.MediaType == "application/vnd.oci.image.index.v1+json" ||
		desc.MediaType == "application/vnd.docker.distribution.manifest.list.v2+json" {
		logger.Debug("image is a manifest list, selecting platform")
		platformDesc, err := p.selectPlatform(ctx, repo, desc, logger)
		if err != nil {
			return platform.PulledImage{}, err
		}

		manifestDesc = platformDesc
		logger.Info("selected platform manifest", "digest", manifestDesc.Digest.String())
	}

	// Fetch the manifest
	rc, err := repo.Manifests().Fetch(ctx, manifestDesc)
	if err != nil {
		logger.Error("failed to fetch manifest", "error", err)
		return platform.PulledImage{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}

	manifestContent, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Parse manifest to find layers and config
	var manifest struct {
		Config ocispec.Descriptor `json:"config"`
		Layers []struct {
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
			MediaType string `json:"mediaType"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to parse manifest: %w", err)
	}

	logger.Info("manifest parsed", "layers", len(manifest.Layers))

	if len(manifest.Layers) == 0 {
		return platform.PulledImage{}, fmt.Errorf("image has no layers")
	}

	// Extract labels from config
	programs, maps, err := p.extractLabels(ctx, repo, manifest.Config, logger)
	if err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to extract labels: %w", err)
	}

	logger.Debug("extracted image labels", "programs", programs, "maps", maps)

	// Fetch the first layer (should contain the bytecode)
	layer := manifest.Layers[0]
	logger.Info("fetching bytecode layer", "digest", layer.Digest, "size", layer.Size, "media_type", layer.MediaType)

	layerDesc := ocispec.Descriptor{
		MediaType: layer.MediaType,
		Digest:    digest.Digest(layer.Digest),
		Size:      layer.Size,
	}
	layerRC, err := repo.Blobs().Fetch(ctx, layerDesc)
	if err != nil {
		logger.Error("failed to fetch layer", "error", err)
		return platform.PulledImage{}, fmt.Errorf("failed to fetch layer: %w", err)
	}

	layerContent, err := io.ReadAll(layerRC)
	layerRC.Close()
	if err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to read layer: %w", err)
	}

	logger.Info("layer fetched", "size", len(layerContent))

	// Create temp directory for extraction
	tempDir, cleanup, err := p.cache.CreateTempDir()
	if err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to create temp directory: %w", err)
	}

	defer cleanup()

	// Write layer content to temp file
	layerFile := filepath.Join(tempDir, "layer.blob")
	if err := p.cache.WriteTempFile(tempDir, "layer.blob", layerContent); err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to write layer: %w", err)
	}

	_ = layerFile // used by extractBytecode via the tempDir

	// Extract bytecode from the layer
	bytecodeFile, err := extractBytecode(tempDir, logger)
	if err != nil {
		return platform.PulledImage{}, err
	}

	// Create cache directory and move bytecode
	if err := p.cache.EnsureCacheDir(cacheKey); err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := p.cache.CacheBytecode(cacheKey, bytecodeFile); err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to cache bytecode: %w", err)
	}

	destPath := p.cache.BytecodePath(cacheKey)
	logger.Debug("bytecode cached", "path", destPath)

	// Validate the ELF file
	if err := validateELF(destPath, logger); err != nil {
		// Clean up invalid file
		if rmErr := p.cache.RemoveCacheEntry(cacheKey); rmErr != nil {
			logger.Warn("failed to remove cache directory during cleanup", "cache_key", cacheKey, "error", rmErr)
		}
		return platform.PulledImage{}, err
	}

	resolvedDigest := manifestDesc.Digest.String()

	// Save metadata
	meta := cachedMetadata{
		Digest:         resolvedDigest,
		Programs:       programs,
		Maps:           maps,
		PulledAt:       time.Now(),
		Policy:         admittedBy,
		VerifiedDigest: verifiedDigest,
	}

	if err := p.cache.SaveMetadata(cacheKey, meta); err != nil {
		return platform.PulledImage{}, fmt.Errorf("failed to save metadata: %w", err)
	}

	logger.Info("image cached successfully", "path", destPath)

	return platform.PulledImage{
		ObjectPath: destPath,
		Programs:   programs,
		Maps:       maps,
		URL:        ref.URL,
		Digest:     resolvedDigest,
		PullPolicy: ref.PullPolicy,
	}, nil
}

// checkCache checks if a valid cached image exists, returning the
// image and the metadata recording how it was admitted.
func (p *puller) checkCache(cacheKey string, ref platform.ImageRef, logger *slog.Logger) (platform.PulledImage, cachedMetadata, bool) {
	// Check if bytecode exists
	if !p.cache.BytecodeExists(cacheKey) {
		logger.Debug("cache miss: bytecode not found")
		return platform.PulledImage{}, cachedMetadata{}, false
	}

	// Try to load metadata
	var meta cachedMetadata
	if err := p.cache.LoadMetadata(cacheKey, &meta); err != nil {
		logger.Debug("cache miss: metadata not found", "error", err)
		return platform.PulledImage{}, cachedMetadata{}, false
	}

	logger.Debug("cache hit", "digest", meta.Digest, "pulled_at", meta.PulledAt)

	return platform.PulledImage{
		ObjectPath: p.cache.BytecodePath(cacheKey),
		Programs:   meta.Programs,
		Maps:       meta.Maps,
		URL:        ref.URL,
		Digest:     meta.Digest,
		PullPolicy: ref.PullPolicy,
	}, meta, true
}

// cachedEntryAdmitted reports whether a cached entry may be served
// under the signing policy now in force. An entry admitted under that
// same policy is served untouched, which is the steady state and costs
// nothing. Otherwise the image is verified again and the new decision
// recorded, so the check happens once per policy change rather than
// once per load.
//
// Verification asks about the digest the verifier was originally asked
// about, so a policy change does not force the image to be downloaded
// again. Returns false when the entry cannot be judged without a fresh
// resolve, leaving the caller to pull it.
func (p *puller) cachedEntryAdmitted(ctx context.Context, cacheKey string, ref platform.ImageRef, meta cachedMetadata, logger *slog.Logger) (bool, error) {
	if p.verifier == nil {
		return true, nil
	}

	// An entry carrying no policy was admitted by something this
	// code cannot reason about, so it never counts as admitted --
	// including against a verifier that names its policy with the
	// empty string.
	policy := p.verifier.PolicyID()
	recorded := meta.Policy != "" && meta.VerifiedDigest != ""
	if recorded && meta.Policy == policy {
		return true, nil
	}

	// Verification reaches sigstore and the registry, which
	// PullNever exists to avoid. Refusing is the only choice that
	// neither breaks that contract nor serves bytecode the current
	// policy has never passed.
	if ref.PullPolicy == bpfman.PullNever {
		return false, fmt.Errorf("image %s was cached under a different signing policy and pull policy is Never, so it cannot be verified", ref.URL)
	}

	// Nothing recorded to re-verify, so pull the image again and
	// put it through the full check.
	if !recorded {
		logger.Info("cached image predates signing policy tracking, pulling again")
		return false, nil
	}

	logger.Info("signing policy changed since image was cached, verifying again",
		"cached_policy", meta.Policy, "current_policy", policy)

	if _, err := p.verifier.Verify(ctx, platform.SignatureVerificationRequest{
		ImageRef: ref.URL + "@" + meta.VerifiedDigest,
		Auth:     ref.Auth,
	}); err != nil {
		logger.Error("cached image rejected by current signing policy", "error", err)
		return false, fmt.Errorf("signature verification failed: %w", err)
	}

	// Recording the new policy saves the next load a verification;
	// failing to record it costs a repeat, which is not worth
	// failing a load that policy has just accepted.
	meta.Policy = policy
	if err := p.cache.SaveMetadata(cacheKey, meta); err != nil {
		logger.Warn("failed to record signing policy against cached image", "error", err)
	}

	return true, nil
}

// configureAuth sets up authentication for the repository.
func (p *puller) configureAuth(repo *remote.Repository, authConfig *platform.ImageAuth, logger *slog.Logger) error {
	// If explicit credentials provided, use them
	if authConfig.Complete() {
		logger.Debug("using explicit credentials", "username", authConfig.Username)
		repo.Client = &auth.Client{
			Client: retry.DefaultClient,
			Credential: auth.StaticCredential(repo.Reference.Registry, auth.Credential{
				Username: authConfig.Username,
				Password: authConfig.Password,
			}),
		}
		return nil
	}
	if isLoopbackRegistry(repo.Reference.Registry) {
		logger.Debug("using anonymous access for loopback registry")
		return nil
	}

	// Try to load credentials from credential stores
	credStore, err := newCredentialStore(logger)
	if err != nil {
		logger.Debug("no credential store found, using anonymous access", "error", err)
		return nil
	}

	logger.Debug("using credential store for authentication")
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Credential: credentials.Credential(credStore),
	}

	return nil
}

func isLoopbackRegistry(registry string) bool {
	host := registry
	if h, _, err := net.SplitHostPort(registry); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// selectPlatform selects the appropriate platform manifest from an image index.
func (p *puller) selectPlatform(ctx context.Context, repo *remote.Repository, indexDesc ocispec.Descriptor, logger *slog.Logger) (ocispec.Descriptor, error) {
	rc, err := repo.Manifests().Fetch(ctx, indexDesc)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to fetch index: %w", err)
	}

	defer rc.Close()

	indexContent, err := io.ReadAll(rc)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to read index: %w", err)
	}

	var index struct {
		Manifests []struct {
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
			MediaType string `json:"mediaType"`
			Platform  struct {
				Architecture string `json:"architecture"`
				OS           string `json:"os"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(indexContent, &index); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to parse index: %w", err)
	}

	if len(index.Manifests) == 0 {
		return ocispec.Descriptor{}, fmt.Errorf("image index has no manifests")
	}

	// Get current architecture
	hostArch := getHostArch()
	logger.Debug("selecting platform", "host_arch", hostArch, "available", len(index.Manifests))

	// Try to find matching architecture
	for _, m := range index.Manifests {
		logger.Debug("checking manifest", "arch", m.Platform.Architecture, "os", m.Platform.OS, "digest", m.Digest)
		if m.Platform.Architecture == hostArch && m.Platform.OS == "linux" {
			return ocispec.Descriptor{
				MediaType: m.MediaType,
				Digest:    digest.Digest(m.Digest),
				Size:      m.Size,
			}, nil
		}
	}

	return ocispec.Descriptor{}, fmt.Errorf("no linux/%s manifest found in image index", hostArch)
}

// getHostArch returns the host architecture in OCI format.
func getHostArch() string {
	// Map Go GOARCH to OCI architecture names
	switch arch := os.Getenv("GOARCH"); arch {
	case "":
		// Detect at runtime
		return detectArch()
	default:
		return goArchToOCI(arch)
	}
}

// detectArch detects the current architecture.
func detectArch() string {
	return goArchToOCI(runtime.GOARCH)
}

// goArchToOCI converts Go architecture names to OCI format.
func goArchToOCI(goArch string) string {
	switch goArch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm"
	case "386":
		return "386"
	case "ppc64le":
		return "ppc64le"
	case "s390x":
		return "s390x"
	default:
		return goArch
	}
}

// extractLabels fetches the image config blob and extracts BPF labels.
func (p *puller) extractLabels(ctx context.Context, repo *remote.Repository, configDesc ocispec.Descriptor, logger *slog.Logger) (programs, maps map[string]string, err error) {
	if configDesc.Digest == "" {
		logger.Debug("no config digest provided, skipping label extraction")
		return nil, nil, nil
	}

	logger.Debug("fetching config for labels", "config_digest", configDesc.Digest.String(), "size", configDesc.Size, "media_type", configDesc.MediaType)

	// Fetch the config blob directly. Use the full descriptor from
	// the manifest so ORAS can validate the registry response size.
	rc, err := repo.Blobs().Fetch(ctx, configDesc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch config: %w", err)
	}

	defer rc.Close()

	configContent, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Parse config to get labels
	var config struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal(configContent, &config); err != nil {
		return nil, nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Extract program labels
	if progJSON := config.Config.Labels[LabelPrograms]; progJSON != "" {
		programs = make(map[string]string)
		if err := json.Unmarshal([]byte(progJSON), &programs); err != nil {
			return nil, nil, fmt.Errorf("failed to parse %s label: %w", LabelPrograms, err)
		}
	}

	// Extract map labels
	if mapJSON := config.Config.Labels[LabelMaps]; mapJSON != "" {
		maps = make(map[string]string)
		if err := json.Unmarshal([]byte(mapJSON), &maps); err != nil {
			return nil, nil, fmt.Errorf("failed to parse %s label: %w", LabelMaps, err)
		}
	}

	return programs, maps, nil
}
