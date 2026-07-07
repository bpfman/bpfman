package logging_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/logging"
)

func TestParseSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantBase   logging.Level
		wantComps  map[string]logging.Level
		wantErr    bool
		errContain string
	}{
		{
			name:     "empty string defaults to warn",
			input:    "",
			wantBase: logging.LevelWarn,
		},
		{
			name:     "base level only",
			input:    "debug",
			wantBase: logging.LevelDebug,
		},
		{
			name:      "single component override",
			input:     "info,manager=debug",
			wantBase:  logging.LevelInfo,
			wantComps: map[string]logging.Level{"manager": logging.LevelDebug},
		},
		{
			name:      "multiple component overrides",
			input:     "warn,manager=debug,store=trace",
			wantBase:  logging.LevelWarn,
			wantComps: map[string]logging.Level{"manager": logging.LevelDebug, "store": logging.LevelTrace},
		},
		{
			name:      "with whitespace",
			input:     "  info , manager = debug , store = trace  ",
			wantBase:  logging.LevelInfo,
			wantComps: map[string]logging.Level{"manager": logging.LevelDebug, "store": logging.LevelTrace},
		},
		{
			name:      "component only (no base level specified)",
			input:     "manager=debug",
			wantBase:  logging.LevelWarn,
			wantComps: map[string]logging.Level{"manager": logging.LevelDebug},
		},
		{
			name:       "invalid base level",
			input:      "invalid",
			wantErr:    true,
			errContain: "unknown log level",
		},
		{
			name:       "invalid component level",
			input:      "info,manager=invalid",
			wantErr:    true,
			errContain: "invalid level for component",
		},
		{
			name:       "base level not first",
			input:      "manager=debug,info",
			wantErr:    true,
			errContain: "must be first",
		},
		{
			name:       "empty component name",
			input:      "info,=debug",
			wantErr:    true,
			errContain: "empty component name",
		},
		{
			name:      "empty parts are skipped",
			input:     "info,,manager=debug,",
			wantBase:  logging.LevelInfo,
			wantComps: map[string]logging.Level{"manager": logging.LevelDebug},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := logging.ParseSpec(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBase, got.BaseLevel)

			if tt.wantComps == nil {
				assert.Empty(t, got.Components)
			} else {
				assert.Equal(t, tt.wantComps, got.Components)
			}
		})
	}
}

func TestSpec_LevelFor(t *testing.T) {
	t.Parallel()

	spec := logging.Spec{
		BaseLevel: logging.LevelWarn,
		Components: map[string]logging.Level{
			"manager": logging.LevelDebug,
			"store":   logging.LevelTrace,
		},
	}

	tests := []struct {
		component string
		want      logging.Level
	}{
		{"manager", logging.LevelDebug},
		{"store", logging.LevelTrace},
		{"server", logging.LevelWarn},  // falls back to base
		{"", logging.LevelWarn},        // empty falls back to base
		{"unknown", logging.LevelWarn}, // unknown falls back to base
	}

	for _, tt := range tests {
		t.Run(tt.component, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, spec.LevelFor(tt.component))
		})
	}
}

func TestSpec_String(t *testing.T) {
	t.Parallel()

	spec := logging.Spec{
		BaseLevel:  logging.LevelInfo,
		Components: map[string]logging.Level{},
	}
	assert.Equal(t, "info", spec.String())

	// With components - order may vary due to map iteration
	spec.Components["manager"] = logging.LevelDebug
	s := spec.String()
	assert.Contains(t, s, "info")
	assert.Contains(t, s, "manager=debug")
}
