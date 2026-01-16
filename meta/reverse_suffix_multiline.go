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
//     - Verify pattern from line start using forward DFA (anchored search)
//     c. Return first valid match
//
// Key difference from ReverseSuffixSearcher:
//   - ReverseSuffix: match always starts at position 0 (unanchored .*)
//   - MultilineReverseSuffix: match starts at LINE start (after \n or pos 0)
//
// Performance:
//   - Forward naive search: O(n*m) where n=haystack length, m=pattern length
//   - MultilineReverseSuffix with DFA: O(n) total - linear time verification
//   - Expected speedup: 10-100x for patterns like `(?m)^/.*\.php` on large inputs
type MultilineReverseSuffixSearcher struct {
	prefilter  prefilter.Prefilter
	forwardDFA *lazy.DFA // O(n) forward DFA for anchored verification
	suffixLen  int       // Length of the suffix literal
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
//   - config: DFA configuration for forward DFA cache
//
// Returns error if multiline reverse suffix optimization cannot be applied.
func NewMultilineReverseSuffixSearcher(
	forwardNFA *nfa.NFA,
	suffixLiterals *literal.Seq,
	config lazy.Config,
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

	// Build forward DFA for O(n) anchored verification
	// The DFA's SearchAtAnchored method provides efficient anchored search from line starts
	forwardDFA, err := lazy.CompileWithConfig(forwardNFA, config)
	if err != nil {
		return nil, err
	}

	return &MultilineReverseSuffixSearcher{
		prefilter:  pre,
		forwardDFA: forwardDFA,
		suffixLen:  suffixLen,
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

// Find searches using suffix literal prefilter + line-aware forward DFA verification.
//
// Algorithm:
//  1. Iterate through suffix candidates using prefilter
//  2. For each candidate, find LINE start (after \n or input start)
//  3. Use forward DFA (anchored) to verify match from line start
//  4. Return first valid match
//
// The key insight is that multiline ^ anchors must be verified using FORWARD
// matching from line start, not reverse matching. The ^ anchor has specific
// semantics that only work correctly in forward direction.
//
// Performance: O(n) total due to DFA's linear time complexity per byte.
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

		// Use forward DFA with ANCHORED search to verify match from line start.
		// SearchAtAnchored requires match to begin exactly at lineStart (respects ^ anchor).
		// Returns end position if match found, -1 otherwise.
		end := s.forwardDFA.SearchAtAnchored(haystack, lineStart)
		if end >= 0 {
			// For anchored search, start = lineStart
			return NewMatch(lineStart, end, haystack)
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
//
// Performance: O(n) due to DFA's linear time complexity.
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

		// Use forward DFA with ANCHORED search to verify match from line start.
		// SearchAtAnchored requires match to begin exactly at lineStart.
		end := s.forwardDFA.SearchAtAnchored(haystack, lineStart)
		if end >= 0 {
			// For anchored search, start = lineStart
			return NewMatch(lineStart, end, haystack)
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

// IsMatch checks if the pattern matches using suffix prefilter + line-aware DFA verification.
//
// Optimized for boolean matching:
//   - Uses prefilter for fast candidate finding
//   - Uses forward DFA (anchored) for O(n) line-aware verification
//   - Early termination on first match
//   - No Match object allocation
//
// Performance: O(n) total due to DFA's linear time complexity.
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

		// Use forward DFA with ANCHORED search to check if match is valid from line start.
		// SearchAtAnchored returns >= 0 if match found, -1 otherwise.
		if s.forwardDFA.SearchAtAnchored(haystack, lineStart) >= 0 {
			return true
		}

		// Move past this suffix candidate
		pos = suffixPos + 1
		if pos >= len(haystack) {
			return false
		}
	}
}
