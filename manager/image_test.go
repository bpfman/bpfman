package manager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

func TestPullBytecode_UsesConfiguredImagePuller(t *testing.T) {
	t.Parallel()

	puller := newFakeImagePuller()
	puller.SetObjectPath("/cache/program.o")
	fix := newTestFixtureWithOptions(t, nil, puller)
	ctx := context.Background()

	ref := platform.ImageRef{
		URL:        "quay.example.com/org/prog:latest",
		PullPolicy: bpfman.PullAlways,
		Auth: &platform.ImageAuth{
			Username: "user",
			Password: "pass",
		},
	}

	pulled, err := fix.Manager.PullBytecode(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "/cache/program.o", pulled.ObjectPath)
	assert.Equal(t, "sha256:fake", pulled.Digest)
	assert.Equal(t, []platform.ImageRef{ref}, puller.Pulls())
}

func TestPullBytecode_RequiresConfiguredImagePuller(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	_, err := fix.Manager.PullBytecode(ctx, platform.ImageRef{URL: "quay.example.com/org/prog:latest"})
	require.ErrorIs(t, err, manager.ErrImagePullerNotConfigured)
}

func TestPullBytecode_ReturnsPullError(t *testing.T) {
	t.Parallel()

	pullErr := errors.New("pull failed")
	puller := newFakeImagePuller()
	puller.SetPullError(pullErr)
	fix := newTestFixtureWithOptions(t, nil, puller)
	ctx := context.Background()

	ref := platform.ImageRef{URL: "quay.example.com/org/prog:latest"}
	_, err := fix.Manager.PullBytecode(ctx, ref)
	require.ErrorIs(t, err, pullErr)
	assert.Equal(t, []platform.ImageRef{ref}, puller.Pulls())
}
