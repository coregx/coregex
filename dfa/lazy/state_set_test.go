package lazy

import (
	"testing"

	"github.com/coregx/coregex/nfa"
)

func TestNewStateSet(t *testing.T) {
	ss := NewStateSet()
	if ss == nil {
		t.Fatal("NewStateSet returned nil")
	}
	if ss.Len() != 0 {
		t.Errorf("NewStateSet().Len() = %d, want 0", ss.Len())
	}
}

func TestNewStateSetWithCapacity(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
	}{
		{name: "small", capacity: 8},
		{name: "default", capacity: 256},
		{name: "large", capacity: 4096},
		{name: "zero uses default", capacity: 0},
		{name: "negative uses default", capacity: -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := NewStateSetWithCapacity(tt.capacity)
			if ss == nil {
				t.Fatal("NewStateSetWithCapacity returned nil")
			}
			if ss.Len() != 0 {
				t.Errorf("Len() = %d, want 0", ss.Len())
			}
		})
	}
}

func TestStateSetAddAndContains(t *testing.T) {
	ss := NewStateSet()

	// Add states
	states := []nfa.StateID{5, 10, 15, 20}
	for _, s := range states {
		ss.Add(s)
	}

	// Verify all added states are present
	for _, s := range states {
		if !ss.Contains(s) {
			t.Errorf("Contains(%d) = false after Add", s)
		}
	}

	// Verify non-added states are absent
	absent := []nfa.StateID{0, 1, 6, 11, 25}
	for _, s := range absent {
		if ss.Contains(s) {
			t.Errorf("Contains(%d) = true, want false", s)
		}
	}

	if ss.Len() != len(states) {
		t.Errorf("Len() = %d, want %d", ss.Len(), len(states))
	}
}

func TestStateSetDuplicateAdd(t *testing.T) {
	ss := NewStateSet()

	ss.Add(nfa.StateID(5))
	ss.Add(nfa.StateID(5))
	ss.Add(nfa.StateID(5))

	if ss.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after duplicate adds", ss.Len())
	}
}

func TestStateSetClear(t *testing.T) {
	ss := NewStateSet()

	// Add states
	for i := nfa.StateID(0); i < 10; i++ {
		ss.Add(i)
	}

	if ss.Len() != 10 {
		t.Fatalf("Len() = %d, want 10 before clear", ss.Len())
	}

	// Clear - should be O(1)
	ss.Clear()

	if ss.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after Clear", ss.Len())
	}

	// Previously added states should not be found
	for i := nfa.StateID(0); i < 10; i++ {
		if ss.Contains(i) {
			t.Errorf("Contains(%d) = true after Clear", i)
		}
	}

	// Can add states again after clear
	ss.Add(nfa.StateID(42))
	if !ss.Contains(nfa.StateID(42)) {
		t.Error("Contains(42) = false after re-Add post Clear")
	}
	if ss.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after re-Add", ss.Len())
	}
}

func TestStateSetGrow(t *testing.T) {
	// Start with small capacity
	ss := NewStateSetWithCapacity(4)

	// Add states that exceed initial capacity
	for i := nfa.StateID(0); i < 100; i++ {
		ss.Add(i)
	}

	// Verify all states are present
	if ss.Len() != 100 {
		t.Errorf("Len() = %d, want 100", ss.Len())
	}

	for i := nfa.StateID(0); i < 100; i++ {
		if !ss.Contains(i) {
			t.Errorf("Contains(%d) = false after grow", i)
		}
	}
}

func TestStateSetGrowLargeStateID(t *testing.T) {
	ss := NewStateSetWithCapacity(8)

	// Add a state with an ID much larger than initial capacity
	largeID := nfa.StateID(1000)
	ss.Add(largeID)

	if !ss.Contains(largeID) {
		t.Error("Contains(1000) = false after Add with large ID")
	}
	if ss.Len() != 1 {
		t.Errorf("Len() = %d, want 1", ss.Len())
	}

	// Verify smaller IDs are not falsely present
	if ss.Contains(nfa.StateID(0)) {
		t.Error("Contains(0) should be false")
	}
}

func TestStateSetToSlice(t *testing.T) {
	ss := NewStateSet()

	// Empty set
	if slice := ss.ToSlice(); slice != nil {
		t.Errorf("ToSlice() on empty set = %v, want nil", slice)
	}

	// Add states in non-sorted order
	ss.Add(nfa.StateID(10))
	ss.Add(nfa.StateID(5))
	ss.Add(nfa.StateID(15))
	ss.Add(nfa.StateID(1))

	slice := ss.ToSlice()
	if len(slice) != 4 {
		t.Fatalf("ToSlice() length = %d, want 4", len(slice))
	}

	// ToSlice returns sorted order
	expected := []nfa.StateID{1, 5, 10, 15}
	for i, want := range expected {
		if slice[i] != want {
			t.Errorf("ToSlice()[%d] = %d, want %d", i, slice[i], want)
		}
	}
}

func TestStateSetToSliceIndependence(t *testing.T) {
	ss := NewStateSet()
	ss.Add(nfa.StateID(1))
	ss.Add(nfa.StateID(2))

	slice := ss.ToSlice()

	// Modifying slice should not affect StateSet
	slice[0] = nfa.StateID(999)

	if !ss.Contains(nfa.StateID(1)) {
		t.Error("Modifying ToSlice result should not affect StateSet")
	}
}

func TestStateSetClone(t *testing.T) {
	ss := NewStateSet()
	ss.Add(nfa.StateID(1))
	ss.Add(nfa.StateID(5))
	ss.Add(nfa.StateID(10))

	clone := ss.Clone()

	// Clone should have same contents
	if clone.Len() != ss.Len() {
		t.Errorf("Clone.Len() = %d, want %d", clone.Len(), ss.Len())
	}

	for _, id := range []nfa.StateID{1, 5, 10} {
		if !clone.Contains(id) {
			t.Errorf("Clone.Contains(%d) = false", id)
		}
	}
}

func TestStateSetCloneIndependence(t *testing.T) {
	ss := NewStateSet()
	ss.Add(nfa.StateID(1))
	ss.Add(nfa.StateID(5))

	clone := ss.Clone()

	// Modify original
	ss.Add(nfa.StateID(99))
	ss.Clear()

	// Clone should be unaffected
	if clone.Len() != 2 {
		t.Errorf("Clone.Len() = %d, want 2 after modifying original", clone.Len())
	}
	if !clone.Contains(nfa.StateID(1)) {
		t.Error("Clone.Contains(1) = false after clearing original")
	}
	if !clone.Contains(nfa.StateID(5)) {
		t.Error("Clone.Contains(5) = false after clearing original")
	}
	if clone.Contains(nfa.StateID(99)) {
		t.Error("Clone should not contain state added to original after cloning")
	}
}

func TestStateSetCloneEmpty(t *testing.T) {
	ss := NewStateSet()
	clone := ss.Clone()

	if clone.Len() != 0 {
		t.Errorf("Clone of empty set has Len() = %d, want 0", clone.Len())
	}

	// Adding to clone should not affect original
	clone.Add(nfa.StateID(42))
	if ss.Len() != 0 {
		t.Error("Adding to clone should not affect original")
	}
}

func TestStateSetContainsOutOfBounds(t *testing.T) {
	ss := NewStateSetWithCapacity(8)
	ss.Add(nfa.StateID(1))

	// Querying state beyond capacity should return false (not panic)
	if ss.Contains(nfa.StateID(1000)) {
		t.Error("Contains(1000) should be false for small capacity set")
	}
}

func TestComputeStateKey(t *testing.T) {
	// Same NFA states should produce same key
	key1 := ComputeStateKey([]nfa.StateID{1, 2, 3})
	key2 := ComputeStateKey([]nfa.StateID{1, 2, 3})
	if key1 != key2 {
		t.Errorf("Same states should produce same key: %d vs %d", key1, key2)
	}

	// Order should not matter (sorted internally)
	key3 := ComputeStateKey([]nfa.StateID{3, 1, 2})
	if key1 != key3 {
		t.Errorf("Different order should produce same key: %d vs %d", key1, key3)
	}

	// Different states should produce different keys
	key4 := ComputeStateKey([]nfa.StateID{4, 5, 6})
	if key1 == key4 {
		t.Errorf("Different states should produce different key: both %d", key1)
	}

	// Empty states
	keyEmpty := ComputeStateKey([]nfa.StateID{})
	if keyEmpty != StateKey(0) {
		t.Errorf("Empty states key = %d, want 0", keyEmpty)
	}
}

func TestComputeStateKeyWithWord(t *testing.T) {
	nfaStates := []nfa.StateID{1, 2, 3}

	// Same states with different word context should produce different keys
	keyWord := ComputeStateKeyWithWord(nfaStates, true)
	keyNonWord := ComputeStateKeyWithWord(nfaStates, false)
	if keyWord == keyNonWord {
		t.Errorf("Word and non-word keys should differ: both %d", keyWord)
	}

	// Empty states with word context
	keyEmptyWord := ComputeStateKeyWithWord([]nfa.StateID{}, true)
	keyEmptyNonWord := ComputeStateKeyWithWord([]nfa.StateID{}, false)
	if keyEmptyWord == keyEmptyNonWord {
		t.Errorf("Empty word and non-word keys should differ: %d vs %d", keyEmptyWord, keyEmptyNonWord)
	}
	if keyEmptyNonWord != StateKey(0) {
		t.Errorf("Empty non-word key = %d, want 0", keyEmptyNonWord)
	}
	if keyEmptyWord != StateKey(1) {
		t.Errorf("Empty word key = %d, want 1", keyEmptyWord)
	}
}

func TestStateCreation(t *testing.T) {
	tests := []struct {
		name       string
		id         StateID
		nfaStates  []nfa.StateID
		isMatch    bool
		isFromWord bool
	}{
		{
			name:       "basic non-match state",
			id:         StateID(1),
			nfaStates:  []nfa.StateID{0, 1, 2},
			isMatch:    false,
			isFromWord: false,
		},
		{
			name:       "match state",
			id:         StateID(5),
			nfaStates:  []nfa.StateID{3, 4},
			isMatch:    true,
			isFromWord: false,
		},
		{
			name:       "word context state",
			id:         StateID(10),
			nfaStates:  []nfa.StateID{1},
			isMatch:    false,
			isFromWord: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var state *State
			if tt.isFromWord {
				state = NewStateWithWordContext(tt.id, tt.nfaStates, tt.isMatch, tt.isFromWord)
			} else {
				state = NewState(tt.id, tt.nfaStates, tt.isMatch)
			}

			if state.ID() != tt.id {
				t.Errorf("ID() = %d, want %d", state.ID(), tt.id)
			}
			if state.IsMatch() != tt.isMatch {
				t.Errorf("IsMatch() = %v, want %v", state.IsMatch(), tt.isMatch)
			}
			if state.IsFromWord() != tt.isFromWord {
				t.Errorf("IsFromWord() = %v, want %v", state.IsFromWord(), tt.isFromWord)
			}

			// NFA states should be a copy
			gotNFA := state.NFAStates()
			if len(gotNFA) != len(tt.nfaStates) {
				t.Errorf("NFAStates() length = %d, want %d", len(gotNFA), len(tt.nfaStates))
			}
		})
	}
}

func TestStateTransitions(t *testing.T) {
	state := NewState(StateID(1), []nfa.StateID{0}, false)

	// Initially no valid transitions
	if state.TransitionCount() != 0 {
		t.Errorf("Initial TransitionCount() = %d, want 0", state.TransitionCount())
	}

	// All transitions should be InvalidState
	for i := 0; i < 256; i++ {
		next, ok := state.Transition(byte(i))
		if ok {
			t.Errorf("Transition(%d) should be invalid, got %d", i, next)
		}
		if next != InvalidState {
			t.Errorf("Transition(%d) = %d, want InvalidState", i, next)
		}
	}

	// Add some transitions
	state.AddTransition(byte('a'), StateID(2))
	state.AddTransition(byte('b'), StateID(3))

	if state.TransitionCount() != 2 {
		t.Errorf("TransitionCount() = %d, want 2", state.TransitionCount())
	}

	next, ok := state.Transition(byte('a'))
	if !ok || next != StateID(2) {
		t.Errorf("Transition('a') = (%d, %v), want (2, true)", next, ok)
	}

	next, ok = state.Transition(byte('b'))
	if !ok || next != StateID(3) {
		t.Errorf("Transition('b') = (%d, %v), want (3, true)", next, ok)
	}

	// Overwrite transition
	state.AddTransition(byte('a'), StateID(5))
	next, ok = state.Transition(byte('a'))
	if !ok || next != StateID(5) {
		t.Errorf("After overwrite, Transition('a') = (%d, %v), want (5, true)", next, ok)
	}
	if state.TransitionCount() != 2 {
		t.Errorf("TransitionCount() = %d, want 2 after overwrite", state.TransitionCount())
	}

	// Remove transition by setting to InvalidState
	state.AddTransition(byte('a'), InvalidState)
	_, ok = state.Transition(byte('a'))
	if ok {
		t.Error("Transition('a') should be invalid after removal")
	}
	if state.TransitionCount() != 1 {
		t.Errorf("TransitionCount() = %d, want 1 after removal", state.TransitionCount())
	}
}

func TestStateTransitionOutOfBounds(t *testing.T) {
	// Create state with small stride
	state := NewStateWithStride(StateID(1), []nfa.StateID{0}, false, false, 4)

	// Transition beyond stride should return InvalidState
	next, ok := state.Transition(byte(10))
	if ok {
		t.Errorf("Transition beyond stride should be invalid, got %d", next)
	}

	// AddTransition beyond stride should be ignored (no panic)
	state.AddTransition(byte(10), StateID(5))
	if state.TransitionCount() != 0 {
		t.Error("AddTransition beyond stride should be ignored")
	}
}

func TestStateStride(t *testing.T) {
	tests := []struct {
		name   string
		stride int
	}{
		{name: "default", stride: 256},
		{name: "small", stride: 4},
		{name: "medium", stride: 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewStateWithStride(StateID(1), []nfa.StateID{0}, false, false, tt.stride)
			if state.Stride() != tt.stride {
				t.Errorf("Stride() = %d, want %d", state.Stride(), tt.stride)
			}
		})
	}
}

func TestStateString(t *testing.T) {
	state := NewState(StateID(5), []nfa.StateID{1, 2, 3}, true)
	s := state.String()

	// Should contain key information
	if s == "" {
		t.Error("String() returned empty string")
	}
	// Just verify it doesn't panic and contains the ID
	if len(s) < 10 {
		t.Errorf("String() unexpectedly short: %q", s)
	}
}

func TestStateNFAStatesCopy(t *testing.T) {
	original := []nfa.StateID{1, 2, 3}
	state := NewState(StateID(1), original, false)

	// Modify original - should not affect state
	original[0] = 999

	got := state.NFAStates()
	if got[0] == 999 {
		t.Error("NewState should copy NFA states, not alias them")
	}
}
