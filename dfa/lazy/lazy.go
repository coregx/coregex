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

	// freshStartStates contains NFA state IDs that are part of the epsilon closure
	// of the anchored start. These are "fresh start" states that get re-introduced
	// via the unanchored machinery after each position. Used for leftmost matching:
	// when all remaining states are in this set, the committed match is final.
	freshStartStates map[nfa.StateID]bool

	// unanchoredStart caches the unanchored start state ID for hasInProgressPattern
	unanchoredStart nfa.StateID

	// hasWordBoundary is true if the pattern contains \b or \B assertions.
	// When false, we can skip expensive word boundary checks in the search loop.
	hasWordBoundary bool

	// isAlwaysAnchored is true if the pattern is inherently anchored (has ^ prefix).
	// When true, we only need to try matching from position 0.
	isAlwaysAnchored bool
}

// hasInProgressPattern checks if any pattern threads are still active (could extend the match).
// Returns true if there are intermediate pattern states (not fresh starts or unanchored machinery).
//
// This is used for leftmost-longest semantics: after finding a match, we continue searching
// only if pattern threads are still active. If all remaining NFA states are either fresh
// starts (re-introduced via unanchored) or unanchored machinery, the committed match is final.
func (d *DFA) hasInProgressPattern(state *State) bool {
	for _, nfaState := range state.NFAStates() {
		// Skip fresh start states (re-introduced via unanchored)
		if d.freshStartStates[nfaState] {
			continue
		}
		// Skip unanchored machinery (states near/at unanchoredStart)
		if nfaState >= d.unanchoredStart-1 {
			continue
		}
		// Found an intermediate pattern state - still in progress
		return true
	}
	return false
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
	return d.FindAt(haystack, 0)
}

// FindAt finds a match starting from position 'at' in the haystack.
// Returns the end position of the first match, or -1 if no match.
//
// This method is used by FindAll* operations to correctly handle anchors like ^.
// Unlike Find, it takes the FULL haystack and a starting position, so assertions
// like ^ correctly check against the original input start, not a sliced position.
func (d *DFA) FindAt(haystack []byte, at int) int {
	if at > len(haystack) {
		return -1
	}

	if at == len(haystack) {
		// At end of input - check if empty string matches
		if d.matchesEmpty() {
			return at
		}
		return -1
	}

	if len(haystack) == 0 {
		// Check if empty string matches
		if d.matchesEmpty() {
			return 0
		}
		return -1
	}

	// If prefilter available, use it to find candidates
	if d.prefilter != nil {
		return d.findWithPrefilterAt(haystack, at)
	}

	// No prefilter: use DFA search from position 'at'
	// The NFA now has proper unanchored start state with implicit (?s:.)*? prefix,
	// so DFA search is O(n) for both anchored and unanchored patterns
	return d.searchAt(haystack, at)
}

// SearchAt performs DFA search from position 'at' WITHOUT using prefilter.
// Returns the end position of the match, or -1 if no match.
//
// This is useful when the caller has already located a candidate position
// (e.g., via reverse search) and needs forward DFA scan for greedy matching.
// Unlike FindAt, this always uses direct DFA search, avoiding prefilter overhead.
func (d *DFA) SearchAt(haystack []byte, at int) int {
	if at > len(haystack) {
		return -1
	}

	if at == len(haystack) {
		if d.matchesEmpty() {
			return at
		}
		return -1
	}

	if len(haystack) == 0 {
		if d.matchesEmpty() {
			return 0
		}
		return -1
	}

	// Direct DFA search without prefilter
	return d.searchAt(haystack, at)
}

// SearchAtAnchored performs ANCHORED DFA search from position 'at'.
// Returns the end position of the match, or -1 if no match.
//
// Unlike SearchAt (unanchored), this uses the anchored start state which
// requires the match to begin exactly at position 'at' (no implicit (?s:.)*? prefix).
// This is used by ReverseSuffix after finding match start via reverse DFA.
func (d *DFA) SearchAtAnchored(haystack []byte, at int) int {
	if at > len(haystack) {
		return -1
	}

	if at == len(haystack) {
		if d.matchesEmpty() {
			return at
		}
		return -1
	}

	if len(haystack) == 0 {
		if d.matchesEmpty() {
			return 0
		}
		return -1
	}

	// Get ANCHORED start state (requires match to start exactly at 'at')
	currentState := d.getStartState(haystack, at, true)
	if currentState == nil {
		return d.nfaFallback(haystack, at)
	}

	lastMatch := -1
	if currentState.IsMatch() {
		lastMatch = at
	}

	for pos := at; pos < len(haystack); pos++ {
		b := haystack[pos]

		if d.checkWordBoundaryMatch(currentState, b) {
			return pos
		}

		// Convert byte to equivalence class for transition lookup
		classIdx := d.byteToClass(b)
		nextID, ok := currentState.Transition(classIdx)
		switch {
		case !ok:
			nextState, err := d.determinize(currentState, b)
			if err != nil {
				return d.nfaFallback(haystack, at)
			}
			if nextState == nil {
				return lastMatch
			}
			currentState = nextState

		case nextID == DeadState:
			return lastMatch

		default:
			currentState = d.getState(nextID)
			if currentState == nil {
				return d.nfaFallback(haystack, at)
			}
		}

		if currentState.IsMatch() {
			lastMatch = pos + 1
		}
	}

	if d.checkEOIMatch(currentState) {
		return len(haystack)
	}

	return lastMatch
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

	// Fast path: for anchored patterns (^...), only try from position 0.
	// Patterns like ^foo can never match at position > 0.
	if d.isAlwaysAnchored && startPos > 0 {
		return false
	}

	// Get context-aware start state
	currentState := d.getStartStateForUnanchored(haystack, startPos)
	if currentState == nil {
		// Fallback to NFA using SearchAt to preserve absolute positions
		start, end, matched := d.pikevm.SearchAt(haystack, startPos)
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

		// Check if word boundary would result in a match BEFORE consuming the byte.
		// This handles patterns like `test\b` where after matching "test",
		// the next byte '!' creates a word boundary that satisfies \b.
		// We need to detect this match before trying to consume '!'.
		if d.checkWordBoundaryMatch(currentState, b) {
			return true
		}

		// Get next state (convert byte to class for transition lookup)
		classIdx := d.byteToClass(b)
		nextID, ok := currentState.Transition(classIdx)
		switch {
		case !ok:
			// Determinize on demand
			nextState, err := d.determinize(currentState, b)
			if err != nil {
				// Dead state or cache full - try NFA fallback
				start, end, matched := d.pikevm.SearchAt(haystack, pos)
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
				start, end, matched := d.pikevm.SearchAt(haystack, pos)
				return matched && start >= 0 && end >= start
			}
		}

		pos++

		// Early termination: return true immediately on any match
		if currentState.IsMatch() {
			return true
		}
	}

	// Reached end of input without finding a match in the loop.
	// But we might still match if there are pending word boundary assertions
	// that are satisfied at end-of-input.
	// Example: pattern `test\b` matching "test" - the \b is satisfied at EOI
	// because prev='t'(word), next=none(non-word) → word boundary.
	return d.checkEOIMatch(currentState)
}

// findWithPrefilterAt searches using prefilter to accelerate unanchored search.
// This is used by FindAt to correctly handle anchors when searching from non-zero positions.
func (d *DFA) findWithPrefilterAt(haystack []byte, startAt int) int {
	// If prefilter is complete, its match is the final match
	if d.prefilter.IsComplete() {
		return d.prefilter.Find(haystack, startAt)
	}

	// Initial prefilter scan to find first candidate
	candidate := d.prefilter.Find(haystack, startAt)
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

		// Check if word boundary would result in a match BEFORE consuming the byte.
		// This handles patterns like `test\b` where after matching "test",
		// the next byte '!' creates a word boundary that satisfies \b.
		// Skip this expensive check for patterns without word boundaries.
		if d.hasWordBoundary && d.checkWordBoundaryMatch(currentState, b) {
			return pos // Return current position as match end
		}

		// Get next state (convert byte to class for transition lookup)
		classIdx := d.byteToClass(b)
		nextID, ok := currentState.Transition(classIdx)
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

	// Reached end of input.
	// Check if there's a match at EOI due to pending word boundary assertions.
	// Example: pattern `test\b` matching "test" - the \b is satisfied at EOI.
	if d.checkEOIMatch(currentState) {
		return len(haystack)
	}

	// Return last match position (if any)
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

	// Fast path: for anchored patterns (^...), only try from position 0.
	if d.isAlwaysAnchored && startPos > 0 {
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
	committed := false // True once we've found a match

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

		// Check if word boundary would result in a match BEFORE consuming the byte.
		// This handles patterns like `test\b` where after matching "test",
		// the next byte '!' creates a word boundary that satisfies \b.
		// Skip this expensive check for patterns without word boundaries.
		if d.hasWordBoundary && d.checkWordBoundaryMatch(currentState, b) {
			return pos // Return current position as match end
		}

		// Check if current state has a transition for this byte
		// Convert byte to equivalence class for transition lookup
		classIdx := d.byteToClass(b)
		nextID, ok := currentState.Transition(classIdx)
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

		// Track match state for leftmost-longest semantics
		if currentState.IsMatch() {
			lastMatch = pos
			committed = true
		} else if committed {
			// We were in a match but now we're not.
			// Check if any pattern threads are still active (could extend the match).
			// If only fresh starts or unanchored machinery remain, return the committed match.
			if !d.hasInProgressPattern(currentState) {
				return lastMatch
			}
			// Pattern threads still active - continue to find potential longer match
		}
	}

	// Reached end of input.
	// Check if there's a match at EOI due to pending word boundary assertions.
	// Example: pattern `test\b` matching "test" - the \b is satisfied at EOI.
	if d.checkEOIMatch(currentState) {
		return len(haystack)
	}

	// Return last match position (if any)
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
	// Need builder for move operations.
	// Use NewBuilderWithWordBoundary to pass pre-computed flag and avoid O(states) scan.
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)

	// Convert input byte to equivalence class index for transition storage
	// The actual byte value is still used for NFA move operations
	classIdx := d.byteToClass(b)

	// Compute next NFA state set via move operation WITH word context
	// This is essential for correct \b and \B handling in DFA.
	// The current state's isFromWord tells us if the previous byte was a word char.
	// Note: use actual byte 'b' (not classIdx) for NFA move - NFA uses raw bytes
	nextNFAStates := builder.moveWithWordContext(current.NFAStates(), b, current.IsFromWord())

	// No transitions on this byte → dead state
	if len(nextNFAStates) == 0 {
		// Cache the dead state transition to avoid re-computation
		// Use classIdx for transition storage (compressed alphabet)
		current.AddTransition(classIdx, DeadState)
		// Return nil state with NO error - dead state is NOT an error condition.
		// This follows the documented behavior: (nil, nil) for dead state.
		// Returning an error here would incorrectly trigger NFA fallback.
		return nil, nil //nolint:nilnil // dead state is valid, not an error
	}

	// Check if we've exceeded determinization limit
	if len(nextNFAStates) > d.config.DeterminizationLimit {
		// Too many NFA states: fall back to avoid exponential blowup
		return nil, &DFAError{
			Kind:    StateLimitExceeded,
			Message: "determinization limit exceeded",
		}
	}

	// The next state's isFromWord is determined by the CURRENT byte
	// This is the Rust regex-automata approach: the state we're transitioning TO
	// needs to know what byte got us there (for the next transition's word boundary check)
	nextIsFromWord := isWordByte(b)

	// Compute state key INCLUDING word context
	// States with same NFA states but different isFromWord are DIFFERENT DFA states!
	key := ComputeStateKeyWithWord(nextNFAStates, nextIsFromWord)

	// Check if state already exists in cache
	if existing, ok := d.cache.Get(key); ok {
		// Cache hit: reuse existing state
		// Use classIdx for transition storage (compressed alphabet)
		current.AddTransition(classIdx, existing.ID())
		return existing, nil
	}

	// Create new DFA state with word context and compressed alphabet stride
	isMatch := builder.containsMatchState(nextNFAStates)
	newState := NewStateWithStride(InvalidState, nextNFAStates, isMatch, nextIsFromWord, d.AlphabetLen())

	// Insert into cache
	_, err := d.cache.Insert(key, newState)
	if err != nil {
		// Cache full
		return nil, err
	}

	// Register state in ID lookup map
	d.registerState(newState)

	// Add transition from current state to new state
	// Use classIdx for transition storage (compressed alphabet)
	current.AddTransition(classIdx, newState.ID())

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

// checkEOIMatch checks if the current state would match at end-of-input.
// This handles patterns with trailing word boundary assertions like `test\b`.
//
// At end-of-input:
//   - Previous byte is known from state.IsFromWord()
//   - "Next" byte is conceptually non-word (outside the string)
//   - \b is satisfied if previous was word char (word → non-word transition)
//   - \B is satisfied if previous was non-word char (non-word → non-word)
func (d *DFA) checkEOIMatch(state *State) bool {
	if state == nil {
		return false
	}

	// Create a temporary builder for EOI resolution
	builder := NewBuilder(d.nfa, d.config)
	return builder.CheckEOIMatch(state.NFAStates(), state.IsFromWord())
}

// checkWordBoundaryMatch checks if resolving word boundary assertions with
// the given next byte would result in a match.
//
// This is needed for patterns like `test\b` where after matching "test",
// the next byte (e.g., '!') creates a word boundary that satisfies \b.
// The \b resolves to Match state, but we shouldn't consume the '!'.
//
// Returns true if crossing a word boundary results in a NEW match (i.e., the current
// state wasn't already a match, but resolving word boundaries produces one).
// Returns false for patterns without word boundaries (e.g., `a*`).
func (d *DFA) checkWordBoundaryMatch(state *State, nextByte byte) bool {
	if state == nil {
		return false
	}

	// If already a match state, don't use word boundary shortcut
	// Let normal processing handle it (for leftmost-longest semantics)
	if state.IsMatch() {
		return false
	}

	builder := NewBuilder(d.nfa, d.config)
	isFromWord := state.IsFromWord()
	isNextWord := isWordByte(nextByte)
	wordBoundarySatisfied := isFromWord != isNextWord

	// Resolve word boundary assertions
	// This only expands states if word boundary assertions are actually crossed
	resolved := builder.resolveWordBoundaries(state.NFAStates(), wordBoundarySatisfied)

	// Check if resolving word boundaries added any match states
	// If resolved == original states (no word boundaries crossed), this returns false
	// because the original states weren't matches (checked above)
	return builder.containsMatchState(resolved)
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

	// Not cached - compute and store with proper stride for ByteClasses compression
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)
	config := StartConfig{Kind: kind, Anchored: anchored}
	state, key := ComputeStartStateWithStride(builder, d.nfa, config, d.AlphabetLen())

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
	// Search from startPos to end using SearchAt to preserve absolute positions
	// This is critical for anchor handling (^ should only match at position 0)
	_, end, matched := d.pikevm.SearchAt(haystack, startPos)
	if !matched {
		return -1
	}

	// PikeVM.SearchAt returns absolute positions
	return end
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

	// Try lazy detection from cached transitions with ByteClasses support
	if exitBytes := DetectAccelerationFromCachedWithClasses(state, d.byteClasses); len(exitBytes) > 0 {
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
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)
	startLook := LookSetFromStartKind(StartText)
	startStateSet := builder.epsilonClosure([]nfa.StateID{d.nfa.StartUnanchored()}, startLook)
	isMatch := builder.containsMatchState(startStateSet)
	// Use proper stride for ByteClasses compression
	startState := NewStateWithStride(StartState, startStateSet, isMatch, false, d.AlphabetLen())
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

// byteToClass converts a raw input byte to its equivalence class index.
// This is the key operation for ByteClasses compression - all transition
// lookups use the class index instead of the raw byte.
//
// Returns the byte itself if ByteClasses are not available (no compression).
func (d *DFA) byteToClass(b byte) byte {
	if d.byteClasses == nil {
		return b
	}
	return d.byteClasses.Get(b)
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

		// Get next state (convert byte to class for transition lookup)
		classIdx := d.byteToClass(b)
		nextID, ok := currentState.Transition(classIdx)
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

		// Convert byte to equivalence class for transition lookup
		classIdx := d.byteToClass(b)
		nextID, ok := currentState.Transition(classIdx)
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

	// Not cached - compute and store with proper stride for ByteClasses compression
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)
	cfg := StartConfig{Kind: kind, Anchored: false}
	state, key := ComputeStartStateWithStride(builder, d.nfa, cfg, d.AlphabetLen())

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
