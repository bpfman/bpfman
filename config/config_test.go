package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bpfman/bpfman/logging"
)

func TestLoggingConfigToSpec_MergesComponents(t *testing.T) {
	t.Parallel()

	cfg := LoggingConfig{
		Level: "info",
		Components: map[string]string{
			"manager": "debug",
			"store":   "warn",
		},
	}

	spec, err := logging.ParseSpec(cfg.ToSpec())
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	if spec.BaseLevel != logging.LevelInfo {
		t.Fatalf("base level = %v, want %v", spec.BaseLevel, logging.LevelInfo)
	}

	want := map[string]logging.Level{
		"manager": logging.LevelDebug,
		"store":   logging.LevelWarn,
	}
	if !reflect.DeepEqual(spec.Components, want) {
		t.Fatalf("components = %#v, want %#v", spec.Components, want)
	}
}

func TestLoggingConfigToSpec_DefaultBase(t *testing.T) {
	t.Parallel()

	cfg := LoggingConfig{
		Components: map[string]string{
			"manager": "debug",
		},
	}

	spec, err := logging.ParseSpec(cfg.ToSpec())
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	if spec.BaseLevel != logging.LevelInfo {
		t.Fatalf("base level = %v, want %v", spec.BaseLevel, logging.LevelInfo)
	}
	if spec.Components["manager"] != logging.LevelDebug {
		t.Fatalf("component manager = %v, want %v", spec.Components["manager"], logging.LevelDebug)
	}
}

func TestLoggingConfigToSpec_Empty(t *testing.T) {
	t.Parallel()

	cfg := LoggingConfig{}
	if got := cfg.ToSpec(); got != "" {
		t.Fatalf("spec = %q, want empty string", got)
	}
}

func TestConfigValidate_InvalidLoggingFormat(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Logging.Format = "xml"

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for invalid logging format")
	}
}

func TestConfigValidate_InvalidLoggingSpec(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Logging.Components = map[string]string{
		"manager": "verbose",
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for invalid logging level")
	}
}

func TestConfigLoadTrustedIdentities(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bpfman.toml")
	if err := os.WriteFile(path, []byte(`
[signing]
allow_unsigned = false
verify_enabled = true

[[signing.trusted_identities]]
certificate_identity = "signer@example.com"
certificate_oidc_issuer = "https://github.com/login/oauth"

[[signing.trusted_identities]]
certificate_identity = "builder@example.com"
certificate_oidc_issuer = "https://accounts.google.com"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Signing.TrustedIdentities) != 2 {
		t.Fatalf("len(TrustedIdentities) = %d, want 2", len(cfg.Signing.TrustedIdentities))
	}
}

func TestConfigLoadMissingDefaultFileReturnsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := load("", filepath.Join(t.TempDir(), "missing-default.toml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Signing.AllowUnsigned != DefaultConfig().Signing.AllowUnsigned {
		t.Fatalf("AllowUnsigned = %v, want default", cfg.Signing.AllowUnsigned)
	}
}

func TestConfigLoadExplicitMissingFileReturnsError(t *testing.T) {
	t.Parallel()

	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatal("Load returned nil error for explicit missing config path")
	}
}

func TestConfigLoadUnreadableFileReturnsError(t *testing.T) {
	t.Parallel()

	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("Load returned nil error for unreadable config path")
	}
}

func TestSigningConfigTrustedSigningIdentitiesExactValuesStayExact(t *testing.T) {
	t.Parallel()

	cfg := SigningConfig{
		TrustedIdentities: []TrustedIdentityConfig{
			{
				CertificateIdentity:   "signer@example.com",
				CertificateOIDCIssuer: "https://github.com/login/oauth",
			},
		},
	}
	identities, err := cfg.TrustedSigningIdentities()
	if err != nil {
		t.Fatalf("TrustedSigningIdentities returned error: %v", err)
	}

	identity := identities[0]
	if identity.Subject != "signer@example.com" {
		t.Fatalf("Subject = %q", identity.Subject)
	}
	if identity.Issuer != "https://github.com/login/oauth" {
		t.Fatalf("Issuer = %q", identity.Issuer)
	}
	if identity.SubjectRegexp != "" {
		t.Fatalf("SubjectRegexp = %q, want empty", identity.SubjectRegexp)
	}
	if identity.IssuerRegexp != "" {
		t.Fatalf("IssuerRegexp = %q, want empty", identity.IssuerRegexp)
	}
}

func TestSigningConfigTrustedSigningIdentitiesAcceptsRegexps(t *testing.T) {
	t.Parallel()

	cfg := SigningConfig{
		TrustedIdentities: []TrustedIdentityConfig{
			{
				CertificateIdentityRegexp:   `.*@example\.com`,
				CertificateOIDCIssuerRegexp: `https://github\.com/.*`,
			},
		},
	}
	identities, err := cfg.TrustedSigningIdentities()
	if err != nil {
		t.Fatalf("TrustedSigningIdentities returned error: %v", err)
	}

	identity := identities[0]
	wantSubject := `^(?:.*@example\.com)$`
	if identity.SubjectRegexp != wantSubject {
		t.Fatalf("SubjectRegexp = %q, want %q", identity.SubjectRegexp, wantSubject)
	}
	wantIssuer := `^(?:https://github\.com/.*)$`
	if identity.IssuerRegexp != wantIssuer {
		t.Fatalf("IssuerRegexp = %q, want %q", identity.IssuerRegexp, wantIssuer)
	}
}

func TestSigningConfigTrustedSigningIdentitiesReturnsMultipleEntries(t *testing.T) {
	t.Parallel()

	cfg := SigningConfig{
		TrustedIdentities: []TrustedIdentityConfig{
			{
				CertificateIdentity:   "signer@example.com",
				CertificateOIDCIssuer: "https://github.com/login/oauth",
			},
			{
				CertificateIdentity:   "builder@example.com",
				CertificateOIDCIssuer: "https://accounts.google.com",
			},
		},
	}
	identities, err := cfg.TrustedSigningIdentities()
	if err != nil {
		t.Fatalf("TrustedSigningIdentities returned error: %v", err)
	}

	if len(identities) != 2 {
		t.Fatalf("len(identities) = %d, want 2", len(identities))
	}
}

func TestSigningConfigTrustedSigningIdentitiesRequiresIssuerAndSubject(t *testing.T) {
	t.Parallel()

	cfg := SigningConfig{
		TrustedIdentities: []TrustedIdentityConfig{
			{CertificateIdentity: "signer@example.com"},
		},
	}
	if _, err := cfg.TrustedSigningIdentities(); err == nil {
		t.Fatal("TrustedSigningIdentities returned nil error for incomplete identity")
	}
}

func TestSigningConfigTrustedSigningIdentitiesRejectsExactAndRegexp(t *testing.T) {
	t.Parallel()

	cfg := SigningConfig{
		TrustedIdentities: []TrustedIdentityConfig{
			{
				CertificateIdentity:         "signer@example.com",
				CertificateIdentityRegexp:   `.*@example\.com`,
				CertificateOIDCIssuerRegexp: `https://github\.com/.*`,
			},
		},
	}
	if _, err := cfg.TrustedSigningIdentities(); err == nil {
		t.Fatal("TrustedSigningIdentities returned nil error for exact and regexp identity")
	}
}

func TestSigningConfigTrustedSigningIdentitiesRejectsWildcardMixedWithSpecific(t *testing.T) {
	t.Parallel()

	cfg := SigningConfig{
		TrustedIdentities: []TrustedIdentityConfig{
			{
				CertificateIdentity:   "signer@example.com",
				CertificateOIDCIssuer: "https://github.com/login/oauth",
			},
			{
				CertificateIdentityRegexp:   ".*",
				CertificateOIDCIssuerRegexp: ".*",
			},
		},
	}
	if _, err := cfg.TrustedSigningIdentities(); err == nil {
		t.Fatal("TrustedSigningIdentities returned nil error for mixed wildcard and specific identities")
	}
}

func TestSigningConfigValidateAllowsSignedRequiredWithoutTrustedIdentity(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Signing.AllowUnsigned = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestSigningConfigValidateAllowsSignedRequiredWithIdentity(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Signing.AllowUnsigned = false
	cfg.Signing.TrustedIdentities = []TrustedIdentityConfig{
		{
			CertificateIdentity:   "signer@example.com",
			CertificateOIDCIssuer: "https://github.com/login/oauth",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}
