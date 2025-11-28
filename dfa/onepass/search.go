package onepass

// Search performs an anchored search starting at input[0].
// Returns the capture group slots or nil if no match.
//
// The returned slice contains [start0, end0, start1, end1, ...]
// where group i is at indices [i*2, i*2+1].
// Group 0 is the entire match.
//
// Example:
//
//	dfa, _ := Build(nfa)
//	cache := NewCache(dfa.NumCaptures())
//	slots := dfa.Search(input, cache)
//	if slots != nil {
//	    entireMatch := input[slots[0]:slots[1]]
//	    group1 := input[slots[2]:slots[3]]
//	}
func (d *DFA) Search(input []byte, cache *Cache) []int {
	cache.Reset()

	// Initialize group 0 start (entire match always starts at 0 for anchored search)
	if len(cache.slots) >= 2 {
		cache.slots[0] = 0
	}

	state := d.startState
	pos := 0

	// Main search loop
	for pos < len(input) {
		b := input[pos]
		class := d.classes.Get(b)
		trans := d.getTransition(state, class)

		// Check for dead state (no match)
		if trans.IsDead() {
			return nil
		}

		// Consume the byte
		pos++

		// Update capture slots AFTER consuming byte
		// All slots in the transition are saved at current position (after the byte)
		trans.UpdateSlots(cache.slots, pos)

		// Transition to next state
		nextState := trans.NextState()

		// Check for match (leftmost-first: return on first match if match-wins)
		if trans.IsMatchWins() && d.isMatchState(nextState) {
			// Set end of entire match (group 0)
			if len(cache.slots) >= 2 {
				cache.slots[1] = pos
			}
			return cache.slots
		}

		state = nextState
	}

	// Check final state for match
	if d.isMatchState(state) {
		// Set end of entire match to end of input
		if len(cache.slots) >= 2 {
			cache.slots[1] = len(input)
		}
		return cache.slots
	}

	return nil
}

// SearchAt performs an anchored search starting at input[start:].
// This is a convenience wrapper around Search.
func (d *DFA) SearchAt(input []byte, start int, cache *Cache) []int {
	if start < 0 || start > len(input) {
		return nil
	}

	// Search on the substring
	slots := d.Search(input[start:], cache)
	if slots == nil {
		return nil
	}

	// Adjust slot positions to be relative to original input
	for i := range slots {
		if slots[i] >= 0 {
			slots[i] += start
		}
	}

	return slots
}
