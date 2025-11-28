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
	default:
		return "Unknown"
	}
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
	if re != nil && config.EnableDFA {
		isEndAnchored := nfa.IsPatternEndAnchored(re)
		isStartAnchored := n.IsAlwaysAnchored()

		if isEndAnchored && !isStartAnchored {
			// Perfect candidate for reverse search
			// Example: "pattern.*suffix$" on large haystack
			// Forward: O(n*m) tries, Reverse: O(m) one try
			return UseReverseAnchored
		}
	}

	// Check for suffix literal optimization (second priority)
	// Pattern must:
	//   1. NOT be start-anchored (^ or \A)
	//   2. NOT be end-anchored ($ or \z) - already handled above
	//   3. Have good suffix literals for prefiltering
	//   4. NOT have good prefix literals (prefer prefix search with UseDFA)
	//   5. Have DFA enabled
	// This converts O(n*m) to O(k*m) where k=suffix candidates
	//
	// IMPORTANT: Only use ReverseSuffix for patterns like `.*\.txt` where:
	//   - Prefix is a wildcard (no good prefix literal)
	//   - Suffix is a concrete literal
	// For pure literals like "hello", use UseDFA with prefix prefilter (much faster).
	//
	// ZERO-ALLOCATION: Uses IsMatchReverse for backward scanning without byte reversal
	if re != nil && config.EnableDFA && config.EnablePrefilter {
		isStartAnchored := n.IsAlwaysAnchored()
		isEndAnchored := nfa.IsPatternEndAnchored(re)

		if !isStartAnchored && !isEndAnchored {
			// First check if we have good PREFIX literals - if so, prefer UseDFA
			hasGoodPrefixLiterals := false
			if literals != nil && !literals.IsEmpty() {
				lcp := literals.LongestCommonPrefix()
				if len(lcp) >= config.MinLiteralLen {
					hasGoodPrefixLiterals = true
				}
			}

			// Only use ReverseSuffix if NO good prefix literals (i.e., prefix is wildcard)
			if !hasGoodPrefixLiterals {
				// Extract suffix literals
				extractor := literal.New(literal.ExtractorConfig{
					MaxLiterals:   config.MaxLiterals,
					MaxLiteralLen: 64,
					MaxClassSize:  10,
				})
				suffixLiterals := extractor.ExtractSuffixes(re)

				// Check if we have good suffix literals
				if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
					lcs := suffixLiterals.LongestCommonSuffix()
					if len(lcs) >= config.MinLiteralLen {
						// Good suffix literal available - use ReverseSuffix
						// Example: ".*\.txt" with suffix ".txt"
						return UseReverseSuffix
					}
				}
			}
		}
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

	default:
		return "unknown strategy"
	}
}
