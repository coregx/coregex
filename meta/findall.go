// Package meta implements the meta-engine orchestrator.
//
// findall.go contains FindAll*, Count, and FindSubmatch methods.

package meta

import (
	"sync/atomic"
)

// FindSubmatch returns the first match with capture group information.
// Returns nil if no match is found.
//
// Group 0 is always the entire match. Groups 1+ are explicit capture groups.
// Unmatched optional groups will have nil values.
//
// When a one-pass DFA is available (for anchored patterns), this method
// is 10-20x faster than PikeVM for capture group extraction.
//
// Example:
//
//	engine, _ := meta.Compile(`(\w+)@(\w+)\.(\w+)`)
//	match := engine.FindSubmatch([]byte("user@example.com"))
//	if match != nil {
//	    fmt.Println(match.Group(0)) // "user@example.com"
//	    fmt.Println(match.Group(1)) // "user"
//	    fmt.Println(match.Group(2)) // "example"
//	    fmt.Println(match.Group(3)) // "com"
//	}
func (e *Engine) FindSubmatch(haystack []byte) *MatchWithCaptures {
	return e.FindSubmatchAt(haystack, 0)
}

// FindSubmatchAt returns the first match with capture group information,
// starting from position 'at' in the haystack.
// Returns nil if no match is found.
//
// This method is used by ReplaceAll* operations to correctly handle anchors like ^.
// Unlike FindSubmatch, it takes the FULL haystack and a starting position.
// Thread-safe: uses pooled state for both OnePass cache and PikeVM.
func (e *Engine) FindSubmatchAt(haystack []byte, at int) *MatchWithCaptures {
	// Get pooled state first for thread-safe access
	state := e.getSearchState()
	defer e.putSearchState(state)

	// For position 0, try OnePass DFA if available (10-20x faster for anchored patterns)
	if at == 0 && e.onepass != nil && state.onepassCache != nil {
		atomic.AddUint64(&e.stats.OnePassSearches, 1)
		slots := e.onepass.Search(haystack, state.onepassCache)
		if slots != nil {
			// Convert flat slots [start0, end0, start1, end1, ...] to nested captures
			captures := slotsToCaptures(slots)
			return NewMatchWithCaptures(haystack, captures)
		}
		// OnePass failed (input doesn't match from position 0)
		// Fall through to PikeVM which can find match anywhere
	}

	atomic.AddUint64(&e.stats.NFASearches, 1)

	nfaMatch := state.pikevm.SearchWithCapturesAt(haystack, at)
	if nfaMatch == nil {
		return nil
	}

	return NewMatchWithCaptures(haystack, nfaMatch.Captures)
}

// slotsToCaptures converts flat slots [start0, end0, start1, end1, ...]
// to nested captures [[start0, end0], [start1, end1], ...].
func slotsToCaptures(slots []int) [][]int {
	numCaptures := len(slots) / 2
	captures := make([][]int, numCaptures)
	for i := 0; i < numCaptures; i++ {
		start := slots[i*2]
		end := slots[i*2+1]
		if start >= 0 && end >= 0 {
			captures[i] = []int{start, end}
		} else {
			captures[i] = nil // Unmatched capture
		}
	}
	return captures
}

// FindAllIndicesStreaming returns all non-overlapping match indices using streaming algorithm.
// For CharClassSearcher strategy, this uses single-pass state machine which is significantly
// faster than repeated FindIndicesAt calls (no per-match function call overhead).
//
// Returns slice of [2]int{start, end} pairs. Limit n (0=no limit) restricts match count.
// The results slice is reused if provided (pass nil for fresh allocation).
//
// This method is optimized for patterns like \w+, \d+, [a-z]+ where matches are frequent.
func (e *Engine) FindAllIndicesStreaming(haystack []byte, n int, results [][2]int) [][2]int {
	// Only CharClassSearcher benefits from streaming - others use standard loop
	if e.strategy != UseCharClassSearcher || e.charClassSearcher == nil {
		return e.findAllIndicesLoop(haystack, n, results)
	}

	// Use streaming state machine for CharClassSearcher
	allMatches := e.charClassSearcher.FindAllIndices(haystack, results)

	// Apply limit if specified
	if n > 0 && len(allMatches) > n {
		return allMatches[:n]
	}

	return allMatches
}

// findAllIndicesLoop is the standard loop-based FindAll for non-streaming strategies.
// Optimized: acquires SearchState once for entire loop to avoid sync.Pool overhead per match.
func (e *Engine) findAllIndicesLoop(haystack []byte, n int, results [][2]int) [][2]int {
	if results == nil {
		// Smart allocation: anchored patterns have max 1 match, others use capped heuristic.
		// This avoids huge allocations on large inputs (6MB → 62k capacity was causing 170µs overhead).
		var initCap int
		if e.isStartAnchored {
			initCap = 1 // Start-anchored patterns match at most once (position 0 only)
		} else {
			// Estimate ~1 match per 100 bytes, but cap at reasonable size to avoid
			// allocating megabytes for large inputs with few matches.
			initCap = len(haystack)/100 + 1
			if initCap > 256 {
				initCap = 256 // Cap at 256 to limit allocation overhead; append will grow if needed
			}
		}
		results = make([][2]int, 0, initCap)
	} else {
		results = results[:0]
	}

	pos := 0
	lastMatchEnd := -1

	// Get state ONCE for entire iteration - eliminates 1.29M sync.Pool ops for FindAll
	state := e.getSearchState()
	defer e.putSearchState(state)

	for n <= 0 || len(results) < n {
		start, end, found := e.findIndicesAtWithState(haystack, pos, state)
		if !found {
			break
		}

		// Skip empty matches that start exactly where the previous non-empty match ended.
		// This matches Go's stdlib behavior:
		// - "a*" on "ab" returns [[0 1] [2 2]], not [[0 1] [1 1] [2 2]]
		if start == end && start == lastMatchEnd {
			pos++
			if pos > len(haystack) {
				break
			}
			continue
		}

		results = append(results, [2]int{start, end})

		// Track non-empty match ends for the skip rule
		if start != end {
			lastMatchEnd = end
		}

		// Move position past this match
		switch {
		case start == end:
			// Empty match: advance by 1 to avoid infinite loop
			pos = end + 1
		case end > pos:
			pos = end
		default:
			pos++
		}

		if pos > len(haystack) {
			break
		}
	}

	return results
}

// Count returns the number of non-overlapping matches in the haystack.
//
// This is optimized for counting without allocating result slices.
// Uses early termination for boolean checks at each step.
// If n > 0, counts at most n matches. If n <= 0, counts all matches.
// Optimized: acquires SearchState once for entire loop to avoid sync.Pool overhead per match.
//
// Example:
//
//	engine, _ := meta.Compile(`\d+`)
//	count := engine.Count([]byte("1 2 3 4 5"), -1)
//	// count == 5
func (e *Engine) Count(haystack []byte, n int) int {
	if n == 0 {
		return 0
	}

	count := 0
	pos := 0
	lastNonEmptyEnd := -1

	// Get state ONCE for entire iteration - eliminates sync.Pool overhead per match
	state := e.getSearchState()
	defer e.putSearchState(state)

	for pos <= len(haystack) {
		// Use state-reusing version for zero sync.Pool overhead per match
		start, end, found := e.findIndicesAtWithState(haystack, pos, state)
		if !found {
			break
		}

		// Skip empty matches at lastNonEmptyEnd (stdlib behavior)
		//nolint:gocritic // badCond: intentional - checking empty match (start==end) at lastNonEmptyEnd
		if start == end && start == lastNonEmptyEnd {
			pos++
			if pos > len(haystack) {
				break
			}
			continue
		}

		count++

		// Track non-empty match ends
		if start != end {
			lastNonEmptyEnd = end
		}

		// Move position past this match
		switch {
		case start == end:
			// Empty match: advance by 1 to avoid infinite loop
			pos = end + 1
		case end > pos:
			pos = end
		default:
			pos++
		}

		// Check limit
		if n > 0 && count >= n {
			break
		}
	}

	return count
}

// FindAllSubmatch returns all successive matches with capture group information.
// If n > 0, returns at most n matches. If n <= 0, returns all matches.
//
// Example:
//
//	engine, _ := meta.Compile(`(\w+)@(\w+)\.(\w+)`)
//	matches := engine.FindAllSubmatch([]byte("a@b.c x@y.z"), -1)
//	// len(matches) == 2
func (e *Engine) FindAllSubmatch(haystack []byte, n int) []*MatchWithCaptures {
	if n == 0 {
		return nil
	}

	var matches []*MatchWithCaptures
	pos := 0

	for pos <= len(haystack) {
		// Use PikeVM for capture extraction
		atomic.AddUint64(&e.stats.NFASearches, 1)
		nfaMatch := e.pikevm.SearchWithCaptures(haystack[pos:])
		if nfaMatch == nil {
			break
		}

		// Adjust captures to absolute positions
		// Captures is [][]int where each element is [start, end] for a group
		adjustedCaptures := make([][]int, len(nfaMatch.Captures))
		for i, cap := range nfaMatch.Captures {
			if len(cap) >= 2 && cap[0] >= 0 {
				adjustedCaptures[i] = []int{pos + cap[0], pos + cap[1]}
			} else {
				adjustedCaptures[i] = nil // Unmatched group
			}
		}

		match := NewMatchWithCaptures(haystack, adjustedCaptures)
		matches = append(matches, match)

		// Move position past this match
		end := nfaMatch.End
		if end > 0 {
			pos += end
		} else {
			// Empty match: advance by 1 to avoid infinite loop
			pos++
		}

		// Check limit
		if n > 0 && len(matches) >= n {
			break
		}
	}

	return matches
}
