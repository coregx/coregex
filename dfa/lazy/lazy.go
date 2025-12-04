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
	"github.com/coregx/coregex/simd"
)

// DFA is a Lazy DFA engine that performs on-demand determinization.
//
// The DFA maintains:
//   - An NFA (the source automaton)
//   - A cache of determinized states
//   - An optional prefilter for fast candidate finding
//   - A PikeVM for NFA fallback
//   - A StartTable for caching start states by look-behind context
//   - ByteClasses for alphabet reduction (used by advanced optimizations)
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

	// startTable caches start states for different look-behind contexts
	// This enables correct handling of assertions (^, \b, etc.) and
	// avoids recomputing epsilon closures on every search
	startTable *StartTable

	// byteClasses maps bytes to equivalence classes for alphabet reduction.
	// Bytes in the same class have identical transitions in all DFA states.
	// This enables memory optimization from 256 to ~8-16 transitions per state.
	byteClasses *nfa.ByteClasses
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

	// DEBUG: Print which path we're taking
	// import "fmt"  // ADD THIS TO TOP OF FILE
	//if d.prefilter != nil {
	//	fmt.Printf("[DFA.Find] using findWithPrefilter\n")
	//} else {
	//	fmt.Printf("[DFA.Find] NO PREFILTER, using searchAt\n")
	//}

	// If prefilter available, use it to find candidates
	if d.prefilter != nil {
		return d.findWithPrefilter(haystack)
	}

	// No prefilter: use DFA search from position 0
	// The NFA now has proper unanchored start state with implicit (?s:.)*? prefix,
	// so DFA search is O(n) for both anchored and unanchored patterns
	return d.searchAt(haystack, 0)
}

// IsMatch returns true if the pattern matches anywhere in the haystack.
//
// This is optimized for early termination: returns true as soon as any
// match state is reached, without continuing to find leftmost-longest.
// This provides 2-10x speedup compared to Find() for boolean queries.
//
// Example:
//
//	if dfa.IsMatch([]byte("test foo123 end")) {
//	    fmt.Println("Pattern matches!")
//	}
func (d *DFA) IsMatch(haystack []byte) bool {
	if len(haystack) == 0 {
		return d.matchesEmpty()
	}

	// Use prefilter for acceleration if available
	if d.prefilter != nil {
		return d.isMatchWithPrefilter(haystack)
	}

	// No prefilter: use optimized DFA search with early termination
	return d.searchEarliestMatch(haystack, 0)
}

// isMatchWithPrefilter uses prefilter for fast boolean match.
// Returns as soon as any match is found.
func (d *DFA) isMatchWithPrefilter(haystack []byte) bool {
	// If prefilter is complete, its match is sufficient
	if d.prefilter.IsComplete() {
		return d.prefilter.Find(haystack, 0) != -1
	}

	// Find first candidate
	pos := d.prefilter.Find(haystack, 0)
	if pos == -1 {
		return false
	}

	// Try to match at candidate - use early termination
	if d.searchEarliestMatch(haystack, pos) {
		return true
	}

	// Continue searching from next position
	for pos < len(haystack) {
		pos++
		candidate := d.prefilter.Find(haystack, pos)
		if candidate == -1 {
			return false
		}
		pos = candidate
		if d.searchEarliestMatch(haystack, pos) {
			return true
		}
	}

	return false
}

// searchEarliestMatch performs DFA search with early termination.
// Returns true as soon as any match state is reached.
// This is faster than searchAt because it doesn't track match positions
// or enforce leftmost-longest semantics.
func (d *DFA) searchEarliestMatch(haystack []byte, startPos int) bool {
	if startPos > len(haystack) {
		return false
	}

	// Get context-aware start state
	currentState := d.getStartStateForUnanchored(haystack, startPos)
	if currentState == nil {
		// Fallback to NFA
		start, end, matched := d.pikevm.Search(haystack[startPos:])
		return matched && start >= 0 && end >= start
	}

	// Check if start state is already a match
	if currentState.IsMatch() {
		return true
	}

	// Scan input byte by byte with early termination
	for pos := startPos; pos < len(haystack); {
		// Try lazy acceleration detection if not yet checked
		d.tryDetectAcceleration(currentState)

		// State acceleration: if current state is accelerable, use SIMD to skip ahead
		if exitBytes := currentState.AccelExitBytes(); len(exitBytes) > 0 {
			nextPos := d.accelerate(haystack, pos, exitBytes)
			if nextPos == -1 {
				// No exit byte found - can't match
				return false
			}
			// Skip to the exit byte position
			pos = nextPos
		}

		b := haystack[pos]

		// Get next state
		nextID, ok := currentState.Transition(b)
		switch {
		case !ok:
			// Determinize on demand
			nextState, err := d.determinize(currentState, b)
			if err != nil {
				// Dead state or cache full - try NFA fallback
				start, end, matched := d.pikevm.Search(haystack[pos:])
				return matched && start >= 0 && end >= start
			}
			if nextState == nil {
				// Dead state - no match possible from here
				return false
			}
			currentState = nextState

		case nextID == DeadState:
			// Dead state - no match possible from here
			return false

		default:
			currentState = d.getState(nextID)
			if currentState == nil {
				// State not in cache - fallback to NFA
				start, end, matched := d.pikevm.Search(haystack[pos:])
				return matched && start >= 0 && end >= start
			}
		}

		pos++

		// Early termination: return true immediately on any match
		if currentState.IsMatch() {
			return true
		}
	}

	// Reached end of input without finding a match
	return false
}

// findWithPrefilter searches using prefilter to accelerate unanchored search.
// Uses single-pass approach: when in start state, use prefilter to skip ahead.
// Returns the end position of the leftmost-longest match.
func (d *DFA) findWithPrefilter(haystack []byte) int {
	// If prefilter is complete, its match is the final match
	if d.prefilter.IsComplete() {
		return d.prefilter.Find(haystack, 0)
	}

	// Initial prefilter scan to find first candidate
	candidate := d.prefilter.Find(haystack, 0)
	if candidate == -1 {
		return -1
	}
	pos := candidate

	// Get start state based on look-behind context at candidate position
	currentState := d.getStartStateForUnanchored(haystack, pos)
	if currentState == nil {
		return d.nfaFallback(haystack, 0)
	}

	// Track last match position for leftmost-longest semantics
	lastMatch := -1
	committed := false // True once we've entered a match state

	if currentState.IsMatch() {
		lastMatch = pos // Empty match at start
		committed = true
	}

	for pos < len(haystack) {
		b := haystack[pos]

		// Get next state
		nextID, ok := currentState.Transition(b)
		var nextState *State
		switch {
		case !ok:
			// Determinize on demand
			var err error
			nextState, err = d.determinize(currentState, b)
			if err != nil {
				return d.nfaFallback(haystack, 0)
			}
			if nextState == nil {
				// Dead state - return last match if we had one
				if lastMatch != -1 {
					return lastMatch
				}
				// No match yet - find next candidate
				pos++
				candidate = d.prefilter.Find(haystack, pos)
				if candidate == -1 {
					return -1
				}
				pos = candidate
				// Get context-aware start state based on look-behind at new position
				currentState = d.getStartStateForUnanchored(haystack, pos)
				if currentState == nil {
					return d.nfaFallback(haystack, 0)
				}
				lastMatch = -1
				committed = false
				if currentState.IsMatch() {
					lastMatch = pos
					committed = true
				}
				continue
			}
		case nextID == DeadState:
			// Dead state - return last match if we had one
			if lastMatch != -1 {
				return lastMatch
			}
			// No match yet - find next candidate
			pos++
			candidate = d.prefilter.Find(haystack, pos)
			if candidate == -1 {
				return -1
			}
			pos = candidate
			// Get context-aware start state based on look-behind at new position
			currentState = d.getStartStateForUnanchored(haystack, pos)
			if currentState == nil {
				return d.nfaFallback(haystack, 0)
			}
			lastMatch = -1
			committed = false
			if currentState.IsMatch() {
				lastMatch = pos
				committed = true
			}
			continue
		default:
			nextState = d.getState(nextID)
			if nextState == nil {
				return d.nfaFallback(haystack, 0)
			}
		}

		pos++
		currentState = nextState

		// Track match state and enforce leftmost semantics
		if currentState.IsMatch() {
			lastMatch = pos
			committed = true
		} else if committed {
			// We were in a match but now we're not - return leftmost match
			return lastMatch
		}

		// If back in start state (unanchored prefix self-loop), use prefilter to skip
		// Only do this if we haven't committed to a match yet
		if !committed && currentState.ID() == StartState && pos < len(haystack) {
			candidate = d.prefilter.Find(haystack, pos)
			if candidate == -1 {
				return -1
			}
			if candidate > pos {
				pos = candidate
				// Stay in start state
			}
		}
	}

	// Reached end of input - return last match position
	return lastMatch
}

// searchAt attempts to find a match starting at the given position.
// Returns the end position of the leftmost-longest match, or -1 if no match.
//
// This is the core DFA search algorithm with lazy determinization.
// Uses leftmost-longest semantics: find the earliest match start, then extend greedily.
func (d *DFA) searchAt(haystack []byte, startPos int) int {
	if startPos > len(haystack) {
		return -1
	}

	// Get appropriate start state based on look-behind context
	// This enables correct handling of assertions like ^, \b, etc.
	currentState := d.getStartStateForUnanchored(haystack, startPos)
	if currentState == nil {
		// Start state not in cache? This should never happen
		return d.nfaFallback(haystack, startPos)
	}

	// Track last match position for leftmost-longest semantics
	lastMatch := -1
	committed := false // True once we've entered a match state

	if currentState.IsMatch() {
		lastMatch = startPos // Empty match at start
		committed = true
	}

	// Scan input byte by byte
	pos := startPos
	for pos < len(haystack) {
		// Try lazy acceleration detection if not yet checked
		d.tryDetectAcceleration(currentState)

		// State acceleration: if current state is accelerable, use SIMD to skip ahead
		if exitBytes := currentState.AccelExitBytes(); len(exitBytes) > 0 {
			nextPos := d.accelerate(haystack, pos, exitBytes)
			if nextPos == -1 {
				// No exit byte found in remainder - no match possible from here
				return lastMatch
			}
			// Skip to the exit byte position
			pos = nextPos
		}

		b := haystack[pos]

		// Check if current state has a transition for this byte
		nextID, ok := currentState.Transition(b)
		switch {
		case !ok:
			// No cached transition: determinize on-demand
			nextState, err := d.determinize(currentState, b)
			if err != nil {
				// Cache full or other error: fall back to NFA
				return d.nfaFallback(haystack, startPos)
			}

			// Check for dead state (no possible transitions)
			if nextState == nil {
				// Dead state: return last match (if any)
				return lastMatch
			}

			currentState = nextState
		case nextID == DeadState:
			// Cached dead state: return last match (if any)
			return lastMatch
		default:
			// Cached transition: follow it
			currentState = d.getState(nextID)
			if currentState == nil {
				// State not in cache? Shouldn't happen, fall back
				return d.nfaFallback(haystack, startPos)
			}
		}

		pos++

		// Track match state and enforce leftmost semantics
		if currentState.IsMatch() {
			lastMatch = pos
			committed = true
		} else if committed {
			// We were in a match but now we're not - return leftmost match
			// This ensures we don't keep searching for later matches
			return lastMatch
		}
	}

	// Reached end of input - return last match position
	return lastMatch
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

// getStartState returns the appropriate start state for the given position.
//
// The start state depends on:
//   - Position in haystack (0 = start of text)
//   - Previous byte (for word boundary, line boundary detection)
//   - Anchored flag (anchored patterns use different NFA start)
//
// Start states are cached in the StartTable for O(1) access.
// If not cached, the state is computed and stored for future use.
func (d *DFA) getStartState(haystack []byte, pos int, anchored bool) *State {
	// Determine start kind based on position and previous byte
	var kind StartKind
	if pos == 0 {
		kind = StartText
	} else {
		kind = d.startTable.GetKind(haystack[pos-1])
	}

	// Check if already cached in StartTable
	stateID := d.startTable.Get(kind, anchored)
	if stateID != InvalidState {
		return d.getState(stateID)
	}

	// Not cached - compute and store
	builder := NewBuilder(d.nfa, d.config)
	config := StartConfig{Kind: kind, Anchored: anchored}
	state, key := ComputeStartState(builder, d.nfa, config)

	// Try to insert into cache using GetOrInsert
	// This handles the case where another goroutine may have inserted it
	insertedState, existed, err := d.cache.GetOrInsert(key, state)
	if err != nil {
		// Cache full - return the computed state anyway
		// (it won't be cached, but search can continue)
		return state
	}

	// Register in ID lookup map (only if we inserted a new state)
	if !existed {
		d.registerState(insertedState)
	}

	// Cache in StartTable for fast lookup next time
	d.startTable.Set(kind, anchored, insertedState.ID())

	return insertedState
}

// getStartStateForUnanchored is a convenience method for unanchored search.
// This is the common case for Find() operations.
func (d *DFA) getStartStateForUnanchored(haystack []byte, pos int) *State {
	return d.getStartState(haystack, pos, false)
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

// tryDetectAcceleration attempts lazy acceleration detection for a state.
// This is called when a state has enough cached transitions to detect reliably.
// It only runs once per state (tracked via AccelChecked flag).
func (d *DFA) tryDetectAcceleration(state *State) {
	if state == nil || state.AccelChecked() {
		return
	}

	// Try lazy detection from cached transitions
	if exitBytes := DetectAccelerationFromCached(state); len(exitBytes) > 0 {
		state.SetAccelBytes(exitBytes)
	} else {
		state.MarkAccelChecked()
	}
}

// accelerate uses SIMD to skip ahead in the input when in an accelerable state.
//
// An accelerable state has 1-3 "exit bytes" - the only bytes that can transition
// to a different state. All other bytes either loop back to self or go dead.
//
// This uses memchr/memchr2/memchr3 to find the next exit byte position,
// allowing us to skip large portions of input that would just self-loop.
//
// Returns the position of the next exit byte, or -1 if none found.
func (d *DFA) accelerate(haystack []byte, pos int, exitBytes []byte) int {
	if pos >= len(haystack) {
		return -1
	}

	remaining := haystack[pos:]
	var found int

	switch len(exitBytes) {
	case 1:
		found = simd.Memchr(remaining, exitBytes[0])
	case 2:
		found = simd.Memchr2(remaining, exitBytes[0], exitBytes[1])
	case 3:
		found = simd.Memchr3(remaining, exitBytes[0], exitBytes[1], exitBytes[2])
	default:
		return pos // Not accelerable, stay at current position
	}

	if found == -1 {
		return -1
	}

	return pos + found
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

	// Reset StartTable
	d.startTable = NewStartTable()

	// Recreate start state using unanchored start (with implicit (?s:.)*? prefix)
	// Use StartText look assertions (both \A and ^ are satisfied at position 0)
	builder := NewBuilder(d.nfa, d.config)
	startLook := LookSetFromStartKind(StartText)
	startStateSet := builder.epsilonClosure([]nfa.StateID{d.nfa.StartUnanchored()}, startLook)
	isMatch := builder.containsMatchState(startStateSet)
	startState := NewState(StartState, startStateSet, isMatch)
	key := ComputeStateKey(startStateSet)
	_, _ = d.cache.Insert(key, startState) // Ignore error (cache is empty)
	d.registerState(startState)

	// Cache the default start state in StartTable
	d.startTable.Set(StartText, false, startState.ID())
}

// ByteClasses returns the byte equivalence classes for this DFA.
// Bytes in the same class have identical transitions in all DFA states.
// This can be used for memory optimization (256 → ~8-16 classes).
//
// Returns nil if ByteClasses are not available (e.g., NFA without byte classes).
func (d *DFA) ByteClasses() *nfa.ByteClasses {
	return d.byteClasses
}

// AlphabetLen returns the number of equivalence classes in the alphabet.
// Returns 256 if ByteClasses are not available (no alphabet reduction).
func (d *DFA) AlphabetLen() int {
	if d.byteClasses == nil {
		return 256
	}
	return d.byteClasses.AlphabetLen()
}

// SearchReverse performs backward DFA search from end to start.
// This is a zero-allocation implementation that reads bytes in reverse order
// instead of physically reversing the byte slice.
//
// Used by ReverseSuffix and ReverseAnchored strategies for efficient
// backward matching without memory allocation.
//
// Parameters:
//   - haystack: the input to search
//   - start: the start index (inclusive, search stops here)
//   - end: the end index (exclusive, search starts from end-1)
//
// Returns the position where a match ends (scanning backward), or -1 if no match.
// For reverse search, a "match" means the reverse DFA reached a match state,
// which corresponds to finding the START of a match in the original direction.
func (d *DFA) SearchReverse(haystack []byte, start, end int) int {
	if end <= start || end > len(haystack) {
		return -1
	}

	// Get start state for reverse search
	// For reverse DFA, we start from what would be "end of match" in forward direction
	currentState := d.getStartStateForReverse(haystack, end)
	if currentState == nil {
		return d.nfaFallbackReverse(haystack, start, end)
	}

	// Track last match position (in reverse, this is the START of match)
	lastMatch := -1

	// Check if start state is already a match (empty match case)
	if currentState.IsMatch() {
		lastMatch = end
	}

	// Scan BACKWARD from end-1 to start
	for at := end - 1; at >= start; at-- {
		b := haystack[at] // Direct access, no reversal needed!

		// Get next state
		nextID, ok := currentState.Transition(b)
		switch {
		case !ok:
			// Determinize on demand
			nextState, err := d.determinize(currentState, b)
			if err != nil {
				return d.nfaFallbackReverse(haystack, start, end)
			}
			if nextState == nil {
				// Dead state - return last match if we had one
				return lastMatch
			}
			currentState = nextState

		case nextID == DeadState:
			// Dead state - return last match if we had one
			return lastMatch

		default:
			currentState = d.getState(nextID)
			if currentState == nil {
				return d.nfaFallbackReverse(haystack, start, end)
			}
		}

		// Track match state
		if currentState.IsMatch() {
			lastMatch = at // Position where match starts (in forward direction)
		}
	}

	return lastMatch
}

// IsMatchReverse performs backward DFA search and returns true if any match is found.
// This is optimized for early termination - returns true as soon as any match state is reached.
//
// Zero-allocation implementation that reads bytes in reverse order.
func (d *DFA) IsMatchReverse(haystack []byte, start, end int) bool {
	if end <= start || end > len(haystack) {
		return false
	}

	// Get start state for reverse search
	currentState := d.getStartStateForReverse(haystack, end)
	if currentState == nil {
		_, _, matched := d.pikevm.Search(haystack[start:end])
		return matched
	}

	// Check if start state is already a match
	if currentState.IsMatch() {
		return true
	}

	// Scan BACKWARD from end-1 to start with early termination
	for at := end - 1; at >= start; at-- {
		b := haystack[at]

		nextID, ok := currentState.Transition(b)
		switch {
		case !ok:
			nextState, err := d.determinize(currentState, b)
			if err != nil {
				_, _, matched := d.pikevm.Search(haystack[start:end])
				return matched
			}
			if nextState == nil {
				return false
			}
			currentState = nextState

		case nextID == DeadState:
			return false

		default:
			currentState = d.getState(nextID)
			if currentState == nil {
				_, _, matched := d.pikevm.Search(haystack[start:end])
				return matched
			}
		}

		// Early termination on any match
		if currentState.IsMatch() {
			return true
		}
	}

	return false
}

// getStartStateForReverse returns the appropriate start state for reverse search.
// For reverse search, we need to consider the context at the END of the search region.
func (d *DFA) getStartStateForReverse(haystack []byte, end int) *State {
	// For reverse search, the "start" is at the end of the region
	// Use StartText kind if at end of haystack, otherwise determine from next byte
	var kind StartKind
	if end >= len(haystack) {
		kind = StartText // End of input = "start of text" for reverse DFA
	} else {
		kind = d.startTable.GetKind(haystack[end])
	}

	// Check if already cached in StartTable (use anchored=false for reverse)
	stateID := d.startTable.Get(kind, false)
	if stateID != InvalidState {
		return d.getState(stateID)
	}

	// Not cached - compute and store
	builder := NewBuilder(d.nfa, d.config)
	config := StartConfig{Kind: kind, Anchored: false}
	state, key := ComputeStartState(builder, d.nfa, config)

	insertedState, existed, err := d.cache.GetOrInsert(key, state)
	if err != nil {
		return state
	}

	if !existed {
		d.registerState(insertedState)
	}

	d.startTable.Set(kind, false, insertedState.ID())
	return insertedState
}

// nfaFallbackReverse handles NFA fallback for reverse search.
func (d *DFA) nfaFallbackReverse(haystack []byte, start, end int) int {
	// For reverse fallback, we need to search the region and find match start
	matchStart, _, matched := d.pikevm.Search(haystack[start:end])
	if !matched {
		return -1
	}
	return start + matchStart
}
