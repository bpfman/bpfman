package builtins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/internal/registryfixture"
)

func registryCtx(t *testing.T, args ...runtime.Arg) driver.Ctx {
	return driver.Ctx{Ctx: t.Context(), Args: args}
}

func TestHandleRegistryRefReturnsAliasReference(t *testing.T) {
	t.Parallel()

	v, err := handleRegistry(registryCtx(t,
		runtime.WordArg{Text: "ref"},
		runtime.WordArg{Text: "Explicit XDP Pass"},
	))
	require.NoError(t, err)
	ref, err := v.Scalar()
	require.NoError(t, err)

	assert.Contains(t, ref, registryfixture.RegistryAlias+"/"+registryfixture.RepositoryPrefix+"/explicit-xdp-pass:")
}

func TestHandleRegistryHostUsesEnvOverride(t *testing.T) {
	t.Setenv(registryfixture.RegistryEnv, "127.0.0.1:5000")

	v, err := handleRegistry(registryCtx(t, runtime.WordArg{Text: "host"}))
	require.NoError(t, err)
	host, err := v.Scalar()
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:5000", host)
}

func TestHandleRegistryURLUsesLoopbackHTTP(t *testing.T) {
	t.Setenv(registryfixture.RegistryEnv, "127.0.0.1:5000")

	v, err := handleRegistry(registryCtx(t, runtime.WordArg{Text: "url"}))
	require.NoError(t, err)
	url, err := v.Scalar()
	require.NoError(t, err)

	assert.Equal(t, "http://127.0.0.1:5000", url)
}

func TestRegistryBuiltinRegistered(t *testing.T) {
	t.Parallel()

	entry, ok := driver.Builtins()["registry"]
	require.True(t, ok, "registry is not in builtinRegistry")
	assert.NotNil(t, entry.Handler)
	assert.NotEmpty(t, entry.Usage)
	assert.NotEmpty(t, entry.Summary)
}
