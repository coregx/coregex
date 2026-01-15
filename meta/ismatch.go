// Package meta implements the meta-engine orchestrator.
//
// ismatch.go contains IsMatch methods for boolean matching.

package meta

import (
	"bytes"
	"sync/atomic"

	"github.com/coregx/coregex/simd"
)

// IsMatch returns true if the pattern matches anywhere in the haystack.
//
// This is optimized for boolean matching:
//   - Uses early termination (returns immediately on first match)
//   - Avoids Match object creation
//   - Uses DFA.IsMatch when available (2-10x faster than Find)
//
// Example:
//
//	engine, _ := meta.Compile("hello")
//	if engine.IsMatch([]byte("say hello world")) {
//	    println("matches!")
//	}
func (e *Engine) IsMatch(haystack []byte) bool {
	switch e.strategy {
	case UseNFA:
		return e.isMatchNFA(haystack)
	case UseDFA:
		return e.isMatchDFA(haystack)
	case UseBoth:
		return e.isMatchAdaptive(haystack)
	case UseReverseAnchored:
		return e.isMatchReverseAnchored(haystack)
	case UseReverseSuffix:
		return e.isMatchReverseSuffix(haystack)
	case UseReverseSuffixSet:
		return e.isMatchReverseSuffixSet(haystack)
	case UseReverseInner:
		return e.isMatchReverseInner(haystack)
	case UseMultilineReverseSuffix:
		return e.isMatchMultilineReverseSuffix(haystack)
	case UseBoundedBacktracker:
		return e.isMatchBoundedBacktracker(haystack)
	case UseCharClassSearcher:
		return e.isMatchCharClassSearcher(haystack)
	case UseCompositeSearcher:
		return e.isMatchCompositeSearcher(haystack)
	case UseBranchDispatch:
		return e.isMatchBranchDispatch(haystack)
	case UseTeddy:
		return e.isMatchTeddy(haystack)
	case UseDigitPrefilter:
		return e.isMatchDigitPrefilter(haystack)
	case UseAhoCorasick:
		return e.isMatchAhoCorasick(haystack)
	case UseAnchoredLiteral:
		return e.isMatchAnchoredLiteral(haystack)
	default:
		return e.isMatchNFA(haystack)
	}
}

// isMatchAnchoredLiteral uses O(1) specialized matching for ^prefix.*suffix$ patterns.
// This is the fastest path for URL/path patterns like ^/.*[\w-]+\.php$.
// Algorithm:
//  1. O(1) length check
//  2. O(k) prefix check
//  3. O(k) suffix check
//  4. O(m) charclass bridge verification (if required)
func (e *Engine) isMatchAnchoredLiteral(haystack []byte) bool {
	return MatchAnchoredLiteral(haystack, e.anchoredLiteralInfo)
}

// isMatchNFA checks for match using NFA (PikeVM or BoundedBacktracker) with early termination.
// Uses prefilter for skip-ahead when available (like Rust regex).
// For small NFAs, prefers BoundedBacktracker (2-3x faster than PikeVM on small inputs).
// Thread-safe: uses pooled state for both BoundedBacktracker and PikeVM.
func (e *Engine) isMatchNFA(haystack []byte) bool {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// BoundedBacktracker is preferred when available (supports both default and Longest modes)
	useBT := e.boundedBacktracker != nil

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
				return false // No more candidates
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)

			// Try to match at candidate position
			// Prefer BoundedBacktracker for small inputs (2-3x faster)
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				_, _, found = e.boundedBacktracker.SearchAtWithState(haystack, pos, state.backtracker)
			} else {
				_, _, found = state.pikevm.SearchAt(haystack, pos)
			}
			if found {
				return true
			}

			// Move past this position
			atomic.AddUint64(&e.stats.PrefilterMisses, 1)
			at = pos + 1
		}
		return false
	}

	// No prefilter: use BoundedBacktracker if available, else PikeVM
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.boundedBacktracker.IsMatchWithState(haystack, state.backtracker)
	}

	// Use optimized IsMatch that returns immediately on first match
	// without computing exact match positions
	return state.pikevm.IsMatch(haystack)
}

// isMatchDFA checks for match using DFA with early termination.
func (e *Engine) isMatchDFA(haystack []byte) bool {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// Use DFA.IsMatch which has early termination optimization
	return e.dfa.IsMatch(haystack)
}

// isMatchAdaptive tries prefilter/DFA first, falls back to NFA.
func (e *Engine) isMatchAdaptive(haystack []byte) bool {
	// Use prefilter if available for fast boolean matching
	if e.prefilter != nil {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return false // Prefilter says no match
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		// For complete prefilters (Teddy with literals), the find is sufficient
		if e.prefilter.IsComplete() {
			return true
		}
		// Verify with NFA for incomplete prefilters
		return e.isMatchNFA(haystack)
	}

	// Fall back to DFA
	if e.dfa != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		if e.dfa.IsMatch(haystack) {
			return true
		}
		// DFA returned false - check if cache was full
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 {
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
			// Cache nearly full, fall back to NFA
			return e.isMatchNFA(haystack)
		}
		return false
	}
	return e.isMatchNFA(haystack)
}

// isMatchBoundedBacktracker checks for match using bounded backtracker.
// 2-4x faster than PikeVM for simple character class patterns.
// Thread-safe: uses pooled state.
//
// V11-002 ASCII optimization: When pattern contains '.' and input is ASCII-only,
// uses the faster ASCII NFA with ~2.8x fewer states.
func (e *Engine) isMatchBoundedBacktracker(haystack []byte) bool {
	if e.boundedBacktracker == nil {
		return e.isMatchNFA(haystack)
	}

	// O(1) early rejection for anchored patterns using first-byte prefilter.
	// For ^(\d+|UUID|hex32), quickly reject inputs not starting with valid byte.
	if e.anchoredFirstBytes != nil && len(haystack) > 0 {
		if !e.anchoredFirstBytes.Contains(haystack[0]) {
			return false
		}
	}

	// O(1) early rejection for anchored patterns using suffix prefilter.
	// For ^/.*[\w-]+\.php, quickly reject inputs not ending with ".php".
	if len(e.anchoredSuffix) > 0 && !bytes.HasSuffix(haystack, e.anchoredSuffix) {
		return false
	}

	atomic.AddUint64(&e.stats.NFASearches, 1) // Count as NFA-family search for stats

	// V11-002 ASCII optimization: use ASCII NFA when input is ASCII-only.
	// SIMD isASCII check runs at ~20-40 GB/s, adding minimal overhead (~3-4ns).
	// For Issue #79 pattern ^/.*[\w-]+\.php, ASCII NFA has 14 states vs 39 states.
	if e.asciiBoundedBacktracker != nil && simd.IsASCII(haystack) {
		if !e.asciiBoundedBacktracker.CanHandle(len(haystack)) {
			return e.pikevm.IsMatch(haystack)
		}
		// Use ASCII backtracker directly (no pooled state needed - it's independent)
		return e.asciiBoundedBacktracker.IsMatch(haystack)
	}

	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		// Input too large for bounded backtracker, fall back to PikeVM
		return e.pikevm.IsMatch(haystack)
	}

	// Use pooled state for thread-safety
	state := e.getSearchState()
	defer e.putSearchState(state)
	return e.boundedBacktracker.IsMatchWithState(haystack, state.backtracker)
}

// isMatchCharClassSearcher checks for match using specialized char_class+ searcher.
func (e *Engine) isMatchCharClassSearcher(haystack []byte) bool {
	if e.charClassSearcher == nil {
		return e.isMatchNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.charClassSearcher.IsMatch(haystack)
}

// isMatchCompositeSearcher checks for match using CompositeSearcher.
func (e *Engine) isMatchCompositeSearcher(haystack []byte) bool {
	// Prefer DFA over backtracking
	if e.compositeSequenceDFA != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		return e.compositeSequenceDFA.IsMatch(haystack)
	}
	if e.compositeSearcher == nil {
		return e.isMatchNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.compositeSearcher.IsMatch(haystack)
}

// isMatchBranchDispatch checks for match using O(1) branch dispatch.
func (e *Engine) isMatchBranchDispatch(haystack []byte) bool {
	if e.branchDispatcher == nil {
		return e.isMatchBoundedBacktracker(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.branchDispatcher.IsMatch(haystack)
}

// isMatchTeddy checks for match using Teddy prefilter.
func (e *Engine) isMatchTeddy(haystack []byte) bool {
	if e.prefilter == nil {
		return e.isMatchNFA(haystack)
	}

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		return e.fatTeddyFallback.IsMatch(haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	return e.prefilter.Find(haystack, 0) != -1
}

// isMatchDigitPrefilter checks for match using digit prefilter.
// Optimized for boolean matching with early termination.
func (e *Engine) isMatchDigitPrefilter(haystack []byte) bool {
	if e.digitPrefilter == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := 0

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return false // No more digits
		}

		// Use DFA for fast boolean check if available
		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			if e.dfa.FindAt(haystack, digitPos) != -1 {
				return true
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
			_, _, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return true
			}
		}

		pos = digitPos + 1
	}

	return false
}

// isMatchAhoCorasick checks for match using Aho-Corasick automaton.
// Optimized for boolean matching with zero allocations.
func (e *Engine) isMatchAhoCorasick(haystack []byte) bool {
	if e.ahoCorasick == nil {
		return e.isMatchNFA(haystack)
	}
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
	return e.ahoCorasick.IsMatch(haystack)
}

// isMatchReverseAnchored checks for match using reverse DFA.
func (e *Engine) isMatchReverseAnchored(haystack []byte) bool {
	if e.reverseSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSearcher.IsMatch(haystack)
}

// isMatchReverseSuffix checks for match using suffix prefilter + reverse DFA.
func (e *Engine) isMatchReverseSuffix(haystack []byte) bool {
	if e.reverseSuffixSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSearcher.IsMatch(haystack)
}

// isMatchReverseSuffixSet checks for match using Teddy multi-suffix prefilter.
func (e *Engine) isMatchReverseSuffixSet(haystack []byte) bool {
	if e.reverseSuffixSetSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSetSearcher.IsMatch(haystack)
}

// isMatchReverseInner checks for match using inner prefilter + bidirectional DFA.
func (e *Engine) isMatchReverseInner(haystack []byte) bool {
	if e.reverseInnerSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseInnerSearcher.IsMatch(haystack)
}

// isMatchMultilineReverseSuffix checks for match using line-aware suffix prefilter.
// This handles multiline patterns like (?m)^/.*\.php where ^ matches at line starts.
func (e *Engine) isMatchMultilineReverseSuffix(haystack []byte) bool {
	if e.multilineReverseSuffixSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.multilineReverseSuffixSearcher.IsMatch(haystack)
}
