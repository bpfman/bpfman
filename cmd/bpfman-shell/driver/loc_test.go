package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSourceLoc_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		loc  SourceLoc
		want string
	}{
		{"zero value", SourceLoc{}, ""},
		{"with file and line", SourceLoc{File: "test.bpfman", Line: 5}, "test.bpfman:5: "},
		{"line one", SourceLoc{File: "script.bpfman", Line: 1}, "script.bpfman:1: "},
		{"with column", SourceLoc{File: "test.bpfman", Line: 5, Col: 9}, "test.bpfman:5:9: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.loc.String())
		})
	}
}
