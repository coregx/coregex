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
}

// NewBuilder creates a new DFA builder for the given NFA
func NewBuilder(n *nfa.NFA, config Config) *Builder {
	return &Builder{
		nfa:    n,
		config: config,
	}
}

// Build constructs and returns a Lazy DFA ready for searching.
// Returns error if configuration is invalid.
func (b *Builder) Build() (*DFA, error) {
	// Validate configuration
	if err := b.config.Validate(); err != nil {
		return nil, err
	}

	// Create cache
	cache := NewCache(b.config.MaxStates)

	// Build prefilter if enabled
	var pf prefilter.Prefilter
	if b.config.UsePrefilter {
		pf = b.buildPrefilter()
	}

	// Create start state from NFA unanchored start (for O(n) unanchored search)
	// Use StartUnanchored() which includes the implicit (?s:.)*? prefix
	startStateSet := b.epsilonClosure([]nfa.StateID{b.nfa.StartUnanchored()})
	isMatch := b.containsMatchState(startStateSet)
	startState := NewState(StartState, startStateSet, isMatch)

	// Insert start state into cache
	key := ComputeStateKey(startStateSet)
	_, err := cache.Insert(key, startState)
	if err != nil {
		// This should never happen for start state (cache is empty)
		return nil, &DFAError{
			Kind:    InvalidConfig,
			Message: "failed to insert start state",
			Cause:   err,
		}
	}

	// Create StartTable for caching start states by look-behind context
	startTable := NewStartTable()

	// Create DFA
	dfa := &DFA{
		nfa:        b.nfa,
		cache:      cache,
		config:     b.config,
		prefilter:  pf,
		pikevm:     nfa.NewPikeVM(b.nfa),
		stateByID:  make(map[StateID]*State, b.config.MaxStates),
		startTable: startTable,
	}

	// Register start state in ID lookup map
	dfa.registerState(startState)

	// Cache the default start state (StartText, unanchored) in StartTable
	// This is the most common start configuration
	startTable.Set(StartText, false, startState.ID())

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
// states via epsilon transitions (Split, Epsilon states).
//
// This is a fundamental operation in NFA → DFA conversion.
//
// Algorithm: Iterative DFS with visited set
//  1. Start with input states
//  2. Follow all epsilon transitions (Split, Epsilon)
//  3. Collect all reachable states
//  4. Return sorted list for consistent ordering
func (b *Builder) epsilonClosure(states []nfa.StateID) []nfa.StateID {
	// Use StateSet for efficient membership testing and deduplication
	closure := NewStateSetWithCapacity(len(states) * 2)
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
		}
	}

	// Return sorted slice for consistent state keys
	return closure.ToSlice()
}

// move computes the set of NFA states reachable from the given states on input byte b.
//
// This is the core determinization operation:
//  1. For each NFA state in the input set
//  2. Check if it has a transition on byte b
//  3. Collect all target states
//  4. Compute epsilon-closure of targets
//  5. Return the resulting state set
//
// This effectively simulates one step of the NFA for all active states.
func (b *Builder) move(states []nfa.StateID, input byte) []nfa.StateID {
	// Collect target states for this input byte
	targets := NewStateSet()

	for _, sid := range states {
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
		return nil
	}

	// Compute epsilon-closure of target states
	return b.epsilonClosure(targets.ToSlice())
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
