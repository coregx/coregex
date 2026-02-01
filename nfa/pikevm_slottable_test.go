package nfa

import (
	"regexp"
	"testing"
)

// TestSearchMode_SlotsNeeded verifies slot calculation for each mode
func TestSearchMode_SlotsNeeded(t *testing.T) {
	tests := []struct {
		name       string
		mode       SearchMode
		totalSlots int
		want       int
	}{
		{"IsMatch with 0 slots", SearchModeIsMatch, 0, 0},
		{"IsMatch with 6 slots", SearchModeIsMatch, 6, 0},
		{"Find with 0 slots", SearchModeFind, 0, 0},
		{"Find with 2 slots", SearchModeFind, 2, 2},
		{"Find with 6 slots", SearchModeFind, 6, 2},
		{"Captures with 0 slots", SearchModeCaptures, 0, 0},
		{"Captures with 6 slots", SearchModeCaptures, 6, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.mode.SlotsNeeded(tt.totalSlots)
			if got != tt.want {
				t.Errorf("SlotsNeeded(%d) = %d, want %d", tt.totalSlots, got, tt.want)
			}
		})
	}
}

// TestSearchWithSlotTable_Basic tests basic search functionality
func TestSearchWithSlotTable_Basic(t *testing.T) {
	tests := []struct {
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantMatch bool
	}{
		{"foo", "hello foo world", 6, 9, true},
		{"bar", "hello world", -1, -1, false},
		{"test", "this is a test", 10, 14, true},
		{"hello", "hello", 0, 5, true},
		{"", "", 0, 0, true},
		{"a", "a", 0, 1, true},
		{"abc", "abc", 0, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("failed to compile pattern: %v", err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.SearchWithSlotTable([]byte(tt.haystack), SearchModeFind)

			if match != tt.wantMatch {
				t.Errorf("match = %v, want %v", match, tt.wantMatch)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("positions = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestSearchWithSlotTable_VsStdlib compares results with Go's stdlib regexp
func TestSearchWithSlotTable_VsStdlib(t *testing.T) {
	// NOTE: Patterns with alternations inside quantifiers like "(foo|bar)+"
	// require tookLeft tracking which the lightweight searchThread doesn't have.
	// Those patterns should use the legacy Search methods or be extended to support.
	patterns := []string{
		"foo",
		"[a-z]+",
		"[0-9]+",
		"a+",
		"a*",
		"a?",
		"foo|bar",
		"a{2,4}",
		// "(foo|bar)+" - requires tookLeft tracking, not supported in lightweight threads
	}

	haystacks := []string{
		"hello foo world",
		"test123abc",
		"aaaaa",
		"",
		"foobar",
		"this is a test with an ending",
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)

		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(pattern)
		if err != nil {
			t.Fatalf("failed to compile %q: %v", pattern, err)
		}
		pikevm := NewPikeVM(nfa)

		for _, haystack := range haystacks {
			t.Run(pattern+"/"+haystack, func(t *testing.T) {
				stdLoc := stdRe.FindStringIndex(haystack)
				start, end, match := pikevm.SearchWithSlotTable([]byte(haystack), SearchModeFind)

				// Compare results
				switch {
				case stdLoc == nil && match:
					t.Errorf("stdlib: no match, SlotTable: (%d, %d)", start, end)
				case stdLoc != nil && !match:
					t.Errorf("stdlib: (%d, %d), SlotTable: no match", stdLoc[0], stdLoc[1])
				case stdLoc != nil && match && (start != stdLoc[0] || end != stdLoc[1]):
					t.Errorf("stdlib: (%d, %d), SlotTable: (%d, %d)", stdLoc[0], stdLoc[1], start, end)
				}
			})
		}
	}
}

// TestSearchWithSlotTable_Modes tests all three search modes
func TestSearchWithSlotTable_Modes(t *testing.T) {
	compiler := NewDefaultCompiler()
	nfa, err := compiler.Compile("(a+)(b+)")
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	pikevm := NewPikeVM(nfa)
	haystack := []byte("xxxaaabbbyyy")

	// Test IsMatch mode (0 slots)
	start, end, found := pikevm.SearchWithSlotTable(haystack, SearchModeIsMatch)
	if !found {
		t.Error("IsMatch mode: expected match")
	}
	if start != 3 || end != 9 {
		t.Errorf("IsMatch mode: got (%d, %d), want (3, 9)", start, end)
	}

	// Test Find mode (2 slots)
	start, end, found = pikevm.SearchWithSlotTable(haystack, SearchModeFind)
	if !found {
		t.Error("Find mode: expected match")
	}
	if start != 3 || end != 9 {
		t.Errorf("Find mode: got (%d, %d), want (3, 9)", start, end)
	}

	// Test Captures mode (full slots)
	start, end, found = pikevm.SearchWithSlotTable(haystack, SearchModeCaptures)
	if !found {
		t.Error("Captures mode: expected match")
	}
	if start != 3 || end != 9 {
		t.Errorf("Captures mode: got (%d, %d), want (3, 9)", start, end)
	}
}

// TestSearchWithSlotTableCaptures tests capture extraction
func TestSearchWithSlotTableCaptures(t *testing.T) {
	tests := []struct {
		pattern      string
		haystack     string
		wantCaptures [][]int // nil means no match
	}{
		{
			pattern:  "(a+)(b+)",
			haystack: "xxxaaabbbyyy",
			wantCaptures: [][]int{
				{3, 9},  // group 0: entire match
				{3, 6},  // group 1: a+
				{6, 9},  // group 2: b+
			},
		},
		{
			pattern:  "([a-z]+)([0-9]+)",
			haystack: "abc123xyz",
			wantCaptures: [][]int{
				{0, 6},  // group 0: entire match
				{0, 3},  // group 1: [a-z]+
				{3, 6},  // group 2: [0-9]+
			},
		},
		{
			pattern:      "(foo)(bar)",
			haystack:     "no match here",
			wantCaptures: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("failed to compile: %v", err)
			}

			pikevm := NewPikeVM(nfa)
			result := pikevm.SearchWithSlotTableCaptures([]byte(tt.haystack))

			if tt.wantCaptures == nil {
				if result != nil {
					t.Errorf("expected no match, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Error("expected match, got nil")
				return
			}

			if len(result.Captures) != len(tt.wantCaptures) {
				t.Errorf("capture count = %d, want %d", len(result.Captures), len(tt.wantCaptures))
				return
			}

			for i, want := range tt.wantCaptures {
				got := result.Captures[i]
				if len(got) == 0 && want != nil {
					t.Errorf("group %d: got nil, want %v", i, want)
				} else if len(got) >= 2 && (got[0] != want[0] || got[1] != want[1]) {
					t.Errorf("group %d: got %v, want %v", i, got, want)
				}
			}
		})
	}
}

// TestSearchWithSlotTable_EdgeCases tests edge cases
func TestSearchWithSlotTable_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		at        int
		wantMatch bool
	}{
		{"empty pattern empty haystack", "", "", 0, true},
		{"empty pattern non-empty haystack", "", "abc", 0, true},
		{"pattern longer than haystack", "verylongpattern", "short", 0, false},
		{"search at end", "a", "abc", 3, false},
		{"search past end", "a", "abc", 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("failed to compile: %v", err)
			}

			pikevm := NewPikeVM(nfa)
			_, _, match := pikevm.SearchWithSlotTableAt([]byte(tt.haystack), tt.at, SearchModeFind)

			if match != tt.wantMatch {
				t.Errorf("match = %v, want %v", match, tt.wantMatch)
			}
		})
	}
}

// TestSearchWithSlotTable_Unicode tests Unicode support
func TestSearchWithSlotTable_Unicode(t *testing.T) {
	tests := []struct {
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
	}{
		{"привет", "привет мир", 0, 12}, // Cyrillic
		{"世界", "你好世界", 6, 12},        // Chinese
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("failed to compile: %v", err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.SearchWithSlotTable([]byte(tt.haystack), SearchModeFind)

			if !match {
				t.Error("expected match")
				return
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("positions = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestSearchWithSlotTable_ConsistencyWithLegacy verifies new methods match legacy behavior
func TestSearchWithSlotTable_ConsistencyWithLegacy(t *testing.T) {
	patterns := []string{
		"foo",
		"[a-z]+",
		"a+",
		"foo|bar",
		"(ab)+",
	}

	haystacks := []string{
		"hello foo world",
		"test123abc",
		"aaaaa",
		"foobar",
	}

	for _, pattern := range patterns {
		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(pattern)
		if err != nil {
			t.Fatalf("failed to compile %q: %v", pattern, err)
		}
		pikevm := NewPikeVM(nfa)

		for _, haystack := range haystacks {
			t.Run(pattern+"/"+haystack, func(t *testing.T) {
				// Legacy method
				legacyStart, legacyEnd, legacyMatch := pikevm.Search([]byte(haystack))

				// New SlotTable method
				slotStart, slotEnd, slotMatch := pikevm.SearchWithSlotTable([]byte(haystack), SearchModeFind)

				// Compare
				if legacyMatch != slotMatch {
					t.Errorf("match mismatch: legacy=%v, slot=%v", legacyMatch, slotMatch)
				}
				if legacyStart != slotStart || legacyEnd != slotEnd {
					t.Errorf("position mismatch: legacy=(%d,%d), slot=(%d,%d)",
						legacyStart, legacyEnd, slotStart, slotEnd)
				}
			})
		}
	}
}

// BenchmarkSearchWithSlotTable_Find benchmarks the new Find mode
func BenchmarkSearchWithSlotTable_Find(b *testing.B) {
	compiler := NewDefaultCompiler()
	nfa, _ := compiler.Compile("[a-z]+")
	pikevm := NewPikeVM(nfa)
	haystack := []byte("hello123world456test789")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pikevm.SearchWithSlotTable(haystack, SearchModeFind)
	}
}

// BenchmarkSearchWithSlotTable_IsMatch benchmarks the new IsMatch mode
func BenchmarkSearchWithSlotTable_IsMatch(b *testing.B) {
	compiler := NewDefaultCompiler()
	nfa, _ := compiler.Compile("[a-z]+")
	pikevm := NewPikeVM(nfa)
	haystack := []byte("hello123world456test789")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pikevm.SearchWithSlotTable(haystack, SearchModeIsMatch)
	}
}

// BenchmarkSearchWithSlotTable_Captures benchmarks the new Captures mode
func BenchmarkSearchWithSlotTable_Captures(b *testing.B) {
	compiler := NewDefaultCompiler()
	nfa, _ := compiler.Compile("([a-z]+)([0-9]+)")
	pikevm := NewPikeVM(nfa)
	haystack := []byte("hello123world456test789")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pikevm.SearchWithSlotTableCaptures(haystack)
	}
}

// BenchmarkLegacySearch benchmarks the legacy Search for comparison
func BenchmarkLegacySearch(b *testing.B) {
	compiler := NewDefaultCompiler()
	nfa, _ := compiler.Compile("[a-z]+")
	pikevm := NewPikeVM(nfa)
	haystack := []byte("hello123world456test789")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pikevm.Search(haystack)
	}
}

// BenchmarkSearchThread_MemoryFootprint verifies thread size
func BenchmarkSearchThread_MemoryFootprint(b *testing.B) {
	// This benchmark is just to document the expected memory layout
	// searchThread: 4 (StateID) + 8 (int) + 4 (uint32) = 16 bytes
	// thread: 4 + 8 + 16 (cowCaptures) + 4 + 1 = 33 bytes minimum (40+ with alignment)

	b.Run("searchThread", func(b *testing.B) {
		threads := make([]searchThread, b.N)
		for i := 0; i < b.N; i++ {
			threads[i] = searchThread{
				state:    StateID(i),
				startPos: i,
				priority: uint32(i),
			}
		}
	})

	b.Run("thread", func(b *testing.B) {
		threads := make([]thread, b.N)
		for i := 0; i < b.N; i++ {
			threads[i] = thread{
				state:    StateID(i),
				startPos: i,
				priority: uint32(i),
			}
		}
	})
}
