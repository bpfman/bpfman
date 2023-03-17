package utils

import (
	"context"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/assert"
)

// DumpDiagnosticsIfFailed dumps the diagnostics if the test failed.
func DumpDiagnosticsIfFailed(ctx context.Context, t *testing.T, clusters clusters.Cluster) {
	t.Helper()

	if t.Failed() {
		output, err := clusters.DumpDiagnostics(ctx, t.Name())
		t.Logf("%s failed, dumped diagnostics to %s", t.Name(), output)
		assert.NoError(t, err)
	}
}
