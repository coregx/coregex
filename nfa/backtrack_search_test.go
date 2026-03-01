package nfa

import (
	"regexp"
	"testing"
)

// TestBacktracker_SearchWithCaptures tests that BoundedBacktracker does not
// return captures (it uses internal backtracking without capture tracking),
// but correctly finds match boundaries. We test via Search returning
// (start, end, found) and verify against stdlib.
func TestBacktracker_Search_VsStdlib(t *testing.T) {
	patterns := []string{
		`\d+`,
		`\w+`,
		`[a-z]+`,
		"foo|bar|baz",
		`a+b+c+`,
		`\d{2,4}`,
		`[a-zA-Z]+\d+`,
		"hello",
		"(abc)+",
		`\d+-\d+`,
	}

	inputs := []string{
		"hello world",
		"abc123def456",
		"the quick brown fox",
		"foo bar baz qux",
		"aaabbbccc",
		"12 1234 123456",
		"test99end",
		"say hello there",
		"abcabcabc",
		"123-456-789",
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)
		nfa := compileNFAForTest(pattern)
		bt := NewBoundedBacktracker(nfa)

		for _, input := range inputs {
			t.Run(pattern+"/"+input, func(t *testing.T) {
				stdLoc := stdRe.FindStringIndex(input)
				btStart, btEnd, btFound := bt.Search([]byte(input))

				stdMatched := stdLoc != nil
				if stdMatched != btFound {
					t.Errorf("match mismatch: stdlib=%v, bt=%v", stdMatched, btFound)
					return
				}

				if stdMatched && btFound {
					if stdLoc[0] != btStart || stdLoc[1] != btEnd {
						t.Errorf("position mismatch: stdlib=[%d,%d], bt=[%d,%d]",
							stdLoc[0], stdLoc[1], btStart, btEnd)
					}
				}
			})
		}
	}
}

// TestBacktracker_SearchAt_MultipleMatches tests finding subsequent matches using SearchAt.
func TestBacktracker_SearchAt_MultipleMatches(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)

	input := []byte("abc123def456ghi789")

	// First match: "123" at [3, 6)
	start, end, found := bt.SearchAt(input, 0)
	if !found || start != 3 || end != 6 {
		t.Errorf("match 1: got (%d, %d, %v), want (3, 6, true)", start, end, found)
	}

	// Second match: "456" at [9, 12)
	start, end, found = bt.SearchAt(input, 6)
	if !found || start != 9 || end != 12 {
		t.Errorf("match 2: got (%d, %d, %v), want (9, 12, true)", start, end, found)
	}

	// Third match: "789" at [15, 18)
	start, end, found = bt.SearchAt(input, 12)
	if !found || start != 15 || end != 18 {
		t.Errorf("match 3: got (%d, %d, %v), want (15, 18, true)", start, end, found)
	}

	// No more matches
	_, _, found = bt.SearchAt(input, 18)
	if found {
		t.Error("should not find match 4")
	}
}

// TestBacktracker_SearchAt_Boundary tests SearchAt at exact match boundaries.
func TestBacktracker_SearchAt_Boundary(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		at        int
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{
			name: "start at match beginning", pattern: "abc", input: "abc",
			at: 0, wantStart: 0, wantEnd: 3, wantFound: true,
		},
		{
			name: "start at match end", pattern: "abc", input: "abc",
			at: 3, wantStart: -1, wantEnd: -1, wantFound: false,
		},
		{
			name: "start in middle of match", pattern: "abcd", input: "abcd",
			at: 2, wantStart: -1, wantEnd: -1, wantFound: false,
		},
		{
			name: "start at second match", pattern: "ab", input: "abab",
			at: 2, wantStart: 2, wantEnd: 4, wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(nfa)

			start, end, found := bt.SearchAt([]byte(tt.input), tt.at)
			if found != tt.wantFound {
				t.Errorf("found=%v, want %v", found, tt.wantFound)
				return
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_LongestMode tests leftmost-longest match semantics.
func TestBacktracker_LongestMode(t *testing.T) {
	nfa := compileNFAForTest("a+")
	bt := NewBoundedBacktracker(nfa)

	// Default (greedy, but leftmost-first)
	start, end, found := bt.Search([]byte("baaab"))
	if !found || start != 1 || end != 4 {
		t.Errorf("default: got (%d, %d, %v), want (1, 4, true)", start, end, found)
	}

	// Longest mode
	bt.SetLongest(true)
	start, end, found = bt.Search([]byte("baaab"))
	if !found || start != 1 || end != 4 {
		t.Errorf("longest: got (%d, %d, %v), want (1, 4, true)", start, end, found)
	}
}

// TestBacktracker_CanHandle_Various tests CanHandle with different patterns and sizes.
func TestBacktracker_CanHandle_Various(t *testing.T) {
	patterns := []struct {
		name    string
		pattern string
	}{
		{"simple", "abc"},
		{"digit", `\d+`},
		{"complex", `[a-zA-Z]+\d{2,4}`},
		{"alternation", "foo|bar|baz|qux"},
	}

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			nfa := compileNFAForTest(p.pattern)
			bt := NewBoundedBacktracker(nfa)

			// Small inputs should always be handleable
			if !bt.CanHandle(100) {
				t.Error("should handle 100 bytes")
			}
			if !bt.CanHandle(0) {
				t.Error("should handle 0 bytes")
			}

			// MaxInputSize should be consistent
			maxInput := bt.MaxInputSize()
			if maxInput <= 0 {
				t.Errorf("MaxInputSize() = %d, should be > 0", maxInput)
			}
			if !bt.CanHandle(maxInput) {
				t.Error("should handle MaxInputSize()")
			}
		})
	}
}

// TestBacktracker_StateReuse tests that BacktrackerState can be reused across searches.
func TestBacktracker_StateReuse(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)

	state := NewBacktrackerState()
	input := []byte("abc123def456")

	// First search
	s1, e1, f1 := bt.SearchWithState(input, state)
	if !f1 || s1 != 3 || e1 != 6 {
		t.Errorf("search 1: got (%d, %d, %v), want (3, 6, true)", s1, e1, f1)
	}

	// Second search with same state (should work correctly after reset)
	s2, e2, f2 := bt.SearchWithState(input, state)
	if !f2 || s2 != 3 || e2 != 6 {
		t.Errorf("search 2: got (%d, %d, %v), want (3, 6, true)", s2, e2, f2)
	}

	// Different input
	input2 := []byte("xyz789")
	s3, e3, f3 := bt.SearchWithState(input2, state)
	if !f3 || s3 != 3 || e3 != 6 {
		t.Errorf("search 3: got (%d, %d, %v), want (3, 6, true)", s3, e3, f3)
	}
}

// TestBacktracker_IsMatchAnchored_WithState tests the stateful anchored match variant.
func TestBacktracker_IsMatchAnchored_WithState(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)

	state := NewBacktrackerState()

	// Digits at start
	if !bt.IsMatchAnchoredWithState([]byte("123abc"), state) {
		t.Error("should match digits at start")
	}

	// No digits at start
	if bt.IsMatchAnchoredWithState([]byte("abc123"), state) {
		t.Error("should not match: digits not at start")
	}
}

// TestBacktracker_SearchAtWithState_Iteration tests iterating matches with external state.
func TestBacktracker_SearchAtWithState_Iteration(t *testing.T) {
	nfa := compileNFAForTest("[a-z]+")
	bt := NewBoundedBacktracker(nfa)
	state := NewBacktrackerState()

	input := []byte("abc 123 def 456 ghi")
	var matches [][2]int

	at := 0
	for {
		start, end, found := bt.SearchAtWithState(input, at, state)
		if !found {
			break
		}
		matches = append(matches, [2]int{start, end})
		if end > at {
			at = end
		} else {
			at++
		}
	}

	expected := [][2]int{{0, 3}, {8, 11}, {16, 19}}
	if len(matches) != len(expected) {
		t.Errorf("found %d matches, want %d", len(matches), len(expected))
		t.Logf("matches: %v", matches)
		return
	}
	for i, m := range matches {
		if m != expected[i] {
			t.Errorf("match %d: got %v, want %v", i, m, expected[i])
		}
	}
}

// TestBacktracker_Unicode tests backtracker with multi-byte UTF-8 input.
func TestBacktracker_Unicode(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		wantPos  []int
		wantBool bool
	}{
		{
			name: "cyrillic", pattern: "мир", input: "привет мир",
			wantPos: []int{13, 19}, wantBool: true,
		},
		{
			name: "dot match unicode", pattern: "a.c", input: "aбc",
			wantPos: []int{0, 4}, wantBool: true,
		},
		{
			name: "unicode no match", pattern: "мир", input: "hello world",
			wantPos: nil, wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(nfa)

			got := bt.IsMatch([]byte(tt.input))
			if got != tt.wantBool {
				t.Errorf("IsMatch = %v, want %v", got, tt.wantBool)
			}

			start, end, found := bt.Search([]byte(tt.input))
			switch {
			case tt.wantPos == nil && found:
				t.Errorf("Search: expected no match, got (%d, %d)", start, end)
			case tt.wantPos != nil && !found:
				t.Errorf("Search: expected match at %v, got no match", tt.wantPos)
			case tt.wantPos != nil && found && (start != tt.wantPos[0] || end != tt.wantPos[1]):
				t.Errorf("Search: got (%d, %d), want (%d, %d)",
					start, end, tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestBacktracker_LargeInput_NotHandled tests graceful rejection of oversized inputs.
func TestBacktracker_LargeInput_NotHandled(t *testing.T) {
	nfa := compileNFAForTest(`\w+`)
	bt := NewBoundedBacktracker(nfa)

	maxInput := bt.MaxInputSize()
	largeInput := make([]byte, maxInput+100)
	for i := range largeInput {
		largeInput[i] = 'a'
	}

	// IsMatch should return false (not handle)
	if bt.IsMatch(largeInput) {
		t.Error("IsMatch should return false for oversized input")
	}

	// IsMatchAnchored should return false
	if bt.IsMatchAnchored(largeInput) {
		t.Error("IsMatchAnchored should return false for oversized input")
	}

	// Search should return not found
	_, _, found := bt.Search(largeInput)
	if found {
		t.Error("Search should return not found for oversized input")
	}

	// SearchAt should return not found
	_, _, found = bt.SearchAt(largeInput, 0)
	if found {
		t.Error("SearchAt should return not found for oversized input")
	}

	// WithState variants
	state := NewBacktrackerState()
	if bt.IsMatchWithState(largeInput, state) {
		t.Error("IsMatchWithState should return false for oversized input")
	}
	if bt.IsMatchAnchoredWithState(largeInput, state) {
		t.Error("IsMatchAnchoredWithState should return false for oversized input")
	}
	_, _, found = bt.SearchAtWithState(largeInput, 0, state)
	if found {
		t.Error("SearchAtWithState should return not found for oversized input")
	}
}

// TestBacktracker_GenerationOverflow tests that generation counter overflow
// is handled correctly (array gets cleared, search still works).
func TestBacktracker_GenerationOverflow(t *testing.T) {
	nfa := compileNFAForTest("abc")
	bt := NewBoundedBacktracker(nfa)

	state := NewBacktrackerState()
	input := []byte("xxxabcyyy")

	// Run many searches to exercise generation overflow (uint16 wraps at 65536)
	for i := 0; i < 70000; i++ {
		start, end, found := bt.SearchWithState(input, state)
		if !found {
			t.Fatalf("iteration %d: expected match, got none", i)
		}
		if start != 3 || end != 6 {
			t.Fatalf("iteration %d: got (%d, %d), want (3, 6)", i, start, end)
		}
	}
}
