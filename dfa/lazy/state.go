package lazy

import (
	"fmt"
	"hash/fnv"

	"github.com/coregx/coregex/nfa"
)

// StateID uniquely identifies a DFA state in the cache.
// This is a 32-bit unsigned integer for compact representation.
type StateID uint32

// Special state constants
const (
	// InvalidState represents an invalid/uninitialized state ID
	InvalidState StateID = 0xFFFFFFFF

	// DeadState represents a dead/failure state with no outgoing transitions.
	// Once in this state, the DFA can never match.
	DeadState StateID = 0xFFFFFFFE

	// StartState is always state ID 0 (the initial state)
	StartState StateID = 0
)

// invalidTransitions is a pre-initialized template with all transitions set to InvalidState.
// Used for O(1) initialization of new states instead of O(256) loop.
var invalidTransitions [256]StateID

func init() {
	for i := range invalidTransitions {
		invalidTransitions[i] = InvalidState
	}
}

// State represents a DFA state with its transitions.
//
// A DFA state is deterministic: for each input byte, there is at most one
// target state. Uses a fixed-size 256-element array for O(1) transition lookup.
//
// Memory: 256 * 4 bytes = 1KB per state for transitions.
// With 10,000 states max, that's ~10MB total. This is acceptable for the
// 10x speedup over map-based lookups.
//
// Word Boundary Tracking (Rust regex-automata approach):
// The isFromWord field tracks whether this state was entered via a word byte.
// This is essential for correct \b and \B handling in DFA:
//   - When computing transitions, we compare isFromWord with isWordByte(input)
//   - If different → word boundary (\b) satisfied
//   - If same → non-word boundary (\B) satisfied
//
// States with same NFA states but different isFromWord are DIFFERENT DFA states!
type State struct {
	// id uniquely identifies this state in the cache
	id StateID

	// transitions is a fixed-size 256-element array: transitions[byte] = next state ID.
	// InvalidState means no transition for that byte.
	// Using array instead of map gives O(1) lookup without hash computation.
	transitions [256]StateID

	// transitionCount tracks how many valid transitions exist (for statistics/debugging)
	transitionCount int

	// isMatch indicates if this is an accepting state
	isMatch bool

	// isFromWord indicates if this state was entered via a word byte transition.
	// Used for word boundary (\b, \B) assertion evaluation.
	// At start of input, this is false (no previous byte = non-word).
	isFromWord bool

	// nfaStates is the set of NFA states this DFA state represents.
	// This is used during determinization to compute transitions.
	// Pre-allocated to avoid heap allocations during search.
	nfaStates []nfa.StateID

	// accelBytes contains 1-3 exit bytes for accelerable states.
	// An accelerable state is one where most bytes loop back to self,
	// and only 1-3 bytes cause a transition to a different state.
	// This allows using memchr/memchr2/memchr3 to skip ahead in the input.
	// nil means the state is not accelerable.
	accelBytes []byte

	// accelChecked is true if acceleration detection has been attempted.
	// This prevents repeated detection attempts on non-accelerable states.
	accelChecked bool
}

// NewState creates a new DFA state with the given ID and NFA state set.
// isFromWord indicates if this state was entered via a word byte (for \b/\B handling).
func NewState(id StateID, nfaStates []nfa.StateID, isMatch bool) *State {
	return NewStateWithWordContext(id, nfaStates, isMatch, false)
}

// NewStateWithWordContext creates a new DFA state with explicit word context.
// isFromWord indicates if this state was entered via a word byte transition.
// This is essential for correct word boundary (\b, \B) handling in DFA.
func NewStateWithWordContext(id StateID, nfaStates []nfa.StateID, isMatch bool, isFromWord bool) *State {
	// Copy NFA states to avoid aliasing
	nfaStatesCopy := make([]nfa.StateID, len(nfaStates))
	copy(nfaStatesCopy, nfaStates)

	// Create state with transitions initialized from template (O(1) copy instead of O(256) loop)
	return &State{
		id:          id,
		transitions: invalidTransitions, // Copy by value from pre-initialized template
		isMatch:     isMatch,
		isFromWord:  isFromWord,
		nfaStates:   nfaStatesCopy,
	}
}

// ID returns the state's unique identifier
func (s *State) ID() StateID {
	return s.id
}

// IsMatch returns true if this is an accepting state
func (s *State) IsMatch() bool {
	return s.isMatch
}

// IsFromWord returns true if this state was entered via a word byte transition.
// Used for word boundary (\b, \B) assertion evaluation.
func (s *State) IsFromWord() bool {
	return s.isFromWord
}

// Transition returns the next state for the given input byte.
// Returns (InvalidState, false) if no transition exists.
// This is the hot path - O(1) array lookup instead of map.
func (s *State) Transition(b byte) (StateID, bool) {
	next := s.transitions[b]
	return next, next != InvalidState
}

// AddTransition adds a transition from this state to another on input byte b.
// Overwrites any existing transition for this byte.
func (s *State) AddTransition(b byte, next StateID) {
	if s.transitions[b] == InvalidState && next != InvalidState {
		s.transitionCount++
	} else if s.transitions[b] != InvalidState && next == InvalidState {
		s.transitionCount--
	}
	s.transitions[b] = next
}

// NFAStates returns the NFA states represented by this DFA state
func (s *State) NFAStates() []nfa.StateID {
	return s.nfaStates
}

// TransitionCount returns the number of valid transitions from this state
func (s *State) TransitionCount() int {
	return s.transitionCount
}

// String returns a human-readable representation of the state
func (s *State) String() string {
	return fmt.Sprintf("DFAState(id=%d, isMatch=%v, transitions=%d, nfaStates=%v)",
		s.id, s.isMatch, s.transitionCount, s.nfaStates)
}

// IsAccelerable returns true if this state can use SIMD acceleration.
//
// An accelerable state is one where:
//   - Most bytes (252+) loop back to self
//   - Only 1-3 bytes cause a transition to a different state
//
// This allows using memchr/memchr2/memchr3 to skip ahead in the input.
func (s *State) IsAccelerable() bool {
	return len(s.accelBytes) > 0 && len(s.accelBytes) <= 3
}

// AccelExitBytes returns the 1-3 exit bytes for an accelerable state.
// Returns nil if the state is not accelerable.
func (s *State) AccelExitBytes() []byte {
	return s.accelBytes
}

// SetAccelBytes sets the acceleration bytes for this state.
// Called during state construction when acceleration is detected.
func (s *State) SetAccelBytes(bytes []byte) {
	s.accelChecked = true
	if len(bytes) > 0 && len(bytes) <= 3 {
		s.accelBytes = make([]byte, len(bytes))
		copy(s.accelBytes, bytes)
	}
}

// AccelChecked returns true if acceleration detection has been attempted.
func (s *State) AccelChecked() bool {
	return s.accelChecked
}

// MarkAccelChecked marks that acceleration detection has been attempted.
// Call this even if no acceleration was found to avoid re-checking.
func (s *State) MarkAccelChecked() {
	s.accelChecked = true
}

// StateKey uniquely identifies a DFA state based on its NFA state set and word context.
//
// Two DFA states are equivalent if they represent the same set of NFA states
// AND have the same isFromWord value. This is critical for word boundary handling:
// states entered via word bytes vs non-word bytes are DIFFERENT states!
//
// We use a hash-based key for fast lookups in the cache.
type StateKey uint64

// ComputeStateKey computes a hash-based key for a set of NFA states.
// This version does not include word context - use ComputeStateKeyWithWord for patterns with \b/\B.
//
// The key must be consistent: the same set of NFA states (regardless of order)
// should produce the same key. We achieve this by sorting the states before hashing.
//
// This uses FNV-1a hash for speed and decent distribution.
func ComputeStateKey(nfaStates []nfa.StateID) StateKey {
	return ComputeStateKeyWithWord(nfaStates, false)
}

// ComputeStateKeyWithWord computes a hash-based key including word context.
// States with same NFA states but different isFromWord are DIFFERENT DFA states.
// This is essential for correct \b and \B handling.
func ComputeStateKeyWithWord(nfaStates []nfa.StateID, isFromWord bool) StateKey {
	if len(nfaStates) == 0 {
		if isFromWord {
			return StateKey(1) // Distinguish empty+fromWord from empty+notFromWord
		}
		return StateKey(0)
	}

	// Sort NFA states for canonical ordering
	// This ensures {1,2,3} and {3,2,1} produce the same key
	sorted := make([]nfa.StateID, len(nfaStates))
	copy(sorted, nfaStates)
	sortStateIDs(sorted)

	// Hash the sorted states using FNV-1a
	h := fnv.New64a()

	// Include isFromWord in the hash FIRST to distinguish states
	if isFromWord {
		_, _ = h.Write([]byte{1})
	} else {
		_, _ = h.Write([]byte{0})
	}

	for _, sid := range sorted {
		// Write each StateID as 4 bytes (uint32)
		// hash.Hash.Write never returns an error per documentation
		_, _ = h.Write([]byte{
			byte(sid),
			byte(sid >> 8),
			byte(sid >> 16),
			byte(sid >> 24),
		})
	}

	return StateKey(h.Sum64())
}

// sortStateIDs performs insertion sort on NFA state IDs.
//
// Insertion sort is used because:
//  1. NFA state sets are typically small (< 32 states)
//  2. Often partially sorted already (epsilon closure)
//  3. No allocations (in-place sort)
//
// For larger sets, this could be replaced with quicksort.
func sortStateIDs(states []nfa.StateID) {
	for i := 1; i < len(states); i++ {
		key := states[i]
		j := i - 1
		for j >= 0 && states[j] > key {
			states[j+1] = states[j]
			j--
		}
		states[j+1] = key
	}
}

// StateSet represents a set of NFA states used during determinization.
//
// This is a helper type for epsilon-closure computation and state transitions.
// It uses a map for fast membership testing and automatic deduplication.
type StateSet struct {
	states map[nfa.StateID]struct{}
}

// NewStateSet creates a new empty state set
func NewStateSet() *StateSet {
	return &StateSet{
		states: make(map[nfa.StateID]struct{}),
	}
}

// NewStateSetWithCapacity creates a new state set with pre-allocated capacity
func NewStateSetWithCapacity(capacity int) *StateSet {
	return &StateSet{
		states: make(map[nfa.StateID]struct{}, capacity),
	}
}

// Add adds an NFA state to the set
func (ss *StateSet) Add(state nfa.StateID) {
	ss.states[state] = struct{}{}
}

// Contains returns true if the state is in the set
func (ss *StateSet) Contains(state nfa.StateID) bool {
	_, ok := ss.states[state]
	return ok
}

// Len returns the number of states in the set
func (ss *StateSet) Len() int {
	return len(ss.states)
}

// Clear removes all states from the set (reuses map capacity)
func (ss *StateSet) Clear() {
	for k := range ss.states {
		delete(ss.states, k)
	}
}

// ToSlice returns the states as a sorted slice for consistent ordering
func (ss *StateSet) ToSlice() []nfa.StateID {
	if len(ss.states) == 0 {
		return nil
	}

	slice := make([]nfa.StateID, 0, len(ss.states))
	for state := range ss.states {
		slice = append(slice, state)
	}

	sortStateIDs(slice)
	return slice
}

// Clone creates a deep copy of the state set
func (ss *StateSet) Clone() *StateSet {
	clone := NewStateSetWithCapacity(len(ss.states))
	for state := range ss.states {
		clone.Add(state)
	}
	return clone
}
