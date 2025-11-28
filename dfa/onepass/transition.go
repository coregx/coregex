package onepass

// Transition encodes DFA state transition + slot updates in 64 bits.
//
// Bit layout (from high to low):
//   - Bits 43-63 (21 bits): Next StateID (max 2M states)
//   - Bit 42 (1 bit): MatchWins flag for leftmost-first semantics
//   - Bits 32-41 (10 bits): Look-around assertions (word boundary, line boundary, etc.)
//   - Bits 0-31 (32 bits): Slot update mask (one bit per slot)
//
// This encoding enables single uint64 lookup per transition with all metadata included.
type Transition uint64

const (
	// Bit positions and masks for transition encoding
	stateIDBits  = 21
	stateIDShift = 64 - stateIDBits // 43
	stateIDMask  = (1 << stateIDBits) - 1

	matchWinsShift = 42
	matchWinsMask  = uint64(1) << matchWinsShift

	lookAroundShift = 32
	lookAroundBits  = 10
	lookAroundMask  = ((1 << lookAroundBits) - 1) << lookAroundShift

	slotMask = 0xFFFFFFFF // bits 0-31

	// DeadState represents a dead/fail state (no valid transition)
	DeadState StateID = 0

	// MaxStateID is the maximum valid state ID (21 bits)
	MaxStateID StateID = (1 << stateIDBits) - 1
)

// NewTransition creates a new transition with the given next state, match-wins flag, and slot mask.
func NewTransition(next StateID, matchWins bool, slots uint32) Transition {
	t := Transition(next) << stateIDShift
	if matchWins {
		t |= Transition(matchWinsMask)
	}
	t |= Transition(slots)
	return t
}

// NextState extracts the next state ID from the transition.
func (t Transition) NextState() StateID {
	//nolint:gosec // G115: masked value is within StateID range (21 bits max), safe conversion
	return StateID((t >> stateIDShift) & stateIDMask)
}

// IsDead returns true if this transition leads to a dead state.
func (t Transition) IsDead() bool {
	return t.NextState() == DeadState
}

// IsMatchWins returns true if the match-wins flag is set.
// This flag indicates that if the current state is a match state,
// the match should be accepted immediately (leftmost-first semantics).
func (t Transition) IsMatchWins() bool {
	return (t & Transition(matchWinsMask)) != 0
}

// SlotMask returns the 32-bit slot update mask.
// Each bit indicates whether to save the current position to that slot.
func (t Transition) SlotMask() uint32 {
	//nolint:gosec // G115: slotMask is 32-bit constant, safe conversion
	return uint32(t & slotMask)
}

// LookAround returns the look-around assertion flags.
func (t Transition) LookAround() uint16 {
	//nolint:gosec // G115: look-around is 10 bits max, safe conversion to uint16
	return uint16((t & lookAroundMask) >> lookAroundShift)
}

// UpdateSlots applies the slot updates to the given slots array.
// For each bit set in the slot mask, slots[i] is set to pos.
func (t Transition) UpdateSlots(slots []int, pos int) {
	mask := t.SlotMask()
	if mask == 0 {
		return
	}

	// Iterate through bits to update slots
	for i := 0; i < 32 && mask != 0; i++ {
		if mask&1 != 0 {
			if i < len(slots) {
				slots[i] = pos
			}
		}
		mask >>= 1
	}
}

// WithSlotMask creates a new transition with the given slot mask, preserving other fields.
func (t Transition) WithSlotMask(slots uint32) Transition {
	// Clear existing slot bits, set new ones
	return (t &^ Transition(slotMask)) | Transition(slots)
}

// WithLookAround creates a new transition with the given look-around flags.
func (t Transition) WithLookAround(look uint16) Transition {
	// Clear existing look-around bits, set new ones
	return (t &^ Transition(lookAroundMask)) | (Transition(look) << lookAroundShift)
}
