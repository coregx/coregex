package nfa

import (
	"fmt"
)

// StateID uniquely identifies an NFA state.
// This is a 32-bit unsigned integer for compact representation.
type StateID uint32

// Special state constants
const (
	// InvalidState represents an invalid/uninitialized state ID
	InvalidState StateID = 0xFFFFFFFF

	// FailState represents a dead/failure state (no transitions)
	FailState StateID = 0xFFFFFFFE
)

// StateKind identifies the type of NFA state and determines which transitions are valid.
type StateKind uint8

const (
	// StateMatch represents a match state (accepting state)
	StateMatch StateKind = iota

	// StateByteRange represents a single byte or byte range transition [lo, hi]
	StateByteRange

	// StateSparse represents multiple byte transitions (character class)
	// e.g., [a-zA-Z0-9] would use this with a list of byte ranges
	StateSparse

	// StateSplit represents an epsilon transition to 2 states (alternation)
	// Used for alternation (a|b) and optional patterns (a?)
	StateSplit

	// StateEpsilon represents an epsilon transition to 1 state
	// Used for sequencing without consuming input
	StateEpsilon

	// StateCapture represents a capture group boundary (future feature)
	// Not implemented in MVP but reserved for future use
	StateCapture

	// StateFail represents a dead state (no valid transitions)
	StateFail
)

// String returns a human-readable representation of the StateKind
func (k StateKind) String() string {
	switch k {
	case StateMatch:
		return "Match"
	case StateByteRange:
		return "ByteRange"
	case StateSparse:
		return "Sparse"
	case StateSplit:
		return "Split"
	case StateEpsilon:
		return "Epsilon"
	case StateCapture:
		return "Capture"
	case StateFail:
		return "Fail"
	default:
		return fmt.Sprintf("Unknown(%d)", k)
	}
}

// State represents a single NFA state with its transitions.
// The state's kind determines which fields are valid.
type State struct {
	id   StateID
	kind StateKind

	// For ByteRange: single byte or range [lo, hi]
	lo, hi byte
	next   StateID // target state for ByteRange/Epsilon

	// For Sparse: multiple byte ranges with corresponding targets
	// Pre-allocated to avoid heap allocations during search
	transitions []Transition

	// For Split: epsilon transitions to two states
	left, right StateID

	// For Capture: capture group index and whether this is opening/closing
	captureIndex uint32
	captureStart bool // true = opening boundary, false = closing boundary
}

// Transition represents a byte range and target state for sparse transitions.
// Used in character classes like [a-zA-Z0-9].
type Transition struct {
	Lo   byte    // inclusive lower bound
	Hi   byte    // inclusive upper bound
	Next StateID // target state
}

// ID returns the state's unique identifier
func (s *State) ID() StateID {
	return s.id
}

// Kind returns the state's type
func (s *State) Kind() StateKind {
	return s.kind
}

// IsMatch returns true if this is a match state
func (s *State) IsMatch() bool {
	return s.kind == StateMatch
}

// ByteRange returns the byte range for ByteRange states.
// Returns (0, 0, InvalidState) for non-ByteRange states.
func (s *State) ByteRange() (lo, hi byte, next StateID) {
	if s.kind == StateByteRange {
		return s.lo, s.hi, s.next
	}
	return 0, 0, InvalidState
}

// Split returns the two target states for Split states.
// Returns (InvalidState, InvalidState) for non-Split states.
func (s *State) Split() (left, right StateID) {
	if s.kind == StateSplit {
		return s.left, s.right
	}
	return InvalidState, InvalidState
}

// Epsilon returns the target state for Epsilon states.
// Returns InvalidState for non-Epsilon states.
func (s *State) Epsilon() StateID {
	if s.kind == StateEpsilon {
		return s.next
	}
	return InvalidState
}

// Transitions returns the list of transitions for Sparse states.
// Returns nil for non-Sparse states.
func (s *State) Transitions() []Transition {
	if s.kind == StateSparse {
		return s.transitions
	}
	return nil
}

// Capture returns capture group info for Capture states.
// Returns (group index, isStart, next state).
// isStart is true for opening boundary '(' and false for closing ')'.
func (s *State) Capture() (index uint32, isStart bool, next StateID) {
	if s.kind == StateCapture {
		return s.captureIndex, s.captureStart, s.next
	}
	return 0, false, InvalidState
}

// String returns a human-readable representation of the state
func (s *State) String() string {
	switch s.kind {
	case StateMatch:
		return fmt.Sprintf("State(%d, Match)", s.id)
	case StateByteRange:
		if s.lo == s.hi {
			return fmt.Sprintf("State(%d, ByteRange '%c' -> %d)", s.id, s.lo, s.next)
		}
		return fmt.Sprintf("State(%d, ByteRange ['%c'-'%c'] -> %d)", s.id, s.lo, s.hi, s.next)
	case StateSparse:
		return fmt.Sprintf("State(%d, Sparse %d transitions)", s.id, len(s.transitions))
	case StateSplit:
		return fmt.Sprintf("State(%d, Split -> [%d, %d])", s.id, s.left, s.right)
	case StateEpsilon:
		return fmt.Sprintf("State(%d, Epsilon -> %d)", s.id, s.next)
	case StateFail:
		return fmt.Sprintf("State(%d, Fail)", s.id)
	default:
		return fmt.Sprintf("State(%d, Unknown)", s.id)
	}
}

// NFA represents a compiled Thompson NFA.
// It is the result of compiling a regexp/syntax.Regexp pattern.
type NFA struct {
	// states contains all NFA states indexed by StateID
	states []State

	// startAnchored is the start state for anchored searches.
	// Points directly to the compiled pattern.
	startAnchored StateID

	// startUnanchored is the start state for unanchored searches.
	// Points to the (?s:.)*? prefix for O(n) unanchored matching.
	// When pattern is anchored (has ^ prefix), equals startAnchored.
	startUnanchored StateID

	// anchored indicates if the pattern must match at the start of input
	anchored bool

	// utf8 indicates if the NFA respects UTF-8 boundaries
	// When true, matches won't split multi-byte UTF-8 sequences
	utf8 bool

	// patternCount is the number of patterns in a multi-pattern NFA
	// For single patterns, this is 1
	patternCount int

	// captureCount is the number of capture groups in the pattern
	// Group 0 is the entire match, groups 1+ are explicit captures
	captureCount int

	// captureNames stores the names of named capture groups.
	// Index 0 is always "" (entire match), subsequent indices correspond to capture groups.
	// For unnamed captures, the name is "".
	// Example: pattern `(?P<year>\d+)-(\d+)` → ["", "year", ""]
	captureNames []string

	// byteClasses maps bytes to equivalence classes for DFA optimization.
	// Bytes in the same class always have identical transitions in any DFA state.
	// This reduces DFA state size from 256 transitions to ~8-16 transitions.
	byteClasses ByteClasses
}

// Start returns the starting state ID of the NFA
//
// Deprecated: Use StartAnchored() or StartUnanchored() for explicit control
func (n *NFA) Start() StateID {
	return n.startAnchored
}

// StartAnchored returns the start state for anchored searches
func (n *NFA) StartAnchored() StateID {
	return n.startAnchored
}

// StartUnanchored returns the start state for unanchored searches
func (n *NFA) StartUnanchored() StateID {
	return n.startUnanchored
}

// IsAlwaysAnchored returns true if anchored and unanchored starts are the same.
// This indicates the pattern is inherently anchored (has ^ prefix).
func (n *NFA) IsAlwaysAnchored() bool {
	return n.startAnchored == n.startUnanchored
}

// State returns the state with the given ID.
// Returns nil if the ID is invalid.
func (n *NFA) State(id StateID) *State {
	if id == InvalidState || int(id) >= len(n.states) {
		return nil
	}
	return &n.states[id]
}

// IsMatch returns true if the given state is a match state
func (n *NFA) IsMatch(id StateID) bool {
	if s := n.State(id); s != nil {
		return s.IsMatch()
	}
	return false
}

// States returns the total number of states in the NFA
func (n *NFA) States() int {
	return len(n.states)
}

// IsAnchored returns true if the NFA requires anchored matching
func (n *NFA) IsAnchored() bool {
	return n.anchored
}

// IsUTF8 returns true if the NFA respects UTF-8 boundaries
func (n *NFA) IsUTF8() bool {
	return n.utf8
}

// PatternCount returns the number of patterns in the NFA
func (n *NFA) PatternCount() int {
	return n.patternCount
}

// CaptureCount returns the number of capture groups in the NFA.
// Group 0 is the entire match, groups 1+ are explicit captures.
// For a pattern like "(a)(b)", this returns 3 (entire match + 2 groups).
func (n *NFA) CaptureCount() int {
	return n.captureCount
}

// SubexpNames returns the names of capture groups in the pattern.
// Index 0 is always "" (representing the entire match).
// Named groups return their names, unnamed groups return "".
//
// Example:
//
//	pattern: `(?P<year>\d+)-(\d+)-(?P<day>\d+)`
//	returns: ["", "year", "", "day"]
//
// This matches stdlib regexp.Regexp.SubexpNames() behavior.
func (n *NFA) SubexpNames() []string {
	if len(n.captureNames) == 0 {
		// No capture names stored - return empty strings for all groups
		names := make([]string, n.captureCount)
		return names
	}
	// Return a copy to prevent external modification
	names := make([]string, len(n.captureNames))
	copy(names, n.captureNames)
	return names
}

// ByteClasses returns the byte equivalence classes for this NFA.
// Used by DFA to reduce transition table size from 256 to ~8-16 entries.
func (n *NFA) ByteClasses() *ByteClasses {
	return &n.byteClasses
}

// Iter returns an iterator over all states in the NFA
func (n *NFA) Iter() *StateIter {
	return &StateIter{
		nfa: n,
		pos: 0,
	}
}

// StateIter is an iterator over NFA states
type StateIter struct {
	nfa *NFA
	pos int
}

// Next returns the next state in the iteration.
// Returns nil when iteration is complete.
func (it *StateIter) Next() *State {
	if it.pos >= len(it.nfa.states) {
		return nil
	}
	s := &it.nfa.states[it.pos]
	it.pos++
	return s
}

// HasNext returns true if there are more states to iterate
func (it *StateIter) HasNext() bool {
	return it.pos < len(it.nfa.states)
}

// String returns a human-readable representation of the NFA
func (n *NFA) String() string {
	return fmt.Sprintf("NFA{states: %d, startAnchored: %d, startUnanchored: %d, anchored: %v, utf8: %v}",
		len(n.states), n.startAnchored, n.startUnanchored, n.anchored, n.utf8)
}
