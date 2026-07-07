package manager

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLevenshtein(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want int
	}{
		// Identity and empty-string boundaries.
		{"both empty", "", "", 0},
		{"empty vs three-char", "", "abc", 3},
		{"three-char vs empty", "abc", "", 3},
		{"identical single char", "a", "a", 0},
		{"identical long", "sched/sched_switch", "sched/sched_switch", 0},

		// Single-edit primitives.
		{"single substitution", "a", "b", 1},
		{"single insertion", "a", "ab", 1},
		{"single deletion", "ab", "a", 1},

		// Pure insertion and deletion runs.
		{"pure insertion", "a", "abc", 2},
		{"pure deletion", "abc", "a", 2},

		// Substitutions mid-word and the classic wiki example.
		{"kitten vs sitting", "kitten", "sitting", 3},

		// Case sensitivity: each letter differs.
		{"case sensitive", "abc", "ABC", 3},

		// Transposition is two edits in plain Levenshtein (not
		// Damerau-Levenshtein, which would be one).
		{"transposition is two edits", "ab", "ba", 2},

		// Disjoint strings of equal length: every character differs.
		{"disjoint equal length", "abcd", "wxyz", 4},

		// Large shared prefix: only the tail differs.
		{"shared prefix diverges at end", "abcdef", "abcdxy", 2},

		// Large shared suffix: only the head differs.
		{"shared suffix diverges at start", "xyabc", "wwabc", 2},

		// Multi-byte runes: compared rune-by-rune, not byte-by-byte,
		// so "héllo" differs from "hello" by one rune.
		{"unicode single-rune substitution", "héllo", "hello", 1},

		// Realistic tracepoint typos.
		{"single-letter typo mid-word", "sched/sched_suitch", "sched/sched_switch", 1},
		{"wrong category prefix", "syscalls/sched_switch", "sched/sched_switch", 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, levenshtein(tt.a, tt.b))
			assert.Equal(t, tt.want, levenshtein(tt.b, tt.a), "levenshtein should be symmetric")
		})
	}
}

// TestLevenshtein_TriangleInequality is a property check on a handful
// of realistic inputs: for any three strings d(a,c) <= d(a,b) + d(b,c).
func TestLevenshtein_TriangleInequality(t *testing.T) {
	t.Parallel()

	strs := []string{
		"",
		"a",
		"abc",
		"sched/sched_switch",
		"syscalls/sys_enter_kill",
		"héllo",
	}
	for _, a := range strs {
		for _, b := range strs {
			for _, c := range strs {
				ac := levenshtein(a, c)
				ab := levenshtein(a, b)
				bc := levenshtein(b, c)
				assert.LessOrEqual(t, ac, ab+bc, "triangle inequality broken for %q, %q, %q: %d > %d + %d", a, b, c, ac, ab, bc)
			}
		}
	}
}

func TestNearestStrings(t *testing.T) {
	t.Parallel()

	candidates := []string{
		"sched/sched_switch",
		"sched/sched_wakeup",
		"syscalls/sys_enter_kill",
		"syscalls/sys_enter_close",
		"net/netif_rx",
	}

	t.Run("single-character typo surfaces the match", func(t *testing.T) {
		t.Parallel()
		got := nearestStrings("sched/sched_suitch", candidates, 3)
		assert.NotEmpty(t, got)
		assert.Equal(t, "sched/sched_switch", got[0])
	})

	t.Run("wrong category picks the right real tracepoint", func(t *testing.T) {
		t.Parallel()
		got := nearestStrings("syscalls/sched_switch", candidates, 3)
		assert.Contains(t, got, "sched/sched_switch")
	})

	t.Run("unrelated garbage returns nothing", func(t *testing.T) {
		t.Parallel()
		got := nearestStrings("jkfdhjkasdf/qwerqwer", candidates, 3)
		assert.Empty(t, got)
	})

	t.Run("limit caps the output", func(t *testing.T) {
		t.Parallel()
		got := nearestStrings("sched/sched", candidates, 2)
		assert.LessOrEqual(t, len(got), 2)
	})

	t.Run("empty inputs yield nothing", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, nearestStrings("foo", nil, 3))
		assert.Empty(t, nearestStrings("foo", []string{}, 3))
		assert.Empty(t, nearestStrings("foo", candidates, 0))
	})

	t.Run("ties broken lexicographically", func(t *testing.T) {
		t.Parallel()
		// Two candidates at equal distance from "ac": "ab" and "ad"
		// (both distance 1). Lex order puts "ab" first.
		got := nearestStrings("ac", []string{"ad", "ab"}, 2)
		assert.Equal(t, []string{"ab", "ad"}, got)
	})

	t.Run("distance cap trims distant matches", func(t *testing.T) {
		t.Parallel()
		// Target length 4 means maxDist = max(4/2, 3) = 3. "a"
		// (distance 3) is on the edge; "wxyz" (distance 4 vs "abcd")
		// must be dropped.
		got := nearestStrings("abcd", []string{"abce", "wxyz"}, 5)
		assert.Equal(t, []string{"abce"}, got)
	})

	t.Run("limit of one returns the single best", func(t *testing.T) {
		t.Parallel()
		got := nearestStrings("sched/sched_switch", candidates, 1)
		assert.Equal(t, []string{"sched/sched_switch"}, got)
	})

	t.Run("negative limit is treated as zero", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, nearestStrings("foo", candidates, -1))
	})

	t.Run("relative cap trims weak tail when best match is close", func(t *testing.T) {
		t.Parallel()
		// The target shares a long prefix with the candidates, so the
		// absolute cap based on length (max(23/2, 3) = 11) is very
		// permissive. With a close match at distance 1, the relative
		// cap (2 * best = 2) kicks in and filters the coincidental
		// distance-3 match whose only resemblance is a shared 'll'
		// substring after the long prefix.
		target := "syscalls/sys_enter_killx"
		cands := []string{
			"syscalls/sys_enter_kill",  // dist 1: drop trailing 'x'
			"syscalls/sys_enter_tkill", // dist 2: insert 't', drop 'x'
			"syscalls/sys_enter_poll",  // dist 3: coincidental 'll'
		}
		got := nearestStrings(target, cands, 5)
		assert.Equal(t, []string{
			"syscalls/sys_enter_kill",
			"syscalls/sys_enter_tkill",
		}, got)
	})

	t.Run("relative cap relaxes when best match is distant", func(t *testing.T) {
		t.Parallel()
		// When the best match is itself several edits away, the
		// relative cap doubles and still admits it. Here the target
		// has the wrong category prefix: the nearest match is six
		// edits away, the relative cap becomes twelve, and the
		// absolute cap (max(len/2, 3)) dominates.
		target := "syscalls/sched_switch"
		cands := []string{
			"sched/sched_switch", // dist 6 (syscalls/ vs sched/)
		}
		got := nearestStrings(target, cands, 3)
		assert.Contains(t, got, "sched/sched_switch")
	})
}

func TestNearestTracepoints(t *testing.T) {
	t.Parallel()

	candidates := []string{
		"sched/sched_switch",
		"sched/sched_wakeup",
		"syscalls/sys_enter_kill",
		"syscalls/sys_enter_tkill",
		"syscalls/sys_enter_poll",
		"syscalls/sys_enter_close",
		"net/netif_rx",
	}

	t.Run("typo in name ranks same-group candidates by name only", func(t *testing.T) {
		t.Parallel()
		// Target group matches syscalls/; the ranking is driven by
		// name distance (kill=1, tkill=2), and name-only scoring
		// prevents the long shared prefix from diluting the relative
		// cap, so poll (name-distance 3) is filtered.
		got := nearestTracepoints("syscalls/sys_enter_killx", candidates, 3)
		assert.Equal(t, []string{
			"syscalls/sys_enter_kill",
			"syscalls/sys_enter_tkill",
		}, got)
	})

	t.Run("wrong group but correct name ranks by group distance", func(t *testing.T) {
		t.Parallel()
		// Target name "sched_switch" exists under group "sched"; the
		// real tracepoint is sched/sched_switch but the user typed
		// syscalls/sched_switch. Group-only scoring surfaces the
		// correct match at distance 6 (levenshtein("syscalls",
		// "sched")) ahead of same-group candidates whose names are
		// totally unrelated.
		got := nearestTracepoints("syscalls/sched_switch", candidates, 3)
		require.NotEmpty(t, got)
		assert.Equal(t, "sched/sched_switch", got[0])
	})

	t.Run("fallback to full-string distance for unrelated both", func(t *testing.T) {
		t.Parallel()
		// Neither group nor name matches anything in the list; all
		// scores fall back to full-string distance. The best match is
		// whichever candidate has the most characters in common.
		got := nearestTracepoints("foo/bar_baz", candidates, 3)
		for _, s := range got {
			assert.Contains(t, candidates, s)
		}
	})

	t.Run("target without a slash scores against candidate names only", func(t *testing.T) {
		t.Parallel()
		// When the user omits the group, ranking against the full
		// "group/name" path would charge the length of the absent
		// group as edit distance and exhaust the absolute cap. Scoring
		// against the name portion lands a bare tracepoint name on the
		// right "group/name".
		got := nearestTracepoints("sched_switch", candidates, 3)
		require.NotEmpty(t, got)
		assert.Equal(t, "sched/sched_switch", got[0])
	})

	t.Run("target without a slash tolerates a single typo", func(t *testing.T) {
		t.Parallel()
		// Name-only scoring keeps "sched_swtch" against
		// "sched/sched_switch" within the absolute cap: a single
		// typo in the name portion scores below the cap even though
		// the full strings differ by the group prefix too.
		got := nearestTracepoints("sched_swtch", candidates, 3)
		assert.Contains(t, got, "sched/sched_switch")
	})

	t.Run("target without a slash still handles flat candidates", func(t *testing.T) {
		t.Parallel()
		// Defensive: candidates without a '/' are scored whole.
		got := nearestTracepoints("sched", []string{"sched", "other"}, 3)
		assert.Contains(t, got, "sched")
	})

	t.Run("name prefix under the correct group returns the family", func(t *testing.T) {
		t.Parallel()
		// "sched/sched" is not a real tracepoint, but the user has
		// supplied the right group and the start of a real name. The
		// prefix branch scores every matching candidate as 0, and the
		// tie-breaking limit cuts the family at three.
		got := nearestTracepoints("sched/sched", candidates, 3)
		require.NotEmpty(t, got)
		for _, s := range got {
			assert.True(t, strings.HasPrefix(s, "sched/sched_"), "expected only sched/sched_* entries, got %q", s)
		}
	})

	t.Run("name prefix under the wrong group surfaces the right group", func(t *testing.T) {
		t.Parallel()
		// "foo/sched" has no exact group or name hit. The prefix
		// branch in the default case scores by group distance alone,
		// so sched/* candidates surface at distance levenshtein("foo",
		// "sched") = 4, within the cap for a 9-char target.
		got := nearestTracepoints("foo/sched", candidates, 3)
		require.NotEmpty(t, got)
		for _, s := range got {
			assert.True(t, strings.HasPrefix(s, "sched/sched_"), "expected only sched/sched_* entries, got %q", s)
		}
	})

	t.Run("name prefix below the length threshold is ignored", func(t *testing.T) {
		t.Parallel()
		// A one- or two-character fragment is not a signal. Without
		// the threshold, "foo/sc" would sweep in every sched_*
		// candidate and more. The threshold drops it back to
		// full-string Levenshtein, which cannot rescue such a short
		// target.
		assert.Empty(t, nearestTracepoints("foo/sc", candidates, 3))
	})

	t.Run("empty inputs yield nothing", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, nearestTracepoints("sched/switch", nil, 3))
		assert.Empty(t, nearestTracepoints("sched/switch", candidates, 0))
	})
}
