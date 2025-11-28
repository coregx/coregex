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
//  1. Reverse the haystack bytes
//  2. Search using reverse DFA from position 0 (which corresponds to end of original haystack)
//  3. If match found, use PikeVM to get exact bounds in reversed haystack
//  4. Convert reverse positions back to forward positions
//
// For a pattern ending with $, the reverse NFA is anchored at start (^), so the
// reverse DFA will match from position 0 of reversed haystack = end of original.
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

	// Reverse haystack for reverse search
	reversed := reverseBytes(haystack)

	// Use PikeVM to get exact match bounds in reversed haystack
	// The reverse NFA is anchored at start (for $ in forward pattern)
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

// IsMatch checks if the pattern matches at the end of haystack.
//
// This is optimized for boolean matching:
//   - Uses DFA for fast rejection
//   - No Match object allocation
//   - Early termination
func (s *ReverseAnchoredSearcher) IsMatch(haystack []byte) bool {
	if len(haystack) == 0 {
		return false
	}

	// Reverse haystack
	reversed := reverseBytes(haystack)

	// Use DFA for fast check
	return s.reverseDFA.IsMatch(reversed)
}

// reverseBytes creates a reversed copy of the byte slice.
//
// Note: This allocates a new slice. For very large haystacks, we might want
// to implement a zero-copy reverse iterator instead.
func reverseBytes(b []byte) []byte {
	n := len(b)
	reversed := make([]byte, n)
	for i := 0; i < n; i++ {
		reversed[i] = b[n-1-i]
	}
	return reversed
}
