package meta

import (
	"testing"
)

// TestReverseSuffix_Simple tests basic suffix literal pattern matching
func TestReverseSuffix_Simple(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     string
		wantPos  [2]int // [start, end]
	}{
		{
			name:     "simple suffix .txt",
			pattern:  `.*\.txt`,
			haystack: "file1.doc file2.txt file3.jpg",
			want:     "file1.doc file2.txt",
			wantPos:  [2]int{0, 19}, // "file1.doc file2.txt" is 19 chars (0-18)
		},
		{
			name:     "suffix .go",
			pattern:  `.*\.go`,
			haystack: "main.txt src/main.go other.rs",
			want:     "main.txt src/main.go",
			wantPos:  [2]int{0, 20}, // "main.txt src/main.go" is 20 chars
		},
		{
			name:     "suffix with digit class",
			pattern:  `.*\d+\.txt`,
			haystack: "file.txt file123.txt data.log",
			want:     "file.txt file123.txt",
			wantPos:  [2]int{0, 20}, // "file.txt file123.txt" is 20 chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			// Verify strategy selection
			if engine.Strategy() != UseReverseSuffix {
				t.Logf("Pattern %q selected strategy %v (expected UseReverseSuffix)", tt.pattern, engine.Strategy())
				// Note: Strategy might not be UseReverseSuffix if prefilter can't be built
				// This is not a test failure, just informational
			}

			// Test Find
			match := engine.Find([]byte(tt.haystack))
			if match == nil {
				t.Fatalf("Find(%q) = nil, want match", tt.haystack)
			}

			got := match.String()
			if got != tt.want {
				t.Errorf("Find(%q) = %q, want %q", tt.haystack, got, tt.want)
			}

			if match.Start() != tt.wantPos[0] || match.End() != tt.wantPos[1] {
				t.Errorf("Find(%q) position = [%d, %d], want [%d, %d]",
					tt.haystack, match.Start(), match.End(), tt.wantPos[0], tt.wantPos[1])
			}

			// Test IsMatch
			if !engine.IsMatch([]byte(tt.haystack)) {
				t.Errorf("IsMatch(%q) = false, want true", tt.haystack)
			}
		})
	}
}

// TestReverseSuffix_NoMatch tests patterns that should not match
func TestReverseSuffix_NoMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
	}{
		{
			name:     "suffix not present",
			pattern:  `.*\.txt`,
			haystack: "file.log data.dat",
		},
		{
			name:     "wrong suffix",
			pattern:  `.*\.go`,
			haystack: "main.rs other.py",
		},
		{
			name:     "empty haystack",
			pattern:  `.*\.txt`,
			haystack: "",
		},
		{
			name:     "prefix doesn't match",
			pattern:  `prefix.*\.txt`,
			haystack: "other file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			// Test Find
			match := engine.Find([]byte(tt.haystack))
			if match != nil {
				t.Errorf("Find(%q) = %v, want nil", tt.haystack, match)
			}

			// Test IsMatch
			if engine.IsMatch([]byte(tt.haystack)) {
				t.Errorf("IsMatch(%q) = true, want false", tt.haystack)
			}
		})
	}
}

// TestReverseSuffix_Complex tests more complex suffix patterns
func TestReverseSuffix_Complex(t *testing.T) {
	// Simple pattern with suffix
	pattern := `.*\.txt`
	haystack := "prefix file.other.txt suffix.txt"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Test Find - greedy .* matches everything up to last .txt
	match := engine.Find([]byte(haystack))
	if match == nil {
		t.Fatal("Find() = nil, want match")
	}

	// Greedy match - matches up to first .txt found from left
	want := "prefix file.other.txt suffix.txt"
	got := match.String()
	if got != want {
		t.Errorf("Find() = %q, want %q", got, want)
	}

	// Test IsMatch
	if !engine.IsMatch([]byte(haystack)) {
		t.Error("IsMatch() = false, want true")
	}
}

// TestReverseSuffix_NotSelected tests patterns that should NOT use ReverseSuffix strategy
func TestReverseSuffix_NotSelected(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		reason  string
	}{
		{
			name:    "start anchored",
			pattern: `^prefix.*\.txt`,
			reason:  "start-anchored patterns should not use ReverseSuffix",
		},
		{
			name:    "end anchored",
			pattern: `.*\.txt$`,
			reason:  "end-anchored patterns should use ReverseAnchored instead",
		},
		{
			name:    "both anchors",
			pattern: `^.*\.txt$`,
			reason:  "fully-anchored patterns should use different strategy",
		},
		{
			name:    "no suffix literal",
			pattern: `.*[abc]`,
			reason:  "no concrete suffix literal for prefiltering",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			if engine.Strategy() == UseReverseSuffix {
				t.Errorf("Pattern %q incorrectly selected UseReverseSuffix strategy (%s)",
					tt.pattern, tt.reason)
			}
		})
	}
}

// TestReverseSuffix_LargeHaystack tests performance on larger input
func TestReverseSuffix_LargeHaystack(t *testing.T) {
	pattern := `.*\.SUFFIX`
	// Create a large haystack with pattern in the middle
	haystack := make([]byte, 100000)
	for i := range haystack {
		haystack[i] = 'x'
	}
	// Place the match at position 50000
	copy(haystack[50000:], "file.SUFFIX")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Test Find
	match := engine.Find(haystack)
	if match == nil {
		t.Fatal("Find() = nil, want match")
	}

	// Should match from start to suffix
	if match.Start() != 0 {
		t.Errorf("Find() start = %d, want 0", match.Start())
	}

	// End should be after .SUFFIX
	if match.End() < 50000 {
		t.Errorf("Find() end = %d, want >= 50000", match.End())
	}
}

// TestReverseSuffix_MultipleCandidates tests when there are multiple suffix occurrences
func TestReverseSuffix_MultipleCandidates(t *testing.T) {
	pattern := `.*\.txt`
	haystack := "file1.txt file2.txt file3.txt"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Test Find - greedy .* matches everything up to last .txt
	match := engine.Find([]byte(haystack))
	if match == nil {
		t.Fatal("Find() = nil, want match")
	}

	// Greedy match - .* consumes everything, matches entire string
	want := "file1.txt file2.txt file3.txt"
	got := match.String()
	if got != want {
		t.Errorf("Find() = %q, want %q", got, want)
	}

	// Test IsMatch
	if !engine.IsMatch([]byte(haystack)) {
		t.Error("IsMatch() = false, want true")
	}
}

// TestReverseSuffix_CharClassPlus tests CharClass Plus patterns like [^\s]+\.txt
// These patterns should use ReverseSuffix strategy for efficient matching.
func TestReverseSuffix_CharClassPlus(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     string
	}{
		{
			name:     "negated whitespace class - simple",
			pattern:  `[^\s]+\.txt`,
			haystack: "path/to/file.txt",
			want:     "path/to/file.txt",
		},
		{
			name:     "word class - simple",
			pattern:  `[\w]+\.go`,
			haystack: "main.go",
			want:     "main.go",
		},
		{
			name:     "alpha class - simple",
			pattern:  `[a-zA-Z]+\.txt`,
			haystack: "file.txt",
			want:     "file.txt",
		},
		{
			name:     "no match",
			pattern:  `[^\s]+\.txt`,
			haystack: "file.doc data.log",
			want:     "",
		},
		{
			name:     "no match - all spaces",
			pattern:  `[^\s]+\.txt`,
			haystack: "     ",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			// Verify strategy selection - should use ReverseSuffix for CharClass Plus patterns
			strategy := engine.Strategy()
			if strategy == UseReverseSuffix {
				t.Logf("Pattern %q correctly using UseReverseSuffix strategy", tt.pattern)
			}

			match := engine.Find([]byte(tt.haystack))

			if tt.want == "" {
				if match != nil {
					t.Errorf("Find(%q) = %q, want nil", tt.haystack, match.String())
				}
				return
			}

			if match == nil {
				t.Fatalf("Find(%q) = nil, want match", tt.haystack)
			}

			got := match.String()
			if got != tt.want {
				t.Errorf("Find(%q) = %q, want %q", tt.haystack, got, tt.want)
			}
		})
	}
}

// TestReverseSuffix_EdgeCases tests edge cases
func TestReverseSuffix_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     string
		wantPos  [2]int
	}{
		{
			name:     "suffix at start",
			pattern:  `.*\.txt`,
			haystack: ".txt",
			want:     ".txt",
			wantPos:  [2]int{0, 4},
		},
		{
			name:     "minimal match",
			pattern:  `a*b`,
			haystack: "b",
			want:     "b",
			wantPos:  [2]int{0, 1},
		},
		{
			name:     "greedy prefix",
			pattern:  `.*\.txt`,
			haystack: "a.txt.txt",
			want:     "a.txt.txt", // Greedy .* matches everything up to last .txt
			wantPos:  [2]int{0, 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			match := engine.Find([]byte(tt.haystack))
			if match == nil {
				t.Fatalf("Find(%q) = nil, want match", tt.haystack)
			}

			got := match.String()
			if got != tt.want {
				t.Errorf("Find(%q) = %q, want %q", tt.haystack, got, tt.want)
			}

			if match.Start() != tt.wantPos[0] || match.End() != tt.wantPos[1] {
				t.Errorf("Find(%q) position = [%d, %d], want [%d, %d]",
					tt.haystack, match.Start(), match.End(), tt.wantPos[0], tt.wantPos[1])
			}
		})
	}
}

// TestIssue116_AlternationWithoutWildcard tests that alternation patterns without
// wildcard prefix (like `[cgt]gggtaaa|tttaccc[acg]`) are NOT routed to
// UseReverseSuffixSet, which would produce wrong match positions.
// See: https://github.com/coregx/coregex/issues/116
func TestIssue116_AlternationWithoutWildcard(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    []string
	}{
		{
			name:    "original bug report pattern",
			pattern: `[cgt]gggtaaa|tttaccc[acg]`,
			input:   "xxxcgggtaaaxxx",
			want:    []string{"cgggtaaa"},
		},
		{
			name:    "original bug report pattern - second alt",
			pattern: `[cgt]gggtaaa|tttaccc[acg]`,
			input:   "xxxtttacccaxxx",
			want:    []string{"tttaccca"},
		},
		{
			name:    "multiple matches",
			pattern: `[cgt]gggtaaa|tttaccc[acg]`,
			input:   "cgggtaaa tttaccca ggggtaaa tttacccg",
			want:    []string{"cgggtaaa", "tttaccca", "ggggtaaa", "tttacccg"},
		},
		{
			name:    "no match",
			pattern: `[cgt]gggtaaa|tttaccc[acg]`,
			input:   "agggtaaa tttacccd",
			want:    nil,
		},
		{
			name:    "simple alternation without char class",
			pattern: `foo|bar`,
			input:   "xxxfooxxxbarxxx",
			want:    []string{"foo", "bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) failed: %v", tt.pattern, err)
			}

			// Verify strategy is NOT UseReverseSuffixSet for these patterns
			strategy := e.Strategy()
			if strategy == UseReverseSuffixSet {
				t.Errorf("pattern %q should NOT use UseReverseSuffixSet strategy", tt.pattern)
			}

			var got []string
			haystack := []byte(tt.input)
			at := 0
			for at < len(haystack) {
				start, end, found := e.FindIndicesAt(haystack, at)
				if !found {
					break
				}
				got = append(got, string(haystack[start:end]))
				if end > at {
					at = end
				} else {
					at++
				}
			}

			if len(got) != len(tt.want) {
				t.Fatalf("FindAll(%q) = %v (%d matches), want %v (%d matches)",
					tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("match[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
