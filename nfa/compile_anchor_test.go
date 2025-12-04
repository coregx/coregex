package nfa

import (
	"testing"
)

// TestAnchorBehavior tests zero-width assertions (^, $, \A, \z)
// This is the comprehensive test for Issue #10 fix
func TestAnchorBehavior(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Start anchor ^ (LookStartLine)
		{"^abc", "abc", true},
		{"^abc", "xabc", false},
		{"^abc", "abcx", true}, // ^ doesn't require end

		// End anchor $ (LookEndLine)
		{"abc$", "abc", true},
		{"abc$", "abcx", false},
		{"abc$", "xabc", true}, // $ doesn't require start

		// Both anchors together
		{"^abc$", "abc", true},
		{"^abc$", "xabc", false},
		{"^abc$", "abcx", false},
		{"^abc$", "xabcx", false},

		// Original bug cases from Issue #10
		{"^.$", "1", true},
		{"^.$", "22", false},  // BUG: was matching (returned true)
		{"^.$", "333", false}, // BUG: was matching (returned true)
		{"^[a-z]+$", "abc", true},
		{"^[a-z]+$", "a68", false}, // BUG: was matching (returned true)
		{"^[a-z]+$", "123", false},

		// With quantifiers
		{"^.+$", "abc", true},
		{"^.+$", "", false},
		{"^.*$", "", true}, // .* can match empty
		{"^.*$", "anything", true},

		// Empty pattern with anchors
		{"^$", "", true},
		{"^$", "x", false},

		// Line boundaries with \n
		{"^abc", "abc\ndef", true},    // ^ matches at start
		{"^abc", "x\nabc", false},     // ^ doesn't match after \n in single-line mode
		{"abc$", "abc\ndef", false},   // $ does NOT match before \n (Go behavior)
		{"abc$", "xabc\n", false},     // $ does NOT match before \n (Go behavior)
		{"abc$", "xabc\nmore", false}, // $ doesn't match before \n in middle

		// Text boundaries \A and \z (if supported by syntax package)
		// Note: Go's regexp/syntax uses OpBeginText/OpEndText for \A/\z

		// Complex patterns with anchors
		{"^[0-9]+$", "123", true},
		{"^[0-9]+$", "12a", false},
		{"^[0-9]+$", "a12", false},
		{"^[a-z]+[0-9]+$", "abc123", true},
		{"^[a-z]+[0-9]+$", "123abc", false},

		// Anchors with alternation
		{"^(a|b)$", "a", true},
		{"^(a|b)$", "b", true},
		{"^(a|b)$", "ab", false},
		{"^(a|b)$", "c", false},

		// Multiple character classes (from pikevm test)
		{"^[AB][BC][CD]$", "ABC", true},
		{"^[AB][BC][CD]$", "BBD", true},
		{"^[AB][BC][CD]$", "ABCD", false}, // Too long
		{"^[AB][BC][CD]$", "AB", false},   // Too short
	}

	for _, tt := range tests {
		// Compile to NFA
		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) failed: %v", tt.pattern, err)
		}

		// Test with PikeVM
		vm := NewPikeVM(nfa)
		_, _, got := vm.Search([]byte(tt.input))

		if got != tt.want {
			t.Errorf("Pattern %q, Input %q: got %v, want %v",
				tt.pattern, tt.input, got, tt.want)
		}
	}
}

// TestStartAnchorOnly tests patterns with only start anchor ^
func TestStartAnchorOnly(t *testing.T) {
	tests := []struct {
		pattern   string
		input     string
		wantMatch bool
		wantStart int
		wantEnd   int
	}{
		{"^test", "test", true, 0, 4},
		{"^test", "test123", true, 0, 4},
		{"^test", "xtest", false, -1, -1},
		{"^test", "x\ntest", false, -1, -1}, // ^ doesn't match after \n (single-line mode)
		{"^", "", true, 0, 0},               // ^ always matches at start
		{"^", "x", true, 0, 0},              // ^ matches at position 0 (zero-width)
	}

	for _, tt := range tests {
		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) failed: %v", tt.pattern, err)
		}

		vm := NewPikeVM(nfa)
		start, end, matched := vm.Search([]byte(tt.input))

		if matched != tt.wantMatch {
			t.Errorf("Pattern %q, Input %q: matched=%v, want %v",
				tt.pattern, tt.input, matched, tt.wantMatch)
			continue
		}

		if matched {
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("Pattern %q, Input %q: got [%d, %d], want [%d, %d]",
					tt.pattern, tt.input, start, end, tt.wantStart, tt.wantEnd)
			}
		}
	}
}

// TestEndAnchorOnly tests patterns with only end anchor $
func TestEndAnchorOnly(t *testing.T) {
	tests := []struct {
		pattern   string
		input     string
		wantMatch bool
		wantStart int
		wantEnd   int
	}{
		{"test$", "test", true, 0, 4},
		{"test$", "xtest", true, 1, 5},
		{"test$", "testx", false, -1, -1},
		{"test$", "test\n", false, -1, -1},  // $ does NOT match before \n (Go behavior)
		{"test$", "test\nx", false, -1, -1}, // $ doesn't match before \n in middle
		{"$", "", true, 0, 0},               // $ matches at end
		{"$", "x", true, 1, 1},              // $ matches at end (zero-width)
	}

	for _, tt := range tests {
		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) failed: %v", tt.pattern, err)
		}

		vm := NewPikeVM(nfa)
		start, end, matched := vm.Search([]byte(tt.input))

		if matched != tt.wantMatch {
			t.Errorf("Pattern %q, Input %q: matched=%v, want %v",
				tt.pattern, tt.input, matched, tt.wantMatch)
			continue
		}

		if matched {
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("Pattern %q, Input %q: got [%d, %d], want [%d, %d]",
					tt.pattern, tt.input, start, end, tt.wantStart, tt.wantEnd)
			}
		}
	}
}

// TestBothAnchors tests patterns with both ^ and $
func TestBothAnchors(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Exact match required
		{"^test$", "test", true},
		{"^test$", "xtest", false},
		{"^test$", "testx", false},
		{"^test$", "xtest", false},

		// With character classes
		{"^[a-z]+$", "abc", true},
		{"^[a-z]+$", "abc123", false},
		{"^[a-z]+$", "123abc", false},
		{"^[a-z]+$", "123", false},

		// With quantifiers
		{"^[0-9]{3}$", "123", true},
		{"^[0-9]{3}$", "12", false},
		{"^[0-9]{3}$", "1234", false},

		// Empty match
		{"^$", "", true},
		{"^$", "x", false},

		// With optional
		{"^a?$", "", true},
		{"^a?$", "a", true},
		{"^a?$", "aa", false},
	}

	for _, tt := range tests {
		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) failed: %v", tt.pattern, err)
		}

		vm := NewPikeVM(nfa)
		_, _, got := vm.Search([]byte(tt.input))

		if got != tt.want {
			t.Errorf("Pattern %q, Input %q: got %v, want %v",
				tt.pattern, tt.input, got, tt.want)
		}
	}
}

// TestAnchorWithNewlines tests anchor behavior with newlines
func TestAnchorWithNewlines(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// $ before newline (Go does NOT match before \n)
		{"test$", "test\n", false},     // $ does NOT match before \n (Go behavior)
		{"test$", "test\nmore", false}, // $ doesn't match before \n

		// ^ after newline (in single-line mode, doesn't match)
		{"^test", "test", true},
		{"^test", "x\ntest", false}, // ^ doesn't match after \n (single-line)

		// Both with newlines
		{"^test$", "test", true},
		{"^test$", "test\n", false}, // $ does NOT match before \n
	}

	for _, tt := range tests {
		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) failed: %v", tt.pattern, err)
		}

		vm := NewPikeVM(nfa)
		_, _, got := vm.Search([]byte(tt.input))

		if got != tt.want {
			t.Errorf("Pattern %q, Input %q: got %v, want %v",
				tt.pattern, tt.input, got, tt.want)
		}
	}
}

// TestUnanchoredSearchCorrectness tests that unanchored search correctly
// tracks startPos when matches occur anywhere in the input.
// This test verifies the fix for Issue #10 where embedded NFA prefix
// caused incorrect startPos tracking.
func TestUnanchoredSearchCorrectness(t *testing.T) {
	tests := []struct {
		pattern   string
		input     string
		wantMatch bool
		wantStart int
		wantEnd   int
	}{
		// Original bug cases from Issue #10
		{"test$", "xtest", true, 1, 5},
		{"test$", "test", true, 0, 4},
		{"abc", "xyzabc", true, 3, 6},

		// Verify anchors work correctly
		{"^test", "test", true, 0, 4},
		{"^test", "xtest", false, -1, -1}, // no match

		// Empty matches
		{"a*", "bbb", true, 0, 0}, // matches empty at start

		// Multiple potential matches - leftmost wins
		{"test", "test test", true, 0, 4},
		{"test", "xtest", true, 1, 5},

		// Character classes with prefix
		{"[a-z]+$", "123abc", true, 3, 6},
		{"\\d+$", "abc123", true, 3, 6},

		// Complex patterns
		{"test.*end", "xxtestmiddleend", true, 2, 15},
		{"a+", "bbbaaa", true, 3, 6},

		// Edge cases
		{"", "test", true, 0, 0},     // empty pattern matches at start
		{"$", "x", true, 1, 1},       // end anchor at position 1
		{"test", "test", true, 0, 4}, // exact match at start
	}

	for _, tt := range tests {
		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
		}

		vm := NewPikeVM(nfa)
		start, end, matched := vm.Search([]byte(tt.input))

		// Check if match expectation is correct
		if matched != tt.wantMatch {
			if matched {
				t.Errorf("%q in %q: got match [%d,%d], want no match",
					tt.pattern, tt.input, start, end)
			} else {
				t.Errorf("%q in %q: got no match, want [%d,%d]",
					tt.pattern, tt.input, tt.wantStart, tt.wantEnd)
			}
			continue
		}

		// If we got a match, check the positions
		if matched && (start != tt.wantStart || end != tt.wantEnd) {
			t.Errorf("%q in %q: got [%d,%d], want [%d,%d]",
				tt.pattern, tt.input, start, end, tt.wantStart, tt.wantEnd)
		}
	}
}
