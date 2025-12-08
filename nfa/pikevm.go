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
type PikeVM struct {
	nfa *NFA

	// Thread queues for current and next generation
	// Pre-allocated to avoid allocations during search
	queue     []thread
	nextQueue []thread

	// Sparse set for tracking visited states in current generation
	// This prevents processing the same state multiple times
	visited *sparse.SparseSet

	// longest enables leftmost-longest (POSIX) matching semantics.
	// By default (false), uses leftmost-first (Perl) semantics where
	// the first alternative wins. When true, the longest match wins.
	longest bool
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
	// Pre-allocate thread queues with capacity based on NFA size
	capacity := nfa.States()
	if capacity < 16 {
		capacity = 16
	}

	return &PikeVM{
		nfa:       nfa,
		queue:     make([]thread, 0, capacity),
		nextQueue: make([]thread, 0, capacity),
		visited:   sparse.NewSparseSet(conv.IntToUint32(capacity)),
	}
}

// SetLongest enables or disables leftmost-longest (POSIX) matching semantics.
// By default, uses leftmost-first (Perl) semantics where first alternative wins.
// When longest=true, the longest match at the same start position wins.
func (p *PikeVM) SetLongest(longest bool) {
	p.longest = longest
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
	return p.SearchAt(haystack, 0)
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
	p.queue = p.queue[:0]
	p.nextQueue = p.nextQueue[:0]
	p.visited.Clear()

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
			p.visited.Clear()
			p.addThread(thread{state: p.nfa.StartAnchored(), startPos: pos, priority: 0}, haystack, pos)
		}

		// Check for matches in current generation
		// We check AFTER adding threads to ensure we capture all potential matches
		for _, t := range p.queue {
			if p.nfa.IsMatch(t.state) {
				// Found a match ending at current position
				// Update best match based on semantics:
				if p.longest {
					// Leftmost-longest (POSIX) semantics:
					// 1. Leftmost start position wins
					// 2. For same start, longer match wins (ignore priority)
					if bestStart == -1 || t.startPos < bestStart ||
						(t.startPos == bestStart && pos > bestEnd) {
						bestStart = t.startPos
						bestEnd = pos
						bestPriority = t.priority
					}
				} else {
					// Leftmost-first (Perl) semantics:
					// 1. Leftmost start position wins
					// 2. For same start, higher priority (lower number = first branch) wins
					// 3. For same start and priority, longer match wins (greedy extension)
					if bestStart == -1 || t.startPos < bestStart ||
						(t.startPos == bestStart && t.priority < bestPriority) ||
						(t.startPos == bestStart && t.priority == bestPriority && pos > bestEnd) {
						bestStart = t.startPos
						bestEnd = pos
						bestPriority = t.priority
					}
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

		// Process current byte for all active threads
		// Note: We continue even if queue is empty because we might add
		// new start threads at the next position (unanchored search)
		if len(p.queue) > 0 {
			b := haystack[pos]
			p.visited.Clear() // Clear before processing to track visited states for epsilon closures
			for _, t := range p.queue {
				p.step(t, b, haystack, pos+1)
			}
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
	p.queue = p.queue[:0]
	p.nextQueue = p.nextQueue[:0]
	p.visited.Clear()

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
			p.visited.Clear()
			caps := p.newCaptures()
			p.addThread(thread{state: p.nfa.StartAnchored(), startPos: pos, captures: caps, priority: 0}, haystack, pos)
		}

		// Check for matches in current generation
		for _, t := range p.queue {
			if p.nfa.IsMatch(t.state) {
				// Update best match based on semantics:
				if p.longest {
					// Leftmost-longest (POSIX) semantics:
					// 1. Leftmost start position wins
					// 2. For same start, longer match wins (ignore priority)
					if bestStart == -1 || t.startPos < bestStart ||
						(t.startPos == bestStart && pos > bestEnd) {
						bestStart = t.startPos
						bestEnd = pos
						bestPriority = t.priority
						bestCaptures = t.captures.copyData()
					}
				} else {
					// Leftmost-first (Perl) semantics:
					// 1. Leftmost start position wins
					// 2. For same start, higher priority (lower number) wins
					// 3. For same start and priority, longer match wins (greedy extension)
					if bestStart == -1 || t.startPos < bestStart ||
						(t.startPos == bestStart && t.priority < bestPriority) ||
						(t.startPos == bestStart && t.priority == bestPriority && pos > bestEnd) {
						bestStart = t.startPos
						bestEnd = pos
						bestPriority = t.priority
						bestCaptures = t.captures.copyData()
					}
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

		// Process current byte for all active threads
		// Continue even if queue is empty (unanchored search may add new starts)
		if len(p.queue) > 0 {
			b := haystack[pos]
			p.visited.Clear()
			for _, t := range p.queue {
				p.step(t, b, haystack, pos+1)
			}
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
	p.addThread(thread{state: p.nfa.StartAnchored(), startPos: startPos, captures: caps, priority: 0}, haystack, startPos)

	// Track the best match
	lastMatchPos := -1
	var lastMatchPriority uint32
	var lastMatchCaptures []int

	for pos := startPos; pos <= len(haystack); pos++ {
		for _, t := range p.queue {
			if p.nfa.IsMatch(t.state) {
				if p.longest {
					// Leftmost-longest: always prefer longer match (higher end position)
					if pos > lastMatchPos {
						lastMatchPos = pos
						lastMatchPriority = t.priority
						lastMatchCaptures = t.captures.copyData()
					}
				} else {
					// Leftmost-first: prefer first branch (lower priority), then greedy extension
					if lastMatchPos == -1 || t.priority < lastMatchPriority ||
						t.priority == lastMatchPriority {
						lastMatchPos = pos
						lastMatchPriority = t.priority
						lastMatchCaptures = t.captures.copyData()
					}
					break // Found a match at this position (leftmost-first)
				}
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

// searchAt attempts to find a match starting at the given position.
// Uses leftmost-first (Perl) or leftmost-longest (POSIX) semantics based on p.longest flag.
func (p *PikeVM) searchAt(haystack []byte, startPos int) (int, int, bool) {
	// Reset state
	p.queue = p.queue[:0]
	p.nextQueue = p.nextQueue[:0]
	p.visited.Clear()

	// Initialize with start state
	p.addThread(thread{state: p.nfa.StartAnchored(), startPos: startPos, priority: 0}, haystack, startPos)

	// Track the best match
	lastMatchPos := -1
	var lastMatchPriority uint32

	// Process each byte position
	for pos := startPos; pos <= len(haystack); pos++ {
		// Check if any current threads are in a match state
		for _, t := range p.queue {
			if p.nfa.IsMatch(t.state) {
				if p.longest {
					// Leftmost-longest: always prefer longer match (higher end position)
					// Ignore priority - only match length matters
					if pos > lastMatchPos {
						lastMatchPos = pos
						lastMatchPriority = t.priority
					}
				} else {
					// Leftmost-first: prefer first branch (lower priority), then greedy extension
					if lastMatchPos == -1 || t.priority < lastMatchPriority ||
						t.priority == lastMatchPriority {
						lastMatchPos = pos
						lastMatchPriority = t.priority
					}
					break // Found a match at this position, record it (leftmost-first)
				}
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

	// Return the match found
	if lastMatchPos != -1 {
		return startPos, lastMatchPos, true
	}

	return -1, -1, false
}

// addThread adds a new thread to the current queue, following epsilon transitions
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

	case StateByteRange, StateSparse, StateRuneAny, StateRuneAnyNotNL:
		// Input-consuming states - add to queue
		p.queue = append(p.queue, t)

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
			p.addThread(thread{state: right, startPos: t.startPos, captures: t.captures, priority: rightPriority, tookLeft: rightTookLeft}, haystack, pos)
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
	if p.visited.Contains(uint32(t.state)) {
		return
	}
	p.visited.Insert(uint32(t.state))

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
			p.addThreadToNext(thread{state: right, startPos: t.startPos, captures: t.captures, priority: rightPriority, tookLeft: rightTookLeft}, haystack, pos)
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
	p.nextQueue = append(p.nextQueue, t)
}

// matchesEmpty checks if the NFA matches an empty string at position 0
func (p *PikeVM) matchesEmpty() bool {
	return p.matchesEmptyAt(nil, 0)
}

// matchesEmptyAt checks if the NFA matches an empty string at the given position.
// This is needed for correctly evaluating look assertions like ^ and $ in multiline mode.
func (p *PikeVM) matchesEmptyAt(haystack []byte, pos int) bool {
	// Reset state
	p.queue = p.queue[:0]
	p.visited.Clear()

	// Check if we can reach a match state via epsilon transitions only
	var stack []StateID
	stack = append(stack, p.nfa.StartAnchored())
	p.visited.Insert(uint32(p.nfa.StartAnchored()))

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

		case StateLook:
			// Check if assertion holds at the actual position
			look, next := state.Look()
			if checkLookAssertion(look, haystack, pos) && next != InvalidState && !p.visited.Contains(uint32(next)) {
				p.visited.Insert(uint32(next))
				stack = append(stack, next)
			}

		case StateCapture:
			// Capture states are epsilon transitions, follow through
			_, _, next := state.Capture()
			if next != InvalidState && !p.visited.Contains(uint32(next)) {
				p.visited.Insert(uint32(next))
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
