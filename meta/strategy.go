package meta

import (
	"regexp/syntax"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
)

// Strategy represents the execution strategy for regex matching.
//
// The meta-engine chooses between:
//   - UseNFA: use PikeVM exclusively (simple patterns, no cache needed)
//   - UseDFA: use Lazy DFA with NFA fallback (complex patterns, good literals)
//   - UseBoth: adaptive strategy (try DFA first, fallback to NFA on cache full)
//
// Strategy selection is automatic based on pattern analysis.
type Strategy int

const (
	// UseNFA uses only the NFA (PikeVM) engine.
	// Selected for:
	//   - Very small NFAs (< 20 states) where DFA overhead isn't worth it
	//   - Patterns without literals where DFA has no advantage
	//   - When EnableDFA is false in config
	UseNFA Strategy = iota

	// UseDFA uses Lazy DFA with NFA fallback on cache overflow.
	// Selected for:
	//   - Large NFAs (> 100 states) where DFA is essential
	//   - Patterns with good literals (prefilter + DFA is fastest)
	//   - Simple patterns (no alternations) where DFA doesn't blow up
	UseDFA

	// UseBoth uses adaptive strategy: try DFA, fallback to NFA if cache fills.
	// Selected for:
	//   - Medium-sized NFAs (20-100 states)
	//   - Patterns with some literals but complex structure
	//   - Default when pattern characteristics are unclear
	UseBoth

	// UseReverseAnchored uses reverse DFA search for patterns anchored at end.
	// Selected for:
	//   - Patterns with $ or \z anchor (end of text)
	//   - NOT also anchored at start (^)
	//   - Searches backward from end of haystack
	//   - Converts O(n*m) to O(m) for end-anchored patterns
	UseReverseAnchored

	// UseReverseSuffix uses suffix literal prefilter + reverse DFA search.
	// Selected for:
	//   - Patterns with literal suffix (e.g., `.*\.txt`)
	//   - NOT start-anchored (^)
	//   - Has good suffix literal for prefiltering
	//   - Speedup: 10-100x for patterns like `.*\.txt`
	UseReverseSuffix

	// UseOnePass uses one-pass DFA for anchored patterns with capture groups.
	// Selected for:
	//   - Pattern is always anchored (^ or implicit anchor)
	//   - Pattern is "one-pass" (no ambiguity in matching paths)
	//   - Pattern has capture groups (otherwise lazy DFA is faster)
	//   - Speedup: 10-20x over PikeVM for capture group extraction
	//   - Only used for FindSubmatch, not Find
	UseOnePass

	// UseReverseInner uses inner literal prefilter + bidirectional DFA search.
	// Selected for:
	//   - Patterns with inner literal (e.g., `prefix.*inner.*suffix`)
	//   - NOT start-anchored (^) or end-anchored ($)
	//   - Has good inner literal for prefiltering
	//   - NO good prefix or suffix literals (otherwise prefer UseDFA/UseReverseSuffix)
	//   - Has wildcards both before AND after inner literal
	//   - Speedup: 10-100x for patterns like `ERROR.*connection.*timeout`
	UseReverseInner

	// UseBoundedBacktracker uses bounded backtracking with bit-vector visited tracking.
	// Selected for:
	//   - Simple character class patterns (\d+, \w+, [a-z]+) without literals
	//   - Small enough input (states * inputLen <= threshold)
	//   - No prefilter benefit (no extractable literals)
	//   - Speedup: 2-4x over PikeVM for character class patterns
	UseBoundedBacktracker

	// UseTeddy uses Teddy multi-pattern prefilter directly without DFA.
	// Selected for:
	//   - Exact literal alternations like (foo|bar|baz)
	//   - All literals are complete (no regex engine verification needed)
	//   - 2-8 patterns, each >= 3 bytes
	//   - Speedup: 50-250x over PikeVM by skipping all DFA/NFA overhead
	//
	// This implements the "literal engine bypass" optimization from Rust regex:
	// when patterns are exact literals, the prefilter IS the engine.
	UseTeddy

	// UseReverseSuffixSet uses Teddy multi-pattern prefilter for suffix alternations.
	// Selected for:
	//   - Patterns like `.*\.(txt|log|md)` where suffix is an alternation
	//   - No common suffix (LCS is empty), but multiple suffix literals available
	//   - 2-8 suffix literals, each >= 3 bytes
	//   - Speedup: 5-10x over UseBoth by using Teddy for suffix candidates
	//
	// Algorithm:
	//   1. Teddy finds any of the suffix literals (e.g., ".txt", ".log", ".md")
	//   2. Reverse DFA scan from suffix position to find match start
	//   3. For `.*` prefix patterns, match starts at position 0 (skip reverse scan)
	//
	// This is an optimization NOT present in rust-regex (they fallback to Core).
	UseReverseSuffixSet

	// UseCharClassSearcher uses specialized lookup-table searcher for simple char_class+ patterns.
	// Selected for:
	//   - Patterns like `[\w]+`, `[a-z]+`, `\d+` (simple repeated character class)
	//   - NOT concatenations (those use BoundedBacktracker)
	//   - NOT patterns with capture groups
	//   - Speedup: 14-22x over stdlib, 14-17x over BoundedBacktracker
	//
	// Uses 256-byte membership table for O(1) byte classification instead of
	// NFA state tracking. Optimal for "find all words" type patterns.
	UseCharClassSearcher

	// UseDigitPrefilter uses SIMD digit scanning for patterns that must start with digits.
	// Selected for:
	//   - Patterns where ALL alternation branches must start with a digit [0-9]
	//   - Examples: IP address patterns, numeric validators
	//   - Pattern has no extractable prefix literals (due to alternation structure)
	//   - Speedup: 5-10x by skipping non-digit regions with SIMD
	//
	// Algorithm:
	//   1. SIMD scan haystack for digit sequences
	//   2. At each digit position, run lazy DFA to verify match
	//   3. Skip non-digit regions entirely (major speedup for sparse matches)
	//
	// This addresses Issue #50 (IP regex optimization) where alternations like
	// `25[0-5]|2[0-4][0-9]|...` produce no extractable prefix literals.
	UseDigitPrefilter

	// UseAhoCorasick uses Aho-Corasick automaton for large literal alternations.
	// Selected for:
	//   - Exact literal alternations with >8 patterns (beyond Teddy's limit)
	//   - All literals are complete (no regex engine verification needed)
	//   - Each pattern >= 1 byte
	//   - Speedup: 50-500x over PikeVM by using O(n) multi-pattern matching
	//
	// Uses github.com/coregx/ahocorasick library with:
	//   - Dense array transitions for O(1) state lookup
	//   - Byte class compression for memory efficiency
	//   - ~1.6 GB/s throughput
	//
	// This extends the "literal engine bypass" optimization for large pattern sets
	// where Teddy's SIMD approach becomes impractical.
	UseAhoCorasick
)

// String returns a human-readable representation of the Strategy.
func (s Strategy) String() string {
	switch s {
	case UseNFA:
		return "UseNFA"
	case UseDFA:
		return "UseDFA"
	case UseBoth:
		return "UseBoth"
	case UseReverseAnchored:
		return "UseReverseAnchored"
	case UseReverseSuffix:
		return "UseReverseSuffix"
	case UseOnePass:
		return "UseOnePass"
	case UseReverseInner:
		return "UseReverseInner"
	case UseBoundedBacktracker:
		return "UseBoundedBacktracker"
	case UseTeddy:
		return "UseTeddy"
	case UseReverseSuffixSet:
		return "UseReverseSuffixSet"
	case UseCharClassSearcher:
		return "UseCharClassSearcher"
	case UseDigitPrefilter:
		return "UseDigitPrefilter"
	case UseAhoCorasick:
		return "UseAhoCorasick"
	default:
		return "Unknown"
	}
}

// hasWordBoundary recursively checks if a syntax.Regexp contains word boundary assertions.
// Returns true if the pattern contains \b or \B.
//
// Word boundary assertions don't work correctly with reverse DFA search because
// the boundary depends on both adjacent characters, which changes meaning in reverse.
func hasWordBoundary(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			if hasWordBoundary(sub) {
				return true
			}
		}
	case syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		for _, sub := range re.Sub {
			if hasWordBoundary(sub) {
				return true
			}
		}
	}
	return false
}

// isDigitOnlyClass returns true if the character class contains ONLY digits [0-9].
// The runes slice contains pairs: [lo1, hi1, lo2, hi2, ...] representing ranges.
//
// Examples:
//   - [0-9] → runes = [48, 57] → true
//   - [0-5] → runes = [48, 53] → true
//   - [0-9a-z] → runes = [48, 57, 97, 122] → false (includes letters)
//   - [a-z] → runes = [97, 122] → false (no digits)
func isDigitOnlyClass(runes []rune) bool {
	if len(runes) == 0 || len(runes)%2 != 0 {
		return false
	}

	for i := 0; i < len(runes); i += 2 {
		lo, hi := runes[i], runes[i+1]
		// Range must be within '0' (48) to '9' (57)
		if lo < '0' || hi > '9' {
			return false
		}
	}
	return true
}

// isDigitLeadConcat checks if a concatenation pattern is digit-lead.
// For concatenation, we iterate through elements:
// - If an element is optional AND digit-only, we continue (it's fine either way)
// - If an element is optional but NOT digit-only, the pattern is NOT digit-lead
// - If an element is required, we check if it's digit-lead
func isDigitLeadConcat(subs []*syntax.Regexp) bool {
	for _, sub := range subs {
		if isOptionalElement(sub) {
			// Optional element - if it matches, must be digit-only
			if !isOptionalDigitOnly(sub) {
				// Could match non-digit character, so not digit-lead
				return false
			}
			// Optional and digit-only, continue to next element
			continue
		}
		// Required element - must be digit-lead
		return isDigitLeadPattern(sub)
	}
	// All elements were optional - pattern can match empty, not digit-lead
	return false
}

// isOptionalElement returns true if the syntax element can match zero characters.
// This includes Quest (?), Star (*), and Repeat with min=0.
func isOptionalElement(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}
	switch re.Op {
	case syntax.OpQuest, syntax.OpStar:
		return true
	case syntax.OpRepeat:
		return re.Min == 0
	default:
		return false
	}
}

// isOptionalDigitOnly returns true if the optional element, when it matches,
// only matches digits. This is used for [1-9]? type patterns where we need
// to verify the element is safe to skip over in digit-lead detection.
func isOptionalDigitOnly(re *syntax.Regexp) bool {
	if re == nil || len(re.Sub) == 0 {
		return false
	}
	// Check if the sub-pattern (the thing being made optional) is digit-only
	sub := re.Sub[0]
	switch sub.Op {
	case syntax.OpCharClass:
		return isDigitOnlyClass(sub.Rune)
	case syntax.OpLiteral:
		// All runes must be digits
		for _, r := range sub.Rune {
			if r < '0' || r > '9' {
				return false
			}
		}
		return len(sub.Rune) > 0
	default:
		// For other cases, recursively check if digit-lead
		// (If it's digit-lead, any match starts with digit)
		return isDigitLeadPattern(sub)
	}
}

// isDigitLeadPattern returns true if ALL branches of the pattern must start with a digit [0-9].
// This is used to enable digit prefilter optimization for patterns like IP addresses.
//
// The function recursively analyzes the AST to determine if every possible match
// must begin with a digit character. This enables SIMD prefiltering to skip
// non-digit regions entirely.
//
// Examples that return true:
//   - \d+ (digit class with plus)
//   - [0-9]+ (explicit digit range)
//   - [0-9] (single digit required)
//   - 25[0-5]|2[0-4][0-9] (all branches start with digit literal)
//   - (?:25[0-5]|...) (non-capturing group)
//   - (\d+) (capture group wrapping digit pattern)
//   - [0-5][0-9] (concatenation starting with digit)
//
// Examples that return false:
//   - [a-z0-9]+ (may start with letter)
//   - a\d+ (starts with literal 'a')
//   - \d*foo (star can match zero - may start with 'f')
//   - \d?foo (quest can match zero - may start with 'f')
//   - [0-9]* (star can match zero)
//   - .*\d+ (dot-star matches anything)
//   - \w+ (word class includes letters)
func isDigitLeadPattern(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpCharClass:
		// Character class must contain ONLY digits
		return isDigitOnlyClass(re.Rune)

	case syntax.OpLiteral:
		// First rune must be a digit
		return len(re.Rune) > 0 && re.Rune[0] >= '0' && re.Rune[0] <= '9'

	case syntax.OpAlternate:
		// ALL branches must start with digit
		if len(re.Sub) == 0 {
			return false
		}
		for _, sub := range re.Sub {
			if !isDigitLeadPattern(sub) {
				return false
			}
		}
		return true

	case syntax.OpConcat:
		// Delegate to helper to reduce cyclomatic complexity
		if len(re.Sub) == 0 {
			return false
		}
		return isDigitLeadConcat(re.Sub)

	case syntax.OpCapture:
		// Look through capture group
		if len(re.Sub) == 0 {
			return false
		}
		return isDigitLeadPattern(re.Sub[0])

	case syntax.OpPlus:
		// Plus requires at least one match, check the sub-pattern
		if len(re.Sub) == 0 {
			return false
		}
		return isDigitLeadPattern(re.Sub[0])

	case syntax.OpRepeat:
		// OpRepeat with min >= 1 guarantees at least one match
		if len(re.Sub) == 0 {
			return false
		}
		if re.Min >= 1 {
			return isDigitLeadPattern(re.Sub[0])
		}
		// min == 0 means could match zero times
		return false

	case syntax.OpStar, syntax.OpQuest:
		// Star (*) and Quest (?) can match zero times, so pattern might not start with digit
		return false

	case syntax.OpEmptyMatch:
		// Empty match doesn't require any character
		return false

	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		// Dot (.) matches any character, not specifically digits
		return false

	case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText:
		// Anchors don't consume characters
		return false

	case syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		// Word boundaries don't consume characters
		return false

	default:
		return false
	}
}

// digitPrefilterComplexity holds metrics for determining if DigitPrefilter is beneficial.
// High complexity patterns have high false positive rates, making prefiltering counterproductive.
type digitPrefilterComplexity struct {
	// alternationBranches is the total number of alternation branches in the pattern.
	// Example: `(a|b|c)|(d|e)` has 5 branches (3 + 2).
	alternationBranches int

	// maxNestingDepth is the maximum depth of nested alternations.
	// Example: `(a|(b|c))` has depth 2.
	maxNestingDepth int

	// hasNestedRepetition indicates if there's a repetition inside alternation.
	// Example: `(a+|b*)` has nested repetition.
	hasNestedRepetition bool
}

// analyzeDigitPrefilterComplexity analyzes the pattern to determine if DigitPrefilter
// would be beneficial. Returns complexity metrics used to decide if prefiltering
// would have too high a false positive rate.
//
// The analysis is based on Rust regex's observation that prefiltering is counterproductive
// when it produces many false positives that require expensive DFA verification.
func analyzeDigitPrefilterComplexity(re *syntax.Regexp) digitPrefilterComplexity {
	var result digitPrefilterComplexity
	analyzeComplexityRecursive(re, 0, &result)
	return result
}

// analyzeComplexityRecursive recursively analyzes pattern complexity.
func analyzeComplexityRecursive(re *syntax.Regexp, depth int, result *digitPrefilterComplexity) {
	if re == nil {
		return
	}

	switch re.Op {
	case syntax.OpAlternate:
		result.alternationBranches += len(re.Sub)
		newDepth := depth + 1
		if newDepth > result.maxNestingDepth {
			result.maxNestingDepth = newDepth
		}
		for _, sub := range re.Sub {
			analyzeComplexityRecursive(sub, newDepth, result)
		}

	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if depth > 0 {
			result.hasNestedRepetition = true
		}
		for _, sub := range re.Sub {
			analyzeComplexityRecursive(sub, depth, result)
		}

	case syntax.OpConcat, syntax.OpCapture:
		for _, sub := range re.Sub {
			analyzeComplexityRecursive(sub, depth, result)
		}

	default:
		// Leaf nodes (literals, char classes, etc.) - no recursion needed
	}
}

// isDigitPrefilterBeneficial determines if DigitPrefilter would be beneficial
// for the given pattern based on complexity analysis and NFA size.
//
// Returns false (not beneficial) when:
//   - NFA has > 50 states (verification cost too high)
//   - Pattern has > 8 alternation branches (false positive rate too high)
//   - Pattern has > 2 levels of nested alternations
//   - Pattern has repetition inside alternation (state explosion)
//
// These thresholds are based on empirical analysis:
//   - IP pattern: 74 states, 16 branches, depth 2 → NOT beneficial (1.3x slower)
//   - Version pattern: 14 states, 0 branches → beneficial via ReverseInner instead
//
// Reference: Rust regex uses similar heuristics to avoid counterproductive prefiltering.
func isDigitPrefilterBeneficial(re *syntax.Regexp, nfaSize int) bool {
	// Threshold: NFA with more than 50 states has expensive verification
	// IP pattern has 74 states - too complex for digit prefiltering
	const maxNFAStatesForDigitPrefilter = 50

	if nfaSize > maxNFAStatesForDigitPrefilter {
		return false
	}

	complexity := analyzeDigitPrefilterComplexity(re)

	// Threshold: More than 8 alternation branches = high false positive rate
	// IP pattern has ~16 branches across nested alternations
	const maxAlternationBranches = 8
	if complexity.alternationBranches > maxAlternationBranches {
		return false
	}

	// Threshold: Deep nesting (>2) indicates complex structure
	// IP pattern has depth 2 with repetition - borderline
	const maxNestingDepth = 2
	if complexity.maxNestingDepth > maxNestingDepth {
		return false
	}

	// Nested repetition inside alternation causes state explosion
	// Example: `(a+|b+){3}` has exponential state growth
	if complexity.hasNestedRepetition && complexity.maxNestingDepth > 1 {
		return false
	}

	return true
}

// shouldUseDigitPrefilter checks if the pattern should use digit prefilter optimization.
// Returns true if:
//   - Pattern must start with a digit [0-9]
//   - DFA and prefilter are enabled
//   - Pattern complexity is low enough for prefiltering to be beneficial
//
// This is used for simple digit-lead patterns where SIMD digit scanning provides
// speedup. Complex patterns like IP addresses with many alternations are excluded
// because the high false positive rate makes prefiltering counterproductive.
//
// Based on Rust regex's observation: "if a prefilter has a high false positive rate
// and it produces lots of candidates, then a prefilter can overall make a regex
// search slower" (regex-automata/src/util/prefilter/mod.rs).
func shouldUseDigitPrefilter(re *syntax.Regexp, nfaSize int, config Config) bool {
	if re == nil || !config.EnableDFA || !config.EnablePrefilter {
		return false
	}

	// First check if pattern is digit-lead
	if !isDigitLeadPattern(re) {
		return false
	}

	// Then check if prefiltering would be beneficial given complexity
	return isDigitPrefilterBeneficial(re, nfaSize)
}

// shouldUseReverseSuffixSet checks if multiple suffix literals are available for Teddy prefilter.
// This handles patterns like `.*\.(txt|log|md)` where LCS is empty but individual suffixes are useful.
// Returns true if ReverseSuffixSet strategy should be used.
func shouldUseReverseSuffixSet(prefixLiterals, suffixLiterals *literal.Seq) bool {
	if suffixLiterals == nil || suffixLiterals.IsEmpty() {
		return false
	}

	// Skip if this is an exact literal alternation (would be better served by UseTeddy)
	// For exact alternations like `foo|bar|baz`:
	// - PREFIX literals = ["foo", "bar", "baz"]
	// - SUFFIX literals = ["foo", "bar", "baz"] (same!)
	// - All literals are complete
	// For suffix patterns like `.*\.(txt|log|md)`:
	// - PREFIX literals = [] or [""] (due to .*)
	// - SUFFIX literals = [".txt", ".log", ".md"]
	if prefixLiterals != nil && !prefixLiterals.IsEmpty() && prefixLiterals.AllComplete() {
		if prefixLiterals.Len() == suffixLiterals.Len() {
			return false // Exact alternation - use UseTeddy instead
		}
	}

	litCount := suffixLiterals.Len()
	if litCount < 2 || litCount > 8 {
		return false // Teddy requires 2-8 patterns
	}

	// Check if all suffix literals are long enough for efficient Teddy
	for i := 0; i < litCount; i++ {
		if len(suffixLiterals.Get(i).Bytes) < 2 { // Allow 2-byte suffixes for extensions
			return false
		}
	}

	return true
}

// selectReverseStrategy selects reverse-based strategies (ReverseSuffix, ReverseInner).
// Returns 0 if no reverse strategy is suitable.
//
// This is a helper function to reduce cyclomatic complexity in SelectStrategy.
func selectReverseStrategy(n *nfa.NFA, re *syntax.Regexp, literals *literal.Seq, config Config) Strategy {
	// Only applicable if DFA and prefilter enabled, not anchored
	if re == nil || !config.EnableDFA || !config.EnablePrefilter {
		return 0
	}

	// Word boundary assertions (\b, \B) don't work correctly with reverse DFA search.
	if hasWordBoundary(re) {
		return 0
	}

	if n.IsAlwaysAnchored() || nfa.IsPatternEndAnchored(re) {
		return 0 // Anchored patterns use other strategies
	}

	// Check if we have good PREFIX literals - if so, prefer UseDFA
	if literals != nil && !literals.IsEmpty() {
		lcp := literals.LongestCommonPrefix()
		if len(lcp) >= config.MinLiteralLen {
			return 0 // Prefix literals available - use forward DFA
		}
	}

	// No good prefix - check suffix and inner literals
	extractor := literal.New(literal.ExtractorConfig{
		MaxLiterals:   config.MaxLiterals,
		MaxLiteralLen: 64,
		MaxClassSize:  10,
	})

	// Check suffix literals (for patterns like `.*\.txt`)
	suffixLiterals := extractor.ExtractSuffixes(re)
	if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
		lcs := suffixLiterals.LongestCommonSuffix()
		if len(lcs) >= config.MinLiteralLen {
			return UseReverseSuffix // Good suffix literal available
		}
	}

	// No common suffix (LCS empty), but check if multiple suffix literals available
	// for Teddy multi-suffix prefilter. This handles patterns like `.*\.(txt|log|md)`.
	if shouldUseReverseSuffixSet(literals, suffixLiterals) {
		return UseReverseSuffixSet
	}

	// No prefix or suffix - try inner literal (for patterns like `.*keyword.*`)
	innerInfo := extractor.ExtractInnerForReverseSearch(re)
	if innerInfo != nil {
		lcp := innerInfo.Literals.LongestCommonPrefix()
		// Single-character inner literals like "@" can be effective for email patterns
		// because: (1) Match() is fast with memchr prefilter, (2) Find() uses
		// early return optimization. ReverseInner detects quadratic behavior
		// and falls back to Core when needed.
		if len(lcp) >= 1 {
			return UseReverseInner // Inner literal available - use ReverseInner
		}
	}

	return 0 // No suitable reverse strategy
}

// isSimpleCharClass checks if a regexp is a simple character class pattern
// like [0-9], \d, \w, etc. that doesn't benefit from DFA overhead.
// Returns true for patterns that are just repeats of character classes.
//
// This also handles patterns with capture groups wrapping character classes,
// like (a|b|c)+ which Go's parser optimizes to Plus(Capture(CharClass)).
// BoundedBacktracker can handle capture groups efficiently (they're epsilon
// transitions in the NFA), and is 3-7x faster than PikeVM for these patterns.
func isSimpleCharClass(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpCharClass:
		// Direct character class like [0-9] or \d
		return true
	case syntax.OpPlus, syntax.OpStar, syntax.OpQuest, syntax.OpRepeat:
		// Repeat of character class like [0-9]+ or \d*
		if len(re.Sub) == 1 {
			return isSimpleCharClass(re.Sub[0])
		}
	case syntax.OpConcat:
		// Allow concatenations of character classes like [0-9]+[a-z]+
		// but only if all are simple
		for _, sub := range re.Sub {
			if !isSimpleCharClass(sub) {
				return false
			}
		}
		return true
	case syntax.OpCapture:
		// Look through capture groups - (a|b|c)+ parses as Plus(Capture(CharClass))
		// BoundedBacktracker handles captures correctly (epsilon transitions)
		if len(re.Sub) == 1 {
			return isSimpleCharClass(re.Sub[0])
		}
	}
	return false
}

// literalAnalysis contains the results of analyzing literals for strategy selection.
type literalAnalysis struct {
	hasGoodLiterals        bool // Good prefix literal (LCP >= MinLiteralLen)
	hasTeddyLiterals       bool // Suitable for Teddy (2-8 patterns, each >= 3 bytes)
	hasAhoCorasickLiterals bool // Suitable for Aho-Corasick (>8 patterns, each >= 1 byte)
}

// selectLiteralStrategy selects strategy based on literal analysis.
// Returns 0 if no literal-based strategy is suitable.
// This is a helper function to reduce cyclomatic complexity in SelectStrategy.
func selectLiteralStrategy(literals *literal.Seq, litAnalysis literalAnalysis) Strategy {
	if literals == nil {
		return 0
	}

	// Exact literal alternations → use Teddy directly (literal engine bypass)
	// Patterns like "(foo|bar|baz)" where all literals are complete don't need
	// DFA verification - Teddy.Find() returns exact matches.
	// Speedup: 50-250x by skipping all DFA/NFA construction overhead.
	if litAnalysis.hasTeddyLiterals && literals.AllComplete() {
		return UseTeddy
	}

	// Large literal alternations → use Aho-Corasick (extends literal engine bypass)
	// Patterns with >8 literals exceed Teddy's capacity but Aho-Corasick handles
	// thousands of patterns with O(n) matching time.
	// Speedup: 50-500x by using dense array transitions (~1.6 GB/s throughput).
	if litAnalysis.hasAhoCorasickLiterals && literals.AllComplete() {
		return UseAhoCorasick
	}

	return 0
}

// analyzeLiterals checks if literals are suitable for prefiltering.
// This is a helper function to reduce cyclomatic complexity in SelectStrategy.
func analyzeLiterals(literals *literal.Seq, config Config) literalAnalysis {
	result := literalAnalysis{}

	if literals == nil || literals.IsEmpty() {
		return result
	}

	// Check longest common prefix (for single-literal prefilter like Memmem)
	lcp := literals.LongestCommonPrefix()
	if len(lcp) >= config.MinLiteralLen {
		result.hasGoodLiterals = true
	}

	// Check for Teddy prefilter suitability (2-8 literals, each >= 3 bytes)
	// Teddy doesn't need common prefix - it can search for multiple distinct literals.
	// This enables fast alternation pattern matching: (foo|bar|baz|qux)
	litCount := literals.Len()
	if litCount >= 2 && litCount <= 8 {
		allLongEnough := true
		for i := 0; i < litCount; i++ {
			if len(literals.Get(i).Bytes) < 3 {
				allLongEnough = false
				break
			}
		}
		if allLongEnough {
			result.hasTeddyLiterals = true
		}
	}

	// Check for Aho-Corasick suitability (>8 literals, each >= 1 byte)
	// Aho-Corasick handles large pattern sets efficiently with O(n) matching.
	// This extends the "literal engine bypass" optimization beyond Teddy's 8 pattern limit.
	if litCount > 8 {
		allNonEmpty := true
		for i := 0; i < litCount; i++ {
			if len(literals.Get(i).Bytes) < 1 {
				allNonEmpty = false
				break
			}
		}
		if allNonEmpty {
			result.hasAhoCorasickLiterals = true
		}
	}

	return result
}

// SelectStrategy analyzes the NFA and literals to choose the best execution strategy.
//
// Algorithm:
//  1. If end-anchored ($ or \z) and not start-anchored → UseReverseAnchored
//  2. If DFA disabled in config → UseNFA
//  3. If NFA is tiny (< 20 states) → UseNFA (DFA overhead not worth it)
//  4. If simple character class pattern without literals → UseNFA (DFA overhead not worth it)
//  5. If good literals exist → UseDFA (prefilter + DFA is fastest)
//  6. If NFA is large (> 100 states) → UseDFA (essential for performance)
//  7. Otherwise → UseBoth (adaptive)
//
// "Good literals" means:
//   - At least one literal exists
//   - Longest common prefix (LCP) length >= MinLiteralLen
//   - This enables effective prefiltering
//
// Parameters:
//   - n: the compiled NFA to analyze
//   - re: the parsed regexp (for anchor detection, can be nil)
//   - literals: extracted prefix literals (can be nil)
//   - config: meta-engine configuration
//
// Example:
//
//	strategy := meta.SelectStrategy(nfa, re, literals, config)
//	switch strategy {
//	case meta.UseNFA:
//	    // Use PikeVM only
//	case meta.UseDFA:
//	    // Use Lazy DFA
//	case meta.UseReverseAnchored:
//	    // Use reverse search
//	case meta.UseBoth:
//	    // Adaptive
//	}
func SelectStrategy(n *nfa.NFA, re *syntax.Regexp, literals *literal.Seq, config Config) Strategy {
	// Check for end-anchored patterns (highest priority optimization)
	// Pattern must:
	//   1. Be anchored at end ($ or \z)
	//   2. NOT be anchored at start (^ or \A)
	//   3. Have DFA enabled
	// This converts O(n*m) forward search to O(m) reverse search
	//
	// Note: We must avoid UseReverseAnchored for patterns that contain any start
	// anchor (^ or \A), even in alternations like `^a?$|^b?$`. The reverse DFA
	// cannot properly handle start anchors and would produce false positives.
	isStartAnchored := n.IsAlwaysAnchored()
	isEndAnchored := re != nil && nfa.IsPatternEndAnchored(re)
	hasStartAnchor := re != nil && nfa.IsPatternStartAnchored(re)

	if re != nil && config.EnableDFA && isEndAnchored && !isStartAnchored && !hasStartAnchor {
		// Perfect candidate for reverse search
		// Example: "pattern.*suffix$" on large haystack
		// Forward: O(n*m) tries, Reverse: O(m) one try
		return UseReverseAnchored
	}

	// Check for inner/suffix literal optimizations (second priority)
	// Delegated to helper function to reduce cyclomatic complexity
	if strategy := selectReverseStrategy(n, re, literals, config); strategy != 0 {
		return strategy
	}

	// If DFA disabled, always use NFA
	if !config.EnableDFA {
		return UseNFA
	}

	// Analyze NFA size and literals
	nfaSize := n.States()
	litAnalysis := analyzeLiterals(literals, config)

	// Check for simple char_class+ patterns (HIGHEST priority for character class patterns)
	// Patterns like [\w]+, [a-z]+, \d+ use CharClassSearcher: 14-17x faster than BoundedBacktracker
	// This must come BEFORE BoundedBacktracker check because CharClassSearcher is much faster
	// for the simple case (no concatenations, no capture groups).
	if !litAnalysis.hasGoodLiterals && !litAnalysis.hasTeddyLiterals && nfa.IsSimpleCharClassPlus(re) {
		return UseCharClassSearcher
	}

	// Check for complex character class patterns (concatenations, captures) without literals
	// Patterns like [0-9]+[a-z]+ or (a|b|c)+ benefit from BoundedBacktracker:
	// 2-4x faster than PikeVM due to bit-vector visited tracking instead of SparseSet.
	if !litAnalysis.hasGoodLiterals && !litAnalysis.hasTeddyLiterals && isSimpleCharClass(re) {
		return UseBoundedBacktracker
	}

	// Check for exact literal alternations (Teddy, Aho-Corasick)
	// Delegated to helper function to reduce cyclomatic complexity.
	if strategy := selectLiteralStrategy(literals, litAnalysis); strategy != 0 {
		return strategy
	}

	// Tiny NFA with literals: use prefilter + NFA (like Rust)
	// For patterns like "j[a-z]+p", DFA construction overhead is not worth it
	// on small inputs. NFA with prefilter skip-ahead is faster.
	// The prefilter (memchr) jumps to candidates, NFA verifies in O(pattern) time.
	if nfaSize < 20 && litAnalysis.hasGoodLiterals {
		return UseNFA // findIndicesNFA now uses prefilter for skip-ahead
	}

	// Tiny NFA without literals: use PikeVM directly (DFA overhead not worth it)
	// For patterns like "a", ".", "[0-9]", the DFA cache lookup and
	// determinization overhead exceeds the benefit.
	if nfaSize < 20 {
		return UseNFA
	}

	// Good literals on larger NFA → use prefilter + DFA (best performance)
	// Patterns like "ABXBYXCX" or "(foo|foobar)\d+" benefit massively from:
	//  1. Prefilter finds literal candidates quickly (5-50x speedup)
	//  2. DFA verifies with O(n) deterministic scan
	// Also covers Teddy multi-pattern prefilter for alternation patterns where
	// literals are not complete (e.g., "(foo|bar)\d+" needs DFA verification).
	if litAnalysis.hasGoodLiterals || litAnalysis.hasTeddyLiterals {
		return UseDFA
	}

	// Large NFA without literals → still use DFA
	// For patterns like "(a|b|c|d|e|f|g|h)*z", the DFA cache
	// prevents re-exploration of the same NFA state sets.
	// Even without prefilter, DFA's deterministic execution is faster
	// than NFA's parallel state tracking.
	if nfaSize > 100 {
		return UseDFA
	}

	// Check for simple digit-lead patterns that have no extractable literals.
	// Complex patterns like IP addresses are excluded due to high false positive rate.
	// Delegated to helper function to reduce cyclomatic complexity.
	if shouldUseDigitPrefilter(re, nfaSize, config) {
		return UseDigitPrefilter
	}

	// Medium NFA without strong characteristics → adaptive
	// Try DFA first (may hit cache), fallback to NFA if cache fills.
	// This handles patterns like "a*b*c*" where DFA may or may not help.
	return UseBoth
}

// StrategyReason provides a human-readable explanation for strategy selection.
//
// This is useful for debugging and performance tuning.
//
// Example:
//
//	strategy := meta.SelectStrategy(nfa, literals, config)
//	reason := meta.StrategyReason(strategy, nfa, literals, config)
//	log.Printf("Using %s: %s", strategy, reason)
func StrategyReason(strategy Strategy, n *nfa.NFA, literals *literal.Seq, config Config) string {
	nfaSize := n.States()

	switch strategy {
	case UseNFA:
		if !config.EnableDFA {
			return "DFA disabled in configuration"
		}
		if nfaSize < 20 {
			return "tiny NFA (< 20 states), DFA overhead not worth it"
		}
		return "no good literals and small NFA"

	case UseDFA:
		if literals != nil && !literals.IsEmpty() {
			lcp := literals.LongestCommonPrefix()
			if len(lcp) >= config.MinLiteralLen {
				return "good literals available for prefilter + DFA"
			}
		}
		if nfaSize > 100 {
			return "large NFA (> 100 states), DFA essential"
		}
		return "DFA selected for performance"

	case UseBoth:
		return "adaptive strategy (medium complexity pattern)"

	case UseReverseAnchored:
		return "reverse search for end-anchored pattern (O(m) instead of O(n*m))"

	case UseReverseSuffix:
		return "suffix literal prefilter + reverse DFA (10-100x for patterns like .*\\.txt)"

	case UseOnePass:
		return "one-pass DFA for anchored pattern with captures (10-20x over PikeVM)"

	case UseReverseInner:
		return "inner literal prefilter + bidirectional DFA (10-100x for patterns like ERROR.*connection.*timeout)"

	case UseBoundedBacktracker:
		return "bounded backtracker for simple character class pattern (2-4x faster than PikeVM)"

	case UseTeddy:
		return "Teddy multi-pattern prefilter for exact literal alternation (50-250x by skipping DFA)"

	case UseReverseSuffixSet:
		return "Teddy multi-suffix prefilter for suffix alternation (5-10x for patterns like .*\\.(txt|log|md))"

	case UseCharClassSearcher:
		return "specialized lookup-table searcher for char_class+ patterns (14-17x faster than BoundedBacktracker)"

	case UseDigitPrefilter:
		return "SIMD digit scanner for simple digit-lead patterns (5-10x, excludes complex alternations)"

	case UseAhoCorasick:
		return "Aho-Corasick automaton for large literal alternations (50-500x for >8 pattern sets)"

	default:
		return "unknown strategy"
	}
}
