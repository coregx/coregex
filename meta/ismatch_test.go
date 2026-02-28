package meta

import (
	"strings"
	"testing"
)

// TestIsMatchStrategyDispatch tests IsMatch through various pattern types
// that trigger different execution strategies.
func TestIsMatchStrategyDispatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		// Simple literals / NFA path
		{"literal match", "hello", "say hello world", true},
		{"literal no match", "hello", "goodbye world", false},
		{"single char", "x", "abcxdef", true},
		{"single char no match", "x", "abcdef", false},

		// Digit class / char class searcher
		{"digit class match", `\d+`, "abc 123 def", true},
		{"digit class no match", `\d+`, "no digits here", false},
		{"word class match", `\w+`, "hello", true},
		{"word class no match", `\w+`, "   ", false},

		// Anchored patterns
		{"start anchored match", `^hello`, "hello world", true},
		{"start anchored no match", `^hello`, "say hello", false},
		{"end anchored match", `world$`, "hello world", true},
		{"end anchored no match", `world$`, "world hello", false},
		{"fully anchored match", `^hello$`, "hello", true},
		{"fully anchored no match", `^hello$`, "hello world", false},

		// Alternations
		{"alternation first branch", "foo|bar|baz", "prefix foo suffix", true},
		{"alternation second branch", "foo|bar|baz", "prefix bar suffix", true},
		{"alternation third branch", "foo|bar|baz", "prefix baz suffix", true},
		{"alternation no match", "foo|bar|baz", "prefix qux suffix", false},

		// Dot-star patterns (reverse suffix / reverse inner)
		{"dot-star suffix match", `.*\.txt`, "document.txt", true},
		{"dot-star suffix no match", `.*\.txt`, "document.pdf", false},
		{"dot-star inner match", `.*keyword.*`, "has keyword inside", true},
		{"dot-star inner no match", `.*keyword.*`, "nothing here", false},

		// Character class patterns
		{"bracket class match", `[a-z]+`, "Hello World", true},
		{"bracket class no match", `[a-z]+`, "12345", false},
		{"hex pattern match", `[0-9a-f]+`, "deadbeef", true},

		// Composite patterns
		{"alpha digit match", `[a-zA-Z]+[0-9]+`, "abc123", true},
		{"alpha digit no match", `[a-zA-Z]+[0-9]+`, "abc", false},

		// Empty pattern / empty input
		{"empty pattern on text", "", "test", true},
		{"empty pattern on empty input", "", "", true},
		{"pattern on empty input", "a", "", false},
		{"digit on empty input", `\d+`, "", false},

		// Repetitions
		{"star match", "a*b", "aaab", true},
		{"star no match", "a*b", "aaa", false},
		{"plus match", "a+", "aaa", true},
		{"plus no match", "a+", "bbb", false},
		{"question match", "ab?c", "ac", true},
		{"question match with opt", "ab?c", "abc", true},

		// Captures (should not affect IsMatch correctness)
		{"with capture match", `(\w+)@(\w+)`, "user@host", true},
		{"with capture no match", `(\w+)@(\w+)`, "no at sign", false},

		// Unicode
		{"ascii in unicode haystack", "hello", "say hello \u00e9", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v (strategy: %s)",
					tt.haystack, got, tt.want, engine.Strategy())
			}
		})
	}
}

// TestIsMatchLargeInput tests IsMatch on large haystacks.
func TestIsMatchLargeInput(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{
			name:     "literal at end of 1MB",
			pattern:  "needle",
			haystack: strings.Repeat("x", 1024*1024) + "needle",
			want:     true,
		},
		{
			name:     "literal not found in 1MB",
			pattern:  "needle",
			haystack: strings.Repeat("x", 1024*1024),
			want:     false,
		},
		{
			name:     "digit in large text",
			pattern:  `\d+`,
			haystack: strings.Repeat("abc ", 10000) + "42",
			want:     true,
		},
		{
			name:     "word class in large text",
			pattern:  `\w+`,
			haystack: strings.Repeat("   ", 10000) + "word",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsMatchConsistentWithFind verifies that IsMatch and Find agree on match presence.
func TestIsMatchConsistentWithFind(t *testing.T) {
	patterns := []string{
		"hello",
		`\d+`,
		`\w+`,
		`[a-z]+`,
		"foo|bar|baz",
		`^hello`,
		`world$`,
		`.*\.txt`,
		`[a-zA-Z]+[0-9]+`,
		`(a|b)+`,
	}

	haystacks := []string{
		"hello world",
		"123 abc 456",
		"foo bar baz",
		"document.txt",
		"abc123",
		"",
		"   ",
		"HELLO",
		strings.Repeat("a", 1000),
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", pattern, err)
		}

		for _, haystack := range haystacks {
			h := []byte(haystack)
			isMatch := engine.IsMatch(h)
			find := engine.Find(h)
			findMatch := find != nil

			if isMatch != findMatch {
				t.Errorf("pattern %q, haystack %q: IsMatch=%v but Find=%v (strategy: %s)",
					pattern, haystack, isMatch, findMatch, engine.Strategy())
			}
		}
	}
}

// TestIsMatchSpecificStrategies tests IsMatch on patterns known to trigger specific strategies.
func TestIsMatchSpecificStrategies(t *testing.T) {
	tests := []struct {
		name             string
		pattern          string
		haystack         string
		want             bool
		expectedStrategy Strategy // for logging only
	}{
		// Suffix patterns - UseReverseSuffix
		{
			name:             "reverse suffix .txt match",
			pattern:          `.*\.txt`,
			haystack:         "readme.txt",
			want:             true,
			expectedStrategy: UseReverseSuffix,
		},
		{
			name:             "reverse suffix .txt no match",
			pattern:          `.*\.txt`,
			haystack:         "readme.pdf",
			want:             false,
			expectedStrategy: UseReverseSuffix,
		},

		// Inner patterns - UseReverseInner
		{
			name:             "reverse inner match",
			pattern:          `.*keyword.*`,
			haystack:         "some keyword here",
			want:             true,
			expectedStrategy: UseReverseInner,
		},
		{
			name:             "reverse inner no match",
			pattern:          `.*keyword.*`,
			haystack:         "nothing here",
			want:             false,
			expectedStrategy: UseReverseInner,
		},

		// CharClassSearcher
		{
			name:             "char class searcher match",
			pattern:          `\w+`,
			haystack:         "hello",
			want:             true,
			expectedStrategy: UseCharClassSearcher,
		},
		{
			name:             "char class searcher no match",
			pattern:          `\w+`,
			haystack:         "   ",
			want:             false,
			expectedStrategy: UseCharClassSearcher,
		},

		// BoundedBacktracker
		{
			name:             "bounded backtracker with capture",
			pattern:          `(\w)+`,
			haystack:         "abc",
			want:             true,
			expectedStrategy: UseBoundedBacktracker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, got, tt.want)
			}

			t.Logf("pattern %q: strategy=%s (expected %s)",
				tt.pattern, engine.Strategy(), tt.expectedStrategy)
		})
	}
}

// TestIsMatchEmptyPatternMatches tests that empty pattern matches everything.
func TestIsMatchEmptyPatternMatches(t *testing.T) {
	engine, err := Compile("")
	if err != nil {
		t.Fatal(err)
	}

	haystacks := []string{"", "a", "hello world", strings.Repeat("x", 1000)}
	for _, h := range haystacks {
		if !engine.IsMatch([]byte(h)) {
			t.Errorf("empty pattern should match %q", h)
		}
	}
}

// TestIsMatchMultipleCallsSameEngine tests calling IsMatch multiple times
// on the same engine to verify thread-safety of pooled state.
func TestIsMatchMultipleCallsSameEngine(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		got := engine.IsMatch([]byte("abc 123 def"))
		if !got {
			t.Fatalf("iteration %d: IsMatch returned false, want true", i)
		}
	}

	for i := 0; i < 100; i++ {
		got := engine.IsMatch([]byte("no digits"))
		if got {
			t.Fatalf("iteration %d: IsMatch returned true, want false", i)
		}
	}
}

// TestIsMatchWithDFADisabled tests IsMatch when DFA is explicitly disabled.
func TestIsMatchWithDFADisabled(t *testing.T) {
	config := Config{
		EnableDFA:         false,
		EnablePrefilter:   false,
		MaxRecursionDepth: 100,
	}

	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"literal match", "hello", "hello world", true},
		{"literal no match", "hello", "goodbye", false},
		{"digit match", `\d+`, "abc123", true},
		{"alternation match", "foo|bar", "has bar", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v (strategy: %s)",
					tt.haystack, got, tt.want, engine.Strategy())
			}
		})
	}
}
