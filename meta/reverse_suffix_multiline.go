package meta

import (
	"bytes"
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
// Algorithm (fast path for simple patterns like `(?m)^prefix.*suffix`):
//  1. Prefilter finds suffix candidates in haystack (SIMD memmem)
//  2. For each candidate:
//     - Scan backward to find LINE start (\n or start of input)
//     - Verify prefix literal at line start (simple byte comparison)
//     - If prefix matches, return match immediately
//
// Algorithm (slow path for complex patterns):
//  1. Same as fast path for candidate finding
//  2. Use forward DFA for verification
//
// Performance:
//   - Fast path: O(n) with very low constant factor (just byte comparisons)
//   - Slow path: O(n) with DFA overhead
//   - Expected speedup: 50-100x for simple patterns like `(?m)^/.*\.php`
type MultilineReverseSuffixSearcher struct {
	prefilter   prefilter.Prefilter
	prefixBytes []byte    // Prefix literal for fast verification (nil = use DFA)
	suffixLen   int       // Length of the suffix literal
	forwardDFA  *lazy.DFA // Fallback DFA for complex patterns
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
//   - prefixLiterals: extracted prefix literals from pattern (may be nil)
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

	// Build forward DFA for verification (always needed as fallback)
	forwardDFA, err := lazy.CompileWithConfig(forwardNFA, config)
	if err != nil {
		return nil, err
	}

	return &MultilineReverseSuffixSearcher{
		prefilter:   pre,
		prefixBytes: nil, // Will be set by SetPrefixLiterals if applicable
		forwardDFA:  forwardDFA,
		suffixLen:   suffixLen,
	}, nil
}

// SetPrefixLiterals enables fast path verification using prefix literals.
// Call this after construction if the pattern has a simple structure: ^prefix.*suffix
func (s *MultilineReverseSuffixSearcher) SetPrefixLiterals(prefixLiterals *literal.Seq) {
	if prefixLiterals != nil && !prefixLiterals.IsEmpty() {
		// Get the longest common prefix for verification
		s.prefixBytes = prefixLiterals.LongestCommonPrefix()
	}
}

// findLineStart scans backward from pos to find the start of the line.
// Returns 0 if no newline is found (meaning we're on the first line).
// Returns pos+1 of the \n character if found (start of next line after \n).
//
// Uses bytes.LastIndexByte which is optimized with SIMD on amd64.
func findLineStart(haystack []byte, pos int) int {
	if pos <= 0 {
		return 0
	}
	// Search backward in haystack[:pos] for '\n'
	idx := bytes.LastIndexByte(haystack[:pos], '\n')
	if idx == -1 {
		return 0 // No newline found, line starts at beginning
	}
	return idx + 1 // Line starts after the \n
}

// verifyPrefix checks if the prefix literal matches at the given position.
// Returns true if prefix matches or if no prefix verification is needed.
func (s *MultilineReverseSuffixSearcher) verifyPrefix(haystack []byte, at int) bool {
	if len(s.prefixBytes) == 0 {
		return false // No fast path available
	}
	if at+len(s.prefixBytes) > len(haystack) {
		return false
	}
	return bytes.HasPrefix(haystack[at:], s.prefixBytes)
}

// Find searches using suffix literal prefilter + line-aware verification.
//
// Fast path (when prefix literals available):
//  1. Find suffix using SIMD prefilter
//  2. Find line start (backward scan using SIMD)
//  3. Verify prefix with simple byte comparison
//  4. Return match immediately if prefix matches
//  5. On failure, skip to next line (all candidates on same line will fail)
//
// Slow path (complex patterns):
//  1. Same candidate finding
//  2. Use forward DFA for verification
//
// Performance: O(n) with very low constant factor for fast path.
// Key optimization: when prefix fails, skip entire line - avoids O(n²) worst case.
func (s *MultilineReverseSuffixSearcher) Find(haystack []byte) *Match {
	if len(haystack) == 0 {
		return nil
	}

	// Iterate through suffix candidates
	pos := 0
	for {
		// Find next suffix candidate using prefilter (SIMD accelerated)
		suffixPos := s.prefilter.Find(haystack, pos)
		if suffixPos == -1 {
			return nil
		}

		// Find the start of the line containing this suffix
		lineStart := findLineStart(haystack, suffixPos)

		// Fast path: simple prefix verification (just byte comparison)
		if len(s.prefixBytes) > 0 {
			if s.verifyPrefix(haystack, lineStart) {
				// Match found! No DFA needed.
				return NewMatch(lineStart, suffixPos+s.suffixLen, haystack)
			}
			// Prefix doesn't match at this line start.
			// Optimization: skip to next line - all other candidates on this line
			// will have the same lineStart and will also fail.
			nextLine := bytes.IndexByte(haystack[suffixPos:], '\n')
			if nextLine == -1 {
				return nil // No more lines
			}
			pos = suffixPos + nextLine + 1
		} else {
			// Slow path: use DFA for complex pattern verification
			end := s.forwardDFA.SearchAtAnchored(haystack, lineStart)
			if end >= 0 {
				return NewMatch(lineStart, end, haystack)
			}
			// Move past this suffix candidate
			pos = suffixPos + 1
		}

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
// Performance: O(n) with very low constant factor for fast path.
// Key optimization: when prefix fails, skip entire line - avoids O(n²) worst case.
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
			lineStart = at
		}

		// Fast path: simple prefix verification
		if len(s.prefixBytes) > 0 {
			if s.verifyPrefix(haystack, lineStart) {
				return NewMatch(lineStart, suffixPos+s.suffixLen, haystack)
			}
			// Prefix doesn't match - skip to next line
			nextLine := bytes.IndexByte(haystack[suffixPos:], '\n')
			if nextLine == -1 {
				return nil // No more lines
			}
			pos = suffixPos + nextLine + 1
		} else {
			// Slow path: use DFA
			end := s.forwardDFA.SearchAtAnchored(haystack, lineStart)
			if end >= 0 {
				return NewMatch(lineStart, end, haystack)
			}
			// Move past this suffix candidate
			pos = suffixPos + 1
		}

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
//   - Fast path: simple prefix byte comparison
//   - Slow path: forward DFA verification
//   - Early termination on first match
//   - No Match object allocation
//
// Performance: O(n) with very low constant factor for fast path.
// Key optimization: when prefix fails, skip entire line - avoids O(n²) worst case.
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

		// Fast path: simple prefix verification
		if len(s.prefixBytes) > 0 {
			if s.verifyPrefix(haystack, lineStart) {
				return true
			}
			// Prefix doesn't match - skip to next line
			nextLine := bytes.IndexByte(haystack[suffixPos:], '\n')
			if nextLine == -1 {
				return false // No more lines
			}
			pos = suffixPos + nextLine + 1
		} else {
			// Slow path: use DFA
			if s.forwardDFA.SearchAtAnchored(haystack, lineStart) >= 0 {
				return true
			}
			// Move past this suffix candidate
			pos = suffixPos + 1
		}

		if pos >= len(haystack) {
			return false
		}
	}
}
