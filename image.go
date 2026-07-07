package bpfman

import (
	"fmt"
	"strings"
)

// ImagePullPolicy specifies when to pull an OCI image.
//
// It is a plain string enum, so a value carries no proof of validity.
// Validity is enforced at the boundaries: ParseImagePullPolicy is the
// strict parser for external input (case-insensitive, with the empty
// string mapping to the IfNotPresent default), and Valid reports
// membership of the known set. JSON decoding is permissive, trusting
// bpfman's own stored records.
type ImagePullPolicy string

const (
	// PullAlways always pulls the image, even if cached.
	PullAlways ImagePullPolicy = "Always"
	// PullIfNotPresent uses the cache if available, otherwise pulls.
	PullIfNotPresent ImagePullPolicy = "IfNotPresent"
	// PullNever only uses the cache, fails if not present.
	PullNever ImagePullPolicy = "Never"
)

// Valid reports whether p is one of the known pull policies. It is
// strict membership: the zero value and any unrecognised value are not
// valid. ParseImagePullPolicy is deliberately more lenient at the input
// boundary (empty maps to the IfNotPresent default), so Valid cannot
// delegate to it.
func (p ImagePullPolicy) Valid() bool {
	switch p {
	case PullAlways, PullIfNotPresent, PullNever:
		return true
	default:
		return false
	}
}

// String returns the string representation of the pull policy.
func (p ImagePullPolicy) String() string { return string(p) }

// ParseImagePullPolicy parses a string into an ImagePullPolicy.
// Returns the ImagePullPolicy and a nil error if valid, or the zero
// value and an error if not recognised. Matching is case-insensitive,
// and the empty string maps to the IfNotPresent default.
func ParseImagePullPolicy(s string) (ImagePullPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return PullIfNotPresent, nil
	case "always":
		return PullAlways, nil
	case "ifnotpresent":
		return PullIfNotPresent, nil
	case "never":
		return PullNever, nil
	default:
		return "", fmt.Errorf("unknown pull policy %q", s)
	}
}
