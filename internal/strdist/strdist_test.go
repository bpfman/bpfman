package strdist

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNearest_ShortExternalNamesDoNotMatchDistantCandidates(t *testing.T) {
	t.Parallel()

	assert.Empty(t, Nearest("ip", []string{"main"}, 1))
	assert.Empty(t, Nearest("go", []string{"load"}, 1))
}

func TestNearest_ShortNamesStillSuggestOneEditMisses(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"ip"}, Nearest("i", []string{"ip"}, 1))
	assert.Equal(t, []string{"run"}, Nearest("rn", []string{"run"}, 1))
}
