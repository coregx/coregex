package meta

import (
	"sync"

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

	// onepassSlots holds capture slot storage for OnePass DFA searches.
	// Pre-allocated to avoid allocation per search.
	onepassSlots []int

	// onepassCache is the cache for OnePass DFA searches.
	onepassCache *onepass.Cache
}

// newSearchState creates a new SearchState with pre-allocated buffers.
// nfaEngine is required to create a new PikeVM instance.
func newSearchState(nfaEngine *nfa.NFA, numCaptures int) *SearchState {
	state := &SearchState{
		backtracker: nfa.NewBacktrackerState(),
		pikevm:      nfa.NewPikeVM(nfaEngine), // Create per-search PikeVM instance
	}

	// Pre-allocate onepass slots if captures are present
	if numCaptures > 0 {
		state.onepassSlots = make([]int, numCaptures*2)
		state.onepassCache = onepass.NewCache(numCaptures)
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

	// Configuration for creating new states
	nfaEngine   *nfa.NFA
	numCaptures int
}

// newSearchStatePool creates a pool configured for the given engine parameters.
func newSearchStatePool(nfaEngine *nfa.NFA, numCaptures int) *searchStatePool {
	p := &searchStatePool{
		nfaEngine:   nfaEngine,
		numCaptures: numCaptures,
	}
	p.pool = sync.Pool{
		New: func() any {
			return newSearchState(p.nfaEngine, p.numCaptures)
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
