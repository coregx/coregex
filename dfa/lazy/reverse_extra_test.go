package lazy

import (
	"testing"

	"github.com/coregx/coregex/nfa"
)

// TestSearchReverseMultipleMatches tests that reverse search correctly finds match start
// across multiple positions when scanning different windows.
func TestSearchReverseMultipleMatches(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		start   int
		end     int
		want    int
	}{
		{
			name:    "suffix .txt finds start of extension",
			pattern: `\.txt`,
			input:   "file.txt",
			start:   0,
			end:     8,
			want:    4,
		},
		{
			name:    "alternation finds last match start",
			pattern: "foo|bar",
			input:   "xfoox",
			start:   0,
			end:     4,
			want:    1,
		},
		{
			name:    "repetition pattern finds earliest start",
			pattern: "a+",
			input:   "bbbaaab",
			start:   0,
			end:     6,
			want:    3,
		},
		{
			name:    "reverse search in sub-window",
			pattern: "test",
			input:   "aaa test bbb test ccc",
			start:   4,
			end:     8,
			want:    4,
		},
		{
			name:    "reverse search with char class",
			pattern: `[0-9]{3}`,
			input:   "abc123def",
			start:   0,
			end:     6,
			want:    3,
		},
		{
			name:    "digit sequence boundary",
			pattern: `[0-9]+`,
			input:   "x99y888z",
			start:   4,
			end:     7,
			want:    4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			revDFA := compileReverseDFA(t, tt.pattern)
			got := revDFA.SearchReverse([]byte(tt.input), tt.start, tt.end)
			if got != tt.want {
				t.Errorf("SearchReverse(%q, %d, %d) = %d, want %d",
					tt.input, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

// TestSearchReverseMatchAtBoundaries tests matches at various boundary positions.
func TestSearchReverseMatchAtBoundaries(t *testing.T) {
	revDFA := compileReverseDFA(t, "a")
	input := []byte("abcda")

	// Match at start (position 0)
	got := revDFA.SearchReverse(input, 0, 1)
	if got != 0 {
		t.Errorf("SearchReverse for 'a' at start = %d, want 0", got)
	}

	// Match at end
	got = revDFA.SearchReverse(input, 0, 5)
	if got != 4 {
		t.Errorf("SearchReverse for 'a' at end = %d, want 4", got)
	}

	// Single byte window that matches
	got = revDFA.SearchReverse(input, 4, 5)
	if got != 4 {
		t.Errorf("SearchReverse for 'a' single byte window = %d, want 4", got)
	}
}

// TestIsMatchReverseMatchBehavior tests that IsMatchReverse correctly scans backwards.
// Note: a reverse DFA compiled from ReverseAnchored NFA is used for finding match START
// positions in bidirectional search. It matches the pattern in REVERSE from the end position.
// This is NOT equivalent to a forward stdlib match.
func TestIsMatchReverseMatchBehavior(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		start   int
		end     int
		want    bool
	}{
		// The reverse DFA scans backward from end; these windows are chosen
		// so the reverse scan encounters the pattern bytes in reverse order.
		{"hello from start", "hello", "hello", 0, 5, true},
		{"digits at end", "[0-9]+", "abc123", 0, 6, true},
		{"no match", "xyz", "abcdef", 0, 6, false},
		{"char class match", "[a-z]+", "abc", 0, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			revDFA := compileReverseDFA(t, tt.pattern)
			got := revDFA.IsMatchReverse([]byte(tt.input), tt.start, tt.end)
			if got != tt.want {
				t.Errorf("IsMatchReverse(%q, %d, %d) = %v, want %v",
					tt.input, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

// TestSearchReverseLimitedAntiQuadraticGuard validates the anti-quadratic behavior.
func TestSearchReverseLimitedAntiQuadraticGuard(t *testing.T) {
	// Use a pattern that matches everywhere to trigger the quadratic signal
	revDFA := compileReverseDFA(t, "[a-z]+")
	input := []byte("abcdefghijklmnopqrstuvwxyz")

	// With minStart near the end, the search is limited
	got := revDFA.SearchReverseLimited(input, 0, len(input), 20)

	// Should either find a match or return quadratic signal
	if got == -1 {
		t.Error("SearchReverseLimited should not return -1 for all-matching lowercase input")
	}
	t.Logf("SearchReverseLimited result = %d (quadratic signal = %d)", got, SearchReverseLimitedQuadratic)
}

// TestSearchReverseLimitedMinStartAtStart ensures minStart=0 behaves like unlimited.
func TestSearchReverseLimitedMinStartAtStart(t *testing.T) {
	revDFA := compileReverseDFA(t, "abc")
	input := []byte("xxxabcyyy")

	// minStart = 0 should be equivalent to SearchReverse
	limited := revDFA.SearchReverseLimited(input, 0, 6, 0)
	unlimited := revDFA.SearchReverse(input, 0, 6)

	if limited != unlimited {
		t.Errorf("SearchReverseLimited(minStart=0) = %d, SearchReverse = %d; should be equal",
			limited, unlimited)
	}
}

// TestSearchReverseLimitedNoMatchBeyondMinStart tests that when the pattern has no match
// above the minStart, SearchReverseLimited returns -1 not -2.
func TestSearchReverseLimitedNoMatchBeyondMinStart(t *testing.T) {
	revDFA := compileReverseDFA(t, "xyz")
	input := []byte("abcdefghij")

	// No match at all -> should return -1 (dead state reached before minStart)
	got := revDFA.SearchReverseLimited(input, 0, 10, 5)
	if got != -1 {
		t.Errorf("SearchReverseLimited for non-matching pattern = %d, want -1", got)
	}
}

// TestSearchReverseWithCacheClearing verifies that reverse search handles cache clearing correctly.
func TestSearchReverseWithCacheClearing(t *testing.T) {
	config := DefaultConfig().WithMaxStates(3).WithMaxCacheClears(5)
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("[a-zA-Z]+[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	revDFA, err := CompileWithConfig(revNFA, config)
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	// This input should trigger cache clears during reverse search
	input := []byte("abc123")
	got := revDFA.SearchReverse(input, 0, 6)
	// Should find some match (exact position depends on NFA fallback)
	t.Logf("SearchReverse with cache clearing = %d", got)
}

// TestSearchReverseConsistency ensures SearchReverse and IsMatchReverse are consistent.
func TestSearchReverseConsistency(t *testing.T) {
	patterns := []string{
		"hello",
		"[0-9]+",
		"a+b+",
		"foo|bar|baz",
	}

	inputs := []string{
		"say hello there",
		"abc 123 def",
		"aaabbb done",
		"test baz end",
		"no match here",
	}

	for _, pattern := range patterns {
		for _, input := range inputs {
			t.Run(pattern+"_"+input, func(t *testing.T) {
				revDFA := compileReverseDFA(t, pattern)
				haystack := []byte(input)

				searchResult := revDFA.SearchReverse(haystack, 0, len(haystack))
				isMatchResult := revDFA.IsMatchReverse(haystack, 0, len(haystack))

				searchFound := searchResult >= 0
				if searchFound != isMatchResult {
					t.Errorf("Inconsistency: SearchReverse=%d (found=%v), IsMatchReverse=%v",
						searchResult, searchFound, isMatchResult)
				}
			})
		}
	}
}
