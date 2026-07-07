package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListProgramsCmd_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		attached   bool
		unattached bool
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "neither flag is valid",
			attached:   false,
			unattached: false,
			wantErr:    false,
		},
		{
			name:       "only attached is valid",
			attached:   true,
			unattached: false,
			wantErr:    false,
		},
		{
			name:       "only unattached is valid",
			attached:   false,
			unattached: true,
			wantErr:    false,
		},
		{
			name:       "both flags is invalid",
			attached:   true,
			unattached: true,
			wantErr:    true,
			errMsg:     "mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &ListProgramsCmd{
				Attached:   tt.attached,
				Unattached: tt.unattached,
			}
			err := cmd.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestListLinksProgramScopeOptions_ApplicationOnly guards against a nil
// map panic: --application without --metadata-selector must build a
// program-scope selector rather than writing to a nil map.
func TestListLinksProgramScopeOptions_ApplicationOnly(t *testing.T) {
	t.Parallel()

	c := &ListLinksCmd{Application: "demo"}
	opts, scoped := c.programScopeOptions()
	assert.True(t, scoped, "expected scoped=true when --application is set")
	assert.NotEmpty(t, opts, "expected a program-list option for --application")
}

// TestListProgramsBuildOptions_ApplicationOnly guards the same nil map
// panic on the program list path.
func TestListProgramsBuildOptions_ApplicationOnly(t *testing.T) {
	t.Parallel()

	c := &ListProgramsCmd{Application: "demo"}
	opts, err := c.buildListOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "expected a selector option for --application")
}
