package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func TestArgTexts(t *testing.T) {
	t.Parallel()

	args := []runtime.Arg{
		runtime.WordArg{Text: "show"},
		runtime.WordArg{Text: "program"},
		runtime.ScalarValueArg{Text: "42"},
	}
	got := ArgTexts(args)
	assert.Equal(t, []string{"show", "program", "42"}, got)
}

func TestArgTexts_Empty(t *testing.T) {
	t.Parallel()

	got := ArgTexts(nil)
	assert.Empty(t, got)
}

func TestArgTexts_StructuredValueArg(t *testing.T) {
	t.Parallel()

	args := []runtime.Arg{
		runtime.WordArg{Text: "show"},
		runtime.WordArg{Text: "program"},
		runtime.StructuredValueArg{Name: "prog", Value: runtime.ValueFromMap(map[string]any{"id": "42"})},
	}
	got := ArgTexts(args)
	assert.Equal(t, []string{"show", "program", "$prog"}, got)
}

func TestArgTexts_QuotedArg(t *testing.T) {
	t.Parallel()

	args := []runtime.Arg{
		runtime.WordArg{Text: "load"},
		runtime.QuotedArg{Text: "my file.o"},
	}
	got := ArgTexts(args)
	assert.Equal(t, []string{"load", "my file.o"}, got)
}
