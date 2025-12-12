package nfa

import "regexp/syntax"

// ExtractCharClassRanges extracts byte ranges from a simple char_class+ pattern AST.
// Returns nil if the pattern is not a simple char_class+ pattern.
//
// Simple char_class+ patterns are:
//   - [a-z]+, [A-Z]+, [0-9]+ (single range)
//   - [\w]+, [\d]+, [\s]+ (predefined classes)
//   - [a-zA-Z0-9_]+ (multiple ranges)
//
// NOT supported:
//   - Patterns with anchors (^, $)
//   - Patterns with alternation outside char class
//   - Patterns with concatenation (abc[\w]+)
//   - Unicode char classes (need rune handling)
func ExtractCharClassRanges(re *syntax.Regexp) [][2]byte {
	if re == nil {
		return nil
	}

	// Must be OpPlus or OpStar of a char class
	if re.Op != syntax.OpPlus && re.Op != syntax.OpStar {
		return nil
	}

	if len(re.Sub) != 1 {
		return nil
	}

	sub := re.Sub[0]

	// Do NOT handle capture groups wrapping char class: ([\w])+
	// Capture groups have different semantics (submatch tracking)
	// and CharClassSearcher doesn't support them.

	// Must be a character class
	if sub.Op != syntax.OpCharClass {
		return nil
	}

	// Extract ranges from char class
	// sub.Rune contains pairs: [lo1, hi1, lo2, hi2, ...]
	if len(sub.Rune)%2 != 0 {
		return nil
	}

	var ranges [][2]byte
	for i := 0; i < len(sub.Rune); i += 2 {
		lo, hi := sub.Rune[i], sub.Rune[i+1]

		// Only support ASCII byte ranges
		if lo > 255 || hi > 255 {
			return nil // Unicode - not supported
		}

		ranges = append(ranges, [2]byte{byte(lo), byte(hi)})
	}

	if len(ranges) == 0 {
		return nil
	}

	return ranges
}

// IsSimpleCharClassPlus returns true if the pattern is a simple char_class+ pattern
// that can use CharClassSearcher for optimized matching.
func IsSimpleCharClassPlus(re *syntax.Regexp) bool {
	return ExtractCharClassRanges(re) != nil
}
