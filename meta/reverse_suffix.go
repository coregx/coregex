package meta

import (
	"errors"

	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// ErrNoPrefilter indicates that no prefilter could be built for suffix literals.
// This is not a fatal error - it just means ReverseSuffix optimization cannot be used.
var ErrNoPrefilter = errors.New("no prefilter available for suffix literals")

// ReverseSuffixSearcher performs suffix literal prefilter + reverse DFA search.
//
// This strategy is used for patterns with literal suffixes like `.*\.txt` where:
//   - The pattern is NOT anchored at start (^)
//   - Has a good suffix literal for prefiltering
//   - Can use reverse DFA to verify the prefix pattern
//
// Algorithm:
//  1. Extract suffix literals from pattern
//  2. Build prefilter for suffix literals
//  3. Search algorithm:
//     a. Prefilter finds suffix candidates in haystack
//     b. For each candidate:
//     - Build reverse search from haystack start to suffix end
//     - Use reverse DFA to verify prefix pattern
//     - If match, use forward DFA to find full match end
//     c. Return first match
//
// Performance:
//   - Forward naive search: O(n*m) where n=haystack length, m=pattern length
//   - ReverseSuffix: O(k*m) where k=number of suffix candidates (usually k << n)
//   - Speedup: 10-100x for patterns like `.*\.txt` on large haystacks
//
// Example:
//
//	// Pattern `.*\.txt` on 1MB data with 10 `.txt` occurrences
//	// Forward: tries pattern match at every position (~1M attempts)
//	// ReverseSuffix: prefilter finds 10 `.txt` positions, reverse DFA verifies (~10 attempts)
//	// Speedup: ~100,000x
type ReverseSuffixSearcher struct {
	forwardNFA *nfa.NFA
	reverseNFA *nfa.NFA
	reverseDFA *lazy.DFA
	forwardDFA *lazy.DFA
	prefilter  prefilter.Prefilter
	pikevm     *nfa.PikeVM
	suffixLen  int // Length of the suffix literal for calculating revEnd
}

// NewReverseSuffixSearcher creates a reverse suffix searcher from forward NFA.
//
// Requirements:
//   - Pattern must have good suffix literals
//   - Pattern must NOT be start-anchored (^)
//   - Prefilter must be available
//
// Parameters:
//   - forwardNFA: the compiled forward NFA
//   - suffixLiterals: extracted suffix literals from pattern
//   - config: DFA configuration for reverse DFA cache
//
// Returns nil if reverse suffix optimization cannot be applied.
func NewReverseSuffixSearcher(
	forwardNFA *nfa.NFA,
	suffixLiterals *literal.Seq,
	config lazy.Config,
) (*ReverseSuffixSearcher, error) {
	// Get suffix length from longest common suffix
	suffixLen := 0
	if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
		lcs := suffixLiterals.LongestCommonSuffix()
		suffixLen = len(lcs)
	}
	if suffixLen == 0 {
		return nil, ErrNoPrefilter
	}

	// Build prefilter from suffix literals
	builder := prefilter.NewBuilder(nil, suffixLiterals)
	pre := builder.Build()
	if pre == nil {
		// No prefilter available - cannot use this optimization
		return nil, ErrNoPrefilter
	}

	// Build reverse NFA - unanchored (we need to match from any position backward)
	// Unlike ReverseAnchored, we don't use ReverseAnchored() because we're not
	// searching for $ anchor, but for suffix literals.
	reverseNFA := nfa.Reverse(forwardNFA)

	// Build reverse DFA from reverse NFA
	reverseDFA, err := lazy.CompileWithConfig(reverseNFA, config)
	if err != nil {
		return nil, err
	}

	// Build forward DFA for finding match end after reverse match
	forwardDFA, err := lazy.CompileWithConfig(forwardNFA, config)
	if err != nil {
		return nil, err
	}

	// Create PikeVM for fallback
	pikevm := nfa.NewPikeVM(forwardNFA)

	return &ReverseSuffixSearcher{
		forwardNFA: forwardNFA,
		reverseNFA: reverseNFA,
		reverseDFA: reverseDFA,
		forwardDFA: forwardDFA,
		prefilter:  pre,
		pikevm:     pikevm,
		suffixLen:  suffixLen,
	}, nil
}

// Find searches using suffix literal prefilter + reverse DFA and returns the match.
//
// Algorithm (leftmost-longest/greedy semantics):
//  1. Use prefilter to find ALL suffix literal candidates
//  2. For the FIRST candidate that produces a valid match:
//     a. Use reverse DFA SearchReverse to find match START (leftmost)
//     b. Record this as the leftmost match start
//  3. Continue scanning for more candidates with the SAME match start
//     a. Keep track of the longest match end (greedy)
//  4. Return the match with leftmost start and longest end
//
// Performance:
//   - ZERO PikeVM calls - uses DFA exclusively
//   - Single reverse DFA scan finds both match validity AND start position
//   - Multiple candidates scanned for greedy semantics
//
// Example (greedy matching):
//
//	Pattern: `.*\.txt`
//	Haystack: "a.txt.txt"
//	Suffix literal: `.txt`
//
//	1. Prefilter finds `.txt` at position 1, then at position 5
//	2. Candidate 1 (pos=1): SearchReverse returns 0, match = [0:5]
//	3. Candidate 2 (pos=5): SearchReverse returns 0, match = [0:9] (longer!)
//	4. Return [0:9] = "a.txt.txt" (greedy)
func (s *ReverseSuffixSearcher) Find(haystack []byte) *Match {
	if len(haystack) == 0 {
		return nil
	}

	// Track the best (leftmost-longest) match found
	bestMatchStart := -1
	bestMatchEnd := -1

	// Use prefilter to find suffix candidates
	start := 0
	for {
		// Find next suffix candidate
		pos := s.prefilter.Find(haystack, start)
		if pos == -1 {
			// No more candidates - return best match if found
			break
		}

		// Reverse search from haystack start to suffix end
		// pos is the START of the suffix, so we need to add suffixLen
		revEnd := pos + s.suffixLen
		if revEnd > len(haystack) {
			revEnd = len(haystack)
		}

		// Use reverse DFA SearchReverse to find match START position
		// ZERO-ALLOCATION: Scans backward without byte reversal
		matchStart := s.reverseDFA.SearchReverse(haystack, 0, revEnd)
		if matchStart >= 0 {
			// Update best match if:
			// 1. First valid match (bestMatchStart == -1)
			// 2. Earlier start found (matchStart < bestMatchStart) - shouldn't happen, but be safe
			// 3. Same start but longer end (matchStart == bestMatchStart && revEnd > bestMatchEnd) - greedy
			// Ignore if matchStart > bestMatchStart (not leftmost)
			switch {
			case bestMatchStart == -1, matchStart < bestMatchStart:
				// First match or earlier start
				bestMatchStart = matchStart
				bestMatchEnd = revEnd
			case matchStart == bestMatchStart && revEnd > bestMatchEnd:
				// Same start, but longer match (greedy)
				bestMatchEnd = revEnd
			}
		}

		// Continue to find more candidates for greedy matching
		start = pos + 1
		if start >= len(haystack) {
			break
		}
	}

	if bestMatchStart >= 0 {
		return NewMatch(bestMatchStart, bestMatchEnd, haystack)
	}
	return nil
}

// IsMatch checks if the pattern matches using suffix prefilter + reverse DFA.
//
// This is optimized for boolean matching:
//   - Uses prefilter for fast candidate finding
//   - Uses reverse DFA for fast prefix verification
//   - No Match object allocation
//   - Early termination on first match
//   - ZERO PikeVM calls - reverse DFA confirmation is sufficient
func (s *ReverseSuffixSearcher) IsMatch(haystack []byte) bool {
	if len(haystack) == 0 {
		return false
	}

	// Use prefilter to find suffix candidates
	start := 0
	for {
		// Find next suffix candidate
		pos := s.prefilter.Find(haystack, start)
		if pos == -1 {
			// No more candidates
			return false
		}

		// Reverse search from haystack start to suffix end
		// pos is the START of the suffix, so we need to add suffixLen
		revEnd := pos + s.suffixLen
		if revEnd > len(haystack) {
			revEnd = len(haystack)
		}

		// Use reverse DFA to check if we can reach suffix from start
		// ZERO-ALLOCATION: IsMatchReverse scans backward without byte reversal
		//
		// KEY OPTIMIZATION: If reverse DFA matches, the forward pattern definitely
		// matches haystack[0:revEnd]. No need to verify with PikeVM again!
		// This eliminates the redundant full-haystack scan that was causing
		// 6-8x slowdown vs stdlib.
		if s.reverseDFA.IsMatchReverse(haystack, 0, revEnd) {
			// Reverse DFA confirmed: pattern matches haystack[0:revEnd]
			// Since suffix is at pos..revEnd, this is a valid match!
			return true
		}

		// Try next candidate
		start = pos + 1
		if start >= len(haystack) {
			return false
		}
	}
}
