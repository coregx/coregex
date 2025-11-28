package meta

import (
	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/nfa"
)

// ReverseAnchoredSearcher performs reverse search for patterns anchored at end.
//
// This strategy is used for patterns like "abc$" or "pattern.*suffix$" where
// the pattern must match at the end of the haystack. Instead of trying to
// match from every position in the haystack (O(n) attempts), we search backward
// from the end of the haystack (O(1) attempt).
//
// Algorithm:
//  1. Build reverse NFA from forward NFA
//  2. Build reverse DFA from reverse NFA
//  3. Search backward from end of haystack using reverse DFA
//  4. If match found, convert reverse positions to forward positions
//
// Performance:
//   - Forward search (naive): O(n*m) where n=haystack length, m=pattern length
//   - Reverse search: O(m) - only one search attempt from the end
//   - Speedup: ~n/m (e.g., 1000x for 1MB haystack and 1KB pattern)
//
// Example:
//
//	// Pattern "Easy1$" on 1MB data
//	// Forward: 340 seconds (tries match at every position)
//	// Reverse: ~1 millisecond (one match attempt from end)
type ReverseAnchoredSearcher struct {
	reverseNFA *nfa.NFA
	reverseDFA *lazy.DFA
	pikevm     *nfa.PikeVM
}

// NewReverseAnchoredSearcher creates a reverse searcher from forward NFA.
//
// Parameters:
//   - forwardNFA: the compiled forward NFA
//   - config: DFA configuration for reverse DFA cache
//
// Returns nil if reverse DFA cannot be built (falls back to forward search).
func NewReverseAnchoredSearcher(forwardNFA *nfa.NFA, config lazy.Config) (*ReverseAnchoredSearcher, error) {
	// Build reverse NFA - must be anchored at start (because $ in forward becomes ^ in reverse)
	reverseNFA := nfa.ReverseAnchored(forwardNFA)

	// Build reverse DFA from reverse NFA
	reverseDFA, err := lazy.CompileWithConfig(reverseNFA, config)
	if err != nil {
		// Cannot build reverse DFA - this should be rare
		return nil, err
	}

	// Create PikeVM for fallback (when DFA cache is full)
	pikevm := nfa.NewPikeVM(reverseNFA)

	return &ReverseAnchoredSearcher{
		reverseNFA: reverseNFA,
		reverseDFA: reverseDFA,
		pikevm:     pikevm,
	}, nil
}

// Find searches backward from end of haystack and returns the match.
//
// Algorithm:
//  1. Quick check with reverse DFA (zero-allocation backward scan)
//  2. If DFA confirms match, reverse bytes for PikeVM to get exact bounds
//  3. Convert reverse positions back to forward positions
//
// For a pattern ending with $, the reverse NFA is anchored at start (^).
// PikeVM on reverse NFA requires reversed bytes to find exact match bounds.
//
// Example:
//
//	Forward pattern: "abc$"
//	Forward haystack: "xxxabc"
//	Reverse haystack: "cbaxxx"
//	Reverse pattern: "^cba"
//	Match in reverse: [0:3] = "cba"
//	Convert to forward: [3:6] = "abc"
func (s *ReverseAnchoredSearcher) Find(haystack []byte) *Match {
	if len(haystack) == 0 {
		return nil
	}

	// Quick check: use zero-allocation reverse DFA scan
	if !s.reverseDFA.IsMatchReverse(haystack, 0, len(haystack)) {
		return nil
	}

	// Match confirmed - need exact bounds from PikeVM
	// PikeVM requires reversed bytes for reverse NFA
	reversed := reverseBytes(haystack)
	revStart, revEnd, matched := s.pikevm.Search(reversed)
	if !matched {
		return nil
	}

	// Convert reverse positions to forward positions
	// Reverse haystack: reversed[revStart:revEnd]
	// Forward haystack: haystack[len-revEnd:len-revStart]
	//
	// Example: haystack="xxxabc" (len=6), reversed="cbaxxx"
	// If reverse match is [0:3] (= "cba")
	// Then forward match is [6-3:6-0] = [3:6] (= "abc")
	n := len(haystack)
	start := n - revEnd
	end := n - revStart

	return NewMatch(start, end, haystack)
}

// reverseBytes creates a reversed copy of the byte slice.
// Only used by Find() for PikeVM, which requires reversed bytes.
// IsMatch() uses zero-allocation IsMatchReverse() instead.
func reverseBytes(b []byte) []byte {
	n := len(b)
	reversed := make([]byte, n)
	for i := 0; i < n; i++ {
		reversed[i] = b[n-1-i]
	}
	return reversed
}

// IsMatch checks if the pattern matches at the end of haystack.
//
// This is optimized for boolean matching:
//   - Uses reverse DFA for fast rejection
//   - ZERO-ALLOCATION: backward scan without byte reversal
//   - No Match object allocation
//   - Early termination
func (s *ReverseAnchoredSearcher) IsMatch(haystack []byte) bool {
	if len(haystack) == 0 {
		return false
	}

	// Use reverse DFA to scan backward from end to start
	// ZERO-ALLOCATION: IsMatchReverse scans backward without byte reversal
	return s.reverseDFA.IsMatchReverse(haystack, 0, len(haystack))
}
