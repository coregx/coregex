// Package literal provides types and operations for extracting literal sequences
// from regex patterns for prefilter optimization.
package literal

import (
	"regexp/syntax"
)

// ExtractorConfig configures literal extraction limits.
//
// These limits prevent excessive extraction from complex patterns:
//   - MaxLiterals: prevents memory bloat from alternations like (a|b|c|d|...)
//   - MaxLiteralLen: prevents extracting very long literals that hurt cache locality
//   - MaxClassSize: prevents expanding large character classes like [a-z]
//
// Example:
//
//	config := literal.ExtractorConfig{
//	    MaxLiterals:   64,
//	    MaxLiteralLen: 64,
//	    MaxClassSize:  10,
//	}
//	extractor := literal.New(config)
type ExtractorConfig struct {
	// MaxLiterals limits the maximum number of literals to extract.
	// For patterns with many alternations like (a|b|c|...|z), this prevents
	// unbounded memory growth. Default: 64.
	MaxLiterals int

	// MaxLiteralLen limits the maximum length of each extracted literal.
	// Very long literals hurt prefilter performance due to cache misses.
	// Default: 64.
	MaxLiteralLen int

	// MaxClassSize limits the size of character classes to expand.
	// Character classes like [abc] are expanded to ["a", "b", "c"].
	// Large classes like [a-z] (26 chars) are NOT expanded if > MaxClassSize.
	// Default: 10.
	MaxClassSize int
}

// DefaultConfig returns the default extractor configuration.
//
// Defaults are tuned for typical regex patterns:
//   - MaxLiterals: 64 (handles most alternations without bloat)
//   - MaxLiteralLen: 64 (good cache locality for prefilters)
//   - MaxClassSize: 10 (small classes only, avoids [a-z] explosion)
//
// Example:
//
//	extractor := literal.New(literal.DefaultConfig())
func DefaultConfig() ExtractorConfig {
	return ExtractorConfig{
		MaxLiterals:   64,
		MaxLiteralLen: 64,
		MaxClassSize:  10,
	}
}

// Extractor extracts literal sequences from regex patterns.
//
// It analyzes the regex AST (regexp/syntax.Regexp) and extracts:
//   - Prefix literals: literals that must appear at the start
//   - Suffix literals: literals that must appear at the end
//   - Inner literals: any literals that must appear somewhere
//
// These literals enable fast prefiltering before running the full regex engine.
//
// Algorithm overview:
//  1. Parse regex to AST (caller uses regexp/syntax.Parse)
//  2. Walk AST to extract literals based on operation type (OpLiteral, OpConcat, etc.)
//  3. Apply limits (MaxLiterals, MaxLiteralLen, MaxClassSize)
//  4. Return Seq of literals for prefilter selection
//
// Example:
//
//	re, _ := syntax.Parse("(hello|world)", syntax.Perl)
//	extractor := literal.New(literal.DefaultConfig())
//	prefixes := extractor.ExtractPrefixes(re)
//	// prefixes = ["hello", "world"]
type Extractor struct {
	config ExtractorConfig
}

// New creates a new Extractor with the given configuration.
//
// Example:
//
//	config := literal.DefaultConfig()
//	config.MaxLiterals = 128 // Allow more literals
//	extractor := literal.New(config)
func New(config ExtractorConfig) *Extractor {
	return &Extractor{config: config}
}

// ExtractPrefixes extracts prefix literals from the regex.
// Returns literals that must appear at the start of any match.
//
// Handles these syntax.Op types:
//   - OpLiteral: direct literal string → returns it
//   - OpConcat: take first sub-expression
//   - OpAlternate: union of all alternatives (e.g., (foo|bar) → ["foo", "bar"])
//   - OpCharClass: expand small classes (e.g., [abc] → ["a", "b", "c"])
//   - OpCapture: ignore capture group, extract from sub-expression
//   - OpStar/OpQuest/OpPlus: repetition makes prefix optional → return empty
//
// Examples:
//
//	"hello"         → ["hello"]
//	"(foo|bar)"     → ["foo", "bar"]
//	"[abc]test"     → ["atest", "btest", "ctest"]
//	"hello.*world"  → ["hello"]
//	".*foo"         → [] (no prefix requirement)
//
// Returns empty Seq if no prefix literals can be extracted.
func (e *Extractor) ExtractPrefixes(re *syntax.Regexp) *Seq {
	return e.extractPrefixes(re, 0)
}

// extractPrefixes is the internal recursive implementation.
// The depth parameter prevents infinite recursion on malformed patterns.
func (e *Extractor) extractPrefixes(re *syntax.Regexp, depth int) *Seq {
	// Guard against excessive recursion (malformed or deeply nested patterns)
	if depth > 100 {
		return NewSeq()
	}

	switch re.Op {
	case syntax.OpLiteral:
		// Direct literal: "hello" → ["hello"]
		bytes := runeSliceToBytes(re.Rune)
		if len(bytes) > e.config.MaxLiteralLen {
			bytes = bytes[:e.config.MaxLiteralLen]
		}
		return NewSeq(NewLiteral(bytes, true))

	case syntax.OpConcat:
		// Concatenation: take prefix from first sub-expression
		// "abc" → extract from "a" (first part)
		// "hello.*world" → extract from "hello" (first part)
		// "^foo" → skip anchor, extract from "foo"
		if len(re.Sub) == 0 {
			return NewSeq()
		}

		// Skip leading anchors (OpBeginLine, OpBeginText, etc.)
		startIdx := 0
		for startIdx < len(re.Sub) {
			op := re.Sub[startIdx].Op
			if op == syntax.OpBeginLine || op == syntax.OpBeginText {
				startIdx++
			} else {
				break
			}
		}

		if startIdx >= len(re.Sub) {
			return NewSeq() // Only anchors, no literals
		}

		// Get prefixes from first non-anchor part
		firstPrefixes := e.extractPrefixes(re.Sub[startIdx], depth+1)

		// If first part has complete literals, they're not prefixes of the full pattern
		// Mark them as incomplete since more follows
		if firstPrefixes.Len() > 0 && startIdx+1 < len(re.Sub) {
			lits := make([]Literal, firstPrefixes.Len())
			for i := 0; i < firstPrefixes.Len(); i++ {
				lit := firstPrefixes.Get(i)
				lits[i] = NewLiteral(lit.Bytes, false) // Mark as incomplete
			}
			return NewSeq(lits...)
		}

		return firstPrefixes

	case syntax.OpAlternate:
		// Alternation: union of all alternatives
		// (foo|bar) → ["foo", "bar"]
		// (a|b|c) → ["a", "b", "c"]
		var allLits []Literal
		for _, sub := range re.Sub {
			seq := e.extractPrefixes(sub, depth+1)
			for i := 0; i < seq.Len(); i++ {
				allLits = append(allLits, seq.Get(i))
				// Respect MaxLiterals limit
				if len(allLits) >= e.config.MaxLiterals {
					return NewSeq(allLits...)
				}
			}
		}
		return NewSeq(allLits...)

	case syntax.OpCharClass:
		// Character class: expand if small enough
		// [abc] → ["a", "b", "c"]
		// [a-z] → [] (too large, skip)
		return e.expandCharClass(re)

	case syntax.OpCapture:
		// Capture group: ignore the capture, extract from content
		// (foo) → extract from "foo"
		if len(re.Sub) == 0 {
			return NewSeq()
		}
		return e.extractPrefixes(re.Sub[0], depth+1)

	case syntax.OpStar, syntax.OpQuest, syntax.OpPlus:
		// Repetition: treat conservatively as no reliable prefix
		// a*bc → prefix could be "", "a", "aa", ... → no reliable prefix
		// a?bc → prefix could be "" or "a" → no reliable prefix
		// a+bc → prefix is "a"+ → conservatively no prefix
		return NewSeq()

	case syntax.OpBeginLine, syntax.OpBeginText, syntax.OpEndLine, syntax.OpEndText:
		// Anchors: don't contribute literals
		// Handled by parent OpConcat for begin anchors
		return NewSeq()

	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		// Wildcards: can't extract literal
		return NewSeq()

	default:
		// OpEmptyMatch, OpRepeat, etc.: no extractable prefix
		return NewSeq()
	}
}

// ExtractSuffixes extracts suffix literals from the regex.
// Returns literals that must appear at the end of any match.
//
// Algorithm is similar to ExtractPrefixes but analyzes from the end.
//
// Examples:
//
//	"world"         → ["world"]
//	"(foo|bar)"     → ["foo", "bar"]
//	"test[xyz]"     → ["testx", "testy", "testz"]
//	"hello.*world"  → ["world"]
//	"foo.*"         → [] (no suffix requirement)
//
// Returns empty Seq if no suffix literals can be extracted.
func (e *Extractor) ExtractSuffixes(re *syntax.Regexp) *Seq {
	return e.extractSuffixes(re, 0)
}

// extractSuffixes is the internal recursive implementation for suffix extraction.
func (e *Extractor) extractSuffixes(re *syntax.Regexp, depth int) *Seq {
	// Guard against excessive recursion
	if depth > 100 {
		return NewSeq()
	}

	switch re.Op {
	case syntax.OpLiteral:
		// Direct literal
		bytes := runeSliceToBytes(re.Rune)
		if len(bytes) > e.config.MaxLiteralLen {
			// For suffix, take the LAST MaxLiteralLen bytes
			bytes = bytes[len(bytes)-e.config.MaxLiteralLen:]
		}
		return NewSeq(NewLiteral(bytes, true))

	case syntax.OpConcat:
		// Concatenation: take suffix from LAST sub-expression
		if len(re.Sub) == 0 {
			return NewSeq()
		}

		// Get suffixes from last part
		lastSuffixes := e.extractSuffixes(re.Sub[len(re.Sub)-1], depth+1)

		// If last part has complete literals, mark as incomplete if more precedes
		if lastSuffixes.Len() > 0 && len(re.Sub) > 1 {
			lits := make([]Literal, lastSuffixes.Len())
			for i := 0; i < lastSuffixes.Len(); i++ {
				lit := lastSuffixes.Get(i)
				lits[i] = NewLiteral(lit.Bytes, false) // Mark as incomplete
			}
			return NewSeq(lits...)
		}

		return lastSuffixes

	case syntax.OpAlternate:
		// Alternation: union of all alternatives
		var allLits []Literal
		for _, sub := range re.Sub {
			seq := e.extractSuffixes(sub, depth+1)
			for i := 0; i < seq.Len(); i++ {
				allLits = append(allLits, seq.Get(i))
				if len(allLits) >= e.config.MaxLiterals {
					return NewSeq(allLits...)
				}
			}
		}
		return NewSeq(allLits...)

	case syntax.OpCharClass:
		// Character class expansion
		return e.expandCharClass(re)

	case syntax.OpCapture:
		// Ignore capture, extract from content
		if len(re.Sub) == 0 {
			return NewSeq()
		}
		return e.extractSuffixes(re.Sub[0], depth+1)

	case syntax.OpStar, syntax.OpQuest, syntax.OpPlus:
		// Repetition makes suffix optional/variable
		return NewSeq()

	case syntax.OpBeginLine, syntax.OpBeginText, syntax.OpEndLine, syntax.OpEndText:
		// Anchors don't contribute literals
		return NewSeq()

	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		// Wildcard
		return NewSeq()

	default:
		return NewSeq()
	}
}

// ExtractInner extracts inner literals (not necessarily prefix/suffix).
// Useful for patterns like ".*foo.*" where foo must appear somewhere.
//
// This is a simpler extraction that just looks for any required literals
// in the pattern, regardless of position.
//
// Examples:
//
//	".*foo.*"           → ["foo"]
//	".*(hello|world).*" → ["hello", "world"]
//	"prefix.*middle.*suffix" → ["prefix", "middle", "suffix"] (first found)
//
// Returns empty Seq if no inner literals can be extracted.
func (e *Extractor) ExtractInner(re *syntax.Regexp) *Seq {
	return e.extractInner(re, 0)
}

// extractInner is the internal recursive implementation for inner literal extraction.
func (e *Extractor) extractInner(re *syntax.Regexp, depth int) *Seq {
	// Guard against excessive recursion
	if depth > 100 {
		return NewSeq()
	}

	switch re.Op {
	case syntax.OpLiteral:
		bytes := runeSliceToBytes(re.Rune)
		if len(bytes) > e.config.MaxLiteralLen {
			bytes = bytes[:e.config.MaxLiteralLen]
		}
		return NewSeq(NewLiteral(bytes, false)) // Inner literals are never "complete"

	case syntax.OpConcat:
		// For inner, try to find any literal in the concatenation
		// Take the first one we find
		for _, sub := range re.Sub {
			seq := e.extractInner(sub, depth+1)
			if !seq.IsEmpty() {
				return seq
			}
		}
		return NewSeq()

	case syntax.OpAlternate:
		// Union of all alternatives
		var allLits []Literal
		for _, sub := range re.Sub {
			seq := e.extractInner(sub, depth+1)
			for i := 0; i < seq.Len(); i++ {
				allLits = append(allLits, seq.Get(i))
				if len(allLits) >= e.config.MaxLiterals {
					return NewSeq(allLits...)
				}
			}
		}
		return NewSeq(allLits...)

	case syntax.OpCharClass:
		return e.expandCharClass(re)

	case syntax.OpCapture:
		if len(re.Sub) == 0 {
			return NewSeq()
		}
		return e.extractInner(re.Sub[0], depth+1)

	case syntax.OpStar, syntax.OpQuest, syntax.OpPlus:
		// Even for inner, optional repetition means we can't rely on it
		return NewSeq()

	case syntax.OpBeginLine, syntax.OpBeginText, syntax.OpEndLine, syntax.OpEndText:
		return NewSeq()

	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return NewSeq()

	default:
		return NewSeq()
	}
}

// expandCharClass expands character class to literals.
//
// Small character classes like [abc] are expanded to ["a", "b", "c"].
// Large classes like [a-z] (26 characters) are NOT expanded if they exceed
// MaxClassSize, returning an empty Seq instead.
//
// Algorithm:
//  1. Count total runes in the character class
//  2. If count > MaxClassSize, return empty (too large)
//  3. Otherwise, iterate through rune ranges and create a literal for each
//
// Examples:
//
//	[abc]   → ["a", "b", "c"] (3 chars, under limit)
//	[a-c]   → ["a", "b", "c"] (3 chars, under limit)
//	[a-z]   → [] (26 chars, over default limit of 10)
//	[0-9]   → ["0", "1", ..., "9"] if MaxClassSize >= 10
//
// Returns empty Seq if:
//   - Not a character class
//   - Class size exceeds MaxClassSize
func (e *Extractor) expandCharClass(re *syntax.Regexp) *Seq {
	if re.Op != syntax.OpCharClass {
		return NewSeq()
	}

	// Count how many runes are in the class
	// re.Rune contains pairs: [lo1, hi1, lo2, hi2, ...]
	count := 0
	for i := 0; i < len(re.Rune); i += 2 {
		lo, hi := re.Rune[i], re.Rune[i+1]
		count += int(hi - lo + 1)
		if count > e.config.MaxClassSize {
			// Too large, don't expand
			return NewSeq()
		}
	}

	// Expand the class
	var lits []Literal
	for i := 0; i < len(re.Rune); i += 2 {
		lo, hi := re.Rune[i], re.Rune[i+1]
		for r := lo; r <= hi; r++ {
			bytes := []byte(string(r))
			// Truncate if exceeds MaxLiteralLen
			if len(bytes) > e.config.MaxLiteralLen {
				bytes = bytes[:e.config.MaxLiteralLen]
			}
			lits = append(lits, NewLiteral(bytes, true))

			// Respect MaxLiterals limit
			if len(lits) >= e.config.MaxLiterals {
				return NewSeq(lits...)
			}
		}
	}

	return NewSeq(lits...)
}

// Helper functions

// runeSliceToBytes converts []rune to []byte using UTF-8 encoding.
func runeSliceToBytes(runes []rune) []byte {
	return []byte(string(runes))
}
