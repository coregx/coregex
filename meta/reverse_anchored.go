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
	reverseNFA    *nfa.NFA
	reverseDFA    *lazy.DFA
	pikevm        *nfa.PikeVM
	forwardPikevm *nfa.PikeVM // For empty string matching (reverse NFA has issues with empty)
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

	// Create forward PikeVM for empty string matching
	// Reverse NFA has issues with empty strings and certain alternations
	forwardPikevm := nfa.NewPikeVM(forwardNFA)

	return &ReverseAnchoredSearcher{
		reverseNFA:    reverseNFA,
		reverseDFA:    reverseDFA,
		pikevm:        pikevm,
		forwardPikevm: forwardPikevm,
	}, nil
}

// Find searches backward from end of haystack and returns the match.
//
// Algorithm:
//  1. Use reverse DFA SearchReverse to find match START (zero-allocation)
//  2. For $-anchored patterns, END is always len(haystack)
//
// Performance:
//   - ZERO-ALLOCATION: no byte reversal needed
//   - Single DFA scan: O(m) where m = match length
//   - Much faster than PikeVM + reverseBytes approach
//
// Example:
//
//	Forward pattern: "abc$"
//	Forward haystack: "xxxabc"
//	SearchReverse finds start=3, end=6 (because $ anchor)
func (s *ReverseAnchoredSearcher) Find(haystack []byte) *Match {
	// For empty strings, use forward PikeVM
	// Reverse NFA has issues with empty strings and certain alternations
	if len(haystack) == 0 {
		start, end, matched := s.forwardPikevm.Search(haystack)
		if !matched {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Use SearchReverse to find match START (zero-allocation backward scan)
	// For $-anchored patterns, the END is always len(haystack)
	matchStart := s.reverseDFA.SearchReverse(haystack, 0, len(haystack))
	if matchStart < 0 {
		return nil
	}

	// For $-anchored patterns, the match always ends at len(haystack)
	return NewMatch(matchStart, len(haystack), haystack)
}

// IsMatch checks if the pattern matches at the end of haystack.
//
// This is optimized for boolean matching:
//   - Uses reverse DFA for fast rejection
//   - ZERO-ALLOCATION: backward scan without byte reversal
//   - No Match object allocation
//   - Early termination
func (s *ReverseAnchoredSearcher) IsMatch(haystack []byte) bool {
	// For empty strings, use forward PikeVM
	// Reverse NFA has issues with empty strings and certain alternations
	if len(haystack) == 0 {
		_, _, matched := s.forwardPikevm.Search(haystack)
		return matched
	}

	// Use reverse DFA to scan backward from end to start
	// ZERO-ALLOCATION: IsMatchReverse scans backward without byte reversal
	return s.reverseDFA.IsMatchReverse(haystack, 0, len(haystack))
}
