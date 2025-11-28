package meta

import (
	"testing"
)

// TestReverseAnchored_Simple tests basic end-anchored pattern matching
func TestReverseAnchored_Simple(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     string
		wantPos  [2]int // [start, end]
	}{
		{
			name:     "simple literal with $",
			pattern:  "abc$",
			haystack: "xxxabc",
			want:     "abc",
			wantPos:  [2]int{3, 6},
		},
		{
			name:     "pattern with dot and $",
			pattern:  "a.c$",
			haystack: "xxxabc",
			want:     "abc",
			wantPos:  [2]int{3, 6},
		},
		{
			name:     "character class with $",
			pattern:  "[abc]$",
			haystack: "xxxb",
			want:     "b",
			wantPos:  [2]int{3, 4},
		},
		{
			name:     "longer pattern with $",
			pattern:  "hello world$",
			haystack: "say hello world",
			want:     "hello world",
			wantPos:  [2]int{4, 15},
		},
		{
			name:     "pattern with repetition and $",
			pattern:  "a+b$",
			haystack: "xxxaaab",
			want:     "aaab",
			wantPos:  [2]int{3, 7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			// Verify strategy selection
			if engine.Strategy() != UseReverseAnchored {
				t.Errorf("Expected UseReverseAnchored strategy, got %v", engine.Strategy())
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

// TestReverseAnchored_NoMatch tests patterns that should not match
func TestReverseAnchored_NoMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
	}{
		{
			name:     "literal not at end",
			pattern:  "abc$",
			haystack: "abcxxx",
		},
		{
			name:     "pattern too long",
			pattern:  "hello$",
			haystack: "hel",
		},
		{
			name:     "wrong pattern",
			pattern:  "xyz$",
			haystack: "abc",
		},
		{
			name:     "empty haystack",
			pattern:  "a$",
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

// TestReverseAnchored_Complex tests the Easy1 pattern that motivated this optimization
func TestReverseAnchored_Complex(t *testing.T) {
	// Easy1 pattern from benchmarks
	pattern := "A[AB]B[BC]C[CD]D[DE]E[EF]F[FG]G[GH]H[HI]I[IJ]J$"
	haystack := "xxxxxxxABBCCDDEEFFGGHHIIJJ"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Verify strategy selection
	if engine.Strategy() != UseReverseAnchored {
		t.Errorf("Expected UseReverseAnchored strategy, got %v", engine.Strategy())
	}

	// Test Find
	match := engine.Find([]byte(haystack))
	if match == nil {
		t.Fatal("Find() = nil, want match")
	}

	want := "ABBCCDDEEFFGGHHIIJJ"
	got := match.String()
	if got != want {
		t.Errorf("Find() = %q, want %q", got, want)
	}

	// Test IsMatch
	if !engine.IsMatch([]byte(haystack)) {
		t.Error("IsMatch() = false, want true")
	}
}

// TestReverseAnchored_NotSelected tests patterns that should NOT use reverse strategy
func TestReverseAnchored_NotSelected(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		strategy Strategy
	}{
		{
			name:     "no end anchor",
			pattern:  "abc",
			strategy: UseNFA, // Tiny pattern
		},
		{
			name:     "both anchors",
			pattern:  "^abc$",
			strategy: UseNFA, // Start-anchored, so not reverse
		},
		{
			name:     "start anchor only",
			pattern:  "^abc",
			strategy: UseNFA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error = %v", tt.pattern, err)
			}

			if engine.Strategy() == UseReverseAnchored {
				t.Errorf("Pattern %q incorrectly selected UseReverseAnchored strategy", tt.pattern)
			}
		})
	}
}

// TestReverseAnchored_LargeHaystack tests performance on larger input
func TestReverseAnchored_LargeHaystack(t *testing.T) {
	pattern := "SUFFIX$"
	// Create a large haystack with pattern at the end
	haystack := make([]byte, 10000)
	for i := range haystack {
		haystack[i] = 'x'
	}
	copy(haystack[len(haystack)-6:], "SUFFIX")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Verify strategy selection
	if engine.Strategy() != UseReverseAnchored {
		t.Errorf("Expected UseReverseAnchored strategy, got %v", engine.Strategy())
	}

	// Test Find
	match := engine.Find(haystack)
	if match == nil {
		t.Fatal("Find() = nil, want match")
	}

	want := "SUFFIX"
	got := match.String()
	if got != want {
		t.Errorf("Find() = %q, want %q", got, want)
	}

	// Verify positions
	if match.Start() != 9994 || match.End() != 10000 {
		t.Errorf("Find() position = [%d, %d], want [9994, 10000]",
			match.Start(), match.End())
	}
}

// TestReverseAnchored_AlternationWithEndAnchor tests alternation where all branches are end-anchored
func TestReverseAnchored_AlternationWithEndAnchor(t *testing.T) {
	pattern := "(abc|xyz)$"
	tests := []struct {
		haystack string
		want     string
	}{
		{"prefixabc", "abc"},
		{"prefixxyz", "xyz"},
		{"prefixabc", "abc"},
	}

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", pattern, err)
	}

	// Verify strategy selection
	if engine.Strategy() != UseReverseAnchored {
		t.Errorf("Expected UseReverseAnchored strategy, got %v", engine.Strategy())
	}

	for _, tt := range tests {
		t.Run(tt.haystack, func(t *testing.T) {
			match := engine.Find([]byte(tt.haystack))
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
