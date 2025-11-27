package meta

import (
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
	default:
		return "Unknown"
	}
}

// SelectStrategy analyzes the NFA and literals to choose the best execution strategy.
//
// Algorithm:
//  1. If DFA disabled in config → UseNFA
//  2. If NFA is tiny (< 20 states) → UseNFA (DFA overhead not worth it)
//  3. If good literals exist → UseDFA (prefilter + DFA is fastest)
//  4. If NFA is large (> 100 states) → UseDFA (essential for performance)
//  5. Otherwise → UseBoth (adaptive)
//
// "Good literals" means:
//   - At least one literal exists
//   - Longest common prefix (LCP) length >= MinLiteralLen
//   - This enables effective prefiltering
//
// Parameters:
//   - nfa: the compiled NFA to analyze
//   - literals: extracted prefix literals (can be nil)
//   - config: meta-engine configuration
//
// Example:
//
//	strategy := meta.SelectStrategy(nfa, literals, config)
//	switch strategy {
//	case meta.UseNFA:
//	    // Use PikeVM only
//	case meta.UseDFA:
//	    // Use Lazy DFA
//	case meta.UseBoth:
//	    // Adaptive
//	}
func SelectStrategy(n *nfa.NFA, literals *literal.Seq, config Config) Strategy {
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

	default:
		return "unknown strategy"
	}
}
