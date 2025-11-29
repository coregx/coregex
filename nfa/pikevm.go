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
// Each thread tracks a position in the NFA state graph and capture positions.
type thread struct {
	state    StateID
	startPos int         // Position where this thread's match attempt started
	captures cowCaptures // COW capture positions: [start0, end0, start1, end1, ...] (-1 = not set)
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

// SearchWithCaptures finds the first match with capture group positions.
// Returns nil if no match is found.
func (p *PikeVM) SearchWithCaptures(haystack []byte) *MatchWithCaptures {
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
		return p.searchAtWithCaptures(haystack, 0)
	}

	return p.searchUnanchoredWithCaptures(haystack)
}

// searchUnanchoredWithCaptures is like searchUnanchored but returns captures
func (p *PikeVM) searchUnanchoredWithCaptures(haystack []byte) *MatchWithCaptures {
	// Reset state
	p.queue = p.queue[:0]
	p.nextQueue = p.nextQueue[:0]
	p.visited.Clear()

	// Track leftmost-longest match
	bestStart := -1
	bestEnd := -1
	var bestCaptures []int

	// Process each byte position once
	for pos := 0; pos <= len(haystack); pos++ {
		// Add new start thread at current position (simulates .*? prefix)
		if bestStart == -1 {
			p.visited.Clear()
			caps := p.newCaptures()
			p.addThread(thread{state: p.nfa.Start(), startPos: pos, captures: caps}, haystack, pos)
		}

		// Check for matches in current generation
		for _, t := range p.queue {
			if p.nfa.IsMatch(t.state) {
				if bestStart == -1 || t.startPos < bestStart ||
					(t.startPos == bestStart && pos > bestEnd) {
					bestStart = t.startPos
					bestEnd = pos
					bestCaptures = t.captures.copyData()
				}
			}
		}

		if pos >= len(haystack) {
			break
		}

		// Early termination check
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

		if len(p.queue) == 0 {
			break
		}

		// Process current byte
		b := haystack[pos]
		p.visited.Clear()
		for _, t := range p.queue {
			p.step(t, b, haystack, pos+1)
		}

		p.queue, p.nextQueue = p.nextQueue, p.queue[:0]
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
	p.queue = p.queue[:0]
	p.nextQueue = p.nextQueue[:0]
	p.visited.Clear()

	caps := p.newCaptures()
	p.addThread(thread{state: p.nfa.Start(), startPos: startPos, captures: caps}, haystack, startPos)

	lastMatchPos := -1
	var lastMatchCaptures []int

	for pos := startPos; pos <= len(haystack); pos++ {
		for _, t := range p.queue {
			if p.nfa.IsMatch(t.state) {
				lastMatchPos = pos
				lastMatchCaptures = t.captures.copyData()
				break
			}
		}

		if len(p.queue) == 0 {
			break
		}

		if pos >= len(haystack) {
			break
		}

		b := haystack[pos]

		// Clear visited BEFORE step loop for next-gen state tracking
		p.visited.Clear()

		for _, t := range p.queue {
			p.step(t, b, haystack, pos+1)
		}

		p.queue, p.nextQueue = p.nextQueue, p.queue[:0]
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

		// Clear visited BEFORE step loop so addThreadToNext can track next-gen states
		// This is critical: visited was used by addThread for current gen,
		// we need fresh tracking for next gen to allow +/* quantifiers to work
		p.visited.Clear()

		// Process all active threads
		for _, t := range p.queue {
			p.step(t, b, haystack, pos+1)
		}

		// Swap queues for next iteration
		p.queue, p.nextQueue = p.nextQueue, p.queue[:0]
	}

	// Return the last (longest) match found
	if lastMatchPos != -1 {
		return startPos, lastMatchPos, true
	}

	return -1, -1, false
}

// addThread adds a new thread to the current queue, following epsilon transitions
//
//nolint:unparam // haystack parameter reserved for future use
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
		// Follow epsilon transition immediately, preserving startPos and captures
		next := state.Epsilon()
		if next != InvalidState {
			p.addThread(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, pos)
		}

	case StateSplit:
		// Follow both branches, preserving startPos and captures
		// Note: captures are shared (copy-on-write when modified)
		left, right := state.Split()
		if left != InvalidState {
			p.addThread(thread{state: left, startPos: t.startPos, captures: t.captures}, haystack, pos)
		}
		if right != InvalidState {
			p.addThread(thread{state: right, startPos: t.startPos, captures: t.captures}, haystack, pos)
		}

	case StateCapture:
		// Record capture position and follow epsilon transition
		groupIndex, isStart, next := state.Capture()
		if next != InvalidState {
			newCaps := updateCapture(t.captures, groupIndex, isStart, pos)
			p.addThread(thread{state: next, startPos: t.startPos, captures: newCaps}, haystack, pos)
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
			// Byte matches - add thread for next state, preserving startPos and captures
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, nextPos)
		}

	case StateSparse:
		// Check all transitions
		for _, tr := range state.Transitions() {
			if b >= tr.Lo && b <= tr.Hi {
				// Byte matches this transition, preserving startPos and captures
				p.addThreadToNext(thread{state: tr.Next, startPos: t.startPos, captures: t.captures}, haystack, nextPos)
			}
		}
	}
}

// addThreadToNext adds a thread to the next generation queue
//
//nolint:unparam // haystack parameter reserved for future use
func (p *PikeVM) addThreadToNext(t thread, haystack []byte, pos int) {
	// CRITICAL: Check if we've already visited this state in this generation
	// Without this check, patterns with multiple character classes like
	// A[AB]B[BC]C[CD]... can cause exponential thread explosion (2^N duplicates)
	// Reference: rust-regex pikevm.rs line 1683: "if !next.set.insert(sid) { return; }"
	if p.visited.Contains(uint32(t.state)) {
		return
	}
	p.visited.Insert(uint32(t.state))

	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	// Follow epsilon transitions immediately, preserving startPos and captures
	switch state.Kind() {
	case StateEpsilon:
		next := state.Epsilon()
		if next != InvalidState {
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, pos)
		}
		return

	case StateSplit:
		left, right := state.Split()
		if left != InvalidState {
			p.addThreadToNext(thread{state: left, startPos: t.startPos, captures: t.captures}, haystack, pos)
		}
		if right != InvalidState {
			p.addThreadToNext(thread{state: right, startPos: t.startPos, captures: t.captures}, haystack, pos)
		}
		return

	case StateCapture:
		// Record capture position and follow epsilon transition
		groupIndex, isStart, next := state.Capture()
		if next != InvalidState {
			newCaps := updateCapture(t.captures, groupIndex, isStart, pos)
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: newCaps}, haystack, pos)
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
