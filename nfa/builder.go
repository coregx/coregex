package nfa

import (
	"fmt"
)

// Builder constructs NFAs incrementally using a low-level API.
// This provides full control over NFA construction and is used by the Compiler.
type Builder struct {
	states          []State
	startAnchored   StateID
	startUnanchored StateID
	byteClassSet    *ByteClassSet // Tracks byte class boundaries for DFA optimization
}

// NewBuilder creates a new NFA builder with default capacity
func NewBuilder() *Builder {
	return NewBuilderWithCapacity(16)
}

// NewBuilderWithCapacity creates a new NFA builder with specified initial capacity
func NewBuilderWithCapacity(capacity int) *Builder {
	return &Builder{
		states:          make([]State, 0, capacity),
		startAnchored:   InvalidState,
		startUnanchored: InvalidState,
		byteClassSet:    NewByteClassSet(),
	}
}

// AddMatch adds a match (accepting) state and returns its ID
func (b *Builder) AddMatch() StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:   id,
		kind: StateMatch,
	})
	return id
}

// AddByteRange adds a state that transitions on a single byte or byte range [lo, hi].
// For a single byte, set lo == hi.
func (b *Builder) AddByteRange(lo, hi byte, next StateID) StateID {
	// Track byte class boundaries for DFA optimization
	b.byteClassSet.SetRange(lo, hi)

	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:   id,
		kind: StateByteRange,
		lo:   lo,
		hi:   hi,
		next: next,
	})
	return id
}

// AddSparse adds a state with multiple byte range transitions (character class).
// The transitions slice is copied to avoid aliasing issues.
func (b *Builder) AddSparse(transitions []Transition) StateID {
	// Track byte class boundaries for each transition range
	for _, tr := range transitions {
		b.byteClassSet.SetRange(tr.Lo, tr.Hi)
	}

	id := StateID(len(b.states))
	// Copy transitions to avoid aliasing
	trans := make([]Transition, len(transitions))
	copy(trans, transitions)
	b.states = append(b.states, State{
		id:          id,
		kind:        StateSparse,
		transitions: trans,
	})
	return id
}

// AddSplit adds a state with epsilon transitions to two states (alternation).
// This is used for alternation (a|b). For quantifiers, use AddQuantifierSplit.
func (b *Builder) AddSplit(left, right StateID) StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:    id,
		kind:  StateSplit,
		left:  left,
		right: right,
	})
	return id
}

// AddQuantifierSplit adds a state with epsilon transitions for quantifiers (*, +, ?, {n,m}).
// Unlike alternation splits, quantifier splits don't affect thread priority.
// Left branch is the "continue/repeat" path, right branch is the "exit" path.
// This distinction is crucial for correct leftmost-first matching semantics.
func (b *Builder) AddQuantifierSplit(left, right StateID) StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:                id,
		kind:              StateSplit,
		left:              left,
		right:             right,
		isQuantifierSplit: true,
	})
	return id
}

// AddEpsilon adds a state with a single epsilon transition (no input consumed)
func (b *Builder) AddEpsilon(next StateID) StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:   id,
		kind: StateEpsilon,
		next: next,
	})
	return id
}

// AddFail adds a dead state with no transitions
func (b *Builder) AddFail() StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:   id,
		kind: StateFail,
	})
	return id
}

// AddCapture adds a capture boundary state.
// captureIndex is the capture group number (1-based for explicit groups, 0 for entire match).
// isStart is true for opening boundary '(', false for closing ')'.
// next is the state to transition to after recording the capture position.
func (b *Builder) AddCapture(captureIndex uint32, isStart bool, next StateID) StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:           id,
		kind:         StateCapture,
		captureIndex: captureIndex,
		captureStart: isStart,
		next:         next,
	})
	return id
}

// AddLook adds a zero-width assertion state (look-around).
// look is the assertion type (start/end of text/line).
// next is the state to transition to if the assertion succeeds.
func (b *Builder) AddLook(look Look, next StateID) StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:   id,
		kind: StateLook,
		look: look,
		next: next,
	})
	return id
}

// AddRuneAny adds a state that matches any Unicode codepoint (including newlines).
// This is used for (?s). (dot with DOTALL flag).
// The state consumes 1-4 bytes (UTF-8 encoded rune) and transitions to next.
func (b *Builder) AddRuneAny(next StateID) StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:   id,
		kind: StateRuneAny,
		next: next,
	})
	return id
}

// AddRuneAnyNotNL adds a state that matches any Unicode codepoint except newline.
// This is used for the default . (dot) behavior.
// The state consumes 1-4 bytes (UTF-8 encoded rune) and transitions to next.
func (b *Builder) AddRuneAnyNotNL(next StateID) StateID {
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:   id,
		kind: StateRuneAnyNotNL,
		next: next,
	})
	return id
}

// Patch updates a state's target. This is used during compilation to handle
// forward references (e.g., loops, alternations).
// This only works for states with a single 'next' target (ByteRange, Epsilon).
func (b *Builder) Patch(stateID, target StateID) error {
	if int(stateID) >= len(b.states) {
		return &BuildError{
			Message: "state ID out of bounds",
			StateID: stateID,
		}
	}

	s := &b.states[stateID]
	switch s.kind {
	case StateByteRange, StateEpsilon, StateCapture, StateLook, StateRuneAny, StateRuneAnyNotNL:
		s.next = target
		return nil
	default:
		return &BuildError{
			Message: fmt.Sprintf("cannot patch state of kind %s", s.kind),
			StateID: stateID,
		}
	}
}

// PatchSplit updates the left or right target of a Split state
func (b *Builder) PatchSplit(stateID StateID, left, right StateID) error {
	if int(stateID) >= len(b.states) {
		return &BuildError{
			Message: "state ID out of bounds",
			StateID: stateID,
		}
	}

	s := &b.states[stateID]
	if s.kind != StateSplit {
		return &BuildError{
			Message: fmt.Sprintf("expected Split state, got %s", s.kind),
			StateID: stateID,
		}
	}

	s.left = left
	s.right = right
	return nil
}

// SetStart sets the starting state for the NFA (both anchored and unanchored)
//
// Deprecated: Use SetStarts() to set dual start states explicitly
func (b *Builder) SetStart(start StateID) {
	b.startAnchored = start
	b.startUnanchored = start
}

// SetStarts sets separate anchored and unanchored start states
func (b *Builder) SetStarts(anchored, unanchored StateID) {
	b.startAnchored = anchored
	b.startUnanchored = unanchored
}

// States returns the current number of states
func (b *Builder) States() int {
	return len(b.states)
}

// Validate checks that the NFA is well-formed:
// - Start state is valid
// - All state references point to valid states
// - No dangling references
func (b *Builder) Validate() error {
	if b.startAnchored == InvalidState {
		return &BuildError{Message: "anchored start state not set"}
	}
	if int(b.startAnchored) >= len(b.states) {
		return &BuildError{
			Message: "anchored start state out of bounds",
			StateID: b.startAnchored,
		}
	}
	if b.startUnanchored == InvalidState {
		return &BuildError{Message: "unanchored start state not set"}
	}
	if int(b.startUnanchored) >= len(b.states) {
		return &BuildError{
			Message: "unanchored start state out of bounds",
			StateID: b.startUnanchored,
		}
	}

	// Check all states have valid target references
	for i, s := range b.states {
		id := StateID(i)
		switch s.kind {
		case StateByteRange, StateEpsilon, StateCapture, StateLook, StateRuneAny, StateRuneAnyNotNL:
			if s.next != InvalidState && int(s.next) >= len(b.states) {
				return &BuildError{
					Message: fmt.Sprintf("invalid next state %d", s.next),
					StateID: id,
				}
			}
		case StateSplit:
			if s.left != InvalidState && int(s.left) >= len(b.states) {
				return &BuildError{
					Message: fmt.Sprintf("invalid left state %d", s.left),
					StateID: id,
				}
			}
			if s.right != InvalidState && int(s.right) >= len(b.states) {
				return &BuildError{
					Message: fmt.Sprintf("invalid right state %d", s.right),
					StateID: id,
				}
			}
		case StateSparse:
			for j, t := range s.transitions {
				if t.Next != InvalidState && int(t.Next) >= len(b.states) {
					return &BuildError{
						Message: fmt.Sprintf("invalid transition %d target %d", j, t.Next),
						StateID: id,
					}
				}
			}
		}
	}

	return nil
}

// Build finalizes and returns the constructed NFA.
// Options can be provided to set anchored/utf8 modes and pattern count.
func (b *Builder) Build(opts ...BuildOption) (*NFA, error) {
	// Apply default options
	nfa := &NFA{
		states:          b.states,
		startAnchored:   b.startAnchored,
		startUnanchored: b.startUnanchored,
		anchored:        false,
		utf8:            true,
		patternCount:    1,
		byteClasses:     b.byteClassSet.ByteClasses(), // Finalize byte classes
	}

	// Apply user options
	for _, opt := range opts {
		opt(nfa)
	}

	// Validate before returning
	if err := b.Validate(); err != nil {
		return nil, err
	}

	return nfa, nil
}

// BuildOption is a functional option for configuring the built NFA
type BuildOption func(*NFA)

// WithAnchored sets whether the NFA requires anchored matching
func WithAnchored(anchored bool) BuildOption {
	return func(n *NFA) {
		n.anchored = anchored
	}
}

// WithUTF8 sets whether the NFA respects UTF-8 boundaries
func WithUTF8(utf8 bool) BuildOption {
	return func(n *NFA) {
		n.utf8 = utf8
	}
}

// WithPatternCount sets the number of patterns in the NFA
func WithPatternCount(count int) BuildOption {
	return func(n *NFA) {
		n.patternCount = count
	}
}

// WithCaptureCount sets the number of capture groups in the NFA
func WithCaptureCount(count int) BuildOption {
	return func(n *NFA) {
		n.captureCount = count
	}
}

// WithCaptureNames sets the names of capture groups in the NFA.
// The slice should have length equal to captureCount.
// Index 0 should be "" (entire match), named groups have their names, unnamed groups are "".
func WithCaptureNames(names []string) BuildOption {
	return func(n *NFA) {
		if len(names) > 0 {
			n.captureNames = make([]string, len(names))
			copy(n.captureNames, names)
		}
	}
}
