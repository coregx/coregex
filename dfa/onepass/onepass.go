// Package onepass implements a one-pass DFA for regex patterns
// that have no ambiguity in their matching paths.
//
// A regex is "one-pass" when at each input byte during an anchored match,
// there is at most one possible path through the automaton. This property
// enables efficient capture group extraction without backtracking.
//
// Performance: OnePass DFA provides ~10-20x speedup over PikeVM for patterns
// with capture groups, approaching the speed of non-capturing DFA.
//
// Limitations:
//   - Only supports anchored searches (no unanchored prefix)
//   - Maximum 16 capture groups (32 slots)
//   - Not all patterns are one-pass (e.g., `a*a`, `(.*)x` are NOT one-pass)
//
// Example one-pass patterns:
//   - `(\d+)-(\d+)`           - Digit groups separated by dash
//   - `([a-z]+)\s+([a-z]+)`   - Word pairs
//   - `x*yx*`                 - Unambiguous repetition
//   - `[^ ]* .*`              - Non-space followed by anything
//
// Example non-one-pass patterns:
//   - `a*a`                   - Ambiguous: extend a* or final a?
//   - `(.*) (.*)`             - Where does first group end?
//   - `(ab|ac)`               - Same first byte in alternation
package onepass

import (
	"errors"
	"github.com/coregx/coregex/nfa"
)

var (
	// ErrNotOnePass is returned when a pattern is not one-pass.
	ErrNotOnePass = errors.New("pattern is not one-pass")

	// ErrTooManyCaptures is returned when a pattern has more than 16 capture groups.
	ErrTooManyCaptures = errors.New("too many capture groups (max 16)")
)

// StateID is a DFA state identifier (21 bits max = 2M states).
type StateID uint32

// DFA represents a one-pass deterministic finite automaton.
//
// The DFA can only be used for anchored searches but provides
// ~10-20x speedup over PikeVM for patterns with capture groups.
//
// The transition table is organized as:
//
//	table[stateID * stride + byteClass] → Transition
//
// where stride is the next power of 2 >= alphabetLen.
type DFA struct {
	// Pattern information
	numCaptures int // number of capture groups (max 16)

	// Transition table: dense array indexed by [stateID][byteClass]
	// Layout: [state0_class0, state0_class1, ..., state1_class0, ...]
	table []Transition

	// Byte equivalence classes (from NFA)
	// Maps each byte to a class ID [0, alphabetLen)
	classes *nfa.ByteClasses

	// Alphabet size (number of byte equivalence classes)
	alphabetLen int

	// Stride for indexing: next power of 2 >= alphabetLen
	// Enables fast indexing: table[sid << stride2 + class]
	stride  int
	stride2 uint // log2(stride) for shift operations

	// Start state ID (always anchored)
	startState StateID

	// Match states bitmap for O(1) match detection
	// matchStates[sid] is true if state sid is a match state
	matchStates []bool

	// Match slots: slots to apply when reaching each match state
	// matchSlots[sid] contains the slot mask for match state sid
	// These slots represent capture positions at the match (END positions)
	matchSlots []uint32

	// Minimum match state ID for fast match detection
	// States with ID >= minMatchID are match states
	minMatchID StateID

	// State count
	stateCount int
}

// Cache holds per-search state for capture groups.
//
// This is allocated once and reused across searches to avoid allocations.
type Cache struct {
	// slots stores capture group positions: [start0, end0, start1, end1, ...]
	// Group 0 is the entire match, groups 1+ are explicit captures
	slots []int
}

// NewCache creates a new cache for the given number of capture groups.
// numCaptures includes group 0 (entire match).
func NewCache(numCaptures int) *Cache {
	return &Cache{
		slots: make([]int, numCaptures*2),
	}
}

// Reset clears the cache for a new search.
func (c *Cache) Reset() {
	for i := range c.slots {
		c.slots[i] = -1
	}
}

// Slots returns the capture group slots.
// Returns [start0, end0, start1, end1, ...] where group i is at [i*2, i*2+1].
func (c *Cache) Slots() []int {
	return c.slots
}

// NumCaptures returns the number of capture groups tracked by this DFA.
// This includes group 0 (entire match) plus explicit capture groups.
func (d *DFA) NumCaptures() int {
	return d.numCaptures
}

// IsMatch returns true if the input matches (anchored).
// Faster than Search when captures aren't needed.
func (d *DFA) IsMatch(input []byte) bool {
	state := d.startState

	for _, b := range input {
		class := d.classes.Get(b)
		trans := d.getTransition(state, class)

		if trans.IsDead() {
			return false
		}

		state = trans.NextState()

		// Check for match (early termination)
		if d.isMatchState(state) {
			return true
		}
	}

	// Check final state
	return d.isMatchState(state)
}

// getTransition retrieves the transition for the given state and byte class.
func (d *DFA) getTransition(state StateID, class byte) Transition {
	idx := (int(state) << d.stride2) + int(class)
	if idx >= len(d.table) {
		return NewTransition(DeadState, false, 0)
	}
	return d.table[idx]
}

// isMatchState returns true if the given state is a match state.
func (d *DFA) isMatchState(state StateID) bool {
	if int(state) >= len(d.matchStates) {
		return false
	}
	return d.matchStates[state]
}

// getMatchSlots returns the slot mask to apply when reaching the given match state.
// These slots represent capture END positions.
func (d *DFA) getMatchSlots(state StateID) uint32 {
	if int(state) >= len(d.matchSlots) {
		return 0
	}
	return d.matchSlots[state]
}

// nextPowerOf2 returns the next power of 2 >= n.
func nextPowerOf2(n int) int {
	if n <= 0 {
		return 1
	}
	// Check if already power of 2
	if n&(n-1) == 0 {
		return n
	}
	// Find next power of 2
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// log2 returns the base-2 logarithm of n (must be power of 2).
func log2(n int) uint {
	if n <= 0 {
		return 0
	}
	var log uint
	for n > 1 {
		n >>= 1
		log++
	}
	return log
}
