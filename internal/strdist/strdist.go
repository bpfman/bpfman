// Package strdist holds string-distance primitives shared between
// the manager (tracepoint and syscall suggestions) and the shell
// (suggested-field hints in static-check diagnostics).
package strdist

import (
	"cmp"
	"slices"
)

// Levenshtein returns the Levenshtein edit distance between two
// strings, counting insertions, deletions, and substitutions each
// as cost 1. Inputs are iterated rune-by-rune so multi-byte
// characters count correctly.
func Levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	m, n := len(ar), len(br)
	if m == 0 {
		return n
	}
	if n == 0 {
		return m
	}
	prev := make([]int, n+1)
	curr := make([]int, n+1)
	for j := 0; j <= n; j++ {
		prev[j] = j
	}
	for i := 1; i <= m; i++ {
		curr[0] = i
		for j := 1; j <= n; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[n]
}

// Nearest returns up to limit entries from candidates that are
// closest to target by Levenshtein distance, breaking ties
// lexicographically.
//
// Two caps filter the results. An absolute cap of
// max(len(target)/2, 3) drops entries that are more than half an
// edit away and keeps nonsense input from yielding unrelated
// matches. Very short targets are capped at one edit; otherwise
// common external command names such as "ip" or "go" pick up noisy
// suggestions from unrelated defs. A relative cap of 2 *
// bestDistance, applied on top, trims the tail when there is a
// clearly better candidate: if the closest match is a single edit
// away, a candidate three edits away is almost certainly
// coincidental (long shared prefix, happens-to-match substring) and
// is dropped. When the closest match is itself distant, the relative
// cap relaxes and the absolute cap dominates.
func Nearest(target string, candidates []string, limit int) []string {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	type scored struct {
		s    string
		dist int
	}
	scores := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		scores = append(scores, scored{s: c, dist: Levenshtein(target, c)})
	}
	slices.SortFunc(scores, func(a, b scored) int {
		if c := cmp.Compare(a.dist, b.dist); c != 0 {
			return c
		}
		return cmp.Compare(a.s, b.s)
	})
	targetLen := len([]rune(target))
	maxDist := max(targetLen/2, 3)
	if targetLen <= 3 {
		maxDist = 1
	}
	if tight := 2 * scores[0].dist; tight < maxDist {
		maxDist = tight
	}
	out := make([]string, 0, limit)
	for _, s := range scores {
		if len(out) >= limit {
			break
		}
		if s.dist > maxDist {
			break
		}
		out = append(out, s.s)
	}
	return out
}
