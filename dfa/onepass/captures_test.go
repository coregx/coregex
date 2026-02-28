package onepass

import (
	"regexp"
	"regexp/syntax"
	"testing"

	"github.com/coregx/coregex/nfa"
)

// compileOnePass is a helper that compiles a one-pass DFA from a pattern.
// Skips the test if the pattern is not one-pass.
func compileOnePass(t *testing.T, pattern string) *DFA {
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

// TestSearchWithCapturesDigitGroups tests capture extraction for digit group patterns.
func TestSearchWithCapturesDigitGroups(t *testing.T) {
	dfa := compileOnePass(t, `(\d+)-(\d+)`)
	if dfa == nil {
		return
	}

	tests := []struct {
		name      string
		input     string
		wantSlots []int
	}{
		{
			name:      "basic digit groups",
			input:     "123-456",
			wantSlots: []int{0, 7, 0, 3, 4, 7},
		},
		{
			name:      "single digits",
			input:     "1-2",
			wantSlots: []int{0, 3, 0, 1, 2, 3},
		},
		{
			name:      "long digit groups",
			input:     "12345-67890",
			wantSlots: []int{0, 11, 0, 5, 6, 11},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			if got == nil {
				t.Fatal("expected match, got nil")
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

// TestSearchWithCapturesWordPairs tests capture extraction for word pair patterns.
func TestSearchWithCapturesWordPairs(t *testing.T) {
	dfa := compileOnePass(t, `([a-z]+)\s+([a-z]+)`)
	if dfa == nil {
		return
	}

	tests := []struct {
		name      string
		input     string
		wantSlots []int
	}{
		{
			name:      "two words single space",
			input:     "hello world",
			wantSlots: []int{0, 11, 0, 5, 6, 11},
		},
		{
			name:      "two words multiple spaces",
			input:     "foo   bar",
			wantSlots: []int{0, 9, 0, 3, 6, 9},
		},
		{
			name:      "single char words",
			input:     "a b",
			wantSlots: []int{0, 3, 0, 1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			if got == nil {
				t.Fatal("expected match, got nil")
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

// TestSearchWithCaptureSingleGroup tests single capture group patterns.
func TestSearchWithCaptureSingleGroup(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		wantSlots []int
	}{
		{
			name:      "single digit group",
			pattern:   `([0-9]+)`,
			input:     "12345",
			wantSlots: []int{0, 5, 0, 5},
		},
		{
			name:      "single letter group",
			pattern:   `([a-z]+)`,
			input:     "hello",
			wantSlots: []int{0, 5, 0, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa := compileOnePass(t, tt.pattern)
			if dfa == nil {
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			if got == nil {
				t.Fatal("expected match, got nil")
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

// TestSearchWithCaptureAlternation tests capture groups with alternation.
func TestSearchWithCaptureAlternation(t *testing.T) {
	dfa := compileOnePass(t, `a(b|c)d`)
	if dfa == nil {
		return
	}

	tests := []struct {
		name      string
		input     string
		wantMatch bool
		wantGroup string // expected group 1 content
	}{
		{"abd", "abd", true, "b"},
		{"acd", "acd", true, "c"},
		{"aad", "aad", false, ""},
		{"add", "add", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			if tt.wantMatch {
				if got == nil {
					t.Fatal("expected match, got nil")
				}
				// Verify group 1 captures the alternation
				if len(got) >= 4 {
					g1 := string([]byte(tt.input)[got[2]:got[3]])
					if g1 != tt.wantGroup {
						t.Errorf("group 1 = %q, want %q", g1, tt.wantGroup)
					}
				}
			} else {
				if got != nil {
					t.Errorf("expected no match, got slots %v", got)
				}
			}
		})
	}
}

// TestSearchWithThreeCaptureGroups tests patterns with three capture groups.
func TestSearchWithThreeCaptureGroups(t *testing.T) {
	dfa := compileOnePass(t, `(\d+)-(\d+)-(\d+)`)
	if dfa == nil {
		return
	}

	cache := NewCache(dfa.NumCaptures())
	input := []byte("2025-11-28")
	got := dfa.Search(input, cache)

	if got == nil {
		t.Fatal("expected match, got nil")
	}

	// Group 0: entire match [0, 10]
	if got[0] != 0 || got[1] != 10 {
		t.Errorf("group 0 = [%d, %d], want [0, 10]", got[0], got[1])
	}

	// Group 1: year [0, 4]
	if len(got) >= 4 {
		g1 := string(input[got[2]:got[3]])
		if g1 != "2025" {
			t.Errorf("group 1 = %q, want '2025'", g1)
		}
	}

	// Group 2: month [5, 7]
	if len(got) >= 6 {
		g2 := string(input[got[4]:got[5]])
		if g2 != "11" {
			t.Errorf("group 2 = %q, want '11'", g2)
		}
	}

	// Group 3: day [8, 10]
	if len(got) >= 8 {
		g3 := string(input[got[6]:got[7]])
		if g3 != "28" {
			t.Errorf("group 3 = %q, want '28'", g3)
		}
	}
}

// TestSearchCaptureNoMatch tests that Search returns nil for non-matching inputs.
func TestSearchCaptureNoMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{"digit group no digits", `(\d+)-(\d+)`, "abc-def"},
		{"word pair no space", `([a-z]+)\s+([a-z]+)`, "helloworld"},
		{"literal no match", `(abc)`, "def"},
		{"empty input", `([a-z]+)`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa := compileOnePass(t, tt.pattern)
			if dfa == nil {
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			if got != nil {
				t.Errorf("expected nil, got %v", got)
			}
		})
	}
}

// TestSearchCaptureVsStdlib validates OnePass capture results against stdlib regexp.
func TestSearchCaptureVsStdlib(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`([a-z]+)`, "hello"},
		{`(\d+)-(\d+)`, "123-456"},
		{`([a-z]+)\s+([a-z]+)`, "hello world"},
		{`(\d+)-(\d+)-(\d+)`, "2025-01-15"},
		{`a(b|c)d`, "abd"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			dfa := compileOnePass(t, tt.pattern)
			if dfa == nil {
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			re := regexp.MustCompile("^" + tt.pattern)
			stdlibLocs := re.FindStringSubmatchIndex(tt.input)

			if got == nil && stdlibLocs == nil {
				return // Both agree: no match
			}
			if got == nil && stdlibLocs != nil {
				t.Fatalf("OnePass returned nil, stdlib found match: %v", stdlibLocs)
			}
			if got != nil && stdlibLocs == nil {
				t.Fatalf("OnePass found match %v, stdlib returned nil", got)
			}

			// Compare each slot pair
			minLen := len(got)
			if len(stdlibLocs) < minLen {
				minLen = len(stdlibLocs)
			}

			for i := 0; i < minLen; i++ {
				if got[i] != stdlibLocs[i] {
					t.Errorf("slot[%d]: onepass=%d, stdlib=%d", i, got[i], stdlibLocs[i])
				}
			}
		})
	}
}

// TestSearchAtWithCaptureGroupOffsets tests SearchAt with capture groups at various offsets.
func TestSearchAtWithCaptureGroupOffsets(t *testing.T) {
	dfa := compileOnePass(t, `(\d+)-(\d+)`)
	if dfa == nil {
		return
	}

	tests := []struct {
		name      string
		input     string
		start     int
		wantMatch bool
		wantSlots []int
	}{
		{
			name:      "match at offset 0",
			input:     "123-456",
			start:     0,
			wantMatch: true,
			wantSlots: []int{0, 7, 0, 3, 4, 7},
		},
		{
			name:      "match at offset 3",
			input:     "xxx123-456",
			start:     3,
			wantMatch: true,
			wantSlots: []int{3, 10, 3, 6, 7, 10},
		},
		{
			name:      "no match at offset",
			input:     "xxx abc-def",
			start:     4,
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(dfa.NumCaptures())
			got := dfa.SearchAt([]byte(tt.input), tt.start, cache)

			if tt.wantMatch {
				if got == nil {
					t.Fatal("expected match, got nil")
				}
				for i := range tt.wantSlots {
					if i < len(got) && got[i] != tt.wantSlots[i] {
						t.Errorf("slots[%d] = %d, want %d", i, got[i], tt.wantSlots[i])
					}
				}
			} else {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
			}
		})
	}
}

// TestCacheReuse tests that the cache is properly reset between calls.
func TestCacheReuse(t *testing.T) {
	dfa := compileOnePass(t, `([a-z]+)`)
	if dfa == nil {
		return
	}

	cache := NewCache(dfa.NumCaptures())

	// First search
	got1 := dfa.Search([]byte("abc"), cache)
	if got1 == nil || got1[0] != 0 || got1[1] != 3 {
		t.Fatalf("First search failed: %v", got1)
	}

	// Second search (cache should be reset internally by Search)
	got2 := dfa.Search([]byte("xyz"), cache)
	if got2 == nil || got2[0] != 0 || got2[1] != 3 {
		t.Fatalf("Second search failed: %v", got2)
	}

	// Verify group 1 content is correct for second search
	if len(got2) >= 4 {
		if got2[2] != 0 || got2[3] != 3 {
			t.Errorf("Second search group 1 = [%d, %d], want [0, 3]", got2[2], got2[3])
		}
	}
}

// TestCacheReset tests the explicit Reset method.
func TestCacheReset(t *testing.T) {
	cache := NewCache(3) // 3 groups = 6 slots

	// Fill with values
	for i := range cache.slots {
		cache.slots[i] = i * 10
	}

	// Reset
	cache.Reset()

	// All slots should be -1
	for i, v := range cache.slots {
		if v != -1 {
			t.Errorf("After Reset, slots[%d] = %d, want -1", i, v)
		}
	}
}

// TestNumCaptures tests that NumCaptures returns correct values.
func TestNumCaptures(t *testing.T) {
	tests := []struct {
		pattern     string
		wantCapture int
	}{
		{`a`, 1},                   // group 0 only
		{`(a)`, 2},                 // group 0 + group 1
		{`(\d+)-(\d+)`, 3},         // group 0 + 2 groups
		{`(\d+)-(\d+)-(\d+)`, 4},   // group 0 + 3 groups
		{`([a-z]+)\s+([a-z]+)`, 3}, // group 0 + 2 groups
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			dfa := compileOnePass(t, tt.pattern)
			if dfa == nil {
				return
			}

			if dfa.NumCaptures() != tt.wantCapture {
				t.Errorf("NumCaptures() = %d, want %d", dfa.NumCaptures(), tt.wantCapture)
			}
		})
	}
}

// TestIsMatchVsSearchConsistencyExactInputs tests that IsMatch and Search agree
// when the input is an exact match for the pattern (no trailing bytes).
// Note: IsMatch uses early termination on match states, while Search uses
// IsMatchWins() transitions. They may differ when input has trailing bytes
// beyond the match (IsMatch returns true, Search reads past and hits dead).
func TestIsMatchVsSearchConsistencyExactInputs(t *testing.T) {
	tests := []struct {
		pattern   string
		input     string
		wantMatch bool
	}{
		{`a`, "a", true},
		{`a`, "b", false},
		{`abc`, "abc", true},
		{`abc`, "abd", false},
		{`[0-9]+`, "12345", true},
		{`[0-9]+`, "abc", false},
		{`([a-z]+)`, "hello", true},
		{`(\d+)-(\d+)`, "123-456", true},
		{`(\d+)-(\d+)`, "abc-def", false},
		{`a+b`, "aaab", true},
		{`a+b`, "bbb", false},
		{`a+b`, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			dfa := compileOnePass(t, tt.pattern)
			if dfa == nil {
				return
			}

			isMatch := dfa.IsMatch([]byte(tt.input))
			cache := NewCache(dfa.NumCaptures())
			search := dfa.Search([]byte(tt.input), cache)
			searchFound := search != nil

			// For exact inputs (no trailing bytes), both should agree
			if isMatch != searchFound {
				t.Errorf("IsMatch=%v but Search found=%v for exact input %q",
					isMatch, searchFound, tt.input)
			}

			if isMatch != tt.wantMatch {
				t.Errorf("IsMatch=%v, want %v", isMatch, tt.wantMatch)
			}
		})
	}
}

// TestSearchWithRepetition tests capture groups involving repetition.
func TestSearchWithRepetition(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		wantMatch bool
	}{
		{"a+b match", `a+b`, "aaab", true},
		{"a+b no match", `a+b`, "bbb", false},
		{"a*b empty a", `a*b`, "b", true},
		{"[a-z]+[0-9]+", `[a-z]+[0-9]+`, "abc123", true},
		{"optional group", `a?b`, "b", true},
		{"optional group with a", `a?b`, "ab", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa := compileOnePass(t, tt.pattern)
			if dfa == nil {
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)
			gotMatch := got != nil

			if gotMatch != tt.wantMatch {
				t.Errorf("Search(%q, %q) match=%v, want %v",
					tt.pattern, tt.input, gotMatch, tt.wantMatch)
			}

			// If match, verify group 0 bounds
			if gotMatch && len(got) >= 2 {
				if got[0] != 0 {
					t.Errorf("group 0 start = %d, want 0", got[0])
				}
				// Group 0 end should be within input
				if got[1] < 0 || got[1] > len(tt.input) {
					t.Errorf("group 0 end = %d, out of range [0, %d]", got[1], len(tt.input))
				}
			}
		})
	}
}

// TestSearchAtBoundaryEdgeCases tests SearchAt boundary conditions.
func TestSearchAtBoundaryEdgeCases(t *testing.T) {
	dfa := compileOnePass(t, `[a-z]+`)
	if dfa == nil {
		return
	}

	tests := []struct {
		name      string
		input     string
		start     int
		wantMatch bool
	}{
		{"start 0", "abc", 0, true},
		{"start at end", "abc", 3, false},
		{"start past end", "abc", 5, false},
		{"negative start", "abc", -1, false},
		{"empty input start 0", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(dfa.NumCaptures())
			got := dfa.SearchAt([]byte(tt.input), tt.start, cache)

			gotMatch := got != nil
			if gotMatch != tt.wantMatch {
				t.Errorf("SearchAt(%q, %d) match=%v, want %v",
					tt.input, tt.start, gotMatch, tt.wantMatch)
			}
		})
	}
}

// TestOnePassHelpers tests nextPowerOf2 and log2 helper functions.
func TestOnePassHelpers(t *testing.T) {
	t.Run("nextPowerOf2", func(t *testing.T) {
		tests := []struct {
			input int
			want  int
		}{
			{0, 1},
			{1, 1},
			{2, 2},
			{3, 4},
			{4, 4},
			{5, 8},
			{7, 8},
			{8, 8},
			{9, 16},
			{255, 256},
			{256, 256},
			{257, 512},
		}

		for _, tt := range tests {
			got := nextPowerOf2(tt.input)
			if got != tt.want {
				t.Errorf("nextPowerOf2(%d) = %d, want %d", tt.input, got, tt.want)
			}
		}
	})

	t.Run("log2", func(t *testing.T) {
		tests := []struct {
			input int
			want  uint
		}{
			{0, 0},
			{1, 0},
			{2, 1},
			{4, 2},
			{8, 3},
			{16, 4},
			{32, 5},
			{64, 6},
			{128, 7},
			{256, 8},
		}

		for _, tt := range tests {
			got := log2(tt.input)
			if got != tt.want {
				t.Errorf("log2(%d) = %d, want %d", tt.input, got, tt.want)
			}
		}
	})
}
