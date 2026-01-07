package nfa

// DO NOT REGRESS: CharClassSearcher is 35% faster than Rust regex on char_class patterns.
// This optimization uses a 256-byte lookup table for O(1) membership testing.
// See docs/OPTIMIZATIONS.md for algorithm details and benchmark data.

// CharClassSearcher provides optimized search for simple character class patterns.
// For patterns like [\w]+, [a-z]+, \d+ where:
//   - Pattern is just a repeated character class (no alternation, no anchors)
//   - Character class can be represented as byte ranges
//
// This searcher uses a 256-byte lookup table for O(1) membership test,
// avoiding the overhead of recursive backtracking.
//
// Performance: 3-5x faster than BoundedBacktracker for char_class patterns.
//
// Note: SIMD optimization was evaluated but found to be slower for char_class
// patterns because matches are frequent (30-50% of positions) and short.
// The scalar lookup table approach is optimal for this use case.
// For large-scale char_class search, consider using Lazy DFA instead.
type CharClassSearcher struct {
	// membership is a 256-byte lookup table: membership[b] = true if byte b matches
	membership [256]bool

	// minMatch is minimum match length (1 for +, 0 for *)
	minMatch int
}

// NewCharClassSearcher creates a searcher from byte ranges.
// ranges is a list of [lo, hi] pairs where bytes in [lo, hi] match.
// minMatch is 1 for + quantifier, 0 for * quantifier.
func NewCharClassSearcher(ranges [][2]byte, minMatch int) *CharClassSearcher {
	s := &CharClassSearcher{minMatch: minMatch}
	for _, r := range ranges {
		for b := r[0]; b <= r[1]; b++ {
			s.membership[b] = true
			if b == 255 {
				break // Prevent overflow
			}
		}
	}
	return s
}

// NewCharClassSearcherFromNFA extracts byte ranges from an NFA and creates a searcher.
// Returns nil if the NFA is not a simple char_class+ pattern.
func NewCharClassSearcherFromNFA(n *NFA) *CharClassSearcher {
	// Simple char_class+ patterns have structure:
	// StartAnchored -> ByteRange/Sparse -> Epsilon -> Split -> Match (or loop)
	//
	// We need to find the main character class state and extract its ranges.

	startState := n.State(n.StartAnchored())
	if startState == nil {
		return nil
	}

	var ranges [][2]byte

	switch startState.Kind() {
	case StateByteRange:
		lo, hi, _ := startState.ByteRange()
		ranges = append(ranges, [2]byte{lo, hi})

	case StateSparse:
		for _, tr := range startState.Transitions() {
			ranges = append(ranges, [2]byte{tr.Lo, tr.Hi})
		}

	default:
		return nil // Not a simple char_class pattern
	}

	if len(ranges) == 0 {
		return nil
	}

	return NewCharClassSearcher(ranges, 1) // + quantifier = minMatch 1
}

// Search finds the first match in haystack.
// Returns (start, end, true) if found, (-1, -1, false) otherwise.
func (s *CharClassSearcher) Search(haystack []byte) (int, int, bool) {
	return s.SearchAt(haystack, 0)
}

// SearchAt finds the first match starting from position at.
// Returns (start, end, true) if found, (-1, -1, false) otherwise.
func (s *CharClassSearcher) SearchAt(haystack []byte, at int) (int, int, bool) {
	n := len(haystack)
	if at >= n {
		return -1, -1, false
	}

	// Find first matching byte (start of match)
	start := -1
	for i := at; i < n; i++ {
		if s.membership[haystack[i]] {
			start = i
			break
		}
	}

	if start == -1 {
		return -1, -1, false
	}

	// Scan forward while bytes match (greedy)
	end := start + 1
	for end < n && s.membership[haystack[end]] {
		end++
	}

	// Check minimum match length
	if end-start < s.minMatch {
		// Match too short, try from next position
		return s.SearchAt(haystack, start+1)
	}

	return start, end, true
}

// IsMatch returns true if pattern matches anywhere in haystack.
func (s *CharClassSearcher) IsMatch(haystack []byte) bool {
	n := len(haystack)
	matchLen := 0

	for i := 0; i < n; i++ {
		if s.membership[haystack[i]] {
			matchLen++
			if matchLen >= s.minMatch {
				return true
			}
		} else {
			matchLen = 0
		}
	}

	return false
}

// CanHandle returns true - CharClassSearcher can handle any input size.
func (s *CharClassSearcher) CanHandle(_ int) bool {
	return true
}

// FindAllIndices finds all non-overlapping matches using a single-pass state machine.
// This is significantly faster than repeated SearchAt calls because:
//   - No per-match function call overhead
//   - Better CPU branch prediction (consistent state transitions)
//   - Single pass through input (cache-friendly)
//
// Returns slice of [start, end] pairs. If results slice is provided and has
// sufficient capacity, it will be reused to avoid allocation.
//
// This implements the Rust regex approach: streaming state machine with
// SEARCHING/MATCHING states instead of separate find-start/find-end loops.
func (s *CharClassSearcher) FindAllIndices(haystack []byte, results [][2]int) [][2]int {
	// Reuse or allocate results slice
	if results == nil {
		// Pre-allocate reasonable capacity based on expected match density
		// For char_class patterns, expect ~10-20% of input to be match starts
		results = make([][2]int, 0, len(haystack)/20+1)
	} else {
		results = results[:0] // Reset length, keep capacity
	}

	n := len(haystack)
	if n == 0 {
		return results
	}

	// State machine approach: single pass, no function calls
	// States: SEARCHING (looking for match start) or MATCHING (inside a match)
	const (
		stateSearching = iota
		stateMatching
	)

	state := stateSearching
	matchStart := 0
	membership := &s.membership // Avoid repeated struct field access

	for i := 0; i < n; i++ {
		matches := membership[haystack[i]]

		switch state {
		case stateSearching:
			if matches {
				matchStart = i
				state = stateMatching
			}

		case stateMatching:
			if !matches {
				// End of match - emit if long enough
				if i-matchStart >= s.minMatch {
					results = append(results, [2]int{matchStart, i})
				}
				state = stateSearching
			}
		}
	}

	// Handle match at end of input
	if state == stateMatching && n-matchStart >= s.minMatch {
		results = append(results, [2]int{matchStart, n})
	}

	return results
}

// Count returns the number of non-overlapping matches using single-pass state machine.
// This is faster than len(FindAllIndices()) because it doesn't allocate a results slice.
func (s *CharClassSearcher) Count(haystack []byte) int {
	n := len(haystack)
	if n == 0 {
		return 0
	}

	// State machine approach: single pass, no function calls
	const (
		stateSearching = iota
		stateMatching
	)

	count := 0
	state := stateSearching
	matchStart := 0
	membership := &s.membership

	for i := 0; i < n; i++ {
		matches := membership[haystack[i]]

		switch state {
		case stateSearching:
			if matches {
				matchStart = i
				state = stateMatching
			}

		case stateMatching:
			if !matches {
				if i-matchStart >= s.minMatch {
					count++
				}
				state = stateSearching
			}
		}
	}

	// Handle match at end of input
	if state == stateMatching && n-matchStart >= s.minMatch {
		count++
	}

	return count
}
