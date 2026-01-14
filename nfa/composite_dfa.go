package nfa

import (
	"regexp/syntax"
)

// CompositeSequenceDFA is a specialized DFA for composite character class patterns.
// It uses NFA subset construction to handle overlapping character classes correctly.
//
// For pattern `\w+[0-9]+` with overlapping chars (digits are in both \w and [0-9]):
//   - Uses byte classes to reduce transitions
//   - Each DFA state represents a set of NFA positions (which parts could be active)
//   - Handles overlap by tracking multiple possible parse positions simultaneously
//
// Thread safety: NOT thread-safe. For concurrent usage, each goroutine needs its own instance.
type CompositeSequenceDFA struct {
	// byteToClass maps each byte to its equivalence class
	byteToClass [256]byte

	// numClasses is the number of byte equivalence classes
	numClasses int

	// transitions[state * numClasses + class] = next state
	transitions []uint16

	// accepting marks which states are accepting
	accepting []bool

	// numStates is the total number of DFA states
	numStates int

	// parts are the original pattern parts (for reference)
	parts []*charClassPart
}

// NewCompositeSequenceDFA creates a specialized DFA for composite patterns.
// Returns nil if the pattern is not suitable for this DFA.
func NewCompositeSequenceDFA(re *syntax.Regexp) *CompositeSequenceDFA {
	parts := extractCompositeCharClassParts(re)
	if len(parts) == 0 || len(parts) > 8 {
		return nil // Too many parts, or not a composite pattern
	}

	// Check all parts have minMatch >= 1 (no * quantifiers for now)
	for _, p := range parts {
		if p.minMatch == 0 {
			return nil // Star quantifiers need more complex handling
		}
	}

	d := &CompositeSequenceDFA{parts: parts}
	d.buildByteClasses(parts)
	d.buildDFASubsetConstruction(parts)

	return d
}

// buildByteClasses creates byte equivalence classes from the pattern's char classes.
func (d *CompositeSequenceDFA) buildByteClasses(parts []*charClassPart) {
	// classSignature[byte] = which parts this byte matches (as a bitmask)
	var signatures [256]uint16
	for i, part := range parts {
		for b := 0; b < 256; b++ {
			if part.membership[b] {
				signatures[b] |= 1 << i
			}
		}
	}

	// Group bytes by signature
	signatureToClass := make(map[uint16]byte)
	classCount := byte(1) // Class 0 is reserved for "matches nothing"

	for b := 0; b < 256; b++ {
		sig := signatures[b]
		if sig == 0 {
			d.byteToClass[b] = 0 // Non-matching byte
		} else if class, ok := signatureToClass[sig]; ok {
			d.byteToClass[b] = class
		} else {
			signatureToClass[sig] = classCount
			d.byteToClass[b] = classCount
			classCount++
		}
	}

	d.numClasses = int(classCount)
}

// configSet represents a set of NFA configurations (DFA state)
type configSet uint32 // bitmask of (part * 2 + metMin)

func (c configSet) has(part int, metMin bool) bool {
	idx := part * 2
	if metMin {
		idx++
	}
	return (c & (1 << idx)) != 0
}

//nolint:unparam // metMin always true for minMatch>=1 patterns, but keeps API consistent with has()
func (c configSet) add(part int, metMin bool) configSet {
	idx := part * 2
	if metMin {
		idx++
	}
	return c | (1 << idx)
}

// buildDFASubsetConstruction uses subset construction to build DFA from NFA configs
func (d *CompositeSequenceDFA) buildDFASubsetConstruction(parts []*charClassPart) {
	numParts := len(parts)

	// Map from config set to DFA state ID
	configToState := make(map[configSet]uint16)

	// DFA states: list of config sets
	var states []configSet

	// Add dead state (empty config set)
	configToState[0] = 0
	states = append(states, 0)

	// Work queue of states to process
	queue := []configSet{}

	// Build transitions
	var transitionList [][]uint16 // transitionList[stateID][class] = nextStateID

	// Process dead state: when we consume a char matching first part,
	// we go to state (part0, metMin=true) since minMatch=1 and we consumed 1 char
	firstCharState := configSet(0).add(0, true) // After consuming 1 char of first part
	if _, ok := configToState[firstCharState]; !ok {
		configToState[firstCharState] = uint16(len(states)) //nolint:gosec // max 8 parts = max 512 states, fits uint16
		states = append(states, firstCharState)
		queue = append(queue, firstCharState)
	}

	deadTrans := make([]uint16, d.numClasses)
	for class := 0; class < d.numClasses; class++ {
		if d.classMatchesPart(class, parts[0]) {
			deadTrans[class] = configToState[firstCharState]
		} else {
			deadTrans[class] = 0 // Stay dead
		}
	}
	transitionList = append(transitionList, deadTrans)

	// Process queue
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentID := configToState[current]

		trans := make([]uint16, d.numClasses)

		for class := 0; class < d.numClasses; class++ {
			// Compute next config set for this class
			next := d.computeNextConfigs(current, class, parts)

			// If dead and matches first part, can restart (consuming this char)
			if next == 0 && d.classMatchesPart(class, parts[0]) {
				next = configSet(0).add(0, true) // metMin=true since we consumed 1 char
			}

			// Get or create DFA state for next config set
			if nextID, ok := configToState[next]; ok {
				trans[class] = nextID
			} else {
				nextID := uint16(len(states)) //nolint:gosec // max 8 parts = max 512 states, fits uint16
				configToState[next] = nextID
				states = append(states, next)
				queue = append(queue, next)
				trans[class] = nextID
			}
		}

		// Ensure transitionList has enough slots
		for len(transitionList) <= int(currentID) {
			transitionList = append(transitionList, nil)
		}
		transitionList[currentID] = trans
	}

	// Flatten transitions
	d.numStates = len(states)
	d.transitions = make([]uint16, d.numStates*d.numClasses)
	for stateID, trans := range transitionList {
		for class, nextID := range trans {
			d.transitions[stateID*d.numClasses+class] = nextID
		}
	}

	// Mark accepting states (those where last part has metMin=true)
	d.accepting = make([]bool, d.numStates)
	for stateID, configs := range states {
		// A state is accepting if it contains (lastPart, metMin=true)
		if configs.has(numParts-1, true) {
			d.accepting[stateID] = true
		}
	}
}

// computeNextConfigs computes the next config set after consuming a byte of given class
func (d *CompositeSequenceDFA) computeNextConfigs(current configSet, class int, parts []*charClassPart) configSet {
	numParts := len(parts)
	var next configSet

	for part := 0; part < numParts; part++ {
		for _, metMin := range []bool{false, true} {
			if !current.has(part, metMin) {
				continue // This config not active
			}

			// Can we stay in current part?
			if d.classMatchesPart(class, parts[part]) {
				// Stay in part, update metMin
				// For minMatch=1, seeing one char means metMin=true
				next = next.add(part, true)
			}

			// If we met minimum for current part, can we transition to next?
			if metMin && part+1 < numParts && d.classMatchesPart(class, parts[part+1]) {
				// Transition to next part
				next = next.add(part+1, true) // parts have minMatch=1, so one char = metMin
			}
		}
	}

	return next
}

// classMatchesPart checks if any byte in the given class matches the part.
func (d *CompositeSequenceDFA) classMatchesPart(class int, part *charClassPart) bool {
	for b := 0; b < 256; b++ {
		if int(d.byteToClass[b]) == class {
			return part.membership[b]
		}
	}
	return false
}

// IsMatch returns true if the haystack contains a match.
func (d *CompositeSequenceDFA) IsMatch(haystack []byte) bool {
	_, _, ok := d.Search(haystack)
	return ok
}

// Search finds the first match in haystack.
// Returns (start, end, found).
func (d *CompositeSequenceDFA) Search(haystack []byte) (int, int, bool) {
	return d.SearchAt(haystack, 0)
}

// SearchAt finds the first match starting at or after position 'at'.
// Returns (start, end, found).
//
//nolint:gocognit // Loop unrolling for performance intentionally increases complexity
func (d *CompositeSequenceDFA) SearchAt(haystack []byte, at int) (int, int, bool) {
	n := len(haystack)
	if n == 0 {
		return -1, -1, false
	}

	// Copy to local vars to help compiler optimize
	byteToClass := d.byteToClass
	transitions := d.transitions
	numClasses := d.numClasses
	accepting := d.accepting

	// Skip positions where first byte doesn't match first part
	// Find the first byte class that can start a match
	firstPartClass := d.firstPartClasses()

	for start := at; start < n; start++ {
		// Fast skip: check if this position can start a match
		if !firstPartClass[haystack[start]] {
			continue
		}

		state := 0 // Dead state
		matchStart := start
		lastAcceptEnd := -1

		// First transition (we know first byte matches)
		class := int(byteToClass[haystack[start]])
		state = int(transitions[class]) // From dead state (0)

		if accepting[state] {
			lastAcceptEnd = start + 1
		}

		pos := start + 1

		// Unrolled loop: process 4 bytes at a time when possible
		for pos+3 < n {
			// Byte 1
			b0 := haystack[pos]
			class0 := int(byteToClass[b0])
			state = int(transitions[state*numClasses+class0])
			if state == 0 {
				if lastAcceptEnd > 0 {
					return matchStart, lastAcceptEnd, true
				}
				goto nextStart
			}
			if accepting[state] {
				lastAcceptEnd = pos + 1
			}

			// Byte 2
			b1 := haystack[pos+1]
			class1 := int(byteToClass[b1])
			state = int(transitions[state*numClasses+class1])
			if state == 0 {
				if lastAcceptEnd > 0 {
					return matchStart, lastAcceptEnd, true
				}
				goto nextStart
			}
			if accepting[state] {
				lastAcceptEnd = pos + 2
			}

			// Byte 3
			b2 := haystack[pos+2]
			class2 := int(byteToClass[b2])
			state = int(transitions[state*numClasses+class2])
			if state == 0 {
				if lastAcceptEnd > 0 {
					return matchStart, lastAcceptEnd, true
				}
				goto nextStart
			}
			if accepting[state] {
				lastAcceptEnd = pos + 3
			}

			// Byte 4
			b3 := haystack[pos+3]
			class3 := int(byteToClass[b3])
			state = int(transitions[state*numClasses+class3])
			if state == 0 {
				if lastAcceptEnd > 0 {
					return matchStart, lastAcceptEnd, true
				}
				goto nextStart
			}
			if accepting[state] {
				lastAcceptEnd = pos + 4
			}

			pos += 4
		}

		// Handle remaining bytes
		for pos < n {
			b := haystack[pos]
			class := int(byteToClass[b])
			idx := state*numClasses + class
			nextState := int(transitions[idx])

			if nextState == 0 {
				if lastAcceptEnd > 0 {
					return matchStart, lastAcceptEnd, true
				}
				break
			}

			state = nextState

			if accepting[state] {
				lastAcceptEnd = pos + 1
			}
			pos++
		}

		// End of input - check if we have an accepting position
		if lastAcceptEnd > 0 {
			return matchStart, lastAcceptEnd, true
		}

	nextStart:
	}

	return -1, -1, false
}

// firstPartClasses returns a lookup table indicating which bytes can start a match.
func (d *CompositeSequenceDFA) firstPartClasses() [256]bool {
	var result [256]bool
	if len(d.parts) == 0 {
		return result
	}
	// First part membership tells us which bytes can start a match
	return d.parts[0].membership
}

// IsCompositeSequenceDFAPattern checks if a pattern is suitable for CompositeSequenceDFA.
func IsCompositeSequenceDFAPattern(re *syntax.Regexp) bool {
	parts := extractCompositeCharClassParts(re)
	if len(parts) == 0 || len(parts) > 8 {
		return false
	}

	// Check all parts have minMatch >= 1
	for _, p := range parts {
		if p.minMatch == 0 {
			return false
		}
	}

	return true
}
