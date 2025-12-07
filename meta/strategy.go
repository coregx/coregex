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
	// The boundary depends on both the previous and next characters, which changes
	// meaning when searching in reverse. Fall back to forward DFA for these patterns.
	if hasWordBoundary(re) {
		return 0
	}

	isStartAnchored := n.IsAlwaysAnchored()
	isEndAnchored := nfa.IsPatternEndAnchored(re)

	if isStartAnchored || isEndAnchored {
		return 0 // Anchored patterns use other strategies
	}

	// Check if we have good PREFIX literals - if so, prefer UseDFA
	// Prefix literals enable effective forward prefiltering
	hasGoodPrefixLiterals := false
	if literals != nil && !literals.IsEmpty() {
		lcp := literals.LongestCommonPrefix()
		if len(lcp) >= config.MinLiteralLen {
			hasGoodPrefixLiterals = true
		}
	}

	// If good prefix exists, use forward DFA (most efficient)
	if hasGoodPrefixLiterals {
		return 0 // Prefix literals available - use forward DFA
	}

	// No good prefix - check suffix and inner literals
	extractor := literal.New(literal.ExtractorConfig{
		MaxLiterals:   config.MaxLiterals,
		MaxLiteralLen: 64,
		MaxClassSize:  10,
	})

	// Check suffix literals (for patterns like `.*\.txt`)
	suffixLiterals := extractor.ExtractSuffixes(re)
	hasGoodSuffixLiterals := false
	if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
		lcs := suffixLiterals.LongestCommonSuffix()
		if len(lcs) >= config.MinLiteralLen {
			hasGoodSuffixLiterals = true
		}
	}

	if hasGoodSuffixLiterals {
		// Good suffix literal available - use ReverseSuffix
		return UseReverseSuffix
	}

	// No prefix or suffix - try inner literal (for patterns like `.*keyword.*`)
	// Uses AST splitting to build separate NFAs for prefix and suffix portions,
	// enabling true bidirectional search with 10-100x speedup.
	innerInfo := extractor.ExtractInnerForReverseSearch(re)
	if innerInfo != nil {
		lcp := innerInfo.Literals.LongestCommonPrefix()
		if len(lcp) >= config.MinLiteralLen {
			// Good inner literal available - use ReverseInner with AST splitting
			return UseReverseInner
		}
	}

	return 0 // No suitable reverse strategy
}

// SelectStrategy analyzes the NFA and literals to choose the best execution strategy.
//
// Algorithm:
//  1. If end-anchored ($ or \z) and not start-anchored → UseReverseAnchored
//  2. If DFA disabled in config → UseNFA
//  3. If NFA is tiny (< 20 states) → UseNFA (DFA overhead not worth it)
//  4. If good literals exist → UseDFA (prefilter + DFA is fastest)
//  5. If NFA is large (> 100 states) → UseDFA (essential for performance)
//  6. Otherwise → UseBoth (adaptive)
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

	if re != nil && config.EnableDFA {
		// Only use reverse search if:
		// 1. Pattern is end-anchored ($)
		// 2. Pattern is NOT fully start-anchored (not always starting at position 0)
		// 3. Pattern does NOT contain any start anchor (^ or \A) - this catches
		//    alternations like `^a?$|^b?$` where IsAlwaysAnchored() returns false
		//    but the pattern still has start anchors that need proper handling
		if isEndAnchored && !isStartAnchored && !hasStartAnchor {
			// Perfect candidate for reverse search
			// Example: "pattern.*suffix$" on large haystack
			// Forward: O(n*m) tries, Reverse: O(m) one try
			return UseReverseAnchored
		}
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

	// Analyze NFA size
	nfaSize := n.States()

	// Check if we have good literals for prefiltering
	hasGoodLiterals := false
	if literals != nil && !literals.IsEmpty() {
		// Check longest common prefix
		lcp := literals.LongestCommonPrefix()
		if len(lcp) >= config.MinLiteralLen {
			hasGoodLiterals = true
		}
	}

	// Good literals → use prefilter + DFA (best performance)
	// Patterns like "ABXBYXCX" or "(foo|foobar)\d+" benefit massively from:
	//  1. Prefilter finds literal candidates quickly (5-50x speedup)
	//  2. DFA verifies with O(n) deterministic scan
	// This is fast even for tiny NFAs because prefilter does the heavy lifting.
	if hasGoodLiterals {
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

	default:
		return "unknown strategy"
	}
}
