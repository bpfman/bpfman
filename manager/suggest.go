// Tracepoint name suggestions: design notes.
//
// # Goal
//
// When a user runs `bpfman link attach tracepoint <prog-id>
// <group/name>` and no such tracepoint exists, return up to three
// plausible alternatives from the kernel catalogue. The catalogue is
// in the low thousands of entries; the user input is short; the
// output appears inline in an error message where a wrong suggestion
// is cheap (the user rejects it and retypes) but silence on a
// meaningful fragment is expensive (the user has to list and grep
// the catalogue by hand).
//
// # Stack
//
//	levenshtein          primitive edit distance (no transpositions)
//	nearestStrings       generic ranker with caps; for any flat
//	                     string list that needs a suggestion
//	tracepointDistance   per-candidate scoring, split-aware
//	nearestTracepoints   tracepoint-specific ranker: scoring delegates
//	                     to tracepointDistance, with the same cap
//	                     shape as nearestStrings but a taller floor
//
// The primitive is plain Levenshtein, not Damerau-Levenshtein: a
// transposition like "siwtch" -> "switch" costs two edits here, not
// one. Deliberate simplicity; upgrading is a localised change in
// levenshtein() if the cost of the noise ever justifies it.
//
// # Two layers of tolerance
//
// A candidate is surfaced if it survives both:
//
//  1. Per-candidate score -- how close is THIS candidate to the
//     target, on a scale where each unit is roughly one keystroke of
//     error. Cheaper when the shape of the error is known (wrong
//     group, partial name, etc.); falls back to whole-string distance
//     otherwise.
//
//  2. Top-level caps -- how close does the BEST candidate need to be
//     before we show anything, and how close do the runners-up need
//     to be relative to the best. Two caps combine:
//
//       absolute floor: max(len([]rune(target))/2, F)
//         A candidate must lie within this many edits, regardless
//         of anything else. Prevents nonsense like "x/y" yielding
//         unrelated matches. F is 3 in nearestStrings (the generic
//         primitive stays conservative) and 5 in nearestTracepoints
//         (split-aware scoring earns the headroom; see the code
//         comment by the cap for the worked example).
//
//       relative cap:   2 * bestDistance
//         Applied on top when tighter than the absolute floor. When
//         the top match is one edit away, a candidate three edits
//         away is almost certainly coincidental (long shared prefix,
//         happens-to-match substring) and is dropped. When the top
//         match is far, this relaxes and the absolute cap dominates.
//
// The caps are intentionally boring. Most of the intelligence lives
// in the per-candidate score.
//
// # Why split-aware scoring
//
// Tracepoint paths are "group/name" and the two halves are
// semantically independent. A naive full-string Levenshtein dilutes
// the signal: the shared "/" and long shared prefixes inflate the
// match budget, letting unrelated candidates through; at the same
// time, a typo confined to one half is charged against the full
// length.
//
// tracepointDistance handles each shape of error as its own branch:
//
//   - exact match: 0.
//   - same group, different name: name-only Levenshtein. The user
//     got the group right; treat them as asking "which variant of
//     this family did I mean?"
//   - wrong group, exact name: group-only Levenshtein. The user got
//     the name right; treat them as asking "which group does this
//     tracepoint live in?"
//   - prefix match (same group): score 0, treated as a family menu.
//     "sched/sched" returns every tracepoint in sched whose name
//     starts with "sched". Only triggers once tn is at least
//     tracepointNamePrefixMin characters, to avoid one-letter
//     fragments turning into thousand-entry dumps.
//   - prefix match (wrong group): score = group distance. Mirrors
//     the exact-name branch: the user gave us the start of a real
//     name under the wrong group; show the real group at the usual
//     group-distance cost.
//   - everything else: full-string Levenshtein. A last-ditch branch
//     for inputs that don't cleanly fit any of the above.
//
// # Why there's a no-slash fallback
//
// If the user forgets the group ("sched_switch" instead of
// "sched/sched_switch"), every candidate is longer than the target
// by roughly the length of the absent group prefix. Scoring against
// the full candidate path charges that prefix as edit cost, busting
// the absolute cap for anything but a perfect tail match. A single
// typo in a no-slash name yields no suggestion at all, which was the
// whole point of having a suggestion layer.
//
// nearestTracepoints strips the group prefix from candidates before
// scoring when the target has no slash. The ranking is still driven
// by Levenshtein; the result carries the full "group/name" form so
// the user can copy it straight back to the command line.
//
// # Knobs
//
// The layers above have explicit tuning points. All of them live in
// this file; if you change one, update the comments here at the same
// time so future readers don't have to reverse-engineer the intent.
//
// To be MORE generous (wider suggestion menu):
//
//   - Lower tracepointNamePrefixMin from 3 to 2. Accepts "s/sc" etc.
//     Noisy on short prefixes but trivially reversible.
//   - Raise the absolute-cap floors: nearestStrings from 3, or
//     nearestTracepoints from 5. Tolerates more edits on short
//     targets.
//   - Widen the relative cap from 2*best to 3*best. Keeps more tail
//     matches when the top candidate is close.
//   - Add substring matching as a further branch alongside prefix:
//     `strings.Contains(cn, tn)` with the same length guard. Catches
//     "foo/switch" -> "sched/sched_switch".
//   - Upgrade levenshtein to Damerau-Levenshtein. Small algorithm
//     change; benefits real-world adjacent-key typos.
//   - Bump the limit passed by callers (currently 3). Rarely the
//     right lever -- three is enough to spot a family.
//
// To be MORE STRICT (fewer suggestions, higher precision):
//
//   - Raise tracepointNamePrefixMin to 4 or 5. Short prefixes stop
//     matching.
//   - Drop the relative cap's "2*" multiplier to "1*": only matches
//     tying with the best are surfaced. Eliminates tail noise at
//     the cost of useful runner-ups.
//   - Remove the prefix branches from tracepointDistance. Falls back
//     to the original strict scoring: exact-group-or-name, else
//     full-string distance.
//   - Lower the absolute-cap floor in nearestTracepoints back to 3
//     (or below). Short targets collapse to a near-zero cap and
//     yield nothing.
//
// # What this algorithm deliberately does NOT do
//
//   - Phonetic matching (Soundex/Metaphone). Tracepoint names are
//     underscore-heavy identifiers, not words; phonetics buy little.
//   - Semantic search. "sched" is not synonymous with "task" here;
//     keep the matching syntactic.
//   - Fuzzy group-match when the name has no exact or prefix hit.
//     Could be added as a further branch, but "foo/xyz" with no
//     signal in either half is genuinely ambiguous and silence is
//     acceptable.
//   - Learning / weighting by historical user selection. Out of
//     scope; would need persistent state and a feedback channel.
//
// # Testing posture
//
// Unit tests live in suggest_test.go and use small synthetic
// catalogues. Do NOT bundle the live kernel catalogue as testdata:
// it is machine-specific, drifts with kernel version, and will fail
// on CI boxes that differ from dev machines. When a real-world probe
// turns up a regression-worthy case, distil it into a synthetic
// fixture inside suggest_test.go.

package manager

import (
	"cmp"
	"slices"
	"strings"

	"github.com/bpfman/bpfman/internal/strdist"
)

// levenshtein and nearestStrings are thin shims onto the shared
// internal/strdist primitives so the existing tracepoint-aware
// scoring code below can keep its short call-site names without
// breaking parity with the static checker's "did you mean"
// suggestions.
func levenshtein(a, b string) int { return strdist.Levenshtein(a, b) }

func nearestStrings(target string, candidates []string, limit int) []string {
	return strdist.Nearest(target, candidates, limit)
}

// nearestTracepoints returns up to limit tracepoint paths from
// candidates that are closest to target, using a group/name aware
// score on top of Levenshtein. Tracepoint paths have exactly one '/';
// treating "group" and "name" as independent units avoids false
// positives caused by a long shared prefix diluting the edit-distance
// ratio.
//
// Per-candidate score when target contains a '/' (see the design notes
// at the top of this file for the full rationale):
//
//	target tg/tn, candidate cg/cn, prefix = len(tn) >= 3 && cn starts with tn
//	  tg == cg, tn == cn               -> 0                        // exact match
//	  tg == cg, cn != tn, prefix       -> 0                        // menu of extensions in the same group
//	  tg == cg, cn != tn               -> levenshtein(tn, cn)      // typo in the name
//	  tg != cg, tn == cn               -> levenshtein(tg, cg)      // wrong group, right name
//	  tg != cg, tn != cn, prefix       -> levenshtein(tg, cg)      // partial name + wrong group
//	  otherwise                        -> levenshtein(target, candidate)
//
// When target has no '/', each candidate is scored against its name
// portion only (the group prefix is stripped). Scoring against the
// full path would charge the user the length of the absent group for
// every candidate, which pushes single-typo inputs like
// "sched_swtch" past the absolute cap and yields no suggestion.
//
// The same absolute and relative caps used by nearestStrings apply on
// top of this score.
func nearestTracepoints(target string, candidates []string, limit int) []string {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	tg, tn, hasSlash := strings.Cut(target, "/")

	type scored struct {
		s    string
		dist int
	}
	scores := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		var d int
		if hasSlash {
			d = tracepointDistance(tg, tn, c)
		} else {
			_, cn, ok := strings.Cut(c, "/")
			if !ok {
				cn = c
			}

			d = levenshtein(target, cn)
		}
		scores = append(scores, scored{s: c, dist: d})
	}
	slices.SortFunc(scores, func(a, b scored) int {
		if c := cmp.Compare(a.dist, b.dist); c != 0 {
			return c
		}
		return cmp.Compare(a.s, b.s)
	})

	// Floor of 5 (vs 3 in nearestStrings) lets cross-group prefix
	// matches like "foo/sched" -> "sched/sched_switch" surface: the
	// group distance levenshtein("foo","sched") is 5, which would be
	// filtered under the generic floor. Tracepoint paths have more
	// structure than plain strings, so a slightly more generous floor
	// is paid for by split-aware scoring's sharper ranking.
	maxDist := max(len([]rune(target))/2, 5)
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

// tracepointNamePrefixMin is the minimum name length at which a
// prefix match is treated as a real signal. Below this, a fragment
// like "s" or "sc" would pull in hundreds of unrelated tracepoints.
// See the Knobs section at the top of this file for tuning.
const tracepointNamePrefixMin = 3

// tracepointDistance computes the split-aware distance between a
// target (pre-split into tg/tn) and a single candidate. See
// nearestTracepoints for the ranking rules.
func tracepointDistance(tg, tn, candidate string) int {
	cg, cn, ok := strings.Cut(candidate, "/")
	if !ok {
		return levenshtein(tg+"/"+tn, candidate)
	}

	namePrefix := len(tn) >= tracepointNamePrefixMin && strings.HasPrefix(cn, tn)
	switch {
	case cg == tg && cn == tn:
		return 0
	case cg == tg:
		if namePrefix {
			return 0
		}
		return levenshtein(tn, cn)
	case cn == tn:
		return levenshtein(tg, cg)
	default:
		if namePrefix {
			return levenshtein(tg, cg)
		}
		return levenshtein(tg+"/"+tn, candidate)
	}
}
