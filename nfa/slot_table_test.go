package nfa

import (
	"testing"
)

// TestSlotTable_New verifies SlotTable creation
func TestSlotTable_New(t *testing.T) {
	tests := []struct {
		name          string
		numStates     int
		slotsPerState int
		wantNil       bool
	}{
		{
			name:          "normal creation",
			numStates:     10,
			slotsPerState: 4,
			wantNil:       false,
		},
		{
			name:          "zero states",
			numStates:     0,
			slotsPerState: 4,
			wantNil:       true,
		},
		{
			name:          "negative states",
			numStates:     -1,
			slotsPerState: 4,
			wantNil:       true,
		},
		{
			name:          "zero slots",
			numStates:     10,
			slotsPerState: 0,
			wantNil:       true,
		},
		{
			name:          "single state single slot",
			numStates:     1,
			slotsPerState: 2,
			wantNil:       false,
		},
		{
			name:          "large table",
			numStates:     1000,
			slotsPerState: 20,
			wantNil:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := NewSlotTable(tt.numStates, tt.slotsPerState)
			if tt.wantNil {
				if st.table != nil {
					t.Error("expected nil table")
				}
				return
			}

			// Non-nil table validation
			if st.table == nil {
				t.Error("expected non-nil table")
				return
			}

			// Verify initialization
			expectedSize := tt.numStates*tt.slotsPerState + tt.slotsPerState
			if len(st.table) != expectedSize {
				t.Errorf("wrong table size: got %d, want %d", len(st.table), expectedSize)
			}

			// Verify all initialized to -1
			for i, v := range st.table {
				if v != -1 {
					t.Errorf("slot %d not initialized to -1: got %d", i, v)
					break
				}
			}
		})
	}
}

// TestSlotTable_ForState verifies state slot access
func TestSlotTable_ForState(t *testing.T) {
	st := NewSlotTable(10, 4)

	tests := []struct {
		name      string
		stateID   StateID
		wantLen   int
		wantPanic bool
	}{
		{
			name:    "valid state 0",
			stateID: 0,
			wantLen: 4,
		},
		{
			name:    "valid state 5",
			stateID: 5,
			wantLen: 4,
		},
		{
			name:    "valid last state",
			stateID: 9,
			wantLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slots := st.ForState(tt.stateID)
			if len(slots) != tt.wantLen {
				t.Errorf("wrong slot length: got %d, want %d", len(slots), tt.wantLen)
			}
		})
	}
}

// TestSlotTable_SetActiveSlots verifies dynamic slot sizing
func TestSlotTable_SetActiveSlots(t *testing.T) {
	st := NewSlotTable(10, 6) // 6 slots = 3 capture groups

	tests := []struct {
		name       string
		active     int
		wantActive int
	}{
		{
			name:       "full slots",
			active:     6,
			wantActive: 6,
		},
		{
			name:       "zero slots (IsMatch mode)",
			active:     0,
			wantActive: 0,
		},
		{
			name:       "two slots (Find mode)",
			active:     2,
			wantActive: 2,
		},
		{
			name:       "clamp to max",
			active:     100,
			wantActive: 6,
		},
		{
			name:       "negative clamped to zero",
			active:     -5,
			wantActive: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st.SetActiveSlots(tt.active)
			if st.ActiveSlots() != tt.wantActive {
				t.Errorf("wrong active slots: got %d, want %d", st.ActiveSlots(), tt.wantActive)
			}

			// Verify ForState respects active slots
			if tt.wantActive == 0 {
				slots := st.ForState(0)
				if slots != nil {
					t.Error("expected nil slots when activeSlots=0")
				}
			} else {
				slots := st.ForState(0)
				if len(slots) != tt.wantActive {
					t.Errorf("ForState length wrong: got %d, want %d", len(slots), tt.wantActive)
				}
			}
		})
	}
}

// TestSlotTable_CopySlots verifies slot copying between states
func TestSlotTable_CopySlots(t *testing.T) {
	st := NewSlotTable(10, 4)

	// Set up source state
	srcSlots := st.ForState(3)
	srcSlots[0] = 10
	srcSlots[1] = 20
	srcSlots[2] = 30
	srcSlots[3] = 40

	// Copy to destination
	st.CopySlots(5, 3)

	// Verify copy
	dstSlots := st.ForState(5)
	for i := 0; i < 4; i++ {
		if dstSlots[i] != srcSlots[i] {
			t.Errorf("slot %d mismatch: got %d, want %d", i, dstSlots[i], srcSlots[i])
		}
	}

	// Verify independence (modify src, dst should not change)
	srcSlots[0] = 999
	if dstSlots[0] == 999 {
		t.Error("dst slots should be independent of src after copy")
	}
}

// TestSlotTable_CopySlots_ActiveSlots verifies copy respects active slots
func TestSlotTable_CopySlots_ActiveSlots(t *testing.T) {
	st := NewSlotTable(10, 6)

	// Set all slots in source
	srcSlots := st.ForState(0)
	for i := range srcSlots {
		srcSlots[i] = i * 10
	}

	// Set active slots to 2 (Find mode)
	st.SetActiveSlots(2)

	// Copy - should only copy 2 slots
	st.CopySlots(1, 0)

	// Verify only active slots were copied
	st.SetActiveSlots(6) // restore to check full state
	dstSlots := st.ForState(1)

	if dstSlots[0] != 0 || dstSlots[1] != 10 {
		t.Errorf("active slots not copied correctly: got %v", dstSlots[:2])
	}
	// Slots 2-5 should still be -1 (not copied)
	for i := 2; i < 6; i++ {
		if dstSlots[i] != -1 {
			t.Errorf("slot %d should be -1, got %d", i, dstSlots[i])
		}
	}
}

// TestSlotTable_SetSlot_GetSlot verifies individual slot access
func TestSlotTable_SetSlot_GetSlot(t *testing.T) {
	st := NewSlotTable(10, 4)

	tests := []struct {
		name      string
		stateID   StateID
		slotIndex int
		value     int
		wantGet   int
	}{
		{
			name:      "valid set/get",
			stateID:   3,
			slotIndex: 2,
			value:     42,
			wantGet:   42,
		},
		{
			name:      "negative index",
			stateID:   0,
			slotIndex: -1,
			value:     99,
			wantGet:   -1, // should fail, return -1
		},
		{
			name:      "index out of bounds",
			stateID:   0,
			slotIndex: 100,
			value:     99,
			wantGet:   -1, // should fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st.Reset() // start fresh
			st.SetSlot(tt.stateID, tt.slotIndex, tt.value)
			got := st.GetSlot(tt.stateID, tt.slotIndex)
			if got != tt.wantGet {
				t.Errorf("GetSlot: got %d, want %d", got, tt.wantGet)
			}
		})
	}
}

// TestSlotTable_Reset verifies table reset
func TestSlotTable_Reset(t *testing.T) {
	st := NewSlotTable(10, 4)

	// Set some values
	st.SetSlot(0, 0, 100)
	st.SetSlot(5, 2, 200)
	st.SetSlot(9, 3, 300)

	// Reset
	st.Reset()

	// Verify all are -1
	for sid := StateID(0); sid < 10; sid++ {
		for i := 0; i < 4; i++ {
			if v := st.GetSlot(sid, i); v != -1 {
				t.Errorf("state %d slot %d not reset: got %d", sid, i, v)
			}
		}
	}
}

// TestSlotTable_ResetState verifies single state reset
func TestSlotTable_ResetState(t *testing.T) {
	st := NewSlotTable(10, 4)

	// Set values in multiple states
	st.SetSlot(0, 0, 100)
	st.SetSlot(1, 1, 200)
	st.SetSlot(2, 2, 300)

	// Reset only state 1
	st.ResetState(1)

	// State 0 should be unchanged
	if v := st.GetSlot(0, 0); v != 100 {
		t.Errorf("state 0 was affected: got %d, want 100", v)
	}

	// State 1 should be reset
	if v := st.GetSlot(1, 1); v != -1 {
		t.Errorf("state 1 not reset: got %d, want -1", v)
	}

	// State 2 should be unchanged
	if v := st.GetSlot(2, 2); v != 300 {
		t.Errorf("state 2 was affected: got %d, want 300", v)
	}
}

// TestSlotTable_AllAbsent verifies scratch space access
func TestSlotTable_AllAbsent(t *testing.T) {
	st := NewSlotTable(10, 4)

	// Set some state slots to non-absent values
	st.SetSlot(0, 0, 100)
	st.SetSlot(1, 0, 200)

	// AllAbsent should always return -1s
	absent := st.AllAbsent()
	if len(absent) != 4 {
		t.Errorf("AllAbsent length: got %d, want 4", len(absent))
	}
	for i, v := range absent {
		if v != -1 {
			t.Errorf("AllAbsent slot %d not -1: got %d", i, v)
		}
	}

	// Verify AllAbsent respects active slots
	st.SetActiveSlots(2)
	absent = st.AllAbsent()
	if len(absent) != 2 {
		t.Errorf("AllAbsent with active=2: got len %d, want 2", len(absent))
	}
}

// TestSlotTable_Clone verifies deep copy
func TestSlotTable_Clone(t *testing.T) {
	st := NewSlotTable(10, 4)
	st.SetSlot(5, 2, 42)
	st.SetActiveSlots(2)

	clone := st.Clone()

	// Verify same values
	if clone.numStates != st.numStates {
		t.Errorf("numStates mismatch: got %d, want %d", clone.numStates, st.numStates)
	}
	if clone.slotsPerState != st.slotsPerState {
		t.Errorf("slotsPerState mismatch: got %d, want %d", clone.slotsPerState, st.slotsPerState)
	}
	if clone.activeSlots != st.activeSlots {
		t.Errorf("activeSlots mismatch: got %d, want %d", clone.activeSlots, st.activeSlots)
	}

	// Verify independence
	clone.SetSlot(5, 2, 999)
	clone.SetActiveSlots(4)

	// Original should be unchanged
	st.SetActiveSlots(4) // restore to read full state
	if st.GetSlot(5, 2) != 42 {
		t.Error("original modified after clone change")
	}
	st.SetActiveSlots(2)
	if st.ActiveSlots() != 2 {
		t.Error("original activeSlots changed")
	}
}

// TestSlotTable_ExtractCaptures verifies capture extraction
func TestSlotTable_ExtractCaptures(t *testing.T) {
	// 3 capture groups = 6 slots
	st := NewSlotTable(10, 6)

	// Set up captures for state 3:
	// Group 0: (handled separately via matchStart/matchEnd)
	// Group 1: positions 5-10
	// Group 2: not captured (-1)
	slots := st.ForState(3)
	slots[0] = 0  // group 0 start (ignored, use matchStart)
	slots[1] = 15 // group 0 end (ignored, use matchEnd)
	slots[2] = 5  // group 1 start
	slots[3] = 10 // group 1 end
	slots[4] = -1 // group 2 start (not captured)
	slots[5] = -1 // group 2 end (not captured)

	captures := st.ExtractCaptures(3, 0, 15)

	if len(captures) != 3 {
		t.Fatalf("wrong number of groups: got %d, want 3", len(captures))
	}

	// Group 0 should be match bounds
	if captures[0][0] != 0 || captures[0][1] != 15 {
		t.Errorf("group 0 wrong: got %v, want [0 15]", captures[0])
	}

	// Group 1 should be from slots
	if captures[1][0] != 5 || captures[1][1] != 10 {
		t.Errorf("group 1 wrong: got %v, want [5 10]", captures[1])
	}

	// Group 2 should be nil (not captured)
	if captures[2] != nil {
		t.Errorf("group 2 should be nil: got %v", captures[2])
	}
}

// TestSlotTable_MemoryUsage verifies memory calculation
func TestSlotTable_MemoryUsage(t *testing.T) {
	st := NewSlotTable(100, 10)

	// Expected: (100 * 10 + 10) * 8 = 8080 bytes
	expected := (100*10 + 10) * 8
	if st.MemoryUsage() != expected {
		t.Errorf("MemoryUsage: got %d, want %d", st.MemoryUsage(), expected)
	}

	// Nil table should return 0
	nilSt := NewSlotTable(0, 0)
	if nilSt.MemoryUsage() != 0 {
		t.Errorf("nil table MemoryUsage: got %d, want 0", nilSt.MemoryUsage())
	}
}

// TestSlotTable_Integration tests realistic PikeVM usage pattern
func TestSlotTable_Integration(t *testing.T) {
	// Simulate pattern `(a+)(b+)` with 3 capture groups (0=match, 1=a+, 2=b+)
	numStates := 20
	slotsPerState := 6 // 3 groups * 2

	st := NewSlotTable(numStates, slotsPerState)

	// Simulate epsilon closure propagation:
	// State 0 → State 3 → State 7 → State 10 (match)

	// Initial state (no captures yet)
	st.ResetState(0)

	// At state 3, group 1 starts at position 0
	st.CopySlots(3, 0)
	st.SetSlot(3, 2, 0) // group 1 start

	// At state 7, group 1 ends and group 2 starts
	st.CopySlots(7, 3)
	st.SetSlot(7, 3, 3) // group 1 end = 3
	st.SetSlot(7, 4, 3) // group 2 start = 3

	// At state 10 (match), group 2 ends
	st.CopySlots(10, 7)
	st.SetSlot(10, 5, 6) // group 2 end = 6

	// Extract captures from match state
	captures := st.ExtractCaptures(10, 0, 6)

	// Verify
	if len(captures) != 3 {
		t.Fatalf("wrong capture count: got %d", len(captures))
	}
	if captures[0][0] != 0 || captures[0][1] != 6 {
		t.Errorf("group 0: got %v, want [0 6]", captures[0])
	}
	if captures[1][0] != 0 || captures[1][1] != 3 {
		t.Errorf("group 1: got %v, want [0 3]", captures[1])
	}
	if captures[2][0] != 3 || captures[2][1] != 6 {
		t.Errorf("group 2: got %v, want [3 6]", captures[2])
	}
}

// TestSlotTable_NilSafety verifies operations don't panic on nil table
func TestSlotTable_NilSafety(t *testing.T) {
	st := NewSlotTable(0, 0)

	// These should not panic
	st.Reset()
	st.ResetState(0)
	st.SetSlot(0, 0, 42)
	st.CopySlots(0, 1)

	if st.GetSlot(0, 0) != -1 {
		t.Error("GetSlot on nil should return -1")
	}
	if st.ForState(0) != nil {
		t.Error("ForState on nil should return nil")
	}
	if st.AllAbsent() != nil {
		t.Error("AllAbsent on nil should return nil")
	}
	if st.MemoryUsage() != 0 {
		t.Error("MemoryUsage on nil should return 0")
	}
}

// BenchmarkSlotTable_CopySlots benchmarks slot copying
func BenchmarkSlotTable_CopySlots(b *testing.B) {
	st := NewSlotTable(1000, 10)

	// Pre-populate source state
	slots := st.ForState(0)
	for i := range slots {
		slots[i] = i * 10
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := StateID(i%999 + 1)
		st.CopySlots(dst, 0)
	}
}

// BenchmarkSlotTable_ForState benchmarks slot access
func BenchmarkSlotTable_ForState(b *testing.B) {
	st := NewSlotTable(1000, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sid := StateID(i % 1000)
		_ = st.ForState(sid)
	}
}

// BenchmarkSlotTable_SetSlot benchmarks individual slot setting
func BenchmarkSlotTable_SetSlot(b *testing.B) {
	st := NewSlotTable(1000, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sid := StateID(i % 1000)
		slotIdx := i % 10
		st.SetSlot(sid, slotIdx, i)
	}
}
