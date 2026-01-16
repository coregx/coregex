// Package meta implements the meta-engine orchestrator.
//
// find_indices.go contains FindIndices methods that return (start, end, found) tuples.
// These are zero-allocation alternatives to the Find methods.

package meta

import (
	"sync/atomic"

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

	// Use prefilter for skip-ahead if available
	if e.prefilter != nil {
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
				start, end, found = state.pikevm.SearchAt(haystack, pos)
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

	return state.pikevm.Search(haystack)
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

	// Use prefilter for skip-ahead if available
	if e.prefilter != nil {
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
				start, end, found = state.pikevm.SearchAt(haystack, pos)
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
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)-at) {
		return e.boundedBacktracker.SearchAtWithState(haystack, at, state.backtracker)
	}

	return state.pikevm.SearchAt(haystack, at)
}

// findIndicesDFA searches using DFA with prefilter - zero alloc.
func (e *Engine) findIndicesDFA(haystack []byte) (int, int, bool) {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// Literal fast path
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

	// Use DFA search to check if there's a match
	pos := e.dfa.Find(haystack)
	if pos == -1 {
		return -1, -1, false
	}

	// DFA found a match - use PikeVM for exact bounds (leftmost-first semantics)
	// NOTE: Bidirectional search (reverse DFA) doesn't work correctly here because
	// DFA.Find returns the END of LONGEST match, not FIRST match. For patterns like
	// (?m)abc$ on "abc\nabc", DFA returns 7 but correct first match ends at 3.
	return e.pikevm.Search(haystack)
}

// findIndicesDFAAt searches using DFA starting at position - zero alloc.
func (e *Engine) findIndicesDFAAt(haystack []byte, at int) (int, int, bool) {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// Literal fast path
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		literalLen := e.prefilter.LiteralLen()
		if literalLen > 0 {
			return pos, pos + literalLen, true
		}
		return e.pikevm.SearchAt(haystack, at)
	}

	// Use DFA search to check if there's a match
	pos := e.dfa.FindAt(haystack, at)
	if pos == -1 {
		return -1, -1, false
	}

	// DFA found a match - use PikeVM for exact bounds (leftmost-first semantics)
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
		endPos := e.dfa.Find(haystack)
		if endPos != -1 {
			// Use estimated start position for O(m) search instead of O(n)
			estimatedStart := 0
			if endPos > 100 {
				estimatedStart = endPos - 100
			}
			return e.pikevm.SearchAt(haystack, estimatedStart)
		}
		size, capacity, _, _, _ := e.dfa.CacheStats()
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
		endPos := e.dfa.FindAt(haystack, at)
		if endPos != -1 {
			// Use estimated start for O(m) search
			estimatedStart := at
			if endPos > at+100 {
				estimatedStart = endPos - 100
			}
			return e.pikevm.SearchAt(haystack, estimatedStart)
		}
		size, capacity, _, _, _ := e.dfa.CacheStats()
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

// findIndicesMultilineReverseSuffixAt searches using multiline suffix optimization from position - zero alloc.
func (e *Engine) findIndicesMultilineReverseSuffixAt(haystack []byte, at int) (int, int, bool) {
	if e.multilineReverseSuffixSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.multilineReverseSuffixSearcher.FindIndicesAt(haystack, at)
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
		return e.pikevm.Search(haystack)
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
func (e *Engine) findIndicesBoundedBacktrackerAt(haystack []byte, at int) (int, int, bool) {
	if e.boundedBacktracker == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// V11-002 ASCII optimization
	if e.asciiBoundedBacktracker != nil && simd.IsASCII(haystack) {
		if !e.asciiBoundedBacktracker.CanHandle(len(haystack)) {
			return e.pikevm.SearchAt(haystack, at)
		}
		return e.asciiBoundedBacktracker.SearchAt(haystack, at)
	}

	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.pikevm.SearchAt(haystack, at)
	}

	state := e.getSearchState()
	defer e.putSearchState(state)
	return e.boundedBacktracker.SearchAtWithState(haystack, at, state.backtracker)
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

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		match := e.fatTeddyFallback.Find(haystack, 0)
		if match == nil {
			return -1, -1, false
		}
		return match.Start, match.End, true
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

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		match := e.fatTeddyFallback.FindAt(haystack, at)
		if match == nil {
			return -1, -1, false
		}
		return match.Start, match.End, true
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

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return -1, -1, false
		}

		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			// Use anchored search - pattern MUST start at digitPos
			// This is much faster than PikeVM for patterns that require digit start
			endPos := e.dfa.SearchAtAnchored(haystack, digitPos)
			if endPos != -1 {
				return digitPos, endPos, true
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
			start, end, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return start, end, true
			}
		}

		pos = digitPos + 1
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

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return -1, -1, false
		}

		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			// Use anchored search - pattern MUST start at digitPos
			// This is much faster than PikeVM for patterns that require digit start
			endPos := e.dfa.SearchAtAnchored(haystack, digitPos)
			if endPos != -1 {
				return digitPos, endPos, true
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
			start, end, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return start, end, true
			}
		}

		pos = digitPos + 1
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
