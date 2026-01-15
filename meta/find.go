// Package meta implements the meta-engine orchestrator.
//
// find.go contains Find methods that return *Match objects.

package meta

import (
	"sync/atomic"

	"github.com/coregx/coregex/prefilter"
	"github.com/coregx/coregex/simd"
)

// Find returns the first match in the haystack, or nil if no match.
//
// The search algorithm depends on the selected strategy:
//
//	UseNFA:   PikeVM search directly
//	UseDFA:   Prefilter (if available) → DFA → NFA fallback
//	UseBoth:  Try DFA, fallback to NFA on cache full
//
// Example:
//
//	engine, _ := meta.Compile("hello")
//	match := engine.Find([]byte("say hello world"))
//	if match != nil {
//	    println(match.String()) // "hello"
//	}
func (e *Engine) Find(haystack []byte) *Match {
	return e.FindAt(haystack, 0)
}

// FindAt finds the first match starting from position 'at' in the haystack.
// Returns nil if no match is found.
//
// This method is used by FindAll* operations to correctly handle anchors like ^.
// Unlike Find, it takes the FULL haystack and a starting position, so assertions
// like ^ correctly check against the original input start, not a sliced position.
//
// Example:
//
//	engine, _ := meta.Compile("^test")
//	match := engine.FindAt([]byte("hello test"), 0)  // matches at 0
//	match := engine.FindAt([]byte("hello test"), 6)  // no match (^ won't match at pos 6)
func (e *Engine) FindAt(haystack []byte, at int) *Match {
	if at > len(haystack) {
		return nil
	}

	// Early impossibility check: anchored pattern can only match at position 0
	if at > 0 && e.nfa.IsAlwaysAnchored() {
		return nil
	}

	// For position 0, use the optimized strategy-specific paths
	if at == 0 {
		return e.findAtZero(haystack)
	}

	// For non-zero positions, use FindAt variants that preserve absolute positions
	return e.findAtNonZero(haystack, at)
}

// findAtZero dispatches to the appropriate strategy for position 0.
// This is a helper function to reduce cyclomatic complexity in FindAt.
func (e *Engine) findAtZero(haystack []byte) *Match {
	switch e.strategy {
	case UseNFA:
		return e.findNFA(haystack)
	case UseDFA:
		return e.findDFA(haystack)
	case UseBoth:
		return e.findAdaptive(haystack)
	case UseReverseAnchored:
		return e.findReverseAnchored(haystack)
	case UseReverseSuffix:
		return e.findReverseSuffix(haystack)
	case UseReverseSuffixSet:
		return e.findReverseSuffixSet(haystack)
	case UseReverseInner:
		return e.findReverseInner(haystack)
	case UseMultilineReverseSuffix:
		return e.findMultilineReverseSuffix(haystack)
	case UseBoundedBacktracker:
		return e.findBoundedBacktracker(haystack)
	case UseCharClassSearcher:
		return e.findCharClassSearcher(haystack)
	case UseCompositeSearcher:
		return e.findCompositeSearcher(haystack)
	case UseBranchDispatch:
		return e.findBranchDispatch(haystack)
	case UseTeddy:
		return e.findTeddy(haystack)
	case UseDigitPrefilter:
		return e.findDigitPrefilter(haystack)
	case UseAhoCorasick:
		return e.findAhoCorasick(haystack)
	case UseAnchoredLiteral:
		return e.findAnchoredLiteral(haystack)
	default:
		return e.findNFA(haystack)
	}
}

// findAnchoredLiteral uses O(1) specialized matching for ^prefix.*suffix$ patterns.
// Returns a Match spanning the entire input if it matches (since pattern is fully anchored).
func (e *Engine) findAnchoredLiteral(haystack []byte) *Match {
	if MatchAnchoredLiteral(haystack, e.anchoredLiteralInfo) {
		return &Match{
			start:    0,
			end:      len(haystack),
			haystack: haystack,
		}
	}
	return nil
}

// findAtNonZero dispatches to the appropriate strategy for non-zero positions.
// This is a helper function to reduce cyclomatic complexity in FindAt.
func (e *Engine) findAtNonZero(haystack []byte, at int) *Match {
	switch e.strategy {
	case UseNFA:
		return e.findNFAAt(haystack, at)
	case UseDFA:
		return e.findDFAAt(haystack, at)
	case UseBoth:
		return e.findAdaptiveAt(haystack, at)
	case UseReverseAnchored, UseReverseSuffix, UseReverseInner:
		// Reverse strategies should work correctly with slicing
		// since they operate on specific ranges
		return e.findNFAAt(haystack, at)
	case UseMultilineReverseSuffix:
		return e.findMultilineReverseSuffixAt(haystack, at)
	case UseBoundedBacktracker:
		return e.findBoundedBacktrackerAt(haystack, at)
	case UseCharClassSearcher:
		return e.findCharClassSearcherAt(haystack, at)
	case UseCompositeSearcher:
		return e.findCompositeSearcherAt(haystack, at)
	case UseBranchDispatch:
		return e.findBranchDispatchAt(haystack, at)
	case UseTeddy:
		return e.findTeddyAt(haystack, at)
	case UseDigitPrefilter:
		return e.findDigitPrefilterAt(haystack, at)
	case UseAhoCorasick:
		return e.findAhoCorasickAt(haystack, at)
	case UseAnchoredLiteral:
		// Start-anchored patterns can only match at position 0
		// This case should not be reached due to early check in FindAt
		return nil
	default:
		return e.findNFAAt(haystack, at)
	}
}

// findNFA searches using NFA (PikeVM) directly.
// Thread-safe: uses pooled PikeVM instance.
func (e *Engine) findNFA(haystack []byte) *Match {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	state := e.getSearchState()
	defer e.putSearchState(state)

	start, end, matched := state.pikevm.Search(haystack)
	if !matched {
		return nil
	}

	return NewMatch(start, end, haystack)
}

// findDFA searches using DFA with prefilter and NFA fallback.
func (e *Engine) findDFA(haystack []byte) *Match {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// If prefilter available, use it to find candidate positions quickly
	if e.prefilter != nil {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return nil
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)

		// Literal fast path: if prefilter is complete and we know literal length
		if e.prefilter.IsComplete() {
			literalLen := e.prefilter.LiteralLen()
			if literalLen > 0 {
				// Direct return without PikeVM - prefilter found exact match
				return NewMatch(pos, pos+literalLen, haystack)
			}
		}

		// Use anchored search from prefilter position - O(m) not O(n)!
		// This is much faster than searching the entire haystack
		start, end, matched := e.pikevm.SearchAt(haystack, pos)
		if !matched {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Use DFA search
	endPos := e.dfa.Find(haystack)
	if endPos == -1 {
		return nil
	}

	// DFA found match ending at endPos - use reverse search to find start
	// This is O(m) where m = match length, not O(n)
	// For patterns without prefilter, estimate start position
	// and search from there
	estimatedStart := 0
	if endPos > 100 {
		// For long haystacks, start search closer to the match end
		estimatedStart = endPos - 100
	}
	start, end, matched := e.pikevm.SearchAt(haystack, estimatedStart)
	if !matched {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findAdaptive tries prefilter+DFA first, falls back to NFA on failure.
func (e *Engine) findAdaptive(haystack []byte) *Match {
	// Use prefilter if available for fast candidate finding
	if e.prefilter != nil && e.dfa != nil {
		// Check if prefilter can return match bounds directly (e.g., Teddy)
		if mf, ok := e.prefilter.(prefilter.MatchFinder); ok {
			start, end := mf.FindMatch(haystack, 0)
			if start == -1 {
				return nil
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)
			atomic.AddUint64(&e.stats.DFASearches, 1)
			return NewMatch(start, end, haystack)
		}

		// Standard prefilter path
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			// No candidate found - definitely no match
			return nil
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		atomic.AddUint64(&e.stats.DFASearches, 1)

		// Literal fast path: if prefilter is complete and we know literal length
		if e.prefilter.IsComplete() {
			literalLen := e.prefilter.LiteralLen()
			if literalLen > 0 {
				// Direct return without PikeVM - prefilter found exact match
				return NewMatch(pos, pos+literalLen, haystack)
			}
		}

		// Use anchored search from prefilter position - O(m) not O(n)!
		start, end, matched := e.pikevm.SearchAt(haystack, pos)
		if !matched {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Try DFA without prefilter
	if e.dfa != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		endPos := e.dfa.Find(haystack)
		if endPos != -1 {
			// DFA succeeded - get exact match bounds from NFA
			// Use estimated start position for O(m) search instead of O(n)
			estimatedStart := 0
			if endPos > 100 {
				estimatedStart = endPos - 100
			}
			start, end, matched := e.pikevm.SearchAt(haystack, estimatedStart)
			if !matched {
				return nil
			}
			return NewMatch(start, end, haystack)
		}
		// DFA failed (might be cache full) - check cache stats
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 { // 90% full
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
		}
	}

	// Fall back to NFA
	return e.findNFA(haystack)
}

// findNFAAt searches using NFA starting from a specific position.
// This preserves absolute positions for correct anchor handling.
func (e *Engine) findNFAAt(haystack []byte, at int) *Match {
	atomic.AddUint64(&e.stats.NFASearches, 1)
	start, end, matched := e.pikevm.SearchAt(haystack, at)
	if !matched {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findDFAAt searches using DFA starting from a specific position.
// This preserves absolute positions for correct anchor handling.
func (e *Engine) findDFAAt(haystack []byte, at int) *Match {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// If prefilter available and complete, use literal fast path
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return nil
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		// Literal fast path: prefilter already found exact match
		// Use LiteralLen() to calculate end position directly
		literalLen := e.prefilter.LiteralLen()
		if literalLen > 0 {
			// Direct return without PikeVM
			return NewMatch(pos, pos+literalLen, haystack)
		}
		// Fallback to NFA if LiteralLen not available (e.g., Teddy multi-pattern)
		start, end, matched := e.pikevm.SearchAt(haystack, at)
		if !matched {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Use DFA search with FindAt
	pos := e.dfa.FindAt(haystack, at)
	if pos == -1 {
		return nil
	}

	// DFA returns end position, but doesn't track start position
	// Fall back to NFA to get exact match bounds
	start, end, matched := e.pikevm.SearchAt(haystack, at)
	if !matched {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findAdaptiveAt tries DFA first at a specific position, falls back to NFA on failure.
func (e *Engine) findAdaptiveAt(haystack []byte, at int) *Match {
	// Try DFA first
	if e.dfa != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		pos := e.dfa.FindAt(haystack, at)
		if pos != -1 {
			// DFA succeeded - need to find start position from NFA
			start, end, matched := e.pikevm.SearchAt(haystack, at)
			if matched {
				return NewMatch(start, end, haystack)
			}
		}
		// DFA failed (might be cache full) - check cache stats
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 { // 90% full
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
		}
	}

	// Fall back to NFA
	return e.findNFAAt(haystack, at)
}

// findReverseAnchored searches using reverse DFA for end-anchored patterns.
func (e *Engine) findReverseAnchored(haystack []byte) *Match {
	if e.reverseSearcher == nil {
		// Fallback to NFA if reverse searcher not available
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSearcher.Find(haystack)
}

// findReverseSuffix searches using suffix literal prefilter + reverse DFA.
func (e *Engine) findReverseSuffix(haystack []byte) *Match {
	if e.reverseSuffixSearcher == nil {
		// Fallback to NFA if reverse suffix searcher not available
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSearcher.Find(haystack)
}

// findReverseSuffixSet searches using Teddy multi-suffix prefilter + reverse DFA.
func (e *Engine) findReverseSuffixSet(haystack []byte) *Match {
	if e.reverseSuffixSetSearcher == nil {
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSetSearcher.Find(haystack)
}

// findReverseInner searches using inner literal prefilter + bidirectional DFA.
func (e *Engine) findReverseInner(haystack []byte) *Match {
	if e.reverseInnerSearcher == nil {
		// Fallback to NFA if reverse inner searcher not available
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseInnerSearcher.Find(haystack)
}

// findMultilineReverseSuffix searches using line-aware suffix prefilter + reverse DFA.
// This handles multiline patterns like (?m)^/.*\.php where ^ matches at line starts.
func (e *Engine) findMultilineReverseSuffix(haystack []byte) *Match {
	if e.multilineReverseSuffixSearcher == nil {
		// Fallback to NFA if multiline reverse suffix searcher not available
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.multilineReverseSuffixSearcher.Find(haystack)
}

// findMultilineReverseSuffixAt searches using line-aware suffix prefilter at position.
func (e *Engine) findMultilineReverseSuffixAt(haystack []byte, at int) *Match {
	if e.multilineReverseSuffixSearcher == nil {
		return e.findNFAAt(haystack, at)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.multilineReverseSuffixSearcher.FindAt(haystack, at)
}

// findBoundedBacktracker searches using bounded backtracker.
// Thread-safe: uses pooled state.
//
// V11-002 ASCII optimization: When pattern contains '.' and input is ASCII-only,
// uses the faster ASCII NFA.
func (e *Engine) findBoundedBacktracker(haystack []byte) *Match {
	if e.boundedBacktracker == nil {
		return e.findNFA(haystack)
	}

	// O(1) early rejection for anchored patterns using first-byte prefilter.
	// For ^(\d+|UUID|hex32), quickly reject inputs not starting with valid byte.
	if e.anchoredFirstBytes != nil && len(haystack) > 0 {
		if !e.anchoredFirstBytes.Contains(haystack[0]) {
			return nil
		}
	}

	atomic.AddUint64(&e.stats.NFASearches, 1)

	// V11-002 ASCII optimization
	if e.asciiBoundedBacktracker != nil && simd.IsASCII(haystack) {
		if !e.asciiBoundedBacktracker.CanHandle(len(haystack)) {
			return e.findNFA(haystack)
		}
		start, end, found := e.asciiBoundedBacktracker.Search(haystack)
		if !found {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.findNFA(haystack)
	}

	state := e.getSearchState()
	defer e.putSearchState(state)
	start, end, found := e.boundedBacktracker.SearchWithState(haystack, state.backtracker)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findBoundedBacktrackerAt searches using bounded backtracker at position.
func (e *Engine) findBoundedBacktrackerAt(haystack []byte, at int) *Match {
	// For now, fall back to NFA for non-zero positions
	return e.findNFAAt(haystack, at)
}

// findCharClassSearcher searches using specialized char_class+ searcher.
// 14-17x faster than BoundedBacktracker for simple char_class+ patterns.
func (e *Engine) findCharClassSearcher(haystack []byte) *Match {
	if e.charClassSearcher == nil {
		return e.findNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1) // Count as NFA-family for stats
	start, end, found := e.charClassSearcher.Search(haystack)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findCharClassSearcherAt searches using specialized char_class+ searcher at position.
func (e *Engine) findCharClassSearcherAt(haystack []byte, at int) *Match {
	if e.charClassSearcher == nil {
		return e.findNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	start, end, found := e.charClassSearcher.SearchAt(haystack, at)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findCompositeSearcher searches using CompositeSearcher for concatenated char classes.
func (e *Engine) findCompositeSearcher(haystack []byte) *Match {
	if e.compositeSearcher == nil {
		return e.findNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1) // Count as NFA-family for stats
	start, end, found := e.compositeSearcher.Search(haystack)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findCompositeSearcherAt searches using CompositeSearcher at position.
func (e *Engine) findCompositeSearcherAt(haystack []byte, at int) *Match {
	if e.compositeSearcher == nil {
		return e.findNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	start, end, found := e.compositeSearcher.SearchAt(haystack, at)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findBranchDispatch searches using O(1) branch dispatch for anchored alternations.
// 2-3x faster than BoundedBacktracker on match, 10x+ on no-match.
func (e *Engine) findBranchDispatch(haystack []byte) *Match {
	if e.branchDispatcher == nil {
		return e.findBoundedBacktracker(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	start, end, found := e.branchDispatcher.Search(haystack)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findBranchDispatchAt searches using branch dispatch at position.
// For anchored patterns, only position 0 is meaningful.
func (e *Engine) findBranchDispatchAt(haystack []byte, at int) *Match {
	if at != 0 {
		// Anchored pattern can only match at position 0
		return nil
	}
	return e.findBranchDispatch(haystack)
}

// fatTeddySmallHaystackThreshold is the minimum haystack size for Fat Teddy efficiency.
// For smaller haystacks, Aho-Corasick is faster due to lower setup overhead.
// Benchmarks show ~2x speedup with Aho-Corasick on 37-byte haystacks with 50 patterns.
// Reference: rust-aho-corasick/src/packed/teddy/builder.rs (minimum_len fallback)
const fatTeddySmallHaystackThreshold = 64

// findTeddy searches using Teddy multi-pattern prefilter directly.
// This is the "literal engine bypass" - for exact literal alternations like (foo|bar|baz),
// Teddy.Find() returns complete matches without needing DFA/NFA verification.
func (e *Engine) findTeddy(haystack []byte) *Match {
	if e.prefilter == nil {
		return e.findNFA(haystack)
	}

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	// Fat Teddy's AVX2 SIMD setup overhead exceeds benefit on small inputs.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		match := e.fatTeddyFallback.Find(haystack, 0)
		if match == nil {
			return nil
		}
		return NewMatch(match.Start, match.End, haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)

	// Use FindMatch which returns both start and end positions
	if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
		start, end := matcher.FindMatch(haystack, 0)
		if start == -1 {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Fallback: use Find + LiteralLen
	pos := e.prefilter.Find(haystack, 0)
	if pos == -1 {
		return nil
	}
	literalLen := e.prefilter.LiteralLen()
	if literalLen > 0 {
		return NewMatch(pos, pos+literalLen, haystack)
	}
	// If no uniform length, fall back to NFA for verification
	return e.findNFAAt(haystack, pos)
}

// findTeddyAt searches using Teddy at a specific position.
func (e *Engine) findTeddyAt(haystack []byte, at int) *Match {
	if e.prefilter == nil || at >= len(haystack) {
		return e.findNFAAt(haystack, at)
	}

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		match := e.fatTeddyFallback.FindAt(haystack, at)
		if match == nil {
			return nil
		}
		return NewMatch(match.Start, match.End, haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)

	// Use FindMatch which returns both start and end positions
	if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
		start, end := matcher.FindMatch(haystack, at)
		if start == -1 {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Fallback: use Find + LiteralLen
	pos := e.prefilter.Find(haystack, at)
	if pos == -1 {
		return nil
	}
	literalLen := e.prefilter.LiteralLen()
	if literalLen > 0 {
		return NewMatch(pos, pos+literalLen, haystack)
	}
	return e.findNFAAt(haystack, pos)
}

// findDigitPrefilter searches using SIMD digit scanning + DFA verification.
// Used for simple digit-lead patterns where literal extraction fails
// but all alternation branches must start with a digit.
//
// Note: Complex digit-lead patterns (like IP addresses with 74 NFA states) are
// handled by UseBoth/UseDFA strategies instead. See digitPrefilterMaxNFAStates.
//
// Algorithm:
//  1. Use SIMD to find next digit position in haystack
//  2. Verify match at digit position using lazy DFA + PikeVM
//  3. If no match, continue from digit position + 1
//
// Performance:
//   - Skips non-digit regions with SIMD (15-20x faster for sparse data)
//   - Total: O(n) for scan + O(k*m) for k digit candidates
func (e *Engine) findDigitPrefilter(haystack []byte) *Match {
	if e.digitPrefilter == nil {
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := 0

	for pos < len(haystack) {
		// Use SIMD to find next digit position
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return nil // No more digits, no match possible
		}

		// Verify match at digit position using DFA
		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			endPos := e.dfa.FindAt(haystack, digitPos)
			if endPos != -1 {
				// DFA found potential match - get exact bounds from NFA
				start, end, found := e.pikevm.SearchAt(haystack, digitPos)
				if found {
					return NewMatch(start, end, haystack)
				}
			}
		} else {
			// No DFA - use PikeVM directly
			atomic.AddUint64(&e.stats.NFASearches, 1)
			start, end, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return NewMatch(start, end, haystack)
			}
		}

		// No match at this digit position, continue searching
		pos = digitPos + 1
	}

	return nil
}

// findDigitPrefilterAt searches using digit prefilter starting at position 'at'.
func (e *Engine) findDigitPrefilterAt(haystack []byte, at int) *Match {
	if e.digitPrefilter == nil || at >= len(haystack) {
		return e.findNFAAt(haystack, at)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := at

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return nil
		}

		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			endPos := e.dfa.FindAt(haystack, digitPos)
			if endPos != -1 {
				start, end, found := e.pikevm.SearchAt(haystack, digitPos)
				if found {
					return NewMatch(start, end, haystack)
				}
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
			start, end, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return NewMatch(start, end, haystack)
			}
		}

		pos = digitPos + 1
	}

	return nil
}

// findAhoCorasick searches using Aho-Corasick automaton for large literal alternations.
// This is the "literal engine bypass" for patterns with >32 literals.
// The automaton performs O(n) multi-pattern matching with ~1.6 GB/s throughput.
func (e *Engine) findAhoCorasick(haystack []byte) *Match {
	if e.ahoCorasick == nil {
		return e.findNFA(haystack)
	}
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)

	m := e.ahoCorasick.Find(haystack, 0)
	if m == nil {
		return nil
	}
	return NewMatch(m.Start, m.End, haystack)
}

// findAhoCorasickAt searches using Aho-Corasick starting at position 'at'.
func (e *Engine) findAhoCorasickAt(haystack []byte, at int) *Match {
	if e.ahoCorasick == nil || at >= len(haystack) {
		return e.findNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)

	m := e.ahoCorasick.Find(haystack, at)
	if m == nil {
		return nil
	}
	return NewMatch(m.Start, m.End, haystack)
}
