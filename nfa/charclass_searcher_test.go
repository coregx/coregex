package nfa

import (
	"regexp"
	"regexp/syntax"
	"testing"
)

func TestCharClassSearcher_WordClass(t *testing.T) {
	// \w = [a-zA-Z0-9_]
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	s := NewCharClassSearcher(ranges, 1)

	tests := []struct {
		input     string
		wantS     int
		wantE     int
		wantFound bool
	}{
		{"hello", 0, 5, true},
		{"  hello  ", 2, 7, true},
		{"123abc", 0, 6, true},
		{"   ", -1, -1, false},
		{"!@#$%", -1, -1, false},
		{"a", 0, 1, true},
		{"", -1, -1, false},
		{"hello world", 0, 5, true},
		{"___test___", 0, 10, true},
	}

	for _, tt := range tests {
		start, end, found := s.Search([]byte(tt.input))
		if found != tt.wantFound || start != tt.wantS || end != tt.wantE {
			t.Errorf("Search(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tt.input, start, end, found, tt.wantS, tt.wantE, tt.wantFound)
		}
	}
}

func TestCharClassSearcher_DigitClass(t *testing.T) {
	// \d = [0-9]
	ranges := [][2]byte{{'0', '9'}}
	s := NewCharClassSearcher(ranges, 1)

	tests := []struct {
		input     string
		wantS     int
		wantE     int
		wantFound bool
	}{
		{"12345", 0, 5, true},
		{"abc123def", 3, 6, true},
		{"abc", -1, -1, false},
		{"", -1, -1, false},
	}

	for _, tt := range tests {
		start, end, found := s.Search([]byte(tt.input))
		if found != tt.wantFound || start != tt.wantS || end != tt.wantE {
			t.Errorf("Search(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tt.input, start, end, found, tt.wantS, tt.wantE, tt.wantFound)
		}
	}
}

func TestCharClassSearcher_SearchAt(t *testing.T) {
	ranges := [][2]byte{{'a', 'z'}}
	s := NewCharClassSearcher(ranges, 1)

	input := "123abc456def789"

	// First match at position 3
	start, end, found := s.SearchAt([]byte(input), 0)
	if !found || start != 3 || end != 6 {
		t.Errorf("SearchAt(0) = (%d, %d, %v), want (3, 6, true)", start, end, found)
	}

	// Search from position 6 should find "def"
	start, end, found = s.SearchAt([]byte(input), 6)
	if !found || start != 9 || end != 12 {
		t.Errorf("SearchAt(6) = (%d, %d, %v), want (9, 12, true)", start, end, found)
	}

	// Search from position 12 should find nothing
	start, end, found = s.SearchAt([]byte(input), 12)
	if found {
		t.Errorf("SearchAt(12) = (%d, %d, %v), want not found", start, end, found)
	}
}

func TestCharClassSearcher_IsMatch(t *testing.T) {
	ranges := [][2]byte{{'a', 'z'}}
	s := NewCharClassSearcher(ranges, 1)

	if !s.IsMatch([]byte("hello")) {
		t.Error("IsMatch(hello) should be true")
	}
	if !s.IsMatch([]byte("123abc456")) {
		t.Error("IsMatch(123abc456) should be true")
	}
	if s.IsMatch([]byte("12345")) {
		t.Error("IsMatch(12345) should be false")
	}
	if s.IsMatch([]byte("")) {
		t.Error("IsMatch('') should be false")
	}
}

// Benchmark against stdlib
func BenchmarkCharClassSearcher_Word(b *testing.B) {
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	s := NewCharClassSearcher(ranges, 1)
	re := regexp.MustCompile(`\w+`)

	input := []byte("   hello_world123   this is a test with words and numbers 12345   ")

	b.Run("CharClassSearcher", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s.Search(input)
		}
	})

	b.Run("stdlib", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			re.FindIndex(input)
		}
	})
}

func BenchmarkCharClassSearcher_FindAll(b *testing.B) {
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	s := NewCharClassSearcher(ranges, 1)
	re := regexp.MustCompile(`\w+`)

	input := []byte("   hello_world123   this is a test with words and numbers 12345   ")

	b.Run("CharClassSearcher", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			at := 0
			for {
				_, end, found := s.SearchAt(input, at)
				if !found {
					break
				}
				at = end
			}
		}
	})

	b.Run("stdlib", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			re.FindAllIndex(input, -1)
		}
	})
}

func BenchmarkCharClassSearcher_vs_BoundedBacktracker(b *testing.B) {
	// Build NFA for [\w]+
	re, _ := syntax.Parse(`[\w]+`, syntax.Perl)
	compiler := NewCompiler(CompilerConfig{UTF8: true})
	n, _ := compiler.CompileRegexp(re)

	// Create searchers
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	charClass := NewCharClassSearcher(ranges, 1)
	bounded := NewBoundedBacktracker(n)

	input := []byte("   hello_world123   this is a test with words and numbers 12345   ")

	b.Run("CharClassSearcher", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			charClass.Search(input)
		}
	})

	b.Run("BoundedBacktracker", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bounded.Search(input)
		}
	})
}

func BenchmarkCharClassSearcher_vs_BoundedBacktracker_Large(b *testing.B) {
	// Build NFA for [\w]+
	re, _ := syntax.Parse(`[\w]+`, syntax.Perl)
	compiler := NewCompiler(CompilerConfig{UTF8: true})
	n, _ := compiler.CompileRegexp(re)

	// Create searchers
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	charClass := NewCharClassSearcher(ranges, 1)
	bounded := NewBoundedBacktracker(n)

	// 1KB input with many matches
	input := make([]byte, 1024)
	for i := range input {
		if i%10 < 5 {
			input[i] = 'a' + byte(i%26)
		} else {
			input[i] = ' '
		}
	}

	b.Run("CharClassSearcher/1KB", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			at := 0
			for {
				_, end, found := charClass.SearchAt(input, at)
				if !found {
					break
				}
				at = end
			}
		}
	})

	b.Run("BoundedBacktracker/1KB", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			at := 0
			for {
				_, end, found := bounded.SearchAt(input, at)
				if !found {
					break
				}
				at = end
			}
		}
	})
}

// TestCharClassSearcher_FindAllIndices tests the streaming FindAllIndices method
func TestCharClassSearcher_FindAllIndices(t *testing.T) {
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	s := NewCharClassSearcher(ranges, 1)

	tests := []struct {
		name  string
		input string
		want  [][2]int
	}{
		{"empty", "", nil},
		{"single_word", "hello", [][2]int{{0, 5}}},
		{"multiple_words", "hello world", [][2]int{{0, 5}, {6, 11}}},
		{"leading_spaces", "   hello", [][2]int{{3, 8}}},
		{"trailing_spaces", "hello   ", [][2]int{{0, 5}}},
		{"mixed", "a b c", [][2]int{{0, 1}, {2, 3}, {4, 5}}},
		{"digits", "123 456 789", [][2]int{{0, 3}, {4, 7}, {8, 11}}},
		{"underscore", "foo_bar_baz", [][2]int{{0, 11}}},
		{"no_matches", "!@# $%^", nil},
		{"complex", "  abc123  DEF_456  ", [][2]int{{2, 8}, {10, 17}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.FindAllIndices([]byte(tt.input), nil)
			if len(got) != len(tt.want) {
				t.Errorf("FindAllIndices(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("FindAllIndices(%q)[%d] = %v, want %v", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestCharClassSearcher_Count tests the streaming Count method
func TestCharClassSearcher_Count(t *testing.T) {
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	s := NewCharClassSearcher(ranges, 1)

	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world", 2},
		{"a b c d e", 5},
		{"!@# $%^", 0},
		{"  abc123  DEF_456  ", 2},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := s.Count([]byte(tt.input))
			if got != tt.want {
				t.Errorf("Count(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// BenchmarkCharClassSearcher_StreamingVsLoop compares streaming vs loop-based FindAll
func BenchmarkCharClassSearcher_StreamingVsLoop(b *testing.B) {
	ranges := [][2]byte{
		{'a', 'z'},
		{'A', 'Z'},
		{'0', '9'},
		{'_', '_'},
	}
	s := NewCharClassSearcher(ranges, 1)

	// Generate 4KB input with ~400 matches
	input := make([]byte, 4096)
	for i := range input {
		if i%10 < 5 {
			input[i] = 'a' + byte(i%26)
		} else {
			input[i] = ' '
		}
	}

	// Fair comparison: both build results slice
	b.Run("Streaming/4KB", func(b *testing.B) {
		b.ReportAllocs()
		var results [][2]int // reusable buffer
		for i := 0; i < b.N; i++ {
			results = s.FindAllIndices(input, results)
		}
	})

	b.Run("LoopWithResults/4KB", func(b *testing.B) {
		b.ReportAllocs()
		results := make([][2]int, 0, 512) // pre-allocate
		for i := 0; i < b.N; i++ {
			results = results[:0]
			at := 0
			for {
				start, end, found := s.SearchAt(input, at)
				if !found {
					break
				}
				results = append(results, [2]int{start, end})
				at = end
			}
		}
	})

	// Count-only comparison (for reference)
	b.Run("StreamingCount/4KB", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s.Count(input)
		}
	})

	b.Run("LoopCount/4KB", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			at := 0
			count := 0
			for {
				_, end, found := s.SearchAt(input, at)
				if !found {
					break
				}
				count++
				at = end
			}
		}
	})

	// Verify both produce same results
	streamResults := s.FindAllIndices(input, nil)
	loopCount := 0
	at := 0
	for {
		_, end, found := s.SearchAt(input, at)
		if !found {
			break
		}
		loopCount++
		at = end
	}
	if len(streamResults) != loopCount {
		b.Fatalf("mismatch: streaming=%d, loop=%d", len(streamResults), loopCount)
	}
}
