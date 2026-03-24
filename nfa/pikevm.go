package nfa

import (
	"unicode/utf8"

	"github.com/coregx/coregex/internal/conv"
	"github.com/coregx/coregex/internal/sparse"
)

// SearchMode determines how many capture slots to track during search.
// This enables dynamic slot sizing for optimal performance based on the
// type of search being performed.
//
// Reference: rust-regex/regex-automata/src/nfa/thompson/pikevm.rs:1898-1921
type SearchMode int

const (
	// SearchModeIsMatch tracks 0 slots - just returns boolean match result.
	// This is the fastest mode, used for IsMatch() calls.
	SearchModeIsMatch SearchMode = iota

	// SearchModeFind tracks 2 slots - only overall match start/end positions.
	// Used for Find() and FindIndices() when captures are not needed.
	SearchModeFind

	// SearchModeCaptures tracks all slots - full capture group positions.
	// Used for FindWithCaptures() and similar methods.
	SearchModeCaptures
)

// SlotsNeeded returns the number of slots required for this search mode.
// Parameters:
//   - totalSlots: the total number of capture slots (CaptureCount * 2)
func (m SearchMode) SlotsNeeded(totalSlots int) int {
	switch m {
	case SearchModeIsMatch:
		return 0
	case SearchModeFind:
		if totalSlots < 2 {
			return totalSlots
		}
		return 2
	case SearchModeCaptures:
		return totalSlots
	}
	return totalSlots
}

// searchThread is a lightweight thread for non-capture searches.
// It uses SlotTable for per-state capture storage instead of per-thread COW captures.
//
// Memory layout (12 bytes on 64-bit):
//   - state: 4 bytes (StateID)
//   - startPos: 8 bytes (int)
type searchThread struct {
	state    StateID // Current NFA state
	startPos int     // Position where this thread's match attempt started
}

// captureFrame is a stack frame for capture-aware epsilon closure.
// Two kinds: Explore (process a state) and RestoreCapture (undo a capture write).
// Mirrors Rust's FollowEpsilon enum (pikevm.rs:1611).
type captureFrame struct {
	state    StateID // state to explore (InvalidState = restore frame)
	startPos int     // thread start position
	// For restore frames:
	slot  int // slot index to restore
	value int // old value to restore
}

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
// SkipAhead is a prefilter interface for PikeVM skip-ahead optimization.
// When NFA has no active threads, Find skips to the next candidate position
// instead of scanning byte-by-byte. This is the Rust approach (pikevm.rs:1293).
// Safe for both complete and partial-coverage prefilters.
type SkipAhead interface {
	Find(haystack []byte, start int) int
}

type PikeVM struct {
	nfa       *NFA
	skipAhead SkipAhead // Optional prefilter for skip-ahead (nil = disabled)

	// internalState is used by legacy non-thread-safe methods.
	// For concurrent usage, use *WithState methods with external PikeVMState.
	internalState PikeVMState
}

// PikeVMState holds mutable per-search state for PikeVM.
// This struct should be pooled (via sync.Pool) for concurrent usage.
// Each goroutine must use its own PikeVMState instance.
type PikeVMState struct {
	// Thread queues for current and next generation (legacy, with COW captures)
	// Pre-allocated to avoid allocations during search
	Queue     []thread
	NextQueue []thread

	// Lightweight thread queues for SlotTable-based search (new architecture)
	// These use searchThread which is 16 bytes vs 40+ bytes for thread
	SearchQueue     []searchThread
	SearchNextQueue []searchThread

	// Sparse set for tracking visited states in current generation
	// This prevents processing the same state multiple times
	Visited *sparse.SparseSet

	// epsilonStack is used for loop-based epsilon closure in IsMatch (Rust pattern).
	// Only stores StateID for split right branches - minimizes frame overhead.
	// Reference: rust-regex/regex-automata/src/nfa/thompson/pikevm.rs:2198
	epsilonStack []StateID

	// SlotTable / NextSlotTable: two slot tables swapped between generations.
	// Mirrors Rust's curr/next ActiveStates pattern (pikevm.rs:1878).
	// SlotTable = current generation, NextSlotTable = next generation.
	// After each byte transition: swap(SlotTable, NextSlotTable).
	SlotTable     *SlotTable
	NextSlotTable *SlotTable

	// captureStack is used for stack-based epsilon closure with capture save/restore.
	// Mirrors Rust's FollowEpsilon::RestoreCapture pattern (pikevm.rs:1611-1637).
	captureStack []captureFrame

	// currSlots is the working capture buffer during epsilon closure.
	// Modified in-place, saved/restored via captureStack for StateCapture.
	// Copied to SlotTable row when terminal state reached.
	currSlots []int

	// Longest enables leftmost-longest (POSIX) matching semantics.
	// By default (false), uses leftmost-first (Perl) semantics where
	// the first alternative wins. When true, the longest match wins.
	Longest bool
}

// isBetterMatch returns true if the candidate match is better than the current best.
// Implements leftmost-first semantics: leftmost start wins, then longest end wins.
// Greedy/non-greedy is controlled by DFS thread ordering + break-on-first-match
// in the search loop, not by priority comparison here.
func (p *PikeVM) isBetterMatch(bestStart, bestEnd int,
	candStart, candEnd int) bool {
	return isBetterMatchWithLongest(bestStart, bestEnd, candStart, candEnd)
}

// isBetterMatchWithLongest is the stateless version of isBetterMatch.
// Leftmost start wins, then longest end wins. Greedy/non-greedy semantics
// are handled by DFS ordering in the search loop (Rust's approach).
func isBetterMatchWithLongest(bestStart, bestEnd int,
	candStart, candEnd int) bool {
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
	// Same start position: longer match wins (greedy extension).
	// Non-greedy patterns don't reach here because the search loop
	// breaks after the first Match state in DFS order, preventing
	// extension threads from being stepped.
	return candEnd > bestEnd
}

// thread represents an execution thread in the PikeVM.
// Each thread tracks a position in the NFA state graph and capture positions.
// Greedy/non-greedy semantics are controlled by DFS exploration order
// (left branch explored first) + break-on-first-match in the search loop.
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

	// Legacy thread queues (with COW captures)
	state.Queue = make([]thread, 0, capacity)
	state.NextQueue = make([]thread, 0, capacity)

	// Lightweight thread queues for SlotTable-based search
	state.SearchQueue = make([]searchThread, 0, capacity)
	state.SearchNextQueue = make([]searchThread, 0, capacity)

	state.Visited = sparse.NewSparseSet(conv.IntToUint32(capacity))
	// Pre-allocate epsilon stack for loop-based closure in IsMatch (Rust pattern)
	state.epsilonStack = make([]StateID, 0, capacity)

	// SlotTables for capture tracking are initialized lazily on first use.
	// This avoids allocation overhead for non-capture searches (FindAll, IsMatch).
	// See ensureSlotTables().
	state.SlotTable = nil
	state.NextSlotTable = nil
}

// ensureSlotTables lazily initializes SlotTables and capture support.
// Called only when capture tracking is needed (SearchWithSlotTableCaptures).
func (p *PikeVM) ensureSlotTables(state *PikeVMState) {
	if state.SlotTable != nil {
		return // Already initialized
	}
	slotsPerState := p.nfa.CaptureCount() * 2
	numStates := p.nfa.States()
	state.SlotTable = NewSlotTable(numStates, slotsPerState)
	state.NextSlotTable = NewSlotTable(numStates, slotsPerState)

	capacity := numStates
	if capacity < 16 {
		capacity = 16
	}
	state.captureStack = make([]captureFrame, 0, capacity)
	if slotsPerState > 0 {
		state.currSlots = make([]int, slotsPerState)
	}
}

// SetSkipAhead sets the prefilter for skip-ahead optimization.
// When set, PikeVM uses it to skip positions where no match can start
// (when there are no active NFA threads). Safe for partial-coverage prefilters.
func (p *PikeVM) SetSkipAhead(sa SkipAhead) {
	p.skipAhead = sa
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
//
// Greedy/non-greedy semantics follow Rust's approach: DFS ordering determines thread
// priority (left branch explored first), and the search loop breaks after the first
// Match state encountered in DFS order. This prevents non-greedy extension threads
// from being stepped, while allowing greedy extension threads (which appear before
// Match in DFS order) to continue.
//
//nolint:gocognit // Merged match-check + step loop (Rust's nexts pattern) is inherently complex
func (p *PikeVM) searchUnanchoredAt(haystack []byte, startAt int) (int, int, bool) {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	bestStart := -1
	bestEnd := -1

	// Check if NFA is anchored at start (e.g., reverse NFA for $ patterns)
	isAnchored := p.nfa.IsAnchored()

	// Process each byte position once, starting from startAt
	for pos := startAt; pos <= len(haystack); pos++ {
		// Add new start thread at current position (simulates .*? prefix)
		// Stop adding new starts once we've found a match.
		if bestStart == -1 && (!isAnchored || pos == startAt) {
			// Skip-ahead: when no active threads, use prefilter to jump forward.
			// Rust approach (pikevm.rs:1293). Safe for partial-coverage prefilters.
			if len(p.internalState.Queue) == 0 && p.skipAhead != nil && pos > startAt {
				candidate := p.skipAhead.Find(haystack, pos)
				if candidate == -1 {
					break
				}
				pos = candidate
			}
			p.internalState.Visited.Clear()
			p.addThread(thread{state: p.nfa.StartAnchored(), startPos: pos}, haystack, pos)
		}

		// Combined match-check + step loop (Rust's nexts pattern).
		// Threads are in DFS insertion order. For LeftmostFirst semantics,
		// we break after the first Match state, preventing lower-priority
		// threads from being stepped. This is the sole mechanism for
		// greedy/non-greedy: greedy puts ByteRange before Match (gets stepped),
		// non-greedy puts Match before ByteRange (break prevents stepping).
		if pos < len(haystack) {
			b := haystack[pos]
			p.internalState.Visited.Clear() // Fresh visited for next-gen epsilon closures
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
					}
					if !p.internalState.Longest {
						break // LeftmostFirst: stop after first match in DFS order
					}
					continue // Match state has no byte transitions
				}
				p.step(t, b, haystack, pos+1)
			}
		} else {
			// End of input: check matches only (no stepping)
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
					}
					break // First match in DFS order wins
				}
			}
		}

		if pos >= len(haystack) {
			break
		}

		// Early termination: if we have a match and no threads could produce a leftmost one
		if bestStart != -1 {
			hasLeftmostCandidate := false
			for _, t := range p.internalState.NextQueue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
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
//
//nolint:gocognit // Merged match-check + step loop (Rust's nexts pattern) is inherently complex
func (p *PikeVM) searchUnanchoredBetween(haystack []byte, startAt, maxEnd int) (int, int, bool) {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	bestStart := -1
	bestEnd := -1

	isAnchored := p.nfa.IsAnchored()

	for pos := startAt; pos <= maxEnd; pos++ {
		if bestStart == -1 && (!isAnchored || pos == 0) {
			p.internalState.Visited.Clear()
			p.addThread(thread{state: p.nfa.StartAnchored(), startPos: pos}, haystack, pos)
		}

		// Combined match-check + step with break-on-first-match
		if pos < maxEnd && pos < len(haystack) {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
					}
					if !p.internalState.Longest {
						break
					}
					continue
				}
				p.step(t, b, haystack, pos+1)
			}
		} else {
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
					}
					break
				}
			}
		}

		if pos >= maxEnd || pos >= len(haystack) {
			break
		}

		if bestStart != -1 {
			hasLeftmostCandidate := false
			for _, t := range p.internalState.NextQueue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
			}
		}

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
		// At end of input - check if empty string matches at this position.
		// Must use matchesEmptyAt with full haystack context for correct
		// look assertion evaluation (e.g., \B needs previous byte context).
		if p.matchesEmptyAt(haystack, at) {
			return &MatchWithCaptures{
				Start:    at,
				End:      at,
				Captures: p.buildCapturesResult(nil, at, at),
			}
		}
		return nil
	}

	if len(haystack) == 0 {
		// Check if empty string matches (haystack is empty, pos=0)
		if p.matchesEmptyAt(haystack, 0) {
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
//
//nolint:gocognit // Merged match-check + step loop (Rust's nexts pattern) is inherently complex
func (p *PikeVM) searchUnanchoredWithCapturesAt(haystack []byte, startAt int) *MatchWithCaptures {
	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	bestStart := -1
	bestEnd := -1
	var bestCaptures []int

	for pos := startAt; pos <= len(haystack); pos++ {
		if bestStart == -1 {
			p.internalState.Visited.Clear()
			caps := p.newCaptures()
			p.addThread(thread{state: p.nfa.StartAnchored(), startPos: pos, captures: caps}, haystack, pos)
		}

		// Combined match-check + step with break-on-first-match
		if pos < len(haystack) {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
						bestCaptures = t.captures.copyData()
					}
					if !p.internalState.Longest {
						break
					}
					continue
				}
				p.step(t, b, haystack, pos+1)
			}
		} else {
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
						bestCaptures = t.captures.copyData()
					}
					break
				}
			}
		}

		if pos >= len(haystack) {
			break
		}

		if bestStart != -1 {
			hasLeftmostCandidate := false
			for _, t := range p.internalState.NextQueue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
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
	p.addThread(thread{state: p.nfa.StartAnchored(), startPos: startPos, captures: caps}, haystack, startPos)

	lastMatchPos := -1
	var lastMatchCaptures []int

	for pos := startPos; pos <= len(haystack); pos++ {
		// Combined match-check + step with break-on-first-match
		if pos < len(haystack) {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
						lastMatchCaptures = t.captures.copyData()
					}
					if !p.internalState.Longest {
						break
					}
					continue
				}
				p.step(t, b, haystack, pos+1)
			}
		} else {
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
						lastMatchCaptures = t.captures.copyData()
					}
					break
				}
			}
		}

		if len(p.internalState.NextQueue) == 0 && (pos >= len(haystack) || lastMatchPos != -1) {
			break
		}

		if pos >= len(haystack) {
			break
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

// SearchWithCapturesInSpan searches for a match anchored at spanStart,
// not exceeding spanEnd. The full haystack is preserved for lookbehind
// context (e.g., \b word boundary assertions at spanStart-1).
//
// This implements Phase 2 of the DFA-first two-phase search:
//
//	Phase 1: DFA/strategy finds match boundaries [spanStart, spanEnd]
//	Phase 2: PikeVM extracts captures within [spanStart, spanEnd]
//
// The search is anchored: threads are seeded only at spanStart, not at
// every position. This reduces PikeVM work from O(remaining_haystack)
// to O(match_len) per match.
//
// Preconditions:
//   - 0 <= spanStart <= spanEnd <= len(haystack)
//   - A match is known to exist in [spanStart, spanEnd] (from Phase 1)
//
// Returns nil if no match is found (should not happen if Phase 1 is correct).
//
//nolint:gocognit // Merged match-check + step loop (Rust's nexts pattern) is inherently complex
func (p *PikeVM) SearchWithCapturesInSpan(haystack []byte, spanStart, spanEnd int) *MatchWithCaptures {
	if spanStart > spanEnd || spanEnd > len(haystack) {
		return nil
	}

	// Reset state
	p.internalState.Queue = p.internalState.Queue[:0]
	p.internalState.NextQueue = p.internalState.NextQueue[:0]
	p.internalState.Visited.Clear()

	// Seed thread only at spanStart (anchored search within span)
	caps := p.newCaptures()
	p.addThread(thread{state: p.nfa.StartAnchored(), startPos: spanStart, captures: caps}, haystack, spanStart)

	lastMatchPos := -1
	var lastMatchCaptures []int

	// Process bytes from spanStart to spanEnd (not len(haystack)).
	// The full haystack slice is kept so that addThread/step can evaluate
	// lookbehind assertions (\b) using bytes before spanStart.
	for pos := spanStart; pos <= spanEnd; pos++ {
		if pos < spanEnd {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
						lastMatchCaptures = t.captures.copyData()
					}
					if !p.internalState.Longest {
						break
					}
					continue
				}
				p.step(t, b, haystack, pos+1)
			}
		} else {
			// At spanEnd: only check for match states, don't step further
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
						lastMatchCaptures = t.captures.copyData()
					}
					break
				}
			}
		}

		if len(p.internalState.NextQueue) == 0 && (pos >= spanEnd || lastMatchPos != -1) {
			break
		}

		if pos >= spanEnd {
			break
		}

		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	if lastMatchPos != -1 {
		return &MatchWithCaptures{
			Start:    spanStart,
			End:      lastMatchPos,
			Captures: p.buildCapturesResult(lastMatchCaptures, spanStart, lastMatchPos),
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

	p.addThread(thread{state: p.nfa.StartAnchored(), startPos: startPos}, haystack, startPos)

	lastMatchPos := -1

	for pos := startPos; pos <= len(haystack); pos++ {
		// Combined match-check + step with break-on-first-match
		if pos < len(haystack) {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
					}
					if !p.internalState.Longest {
						break
					}
					continue
				}
				p.step(t, b, haystack, pos+1)
			}
		} else {
			for _, t := range p.internalState.Queue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
					}
					break
				}
			}
		}

		if len(p.internalState.NextQueue) == 0 && (pos >= len(haystack) || lastMatchPos != -1) {
			break
		}

		if pos >= len(haystack) {
			break
		}

		p.internalState.Queue, p.internalState.NextQueue = p.internalState.NextQueue, p.internalState.Queue[:0]
	}

	if lastMatchPos != -1 {
		return startPos, lastMatchPos, true
	}

	return -1, -1, false
}

// addThread adds a new thread to the current queue, following epsilon transitions.
// DFS ordering: left branch is explored first, which determines greedy/non-greedy behavior.
// The sparse set (Visited) ensures first-arrival-wins deduplication.
func (p *PikeVM) addThread(t thread, haystack []byte, pos int) {
	if !p.internalState.Visited.Insert(uint32(t.state)) {
		return
	}

	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	switch state.Kind() {
	case StateMatch:
		p.internalState.Queue = append(p.internalState.Queue, t)

	case StateByteRange, StateSparse, StateRuneAny, StateRuneAnyNotNL:
		p.internalState.Queue = append(p.internalState.Queue, t)

	case StateEpsilon:
		next := state.Epsilon()
		if next != InvalidState {
			p.addThread(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, pos)
		}

	case StateSplit:
		// DFS: explore left branch first, then right.
		// For greedy quantifiers: left=continue, right=exit → continue explored first
		// For non-greedy quantifiers: left=exit, right=continue → exit explored first
		// For alternation: left=first alt, right=second alt → first alt explored first
		left, right := state.Split()

		if left != InvalidState {
			p.addThread(thread{state: left, startPos: t.startPos, captures: t.captures}, haystack, pos)
		}
		if right != InvalidState {
			// Clone captures for right branch to ensure COW works properly.
			p.addThread(thread{state: right, startPos: t.startPos, captures: t.captures.clone()}, haystack, pos)
		}

	case StateCapture:
		groupIndex, isStart, next := state.Capture()
		if next != InvalidState {
			newCaps := updateCapture(t.captures, groupIndex, isStart, pos)
			p.addThread(thread{state: next, startPos: t.startPos, captures: newCaps}, haystack, pos)
		}

	case StateLook:
		look, next := state.Look()
		if checkLookAssertion(look, haystack, pos) && next != InvalidState {
			p.addThread(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, pos)
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
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, nextPos)
		}

	case StateSparse:
		for _, tr := range state.Transitions() {
			if b >= tr.Lo && b <= tr.Hi {
				p.addThreadToNext(thread{state: tr.Next, startPos: t.startPos, captures: t.captures}, haystack, nextPos)
			}
		}

	case StateRuneAny:
		if b >= 0x80 && b <= 0xBF {
			p.addThreadToNext(t, haystack, nextPos)
			return
		}
		runePos := nextPos - 1
		if runePos < len(haystack) {
			r, width := utf8.DecodeRune(haystack[runePos:])
			if r != utf8.RuneError || width == 1 {
				// Valid rune (or single byte for ASCII/invalid UTF-8) - advance by full rune width
				next := state.RuneAny()
				newPos := runePos + width
				p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, newPos)
			}
		}

	case StateRuneAnyNotNL:
		if b >= 0x80 && b <= 0xBF {
			p.addThreadToNext(t, haystack, nextPos)
			return
		}
		runePos := nextPos - 1
		if runePos < len(haystack) {
			r, width := utf8.DecodeRune(haystack[runePos:])
			if (r != utf8.RuneError || width == 1) && r != '\n' {
				next := state.RuneAnyNotNL()
				newPos := runePos + width
				p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, newPos)
			}
		}
	}
}

// addThreadToNext adds a thread to the next generation queue.
// DFS ordering: left branch explored first (same as addThread).
func (p *PikeVM) addThreadToNext(t thread, haystack []byte, pos int) {
	if !p.internalState.Visited.Insert(uint32(t.state)) {
		return
	}

	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

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
			p.addThreadToNext(thread{state: right, startPos: t.startPos, captures: t.captures.clone()}, haystack, pos)
		}
		return

	case StateCapture:
		groupIndex, isStart, next := state.Capture()
		if next != InvalidState {
			newCaps := updateCapture(t.captures, groupIndex, isStart, pos)
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: newCaps}, haystack, pos)
		}
		return

	case StateLook:
		look, next := state.Look()
		if checkLookAssertion(look, haystack, pos) && next != InvalidState {
			p.addThreadToNext(thread{state: next, startPos: t.startPos, captures: t.captures}, haystack, pos)
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

// (checkLeftLookSucceeds and calcRightBranchPriority removed —
// greedy/non-greedy is now handled by DFS ordering + break-on-first-match)

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

// =============================================================================
// SlotTable-based Search Methods (New Architecture)
// =============================================================================
//
// These methods use the lightweight searchThread struct and SlotTable for
// capture storage, providing significant memory savings compared to per-thread
// COW captures.
//
// Reference: rust-regex/regex-automata/src/nfa/thompson/pikevm.rs:1811-2160

// SearchWithSlotTable finds the first match using the SlotTable architecture.
// This is more memory-efficient than the legacy Search methods as it uses
// lightweight threads (16 bytes) with per-state capture storage.
//
// Parameters:
//   - haystack: input bytes to search
//   - mode: determines how many capture slots to track (0/2/full)
//
// Returns (start, end, found) for the first match.
//
// This method uses internal state and is NOT thread-safe.
func (p *PikeVM) SearchWithSlotTable(haystack []byte, mode SearchMode) (int, int, bool) {
	return p.SearchWithSlotTableAt(haystack, 0, mode)
}

// SearchWithSlotTableAt finds the first match starting from position 'at'.
// Uses the SlotTable architecture for efficient capture tracking.
//
// Parameters:
//   - haystack: input bytes to search
//   - at: starting position in haystack
//   - mode: determines how many capture slots to track
//
// Returns (start, end, found) for the first match.
func (p *PikeVM) SearchWithSlotTableAt(haystack []byte, at int, mode SearchMode) (int, int, bool) {
	if at > len(haystack) {
		return -1, -1, false
	}

	// Configure slot table for this search mode
	totalSlots := p.nfa.CaptureCount() * 2
	p.internalState.SlotTable.SetActiveSlots(mode.SlotsNeeded(totalSlots))

	// Handle edge cases
	if at == len(haystack) {
		if p.matchesEmptyAt(haystack, at) {
			return at, at, true
		}
		return -1, -1, false
	}

	if len(haystack) == 0 {
		if p.matchesEmpty() {
			return 0, 0, true
		}
		return -1, -1, false
	}

	if p.nfa.IsAnchored() {
		return p.searchWithSlotTableAnchored(haystack, at)
	}

	return p.searchWithSlotTableUnanchored(haystack, at)
}

// searchWithSlotTableUnanchored implements unanchored search using lightweight threads.
// Captures are stored in SlotTable per-state, not per-thread.
//
//nolint:gocognit // Merged match-check + step loop (Rust's nexts pattern) is inherently complex
func (p *PikeVM) searchWithSlotTableUnanchored(haystack []byte, startAt int) (int, int, bool) {
	p.internalState.SearchQueue = p.internalState.SearchQueue[:0]
	p.internalState.SearchNextQueue = p.internalState.SearchNextQueue[:0]
	p.internalState.Visited.Clear()

	if p.internalState.SlotTable.ActiveSlots() > 2 {
		p.internalState.SlotTable.Reset()
	}

	bestStart := -1
	bestEnd := -1

	isAnchored := p.nfa.IsAnchored()

	for pos := startAt; pos <= len(haystack); pos++ {
		if bestStart == -1 && (!isAnchored || pos == 0) {
			// Skip-ahead (Rust pikevm.rs:1293)
			if len(p.internalState.SearchQueue) == 0 && p.skipAhead != nil && pos > startAt {
				candidate := p.skipAhead.Find(haystack, pos)
				if candidate == -1 {
					break
				}
				pos = candidate
			}
			p.internalState.Visited.Clear()
			p.addSearchThread(searchThread{state: p.nfa.StartAnchored(), startPos: pos}, haystack, pos)
		}

		// Combined match-check + step with break-on-first-match
		if pos < len(haystack) {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.SearchQueue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
					}
					if !p.internalState.Longest {
						break
					}
					continue
				}
				p.stepSearchThread(t, b, haystack, pos+1)
			}
		} else {
			for _, t := range p.internalState.SearchQueue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
					}
					break
				}
			}
		}

		if pos >= len(haystack) {
			break
		}

		if bestStart != -1 {
			hasLeftmostCandidate := false
			for _, t := range p.internalState.SearchNextQueue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
			}
		}

		p.internalState.SearchQueue, p.internalState.SearchNextQueue =
			p.internalState.SearchNextQueue, p.internalState.SearchQueue[:0]
	}

	if bestStart != -1 {
		return bestStart, bestEnd, true
	}
	return -1, -1, false
}

// searchWithSlotTableAnchored implements anchored search using lightweight threads.
func (p *PikeVM) searchWithSlotTableAnchored(haystack []byte, startPos int) (int, int, bool) {
	p.internalState.SearchQueue = p.internalState.SearchQueue[:0]
	p.internalState.SearchNextQueue = p.internalState.SearchNextQueue[:0]
	p.internalState.Visited.Clear()

	if p.internalState.SlotTable.ActiveSlots() > 2 {
		p.internalState.SlotTable.Reset()
	}

	p.addSearchThread(searchThread{state: p.nfa.StartAnchored(), startPos: startPos}, haystack, startPos)

	lastMatchPos := -1

	for pos := startPos; pos <= len(haystack); pos++ {
		// Combined match-check + step with break-on-first-match
		if pos < len(haystack) {
			b := haystack[pos]
			p.internalState.Visited.Clear()
			for _, t := range p.internalState.SearchQueue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
					}
					if !p.internalState.Longest {
						break
					}
					continue
				}
				p.stepSearchThread(t, b, haystack, pos+1)
			}
		} else {
			for _, t := range p.internalState.SearchQueue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
					}
					break
				}
			}
		}

		if len(p.internalState.SearchNextQueue) == 0 && (pos >= len(haystack) || lastMatchPos != -1) {
			break
		}

		if pos >= len(haystack) {
			break
		}

		p.internalState.SearchQueue, p.internalState.SearchNextQueue =
			p.internalState.SearchNextQueue, p.internalState.SearchQueue[:0]
	}

	if lastMatchPos != -1 {
		return startPos, lastMatchPos, true
	}
	return -1, -1, false
}

// addSearchThread adds a lightweight thread to the current queue, following epsilon transitions.
// Captures are stored in SlotTable, not in the thread.
//
//nolint:gocognit // Stack-based epsilon closure with 7 state types is inherently complex
func (p *PikeVM) addSearchThread(t searchThread, haystack []byte, pos int) {
	st := &p.internalState
	activeSlots := st.SlotTable.ActiveSlots()

	// Stack-based epsilon closure with capture save/restore (Rust approach).
	// Uses currSlots as working buffer, modified in-place during closure.
	// When terminal state reached: copy currSlots to SlotTable row.
	// When StateCapture encountered: save old value, set new, push RestoreCapture.
	// Reference: rust-regex pikevm.rs:1611-1749 (FollowEpsilon::RestoreCapture)

	st.captureStack = st.captureStack[:0]
	st.captureStack = append(st.captureStack, captureFrame{
		state: t.state, startPos: t.startPos,
	})

	for len(st.captureStack) > 0 {
		n := len(st.captureStack)
		frame := st.captureStack[n-1]
		st.captureStack = st.captureStack[:n-1]

		// RestoreCapture frame: undo a capture slot modification
		if frame.state == InvalidState {
			if activeSlots > 2 && frame.slot < len(st.currSlots) {
				st.currSlots[frame.slot] = frame.value
			}
			continue
		}

		sid := frame.state
		if !st.Visited.Insert(uint32(sid)) {
			continue
		}

		state := p.nfa.State(sid)
		if state == nil {
			continue
		}

		switch state.Kind() {
		case StateMatch, StateByteRange, StateSparse, StateRuneAny, StateRuneAnyNotNL:
			// Terminal state: copy currSlots to this state's SlotTable row
			if activeSlots > 2 {
				stateSlots := st.SlotTable.ForState(sid)
				if stateSlots != nil {
					copy(stateSlots, st.currSlots)
				}
			}
			st.SearchQueue = append(st.SearchQueue, searchThread{
				state: sid, startPos: frame.startPos,
			})

		case StateEpsilon:
			next := state.Epsilon()
			if next != InvalidState {
				st.captureStack = append(st.captureStack, captureFrame{
					state: next, startPos: frame.startPos,
				})
			}

		case StateSplit:
			left, right := state.Split()
			// Push right first (processed last = DFS left-first ordering)
			if right != InvalidState {
				st.captureStack = append(st.captureStack, captureFrame{
					state: right, startPos: frame.startPos,
				})
			}
			if left != InvalidState {
				st.captureStack = append(st.captureStack, captureFrame{
					state: left, startPos: frame.startPos,
				})
			}

		case StateCapture:
			groupIndex, isStart, next := state.Capture()
			if next != InvalidState {
				if activeSlots > 2 {
					slotIndex := int(groupIndex) * 2
					if !isStart {
						slotIndex++
					}
					if slotIndex < len(st.currSlots) {
						// Save old value for restore
						oldValue := st.currSlots[slotIndex]
						// Push RestoreCapture BEFORE explore (will execute after)
						st.captureStack = append(st.captureStack, captureFrame{
							state: InvalidState, // marker: restore frame
							slot:  slotIndex,
							value: oldValue,
						})
						// Set new capture value
						st.currSlots[slotIndex] = pos
					}
				}
				st.captureStack = append(st.captureStack, captureFrame{
					state: next, startPos: frame.startPos,
				})
			}

		case StateLook:
			look, next := state.Look()
			if checkLookAssertion(look, haystack, pos) && next != InvalidState {
				st.captureStack = append(st.captureStack, captureFrame{
					state: next, startPos: frame.startPos,
				})
			}

		case StateFail:
			// Dead state
		}
	}
}

// stepSearchThread processes a byte transition for a lightweight thread.
func (p *PikeVM) stepSearchThread(t searchThread, b byte, haystack []byte, nextPos int) {
	state := p.nfa.State(t.state)
	if state == nil {
		return
	}

	switch state.Kind() {
	case StateByteRange:
		lo, hi, next := state.ByteRange()
		if b >= lo && b <= hi {
			p.addSearchThreadToNext(searchThread{state: next, startPos: t.startPos}, t.state, haystack, nextPos)
		}

	case StateSparse:
		for _, tr := range state.Transitions() {
			if b >= tr.Lo && b <= tr.Hi {
				p.addSearchThreadToNext(searchThread{state: tr.Next, startPos: t.startPos}, t.state, haystack, nextPos)
			}
		}

	case StateRuneAny:
		if b >= 0x80 && b <= 0xBF {
			p.internalState.SearchNextQueue = append(p.internalState.SearchNextQueue, t)
			return
		}
		runePos := nextPos - 1
		if runePos < len(haystack) {
			r, width := utf8.DecodeRune(haystack[runePos:])
			if r != utf8.RuneError || width == 1 {
				next := state.RuneAny()
				newPos := runePos + width
				p.addSearchThreadToNext(searchThread{state: next, startPos: t.startPos}, t.state, haystack, newPos)
			}
		}

	case StateRuneAnyNotNL:
		if b >= 0x80 && b <= 0xBF {
			p.internalState.SearchNextQueue = append(p.internalState.SearchNextQueue, t)
			return
		}
		runePos := nextPos - 1
		if runePos < len(haystack) {
			r, width := utf8.DecodeRune(haystack[runePos:])
			if (r != utf8.RuneError || width == 1) && r != '\n' {
				next := state.RuneAnyNotNL()
				newPos := runePos + width
				p.addSearchThreadToNext(searchThread{state: next, startPos: t.startPos}, t.state, haystack, newPos)
			}
		}
	}
}

// addSearchThreadToNext adds a lightweight thread to the next queue.
// srcState is the state we came from (for slot copying).
// Uses stack-based epsilon closure with capture save/restore.
//
//nolint:gocognit,gocyclo,cyclop // Stack-based epsilon closure with capture save/restore
func (p *PikeVM) addSearchThreadToNext(t searchThread, srcState StateID, haystack []byte, pos int) {
	st := &p.internalState
	activeSlots := st.SlotTable.ActiveSlots()

	// Load currSlots from CURRENT SlotTable (source state's row)
	if activeSlots > 2 {
		srcSlots := st.SlotTable.ForState(srcState)
		if srcSlots != nil && len(st.currSlots) > 0 {
			copy(st.currSlots, srcSlots)
		}
	}

	// Stack-based epsilon closure writing to NEXT SlotTable
	st.captureStack = st.captureStack[:0]
	st.captureStack = append(st.captureStack, captureFrame{
		state: t.state, startPos: t.startPos,
	})

	for len(st.captureStack) > 0 {
		n := len(st.captureStack)
		frame := st.captureStack[n-1]
		st.captureStack = st.captureStack[:n-1]

		if frame.state == InvalidState {
			if activeSlots > 2 && frame.slot < len(st.currSlots) {
				st.currSlots[frame.slot] = frame.value
			}
			continue
		}

		sid := frame.state
		if !st.Visited.Insert(uint32(sid)) {
			continue
		}

		state := p.nfa.State(sid)
		if state == nil {
			continue
		}

		switch state.Kind() {
		case StateMatch, StateByteRange, StateSparse, StateRuneAny, StateRuneAnyNotNL:
			// Write to NEXT SlotTable (not current!)
			if activeSlots > 2 {
				stateSlots := st.NextSlotTable.ForState(sid)
				if stateSlots != nil {
					copy(stateSlots, st.currSlots)
				}
			}
			st.SearchNextQueue = append(st.SearchNextQueue, searchThread{
				state: sid, startPos: frame.startPos,
			})

		case StateEpsilon:
			next := state.Epsilon()
			if next != InvalidState {
				st.captureStack = append(st.captureStack, captureFrame{
					state: next, startPos: frame.startPos,
				})
			}

		case StateSplit:
			left, right := state.Split()
			if right != InvalidState {
				st.captureStack = append(st.captureStack, captureFrame{
					state: right, startPos: frame.startPos,
				})
			}
			if left != InvalidState {
				st.captureStack = append(st.captureStack, captureFrame{
					state: left, startPos: frame.startPos,
				})
			}

		case StateCapture:
			groupIndex, isStart, next := state.Capture()
			if next != InvalidState {
				if activeSlots > 2 {
					slotIndex := int(groupIndex) * 2
					if !isStart {
						slotIndex++
					}
					if slotIndex < len(st.currSlots) {
						oldValue := st.currSlots[slotIndex]
						st.captureStack = append(st.captureStack, captureFrame{
							state: InvalidState,
							slot:  slotIndex,
							value: oldValue,
						})
						st.currSlots[slotIndex] = pos
					}
				}
				st.captureStack = append(st.captureStack, captureFrame{
					state: next, startPos: frame.startPos,
				})
			}

		case StateLook:
			look, next := state.Look()
			if checkLookAssertion(look, haystack, pos) && next != InvalidState {
				st.captureStack = append(st.captureStack, captureFrame{
					state: next, startPos: frame.startPos,
				})
			}

		case StateFail:
		}
	}
}

// SearchWithSlotTableCaptures finds the first match and returns captures.
// Uses zero-allocation SlotTable architecture (Rust approach).
func (p *PikeVM) SearchWithSlotTableCaptures(haystack []byte) *MatchWithCaptures {
	return p.SearchWithSlotTableCapturesAt(haystack, 0)
}

// SearchWithSlotTableCapturesAt finds the first match with captures starting from 'at'.
// Uses dual SlotTable (curr/next) for zero-allocation capture tracking.
// Matches Rust's PikeVM Cache with curr/next ActiveStates (pikevm.rs:1878).
func (p *PikeVM) SearchWithSlotTableCapturesAt(haystack []byte, at int) *MatchWithCaptures {
	if at > len(haystack) {
		return nil
	}

	// Lazy init SlotTables (only on first capture search)
	p.ensureSlotTables(&p.internalState)

	totalSlots := p.nfa.CaptureCount() * 2
	p.internalState.SlotTable.SetActiveSlots(totalSlots)
	p.internalState.NextSlotTable.SetActiveSlots(totalSlots)

	numGroups := p.nfa.CaptureCount()

	if at == len(haystack) {
		if p.matchesEmptyAt(haystack, at) {
			return p.buildCapturesFromSlots(nil, at, at)
		}
		return nil
	}
	if len(haystack) == 0 {
		if p.matchesEmpty() {
			return p.buildCapturesFromSlots(nil, 0, 0)
		}
		return nil
	}
	_ = numGroups

	if p.nfa.IsAnchored() {
		return p.searchWithSlotTableCapturesAnchored(haystack, at)
	}
	return p.searchWithSlotTableCapturesUnanchored(haystack, at)
}

// searchWithSlotTableCapturesUnanchored implements unanchored search with captures.
// Captures stored in SlotTable per-state, saved to bestSlots on match.
//
//nolint:gocognit,gocyclo,cyclop // Merged match-check + step + seed loop
func (p *PikeVM) searchWithSlotTableCapturesUnanchored(haystack []byte, startAt int) *MatchWithCaptures {
	st := &p.internalState
	st.SearchQueue = st.SearchQueue[:0]
	st.SearchNextQueue = st.SearchNextQueue[:0]
	st.Visited.Clear()
	st.SlotTable.Reset()
	st.NextSlotTable.Reset()

	totalSlots := st.SlotTable.ActiveSlots()
	st.NextSlotTable.SetActiveSlots(totalSlots)
	bestStart := -1
	bestEnd := -1
	var bestSlots []int

	for pos := startAt; pos <= len(haystack); pos++ {
		if bestStart == -1 {
			if len(st.SearchQueue) == 0 && p.skipAhead != nil && pos > startAt {
				candidate := p.skipAhead.Find(haystack, pos)
				if candidate == -1 {
					break
				}
				pos = candidate
			}
			// DON'T clear Visited here — states already in SearchQueue must
			// not be overwritten by new seed. Visited prevents re-entry.
			// Visited is cleared before step loop (below), not before seed.
			startSid := p.nfa.StartAnchored()
			for i := range st.currSlots {
				st.currSlots[i] = -1
			}
			p.addSearchThread(searchThread{state: startSid, startPos: pos}, haystack, pos)
		}

		if pos < len(haystack) {
			b := haystack[pos]
			st.Visited.Clear()
			for _, t := range st.SearchQueue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
						// Read from CURRENT SlotTable
						matchSlots := st.SlotTable.ForState(t.state)
						if matchSlots != nil && totalSlots > 0 {
							if bestSlots == nil {
								bestSlots = make([]int, totalSlots)
							}
							copy(bestSlots, matchSlots)
						}
					}
					if !st.Longest {
						break
					}
					continue
				}
				// stepSearchThread → addSearchThreadToNext reads curr, writes next
				p.stepSearchThread(t, b, haystack, pos+1)
			}
		} else {
			for _, t := range st.SearchQueue {
				if p.nfa.IsMatch(t.state) {
					if p.isBetterMatch(bestStart, bestEnd, t.startPos, pos) {
						bestStart = t.startPos
						bestEnd = pos
						matchSlots := st.SlotTable.ForState(t.state)
						if matchSlots != nil && totalSlots > 0 {
							if bestSlots == nil {
								bestSlots = make([]int, totalSlots)
							}
							copy(bestSlots, matchSlots)
						}
					}
					break
				}
			}
		}

		if pos >= len(haystack) {
			break
		}

		if bestStart != -1 {
			hasLeftmostCandidate := false
			for _, t := range st.SearchNextQueue {
				if t.startPos <= bestStart {
					hasLeftmostCandidate = true
					break
				}
			}
			if !hasLeftmostCandidate {
				break
			}
		}

		// Swap queues AND SlotTables (Rust: core::mem::swap(curr, next))
		st.SearchQueue, st.SearchNextQueue = st.SearchNextQueue, st.SearchQueue[:0]
		st.SlotTable, st.NextSlotTable = st.NextSlotTable, st.SlotTable
	}

	if bestStart == -1 {
		return nil
	}
	return p.buildCapturesFromSlots(bestSlots, bestStart, bestEnd)
}

// searchWithSlotTableCapturesAnchored implements anchored search with captures.
//
//nolint:gocognit // Merged match-check + step loop (Rust's nexts pattern)
func (p *PikeVM) searchWithSlotTableCapturesAnchored(haystack []byte, startPos int) *MatchWithCaptures {
	st := &p.internalState
	st.SearchQueue = st.SearchQueue[:0]
	st.SearchNextQueue = st.SearchNextQueue[:0]
	st.Visited.Clear()
	st.SlotTable.Reset()
	st.NextSlotTable.Reset()

	totalSlots := st.SlotTable.ActiveSlots()
	st.NextSlotTable.SetActiveSlots(totalSlots)

	startSid := p.nfa.StartAnchored()
	for i := range st.currSlots {
		st.currSlots[i] = -1
	}
	p.addSearchThread(searchThread{state: startSid, startPos: startPos}, haystack, startPos)

	lastMatchPos := -1
	var bestSlots []int

	for pos := startPos; pos <= len(haystack); pos++ {
		if pos < len(haystack) {
			b := haystack[pos]
			st.Visited.Clear()
			for _, t := range st.SearchQueue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
						matchSlots := st.SlotTable.ForState(t.state)
						if matchSlots != nil && totalSlots > 0 {
							if bestSlots == nil {
								bestSlots = make([]int, totalSlots)
							}
							copy(bestSlots, matchSlots)
						}
					}
					if !st.Longest {
						break
					}
					continue
				}
				p.stepSearchThread(t, b, haystack, pos+1)
			}
		} else {
			for _, t := range st.SearchQueue {
				if p.nfa.IsMatch(t.state) {
					if pos > lastMatchPos || lastMatchPos == -1 {
						lastMatchPos = pos
						matchSlots := st.SlotTable.ForState(t.state)
						if matchSlots != nil && totalSlots > 0 {
							if bestSlots == nil {
								bestSlots = make([]int, totalSlots)
							}
							copy(bestSlots, matchSlots)
						}
					}
					break
				}
			}
		}

		if len(st.SearchNextQueue) == 0 && (pos >= len(haystack) || lastMatchPos != -1) {
			break
		}
		if pos >= len(haystack) {
			break
		}

		st.SearchQueue, st.SearchNextQueue = st.SearchNextQueue, st.SearchQueue[:0]
		st.SlotTable, st.NextSlotTable = st.NextSlotTable, st.SlotTable
	}

	if lastMatchPos == -1 {
		return nil
	}
	return p.buildCapturesFromSlots(bestSlots, startPos, lastMatchPos)
}

// buildCapturesFromSlots converts flat slot data to MatchWithCaptures result.
func (p *PikeVM) buildCapturesFromSlots(slots []int, matchStart, matchEnd int) *MatchWithCaptures {
	numGroups := p.nfa.CaptureCount()
	captures := make([][]int, numGroups)
	captures[0] = []int{matchStart, matchEnd}

	if slots != nil {
		for i := 1; i < numGroups && i*2+1 < len(slots); i++ {
			start := slots[i*2]
			end := slots[i*2+1]
			if start >= 0 && end >= 0 {
				captures[i] = []int{start, end}
			}
		}
	}

	return &MatchWithCaptures{
		Start:    matchStart,
		End:      matchEnd,
		Captures: captures,
	}
}
