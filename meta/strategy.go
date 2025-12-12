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
	hasGoodLiterals  bool // Good prefix literal (LCP >= MinLiteralLen)
	hasTeddyLiterals bool // Suitable for Teddy (2-8 patterns, each >= 3 bytes)
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

	// Check for simple character class patterns without literals
	// Patterns like [0-9]+, \d+, \w+ benefit from BoundedBacktracker:
	// 2-4x faster than PikeVM due to bit-vector visited tracking instead of SparseSet.
	if !litAnalysis.hasGoodLiterals && !litAnalysis.hasTeddyLiterals && isSimpleCharClass(re) {
		return UseBoundedBacktracker
	}

	// Exact literal alternations → use Teddy directly (literal engine bypass)
	// Patterns like "(foo|bar|baz)" where all literals are complete don't need
	// DFA verification - Teddy.Find() returns exact matches.
	// This is the "literal engine bypass" optimization from Rust regex.
	// Speedup: 50-250x by skipping all DFA/NFA construction overhead.
	if litAnalysis.hasTeddyLiterals && literals.AllComplete() {
		return UseTeddy
	}

	// Good literals → use prefilter + DFA (best performance)
	// Patterns like "ABXBYXCX" or "(foo|foobar)\d+" benefit massively from:
	//  1. Prefilter finds literal candidates quickly (5-50x speedup)
	//  2. DFA verifies with O(n) deterministic scan
	// This is fast even for tiny NFAs because prefilter does the heavy lifting.
	// Also covers Teddy multi-pattern prefilter for alternation patterns where
	// literals are not complete (e.g., "(foo|bar)\d+" needs DFA verification).
	if litAnalysis.hasGoodLiterals || litAnalysis.hasTeddyLiterals {
		return UseDFA
	}

	// Tiny NFA without literals: use PikeVM directly (DFA overhead not worth it)
	// For patterns like "a", ".", "[0-9]", the DFA cache lookup and
	// determinization overhead exceeds the benefit.
	if nfaSize < 20 {
		return UseNFA
	}

	// Large NFA without literals → still use DFA
	// For patterns like "(a|b|c|d|e|f|g|h)*z", the DFA cache
	// prevents re-exploration of the same NFA state sets.
	// Even without prefilter, DFA's deterministic execution is faster
	// than NFA's parallel state tracking.
	if nfaSize > 100 {
		return UseDFA
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

	default:
		return "unknown strategy"
	}
}
