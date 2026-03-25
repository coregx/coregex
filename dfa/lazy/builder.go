package lazy

import (
	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// Builder constructs a Lazy DFA from an NFA with optional prefilter integration.
//
// The builder performs the initial setup:
//  1. Analyze NFA for complexity
//  2. Extract literals for prefilter
//  3. Create initial start state
//  4. Set up cache
//
// The actual determinization happens lazily during search in the DFA.
type Builder struct {
	nfa    *nfa.NFA
	config Config

	// hasWordBoundary is true if the NFA contains \b or \B assertions.
	// When false, moveWithWordContext skips expensive resolveWordBoundaries calls.
	// This optimization provides ~4x speedup for patterns without word boundaries.
	hasWordBoundary bool
}

// NewBuilder creates a new DFA builder for the given NFA.
// This constructor checks the NFA for word boundary assertions.
func NewBuilder(n *nfa.NFA, config Config) *Builder {
	b := &Builder{
		nfa:    n,
		config: config,
	}
	b.hasWordBoundary = b.checkHasWordBoundary()
	return b
}

// NewBuilderWithWordBoundary creates a new DFA builder with pre-computed word boundary flag.
// This avoids re-scanning the NFA when the caller already knows whether it has word boundaries.
// Used by DFA.determinize() for performance (avoids O(states) scan on every byte transition).
func NewBuilderWithWordBoundary(n *nfa.NFA, config Config, hasWordBoundary bool) *Builder {
	return &Builder{
		nfa:             n,
		config:          config,
		hasWordBoundary: hasWordBoundary,
	}
}

// Build constructs and returns a Lazy DFA ready for searching.
// Returns error if configuration is invalid.
func (b *Builder) Build() (*DFA, error) {
	// Validate configuration
	if err := b.config.Validate(); err != nil {
		return nil, err
	}

	// Build prefilter if enabled
	var pf prefilter.Prefilter
	if b.config.UsePrefilter {
		pf = b.buildPrefilter()
	}

	// Check if the NFA contains word boundary assertions
	hasWordBoundary := b.checkHasWordBoundary()

	// Check if the pattern is always anchored (has ^ prefix)
	isAlwaysAnchored := b.nfa.IsAlwaysAnchored()

	// Build the immutable start byte map
	var startByteMap [256]StartKind
	initByteMap(&startByteMap)

	// Create DFA — fully immutable after this point
	dfa := &DFA{
		nfa:              b.nfa,
		config:           b.config,
		prefilter:        pf,
		pikevm:           nfa.NewPikeVM(b.nfa),
		byteClasses:      b.nfa.ByteClasses(),
		unanchoredStart:  b.nfa.StartUnanchored(),
		hasWordBoundary:  hasWordBoundary,
		isAlwaysAnchored: isAlwaysAnchored,
		startByteMap:     startByteMap,
	}

	return dfa, nil
}

// buildPrefilter extracts literals from the NFA and builds a prefilter.
// Returns nil if no suitable prefilter can be constructed.
//
// For MVP, prefilter extraction from NFA is not implemented.
// Future enhancement: Walk NFA to extract literal sequences.
func (b *Builder) buildPrefilter() prefilter.Prefilter {
	// TODO: Extract literals from NFA
	// For now, we don't have NFA → syntax.Regexp conversion
	// This will be implemented when we add regex compilation
	// For MVP, prefilter is optional

	// Placeholder: extract from pattern if available
	// In a full implementation, we would:
	// 1. Walk NFA to reconstruct literal sequences
	// 2. Use literal.Extractor to extract prefixes/suffixes
	// 3. Use prefilter.Builder to select optimal strategy

	// No prefilter for MVP - not an error condition
	return nil
}

// epsilonClosure computes the epsilon-closure of a set of NFA states.
//
// The epsilon-closure is the set of all NFA states reachable from the input
// states via epsilon transitions (Split, Epsilon states) and satisfied
// look-around assertions (StateLook).
//
// This is a fundamental operation in NFA → DFA conversion.
//
// The lookHave parameter specifies which look assertions are currently satisfied.
// For example, at the start of input both LookStartText and LookStartLine
// are satisfied. StateLook transitions are only followed if their assertion
// is in the lookHave set.
//
// Algorithm: Iterative DFS with visited set
//  1. Start with input states
//  2. Follow all epsilon transitions (Split, Epsilon)
//  3. Follow StateLook transitions only if assertion is satisfied
//  4. Collect all reachable states
//  5. Return sorted list for consistent ordering
func (b *Builder) epsilonClosure(states []nfa.StateID, lookHave LookSet) []nfa.StateID {
	// Use pooled StateSet for efficient membership testing and deduplication
	closure := acquireStateSet()
	defer releaseStateSet(closure)
	stack := make([]nfa.StateID, 0, len(states)*2)

	// Initialize with input states
	for _, sid := range states {
		if !closure.Contains(sid) {
			closure.Add(sid)
			stack = append(stack, sid)
		}
	}

	// DFS through epsilon transitions
	for len(stack) > 0 {
		// Pop from stack
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// Get NFA state
		state := b.nfa.State(current)
		if state == nil {
			continue
		}

		// Follow epsilon transitions
		switch state.Kind() {
		case nfa.StateEpsilon:
			next := state.Epsilon()
			if next != nfa.InvalidState && !closure.Contains(next) {
				closure.Add(next)
				stack = append(stack, next)
			}

		case nfa.StateSplit:
			left, right := state.Split()
			if left != nfa.InvalidState && !closure.Contains(left) {
				closure.Add(left)
				stack = append(stack, left)
			}
			if right != nfa.InvalidState && !closure.Contains(right) {
				closure.Add(right)
				stack = append(stack, right)
			}

		case nfa.StateLook:
			// CRITICAL: Only follow if the look assertion is satisfied
			// This is the key fix for proper ^ and $ handling in DFA.
			// Without this check, the DFA would incorrectly match patterns
			// like "^abc" at any position in the input.
			look, next := state.Look()
			if lookHave.Contains(look) && next != nfa.InvalidState && !closure.Contains(next) {
				closure.Add(next)
				stack = append(stack, next)
			}

		case nfa.StateCapture:
			// Capture states are epsilon transitions that record positions.
			// The DFA ignores captures (it only tracks match/no-match),
			// but we must follow through to reach the actual consuming states.
			// Fix for Issue #15: DFA.IsMatch returns false for patterns with capture groups.
			_, _, next := state.Capture()
			if next != nfa.InvalidState && !closure.Contains(next) {
				closure.Add(next)
				stack = append(stack, next)
			}
		}
	}

	// Return sorted slice for consistent state keys
	return closure.ToSlice()
}

// moveWithWordContext computes the set of NFA states reachable from the given states on input byte b,
// with full word boundary tracking.
//
// This is the core determinization operation:
//  1. Resolve word boundary assertions based on isFromWord and current byte
//  2. For each NFA state in the resolved set
//  3. Check if it has a transition on byte b
//  4. Collect all target states
//  5. Compute epsilon-closure of targets with appropriate look assertions
//  6. Return the resulting state set
//
// The look assertions after a byte transition depend on:
//   - Line context: After '\n', LookStartLine is satisfied (multiline ^)
//   - Word context: Compare isFromWord with isWordByte(input) for \b/\B
//
// isFromWord indicates whether the PREVIOUS byte (before this transition) was a word char.
// This is used to compute word boundary assertions:
//   - If isFromWord != isWordByte(input) → word boundary (\b) satisfied
//   - If isFromWord == isWordByte(input) → non-word boundary (\B) satisfied
//
// This effectively simulates one step of the NFA for all active states.
func (b *Builder) moveWithWordContext(states []nfa.StateID, input byte, isFromWord bool) []nfa.StateID {
	// Fast path: skip word boundary resolution if NFA has no word boundaries.
	// This optimization eliminates ~74% of allocations for patterns without \b/\B.
	// Based on Rust regex-automata approach: only resolve boundaries when needed.
	var resolvedStates []nfa.StateID
	if !b.hasWordBoundary {
		// No word boundaries - use states directly, skip expensive resolution
		resolvedStates = states
	} else {
		// Compute word boundary status for this transition
		isCurrentWord := isWordByte(input)
		wordBoundarySatisfied := isFromWord != isCurrentWord

		// Step 1: Resolve word boundary assertions in the current state set.
		// StateLook(\b) and StateLook(\B) that weren't followed during epsilon closure
		// need to be resolved now that we know the current byte.
		resolvedStates = b.resolveWordBoundaries(states, wordBoundarySatisfied)
	}

	// Step 2: Collect target states for this input byte (use pooled StateSet)
	targets := acquireStateSet()

	for _, sid := range resolvedStates {
		state := b.nfa.State(sid)
		if state == nil {
			continue
		}

		switch state.Kind() {
		case nfa.StateByteRange:
			lo, hi, next := state.ByteRange()
			if input >= lo && input <= hi {
				targets.Add(next)
			}

		case nfa.StateSparse:
			for _, tr := range state.Transitions() {
				if input >= tr.Lo && input <= tr.Hi {
					targets.Add(tr.Next)
				}
			}
		}
	}

	// No transitions on this byte
	if targets.Len() == 0 {
		releaseStateSet(targets)
		return nil
	}

	return b.completeMove(targets, input)
}

// completeMove finishes the move operation: computes look assertions from the
// input byte and runs epsilon closure on the target state set.
func (b *Builder) completeMove(targets *StateSet, input byte) []nfa.StateID {
	// Determine look assertions satisfied after this byte transition.
	// Only line assertions (^, $) — word boundary handled by resolveWordBoundaries.
	var lookAfter LookSet
	if input == '\n' {
		lookAfter = LookStartLine
	}

	targetSlice := targets.ToSlice()
	releaseStateSet(targets)
	return b.epsilonClosure(targetSlice, lookAfter)
}

// resolveWordBoundaries expands the NFA state set by following word boundary assertions
// (StateLook(\b) and StateLook(\B)) that are now satisfied.
//
// This is necessary because word boundary assertions can't be resolved during initial
// epsilon closure - they require knowledge of BOTH the previous byte (isFromWord) AND
// the current byte being consumed. This function is called during move() after we know
// both bytes.
//
// The expansion follows:
//   - StateLook(\b) if wordBoundarySatisfied is true
//   - StateLook(\B) if wordBoundarySatisfied is false
//   - Epsilon and Split transitions (to reach consuming states after word boundaries)
//
// This enables patterns like \bword to work correctly:
//  1. Start state contains StateLook(\b) but not states after it
//  2. When consuming 'w', we check word boundary (satisfied at word start)
//  3. resolveWordBoundaries follows StateLook(\b) → ByteRange('w')
//  4. Now ByteRange('w') can match and continue
//
// IMPORTANT: This function only expands states reachable by CROSSING a word boundary assertion.
// It does NOT follow epsilon/split transitions from states that haven't crossed a word boundary.
// This prevents false matches in patterns without word boundaries (like `a*`).
func (b *Builder) resolveWordBoundaries(states []nfa.StateID, wordBoundarySatisfied bool) []nfa.StateID {
	// First, find states reachable by crossing a word boundary assertion
	// We track states that have crossed a boundary separately (use pooled StateSet)
	crossedBoundary := acquireStateSet()
	stack := make([]nfa.StateID, 0, len(states))

	// Start by looking for word boundary assertions in input states
	for _, sid := range states {
		state := b.nfa.State(sid)
		if state == nil {
			continue
		}
		if state.Kind() == nfa.StateLook {
			look, next := state.Look()
			if next == nfa.InvalidState {
				continue
			}
			// Check if word boundary assertion is satisfied
			switch look {
			case nfa.LookWordBoundary:
				if wordBoundarySatisfied && !crossedBoundary.Contains(next) {
					crossedBoundary.Add(next)
					stack = append(stack, next)
				}
			case nfa.LookNoWordBoundary:
				if !wordBoundarySatisfied && !crossedBoundary.Contains(next) {
					crossedBoundary.Add(next)
					stack = append(stack, next)
				}
			}
		}
	}

	// If no word boundary was crossed, return original states unchanged
	if len(stack) == 0 {
		releaseStateSet(crossedBoundary)
		return states
	}

	// Now expand states reachable from crossed boundaries via epsilon/split
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		state := b.nfa.State(current)
		if state == nil {
			continue
		}

		switch state.Kind() {
		case nfa.StateLook:
			// Continue through any additional word boundary assertions
			look, next := state.Look()
			if next == nfa.InvalidState {
				continue
			}
			switch look {
			case nfa.LookWordBoundary:
				if wordBoundarySatisfied && !crossedBoundary.Contains(next) {
					crossedBoundary.Add(next)
					stack = append(stack, next)
				}
			case nfa.LookNoWordBoundary:
				if !wordBoundarySatisfied && !crossedBoundary.Contains(next) {
					crossedBoundary.Add(next)
					stack = append(stack, next)
				}
			}

		case nfa.StateEpsilon:
			// Follow epsilon transitions to reach consuming states after word boundaries
			next := state.Epsilon()
			if next != nfa.InvalidState && !crossedBoundary.Contains(next) {
				crossedBoundary.Add(next)
				stack = append(stack, next)
			}

		case nfa.StateSplit:
			// Follow split transitions
			left, right := state.Split()
			if left != nfa.InvalidState && !crossedBoundary.Contains(left) {
				crossedBoundary.Add(left)
				stack = append(stack, left)
			}
			if right != nfa.InvalidState && !crossedBoundary.Contains(right) {
				crossedBoundary.Add(right)
				stack = append(stack, right)
			}

		case nfa.StateCapture:
			// Follow through capture states when resolving word boundaries
			// Fix for Issue #15: capture states are epsilon transitions
			_, _, next := state.Capture()
			if next != nfa.InvalidState && !crossedBoundary.Contains(next) {
				crossedBoundary.Add(next)
				stack = append(stack, next)
			}
		}
	}

	// Combine original states with states reached by crossing word boundaries
	result := acquireStateSet()
	for _, sid := range states {
		result.Add(sid)
	}
	for _, sid := range crossedBoundary.ToSlice() {
		result.Add(sid)
	}
	releaseStateSet(crossedBoundary)

	// Get result slice before releasing
	resultSlice := result.ToSlice()
	releaseStateSet(result)
	return resultSlice
}

// containsMatchState returns true if any state in the set is a match state
func (b *Builder) containsMatchState(states []nfa.StateID) bool {
	for _, sid := range states {
		if b.nfa.IsMatch(sid) {
			return true
		}
	}
	return false
}

// CheckEOIMatch checks if there's a match at end-of-input by resolving pending
// word boundary assertions.
//
// At end of input:
//   - "Previous" byte is known from isFromWord
//   - "Next" byte is conceptually non-word (outside the string)
//   - Word boundary (\b) is satisfied if isFromWord is true (word → non-word)
//   - Non-word boundary (\B) is satisfied if isFromWord is false (non-word → non-word)
//
// This is called after the main search loop when we've exhausted input
// but might still have pending word boundary assertions that could match.
func (b *Builder) CheckEOIMatch(states []nfa.StateID, isFromWord bool) bool {
	// At EOI, "next" byte is non-word, so:
	// - \b is satisfied if isFromWord is true (transition from word to non-word)
	// - \B is satisfied if isFromWord is false (staying in non-word)
	wordBoundarySatisfied := isFromWord

	// Resolve word boundary assertions
	resolved := b.resolveWordBoundaries(states, wordBoundarySatisfied)

	// Also check end-of-text assertions (\z, $)
	// At EOI, both are satisfied
	lookHave := LookSetForEOI()

	// Expand with end-of-text assertions
	final := b.epsilonClosure(resolved, lookHave)

	// Check if any resulting state is a match
	return b.containsMatchState(final)
}

// Compile is a convenience function to build a DFA from an NFA with default config
func Compile(n *nfa.NFA) (*DFA, error) {
	return CompileWithConfig(n, DefaultConfig())
}

// CompileWithConfig builds a DFA from an NFA with the specified configuration
func CompileWithConfig(n *nfa.NFA, config Config) (*DFA, error) {
	builder := NewBuilder(n, config)
	return builder.Build()
}

// CompileWithPrefilter builds a DFA from an NFA with the specified configuration and prefilter.
// The prefilter is used to accelerate unanchored search by skipping non-matching regions.
func CompileWithPrefilter(n *nfa.NFA, config Config, pf prefilter.Prefilter) (*DFA, error) {
	builder := NewBuilder(n, config)
	dfa, err := builder.Build()
	if err != nil {
		return nil, err
	}
	dfa.prefilter = pf
	return dfa, nil
}

// CompilePattern is a convenience function to compile a regex pattern directly to DFA.
// This combines NFA compilation and DFA construction.
//
// Example:
//
//	dfa, err := lazy.CompilePattern("(foo|bar)\\d+")
//	if err != nil {
//	    return err
//	}
//	pos := dfa.Find([]byte("test foo123 end"))
func CompilePattern(pattern string) (*DFA, error) {
	return CompilePatternWithConfig(pattern, DefaultConfig())
}

// CompilePatternWithConfig compiles a pattern with custom configuration
func CompilePatternWithConfig(pattern string, config Config) (*DFA, error) {
	// Compile to NFA first
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(pattern)
	if err != nil {
		return nil, &DFAError{
			Kind:    InvalidConfig,
			Message: "NFA compilation failed",
			Cause:   err,
		}
	}

	// Build DFA from NFA
	return CompileWithConfig(nfaObj, config)
}

// ExtractPrefilter extracts and builds a prefilter from a regex pattern.
// Returns (nil, nil) if no suitable prefilter can be built (not an error).
//
// This is a helper for manual prefilter construction.
//
// For MVP, literal extraction from NFA is not implemented.
func ExtractPrefilter(pattern string) (prefilter.Prefilter, error) {
	// Parse pattern
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(pattern)
	if err != nil {
		return nil, err
	}

	// TODO: Extract literals from NFA
	// For now, return (nil, nil) indicating no prefilter (not an error)
	// Full implementation requires NFA → AST conversion or literal extraction from NFA
	_ = nfaObj // Suppress unused variable warning

	// No prefilter available - this is not an error condition
	// Returning (nil, nil) is intentional and documented
	//nolint:nilnil // nil prefilter with nil error indicates "no prefilter available" (not an error)
	return nil, nil
}

// BuildPrefilterFromLiterals constructs a prefilter from extracted literal sequences.
// This is useful when literals are known in advance.
func BuildPrefilterFromLiterals(prefixes, suffixes *literal.Seq) prefilter.Prefilter {
	builder := prefilter.NewBuilder(prefixes, suffixes)
	return builder.Build()
}

// DetectAccelerationFromCached analyzes a state's CACHED transitions only.
//
// This is a lazy version that only checks already-computed transitions.
// It requires most transitions to be cached for accurate detection.
// This avoids the performance hit of computing all transitions upfront.
//
// With ByteClasses compression, the state has fewer transitions (stride < 256),
// so we need most of the stride's transitions cached, not 240.
//
// A state is accelerable if:
//  1. Most equivalence classes loop back to self or go to dead state
//  2. Only 1-3 equivalence classes cause a transition to a different non-dead state
//
// Returns the exit bytes (1-3) or nil if not accelerable or insufficient data.
// Note: Returns representative bytes for exit classes, not class indices.
func DetectAccelerationFromCached(state *State) []byte {
	return DetectAccelerationFromCachedWithClasses(state, nil)
}

// DetectAccelerationFromCachedWithClasses analyzes a state's CACHED transitions
// with ByteClasses support for alphabet compression.
//
// When byteClasses is nil, falls back to identity mapping (no compression).
func DetectAccelerationFromCachedWithClasses(state *State, byteClasses *nfa.ByteClasses) []byte {
	// State no longer stores transitions — they live in DFACache.flatTrans.
	// This function cannot detect acceleration without the flat table.
	// Use DetectAccelerationFromFlat() instead.
	return nil
}

// DetectAccelerationFromFlat analyzes transitions from the flat table.
// Used by tryDetectAcceleration when State.transitions will be removed.
func DetectAccelerationFromFlat(sid StateID, flatTrans []StateID, stride int, byteClasses *nfa.ByteClasses) []byte {
	ftLen := len(flatTrans)
	return detectAccelFromTransitions(sid, stride, func(classIdx int) (StateID, bool) {
		offset := safeOffset(sid, classIdx)
		if offset >= ftLen {
			return InvalidState, false
		}
		next := flatTrans[offset]
		return next, next != InvalidState
	}, byteClasses)
}

// detectAccelFromTransitions is the shared implementation for acceleration detection.
// transitionFn returns (nextID, cached) for a given class index.
func detectAccelFromTransitions(selfID StateID, stride int, transitionFn func(int) (StateID, bool), byteClasses *nfa.ByteClasses) []byte {
	// Count cached transitions first
	cachedCount := 0
	for classIdx := 0; classIdx < stride; classIdx++ {
		if _, ok := transitionFn(classIdx); ok {
			cachedCount++
		}
	}
	minCachedRequired := stride - stride/16
	if minCachedRequired < 1 {
		minCachedRequired = 1
	}
	if cachedCount < minCachedRequired {
		return nil
	}

	var exitClasses []byte
	uncachedCount := 0

	for classIdx := 0; classIdx < stride; classIdx++ {
		nextID, ok := transitionFn(classIdx)
		if !ok {
			uncachedCount++
			maxUncached := stride / 16
			if maxUncached < 1 {
				maxUncached = 1
			}
			if uncachedCount > maxUncached {
				return nil
			}
			continue
		}

		if nextID == selfID || nextID == DeadState {
			continue
		}

		exitClasses = append(exitClasses, byte(classIdx))
		if len(exitClasses) > 3 {
			return nil
		}
	}

	// Accelerable if we have 1-3 exit classes
	if len(exitClasses) < 1 || len(exitClasses) > 3 {
		return nil
	}

	// Convert class indices back to representative bytes for memchr
	// If no ByteClasses, class index == byte value (identity mapping)
	if byteClasses == nil {
		return exitClasses
	}

	// Find representative bytes for each exit class
	exitBytes := make([]byte, 0, len(exitClasses))
	for _, classIdx := range exitClasses {
		// Find first byte that maps to this class
		for b := 0; b < 256; b++ {
			if byteClasses.Get(byte(b)) == classIdx {
				exitBytes = append(exitBytes, byte(b))
				break
			}
		}
	}

	return exitBytes
}

// DetectAcceleration analyzes a state by computing all byte transitions.
//
// WARNING: This is expensive! It computes move() for every byte value.
// Only call this when you're sure the state is worth optimizing (hot state).
//
// With ByteClasses compression, we iterate over equivalence classes and
// find representative bytes for exit classes.
//
// A state is accelerable if:
//  1. Most equivalence classes loop back to self or go to dead state
//  2. Only 1-3 classes cause a transition to a different non-dead state
//
// Returns the exit bytes (1-3) or nil if not accelerable.
func (b *Builder) DetectAcceleration(state *State) []byte {
	// State no longer stores transitions — they live in DFACache.flatTrans.
	// This method cannot detect acceleration without the flat table.
	// Use DetectAccelerationFromFlat() instead.
	return nil
}

// checkHasWordBoundary checks if the NFA contains any word boundary assertions (\b or \B).
// This is used to skip expensive word boundary checks in the search loop when not needed.
func (b *Builder) checkHasWordBoundary() bool {
	numStates := b.nfa.States()
	for i := nfa.StateID(0); int(i) < numStates; i++ {
		state := b.nfa.State(i)
		if state == nil {
			continue
		}
		if state.Kind() == nfa.StateLook {
			look, _ := state.Look()
			if look == nfa.LookWordBoundary || look == nfa.LookNoWordBoundary {
				return true
			}
		}
	}
	return false
}
