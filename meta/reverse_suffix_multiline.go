package meta

import (
	"errors"

	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// ErrNoMultilinePrefilter indicates that no prefilter could be built for multiline suffix literals.
var ErrNoMultilinePrefilter = errors.New("no prefilter available for multiline suffix literals")

// MultilineReverseSuffixSearcher performs suffix literal prefilter + line-aware forward verification.
//
// This strategy is used for multiline patterns like `(?m)^/.*[\w-]+\.php` where:
//   - The pattern has the multiline flag (?m)
//   - The pattern is anchored at start of LINE (^), not start of TEXT (\A)
//   - Has a good suffix literal for prefiltering
//   - Match can occur at ANY line start, not just position 0
//
// Algorithm:
//  1. Extract suffix literals from pattern
//  2. Build prefilter for suffix literals
//  3. Search algorithm:
//     a. Prefilter finds suffix candidates in haystack
//     b. For each candidate:
//     - Scan backward to find LINE start (\n or start of input)
//     - Verify pattern from line start using forward PikeVM
//     c. Return first valid match
//
// Key difference from ReverseSuffixSearcher:
//   - ReverseSuffix: match always starts at position 0 (unanchored .*)
//   - MultilineReverseSuffix: match starts at LINE start (after \n or pos 0)
//
// Performance:
//   - Forward naive search: O(n*m) where n=haystack length, m=pattern length
//   - MultilineReverseSuffix: O(k*l) where k=suffix candidates, l=avg line length
//   - Expected speedup: 5-20x for patterns like `(?m)^/.*\.php` on large inputs
type MultilineReverseSuffixSearcher struct {
	prefilter prefilter.Prefilter
	pikevm    *nfa.PikeVM
	suffixLen int // Length of the suffix literal
}

// NewMultilineReverseSuffixSearcher creates a multiline-aware suffix searcher.
//
// Requirements:
//   - Pattern must have good suffix literals
//   - Pattern must have multiline ^ anchor
//   - Prefilter must be available
//
// Parameters:
//   - forwardNFA: the compiled forward NFA
//   - suffixLiterals: extracted suffix literals from pattern
//   - config: DFA configuration (unused, kept for API compatibility)
//
// Returns error if multiline reverse suffix optimization cannot be applied.
func NewMultilineReverseSuffixSearcher(
	forwardNFA *nfa.NFA,
	suffixLiterals *literal.Seq,
	_ lazy.Config, // unused, kept for API compatibility
) (*MultilineReverseSuffixSearcher, error) {
	// Get suffix bytes from longest common suffix
	var suffixBytes []byte
	if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
		suffixBytes = suffixLiterals.LongestCommonSuffix()
	}
	if len(suffixBytes) == 0 {
		return nil, ErrNoMultilinePrefilter
	}
	suffixLen := len(suffixBytes)

	// Build prefilter from suffix literals
	builder := prefilter.NewBuilder(nil, suffixLiterals)
	pre := builder.Build()
	if pre == nil {
		return nil, ErrNoMultilinePrefilter
	}

	// Create PikeVM for forward verification
	pikevm := nfa.NewPikeVM(forwardNFA)

	return &MultilineReverseSuffixSearcher{
		prefilter: pre,
		pikevm:    pikevm,
		suffixLen: suffixLen,
	}, nil
}

// findLineStart scans backward from pos to find the start of the line.
// Returns 0 if no newline is found (meaning we're on the first line).
// Returns pos+1 of the \n character if found (start of next line after \n).
func findLineStart(haystack []byte, pos int) int {
	// Scan backward from pos-1 (we don't want to match a \n AT pos)
	for i := pos - 1; i >= 0; i-- {
		if haystack[i] == '\n' {
			return i + 1 // Line starts after the \n
		}
	}
	return 0 // No newline found, line starts at beginning
}

// Find searches using suffix literal prefilter + line-aware forward verification.
//
// Algorithm:
//  1. Iterate through suffix candidates using prefilter
//  2. For each candidate, find LINE start (after \n or input start)
//  3. Use forward PikeVM to verify match from line start
//  4. Return first valid match
//
// The key insight is that multiline ^ anchors must be verified using FORWARD
// matching from line start, not reverse matching. The ^ anchor has specific
// semantics that only work correctly in forward direction.
func (s *MultilineReverseSuffixSearcher) Find(haystack []byte) *Match {
	if len(haystack) == 0 {
		return nil
	}

	// Iterate through suffix candidates
	pos := 0
	for {
		// Find next suffix candidate using prefilter
		suffixPos := s.prefilter.Find(haystack, pos)
		if suffixPos == -1 {
			return nil
		}

		// Find the start of the line containing this suffix
		lineStart := findLineStart(haystack, suffixPos)

		// Use forward PikeVM to verify match from line start
		// SearchAt respects the ^ anchor semantics correctly
		start, end, matched := s.pikevm.SearchAt(haystack, lineStart)
		if matched && start >= lineStart && end <= len(haystack) {
			return NewMatch(start, end, haystack)
		}

		// Move past this suffix candidate
		pos = suffixPos + 1
		if pos >= len(haystack) {
			return nil
		}
	}
}

// FindAt searches for a match starting from position 'at'.
//
// Returns the first match starting at or after position 'at'.
// Essential for FindAll iteration.
func (s *MultilineReverseSuffixSearcher) FindAt(haystack []byte, at int) *Match {
	if at >= len(haystack) {
		return nil
	}

	pos := at
	for {
		// Find next suffix candidate starting from pos
		suffixPos := s.prefilter.Find(haystack, pos)
		if suffixPos == -1 {
			return nil
		}

		// Find line start (but not before 'at' for FindAt semantics)
		lineStart := findLineStart(haystack, suffixPos)
		if lineStart < at {
			// The line starts before our search position.
			// We can still match if the suffix is on a valid line.
			lineStart = at
		}

		// Use forward PikeVM to verify match from line start
		start, end, matched := s.pikevm.SearchAt(haystack, lineStart)
		if matched && start >= lineStart {
			return NewMatch(start, end, haystack)
		}

		// Move past this suffix candidate
		pos = suffixPos + 1
		if pos >= len(haystack) {
			return nil
		}
	}
}

// FindIndicesAt returns match indices starting from position 'at' - zero allocation version.
func (s *MultilineReverseSuffixSearcher) FindIndicesAt(haystack []byte, at int) (start, end int, found bool) {
	match := s.FindAt(haystack, at)
	if match == nil {
		return -1, -1, false
	}
	return match.start, match.end, true
}

// IsMatch checks if the pattern matches using suffix prefilter + line-aware verification.
//
// Optimized for boolean matching:
//   - Uses prefilter for fast candidate finding
//   - Uses forward PikeVM for line-aware verification
//   - Early termination on first match
//   - No Match object allocation
func (s *MultilineReverseSuffixSearcher) IsMatch(haystack []byte) bool {
	if len(haystack) == 0 {
		return false
	}

	// Iterate through suffix candidates
	pos := 0
	for {
		// Find next suffix candidate
		suffixPos := s.prefilter.Find(haystack, pos)
		if suffixPos == -1 {
			return false
		}

		// Find line start
		lineStart := findLineStart(haystack, suffixPos)

		// Use forward PikeVM to check if match is valid from line start
		_, _, matched := s.pikevm.SearchAt(haystack, lineStart)
		if matched {
			return true
		}

		// Move past this suffix candidate
		pos = suffixPos + 1
		if pos >= len(haystack) {
			return false
		}
	}
}
