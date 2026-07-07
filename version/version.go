// Package version holds build-time version information injected via
// ldflags. The Makefile sets these using -X linker flags.
package version

import (
	"fmt"
	"runtime"
	"strings"
)

// These variables are set at build time via -ldflags -X.
var (
	gitCommit string // full git commit hash
	gitBranch string // git branch name
	gitState  string // "clean" or "dirty"
	buildDate string // ISO 8601 build timestamp
	version   string // semantic version tag, if any

	// The next three are populated only by the CI image-build
	// workflow (.github/workflows/go-image.yaml). Local `make
	// build`, host-build paths via Dockerfile.bpfman.dev, and
	// downstream Konflux/RHEL/UBI builds intentionally leave
	// them empty: they are only meaningful for binaries that
	// were published as part of a signed multi-arch image, and
	// the Attestation field below is omitted entirely when any
	// of them is empty.

	imageRef       string // e.g. "quay.io/bpfman/bpfman"
	signerIdentity string // e.g. "https://github.com/bpfman/bpfman/.github/workflows/go-image.yaml@refs/heads/main"
	oidcIssuer     string // e.g. "https://token.actions.githubusercontent.com"
)

// Info contains structured version information.
type Info struct {
	// Version is the semantic version tag stamped via -ldflags -X, or
	// "(devel)" when the binary was built without one.
	Version string `json:"version"`

	// GitCommit is the full git commit hash the binary was built from,
	// stamped via -ldflags -X. Empty when not stamped.
	GitCommit string `json:"git_commit"`

	// GitBranch is the git branch name the binary was built from,
	// stamped via -ldflags -X. Empty when not stamped.
	GitBranch string `json:"git_branch"`

	// GitState is "clean" or "dirty", recording whether the working
	// tree had uncommitted changes at build time. Stamped via -ldflags
	// -X; empty when not stamped.
	GitState string `json:"git_state"`

	// BuildDate is the ISO 8601 build timestamp, stamped via -ldflags
	// -X. Empty when not stamped.
	BuildDate string `json:"build_date"`

	// GoVersion is the Go toolchain version, taken from
	// runtime.Version() at the time Get is called.
	GoVersion string `json:"go_version"`

	// Platform is the build's "GOOS/GOARCH" pair, taken from runtime at
	// the time Get is called.
	Platform string `json:"platform"`

	// Attestation is the cosign verify command for the image
	// this binary was published from. Empty unless the binary
	// was built by the CI image workflow with all three of
	// imageRef, signerIdentity, oidcIssuer set via -ldflags.
	Attestation string `json:"attestation"`
}

// Get returns the current build version information.
func Get() Info {
	v := version
	if v == "" {
		v = "(devel)"
	}
	return Info{
		Version:     v,
		GitCommit:   gitCommit,
		GitBranch:   gitBranch,
		GitState:    gitState,
		BuildDate:   buildDate,
		GoVersion:   runtime.Version(),
		Platform:    runtime.GOOS + "/" + runtime.GOARCH,
		Attestation: attestation(),
	}
}

// attestation returns a ready-to-pipe `cosign verify` command line
// for the published image, or an empty string if any of the three
// build-time identity fields are unset (which is the normal case
// for any binary not built by the CI image workflow).
//
// The returned string is the bare command on a single line: it is
// designed to be readable by a human and pipeable to `sh` after
// the human has decided whether to trust the embedded signing
// identity. The binary itself does not perform verification.
func attestation() string {
	if imageRef == "" || signerIdentity == "" || oidcIssuer == "" {
		return ""
	}
	return fmt.Sprintf(
		"cosign verify %s:latest --certificate-identity '%s' --certificate-oidc-issuer %s",
		imageRef, signerIdentity, oidcIssuer,
	)
}

// String returns a single-line summary suitable for log output.
func (i Info) String() string {
	var parts []string
	parts = append(parts, i.Version)
	if i.GitCommit != "" {
		short := i.GitCommit
		if len(short) > 12 {
			short = short[:12]
		}
		parts = append(parts, short)
	}
	if i.GitState == "dirty" {
		parts = append(parts, "dirty")
	}
	if i.BuildDate != "" {
		parts = append(parts, i.BuildDate)
	}
	parts = append(parts, i.GoVersion)
	parts = append(parts, i.Platform)
	return strings.Join(parts, " ")
}

// Long returns a multi-line version string for display.
func (i Info) Long() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Version:     %s\n", i.Version)
	fmt.Fprintf(&b, "Git commit:  %s\n", i.GitCommit)
	fmt.Fprintf(&b, "Git branch:  %s\n", i.GitBranch)
	fmt.Fprintf(&b, "Git state:   %s\n", i.GitState)
	fmt.Fprintf(&b, "Build date:  %s\n", i.BuildDate)
	fmt.Fprintf(&b, "Go version:  %s\n", i.GoVersion)
	fmt.Fprintf(&b, "Platform:    %s\n", i.Platform)
	if i.Attestation != "" {
		fmt.Fprintf(&b, "Attestation: %s\n", i.Attestation)
	}
	return b.String()
}
