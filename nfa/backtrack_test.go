package nfa

import (
	"regexp"
	"testing"
)

func compileNFAForTest(pattern string) *NFA {
	compiler := NewDefaultCompiler()
	nfa, err := compiler.Compile(pattern)
	if err != nil {
		panic(err)
	}
	return nfa
}

func TestBoundedBacktracker_IsMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		input    string
		expected bool
	}{
		// Simple literals
		{"hello", "hello world", true},
		{"hello", "world", false},
		{"hello", "say hello", true},

		// Character classes
		{`\d+`, "abc123def", true},
		{`\d+`, "abcdef", false},
		{`\w+`, "hello", true},
		{`\w+`, "   ", false},
		{`[a-z]+`, "hello", true},
		{`[a-z]+`, "HELLO", false},
		{`[A-Za-z]+`, "Hello", true},

		// Quantifiers
		{"a*", "", true},
		{"a+", "", false},
		{"a+", "a", true},
		{"a+", "aaa", true},
		{"a?", "", true},
		{"a?", "a", true},
		{"a{2,4}", "a", false},
		{"a{2,4}", "aa", true},
		{"a{2,4}", "aaaa", true},

		// Alternation
		{"foo|bar", "foo", true},
		{"foo|bar", "bar", true},
		{"foo|bar", "baz", false},
		{"cat|dog|bird", "I have a dog", true},

		// Anchors
		{"^hello", "hello world", true},
		{"^hello", "say hello", false},
		{"world$", "hello world", true},
		{"world$", "world hello", false},

		// Dot
		{"a.c", "abc", true},
		{"a.c", "aXc", true},
		{"a.c", "ac", false},
		{"a.*c", "abc", true},
		{"a.*c", "aXXXc", true},

		// Empty pattern
		{"", "", true},
		{"", "anything", true},

		// Complex patterns
		{`\d{3}-\d{4}`, "123-4567", true},
		{`\d{3}-\d{4}`, "12-4567", false},
		{`[a-z]+@[a-z]+\.[a-z]+`, "test@example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.input, func(t *testing.T) {
			nfa := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(nfa)

			got := bt.IsMatch([]byte(tt.input))
			if got != tt.expected {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.expected)
			}
		})
	}
}

func TestBoundedBacktracker_Search(t *testing.T) {
	tests := []struct {
		pattern       string
		input         string
		expectedStart int
		expectedEnd   int
		expectedFound bool
	}{
		// Simple matches
		{"hello", "hello world", 0, 5, true},
		{"world", "hello world", 6, 11, true},
		{"xyz", "hello world", -1, -1, false},

		// Character classes
		{`\d+`, "abc123def", 3, 6, true},
		{`\d+`, "abcdef", -1, -1, false},
		{`\w+`, "  hello  ", 2, 7, true},

		// Quantifiers
		{"a+", "baaab", 1, 4, true},
		{"a*", "bbb", 0, 0, true}, // Empty match at start

		// Alternation
		{"foo|bar", "the bar is open", 4, 7, true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.input, func(t *testing.T) {
			nfa := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(nfa)

			start, end, found := bt.Search([]byte(tt.input))
			if found != tt.expectedFound {
				t.Errorf("Search(%q, %q) found = %v, want %v", tt.pattern, tt.input, found, tt.expectedFound)
			}
			if found && (start != tt.expectedStart || end != tt.expectedEnd) {
				t.Errorf("Search(%q, %q) = (%d, %d), want (%d, %d)",
					tt.pattern, tt.input, start, end, tt.expectedStart, tt.expectedEnd)
			}
		})
	}
}

func TestBoundedBacktracker_CanHandle(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)

	// Small input should be handleable
	if !bt.CanHandle(1000) {
		t.Error("Should handle small input")
	}

	// Very large input might not be handleable
	// With default 256KB limit and ~10 states, max input is ~25K
	// But let's test with a huge input
	if bt.CanHandle(10_000_000) {
		t.Error("Should not handle 10MB input with default limits")
	}
}

func TestBoundedBacktracker_VsStdlib(t *testing.T) {
	patterns := []string{
		`\d+`,
		`\w+`,
		`[a-z]+`,
		`foo|bar|baz`,
		`a+b+c+`,
		`\d{2,4}`,
	}

	inputs := []string{
		"hello world",
		"abc123def456",
		"the quick brown fox",
		"foo bar baz qux",
		"aaabbbccc",
		"12 1234 123456",
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)
		nfa := compileNFAForTest(pattern)
		bt := NewBoundedBacktracker(nfa)

		for _, input := range inputs {
			t.Run(pattern+"/"+input, func(t *testing.T) {
				stdMatch := stdRe.MatchString(input)
				btMatch := bt.IsMatch([]byte(input))

				if stdMatch != btMatch {
					t.Errorf("IsMatch mismatch for %q on %q: stdlib=%v, backtracker=%v",
						pattern, input, stdMatch, btMatch)
				}
			})
		}
	}
}

func BenchmarkBoundedBacktracker_IsMatch(b *testing.B) {
	patterns := []struct {
		name    string
		pattern string
	}{
		{"digit", `\d+`},
		{"word", `\w+`},
		{"alpha", `[a-z]+`},
		{"alternation", `foo|bar|baz`},
	}

	input := []byte("the quick brown fox jumps over 12345 lazy dogs")

	for _, p := range patterns {
		b.Run(p.name, func(b *testing.B) {
			nfa := compileNFAForTest(p.pattern)
			bt := NewBoundedBacktracker(nfa)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bt.IsMatch(input)
			}
		})
	}
}

func BenchmarkBoundedBacktracker_VsPikeVM(b *testing.B) {
	patterns := []struct {
		name    string
		pattern string
	}{
		{"digit", `\d+`},
		{"word", `\w+`},
	}

	input := []byte("the quick brown fox jumps over 12345 lazy dogs")

	for _, p := range patterns {
		nfa := compileNFAForTest(p.pattern)

		b.Run(p.name+"/backtracker", func(b *testing.B) {
			bt := NewBoundedBacktracker(nfa)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bt.IsMatch(input)
			}
		})

		b.Run(p.name+"/pikevm", func(b *testing.B) {
			vm := NewPikeVM(nfa)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				vm.IsMatch(input)
			}
		})
	}
}
