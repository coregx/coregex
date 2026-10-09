package nfa

import (
	"regexp/syntax"
	"unicode"
	"unicode/utf8"
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

func addRuneFirstByte(r rune, result *FirstByteSet) {
	if r < 0x80 {
		if !result.bytes[byte(r)] {
			result.bytes[byte(r)] = true
			result.count++
		}
	} else {
		var buf [utf8.UTFMax]byte
		utf8.EncodeRune(buf[:], r)
		if !result.bytes[buf[0]] {
			result.bytes[buf[0]] = true
			result.count++
		}
		result.complete = false
	}
}

func extractFirstBytesLiteral(re *syntax.Regexp, result *FirstByteSet) bool {
	if len(re.Rune) == 0 {
		return false
	}
	r := re.Rune[0]
	addRuneFirstByte(r, result)
	if re.Flags&syntax.FoldCase != 0 {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			addRuneFirstByte(f, result)
		}
	}
	return true
}

func extractFirstBytesCharClass(re *syntax.Regexp, result *FirstByteSet) bool {
	for i := 0; i < len(re.Rune); i += 2 {
		lo, hi := re.Rune[i], re.Rune[i+1]
		if lo >= 0x80 {
			var buf [utf8.UTFMax]byte
			utf8.EncodeRune(buf[:], lo)
			leadLo := buf[0]
			utf8.EncodeRune(buf[:], hi)
			leadHi := buf[0]
			for b := leadLo; b <= leadHi; b++ {
				if !result.bytes[b] {
					result.bytes[b] = true
					result.count++
				}
			}
			result.complete = false
			continue
		}
		asciiHi := hi
		if asciiHi >= 0x80 {
			asciiHi = 0x7F
			result.complete = false
			for b := byte(0xC2); b <= 0xF4; b++ {
				if !result.bytes[b] {
					result.bytes[b] = true
					result.count++
				}
			}
		}
		for r := lo; r <= asciiHi; r++ {
			if !result.bytes[byte(r)] {
				result.bytes[byte(r)] = true
				result.count++
			}
		}
	}
	return result.count > 0
}

//nolint:gocyclo,cyclop
func extractFirstBytesRecursive(re *syntax.Regexp, result *FirstByteSet, depth int) bool {
	if depth > maxFirstBytesDepth {
		return false
	}

	switch re.Op {
	case syntax.OpLiteral:
		return extractFirstBytesLiteral(re, result)

	case syntax.OpCharClass:
		return extractFirstBytesCharClass(re, result)

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
