// Package meta implements the meta-engine orchestrator.
//
// anchored_literal.go implements the UseAnchoredLiteral strategy for patterns
// matching the form: ^[prefix].*[charclass+]suffix$
//
// This strategy provides O(1) matching for common URL/path patterns by:
// 1. O(1) prefix check (if present)
// 2. O(k) suffix check
// 3. O(m) charclass bridge verification (if present)
//
// Example patterns:
//   - ^/.*[\w-]+\.php$  → prefix="/", charclass=[\w-], suffix=".php"
//   - ^.*\.txt$         → no prefix, no charclass, suffix=".txt"
//   - ^https?://.*$     → NOT eligible (no suffix literal)

package meta

import (
	"regexp/syntax"
)

// AnchoredLiteralInfo contains extracted components for fast matching.
// This is used by the UseAnchoredLiteral strategy.
type AnchoredLiteralInfo struct {
	// Prefix is the required prefix literal (may be empty).
	// For ^/.*\.php$, this is "/".
	Prefix []byte

	// Suffix is the required suffix literal (always non-empty).
	// For ^/.*\.php$, this is ".php".
	Suffix []byte

	// CharClassTable is a 256-byte lookup table for the charclass bridge.
	// For [\w-], table[c] is true for [A-Za-z0-9_-].
	// nil if no charclass bridge required.
	CharClassTable *[256]bool

	// CharClassMin is the minimum count of charclass matches required.
	// Usually 1 for charclass+.
	CharClassMin int

	// WildcardMin is 0 for .* or 1 for .+
	WildcardMin int

	// MinLength is the minimum input length for a possible match.
	// Calculated as: len(Prefix) + WildcardMin + CharClassMin + len(Suffix)
	MinLength int
}

// DetectAnchoredLiteral analyzes a regex AST to detect patterns suitable
// for the UseAnchoredLiteral strategy.
//
// Pattern structure (all variations):
//
//	^prefix.*charclass+suffix$   (full form)
//	^prefix.*suffix$             (no charclass bridge)
//	^.*suffix$                   (no prefix)
//	^prefix.+suffix$             (.+ instead of .*)
//
// Requirements:
//   - Must be anchored at both start (^ or \A) and end ($ or \z)
//   - Must contain .* or .+ (greedy wildcard)
//   - Must have a literal suffix before end anchor
//
// Returns nil if pattern doesn't match the required structure.
func DetectAnchoredLiteral(re *syntax.Regexp) *AnchoredLiteralInfo {
	// Must be a concatenation
	if re.Op != syntax.OpConcat {
		return nil
	}

	subs := re.Sub
	if len(subs) < 3 {
		// Minimum: anchor + wildcard + anchor (but this has no suffix)
		// Realistic minimum: anchor + wildcard + suffix + anchor = 4
		return nil
	}

	// Check start anchor (first element)
	if !isStartAnchor(subs[0]) {
		return nil
	}

	// Check end anchor (last element)
	if !isEndAnchor(subs[len(subs)-1]) {
		return nil
	}

	// Find the structure: [anchor] [prefix?] [wildcard] [charclass?] [suffix] [anchor]
	// We work from both ends toward the middle.

	// Extract suffix (second to last, before end anchor)
	suffixIdx := len(subs) - 2
	suffix := extractLiteral(subs[suffixIdx])
	if suffix == nil {
		// No suffix literal - not eligible
		return nil
	}

	// Search for wildcard (.* or .+) between start anchor and suffix
	// Also collect any prefix literals and charclass bridge
	var prefix []byte
	var wildcardIdx = -1
	var wildcardMin int
	var charClassTable *[256]bool
	var charClassMin int

	// Scan from after start anchor to before suffix
	for i := 1; i < suffixIdx; i++ {
		sub := subs[i]

		//nolint:gocritic // ifElseChain: conditions are on different expressions, switch not appropriate
		if isGreedyWildcard(sub) {
			if wildcardIdx != -1 {
				// Multiple wildcards - too complex
				return nil
			}
			wildcardIdx = i
			wildcardMin = getWildcardMin(sub)
		} else if wildcardIdx == -1 {
			// Before wildcard - must be literal (prefix)
			lit := extractLiteral(sub)
			if lit == nil {
				// Non-literal before wildcard - not eligible
				// (could be optional like https?, handle later)
				if sub.Op == syntax.OpQuest {
					// Optional element - check if contains literal
					if len(sub.Sub) > 0 && sub.Sub[0].Op == syntax.OpLiteral {
						// Optional literal like "s?" in "https?"
						// For now, include it as part of prefix checking
						// This is complex - skip for MVP
						return nil
					}
				}
				return nil
			}
			prefix = append(prefix, lit...)
		} else {
			// After wildcard - must be charclass+ or nothing
			if isCharClassPlus(sub) && i == suffixIdx-1 {
				// Charclass bridge right before suffix
				charClassTable = buildCharClassTable(sub.Sub[0])
				charClassMin = 1 // Plus requires at least 1
			} else {
				// Something else between wildcard and suffix - not eligible
				return nil
			}
		}
	}

	if wildcardIdx == -1 {
		// No wildcard found - not eligible
		return nil
	}

	// Calculate minimum length
	minLen := len(prefix) + wildcardMin + charClassMin + len(suffix)

	return &AnchoredLiteralInfo{
		Prefix:         prefix,
		Suffix:         suffix,
		CharClassTable: charClassTable,
		CharClassMin:   charClassMin,
		WildcardMin:    wildcardMin,
		MinLength:      minLen,
	}
}

// isStartAnchor returns true if re is a start anchor (^ or \A).
func isStartAnchor(re *syntax.Regexp) bool {
	return re.Op == syntax.OpBeginText || re.Op == syntax.OpBeginLine
}

// isEndAnchor returns true if re is an end anchor ($ or \z).
func isEndAnchor(re *syntax.Regexp) bool {
	return re.Op == syntax.OpEndText || re.Op == syntax.OpEndLine
}

// isGreedyWildcard returns true if re is .* or .+ (greedy).
func isGreedyWildcard(re *syntax.Regexp) bool {
	if re.Op != syntax.OpStar && re.Op != syntax.OpPlus {
		return false
	}
	if len(re.Sub) != 1 {
		return false
	}
	sub := re.Sub[0]
	return sub.Op == syntax.OpAnyChar || sub.Op == syntax.OpAnyCharNotNL
}

// getWildcardMin returns 0 for * or 1 for +.
func getWildcardMin(re *syntax.Regexp) int {
	if re.Op == syntax.OpPlus {
		return 1
	}
	return 0
}

// isCharClassPlus returns true if re is charclass+.
func isCharClassPlus(re *syntax.Regexp) bool {
	if re.Op != syntax.OpPlus {
		return false
	}
	if len(re.Sub) != 1 {
		return false
	}
	return re.Sub[0].Op == syntax.OpCharClass
}

// extractLiteral extracts bytes from a Literal node.
// Returns nil if not a literal.
func extractLiteral(re *syntax.Regexp) []byte {
	if re.Op != syntax.OpLiteral {
		return nil
	}
	// Convert runes to bytes (assuming ASCII for now)
	result := make([]byte, 0, len(re.Rune))
	for _, r := range re.Rune {
		if r > 255 {
			// Non-ASCII literal - still valid but needs UTF-8 encoding
			// For simplicity, encode as UTF-8
			buf := make([]byte, 4)
			n := encodeRuneToBytes(r, buf)
			result = append(result, buf[:n]...)
		} else {
			result = append(result, byte(r))
		}
	}
	return result
}

// encodeRuneToBytes encodes a rune as UTF-8 into buf.
// Returns the number of bytes written.
// buf must be at least 4 bytes.
func encodeRuneToBytes(r rune, buf []byte) int {
	if r < 0x80 {
		buf[0] = byte(r)
		return 1
	}
	if r < 0x800 {
		buf[0] = byte(0xC0 | (r >> 6))
		buf[1] = byte(0x80 | (r & 0x3F))
		return 2
	}
	if r < 0x10000 {
		buf[0] = byte(0xE0 | (r >> 12))
		buf[1] = byte(0x80 | ((r >> 6) & 0x3F))
		buf[2] = byte(0x80 | (r & 0x3F))
		return 3
	}
	buf[0] = byte(0xF0 | (r >> 18))
	buf[1] = byte(0x80 | ((r >> 12) & 0x3F))
	buf[2] = byte(0x80 | ((r >> 6) & 0x3F))
	buf[3] = byte(0x80 | (r & 0x3F))
	return 4
}

// buildCharClassTable builds a 256-byte lookup table for a CharClass node.
func buildCharClassTable(re *syntax.Regexp) *[256]bool {
	if re.Op != syntax.OpCharClass {
		return nil
	}

	var table [256]bool
	runes := re.Rune

	// Runes are stored as pairs [lo, hi] for each range
	for i := 0; i < len(runes); i += 2 {
		lo, hi := runes[i], runes[i+1]
		// Only set bytes in ASCII range (0-255)
		if lo > 255 {
			continue
		}
		if hi > 255 {
			hi = 255
		}
		for c := lo; c <= hi; c++ {
			table[c] = true
		}
	}

	return &table
}

// MatchAnchoredLiteral performs fast O(prefix + suffix + charclass) matching.
// This is the runtime execution for UseAnchoredLiteral strategy.
//
// Algorithm:
//  1. Length check (O(1))
//  2. Prefix check (O(len(prefix)))
//  3. Suffix check (O(len(suffix)))
//  4. Charclass bridge check (O(k) where k = distance to find match)
//
// Returns true if input matches the pattern.
func MatchAnchoredLiteral(input []byte, info *AnchoredLiteralInfo) bool {
	// O(1) length check
	if len(input) < info.MinLength {
		return false
	}

	// O(len(prefix)) prefix check
	if len(info.Prefix) > 0 {
		if len(input) < len(info.Prefix) {
			return false
		}
		for i, b := range info.Prefix {
			if input[i] != b {
				return false
			}
		}
	}

	// O(len(suffix)) suffix check
	suffixStart := len(input) - len(info.Suffix)
	for i, b := range info.Suffix {
		if input[suffixStart+i] != b {
			return false
		}
	}

	// If no charclass bridge required, we're done
	// (wildcard .* matches everything between prefix and suffix)
	if info.CharClassTable == nil {
		// Still need to verify wildcard minimum
		middleLen := suffixStart - len(info.Prefix)
		return middleLen >= info.WildcardMin
	}

	// O(k) charclass bridge check
	// The charclass+ MUST be immediately before the suffix.
	// For pattern like ^/.*[\w-]+\.php$:
	// - The characters immediately before .php MUST match [\w-]+
	// - We scan backwards from suffix and count consecutive matches
	// - If we hit a non-match before CharClassMin, we FAIL (no reset!)
	//
	// This correctly handles UTF-8 input where Cyrillic "файл" doesn't match [\w-]+.
	charClassEnd := suffixStart
	charClassStart := len(info.Prefix) + info.WildcardMin
	found := 0

	for i := charClassEnd - 1; i >= charClassStart; i-- {
		if info.CharClassTable[input[i]] {
			found++
		} else {
			// Non-matching char breaks the charclass+ sequence
			// The charclass MUST be immediately before suffix, so we're done
			break
		}
	}

	return found >= info.CharClassMin
}
