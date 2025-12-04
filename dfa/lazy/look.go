package lazy

import "github.com/coregx/coregex/nfa"

// LookSet is a bitset representing satisfied look-around assertions.
// This enables efficient tracking of which assertions are true at a given position.
//
// The DFA uses LookSet during epsilon closure to determine which StateLook
// transitions can be followed. Different start positions have different
// look assertions satisfied:
//   - Position 0 (StartText): Both \A and ^ are satisfied
//   - After \n (StartLineLF): Only ^ is satisfied
//   - Other positions: Neither is satisfied
//
// This is modeled after Rust regex-automata's approach.
type LookSet uint32

const (
	// LookNone represents no assertions satisfied
	LookNone LookSet = 0

	// LookStartText represents \A - start of input
	LookStartText LookSet = 1 << iota

	// LookEndText represents \z - end of input
	LookEndText

	// LookStartLine represents ^ - start of line (after \n or at input start)
	LookStartLine

	// LookEndLine represents $ - end of line (before \n or at input end)
	LookEndLine
)

// Contains returns true if the look assertion is in this set
func (s LookSet) Contains(look nfa.Look) bool {
	switch look {
	case nfa.LookStartText:
		return s&LookStartText != 0
	case nfa.LookEndText:
		return s&LookEndText != 0
	case nfa.LookStartLine:
		return s&LookStartLine != 0
	case nfa.LookEndLine:
		return s&LookEndLine != 0
	default:
		return false
	}
}

// Insert adds a look assertion to the set
func (s LookSet) Insert(look nfa.Look) LookSet {
	switch look {
	case nfa.LookStartText:
		return s | LookStartText
	case nfa.LookEndText:
		return s | LookEndText
	case nfa.LookStartLine:
		return s | LookStartLine
	case nfa.LookEndLine:
		return s | LookEndLine
	default:
		return s
	}
}

// LookSetFromStartKind returns the look assertions satisfied by a start configuration.
//
// This is the key mapping between DFA start kinds and NFA look assertions:
//   - StartText (position 0): Both \A and ^ are satisfied
//   - StartLineLF (after \n): Only ^ is satisfied (multiline ^ behavior)
//   - StartLineCR (after \r): In Go's regexp, this does NOT satisfy ^
//   - Other kinds: No line-related assertions satisfied
func LookSetFromStartKind(kind StartKind) LookSet {
	switch kind {
	case StartText:
		// At start of input: both \A and ^ are satisfied
		return LookStartText | LookStartLine
	case StartLineLF:
		// After \n: ^ is satisfied (multiline mode)
		return LookStartLine
	case StartLineCR:
		// After \r: in Go's regexp, this does NOT satisfy ^
		// (only \n triggers multiline ^)
		return LookNone
	case StartWord, StartNonWord:
		// No line-related assertions satisfied
		return LookNone
	default:
		return LookNone
	}
}

// LookSetForEOI returns look assertions satisfied at end-of-input.
// Both \z and $ are satisfied at the true end of input.
func LookSetForEOI() LookSet {
	return LookEndText | LookEndLine
}
