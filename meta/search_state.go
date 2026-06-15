package meta

import (
	"sync"

	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/dfa/onepass"
	"github.com/coregx/coregex/nfa"
)

// SearchState holds per-search mutable state for thread-safe concurrent searches.
// This struct should be obtained from a sync.Pool to enable safe concurrent usage
// of the same compiled Engine from multiple goroutines.
//
// Usage pattern:
//
//	state := engine.getSearchState()
//	defer engine.putSearchState(state)
//	// use state for search operations
//
// Thread safety: Each goroutine must use its own SearchState instance.
// The SearchState itself is NOT thread-safe - it must not be shared between goroutines.
type SearchState struct {
	// backtracker holds mutable state for BoundedBacktracker searches.
	// Used when strategy is UseBoundedBacktracker or as fallback for small inputs.
	backtracker *nfa.BacktrackerState

	// pikevm is a per-search PikeVM instance.
	// PikeVM has extensive internal state that is mutated during search,
	// so we pool entire PikeVM instances for thread-safety.
	pikevm *nfa.PikeVM

	// dfaCache holds per-search mutable state for lazy DFA searches.
	// Each goroutine gets its own cache, eliminating the data race on shared
	// DFA state construction that previously required PikeVM workarounds.
	// Nil if no forward DFA was compiled (e.g., NFA-only strategies).
	dfaCache *lazy.DFACache

	// revDFACache holds per-search mutable state for reverse lazy DFA searches.
	// Used by bidirectional DFA strategies (ReverseSuffix, ReverseInner, etc.)
	// to find match start positions. Nil if no reverse DFA was compiled.
	revDFACache *lazy.DFACache

	// stratFwdCache holds per-search cache for strategy-specific forward DFA.
	// Used by ReverseSuffix, ReverseInner, MultilineReverseSuffix searchers
	// which have their own DFAs separate from e.dfa.
	// Nil if strategy doesn't use a strategy-specific forward DFA.
	stratFwdCache *lazy.DFACache

	// stratRevCache holds per-search cache for strategy-specific reverse DFA.
	// Used by ReverseSuffix, ReverseInner, ReverseSuffixSet, ReverseAnchored
	// searchers which have their own reverse DFAs.
	// Nil if strategy doesn't use a strategy-specific reverse DFA.
	stratRevCache *lazy.DFACache

	// onepassSlots holds capture slot storage for OnePass DFA searches.
	// Pre-allocated to avoid allocation per search.
	onepassSlots []int

	// onepassCache is the cache for OnePass DFA searches.
	onepassCache *onepass.Cache
}

// searchStateConfig holds all DFA references needed to create per-search caches.
type searchStateConfig struct {
	nfaEngine   *nfa.NFA
	numCaptures int
	forwardDFA  *lazy.DFA // e.dfa (main engine DFA)
	reverseDFA  *lazy.DFA // e.reverseDFA (main engine reverse DFA)
	stratFwdDFA *lazy.DFA // strategy-specific forward DFA (reverse searchers)
	stratRevDFA *lazy.DFA // strategy-specific reverse DFA (reverse searchers)
	strategy    Strategy  // active strategy — drives conditional allocation
	hasOnePass  bool      // true if OnePass DFA was compiled
}

// newSearchState creates a new SearchState with strategy-aware allocation.
// Only components needed by the active strategy are allocated, reducing
// memory per compiled pattern by 30-70% for WAF-style workloads where
// most patterns use simple strategies (NFA, CharClass, Teddy, etc.).
//
// Issue #158: OWASP CRS patterns averaged 152 KB/pattern vs stdlib's 9.5 KB.
// Strategy-aware allocation cuts unnecessary DFA caches and backtracker state.
func newSearchState(cfg searchStateConfig) *SearchState {
	state := &SearchState{}

	// PikeVM is always needed — it's the universal fallback engine and used
	// by FindSubmatch two-phase search for capture extraction.
	// Issue #158: Use lazy initialization — the PikeVM's internal state (thread
	// queues, sparse set, ~10 KB for 100-state NFA) is allocated on first search,
	// not at SearchState creation. For strategies like UseCharClassSearcher or
	// UseTeddy, the PikeVM is rarely invoked, so this saves significant memory
	// in WAF workloads with ~900 patterns.
	state.pikevm = nfa.NewPikeVMLazy(cfg.nfaEngine)

	// Backtracker state: only allocate for strategies that use it.
	// Strategies: UseBoundedBacktracker, UseNFA (small NFA fallback BT).
	// Also needed by strategies with DFA that may overflow to BT.
	switch cfg.strategy {
	case UseBoundedBacktracker, UseNFA, UseDFA, UseBoth, UseDigitPrefilter:
		state.backtracker = nfa.NewBacktrackerState()
	}

	// Forward DFA cache: only if a forward DFA was compiled AND strategy uses it.
	if cfg.forwardDFA != nil {
		switch cfg.strategy {
		case UseDFA, UseBoth, UseDigitPrefilter, UseBoundedBacktracker:
			state.dfaCache = cfg.forwardDFA.NewCache()
		}
	}

	// Reverse DFA cache: only for bidirectional search strategies.
	if cfg.reverseDFA != nil {
		switch cfg.strategy {
		case UseDFA, UseBoundedBacktracker:
			state.revDFACache = cfg.reverseDFA.NewCache()
		}
	}

	// Strategy-specific DFA caches: only for reverse-search strategies.
	if cfg.stratFwdDFA != nil {
		switch cfg.strategy {
		case UseReverseSuffix, UseReverseInner, UseReverseSuffixSet, UseMultilineReverseSuffix:
			state.stratFwdCache = cfg.stratFwdDFA.NewCache()
		}
	}
	if cfg.stratRevDFA != nil {
		switch cfg.strategy {
		case UseReverseSuffix, UseReverseInner, UseReverseSuffixSet, UseReverseAnchored:
			state.stratRevCache = cfg.stratRevDFA.NewCache()
		}
	}

	// OnePass slots: only if OnePass DFA was compiled and captures exist.
	if cfg.hasOnePass && cfg.numCaptures > 0 {
		state.onepassSlots = make([]int, cfg.numCaptures*2)
		state.onepassCache = onepass.NewCache(cfg.numCaptures)
	}

	return state
}

// reset prepares the SearchState for reuse.
// Called when returning state to the pool.
func (s *SearchState) reset() {
	// Reset backtracker state
	if s.backtracker != nil {
		// IMPORTANT: Do NOT reset Generation here!
		// The generation counter must keep incrementing to ensure the Visited array
		// doesn't contain stale entries from previous searches. The BoundedBacktracker.reset()
		// will increment it before each search, so we just need to preserve its current value.
		// Only reset InputLen and Longest.
		s.backtracker.InputLen = 0
		s.backtracker.Longest = false
	}

	// PikeVM reset is handled internally when search begins

	// Reset onepass slots to -1 (unmatched)
	for i := range s.onepassSlots {
		s.onepassSlots[i] = -1
	}
}

// searchStatePool manages a pool of SearchState instances for thread-safe reuse.
// This follows the stdlib regexp pattern of using sync.Pool for concurrent safety.
type searchStatePool struct {
	pool sync.Pool
	cfg  searchStateConfig
}

// newSearchStatePool creates a pool configured for the given engine parameters.
func newSearchStatePool(cfg searchStateConfig) *searchStatePool {
	p := &searchStatePool{cfg: cfg}
	p.pool = sync.Pool{
		New: func() any {
			return newSearchState(p.cfg)
		},
	}
	return p
}

// get retrieves a SearchState from the pool, creating one if necessary.
// Note: The longest flag is set by Engine.getSearchState() using e.longest,
// not here, because it may change between searches on the same Engine.
func (p *searchStatePool) get() *SearchState {
	return p.pool.Get().(*SearchState)
}

// put returns a SearchState to the pool for reuse.
func (p *searchStatePool) put(state *SearchState) {
	if state == nil {
		return
	}
	state.reset()
	p.pool.Put(state)
}
