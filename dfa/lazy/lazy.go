// Package lazy implements a Lazy DFA (Deterministic Finite Automaton) engine
// for regex matching.
//
// The Lazy DFA constructs DFA states on-demand during matching, rather than
// building the complete DFA upfront. This provides:
//   - Fast matching: O(n) time complexity (linear in input length)
//   - Bounded memory: States are cached with a configurable limit
//   - Graceful degradation: Falls back to NFA when cache is full
//
// The lazy approach is ideal for:
//   - Patterns with many potential states (avoids exponential blowup)
//   - Real-world regex where most states are never visited
//   - Memory-constrained environments
//
// Example usage:
//
//	// Compile pattern to DFA
//	dfa, err := lazy.CompilePattern("(foo|bar)\\d+")
//	if err != nil {
//	    return err
//	}
//
//	// Search for match
//	input := []byte("test foo123 end")
//	pos := dfa.Find(input)
//	if pos != -1 {
//	    fmt.Printf("Match found at position %d\n", pos)
//	}
//
//	// Check if matches (boolean)
//	if dfa.IsMatch(input) {
//	    fmt.Println("Input matches pattern")
//	}
package lazy

import (
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// DFA is a Lazy DFA engine that performs on-demand determinization.
//
// The DFA maintains:
//   - An NFA (the source automaton)
//   - A cache of determinized states
//   - An optional prefilter for fast candidate finding
//   - A PikeVM for NFA fallback
//
// Thread safety: Not thread-safe. Each goroutine should use its own DFA instance.
// The underlying NFA can be shared (it's immutable), but the DFA's cache and
// state are mutable during search.
type DFA struct {
	nfa       *nfa.NFA
	cache     *Cache
	config    Config
	prefilter prefilter.Prefilter
	pikevm    *nfa.PikeVM

	// stateByID provides O(1) lookup of states by ID
	// This maps StateID → *State for fast access during search
	stateByID map[StateID]*State
}

// Find returns the index of the first match in the haystack, or -1 if no match.
//
// The search algorithm:
//  1. If prefilter available: use it to find candidate positions
//  2. For each candidate (or entire input if no prefilter):
//     a. Start from DFA start state
//     b. For each input byte, determinize on-demand if needed
//     c. If match state reached: return position
//     d. If cache full: fall back to NFA
//
// Time complexity:
//   - Best case: O(n) with prefilter hit
//   - Typical case: O(n) DFA search
//   - Worst case: O(m*n) NFA fallback (m = pattern size)
//
// Example:
//
//	dfa, _ := lazy.CompilePattern("hello")
//	pos := dfa.Find([]byte("say hello world"))
//	// pos == 4
func (d *DFA) Find(haystack []byte) int {
	if len(haystack) == 0 {
		// Check if empty string matches
		if d.matchesEmpty() {
			return 0
		}
		return -1
	}

	// If prefilter available, use it to find candidates
	if d.prefilter != nil {
		return d.findWithPrefilter(haystack)
	}

	// No prefilter: try DFA search from position 0
	// If unanchored, try each starting position
	if d.nfa.IsAnchored() {
		// Anchored: only try position 0
		if pos := d.searchAt(haystack, 0); pos != -1 {
			return pos
		}
		return -1
	}

	// Unanchored: try each starting position
	for start := 0; start <= len(haystack); start++ {
		if pos := d.searchAt(haystack, start); pos != -1 {
			return pos
		}
	}

	return -1
}

// IsMatch returns true if the pattern matches anywhere in the haystack.
// This is equivalent to Find(haystack) != -1 but may be optimized.
func (d *DFA) IsMatch(haystack []byte) bool {
	return d.Find(haystack) != -1
}

// findWithPrefilter searches using prefilter to find candidates.
// For each candidate position, verify with DFA or NFA.
func (d *DFA) findWithPrefilter(haystack []byte) int {
	// If prefilter is complete, its match is the final match
	if d.prefilter.IsComplete() {
		return d.prefilter.Find(haystack, 0)
	}

	// Find first candidate with prefilter
	pos := 0
	for {
		candidate := d.prefilter.Find(haystack, pos)
		if candidate == -1 {
			// No more candidates
			return -1
		}

		// Verify candidate with DFA
		// Try starting from candidate position
		matchPos := d.searchAt(haystack, candidate)
		if matchPos != -1 {
			return matchPos
		}

		// Not a match, continue after this candidate
		pos = candidate + 1
	}
}

// searchAt attempts to find a match starting at the given position.
// Returns the end position of the match, or -1 if no match.
//
// This is the core DFA search algorithm with lazy determinization.
func (d *DFA) searchAt(haystack []byte, startPos int) int {
	if startPos > len(haystack) {
		return -1
	}

	// Start from DFA start state
	currentState := d.getState(StartState)
	if currentState == nil {
		// Start state not in cache? This should never happen
		return d.nfaFallback(haystack, startPos)
	}

	// Scan input byte by byte
	pos := startPos
	for pos < len(haystack) {
		b := haystack[pos]

		// Check if current state has a transition for this byte
		nextID, ok := currentState.Transition(b)
		if !ok {
			// No cached transition: determinize on-demand
			nextState, err := d.determinize(currentState, b)
			if err != nil {
				// Cache full or other error: fall back to NFA
				return d.nfaFallback(haystack, startPos)
			}

			// Check for dead state (no possible transitions)
			if nextState == nil {
				// Dead state: no match possible from here
				return -1
			}

			currentState = nextState
		} else {
			// Cached transition: follow it
			currentState = d.getState(nextID)
			if currentState == nil {
				// State not in cache? Shouldn't happen, fall back
				return d.nfaFallback(haystack, startPos)
			}
		}

		pos++

		// Check if we reached a match state
		if currentState.IsMatch() {
			return pos // Return end position of match
		}
	}

	// Reached end of input - check if current state is match
	if currentState.IsMatch() {
		return pos
	}

	return -1
}

// determinize creates a new DFA state for the given state + input byte.
// This is the on-demand state construction that makes the DFA "lazy".
//
// Algorithm:
//  1. Take current DFA state's NFA state set
//  2. Compute move(state set, byte) → new NFA state set
//  3. Check if this state is already cached (by hash key)
//  4. If not cached: create new DFA state and cache it
//  5. Add transition to current state
//
// Returns (nil, nil) if no transition is possible (dead state).
// Returns (nil, error) if cache is full.
func (d *DFA) determinize(current *State, b byte) (*State, error) {
	// Need builder for move operations
	builder := NewBuilder(d.nfa, d.config)

	// Compute next NFA state set via move operation
	nextNFAStates := builder.move(current.NFAStates(), b)

	// No transitions on this byte → dead state
	if len(nextNFAStates) == 0 {
		// Cache the dead state transition to avoid re-computation
		current.AddTransition(b, DeadState)
		// Return nil state with a specific error to indicate dead state
		return nil, &DFAError{Kind: NFAFallback, Message: "dead state (no transitions)"}
	}

	// Check if we've exceeded determinization limit
	if len(nextNFAStates) > d.config.DeterminizationLimit {
		// Too many NFA states: fall back to avoid exponential blowup
		return nil, &DFAError{
			Kind:    StateLimitExceeded,
			Message: "determinization limit exceeded",
		}
	}

	// Compute state key for caching
	key := ComputeStateKey(nextNFAStates)

	// Check if state already exists in cache
	if existing, ok := d.cache.Get(key); ok {
		// Cache hit: reuse existing state
		current.AddTransition(b, existing.ID())
		return existing, nil
	}

	// Create new DFA state
	isMatch := builder.containsMatchState(nextNFAStates)
	newState := NewState(InvalidState, nextNFAStates, isMatch) // ID assigned by cache

	// Insert into cache
	_, err := d.cache.Insert(key, newState)
	if err != nil {
		// Cache full
		return nil, err
	}

	// Register state in ID lookup map
	d.registerState(newState)

	// Add transition from current state to new state
	current.AddTransition(b, newState.ID())

	return newState, nil
}

// getState retrieves a state from the cache by ID
func (d *DFA) getState(id StateID) *State {
	// Special case: dead state
	if id == DeadState {
		return nil
	}

	// O(1) lookup via stateByID map
	state, ok := d.stateByID[id]
	if !ok {
		return nil
	}
	return state
}

// registerState adds a state to the ID-based lookup map
func (d *DFA) registerState(state *State) {
	d.stateByID[state.ID()] = state
}

// nfaFallback executes the NFA (PikeVM) when DFA gives up.
// This ensures correctness even when cache is full or pattern is too complex.
func (d *DFA) nfaFallback(haystack []byte, startPos int) int {
	// Search from startPos to end
	start, end, matched := d.pikevm.Search(haystack[startPos:])
	if !matched {
		return -1
	}

	// PikeVM returns positions relative to the slice
	// Adjust to absolute positions
	_ = start // Start position relative to startPos
	return startPos + end
}

// matchesEmpty checks if the pattern matches an empty string
func (d *DFA) matchesEmpty() bool {
	// Check if start state is a match state
	startState := d.getState(StartState)
	if startState != nil && startState.IsMatch() {
		return true
	}

	// Fall back to NFA for empty match check
	start, end, matched := d.pikevm.Search([]byte{})
	return matched && start == 0 && end == 0
}

// CacheStats returns statistics about the DFA cache.
// Useful for performance tuning and diagnostics.
//
// Returns (size, capacity, hits, misses, hitRate).
func (d *DFA) CacheStats() (size int, capacity uint32, hits, misses uint64, hitRate float64) {
	size = d.cache.Size()
	capacity = d.config.MaxStates
	hits, misses, hitRate = d.cache.Stats()
	return
}

// ResetCache clears the DFA cache and statistics.
// This forces all states to be recomputed on the next search.
// Primarily useful for testing and benchmarking.
func (d *DFA) ResetCache() {
	d.cache.Clear()
	d.stateByID = make(map[StateID]*State, d.config.MaxStates)

	// Recreate start state
	builder := NewBuilder(d.nfa, d.config)
	startStateSet := builder.epsilonClosure([]nfa.StateID{d.nfa.Start()})
	isMatch := builder.containsMatchState(startStateSet)
	startState := NewState(StartState, startStateSet, isMatch)
	key := ComputeStateKey(startStateSet)
	_, _ = d.cache.Insert(key, startState) // Ignore error (cache is empty)
	d.registerState(startState)
}
