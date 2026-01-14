package nfa

import (
	"regexp/syntax"
)

// CompositeSearcher is a specialized searcher for concatenated character class patterns.
// For patterns like `[a-zA-Z]+[0-9]+`, it uses sequential lookup tables to achieve
// 5-6x speedup over BoundedBacktracker.
//
// Algorithm:
//  1. Each char class part has a [256]bool membership table for O(1) lookup
//  2. Greedy matching: consume as many characters as possible for each part
//  3. Backtrack if a part doesn't meet its minimum match requirement
//
// Example patterns:
//   - [a-zA-Z]+[0-9]+  → letters followed by digits
//   - \d+\s+\w+        → digits, whitespace, word chars
//   - [a-z]+[A-Z]+     → lowercase then uppercase
//
// Thread safety: NOT thread-safe. For concurrent usage, each goroutine needs its own instance.
//
// Reference: https://github.com/coregx/coregex/issues/72
type CompositeSearcher struct {
	parts []*charClassPart

	// matchLengths is pre-allocated scratch space for backtracking.
	// Reused across calls to avoid per-match allocations.
	matchLengths []int
}

// charClassPart represents one segment of a composite pattern.
type charClassPart struct {
	// membership is a 256-byte lookup table for O(1) character classification
	membership [256]bool

	// minMatch is the minimum number of characters this part must match
	// For +: minMatch=1, for *: minMatch=0, for ?: minMatch=0
	minMatch int

	// maxMatch is the maximum characters (0 = unlimited for + and *)
	maxMatch int
}

// NewCompositeSearcher creates a CompositeSearcher from a syntax.Regexp.
// Returns nil if the pattern is not a valid composite char class pattern.
//
// Valid patterns are concatenations of character classes with +, *, ?, or {n,m} quantifiers.
func NewCompositeSearcher(re *syntax.Regexp) *CompositeSearcher {
	parts := extractCompositeCharClassParts(re)
	if len(parts) == 0 {
		return nil
	}

	return &CompositeSearcher{
		parts:        parts,
		matchLengths: make([]int, len(parts)), // Pre-allocate to avoid per-match allocation
	}
}

// extractCompositeCharClassParts extracts char class parts from a composite pattern.
// Returns nil if the pattern is not a valid composite.
func extractCompositeCharClassParts(re *syntax.Regexp) []*charClassPart {
	if re == nil {
		return nil
	}

	// Handle concatenation of char classes
	if re.Op == syntax.OpConcat {
		var parts []*charClassPart
		for _, sub := range re.Sub {
			part := extractSinglePart(sub)
			if part == nil {
				return nil // Not a valid composite
			}
			parts = append(parts, part)
		}
		if len(parts) < 2 {
			return nil // Need at least 2 parts for composite
		}
		return parts
	}

	return nil
}

// extractSinglePart extracts a single char class part from a quantified char class.
func extractSinglePart(re *syntax.Regexp) *charClassPart {
	if re == nil {
		return nil
	}

	var charClass *syntax.Regexp
	var minMatch, maxMatch int

	switch re.Op {
	case syntax.OpPlus:
		// cc+ → minMatch=1, maxMatch=unlimited (0 means unlimited)
		if len(re.Sub) != 1 {
			return nil
		}
		charClass = re.Sub[0]
		minMatch = 1
		maxMatch = 0

	case syntax.OpStar:
		// cc* → minMatch=0, maxMatch=unlimited
		if len(re.Sub) != 1 {
			return nil
		}
		charClass = re.Sub[0]
		minMatch = 0
		maxMatch = 0

	case syntax.OpQuest:
		// cc? → minMatch=0, maxMatch=1
		if len(re.Sub) != 1 {
			return nil
		}
		charClass = re.Sub[0]
		minMatch = 0
		maxMatch = 1

	case syntax.OpRepeat:
		// cc{n,m} → minMatch=n, maxMatch=m
		if len(re.Sub) != 1 {
			return nil
		}
		charClass = re.Sub[0]
		minMatch = re.Min
		maxMatch = re.Max

	case syntax.OpCharClass:
		// Bare char class without quantifier - treat as cc{1,1}
		charClass = re
		minMatch = 1
		maxMatch = 1

	default:
		return nil
	}

	// Validate that the inner is a char class
	if charClass.Op != syntax.OpCharClass {
		return nil
	}

	// Build membership table
	var membership [256]bool
	runes := charClass.Rune
	for i := 0; i < len(runes); i += 2 {
		lo, hi := runes[i], runes[i+1]
		// Only support ASCII for now
		if lo > 255 || hi > 255 {
			return nil
		}
		for r := lo; r <= hi; r++ {
			membership[byte(r)] = true
		}
	}

	return &charClassPart{
		membership: membership,
		minMatch:   minMatch,
		maxMatch:   maxMatch,
	}
}

// IsMatch returns true if the haystack contains a match.
func (c *CompositeSearcher) IsMatch(haystack []byte) bool {
	_, _, ok := c.Search(haystack)
	return ok
}

// Search finds the first match in haystack.
// Returns (start, end, found).
func (c *CompositeSearcher) Search(haystack []byte) (int, int, bool) {
	return c.SearchAt(haystack, 0)
}

// SearchAt finds the first match starting at or after position at.
// Returns (start, end, found).
func (c *CompositeSearcher) SearchAt(haystack []byte, at int) (int, int, bool) {
	if len(c.parts) == 0 {
		return at, at, true // Empty pattern matches empty string
	}

	n := len(haystack)

	// Try to match at each position
	for pos := at; pos <= n; pos++ {
		if end, ok := c.matchAt(haystack, pos); ok {
			return pos, end, true
		}
	}

	return -1, -1, false
}

// matchAt tries to match the composite pattern starting at position pos.
// Returns (end position, success).
//
// Uses backtracking to handle overlapping character classes:
// For pattern `\w+[0-9]+` on "abc123", the first part (\w+) initially
// consumes all 6 characters. Backtracking gives back digits until
// [0-9]+ can match its minimum (1 character).
func (c *CompositeSearcher) matchAt(haystack []byte, pos int) (int, bool) {
	// Reset pre-allocated matchLengths (faster than allocating new slice)
	for i := range c.matchLengths {
		c.matchLengths[i] = 0
	}
	return c.matchAtWithBacktrack(haystack, pos, 0, c.matchLengths)
}

// matchAtWithBacktrack recursively matches parts with backtracking support.
func (c *CompositeSearcher) matchAtWithBacktrack(haystack []byte, pos int, partIdx int, matchLengths []int) (int, bool) {
	if partIdx >= len(c.parts) {
		// All parts matched successfully
		return pos, true
	}

	part := c.parts[partIdx]
	n := len(haystack)

	// Greedy match: consume as many characters as possible
	maxLen := n - pos
	if part.maxMatch > 0 && part.maxMatch < maxLen {
		maxLen = part.maxMatch
	}

	// Count how many characters we can consume
	canConsume := 0
	for canConsume < maxLen && pos+canConsume < n && part.membership[haystack[pos+canConsume]] {
		canConsume++
	}

	// Try from greedy (max) down to minimum, backtracking if next parts fail
	for tryLen := canConsume; tryLen >= part.minMatch; tryLen-- {
		matchLengths[partIdx] = tryLen
		if end, ok := c.matchAtWithBacktrack(haystack, pos+tryLen, partIdx+1, matchLengths); ok {
			return end, true
		}
	}

	return -1, false
}

// IsCompositeCharClassPattern returns true if the pattern is a valid composite char class pattern.
// A composite pattern is a concatenation of 2+ quantified character classes like [a-zA-Z]+[0-9]+.
//
// Requirements:
//   - Must be OpConcat
//   - Each sub-pattern must be a quantified char class (OpPlus, OpStar, OpQuest, OpRepeat)
//   - At least 2 parts
//   - No anchors, no captures, no alternations
func IsCompositeCharClassPattern(re *syntax.Regexp) bool {
	if re == nil || re.Op != syntax.OpConcat {
		return false
	}

	if len(re.Sub) < 2 {
		return false
	}

	for _, sub := range re.Sub {
		if !isValidCompositePart(sub) {
			return false
		}
	}

	return true
}

// isValidCompositePart checks if a sub-expression is valid for composite searcher.
func isValidCompositePart(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpPlus, syntax.OpStar, syntax.OpQuest:
		// Must have exactly one sub which is a char class
		if len(re.Sub) != 1 {
			return false
		}
		return re.Sub[0].Op == syntax.OpCharClass

	case syntax.OpRepeat:
		// Must have exactly one sub which is a char class
		if len(re.Sub) != 1 {
			return false
		}
		return re.Sub[0].Op == syntax.OpCharClass

	case syntax.OpCharClass:
		// Bare char class (implicit {1,1})
		return true

	default:
		return false
	}
}
