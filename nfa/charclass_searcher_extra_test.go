package nfa

import (
	"regexp"
	"testing"
)

// TestCharClassSearcher_NegatedClasses tests searcher behavior with
// character classes representing negated patterns like [^a-z], \W, \D.
func TestCharClassSearcher_NegatedClasses(t *testing.T) {
	// \D = [^0-9] — everything except digits
	// Build as all byte ranges excluding 0x30-0x39
	nonDigitRanges := [][2]byte{
		{0x00, 0x2F},
		{0x3A, 0xFF},
	}
	s := NewCharClassSearcher(nonDigitRanges, 1)

	tests := []struct {
		name      string
		input     string
		wantS     int
		wantE     int
		wantFound bool
	}{
		{"all digits no match", "12345", -1, -1, false},
		{"leading non-digits", "abc123", 0, 3, true},
		{"trailing non-digits", "123abc", 3, 6, true},
		{"mixed", "1a2b3c", 1, 2, true},
		{"empty", "", -1, -1, false},
		{"spaces only", "   ", 0, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := s.Search([]byte(tt.input))
			if found != tt.wantFound || start != tt.wantS || end != tt.wantE {
				t.Errorf("Search(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.input, start, end, found, tt.wantS, tt.wantE, tt.wantFound)
			}
		})
	}
}

// TestCharClassSearcher_SpaceClass tests the \s equivalent character class.
func TestCharClassSearcher_SpaceClass(t *testing.T) {
	// \s = [ \t\n\r\f\v]
	spaceRanges := [][2]byte{
		{'\t', '\r'}, // \t, \n, \v, \f, \r (0x09-0x0D)
		{' ', ' '},   // space (0x20)
	}
	s := NewCharClassSearcher(spaceRanges, 1)

	tests := []struct {
		name      string
		input     string
		wantS     int
		wantE     int
		wantFound bool
	}{
		{"spaces", "abc def", 3, 4, true},
		{"tabs", "abc\tdef", 3, 4, true},
		{"newlines", "abc\ndef", 3, 4, true},
		{"multiple spaces", "  abc  ", 0, 2, true},
		{"no spaces", "abcdef", -1, -1, false},
		{"empty", "", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := s.Search([]byte(tt.input))
			if found != tt.wantFound || start != tt.wantS || end != tt.wantE {
				t.Errorf("Search(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.input, start, end, found, tt.wantS, tt.wantE, tt.wantFound)
			}
		})
	}
}

// TestCharClassSearcher_SingleByteRange tests a searcher with a single byte range.
func TestCharClassSearcher_SingleByteRange(t *testing.T) {
	// Only lowercase vowels
	vowels := [][2]byte{
		{'a', 'a'},
		{'e', 'e'},
		{'i', 'i'},
		{'o', 'o'},
		{'u', 'u'},
	}
	s := NewCharClassSearcher(vowels, 1)

	tests := []struct {
		input     string
		wantFound bool
	}{
		{"hello", true},   // 'e' and 'o' are vowels
		{"xyz", false},    // no vowels
		{"aeiou", true},   // all vowels
		{"bcdfg", false},  // no vowels
		{"AEI", false},    // uppercase vowels don't match
		{"", false},       // empty
		{"rhythm", false}, // no vowels
		{"a", true},       // single vowel
		{"b", false},      // single consonant
		{"ea", true},      // two vowels adjacent
		{"cat", true},     // vowel in middle
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, _, found := s.Search([]byte(tt.input))
			if found != tt.wantFound {
				t.Errorf("Search(%q) found=%v, want %v", tt.input, found, tt.wantFound)
			}
		})
	}
}

// TestCharClassSearcher_MinMatch_Zero tests searcher with minMatch=0 (star quantifier).
func TestCharClassSearcher_MinMatch_Zero(t *testing.T) {
	ranges := [][2]byte{{'a', 'z'}}
	s := NewCharClassSearcher(ranges, 0) // * quantifier — minMatch = 0

	tests := []struct {
		name      string
		input     string
		wantS     int
		wantE     int
		wantFound bool
	}{
		// With minMatch=0, even a zero-length run at a non-matching position
		// should be found. But our implementation skips zero-length in SearchAt
		// when no character matches — the first matching character starts the run.
		{"all matching", "abc", 0, 3, true},
		{"partial", "123abc456", 3, 6, true},
		{"no matching chars", "123", -1, -1, false},
		{"empty", "", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := s.Search([]byte(tt.input))
			if found != tt.wantFound || start != tt.wantS || end != tt.wantE {
				t.Errorf("Search(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.input, start, end, found, tt.wantS, tt.wantE, tt.wantFound)
			}
		})
	}
}

// TestCharClassSearcher_FindAllIndices_Comprehensive tests FindAllIndices with various inputs.
func TestCharClassSearcher_FindAllIndices_Comprehensive(t *testing.T) {
	// Digit class
	digitRanges := [][2]byte{{'0', '9'}}
	s := NewCharClassSearcher(digitRanges, 1)

	tests := []struct {
		name  string
		input string
		want  [][2]int
	}{
		{"no digits", "hello world", nil},
		{"single digit", "a1b", [][2]int{{1, 2}}},
		{"consecutive digits", "abc123def", [][2]int{{3, 6}}},
		{"multiple groups", "1 22 333", [][2]int{{0, 1}, {2, 4}, {5, 8}}},
		{"digits at boundaries", "123abc456", [][2]int{{0, 3}, {6, 9}}},
		{"all digits", "12345", [][2]int{{0, 5}}},
		{"empty", "", nil},
		{"alternating", "1a2b3c", [][2]int{{0, 1}, {2, 3}, {4, 5}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.FindAllIndices([]byte(tt.input), nil)
			if len(got) != len(tt.want) {
				t.Errorf("FindAllIndices(%q) = %v (len=%d), want %v (len=%d)",
					tt.input, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("FindAllIndices(%q)[%d] = %v, want %v",
						tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestCharClassSearcher_FindAllIndices_ReuseBuffer tests buffer reuse optimization.
func TestCharClassSearcher_FindAllIndices_ReuseBuffer(t *testing.T) {
	ranges := [][2]byte{{'a', 'z'}}
	s := NewCharClassSearcher(ranges, 1)

	// Pre-allocate buffer
	buf := make([][2]int, 0, 100)

	// First call
	result := s.FindAllIndices([]byte("abc 123 def"), buf)
	if len(result) != 2 {
		t.Errorf("first call: got %d matches, want 2", len(result))
	}

	// Second call reusing buffer
	result2 := s.FindAllIndices([]byte("one two three"), result)
	if len(result2) != 3 {
		t.Errorf("second call: got %d matches, want 3", len(result2))
	}
}

// TestCharClassSearcher_Count_VsFindAll verifies Count matches len(FindAllIndices).
func TestCharClassSearcher_Count_VsFindAll(t *testing.T) {
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	s := NewCharClassSearcher(ranges, 1)

	inputs := []string{
		"hello world",
		"",
		"   ",
		"abc 123 def 456",
		"singleword",
		"a b c d e f g",
		"!@# $%^ &*()",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			count := s.Count([]byte(input))
			indices := s.FindAllIndices([]byte(input), nil)

			if count != len(indices) {
				t.Errorf("Count=%d, len(FindAllIndices)=%d for %q", count, len(indices), input)
			}
		})
	}
}

// TestCharClassSearcher_CanHandle tests that CanHandle always returns true.
func TestCharClassSearcher_CanHandle(t *testing.T) {
	ranges := [][2]byte{{'a', 'z'}}
	s := NewCharClassSearcher(ranges, 1)

	sizes := []int{0, 1, 100, 1000, 1000000, 100000000}
	for _, size := range sizes {
		if !s.CanHandle(size) {
			t.Errorf("CanHandle(%d) = false, want true", size)
		}
	}
}

// TestCharClassSearcher_FullByteRange tests searcher matching all bytes (0x00-0xFF).
func TestCharClassSearcher_FullByteRange(t *testing.T) {
	ranges := [][2]byte{{0x00, 0xFF}}
	s := NewCharClassSearcher(ranges, 1)

	// Everything matches — should find single match spanning entire input
	input := []byte("any\x00thing\xFFhere")
	start, end, found := s.Search(input)
	if !found {
		t.Error("full range should match anything")
	}
	if start != 0 || end != len(input) {
		t.Errorf("got (%d, %d), want (0, %d)", start, end, len(input))
	}
}

// TestCharClassSearcher_HighByte tests searcher with high byte values (0x80-0xFF).
func TestCharClassSearcher_HighByte(t *testing.T) {
	ranges := [][2]byte{{0x80, 0xFF}}
	s := NewCharClassSearcher(ranges, 1)

	// UTF-8 multi-byte sequences have high bytes
	input := []byte("hello мир end") // мир = 6 high bytes
	start, end, found := s.Search(input)
	_ = end
	if !found {
		t.Error("should match high bytes in UTF-8 text")
	}
	if start != 6 {
		t.Errorf("start = %d, want 6 (start of мир)", start)
	}
}

// TestCharClassSearcher_VsStdlib_WordClass compares \w+ searcher against stdlib.
func TestCharClassSearcher_VsStdlib_WordClass(t *testing.T) {
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	s := NewCharClassSearcher(ranges, 1)
	re := regexp.MustCompile(`\w+`)

	inputs := []string{
		"hello world",
		"  spaces  ",
		"123abc_DEF",
		"!@#$%",
		"",
		"a",
		"one two three four five",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			stdAll := re.FindAllStringIndex(input, -1)
			ourAll := s.FindAllIndices([]byte(input), nil)

			if len(stdAll) != len(ourAll) {
				t.Errorf("count mismatch: stdlib=%d, ours=%d for %q",
					len(stdAll), len(ourAll), input)
				return
			}

			for i := range stdAll {
				if stdAll[i][0] != ourAll[i][0] || stdAll[i][1] != ourAll[i][1] {
					t.Errorf("match %d: stdlib=%v, ours=%v",
						i, stdAll[i], ourAll[i])
				}
			}
		})
	}
}

// TestCharClassSearcherFromNFA_Integration tests NewCharClassSearcherFromNFA
// with compiled NFA patterns.
func TestCharClassSearcherFromNFA_Integration(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantNil   bool
		testInput string
		wantMatch bool
		wantStart int
		wantEnd   int
	}{
		{
			name:      "simple byte range",
			pattern:   "[a-z]",
			wantNil:   false,
			testInput: "123abc",
			wantMatch: true,
			wantStart: 3,
			wantEnd:   6,
		},
		{
			name:    "complex pattern not extractable",
			pattern: "foo|bar",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("compile %q: %v", tt.pattern, err)
			}

			searcher := NewCharClassSearcherFromNFA(nfa)
			if tt.wantNil {
				if searcher != nil {
					t.Error("expected nil searcher for non-char-class pattern")
				}
				return
			}

			if searcher == nil {
				t.Fatal("expected non-nil searcher")
			}

			if tt.testInput != "" {
				start, end, found := searcher.Search([]byte(tt.testInput))
				if found != tt.wantMatch {
					t.Errorf("Search found=%v, want %v", found, tt.wantMatch)
				}
				if found && (start != tt.wantStart || end != tt.wantEnd) {
					t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
				}
			}
		})
	}
}
