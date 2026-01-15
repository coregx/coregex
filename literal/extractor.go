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
//
//nolint:cyclop // complexity 26 vs 25 limit due to FoldCase check (Issue #87 fix)
func (e *Extractor) extractPrefixes(re *syntax.Regexp, depth int) *Seq {
	// Guard against excessive recursion (malformed or deeply nested patterns)
	// Also skip case-insensitive patterns because prefilter does case-sensitive
	// byte matching which would miss matches. Issue #87
	if depth > 100 || re.Flags&syntax.FoldCase != 0 {
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

		// Try to expand factored patterns (e.g., ba[rz] → bar, baz)
		if firstPrefixes.Len() > 0 && startIdx+1 < len(re.Sub) {
			expanded := e.tryExpandConcatSuffix(firstPrefixes, re.Sub, startIdx, depth)
			if expanded != nil {
				return expanded
			}
		}

		return firstPrefixes

	case syntax.OpAlternate:
		// Alternation: union of all alternatives
		// (foo|bar) → ["foo", "bar"]
		// (a|b|c) → ["a", "b", "c"]
		// IMPORTANT: If ANY alternative has no prefix requirement (empty Seq),
		// the whole alternation has no prefix requirement.
		// Example: abc|.*? → [] (.*? can match anything, so "abc" isn't required)
		var allLits []Literal
		for _, sub := range re.Sub {
			seq := e.extractPrefixes(sub, depth+1)
			if seq.IsEmpty() {
				// This branch has no prefix requirement (e.g., .*?, .+, empty match)
				// Therefore the whole alternation has no prefix requirement
				return NewSeq()
			}
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
//
//nolint:cyclop // complexity 26 vs 25 limit due to FoldCase check (Issue #87 fix)
func (e *Extractor) extractSuffixes(re *syntax.Regexp, depth int) *Seq {
	// Guard against excessive recursion and skip case-insensitive patterns (Issue #87)
	if depth > 100 || re.Flags&syntax.FoldCase != 0 {
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
		// Concatenation: take suffix from LAST sub-expression and extend with preceding literals
		// This implements the "cross_reverse" operation from rust-regex.
		//
		// Example: for `.*\.(txt|log|md)`:
		//   1. Extract from last part (txt|log|md) → ["txt", "log", "md"]
		//   2. Cross with preceding "." → [".txt", ".log", ".md"]
		//   3. Stop at .* (non-literal) → mark as incomplete
		if len(re.Sub) == 0 {
			return NewSeq()
		}

		// Skip trailing anchors ($, \z) to find the actual last element.
		// For patterns like `\.php$`, the last AST element is OpEndLine,
		// but we want to extract from the `.php` literal before it.
		lastIdx := len(re.Sub) - 1
		for lastIdx >= 0 {
			op := re.Sub[lastIdx].Op
			if op != syntax.OpEndLine && op != syntax.OpEndText {
				break
			}
			lastIdx--
		}
		if lastIdx < 0 {
			return NewSeq()
		}

		// Get suffixes from last non-anchor part
		suffixes := e.extractSuffixes(re.Sub[lastIdx], depth+1)
		if suffixes.IsEmpty() {
			return NewSeq()
		}

		// Walk backwards through concatenation, extending suffixes with preceding literals
		for i := lastIdx - 1; i >= 0; i-- {
			sub := re.Sub[i]

			// Can only extend with literal sub-expressions
			if sub.Op != syntax.OpLiteral {
				// Non-literal encountered: mark all suffixes as incomplete and stop
				lits := make([]Literal, suffixes.Len())
				for j := 0; j < suffixes.Len(); j++ {
					lit := suffixes.Get(j)
					lits[j] = NewLiteral(lit.Bytes, false) // Mark as incomplete
				}
				return NewSeq(lits...)
			}

			// Prepend this literal to all suffixes (cross_reverse)
			prefix := runeSliceToBytes(sub.Rune)
			lits := make([]Literal, suffixes.Len())
			for j := 0; j < suffixes.Len(); j++ {
				lit := suffixes.Get(j)
				// Create new byte slice: prefix + suffix
				newBytes := make([]byte, len(prefix)+len(lit.Bytes))
				copy(newBytes, prefix)
				copy(newBytes[len(prefix):], lit.Bytes)
				// Truncate if too long
				if len(newBytes) > e.config.MaxLiteralLen {
					// For suffix, keep the last MaxLiteralLen bytes
					newBytes = newBytes[len(newBytes)-e.config.MaxLiteralLen:]
				}
				lits[j] = NewLiteral(newBytes, lit.Complete)
			}
			suffixes = NewSeq(lits...)

			// Check size limit
			if suffixes.Len() > e.config.MaxLiterals {
				return suffixes
			}
		}

		return suffixes

	case syntax.OpAlternate:
		// Alternation: union of all alternatives
		// If ANY alternative has no suffix requirement, the whole alternation has none
		var allLits []Literal
		for _, sub := range re.Sub {
			seq := e.extractSuffixes(sub, depth+1)
			if seq.IsEmpty() {
				// This branch has no suffix requirement
				return NewSeq()
			}
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
	// Guard against excessive recursion and skip case-insensitive patterns (Issue #87)
	if depth > 100 || re.Flags&syntax.FoldCase != 0 {
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
		// If ANY alternative has no inner literal requirement, the whole alternation has none
		var allLits []Literal
		for _, sub := range re.Sub {
			seq := e.extractInner(sub, depth+1)
			if seq.IsEmpty() {
				// This branch has no inner literal requirement
				return NewSeq()
			}
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
// expandLiteralCharClass expands a combination of prefix literals and a CharClass
// into multiple complete literals.
//
// This handles the case where the regex parser optimizes "bar|baz" to "ba[rz]".
// We expand it back to ["bar", "baz"] for effective Teddy prefilter matching.
//
// Parameters:
//   - prefixes: the literal prefixes (e.g., ["ba"])
//   - charClass: the character class regex node (e.g., [rz])
//   - hasMore: true if there are more elements after the CharClass
//
// Returns nil if expansion is not possible or would exceed limits.
func (e *Extractor) expandLiteralCharClass(prefixes *Seq, charClass *syntax.Regexp, hasMore bool) *Seq {
	if charClass.Op != syntax.OpCharClass {
		return nil
	}

	// Count characters in the class
	classSize := 0
	for i := 0; i < len(charClass.Rune); i += 2 {
		lo, hi := charClass.Rune[i], charClass.Rune[i+1]
		classSize += int(hi - lo + 1)
		if classSize > e.config.MaxClassSize {
			return nil // Class too large
		}
	}

	// Calculate total number of expanded literals
	totalLits := prefixes.Len() * classSize
	if totalLits > e.config.MaxLiterals {
		return nil // Would exceed limit
	}

	// Expand: each prefix combined with each char class character
	var lits []Literal
	for i := 0; i < prefixes.Len(); i++ {
		prefix := prefixes.Get(i)
		for j := 0; j < len(charClass.Rune); j += 2 {
			lo, hi := charClass.Rune[j], charClass.Rune[j+1]
			for r := lo; r <= hi; r++ {
				// Combine prefix + char
				combined := make([]byte, len(prefix.Bytes)+len(string(r)))
				copy(combined, prefix.Bytes)
				copy(combined[len(prefix.Bytes):], string(r))

				// Truncate if needed
				if len(combined) > e.config.MaxLiteralLen {
					combined = combined[:e.config.MaxLiteralLen]
				}

				// Mark as incomplete if more elements follow
				lits = append(lits, NewLiteral(combined, !hasMore))

				if len(lits) >= e.config.MaxLiterals {
					return NewSeq(lits...)
				}
			}
		}
	}

	return NewSeq(lits...)
}

// tryExpandConcatSuffix attempts to expand factored patterns in a concatenation.
// This handles cases where the regex parser factors common prefixes:
//   - "bar|baz" → ba[rz] (CharClass suffix)
//   - (Wanderlust|Weltanschauung) → W(anderlust|eltanschauung) (Alternation suffix)
//
// Returns expanded Seq or nil if expansion is not possible.
func (e *Extractor) tryExpandConcatSuffix(prefixes *Seq, subs []*syntax.Regexp, startIdx int, depth int) *Seq {
	nextSub := subs[startIdx+1]
	hasMore := startIdx+2 < len(subs)

	// Try CharClass expansion: ba[rz] → bar, baz
	if nextSub.Op == syntax.OpCharClass {
		expanded := e.expandLiteralCharClass(prefixes, nextSub, hasMore)
		if expanded != nil && !expanded.IsEmpty() {
			return expanded
		}
	}

	// Try Alternation expansion: W(anderlust|eltanschauung) → Wanderlust, Weltanschauung
	if nextSub.Op == syntax.OpAlternate {
		expanded := e.expandLiteralAlternate(prefixes, nextSub, hasMore, depth)
		if expanded != nil && !expanded.IsEmpty() {
			return expanded
		}
	}

	// Default: mark prefixes as incomplete since more elements follow
	lits := make([]Literal, prefixes.Len())
	for i := 0; i < prefixes.Len(); i++ {
		lit := prefixes.Get(i)
		lits[i] = NewLiteral(lit.Bytes, false) // Incomplete
	}
	return NewSeq(lits...)
}

// expandLiteralAlternate expands Literal + Alternation back into individual complete literals.
// This handles the case where the regex parser factors common prefixes:
//
//	(Wanderlust|Weltanschauung) → W(anderlust|eltanschauung)
//
// We expand this back to ["Wanderlust", "Weltanschauung"] so that Teddy and Aho-Corasick
// can efficiently search for the complete patterns.
//
// Parameters:
//   - prefixes: the common prefix literals (e.g., ["W"])
//   - alternate: the OpAlternate node containing suffixes
//   - hasMore: true if more elements follow the alternation
//   - depth: current recursion depth
//
// Returns nil if expansion is not possible or would exceed limits.
func (e *Extractor) expandLiteralAlternate(prefixes *Seq, alternate *syntax.Regexp, hasMore bool, depth int) *Seq {
	if alternate.Op != syntax.OpAlternate {
		return nil
	}

	// Extract suffix literals from each alternative
	var suffixLits []Literal
	for _, sub := range alternate.Sub {
		seq := e.extractPrefixes(sub, depth+1)
		if seq.IsEmpty() {
			// One branch has no extractable literal - can't expand
			return nil
		}
		for i := 0; i < seq.Len(); i++ {
			suffixLits = append(suffixLits, seq.Get(i))
		}
	}

	// Calculate total expanded literals
	totalLits := prefixes.Len() * len(suffixLits)
	if totalLits > e.config.MaxLiterals {
		return nil // Would exceed limit
	}

	// Expand: each prefix combined with each suffix
	var lits []Literal
	for i := 0; i < prefixes.Len(); i++ {
		prefix := prefixes.Get(i)
		for _, suffix := range suffixLits {
			// Combine prefix + suffix
			combined := make([]byte, len(prefix.Bytes)+len(suffix.Bytes))
			copy(combined, prefix.Bytes)
			copy(combined[len(prefix.Bytes):], suffix.Bytes)

			// Truncate if needed
			if len(combined) > e.config.MaxLiteralLen {
				combined = combined[:e.config.MaxLiteralLen]
			}

			// Mark as complete only if: no more elements follow AND suffix was complete
			complete := !hasMore && suffix.Complete
			lits = append(lits, NewLiteral(combined, complete))

			if len(lits) >= e.config.MaxLiterals {
				return NewSeq(lits...)
			}
		}
	}

	return NewSeq(lits...)
}

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

// InnerLiteralInfo contains information about an inner literal and its position.
// Used for ReverseInner strategy to identify literals suitable for bidirectional search.
//
// The key insight from rust-regex: we need to split the AST into three parts:
//   - PrefixAST: the portion BEFORE the inner literal (for reverse NFA)
//   - Inner literal: for SIMD prefiltering
//   - SuffixAST: the portion FROM the inner literal onward (for forward NFA)
type InnerLiteralInfo struct {
	// Literals contains the inner literals for prefiltering
	Literals *Seq

	// InnerIdx is the index in concatenation where inner literal was found
	InnerIdx int

	// PrefixAST is the regex AST for the portion BEFORE the inner literal.
	// This is used to build a reverse NFA for finding match start.
	// For pattern `ERROR.*connection.*timeout`, PrefixAST represents `ERROR.*`
	PrefixAST *syntax.Regexp

	// SuffixAST is the regex AST for the portion FROM the inner literal onward.
	// This is used to build a forward NFA for finding match end.
	// For pattern `ERROR.*connection.*timeout`, SuffixAST represents `connection.*timeout`
	SuffixAST *syntax.Regexp
}

// ExtractInnerForReverseSearch extracts inner literals suitable for ReverseInner strategy.
// Returns nil if no suitable inner literal found (only prefix/suffix available).
//
// "Inner" means:
//   - NOT at the very start (otherwise use prefix strategy)
//   - NOT at the very end (otherwise use suffix strategy)
//   - Has wildcards/repetitions both before AND after
//
// This is specifically for patterns like:
//   - `ERROR.*connection.*timeout` → inner literal: "connection"
//   - `func.*Error.*return` → inner literal: "Error"
//   - `prefix.*middle.*suffix` → inner literal: "middle"
//
// Algorithm:
//  1. Pattern must be OpConcat (concatenation of parts)
//  2. Find the first literal that is:
//     a. NOT at position 0 (has content before)
//     b. NOT at last position (has content after)
//     c. Both before and after have wildcards (.*|.+|.?)
//  3. Prefer longer literals
//
// Returns nil if:
//   - Not a concat pattern
//   - Only prefix or suffix literals available
//   - No wildcards before/after literals
//
// Example:
//
//	// Pattern: `ERROR.*connection.*timeout`
//	re, _ := syntax.Parse(`ERROR.*connection.*timeout`, syntax.Perl)
//	extractor := literal.New(literal.DefaultConfig())
//	innerInfo := extractor.ExtractInnerForReverseSearch(re)
//	// innerInfo.Literals = ["connection"]
//	// innerInfo.InnerIdx = 2 (position in concat)
func (e *Extractor) ExtractInnerForReverseSearch(re *syntax.Regexp) *InnerLiteralInfo {
	// Only works on concatenation patterns
	if re.Op != syntax.OpConcat || len(re.Sub) < 3 {
		// Need at least 3 parts: prefix + inner + suffix
		return nil
	}

	// Find the first good inner literal
	// Criteria:
	//  1. Index > 0 (not first position - has prefix)
	//  2. Index < len-1 (not last position - has suffix)
	//  3. Has wildcards before it
	//  4. Has wildcards after it
	for i := 1; i < len(re.Sub)-1; i++ {
		// Check if this sub-expression has extractable literals
		literals := e.extractInner(re.Sub[i], 0)
		if literals.IsEmpty() {
			continue
		}

		// Check if there are wildcards/repetitions before this position
		hasWildcardBefore := false
		for j := 0; j < i; j++ {
			if isWildcardOrRepetition(re.Sub[j]) {
				hasWildcardBefore = true
				break
			}
		}

		// Check if there are wildcards/repetitions after this position
		hasWildcardAfter := false
		for j := i + 1; j < len(re.Sub); j++ {
			if isWildcardOrRepetition(re.Sub[j]) {
				hasWildcardAfter = true
				break
			}
		}

		// If both before and after have wildcards, this is a good inner literal
		if hasWildcardBefore && hasWildcardAfter {
			return &InnerLiteralInfo{
				Literals:  literals,
				InnerIdx:  i,
				PrefixAST: buildPrefixAST(re, i),
				SuffixAST: buildSuffixAST(re, i),
			}
		}
	}

	// No suitable inner literal found
	return nil
}

// buildPrefixAST creates a new Regexp that matches only the prefix portion.
// This is concat[0:splitIdx] - the part BEFORE the inner literal.
// Used for building reverse NFA in ReverseInner strategy.
func buildPrefixAST(concat *syntax.Regexp, splitIdx int) *syntax.Regexp {
	if splitIdx <= 0 {
		return &syntax.Regexp{Op: syntax.OpEmptyMatch}
	}

	if splitIdx == 1 {
		return cloneRegexp(concat.Sub[0])
	}

	// Multiple elements - create new concat
	prefix := &syntax.Regexp{
		Op:    syntax.OpConcat,
		Flags: concat.Flags,
		Sub:   make([]*syntax.Regexp, splitIdx),
	}
	for i := 0; i < splitIdx; i++ {
		prefix.Sub[i] = cloneRegexp(concat.Sub[i])
	}

	return prefix
}

// buildSuffixAST creates a new Regexp that matches the suffix portion.
// This is concat[splitIdx:] - includes the inner literal and everything after.
// Used for building forward NFA in ReverseInner strategy.
func buildSuffixAST(concat *syntax.Regexp, splitIdx int) *syntax.Regexp {
	remaining := len(concat.Sub) - splitIdx
	if remaining <= 0 {
		return &syntax.Regexp{Op: syntax.OpEmptyMatch}
	}

	if remaining == 1 {
		return cloneRegexp(concat.Sub[splitIdx])
	}

	suffix := &syntax.Regexp{
		Op:    syntax.OpConcat,
		Flags: concat.Flags,
		Sub:   make([]*syntax.Regexp, remaining),
	}
	for i := 0; i < remaining; i++ {
		suffix.Sub[i] = cloneRegexp(concat.Sub[splitIdx+i])
	}

	return suffix
}

// cloneRegexp creates a deep copy of a syntax.Regexp.
// This is necessary because Go's syntax.Regexp is mutable and we don't
// want to modify the original AST.
func cloneRegexp(re *syntax.Regexp) *syntax.Regexp {
	if re == nil {
		return nil
	}

	clone := &syntax.Regexp{
		Op:    re.Op,
		Flags: re.Flags,
		Min:   re.Min,
		Max:   re.Max,
		Cap:   re.Cap,
		Name:  re.Name,
	}

	// Clone Rune slice
	if len(re.Rune) > 0 {
		clone.Rune = make([]rune, len(re.Rune))
		copy(clone.Rune, re.Rune)
	}

	// Clone Rune0 (inline storage for small literals)
	clone.Rune0 = re.Rune0

	// Clone Sub slice (recursively)
	if len(re.Sub) > 0 {
		clone.Sub = make([]*syntax.Regexp, len(re.Sub))
		for i, sub := range re.Sub {
			clone.Sub[i] = cloneRegexp(sub)
		}
	}

	// Clone Sub0 (inline storage)
	for i := range re.Sub0 {
		if re.Sub0[i] != nil {
			clone.Sub0[i] = cloneRegexp(re.Sub0[i])
		}
	}

	return clone
}

// isWildcardOrRepetition checks if a regexp node is a wildcard or repetition.
// These indicate variable-length matching before/after inner literal.
func isWildcardOrRepetition(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		return true
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true
	case syntax.OpConcat:
		// Check if any sub-expression is wildcard
		for _, sub := range re.Sub {
			if isWildcardOrRepetition(sub) {
				return true
			}
		}
		return false
	case syntax.OpAlternate:
		// Check if any alternative is wildcard
		for _, sub := range re.Sub {
			if isWildcardOrRepetition(sub) {
				return true
			}
		}
		return false
	case syntax.OpCapture:
		// Check captured content
		if len(re.Sub) > 0 {
			return isWildcardOrRepetition(re.Sub[0])
		}
		return false
	default:
		return false
	}
}

// Helper functions

// runeSliceToBytes converts []rune to []byte using UTF-8 encoding.
func runeSliceToBytes(runes []rune) []byte {
	return []byte(string(runes))
}
