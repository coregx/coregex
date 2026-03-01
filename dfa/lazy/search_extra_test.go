package lazy

import (
	"regexp"
	"strings"
	"testing"

	"github.com/coregx/coregex/nfa"
)

// TestSearchAtPositionVariations tests SearchAt and FindAt from various positions.
func TestSearchAtPositionVariations(t *testing.T) {
	dfa, err := CompilePattern("foo")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	input := []byte("foo bar foo baz foo")

	tests := []struct {
		name string
		at   int
		want int
	}{
		{"from 0", 0, 3},
		{"from 1 (past first)", 1, 11},
		{"from 4 (second foo)", 4, 11},
		{"from 12 (third foo)", 12, 19},
		{"past all matches", 17, -1},
		{"at end", len(input), -1},
		{"past end", len(input) + 1, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dfa.FindAt(input, tt.at)
			if got != tt.want {
				t.Errorf("FindAt(%d) = %d, want %d", tt.at, got, tt.want)
			}
		})
	}
}

// TestSearchAtAnchored tests the anchored search from various positions.
func TestSearchAtAnchored(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		at      int
		want    int
	}{
		{"match at position", "foo", "xfoox", 1, 4},
		{"no match at position", "foo", "xbarx", 1, -1},
		{"match at start", "hello", "hello world", 0, 5},
		{"no match past start", "^hello", "hello world", 1, -1},
		{"empty at end", "a*", "abc", 3, 3},
		{"past end", "abc", "abc", 4, -1},
		{"at end no match", "abc", "abc", 3, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
			}

			got := dfa.SearchAtAnchored([]byte(tt.input), tt.at)
			if got != tt.want {
				t.Errorf("SearchAtAnchored(%q, %d) = %d, want %d",
					tt.input, tt.at, got, tt.want)
			}
		})
	}
}

// TestSearchAtWithoutPrefilter tests SearchAt (the non-prefilter variant).
func TestSearchAtWithoutPrefilter(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		at      int
		want    int
	}{
		{"simple literal from 0", "abc", "xyzabc", 0, 6},
		{"simple literal from 3", "abc", "xyzabc", 3, 6},
		{"no match", "xyz", "abcdef", 0, -1},
		{"empty pattern from 0", "", "abc", 0, 3}, // empty pattern greedy-matches entire input
		{"empty input", "abc", "", 0, -1},
		{"at end", "abc", "abc", 3, -1},
		{"past end", "abc", "abc", 4, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
			}

			got := dfa.SearchAt([]byte(tt.input), tt.at)
			if got != tt.want {
				t.Errorf("SearchAt(%q, %d) = %d, want %d",
					tt.input, tt.at, got, tt.want)
			}
		})
	}
}

// TestIsMatchFindConsistency tests that IsMatch and Find agree on match existence.
func TestIsMatchFindConsistency(t *testing.T) {
	patterns := []string{
		"hello",
		"a+",
		"a*b*",
		"foo|bar",
		"[a-z]+",
		"a+b+c+",
		`\bword\b`,
		`[0-9]{2,4}`,
		"^test",
	}

	inputs := []string{
		"hello world",
		"aaabbbccc",
		"test123end",
		"foo bar baz",
		"xyzabc123",
		"",
		"a",
		"word is here",
		"1234",
		"test at start",
	}

	for _, pattern := range patterns {
		for _, input := range inputs {
			t.Run(pattern+"_"+input, func(t *testing.T) {
				dfa, err := CompilePattern(pattern)
				if err != nil {
					t.Skipf("Pattern %q not supported: %v", pattern, err)
					return
				}

				isMatch := dfa.IsMatch([]byte(input))
				findResult := dfa.Find([]byte(input))
				findFound := findResult >= 0

				if isMatch != findFound {
					t.Errorf("Inconsistency: IsMatch=%v but Find=%d (found=%v)",
						isMatch, findResult, findFound)
				}
			})
		}
	}
}

// TestCachePressureWithManyPatterns tests DFA behavior under cache pressure
// with patterns that generate many unique states.
func TestCachePressureWithManyPatterns(t *testing.T) {
	patterns := []struct {
		name    string
		pattern string
		input   string
	}{
		{"deep alternation", "a|b|c|d|e|f|g|h|i|j", "j"},
		{"nested repetition", "(a+)(b+)(c+)(d+)", "aabbccdd"},
		{"wide char class", "[a-zA-Z0-9!@#$%]+", "Test123!@#"},
		{"complex concat", "a+b+c+d+e+f+", "aabbccddeeff"},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			// Use a small cache to force cache pressure
			config := DefaultConfig().WithMaxStates(10).WithMaxCacheClears(5)
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			dfa, err := CompileWithConfig(nfaObj, config)
			if err != nil {
				t.Fatalf("DFA compile error: %v", err)
			}

			// Should still produce correct result (via NFA fallback if needed)
			got := dfa.IsMatch([]byte(tt.input))
			re := regexp.MustCompile(tt.pattern)
			want := re.MatchString(tt.input)

			if got != want {
				t.Errorf("IsMatch(%q, %q) under cache pressure = %v, stdlib = %v",
					tt.pattern, tt.input, got, want)
			}
		})
	}
}

// TestLargeInputSearchCorrectness tests DFA search on larger inputs.
func TestLargeInputSearchCorrectness(t *testing.T) {
	patterns := []struct {
		name     string
		pattern  string
		haystack func() string
		want     bool
	}{
		{
			name:    "literal in 1KB",
			pattern: "needle",
			haystack: func() string {
				return strings.Repeat("x", 500) + "needle" + strings.Repeat("y", 500)
			},
			want: true,
		},
		{
			name:    "no match in 1KB",
			pattern: "needle",
			haystack: func() string {
				return strings.Repeat("x", 1024)
			},
			want: false,
		},
		{
			name:    "pattern at very end",
			pattern: "end",
			haystack: func() string {
				return strings.Repeat("x", 997) + "end"
			},
			want: true,
		},
		{
			name:    "char class across large input",
			pattern: "[0-9]+",
			haystack: func() string {
				return strings.Repeat("a", 500) + "12345" + strings.Repeat("b", 500)
			},
			want: true,
		},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern error: %v", err)
			}

			input := []byte(tt.haystack())
			got := dfa.IsMatch(input)
			if got != tt.want {
				t.Errorf("IsMatch on %d bytes = %v, want %v", len(input), got, tt.want)
			}

			// Verify with Find
			findResult := dfa.Find(input)
			findFound := findResult >= 0
			if findFound != tt.want {
				t.Errorf("Find on %d bytes = %d (found=%v), want found=%v",
					len(input), findResult, findFound, tt.want)
			}
		})
	}
}

// TestSearchAtVsStdlib does a comprehensive comparison of DFA SearchAt results
// against stdlib regexp for various patterns and starting positions.
func TestSearchAtVsStdlib(t *testing.T) {
	type testCase struct {
		pattern string
		input   string
	}

	cases := []testCase{
		{"hello", "hello world"},
		{"a+", "bbbaaa"},
		{"[0-9]+", "abc123def456"},
		{"foo|bar", "bar baz foo"},
		{"a*b*", "aaabbb"},
	}

	for _, tc := range cases {
		t.Run(tc.pattern+"_"+tc.input, func(t *testing.T) {
			dfa, err := CompilePattern(tc.pattern)
			if err != nil {
				t.Fatalf("CompilePattern error: %v", err)
			}

			re := regexp.MustCompile(tc.pattern)

			// Compare Find (at=0) with stdlib
			dfaEnd := dfa.Find([]byte(tc.input))
			loc := re.FindStringIndex(tc.input)

			var stdlibEnd int
			if loc == nil {
				stdlibEnd = -1
			} else {
				stdlibEnd = loc[1]
			}

			if dfaEnd != stdlibEnd {
				t.Errorf("Find(%q, %q) = %d, stdlib = %d",
					tc.pattern, tc.input, dfaEnd, stdlibEnd)
			}
		})
	}
}

// TestEmptyPatternBehavior tests that empty pattern matches correctly.
func TestEmptyPatternBehavior(t *testing.T) {
	dfa, err := CompilePattern("")
	if err != nil {
		t.Fatalf("CompilePattern('') error: %v", err)
	}

	tests := []struct {
		name      string
		input     string
		findWant  int
		matchWant bool
	}{
		{"empty input", "", 0, true},
		{"non-empty input", "abc", 3, true}, // empty pattern greedy-matches entire input
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dfa.Find([]byte(tt.input))
			if got != tt.findWant {
				t.Errorf("Find(%q) = %d, want %d", tt.input, got, tt.findWant)
			}

			isMatch := dfa.IsMatch([]byte(tt.input))
			if isMatch != tt.matchWant {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, isMatch, tt.matchWant)
			}
		})
	}
}

// TestSearchAtAnchoredEmptyPattern tests edge cases with empty pattern and anchored search.
func TestSearchAtAnchoredEmptyPattern(t *testing.T) {
	dfa, err := CompilePattern("")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	// Empty pattern matches at any position (returns that position)
	tests := []struct {
		at   int
		want int
	}{
		{0, 0},
		{1, 1},
		{3, 3},
	}

	input := []byte("abc")
	for _, tt := range tests {
		got := dfa.SearchAtAnchored(input, tt.at)
		if got != tt.want {
			t.Errorf("SearchAtAnchored(%q, %d) = %d, want %d", input, tt.at, got, tt.want)
		}
	}
}

// TestCacheStatsAfterSearch verifies that CacheStats returns sane values.
func TestCacheStatsAfterSearch(t *testing.T) {
	dfa, err := CompilePattern("[a-z]+[0-9]+")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	// Search to populate cache
	dfa.Find([]byte("abc123"))
	dfa.Find([]byte("xyz789"))

	size, capacity, hits, misses, hitRate := dfa.CacheStats()

	if size == 0 {
		t.Error("Expected non-zero cache size after search")
	}
	if capacity == 0 {
		t.Error("Expected non-zero capacity")
	}

	t.Logf("Cache stats: size=%d, capacity=%d, hits=%d, misses=%d, hitRate=%.2f",
		size, capacity, hits, misses, hitRate)
}

// TestResetCacheRecovery tests that the DFA works correctly after a cache reset.
func TestResetCacheRecovery(t *testing.T) {
	dfa, err := CompilePattern("hello")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	input := []byte("say hello there")

	// First search
	got1 := dfa.Find(input)
	if got1 != 9 {
		t.Fatalf("First Find = %d, want 9", got1)
	}

	// Reset cache
	dfa.ResetCache()

	// Second search should still work
	got2 := dfa.Find(input)
	if got2 != 9 {
		t.Errorf("Find after ResetCache = %d, want 9", got2)
	}

	// IsMatch should also work
	if !dfa.IsMatch(input) {
		t.Error("IsMatch after ResetCache returned false")
	}
}

// TestAnchoredPatternAtNonZeroPosition tests that anchored patterns (^)
// correctly fail when searched from non-zero positions.
func TestAnchoredPatternAtNonZeroPosition(t *testing.T) {
	dfa, err := CompilePattern("^hello")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	// Match at position 0
	got := dfa.Find([]byte("hello world"))
	if got != 5 {
		t.Errorf("Find('^hello', 'hello world') = %d, want 5", got)
	}

	// Should NOT match when searching from position > 0
	got = dfa.FindAt([]byte("say hello world"), 4)
	if got != -1 {
		t.Errorf("FindAt('^hello', offset=4) = %d, want -1 (anchored)", got)
	}

	// IsMatch should reflect the same
	if dfa.IsMatch([]byte("hello world")) != true {
		t.Error("IsMatch should be true when pattern starts at position 0")
	}
	if dfa.IsMatch([]byte("say hello")) != false {
		t.Error("IsMatch should be false when ^hello doesn't start at position 0")
	}
}

// TestMatchesEmptyForVariousPatterns tests the matchesEmpty behavior.
func TestMatchesEmptyForVariousPatterns(t *testing.T) {
	tests := []struct {
		pattern   string
		wantEmpty bool
	}{
		{"", true},
		{"a*", true},
		{"a?", true},
		{"a+", false},
		{"hello", false},
		{"(a|b)*", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern error: %v", err)
			}

			// Test via Find with empty input
			got := dfa.Find([]byte{})
			if tt.wantEmpty && got < 0 {
				t.Errorf("Find(empty) = %d, want >= 0 for empty-matching pattern", got)
			}
			if !tt.wantEmpty && got >= 0 {
				t.Errorf("Find(empty) = %d, want -1 for non-empty-matching pattern", got)
			}

			// Test via IsMatch
			isMatch := dfa.IsMatch([]byte{})
			if isMatch != tt.wantEmpty {
				t.Errorf("IsMatch(empty) = %v, want %v", isMatch, tt.wantEmpty)
			}
		})
	}
}

// TestUnicodePatterns tests DFA behavior with Unicode character classes.
func TestUnicodePatterns(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		wantMatch bool
	}{
		{"dot matches ascii", ".", "a", true},
		{"dot matches utf8", ".", "\xc3\xa9", true}, // é (2-byte UTF-8)
		{"ascii class", "[a-z]+", "hello", true},
		{"digit class", `\d+`, "12345", true},
		{"word class", `\w+`, "hello_world", true},
		{"space class", `\s+`, "  \t\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
			}

			got := dfa.IsMatch([]byte(tt.input))
			if got != tt.wantMatch {
				t.Errorf("IsMatch(%q, %q) = %v, want %v",
					tt.pattern, tt.input, got, tt.wantMatch)
			}
		})
	}
}
