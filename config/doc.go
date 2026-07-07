// Package config handles bpfman daemon configuration.
//
// # Overview
//
// Configuration is loaded with overlay semantics:
//
//  1. Start with built-in defaults (embedded via go:embed from default.toml)
//  2. Overlay with config file values (if file exists)
//  3. CLI flags and environment variables override at runtime (handled by CLI layer)
//
// This ensures a valid configuration is always available, even when no
// default config file exists. An explicitly supplied config path must
// exist. The TOML decoder only sets fields present in the file, leaving
// unspecified fields at their default values.
//
// If the config file exists but cannot be read, is malformed TOML, or
// fails validation, [Load] returns an error rather than silently falling
// back to defaults. This fail-fast behaviour prevents running with
// unintended configuration.
//
// # Configuration Sections
//
// The configuration has two sections:
//
//   - [signing]: controls image signature verification
//   - [logging]: controls log output level and format
//
// # Signing Configuration
//
// The [SigningConfig] section controls how OCI image signatures are handled:
//
//   - AllowUnsigned: whether unsigned images can be loaded (default: true)
//   - VerifyEnabled: whether to verify signatures on signed images (default: true)
//   - TrustedIdentities: keyless signing identity/issuer pairs trusted for
//     signed images
//
// The interaction between these fields:
//
//	AllowUnsigned=true,  VerifyEnabled=true  -> verify if signed, accept if unsigned
//	AllowUnsigned=true,  VerifyEnabled=false -> accept all, no verification
//	AllowUnsigned=false, VerifyEnabled=true  -> require valid signature
//	AllowUnsigned=false, VerifyEnabled=false -> accept all, no verification
//
// When unsigned images are forbidden and verification is enabled, a trusted
// certificate identity list may be configured to restrict who can sign.
// If the list is empty, any valid sigstore identity is accepted. Exact
// values are matched exactly, regexp values are anchored to the whole
// certificate value, and exact and regexp forms are mutually exclusive
// within each trusted identity entry. A wildcard identity entry may not
// be mixed with specific entries.
//
// Use [SigningConfig.MustRequireSignatures] to check if signatures are
// mandatory, and [SigningConfig.ShouldVerify] to check if verification
// should be performed.
//
// # Logging Configuration
//
// The [LoggingConfig] section controls log output:
//
//   - Level: log spec string (e.g., "info" or "info,manager=debug")
//   - Format: output format, either "text" or "json"
//   - Components: alternative per-component level map
//
// The Level field uses a comma-separated spec format where the first
// element is the default level and subsequent elements are component
// overrides. For example, "info,manager=debug,store=warn" sets info as
// the default, debug for the manager component, and warn for store.
//
// The Components map provides an alternative way to express the same
// configuration in TOML:
//
//	[logging.components]
//	manager = "debug"
//	store = "warn"
//
// If Level is set, it provides the base level and Components act as per-component
// overrides. The [LoggingConfig.ToSpec] method converts the configuration to a
// spec string.
//
// # Default Configuration
//
// The default configuration is embedded from default.toml at build time:
//
//	[signing]
//	allow_unsigned = true
//	verify_enabled = true
//	# [[signing.trusted_identities]]
//	# certificate_identity = "signer@example.com"
//	# certificate_oidc_issuer = "https://github.com/login/oauth"
//
//	[logging]
//	level = "info"
//	format = "text"
//
// Call [DefaultConfig] to obtain these defaults programmatically.
//
// # File Location
//
// The default configuration file path is /etc/bpfman/bpfman.toml
// ([DefaultConfigPath]). The [Load] function accepts an alternative path.
//
// # Usage
//
// Typical usage:
//
//	cfg, err := config.Load("")  // uses DefaultConfigPath
//	if err != nil {
//	    // file exists but is invalid
//	}
//	// cfg is valid (either from file or defaults)
//
// For testing or when defaults are sufficient:
//
//	cfg := config.DefaultConfig()
package config
