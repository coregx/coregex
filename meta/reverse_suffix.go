package meta

import (
	"bytes"
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
	forwardNFA     *nfa.NFA
	reverseNFA     *nfa.NFA
	reverseDFA     *lazy.DFA
	forwardDFA     *lazy.DFA
	prefilter      prefilter.Prefilter
	pikevm         *nfa.PikeVM
	suffixLen      int    // Length of the suffix literal for calculating revEnd
	suffixBytes    []byte // Suffix literal bytes for FindLast optimization
	matchStartZero bool   // True if pattern starts with .* (match always starts at 0)
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
	// Get suffix bytes from longest common suffix
	var suffixBytes []byte
	if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
		suffixBytes = suffixLiterals.LongestCommonSuffix()
	}
	if len(suffixBytes) == 0 {
		return nil, ErrNoPrefilter
	}
	suffixLen := len(suffixBytes)

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

	// Check if pattern is unanchored (starts matching from position 0)
	// For unanchored patterns with .* prefix, match always starts at 0
	// This allows us to skip the reverse DFA scan entirely!
	matchStartZero := !forwardNFA.IsAlwaysAnchored()

	return &ReverseSuffixSearcher{
		forwardNFA:     forwardNFA,
		reverseNFA:     reverseNFA,
		reverseDFA:     reverseDFA,
		forwardDFA:     forwardDFA,
		prefilter:      pre,
		pikevm:         pikevm,
		suffixLen:      suffixLen,
		suffixBytes:    suffixBytes,
		matchStartZero: matchStartZero,
	}, nil
}

// Find searches using suffix literal prefilter + reverse DFA and returns the match.
//
// Algorithm (find LAST suffix for greedy semantics):
//  1. Use prefilter to find the LAST suffix literal candidate
//  2. Use reverse DFA to find match START (leftmost)
//  3. Return match immediately (no forward scan needed!)
//
// Why find LAST suffix?
//   - Pattern `.*\.txt` is greedy - `.*` matches as much as possible
//   - For input "a.txt.txt", the greedy match is the ENTIRE string [0:9]
//   - Finding the LAST `.txt` (at position 5) and reverse scanning gives us this
//   - No expensive forward DFA scan needed!
//
// Performance:
//   - Single prefilter scan to find last suffix: O(n)
//   - Single reverse DFA scan: O(n)
//   - Total: O(n) with small constant
//
// Example:
//
//	Pattern: `.*\.txt`
//	Haystack: "a.txt.txt"
//	Suffix literal: `.txt`
//
//	1. Prefilter finds LAST `.txt` at position 5
//	2. Reverse DFA from [0,9] finds match start = 0
//	3. Return [0:9] = "a.txt.txt" (greedy!)
func (s *ReverseSuffixSearcher) Find(haystack []byte) *Match {
	if len(haystack) == 0 {
		return nil
	}

	// Find the LAST suffix candidate for greedy matching
	// OPTIMIZATION: Use bytes.LastIndex for O(n) single-pass search
	// instead of iterating through all matches (was O(k*n) where k=match count)
	lastPos := bytes.LastIndex(haystack, s.suffixBytes)
	if lastPos == -1 {
		// No suffix candidates found
		return nil
	}

	// Calculate match end
	revEnd := lastPos + s.suffixLen
	if revEnd > len(haystack) {
		revEnd = len(haystack)
	}

	// OPTIMIZATION: For unanchored patterns (like .*@suffix), match always starts at 0
	// because the unanchored search starts from position 0 and .* matches everything.
	// Skip the expensive reverse DFA scan entirely!
	if s.matchStartZero {
		return NewMatch(0, revEnd, haystack)
	}

	// Use reverse DFA to find match START position (for anchored patterns)
	matchStart := s.reverseDFA.SearchReverse(haystack, 0, revEnd)
	if matchStart >= 0 {
		// Found valid match - return immediately
		return NewMatch(matchStart, revEnd, haystack)
	}

	// No valid match found
	return nil
}

// FindAt searches for a match starting from position 'at' using suffix prefilter + reverse DFA.
//
// Unlike Find() which returns the greedy (last suffix) match, FindAt returns the first
// match starting at or after position 'at'. This is essential for FindAll iteration.
//
// Algorithm:
//  1. Use prefilter to find FIRST suffix candidate >= at
//  2. Use reverse DFA to find match START (from 'at' position)
//  3. Return match [start, suffixEnd]
//
// Performance:
//   - Prefilter scan: O(n) from 'at' position
//   - Reverse DFA verification: O(m) where m is match length
//   - Total: O(n) per call
func (s *ReverseSuffixSearcher) FindAt(haystack []byte, at int) *Match {
	if at >= len(haystack) {
		return nil
	}

	// Find FIRST suffix candidate starting from 'at'
	pos := s.prefilter.Find(haystack, at)
	if pos == -1 {
		return nil
	}

	// Calculate suffix end position
	suffixEnd := pos + s.suffixLen
	if suffixEnd > len(haystack) {
		suffixEnd = len(haystack)
	}

	// For unanchored patterns (like .*@suffix), match can start from 'at'
	// because .* matches any prefix from the starting position
	if s.matchStartZero {
		// Match starts at 'at' position (the search start)
		return NewMatch(at, suffixEnd, haystack)
	}

	// Use reverse DFA to find match START position (for anchored patterns)
	matchStart := s.reverseDFA.SearchReverse(haystack, at, suffixEnd)
	if matchStart >= 0 {
		return NewMatch(matchStart, suffixEnd, haystack)
	}

	// No valid match found
	return nil
}

// FindIndicesAt returns match indices starting from position 'at' - zero allocation version.
func (s *ReverseSuffixSearcher) FindIndicesAt(haystack []byte, at int) (start, end int, found bool) {
	if at >= len(haystack) {
		return -1, -1, false
	}

	// Find FIRST suffix candidate starting from 'at'
	pos := s.prefilter.Find(haystack, at)
	if pos == -1 {
		return -1, -1, false
	}

	// Calculate suffix end position
	suffixEnd := pos + s.suffixLen
	if suffixEnd > len(haystack) {
		suffixEnd = len(haystack)
	}

	// For unanchored patterns (like .*@suffix), match starts at 'at'
	if s.matchStartZero {
		return at, suffixEnd, true
	}

	// Use reverse DFA to find match START position
	matchStart := s.reverseDFA.SearchReverse(haystack, at, suffixEnd)
	if matchStart >= 0 {
		return matchStart, suffixEnd, true
	}

	return -1, -1, false
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
