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

// State represents a DFA state with its transitions.
//
// A DFA state is deterministic: for each input byte, there is at most one
// target state. This is represented as a map from byte → StateID.
//
// Memory layout is optimized for cache efficiency:
//   - Small states (few transitions) use a map
//   - Dense states (many transitions) could use a 256-element array (future optimization)
type State struct {
	// id uniquely identifies this state in the cache
	id StateID

	// transitions maps input byte → next state ID
	// For a fully determinized state, this contains all relevant transitions
	transitions map[byte]StateID

	// isMatch indicates if this is an accepting state
	isMatch bool

	// nfaStates is the set of NFA states this DFA state represents.
	// This is used during determinization to compute transitions.
	// Pre-allocated to avoid heap allocations during search.
	nfaStates []nfa.StateID
}

// NewState creates a new DFA state with the given ID and NFA state set
func NewState(id StateID, nfaStates []nfa.StateID, isMatch bool) *State {
	// Pre-allocate transitions map with reasonable capacity
	// Most states have few transitions (< 16)
	transitions := make(map[byte]StateID, 16)

	// Copy NFA states to avoid aliasing
	nfaStatesCopy := make([]nfa.StateID, len(nfaStates))
	copy(nfaStatesCopy, nfaStates)

	return &State{
		id:          id,
		transitions: transitions,
		isMatch:     isMatch,
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

// Transition returns the next state for the given input byte.
// Returns (InvalidState, false) if no transition exists.
func (s *State) Transition(b byte) (StateID, bool) {
	next, ok := s.transitions[b]
	return next, ok
}

// AddTransition adds a transition from this state to another on input byte b.
// Overwrites any existing transition for this byte.
func (s *State) AddTransition(b byte, next StateID) {
	s.transitions[b] = next
}

// NFAStates returns the NFA states represented by this DFA state
func (s *State) NFAStates() []nfa.StateID {
	return s.nfaStates
}

// TransitionCount returns the number of transitions from this state
func (s *State) TransitionCount() int {
	return len(s.transitions)
}

// String returns a human-readable representation of the state
func (s *State) String() string {
	return fmt.Sprintf("DFAState(id=%d, isMatch=%v, transitions=%d, nfaStates=%v)",
		s.id, s.isMatch, len(s.transitions), s.nfaStates)
}

// StateKey uniquely identifies a DFA state based on its NFA state set.
//
// Two DFA states are equivalent if they represent the same set of NFA states.
// We use a hash-based key for fast lookups in the cache.
type StateKey uint64

// ComputeStateKey computes a hash-based key for a set of NFA states.
//
// The key must be consistent: the same set of NFA states (regardless of order)
// should produce the same key. We achieve this by sorting the states before hashing.
//
// This uses FNV-1a hash for speed and decent distribution.
func ComputeStateKey(nfaStates []nfa.StateID) StateKey {
	if len(nfaStates) == 0 {
		return StateKey(0)
	}

	// Sort NFA states for canonical ordering
	// This ensures {1,2,3} and {3,2,1} produce the same key
	sorted := make([]nfa.StateID, len(nfaStates))
	copy(sorted, nfaStates)
	sortStateIDs(sorted)

	// Hash the sorted states using FNV-1a
	h := fnv.New64a()
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
