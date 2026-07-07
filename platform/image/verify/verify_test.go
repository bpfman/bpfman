package verify

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	cosignremote "github.com/sigstore/cosign/v2/pkg/oci/remote"

	"github.com/bpfman/bpfman/config"
	"github.com/bpfman/bpfman/platform"
)

func TestNoSignReportsVerificationDisabled(t *testing.T) {
	t.Parallel()

	result, err := NoSign().Verify(context.Background(), platform.SignatureVerificationRequest{
		ImageRef: "example.test/x:latest",
	})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}

	if result.Status != platform.SignatureVerificationDisabled {
		t.Fatalf("Status = %q, want %q", result.Status, platform.SignatureVerificationDisabled)
	}
}

func TestIsNoSignaturesError(t *testing.T) {
	t.Parallel()

	if !isNoSignaturesError(fmt.Errorf("wrapped: %w", &cosign.ErrNoSignaturesFound{})) {
		t.Fatal("isNoSignaturesError returned false for ErrNoSignaturesFound")
	}
}

func TestIsNoSignaturesErrorRejectsNoMatchingSignatures(t *testing.T) {
	t.Parallel()

	if isNoSignaturesError(fmt.Errorf("wrapped: %w", &cosign.ErrNoMatchingSignatures{})) {
		t.Fatal("isNoSignaturesError returned true for ErrNoMatchingSignatures")
	}
}

func TestFromSigningConfigReturnsNoSignWhenVerificationDisabled(t *testing.T) {
	t.Parallel()

	cfg := config.SigningConfig{
		AllowUnsigned: true,
		VerifyEnabled: false,
	}
	verifier, err := FromSigningConfig(cfg, nil)
	if err != nil {
		t.Fatalf("FromSigningConfig returned error: %v", err)
	}

	result, err := verifier.Verify(context.Background(), platform.SignatureVerificationRequest{
		ImageRef: "example.test/x:latest",
	})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}

	if result.Status != platform.SignatureVerificationDisabled {
		t.Fatalf("Status = %q, want %q", result.Status, platform.SignatureVerificationDisabled)
	}
}

func TestFromSigningConfigDefaultsToAnyIdentityWhenUnsignedForbidden(t *testing.T) {
	t.Parallel()

	cfg := config.SigningConfig{
		AllowUnsigned: false,
		VerifyEnabled: true,
	}
	verifier, err := FromSigningConfig(cfg, nil)
	if err != nil {
		t.Fatalf("FromSigningConfig returned error: %v", err)
	}

	cosignVerifier, ok := verifier.(*cosignVerifier)
	if !ok {
		t.Fatalf("verifier has type %T, want *cosignVerifier", verifier)
	}

	if cosignVerifier.allowUnsigned {
		t.Fatal("allowUnsigned = true, want false")
	}
	if len(cosignVerifier.identities) != 1 {
		t.Fatalf("len(identities) = %d, want 1", len(cosignVerifier.identities))
	}
	if cosignVerifier.identities[0].SubjectRegExp != ".*" {
		t.Fatalf("SubjectRegExp = %q, want .*", cosignVerifier.identities[0].SubjectRegExp)
	}
	if cosignVerifier.identities[0].IssuerRegExp != ".*" {
		t.Fatalf("IssuerRegExp = %q, want .*", cosignVerifier.identities[0].IssuerRegExp)
	}
}

func TestFromSigningConfigWarnsWhenUnsignedForbiddenWithoutTrustedIdentity(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	cfg := config.SigningConfig{
		AllowUnsigned: false,
		VerifyEnabled: true,
	}
	if _, err := FromSigningConfig(cfg, logger); err != nil {
		t.Fatalf("FromSigningConfig returned error: %v", err)
	}

	if !strings.Contains(logs.String(), "accepting any valid signer") {
		t.Fatalf("logs = %q, want any-signer warning", logs.String())
	}
}

func TestFromSigningConfigAppliesTrustedIdentity(t *testing.T) {
	t.Parallel()

	cfg := config.SigningConfig{
		AllowUnsigned: false,
		VerifyEnabled: true,
		TrustedIdentities: []config.TrustedIdentityConfig{
			{
				CertificateIdentityRegexp:   `.*@example\.com`,
				CertificateOIDCIssuerRegexp: `https://github\.com/.*`,
			},
			{
				CertificateIdentity:   "builder@example.com",
				CertificateOIDCIssuer: "https://accounts.google.com",
			},
		},
	}
	verifier, err := FromSigningConfig(cfg, nil)
	if err != nil {
		t.Fatalf("FromSigningConfig returned error: %v", err)
	}

	cosignVerifier, ok := verifier.(*cosignVerifier)
	if !ok {
		t.Fatalf("verifier has type %T, want *cosignVerifier", verifier)
	}

	if cosignVerifier.allowUnsigned {
		t.Fatal("allowUnsigned = true, want false")
	}
	if len(cosignVerifier.identities) != 2 {
		t.Fatalf("len(identities) = %d, want 2", len(cosignVerifier.identities))
	}
	first := cosignVerifier.identities[0]
	wantSubjectRegexp := `^(?:.*@example\.com)$`
	if first.SubjectRegExp != wantSubjectRegexp {
		t.Fatalf("SubjectRegExp = %q, want %q", first.SubjectRegExp, wantSubjectRegexp)
	}
	wantIssuerRegexp := `^(?:https://github\.com/.*)$`
	if first.IssuerRegExp != wantIssuerRegexp {
		t.Fatalf("IssuerRegExp = %q, want %q", first.IssuerRegExp, wantIssuerRegexp)
	}
	second := cosignVerifier.identities[1]
	if second.Subject != "builder@example.com" {
		t.Fatalf("Subject = %q, want builder@example.com", second.Subject)
	}
	if second.Issuer != "https://accounts.google.com" {
		t.Fatalf("Issuer = %q, want https://accounts.google.com", second.Issuer)
	}
	if second.SubjectRegExp != "" {
		t.Fatalf("SubjectRegExp = %q, want empty", second.SubjectRegExp)
	}
	if second.IssuerRegExp != "" {
		t.Fatalf("IssuerRegExp = %q, want empty", second.IssuerRegExp)
	}
}

// TestRegistryClientOptsDoNotConflictWithDefaultKeychain guards the
// auth wiring. cosign seeds its remote options with a default keychain,
// and go-containerregistry rejects an option set that carries both an
// Authenticator and a Keychain. The explicit credentials must therefore
// replace the default rather than be appended to it. Pointing at a
// closed port keeps this offline: the call must reach the network (a
// connection error), not fail at option validation with "not both".
func TestRegistryClientOptsDoNotConflictWithDefaultKeychain(t *testing.T) {
	t.Parallel()

	ref, err := name.NewTag("127.0.0.1:1/repo:sha256-aaaa.sig")
	if err != nil {
		t.Fatalf("NewTag returned error: %v", err)
	}

	opts := registryClientOpts(&platform.ImageAuth{Username: "u", Password: "p"})
	_, err = cosignremote.Signatures(ref, opts...)
	if err != nil && strings.Contains(err.Error(), "not both") {
		t.Fatalf("auth and default keychain conflict: %v", err)
	}
}

func TestRegistryClientOptsRequireBothCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		auth *platform.ImageAuth
		want bool
	}{
		{"nil", nil, false},
		{"missing password", &platform.ImageAuth{Username: "u"}, false},
		{"missing username", &platform.ImageAuth{Password: "p"}, false},
		{"both present", &platform.ImageAuth{Username: "u", Password: "p"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := len(registryClientOpts(tc.auth)) > 0
			if got != tc.want {
				t.Fatalf("registryClientOpts options present = %v, want %v", got, tc.want)
			}
		})
	}
}
