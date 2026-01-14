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
//
// Thread safety: BoundedBacktracker config is immutable after creation.
// Use BacktrackerState for per-search mutable state to enable concurrent usage.
type BoundedBacktracker struct {
	nfa *NFA

	// numStates is cached for bounds checking (immutable)
	numStates int

	// maxVisitedSize limits memory usage (in entries, not bits)
	// Default: 256 * 1024 = 256K entries = 1MB (immutable)
	maxVisitedSize int

	// internalState is used by legacy non-thread-safe methods.
	// For concurrent usage, use *WithState methods with external BacktrackerState.
	internalState BacktrackerState
}

// BacktrackerState holds mutable per-search state for BoundedBacktracker.
// This struct should be pooled (via sync.Pool) for concurrent usage.
// Each goroutine must use its own BacktrackerState instance.
type BacktrackerState struct {
	// Visited stores generation numbers for (state, position) pairs.
	// Layout: Visited[state * (InputLen+1) + pos] = generation when visited.
	// Using generation counter enables O(1) reset instead of O(n) clearing.
	Visited []uint32

	// Generation is incremented for each new search attempt.
	// A position is considered visited if Visited[idx] == Generation.
	Generation uint32

	// InputLen is cached for index calculations
	InputLen int

	// Longest enables leftmost-longest match semantics (POSIX/AWK compatibility).
	// When true, explores all branches to find the longest match instead of
	// returning on the first match found.
	Longest bool
}

// NewBoundedBacktracker creates a new bounded backtracker for the given NFA.
func NewBoundedBacktracker(nfa *NFA) *BoundedBacktracker {
	return &BoundedBacktracker{
		nfa:            nfa,
		numStates:      nfa.States(),
		maxVisitedSize: 256 * 1024, // 256K entries = 1MB (4 bytes per entry)
	}
}

// NewBacktrackerState creates a new mutable state for use with BoundedBacktracker.
// This should be pooled via sync.Pool for concurrent usage.
func NewBacktrackerState() *BacktrackerState {
	return &BacktrackerState{}
}

// SetLongest enables or disables leftmost-longest match semantics on internal state.
// When enabled, the backtracker finds the longest match at each position
// instead of returning on the first match found.
// Note: For thread-safe usage, set Longest directly on BacktrackerState.
func (b *BoundedBacktracker) SetLongest(longest bool) {
	b.internalState.Longest = longest
}

// NumStates returns the number of NFA states (for state allocation).
func (b *BoundedBacktracker) NumStates() int {
	return b.numStates
}

// MaxVisitedSize returns the maximum visited array size.
func (b *BoundedBacktracker) MaxVisitedSize() int {
	return b.maxVisitedSize
}

// CanHandle returns true if this engine can handle the given input size.
// Returns false if the visited array would exceed maxVisitedSize entries.
func (b *BoundedBacktracker) CanHandle(haystackLen int) bool {
	// Need (numStates * (haystackLen + 1)) entries
	entriesNeeded := b.numStates * (haystackLen + 1)
	return entriesNeeded <= b.maxVisitedSize
}

// reset prepares the given state for a new search.
// This is an internal method that operates on external state.
func (b *BoundedBacktracker) reset(state *BacktrackerState, haystackLen int) {
	state.InputLen = haystackLen

	// Calculate required size in entries
	entriesNeeded := b.numStates * (haystackLen + 1)

	// Reuse or allocate visited array
	if cap(state.Visited) >= entriesNeeded {
		state.Visited = state.Visited[:entriesNeeded]
	} else {
		state.Visited = make([]uint32, entriesNeeded)
		state.Generation = 0 // New array starts fresh
	}

	// Increment generation for fresh visited state (O(1) instead of O(n) clear)
	state.Generation++
	// Handle overflow by clearing array (rare, ~4B calls)
	if state.Generation == 0 {
		for i := range state.Visited {
			state.Visited[i] = 0
		}
		state.Generation = 1
	}
}

// shouldVisit checks if (state, pos) has been visited and marks it if not.
// Returns true if we should visit (not yet visited), false if already visited.
// This is the hot path - must be as fast as possible.
// This method operates on external state for thread safety.
func (b *BoundedBacktracker) shouldVisit(s *BacktrackerState, state StateID, pos int) bool {
	// Calculate index: state * (inputLen + 1) + pos
	idx := int(state)*(s.InputLen+1) + pos

	// Check if visited in current generation
	if s.Visited[idx] == s.Generation {
		return false // Already visited
	}
	s.Visited[idx] = s.Generation
	return true
}

// IsMatch returns true if the pattern matches anywhere in the haystack.
// This is optimized for boolean-only matching.
// This method uses internal state and is NOT thread-safe.
// For concurrent usage, use IsMatchWithState.
func (b *BoundedBacktracker) IsMatch(haystack []byte) bool {
	return b.IsMatchWithState(haystack, &b.internalState)
}

// IsMatchWithState returns true if the pattern matches anywhere in the haystack.
// This method uses external state and IS thread-safe when each goroutine uses its own state.
func (b *BoundedBacktracker) IsMatchWithState(haystack []byte, state *BacktrackerState) bool {
	if !b.CanHandle(len(haystack)) {
		return false // Caller should use PikeVM instead
	}

	b.reset(state, len(haystack))

	// Try to match starting at each position (unanchored)
	for startPos := 0; startPos <= len(haystack); startPos++ {
		if b.backtrackWithState(haystack, startPos, b.nfa.StartAnchored(), state) {
			return true
		}
	}
	return false
}

// IsMatchAnchored returns true if the pattern matches at the start of haystack.
// This method uses internal state and is NOT thread-safe.
func (b *BoundedBacktracker) IsMatchAnchored(haystack []byte) bool {
	return b.IsMatchAnchoredWithState(haystack, &b.internalState)
}

// IsMatchAnchoredWithState returns true if the pattern matches at the start of haystack.
// This method uses external state and IS thread-safe when each goroutine uses its own state.
func (b *BoundedBacktracker) IsMatchAnchoredWithState(haystack []byte, state *BacktrackerState) bool {
	if !b.CanHandle(len(haystack)) {
		return false
	}

	b.reset(state, len(haystack))
	return b.backtrackWithState(haystack, 0, b.nfa.StartAnchored(), state)
}

// Search finds the first match in the haystack.
// Returns (start, end, true) if found, (-1, -1, false) otherwise.
// This method uses internal state and is NOT thread-safe.
func (b *BoundedBacktracker) Search(haystack []byte) (int, int, bool) {
	return b.SearchAtWithState(haystack, 0, &b.internalState)
}

// SearchWithState finds the first match in the haystack.
// Returns (start, end, true) if found, (-1, -1, false) otherwise.
// This method uses external state and IS thread-safe when each goroutine uses its own state.
func (b *BoundedBacktracker) SearchWithState(haystack []byte, state *BacktrackerState) (int, int, bool) {
	return b.SearchAtWithState(haystack, 0, state)
}

// SearchAt finds the first match starting from position 'at'.
// Returns (start, end, true) if found, (-1, -1, false) otherwise.
// This is used by FindAll* operations for efficient iteration.
// In longest mode, finds the longest match at the leftmost position.
// This method uses internal state and is NOT thread-safe.
func (b *BoundedBacktracker) SearchAt(haystack []byte, at int) (int, int, bool) {
	return b.SearchAtWithState(haystack, at, &b.internalState)
}

// SearchAtWithState finds the first match starting from position 'at'.
// Returns (start, end, true) if found, (-1, -1, false) otherwise.
// This method uses external state and IS thread-safe when each goroutine uses its own state.
func (b *BoundedBacktracker) SearchAtWithState(haystack []byte, at int, state *BacktrackerState) (int, int, bool) {
	if !b.CanHandle(len(haystack)) {
		return -1, -1, false
	}

	b.reset(state, len(haystack))

	// Try to match starting at each position from 'at'
	for startPos := at; startPos <= len(haystack); startPos++ {
		var end int
		if state.Longest {
			end = b.backtrackFindLongestWithState(haystack, startPos, b.nfa.StartAnchored(), state)
		} else {
			end = b.backtrackFindWithState(haystack, startPos, b.nfa.StartAnchored(), state)
		}
		if end >= 0 {
			return startPos, end, true
		}
		// O(1) reset: increment generation instead of O(n) array clear
		// This is the key optimization that makes Search fast on large inputs
		state.Generation++
		// Handle overflow by resetting the array (rare, ~4B searches)
		if state.Generation == 0 {
			for i := range state.Visited {
				state.Visited[i] = 0
			}
			state.Generation = 1
		}
	}
	return -1, -1, false
}

// backtrackWithState performs recursive backtracking search for IsMatch.
// Returns true if a match is found from the given (pos, state).
// This method uses external state for thread safety.
//
//nolint:gocyclo,cyclop // complexity is inherent to state machine dispatch
func (b *BoundedBacktracker) backtrackWithState(haystack []byte, pos int, nfaState StateID, st *BacktrackerState) bool {
	// Check bounds
	if nfaState == InvalidState || int(nfaState) >= b.numStates {
		return false
	}

	// Check and mark visited
	if !b.shouldVisit(st, nfaState, pos) {
		return false
	}

	s := b.nfa.State(nfaState)
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
				return b.backtrackWithState(haystack, pos+1, next, st)
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
				return b.backtrackWithState(haystack, pos+1, tr.Next, st)
			}
		}
		return false

	case StateSplit:
		left, right := s.Split()
		// Try left branch first (greedy), then right
		return b.backtrackWithState(haystack, pos, left, st) || b.backtrackWithState(haystack, pos, right, st)

	case StateEpsilon:
		return b.backtrackWithState(haystack, pos, s.Epsilon(), st)

	case StateCapture:
		_, _, next := s.Capture()
		return b.backtrackWithState(haystack, pos, next, st)

	case StateLook:
		look, next := s.Look()
		if checkLookAssertion(look, haystack, pos) {
			return b.backtrackWithState(haystack, pos, next, st)
		}
		return false

	case StateRuneAny:
		// Match any rune (including newline)
		if pos < len(haystack) {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrackWithState(haystack, pos+width, s.RuneAny(), st)
			}
		}
		return false

	case StateRuneAnyNotNL:
		// Match any rune except newline
		if pos < len(haystack) && haystack[pos] != '\n' {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrackWithState(haystack, pos+width, s.RuneAnyNotNL(), st)
			}
		}
		return false

	case StateFail:
		return false
	}

	return false
}

// backtrackFindWithState performs recursive backtracking to find match end position.
// Returns end position if match found, -1 otherwise.
// This method uses external state for thread safety.
//
//nolint:gocyclo,cyclop // complexity is inherent to state machine dispatch
func (b *BoundedBacktracker) backtrackFindWithState(haystack []byte, pos int, nfaState StateID, st *BacktrackerState) int {
	// Check bounds
	if nfaState == InvalidState || int(nfaState) >= b.numStates {
		return -1
	}

	// Check and mark visited
	if !b.shouldVisit(st, nfaState, pos) {
		return -1
	}

	s := b.nfa.State(nfaState)
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
				return b.backtrackFindWithState(haystack, pos+1, next, st)
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
				return b.backtrackFindWithState(haystack, pos+1, tr.Next, st)
			}
		}
		return -1

	case StateSplit:
		left, right := s.Split()
		// Try left first, then right
		if end := b.backtrackFindWithState(haystack, pos, left, st); end >= 0 {
			return end
		}
		return b.backtrackFindWithState(haystack, pos, right, st)

	case StateEpsilon:
		return b.backtrackFindWithState(haystack, pos, s.Epsilon(), st)

	case StateCapture:
		_, _, next := s.Capture()
		return b.backtrackFindWithState(haystack, pos, next, st)

	case StateLook:
		look, next := s.Look()
		if checkLookAssertion(look, haystack, pos) {
			return b.backtrackFindWithState(haystack, pos, next, st)
		}
		return -1

	case StateRuneAny:
		if pos < len(haystack) {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrackFindWithState(haystack, pos+width, s.RuneAny(), st)
			}
		}
		return -1

	case StateRuneAnyNotNL:
		if pos < len(haystack) && haystack[pos] != '\n' {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrackFindWithState(haystack, pos+width, s.RuneAnyNotNL(), st)
			}
		}
		return -1

	case StateFail:
		return -1
	}

	return -1
}

// backtrackFindLongestWithState performs backtracking to find the longest match end position.
// Unlike backtrackFindWithState, this explores ALL branches at splits to find the longest match.
// Returns end position if match found, -1 otherwise.
// This method uses external state for thread safety.
//
//nolint:gocyclo,cyclop // complexity is inherent to state machine dispatch
func (b *BoundedBacktracker) backtrackFindLongestWithState(haystack []byte, pos int, nfaState StateID, st *BacktrackerState) int {
	// Check bounds
	if nfaState == InvalidState || int(nfaState) >= b.numStates {
		return -1
	}

	// Check and mark visited
	if !b.shouldVisit(st, nfaState, pos) {
		return -1
	}

	s := b.nfa.State(nfaState)
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
				return b.backtrackFindLongestWithState(haystack, pos+1, next, st)
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
				return b.backtrackFindLongestWithState(haystack, pos+1, tr.Next, st)
			}
		}
		return -1

	case StateSplit:
		left, right := s.Split()
		// For longest match: try BOTH branches and return the longer one
		leftEnd := b.backtrackFindLongestWithState(haystack, pos, left, st)
		rightEnd := b.backtrackFindLongestWithState(haystack, pos, right, st)

		// Return the longer match (or the one that matched if only one did)
		if leftEnd >= rightEnd {
			return leftEnd
		}
		return rightEnd

	case StateEpsilon:
		return b.backtrackFindLongestWithState(haystack, pos, s.Epsilon(), st)

	case StateCapture:
		_, _, next := s.Capture()
		return b.backtrackFindLongestWithState(haystack, pos, next, st)

	case StateLook:
		look, next := s.Look()
		if checkLookAssertion(look, haystack, pos) {
			return b.backtrackFindLongestWithState(haystack, pos, next, st)
		}
		return -1

	case StateRuneAny:
		if pos < len(haystack) {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrackFindLongestWithState(haystack, pos+width, s.RuneAny(), st)
			}
		}
		return -1

	case StateRuneAnyNotNL:
		if pos < len(haystack) && haystack[pos] != '\n' {
			width := runeWidth(haystack[pos:])
			if width > 0 {
				return b.backtrackFindLongestWithState(haystack, pos+width, s.RuneAnyNotNL(), st)
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
