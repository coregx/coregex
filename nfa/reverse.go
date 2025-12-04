package nfa

// ReverseAnchored builds a reverse NFA that is anchored at start.
// This is used for patterns ending with $ anchor, where the reverse NFA
// should only match starting at position 0 of the reversed haystack.
//
// This is equivalent to Reverse() followed by marking the result as anchored.
func ReverseAnchored(forward *NFA) *NFA {
	return reverseWithOptions(forward, true)
}

// Reverse builds a reverse NFA from the given forward NFA.
//
// In a reverse NFA:
//   - Start and match states are swapped
//   - All transitions are reversed (A->B becomes B->A)
//   - The automaton matches patterns backward from the end
//
// This is used for reverse searching, which enables efficient
// matching of patterns with $ anchor by searching backward from
// the end of the haystack.
//
// Algorithm (Two-Pass):
//
//	Pass 1: Collect all reverse edges and allocate state IDs
//	Pass 2: Build actual states with correct transition targets
//
// Note: Reverse NFA does NOT support capture groups.
// Captures require forward search to maintain correct positions.
//
// Example:
//
//	Forward NFA for "abc":
//	  start -> a -> b -> c -> match
//
//	Reverse NFA:
//	  start(from match) -> c -> b -> a -> match(from start)
func Reverse(forward *NFA) *NFA {
	return reverseWithOptions(forward, false)
}

// reverseWithOptions builds a reverse NFA with configurable anchoring.
func reverseWithOptions(forward *NFA, anchored bool) *NFA {
	// PASS 0: Collect all reverse edges
	reverseEdges := collectReverseEdges(forward)

	// PASS 1: Pre-allocate all state IDs
	builder := NewBuilderWithCapacity(forward.States() + 2)
	revStateMap := make(map[StateID]StateID, forward.States())
	reverseMatchID := builder.AddMatch()

	// Map forward start states
	// CRITICAL FIX: For ReverseAnchored, we must NOT include the unanchored prefix (.*?)
	// because it creates spurious match paths in the reverse NFA, causing O(n*m) instead of O(m)
	fwdStartAnchored := forward.StartAnchored()
	fwdStartUnanchored := forward.StartUnanchored()
	mapStartStates(builder, reverseEdges, fwdStartAnchored, fwdStartUnanchored, reverseMatchID, revStateMap, anchored)

	// Allocate placeholder states for all other forward states
	// When anchored=true, we need to identify and skip unanchored prefix states
	unanchoredPrefixStates := make(map[StateID]bool)
	if anchored && fwdStartUnanchored != fwdStartAnchored {
		// Find states that are part of the unanchored prefix (.*?)
		// These are states reachable from startUnanchored but not from startAnchored
		unanchoredPrefixStates = findUnanchoredPrefixStates(forward, fwdStartAnchored, fwdStartUnanchored)
	}
	allocatePlaceholders(forward, builder, reverseEdges, revStateMap, unanchoredPrefixStates)

	// PASS 2: Fill in actual transitions
	// Pass anchored flag and skipStates to skip unanchored prefix
	fillAllTransitions(forward, builder, reverseEdges, fwdStartAnchored, fwdStartUnanchored, reverseMatchID, revStateMap, anchored, unanchoredPrefixStates)

	// Build reverse start states from forward match states
	forwardMatchIDs := collectMatchStates(forward)
	reverseStartAnchored, reverseStartUnanchored := buildReverseStarts(builder, forwardMatchIDs, revStateMap)
	builder.SetStarts(reverseStartAnchored, reverseStartUnanchored)

	// Build final reverse NFA
	return buildFinalNFA(builder, forward, anchored)
}

// collectReverseEdges builds reverse transition adjacency lists from forward NFA
func collectReverseEdges(forward *NFA) map[StateID][]reverseEdge {
	reverseEdges := make(map[StateID][]reverseEdge, forward.States())
	for it := forward.Iter(); it.HasNext(); {
		state := it.Next()
		from := state.ID()
		collectEdgesFromState(state, from, reverseEdges)
	}
	return reverseEdges
}

// collectEdgesFromState extracts reverse edges from a single state
func collectEdgesFromState(state *State, from StateID, reverseEdges map[StateID][]reverseEdge) {
	switch state.Kind() {
	case StateByteRange:
		lo, hi, to := state.ByteRange()
		if to != InvalidState {
			reverseEdges[to] = append(reverseEdges[to], reverseEdge{from: from, kind: edgeByteRange, lo: lo, hi: hi})
		}
	case StateSparse:
		for _, tr := range state.Transitions() {
			if tr.Next != InvalidState {
				reverseEdges[tr.Next] = append(reverseEdges[tr.Next], reverseEdge{from: from, kind: edgeSparse, lo: tr.Lo, hi: tr.Hi})
			}
		}
	case StateSplit:
		left, right := state.Split()
		if left != InvalidState {
			reverseEdges[left] = append(reverseEdges[left], reverseEdge{from: from, kind: edgeEpsilon})
		}
		if right != InvalidState {
			reverseEdges[right] = append(reverseEdges[right], reverseEdge{from: from, kind: edgeEpsilon})
		}
	case StateEpsilon:
		if to := state.Epsilon(); to != InvalidState {
			reverseEdges[to] = append(reverseEdges[to], reverseEdge{from: from, kind: edgeEpsilon})
		}
	case StateCapture:
		_, _, to := state.Capture()
		if to != InvalidState {
			reverseEdges[to] = append(reverseEdges[to], reverseEdge{from: from, kind: edgeEpsilon})
		}
	case StateLook:
		// Look assertions are zero-width, treat as epsilon for edge collection
		_, to := state.Look()
		if to != InvalidState {
			reverseEdges[to] = append(reverseEdges[to], reverseEdge{from: from, kind: edgeEpsilon})
		}
	}
}

// mapStartStates maps forward start states to reverse states
// When forAnchored is true, we skip the unanchored start state to avoid
// including the (?s:.)*? prefix loop which would create O(n*m) performance.
func mapStartStates(builder *Builder, reverseEdges map[StateID][]reverseEdge, fwdAnchored, fwdUnanchored, matchID StateID, revStateMap map[StateID]StateID, forAnchored bool) {
	anchoredHasIncoming := len(reverseEdges[fwdAnchored]) > 0
	revStateMap[fwdAnchored] = mapSingleStartState(builder, reverseEdges, fwdAnchored, matchID, anchoredHasIncoming)

	// CRITICAL: For ReverseAnchored (forAnchored=true), do NOT map unanchored start state
	// The unanchored prefix (.*?) loop creates spurious match paths in reverse NFA
	if !forAnchored && fwdUnanchored != fwdAnchored {
		unanchoredHasIncoming := len(reverseEdges[fwdUnanchored]) > 0
		revStateMap[fwdUnanchored] = mapSingleStartState(builder, reverseEdges, fwdUnanchored, matchID, unanchoredHasIncoming)
	}
}

// mapSingleStartState maps a single start state
func mapSingleStartState(builder *Builder, _ map[StateID][]reverseEdge, _ StateID, matchID StateID, hasIncoming bool) StateID {
	if hasIncoming {
		return builder.AddEpsilon(matchID)
	}
	return matchID
}

// allocatePlaceholders allocates placeholder states for all non-start forward states
// skipStates contains state IDs that should not be included (e.g., unanchored prefix)
func allocatePlaceholders(forward *NFA, builder *Builder, reverseEdges map[StateID][]reverseEdge, revStateMap map[StateID]StateID, skipStates map[StateID]bool) {
	for it := forward.Iter(); it.HasNext(); {
		state := it.Next()
		fwdID := state.ID()
		// Skip states in the unanchored prefix
		if skipStates[fwdID] {
			continue
		}
		if _, exists := revStateMap[fwdID]; !exists {
			edges := reverseEdges[fwdID]
			revStateMap[fwdID] = allocatePlaceholder(builder, edges)
		}
	}
}

// findUnanchoredPrefixStates identifies states that are part of the unanchored prefix (.*?)
// These are states reachable from startUnanchored but loop back to it (the .*? loop)
func findUnanchoredPrefixStates(nfa *NFA, _, startUnanchored StateID) map[StateID]bool {
	prefixStates := make(map[StateID]bool)

	// The unanchored prefix typically consists of:
	// startUnanchored (SPLIT) -> right branch -> anyByte (ByteRange) -> back to SPLIT
	// We want to find states that are ONLY in this loop, not reachable from startAnchored

	// Start with the unanchored start state
	prefixStates[startUnanchored] = true

	// Find the "right" branch of the split (the .*? loop part)
	// The left branch goes to the actual pattern (same as startAnchored path)
	for it := nfa.Iter(); it.HasNext(); {
		state := it.Next()
		if state.ID() == startUnanchored && state.Kind() == StateSplit {
			_, right := state.Split()
			// right is the anyByte state in the .*? loop
			// Find all states that loop back to startUnanchored
			findLoopStates(nfa, right, startUnanchored, prefixStates)
			break
		}
	}

	return prefixStates
}

// findLoopStates finds states that are part of a loop back to target
func findLoopStates(nfa *NFA, start, target StateID, result map[StateID]bool) {
	if start == InvalidState || result[start] {
		return
	}

	// Check if this state leads back to target (directly or indirectly)
	for it := nfa.Iter(); it.HasNext(); {
		state := it.Next()
		if state.ID() != start {
			continue
		}

		switch state.Kind() {
		case StateByteRange:
			_, _, next := state.ByteRange()
			if next == target {
				// This is part of the loop
				result[start] = true
			}
		case StateSplit:
			left, right := state.Split()
			if left == target || right == target {
				result[start] = true
			}
		case StateEpsilon:
			if state.Epsilon() == target {
				result[start] = true
			}
		}
		break
	}
}

// fillAllTransitions fills in actual transitions for all states
// When forAnchored is true, skip the unanchored prefix states entirely
func fillAllTransitions(forward *NFA, builder *Builder, reverseEdges map[StateID][]reverseEdge, fwdAnchored, fwdUnanchored, matchID StateID, revStateMap map[StateID]StateID, forAnchored bool, skipStates map[StateID]bool) {
	for it := forward.Iter(); it.HasNext(); {
		state := it.Next()
		fwdID := state.ID()

		// Skip states in the unanchored prefix (e.g., .*? loop)
		if skipStates[fwdID] {
			continue
		}

		isStart := fwdID == fwdAnchored || (!forAnchored && fwdID == fwdUnanchored)
		hasIncoming := len(reverseEdges[fwdID]) > 0

		if isStart && !hasIncoming {
			continue
		}

		revID, exists := revStateMap[fwdID]
		if !exists {
			continue // State not mapped (e.g., skipped unanchored start)
		}

		edges := reverseEdges[fwdID]

		if isStart && hasIncoming {
			fillStartStateWithIncoming(builder, revID, edges, revStateMap, matchID)
		} else {
			fillReverseState(builder, revID, edges, revStateMap)
		}
	}
}

// collectMatchStates collects all match state IDs from forward NFA
func collectMatchStates(forward *NFA) []StateID {
	var matchIDs []StateID
	for it := forward.Iter(); it.HasNext(); {
		state := it.Next()
		if state.IsMatch() {
			matchIDs = append(matchIDs, state.ID())
		}
	}
	return matchIDs
}

// buildFinalNFA builds the final reverse NFA from builder
func buildFinalNFA(builder *Builder, forward *NFA, anchored bool) *NFA {
	isAnchored := anchored || forward.IsAnchored()
	reverseNFA, err := builder.Build(
		WithUTF8(forward.IsUTF8()),
		WithAnchored(isAnchored),
		WithPatternCount(forward.PatternCount()),
		WithCaptureCount(0),
	)
	if err != nil {
		fallback := NewBuilder()
		failStart := fallback.AddFail()
		fallback.SetStarts(failStart, failStart)
		reverseNFA, _ = fallback.Build()
	}
	return reverseNFA
}

// reverseEdge represents an edge in the reverse transition graph
type reverseEdge struct {
	from StateID // source state in forward NFA
	kind edgeKind
	lo   byte // for byte range edges
	hi   byte // for byte range edges
}

type edgeKind uint8

const (
	edgeByteRange edgeKind = iota
	edgeSparse
	edgeEpsilon
)

// allocatePlaceholder allocates a state of the appropriate type based on edges
func allocatePlaceholder(builder *Builder, edges []reverseEdge) StateID {
	if len(edges) == 0 {
		return builder.AddFail()
	}

	byteRangeCount, epsilonCount := countEdgeTypes(edges)

	// Determine what kind of state we need
	if byteRangeCount == 0 && epsilonCount > 0 {
		// Pure epsilon - will be epsilon or split
		if epsilonCount == 1 {
			return builder.AddEpsilon(InvalidState)
		}
		// Multiple epsilon edges need a chain of splits
		// Allocate a split as the root
		return builder.AddSplit(InvalidState, InvalidState)
	}

	if byteRangeCount == 1 && epsilonCount == 0 {
		// Single byte range
		return builder.AddByteRange(0, 0, InvalidState)
	}

	// Multiple byte ranges or mixed - need sparse + possibly splits
	// Allocate sparse as placeholder
	return builder.AddSparse([]Transition{{Lo: 0, Hi: 0, Next: InvalidState}})
}

// countEdgeTypes counts byte range and epsilon edges
func countEdgeTypes(edges []reverseEdge) (byteRange, epsilon int) {
	for _, edge := range edges {
		switch edge.kind {
		case edgeByteRange, edgeSparse:
			byteRange++
		case edgeEpsilon:
			epsilon++
		}
	}
	return
}

// fillReverseState fills in the actual transitions for a reverse state
func fillReverseState(builder *Builder, revID StateID, edges []reverseEdge, revStateMap map[StateID]StateID) {
	if len(edges) == 0 {
		return // Already a FAIL state
	}

	// Separate edges by type
	var byteRangeEdges, epsilonEdges []reverseEdge
	for _, edge := range edges {
		switch edge.kind {
		case edgeByteRange, edgeSparse:
			byteRangeEdges = append(byteRangeEdges, edge)
		case edgeEpsilon:
			epsilonEdges = append(epsilonEdges, edge)
		}
	}

	// Handle pure epsilon case
	if len(byteRangeEdges) == 0 {
		fillEpsilonState(builder, revID, epsilonEdges, revStateMap)
		return
	}

	// Handle single byte range without epsilon
	if len(byteRangeEdges) == 1 && len(epsilonEdges) == 0 {
		edge := byteRangeEdges[0]
		revTarget := revStateMap[edge.from]
		updateByteRangeState(builder, revID, edge.lo, edge.hi, revTarget)
		return
	}

	// Handle multiple byte ranges (with or without epsilon)
	// Note: epsilon edges are ignored when we have byte ranges (sparse states can't have epsilon)
	fillSparseState(builder, revID, byteRangeEdges, revStateMap)
}

// fillStartStateWithIncoming handles forward start states that have incoming edges (loops)
// The proxy state is already an epsilon -> match, but we need to add the loop transitions
func fillStartStateWithIncoming(builder *Builder, proxyID StateID, edges []reverseEdge, revStateMap map[StateID]StateID, matchID StateID) {
	// The proxy is currently epsilon -> match
	// If we have incoming edges (from loops), we need to create a split:
	// proxyID: split -> (transitions from incoming edges), match

	// Collect targets from incoming edges
	var loopTargets []StateID
	for _, edge := range edges {
		if revTarget, ok := revStateMap[edge.from]; ok {
			loopTargets = append(loopTargets, revTarget)
		}
	}

	if len(loopTargets) == 0 {
		// No actual targets, keep the epsilon -> match
		return
	}

	// We need to convert the proxy into a split that goes to both:
	// 1. The loop targets (to continue matching)
	// 2. The match state (to accept)

	// For a single loop target: split -> loopTarget, match
	// For multiple loop targets: split -> split(targets...), match
	if len(loopTargets) == 1 {
		// Change proxy from epsilon to split
		s := &builder.states[proxyID]
		s.kind = StateSplit
		s.left = loopTargets[0]
		s.right = matchID
		s.next = InvalidState // Clear epsilon target
	} else {
		// Multiple loop targets - build a chain
		loopChain := buildSplitChain(builder, loopTargets)
		s := &builder.states[proxyID]
		s.kind = StateSplit
		s.left = loopChain
		s.right = matchID
		s.next = InvalidState
	}
}

// fillEpsilonState fills a state for pure epsilon transitions
func fillEpsilonState(builder *Builder, revID StateID, epsilonEdges []reverseEdge, revStateMap map[StateID]StateID) {
	if len(epsilonEdges) == 1 {
		revTarget := revStateMap[epsilonEdges[0].from]
		_ = builder.Patch(revID, revTarget)
		return
	}

	// Multiple epsilon edges - need split chain
	// The revID should be a split - update it
	targets := make([]StateID, 0, len(epsilonEdges))
	for _, edge := range epsilonEdges {
		if revTarget, ok := revStateMap[edge.from]; ok {
			targets = append(targets, revTarget)
		}
	}

	if len(targets) == 0 {
		return
	}

	if len(targets) == 1 {
		_ = builder.Patch(revID, targets[0])
		return
	}

	// Build split chain starting from revID
	// For multiple targets, we need to build: revID -> split(t0, split(t1, split(t2, t3)))
	// But we already allocated revID as a split, so just patch it
	if len(targets) == 2 {
		_ = builder.PatchSplit(revID, targets[0], targets[1])
		return
	}

	// More than 2 targets - build additional splits
	right := buildSplitChain(builder, targets[1:])
	_ = builder.PatchSplit(revID, targets[0], right)
}

// fillSparseState fills a sparse state with byte range transitions
func fillSparseState(builder *Builder, revID StateID, byteRangeEdges []reverseEdge, revStateMap map[StateID]StateID) {
	// Build transitions
	transitions := make([]Transition, 0, len(byteRangeEdges))
	for _, edge := range byteRangeEdges {
		revTarget := revStateMap[edge.from]
		transitions = append(transitions, Transition{
			Lo:   edge.lo,
			Hi:   edge.hi,
			Next: revTarget,
		})
	}

	// Update the sparse state in place
	// Since Builder doesn't have PatchSparse, we need to recreate the state
	// This is a limitation - we'll need to add a method or use a different approach

	// For now, we'll directly modify the builder's internal state
	// This is a hack but necessary given the current API
	updateSparseState(builder, revID, transitions)

	// If we have epsilon edges, we need to create additional structure
	// But sparse states can't have epsilon edges directly
	// We would need to wrap in a split - but that changes the state ID
	// For now, ignore epsilon edges when we have byte ranges (this is a limitation)
}

// updateByteRangeState updates a byte range state's fields
func updateByteRangeState(builder *Builder, stateID StateID, lo, hi byte, next StateID) {
	if int(stateID) >= builder.States() {
		return
	}
	s := &builder.states[stateID]
	switch s.kind {
	case StateByteRange:
		s.lo = lo
		s.hi = hi
		s.next = next
	case StateEpsilon:
		// Convert epsilon to byte range
		s.kind = StateByteRange
		s.lo = lo
		s.hi = hi
		s.next = next
	}
}

// updateSparseState updates a sparse state's transitions
func updateSparseState(builder *Builder, stateID StateID, transitions []Transition) {
	if int(stateID) >= builder.States() {
		return
	}
	s := &builder.states[stateID]
	switch s.kind {
	case StateSparse:
		s.transitions = transitions
	case StateByteRange, StateEpsilon:
		// Convert to sparse
		s.kind = StateSparse
		s.transitions = transitions
	}
}

// buildSplitChain builds a chain of split states
func buildSplitChain(builder *Builder, targets []StateID) StateID {
	if len(targets) == 0 {
		return builder.AddFail()
	}
	if len(targets) == 1 {
		return targets[0]
	}
	if len(targets) == 2 {
		return builder.AddSplit(targets[0], targets[1])
	}

	// Build binary tree for >2 targets
	right := buildSplitChain(builder, targets[1:])
	return builder.AddSplit(targets[0], right)
}

// buildReverseStarts creates reverse start states from forward match states
func buildReverseStarts(
	builder *Builder,
	forwardMatchIDs []StateID,
	revStateMap map[StateID]StateID,
) (anchored, unanchored StateID) {
	switch len(forwardMatchIDs) {
	case 0:
		// No match states - create fail start
		fail := builder.AddFail()
		return fail, fail

	case 1:
		// Single match - use its reverse state as start
		start := revStateMap[forwardMatchIDs[0]]
		return start, start

	default:
		// Multiple matches - create split to all reverse states
		var reverseStartStates []StateID
		for _, fwdMatchID := range forwardMatchIDs {
			if revID, ok := revStateMap[fwdMatchID]; ok {
				reverseStartStates = append(reverseStartStates, revID)
			}
		}
		start := buildSplitChain(builder, reverseStartStates)
		return start, start
	}
}
