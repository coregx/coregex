package onepass

import (
	"regexp/syntax"
	"testing"

	"github.com/coregx/coregex/nfa"
)

// buildDFA is a helper that compiles a pattern and builds the onepass DFA.
// Returns nil if the pattern is not one-pass.
func buildDFA(t *testing.T, pattern string) *DFA {
	t.Helper()

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("failed to parse pattern %q: %v", pattern, err)
	}

	compiler := nfa.NewCompiler(nfa.CompilerConfig{
		UTF8:              true,
		Anchored:          true,
		DotNewline:        false,
		MaxRecursionDepth: 100,
	})

	n, err := compiler.CompileRegexp(re)
	if err != nil {
		t.Fatalf("failed to compile NFA for %q: %v", pattern, err)
	}

	dfa, err := Build(n)
	if err != nil {
		t.Skipf("pattern %q is not one-pass: %v", pattern, err)
		return nil
	}

	return dfa
}

func TestSearchAtBasic(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		start     int
		wantMatch bool
		wantSlots []int // [start0, end0, ...] - nil means no match
	}{
		{
			name:      "exact match at start 0",
			pattern:   `abc`,
			input:     "abc",
			start:     0,
			wantMatch: true,
			wantSlots: []int{0, 3},
		},
		{
			name:      "match at offset exact",
			pattern:   `abc`,
			input:     "xxxabc",
			start:     3,
			wantMatch: true,
			wantSlots: []int{3, 6},
		},
		{
			name:      "no match at offset",
			pattern:   `abc`,
			input:     "xxxdef",
			start:     3,
			wantMatch: false,
		},
		{
			name:      "match single char",
			pattern:   `a`,
			input:     "a",
			start:     0,
			wantMatch: true,
			wantSlots: []int{0, 1},
		},
		{
			name:      "match single char at offset",
			pattern:   `a`,
			input:     "ba",
			start:     1,
			wantMatch: true,
			wantSlots: []int{1, 2},
		},
		{
			name:      "char class at offset",
			pattern:   `[0-9]+`,
			input:     "abc123",
			start:     3,
			wantMatch: true,
			wantSlots: []int{3, 6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa := buildDFA(t, tt.pattern)
			if dfa == nil {
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.SearchAt([]byte(tt.input), tt.start, cache)

			if !tt.wantMatch {
				if got != nil {
					t.Errorf("SearchAt(%q, %d) = %v, want nil", tt.input, tt.start, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("SearchAt(%q, %d) = nil, want match", tt.input, tt.start)
			}
			if len(got) >= 2 && (got[0] != tt.wantSlots[0] || got[1] != tt.wantSlots[1]) {
				t.Errorf("SearchAt group 0 = [%d, %d], want [%d, %d]",
					got[0], got[1], tt.wantSlots[0], tt.wantSlots[1])
			}
		})
	}
}

func TestSearchAtBoundaryConditions(t *testing.T) {
	dfa := buildDFA(t, `[a-z]+`)
	if dfa == nil {
		return
	}

	tests := []struct {
		name      string
		input     string
		start     int
		wantMatch bool
	}{
		{
			name:      "empty input start 0",
			input:     "",
			start:     0,
			wantMatch: false, // [a-z]+ requires at least one char
		},
		{
			name:      "start past end",
			input:     "abc",
			start:     5,
			wantMatch: false,
		},
		{
			name:      "start at exact end",
			input:     "abc",
			start:     3,
			wantMatch: false,
		},
		{
			name:      "negative start",
			input:     "abc",
			start:     -1,
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(dfa.NumCaptures())
			got := dfa.SearchAt([]byte(tt.input), tt.start, cache)

			if tt.wantMatch && got == nil {
				t.Errorf("SearchAt(%q, %d) = nil, want match", tt.input, tt.start)
			}
			if !tt.wantMatch && got != nil {
				t.Errorf("SearchAt(%q, %d) = %v, want nil", tt.input, tt.start, got)
			}
		})
	}
}

func TestSearchAtWithCaptures(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		start     int
		wantSlots []int
	}{
		{
			name:      "digit groups at offset",
			pattern:   `(\d+)-(\d+)`,
			input:     "xxx123-456",
			start:     3,
			wantSlots: []int{3, 10, 3, 6, 7, 10},
		},
		{
			name:      "word pair at offset",
			pattern:   `([a-z]+)\s+([a-z]+)`,
			input:     "00hello world",
			start:     2,
			wantSlots: []int{2, 13, 2, 7, 8, 13},
		},
		{
			name:      "single capture at start",
			pattern:   `([a-z]+)`,
			input:     "abc",
			start:     0,
			wantSlots: []int{0, 3, 0, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa := buildDFA(t, tt.pattern)
			if dfa == nil {
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.SearchAt([]byte(tt.input), tt.start, cache)

			if got == nil {
				t.Fatalf("SearchAt(%q, %d) = nil, want match with captures", tt.input, tt.start)
			}

			if len(got) != len(tt.wantSlots) {
				t.Fatalf("slot count = %d, want %d", len(got), len(tt.wantSlots))
			}

			for i := range tt.wantSlots {
				if got[i] != tt.wantSlots[i] {
					t.Errorf("slots[%d] = %d, want %d", i, got[i], tt.wantSlots[i])
				}
			}
		})
	}
}

func TestSearchAtMatchNoMatch(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		start     int
		wantMatch bool
	}{
		{name: "literal match", pattern: `abc`, input: "abc", start: 0, wantMatch: true},
		{name: "literal no match", pattern: `abc`, input: "def", start: 0, wantMatch: false},
		{name: "repetition match", pattern: `a+b`, input: "aaab", start: 0, wantMatch: true},
		{name: "repetition no match", pattern: `a+b`, input: "aaac", start: 0, wantMatch: false},
		{name: "digits match", pattern: `[0-9]+`, input: "12345", start: 0, wantMatch: true},
		{name: "digits no match", pattern: `[0-9]+`, input: "abcde", start: 0, wantMatch: false},
		{name: "offset match", pattern: `a`, input: "bba", start: 2, wantMatch: true},
		{name: "offset no match", pattern: `a`, input: "bbb", start: 2, wantMatch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa := buildDFA(t, tt.pattern)
			if dfa == nil {
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.SearchAt([]byte(tt.input), tt.start, cache)

			gotMatch := got != nil
			if gotMatch != tt.wantMatch {
				t.Errorf("SearchAt(%q, %d) match = %v, want %v", tt.input, tt.start, gotMatch, tt.wantMatch)
			}
		})
	}
}

func TestSearchAtSlotAdjustment(t *testing.T) {
	// Verify that SearchAt correctly adjusts slot positions by start offset
	dfa := buildDFA(t, `([a-z]+)`)
	if dfa == nil {
		return
	}

	input := []byte("___hello")
	cache := NewCache(dfa.NumCaptures())
	got := dfa.SearchAt(input, 3, cache)

	if got == nil {
		t.Fatal("SearchAt should match 'hello' at offset 3")
	}

	// Group 0 should be [3, 8]
	if len(got) >= 2 {
		if got[0] != 3 {
			t.Errorf("group 0 start = %d, want 3 (adjusted by offset)", got[0])
		}
		if got[1] != 8 {
			t.Errorf("group 0 end = %d, want 8 (adjusted by offset)", got[1])
		}
	}

	// Group 1 should also be [3, 8] (same as group 0 for this pattern)
	if len(got) >= 4 {
		if got[2] != 3 {
			t.Errorf("group 1 start = %d, want 3", got[2])
		}
		if got[3] != 8 {
			t.Errorf("group 1 end = %d, want 8", got[3])
		}
	}
}

func TestSearchAtConsecutiveCalls(t *testing.T) {
	// Verify that the cache is correctly reset between consecutive calls
	dfa := buildDFA(t, `([a-z]+)`)
	if dfa == nil {
		return
	}

	cache := NewCache(dfa.NumCaptures())

	// First search
	got1 := dfa.SearchAt([]byte("abc"), 0, cache)
	if len(got1) < 2 || got1[1] != 3 {
		t.Fatalf("First SearchAt: unexpected result %v", got1)
	}

	// Second search with different input and offset
	got2 := dfa.SearchAt([]byte("___xyz"), 3, cache)
	if got2 == nil {
		t.Fatal("Second SearchAt: expected match")
	}
	if got2[0] != 3 || got2[1] != 6 {
		t.Errorf("Second SearchAt: group 0 = [%d, %d], want [3, 6]", got2[0], got2[1])
	}
}

func TestSearchEmpty(t *testing.T) {
	// Test Search (not SearchAt) with empty input for patterns that match empty
	tests := []struct {
		name      string
		pattern   string
		wantMatch bool
	}{
		{name: "a+ does not match empty", pattern: `a+`, wantMatch: false},
		{name: "literal does not match empty", pattern: `abc`, wantMatch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa := buildDFA(t, tt.pattern)
			if dfa == nil {
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte{}, cache)

			gotMatch := got != nil
			if gotMatch != tt.wantMatch {
				t.Errorf("Search(empty) match = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}
