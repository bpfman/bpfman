package bpfmanbuiltin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bpfman "github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/check"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// checkSource tokenises and parses src, runs the static checker,
// and returns any issues. Tests use it as a one-liner so the
// source stays readable. Lives on the cmd side rather than in
// shell/ because these tests exercise the bpfman bind-shape
// policy that the cmd-package init registers; the shell-package
// has its own equivalent.
func checkSource(t *testing.T, src string) []check.Issue {
	t.Helper()
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err)
	return check.Check(prog)
}

func TestBindShape_LinkAttachKindSpecialisesDetails(t *testing.T) {
	t.Parallel()

	// `bpfman link attach <kind>` selects the concrete details
	// Shape for record.details so deep field-typo checks no
	// longer bail at the polymorphic interface boundary. The
	// per-kind shape comes from semantics' reflection over the
	// concrete LinkDetails implementer; a typo against a real
	// TCDetails field must surface, and a legitimate field must
	// stay clean.
	bogus := `guard l <- bpfman link attach tc 1 v ingress --priority 100
print $l.record.details.priroity`
	issues := checkSource(t, bogus)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"priroity"`)
	assert.Contains(t, issues[0].Msg, "priority")

	ok := `guard l <- bpfman link attach tc 1 v ingress --priority 100
print $l.record.details.priority
print $l.record.details.position`
	assert.Empty(t, checkSource(t, ok))
}

func TestBindShape_LinkAttachAcrossEveryRegisteredKind(t *testing.T) {
	t.Parallel()

	// Every attach kind the bpfman package recognises must
	// produce a sealed details Shape after reflection so a
	// deep typo is caught. "nonsense_field" is a synthetic
	// field name no LinkDetails struct legitimately exposes,
	// so a single check fires per kind regardless of the
	// concrete field set.
	for _, kind := range bpfman.LinkAttachKinds() {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			src := "guard l <- bpfman link attach " + kind +
				" arg arg arg\nprint $l.record.details.nonsense_field"
			issues := checkSource(t, src)
			require.NotEmpty(t, issues, "kind %s must seal record.details", kind)
			joined := joinIssues(issues)
			assert.Contains(t, joined, `"nonsense_field"`)
		})
	}
}

func TestBindShape_LinkMetadataIsOpenButRecordStaysSealed(t *testing.T) {
	t.Parallel()

	// record.metadata is a map[string]string, so its keys are
	// user-defined and dynamic: arbitrary keys must be allowed. Crucially,
	// the map field must NOT unseal the record -- a top-level field typo is
	// still caught. (A json.Marshaler on LinkRecord would unseal it; a
	// plain map does not.)
	okMeta := `guard l <- bpfman link attach kprobe 1 do_unlinkat
print $l.record.metadata.owner`
	assert.Empty(t, checkSource(t, okMeta), "record.metadata.<key> must be allowed (open map)")

	typo := `guard l <- bpfman link attach kprobe 1 do_unlinkat
print $l.record.detailz`
	issues := checkSource(t, typo)
	require.Len(t, issues, 1, "record must stay sealed: a top-level field typo is caught")
	assert.Contains(t, issues[0].Msg, `"detailz"`)
	assert.Contains(t, issues[0].Msg, "details")
}

func TestBindShape_LinkAttachUnknownKindFallsBackToGenericLink(t *testing.T) {
	t.Parallel()

	// A spelling that is not a registered attach kind leaves
	// record.details unsealed, so a deep field access against
	// it passes without complaint. The top-level Link fields
	// still validate, so a typo on `record` or `status` is
	// caught.
	clean := `guard l <- bpfman link attach mystery_kind 1
print $l.record.details.anything.goes.here`
	assert.Empty(t, checkSource(t, clean))

	bad := `guard l <- bpfman link attach mystery_kind 1
print $l.tortoise`
	issues := checkSource(t, bad)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, `"tortoise"`)
}

func joinIssues(issues []check.Issue) string {
	parts := make([]string, 0, len(issues))
	for _, i := range issues {
		parts = append(parts, i.Msg)
	}
	return strings.Join(parts, "\n")
}
