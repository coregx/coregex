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

	// With 256M bit entries limit and ~6 states for \d+ pattern:
	// Max input = 256M / 6 = ~42MB
	// So 1MB and 10MB should both be handleable
	if !bt.CanHandle(1_000_000) {
		t.Error("Should handle 1MB input")
	}

	if !bt.CanHandle(10_000_000) {
		t.Error("Should handle 10MB input with 1-bit bitset")
	}

	// 100MB input should NOT be handleable
	// 100MB * 6 states = 600M bit entries > 256M limit
	if bt.CanHandle(100_000_000) {
		t.Error("Should not handle 100MB input with default limits")
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

func TestBoundedBacktracker_BitsetVisited(t *testing.T) {
	// Test that the 1-bit visited table correctly tracks visited (state, pos) pairs
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)
	state := NewBacktrackerState()

	// Reset with a small input
	bt.reset(state, 10)

	numStates := bt.NumStates()

	// Verify initial state: nothing visited
	for pos := 0; pos <= 10; pos++ {
		for sid := 0; sid < numStates; sid++ {
			if !bt.shouldVisit(state, StateID(sid), pos) {
				t.Errorf("shouldVisit(state=%d, pos=%d) = false, want true (first visit)", sid, pos)
			}
		}
	}

	// Verify all are now visited (second visit should return false)
	for pos := 0; pos <= 10; pos++ {
		for sid := 0; sid < numStates; sid++ {
			if bt.shouldVisit(state, StateID(sid), pos) {
				t.Errorf("shouldVisit(state=%d, pos=%d) = true, want false (already visited)", sid, pos)
			}
		}
	}

	// After reset, all should be unvisited again
	bt.reset(state, 10)
	for pos := 0; pos <= 10; pos++ {
		for sid := 0; sid < numStates; sid++ {
			if !bt.shouldVisit(state, StateID(sid), pos) {
				t.Errorf("after reset: shouldVisit(state=%d, pos=%d) = false, want true", sid, pos)
			}
		}
	}
}

func TestBoundedBacktracker_BitsetMemoryReduction(t *testing.T) {
	// Verify the bitset uses expected memory (1 bit per entry vs 16 bits previously)
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)
	state := NewBacktrackerState()

	inputLen := 1000
	bt.reset(state, inputLen)

	numStates := bt.NumStates()
	totalEntries := numStates * (inputLen + 1)
	expectedBlocks := (totalEntries + 63) / 64

	if len(state.Visited) != expectedBlocks {
		t.Errorf("Visited blocks = %d, want %d (for %d bit entries)",
			len(state.Visited), expectedBlocks, totalEntries)
	}

	// Memory in bytes: blocks * 8 (uint64)
	actualBytes := len(state.Visited) * 8
	oldBytes := totalEntries * 2 // uint16 = 2 bytes per entry

	if actualBytes >= oldBytes {
		t.Errorf("Bitset memory %d bytes >= old uint16 memory %d bytes; expected ~16x reduction",
			actualBytes, oldBytes)
	}

	// Verify at least 10x reduction (theoretical 16x, allowing some overhead)
	ratio := float64(oldBytes) / float64(actualBytes)
	if ratio < 10.0 {
		t.Errorf("Memory reduction ratio = %.1fx, want >= 10x", ratio)
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
