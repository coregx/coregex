package nfa

import (
	"fmt"
)

// Builder constructs NFAs incrementally using a low-level API.
// This provides full control over NFA construction and is used by the Compiler.
type Builder struct {
	states []State
	start  StateID
}

// NewBuilder creates a new NFA builder with default capacity
func NewBuilder() *Builder {
	return NewBuilderWithCapacity(16)
}

// NewBuilderWithCapacity creates a new NFA builder with specified initial capacity
func NewBuilderWithCapacity(capacity int) *Builder {
	return &Builder{
		states: make([]State, 0, capacity),
		start:  InvalidState,
	}
}

// AddMatch adds a match (accepting) state and returns its ID
func (b *Builder) AddMatch() StateID {
	//nolint:gosec // G115: StateID is uint32, this conversion is safe for realistic NFA sizes
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
	//nolint:gosec // G115: StateID is uint32, this conversion is safe for realistic NFA sizes
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
	//nolint:gosec // G115: StateID is uint32, this conversion is safe for realistic NFA sizes
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
// This is used for alternation (a|b) and optional patterns.
func (b *Builder) AddSplit(left, right StateID) StateID {
	//nolint:gosec // G115: StateID is uint32, this conversion is safe for realistic NFA sizes
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:    id,
		kind:  StateSplit,
		left:  left,
		right: right,
	})
	return id
}

// AddEpsilon adds a state with a single epsilon transition (no input consumed)
func (b *Builder) AddEpsilon(next StateID) StateID {
	//nolint:gosec // G115: StateID is uint32, this conversion is safe for realistic NFA sizes
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
	//nolint:gosec // G115: StateID is uint32, this conversion is safe for realistic NFA sizes
	id := StateID(len(b.states))
	b.states = append(b.states, State{
		id:   id,
		kind: StateFail,
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
	case StateByteRange, StateEpsilon:
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

// SetStart sets the starting state for the NFA
func (b *Builder) SetStart(start StateID) {
	b.start = start
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
	if b.start == InvalidState {
		return &BuildError{Message: "start state not set"}
	}
	if int(b.start) >= len(b.states) {
		return &BuildError{
			Message: "start state out of bounds",
			StateID: b.start,
		}
	}

	// Check all states have valid target references
	for i, s := range b.states {
		//nolint:gosec // G115: StateID is uint32, this conversion is safe for realistic NFA sizes
		id := StateID(i)
		switch s.kind {
		case StateByteRange, StateEpsilon:
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
		states:       b.states,
		start:        b.start,
		anchored:     false,
		utf8:         true,
		patternCount: 1,
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
