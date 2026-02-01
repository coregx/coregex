package meta

import (
	"strings"
	"testing"
)

// TestIssue105WordBoundaryPerformance verifies that patterns with \w quantifiers
// don't cause catastrophic slowdown due to checkHasWordBoundary being called per byte.
//
// Root cause: checkHasWordBoundary() scanned ALL NFA states O(N) and was called
// via NewBuilder() on nearly every byte transition, causing O(N*M) complexity.
//
// Fix: Use NewBuilderWithWordBoundary() to pass pre-computed flag and add
// hasWordBoundary guards to skip unnecessary checks.
//
// See: https://github.com/coregx/coregex/issues/105
func TestIssue105WordBoundaryPerformance(t *testing.T) {
	// Pattern from the issue - contains \w quantifiers but NO word boundaries (\b/\B)
	pattern := `=(\$\w{1,10}\(['"][^\)]{1,200}\)\.chr\(\d{1,64}\)\.){2}`

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Test with sample input - should complete quickly (not minutes)
	input := []byte(strings.Repeat("test data without matches ", 1000))

	// This should complete in milliseconds, not minutes
	_ = engine.IsMatch(input)
}

// TestIssue105WordBoundaryCorrectness verifies that word boundary patterns still work.
func TestIssue105WordBoundaryCorrectness(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Patterns WITH word boundaries should still work
		{`\bword\b`, "a word here", true},
		{`\bword\b`, "sword", false},
		{`test\b`, "test!", true},
		{`test\b`, "testing", false},
		{`\Btest`, "atesting", true},
		{`\Btest`, "test", false},

		// Patterns WITHOUT word boundaries should work
		{`\w+`, "hello", true},
		{`\w{3,5}`, "abc", true},
		{`\w{3,5}`, "ab", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			got := engine.IsMatch([]byte(tt.input))

			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// BenchmarkIssue105Pattern benchmarks the problematic pattern from issue #105.
// Before fix: 3+ minutes on 79KB input
// After fix: should be < 1 second
func BenchmarkIssue105Pattern(b *testing.B) {
	pattern := `=(\$\w{1,10}\(['"][^\)]{1,200}\)\.chr\(\d{1,64}\)\.){2}`

	engine, err := Compile(pattern)
	if err != nil {
		b.Fatalf("Compile failed: %v", err)
	}

	// 10KB test input (smaller than issue's 79KB for faster benchmarks)
	input := []byte(strings.Repeat("some PHP code $var = 'test'; ", 500))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = engine.IsMatch(input)
	}
}

// BenchmarkIssue105WithWordBoundary benchmarks pattern WITH word boundary.
func BenchmarkIssue105WithWordBoundary(b *testing.B) {
	pattern := `\btest\b`

	engine, err := Compile(pattern)
	if err != nil {
		b.Fatalf("Compile failed: %v", err)
	}

	input := []byte(strings.Repeat("this is a test case ", 500))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = engine.IsMatch(input)
	}
}

// BenchmarkIssue105NoWordBoundary benchmarks pattern WITHOUT word boundary.
func BenchmarkIssue105NoWordBoundary(b *testing.B) {
	pattern := `\w{3,10}`

	engine, err := Compile(pattern)
	if err != nil {
		b.Fatalf("Compile failed: %v", err)
	}

	input := []byte(strings.Repeat("hello world testing ", 500))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = engine.IsMatch(input)
	}
}
