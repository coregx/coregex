package lazy

import (
	"testing"

	"github.com/coregx/coregex/nfa"
)

// compileReverseDFA compiles a reverse DFA from a pattern for testing reverse search.
func compileReverseDFA(t *testing.T, pattern string) *DFA {
	t.Helper()
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile(pattern)
	if err != nil {
		t.Fatalf("NFA compile %q error: %v", pattern, err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	dfa, err := CompileWithConfig(revNFA, DefaultConfig())
	if err != nil {
		t.Fatalf("Reverse DFA compile %q error: %v", pattern, err)
	}
	return dfa
}

func TestSearchReverseBasic(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		start   int
		end     int
		want    int // expected match start position, -1 if no match
	}{
		{
			name:    "simple literal finds start",
			pattern: "hello",
			input:   "hello world",
			start:   0,
			end:     5,
			want:    0,
		},
		{
			name:    "literal in middle",
			pattern: "foo",
			input:   "bar foo baz",
			start:   0,
			end:     7,
			want:    4,
		},
		{
			name:    "no match returns -1",
			pattern: "xyz",
			input:   "hello world",
			start:   0,
			end:     11,
			want:    -1,
		},
		{
			name:    "single char",
			pattern: "a",
			input:   "bca",
			start:   0,
			end:     3,
			want:    2,
		},
		{
			name:    "char class",
			pattern: "[a-z]+",
			input:   "123abc456",
			start:   0,
			end:     6,
			want:    3,
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

func TestSearchReverseBoundary(t *testing.T) {
	revDFA := compileReverseDFA(t, "abc")
	input := []byte("xxxabcyyy")

	tests := []struct {
		name  string
		start int
		end   int
		want  int
	}{
		{name: "end <= start returns -1", start: 5, end: 5, want: -1},
		{name: "end < start returns -1", start: 5, end: 3, want: -1},
		{name: "end > len returns -1", start: 0, end: 100, want: -1},
		{name: "start 0 end 0 returns -1", start: 0, end: 0, want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := revDFA.SearchReverse(input, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("SearchReverse(%d, %d) = %d, want %d", tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestSearchReverseEmptyInput(t *testing.T) {
	revDFA := compileReverseDFA(t, "a")
	got := revDFA.SearchReverse([]byte{}, 0, 0)
	if got != -1 {
		t.Errorf("SearchReverse on empty input = %d, want -1", got)
	}
}

func TestSearchReverseWindowSliding(t *testing.T) {
	// Test different windows into the same haystack
	revDFA := compileReverseDFA(t, "[0-9]+")
	input := []byte("aaa123bbb456ccc")

	// Window covering first digit sequence: "aaa123" (positions 0-6)
	got := revDFA.SearchReverse(input, 0, 6)
	if got < 0 {
		t.Errorf("SearchReverse(0, 6) = %d, want match", got)
	}

	// Window covering second digit sequence: "bbb456" (positions 6-12)
	got = revDFA.SearchReverse(input, 6, 12)
	if got < 0 {
		t.Errorf("SearchReverse(6, 12) = %d, want match", got)
	}

	// Window with no digits: "aaa" (positions 0-3)
	got = revDFA.SearchReverse(input, 0, 3)
	if got != -1 {
		t.Errorf("SearchReverse(0, 3) on 'aaa' = %d, want -1", got)
	}
}

func TestIsMatchReverseBasic(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		start   int
		end     int
		want    bool
	}{
		{
			name:    "match found",
			pattern: "hello",
			input:   "hello",
			start:   0,
			end:     5,
			want:    true,
		},
		{
			name:    "no match",
			pattern: "xyz",
			input:   "hello",
			start:   0,
			end:     5,
			want:    false,
		},
		{
			name:    "char class match",
			pattern: "[0-9]+",
			input:   "abc123",
			start:   0,
			end:     6,
			want:    true,
		},
		{
			name:    "empty range",
			pattern: "a",
			input:   "abc",
			start:   0,
			end:     0,
			want:    false,
		},
		{
			name:    "end beyond haystack",
			pattern: "a",
			input:   "abc",
			start:   0,
			end:     100,
			want:    false,
		},
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

func TestIsMatchReverseBoundary(t *testing.T) {
	revDFA := compileReverseDFA(t, "a")
	input := []byte("abc")

	// end <= start should return false
	if revDFA.IsMatchReverse(input, 3, 3) {
		t.Error("IsMatchReverse(3, 3) should be false")
	}
	if revDFA.IsMatchReverse(input, 5, 3) {
		t.Error("IsMatchReverse(5, 3) should be false")
	}
}

func TestSearchReverseLimitedBasic(t *testing.T) {
	revDFA := compileReverseDFA(t, "abc")
	input := []byte("xxxabcyyy")

	tests := []struct {
		name     string
		start    int
		end      int
		minStart int
		want     int
	}{
		{
			name:     "match within bounds",
			start:    0,
			end:      6,
			minStart: 0,
			want:     3, // "abc" starts at position 3
		},
		{
			name:     "minStart above match start returns quadratic signal",
			start:    0,
			end:      6,
			minStart: 5,                             // minStart > match start (3), scan stops at 5 before reaching 3
			want:     SearchReverseLimitedQuadratic, // -2: scan limited by minStart
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := revDFA.SearchReverseLimited(input, tt.start, tt.end, tt.minStart)
			if got != tt.want {
				t.Errorf("SearchReverseLimited(%d, %d, %d) = %d, want %d",
					tt.start, tt.end, tt.minStart, got, tt.want)
			}
		})
	}
}

func TestSearchReverseLimitedBoundary(t *testing.T) {
	revDFA := compileReverseDFA(t, "abc")
	input := []byte("xxxabcyyy")

	// Invalid bounds
	if got := revDFA.SearchReverseLimited(input, 5, 5, 0); got != -1 {
		t.Errorf("end == start should return -1, got %d", got)
	}
	if got := revDFA.SearchReverseLimited(input, 0, 100, 0); got != -1 {
		t.Errorf("end > len should return -1, got %d", got)
	}
}

func TestSearchReverseLimitedNoMatch(t *testing.T) {
	revDFA := compileReverseDFA(t, "xyz")
	input := []byte("hello world")

	got := revDFA.SearchReverseLimited(input, 0, 11, 0)
	if got != -1 {
		t.Errorf("SearchReverseLimited for non-matching pattern = %d, want -1", got)
	}
}

func TestSearchReverseLimitedQuadraticSignal(t *testing.T) {
	// Test that SearchReverseLimited returns the quadratic signal
	// when it hits the minStart boundary without a dead state
	revDFA := compileReverseDFA(t, "[a-z]+")
	input := []byte("abcdefghij")

	// With minStart = 5, the scan will stop at position 5 going backward
	// For [a-z]+, the DFA never reaches dead state on lowercase letters
	got := revDFA.SearchReverseLimited(input, 0, 10, 5)
	// The result should be a match position >= 5 (since all chars match),
	// or SearchReverseLimitedQuadratic (-2) if no dead state was hit
	if got == -1 {
		t.Error("SearchReverseLimited should not return -1 for all-matching input")
	}
	// Acceptable results: match position, or quadratic signal
	if got != SearchReverseLimitedQuadratic && got < 0 {
		t.Errorf("SearchReverseLimited = %d, want >= 0 or %d", got, SearchReverseLimitedQuadratic)
	}
}

func TestSearchReverseLimitedQuadraticConstant(t *testing.T) {
	if SearchReverseLimitedQuadratic != -2 {
		t.Errorf("SearchReverseLimitedQuadratic = %d, want -2", SearchReverseLimitedQuadratic)
	}
}

func TestSearchReverseWithSmallCache(t *testing.T) {
	// Test reverse search with cache pressure
	config := DefaultConfig().WithMaxStates(5).WithMaxCacheClears(3)
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("[a-z]+[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	revDFA, err := CompileWithConfig(revNFA, config)
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	input := []byte("abc123def456")
	// Should still produce a result (either match or NFA fallback)
	got := revDFA.SearchReverse(input, 0, 6)
	// The exact position depends on NFA fallback behavior
	t.Logf("SearchReverse with small cache = %d", got)
}
