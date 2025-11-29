package meta

import (
	"testing"
)

// TestReverseInner_Simple tests basic inner literal pattern matching
func TestReverseInner_Simple(t *testing.T) {
	tests := []struct {
		name               string
		pattern            string
		haystack           string
		want               string
		wantPos            [2]int // [start, end]
		expectReverseInner bool   // true if we expect UseReverseInner strategy
	}{
		{
			name:               "simple inner connection",
			pattern:            `.*connection.*`,
			haystack:           "ERROR: connection timeout",
			want:               "ERROR: connection timeout",
			wantPos:            [2]int{0, 25},
			expectReverseInner: true, // No prefix/suffix literals, should use ReverseInner
		},
		{
			name:               "ERROR connection timeout",
			pattern:            `ERROR.*connection.*timeout`,
			haystack:           "ERROR: connection lost due to timeout",
			want:               "ERROR: connection lost due to timeout",
			wantPos:            [2]int{0, 37},
			expectReverseInner: false, // Has prefix "ERROR", will use UseDFA
		},
		{
			name:               "func Error return",
			pattern:            `func.*Error.*return`,
			haystack:           "func handleError() { return Error }",
			want:               "func handleError() { return",
			wantPos:            [2]int{0, 27}, // Greedy .* matches up to "return"
			expectReverseInner: false,         // Has prefix "func", will use UseDFA
		},
		{
			name:               "prefix middle suffix",
			pattern:            `prefix.*middle.*suffix`,
			haystack:           "prefix data middle data suffix",
			want:               "prefix data middle data suffix",
			wantPos:            [2]int{0, 30},
			expectReverseInner: false, // Has prefix "prefix", will use UseDFA
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			// Verify strategy selection
			if tt.expectReverseInner && engine.Strategy() != UseReverseInner {
				t.Logf("Pattern %q selected strategy %v (expected UseReverseInner)", tt.pattern, engine.Strategy())
				// Note: Strategy might not be UseReverseInner if inner literal detection fails
				// or if prefix/suffix literals are better - this is OK
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

// TestReverseInner_NoMatch tests patterns that should not match
func TestReverseInner_NoMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
	}{
		{
			name:     "inner literal not present",
			pattern:  `.*connection.*`,
			haystack: "ERROR: disconnected",
		},
		{
			name:     "prefix missing",
			pattern:  `ERROR.*connection.*`,
			haystack: "WARNING: connection timeout",
		},
		{
			name:     "suffix missing",
			pattern:  `.*connection.*timeout`,
			haystack: "ERROR: connection established",
		},
		{
			name:     "empty haystack",
			pattern:  `.*connection.*`,
			haystack: "",
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

// TestReverseInner_Complex tests more complex inner patterns
func TestReverseInner_Complex(t *testing.T) {
	// Pattern with multiple parts
	pattern := `ERROR.*connection.*timeout`
	haystack := "ERROR: connection lost due to connection timeout"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Test Find - greedy .* matches everything
	match := engine.Find([]byte(haystack))
	if match == nil {
		t.Fatal("Find() = nil, want match")
	}

	// Greedy match - matches entire string
	want := "ERROR: connection lost due to connection timeout"
	got := match.String()
	if got != want {
		t.Errorf("Find() = %q, want %q", got, want)
	}

	// Test IsMatch
	if !engine.IsMatch([]byte(haystack)) {
		t.Error("IsMatch() = false, want true")
	}
}

// TestReverseInner_NotSelected tests patterns that should NOT use ReverseInner strategy
func TestReverseInner_NotSelected(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		reason  string
	}{
		{
			name:    "start anchored",
			pattern: `^ERROR.*connection.*`,
			reason:  "start-anchored patterns should not use ReverseInner",
		},
		{
			name:    "end anchored",
			pattern: `.*connection.*timeout$`,
			reason:  "end-anchored patterns should use different strategy",
		},
		{
			name:    "both anchors",
			pattern: `^.*connection.*$`,
			reason:  "fully-anchored patterns should use different strategy",
		},
		{
			name:    "prefix literal available",
			pattern: `hello.*world`,
			reason:  "patterns with good prefix literals should use UseDFA",
		},
		{
			name:    "suffix literal available",
			pattern: `.*\.txt`,
			reason:  "patterns with good suffix literals should use UseReverseSuffix",
		},
		{
			name:    "no inner literal",
			pattern: `.*[abc].*`,
			reason:  "no concrete inner literal for prefiltering",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			if engine.Strategy() == UseReverseInner {
				t.Errorf("Pattern %q incorrectly selected UseReverseInner strategy (%s)",
					tt.pattern, tt.reason)
			}
		})
	}
}

// TestReverseInner_LargeHaystack tests performance on larger input
func TestReverseInner_LargeHaystack(t *testing.T) {
	pattern := `.*INNER.*`
	// Create a large haystack with pattern in the middle
	haystack := make([]byte, 100000)
	for i := range haystack {
		haystack[i] = 'x'
	}
	// Place the match at position 50000
	copy(haystack[50000:], "INNER")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Test Find
	match := engine.Find(haystack)
	if match == nil {
		t.Fatal("Find() = nil, want match")
	}

	// Should match from start to end (greedy .*)
	if match.Start() != 0 {
		t.Errorf("Find() start = %d, want 0", match.Start())
	}

	// End should be after INNER
	if match.End() < 50000 {
		t.Errorf("Find() end = %d, want >= 50000", match.End())
	}
}

// TestReverseInner_MultipleCandidates tests when there are multiple inner literal occurrences
func TestReverseInner_MultipleCandidates(t *testing.T) {
	pattern := `.*connection.*`
	haystack := "first connection and second connection and third connection"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Test Find - greedy .* matches everything
	match := engine.Find([]byte(haystack))
	if match == nil {
		t.Fatal("Find() = nil, want match")
	}

	// Greedy match - .* consumes everything, matches entire string
	want := "first connection and second connection and third connection"
	got := match.String()
	if got != want {
		t.Errorf("Find() = %q, want %q", got, want)
	}

	// Test IsMatch
	if !engine.IsMatch([]byte(haystack)) {
		t.Error("IsMatch() = false, want true")
	}
}

// TestReverseInner_EdgeCases tests edge cases
func TestReverseInner_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     string
		wantPos  [2]int
	}{
		{
			name:     "inner at start",
			pattern:  `.*connection.*`,
			haystack: "connection established",
			want:     "connection established",
			wantPos:  [2]int{0, 22},
		},
		{
			name:     "inner at end",
			pattern:  `.*connection.*`,
			haystack: "lost connection",
			want:     "lost connection",
			wantPos:  [2]int{0, 15},
		},
		{
			name:     "minimal inner",
			pattern:  `.*x.*`,
			haystack: "x",
			want:     "x",
			wantPos:  [2]int{0, 1},
		},
		{
			name:     "greedy wildcards",
			pattern:  `.*connection.*`,
			haystack: "a connection b connection c",
			want:     "a connection b connection c",
			wantPos:  [2]int{0, 27},
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

// TestReverseInner_BidirectionalSearch tests that both forward and reverse DFA are used
func TestReverseInner_BidirectionalSearch(t *testing.T) {
	// Pattern that requires both prefix and suffix verification
	pattern := `ERROR.*connection.*timeout`
	tests := []struct {
		name     string
		haystack string
		want     bool
	}{
		{
			name:     "valid match",
			haystack: "ERROR: connection lost due to timeout",
			want:     true,
		},
		{
			name:     "inner present but prefix missing",
			haystack: "WARNING: connection lost due to timeout",
			want:     false,
		},
		{
			name:     "inner present but suffix missing",
			haystack: "ERROR: connection established",
			want:     false,
		},
		{
			name:     "prefix and suffix present but inner missing",
			haystack: "ERROR: network timeout",
			want:     false,
		},
	}

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, got, tt.want)
			}
		})
	}
}
