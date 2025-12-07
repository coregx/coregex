package onepass

import (
	"fmt"

	"github.com/coregx/coregex/internal/conv"
	"github.com/coregx/coregex/internal/sparse"
	"github.com/coregx/coregex/nfa"
)

// Builder constructs a one-pass DFA from an NFA.
type Builder struct {
	nfa *nfa.NFA

	// Working state for DFS during one-pass check
	seen      *sparse.SparseSet // visited NFA states during epsilon closure
	stack     []stackEntry      // DFS stack
	matched   bool              // true if we've reached a match state in current closure
	matchMask uint32            // slot mask accumulated to reach match state

	// DFA state being built
	numStates  int                     // number of DFA states created
	table      []Transition            // transition table
	matchFlags []bool                  // match state flags
	matchSlots []uint32                // slots to apply at each match state
	nfaToDFA   map[nfa.StateID]StateID // maps NFA state to DFA state ID

	// Configuration
	stride  int
	stride2 uint
}

// stackEntry represents an entry in the DFS stack during epsilon closure.
type stackEntry struct {
	nfaID nfa.StateID
	slots uint32 // slot mask accumulated along epsilon path
}

// Build attempts to build a one-pass DFA from the given NFA.
// Returns (nil, ErrNotOnePass) if the pattern is not one-pass.
// Returns (nil, ErrTooManyCaptures) if more than 16 capture groups.
func Build(n *nfa.NFA) (*DFA, error) {
	// Check capture count limit (max 16 explicit captures + group 0)
	if n.CaptureCount() > 17 {
		return nil, ErrTooManyCaptures
	}

	// Quick heuristic check before attempting full build
	if !IsOnePass(n) {
		return nil, ErrNotOnePass
	}

	// Create builder
	b := &Builder{
		nfa:      n,
		seen:     sparse.NewSparseSet(conv.IntToUint32(n.States())),
		stack:    make([]stackEntry, 0, 16),
		nfaToDFA: make(map[nfa.StateID]StateID, n.States()),
	}

	// Get alphabet size from byte classes
	classes := n.ByteClasses()
	alphabetLen := classes.AlphabetLen()

	// Calculate stride (next power of 2 >= alphabetLen)
	b.stride = nextPowerOf2(alphabetLen)
	b.stride2 = log2(b.stride)

	// Allocate transition table (will grow as we add states)
	b.table = make([]Transition, 0, 64*b.stride)
	b.matchFlags = make([]bool, 0, 64)

	// Build DFA starting from anchored start state
	startNFA := n.StartAnchored()
	startDFA, err := b.buildState(startNFA)
	if err != nil {
		return nil, err
	}

	// Create DFA
	dfa := &DFA{
		numCaptures: n.CaptureCount(),
		table:       b.table,
		classes:     classes,
		alphabetLen: alphabetLen,
		stride:      b.stride,
		stride2:     b.stride2,
		startState:  startDFA,
		matchStates: b.matchFlags,
		matchSlots:  b.matchSlots,
		stateCount:  b.numStates,
	}

	// Find minimum match state ID for fast detection
	dfa.minMatchID = StateID(conv.IntToUint32(len(dfa.matchStates)))
	for i := len(dfa.matchStates) - 1; i >= 0; i-- {
		if dfa.matchStates[i] {
			dfa.minMatchID = StateID(conv.IntToUint32(i))
			break
		}
	}

	return dfa, nil
}

// buildState builds a DFA state from the given NFA state's epsilon closure.
// Returns the DFA state ID or error if not one-pass.
func (b *Builder) buildState(nfaRoot nfa.StateID) (StateID, error) {
	// Check if already built
	if sid, ok := b.nfaToDFA[nfaRoot]; ok {
		return sid, nil
	}

	// Compute epsilon closure with one-pass checking
	closure, isMatch, err := b.epsilonClosureOnePass(nfaRoot)
	if err != nil {
		return 0, err
	}

	// Allocate new DFA state
	sid := StateID(conv.IntToUint32(b.numStates))
	if sid > MaxStateID {
		return 0, fmt.Errorf("too many DFA states (max %d)", MaxStateID)
	}

	b.numStates++
	b.matchFlags = append(b.matchFlags, isMatch)
	// Store match slots (slots to apply when reaching this match state)
	if isMatch {
		b.matchSlots = append(b.matchSlots, b.matchMask)
	} else {
		b.matchSlots = append(b.matchSlots, 0)
	}
	b.nfaToDFA[nfaRoot] = sid

	// Allocate transition row (initialize to dead state)
	startIdx := len(b.table)
	for i := 0; i < b.stride; i++ {
		b.table = append(b.table, NewTransition(DeadState, false, 0))
	}

	// Build transitions for each byte class
	err = b.buildTransitions(startIdx, closure)
	if err != nil {
		return 0, err
	}

	return sid, nil
}

// closureEntry represents a state in the epsilon closure with accumulated slots.
type closureEntry struct {
	nfaID nfa.StateID
	slots uint32
}

// epsilonClosureOnePass computes epsilon closure while checking one-pass property.
// Returns (closure entries with slots, isMatch, error).
// If isMatch is true, b.matchMask contains the slot mask to apply at match.
func (b *Builder) epsilonClosureOnePass(root nfa.StateID) ([]closureEntry, bool, error) {
	b.seen.Clear()
	b.matched = false
	b.matchMask = 0
	b.stack = b.stack[:0]

	// Start DFS from root
	if err := b.stackPush(root, 0); err != nil {
		return nil, false, err
	}

	var closure []closureEntry

	for len(b.stack) > 0 {
		// Pop from stack
		entry := b.stack[len(b.stack)-1]
		b.stack = b.stack[:len(b.stack)-1]

		nfaID := entry.nfaID
		slots := entry.slots

		// Save this entry with accumulated slots
		closure = append(closure, closureEntry{nfaID, slots})

		state := b.nfa.State(nfaID)
		if state == nil {
			continue
		}

		switch state.Kind() {
		case nfa.StateMatch:
			// Check for multiple match paths
			if b.matched {
				return nil, false, ErrNotOnePass
			}
			b.matched = true
			// Save the slots accumulated to reach match state
			// These are the capture END positions
			b.matchMask = slots

		case nfa.StateSplit:
			// Follow both epsilon paths
			left, right := state.Split()
			if err := b.stackPush(left, slots); err != nil {
				return nil, false, err
			}
			if err := b.stackPush(right, slots); err != nil {
				return nil, false, err
			}

		case nfa.StateEpsilon:
			// Follow epsilon transition
			next := state.Epsilon()
			if err := b.stackPush(next, slots); err != nil {
				return nil, false, err
			}

		case nfa.StateCapture:
			// Update slot mask and follow next
			idx, isStart, next := state.Capture()
			slotIdx := idx * 2
			if !isStart {
				slotIdx++
			}
			if slotIdx < 32 {
				slots |= (1 << slotIdx)
			}
			if err := b.stackPush(next, slots); err != nil {
				return nil, false, err
			}

			// ByteRange and Sparse are not epsilon transitions
			// They will be handled in buildTransitions
		}
	}

	return closure, b.matched, nil
}

// stackPush adds an NFA state to the DFS stack.
// Returns error if state already visited (indicates non-one-pass).
func (b *Builder) stackPush(nfaID nfa.StateID, slots uint32) error {
	// Check if already visited via epsilon path
	if b.seen.Contains(uint32(nfaID)) {
		// Multiple epsilon paths to same state = NOT one-pass
		return ErrNotOnePass
	}

	b.seen.Insert(uint32(nfaID))
	b.stack = append(b.stack, stackEntry{nfaID, slots})
	return nil
}

// transInfo tracks byte transition info including source slots.
type transInfo struct {
	targetNFA nfa.StateID
	slots     uint32 // Slots accumulated from SOURCE epsilon closure
}

// buildTransitions builds byte transitions for a DFA state.
// For each NFA state in the closure, add its byte transitions.
//
// IMPORTANT: Slots are collected from the SOURCE state's epsilon closure
// (entry.slots), not from the target state. These slots represent capture
// positions that should be recorded BEFORE consuming the byte.
//
//nolint:gocognit // complexity inherent to DFA construction algorithm
func (b *Builder) buildTransitions(tableIdx int, closure []closureEntry) error {
	// Track which byte classes have transitions
	// Key: byte class, Value: target NFA state + source slots
	byteTransitions := make(map[byte]transInfo)

	for _, entry := range closure {
		state := b.nfa.State(entry.nfaID)
		if state == nil {
			continue
		}

		switch state.Kind() {
		case nfa.StateByteRange:
			lo, hi, next := state.ByteRange()
			// Use int to avoid overflow when hi=255 (byte wraps to 0)
			for by := int(lo); by <= int(hi); by++ {
				class := b.nfa.ByteClasses().Get(byte(by))
				// Check for conflict
				if existing, ok := byteTransitions[class]; ok {
					if existing.targetNFA != next {
						return ErrNotOnePass
					}
					// Merge source slots (multiple paths to same transition)
					byteTransitions[class] = transInfo{
						targetNFA: next,
						slots:     existing.slots | entry.slots,
					}
				} else {
					byteTransitions[class] = transInfo{
						targetNFA: next,
						slots:     entry.slots, // SOURCE slots!
					}
				}
			}

		case nfa.StateSparse:
			for _, trans := range state.Transitions() {
				// Use int to avoid overflow when trans.Hi=255 (byte wraps to 0)
				for by := int(trans.Lo); by <= int(trans.Hi); by++ {
					class := b.nfa.ByteClasses().Get(byte(by))
					// Check for conflict
					if existing, ok := byteTransitions[class]; ok {
						if existing.targetNFA != trans.Next {
							return ErrNotOnePass
						}
						byteTransitions[class] = transInfo{
							targetNFA: trans.Next,
							slots:     existing.slots | entry.slots,
						}
					} else {
						byteTransitions[class] = transInfo{
							targetNFA: trans.Next,
							slots:     entry.slots, // SOURCE slots!
						}
					}
				}
			}
		}
	}

	// Build DFA transitions from byte transitions
	for class, info := range byteTransitions {
		// Recursively build target DFA state
		nextDFA, err := b.buildState(info.targetNFA)
		if err != nil {
			return err
		}

		// Create transition with SOURCE slots (applied at current position BEFORE consuming byte)
		trans := NewTransition(nextDFA, false, info.slots)

		// Store in table
		idx := tableIdx + int(class)
		if idx >= len(b.table) {
			return fmt.Errorf("transition table index out of bounds")
		}
		b.table[idx] = trans
	}

	return nil
}

// IsOnePass quickly checks if an NFA might be one-pass (heuristic).
// This is a fast pre-check before attempting full DFA construction.
//
// Returns false for patterns that are definitely not one-pass:
//   - Patterns with unanchored prefix (one-pass requires anchored search)
//   - Patterns with too many capture groups
//
// Returns true for patterns that might be one-pass (need full check).
func IsOnePass(n *nfa.NFA) bool {
	// Check if anchored (one-pass requires anchored matching)
	if !n.IsAlwaysAnchored() {
		return false
	}

	// Check capture count
	if n.CaptureCount() > 17 {
		return false
	}

	// Heuristic: small NFAs are more likely to be one-pass
	// But we can't definitively say without full analysis
	return true
}
