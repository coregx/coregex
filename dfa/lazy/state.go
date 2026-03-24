package lazy

import (
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/coregx/coregex/nfa"
)

// StateID uniquely identifies a DFA state in the cache.
//
// The ID is a **premultiplied byte offset** into the flat transition table,
// with tag bits in the high 5 bits for O(1) special state detection.
//
// Layout (Rust LazyStateID approach, hybrid/id.rs:169):
//
//	[invalid|dead|reserved|start|match| 27 bits: offset into flatTrans ]
//	 bit 31  30    29       28    27    bits 0-26
//
// Hot loop: nextSID = flatTrans[sid & TagMask + classIdx]
//
//	if sid > TagMask { handle special }
//
// No multiply needed — sid already contains the byte offset.
type StateID uint32

// Tag bit masks for StateID high bits.
const (
	tagInvalid  StateID = 1 << 31 // Unknown/not yet computed transition
	tagDead     StateID = 1 << 30 // Dead state — no match possible
	tagReserved StateID = 1 << 29 // Reserved for quit
	tagStart    StateID = 1 << 28 // Start state
	tagMatch    StateID = 1 << 27 // Match/accepting state

	// TagMask extracts the offset (lower 27 bits).
	// Any bit above this = special state requiring slow path.
	TagMask StateID = tagMatch - 1 // 0x07FFFFFF

	// MaxStateOffset is the maximum premultiplied offset (128M entries).
	MaxStateOffset StateID = TagMask
)

// Special state constants (tagged, premultiplied offset = 0).
const (
	// InvalidState represents an unknown/uninitialized transition.
	// In flatTrans, this means the transition hasn't been computed yet.
	InvalidState StateID = tagInvalid // 0x80000000

	// DeadState represents a dead/failure state — no match possible.
	DeadState StateID = tagDead // 0x40000000

	// StartState is the initial state. Offset 0, tagged as start.
	StartState StateID = 0
)

// IsTagged returns true if any tag bit is set (special state).
// This is the single branch in the DFA hot loop.
//
//go:nosplit
func (sid StateID) IsTagged() bool {
	return sid > TagMask
}

// Offset returns the premultiplied byte offset into flatTrans.
// Strips tag bits. Only valid for non-special states.
//
//go:nosplit
func (sid StateID) Offset() int {
	return int(sid & TagMask)
}

// IsMatch returns true if this state has the match tag.
//
//go:nosplit
func (sid StateID) IsMatchTag() bool {
	return sid&tagMatch != 0
}

// IsDeadTag returns true if this state has the dead tag.
//
//go:nosplit
func (sid StateID) IsDeadTag() bool {
	return sid&tagDead != 0
}

// IsInvalidTag returns true if this state has the invalid tag.
//
//go:nosplit
func (sid StateID) IsInvalidTag() bool {
	return sid&tagInvalid != 0
}

// WithMatchTag returns a copy of this StateID with the match tag set.
func (sid StateID) WithMatchTag() StateID {
	return sid | tagMatch
}

// WithStartTag returns a copy of this StateID with the start tag set.
func (sid StateID) WithStartTag() StateID {
	return sid | tagStart
}

// defaultStride is the default alphabet size when ByteClasses compression is not used.
const defaultStride = 256

// State represents a DFA state with its transitions.
//
// A DFA state is deterministic: for each input byte, there is at most one
// target state. Uses a dynamically-sized transitions slice based on ByteClasses
// alphabet reduction for memory efficiency.
//
// Memory with ByteClasses compression:
//   - Pattern "hello": ~7 classes * 4 bytes = 28 bytes/state (vs 1KB without compression)
//   - Pattern "[a-z]+": ~4 classes * 4 bytes = 16 bytes/state
//   - Complex patterns: typically 8-64 classes = 32-256 bytes/state
//
// The lookup is still O(1): transitions[byteClasses.Get(byte)]
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

	// Note: transitions removed — stored in DFACache.flatTrans only.

	// isMatch indicates if this is an accepting state
	isMatch bool

	// isFromWord indicates if this state was entered via a word byte transition.
	// Used for word boundary (\b, \B) assertion evaluation.
	// At start of input, this is false (no previous byte = non-word).
	isFromWord bool

	// matchAtWordBoundary is pre-computed during determinize:
	// true if resolving \b assertions in this state's NFA states (when word
	// boundary IS satisfied) would produce a match. This eliminates the
	// expensive per-byte checkWordBoundaryMatch (30% CPU on \b patterns).
	matchAtWordBoundary bool

	// matchAtNonWordBoundary is the same but for when word boundary is NOT satisfied.
	matchAtNonWordBoundary bool

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
// Uses default stride of 256 (no ByteClasses compression).
// isFromWord indicates if this state was entered via a word byte (for \b/\B handling).
func NewState(id StateID, nfaStates []nfa.StateID, isMatch bool) *State {
	return NewStateWithStride(id, nfaStates, isMatch, false, defaultStride)
}

// NewStateWithWordContext creates a new DFA state with explicit word context.
// Uses default stride of 256 (no ByteClasses compression).
// isFromWord indicates if this state was entered via a word byte transition.
// This is essential for correct word boundary (\b, \B) handling in DFA.
func NewStateWithWordContext(id StateID, nfaStates []nfa.StateID, isMatch bool, isFromWord bool) *State {
	return NewStateWithStride(id, nfaStates, isMatch, isFromWord, defaultStride)
}

// NewStateWithStride creates a new DFA state with explicit stride (alphabet size).
// The stride determines the transitions slice size. Use ByteClasses.AlphabetLen()
// for memory-efficient states with alphabet compression.
//
// Parameters:
//   - id: unique state identifier
//   - nfaStates: set of NFA states this DFA state represents
//   - isMatch: true if this is an accepting state
//   - isFromWord: true if entered via a word byte transition (for \b/\B)
//   - stride: alphabet size (transitions slice length)
func NewStateWithStride(id StateID, nfaStates []nfa.StateID, isMatch bool, isFromWord bool, stride int) *State {
	// Copy NFA states to avoid aliasing
	nfaStatesCopy := make([]nfa.StateID, len(nfaStates))
	copy(nfaStatesCopy, nfaStates)

	// Note: transitions stored in DFACache.flatTrans (single source of truth).
	// State struct keeps only metadata.
	return &State{
		id:         id,
		isMatch:    isMatch,
		isFromWord: isFromWord,
		nfaStates:  nfaStatesCopy,
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

// checkWordBoundaryFast checks if consuming byte b would produce a match
// via word boundary resolution. Uses pre-computed flags — O(1), no allocation.
// Replaces the expensive checkWordBoundaryMatch (30% CPU) which created Builder
// and resolved word boundaries per byte.
func (s *State) checkWordBoundaryFast(b byte) bool {
	if s.isMatch {
		return false // Already a match — let normal processing handle it
	}
	isBoundary := s.isFromWord != isWordByte(b)
	if isBoundary {
		return s.matchAtWordBoundary
	}
	return s.matchAtNonWordBoundary
}

// NFAStates returns the NFA states represented by this DFA state
func (s *State) NFAStates() []nfa.StateID {
	return s.nfaStates
}

// String returns a human-readable representation of the state
func (s *State) String() string {
	return fmt.Sprintf("DFAState(id=%d, isMatch=%v, nfaStates=%v)",
		s.id, s.isMatch, s.nfaStates)
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
// Uses a sparse set internally for O(1) clear, membership testing, and insertion.
// This is a major performance improvement over map-based implementation where
// Clear() was O(n) - now it's O(1) by simply resetting the size counter.
//
// The sparse set also preserves insertion order, which enables deterministic
// DFA construction and consistent state keys.
type StateSet struct {
	sparse []uint32      // Maps state -> index in dense
	dense  []nfa.StateID // Stores states in insertion order
	size   int           // Number of elements
}

// defaultStateSetCapacity is the initial capacity for state sets.
// Most NFA patterns have < 256 states, so this is a reasonable default.
const defaultStateSetCapacity = 256

// NewStateSet creates a new empty state set
func NewStateSet() *StateSet {
	return NewStateSetWithCapacity(defaultStateSetCapacity)
}

// NewStateSetWithCapacity creates a new state set with pre-allocated capacity
func NewStateSetWithCapacity(capacity int) *StateSet {
	if capacity <= 0 {
		capacity = defaultStateSetCapacity
	}
	return &StateSet{
		sparse: make([]uint32, capacity),
		dense:  make([]nfa.StateID, capacity),
		size:   0,
	}
}

// Add adds an NFA state to the set
func (ss *StateSet) Add(state nfa.StateID) {
	// Grow if needed
	if int(state) >= len(ss.sparse) {
		ss.grow(int(state) + 1)
	}
	if ss.Contains(state) {
		return
	}
	// Direct assignment (O(1))
	ss.dense[ss.size] = state
	ss.sparse[state] = uint32(ss.size)
	ss.size++
}

// grow expands the capacity to at least newCap
func (ss *StateSet) grow(newCap int) {
	if newCap <= len(ss.sparse) {
		return
	}
	// Double the capacity or use newCap, whichever is larger
	targetCap := len(ss.sparse) * 2
	if targetCap < newCap {
		targetCap = newCap
	}
	// Reallocate arrays
	newSparse := make([]uint32, targetCap)
	newDense := make([]nfa.StateID, targetCap)
	copy(newSparse, ss.sparse)
	copy(newDense[:ss.size], ss.dense[:ss.size])
	ss.sparse = newSparse
	ss.dense = newDense
}

// Contains returns true if the state is in the set
func (ss *StateSet) Contains(state nfa.StateID) bool {
	if int(state) >= len(ss.sparse) {
		return false
	}
	idx := ss.sparse[state]
	// Cross-validation: sparse[state] must point to valid dense index
	// AND dense[idx] must equal state (handles garbage in sparse)
	return int(idx) < ss.size && ss.dense[idx] == state
}

// Len returns the number of states in the set
func (ss *StateSet) Len() int {
	return ss.size
}

// Clear removes all states from the set in O(1) time.
// This is the key advantage of sparse sets over maps.
func (ss *StateSet) Clear() {
	ss.size = 0
}

// ToSlice returns the states as a sorted slice for consistent ordering
func (ss *StateSet) ToSlice() []nfa.StateID {
	if ss.size == 0 {
		return nil
	}
	// Copy to new slice (dense[:size] is valid)
	slice := make([]nfa.StateID, ss.size)
	copy(slice, ss.dense[:ss.size])
	sortStateIDs(slice)
	return slice
}

// Clone creates a deep copy of the state set
func (ss *StateSet) Clone() *StateSet {
	clone := NewStateSetWithCapacity(len(ss.sparse))
	copy(clone.sparse, ss.sparse)
	copy(clone.dense[:ss.size], ss.dense[:ss.size])
	clone.size = ss.size
	return clone
}

// stateSetPool is a pool of reusable StateSet objects to reduce allocations.
// With sparse sets, Clear() is O(1), so pooling is even more effective.
var stateSetPool = sync.Pool{
	New: func() interface{} {
		return NewStateSetWithCapacity(defaultStateSetCapacity)
	},
}

// acquireStateSet gets a StateSet from the pool, cleared and ready for use.
// The returned StateSet should be released back to the pool via releaseStateSet.
func acquireStateSet() *StateSet {
	ss := stateSetPool.Get().(*StateSet)
	ss.Clear() // O(1) with sparse set!
	return ss
}

// releaseStateSet returns a StateSet to the pool for reuse.
// The StateSet should not be used after calling this function.
func releaseStateSet(ss *StateSet) {
	if ss == nil {
		return
	}
	// Don't pool very large sets to avoid memory bloat
	if len(ss.sparse) > 4096 {
		return
	}
	stateSetPool.Put(ss)
}
