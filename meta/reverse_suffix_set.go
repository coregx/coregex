package meta

// DO NOT REGRESS: ReverseSuffixSet is 27% faster than Rust regex on multi-suffix patterns.
// This optimization is UNIQUE TO COREGEX - Rust regex falls back to Core strategy.
// Uses Teddy multi-pattern prefilter + reverse DFA for patterns like .*\.(txt|log|md).
// See docs/OPTIMIZATIONS.md for algorithm details and benchmark data.

import (
	"errors"

	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// ErrNoSuffixSet indicates that no suffix set prefilter could be built.
var ErrNoSuffixSet = errors.New("no suffix set prefilter available")

// ReverseSuffixSetSearcher performs Teddy multi-suffix prefilter + reverse DFA search.
//
// This strategy handles patterns like `.*\.(txt|log|md)` where:
//   - The suffix is an alternation with no common suffix (LCS is empty)
//   - Multiple suffix literals are available (2-8 literals, each >= 3 bytes)
//   - Teddy can efficiently search for any of the suffixes
//
// This optimization is NOT present in rust-regex (they fall back to Core strategy).
//
// Algorithm:
//  1. Build Teddy prefilter from all suffix literals
//  2. Search algorithm:
//     a. Teddy finds any suffix literal in haystack
//     b. Use reverse DFA to verify prefix pattern
//     c. For `.*` prefix patterns, match starts at position 0 (skip reverse scan)
//     d. Return match
//
// Performance:
//   - Without this optimization: O(n*m) using UseBoth strategy
//   - With Teddy suffix prefilter: O(n + k*m) where k = suffix candidates
//   - Speedup: 5-10x for patterns like `.*\.(txt|log|md)`
type ReverseSuffixSetSearcher struct {
	forwardNFA     *nfa.NFA
	reverseNFA     *nfa.NFA
	reverseDFA     *lazy.DFA
	forwardDFA     *lazy.DFA
	prefilter      prefilter.Prefilter
	pikevm         *nfa.PikeVM
	suffixLiterals *literal.Seq // All suffix literals
	matchStartZero bool         // True if pattern starts with .* (match always starts at 0)
}

// NewReverseSuffixSetSearcher creates a reverse suffix set searcher.
//
// Requirements:
//   - Pattern must have 2-8 suffix literals
//   - Each suffix literal must be >= 2 bytes (allows extensions like ".md")
//   - Pattern must NOT be start-anchored (^)
//
// Returns nil if the optimization cannot be applied.
func NewReverseSuffixSetSearcher(
	forwardNFA *nfa.NFA,
	suffixLiterals *literal.Seq,
	config lazy.Config,
) (*ReverseSuffixSetSearcher, error) {
	if suffixLiterals == nil || suffixLiterals.IsEmpty() {
		return nil, ErrNoSuffixSet
	}

	litCount := suffixLiterals.Len()
	if litCount < 2 || litCount > 32 {
		return nil, ErrNoSuffixSet
	}

	// Verify all literals are long enough (2 bytes minimum for extensions like ".md")
	for i := 0; i < litCount; i++ {
		if len(suffixLiterals.Get(i).Bytes) < 2 {
			return nil, ErrNoSuffixSet
		}
	}

	// Build Teddy prefilter from suffix literals
	builder := prefilter.NewBuilder(nil, suffixLiterals)
	pre := builder.Build()
	if pre == nil {
		return nil, ErrNoSuffixSet
	}

	// Build reverse NFA
	reverseNFA := nfa.Reverse(forwardNFA)

	// Build reverse DFA
	reverseDFA, err := lazy.CompileWithConfig(reverseNFA, config)
	if err != nil {
		return nil, err
	}

	// Build forward DFA
	forwardDFA, err := lazy.CompileWithConfig(forwardNFA, config)
	if err != nil {
		return nil, err
	}

	// Create PikeVM for fallback
	pikevm := nfa.NewPikeVM(forwardNFA)

	// Check if pattern is unanchored (starts matching from position 0)
	matchStartZero := !forwardNFA.IsAlwaysAnchored()

	return &ReverseSuffixSetSearcher{
		forwardNFA:     forwardNFA,
		reverseNFA:     reverseNFA,
		reverseDFA:     reverseDFA,
		forwardDFA:     forwardDFA,
		prefilter:      pre,
		pikevm:         pikevm,
		suffixLiterals: suffixLiterals,
		matchStartZero: matchStartZero,
	}, nil
}

// Find searches using Teddy suffix prefilter + reverse DFA.
//
// For greedy matching (like `.*`), we need to find the LAST matching suffix.
// However, with multiple suffix lengths, we iterate through all candidates
// and track the best (rightmost) match.
// Includes anti-quadratic guard to prevent O(n^2) behavior with many suffix false positives.
func (s *ReverseSuffixSetSearcher) Find(haystack []byte) *Match {
	if len(haystack) == 0 {
		return nil
	}

	// For greedy matching, find the LAST suffix candidate
	// We scan forward and keep track of the last valid match
	var lastMatch *Match
	start := 0
	minStart := 0 // Anti-quadratic guard for reverse scans

	for {
		// Find next suffix candidate
		pos := s.prefilter.Find(haystack, start)
		if pos == -1 {
			break
		}

		// Get the length of the matched suffix literal
		suffixLen := s.getSuffixLen(haystack, pos)
		if suffixLen == 0 {
			start = pos + 1
			continue
		}

		suffixEnd := pos + suffixLen
		if suffixEnd > len(haystack) {
			suffixEnd = len(haystack)
		}

		// For unanchored patterns, match starts at 0
		if s.matchStartZero {
			lastMatch = NewMatch(0, suffixEnd, haystack)
		} else {
			// Use reverse DFA with anti-quadratic guard to find match start
			matchStart := s.reverseDFA.SearchReverseLimited(haystack, 0, suffixEnd, minStart)
			if matchStart == lazy.SearchReverseLimitedQuadratic {
				// Quadratic behavior detected - fall back to PikeVM
				pStart, pEnd, found := s.pikevm.Search(haystack)
				if found {
					return NewMatch(pStart, pEnd, haystack)
				}
				return lastMatch
			}
			if matchStart >= 0 {
				lastMatch = NewMatch(matchStart, suffixEnd, haystack)
			}
			// Update anti-quadratic guard
			if suffixEnd > minStart {
				minStart = suffixEnd
			}
		}

		start = pos + 1
		if start >= len(haystack) {
			break
		}
	}

	return lastMatch
}

// FindAt searches for a match starting from position 'at'.
// Includes anti-quadratic guard to prevent O(n^2) behavior with many suffix false positives.
func (s *ReverseSuffixSetSearcher) FindAt(haystack []byte, at int) *Match {
	if at >= len(haystack) {
		return nil
	}

	searchStart := at
	minStart := at // Anti-quadratic guard
	for {
		// Find next suffix candidate starting from searchStart
		pos := s.prefilter.Find(haystack, searchStart)
		if pos == -1 {
			return nil
		}

		// Get the length of the matched suffix literal
		suffixLen := s.getSuffixLen(haystack, pos)
		if suffixLen == 0 {
			searchStart = pos + 1
			if searchStart >= len(haystack) {
				return nil
			}
			continue
		}

		suffixEnd := pos + suffixLen
		if suffixEnd > len(haystack) {
			suffixEnd = len(haystack)
		}

		// For unanchored patterns, match starts at 'at'
		if s.matchStartZero {
			return NewMatch(at, suffixEnd, haystack)
		}

		// Use reverse DFA with anti-quadratic guard to find match start
		matchStart := s.reverseDFA.SearchReverseLimited(haystack, at, suffixEnd, minStart)
		if matchStart >= 0 {
			return NewMatch(matchStart, suffixEnd, haystack)
		}
		if matchStart == lazy.SearchReverseLimitedQuadratic {
			// Quadratic behavior detected - fall back to PikeVM
			start, end, found := s.pikevm.SearchAt(haystack, at)
			if found {
				return NewMatch(start, end, haystack)
			}
			return nil
		}

		// Update anti-quadratic guard
		if suffixEnd > minStart {
			minStart = suffixEnd
		}

		searchStart = pos + 1
		if searchStart >= len(haystack) {
			return nil
		}
	}
}

// FindIndicesAt returns match indices - zero allocation version.
// Includes anti-quadratic guard to prevent O(n^2) behavior with many suffix false positives.
func (s *ReverseSuffixSetSearcher) FindIndicesAt(haystack []byte, at int) (start, end int, found bool) {
	if at >= len(haystack) {
		return -1, -1, false
	}

	searchStart := at
	minStart := at // Anti-quadratic guard
	for {
		// Find next suffix candidate starting from searchStart
		pos := s.prefilter.Find(haystack, searchStart)
		if pos == -1 {
			return -1, -1, false
		}

		// Get the length of the matched suffix literal
		suffixLen := s.getSuffixLen(haystack, pos)
		if suffixLen == 0 {
			searchStart = pos + 1
			if searchStart >= len(haystack) {
				return -1, -1, false
			}
			continue
		}

		suffixEnd := pos + suffixLen
		if suffixEnd > len(haystack) {
			suffixEnd = len(haystack)
		}

		// For unanchored patterns, match starts at 'at'
		if s.matchStartZero {
			return at, suffixEnd, true
		}

		// Use reverse DFA with anti-quadratic guard to find match start
		matchStart := s.reverseDFA.SearchReverseLimited(haystack, at, suffixEnd, minStart)
		if matchStart >= 0 {
			return matchStart, suffixEnd, true
		}
		if matchStart == lazy.SearchReverseLimitedQuadratic {
			// Quadratic behavior detected - fall back to PikeVM
			return s.pikevm.SearchAt(haystack, at)
		}

		// Update anti-quadratic guard
		if suffixEnd > minStart {
			minStart = suffixEnd
		}

		searchStart = pos + 1
		if searchStart >= len(haystack) {
			return -1, -1, false
		}
	}
}

// IsMatch checks if the pattern matches using suffix set prefilter.
// Includes anti-quadratic guard to prevent O(n^2) behavior with many suffix false positives.
func (s *ReverseSuffixSetSearcher) IsMatch(haystack []byte) bool {
	if len(haystack) == 0 {
		return false
	}

	start := 0
	minStart := 0 // Anti-quadratic guard for reverse scans
	for {
		pos := s.prefilter.Find(haystack, start)
		if pos == -1 {
			return false
		}

		suffixLen := s.getSuffixLen(haystack, pos)
		if suffixLen == 0 {
			start = pos + 1
			if start >= len(haystack) {
				return false
			}
			continue
		}

		revEnd := pos + suffixLen
		if revEnd > len(haystack) {
			revEnd = len(haystack)
		}

		// Use reverse DFA with anti-quadratic guard to check if pattern matches
		revResult := s.reverseDFA.SearchReverseLimited(haystack, 0, revEnd, minStart)
		if revResult >= 0 {
			return true
		}
		if revResult == lazy.SearchReverseLimitedQuadratic {
			// Quadratic behavior detected - fall back to PikeVM
			_, _, matched := s.pikevm.Search(haystack)
			return matched
		}

		// Update anti-quadratic guard
		if revEnd > minStart {
			minStart = revEnd
		}

		start = pos + 1
		if start >= len(haystack) {
			return false
		}
	}
}

// getSuffixLen returns the length of the suffix literal that matched at position pos.
// This iterates through all suffix literals to find which one matched.
func (s *ReverseSuffixSetSearcher) getSuffixLen(haystack []byte, pos int) int {
	for i := 0; i < s.suffixLiterals.Len(); i++ {
		lit := s.suffixLiterals.Get(i)
		litBytes := lit.Bytes
		litLen := len(litBytes)

		// Check if this literal matches at position pos
		if pos+litLen <= len(haystack) {
			match := true
			for j := 0; j < litLen; j++ {
				if haystack[pos+j] != litBytes[j] {
					match = false
					break
				}
			}
			if match {
				return litLen
			}
		}
	}
	return 0
}
