// Package verify provides OCI image signature verification.
package verify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	gcrremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/fulcio"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/rekor"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	cosignremote "github.com/sigstore/cosign/v2/pkg/oci/remote"

	"github.com/bpfman/bpfman/config"
	"github.com/bpfman/bpfman/platform"
)

// NoSign returns a verifier that always succeeds without checking signatures.
// Use this when signature verification is disabled.
func NoSign() platform.SignatureVerifier {
	return noSignVerifier{}
}

type noSignVerifier struct{}

// Verify accepts the image unconditionally and reports that signature
// verification is disabled.
func (noSignVerifier) Verify(ctx context.Context, req platform.SignatureVerificationRequest) (platform.SignatureVerification, error) {
	return platform.SignatureVerification{
		Status: platform.SignatureVerificationDisabled,
	}, nil
}

// CosignOption configures a cosign verifier.
type CosignOption func(*cosignVerifier)

// WithLogger sets the logger for verification operations.
func WithLogger(logger *slog.Logger) CosignOption {
	return func(v *cosignVerifier) {
		if logger != nil {
			v.logger = logger
		}
	}
}

// WithAllowUnsigned controls whether unsigned images are accepted.
func WithAllowUnsigned(allow bool) CosignOption {
	return func(v *cosignVerifier) {
		v.allowUnsigned = allow
	}
}

// WithIdentities sets the acceptable certificate identity constraints.
func WithIdentities(identities []config.SigningIdentity) CosignOption {
	return func(v *cosignVerifier) {
		v.identities = make([]cosign.Identity, 0, len(identities))
		for _, identity := range identities {
			v.identities = append(v.identities, cosign.Identity{
				Issuer:        identity.Issuer,
				IssuerRegExp:  identity.IssuerRegexp,
				Subject:       identity.Subject,
				SubjectRegExp: identity.SubjectRegexp,
			})
		}
	}
}

// Cosign returns a verifier that uses sigstore/cosign for signature verification.
func Cosign(opts ...CosignOption) platform.SignatureVerifier {
	v := &cosignVerifier{
		logger:        slog.Default(),
		allowUnsigned: true, // Permissive default
		identities: []cosign.Identity{
			{
				IssuerRegExp:  ".*", // Accept any issuer by default
				SubjectRegExp: ".*", // Accept any subject by default
			},
		},
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// FromSigningConfig returns the signature verifier described by cfg.
func FromSigningConfig(cfg config.SigningConfig, logger *slog.Logger) (platform.SignatureVerifier, error) {
	if !cfg.ShouldVerify() {
		return NoSign(), nil
	}
	identities, err := cfg.TrustedSigningIdentities()
	if err != nil {
		return nil, err
	}

	if cfg.AllowUnsigned && len(identities) > 0 && logger != nil {
		logger.Warn("trusted signing identities configured but unsigned images are still allowed")
	}
	if !cfg.AllowUnsigned && len(identities) == 0 && logger != nil {
		logger.Warn("signature enforcement enabled but no trusted identities configured; accepting any valid signer")
	}

	opts := []CosignOption{
		WithLogger(logger),
		WithAllowUnsigned(cfg.AllowUnsigned),
	}
	if len(identities) > 0 {
		opts = append(opts, WithIdentities(identities))
	}
	return Cosign(opts...), nil
}

// cosignVerifier verifies OCI image signatures using cosign/sigstore.
type cosignVerifier struct {
	logger        *slog.Logger
	allowUnsigned bool
	identities    []cosign.Identity
}

// Verify checks that the image has a valid sigstore signature.
func (v *cosignVerifier) Verify(ctx context.Context, req platform.SignatureVerificationRequest) (platform.SignatureVerification, error) {
	imageRef := req.ImageRef
	logger := v.logger.With("image", imageRef)
	logger.Debug("verifying image signature")

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return platform.SignatureVerification{}, fmt.Errorf("failed to parse image reference: %w", err)
	}

	rootCerts, err := fulcio.GetRoots()
	if err != nil {
		return platform.SignatureVerification{}, fmt.Errorf("failed to get Fulcio root certificates: %w", err)
	}

	intermediateCerts, err := fulcio.GetIntermediates()
	if err != nil {
		return platform.SignatureVerification{}, fmt.Errorf("failed to get Fulcio intermediate certificates: %w", err)
	}

	rekorClient, err := rekor.NewClient(options.DefaultRekorURL)
	if err != nil {
		return platform.SignatureVerification{}, fmt.Errorf("failed to create Rekor client: %w", err)
	}

	rekorPubKeys, err := cosign.GetRekorPubs(ctx)
	if err != nil {
		return platform.SignatureVerification{}, fmt.Errorf("failed to get Rekor public keys: %w", err)
	}

	ctLogPubKeys, err := cosign.GetCTLogPubs(ctx)
	if err != nil {
		return platform.SignatureVerification{}, fmt.Errorf("failed to get CT log public keys: %w", err)
	}

	co := &cosign.CheckOpts{
		RegistryClientOpts: registryClientOpts(req.Auth),
		RekorClient:        rekorClient,
		RekorPubKeys:       rekorPubKeys,
		RootCerts:          rootCerts,
		IntermediateCerts:  intermediateCerts,
		CTLogPubKeys:       ctLogPubKeys,
		Identities:         v.identities,
	}

	logger.Debug("calling cosign.VerifyImageSignatures", "identities", len(v.identities))
	signatures, bundleVerified, err := cosign.VerifyImageSignatures(ctx, ref, co)
	if err != nil {
		logger.Debug("VerifyImageSignatures returned error", "error", err)
		if isNoSignaturesError(err) {
			if v.allowUnsigned {
				logger.Debug("image has no signatures, but unsigned images are allowed")
				return platform.SignatureVerification{
					Status: platform.SignatureVerificationUnsignedAccepted,
				}, nil
			}
			return platform.SignatureVerification{}, fmt.Errorf("image %s has no signatures and unsigned images are not allowed", imageRef)
		}
		return platform.SignatureVerification{}, fmt.Errorf("signature verification failed for %s: %w", imageRef, err)
	}

	logger.Info("image signature verified", "signatures", len(signatures), "bundle_verified", bundleVerified)

	return platform.SignatureVerification{
		Status: platform.SignatureVerificationVerified,
	}, nil
}

func isNoSignaturesError(err error) bool {
	// Match cosign's typed error only. Classifying by message is
	// version-fragile and can silently widen what counts as unsigned.
	var noSignatures *cosign.ErrNoSignaturesFound
	return errors.As(err, &noSignatures)
}

func registryClientOpts(imageAuth *platform.ImageAuth) []cosignremote.Option {
	if !imageAuth.Complete() {
		return nil
	}
	return []cosignremote.Option{
		cosignremote.WithRemoteOptions(gcrremote.WithAuth(authn.FromConfig(authn.AuthConfig{
			Username: imageAuth.Username,
			Password: imageAuth.Password,
		}))),
	}
}
