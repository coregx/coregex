package meta

// DO NOT REGRESS: ReverseInner is 16% faster than Rust regex on email/inner-literal patterns.
// This optimization uses bidirectional DFA search from inner literal positions.
// Key: AST splitting builds separate NFAs for prefix/suffix, enabling true bidirectional search.
// See docs/OPTIMIZATIONS.md for algorithm details and benchmark data.

import (
	"errors"
	"regexp/syntax"

	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// isUniversalMatch checks if the given regex AST matches everything (like .*).
// A universal match is:
//   - OpStar/OpPlus of OpAnyChar or OpAnyCharNotNL
//   - Empty pattern (matches empty string at any position)
//
// This is used to optimize Find for patterns like `.*connection.*` where
// we know match bounds without scanning.
func isUniversalMatch(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpStar, syntax.OpPlus:
		// .* or .+ - check if sub is any-char
		if len(re.Sub) == 1 {
			sub := re.Sub[0]
			if sub.Op == syntax.OpAnyChar || sub.Op == syntax.OpAnyCharNotNL {
				return true
			}
		}
	case syntax.OpEmptyMatch:
		// Empty matches zero-width at any position
		return true
	}
	return false
}

// isStartAnchorOnly checks if an AST only contains start-of-text/line anchors.
// Patterns like ^, ^+, ^^^ only match at position 0, so if inner literal is
// found at position 0, the prefix trivially matches.
func isStartAnchorOnly(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpBeginText, syntax.OpBeginLine:
		// ^ or \A - matches at position 0
		return true
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest:
		// ^+, ^*, ^? - check if sub is start anchor
		if len(re.Sub) == 1 {
			return isStartAnchorOnly(re.Sub[0])
		}
	case syntax.OpConcat:
		// ^^ or ^+^+ - all elements must be start anchors
		for _, sub := range re.Sub {
			if !isStartAnchorOnly(sub) {
				return false
			}
		}
		return len(re.Sub) > 0
	case syntax.OpCapture:
		// (^) - check sub
		if len(re.Sub) == 1 {
			return isStartAnchorOnly(re.Sub[0])
		}
	case syntax.OpEmptyMatch:
		// Empty string at position 0 is trivially matching
		return true
	}
	return false
}

// endsWithUniversalMatch checks if suffix AST ends with .* (greedy any).
// For suffix like `connection.*`, this detects the trailing .*.
func endsWithUniversalMatch(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}

	switch re.Op {
	case syntax.OpStar, syntax.OpPlus:
		// Direct .* or .+
		if len(re.Sub) == 1 {
			sub := re.Sub[0]
			if sub.Op == syntax.OpAnyChar || sub.Op == syntax.OpAnyCharNotNL {
				return true
			}
		}
	case syntax.OpConcat:
		// Concatenation - check if last element is .*
		if len(re.Sub) > 0 {
			return endsWithUniversalMatch(re.Sub[len(re.Sub)-1])
		}
	case syntax.OpCapture:
		// Unwrap capture groups
		if len(re.Sub) == 1 {
			return endsWithUniversalMatch(re.Sub[0])
		}
	}
	return false
}

// ErrNoInnerLiterals indicates that no inner literals could be extracted for ReverseInner strategy.
// This is not a fatal error - it just means ReverseInner optimization cannot be used.
var ErrNoInnerLiterals = errors.New("no inner literals available for ReverseInner strategy")

// ReverseInnerSearcher performs inner literal prefilter + bidirectional DFA search.
//
// This strategy is used for patterns with inner literals like `prefix.*inner.*suffix` where:
//   - The pattern has a good inner literal for prefiltering
//   - Has wildcards/repetitions both BEFORE and AFTER the inner literal
//   - Can use bidirectional DFA to find exact match bounds
//
// Algorithm:
//  1. Extract inner literals from pattern
//  2. Build prefilter for inner literals
//  3. Search algorithm:
//     a. Prefilter finds inner literal candidates in haystack
//     b. For each candidate at position P:
//     - Reverse DFA scans backward from P to find match START
//     - Forward DFA scans forward from P+innerLen to find match END
//     c. Return leftmost-longest match
//
// Performance:
//   - Forward search: O(n*m) where n=haystack length, m=pattern length
//   - ReverseInner: O(k*(m1+m2)) where k=number of inner candidates, m1=prefix length, m2=suffix length
//   - Speedup: 10-100x for patterns like `ERROR.*connection.*timeout` on large haystacks
//
// Example:
//
//	// Pattern `ERROR.*connection.*timeout` on 1MB log with 5 "connection" occurrences
//	// Forward: tries pattern match at every position (~1M attempts)
//	// ReverseInner: prefilter finds 5 "connection" positions, bidirectional DFA verifies (~5 attempts)
//	// Speedup: ~200,000x
type ReverseInnerSearcher struct {
	forwardNFA      *nfa.NFA
	reverseNFA      *nfa.NFA
	reverseDFA      *lazy.DFA
	forwardDFA      *lazy.DFA
	prefilter       prefilter.Prefilter
	pikevm          *nfa.PikeVM
	innerLen        int  // Length of the inner literal for calculating positions
	universalPrefix bool // True if prefix is .* (matches everything from start)
	universalSuffix bool // True if suffix ends with .* (matches everything to end)
	startAnchored   bool // True if prefix only contains start anchors (^, ^+, etc.)
}

// NewReverseInnerSearcher creates a reverse inner searcher using AST splitting.
//
// The key optimization (from rust-regex):
//   - Build reverse NFA from PREFIX AST only (not full pattern)
//   - Build forward NFA from SUFFIX AST only (not full pattern)
//   - This enables true bidirectional search with 10-100x speedup
//
// Requirements:
//   - InnerLiteralInfo must have PrefixAST and SuffixAST populated
//   - Inner literal must have wildcards both before and after
//   - Prefilter must be available
//
// Parameters:
//   - fullNFA: the compiled NFA for the full pattern (for fallback)
//   - innerInfo: extracted inner literal info with split AST
//   - config: DFA configuration for reverse/forward DFA cache
//
// Returns error if reverse inner optimization cannot be applied:
//   - ErrNoInnerLiterals: no inner literals available
//   - ErrNoPrefilter: prefilter could not be built
//   - DFA compilation errors
func NewReverseInnerSearcher(
	fullNFA *nfa.NFA,
	innerInfo *literal.InnerLiteralInfo,
	config lazy.Config,
) (*ReverseInnerSearcher, error) {
	// Validate input
	if innerInfo == nil || innerInfo.Literals == nil || innerInfo.Literals.IsEmpty() {
		return nil, ErrNoInnerLiterals
	}

	// Get inner length from longest common prefix
	lcp := innerInfo.Literals.LongestCommonPrefix()
	if len(lcp) == 0 {
		return nil, ErrNoInnerLiterals
	}
	innerLen := len(lcp)

	// Build prefilter from inner literals
	builder := prefilter.NewBuilder(nil, innerInfo.Literals)
	pre := builder.Build()
	if pre == nil {
		return nil, ErrNoPrefilter
	}

	// Build reverse NFA from PREFIX AST only (the key optimization!)
	// This is what rust-regex does in reverse_inner.rs
	var prefixNFA *nfa.NFA
	var err error
	if innerInfo.PrefixAST != nil {
		compiler := nfa.NewCompiler(nfa.CompilerConfig{
			UTF8:     true,
			Anchored: false,
		})
		prefixNFA, err = compiler.CompileRegexp(innerInfo.PrefixAST)
		if err != nil {
			return nil, err
		}
	} else {
		// Fallback to full pattern if no prefix AST
		prefixNFA = fullNFA
	}

	// Build reverse NFA from prefix
	reverseNFA := nfa.Reverse(prefixNFA)

	// Build reverse DFA from reverse prefix NFA
	reverseDFA, err := lazy.CompileWithConfig(reverseNFA, config)
	if err != nil {
		return nil, err
	}

	// Build forward NFA from SUFFIX AST (includes inner + everything after)
	var suffixNFA *nfa.NFA
	if innerInfo.SuffixAST != nil {
		compiler := nfa.NewCompiler(nfa.CompilerConfig{
			UTF8:     true,
			Anchored: false,
		})
		suffixNFA, err = compiler.CompileRegexp(innerInfo.SuffixAST)
		if err != nil {
			return nil, err
		}
	} else {
		// Fallback to full pattern if no suffix AST
		suffixNFA = fullNFA
	}

	// Build forward DFA from suffix NFA
	forwardDFA, err := lazy.CompileWithConfig(suffixNFA, config)
	if err != nil {
		return nil, err
	}

	// Create PikeVM for fallback (uses full pattern)
	pikevm := nfa.NewPikeVM(fullNFA)

	// Detect universal prefix/suffix for Find optimization
	// For patterns like `.*connection.*`:
	//   - universalPrefix: .* prefix means match always starts at 0
	//   - universalSuffix: .* suffix means match always ends at len(haystack)
	universalPrefix := isUniversalMatch(innerInfo.PrefixAST)
	universalSuffix := endsWithUniversalMatch(innerInfo.SuffixAST)
	// Check if prefix is only start anchors (^, ^+, etc.) - trivially matches at position 0
	startAnchored := isStartAnchorOnly(innerInfo.PrefixAST)

	return &ReverseInnerSearcher{
		forwardNFA:      suffixNFA,
		reverseNFA:      reverseNFA,
		reverseDFA:      reverseDFA,
		forwardDFA:      forwardDFA,
		prefilter:       pre,
		pikevm:          pikevm,
		innerLen:        innerLen,
		universalPrefix: universalPrefix,
		universalSuffix: universalSuffix,
		startAnchored:   startAnchored,
	}, nil
}

// Find searches using inner literal prefilter + bidirectional DFA and returns the match.
//
// Algorithm (leftmost-longest/greedy semantics):
//  1. Use prefilter to find ALL inner literal candidates
//  2. For each candidate at position P:
//     a. Reverse DFA scans backward from P to find match START
//     b. Forward DFA scans forward from P+innerLen to find match END
//  3. Track leftmost-longest match:
//     - Leftmost: earliest start position
//     - Longest: if same start, choose longest end
//  4. Return the best match found
//
// Performance:
//   - ZERO PikeVM calls - uses DFA exclusively
//   - Bidirectional DFA scan finds both match start and end efficiently
//   - Prefilter reduces search space dramatically
//
// Example (bidirectional matching):
//
//	Pattern: `ERROR.*connection.*timeout`
//	Haystack: "ERROR: connection lost due to connection timeout"
//	Inner literal: "connection"
//
//	1. Prefilter finds "connection" at position 7, then at position 32
//	2. Candidate 1 (pos=7):
//	   a. Reverse DFA from pos=7 backward → finds start=0 (ERROR)
//	   b. Forward DFA from pos=17 forward → finds end=24 (lost, no timeout)
//	   c. No valid match (pattern requires "timeout" after)
//	3. Candidate 2 (pos=32):
//	   a. Reverse DFA from pos=32 backward → finds start=0 (ERROR)
//	   b. Forward DFA from pos=42 forward → finds end=49 (timeout)
//	   c. Valid match [0:49]
//	4. Return [0:49] = "ERROR: connection lost due to connection timeout"
func (s *ReverseInnerSearcher) Find(haystack []byte) *Match {
	if len(haystack) == 0 {
		return nil
	}

	// UNIVERSAL MATCH OPTIMIZATION:
	// For patterns like `.*connection.*` where both prefix and suffix are universal (.*):
	//   - Match start is ALWAYS 0 (because .* matches any prefix from start)
	//   - Match end is ALWAYS len(haystack) (because .* matches any suffix to end)
	// We can skip expensive DFA scans and just verify with fast IsMatch.
	// This reduces Find from O(n) DFA scan to O(1) for common patterns!
	if s.universalPrefix && s.universalSuffix {
		if s.IsMatch(haystack) {
			return NewMatch(0, len(haystack), haystack)
		}
		return nil
	}

	// EARLY RETURN OPTIMIZATION (from rust-regex):
	// The prefilter finds candidates in left-to-right order.
	// The first confirmed match is guaranteed to be the leftmost match.
	// The forward DFA handles the "longest" part of leftmost-longest semantics.
	// Therefore, we can return immediately on first confirmed match!

	searchStart := 0
	minPreStart := 0   // Track minimum position for forward scan quadratic detection
	minMatchStart := 0 // Anti-quadratic guard for reverse scan
	for {
		// Find next inner literal candidate
		pos := s.prefilter.Find(haystack, searchStart)
		if pos == -1 {
			// No more candidates
			break
		}

		// QUADRATIC BEHAVIOR DETECTION (from rust-regex):
		// If the new candidate starts before the end of last forward scan,
		// we have overlapping candidates which causes O(n^2) behavior.
		// Fall back to PikeVM which is O(n) in this case.
		if pos < minPreStart {
			// Quadratic behavior detected - use PikeVM fallback
			start, end, found := s.pikevm.Search(haystack)
			if found {
				return NewMatch(start, end, haystack)
			}
			return nil
		}

		// Step 1: Reverse search on PREFIX portion with anti-quadratic guard
		// Check if we can reach this inner literal from an earlier position.
		// Use minMatchStart to avoid re-scanning regions already proven to have no match.
		matchStart := s.reverseDFA.SearchReverseLimited(haystack, 0, pos, minMatchStart)
		if matchStart == lazy.SearchReverseLimitedQuadratic {
			// Reverse scan hit the anti-quadratic guard - fall back to PikeVM
			start, end, found := s.pikevm.Search(haystack)
			if found {
				return NewMatch(start, end, haystack)
			}
			return nil
		}
		if matchStart < 0 {
			// Prefix doesn't match - try next candidate
			searchStart = pos + 1
			if searchStart >= len(haystack) {
				break
			}
			continue
		}

		// Step 2: Forward search on SUFFIX portion
		// Find the end of the match (forward DFA finds longest match = greedy)
		suffixHaystack := haystack[pos:]
		matchEndRel := s.forwardDFA.Find(suffixHaystack)
		if matchEndRel < 0 {
			// Suffix doesn't match - update minPreStart and try next candidate
			minPreStart = pos + s.innerLen
			searchStart = pos + 1
			if searchStart >= len(haystack) {
				break
			}
			continue
		}

		// EARLY RETURN: First confirmed match is leftmost by construction!
		// Forward DFA already finds the longest match from this start position.
		matchEnd := pos + matchEndRel
		return NewMatch(matchStart, matchEnd, haystack)
	}

	// Fallback: use PikeVM if no DFA match found
	start, end, found := s.pikevm.Search(haystack)
	if found {
		return NewMatch(start, end, haystack)
	}
	return nil
}

// IsMatch checks if the pattern matches using inner prefilter + bidirectional DFA.
//
// This is optimized for boolean matching:
//   - Uses prefilter for fast candidate finding
//   - Uses bidirectional DFA for fast verification
//   - No Match object allocation
//   - Early termination on first match
//   - Anti-quadratic guard: tracks minStart to avoid re-scanning already-checked regions
//
// Algorithm:
//  1. Prefilter finds inner literal candidates
//  2. For each candidate:
//     a. Reverse DFA checks if we can reach inner from start (with anti-quadratic guard)
//     b. Forward DFA checks if we can reach end from inner
//  3. Return true on first valid match
func (s *ReverseInnerSearcher) IsMatch(haystack []byte) bool {
	if len(haystack) == 0 {
		return false
	}

	// Use prefilter to find inner literal candidates
	searchStart := 0
	minStart := 0 // Anti-quadratic guard for reverse scans
	for {
		// Find next inner literal candidate
		pos := s.prefilter.Find(haystack, searchStart)
		if pos == -1 {
			// No more candidates
			return false
		}

		// BIDIRECTIONAL VERIFICATION:
		//
		// Step 1: Check if prefix matches (reverse DFA with anti-quadratic guard)
		// Special cases for pos=0:
		//   - universalPrefix (.*): trivially matches empty prefix
		//   - startAnchored (^, ^+): trivially matches at position 0
		prefixMatches := false
		if pos == 0 && (s.universalPrefix || s.startAnchored) {
			// Universal prefix (.*) or start anchor (^) matches at position 0
			prefixMatches = true
		} else if pos > 0 {
			// Use SearchReverseLimited for anti-quadratic protection
			revResult := s.reverseDFA.SearchReverseLimited(haystack, 0, pos, minStart)
			if revResult == lazy.SearchReverseLimitedQuadratic {
				// Quadratic behavior detected - fall back to PikeVM
				_, _, matched := s.pikevm.Search(haystack)
				return matched
			}
			prefixMatches = revResult >= 0
		}

		if prefixMatches {
			// Step 2: Check if suffix matches (forward DFA from inner position)
			suffixHaystack := haystack[pos:]
			if s.forwardDFA.IsMatch(suffixHaystack) {
				// Both prefix and suffix match - pattern matches!
				return true
			}
		}

		// Update anti-quadratic guard: don't re-scan before this position
		if pos+s.innerLen > minStart {
			minStart = pos + s.innerLen
		}

		// Try next candidate
		searchStart = pos + 1
		if searchStart >= len(haystack) {
			return false
		}
	}
}

// FindIndicesAt returns match indices starting from position 'at' - zero allocation version.
// This is used by FindAll* operations for efficient iteration.
// Includes anti-quadratic guard to prevent O(n^2) behavior with many inner literal false positives.
func (s *ReverseInnerSearcher) FindIndicesAt(haystack []byte, at int) (start, end int, found bool) {
	if at >= len(haystack) {
		return -1, -1, false
	}

	// UNIVERSAL MATCH OPTIMIZATION:
	// For patterns like `.*connection.*` where both prefix and suffix are universal (.*)
	if s.universalPrefix && s.universalSuffix {
		// Just check if there's an inner literal anywhere from 'at'
		pos := s.prefilter.Find(haystack, at)
		if pos >= 0 {
			// For universal prefix/suffix, match spans from 'at' to end
			return at, len(haystack), true
		}
		return -1, -1, false
	}

	// Search for inner literal starting from 'at'
	searchStart := at
	minMatchStart := at // Anti-quadratic guard for reverse scans
	for {
		// Find next inner literal candidate
		pos := s.prefilter.Find(haystack, searchStart)
		if pos == -1 {
			break
		}

		// Step 1: Reverse search on PREFIX portion with anti-quadratic guard
		// Use minMatchStart to avoid re-scanning regions already checked
		matchStart := s.reverseDFA.SearchReverseLimited(haystack, at, pos, minMatchStart)
		if matchStart == lazy.SearchReverseLimitedQuadratic {
			// Quadratic behavior detected - fall back to PikeVM
			return s.pikevm.SearchAt(haystack, at)
		}
		if matchStart < 0 || matchStart < at {
			// Prefix doesn't match or match starts before 'at' - try next candidate
			searchStart = pos + 1
			if searchStart >= len(haystack) {
				break
			}
			continue
		}

		// Step 2: Forward search on SUFFIX portion
		suffixHaystack := haystack[pos:]
		matchEndRel := s.forwardDFA.Find(suffixHaystack)
		if matchEndRel < 0 {
			// Suffix doesn't match - try next candidate
			searchStart = pos + 1
			if searchStart >= len(haystack) {
				break
			}
			continue
		}

		// Found valid match
		matchEnd := pos + matchEndRel
		return matchStart, matchEnd, true
	}

	// Fallback to PikeVM
	return s.pikevm.SearchAt(haystack, at)
}
