// Package lazy implements a Lazy DFA (Deterministic Finite Automaton) engine
// for regex matching.
//
// The Lazy DFA constructs DFA states on-demand during matching, rather than
// building the complete DFA upfront. This provides:
//   - Fast matching: O(n) time complexity (linear in input length)
//   - Bounded memory: States are cached with a configurable limit
//   - Cache clear & continue: When cache fills, it clears and rebuilds on demand
//   - Graceful degradation: Falls back to NFA only after too many cache clears
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
	"errors"

	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
	"github.com/coregx/coregex/simd"
)

// DFA is a Lazy DFA engine that performs on-demand determinization.
//
// After compilation, DFA is fully immutable and safe to share across goroutines.
// All mutable state (cached DFA states, start table, transition cache) lives in
// DFACache, which is created per-goroutine via DFA.NewCache().
//
// The DFA maintains:
//   - An NFA (the source automaton) — immutable
//   - A configuration — immutable
//   - An optional prefilter for fast candidate finding — immutable
//   - A PikeVM for NFA fallback — immutable (Search methods are safe)
//   - ByteClasses for alphabet reduction — immutable
//
// Thread safety: The DFA struct is immutable after compilation and safe
// to share across goroutines. Each goroutine must use its own DFACache.
//
// This separation is inspired by the Rust regex crate's approach where
// the regex configuration is immutable and per-thread cache is mutable.
type DFA struct {
	nfa       *nfa.NFA
	config    Config
	prefilter prefilter.Prefilter
	pikevm    *nfa.PikeVM

	// byteClasses maps bytes to equivalence classes for alphabet reduction.
	// Bytes in the same class have identical transitions in all DFA states.
	// This enables memory optimization from 256 to ~8-16 transitions per state.
	byteClasses *nfa.ByteClasses

	// unanchoredStart caches the unanchored start state ID
	unanchoredStart nfa.StateID

	// hasWordBoundary is true if the pattern contains \b or \B assertions.
	// When false, we can skip expensive word boundary checks in the search loop.
	hasWordBoundary bool

	// isAlwaysAnchored is true if the pattern is inherently anchored (has ^ prefix).
	// When true, we only need to try matching from position 0.
	isAlwaysAnchored bool

	// startByteMap is the immutable byte-to-StartKind mapping used to initialize
	// DFACache.startTable. Computed once during compilation.
	startByteMap [256]StartKind
}

// NewCache creates a new DFACache for use with this DFA.
//
// Each goroutine must have its own DFACache. The cache can be reused
// across searches via Reset(), or pooled via sync.Pool in the meta layer.
//
// The cache is initialized with:
//   - A state map (grows on demand up to CacheCapacityBytes)
//   - A stateList for O(1) state-by-ID lookup
//   - A StartTable with the DFA's immutable byteMap
func (d *DFA) NewCache() *DFACache {
	// Start small — grow on demand. Pre-allocating MaxStates (10,000) wastes
	// ~400KB per cache and dominates cold-start cost for pooled caches.
	const initCap = 64
	stride := d.AlphabetLen()
	return &DFACache{
		states:        make(map[StateKey]*State, initCap),
		stateList:     make([]*State, 0, initCap),
		flatTrans:     make([]StateID, 0, initCap*stride),
		stride:        stride,
		startTable:    newStartTableFromByteMap(&d.startByteMap),
		capacityBytes: d.config.effectiveCapacityBytes(),
		nextID:        StateID(stride), // premultiplied: next state starts at offset=stride
	}
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
func (d *DFA) Find(cache *DFACache, haystack []byte) int {
	return d.FindAt(cache, haystack, 0)
}

// FindAt finds a match starting from position 'at' in the haystack.
// Returns the end position of the first match, or -1 if no match.
//
// This method is used by FindAll* operations to correctly handle anchors like ^.
// Unlike Find, it takes the FULL haystack and a starting position, so assertions
// like ^ correctly check against the original input start, not a sliced position.
func (d *DFA) FindAt(cache *DFACache, haystack []byte, at int) int {
	if at > len(haystack) {
		return -1
	}

	if at == len(haystack) {
		// At end of input - check if empty string matches
		if d.matchesEmpty(cache) {
			return at
		}
		return -1
	}

	if len(haystack) == 0 {
		// Check if empty string matches
		if d.matchesEmpty(cache) {
			return 0
		}
		return -1
	}

	// If prefilter available, use it to find candidates
	if d.prefilter != nil {
		return d.findWithPrefilterAt(cache, haystack, at)
	}

	// No prefilter: use DFA search from position 'at'
	// The NFA now has proper unanchored start state with implicit (?s:.)*? prefix,
	// so DFA search is O(n) for both anchored and unanchored patterns
	return d.searchAt(cache, haystack, at)
}

// SearchAt performs DFA search from position 'at' WITHOUT using prefilter.
// Returns the end position of the match, or -1 if no match.
//
// This is useful when the caller has already located a candidate position
// (e.g., via reverse search) and needs forward DFA scan for greedy matching.
// Unlike FindAt, this always uses direct DFA search, avoiding prefilter overhead.
func (d *DFA) SearchAt(cache *DFACache, haystack []byte, at int) int {
	if at > len(haystack) {
		return -1
	}

	if at == len(haystack) {
		if d.matchesEmpty(cache) {
			return at
		}
		return -1
	}

	if len(haystack) == 0 {
		if d.matchesEmpty(cache) {
			return 0
		}
		return -1
	}

	// Direct DFA search without prefilter
	return d.searchAt(cache, haystack, at)
}

// SearchAtAnchored performs ANCHORED DFA search from position 'at'.
// Returns the end position of the match, or -1 if no match.
//
// Unlike SearchAt (unanchored), this uses the anchored start state which
// requires the match to begin exactly at position 'at' (no implicit (?s:.)*? prefix).
// This is used by ReverseSuffix after finding match start via reverse DFA.
func (d *DFA) SearchAtAnchored(cache *DFACache, haystack []byte, at int) int {
	if at > len(haystack) {
		return -1
	}

	if at == len(haystack) {
		if d.matchesEmpty(cache) {
			return at
		}
		return -1
	}

	if len(haystack) == 0 {
		if d.matchesEmpty(cache) {
			return 0
		}
		return -1
	}

	// Get ANCHORED start state (requires match to start exactly at 'at')
	currentState := d.getStartState(cache, haystack, at, true)
	if currentState == nil {
		return d.nfaFallback(haystack, at)
	}

	lastMatch := -1
	// With 1-byte match delay, start states are never match states.

	sid := currentState.id
	ft := cache.flatTrans
	ftLen := len(ft)

	for pos := at; pos < len(haystack); pos++ {
		b := haystack[pos]

		if d.hasWordBoundary {
			st := cache.getState(sid)
			if st != nil && st.checkWordBoundaryFast(b) {
				return pos
			}
		}

		classIdx := int(d.byteToClass(b))
		offset := sid.Offset() + classIdx
		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			currentState = cache.getState(sid)
			if currentState == nil {
				return d.nfaFallback(haystack, at)
			}
			nextState, err := d.determinize(cache, currentState, b)
			if err != nil {
				if isCacheCleared(err) {
					currentState = d.getStartState(cache, haystack, pos, true)
					if currentState == nil {
						return d.nfaFallback(haystack, at)
					}
					sid = currentState.id
					ft = cache.flatTrans
					ftLen = len(ft)
					pos--
					continue
				}
				return d.nfaFallback(haystack, at)
			}
			if nextState == nil {
				return lastMatch
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)

		case DeadState:
			return lastMatch

		default:
			sid = nextID
		}

		// 1-byte match delay: check AFTER transition.
		// With delay, the match tag on the new sid means the previous state
		// had an NFA match. The exclusive match end = pos (the byte just
		// consumed), because the delay already shifts by 1 byte.
		// Rust: mat = Some(HalfMatch::new(pattern, at)) — at is the byte index.
		if cache.IsMatchState(sid) {
			lastMatch = pos
		}
	}

	// EOI: check for delayed match at end of input.
	// The current state's NFA states may contain a match that hasn't been
	// reported yet (no more bytes to trigger the delay).
	eoi := cache.getState(sid)
	if eoi != nil && d.checkEOIMatch(eoi) {
		return len(haystack)
	}

	return lastMatch
}

// SearchFirstAt finds the end of the FIRST match (leftmost-first semantics).
// Unlike SearchAt which returns the end of the leftmost-LONGEST match,
// this returns the end of the first match and does NOT extend further.
//
// This is used by bidirectional DFA search: forward DFA finds first match end,
// then reverse DFA finds the exact start. Leftmost-longest would over-extend
// past the first match for patterns like "[^"]*" on input with multiple matches.
func (d *DFA) SearchFirstAt(cache *DFACache, haystack []byte, at int) int {
	if at > len(haystack) {
		return -1
	}

	if at == len(haystack) {
		if d.matchesEmpty(cache) {
			return at
		}
		return -1
	}

	if len(haystack) == 0 {
		if d.matchesEmpty(cache) {
			return 0
		}
		return -1
	}

	return d.searchFirstAt(cache, haystack, at)
}

// searchFirstAt is the core DFA search with early termination after first match.
// Returns the end of the first match found, without extending for longest match.
// With 1-byte match delay + filterUnanchoredPrefix, the DFA naturally reaches
// dead state after a match can't extend, providing leftmost-first semantics.
func (d *DFA) searchFirstAt(cache *DFACache, haystack []byte, startPos int) int { //nolint:funlen // 4x unrolled hot loop with integrated prefilter
	if d.isAlwaysAnchored && startPos > 0 {
		return -1
	}

	startState := d.getStartStateForUnanchored(cache, haystack, startPos)
	if startState == nil {
		return d.nfaFallback(haystack, startPos)
	}

	// With 1-byte match delay, start states are never match states.

	end := len(haystack)
	pos := startPos
	lastMatch := -1

	sid := startState.id
	ft := cache.flatTrans
	stride := cache.stride

	if len(ft) > 0 {
		_ = ft[len(ft)-1]
	}

	canUnroll := !d.hasWordBoundary
	ftLen := len(ft)
	startSID := startState.id
	hasPre := d.prefilter != nil

	for pos < end {
		// Prefilter skip-ahead at start state
		if hasPre && sid == startSID && lastMatch < 0 && pos > startPos {
			candidate := d.prefilter.Find(haystack, pos)
			if candidate == -1 {
				return lastMatch
			}
			if candidate > pos {
				pos = candidate
				newStart := d.getStartStateForUnanchored(cache, haystack, pos)
				if newStart == nil {
					return d.nfaFallback(haystack, startPos)
				}
				sid = newStart.id
				startSID = sid
				ft = cache.flatTrans
				ftLen = len(ft)
			}
		}

		// === 4x UNROLLED FAST PATH ===
		// With match delay, tagged states (including match) break to slow path.
		if canUnroll && pos+3 < end {
			if sid.Offset()+stride > ftLen {
				goto searchFirstSlowPath
			}
			// Transition 1
			n1 := ft[sid.Offset()+int(d.byteToClass(haystack[pos]))]
			if n1.IsTagged() {
				goto searchFirstSlowPath
			}
			pos++
			if pos+2 >= end {
				sid = n1
				goto searchFirstSlowPath
			}

			// Transition 2
			n2 := ft[n1.Offset()+int(d.byteToClass(haystack[pos]))]
			if n2.IsTagged() {
				sid = n1
				goto searchFirstSlowPath
			}
			pos++
			if pos+1 >= end {
				sid = n2
				goto searchFirstSlowPath
			}

			// Transition 3
			n3 := ft[n2.Offset()+int(d.byteToClass(haystack[pos]))]
			if n3.IsTagged() {
				sid = n2
				goto searchFirstSlowPath
			}
			pos++

			// Transition 4
			n4 := ft[n3.Offset()+int(d.byteToClass(haystack[pos]))]
			if n4.IsTagged() {
				sid = n3
				goto searchFirstSlowPath
			}
			pos++
			sid = n4

			continue
		}

	searchFirstSlowPath:
		if pos >= end {
			break
		}

		if d.hasWordBoundary {
			st := cache.getState(sid)
			if st != nil && st.checkWordBoundaryFast(haystack[pos]) {
				return pos
			}
		}

		classIdx := int(d.byteToClass(haystack[pos]))
		offset := sid.Offset() + classIdx

		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			currentState := cache.getState(sid)
			if currentState == nil {
				return d.nfaFallback(haystack, startPos)
			}
			nextState, err := d.determinize(cache, currentState, haystack[pos])
			if err != nil {
				return d.nfaFallback(haystack, startPos)
			}
			if nextState == nil {
				return lastMatch
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)
		case DeadState:
			return lastMatch
		default:
			sid = nextID
		}

		// 1-byte match delay: check after transition, before pos advance.
		// For leftmost-first (searchFirstAt), return immediately on first match.
		// The match delay ensures pos is the correct exclusive end.
		if cache.IsMatchState(sid) {
			return pos
		}

		pos++
	}

	// EOI match check
	eoi := cache.getState(sid)
	if eoi != nil && d.checkEOIMatch(eoi) {
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
func (d *DFA) IsMatch(cache *DFACache, haystack []byte) bool {
	if len(haystack) == 0 {
		return d.matchesEmpty(cache)
	}

	// Use prefilter for acceleration if available
	if d.prefilter != nil {
		return d.isMatchWithPrefilter(cache, haystack)
	}

	// No prefilter: use optimized DFA search with early termination
	return d.searchEarliestMatch(cache, haystack, 0)
}

// IsMatchAt returns true if the pattern matches anywhere in haystack[at:].
// Uses early termination: returns as soon as any match state is reached.
// This is O(k) where k is the distance to the first match, vs FindAt's O(n)
// which always scans for the longest match.
func (d *DFA) IsMatchAt(cache *DFACache, haystack []byte, at int) bool {
	if at >= len(haystack) {
		if at == len(haystack) {
			return d.matchesEmpty(cache)
		}
		return false
	}

	return d.searchEarliestMatch(cache, haystack, at)
}

// isMatchWithPrefilter uses an integrated prefilter+DFA loop (Rust approach).
//
// Instead of two separate passes (prefilter.Find → DFA.searchAnchored → repeat),
// this runs a single DFA loop where dead-state transitions trigger prefilter
// skip-ahead. This eliminates Go function call overhead between passes and
// avoids redundant start-state setup on each candidate.
//
// Reference: rust regex-automata hybrid/search.rs find_fwd_imp — prefilter
// is called inside the DFA loop when returning to start state.
func (d *DFA) isMatchWithPrefilter(cache *DFACache, haystack []byte) bool {
	// If prefilter is complete, its match is sufficient
	if d.prefilter.IsComplete() {
		return d.prefilter.Find(haystack, 0) != -1
	}

	// Find first candidate to start DFA from
	pos := d.prefilter.Find(haystack, 0)
	if pos == -1 {
		return false
	}

	// Get anchored start state at candidate position
	currentState := d.getStartState(cache, haystack, pos, true)
	if currentState == nil {
		return d.isMatchWithPrefilterFallback(cache, haystack, pos)
	}
	// With 1-byte match delay, start states are never match states.

	endPos := len(haystack)
	sid := currentState.id
	ft := cache.flatTrans
	ftLen := len(ft)

	for pos < endPos {
		if d.hasWordBoundary {
			st := cache.getState(sid)
			if st != nil && st.checkWordBoundaryFast(haystack[pos]) {
				return true
			}
		}

		classIdx := int(d.byteToClass(haystack[pos]))
		offset := sid.Offset() + classIdx
		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			currentState = cache.getState(sid)
			if currentState == nil {
				start, end, matched := d.pikevm.SearchAt(haystack, pos)
				return matched && start >= 0 && end >= start
			}
			nextState, err := d.determinize(cache, currentState, haystack[pos])
			if err != nil {
				start, end, matched := d.pikevm.SearchAt(haystack, pos)
				return matched && start >= 0 && end >= start
			}
			if nextState == nil {
				goto pfSkip
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)

		case DeadState:
			goto pfSkip

		default:
			sid = nextID
		}

		pos++
		// 1-byte match delay: check after transition
		if cache.IsMatchState(sid) {
			return true
		}
		continue

	pfSkip:
		pos++
		candidate := d.prefilter.Find(haystack, pos)
		if candidate == -1 {
			return false
		}
		pos = candidate

		newStart := d.getStartState(cache, haystack, pos, true)
		if newStart == nil {
			return d.isMatchWithPrefilterFallback(cache, haystack, pos)
		}
		sid = newStart.id
		ft = cache.flatTrans
		ftLen = len(ft)
		// With match delay, start states are never match — continue loop.
	}

	eoi := cache.getState(sid)
	if eoi != nil {
		return d.checkEOIMatch(eoi)
	}
	return false
}

// isMatchWithPrefilterFallback is the old two-pass approach used when
// DFA start state cannot be obtained (NFA fallback needed).
func (d *DFA) isMatchWithPrefilterFallback(cache *DFACache, haystack []byte, pos int) bool {
	// Try anchored DFA search at current position
	if d.searchEarliestMatchAnchored(cache, haystack, pos) {
		return true
	}
	// Continue with remaining candidates
	for pos < len(haystack) {
		pos++
		candidate := d.prefilter.Find(haystack, pos)
		if candidate == -1 {
			return false
		}
		pos = candidate
		if d.searchEarliestMatchAnchored(cache, haystack, pos) {
			return true
		}
	}
	return false
}

// searchEarliestMatch performs DFA search with early termination.
// Returns true as soon as any match state is reached.
// This is faster than searchAt because it doesn't track match positions
// or enforce leftmost-longest semantics.
func (d *DFA) searchEarliestMatch(cache *DFACache, haystack []byte, startPos int) bool { //nolint:funlen,maintidx // DFA search with 4x unrolling
	if startPos > len(haystack) {
		return false
	}

	// Fast path: for anchored patterns (^...), only try from position 0.
	// Patterns like ^foo can never match at position > 0.
	if d.isAlwaysAnchored && startPos > 0 {
		return false
	}

	// Get context-aware start state
	currentState := d.getStartStateForUnanchored(cache, haystack, startPos)
	if currentState == nil {
		// Fallback to NFA using SearchAt to preserve absolute positions
		start, end, matched := d.pikevm.SearchAt(haystack, startPos)
		return matched && start >= 0 && end >= start
	}

	// With 1-byte match delay, start states are never match states.

	// Determine if 4x unrolling can be used.
	canUnroll := !d.hasWordBoundary

	endPos := len(haystack)
	pos := startPos

	// Hot loop: flat transition table (Rust approach).
	// Work with state ID only — no *State pointer chase in fast path.
	sid := currentState.id
	ft := cache.flatTrans
	stride := cache.stride
	ftLen := len(ft)

	// Bounds hint for compiler — eliminates repeated len checks in loop.
	if ftLen > 0 {
		_ = ft[ftLen-1]
	}

	for pos < endPos {
		// === 4x UNROLLED FAST PATH (earliest match) ===
		// For IsMatch(), we return true on ANY match, so no leftmost-longest tracking.
		// This is even simpler than searchAt: just check isMatch after each transition.
		if canUnroll && pos+3 < endPos {
			// Check acceleration on slow→fast transition (once per entry).
			accelState := cache.getState(sid)
			if accelState != nil && accelState.IsAccelerable() {
				goto earliestSlowPath
			}

			// Bounds hint for 4x unrolled transitions
			if sid.Offset()+stride > ftLen {
				goto earliestSlowPath
			}

			// Transition 1
			n1 := ft[sid.Offset()+int(d.byteToClass(haystack[pos]))]
			if n1.IsTagged() {
				if n1.IsMatchTag() {
					return true
				}
				goto earliestSlowPath
			}
			pos++

			if pos+2 >= endPos {
				sid = n1
				goto earliestSlowPath
			}

			// Transition 2
			n2 := ft[n1.Offset()+int(d.byteToClass(haystack[pos]))]
			if n2.IsTagged() {
				if n2.IsMatchTag() {
					return true
				}
				sid = n1
				goto earliestSlowPath
			}
			pos++

			if pos+1 >= endPos {
				sid = n2
				goto earliestSlowPath
			}

			// Transition 3
			n3 := ft[n2.Offset()+int(d.byteToClass(haystack[pos]))]
			if n3.IsTagged() {
				if n3.IsMatchTag() {
					return true
				}
				sid = n2
				goto earliestSlowPath
			}
			pos++

			// Transition 4
			n4 := ft[n3.Offset()+int(d.byteToClass(haystack[pos]))]
			if n4.IsTagged() {
				if n4.IsMatchTag() {
					return true
				}
				sid = n3
				goto earliestSlowPath
			}
			pos++
			sid = n4

			continue
		}

	earliestSlowPath:
		// === SINGLE-BYTE SLOW PATH ===
		if pos >= endPos {
			break
		}

		// Try lazy acceleration detection if not yet checked
		currentState = cache.getState(sid)
		if currentState == nil {
			start, end, matched := d.pikevm.SearchAt(haystack, startPos)
			return matched && start >= 0 && end >= start
		}
		d.tryDetectAccelerationWithCache(currentState, cache)

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
		// O(1) word boundary match check using pre-computed flags (was 30% CPU).
		// matchAtWordBoundary/matchAtNonWordBoundary computed during determinize.
		if d.hasWordBoundary && currentState.checkWordBoundaryFast(b) {
			return true
		}

		// Flat table lookup for transition
		classIdx := int(d.byteToClass(b))
		offset := sid.Offset() + classIdx

		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			// Determinize on demand
			nextState, err := d.determinize(cache, currentState, b)
			if err != nil {
				// Cache cleared or full — fall back to NFA from original start position.
				start, end, matched := d.pikevm.SearchAt(haystack, startPos)
				return matched && start >= 0 && end >= start
			}
			if nextState == nil {
				// Dead state - no match possible from here
				return false
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)

		case DeadState:
			// Dead state - no match possible from here
			return false

		default:
			sid = nextID
		}

		pos++

		// Early termination: return true immediately on any match
		if cache.IsMatchState(sid) {
			return true
		}
	}

	// Reached end of input without finding a match in the loop.
	// But we might still match if there are pending word boundary assertions
	// that are satisfied at end-of-input.
	// Example: pattern `test\b` matching "test" - the \b is satisfied at EOI
	// because prev='t'(word), next=none(non-word) → word boundary.
	eoi := cache.getState(sid)
	return eoi != nil && d.checkEOIMatch(eoi)
}

// searchEarliestMatchAnchored performs ANCHORED DFA search with early termination.
// Unlike searchEarliestMatch, this requires the match to START exactly at startPos.
// This is critical for prefilter verification - we need to confirm the match
// actually starts at the candidate position, not somewhere after it.
//
// Issue #105: Using unanchored search for prefilter verification caused
// catastrophic slowdown because it would re-scan from candidate to end.
func (d *DFA) searchEarliestMatchAnchored(cache *DFACache, haystack []byte, startPos int) bool {
	if startPos > len(haystack) {
		return false
	}

	// Get ANCHORED start state (requires match to start exactly at startPos)
	currentState := d.getStartState(cache, haystack, startPos, true)
	if currentState == nil {
		// Fallback to NFA with anchored search
		start, end, matched := d.pikevm.SearchAt(haystack, startPos)
		// For anchored: match must start exactly at startPos
		return matched && start == startPos && end >= start
	}

	// With 1-byte match delay, start states are never match states.

	sid := currentState.id
	ft := cache.flatTrans
	ftLen := len(ft)

	for pos := startPos; pos < len(haystack); pos++ {
		b := haystack[pos]

		if d.hasWordBoundary {
			st := cache.getState(sid)
			if st != nil && st.checkWordBoundaryFast(b) {
				return true
			}
		}

		classIdx := int(d.byteToClass(b))
		offset := sid.Offset() + classIdx

		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			currentState = cache.getState(sid)
			if currentState == nil {
				start, end, matched := d.pikevm.SearchAt(haystack, startPos)
				return matched && start == startPos && end >= start
			}
			nextState, err := d.determinize(cache, currentState, b)
			if err != nil {
				if isCacheCleared(err) {
					currentState = d.getStartState(cache, haystack, pos, true)
					if currentState == nil {
						start, end, matched := d.pikevm.SearchAt(haystack, startPos)
						return matched && start == startPos && end >= start
					}
					sid = currentState.id
					ft = cache.flatTrans
					ftLen = len(ft)
					pos--
					continue
				}
				start, end, matched := d.pikevm.SearchAt(haystack, startPos)
				return matched && start == startPos && end >= start
			}
			if nextState == nil {
				return false
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)

		case DeadState:
			return false

		default:
			sid = nextID
		}

		// 1-byte match delay: return true on any match state
		if cache.IsMatchState(sid) {
			return true
		}
	}

	eoi := cache.getState(sid)
	return eoi != nil && d.checkEOIMatch(eoi)
}

// findWithPrefilterAt searches using prefilter to accelerate unanchored search.
// This is used by FindAt to correctly handle anchors when searching from non-zero positions.
func (d *DFA) findWithPrefilterAt(cache *DFACache, haystack []byte, startAt int) int { //nolint:funlen // prefilter search with cache-clear handling needs multi-path logic
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
	currentState := d.getStartStateForUnanchored(cache, haystack, pos)
	if currentState == nil {
		return d.nfaFallback(haystack, 0)
	}

	// Track last match position for leftmost-longest semantics
	lastMatch := -1
	// With 1-byte match delay, start states are never match states.

	sid := currentState.id
	ft := cache.flatTrans
	ftLen := len(ft)
	startSID := sid

	for pos < len(haystack) {
		if d.hasWordBoundary {
			st := cache.getState(sid)
			if st != nil && d.checkWordBoundaryMatch(st, haystack[pos]) {
				return pos
			}
		}

		classIdx := int(d.byteToClass(haystack[pos]))
		offset := sid.Offset() + classIdx
		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			currentState = cache.getState(sid)
			if currentState == nil {
				return d.nfaFallback(haystack, 0)
			}
			nextState, err := d.determinize(cache, currentState, haystack[pos])
			if err != nil {
				if isCacheCleared(err) {
					newStart := d.getStartStateForUnanchored(cache, haystack, pos)
					if newStart == nil {
						return d.nfaFallback(haystack, 0)
					}
					sid = newStart.id
					startSID = sid
					ft = cache.flatTrans
					ftLen = len(ft)
					continue
				}
				return d.nfaFallback(haystack, 0)
			}
			if nextState == nil {
				// Dead state — prefilter skip
				if lastMatch != -1 {
					return lastMatch
				}
				pos++
				candidate = d.prefilter.Find(haystack, pos)
				if candidate == -1 {
					return -1
				}
				pos = candidate
				newStart := d.getStartStateForUnanchored(cache, haystack, pos)
				if newStart == nil {
					return d.nfaFallback(haystack, 0)
				}
				sid = newStart.id
				startSID = sid
				ft = cache.flatTrans
				ftLen = len(ft)
				lastMatch = -1
				continue
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)

		case DeadState:
			if lastMatch != -1 {
				return lastMatch
			}
			pos++
			candidate = d.prefilter.Find(haystack, pos)
			if candidate == -1 {
				return -1
			}
			pos = candidate
			newStart := d.getStartStateForUnanchored(cache, haystack, pos)
			if newStart == nil {
				return d.nfaFallback(haystack, 0)
			}
			sid = newStart.id
			startSID = sid
			ft = cache.flatTrans
			ftLen = len(ft)
			lastMatch = -1
			continue

		default:
			sid = nextID
		}

		// 1-byte match delay: check after transition, before pos advance
		if cache.IsMatchState(sid) {
			lastMatch = pos
		}

		pos++

		// Start state prefilter skip-ahead
		if lastMatch < 0 && sid == startSID && pos < len(haystack) {
			candidate = d.prefilter.Find(haystack, pos)
			if candidate == -1 {
				return -1
			}
			if candidate > pos {
				pos = candidate
			}
		}
	}

	// EOI check for delayed match
	eoi := cache.getState(sid)
	if eoi != nil && d.checkEOIMatch(eoi) {
		return len(haystack)
	}

	return lastMatch
}

// isCacheCleared checks if an error from determinize() is the cache-cleared signal.
// When true, the search loop must re-obtain the current state from the start state
// at the current position and continue searching.
func isCacheCleared(err error) bool {
	if err == nil {
		return false
	}
	var dfaErr *DFAError
	if errors.As(err, &dfaErr) {
		return dfaErr.Kind == CacheCleared
	}
	return false
}

// searchAt attempts to find a match starting at the given position.
// Returns the end position of the leftmost-longest match, or -1 if no match.
//
// This is the core DFA search algorithm with lazy determinization.
// Uses leftmost-longest semantics: find the earliest match start, then extend greedily.
//
// The search loop uses 4x loop unrolling for throughput: when conditions allow,
// 4 state transitions are batched together with special-state checking deferred
// to after the batch. This reduces branch overhead by ~75% in the hot path.
// The technique is inspired by the Rust regex crate's DFA search (search.rs).
//
// The unrolled loop activates when:
//   - No word boundary assertions in the pattern (avoids per-byte boundary checks)
//   - No committed match yet (leftmost-longest tracking needs per-byte granularity once committed)
//   - Current state is not accelerable (SIMD acceleration is a separate, more powerful optimization)
//   - At least 4 bytes remain in the input
//
// When any of these conditions are not met, the search falls back to the
// single-byte loop that handles all edge cases correctly.
func (d *DFA) searchAt(cache *DFACache, haystack []byte, startPos int) int { //nolint:funlen,maintidx // DFA search with 4x unrolling is inherently complex
	if startPos > len(haystack) {
		return -1
	}

	// Fast path: for anchored patterns (^...), only try from position 0.
	if d.isAlwaysAnchored && startPos > 0 {
		return -1
	}

	// Get appropriate start state based on look-behind context
	currentState := d.getStartStateForUnanchored(cache, haystack, startPos)
	if currentState == nil {
		return d.nfaFallback(haystack, startPos)
	}

	// Track last match position for leftmost-longest semantics.
	// With 1-byte match delay, start states are never match states.
	lastMatch := -1

	// Determine if the 4x unrolled fast path can be used.
	canUnroll := !d.hasWordBoundary

	end := len(haystack)
	pos := startPos

	// Hot loop: flat transition table (Rust approach).
	sid := currentState.id
	ft := cache.flatTrans
	stride := cache.stride
	ftLen := len(ft)

	// Bounds hint for compiler
	if ftLen > 0 {
		_ = ft[ftLen-1]
	}

	startSID := currentState.id
	hasPre := d.prefilter != nil

	for pos < end {
		// Prefilter skip-ahead at start state (Rust hybrid/search.rs:232-258)
		if hasPre && sid == startSID && lastMatch < 0 && pos > startPos {
			candidate := d.prefilter.Find(haystack, pos)
			if candidate == -1 {
				return lastMatch
			}
			if candidate > pos {
				pos = candidate
				newStart := d.getStartStateForUnanchored(cache, haystack, pos)
				if newStart == nil {
					return d.nfaFallback(haystack, startPos)
				}
				sid = newStart.id
				startSID = sid
				ft = cache.flatTrans
				ftLen = len(ft)
			}
		}

		// === 4x UNROLLED FAST PATH ===
		// Process 4 transitions per iteration when conditions allow.
		// With match delay, match states break out of the unrolled loop
		// to the slow path for proper handling.
		if canUnroll && pos+3 < end {
			// Check acceleration on slow→fast transition
			accelState := cache.getState(sid)
			if accelState != nil && accelState.IsAccelerable() {
				goto slowPath
			}

			// Bounds hint for 4x unrolled transitions
			if sid.Offset()+stride > ftLen {
				goto slowPath
			}

			// Transition 1
			n1 := ft[sid.Offset()+int(d.byteToClass(haystack[pos]))]
			if n1.IsTagged() {
				goto slowPath
			}
			pos++
			if pos+2 >= end {
				sid = n1
				goto slowPath
			}

			// Transition 2
			n2 := ft[n1.Offset()+int(d.byteToClass(haystack[pos]))]
			if n2.IsTagged() {
				sid = n1
				goto slowPath
			}
			pos++
			if pos+1 >= end {
				sid = n2
				goto slowPath
			}

			// Transition 3
			n3 := ft[n2.Offset()+int(d.byteToClass(haystack[pos]))]
			if n3.IsTagged() {
				sid = n2
				goto slowPath
			}
			pos++

			// Transition 4
			n4 := ft[n3.Offset()+int(d.byteToClass(haystack[pos]))]
			if n4.IsTagged() {
				sid = n3
				goto slowPath
			}
			pos++
			sid = n4

			continue
		}

	slowPath:
		if pos >= end {
			break
		}

		// Resolve State for slow path (acceleration, word boundary, determinize).
		currentState = cache.getState(sid)
		if currentState == nil {
			return d.nfaFallback(haystack, startPos)
		}
		d.tryDetectAccelerationWithCache(currentState, cache)

		if exitBytes := currentState.AccelExitBytes(); len(exitBytes) > 0 {
			nextPos := d.accelerate(haystack, pos, exitBytes)
			if nextPos == -1 {
				return lastMatch
			}
			pos = nextPos
		}

		b := haystack[pos]

		if d.hasWordBoundary && d.checkWordBoundaryMatch(currentState, b) {
			return pos
		}

		// Flat table lookup for transition
		classIdx := int(d.byteToClass(b))
		offset := sid.Offset() + classIdx

		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			nextState, err := d.determinize(cache, currentState, b)
			if err != nil {
				return d.nfaFallback(haystack, startPos)
			}
			if nextState == nil {
				return lastMatch
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)
		case DeadState:
			return lastMatch
		default:
			sid = nextID
		}

		// 1-byte match delay: check AFTER transition, BEFORE pos advance.
		// With delay, match tag means previous state had NFA match.
		// Exclusive match end = pos (the consumed byte index), because delay
		// already shifts by 1 byte.
		// Rust: mat = Some(HalfMatch::new(pattern, at)) — at is byte index.
		if cache.IsMatchState(sid) {
			lastMatch = pos
		}

		pos++
	}

	// EOI: check for delayed match at end of input
	eoi := cache.getState(sid)
	if eoi != nil && d.checkEOIMatch(eoi) {
		return len(haystack)
	}

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
// Returns (nil, errCacheCleared) if cache was cleared and rebuilt.
//
//	The caller must re-obtain the current state from the start state
//	at the current position and continue searching.
//
// Returns (nil, error) if cache is full AND max clears exceeded,
//
//	or if determinization limit exceeded.
func (d *DFA) determinize(cache *DFACache, current *State, b byte) (*State, error) {
	// Need builder for move operations.
	// Use NewBuilderWithWordBoundary to pass pre-computed flag and avoid O(states) scan.
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)

	// Convert input byte to equivalence class index for transition storage
	// The actual byte value is still used for NFA move operations
	classIdx := d.byteToClass(b)

	// 1-byte match delay (Rust determinize mod.rs:254-286):
	// Check if source (current) state's NFA states contain a match state.
	// The NEW DFA state will be tagged as match if the OLD state had NFA match.
	// This delays match reporting by 1 byte, enabling correct look-around (^, $, \b).
	sourceHasMatch := builder.containsMatchState(current.NFAStates())

	// Compute next NFA state set via move operation WITH word context.
	// Leftmost-first (Rust determinize::next mod.rs:284):
	// When the source has NFA match, only include NFA states UP TO the first
	// Match state. States after Match are from the unanchored prefix restarting
	// the pattern — they must be excluded for leftmost-first semantics.
	// This matches Rust's "break" after finding Match in the iteration.
	var nfaStatesForMove []nfa.StateID
	if sourceHasMatch {
		nfaStatesForMove = d.filterStatesAfterMatch(current.NFAStates())
	} else {
		nfaStatesForMove = current.NFAStates()
	}
	nextNFAStates := builder.moveWithWordContext(nfaStatesForMove, b, current.IsFromWord())

	isMatch := sourceHasMatch

	// No transitions on this byte → dead state (or dead-end match state)
	if len(nextNFAStates) == 0 && !isMatch {
		// Normal dead state — no match in source either
		cache.SetFlatTransition(current.id, int(classIdx), DeadState)
		return nil, nil //nolint:nilnil // dead state is valid, not an error
	}
	// When len(nextNFAStates) == 0 && isMatch: source has NFA match but target
	// is dead. Create a dead-end match state so the search loop can observe
	// the delayed match before seeing dead transitions. Fall through below.

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

	// Compute state key INCLUDING word context AND match delay flag.
	// With match delay, the same NFA state set can produce both match and
	// non-match DFA states (depending on whether the source had NFA match).
	key := ComputeStateKeyWithWordAndMatch(nextNFAStates, nextIsFromWord, isMatch)

	// Check if state already exists in cache
	if existing, ok := cache.Get(key); ok {
		// Cache hit: reuse existing state
		// Use classIdx for transition storage (compressed alphabet)
		cache.SetFlatTransition(current.id, int(classIdx), existing.ID())
		return existing, nil
	}

	// Create new DFA state with word context and compressed alphabet stride
	newState := NewStateWithStride(InvalidState, nextNFAStates, isMatch, nextIsFromWord, d.AlphabetLen())

	// Pre-compute word boundary match flags to avoid per-byte checkWordBoundaryMatch.
	// This eliminates the expensive Builder + resolveWordBoundaries call in the hot loop.
	if d.hasWordBoundary && !isMatch {
		// Check: would resolving \b (word boundary satisfied) produce a match?
		wbStates := builder.resolveWordBoundaries(nextNFAStates, true)
		newState.matchAtWordBoundary = builder.containsMatchState(wbStates)
		// Check: would resolving \B (word boundary NOT satisfied) produce a match?
		nwbStates := builder.resolveWordBoundaries(nextNFAStates, false)
		newState.matchAtNonWordBoundary = builder.containsMatchState(nwbStates)
	}

	// Insert into cache
	_, err := cache.Insert(key, newState)
	if err != nil {
		// Cache is full. Try to clear and continue instead of NFA fallback.
		if clearErr := d.tryClearCache(cache); clearErr != nil {
			// Max clears exceeded - fall back to NFA
			return nil, clearErr
		}
		// Cache was cleared successfully. Return errCacheCleared to signal
		// the search loop that all state pointers are now stale and it must
		// re-obtain the start state at the current position.
		return nil, errCacheCleared
	}

	// Register state in ID lookup map
	cache.registerState(newState)

	// Add transition from current state to new state
	// Use classIdx for transition storage (compressed alphabet)
	cache.SetFlatTransition(current.id, int(classIdx), newState.ID())

	return newState, nil
}

// containsNFAMatch checks if any of the given NFA state IDs is a match state.
// Used for EOI match detection with 1-byte match delay: at end of input,
// we check the current DFA state's NFA states directly rather than following
// an EOI transition.
func containsNFAMatch(n *nfa.NFA, states []nfa.StateID) bool {
	for _, sid := range states {
		if n.IsMatch(sid) {
			return true
		}
	}
	return false
}

// filterStatesAfterMatch implements leftmost-first match semantics.
// When the source state has an NFA match, this filters the NFA state set to
// prevent new match attempts from starting while allowing existing pattern
// progress to complete.
//
// This combines two filtering operations (matching Rust determinize mod.rs:284):
// 1. Remove unanchored prefix states (split + any-byte) that enable new match starts
// 2. Remove the Match state itself (terminal, has no byte transitions)
//
// The remaining states are "in-progress" pattern states that can only extend
// the current match, not start new ones. When they can't extend, the DFA reaches
// dead state, terminating the search with the committed match.
func (d *DFA) filterStatesAfterMatch(states []nfa.StateID) []nfa.StateID {
	if d.nfa.IsAlwaysAnchored() {
		// For anchored patterns, just remove Match states
		filtered := make([]nfa.StateID, 0, len(states))
		for _, sid := range states {
			if !d.nfa.IsMatch(sid) {
				filtered = append(filtered, sid)
			}
		}
		return filtered
	}

	prefixStart := d.nfa.StartUnanchored()
	anchoredStart := d.nfa.StartAnchored()
	if prefixStart == anchoredStart {
		// No prefix compiled — just remove Match states
		filtered := make([]nfa.StateID, 0, len(states))
		for _, sid := range states {
			if !d.nfa.IsMatch(sid) {
				filtered = append(filtered, sid)
			}
		}
		return filtered
	}

	// Remove: prefix any-byte (prefixStart-1), prefix split (prefixStart), Match states
	prefixByte := prefixStart - 1
	filtered := make([]nfa.StateID, 0, len(states))
	for _, sid := range states {
		if sid == prefixStart || sid == prefixByte || d.nfa.IsMatch(sid) {
			continue
		}
		filtered = append(filtered, sid)
	}
	return filtered
}

// tryClearCache attempts to clear the DFA cache and rebuild the start state.
// Returns nil on success (cache was cleared, search can continue).
// Returns ErrCacheFull if the maximum number of cache clears has been exceeded.
//
// After a successful clear:
//   - All previously returned *State pointers are stale
//   - The start state is rebuilt and registered
//   - The StartTable is reset
//   - The states slice is reset (keeping allocated capacity)
//
// This is inspired by Rust regex-automata's try_clear_cache (hybrid/dfa.rs).
func (d *DFA) tryClearCache(cache *DFACache) error {
	// Check if we've exceeded the maximum number of cache clears
	if cache.ClearCount() >= d.config.MaxCacheClears {
		return ErrCacheFull
	}

	// Clear the cache, keeping allocated memory for reuse.
	// ClearKeepMemory also resets stateList and startTable.
	cache.ClearKeepMemory()

	// Rebuild the start state from scratch.
	// This is necessary because the search needs a valid start state to
	// re-initialize from the current position.
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)
	startLook := LookSetFromStartKind(StartText)
	startStateSet := builder.epsilonClosure([]nfa.StateID{d.nfa.StartUnanchored()}, startLook)
	// With 1-byte match delay, start states are never match states.
	startState := NewStateWithStride(StartState, startStateSet, false, false, d.AlphabetLen())

	key := ComputeStateKeyWithWord(startStateSet, false)
	_, _ = cache.Insert(key, startState) // Cannot fail: cache was just cleared
	cache.registerState(startState)

	// Cache the default start state in StartTable
	cache.startTable.Set(StartText, false, startState.ID())

	return nil
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
	// Use NewBuilderWithWordBoundary to avoid O(states) scan per call (Issue #105)
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)
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

	// Use NewBuilderWithWordBoundary to avoid O(states) scan per call (Issue #105)
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)
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
func (d *DFA) getStartState(cache *DFACache, haystack []byte, pos int, anchored bool) *State {
	// Determine start kind based on position and previous byte
	var kind StartKind
	if pos == 0 {
		kind = StartText
	} else {
		kind = cache.startTable.GetKind(haystack[pos-1])
	}

	// Check if already cached in StartTable
	stateID := cache.startTable.Get(kind, anchored)
	if stateID != InvalidState {
		return cache.getState(stateID)
	}

	// Not cached - compute and store with proper stride for ByteClasses compression
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)
	config := StartConfig{Kind: kind, Anchored: anchored}
	state, key := ComputeStartStateWithStride(builder, d.nfa, config, d.AlphabetLen())

	// Try to insert into cache using GetOrInsert
	// This handles the case where another goroutine may have inserted it
	insertedState, existed, err := cache.GetOrInsert(key, state)
	if err != nil {
		// Cache full - return the computed state anyway
		// (it won't be cached, but search can continue)
		return state
	}

	// Register in ID lookup map (only if we inserted a new state)
	if !existed {
		cache.registerState(insertedState)
	}

	// Cache in StartTable for fast lookup next time
	cache.startTable.Set(kind, anchored, insertedState.ID())

	return insertedState
}

// getStartStateForUnanchored is a convenience method for unanchored search.
// This is the common case for Find() operations.
func (d *DFA) getStartStateForUnanchored(cache *DFACache, haystack []byte, pos int) *State {
	return d.getStartState(cache, haystack, pos, false)
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
func (d *DFA) matchesEmpty(cache *DFACache) bool {
	// With 1-byte match delay, the start state is never tagged as match.
	// Check if the start state's NFA states contain a match (for empty patterns).
	startState := cache.getState(StartState)
	if startState != nil && containsNFAMatch(d.nfa, startState.NFAStates()) {
		return true
	}

	// Fall back to NFA for empty match check (handles word boundaries, etc.)
	start, end, matched := d.pikevm.Search([]byte{})
	return matched && start == 0 && end == 0
}

// tryDetectAccelerationWithCache attempts acceleration detection using flatTrans.
func (d *DFA) tryDetectAccelerationWithCache(state *State, cache *DFACache) {
	if state == nil || state.AccelChecked() {
		return
	}

	var exitBytes []byte
	if cache != nil && cache.stride > 0 {
		exitBytes = DetectAccelerationFromFlat(state.ID(), cache.flatTrans, cache.stride, d.byteClasses)
	}
	if len(exitBytes) > 0 {
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
// capacity is the cache limit in bytes.
func (d *DFA) CacheStats(cache *DFACache) (size int, capacity uint32, hits, misses uint64, hitRate float64) {
	size = cache.Size()
	capacity = uint32(cache.capacityBytes)
	hits, misses, hitRate = cache.Stats()
	return
}

// ResetCache clears the DFA cache and statistics.
// This forces all states to be recomputed on the next search.
// Primarily useful for testing and benchmarking.
func (d *DFA) ResetCache(cache *DFACache) {
	cache.Clear()
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
// Uses 4x loop unrolling for throughput: when enough bytes remain and all
// transitions are cached, 4 state transitions are batched together.
// This reduces branch overhead and improves instruction pipelining.
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
func (d *DFA) SearchReverse(cache *DFACache, haystack []byte, start, end int) int { //nolint:funlen // 4x unrolled reverse DFA search
	if end <= start || end > len(haystack) {
		return -1
	}

	// Get start state for reverse search
	currentState := d.getStartStateForReverse(cache, haystack, end)
	if currentState == nil {
		return d.nfaFallbackReverse(haystack, start, end)
	}

	lastMatch := -1
	// With 1-byte match delay, start states are never match states.

	at := end - 1

	// Hot loop: flat transition table (Rust approach).
	sid := currentState.id
	ft := cache.flatTrans
	ftLen := len(ft)

	if ftLen > 0 {
		_ = ft[ftLen-1]
	}

	// === 4x UNROLLED REVERSE LOOP ===
	// With match delay, any tagged state (including match) breaks to slow path.
	var revOff int
	var nextSID StateID
	for at >= start+3 {
		// Transition 1
		revOff = sid.Offset() + int(d.byteToClass(haystack[at]))
		if revOff >= ftLen {
			goto reverseSlowPath
		}
		nextSID = ft[revOff]
		if nextSID.IsTagged() {
			goto reverseSlowPath
		}
		sid = nextSID
		at--

		// Transition 2
		revOff = sid.Offset() + int(d.byteToClass(haystack[at]))
		if revOff >= ftLen {
			goto reverseSlowPath
		}
		nextSID = ft[revOff]
		if nextSID.IsTagged() {
			goto reverseSlowPath
		}
		sid = nextSID
		at--

		// Transition 3
		revOff = sid.Offset() + int(d.byteToClass(haystack[at]))
		if revOff >= ftLen {
			goto reverseSlowPath
		}
		nextSID = ft[revOff]
		if nextSID.IsTagged() {
			goto reverseSlowPath
		}
		sid = nextSID
		at--

		// Transition 4
		revOff = sid.Offset() + int(d.byteToClass(haystack[at]))
		if revOff >= ftLen {
			goto reverseSlowPath
		}
		nextSID = ft[revOff]
		if nextSID.IsTagged() {
			goto reverseSlowPath
		}
		sid = nextSID
		at--

		continue

	reverseSlowPath:
		break
	}

	// === SINGLE-BYTE REVERSE TAIL LOOP ===
	for at >= start {
		b := haystack[at]

		classIdx := int(d.byteToClass(b))
		offset := sid.Offset() + classIdx

		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			currentState = cache.getState(sid)
			if currentState == nil {
				return d.nfaFallbackReverse(haystack, start, end)
			}
			nextState, err := d.determinize(cache, currentState, b)
			if err != nil {
				if isCacheCleared(err) {
					currentState = d.getStartStateForReverse(cache, haystack, at+1)
					if currentState == nil {
						return d.nfaFallbackReverse(haystack, start, end)
					}
					sid = currentState.id
					ft = cache.flatTrans
					ftLen = len(ft)
					continue
				}
				return d.nfaFallbackReverse(haystack, start, end)
			}
			if nextState == nil {
				return lastMatch
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)

		case DeadState:
			return lastMatch

		default:
			sid = nextID
		}

		// 1-byte match delay for reverse: the match tag on the new state means
		// the OLD state had NFA match. In reverse search, the match position
		// is at+1 (one byte forward from current, since we're going backward).
		// Rust: mat = Some(HalfMatch::new(pattern, at + 1))
		if cache.IsMatchState(sid) {
			lastMatch = at + 1
		}

		at--
	}

	// EOI for reverse: at region start, check if current state's NFA states
	// contain a delayed match. If so, the match starts at 'start'.
	eoi := cache.getState(sid)
	if eoi != nil && containsNFAMatch(d.nfa, eoi.NFAStates()) {
		lastMatch = start
	}

	return lastMatch
}

// SearchReverseLimitedQuadratic is returned by SearchReverseLimited when the reverse
// scan reaches the minStart bound, indicating potential quadratic behavior.
// The caller should fall back to a non-quadratic engine (e.g., PikeVM).
const SearchReverseLimitedQuadratic = -2

// SearchReverseLimited performs a backward DFA search like SearchReverse, but with
// an anti-quadratic guard. If the reverse scan reaches minStart without finding a
// dead state, it returns SearchReverseLimitedQuadratic (-2) to signal that the
// scan was limited and the caller should use a fallback strategy.
//
// This prevents O(n^2) behavior in reverse suffix/inner searches where suffix
// false positives cause repeated scans over the same region.
//
// Parameters:
//   - haystack: the full input
//   - start: the absolute lower bound for the search (from input span)
//   - end: the end index (exclusive, search starts from end-1)
//   - minStart: the anti-quadratic guard position; if the scan would go below this,
//     return SearchReverseLimitedQuadratic instead
//
// Returns:
//   - >= 0: match start position
//   - -1: no match (dead state reached, definitively no match)
//   - -2 (SearchReverseLimitedQuadratic): scan was limited by minStart, caller should
//     retry with a different strategy
func (d *DFA) SearchReverseLimited(cache *DFACache, haystack []byte, start, end, minStart int) int {
	if end <= start || end > len(haystack) {
		return -1
	}

	currentState := d.getStartStateForReverse(cache, haystack, end)
	if currentState == nil {
		return d.nfaFallbackReverse(haystack, start, end)
	}

	lastMatch := -1
	// With 1-byte match delay, start states are never match states.

	lowerBound := start
	if minStart > lowerBound {
		lowerBound = minStart
	}

	// Hot loop: flat transition table (Rust approach).
	sid := currentState.id
	ft := cache.flatTrans
	ftLen := len(ft)

	for at := end - 1; at >= lowerBound; at-- {
		b := haystack[at]

		classIdx := int(d.byteToClass(b))
		offset := sid.Offset() + classIdx

		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			currentState = cache.getState(sid)
			if currentState == nil {
				return d.nfaFallbackReverse(haystack, start, end)
			}
			nextState, err := d.determinize(cache, currentState, b)
			if err != nil {
				if isCacheCleared(err) {
					currentState = d.getStartStateForReverse(cache, haystack, at+1)
					if currentState == nil {
						return d.nfaFallbackReverse(haystack, start, end)
					}
					sid = currentState.id
					ft = cache.flatTrans
					ftLen = len(ft)
					at++ // Will be decremented by for-loop
					continue
				}
				return d.nfaFallbackReverse(haystack, start, end)
			}
			if nextState == nil {
				return lastMatch
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)

		case DeadState:
			return lastMatch

		default:
			sid = nextID
		}

		// 1-byte match delay for reverse: match position is at+1
		if cache.IsMatchState(sid) {
			lastMatch = at + 1
		}
	}

	// EOI for reverse: check delayed match at region start
	eoi := cache.getState(sid)
	if eoi != nil && containsNFAMatch(d.nfa, eoi.NFAStates()) {
		lastMatch = lowerBound
	}

	if lowerBound > start && lastMatch < 0 {
		return SearchReverseLimitedQuadratic
	}

	return lastMatch
}

// IsMatchReverse performs backward DFA search and returns true if any match is found.
// This is optimized for early termination - returns true as soon as any match state is reached.
//
// Zero-allocation implementation that reads bytes in reverse order.
func (d *DFA) IsMatchReverse(cache *DFACache, haystack []byte, start, end int) bool {
	if end <= start || end > len(haystack) {
		return false
	}

	currentState := d.getStartStateForReverse(cache, haystack, end)
	if currentState == nil {
		_, _, matched := d.pikevm.Search(haystack[start:end])
		return matched
	}

	// With 1-byte match delay, start states are never match states.

	// Hot loop: flat transition table (Rust approach).
	sid := currentState.id
	ft := cache.flatTrans
	ftLen := len(ft)

	for at := end - 1; at >= start; at-- {
		b := haystack[at]

		classIdx := int(d.byteToClass(b))
		offset := sid.Offset() + classIdx

		var nextID StateID
		if offset < ftLen {
			nextID = ft[offset]
		} else {
			nextID = InvalidState
		}

		switch nextID {
		case InvalidState:
			currentState = cache.getState(sid)
			if currentState == nil {
				_, _, matched := d.pikevm.Search(haystack[start:end])
				return matched
			}
			nextState, err := d.determinize(cache, currentState, b)
			if err != nil {
				if isCacheCleared(err) {
					currentState = d.getStartStateForReverse(cache, haystack, at+1)
					if currentState == nil {
						_, _, matched := d.pikevm.Search(haystack[start:end])
						return matched
					}
					sid = currentState.id
					ft = cache.flatTrans
					ftLen = len(ft)
					at++ // Will be decremented by for-loop
					continue
				}
				_, _, matched := d.pikevm.Search(haystack[start:end])
				return matched
			}
			if nextState == nil {
				return false
			}
			sid = nextState.id
			ft = cache.flatTrans
			ftLen = len(ft)

		case DeadState:
			return false

		default:
			sid = nextID
		}

		// 1-byte match delay: match detected after transition
		if cache.IsMatchState(sid) {
			return true
		}
	}

	// EOI for reverse: check if current state's NFA states contain match
	eoi := cache.getState(sid)
	return eoi != nil && containsNFAMatch(d.nfa, eoi.NFAStates())
}

// getStartStateForReverse returns the appropriate start state for reverse search.
// For reverse search, we need to consider the context at the END of the search region.
func (d *DFA) getStartStateForReverse(cache *DFACache, haystack []byte, end int) *State {
	// For reverse search, the "start" is at the end of the region
	// Use StartText kind if at end of haystack, otherwise determine from next byte
	var kind StartKind
	if end >= len(haystack) {
		kind = StartText // End of input = "start of text" for reverse DFA
	} else {
		kind = cache.startTable.GetKind(haystack[end])
	}

	// Check if already cached in StartTable (use anchored=false for reverse)
	stateID := cache.startTable.Get(kind, false)
	if stateID != InvalidState {
		return cache.getState(stateID)
	}

	// Not cached - compute and store with proper stride for ByteClasses compression
	builder := NewBuilderWithWordBoundary(d.nfa, d.config, d.hasWordBoundary)
	cfg := StartConfig{Kind: kind, Anchored: false}
	state, key := ComputeStartStateWithStride(builder, d.nfa, cfg, d.AlphabetLen())

	insertedState, existed, err := cache.GetOrInsert(key, state)
	if err != nil {
		return state
	}

	if !existed {
		cache.registerState(insertedState)
	}

	cache.startTable.Set(kind, false, insertedState.ID())
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
