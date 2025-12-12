package nfa

// BoundedBacktracker implements a bounded backtracking regex matcher.
// It uses a generation-based visited tracking for (state, position) pairs,
// providing O(1) reset between search attempts instead of O(n) clearing.
//
// This engine is selected when:
//   - len(haystack) * nfa.States() <= maxVisitedSize (default 256KB)
//   - No prefilter is available (no good literals)
//   - Pattern doesn't benefit from DFA (simple character classes)
//
// BoundedBacktracker is 2-5x faster than PikeVM for patterns like \d+, \w+, [a-z]+.
type BoundedBacktracker struct {
	nfa *NFA

	// visited stores generation numbers for (state, position) pairs.
	// Layout: visited[state * (inputLen+1) + pos] = generation when visited.
	// Using generation counter enables O(1) reset instead of O(n) clearing.
	visited []uint32

	// generation is incremented for each new search attempt.
	// A position is considered visited if visited[idx] == generation.
	generation uint32

	// inputLen is cached for index calculations
	inputLen int

	// numStates is cached for bounds checking
	numStates int

	// maxVisitedSize limits memory usage (in entries, not bits)
	// Default: 256 * 1024 = 256K entries = 1MB
	maxVisitedSize int
}

// NewBoundedBacktracker creates a new bounded backtracker for the given NFA.
func NewBoundedBacktracker(nfa *NFA) *BoundedBacktracker {
	return &BoundedBacktracker{
		nfa:            nfa,
		numStates:      nfa.States(),
		maxVisitedSize: 256 * 1024, // 256K entries = 1MB (4 bytes per entry)
	}
}

// CanHandle returns true if this engine can handle the given input size.
// Returns false if the visited array would exceed maxVisitedSize entries.
func (b *BoundedBacktracker) CanHandle(haystackLen int) bool {
	// Need (numStates * (haystackLen + 1)) entries
	entriesNeeded := b.numStates * (haystackLen + 1)
	return entriesNeeded <= b.maxVisitedSize
}

// reset prepares the backtracker for a new search.
func (b *BoundedBacktracker) reset(haystackLen int) {
	b.inputLen = haystackLen

	// Calculate required size in entries
	entriesNeeded := b.numStates * (haystackLen + 1)

	// Reuse or allocate visited array
	if cap(b.visited) >= entriesNeeded {
		b.visited = b.visited[:entriesNeeded]
	} else {
		b.visited = make([]uint32, entriesNeeded)
		b.generation = 0 // New array starts fresh
	}

	// Increment generation for fresh visited state (O(1) instead of O(n) clear)
	b.generation++
	// Handle overflow by clearing array (rare, ~4B calls)
	if b.generation == 0 {
		for i := range b.visited {
			b.visited[i] = 0
		}
		b.generation = 1
	}
}

// shouldVisit checks if (state, pos) has been visited and marks it if not.
// Returns true if we should visit (not yet visited), false if already visited.
// This is the hot path - must be as fast as possible.
func (b *BoundedBacktracker) shouldVisit(state StateID, pos int) bool {
	// Calculate index: state * (inputLen + 1) + pos
	idx := int(state)*(b.inputLen+1) + pos

	// Check if visited in current generation
	if b.visited[idx] == b.generation {
		return false // Already visited
	}
	b.visited[idx] = b.generation
	return true
}

// IsMatch returns true if the pattern matches anywhere in the haystack.
// This is optimized for boolean-only matching.
func (b *BoundedBacktracker) IsMatch(haystack []byte) bool {
	if !b.CanHandle(len(haystack)) {
		return false // Caller should use PikeVM instead
	}

	b.reset(len(haystack))

	// Try to match starting at each position (unanchored)
	for startPos := 0; startPos <= len(haystack); startPos++ {
		if b.backtrack(haystack, startPos, b.nfa.StartAnchored()) {
			return true
		}
	}
	return false
}

// IsMatchAnchored returns true if the pattern matches at the start of haystack.
func (b *BoundedBacktracker) IsMatchAnchored(haystack []byte) bool {
	if !b.CanHandle(len(haystack)) {
		return false
	}

	b.reset(len(haystack))
	return b.backtrack(haystack, 0, b.nfa.StartAnchored())
}

// Search finds the first match in the haystack.
// Returns (start, end, true) if found, (-1, -1, false) otherwise.
func (b *BoundedBacktracker) Search(haystack []byte) (int, int, bool) {
	if !b.CanHandle(len(haystack)) {
		return -1, -1, false
	}

	b.reset(len(haystack))

	// Try to match starting at each position
	for startPos := 0; startPos <= len(haystack); startPos++ {
		if end := b.backtrackFind(haystack, startPos, b.nfa.StartAnchored()); end >= 0 {
			return startPos, end, true
		}
		// O(1) reset: increment generation instead of O(n) array clear
		// This is the key optimization that makes Search fast on large inputs
		b.generation++
		// Handle overflow by resetting the array (rare, ~4B searches)
		if b.generation == 0 {
			for i := range b.visited {
				b.visited[i] = 0
			}
			b.generation = 1
		}
	}
	return -1, -1, false
}

// backtrack performs recursive backtracking search for IsMatch.
// Returns true if a match is found from the given (pos, state).
//
//nolint:gocyclo,cyclop // complexity is inherent to state machine dispatch
func (b *BoundedBacktracker) backtrack(haystack []byte, pos int, state StateID) bool {
	// Check bounds
	if state == InvalidState || int(state) >= b.numStates {
		return false
	}

	// Check and mark visited
	if !b.shouldVisit(state, pos) {
		return false
	}

	s := b.nfa.State(state)
	if s == nil {
		return false
	}

	switch s.Kind() {
	case StateMatch:
		return true

	case StateByteRange:
		lo, hi, next := s.ByteRange()
		if pos < len(haystack) {
			c := haystack[pos]
			if c >= lo && c <= hi {
				return b.backtrack(haystack, pos+1, next)
			}
		}
		return false

	case StateSparse:
		if pos >= len(haystack) {
			return false
		}
		c := haystack[pos]
		for _, tr := range s.Transitions() {
			if c >= tr.Lo && c <= tr.Hi {
				return b.backtrack(haystack, pos+1, tr.Next)
			}
		}
		return false

	case StateSplit:
		left, right := s.Split()
		// Try left branch first (greedy), then right
		return b.backtrack(haystack, pos, left) || b.backtrack(haystack, pos, right)

	case StateEpsilon:
		return b.backtrack(haystack, pos, s.Epsilon())

	case StateCapture:
		_, _, next := s.Capture()
		return b.backtrack(haystack, pos, next)

	case StateLook:
		look, next := s.Look()
		if checkLookAssertion(look, haystack, pos) {
			return b.backtrack(haystack, pos, next)
		}
		return false

	case StateRuneAny:
		// Match any rune (including newline)
		if pos < len(haystack) {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrack(haystack, pos+width, s.RuneAny())
			}
		}
		return false

	case StateRuneAnyNotNL:
		// Match any rune except newline
		if pos < len(haystack) && haystack[pos] != '\n' {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrack(haystack, pos+width, s.RuneAnyNotNL())
			}
		}
		return false

	case StateFail:
		return false
	}

	return false
}

// backtrackFind performs recursive backtracking to find match end position.
// Returns end position if match found, -1 otherwise.
//
//nolint:gocyclo,cyclop // complexity is inherent to state machine dispatch
func (b *BoundedBacktracker) backtrackFind(haystack []byte, pos int, state StateID) int {
	// Check bounds
	if state == InvalidState || int(state) >= b.numStates {
		return -1
	}

	// Check and mark visited
	if !b.shouldVisit(state, pos) {
		return -1
	}

	s := b.nfa.State(state)
	if s == nil {
		return -1
	}

	switch s.Kind() {
	case StateMatch:
		return pos

	case StateByteRange:
		lo, hi, next := s.ByteRange()
		if pos < len(haystack) {
			c := haystack[pos]
			if c >= lo && c <= hi {
				return b.backtrackFind(haystack, pos+1, next)
			}
		}
		return -1

	case StateSparse:
		if pos >= len(haystack) {
			return -1
		}
		c := haystack[pos]
		for _, tr := range s.Transitions() {
			if c >= tr.Lo && c <= tr.Hi {
				return b.backtrackFind(haystack, pos+1, tr.Next)
			}
		}
		return -1

	case StateSplit:
		left, right := s.Split()
		// Try left first, then right
		if end := b.backtrackFind(haystack, pos, left); end >= 0 {
			return end
		}
		return b.backtrackFind(haystack, pos, right)

	case StateEpsilon:
		return b.backtrackFind(haystack, pos, s.Epsilon())

	case StateCapture:
		_, _, next := s.Capture()
		return b.backtrackFind(haystack, pos, next)

	case StateLook:
		look, next := s.Look()
		if checkLookAssertion(look, haystack, pos) {
			return b.backtrackFind(haystack, pos, next)
		}
		return -1

	case StateRuneAny:
		if pos < len(haystack) {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrackFind(haystack, pos+width, s.RuneAny())
			}
		}
		return -1

	case StateRuneAnyNotNL:
		if pos < len(haystack) && haystack[pos] != '\n' {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrackFind(haystack, pos+width, s.RuneAnyNotNL())
			}
		}
		return -1

	case StateFail:
		return -1
	}

	return -1
}

// runeWidth returns the width in bytes of the first UTF-8 rune in b.
// Returns 0 if b is empty.
func runeWidth(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	// Fast path for ASCII
	if b[0] < 0x80 {
		return 1
	}
	// Multi-byte UTF-8
	switch {
	case b[0]&0xE0 == 0xC0 && len(b) >= 2:
		return 2
	case b[0]&0xF0 == 0xE0 && len(b) >= 3:
		return 3
	case b[0]&0xF8 == 0xF0 && len(b) >= 4:
		return 4
	default:
		return 1 // Invalid UTF-8, treat as single byte
	}
}
