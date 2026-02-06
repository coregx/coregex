package lazy

import (
	"sync"

	"github.com/coregx/coregex/internal/conv"
)

// Cache provides thread-safe storage for DFA states with bounded memory.
//
// The cache maps StateKey (NFA state set hash) → DFA State.
// When the cache reaches maxStates, it can be cleared and rebuilt
// (up to a configured limit) before falling back to NFA.
//
// Thread safety: All methods are safe for concurrent access via RWMutex.
//
// Memory management:
//   - States are never evicted individually (no LRU overhead)
//   - When cache is full, it is cleared entirely and search continues
//   - After too many clears, falls back to NFA
//   - Clearing keeps allocated memory to avoid re-allocation
type Cache struct {
	// mu protects all fields below
	// RWMutex allows concurrent reads (common case during search)
	mu sync.RWMutex

	// states maps StateKey → DFA State
	states map[StateKey]*State

	// maxStates is the capacity limit
	maxStates uint32

	// nextID is the next available state ID
	// Start at 1 (0 is reserved for StartState)
	nextID StateID

	// clearCount tracks how many times the cache has been cleared during
	// the current search. This is used to detect pathological cache thrashing
	// and trigger NFA fallback when clears exceed the configured limit.
	// Inspired by Rust regex-automata's hybrid DFA cache clearing strategy.
	clearCount int

	// Statistics for cache performance tuning
	hits   uint64 // Number of cache hits
	misses uint64 // Number of cache misses
}

// NewCache creates a new state cache with the given maximum capacity
func NewCache(maxStates uint32) *Cache {
	return &Cache{
		states:    make(map[StateKey]*State, maxStates),
		maxStates: maxStates,
		nextID:    StartState + 1, // StartState is 0, start from 1
		hits:      0,
		misses:    0,
	}
}

// Get retrieves a state by its key.
// Returns (state, true) if found, (nil, false) if not in cache.
//
// This method uses RLock for read-only access, allowing concurrent Gets.
func (c *Cache) Get(key StateKey) (*State, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, ok := c.states[key]
	if ok {
		c.hits++ // Tracked under read lock (slight race, but acceptable for stats)
	}
	return state, ok
}

// Insert adds a new state to the cache and returns its assigned ID.
// Returns (stateID, nil) on success.
// Returns (InvalidState, ErrCacheFull) if cache is at capacity.
//
// Thread-safe: uses write lock.
func (c *Cache) Insert(key StateKey, state *State) (StateID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists (another goroutine may have inserted it)
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

	return state.ID(), nil
}

// GetOrInsert retrieves a state from cache or inserts it if not present.
// This is the primary method used during DFA construction.
//
// Returns:
//   - (state, true) if state was already in cache (cache hit)
//   - (state, false) if state was just inserted (cache miss)
//   - (nil, false) with ErrCacheFull if cache is full
//
// Thread-safe: uses appropriate locks.
func (c *Cache) GetOrInsert(key StateKey, state *State) (*State, bool, error) {
	// Fast path: check if exists (read lock)
	if existing, ok := c.Get(key); ok {
		return existing, true, nil
	}

	// Slow path: insert (write lock)
	stateID, err := c.Insert(key, state)
	if err != nil {
		return nil, false, err
	}

	// Retrieve the inserted state (it now has a valid ID)
	c.mu.RLock()
	insertedState := c.states[key]
	c.mu.RUnlock()

	// Verify ID was assigned
	if insertedState.ID() != stateID {
		// Sanity check - should never happen
		panic("cache state ID mismatch")
	}

	return insertedState, false, nil
}

// Size returns the current number of states in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.states)
}

// IsFull returns true if the cache has reached its maximum capacity
func (c *Cache) IsFull() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return conv.IntToUint32(len(c.states)) >= c.maxStates
}

// Stats returns cache hit/miss statistics.
// Returns (hits, misses, hitRate).
//
// Hit rate = hits / (hits + misses)
// A high hit rate (>90%) indicates good cache sizing.
func (c *Cache) Stats() (hits, misses uint64, hitRate float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hits = c.hits
	misses = c.misses
	total := hits + misses
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return hits, misses, hitRate
}

// ResetStats resets hit/miss counters (useful for benchmarking)
func (c *Cache) ResetStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits = 0
	c.misses = 0
}

// Clear removes all states from the cache and resets statistics.
// This also resets the clear counter. Primarily for testing.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear map (GC will reclaim memory)
	c.states = make(map[StateKey]*State, c.maxStates)
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
//
// After calling this, all previously returned *State pointers are stale
// and must not be used. The caller must re-obtain the start state.
//
// Inspired by Rust regex-automata's cache clearing strategy (hybrid/dfa.rs).
func (c *Cache) ClearKeepMemory() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear the map using Go's optimized clear-by-range idiom.
	// This reuses the map's internal memory (buckets) instead of reallocating.
	for k := range c.states {
		delete(c.states, k)
	}
	c.nextID = StartState + 1
	c.clearCount++
}

// ClearCount returns how many times the cache has been cleared.
// Used to check against the MaxCacheClears limit.
func (c *Cache) ClearCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clearCount
}

// ResetClearCount resets the clear counter to zero.
// Called at the start of each new search to give the DFA a fresh budget.
func (c *Cache) ResetClearCount() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearCount = 0
}
