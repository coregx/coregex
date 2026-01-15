// Package meta implements the meta-engine orchestrator.
//
// engine.go contains the Engine struct definition and core API methods.

package meta

import (
	"github.com/coregx/ahocorasick"
	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/dfa/onepass"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// Engine is the meta-engine that orchestrates all regex execution strategies.
//
// The Engine:
//  1. Analyzes the pattern and extracts literals
//  2. Selects the optimal strategy (NFA, DFA, or both)
//  3. Builds prefilter (if literals available)
//  4. Coordinates search across engines
//
// Thread safety: The Engine uses a sync.Pool internally to provide thread-safe
// concurrent access. Multiple goroutines can safely call search methods (Find,
// IsMatch, FindSubmatch, etc.) on the same Engine instance concurrently.
//
// The underlying NFA, DFA, and prefilters are immutable after compilation.
// Per-search mutable state is managed via sync.Pool, following the Go stdlib
// regexp package pattern.
//
// Example:
//
//	// Compile pattern (once)
//	engine, err := meta.Compile("(foo|bar)\\d+")
//	if err != nil {
//	    return err
//	}
//
//	// Search (safe to call from multiple goroutines)
//	haystack := []byte("test foo123 end")
//	match := engine.Find(haystack)
//	if match != nil {
//	    println(match.String()) // "foo123"
//	}
type Engine struct {
	// Statistics (useful for debugging and tuning)
	// IMPORTANT: stats MUST be first field for proper 8-byte alignment on 32-bit platforms.
	// This ensures atomic operations on uint64 fields work correctly.
	stats Stats

	nfa *nfa.NFA

	// asciiNFA is an NFA compiled in ASCII-only mode (V11-002 optimization).
	// When the pattern contains '.' and input is ASCII-only (all bytes < 0x80),
	// this NFA is used instead of the main NFA. ASCII mode compiles '.' to
	// a single byte range (0x00-0x7F) instead of ~28 UTF-8 states.
	//
	// Performance impact for Issue #79 pattern ^/.*[\w-]+\.php:
	//   - UTF-8 NFA: ~39 states, BoundedBacktracker walks all states per byte
	//   - ASCII NFA: ~14 states, 2.8x state reduction
	//
	// Runtime detection uses SIMD (AVX2 on x86-64) to check if input is ASCII,
	// achieving ~20-40 GB/s throughput.
	//
	// This field is nil if:
	//   - Pattern doesn't contain '.' (no benefit from ASCII optimization)
	//   - ASCII optimization is disabled via config
	asciiNFA                       *nfa.NFA
	asciiBoundedBacktracker        *nfa.BoundedBacktracker // BoundedBacktracker for asciiNFA
	dfa                            *lazy.DFA
	pikevm                         *nfa.PikeVM
	boundedBacktracker             *nfa.BoundedBacktracker
	charClassSearcher              *nfa.CharClassSearcher    // Specialized searcher for char_class+ patterns
	compositeSearcher              *nfa.CompositeSearcher    // For concatenated char classes like [a-zA-Z]+[0-9]+
	compositeSequenceDFA           *nfa.CompositeSequenceDFA // DFA for composite patterns (faster than backtracking)
	branchDispatcher               *nfa.BranchDispatcher     // O(1) branch dispatch for anchored alternations
	anchoredFirstBytes             *nfa.FirstByteSet         // O(1) first-byte rejection for anchored patterns
	anchoredSuffix                 []byte                    // O(1) suffix rejection for anchored patterns
	reverseSearcher                *ReverseAnchoredSearcher
	reverseSuffixSearcher          *ReverseSuffixSearcher
	reverseSuffixSetSearcher       *ReverseSuffixSetSearcher
	reverseInnerSearcher           *ReverseInnerSearcher
	multilineReverseSuffixSearcher *MultilineReverseSuffixSearcher // For (?m)^.*suffix patterns
	digitPrefilter                 *prefilter.DigitPrefilter       // For digit-lead patterns like IP addresses
	ahoCorasick                    *ahocorasick.Automaton          // For large literal alternations (>32 patterns)
	anchoredLiteralInfo            *AnchoredLiteralInfo            // For ^prefix.*suffix$ patterns (Issue #79)
	prefilter                      prefilter.Prefilter
	strategy                       Strategy
	config                         Config

	// fatTeddyFallback is an Aho-Corasick automaton used as fallback for small haystacks
	// when the main prefilter is Fat Teddy (33-64 patterns). Fat Teddy's AVX2 SIMD setup
	// overhead makes it slower than Aho-Corasick for haystacks < 64 bytes.
	// Reference: rust-aho-corasick/src/packed/teddy/builder.rs:585 (minimum_len fallback)
	fatTeddyFallback *ahocorasick.Automaton

	// OnePass DFA for anchored patterns with captures (optional optimization)
	// This is independent of strategy - used by FindSubmatch when available
	// Note: The cache is now stored in pooled SearchState for thread-safety
	onepass *onepass.DFA

	// statePool provides thread-safe pooling of per-search mutable state.
	// This enables concurrent searches on the same Engine instance.
	statePool *searchStatePool

	// longest enables leftmost-longest (POSIX) matching semantics
	// By default (false), uses leftmost-first (Perl) semantics
	longest bool

	// canMatchEmpty is true if the pattern can match an empty string.
	// When true, BoundedBacktracker cannot be used for Find operations
	// because its greedy semantics give wrong results for patterns like (?:|a)*
	canMatchEmpty bool

	// isStartAnchored is true if the pattern is anchored at start (^).
	// Used for first-byte prefilter optimization.
	isStartAnchored bool
}

// Stats tracks execution statistics for performance analysis.
type Stats struct {
	// NFASearches counts NFA (PikeVM) searches
	NFASearches uint64

	// DFASearches counts DFA searches
	DFASearches uint64

	// OnePassSearches counts OnePass DFA searches (for FindSubmatch)
	OnePassSearches uint64

	// AhoCorasickSearches counts Aho-Corasick automaton searches
	AhoCorasickSearches uint64

	// PrefilterHits counts successful prefilter matches
	PrefilterHits uint64

	// PrefilterMisses counts prefilter candidates that didn't match
	PrefilterMisses uint64

	// PrefilterAbandoned counts times prefilter was abandoned due to high FP rate
	PrefilterAbandoned uint64

	// DFACacheFull counts times DFA fell back to NFA due to cache full
	DFACacheFull uint64
}

// Strategy returns the execution strategy selected for this engine.
//
// Example:
//
//	strategy := engine.Strategy()
//	println(strategy.String()) // "UseDFA"
func (e *Engine) Strategy() Strategy {
	return e.strategy
}

// IsStartAnchored returns true if the pattern is anchored at the start (^).
// Start-anchored patterns can only match at position 0.
func (e *Engine) IsStartAnchored() bool {
	return e.isStartAnchored
}

// Stats returns execution statistics.
//
// Useful for performance analysis and debugging.
//
// Example:
//
//	stats := engine.Stats()
//	println("NFA searches:", stats.NFASearches)
//	println("DFA searches:", stats.DFASearches)
func (e *Engine) Stats() Stats {
	return e.stats
}

// ResetStats resets execution statistics to zero.
func (e *Engine) ResetStats() {
	e.stats = Stats{}
}

// NumCaptures returns the number of capture groups in the pattern.
// Group 0 is the entire match, groups 1+ are explicit captures.
func (e *Engine) NumCaptures() int {
	return e.nfa.CaptureCount()
}

// SubexpNames returns the names of capture groups in the pattern.
// Index 0 is always "" (entire match). Named groups return their names, unnamed groups return "".
// This matches stdlib regexp.Regexp.SubexpNames() behavior.
func (e *Engine) SubexpNames() []string {
	return e.nfa.SubexpNames()
}

// SetLongest enables or disables leftmost-longest (POSIX) matching semantics.
// By default, the engine uses leftmost-first (Perl) semantics where the first
// alternative in an alternation wins. With longest=true, the longest match wins.
//
// This affects how alternations like `(a|ab)` match:
//   - longest=false (default): "a" wins (first branch)
//   - longest=true: "ab" wins (longest match)
func (e *Engine) SetLongest(longest bool) {
	e.longest = longest
	e.pikevm.SetLongest(longest)
	if e.boundedBacktracker != nil {
		e.boundedBacktracker.SetLongest(longest)
	}
}

// getSearchState retrieves a SearchState from the pool.
// Caller must call putSearchState when done.
// The returned state contains its own PikeVM instance for thread-safe concurrent use.
func (e *Engine) getSearchState() *SearchState {
	state := e.statePool.get()

	// Initialize state for BoundedBacktracker if needed
	if e.boundedBacktracker != nil && state.backtracker != nil {
		state.backtracker.Longest = e.longest
	}

	// PikeVM is already created per-state, just set longest flag if needed
	if state.pikevm != nil {
		state.pikevm.SetLongest(e.longest)
	}

	return state
}

// putSearchState returns a SearchState to the pool.
func (e *Engine) putSearchState(state *SearchState) {
	e.statePool.put(state)
}
