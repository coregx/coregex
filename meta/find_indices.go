// Package meta implements the meta-engine orchestrator.
//
// find_indices.go contains FindIndices methods that return (start, end, found) tuples.
// These are zero-allocation alternatives to the Find methods.

package meta

import (
	"sync/atomic"

	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/simd"
)

// FindIndices returns the start and end indices of the first match.
// Returns (-1, -1, false) if no match is found.
//
// This is a zero-allocation alternative to Find() - it returns indices
// directly instead of creating a Match object.
func (e *Engine) FindIndices(haystack []byte) (start, end int, found bool) {
	switch e.strategy {
	case UseNFA:
		return e.findIndicesNFA(haystack)
	case UseDFA:
		return e.findIndicesDFA(haystack)
	case UseBoth:
		return e.findIndicesAdaptive(haystack)
	case UseReverseAnchored:
		return e.findIndicesReverseAnchored(haystack)
	case UseReverseSuffix:
		return e.findIndicesReverseSuffix(haystack)
	case UseReverseSuffixSet:
		return e.findIndicesReverseSuffixSet(haystack)
	case UseReverseInner:
		return e.findIndicesReverseInner(haystack)
	case UseBoundedBacktracker:
		return e.findIndicesBoundedBacktracker(haystack)
	case UseCharClassSearcher:
		return e.findIndicesCharClassSearcher(haystack)
	case UseCompositeSearcher:
		return e.findIndicesCompositeSearcher(haystack)
	case UseBranchDispatch:
		return e.findIndicesBranchDispatch(haystack)
	case UseTeddy:
		return e.findIndicesTeddy(haystack)
	case UseDigitPrefilter:
		return e.findIndicesDigitPrefilter(haystack)
	case UseAhoCorasick:
		return e.findIndicesAhoCorasick(haystack)
	case UseMultilineReverseSuffix:
		return e.findIndicesMultilineReverseSuffix(haystack)
	case UseAnchoredLiteral:
		return e.findIndicesAnchoredLiteral(haystack)
	default:
		return e.findIndicesNFA(haystack)
	}
}

// FindIndicesAt returns the start and end indices of the first match starting at position 'at'.
// Returns (-1, -1, false) if no match is found.
func (e *Engine) FindIndicesAt(haystack []byte, at int) (start, end int, found bool) {
	// Early impossibility check: anchored pattern can only match at position 0
	if at > 0 && e.nfa.IsAlwaysAnchored() {
		return -1, -1, false
	}

	switch e.strategy {
	case UseNFA:
		return e.findIndicesNFAAt(haystack, at)
	case UseDFA:
		return e.findIndicesDFAAt(haystack, at)
	case UseBoth:
		return e.findIndicesAdaptiveAt(haystack, at)
	case UseReverseSuffix:
		return e.findIndicesReverseSuffixAt(haystack, at)
	case UseReverseSuffixSet:
		return e.findIndicesReverseSuffixSetAt(haystack, at)
	case UseReverseInner:
		return e.findIndicesReverseInnerAt(haystack, at)
	case UseBoundedBacktracker:
		return e.findIndicesBoundedBacktrackerAt(haystack, at)
	case UseCharClassSearcher:
		return e.findIndicesCharClassSearcherAt(haystack, at)
	case UseCompositeSearcher:
		return e.findIndicesCompositeSearcherAt(haystack, at)
	case UseBranchDispatch:
		return e.findIndicesBranchDispatchAt(haystack, at)
	case UseTeddy:
		return e.findIndicesTeddyAt(haystack, at)
	case UseDigitPrefilter:
		return e.findIndicesDigitPrefilterAt(haystack, at)
	case UseAhoCorasick:
		return e.findIndicesAhoCorasickAt(haystack, at)
	case UseMultilineReverseSuffix:
		return e.findIndicesMultilineReverseSuffixAt(haystack, at)
	case UseAnchoredLiteral:
		return e.findIndicesAnchoredLiteralAt(haystack, at)
	default:
		return e.findIndicesNFAAt(haystack, at)
	}
}

// findIndicesNFA searches using NFA (PikeVM) directly - zero alloc.
// Uses prefilter for skip-ahead when available (like Rust regex).
//
// BoundedBacktracker can be used for patterns that cannot match empty.
// For patterns like (?:|a)*, its greedy semantics give wrong results,
// so we must use PikeVM which correctly implements leftmost-first semantics.
// Thread-safe: uses pooled state for both BoundedBacktracker and PikeVM.
func (e *Engine) findIndicesNFA(haystack []byte) (int, int, bool) {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// BoundedBacktracker can be used for Find operations only when:
	// 1. It's available
	// 2. Pattern cannot match empty (BT has greedy semantics that break empty match handling)
	useBT := e.boundedBacktracker != nil && !e.canMatchEmpty

	// Get pooled state for thread-safe execution
	state := e.getSearchState()
	defer e.putSearchState(state)

	// Use prefilter for candidate skip-ahead if available.
	// Prefilter finds PREFIX positions → NFA/BT verifies full match from there.
	// Safe for both complete and incomplete prefilters — as long as all
	// alternation branches are represented in the literal set.
	//
	// NOT safe for partial-coverage prefilters (overflow truncated branches):
	// candidate loop would miss unrepresented branches entirely.
	// Rust avoids this by integrating prefilter inside PikeVM as skip-ahead
	// (not as an external correctness gate). See pikevm.rs:1293-1299.
	if e.prefilter != nil && !e.prefilterPartialCoverage {
		at := 0
		for at < len(haystack) {
			// Find next candidate position via prefilter
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return -1, -1, false // No more candidates
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)

			// Try to match at candidate position
			var start, end int
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				start, end, found = e.boundedBacktracker.SearchAtWithState(haystack, pos, state.backtracker)
			} else {
				start, end, found = state.pikevm.SearchWithSlotTableAt(haystack, pos, nfa.SearchModeFind)
			}
			if found {
				return start, end, true
			}

			// Move past this position
			atomic.AddUint64(&e.stats.PrefilterMisses, 1)
			at = pos + 1
		}
		return -1, -1, false
	}

	// No prefilter: use BoundedBacktracker if available and safe
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.boundedBacktracker.SearchWithState(haystack, state.backtracker)
	}

	// Use optimized SlotTable-based search for large inputs
	return state.pikevm.SearchWithSlotTable(haystack, nfa.SearchModeFind)
}

// findIndicesNFAAt searches using NFA starting at position - zero alloc.
// Uses prefilter for skip-ahead when available (like Rust regex).
// Same BoundedBacktracker rules as findIndicesNFA.
// Thread-safe: uses pooled state for both BoundedBacktracker and PikeVM.
func (e *Engine) findIndicesNFAAt(haystack []byte, at int) (int, int, bool) {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// BoundedBacktracker can be used for Find operations only when safe
	useBT := e.boundedBacktracker != nil && !e.canMatchEmpty

	// Get pooled state for thread-safe execution
	state := e.getSearchState()
	defer e.putSearchState(state)

	// Use prefilter candidate loop — safe unless partial coverage (overflow)
	if e.prefilter != nil && !e.prefilterPartialCoverage {
		for at < len(haystack) {
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return -1, -1, false
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)

			var start, end int
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				start, end, found = e.boundedBacktracker.SearchAtWithState(haystack, pos, state.backtracker)
			} else {
				start, end, found = state.pikevm.SearchWithSlotTableAt(haystack, pos, nfa.SearchModeFind)
			}
			if found {
				return start, end, true
			}

			atomic.AddUint64(&e.stats.PrefilterMisses, 1)
			at = pos + 1
		}
		return -1, -1, false
	}

	// No prefilter or incomplete: use BoundedBacktracker if available and safe
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)-at) {
		return e.boundedBacktracker.SearchAtWithState(haystack, at, state.backtracker)
	}

	// Use optimized SlotTable-based search for large inputs
	return state.pikevm.SearchWithSlotTableAt(haystack, at, nfa.SearchModeFind)
}

// findIndicesDFA searches using DFA with prefilter - zero alloc.
func (e *Engine) findIndicesDFA(haystack []byte) (int, int, bool) { //nolint:cyclop // DFA with prefilter paths
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// Literal fast path — complete prefilter returns match directly
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		literalLen := e.prefilter.LiteralLen()
		if literalLen > 0 {
			return pos, pos + literalLen, true
		}
		return e.pikevm.Search(haystack)
	}

	// Prefilter skip-ahead for DFA — safe even with incomplete prefilter.
	// DFA verifies full pattern at candidate position; prefilter just skips.
	if e.prefilter != nil && !e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		if e.reverseDFA != nil {
			return e.findIndicesBidirectionalDFA(haystack, pos)
		}
		return e.pikevm.SearchAt(haystack, pos)
	}

	// Prefilter-accelerated search: find candidate, verify with anchored DFA.
	// For large NFAs (e.g., 181 states for (?i) patterns), bidirectional DFA
	// cache-thrashes. Anchored verification at candidate position is O(pattern_len).
	// Guards:
	// - reverseDFA != nil: proxy for greedy patterns (non-greedy needs PikeVM)
	// - nfaSize > 100: only for large NFAs where DFA cache-thrashes.
	//   Small NFAs (e.g., 34 states for peak_hours) work fine with bidirectional DFA.
	//   Single-byte prefilters (memchr on '[') produce too many false positives,
	//   making candidate loop slower than single-pass DFA.
	if e.prefilter != nil && e.reverseDFA != nil && e.nfaStateCount > 100 {
		// Acquire state once for the candidate loop
		state := e.getSearchState()
		defer e.putSearchState(state)
		pos := 0
		for pos < len(haystack) {
			candidate := e.prefilter.Find(haystack, pos)
			if candidate == -1 {
				return -1, -1, false
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)
			// Complete prefilter: candidate IS the match
			if e.prefilter.IsComplete() {
				litLen := e.prefilter.LiteralLen()
				if litLen > 0 {
					return candidate, candidate + litLen, true
				}
				// FindMatch for variable-length complete matches
				if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
					s, end := matcher.FindMatch(haystack, pos)
					if s >= 0 {
						return s, end, true
					}
				}
			}
			// Anchored DFA verification at candidate position
			if e.dfa != nil {
				endPos := e.dfa.SearchAtAnchored(state.dfaCache, haystack, candidate)
				if endPos != -1 {
					return candidate, endPos, true
				}
			} else {
				start, end, found := state.pikevm.SearchAt(haystack, candidate)
				if found && start == candidate {
					return start, end, true
				}
			}
			pos = candidate + 1
		}
		return -1, -1, false
	}

	// Prefilter with non-greedy: use prefilter for rejection only, PikeVM for match.
	// Not safe with partial coverage — would miss unrepresented branches.
	if e.prefilter != nil && !e.prefilterPartialCoverage {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		return e.pikevm.SearchAt(haystack, pos)
	}

	// No prefilter: bidirectional DFA or DFA + PikeVM fallback.
	if e.reverseDFA != nil {
		return e.findIndicesBidirectionalDFA(haystack, 0)
	}
	state := e.getSearchState()
	matched := e.dfa.IsMatch(state.dfaCache, haystack)
	e.putSearchState(state)
	if !matched {
		return -1, -1, false
	}

	// DFA confirmed a match exists - use PikeVM for exact bounds
	return e.pikevm.Search(haystack)
}

// findIndicesDFAAt searches using DFA starting at position - zero alloc.
func (e *Engine) findIndicesDFAAt(haystack []byte, at int) (int, int, bool) {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// Prefilter skip-ahead — safe for all prefilters, DFA verifies.
	if e.prefilter != nil {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		// Bidirectional DFA: forward DFA → end, reverse DFA → start. O(n) total.
		if e.reverseDFA != nil {
			return e.findIndicesBidirectionalDFA(haystack, pos)
		}
		return e.pikevm.SearchAt(haystack, pos)
	}

	// No prefilter: bidirectional DFA or DFA + PikeVM fallback.
	if e.reverseDFA != nil {
		return e.findIndicesBidirectionalDFA(haystack, at)
	}
	state := e.getSearchState()
	matched := e.dfa.IsMatchAt(state.dfaCache, haystack, at)
	e.putSearchState(state)
	if !matched {
		return -1, -1, false
	}

	// DFA confirmed a match exists - use PikeVM for exact bounds
	return e.pikevm.SearchAt(haystack, at)
}

// findIndicesAdaptive tries prefilter+DFA first, falls back to NFA - zero alloc.
func (e *Engine) findIndicesAdaptive(haystack []byte) (int, int, bool) {
	// Use prefilter if available for fast candidate finding
	if e.prefilter != nil && e.dfa != nil {
		// Check if prefilter can return match bounds directly (e.g., Teddy)
		if mf, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
			start, end := mf.FindMatch(haystack, 0)
			if start == -1 {
				return -1, -1, false
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)
			atomic.AddUint64(&e.stats.DFASearches, 1)
			return start, end, true
		}

		// Standard prefilter path
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			// No candidate found - definitely no match
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		atomic.AddUint64(&e.stats.DFASearches, 1)

		// Literal fast path
		if e.prefilter.IsComplete() {
			literalLen := e.prefilter.LiteralLen()
			if literalLen > 0 {
				return pos, pos + literalLen, true
			}
		}

		// Search from prefilter position - O(m) not O(n)
		return e.pikevm.SearchAt(haystack, pos)
	}

	// Try DFA without prefilter
	if e.dfa != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		state := e.getSearchState()
		endPos := e.dfa.Find(state.dfaCache, haystack)
		if endPos != -1 {
			e.putSearchState(state)
			// Use estimated start position for O(m) search instead of O(n)
			estimatedStart := 0
			if endPos > 100 {
				estimatedStart = endPos - 100
			}
			return e.pikevm.SearchAt(haystack, estimatedStart)
		}
		size, capacity, _, _, _ := e.dfa.CacheStats(state.dfaCache)
		e.putSearchState(state)
		if size >= int(capacity)*9/10 {
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
		}
	}
	return e.findIndicesNFA(haystack)
}

// findIndicesAdaptiveAt tries prefilter+DFA first at position, falls back to NFA - zero alloc.
func (e *Engine) findIndicesAdaptiveAt(haystack []byte, at int) (int, int, bool) {
	// Use prefilter if available for fast candidate finding
	if e.prefilter != nil && e.dfa != nil {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		atomic.AddUint64(&e.stats.DFASearches, 1)

		// Literal fast path
		if e.prefilter.IsComplete() {
			literalLen := e.prefilter.LiteralLen()
			if literalLen > 0 {
				return pos, pos + literalLen, true
			}
		}

		// Search from prefilter position - O(m) not O(n)
		return e.pikevm.SearchAt(haystack, pos)
	}

	// Try DFA without prefilter
	if e.dfa != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		state := e.getSearchState()
		endPos := e.dfa.FindAt(state.dfaCache, haystack, at)
		if endPos != -1 {
			e.putSearchState(state)
			// Use estimated start for O(m) search
			estimatedStart := at
			if endPos > at+100 {
				estimatedStart = endPos - 100
			}
			return e.pikevm.SearchAt(haystack, estimatedStart)
		}
		size, capacity, _, _, _ := e.dfa.CacheStats(state.dfaCache)
		e.putSearchState(state)
		if size >= int(capacity)*9/10 {
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
		}
	}
	return e.findIndicesNFAAt(haystack, at)
}

// findIndicesReverseAnchored searches using reverse DFA - zero alloc.
func (e *Engine) findIndicesReverseAnchored(haystack []byte) (int, int, bool) {
	if e.reverseSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	match := e.reverseSearcher.Find(haystack)
	if match == nil {
		return -1, -1, false
	}
	return match.Start(), match.End(), true
}

// findIndicesReverseSuffix searches using reverse suffix optimization - zero alloc.
func (e *Engine) findIndicesReverseSuffix(haystack []byte) (int, int, bool) {
	if e.reverseSuffixSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	match := e.reverseSuffixSearcher.Find(haystack)
	if match == nil {
		return -1, -1, false
	}
	return match.Start(), match.End(), true
}

// findIndicesReverseSuffixAt searches using reverse suffix optimization from position - zero alloc.
func (e *Engine) findIndicesReverseSuffixAt(haystack []byte, at int) (int, int, bool) {
	if e.reverseSuffixSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSearcher.FindIndicesAt(haystack, at)
}

// findIndicesReverseSuffixSet searches using reverse suffix SET optimization - zero alloc.
func (e *Engine) findIndicesReverseSuffixSet(haystack []byte) (int, int, bool) {
	if e.reverseSuffixSetSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	match := e.reverseSuffixSetSearcher.Find(haystack)
	if match == nil {
		return -1, -1, false
	}
	return match.Start(), match.End(), true
}

// findIndicesReverseSuffixSetAt searches using reverse suffix SET optimization from position - zero alloc.
func (e *Engine) findIndicesReverseSuffixSetAt(haystack []byte, at int) (int, int, bool) {
	if e.reverseSuffixSetSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSetSearcher.FindIndicesAt(haystack, at)
}

// findIndicesReverseInner searches using reverse inner optimization - zero alloc.
func (e *Engine) findIndicesReverseInner(haystack []byte) (int, int, bool) {
	if e.reverseInnerSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	match := e.reverseInnerSearcher.Find(haystack)
	if match == nil {
		return -1, -1, false
	}
	return match.Start(), match.End(), true
}

// findIndicesReverseInnerAt searches using reverse inner optimization from position - zero alloc.
func (e *Engine) findIndicesReverseInnerAt(haystack []byte, at int) (int, int, bool) {
	if e.reverseInnerSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseInnerSearcher.FindIndicesAt(haystack, at)
}

// findIndicesMultilineReverseSuffix searches using multiline suffix optimization - zero alloc.
func (e *Engine) findIndicesMultilineReverseSuffix(haystack []byte) (int, int, bool) {
	if e.multilineReverseSuffixSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.multilineReverseSuffixSearcher.FindIndicesAt(haystack, 0)
}

// findIndicesAnchoredLiteral uses O(1) specialized matching for ^prefix.*suffix$ patterns.
// For anchored patterns, match always spans [0, len(haystack)].
func (e *Engine) findIndicesAnchoredLiteral(haystack []byte) (int, int, bool) {
	if MatchAnchoredLiteral(haystack, e.anchoredLiteralInfo) {
		return 0, len(haystack), true
	}
	return -1, -1, false
}

// findIndicesAnchoredLiteralAt searches using anchored literal at position - zero alloc.
// Anchored patterns can only match from position 0.
func (e *Engine) findIndicesAnchoredLiteralAt(haystack []byte, at int) (int, int, bool) {
	if at > 0 {
		return -1, -1, false
	}
	return e.findIndicesAnchoredLiteral(haystack)
}

// findIndicesMultilineReverseSuffixAt searches using multiline suffix optimization from position - zero alloc.
func (e *Engine) findIndicesMultilineReverseSuffixAt(haystack []byte, at int) (int, int, bool) {
	if e.multilineReverseSuffixSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.multilineReverseSuffixSearcher.FindIndicesAt(haystack, at)
}

// findIndicesBidirectionalDFA uses forward DFA + reverse DFA for exact match bounds.
// Three-phase: forward DFA → first match end, reverse DFA → match start,
// anchored forward DFA → correct greedy end from that start. O(n) total.
//
// Phase 1 uses SearchFirstAt (stops at first match end) to avoid DFA over-extension
// with unanchored prefix. Phase 3 then runs anchored greedy DFA from the discovered
// start to get the correct (potentially longer) end for patterns like ".*".
func (e *Engine) findIndicesBidirectionalDFA(haystack []byte, at int) (int, int, bool) {
	atomic.AddUint64(&e.stats.DFASearches, 1)
	state := e.getSearchState()
	defer e.putSearchState(state)
	// Phase 1: find first match end (leftmost-first, not leftmost-longest)
	end := e.dfa.SearchFirstAt(state.dfaCache, haystack, at)
	if end == -1 {
		return -1, -1, false
	}
	if end == at {
		return at, at, true // Empty match
	}
	// Phase 2: reverse DFA to find match start
	start := e.reverseDFA.SearchReverse(state.revDFACache, haystack, at, end)
	if start < 0 {
		return -1, -1, false // Reverse DFA failed (cache full)
	}
	// Phase 3: anchored greedy forward DFA from start → correct end.
	// SearchFirstAt may undercount for greedy patterns (e.g., ".*" stops at first ").
	// Anchored DFA from start gives the correct greedy end for this specific match.
	exactEnd := e.dfa.SearchAtAnchored(state.dfaCache, haystack, start)
	if exactEnd > start {
		end = exactEnd
	}
	return start, end, true
}

// findIndicesBidirectionalDFALongest uses forward DFA (leftmost-longest) + reverse DFA.
// Unlike findIndicesBidirectionalDFA, this preserves greedy/longest match semantics.
// Used by BoundedBacktracker fallback where greedy semantics are required.
// Accepts optional state to avoid redundant pool.Get when caller already has one.
func (e *Engine) findIndicesBidirectionalDFALongest(haystack []byte, at int, existingState ...*SearchState) (int, int, bool) {
	atomic.AddUint64(&e.stats.DFASearches, 1)
	var state *SearchState
	if len(existingState) > 0 && existingState[0] != nil {
		state = existingState[0]
	} else {
		state = e.getSearchState()
		defer e.putSearchState(state)
	}
	end := e.dfa.SearchAt(state.dfaCache, haystack, at)
	if end == -1 {
		return -1, -1, false
	}
	if end == at {
		return at, at, true // Empty match
	}
	start := e.reverseDFA.SearchReverse(state.revDFACache, haystack, at, end)
	if start < 0 {
		return -1, -1, false // Reverse DFA failed (cache full)
	}
	return start, end, true
}

// findIndicesBoundedBacktracker searches using bounded backtracker - zero alloc.
// Thread-safe: uses pooled state.
func (e *Engine) findIndicesBoundedBacktracker(haystack []byte) (int, int, bool) {
	if e.boundedBacktracker == nil {
		return e.findIndicesNFA(haystack)
	}

	// O(1) early rejection for anchored patterns using first-byte prefilter.
	if e.anchoredFirstBytes != nil && len(haystack) > 0 {
		if !e.anchoredFirstBytes.Contains(haystack[0]) {
			return -1, -1, false
		}
	}

	atomic.AddUint64(&e.stats.NFASearches, 1)
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		// Bidirectional DFA: O(n) vs PikeVM's O(n*states) for large inputs
		// Use longest variant to preserve greedy semantics for BoundedBacktracker patterns.
		if e.dfa != nil && e.reverseDFA != nil {
			return e.findIndicesBidirectionalDFALongest(haystack, 0)
		}
		return e.pikevm.SearchWithSlotTable(haystack, nfa.SearchModeFind)
	}

	state := e.getSearchState()
	defer e.putSearchState(state)
	return e.boundedBacktracker.SearchWithState(haystack, state.backtracker)
}

// findIndicesBoundedBacktrackerAt searches using bounded backtracker at position.
// Thread-safe: uses pooled state.
//
// V11-002 ASCII optimization: When pattern contains '.' and input is ASCII-only,
// uses the faster ASCII NFA.
//
// V11.5 optimization: When searching from position 'at', only check CanHandle for
// the remaining portion haystack[at:], not the full haystack. This allows
// BoundedBacktracker to handle large inputs in FindAll where each successive
// search operates on a smaller remaining portion.
func (e *Engine) findIndicesBoundedBacktrackerAt(haystack []byte, at int) (int, int, bool) {
	if e.boundedBacktracker == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// Slice to remaining portion for more efficient BoundedBacktracker usage.
	// This allows BT to handle large inputs in FindAll where we only need
	// to search the remaining portion, not the full haystack.
	remaining := haystack[at:]

	// V11-002 ASCII optimization.
	// For start-anchored patterns, limit the IsASCII check to a small prefix
	// to avoid O(n) scan of the entire input when only position 0 matters.
	if e.asciiBoundedBacktracker != nil {
		asciiCheck := remaining
		if e.isStartAnchored && len(asciiCheck) > 4096 {
			asciiCheck = asciiCheck[:4096]
		}
		if simd.IsASCII(asciiCheck) {
			if !e.asciiBoundedBacktracker.CanHandle(len(remaining)) {
				if e.dfa != nil && e.reverseDFA != nil {
					return e.findIndicesBidirectionalDFALongest(haystack, at)
				}
				return e.pikevm.SearchWithSlotTableAt(haystack, at, nfa.SearchModeFind)
			}
			start, end, found := e.asciiBoundedBacktracker.Search(remaining)
			if found {
				return at + start, at + end, true
			}
			return -1, -1, false
		}
	}

	if !e.boundedBacktracker.CanHandle(len(remaining)) {
		if e.dfa != nil && e.reverseDFA != nil {
			return e.findIndicesBidirectionalDFALongest(haystack, at)
		}
		return e.findIndicesNFAAt(haystack, at)
	}

	state := e.getSearchState()
	defer e.putSearchState(state)
	start, end, found := e.boundedBacktracker.SearchWithState(remaining, state.backtracker)
	if found {
		return at + start, at + end, true
	}
	return -1, -1, false
}

// findIndicesCharClassSearcher searches using char_class+ searcher - zero alloc.
func (e *Engine) findIndicesCharClassSearcher(haystack []byte) (int, int, bool) {
	if e.charClassSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.charClassSearcher.Search(haystack)
}

// findIndicesCharClassSearcherAt searches using char_class+ searcher at position - zero alloc.
func (e *Engine) findIndicesCharClassSearcherAt(haystack []byte, at int) (int, int, bool) {
	if e.charClassSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.charClassSearcher.SearchAt(haystack, at)
}

// findIndicesCompositeSearcher searches using CompositeSearcher - zero alloc.
func (e *Engine) findIndicesCompositeSearcher(haystack []byte) (int, int, bool) {
	// Prefer DFA over backtracking (2-4x faster for overlapping patterns)
	if e.compositeSequenceDFA != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		return e.compositeSequenceDFA.Search(haystack)
	}
	if e.compositeSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.compositeSearcher.Search(haystack)
}

// findIndicesCompositeSearcherAt searches using CompositeSearcher at position - zero alloc.
func (e *Engine) findIndicesCompositeSearcherAt(haystack []byte, at int) (int, int, bool) {
	// Prefer DFA over backtracking (2-4x faster for overlapping patterns)
	if e.compositeSequenceDFA != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		return e.compositeSequenceDFA.SearchAt(haystack, at)
	}
	if e.compositeSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.compositeSearcher.SearchAt(haystack, at)
}

// findIndicesBranchDispatch searches using branch dispatch - zero alloc.
func (e *Engine) findIndicesBranchDispatch(haystack []byte) (int, int, bool) {
	if e.branchDispatcher == nil {
		return e.findIndicesBoundedBacktracker(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.branchDispatcher.Search(haystack)
}

// findIndicesBranchDispatchAt searches using branch dispatch at position - zero alloc.
func (e *Engine) findIndicesBranchDispatchAt(haystack []byte, at int) (int, int, bool) {
	if at != 0 {
		// Anchored pattern can only match at position 0
		return -1, -1, false
	}
	return e.findIndicesBranchDispatch(haystack)
}

// findIndicesTeddy returns indices using Teddy prefilter - zero alloc.
func (e *Engine) findIndicesTeddy(haystack []byte) (int, int, bool) {
	if e.prefilter == nil {
		return e.findIndicesNFA(haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)

	// Use FindMatch which returns both start and end positions
	if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
		start, end := matcher.FindMatch(haystack, 0)
		if start == -1 {
			return -1, -1, false
		}
		return start, end, true
	}

	// Fallback: use Find + LiteralLen
	pos := e.prefilter.Find(haystack, 0)
	if pos == -1 {
		return -1, -1, false
	}
	literalLen := e.prefilter.LiteralLen()
	if literalLen > 0 {
		return pos, pos + literalLen, true
	}
	return e.findIndicesNFAAt(haystack, pos)
}

// findIndicesTeddyAt returns indices using Teddy at position - zero alloc.
func (e *Engine) findIndicesTeddyAt(haystack []byte, at int) (int, int, bool) {
	if e.prefilter == nil || at >= len(haystack) {
		return e.findIndicesNFAAt(haystack, at)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)

	// Use FindMatch which returns both start and end positions
	if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
		start, end := matcher.FindMatch(haystack, at)
		if start == -1 {
			return -1, -1, false
		}
		return start, end, true
	}

	// Fallback: use Find + LiteralLen
	pos := e.prefilter.Find(haystack, at)
	if pos == -1 {
		return -1, -1, false
	}
	literalLen := e.prefilter.LiteralLen()
	if literalLen > 0 {
		return pos, pos + literalLen, true
	}
	return e.findIndicesNFAAt(haystack, pos)
}

// findIndicesDigitPrefilter returns indices using digit prefilter - zero alloc.
func (e *Engine) findIndicesDigitPrefilter(haystack []byte) (int, int, bool) {
	if e.digitPrefilter == nil {
		return e.findIndicesNFA(haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := 0

	// Acquire pooled state once for the entire loop
	state := e.getSearchState()
	defer e.putSearchState(state)

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return -1, -1, false
		}

		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			// Use anchored search - pattern MUST start at digitPos
			// This is much faster than PikeVM for patterns that require digit start
			endPos := e.dfa.SearchAtAnchored(state.dfaCache, haystack, digitPos)
			if endPos != -1 {
				return digitPos, endPos, true
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
			start, end, found := state.pikevm.SearchAt(haystack, digitPos)
			if found {
				return start, end, true
			}
		}

		pos = digitPos + 1
		// When the leading digit class is greedy unbounded (\d+, \d*), all
		// positions in the same digit run reach the same DFA state after
		// consuming digits, so they all fail identically. Skip the entire run.
		if e.digitRunSkipSafe {
			for pos < len(haystack) && haystack[pos] >= '0' && haystack[pos] <= '9' {
				pos++
			}
		}
	}

	return -1, -1, false
}

// findIndicesDigitPrefilterAt returns indices starting at position 'at' - zero alloc.
func (e *Engine) findIndicesDigitPrefilterAt(haystack []byte, at int) (int, int, bool) {
	if e.digitPrefilter == nil || at >= len(haystack) {
		return e.findIndicesNFAAt(haystack, at)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := at

	// Acquire pooled state once for the entire loop
	state := e.getSearchState()
	defer e.putSearchState(state)

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return -1, -1, false
		}

		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			// Use anchored search - pattern MUST start at digitPos
			// This is much faster than PikeVM for patterns that require digit start
			endPos := e.dfa.SearchAtAnchored(state.dfaCache, haystack, digitPos)
			if endPos != -1 {
				return digitPos, endPos, true
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
			start, end, found := state.pikevm.SearchAt(haystack, digitPos)
			if found {
				return start, end, true
			}
		}

		pos = digitPos + 1
		if e.digitRunSkipSafe {
			for pos < len(haystack) && haystack[pos] >= '0' && haystack[pos] <= '9' {
				pos++
			}
		}
	}

	return -1, -1, false
}

// findIndicesAhoCorasick returns indices using Aho-Corasick - zero alloc.
func (e *Engine) findIndicesAhoCorasick(haystack []byte) (int, int, bool) {
	if e.ahoCorasick == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)

	m := e.ahoCorasick.Find(haystack, 0)
	if m == nil {
		return -1, -1, false
	}
	return m.Start, m.End, true
}

// findIndicesAhoCorasickAt returns indices using Aho-Corasick starting at position 'at' - zero alloc.
func (e *Engine) findIndicesAhoCorasickAt(haystack []byte, at int) (int, int, bool) {
	if e.ahoCorasick == nil || at >= len(haystack) {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)

	m := e.ahoCorasick.Find(haystack, at)
	if m == nil {
		return -1, -1, false
	}
	return m.Start, m.End, true
}

// =============================================================================
// Internal state-reusing methods (for findAllIndicesLoop optimization)
// =============================================================================

// findIndicesAtWithState is the internal version that reuses provided state.
// Used by findAllIndicesLoop to avoid sync.Pool overhead per match.
// This dispatcher handles all strategies, delegating to existing methods for
// strategies that don't need mutable state, and using *WithState methods for
// strategies that do (NFA, BoundedBacktracker).
func (e *Engine) findIndicesAtWithState(haystack []byte, at int, state *SearchState) (start, end int, found bool) {
	// Early impossibility check: anchored pattern can only match at position 0
	if at > 0 && e.nfa.IsAlwaysAnchored() {
		return -1, -1, false
	}

	switch e.strategy {
	case UseNFA:
		return e.findIndicesNFAAtWithState(haystack, at, state)
	case UseDFA:
		// DFA uses e.pikevm (shared) for final bounds, not pooled state
		return e.findIndicesDFAAt(haystack, at)
	case UseBoth:
		// Adaptive uses e.pikevm (shared) or delegates to NFA path
		return e.findIndicesAdaptiveAt(haystack, at)
	case UseReverseSuffix:
		return e.reverseSuffixSearcher.FindIndicesAtWithCaches(haystack, at, state.stratFwdCache, state.stratRevCache)
	case UseReverseSuffixSet:
		return e.reverseSuffixSetSearcher.FindIndicesAtWithCaches(haystack, at, state.stratRevCache)
	case UseReverseInner:
		return e.reverseInnerSearcher.FindIndicesAtWithCaches(haystack, at, state.stratFwdCache, state.stratRevCache)
	case UseBoundedBacktracker:
		return e.findIndicesBoundedBacktrackerAtWithState(haystack, at, state)
	case UseCharClassSearcher:
		return e.findIndicesCharClassSearcherAt(haystack, at)
	case UseCompositeSearcher:
		return e.findIndicesCompositeSearcherAt(haystack, at)
	case UseBranchDispatch:
		return e.findIndicesBranchDispatchAt(haystack, at)
	case UseTeddy:
		return e.findIndicesTeddyAt(haystack, at)
	case UseDigitPrefilter:
		return e.findIndicesDigitPrefilterAt(haystack, at)
	case UseAhoCorasick:
		return e.findIndicesAhoCorasickAt(haystack, at)
	case UseMultilineReverseSuffix:
		return e.multilineReverseSuffixSearcher.FindIndicesAtWithCaches(haystack, at, state.stratFwdCache)
	case UseAnchoredLiteral:
		return e.findIndicesAnchoredLiteralAt(haystack, at)
	default:
		return e.findIndicesNFAAtWithState(haystack, at, state)
	}
}

// findIndicesNFAAtWithState searches using NFA starting at position - zero alloc.
// This is the state-reusing version for findAllIndicesLoop optimization.
// Thread-safe: reuses provided state (no sync.Pool Get/Put).
func (e *Engine) findIndicesNFAAtWithState(haystack []byte, at int, state *SearchState) (int, int, bool) {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// BoundedBacktracker can be used for Find operations only when safe
	useBT := e.boundedBacktracker != nil && !e.canMatchEmpty

	// Use prefilter candidate loop — safe unless partial coverage (overflow).
	// Partial-coverage prefilters would miss unrepresented branches.
	if e.prefilter != nil && !e.prefilterPartialCoverage {
		for at < len(haystack) {
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return -1, -1, false
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)

			var start, end int
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				start, end, found = e.boundedBacktracker.SearchAtWithState(haystack, pos, state.backtracker)
			} else {
				start, end, found = state.pikevm.SearchWithSlotTableAt(haystack, pos, nfa.SearchModeFind)
			}
			if found {
				return start, end, true
			}

			atomic.AddUint64(&e.stats.PrefilterMisses, 1)
			at = pos + 1
		}
		return -1, -1, false
	}

	// No prefilter or incomplete: use BoundedBacktracker if available and safe
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)-at) {
		return e.boundedBacktracker.SearchAtWithState(haystack, at, state.backtracker)
	}

	// Use optimized SlotTable-based search for large inputs
	return state.pikevm.SearchWithSlotTableAt(haystack, at, nfa.SearchModeFind)
}

// findIndicesBoundedBacktrackerAtWithState searches using bounded backtracker at position.
// This is the state-reusing version for findAllIndicesLoop optimization.
// Thread-safe: reuses provided state (no sync.Pool Get/Put).
//
// V11-002 ASCII optimization: When pattern contains '.' and input is ASCII-only,
// uses the faster ASCII NFA.
//
// V11.5 optimization: When searching from position 'at', only check CanHandle for
// the remaining portion haystack[at:], not the full haystack. This allows
// BoundedBacktracker to handle large inputs in FindAll where each successive
// search operates on a smaller remaining portion.
func (e *Engine) findIndicesBoundedBacktrackerAtWithState(haystack []byte, at int, state *SearchState) (int, int, bool) {
	if e.boundedBacktracker == nil {
		return e.findIndicesNFAAtWithState(haystack, at, state)
	}

	// O(1) early rejection for anchored patterns using first-byte prefilter.
	// For pattern ^/.*\.php, reject inputs not starting with "/" immediately.
	if at == 0 && e.anchoredFirstBytes != nil && len(haystack) > 0 {
		if !e.anchoredFirstBytes.Contains(haystack[0]) {
			return -1, -1, false
		}
	}

	atomic.AddUint64(&e.stats.NFASearches, 1)

	// Slice to remaining portion for more efficient BoundedBacktracker usage.
	// This allows BT to handle large inputs in FindAll where we only need
	// to search the remaining portion, not the full haystack.
	remaining := haystack[at:]

	// V11-002 ASCII optimization.
	// For start-anchored patterns, limit the IsASCII check to a small prefix
	// to avoid O(n) scan of the entire input when only position 0 matters.
	if e.asciiBoundedBacktracker != nil {
		asciiCheck := remaining
		if e.isStartAnchored && len(asciiCheck) > 4096 {
			asciiCheck = asciiCheck[:4096]
		}
		if simd.IsASCII(asciiCheck) {
			if !e.asciiBoundedBacktracker.CanHandle(len(remaining)) {
				// Bidirectional DFA: O(n) vs PikeVM's O(n*states)
				if e.dfa != nil && e.reverseDFA != nil {
					return e.findIndicesBidirectionalDFALongest(haystack, at, state)
				}
				// V12 Windowed BoundedBacktracker for ASCII path
				maxInput := e.asciiBoundedBacktracker.MaxInputSize()
				if maxInput > 0 && len(remaining) > maxInput {
					window := remaining[:maxInput]
					start, end, found := e.asciiBoundedBacktracker.Search(window)
					if found {
						return at + start, at + end, true
					}
				}
				return state.pikevm.SearchWithSlotTableAt(haystack, at, nfa.SearchModeFind)
			}
			start, end, found := e.asciiBoundedBacktracker.Search(remaining)
			if found {
				return at + start, at + end, true
			}
			return -1, -1, false
		}
	}

	if !e.boundedBacktracker.CanHandle(len(remaining)) {
		// Bidirectional DFA: O(n) vs PikeVM's O(n*states) for large inputs
		if e.dfa != nil && e.reverseDFA != nil {
			return e.findIndicesBidirectionalDFALongest(haystack, at, state)
		}
		// V12 Windowed BoundedBacktracker fallback
		maxInput := e.boundedBacktracker.MaxInputSize()
		if maxInput > 0 && len(remaining) > maxInput {
			window := remaining[:maxInput]
			start, end, found := e.boundedBacktracker.SearchWithState(window, state.backtracker)
			if found {
				return at + start, at + end, true
			}
		}
		return state.pikevm.SearchWithSlotTableAt(haystack, at, nfa.SearchModeFind)
	}

	start, end, found := e.boundedBacktracker.SearchWithState(remaining, state.backtracker)
	if found {
		return at + start, at + end, true
	}
	return -1, -1, false
}
