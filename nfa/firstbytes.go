package nfa

import (
	"regexp/syntax"
)

// FirstByteSet represents the set of bytes that can start a match.
// Used for O(1) early rejection of non-matching inputs.
type FirstByteSet struct {
	// bytes is a 256-bit lookup table for O(1) membership test
	bytes [256]bool
	// count is the number of valid first bytes (0-256)
	count int
	// complete is true if this set is exhaustive (pattern cannot start with other bytes)
	complete bool
}

// Contains returns true if b can be the first byte of a match.
func (f *FirstByteSet) Contains(b byte) bool {
	return f.bytes[b]
}

// Count returns the number of possible first bytes.
func (f *FirstByteSet) Count() int {
	return f.count
}

// IsComplete returns true if this set is exhaustive.
func (f *FirstByteSet) IsComplete() bool {
	return f.complete
}

// IsUseful returns true if this prefilter can reject inputs.
// Returns false if:
//   - All 256 bytes are valid (e.g., for .* patterns)
//   - No bytes are valid (e.g., for ^ patterns that match empty at position 0)
//   - Set is incomplete (pattern may match starting with unknown bytes)
func (f *FirstByteSet) IsUseful() bool {
	return f.complete && f.count > 0 && f.count < 256
}

// ExtractFirstBytes extracts the set of possible first bytes from a pattern.
// Returns nil if the pattern is too complex or can match empty string.
//
// This is used for O(1) early rejection of non-matching inputs in anchored patterns.
// For example, for ^(\d+|UUID|hex32):
//   - Valid first bytes: 0-9, 'U', 'h'
//   - Any other first byte → immediate rejection
func ExtractFirstBytes(re *syntax.Regexp) *FirstByteSet {
	if re == nil {
		return nil
	}

	result := &FirstByteSet{complete: true}
	if !extractFirstBytesRecursive(re, result, 0) {
		return nil
	}

	return result
}

const maxFirstBytesDepth = 20

// extractFirstBytesRecursive recursively extracts first bytes from a pattern.
// Returns false if extraction fails (pattern too complex or can match empty).
//
//nolint:gocognit,gocyclo,cyclop // Pattern matching naturally has high branching factor
func extractFirstBytesRecursive(re *syntax.Regexp, result *FirstByteSet, depth int) bool {
	if depth > maxFirstBytesDepth {
		return false
	}

	switch re.Op {
	case syntax.OpLiteral:
		// Literal string: first byte is fixed
		if len(re.Rune) == 0 {
			return false // Empty literal matches empty string
		}
		r := re.Rune[0]
		if r > 255 {
			return false // Non-ASCII, too complex
		}
		result.bytes[byte(r)] = true
		result.count++
		return true

	case syntax.OpCharClass:
		// Character class: add all bytes in the class
		for i := 0; i < len(re.Rune); i += 2 {
			lo, hi := re.Rune[i], re.Rune[i+1]
			if hi > 255 {
				hi = 255 // Truncate to ASCII
			}
			if lo > 255 {
				continue // Skip non-ASCII ranges
			}
			for r := lo; r <= hi; r++ {
				if !result.bytes[byte(r)] {
					result.bytes[byte(r)] = true
					result.count++
				}
			}
		}
		return result.count > 0

	case syntax.OpAnyCharNotNL:
		// . matches any byte except newline
		for i := 0; i < 256; i++ {
			if i != '\n' && !result.bytes[byte(i)] {
				result.bytes[byte(i)] = true
				result.count++
			}
		}
		return true

	case syntax.OpAnyChar:
		// (?s). matches any byte
		for i := 0; i < 256; i++ {
			if !result.bytes[byte(i)] {
				result.bytes[byte(i)] = true
				result.count++
			}
		}
		return true

	case syntax.OpBeginLine, syntax.OpBeginText:
		// Anchors don't consume bytes, skip to next
		return true

	case syntax.OpEndLine, syntax.OpEndText:
		// End anchors: pattern could match at end, need to check next part
		return true

	case syntax.OpCapture:
		// Capture group: recurse into content
		if len(re.Sub) != 1 {
			return false
		}
		return extractFirstBytesRecursive(re.Sub[0], result, depth+1)

	case syntax.OpConcat:
		// Concatenation: find first non-anchor part
		for _, sub := range re.Sub {
			// Skip anchors
			if sub.Op == syntax.OpBeginLine || sub.Op == syntax.OpBeginText {
				continue
			}
			return extractFirstBytesRecursive(sub, result, depth+1)
		}
		return false // All anchors, no content

	case syntax.OpAlternate:
		// Alternation: union of all branches
		for _, sub := range re.Sub {
			if !extractFirstBytesRecursive(sub, result, depth+1) {
				return false
			}
		}
		return true

	case syntax.OpStar, syntax.OpQuest:
		// *, ? can match empty, not suitable for first-byte prefilter
		result.complete = false
		return false

	case syntax.OpPlus:
		// + requires at least one match
		if len(re.Sub) != 1 {
			return false
		}
		return extractFirstBytesRecursive(re.Sub[0], result, depth+1)

	case syntax.OpRepeat:
		// {n,m}: if n > 0, first bytes are from sub
		if re.Min == 0 {
			result.complete = false
			return false
		}
		if len(re.Sub) != 1 {
			return false
		}
		return extractFirstBytesRecursive(re.Sub[0], result, depth+1)

	default:
		// Unknown op, bail out
		return false
	}
}
