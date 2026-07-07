package semantics

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPureBuiltinRegistry_JqRegisteredByPackageInit(t *testing.T) {
	t.Parallel()

	pb, ok := lookupPureBuiltin("jq")
	require.True(t, ok, "shell package init should register jq")
	assert.Equal(t, "jq", pb.Name)
	assert.Equal(t, 2, pb.Arity)
	assert.False(t, pb.ReturnShape.Sealed, "jq's return shape is unknown so any path access is permitted")
	assert.Equal(t, OriginUnknown, pb.ReturnShape.Kind)
}

func TestPureBuiltinRegistry_UnregisteredNameLookupFails(t *testing.T) {
	t.Parallel()

	assert.False(t, IsPureBuiltin("definitely-not-a-pure-builtin"))
}

func TestPureBuiltinRegistry_U32leContract(t *testing.T) {
	t.Parallel()

	pb, ok := lookupPureBuiltin("u32le")
	require.True(t, ok, "u32le should be part of the static pure-builtin table")
	assert.Equal(t, 1, pb.Arity)
	assert.Equal(t, OriginScalar, pb.ReturnShape.Kind)
	assert.Equal(t, pureLiteralContractUnsigned, pb.LiteralContract.kind)
	assert.Equal(t, 32, pb.LiteralContract.bits)
	assert.Equal(t, uint64(math.MaxUint32), pb.LiteralContract.max)
}
