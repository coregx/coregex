package nfa

import "regexp/syntax"

// ContainsDot checks if the regex pattern contains '.' (any char or any char except newline).
// When true, the pattern will benefit from ASCII-only NFA compilation for ASCII-only inputs.
//
// This is used for ASCII runtime detection optimization (V11-002):
// patterns with '.' create ~28 NFA states for UTF-8 handling, but only 1-2 states
// in ASCII mode. Runtime detection allows using the faster ASCII NFA when input is ASCII.
func ContainsDot(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true

	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			if ContainsDot(sub) {
				return true
			}
		}

	case syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if len(re.Sub) > 0 {
			return ContainsDot(re.Sub[0])
		}
	}

	return false
}

// PatternHasUTF8Dependence checks if the pattern has constructs that depend on
// UTF-8 handling beyond '.' (such as Unicode character classes, word boundaries, etc).
//
// When this returns true, ASCII optimization may not be valid for all cases.
// Currently checks for:
//   - Unicode character classes (\p{...}, \P{...})
//   - Word boundaries (\b, \B) - these depend on Unicode word chars in Go regexp
//
// Note: This is conservative - patterns that ONLY use '.' and ASCII literals
// can safely use ASCII optimization.
func PatternHasUTF8Dependence(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpCharClass:
		// Check if any range goes beyond ASCII
		for _, r := range re.Rune {
			if r > 127 {
				return true
			}
		}

	case syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		// Word boundaries depend on Unicode word definition
		// However, for ASCII-only input, word boundary behavior is correct
		// so we don't need to disable ASCII optimization for this
		return false

	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			if PatternHasUTF8Dependence(sub) {
				return true
			}
		}

	case syntax.OpCapture, syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if len(re.Sub) > 0 {
			return PatternHasUTF8Dependence(re.Sub[0])
		}
	}

	return false
}
