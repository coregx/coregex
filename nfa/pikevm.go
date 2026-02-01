package nfa

import (
	"unicode/utf8"

	"github.com/coregx/coregex/internal/conv"
	"github.com/coregx/coregex/internal/sparse"
)

// PikeVM implements the Pike VM algorithm for NFA execution.
// It simulates the NFA by maintaining a set of active states and
// exploring all possible paths through the automaton.
//
// The Pike VM is slower than DFA-based approaches but handles all
// regex features including backreferences (future) and capturing groups.
//
// Thread safety: PikeVM configuration (nfa) is immutable after creation.
// For thread-safe concurrent usage, use *WithState methods with external PikeVMState.
// The legacy methods without state use internal state and are NOT thread-safe.
type PikeVM struct {
	nfa *NFA

	// internalState is used by legacy non-thread-safe methods.
	// For concurrent usage, use *WithState methods with external PikeVMState.
	internalState PikeVMState
}

// PikeVMState holds mutable per-search state for PikeVM.
// This struct should be pooled (via sync.Pool) for concurrent usage.
// Each goroutine must use its own PikeVMState instance.
type PikeVMState struct {
	// Thread queues for current and next generation
	// Pre-allocated to avoid allocations during search
	Queue     []thread
	NextQueue []thread

	// Sparse set for tracking visited states in current generation
	// This prevents processing the same state multiple times
	Visited *sparse.SparseSet

	// epsilonStack is used for loop-based epsilon closure in IsMatch (Rust pattern).
	// Only stores StateID for split right branches - minimizes frame overhead.
	// Reference: rust-regex/regex-automata/src/nfa/thompson/pikevm.rs:2198
	epsilonStack []StateID

	// Longest enables leftmost-longest (POSIX) matching semantics.
	// By default (false), uses leftmost-first (Perl) semantics where
	// the first alternative wins. When true, the longest match wins.
	Longest bool
}

// isBetterMatch returns true if the candidate match is better than the current best.
// This implements both leftmost-first and leftmost-longest match semantics.
// Uses internal state for the longest flag.
func (p *PikeVM) isBetterMatch(bestStart, bestEnd int, bestPriority uint32,
	candStart, candEnd int, candPriority uint32) bool {
	return isBetterMatchWithLongest(p.internalState.Longest, bestStart, bestEnd, bestPriority,
		candStart, candEnd, candPriority)
}

// isBetterMatchWithLongest is the stateless version of isBetterMatch.
func isBetterMatchWithLongest(longest bool, bestStart, bestEnd int, bestPriority uint32,
	candStart, candEnd int, candPriority uint32) bool {
	// No current best - candidate always wins
	if bestStart == -1 {
		return true
	}
	// Leftmost start position always wins
	if candStart < bestStart {
		return true
	}
	if candStart > bestStart {
		return false
	}
	// Same start position - apply semantics
	if longest {
		// Leftmost-longest (POSIX): longer match wins (ignore priority)
		return candEnd > bestEnd
	}
	// Leftmost-first (Perl): higher priority (lower number) wins first,
	// then longer match for same priority (greedy extension)
	if candPriority < bestPriority {
		return true
	}
	return candPriority == bestPriority && candEnd > bestEnd
}

// thread represents an execution thread in the PikeVM.
// Each thread tracks a position in the NFA state graph and capture positions.
type thread struct {
	state    StateID
	startPos int         // Position where this thread's match attempt started
	captures cowCaptures // COW capture positions: [start0, end0, start1, end1, ...] (-1 = not set)
	priority uint32      // Thread priority for alternation (lower = higher priority)
	tookLeft bool        // True if any alternation left branch was taken (used for greedy reset)
}

// cowCaptures implements copy-on-write semantics for capture slots.
// Multiple threads can share the same underlying data until modification.
// This reduces allocations in PikeVM when threads split but don't modify captures.
type cowCaptures struct {
	shared *sharedCaptures
}

type sharedCaptures struct {
	data []int
	refs int
}

// clone increments ref count and returns a reference to the same data (no copy)
func (c cowCaptures) clone() cowCaptures {
	if c.shared == nil {
		return cowCaptures{}
	}
	c.shared.refs++
	return cowCaptures{shared: c.shared}
}

// update modifies a capture slot, copying only if refs > 1 (copy-on-write)
func (c cowCaptures) update(slotIndex, value int) cowCaptures {
	if c.shared == nil || slotIndex < 0 || slotIndex >= len(c.shared.data) {
		return c
	}
	if c.shared.refs > 1 {
		// shared - copy before write
		c.shared.refs--
		newData := make([]int, len(c.shared.data))
		copy(newData, c.shared.data)
		newData[slotIndex] = value
		return cowCaptures{
			shared: &sharedCaptures{
				data: newData,
				refs: 1,
			},
		}
	}
	// exclusive owner - modify in place
	c.shared.data[slotIndex] = value
	return c
}

// get returns the capture data (may be nil)
func (c cowCaptures) get() []int {
	if c.shared == nil {
		return nil
	}
	return c.shared.data
}

// copyData returns a copy of the underlying data (for saving best match)
func (c cowCaptures) copyData() []int {
	if c.shared == nil {
		return nil
	}
	dst := make([]int, len(c.shared.data))
	copy(dst, c.shared.data)
	return dst
}

// Match represents a successful regex match with start and end positions
type Match struct {
	Start int
	End   int
}

// MatchWithCaptures represents a match including capture group positions.
// Captures is a slice where Captures[i] is [start, end] for group i.
// Group 0 is the entire match.
type MatchWithCaptures struct {
	Start    int
	End      int
	Captures [][]int // Captures[i] = [start, end] for group i, or nil if not captured
}

// NewPikeVM creates a new PikeVM for executing the given NFA
func NewPikeVM(nfa *NFA) *PikeVM {
	p := &PikeVM{
		nfa: nfa,
	}
	// Initialize internal state
	p.initState(&p.internalState)
	return p
}

// initState initializes a PikeVMState for use with this PikeVM.
// Call this to prepare a state before using it with *WithState methods.
func (p *PikeVM) initState(state *PikeVMState) {
	// Pre-allocate thread queues with capacity based on NFA size
	capacity := p.nfa.States()
	if capacity < 16 {
		capacity = 16
	}

	state.Queue = make([]thread, 0, capacity)
	state.NextQueue = make([]thread, 0, capacity)
	state.Visited = sparse.NewSparseSet(conv.IntToUint32(capacity))
	// Pre-allocate epsilon stack for loop-based closure in IsMatch (Rust pattern)
	state.epsilonStack = make([]StateID, 0, capacity)
}

// NewPikeVMState creates a new mutable state for use with PikeVM.
// The state must be initialized by calling PikeVM.InitState before use.
// This should be pooled via sync.Pool for concurrent usage.
func NewPikeVMState() *PikeVMState {
	return &PikeVMState{}
}

// InitState initializes this state for use with the given PikeVM.
// Must be called before using the state with *WithState methods.
func (p *PikeVM) InitState(state *PikeVMState) {
	p.initState(state)
}

// NumStates returns the number of NFA states (for state allocation).
func (p *PikeVM) NumStates() int {
	return p.nfa.States()
}

// SetLongest enables or disables leftmost-longest (POSIX) matching semantics.
// By default, uses leftmost-first (Perl) semantics where first alternative wins.
// When longest=true, the longest match at the same start position wins.
// Note: This modifies internal state. For thread-safe usage, set Longest directly on PikeVMState.
func (p *PikeVM) SetLongest(longest bool) {
	p.internalState.Longest = longest
}

// newCaptures creates a new COW capture slots initialized to -1 (unset)
func (p *PikeVM) newCaptures() cowCaptures {
	numSlots := p.nfa.CaptureCount() * 2 // Each group has start and end
	if numSlots == 0 {
		return cowCaptures{}
	}
	data := make([]int, numSlots)
	for i := range data {
		data[i] = -1
	}
	return cowCaptures{
		shared: &sharedCaptures{
			data: data,
			refs: 1,
		},
	}
}

// updateCapture updates a capture slot using COW semantics
func updateCapture(caps cowCaptures, groupIndex uint32, isStart bool, pos int) cowCaptures {
	slotIndex := int(groupIndex) * 2
	if !isStart {
		slotIndex++
	}
	return caps.update(slotIndex, pos)
}

// Search finds the first match in the haystack.
// Returns (start, end, true) if a match is found, or (-1, -1, false) if not.
//
// The search is unanchored by default (matches anywhere in haystack)
// unless the NFA was compiled with anchored mode.
//
// This method uses internal state and is NOT thread-safe.
// For concurrent usage, use SearchWithState.
func (p *PikeVM) Search(haystack []byte) (int, int, bool) {
	return p.SearchAt(haystack, 0)
}

// IsMatch returns true if the pattern matches anywhere in the haystack.
// This is optimized for boolean-only matching - it returns as soon as any
// match is found without computing exact match positions.
//
// This is significantly faster than Search() when you only need to know
// if a match exists, not where it is.
func (p *PikeVM) IsMatch(haystack []byte) bool {
	if len(haystack) == 0 {
		return p.matchesEmpty()
	}

	if p.nfa.IsAnchored() {
		return p.isMatchAnchored(haystack)
	}

	return p.isMatchUnanchored(haystack)
}

// isMatchUnanchored implements fast boolean-only matching for unanchored patterns.
// Unlike searchUnanchoredAt, this doesn't track match positions - just returns
// true as soon as any match state is reached.
func (p *PikeVM) isMatchUnanchored(haystack []byte) bool {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	// Process each byte position
	for pos := 0; pos <= len(haystack); pos++ {
		// Add new start thread at current position
		p.internalState.Visited.Clear()
		p.addThreadForMatch(p.nfa.StartAnchored(), haystack, pos)

		// Check for matches in current generation - return immediately on first match
		for _, t := range p.internalState.Queue {
			if p.nfa.IsMatch(t.state) {
				return true // FAST EXIT - no position tracking needed
			}
		}

		if pos >= len(haystack) {
			break
		}

		// Process current byte for all active threads
		if len(p.internalState.Queue) > 0 {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.Queue {
				p.stepForMatch(t, b, haystack, pos+1)
			}
		}

		// Swap queues
		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	return false
}

// isMatchAnchored implements fast boolean-only matching for anchored patterns.
func (p *PikeVM) isMatchAnchored(haystack []byte) bool {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	// Initialize with start state
	p.addThreadForMatch(p.nfa.StartAnchored(), haystack, 0)

	// Process each byte position
	for pos := 0; pos <= len(haystack); pos++ {
		// Check for match - return immediately
		for _, t := range p.internalState.Queue {
			if p.nfa.IsMatch(t.state) {
				return true
			}
		}

		if len(p.internalState.Queue) == 0 || pos >= len(haystack) {
			break
		}

		b := haystack[pos]
		p.internalState.Visited.Clear()

		for _, t := range p.internalState.Queue {
			p.stepForMatch(t, b, haystack, pos+1)
		}

		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	return false
}

// addThreadForMatch adds thread for IsMatch - loop-based epsilon closure.
// This follows the Rust regex pattern: inner loop for linear chains,
// stack only for split right branches.
// Reference: rust-regex/regex-automata/src/nfa/thompson/pikevm.rs:1664-1749
func (p *PikeVM) addThreadForMatch(id StateID, haystack []byte, pos int) {
	// Use loop-based epsilon closure instead of recursion
	p.internalState.epsilonStack = p.internalState.epsilonStack[:0]
	sid := id

	for {
		// Check visited - Insert returns false if already present
		if !p.internalState.Visited.Insert(uint32(sid)) {
			// Pop next state from stack
			if len(p.internalState.epsilonStack) == 0 {
				return
			}
			n := len(p.internalState.epsilonStack)
			sid = p.internalState.epsilonStack[n-1]
			p.internalState.epsilonStack = p.internalState.epsilonStack[:n-1]
			continue
		}

		state := p.nfa.State(sid)
		if state == nil {
			// Pop next state from stack
			if len(p.internalState.epsilonStack) == 0 {
				return
			}
			n := len(p.internalState.epsilonStack)
			sid = p.internalState.epsilonStack[n-1]
			p.internalState.epsilonStack = p.internalState.epsilonStack[:n-1]
			continue
		}

		switch state.Kind() {
		case StateMatch, StateByteRange, StateSparse, StateRuneAny, StateRuneAnyNotNL:
			// Terminal states - add to queue and pop
			p.internalState.Queue = append(p.internalState.Queue, thread{state: sid})

		case StateEpsilon:
			// Linear chain - continue inner loop (no push)
			if next := state.Epsilon(); next != InvalidState {
				sid = next
				continue
			}

		case StateSplit:
			// Binary split - push right, continue with left
			left, right := state.Split()
			if right != InvalidState {
				p.internalState.epsilonStack = append(p.internalState.epsilonStack, right)
			}
			if left != InvalidState {
				sid = left
				continue
			}

		case StateCapture:
			// Capture is epsilon for IsMatch - continue inner loop
			if _, _, next := state.Capture(); next != InvalidState {
				sid = next
				continue
			}

		case StateLook:
			// Check assertion - continue if passes
			look, next := state.Look()
			if checkLookAssertion(look, haystack, pos) && next != InvalidState {
				sid = next
				continue
			}

		case StateFail:
			// Dead state - do nothing
		}

		// Pop next state from stack
		if len(p.internalState.epsilonStack) == 0 {
			return
		}
		n := len(p.internalState.epsilonStack)
		sid = p.internalState.epsilonStack[n-1]
		p.internalState.epsilonStack = p.internalState.epsilonStack[:n-1]
	}
}

// stepForMatch processes byte transition for IsMatch - simplified
func (p *PikeVM) stepForMatch(t thread, b byte, haystack []byte, nextPos int) {
	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	switch state.Kind() {
	case StateByteRange:
		lo, hi, next := state.ByteRange()
		if b >= lo && b <= hi {
			p.addThreadToNextForMatch(next, haystack, nextPos)
		}

	case StateSparse:
		for _, tr := range state.Transitions() {
			if b >= tr.Lo && b <= tr.Hi {
				p.addThreadToNextForMatch(tr.Next, haystack, nextPos)
			}
		}

	case StateRuneAny:
		if b >= 0x80 && b <= 0xBF {
			p.internalState.NextQueue = append(p.internalState.NextQueue, t)
			return
		}
		runePos := nextPos - 1
		if runePos < len(haystack) {
			r, width := utf8.DecodeRune(haystack[runePos:])
			if r != utf8.RuneError || width == 1 {
				next := state.RuneAny()
				newPos := runePos + width
				p.addThreadToNextForMatch(next, haystack, newPos)
			}
		}

	case StateRuneAnyNotNL:
		if b >= 0x80 && b <= 0xBF {
			p.internalState.NextQueue = append(p.internalState.NextQueue, t)
			return
		}
		runePos := nextPos - 1
		if runePos < len(haystack) {
			r, width := utf8.DecodeRune(haystack[runePos:])
			if (r != utf8.RuneError || width == 1) && r != '\n' {
				next := state.RuneAnyNotNL()
				newPos := runePos + width
				p.addThreadToNextForMatch(next, haystack, newPos)
			}
		}
	}
}

// addThreadToNextForMatch adds to next queue for IsMatch - loop-based epsilon closure.
// This follows the Rust regex pattern: inner loop for linear chains,
// stack only for split right branches.
func (p *PikeVM) addThreadToNextForMatch(id StateID, haystack []byte, pos int) {
	// Use loop-based epsilon closure instead of recursion
	p.internalState.epsilonStack = p.internalState.epsilonStack[:0]
	sid := id

	for {
		// Check visited - Insert returns false if already present
		if !p.internalState.Visited.Insert(uint32(sid)) {
			// Pop next state from stack
			if len(p.internalState.epsilonStack) == 0 {
				return
			}
			n := len(p.internalState.epsilonStack)
			sid = p.internalState.epsilonStack[n-1]
			p.internalState.epsilonStack = p.internalState.epsilonStack[:n-1]
			continue
		}

		state := p.nfa.State(sid)
		if state == nil {
			// Pop next state from stack
			if len(p.internalState.epsilonStack) == 0 {
				return
			}
			n := len(p.internalState.epsilonStack)
			sid = p.internalState.epsilonStack[n-1]
			p.internalState.epsilonStack = p.internalState.epsilonStack[:n-1]
			continue
		}

		switch state.Kind() {
		case StateMatch, StateByteRange, StateSparse, StateRuneAny, StateRuneAnyNotNL:
			// Terminal states - add to next queue and pop
			p.internalState.NextQueue = append(p.internalState.NextQueue, thread{state: sid})

		case StateEpsilon:
			// Linear chain - continue inner loop (no push)
			if next := state.Epsilon(); next != InvalidState {
				sid = next
				continue
			}

		case StateSplit:
			// Binary split - push right, continue with left
			left, right := state.Split()
			if right != InvalidState {
				p.internalState.epsilonStack = append(p.internalState.epsilonStack, right)
			}
			if left != InvalidState {
				sid = left
				continue
			}

		case StateCapture:
			// Capture is epsilon for IsMatch - continue inner loop
			if _, _, next := state.Capture(); next != InvalidState {
				sid = next
				continue
			}

		case StateLook:
			// Check assertion - continue if passes
			look, next := state.Look()
			if checkLookAssertion(look, haystack, pos) && next != InvalidState {
				sid = next
				continue
			}

		case StateFail:
			// Dead state - do nothing
		}

		// Pop next state from stack
		if len(p.internalState.epsilonStack) == 0 {
			return
		}
		n := len(p.internalState.epsilonStack)
		sid = p.internalState.epsilonStack[n-1]
		p.internalState.epsilonStack = p.internalState.epsilonStack[:n-1]
	}
}

// SearchAt finds the first match in the haystack starting from position 'at'.
// Returns (start, end, true) if a match is found, or (-1, -1, false) if not.
//
// This method is used by FindAll* operations to correctly handle anchors like ^.
// Unlike Search, it takes the FULL haystack and a starting position, so assertions
// like ^ correctly check against the original input start, not a sliced position.
func (p *PikeVM) SearchAt(haystack []byte, at int) (int, int, bool) {
	if at > len(haystack) {
		return -1, -1, false
	}

	if at == len(haystack) {
		// At end of input - check if empty string matches at this position.
		// We need to pass the actual haystack and position to correctly
		// evaluate look assertions like ^ and $ in multiline mode.
		if p.matchesEmptyAt(haystack, at) {
			return at, at, true
		}
		return -1, -1, false
	}

	if len(haystack) == 0 {
		// Check if empty string matches
		if p.matchesEmpty() {
			return 0, 0, true
		}
		return -1, -1, false
	}

	if p.nfa.IsAnchored() {
		// Anchored mode: only try the starting position
		start, end, matched := p.searchAt(haystack, at)
		return start, end, matched
	}

	// Unanchored mode: use O(n) parallel simulation starting from 'at'
	return p.searchUnanchoredAt(haystack, at)
}

// searchUnanchoredAt implements Thompson's parallel NFA simulation for unanchored search.
// This is used by SearchAt to correctly handle anchors when searching from non-zero positions.
func (p *PikeVM) searchUnanchoredAt(haystack []byte, startAt int) (int, int, bool) {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	// Track leftmost-first match (with priority for alternation)
	bestStart := -1
	bestEnd := -1
	var bestPriority uint32

	// Check if NFA is anchored at start (e.g., reverse NFA for $ patterns)
	isAnchored := p.nfa.IsAnchored()

	// Process each byte position once, starting from startAt
	for pos := startAt; pos <= len(haystack); pos++ {
		// Add new start thread at current position (simulates .*? prefix)
		// We use StartAnchored() here (not StartUnanchored()) because the prefix
		// is simulated by restarting at each position, not embedded in the NFA.
		// This ensures correct startPos tracking (set to current pos).
		// Stop adding new starts once we've found a match (non-greedy behavior)
		// For anchored NFA, only try at position 0 (like ^ anchor behavior)
		if bestStart == -1 && (!isAnchored || pos == 0) {
			p.internalState.Visited.Clear()
			p.addThread(thread{state: p.nfa.StartAnchored(), startPos: pos, priority: 0}, haystack, pos)
		}

		// Check for matches in current generation
		// We check AFTER adding threads to ensure we capture all potential matches
		for _, t := range p.internalState.Queue {
			if p.nfa.IsMatch(t.state) && p.isBetterMatch(bestStart, bestEnd, bestPriority, t.startPos, pos, t.priority) {
				bestStart = t.startPos
				bestEnd = pos
				bestPriority = t.priority
			}
		}

		// If at end of input, stop (but still process remaining threads above)
		if pos >= len(haystack) {
			break
		}

		// If we have a match and no threads could produce a leftmost match, stop early
		// A thread can only produce a leftmost match if its startPos <= current best
		if bestStart != -1 {
			hasLeftmostCandidate := false
			for _, t := range p.internalState.Queue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
			}
		}

		// Process current byte for all active threads
		// Note: We continue even if queue is empty because we might add
		// new start threads at the next position (unanchored search)
		if len(p.internalState.Queue) > 0 {
			b := haystack[pos]
			p.internalState.Visited.Clear() // Clear before processing to track visited states for epsilon closures
			for _, t := range p.internalState.Queue {
				p.step(t, b, haystack, pos+1)
			}
		}

		// Swap queues for next iteration
		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	if bestStart != -1 {
		return bestStart, bestEnd, true
	}
	return -1, -1, false
}

// SearchBetween finds the first match in the range [startAt, maxEnd] of haystack.
// This is an optimization for cases where we know the match ends at or before maxEnd
// (e.g., after DFA found the end position). It avoids scanning the full haystack.
//
// Parameters:
//   - haystack: the input byte slice
//   - startAt: minimum position to start searching
//   - maxEnd: maximum position where match can end (exclusive for search, inclusive for match)
//
// Returns (start, end, found) where start >= startAt and end <= maxEnd.
//
// Performance: O(maxEnd - startAt) instead of O(len(haystack) - startAt).
func (p *PikeVM) SearchBetween(haystack []byte, startAt, maxEnd int) (int, int, bool) {
	if startAt > len(haystack) || startAt >= maxEnd {
		return -1, -1, false
	}

	// Clamp maxEnd to haystack length
	if maxEnd > len(haystack) {
		maxEnd = len(haystack)
	}

	if p.nfa.IsAnchored() {
		// Anchored mode: only try at startAt position
		start, end, matched := p.searchAt(haystack[:maxEnd], startAt)
		return start, end, matched
	}

	// Unanchored mode: parallel NFA simulation limited to [startAt, maxEnd]
	return p.searchUnanchoredBetween(haystack, startAt, maxEnd)
}

// searchUnanchoredBetween implements Thompson's parallel NFA simulation for bounded search.
// It's identical to searchUnanchoredAt but stops at maxEnd instead of len(haystack).
func (p *PikeVM) searchUnanchoredBetween(haystack []byte, startAt, maxEnd int) (int, int, bool) {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	// Track leftmost-first match (with priority for alternation)
	bestStart := -1
	bestEnd := -1
	var bestPriority uint32

	// Check if NFA is anchored at start (e.g., reverse NFA for $ patterns)
	isAnchored := p.nfa.IsAnchored()

	// Process each byte position, stopping at maxEnd instead of len(haystack)
	for pos := startAt; pos <= maxEnd; pos++ {
		// Add new start thread at current position (simulates .*? prefix)
		if bestStart == -1 && (!isAnchored || pos == 0) {
			p.internalState.Visited.Clear()
			p.addThread(thread{state: p.nfa.StartAnchored(), startPos: pos, priority: 0}, haystack, pos)
		}

		// Check for matches in current generation
		for _, t := range p.internalState.Queue {
			if p.nfa.IsMatch(t.state) && p.isBetterMatch(bestStart, bestEnd, bestPriority, t.startPos, pos, t.priority) {
				bestStart = t.startPos
				bestEnd = pos
				bestPriority = t.priority
			}
		}

		// If at boundary, stop
		if pos >= maxEnd || pos >= len(haystack) {
			break
		}

		// If we have a match and no threads could produce a leftmost match, stop early
		if bestStart != -1 {
			hasLeftmostCandidate := false
			for _, t := range p.internalState.Queue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
			}
		}

		// Process current byte for all active threads
		if len(p.internalState.Queue) > 0 {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.Queue {
				p.step(t, b, haystack, pos+1)
			}
		}

		// Swap queues for next iteration
		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	if bestStart != -1 {
		return bestStart, bestEnd, true
	}
	return -1, -1, false
}

// SearchWithCaptures finds the first match with capture group positions.
// Returns nil if no match is found.
func (p *PikeVM) SearchWithCaptures(haystack []byte) *MatchWithCaptures {
	return p.SearchWithCapturesAt(haystack, 0)
}

// SearchWithCapturesAt finds the first match with capture group positions,
// starting from position 'at' in the haystack.
// Returns nil if no match is found.
//
// This method is used by FindAll* operations to correctly handle anchors like ^.
// Unlike SearchWithCaptures, it takes the FULL haystack and a starting position.
func (p *PikeVM) SearchWithCapturesAt(haystack []byte, at int) *MatchWithCaptures {
	if at > len(haystack) {
		return nil
	}

	if at == len(haystack) {
		// At end of input - check if empty string matches
		if p.matchesEmpty() {
			return &MatchWithCaptures{
				Start:    at,
				End:      at,
				Captures: p.buildCapturesResult(nil, at, at),
			}
		}
		return nil
	}

	if len(haystack) == 0 {
		// Check if empty string matches
		if p.matchesEmpty() {
			return &MatchWithCaptures{
				Start:    0,
				End:      0,
				Captures: p.buildCapturesResult(nil, 0, 0),
			}
		}
		return nil
	}

	if p.nfa.IsAnchored() {
		return p.searchAtWithCaptures(haystack, at)
	}

	return p.searchUnanchoredWithCapturesAt(haystack, at)
}

// searchUnanchoredWithCapturesAt implements Thompson's parallel NFA simulation with capture groups.
func (p *PikeVM) searchUnanchoredWithCapturesAt(haystack []byte, startAt int) *MatchWithCaptures {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	// Track leftmost-first match (with priority for alternation)
	bestStart := -1
	bestEnd := -1
	var bestPriority uint32
	var bestCaptures []int

	// Process each byte position once, starting from startAt
	for pos := startAt; pos <= len(haystack); pos++ {
		// Add new start thread at current position (simulates .*? prefix)
		// Use StartAnchored() to ensure correct startPos tracking
		if bestStart == -1 {
			p.internalState.Visited.Clear()
			caps := p.newCaptures()
			p.addThread(thread{state: p.nfa.StartAnchored(), startPos: pos, captures: caps, priority: 0}, haystack, pos)
		}

		// Check for matches in current generation
		for _, t := range p.internalState.Queue {
			if p.nfa.IsMatch(t.state) && p.isBetterMatch(bestStart, bestEnd, bestPriority, t.startPos, pos, t.priority) {
				bestStart = t.startPos
				bestEnd = pos
				bestPriority = t.priority
				bestCaptures = t.captures.copyData()
			}
		}

		if pos >= len(haystack) {
			break
		}

		// Early termination check
		if bestStart != -1 {
			hasLeftmostCandidate := false
			for _, t := range p.internalState.Queue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
			}
		}

		// Process current byte for all active threads
		// Continue even if queue is empty (unanchored search may add new starts)
		if len(p.internalState.Queue) > 0 {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.Queue {
				p.step(t, b, haystack, pos+1)
			}
		}

		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	if bestStart != -1 {
		return &MatchWithCaptures{
			Start:    bestStart,
			End:      bestEnd,
			Captures: p.buildCapturesResult(bestCaptures, bestStart, bestEnd),
		}
	}
	return nil
}

// searchAtWithCaptures is like searchAt but returns captures
func (p *PikeVM) searchAtWithCaptures(haystack []byte, startPos int) *MatchWithCaptures {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	caps := p.newCaptures()
	p.addThread(thread{state: p.nfa.StartAnchored(), startPos: startPos, captures: caps, priority: 0}, haystack, startPos)

	// Track the best match
	lastMatchPos := -1
	var lastMatchPriority uint32
	var lastMatchCaptures []int

	for pos := startPos; pos <= len(haystack); pos++ {
		for _, t := range p.internalState.Queue {
			if !p.nfa.IsMatch(t.state) {
				continue
			}
			// Determine if this match is better than current best
			shouldUpdate := false
			if p.internalState.Longest {
				// Leftmost-longest: always prefer longer match
				shouldUpdate = pos > lastMatchPos
			} else {
				// Leftmost-first: prefer first branch, then greedy extension
				shouldUpdate = lastMatchPos == -1 || t.priority <= lastMatchPriority
			}
			if shouldUpdate {
				lastMatchPos = pos
				lastMatchPriority = t.priority
				lastMatchCaptures = t.captures.copyData()
			}
			if !p.internalState.Longest {
				break // Found a match at this position (leftmost-first)
			}
		}

		if len(p.internalState.Queue) == 0 {
			break
		}

		if pos >= len(haystack) {
			break
		}

		b := haystack[pos]

		// Clear visited BEFORE step loop for next-gen state tracking
		p.internalState.Visited.Clear()

		for _, t := range p.internalState.Queue {
			p.step(t, b, haystack, pos+1)
		}

		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	if lastMatchPos != -1 {
		return &MatchWithCaptures{
			Start:    startPos,
			End:      lastMatchPos,
			Captures: p.buildCapturesResult(lastMatchCaptures, startPos, lastMatchPos),
		}
	}
	return nil
}

// buildCapturesResult converts internal capture slots to the result format
func (p *PikeVM) buildCapturesResult(caps []int, matchStart, matchEnd int) [][]int {
	numGroups := p.nfa.CaptureCount()
	if numGroups == 0 {
		// No captures defined - return just group 0 (entire match)
		return [][]int{{matchStart, matchEnd}}
	}

	result := make([][]int, numGroups)
	// Group 0 is always the entire match
	result[0] = []int{matchStart, matchEnd}

	// Fill in captured groups
	if caps != nil {
		for i := 1; i < numGroups; i++ {
			startIdx := i * 2
			endIdx := startIdx + 1
			if startIdx < len(caps) && endIdx < len(caps) {
				start := caps[startIdx]
				end := caps[endIdx]
				if start >= 0 && end >= 0 {
					result[i] = []int{start, end}
				}
			}
		}
	}

	return result
}

// SearchAll finds all non-overlapping matches in the haystack.
// Returns a slice of matches in order of occurrence.
func (p *PikeVM) SearchAll(haystack []byte) []Match {
	var matches []Match
	pos := 0

	for pos <= len(haystack) {
		start, end, matched := p.searchAt(haystack, pos)
		if !matched {
			pos++
			continue
		}

		matches = append(matches, Match{Start: start, End: end})

		// Move past this match to find non-overlapping matches
		if end > pos {
			pos = end
		} else {
			// Empty match - advance by 1 to avoid infinite loop
			pos++
		}
	}

	return matches
}

// searchAt attempts to find a match starting at the given position.
// Uses leftmost-first (Perl) or leftmost-longest (POSIX) semantics based on p.internalState.Longest flag.
func (p *PikeVM) searchAt(haystack []byte, startPos int) (int, int, bool) {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	// Initialize with start state
	p.addThread(thread{state: p.nfa.StartAnchored(), startPos: startPos, priority: 0}, haystack, startPos)

	// Track the best match
	lastMatchPos := -1
	var lastMatchPriority uint32

	// Process each byte position
	for pos := startPos; pos <= len(haystack); pos++ {
		// Check if any current threads are in a match state
		for _, t := range p.internalState.Queue {
			if !p.nfa.IsMatch(t.state) {
				continue
			}
			// Determine if this match is better than current best
			shouldUpdate := false
			if p.internalState.Longest {
				// Leftmost-longest: always prefer longer match
				shouldUpdate = pos > lastMatchPos
			} else {
				// Leftmost-first: prefer first branch, then greedy extension
				shouldUpdate = lastMatchPos == -1 || t.priority <= lastMatchPriority
			}
			if shouldUpdate {
				lastMatchPos = pos
				lastMatchPriority = t.priority
			}
			if !p.internalState.Longest {
				break // Found a match at this position (leftmost-first)
			}
		}

		if len(p.internalState.Queue) == 0 {
			// No active threads - search complete
			break
		}

		if pos >= len(haystack) {
			// At end of input - no more bytes to process
			break
		}

		// Get current byte
		b := haystack[pos]

		// Clear visited BEFORE step loop so addThreadToNext can track next-gen states
		// This is critical: visited was used by addThread for current gen,
		// we need fresh tracking for next gen to allow +/* quantifiers to work
		p.internalState.Visited.Clear()

		// Process all active threads
		for _, t := range p.internalState.Queue {
			p.step(t, b, haystack, pos+1)
		}

		// Swap queues for next iteration
		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	// Return the match found
	if lastMatchPos != -1 {
		return startPos, lastMatchPos, true
	}

	return -1, -1, false
}

// addThread adds a new thread to the current queue, following epsilon transitions
func (p *PikeVM) addThread(t thread, haystack []byte, pos int) {
	// Check if we've already visited this state in this generation
	// Optimization: Insert returns false if already present, avoiding double Contains call
	if !p.internalState.Visited.Insert(uint32(t.state)) {
		return
	}

	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	switch state.Kind() {
	case StateMatch:
		// Match state - add to queue
		p.internalState.Queue = append(p.internalState.Queue, t)

	case StateByteRange, StateSparse, StateRuneAny, StateRuneAnyNotNL:
		// Input-consuming states - add to queue
		p.internalState.Queue = append(p.internalState.Queue, t)

	case StateEpsilon:
		// Follow epsilon transition immediately, preserving startPos, captures, priority, and tookLeft
		next := state.Epsilon()
		if next != InvalidState {
			p.addThread(thread{state: next, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: t.tookLeft}, haystack, pos)
		}

	case StateSplit:
		// Follow both branches, preserving startPos and captures
		// For alternation splits: left branch keeps same priority and marks tookLeft, right branch increments priority
		// For quantifier splits: left branch (continue) keeps priority, right branch (exit) resets IF tookLeft is true
		// The conditional reset handles the distinction between "free" and "forced" alternation choices:
		// - In (?:|a)*, all 'a' choices are "free" (empty was available), so don't reset → empty wins
		// - In (foo|bar)+, after matching 'foo', 'bar' is "forced" (foo didn't match), so reset → longer wins
		left, right := state.Split()
		isQuantifier := state.IsQuantifierSplit()

		// For Look-left alternations: check if Look would succeed at current position
		leftLookSucceeds := !isQuantifier && p.checkLeftLookSucceeds(left, haystack, pos)

		if left != InvalidState {
			leftTookLeft := t.tookLeft
			if !isQuantifier {
				leftTookLeft = true // Mark that we took left at an alternation
			}
			p.addThread(thread{state: left, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: leftTookLeft}, haystack, pos)
		}
		if right != InvalidState {
			rightPriority, rightTookLeft := p.calcRightBranchPriority(t, left, isQuantifier, leftLookSucceeds)
			// Clone captures for right branch to ensure COW works properly.
			// Without clone, both branches share captures with refs=1, so updates
			// modify in-place and corrupt the other branch's captures.
			p.addThread(thread{state: right, startPos: t.startPos, captures: t.captures.clone(), priority: rightPriority, tookLeft: rightTookLeft}, haystack, pos)
		}

	case StateCapture:
		// Record capture position and follow epsilon transition, preserving priority and tookLeft
		groupIndex, isStart, next := state.Capture()
		if next != InvalidState {
			newCaps := updateCapture(t.captures, groupIndex, isStart, pos)
			p.addThread(thread{state: next, startPos: t.startPos, captures: newCaps, priority: t.priority, tookLeft: t.tookLeft}, haystack, pos)
		}

	case StateLook:
		// Check zero-width assertion at current position, preserving priority and tookLeft
		look, next := state.Look()
		if checkLookAssertion(look, haystack, pos) && next != InvalidState {
			p.addThread(thread{state: next, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: t.tookLeft}, haystack, pos)
		}

	case StateFail:
		// Dead state - don't add to queue
	}
}

// step processes a single byte transition for a thread
func (p *PikeVM) step(t thread, b byte, haystack []byte, nextPos int) {
	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	switch state.Kind() {
	case StateByteRange:
		lo, hi, next := state.ByteRange()
		if b >= lo && b <= hi {
			// Byte matches - add thread for next state, preserving startPos, captures, priority, and tookLeft
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: t.tookLeft}, haystack, nextPos)
		}

	case StateSparse:
		// Check all transitions
		for _, tr := range state.Transitions() {
			if b >= tr.Lo && b <= tr.Hi {
				// Byte matches this transition, preserving startPos, captures, priority, and tookLeft
				p.addThreadToNext(thread{state: tr.Next, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: t.tookLeft}, haystack, nextPos)
			}
		}

	case StateRuneAny:
		// Match any Unicode codepoint - decode UTF-8 rune at current position
		// Only process at the START of a UTF-8 sequence (keep alive at continuation bytes)
		if b >= 0x80 && b <= 0xBF {
			// This is a UTF-8 continuation byte - keep thread alive for next position
			// The thread will be re-processed until we reach a lead byte or ASCII
			p.addThreadToNext(t, haystack, nextPos)
			return
		}
		runePos := nextPos - 1 // Position of the byte we're processing
		if runePos < len(haystack) {
			r, width := utf8.DecodeRune(haystack[runePos:])
			if r != utf8.RuneError || width == 1 {
				// Valid rune (or single byte for ASCII/invalid UTF-8) - advance by full rune width
				next := state.RuneAny()
				newPos := runePos + width
				p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: t.tookLeft}, haystack, newPos)
			}
		}

	case StateRuneAnyNotNL:
		// Match any Unicode codepoint except newline - decode UTF-8 rune at current position
		// Only process at the START of a UTF-8 sequence (keep alive at continuation bytes)
		if b >= 0x80 && b <= 0xBF {
			// This is a UTF-8 continuation byte - keep thread alive for next position
			// The thread will be re-processed until we reach a lead byte or ASCII
			p.addThreadToNext(t, haystack, nextPos)
			return
		}
		runePos := nextPos - 1 // Position of the byte we're processing
		if runePos < len(haystack) {
			r, width := utf8.DecodeRune(haystack[runePos:])
			if (r != utf8.RuneError || width == 1) && r != '\n' {
				// Valid rune (or single byte for ASCII/invalid UTF-8) and not newline - advance by full rune width
				next := state.RuneAnyNotNL()
				newPos := runePos + width
				p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: t.tookLeft}, haystack, newPos)
			}
		}
	}
}

// addThreadToNext adds a thread to the next generation queue
func (p *PikeVM) addThreadToNext(t thread, haystack []byte, pos int) {
	// CRITICAL: Check if we've already visited this state in this generation
	// Without this check, patterns with multiple character classes like
	// A[AB]B[BC]C[CD]... can cause exponential thread explosion (2^N duplicates)
	// Reference: rust-regex pikevm.rs line 1683: "if !next.set.insert(sid) { return; }"
	// Optimization: Insert returns false if already present, avoiding double Contains call
	if !p.internalState.Visited.Insert(uint32(t.state)) {
		return
	}

	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	// Follow epsilon transitions immediately, preserving startPos, captures, priority, and tookLeft
	switch state.Kind() {
	case StateEpsilon:
		next := state.Epsilon()
		if next != InvalidState {
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: t.tookLeft}, haystack, pos)
		}
		return

	case StateSplit:
		// For alternation splits: left branch keeps same priority and marks tookLeft, right branch increments priority
		// For quantifier splits: left branch (continue) keeps priority, right branch (exit) resets IF tookLeft is true
		left, right := state.Split()
		isQuantifier := state.IsQuantifierSplit()

		// For Look-left alternations: check if Look would succeed at current position
		leftLookSucceeds := !isQuantifier && p.checkLeftLookSucceeds(left, haystack, pos)

		if left != InvalidState {
			leftTookLeft := t.tookLeft
			if !isQuantifier {
				leftTookLeft = true // Mark that we took left at an alternation
			}
			p.addThreadToNext(thread{state: left, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: leftTookLeft}, haystack, pos)
		}
		if right != InvalidState {
			rightPriority, rightTookLeft := p.calcRightBranchPriority(t, left, isQuantifier, leftLookSucceeds)
			// Clone captures for right branch to ensure COW works properly.
			// Without clone, both branches share captures with refs=1, so updates
			// modify in-place and corrupt the other branch's captures.
			p.addThreadToNext(thread{state: right, startPos: t.startPos, captures: t.captures.clone(), priority: rightPriority, tookLeft: rightTookLeft}, haystack, pos)
		}
		return

	case StateCapture:
		// Record capture position and follow epsilon transition, preserving priority and tookLeft
		groupIndex, isStart, next := state.Capture()
		if next != InvalidState {
			newCaps := updateCapture(t.captures, groupIndex, isStart, pos)
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: newCaps, priority: t.priority, tookLeft: t.tookLeft}, haystack, pos)
		}
		return

	case StateLook:
		// Check zero-width assertion at current position, preserving priority and tookLeft
		look, next := state.Look()
		if checkLookAssertion(look, haystack, pos) && next != InvalidState {
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures, priority: t.priority, tookLeft: t.tookLeft}, haystack, pos)
		}
		return
	}

	// Add to next queue
	p.internalState.NextQueue = append(p.internalState.NextQueue, t)
}

// matchesEmpty checks if the NFA matches an empty string at position 0
func (p *PikeVM) matchesEmpty() bool {
	return p.matchesEmptyAt(nil, 0)
}

// matchesEmptyAt checks if the NFA matches an empty string at the given position.
// This is needed for correctly evaluating look assertions like ^ and $ in multiline mode.
func (p *PikeVM) matchesEmptyAt(haystack []byte, pos int) bool {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.Visited.Clear()

	// Check if we can reach a match state via epsilon transitions only
	var stack []StateID
	stack = append(stack, p.nfa.StartAnchored())
	p.internalState.Visited.Insert(uint32(p.nfa.StartAnchored()))

	for len(stack) > 0 {
		// Pop state from stack
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if p.nfa.IsMatch(id) {
			return true
		}

		state := p.nfa.State(id)
		if state == nil {
			continue
		}

		switch state.Kind() {
		case StateEpsilon:
			next := state.Epsilon()
			if next != InvalidState && !p.internalState.Visited.Contains(uint32(next)) {
				p.internalState.Visited.Insert(uint32(next))
				stack = append(stack, next)
			}

		case StateSplit:
			left, right := state.Split()
			if left != InvalidState && !p.internalState.Visited.Contains(uint32(left)) {
				p.internalState.Visited.Insert(uint32(left))
				stack = append(stack, left)
			}
			if right != InvalidState && !p.internalState.Visited.Contains(uint32(right)) {
				p.internalState.Visited.Insert(uint32(right))
				stack = append(stack, right)
			}

		case StateLook:
			// Check if assertion holds at the actual position
			look, next := state.Look()
			if checkLookAssertion(look, haystack, pos) && next != InvalidState && !p.internalState.Visited.Contains(uint32(next)) {
				p.internalState.Visited.Insert(uint32(next))
				stack = append(stack, next)
			}

		case StateCapture:
			// Capture states are epsilon transitions, follow through
			_, _, next := state.Capture()
			if next != InvalidState && !p.internalState.Visited.Contains(uint32(next)) {
				p.internalState.Visited.Insert(uint32(next))
				stack = append(stack, next)
			}
		}
	}

	return false
}

// isWordByte returns true if byte is an ASCII word character [a-zA-Z0-9_]
// This matches Go's regexp/syntax.IsWordChar for ASCII bytes.
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

// checkLeftLookSucceeds checks if the left branch of a split is a Look state
// that would succeed at the current position. This is used to determine
// whether to increment priority for the right branch in alternations.
//
// For patterns like (?:^|a)+ where left branch is a Look assertion:
// - If Look succeeds at current position, return true (prefer left)
// - If Look fails at current position, return false (right is the only viable option)
func (p *PikeVM) checkLeftLookSucceeds(left StateID, haystack []byte, pos int) bool {
	if left == InvalidState {
		return false
	}
	leftState := p.nfa.State(left)
	if leftState == nil || leftState.Kind() != StateLook {
		return false
	}
	look, _ := leftState.Look()
	return checkLookAssertion(look, haystack, pos)
}

// calcRightBranchPriority calculates the priority for the right branch of a split.
// For quantifiers: resets priority if left branch was taken (forced alternation choice).
// For alternations: increments priority unless left is a failing Look assertion.
func (p *PikeVM) calcRightBranchPriority(t thread, left StateID, isQuantifier, leftLookSucceeds bool) (priority uint32, tookLeft bool) {
	priority = t.priority
	tookLeft = t.tookLeft

	if isQuantifier {
		// At quantifier exit: reset priority only if we took left branch in some alternation
		if t.tookLeft {
			return 0, false
		}
		return priority, tookLeft
	}

	// For alternation splits: increment priority unless left is a failing Look
	leftState := p.nfa.State(left)
	if left == InvalidState || leftLookSucceeds || leftState == nil || leftState.Kind() != StateLook {
		priority++
	}
	return priority, tookLeft
}

// checkLookAssertion checks if a zero-width assertion holds at the given position
func checkLookAssertion(look Look, haystack []byte, pos int) bool {
	switch look {
	case LookStartText:
		// \A - matches only at start of input
		return pos == 0
	case LookEndText:
		// \z - matches only at end of input
		return pos == len(haystack)
	case LookStartLine:
		// ^ - matches at start of input or after newline
		return pos == 0 || (pos > 0 && haystack[pos-1] == '\n')
	case LookEndLine:
		// $ in multiline mode - matches at end of input OR before \n
		return pos == len(haystack) || (pos < len(haystack) && haystack[pos] == '\n')
	case LookWordBoundary:
		// \b - matches at word/non-word boundary
		// Word boundary exists when is_word(prev) != is_word(curr)
		wordBefore := pos > 0 && isWordByte(haystack[pos-1])
		wordAfter := pos < len(haystack) && isWordByte(haystack[pos])
		return wordBefore != wordAfter
	case LookNoWordBoundary:
		// \B - matches where there is NO word boundary
		// No boundary when is_word(prev) == is_word(curr)
		wordBefore := pos > 0 && isWordByte(haystack[pos-1])
		wordAfter := pos < len(haystack) && isWordByte(haystack[pos])
		return wordBefore == wordAfter
	}
	return false
}
