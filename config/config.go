package config

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/bpfman/bpfman/logging"
)

//go:embed default.toml
var defaultConfigTOML string

const (
	// DefaultConfigPath is the default path to the bpfman config file.
	DefaultConfigPath = "/etc/bpfman/bpfman.toml"
)

// Config is the top-level bpfman configuration.
type Config struct {
	// Signing controls OCI image signature verification.
	Signing SigningConfig `toml:"signing" json:"signing"`

	// Logging controls the log level and output format.
	Logging LoggingConfig `toml:"logging" json:"logging"`
}

// LoggingConfig controls logging behaviour.
type LoggingConfig struct {
	// Level is the log spec (e.g. "info" or "info,manager=debug").
	Level string `toml:"level" json:"level,omitempty"`

	// Format is the output format: "text" or "json".
	Format string `toml:"format" json:"format,omitempty"`

	// Components provides an alternative way to specify per-component levels.
	Components map[string]string `toml:"components" json:"components,omitempty"`
}

// ToSpec converts the LoggingConfig to a log spec string.
// Level provides the base level; Components override per-component.
func (c *LoggingConfig) ToSpec() string {
	if c.Level == "" && len(c.Components) == 0 {
		return ""
	}

	base := c.Level
	if base == "" {
		base = "info"
	}

	parts := make([]string, 0, len(c.Components)+1)
	parts = append(parts, base)

	for component, level := range c.Components {
		parts = append(parts, component+"="+level)
	}

	return strings.Join(parts, ",")
}

// SigningConfig controls image signature verification.
// These settings match the Rust bpfman implementation.
type SigningConfig struct {
	// AllowUnsigned controls whether unsigned images are accepted.
	// When true (default), unsigned images can be loaded.
	// When false, all images must have valid signatures.
	AllowUnsigned bool `toml:"allow_unsigned" json:"allow_unsigned"`

	// VerifyEnabled controls whether signature verification is performed.
	// When true (default), images with signatures are verified.
	// When false, signature verification is skipped entirely.
	VerifyEnabled bool `toml:"verify_enabled" json:"verify_enabled"`

	// TrustedIdentities is the set of keyless signing identities to
	// trust. Each entry is an identity/issuer pair; entries are ORed.
	TrustedIdentities []TrustedIdentityConfig `toml:"trusted_identities" json:"trusted_identities,omitempty"`
}

// TrustedIdentityConfig is one trusted keyless signing identity rule.
type TrustedIdentityConfig struct {
	// CertificateIdentity is the exact keyless signing certificate
	// identity to trust, such as an email address or GitHub Actions
	// workflow identity.
	CertificateIdentity string `toml:"certificate_identity" json:"certificate_identity,omitempty"`

	// CertificateIdentityRegexp is a regexp form of CertificateIdentity.
	CertificateIdentityRegexp string `toml:"certificate_identity_regexp" json:"certificate_identity_regexp,omitempty"`

	// CertificateOIDCIssuer is the exact OIDC issuer to trust for
	// keyless signing certificates.
	CertificateOIDCIssuer string `toml:"certificate_oidc_issuer" json:"certificate_oidc_issuer,omitempty"`

	// CertificateOIDCIssuerRegexp is a regexp form of CertificateOIDCIssuer.
	CertificateOIDCIssuerRegexp string `toml:"certificate_oidc_issuer_regexp" json:"certificate_oidc_issuer_regexp,omitempty"`
}

// SigningIdentity is the sigstore keyless identity policy expressed in
// the form expected by cosign.
type SigningIdentity struct {
	// Issuer is the exact OIDC issuer to require on the signing
	// certificate. Empty when IssuerRegexp is used instead.
	Issuer string

	// IssuerRegexp is an anchored regexp the certificate's OIDC issuer
	// must match. Empty when Issuer is used instead.
	IssuerRegexp string

	// Subject is the exact signing certificate identity (subject) to
	// require. Empty when SubjectRegexp is used instead.
	Subject string

	// SubjectRegexp is an anchored regexp the certificate identity
	// (subject) must match. Empty when Subject is used instead.
	SubjectRegexp string
}

// DefaultConfig returns the default configuration from the embedded default.toml.
// This provides a valid baseline that is always available.
func DefaultConfig() Config {
	var cfg Config
	if _, err := toml.Decode(defaultConfigTOML, &cfg); err != nil {
		// This should never happen since default.toml is embedded at build time.
		// If it does, return a minimal safe config.
		return Config{
			Signing: SigningConfig{AllowUnsigned: true, VerifyEnabled: true},
			Logging: LoggingConfig{Level: "info", Format: "text"},
		}
	}
	return cfg
}

// Load reads configuration from a file path with overlay semantics.
//
// Behaviour:
//   - Implicit default file missing: returns default configuration (no error)
//   - Explicit config file missing: returns an error
//   - File unreadable (permission denied, etc.): returns an error
//   - File exists and valid: overlays file values onto defaults
//   - File exists but invalid: returns error (fail fast)
//
// The TOML decoder only sets fields present in the file, so unspecified
// fields retain their default values from default.toml.
func Load(path string) (Config, error) {
	return load(path, DefaultConfigPath)
}

func load(path, defaultPath string) (Config, error) {
	explicit := path != ""
	if path == "" {
		path = defaultPath
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			// Config file is optional; fall back to embedded defaults
			// only when the implicit default path is genuinely absent.
			return cfg, nil
		}
		return cfg, fmt.Errorf("failed to read config file: %w", err)
	}

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Validate checks the configuration for consistency.
func (c *Config) Validate() error {
	if _, err := logging.ParseFormat(c.Logging.Format); err != nil {
		return err
	}

	if _, err := logging.ParseSpec(c.Logging.ToSpec()); err != nil {
		return err
	}

	if err := c.Signing.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate checks signing configuration consistency.
func (c *SigningConfig) Validate() error {
	_, err := c.TrustedSigningIdentities()
	return err
}

// MustRequireSignatures returns true if all images must be signed.
func (c *SigningConfig) MustRequireSignatures() bool {
	return !c.AllowUnsigned && c.VerifyEnabled
}

// ShouldVerify returns true if signature verification should be performed.
func (c *SigningConfig) ShouldVerify() bool {
	return c.VerifyEnabled
}

// TrustedSigningIdentities returns the configured keyless signing identity policy.
func (c *SigningConfig) TrustedSigningIdentities() ([]SigningIdentity, error) {
	identities := make([]SigningIdentity, 0, len(c.TrustedIdentities))
	for i, trusted := range c.TrustedIdentities {
		identity, err := trusted.SigningIdentity()
		if err != nil {
			return nil, fmt.Errorf("trusted identity %d: %w", i+1, err)
		}

		identities = append(identities, identity)
	}
	if len(c.TrustedIdentities) > 1 {
		for i, trusted := range c.TrustedIdentities {
			if trusted.IsAnySigner() {
				return nil, fmt.Errorf("trusted identity %d: wildcard identity cannot be mixed with specific identities", i+1)
			}
		}
	}
	return identities, nil
}

// SigningIdentity returns the trusted identity in cosign form.
func (c *TrustedIdentityConfig) SigningIdentity() (SigningIdentity, error) {
	subject, subjectRegexp, subjectConfigured, err := signingIdentityField(
		"certificate_identity",
		c.CertificateIdentity,
		"certificate_identity_regexp",
		c.CertificateIdentityRegexp,
	)
	if err != nil {
		return SigningIdentity{}, err
	}

	issuer, issuerRegexp, issuerConfigured, err := signingIdentityField(
		"certificate_oidc_issuer",
		c.CertificateOIDCIssuer,
		"certificate_oidc_issuer_regexp",
		c.CertificateOIDCIssuerRegexp,
	)
	if err != nil {
		return SigningIdentity{}, err
	}

	if !subjectConfigured || !issuerConfigured {
		return SigningIdentity{}, fmt.Errorf("certificate identity and OIDC issuer must be configured together")
	}
	return SigningIdentity{
		Issuer:        issuer,
		IssuerRegexp:  issuerRegexp,
		Subject:       subject,
		SubjectRegexp: subjectRegexp,
	}, nil
}

// IsAnySigner reports whether this entry accepts any sigstore identity.
func (c *TrustedIdentityConfig) IsAnySigner() bool {
	return wildcardRegexp(c.CertificateIdentityRegexp) && wildcardRegexp(c.CertificateOIDCIssuerRegexp)
}

func signingIdentityField(exactName, exactValue, regexpName, regexpValue string) (string, string, bool, error) {
	exactValue = strings.TrimSpace(exactValue)
	regexpValue = strings.TrimSpace(regexpValue)
	if exactValue != "" && regexpValue != "" {
		return "", "", false, fmt.Errorf("signing %s and %s are mutually exclusive", exactName, regexpName)
	}
	if regexpValue != "" {
		if _, err := regexp.Compile(regexpValue); err != nil {
			return "", "", false, fmt.Errorf("invalid signing %s %q: %w", regexpName, regexpValue, err)
		}

		return "", anchorRegexp(regexpValue), true, nil
	}
	if exactValue != "" {
		return exactValue, "", true, nil
	}
	return "", "", false, nil
}

func anchorRegexp(pattern string) string {
	return "^(?:" + pattern + ")$"
}

func wildcardRegexp(pattern string) bool {
	switch strings.TrimSpace(pattern) {
	case ".*", "^.*$", "^(?:.*)$":
		return true
	default:
		return false
	}
}
