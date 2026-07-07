package bpfman_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

func TestListOptions_NoOptions(t *testing.T) {
	t.Parallel()

	// Zero options should match everything
	opts := bpfman.ApplyListOptions()

	prog := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Load: bpfman.TestLoadSpec(bpfman.ProgramTypeXDP),
		},
	}
	assert.True(t, opts.Matches(prog), "no options should match all programs")
}

func TestListOptions_WithAttached(t *testing.T) {
	t.Parallel()

	opts := bpfman.ApplyListOptions(bpfman.WithAttached())

	progWithLinks := &bpfman.Program{
		Status: bpfman.ProgramStatus{
			Links: []bpfman.Link{
				{Status: bpfman.LinkStatus{Kernel: &kernel.Link{ID: 1}}},
			},
		},
	}
	progWithoutLinks := &bpfman.Program{
		Status: bpfman.ProgramStatus{
			Links: nil,
		},
	}
	progWithStaleLinks := &bpfman.Program{
		Status: bpfman.ProgramStatus{
			Links: []bpfman.Link{
				{Status: bpfman.LinkStatus{Kernel: nil}}, // DB record, no kernel presence
			},
		},
	}

	assert.True(t, opts.Matches(progWithLinks), "attached should match program with kernel links")
	assert.False(t, opts.Matches(progWithoutLinks), "attached should not match program without links")
	assert.False(t, opts.Matches(progWithStaleLinks), "attached should not match program with stale links")
}

func TestListOptions_WithUnattached(t *testing.T) {
	t.Parallel()

	opts := bpfman.ApplyListOptions(bpfman.WithUnattached())

	progWithLinks := &bpfman.Program{
		Status: bpfman.ProgramStatus{
			Links: []bpfman.Link{
				{Status: bpfman.LinkStatus{Kernel: &kernel.Link{ID: 1}}},
			},
		},
	}
	progWithoutLinks := &bpfman.Program{
		Status: bpfman.ProgramStatus{
			Links: nil,
		},
	}

	assert.False(t, opts.Matches(progWithLinks), "unattached should not match program with links")
	assert.True(t, opts.Matches(progWithoutLinks), "unattached should match program without links")
}

func TestListOptions_WithTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filterTypes []bpfman.ProgramType
		progType    bpfman.ProgramType
		expected    bool
	}{
		{
			name:        "single type matches",
			filterTypes: []bpfman.ProgramType{bpfman.ProgramTypeXDP},
			progType:    bpfman.ProgramTypeXDP,
			expected:    true,
		},
		{
			name:        "single type rejects",
			filterTypes: []bpfman.ProgramType{bpfman.ProgramTypeXDP},
			progType:    bpfman.ProgramTypeKprobe,
			expected:    false,
		},
		{
			name:        "multiple types - matches first",
			filterTypes: []bpfman.ProgramType{bpfman.ProgramTypeXDP, bpfman.ProgramTypeKprobe},
			progType:    bpfman.ProgramTypeXDP,
			expected:    true,
		},
		{
			name:        "multiple types - matches second",
			filterTypes: []bpfman.ProgramType{bpfman.ProgramTypeXDP, bpfman.ProgramTypeKprobe},
			progType:    bpfman.ProgramTypeKprobe,
			expected:    true,
		},
		{
			name:        "multiple types - rejects non-match",
			filterTypes: []bpfman.ProgramType{bpfman.ProgramTypeXDP, bpfman.ProgramTypeKprobe},
			progType:    bpfman.ProgramTypeFentry,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := bpfman.ApplyListOptions(bpfman.WithTypes(tt.filterTypes...))
			prog := &bpfman.Program{
				Record: bpfman.ProgramRecord{
					Load: bpfman.TestLoadSpec(tt.progType),
				},
			}
			assert.Equal(t, tt.expected, opts.Matches(prog))
		})
	}
}

func TestListOptions_WithTypes_Empty(t *testing.T) {
	t.Parallel()

	// Empty types should match all
	opts := bpfman.ApplyListOptions(bpfman.WithTypes())

	prog := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Load: bpfman.TestLoadSpec(bpfman.ProgramTypeXDP),
		},
	}
	assert.True(t, opts.Matches(prog), "empty types should match all programs")
}

func TestListOptions_MatchingLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filterLabels map[string]string
		progLabels   map[string]string
		expected     bool
	}{
		{
			name:         "exact match",
			filterLabels: map[string]string{"app": "test"},
			progLabels:   map[string]string{"app": "test"},
			expected:     true,
		},
		{
			name:         "subset match",
			filterLabels: map[string]string{"app": "test"},
			progLabels:   map[string]string{"app": "test", "version": "v1"},
			expected:     true,
		},
		{
			name:         "value mismatch",
			filterLabels: map[string]string{"app": "test"},
			progLabels:   map[string]string{"app": "other"},
			expected:     false,
		},
		{
			name:         "missing key",
			filterLabels: map[string]string{"app": "test"},
			progLabels:   map[string]string{"version": "v1"},
			expected:     false,
		},
		{
			name:         "nil prog labels",
			filterLabels: map[string]string{"app": "test"},
			progLabels:   nil,
			expected:     false,
		},
		{
			name:         "empty filter matches all",
			filterLabels: map[string]string{},
			progLabels:   map[string]string{"app": "test"},
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := bpfman.ApplyListOptions(bpfman.MatchingLabels(tt.filterLabels))
			prog := &bpfman.Program{
				Record: bpfman.ProgramRecord{
					Meta: bpfman.ProgramMeta{Metadata: tt.progLabels},
				},
			}
			assert.Equal(t, tt.expected, opts.Matches(prog))
		})
	}
}

func TestListOptions_MatchingSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		selector   string
		progLabels map[string]string
		expected   bool
	}{
		{
			name:       "equality selector matches",
			selector:   "app=test",
			progLabels: map[string]string{"app": "test"},
			expected:   true,
		},
		{
			name:       "equality selector rejects",
			selector:   "app=test",
			progLabels: map[string]string{"app": "other"},
			expected:   false,
		},
		{
			name:       "in selector matches",
			selector:   "app in (foo,bar)",
			progLabels: map[string]string{"app": "bar"},
			expected:   true,
		},
		{
			name:       "notin selector matches",
			selector:   "app notin (foo,bar)",
			progLabels: map[string]string{"app": "baz"},
			expected:   true,
		},
		{
			name:       "exists selector matches",
			selector:   "app",
			progLabels: map[string]string{"app": "test"},
			expected:   true,
		},
		{
			name:       "exists selector rejects",
			selector:   "app",
			progLabels: map[string]string{"other": "test"},
			expected:   false,
		},
		{
			name:       "not exists selector matches",
			selector:   "!debug",
			progLabels: map[string]string{"app": "test"},
			expected:   true,
		},
		{
			name:       "not exists selector rejects",
			selector:   "!debug",
			progLabels: map[string]string{"debug": "true"},
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sel, err := labels.Parse(tt.selector)
			require.NoError(t, err)

			opts := bpfman.ApplyListOptions(bpfman.MatchingSelector(sel))
			prog := &bpfman.Program{
				Record: bpfman.ProgramRecord{
					Meta: bpfman.ProgramMeta{Metadata: tt.progLabels},
				},
			}
			assert.Equal(t, tt.expected, opts.Matches(prog))
		})
	}
}

func TestListOptions_Combined(t *testing.T) {
	t.Parallel()

	// Program that matches all criteria
	matchingProg := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Load: bpfman.TestLoadSpec(bpfman.ProgramTypeXDP),
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
		},
		Status: bpfman.ProgramStatus{
			Links: []bpfman.Link{
				{Status: bpfman.LinkStatus{Kernel: &kernel.Link{ID: 1}}},
			},
		},
	}

	opts := bpfman.ApplyListOptions(
		bpfman.WithAttached(),
		bpfman.WithTypes(bpfman.ProgramTypeXDP),
		bpfman.MatchingLabels(map[string]string{"app": "test"}),
	)

	t.Run("all criteria match", func(t *testing.T) {
		t.Parallel()
		assert.True(t, opts.Matches(matchingProg))
	})

	t.Run("wrong type fails", func(t *testing.T) {
		t.Parallel()
		prog := &bpfman.Program{
			Record: bpfman.ProgramRecord{
				Load: bpfman.TestLoadSpec(bpfman.ProgramTypeKprobe),
				Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
			},
			Status: bpfman.ProgramStatus{
				Links: []bpfman.Link{
					{Status: bpfman.LinkStatus{Kernel: &kernel.Link{ID: 1}}},
				},
			},
		}
		assert.False(t, opts.Matches(prog))
	})

	t.Run("wrong labels fails", func(t *testing.T) {
		t.Parallel()
		prog := &bpfman.Program{
			Record: bpfman.ProgramRecord{
				Load: bpfman.TestLoadSpec(bpfman.ProgramTypeXDP),
				Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "other"}},
			},
			Status: bpfman.ProgramStatus{
				Links: []bpfman.Link{
					{Status: bpfman.LinkStatus{Kernel: &kernel.Link{ID: 1}}},
				},
			},
		}
		assert.False(t, opts.Matches(prog))
	})

	t.Run("not attached fails", func(t *testing.T) {
		t.Parallel()
		prog := &bpfman.Program{
			Record: bpfman.ProgramRecord{
				Load: bpfman.TestLoadSpec(bpfman.ProgramTypeXDP),
				Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
			},
			Status: bpfman.ProgramStatus{
				Links: nil,
			},
		}
		assert.False(t, opts.Matches(prog))
	})
}

func TestListOptions_MultipleWithTypes(t *testing.T) {
	t.Parallel()

	// Calling WithTypes multiple times should accumulate
	opts := bpfman.ApplyListOptions(
		bpfman.WithTypes(bpfman.ProgramTypeXDP),
		bpfman.WithTypes(bpfman.ProgramTypeKprobe),
	)

	xdpProg := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Load: bpfman.TestLoadSpec(bpfman.ProgramTypeXDP),
		},
	}
	kprobeProg := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Load: bpfman.TestLoadSpec(bpfman.ProgramTypeKprobe),
		},
	}
	tcProg := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Load: bpfman.TestLoadSpec(bpfman.ProgramTypeTC),
		},
	}

	assert.True(t, opts.Matches(xdpProg), "should match XDP")
	assert.True(t, opts.Matches(kprobeProg), "should match Kprobe")
	assert.False(t, opts.Matches(tcProg), "should not match TC")
}

// Edge case tests for list options.

func TestListOptions_MatchingLabels_EmptyValue(t *testing.T) {
	t.Parallel()

	// Empty label values are valid in Kubernetes
	opts := bpfman.ApplyListOptions(
		bpfman.MatchingLabels(map[string]string{"app": ""}),
	)

	progWithEmpty := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": ""}},
		},
	}
	progWithValue := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
		},
	}

	assert.True(t, opts.Matches(progWithEmpty), "should match program with empty label value")
	assert.False(t, opts.Matches(progWithValue), "should not match program with non-empty label value")
}

func TestListOptions_MatchingSelector_OverridesMatchingLabels(t *testing.T) {
	t.Parallel()

	// When both MatchingLabels and MatchingSelector are used,
	// the last one wins (they both set the same selector field)
	sel, err := labels.Parse("env=prod")
	require.NoError(t, err)

	opts := bpfman.ApplyListOptions(
		bpfman.MatchingLabels(map[string]string{"app": "test"}),
		bpfman.MatchingSelector(sel), // This overrides the above
	)

	progMatchesSelector := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"env": "prod"}},
		},
	}
	progMatchesLabels := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
		},
	}

	assert.True(t, opts.Matches(progMatchesSelector), "should match selector criteria")
	assert.False(t, opts.Matches(progMatchesLabels), "should not match overridden labels criteria")
}

func TestListOptions_MatchingLabels_NilMap(t *testing.T) {
	t.Parallel()

	// Nil map should match everything (no label constraints)
	opts := bpfman.ApplyListOptions(
		bpfman.MatchingLabels(nil),
	)

	prog := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
		},
	}

	assert.True(t, opts.Matches(prog), "nil MatchingLabels should match all")
}

func TestListOptions_MatchingSelector_Nil(t *testing.T) {
	t.Parallel()

	// Nil selector should match everything
	opts := bpfman.ApplyListOptions(
		bpfman.MatchingSelector(nil),
	)

	prog := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
		},
	}

	assert.True(t, opts.Matches(prog), "nil MatchingSelector should match all")
}

func TestListOptions_MatchingSelector_Everything(t *testing.T) {
	t.Parallel()

	// labels.Everything() should match all programs
	opts := bpfman.ApplyListOptions(
		bpfman.MatchingSelector(labels.Everything()),
	)

	prog := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
		},
	}
	progNoLabels := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: nil},
		},
	}

	assert.True(t, opts.Matches(prog), "Everything() should match program with labels")
	assert.True(t, opts.Matches(progNoLabels), "Everything() should match program without labels")
}

func TestListOptions_MatchingSelector_Nothing(t *testing.T) {
	t.Parallel()

	// labels.Nothing() should match no programs
	opts := bpfman.ApplyListOptions(
		bpfman.MatchingSelector(labels.Nothing()),
	)

	prog := &bpfman.Program{
		Record: bpfman.ProgramRecord{
			Meta: bpfman.ProgramMeta{Metadata: map[string]string{"app": "test"}},
		},
	}

	assert.False(t, opts.Matches(prog), "Nothing() should match no programs")
}

func TestListOptions_WithAttachedAndUnattached_LastWins(t *testing.T) {
	t.Parallel()

	// Multiple attachment options - last one wins
	opts := bpfman.ApplyListOptions(
		bpfman.WithAttached(),
		bpfman.WithUnattached(), // This wins
	)

	progWithLinks := &bpfman.Program{
		Status: bpfman.ProgramStatus{
			Links: []bpfman.Link{
				{Status: bpfman.LinkStatus{Kernel: &kernel.Link{ID: 1}}},
			},
		},
	}
	progWithoutLinks := &bpfman.Program{
		Status: bpfman.ProgramStatus{
			Links: nil,
		},
	}

	assert.False(t, opts.Matches(progWithLinks), "last option (unattached) should win")
	assert.True(t, opts.Matches(progWithoutLinks), "last option (unattached) should win")
}
