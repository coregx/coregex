package lazy

import (
	"github.com/coregx/coregex/internal/conv"
)

// DFACache holds mutable state for DFA search operations.
//
// The DFACache is the mutable counterpart to the immutable DFA struct.
// After DFA compilation, the DFA is fully immutable and safe to share
// across goroutines. Each goroutine creates its own DFACache via
// DFA.NewCache() (or reuses one from a sync.Pool in the meta layer).
//
// This separation is inspired by the Rust regex crate's approach where
// the DFA configuration is immutable and per-thread cache is mutable.
//
// The cache maps StateKey (NFA state set hash) -> DFA State.
// When the cache reaches maxStates, it can be cleared and rebuilt
// (up to a configured limit) before falling back to NFA.
//
// Thread safety: NOT thread-safe. Each DFACache must be owned by a single
// goroutine. No mutex is needed because there is no concurrent access.
//
// Memory management:
//   - States are never evicted individually (no LRU overhead)
//   - When cache is full, it is cleared entirely and search continues
//   - After too many clears, falls back to NFA
//   - Clearing keeps allocated memory to avoid re-allocation
type DFACache struct {
	// states maps StateKey -> DFA State (used only in determinize slow path)
	states map[StateKey]*State

	// stateList provides O(1) lookup of State structs by ID.
	// Used only in slow path (determinize, word boundary, acceleration).
	// Hot loop uses flatTrans + matchFlags instead.
	stateList []*State

	// --- Flat transition table (Rust approach) ---
	// Hot loop uses ONLY these fields — no *State pointer chase.
	//
	// Rust: cache.trans[sid + class] — single flat array, premultiplied ID.
	// We use: flatTrans[int(sid)*stride + class] — same layout.
	//
	// This replaces per-state State.transitions[] in the hot loop:
	// ONE slice access instead of TWO pointer chases (stateList → State → transitions).

	// flatTrans is the flat transition table.
	// Layout: [state0_c0, state0_c1, ..., state0_cN, state1_c0, ...]
	// InvalidState (0xFFFFFFFF) = unknown transition (needs determinize).
	flatTrans []StateID

	// matchFlags[stateID] = true if state is a match/accepting state.
	// Replaces State.IsMatch() in hot loop — no pointer chase needed.
	matchFlags []bool

	// stride is the number of byte equivalence classes (alphabet size).
	stride int

	// startTable caches start states for different look-behind contexts.
	startTable StartTable

	// maxStates is the capacity limit
	maxStates uint32

	// nextID is the next available state ID.
	nextID StateID

	// clearCount tracks cache clear count for NFA fallback threshold.
	clearCount int

	// Statistics
	hits   uint64
	misses uint64
}

// Get retrieves a state by its key.
// Returns (state, true) if found, (nil, false) if not in cache.
func (c *DFACache) Get(key StateKey) (*State, bool) {
	state, ok := c.states[key]
	if ok {
		c.hits++
	}
	return state, ok
}

// Insert adds a new state to the cache and returns its assigned ID.
// Returns (stateID, nil) on success.
// Returns (InvalidState, ErrCacheFull) if cache is at capacity.
func (c *DFACache) Insert(key StateKey, state *State) (StateID, error) {
	// Check if already exists
	if existing, ok := c.states[key]; ok {
		c.hits++
		return existing.ID(), nil
	}

	// Check capacity
	if conv.IntToUint32(len(c.states)) >= c.maxStates {
		c.misses++
		return InvalidState, ErrCacheFull
	}

	// Assign state ID only if not already set (e.g., StartState = 0)
	if state.id == InvalidState {
		state.id = c.nextID
		c.nextID++
	}

	// Insert into cache
	c.states[key] = state
	c.misses++

	// Grow flat transition table for this state's row (all InvalidState initially).
	if c.stride > 0 {
		sid := int(state.id)
		needed := (sid + 1) * c.stride
		if needed > len(c.flatTrans) {
			growth := needed - len(c.flatTrans)
			for i := 0; i < growth; i++ {
				c.flatTrans = append(c.flatTrans, InvalidState)
			}
		}
		// Grow matchFlags
		for len(c.matchFlags) <= sid {
			c.matchFlags = append(c.matchFlags, false)
		}
		c.matchFlags[sid] = state.isMatch
	}

	return state.ID(), nil
}

// SetFlatTransition records a transition in the flat table.
// Called from determinize when a transition is computed.
func (c *DFACache) SetFlatTransition(fromID StateID, classIdx int, toID StateID) {
	offset := int(fromID)*c.stride + classIdx
	if offset < len(c.flatTrans) {
		c.flatTrans[offset] = toID
	}
}

// FlatNext returns the next state ID from the flat table.
// Returns InvalidState if the transition hasn't been computed yet.
// This is the hot-path function — should be inlined by the compiler.
func (c *DFACache) FlatNext(sid StateID, classIdx int) StateID {
	offset := int(sid)*c.stride + classIdx
	return c.flatTrans[offset]
}

// IsMatchState returns whether the given state ID is a match state.
// Uses compact matchFlags slice — no pointer chase.
func (c *DFACache) IsMatchState(sid StateID) bool {
	if sid >= DeadState {
		return false
	}
	id := int(sid)
	if id >= len(c.matchFlags) {
		return false
	}
	return c.matchFlags[id]
}

// GetOrInsert retrieves a state from cache or inserts it if not present.
// This is the primary method used during DFA construction.
//
// Returns:
//   - (state, true) if state was already in cache (cache hit)
//   - (state, false) if state was just inserted (cache miss)
//   - (nil, false) with ErrCacheFull if cache is full
func (c *DFACache) GetOrInsert(key StateKey, state *State) (*State, bool, error) {
	// Check if exists
	if existing, ok := c.Get(key); ok {
		return existing, true, nil
	}

	// Insert
	stateID, err := c.Insert(key, state)
	if err != nil {
		return nil, false, err
	}

	// Retrieve the inserted state (it now has a valid ID)
	insertedState := c.states[key]

	// Verify ID was assigned
	if insertedState.ID() != stateID {
		// Sanity check - should never happen
		panic("cache state ID mismatch")
	}

	return insertedState, false, nil
}

// Size returns the current number of states in the cache.
func (c *DFACache) Size() int {
	return len(c.states)
}

// IsFull returns true if the cache has reached its maximum capacity.
func (c *DFACache) IsFull() bool {
	return conv.IntToUint32(len(c.states)) >= c.maxStates
}

// Stats returns cache hit/miss statistics.
// Returns (hits, misses, hitRate).
//
// Hit rate = hits / (hits + misses)
// A high hit rate (>90%) indicates good cache sizing.
func (c *DFACache) Stats() (hits, misses uint64, hitRate float64) {
	hits = c.hits
	misses = c.misses
	total := hits + misses
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return hits, misses, hitRate
}

// ResetStats resets hit/miss counters (useful for benchmarking).
func (c *DFACache) ResetStats() {
	c.hits = 0
	c.misses = 0
}

// Clear removes all states from the cache and resets statistics.
// This also resets the clear counter. Primarily for testing.
func (c *DFACache) Clear() {
	// Clear map (GC will reclaim memory)
	c.states = make(map[StateKey]*State, c.maxStates)
	c.stateList = c.stateList[:0]
	c.startTable = newStartTableFromByteMap(&c.startTable.byteMap)
	c.nextID = StartState + 1
	c.clearCount = 0
	c.hits = 0
	c.misses = 0
}

// ClearKeepMemory clears all states from the cache but keeps the allocated
// map memory for reuse and increments the clear counter. This is used during
// search when the cache is full: instead of falling back to NFA permanently,
// we clear the cache and continue DFA search, rebuilding states on demand.
//
// Unlike Clear(), this method:
//   - Increments clearCount (tracks clears during a search)
//   - Does NOT reset hit/miss statistics (they accumulate across clears)
//   - Reuses map memory via Go's map clearing optimization
//   - Resets stateList but keeps allocated capacity
//   - Resets startTable initialized flags
//
// After calling this, all previously returned *State pointers are stale
// and must not be used. The caller must re-obtain the start state.
//
// Inspired by Rust regex-automata's cache clearing strategy (hybrid/dfa.rs).
func (c *DFACache) ClearKeepMemory() {
	// Clear the map using Go's optimized clear-by-range idiom.
	// This reuses the map's internal memory (buckets) instead of reallocating.
	for k := range c.states {
		delete(c.states, k)
	}
	c.stateList = c.stateList[:0]
	c.startTable = newStartTableFromByteMap(&c.startTable.byteMap)
	c.nextID = StartState + 1
	c.clearCount++
}

// ClearCount returns how many times the cache has been cleared.
// Used to check against the MaxCacheClears limit.
func (c *DFACache) ClearCount() int {
	return c.clearCount
}

// ResetClearCount resets the clear counter to zero.
// Called at the start of each new search to give the DFA a fresh budget.
func (c *DFACache) ResetClearCount() {
	c.clearCount = 0
}

// getState retrieves a state from the stateList by ID.
func (c *DFACache) getState(id StateID) *State {
	if id == DeadState {
		return nil
	}

	// Guard against special state IDs (DeadState=0xFFFFFFFE, InvalidState=0xFFFFFFFF).
	// On 386, int(uint32(0xFFFFFFFF)) = -1, causing negative index panic.
	if id >= DeadState {
		return nil
	}
	idx := int(id)
	if idx >= len(c.stateList) {
		return nil
	}
	return c.stateList[idx]
}

// registerState adds a state to the stateList for O(1) lookup by ID.
// StateIDs are assigned sequentially, so we can use direct indexing.
func (c *DFACache) registerState(state *State) {
	id := int(state.ID())
	// Grow slice if needed
	for len(c.stateList) <= id {
		c.stateList = append(c.stateList, nil)
	}
	c.stateList[id] = state
}

// Reset prepares the cache for reuse from a sync.Pool.
// Unlike Clear(), this preserves allocated memory in slices and maps
// for efficient reuse. The startTable byteMap is preserved (immutable).
func (c *DFACache) Reset() {
	// Clear map entries but keep bucket memory
	for k := range c.states {
		delete(c.states, k)
	}
	c.stateList = c.stateList[:0]
	c.startTable = newStartTableFromByteMap(&c.startTable.byteMap)
	c.nextID = StartState + 1
	c.clearCount = 0
	c.hits = 0
	c.misses = 0
}
