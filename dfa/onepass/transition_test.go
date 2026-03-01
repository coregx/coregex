package onepass

import (
	"testing"
)

func TestTransitionCreationAndAccessors(t *testing.T) {
	tests := []struct {
		name      string
		next      StateID
		matchWins bool
		slots     uint32
	}{
		{name: "zero state no match no slots", next: 0, matchWins: false, slots: 0},
		{name: "dead state", next: DeadState, matchWins: false, slots: 0},
		{name: "match wins all slots", next: 1, matchWins: true, slots: 0xFFFFFFFF},
		{name: "max state ID", next: MaxStateID, matchWins: false, slots: 0x00000001},
		{name: "mid state match some slots", next: 42, matchWins: true, slots: 0x00000F0F},
		{name: "single slot bit 0", next: 5, matchWins: false, slots: 0x1},
		{name: "single slot bit 31", next: 5, matchWins: false, slots: 0x80000000},
		{name: "alternating bits", next: 100, matchWins: true, slots: 0xAAAAAAAA},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trans := NewTransition(tt.next, tt.matchWins, tt.slots)

			if got := trans.NextState(); got != tt.next {
				t.Errorf("NextState() = %d, want %d", got, tt.next)
			}

			if got := trans.IsMatchWins(); got != tt.matchWins {
				t.Errorf("IsMatchWins() = %v, want %v", got, tt.matchWins)
			}

			if got := trans.SlotMask(); got != tt.slots {
				t.Errorf("SlotMask() = %#x, want %#x", got, tt.slots)
			}
		})
	}
}

func TestTransitionIsDead(t *testing.T) {
	tests := []struct {
		name     string
		next     StateID
		wantDead bool
	}{
		{name: "dead state", next: DeadState, wantDead: true},
		{name: "state 0 is dead", next: 0, wantDead: true},
		{name: "state 1 is alive", next: 1, wantDead: false},
		{name: "max state is alive", next: MaxStateID, wantDead: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trans := NewTransition(tt.next, false, 0)
			if got := trans.IsDead(); got != tt.wantDead {
				t.Errorf("IsDead() = %v, want %v", got, tt.wantDead)
			}
		})
	}
}

func TestTransitionLookAround(t *testing.T) {
	tests := []struct {
		name string
		look uint16
	}{
		{name: "no look-around", look: 0},
		{name: "look bit 0", look: 1},
		{name: "look bit 9 max", look: 0x3FF}, // 10 bits max
		{name: "look mid value", look: 0x155},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trans := NewTransition(1, false, 0)
			trans = trans.WithLookAround(tt.look)

			got := trans.LookAround()
			if got != tt.look {
				t.Errorf("LookAround() = %#x, want %#x", got, tt.look)
			}

			// Verify other fields are preserved
			if trans.NextState() != 1 {
				t.Errorf("NextState() = %d after WithLookAround, want 1", trans.NextState())
			}
			if trans.SlotMask() != 0 {
				t.Errorf("SlotMask() = %#x after WithLookAround, want 0", trans.SlotMask())
			}
		})
	}
}

func TestTransitionWithSlotMask(t *testing.T) {
	tests := []struct {
		name     string
		original uint32
		newSlots uint32
	}{
		{name: "set from zero", original: 0, newSlots: 0xFF},
		{name: "replace existing", original: 0xFF, newSlots: 0x00FF00FF},
		{name: "clear slots", original: 0xFFFFFFFF, newSlots: 0},
		{name: "keep same", original: 0xABCD, newSlots: 0xABCD},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trans := NewTransition(42, true, tt.original)
			trans = trans.WithSlotMask(tt.newSlots)

			if got := trans.SlotMask(); got != tt.newSlots {
				t.Errorf("SlotMask() = %#x, want %#x", got, tt.newSlots)
			}

			// Verify other fields are preserved
			if trans.NextState() != 42 {
				t.Errorf("NextState() = %d after WithSlotMask, want 42", trans.NextState())
			}
			if !trans.IsMatchWins() {
				t.Error("IsMatchWins() = false after WithSlotMask, want true")
			}
		})
	}
}

func TestTransitionWithSlotMaskRoundtrip(t *testing.T) {
	// Create a transition with all fields set, then modify slot mask
	trans := NewTransition(100, true, 0x12345678)
	trans = trans.WithLookAround(0x1AB)

	// Verify initial state
	if trans.NextState() != 100 {
		t.Fatalf("Initial NextState() = %d, want 100", trans.NextState())
	}
	if trans.LookAround() != 0x1AB {
		t.Fatalf("Initial LookAround() = %#x, want 0x1AB", trans.LookAround())
	}

	// Change slot mask
	trans = trans.WithSlotMask(0xDEADBEEF)

	// All fields should be preserved except slot mask
	if trans.NextState() != 100 {
		t.Errorf("NextState() = %d after WithSlotMask, want 100", trans.NextState())
	}
	if !trans.IsMatchWins() {
		t.Error("IsMatchWins() = false after WithSlotMask, want true")
	}
	if trans.LookAround() != 0x1AB {
		t.Errorf("LookAround() = %#x after WithSlotMask, want 0x1AB", trans.LookAround())
	}
	if trans.SlotMask() != 0xDEADBEEF {
		t.Errorf("SlotMask() = %#x, want 0xDEADBEEF", trans.SlotMask())
	}
}

func TestTransitionWithLookAroundRoundtrip(t *testing.T) {
	// Create transition with all fields
	trans := NewTransition(50, false, 0xAAAABBBB)

	// Add look-around
	trans = trans.WithLookAround(0x2FF)

	// All fields should be preserved except look-around
	if trans.NextState() != 50 {
		t.Errorf("NextState() = %d, want 50", trans.NextState())
	}
	if trans.IsMatchWins() {
		t.Error("IsMatchWins() should be false")
	}
	if trans.SlotMask() != 0xAAAABBBB {
		t.Errorf("SlotMask() = %#x, want 0xAAAABBBB", trans.SlotMask())
	}
	if trans.LookAround() != 0x2FF {
		t.Errorf("LookAround() = %#x, want 0x2FF", trans.LookAround())
	}

	// Change look-around
	trans = trans.WithLookAround(0x001)
	if trans.LookAround() != 0x001 {
		t.Errorf("LookAround() = %#x after second WithLookAround, want 0x001", trans.LookAround())
	}
	// Other fields preserved
	if trans.SlotMask() != 0xAAAABBBB {
		t.Errorf("SlotMask() = %#x after WithLookAround, want 0xAAAABBBB", trans.SlotMask())
	}
}

func TestTransitionUpdateSlots(t *testing.T) {
	tests := []struct {
		name     string
		slotMask uint32
		pos      int
		slotLen  int
		want     map[int]int // slot index -> expected value
	}{
		{
			name:     "no bits set",
			slotMask: 0,
			pos:      10,
			slotLen:  4,
			want:     map[int]int{0: -1, 1: -1, 2: -1, 3: -1},
		},
		{
			name:     "bit 0 only",
			slotMask: 0x1,
			pos:      5,
			slotLen:  4,
			want:     map[int]int{0: 5, 1: -1, 2: -1, 3: -1},
		},
		{
			name:     "bits 0 2 5",
			slotMask: 0b00100101,
			pos:      42,
			slotLen:  10,
			want:     map[int]int{0: 42, 1: -1, 2: 42, 3: -1, 4: -1, 5: 42, 6: -1},
		},
		{
			name:     "all low bits set",
			slotMask: 0xF,
			pos:      7,
			slotLen:  4,
			want:     map[int]int{0: 7, 1: 7, 2: 7, 3: 7},
		},
		{
			name:     "slot mask wider than slots array",
			slotMask: 0xFFFF,
			pos:      3,
			slotLen:  4,
			want:     map[int]int{0: 3, 1: 3, 2: 3, 3: 3}, // only first 4 updated
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trans := NewTransition(1, false, tt.slotMask)

			slots := make([]int, tt.slotLen)
			for i := range slots {
				slots[i] = -1
			}

			trans.UpdateSlots(slots, tt.pos)

			for idx, wantVal := range tt.want {
				if idx < len(slots) && slots[idx] != wantVal {
					t.Errorf("slots[%d] = %d, want %d", idx, slots[idx], wantVal)
				}
			}
		})
	}
}

func TestTransitionUpdateSlotsEmptyArray(t *testing.T) {
	trans := NewTransition(1, false, 0xFF)

	// Should not panic with empty slots
	var slots []int
	trans.UpdateSlots(slots, 5)
}

func TestTransitionBitBoundaryValues(t *testing.T) {
	// Test with boundary state IDs
	tests := []struct {
		name string
		next StateID
	}{
		{name: "state 0 (dead)", next: 0},
		{name: "state 1", next: 1},
		{name: "state 255", next: 255},
		{name: "state 1023", next: 1023},
		{name: "state 65535", next: 65535},
		{name: "max 21-bit", next: MaxStateID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trans := NewTransition(tt.next, true, 0xFFFFFFFF)
			if got := trans.NextState(); got != tt.next {
				t.Errorf("NextState() = %d, want %d", got, tt.next)
			}
		})
	}
}

func TestTransitionConstants(t *testing.T) {
	// Verify DeadState and MaxStateID constants
	if DeadState != 0 {
		t.Errorf("DeadState = %d, want 0", DeadState)
	}
	if MaxStateID != (1<<21)-1 {
		t.Errorf("MaxStateID = %d, want %d", MaxStateID, (1<<21)-1)
	}
}

func TestApplyMatchSlots(t *testing.T) {
	tests := []struct {
		name    string
		initial []int
		mask    uint32
		pos     int
		want    []int
	}{
		{
			name:    "apply to slots 0 and 1",
			initial: []int{-1, -1, -1, -1},
			mask:    0b0011,
			pos:     10,
			want:    []int{10, 10, -1, -1},
		},
		{
			name:    "zero mask changes nothing",
			initial: []int{1, 2, 3, 4},
			mask:    0,
			pos:     99,
			want:    []int{1, 2, 3, 4},
		},
		{
			name:    "all bits set",
			initial: []int{-1, -1, -1, -1},
			mask:    0xF,
			pos:     5,
			want:    []int{5, 5, 5, 5},
		},
		{
			name:    "mask wider than slots",
			initial: []int{-1, -1},
			mask:    0xFF,
			pos:     7,
			want:    []int{7, 7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slots := make([]int, len(tt.initial))
			copy(slots, tt.initial)

			applyMatchSlots(slots, tt.mask, tt.pos)

			for i := range tt.want {
				if slots[i] != tt.want[i] {
					t.Errorf("slots[%d] = %d, want %d", i, slots[i], tt.want[i])
				}
			}
		})
	}
}

func TestTransitionMatchWinsIndependence(t *testing.T) {
	// Verify matchWins flag does not interfere with state ID or slots
	transNoMatch := NewTransition(42, false, 0xABCDEF)
	transMatch := NewTransition(42, true, 0xABCDEF)

	if transNoMatch.NextState() != transMatch.NextState() {
		t.Error("MatchWins should not affect NextState")
	}
	if transNoMatch.SlotMask() != transMatch.SlotMask() {
		t.Error("MatchWins should not affect SlotMask")
	}
	if transNoMatch.IsMatchWins() == transMatch.IsMatchWins() {
		t.Error("MatchWins flag should differ")
	}
}
