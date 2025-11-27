package nfa

import (
	"github.com/coregx/coregex/internal/sparse"
)

// PikeVM implements the Pike VM algorithm for NFA execution.
// It simulates the NFA by maintaining a set of active states and
// exploring all possible paths through the automaton.
//
// The Pike VM is slower than DFA-based approaches but handles all
// regex features including backreferences (future) and capturing groups.
type PikeVM struct {
	nfa *NFA

	// Thread queues for current and next generation
	// Pre-allocated to avoid allocations during search
	queue     []thread
	nextQueue []thread

	// Sparse set for tracking visited states in current generation
	// This prevents processing the same state multiple times
	visited *sparse.SparseSet
}

// thread represents an execution thread in the PikeVM.
// Each thread tracks a position in the NFA state graph.
type thread struct {
	state    StateID
	startPos int // Position where this thread's match attempt started
	// Future: capture group positions
}

// Match represents a successful regex match with start and end positions
type Match struct {
	Start int
	End   int
}

// NewPikeVM creates a new PikeVM for executing the given NFA
func NewPikeVM(nfa *NFA) *PikeVM {
	// Pre-allocate thread queues with capacity based on NFA size
	capacity := nfa.States()
	if capacity < 16 {
		capacity = 16
	}

	return &PikeVM{
		nfa:       nfa,
		queue:     make([]thread, 0, capacity),
		nextQueue: make([]thread, 0, capacity),
		//nolint:gosec // G115: StateID is uint32, this conversion is safe for realistic NFA sizes
		visited: sparse.NewSparseSet(uint32(capacity)),
	}
}

// Search finds the first match in the haystack.
// Returns (start, end, true) if a match is found, or (-1, -1, false) if not.
//
// The search is unanchored by default (matches anywhere in haystack)
// unless the NFA was compiled with anchored mode.
func (p *PikeVM) Search(haystack []byte) (int, int, bool) {
	if len(haystack) == 0 {
		// Check if empty string matches
		if p.matchesEmpty() {
			return 0, 0, true
		}
		return -1, -1, false
	}

	if p.nfa.IsAnchored() {
		// Anchored mode: only try position 0
		start, end, matched := p.searchAt(haystack, 0)
		return start, end, matched
	}

	// Unanchored mode: use O(n) parallel simulation
	return p.searchUnanchored(haystack)
}

// searchUnanchored implements Thompson's parallel NFA simulation for unanchored search.
// This is O(n) because we process each byte exactly once, maintaining all possible
// match starts simultaneously instead of restarting the search at each position.
//
// The algorithm simulates an implicit (?s:.)*? prefix by adding new start threads
// at each byte position, allowing matches to begin anywhere in the input.
//
// Implements leftmost-longest matching semantics:
// 1. Find the leftmost (earliest starting) match
// 2. Among matches with the same start, find the longest
func (p *PikeVM) searchUnanchored(haystack []byte) (int, int, bool) {
	// Reset state
	p.queue = p.queue[:0]
	p.nextQueue = p.nextQueue[:0]
	p.visited.Clear()

	// Track leftmost-longest match
	bestStart := -1
	bestEnd := -1

	// Process each byte position once
	for pos := 0; pos <= len(haystack); pos++ {
		// Add new start thread at current position (simulates .*? prefix)
		// We must clear visited BEFORE adding to allow the same NFA state
		// to be reached from different starting positions
		// Stop adding new starts once we've found a match (non-greedy behavior)
		if bestStart == -1 {
			p.visited.Clear()
			p.addThread(thread{state: p.nfa.Start(), startPos: pos}, haystack, pos)
		}

		// Check for matches in current generation
		// We check AFTER adding threads to ensure we capture all potential matches
		for _, t := range p.queue {
			if p.nfa.IsMatch(t.state) {
				// Found a match ending at current position
				// Update best match if this is leftmost or longest for same start
				if bestStart == -1 || t.startPos < bestStart ||
					(t.startPos == bestStart && pos > bestEnd) {
					bestStart = t.startPos
					bestEnd = pos
				}
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
			for _, t := range p.queue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
			}
		}

		// If no threads, stop
		if len(p.queue) == 0 {
			break
		}

		// Process current byte for all active threads
		b := haystack[pos]
		p.visited.Clear() // Clear before processing to track visited states for epsilon closures
		for _, t := range p.queue {
			p.step(t, b, haystack, pos+1)
		}

		// Swap queues for next iteration
		p.queue, p.nextQueue = p.nextQueue, p.queue[:0]
	}

	if bestStart != -1 {
		return bestStart, bestEnd, true
	}
	return -1, -1, false
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

// searchAt attempts to find a match starting at the given position
// Implements leftmost-longest matching: finds the longest match at the leftmost position
func (p *PikeVM) searchAt(haystack []byte, startPos int) (int, int, bool) {
	// Reset state
	p.queue = p.queue[:0]
	p.nextQueue = p.nextQueue[:0]
	p.visited.Clear()

	// Initialize with start state
	p.addThread(thread{state: p.nfa.Start(), startPos: startPos}, haystack, startPos)

	// Track the last position where we had a match
	lastMatchPos := -1

	// Process each byte position
	for pos := startPos; pos <= len(haystack); pos++ {
		// Check if any current threads are in a match state
		// Record this position but continue searching for longer matches
		for _, t := range p.queue {
			if p.nfa.IsMatch(t.state) {
				lastMatchPos = pos
				break // Found a match at this position, record it
			}
		}

		if len(p.queue) == 0 {
			// No active threads - search complete
			break
		}

		if pos >= len(haystack) {
			// At end of input - no more bytes to process
			break
		}

		// Get current byte
		b := haystack[pos]

		// Process all active threads
		for _, t := range p.queue {
			p.step(t, b, haystack, pos+1)
		}

		// Swap queues for next iteration
		p.queue, p.nextQueue = p.nextQueue, p.queue[:0]
		p.visited.Clear()
	}

	// Return the last (longest) match found
	if lastMatchPos != -1 {
		return startPos, lastMatchPos, true
	}

	return -1, -1, false
}

// addThread adds a new thread to the current queue, following epsilon transitions
//
//nolint:unparam // haystack parameter reserved for future capture group support
func (p *PikeVM) addThread(t thread, haystack []byte, pos int) {
	// Check if we've already visited this state in this generation
	if p.visited.Contains(uint32(t.state)) {
		return
	}
	p.visited.Insert(uint32(t.state))

	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	switch state.Kind() {
	case StateMatch:
		// Match state - add to queue
		p.queue = append(p.queue, t)

	case StateByteRange, StateSparse:
		// Input-consuming states - add to queue
		p.queue = append(p.queue, t)

	case StateEpsilon:
		// Follow epsilon transition immediately, preserving startPos
		next := state.Epsilon()
		if next != InvalidState {
			p.addThread(thread{state: next, startPos: t.startPos}, haystack, pos)
		}

	case StateSplit:
		// Follow both branches, preserving startPos
		left, right := state.Split()
		if left != InvalidState {
			p.addThread(thread{state: left, startPos: t.startPos}, haystack, pos)
		}
		if right != InvalidState {
			p.addThread(thread{state: right, startPos: t.startPos}, haystack, pos)
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
			// Byte matches - add thread for next state, preserving startPos
			p.addThreadToNext(thread{state: next, startPos: t.startPos}, haystack, nextPos)
		}

	case StateSparse:
		// Check all transitions
		for _, tr := range state.Transitions() {
			if b >= tr.Lo && b <= tr.Hi {
				// Byte matches this transition, preserving startPos
				p.addThreadToNext(thread{state: tr.Next, startPos: t.startPos}, haystack, nextPos)
			}
		}
	}
}

// addThreadToNext adds a thread to the next generation queue
//
//nolint:unparam // haystack parameter reserved for future capture group support
func (p *PikeVM) addThreadToNext(t thread, haystack []byte, pos int) {
	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	// Follow epsilon transitions immediately, preserving startPos
	switch state.Kind() {
	case StateEpsilon:
		next := state.Epsilon()
		if next != InvalidState {
			p.addThreadToNext(thread{state: next, startPos: t.startPos}, haystack, pos)
		}
		return

	case StateSplit:
		left, right := state.Split()
		if left != InvalidState {
			p.addThreadToNext(thread{state: left, startPos: t.startPos}, haystack, pos)
		}
		if right != InvalidState {
			p.addThreadToNext(thread{state: right, startPos: t.startPos}, haystack, pos)
		}
		return
	}

	// Add to next queue
	p.nextQueue = append(p.nextQueue, t)
}

// matchesEmpty checks if the NFA matches an empty string
func (p *PikeVM) matchesEmpty() bool {
	// Reset state
	p.queue = p.queue[:0]
	p.visited.Clear()

	// Check if we can reach a match state via epsilon transitions only
	var stack []StateID
	stack = append(stack, p.nfa.Start())
	p.visited.Insert(uint32(p.nfa.Start()))

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
			if next != InvalidState && !p.visited.Contains(uint32(next)) {
				p.visited.Insert(uint32(next))
				stack = append(stack, next)
			}

		case StateSplit:
			left, right := state.Split()
			if left != InvalidState && !p.visited.Contains(uint32(left)) {
				p.visited.Insert(uint32(left))
				stack = append(stack, left)
			}
			if right != InvalidState && !p.visited.Contains(uint32(right)) {
				p.visited.Insert(uint32(right))
				stack = append(stack, right)
			}
		}
	}

	return false
}
