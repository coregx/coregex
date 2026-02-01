package nfa

// SlotTable is a 2D table (flattened to 1D) storing capture slot values per NFA state.
//
// This design follows the Rust regex crate's SlotTable architecture:
// - Each state has a row of slots (one slot per capture position: start/end for each group)
// - The table is pre-allocated for all states to enable O(1) random access
// - Dynamic slot sizing allows using 0 slots (IsMatch), 2 slots (Find), or full slots (Captures)
//
// Memory layout: table[stateID * slotsPerState + slotIndex]
//
// Reference: rust-regex/regex-automata/src/nfa/thompson/pikevm.rs:2044-2160
type SlotTable struct {
	// table is the flattened 2D array: [stateID][slotIndex] → value
	// Value of -1 means "not set" (like Option<NonMaxUsize> in Rust)
	table []int

	// slotsPerState is the stride (row length) - total slots per state.
	// This equals NFA.CaptureCount() * 2 (start + end for each group).
	slotsPerState int

	// activeSlots is the number of slots to actually use during search.
	// This enables dynamic slot sizing:
	// - 0: IsMatch mode (no capture tracking)
	// - 2: Find mode (only track overall match start/end)
	// - full: Captures mode (track all groups)
	activeSlots int

	// numStates is the total NFA states count.
	numStates int

	// scratchOffset is the offset where scratch space begins.
	// Scratch space is used for temporary slot storage (e.g., all_absent).
	scratchOffset int
}

// NewSlotTable creates a new SlotTable for an NFA with the given parameters.
//
// Parameters:
//   - numStates: total NFA states (determines table rows)
//   - slotsPerState: total capture slots per state (CaptureCount * 2)
//
// The table is pre-allocated to enable O(1) access. All slots are initialized
// to -1 (unset).
func NewSlotTable(numStates, slotsPerState int) *SlotTable {
	if numStates <= 0 || slotsPerState <= 0 {
		return &SlotTable{
			table:         nil,
			slotsPerState: slotsPerState,
			activeSlots:   slotsPerState,
			numStates:     numStates,
			scratchOffset: 0,
		}
	}

	// Total size: (numStates * slotsPerState) + slotsPerState (scratch space)
	// The extra slotsPerState is scratch space for all_absent() like Rust.
	scratchOffset := numStates * slotsPerState
	totalSize := scratchOffset + slotsPerState

	table := make([]int, totalSize)
	// Initialize all slots to -1 (unset)
	for i := range table {
		table[i] = -1
	}

	return &SlotTable{
		table:         table,
		slotsPerState: slotsPerState,
		activeSlots:   slotsPerState, // default to full capture tracking
		numStates:     numStates,
		scratchOffset: scratchOffset,
	}
}

// ForState returns a slice of slots for the given state ID.
//
// The returned slice has length = activeSlots (not slotsPerState), allowing
// dynamic slot sizing. Modifying the returned slice modifies the table.
//
// Panics if stateID is out of bounds.
func (st *SlotTable) ForState(sid StateID) []int {
	if st.table == nil || st.slotsPerState == 0 || st.activeSlots == 0 {
		return nil
	}
	i := int(sid) * st.slotsPerState
	return st.table[i : i+st.activeSlots]
}

// ForStateUnchecked returns slots for a state without bounds checking.
// This is a performance optimization for hot paths where bounds are known valid.
func (st *SlotTable) ForStateUnchecked(sid StateID) []int {
	if st.activeSlots == 0 {
		return nil
	}
	i := int(sid) * st.slotsPerState
	return st.table[i : i+st.activeSlots]
}

// SetActiveSlots sets the number of active slots to track during search.
//
// This enables dynamic slot sizing:
//   - 0: IsMatch mode (no capture tracking, fastest)
//   - 2: Find mode (only track overall match start/end)
//   - full (slotsPerState): Captures mode (track all groups)
//
// If n > slotsPerState, it is clamped to slotsPerState.
func (st *SlotTable) SetActiveSlots(n int) {
	if n < 0 {
		n = 0
	}
	if n > st.slotsPerState {
		n = st.slotsPerState
	}
	st.activeSlots = n
}

// ActiveSlots returns the current number of active slots.
func (st *SlotTable) ActiveSlots() int {
	return st.activeSlots
}

// SlotsPerState returns the total slots per state (stride).
func (st *SlotTable) SlotsPerState() int {
	return st.slotsPerState
}

// CopySlots copies active slots from src state to dst state.
//
// This is used during epsilon closure to propagate capture positions
// from one state to another.
func (st *SlotTable) CopySlots(dst, src StateID) {
	if st.activeSlots == 0 || st.table == nil {
		return
	}
	dstSlots := st.ForStateUnchecked(dst)
	srcSlots := st.ForStateUnchecked(src)
	copy(dstSlots, srcSlots)
}

// SetSlot sets a specific slot value for a state.
//
// Parameters:
//   - sid: state ID
//   - slotIndex: index within the state's slots (0 = group 0 start, 1 = group 0 end, etc.)
//   - value: position value (-1 for unset)
//
// Does nothing if slotIndex >= activeSlots (respects dynamic sizing).
func (st *SlotTable) SetSlot(sid StateID, slotIndex, value int) {
	if slotIndex < 0 || slotIndex >= st.activeSlots || st.table == nil {
		return
	}
	i := int(sid)*st.slotsPerState + slotIndex
	st.table[i] = value
}

// GetSlot returns a slot value for a state.
//
// Returns -1 if slot is unset or if slotIndex is out of bounds.
func (st *SlotTable) GetSlot(sid StateID, slotIndex int) int {
	if slotIndex < 0 || slotIndex >= st.activeSlots || st.table == nil {
		return -1
	}
	i := int(sid)*st.slotsPerState + slotIndex
	return st.table[i]
}

// Reset clears all slots to -1 (unset).
//
// This is called at the start of each search to reset the table.
// Note: This is O(n) where n = numStates * slotsPerState.
// For large tables, consider using generation-based clearing instead.
func (st *SlotTable) Reset() {
	for i := range st.table {
		st.table[i] = -1
	}
}

// ResetState clears only the slots for a specific state.
//
// More efficient than Reset() when only specific states need clearing.
func (st *SlotTable) ResetState(sid StateID) {
	if st.table == nil || st.slotsPerState == 0 {
		return
	}
	slots := st.ForState(sid)
	for i := range slots {
		slots[i] = -1
	}
}

// AllAbsent returns a slice of slots where all values are guaranteed to be -1.
//
// This is used for epsilon closure when computing transitions that should not
// have any capture slots set. The scratch space at the end of the table is
// used for this purpose.
//
// Reference: rust-regex pikevm.rs:2156-2159
func (st *SlotTable) AllAbsent() []int {
	if st.table == nil || st.activeSlots == 0 {
		return nil
	}
	// Use scratch space at the end of the table
	return st.table[st.scratchOffset : st.scratchOffset+st.activeSlots]
}

// MemoryUsage returns the heap memory usage in bytes.
func (st *SlotTable) MemoryUsage() int {
	if st.table == nil {
		return 0
	}
	// Each int is 8 bytes on 64-bit systems
	return len(st.table) * 8
}

// Clone creates a deep copy of the SlotTable.
//
// This is useful for creating independent copies for parallel searches.
func (st *SlotTable) Clone() *SlotTable {
	if st.table == nil {
		return &SlotTable{
			slotsPerState: st.slotsPerState,
			activeSlots:   st.activeSlots,
			numStates:     st.numStates,
			scratchOffset: st.scratchOffset,
		}
	}

	tableCopy := make([]int, len(st.table))
	copy(tableCopy, st.table)

	return &SlotTable{
		table:         tableCopy,
		slotsPerState: st.slotsPerState,
		activeSlots:   st.activeSlots,
		numStates:     st.numStates,
		scratchOffset: st.scratchOffset,
	}
}

// ExtractCaptures extracts capture group positions from a state's slots.
//
// Returns a slice of [start, end] pairs for each capture group.
// If a group was not captured, its entry is nil.
//
// This is a convenience method for converting slot data to the result format.
func (st *SlotTable) ExtractCaptures(sid StateID, matchStart, matchEnd int) [][]int {
	if st.table == nil || st.slotsPerState == 0 {
		// No captures defined - return just group 0 (entire match)
		return [][]int{{matchStart, matchEnd}}
	}

	numGroups := st.slotsPerState / 2
	result := make([][]int, numGroups)

	// Group 0 is always the entire match
	result[0] = []int{matchStart, matchEnd}

	// Extract other groups from slots
	slots := st.ForState(sid)
	for i := 1; i < numGroups && i*2+1 < len(slots); i++ {
		start := slots[i*2]
		end := slots[i*2+1]
		if start >= 0 && end >= 0 {
			result[i] = []int{start, end}
		}
	}

	return result
}
