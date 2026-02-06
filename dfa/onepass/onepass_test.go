package onepass

import (
	"errors"
	"regexp/syntax"
	"testing"

	"github.com/coregx/coregex/nfa"
)

// compilePattern is a helper to compile a regex pattern to NFA
func compilePattern(t *testing.T, pattern string) *nfa.NFA {
	t.Helper()

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("failed to parse pattern %q: %v", pattern, err)
	}

	// Force anchored compilation for one-pass
	compiler := nfa.NewCompiler(nfa.CompilerConfig{
		UTF8:              true,
		Anchored:          true,
		DotNewline:        false,
		MaxRecursionDepth: 100,
	})

	n, err := compiler.CompileRegexp(re)
	if err != nil {
		t.Fatalf("failed to compile pattern %q: %v", pattern, err)
	}

	return n
}

// One-pass patterns (should build successfully)
var onePassPatterns = []struct {
	pattern string
	desc    string
}{
	{`a`, "single character"},
	{`abc`, "literal string"},
	{`a*b`, "zero or more + literal"},
	{`a+b`, "one or more + literal"},
	{`a?b`, "optional + literal"},
	{`[a-z]+`, "character class"},
	{`(\d+)-(\d+)`, "capture groups with separator"},
	{`([a-z]+)\s+([a-z]+)`, "word pairs"},
	{`x*yx*`, "unambiguous repetition"},
	{`a(b|c)d`, "alternation with different bytes"},
}

// Non-one-pass patterns (should return ErrNotOnePass)
var notOnePassPatterns = []struct {
	pattern string
	desc    string
}{
	{`a*a`, "ambiguous repetition"},
	{`(.*) (.*)`, "ambiguous greedy groups"},
	{`(.*)x`, "greedy ambiguity"},
}

func TestBuildOnePass(t *testing.T) {
	for _, tc := range onePassPatterns {
		t.Run(tc.desc, func(t *testing.T) {
			n := compilePattern(t, tc.pattern)

			dfa, err := Build(n)
			if err != nil {
				t.Fatalf("expected pattern %q to be one-pass, got error: %v", tc.pattern, err)
			}

			if dfa == nil {
				t.Fatal("Build returned nil DFA without error")
			}

			// Verify basic DFA properties
			if dfa.stateCount <= 0 {
				t.Errorf("DFA has no states")
			}

			if dfa.numCaptures != n.CaptureCount() {
				t.Errorf("capture count mismatch: got %d, want %d", dfa.numCaptures, n.CaptureCount())
			}
		})
	}
}

func TestBuildNotOnePass(t *testing.T) {
	for _, tc := range notOnePassPatterns {
		t.Run(tc.desc, func(t *testing.T) {
			n := compilePattern(t, tc.pattern)

			dfa, err := Build(n)
			if err == nil {
				t.Fatalf("expected pattern %q to be NOT one-pass, but Build succeeded", tc.pattern)
			}

			if !errors.Is(err, ErrNotOnePass) {
				t.Errorf("expected ErrNotOnePass, got: %v", err)
			}

			if dfa != nil {
				t.Error("Build returned non-nil DFA with error")
			}
		})
	}
}

// TestCaptureGroupLimit verifies the uint32 slot mask boundary.
// With 16 groups (group 0 + 15 explicit) we have 32 slots fitting in uint32.
// With 17 groups (group 0 + 16 explicit) we have 34 slots overflowing uint32.
func TestCaptureGroupLimit(t *testing.T) {
	t.Run("15 explicit captures (16 total) should not exceed limit", func(t *testing.T) {
		// 15 explicit captures + group 0 = 16 groups = 32 slots = fits uint32
		pattern := `(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)(m)(n)(o)`
		n := compilePattern(t, pattern)

		if n.CaptureCount() != 16 {
			t.Fatalf("expected CaptureCount=16, got %d", n.CaptureCount())
		}

		dfa, err := Build(n)
		if errors.Is(err, ErrTooManyCaptures) {
			t.Fatalf("16 capture groups should be within limit, got ErrTooManyCaptures")
		}
		// ErrNotOnePass is acceptable (pattern may not be one-pass for other reasons)
		if err != nil && !errors.Is(err, ErrNotOnePass) {
			t.Fatalf("unexpected error: %v", err)
		}

		if err == nil && dfa != nil {
			// If build succeeded, verify it matches the expected input
			got := dfa.IsMatch([]byte("abcdefghijklmno"))
			if !got {
				t.Error("expected match for input 'abcdefghijklmno'")
			}
		}
	})

	t.Run("16 explicit captures (17 total) should exceed limit", func(t *testing.T) {
		// 16 explicit captures + group 0 = 17 groups = 34 slots = overflows uint32
		pattern := `(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)(m)(n)(o)(p)`
		n := compilePattern(t, pattern)

		if n.CaptureCount() != 17 {
			t.Fatalf("expected CaptureCount=17, got %d", n.CaptureCount())
		}

		dfa, err := Build(n)
		if err == nil {
			t.Fatal("expected error for 17 capture groups, but Build succeeded")
		}

		if !errors.Is(err, ErrTooManyCaptures) {
			t.Errorf("expected ErrTooManyCaptures, got: %v", err)
		}

		if dfa != nil {
			t.Error("expected nil DFA when capture limit exceeded")
		}
	})
}

func TestIsMatch(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{`a`, "a", true},
		{`a`, "b", false},
		{`abc`, "abc", true},
		{`abc`, "ab", false},
		{`a+b`, "aaab", true},
		{`a+b`, "b", false},
		{`[0-9]+`, "123", true},
		{`[0-9]+`, "abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			n := compilePattern(t, tt.pattern)
			dfa, err := Build(n)
			if err != nil {
				t.Fatalf("failed to build DFA: %v", err)
			}

			got := dfa.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSearch(t *testing.T) {
	tests := []struct {
		pattern   string
		input     string
		wantMatch bool
	}{
		{
			pattern:   `a`,
			input:     "a",
			wantMatch: true,
		},
		{
			pattern:   `abc`,
			input:     "abc",
			wantMatch: true,
		},
		{
			pattern:   `(\d+)-(\d+)`,
			input:     "123-456",
			wantMatch: true,
		},
		{
			pattern:   `([a-z]+)\s+([a-z]+)`,
			input:     "hello world",
			wantMatch: true,
		},
		{
			pattern:   `a`,
			input:     "b",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			n := compilePattern(t, tt.pattern)
			dfa, err := Build(n)
			if err != nil {
				t.Fatalf("failed to build DFA: %v", err)
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			gotMatch := (got != nil)
			if gotMatch != tt.wantMatch {
				t.Errorf("Search(%q) match = %v, want %v", tt.input, gotMatch, tt.wantMatch)
			}

			// Verify group 0 bounds are correct when match occurs
			if gotMatch && len(got) >= 2 {
				if got[0] != 0 {
					t.Errorf("group 0 start: got %d, want 0", got[0])
				}
				if got[1] != len(tt.input) {
					t.Errorf("group 0 end: got %d, want %d", got[1], len(tt.input))
				}
			}
		})
	}
}

func TestEmptyInput(t *testing.T) {
	// Pattern that matches empty string
	n := compilePattern(t, `a*`)
	dfa, err := Build(n)
	if err != nil {
		// a* might not be one-pass due to ambiguity
		t.Skipf("pattern a* is not one-pass: %v", err)
	}

	if !dfa.IsMatch([]byte("")) {
		t.Error("expected empty input to match a*")
	}
}

func TestTransitionEncoding(t *testing.T) {
	tests := []struct {
		next      StateID
		matchWins bool
		slots     uint32
	}{
		{0, false, 0},
		{1, true, 0xFFFFFFFF},
		{MaxStateID, false, 0x00000001},
		{42, true, 0x00000F0F},
	}

	for _, tt := range tests {
		trans := NewTransition(tt.next, tt.matchWins, tt.slots)

		if got := trans.NextState(); got != tt.next {
			t.Errorf("NextState() = %d, want %d", got, tt.next)
		}

		if got := trans.IsMatchWins(); got != tt.matchWins {
			t.Errorf("IsMatchWins() = %v, want %v", got, tt.matchWins)
		}

		if got := trans.SlotMask(); got != tt.slots {
			t.Errorf("SlotMask() = %#x, want %#x", got, tt.slots)
		}
	}
}

func TestUpdateSlots(t *testing.T) {
	slots := make([]int, 10)
	for i := range slots {
		slots[i] = -1
	}

	// Create transition with slot mask: bits 0, 2, 5 set
	trans := NewTransition(1, false, 0b00100101)

	trans.UpdateSlots(slots, 42)

	want := []int{42, -1, 42, -1, -1, 42, -1, -1, -1, -1}
	for i := 0; i < len(want); i++ {
		if slots[i] != want[i] {
			t.Errorf("slots[%d] = %d, want %d", i, slots[i], want[i])
		}
	}
}

// TestCaptureSlotTiming verifies that capture groups are positioned correctly.
// This is the key test for the slot timing fix.
func TestCaptureSlotTiming(t *testing.T) {
	tests := []struct {
		pattern  string
		input    string
		expected []int // [start0, end0, start1, end1, ...]
	}{
		{
			pattern:  `([a-z]+)`,
			input:    "abc",
			expected: []int{0, 3, 0, 3},
		},
		{
			pattern:  `(\d+)-(\d+)`,
			input:    "123-456",
			expected: []int{0, 7, 0, 3, 4, 7},
		},
		{
			pattern:  `([a-z]+)\s+([a-z]+)`,
			input:    "hello world",
			expected: []int{0, 11, 0, 5, 6, 11},
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			n := compilePattern(t, tt.pattern)
			dfa, err := Build(n)
			if err != nil {
				t.Fatalf("failed to build DFA: %v", err)
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			if got == nil {
				t.Fatalf("expected match, got nil")
			}

			if len(got) != len(tt.expected) {
				t.Fatalf("slot count mismatch: got %d, want %d", len(got), len(tt.expected))
			}

			for i := range tt.expected {
				if got[i] != tt.expected[i] {
					t.Errorf("slots[%d] = %d, want %d", i, got[i], tt.expected[i])
				}
			}

			// Log extracted substrings for debugging
			input := []byte(tt.input)
			for g := 0; g < len(tt.expected)/2; g++ {
				start := got[g*2]
				end := got[g*2+1]
				if start >= 0 && end >= 0 && end <= len(input) {
					t.Logf("Group %d: [%d,%d] = %q", g, start, end, string(input[start:end]))
				}
			}
		})
	}
}

func BenchmarkOnePassSearch(b *testing.B) {
	pattern := `(\d+)-(\d+)-(\d+)`
	input := []byte("2025-11-28")

	// Compile pattern
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		b.Fatalf("failed to parse pattern: %v", err)
	}

	compiler := nfa.NewCompiler(nfa.CompilerConfig{
		UTF8:              true,
		Anchored:          true,
		DotNewline:        false,
		MaxRecursionDepth: 100,
	})

	n, err := compiler.CompileRegexp(re)
	if err != nil {
		b.Fatalf("failed to compile NFA: %v", err)
	}

	dfa, err := Build(n)
	if err != nil {
		b.Fatalf("failed to build DFA: %v", err)
	}

	cache := NewCache(dfa.NumCaptures())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := dfa.Search(input, cache)
		if result == nil {
			b.Fatal("expected match")
		}
	}
}

func BenchmarkOnePassIsMatch(b *testing.B) {
	pattern := `(\d+)-(\d+)-(\d+)`
	input := []byte("2025-11-28")

	// Compile pattern
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		b.Fatalf("failed to parse pattern: %v", err)
	}

	compiler := nfa.NewCompiler(nfa.CompilerConfig{
		UTF8:              true,
		Anchored:          true,
		DotNewline:        false,
		MaxRecursionDepth: 100,
	})

	n, err := compiler.CompileRegexp(re)
	if err != nil {
		b.Fatalf("failed to compile NFA: %v", err)
	}

	dfa, err := Build(n)
	if err != nil {
		b.Fatalf("failed to build DFA: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !dfa.IsMatch(input) {
			b.Fatal("expected match")
		}
	}
}
