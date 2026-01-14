package nfa

import (
	"regexp/syntax"
	"testing"
)

func TestCompositeSearcher_Basic(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Basic patterns
		{"[a-z]+[0-9]+", "abc123", true},
		{"[a-z]+[0-9]+", "test456end", true},
		{"[a-z]+[0-9]+", "a1", true},
		{"[a-z]+[0-9]+", "123", false},
		{"[a-z]+[0-9]+", "abc", false},
		{"[a-z]+[0-9]+", "", false},

		// Mixed case
		{"[a-zA-Z]+[0-9]+", "ABC123", true},
		{"[a-zA-Z]+[0-9]+", "abc456", true},
		{"[a-zA-Z]+[0-9]+", "Test789", true},

		// Three parts
		{"[a-z]+[0-9]+[a-z]+", "abc123def", true},
		{"[a-z]+[0-9]+[a-z]+", "abc123", false},
		{"[a-z]+[0-9]+[a-z]+", "a1b", true},

		// With star (optional)
		{"[a-z]+[0-9]*", "abc", true},
		{"[a-z]+[0-9]*", "abc123", true},
		{"[a-z]+[0-9]*", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			cs := NewCompositeSearcher(re)
			if cs == nil {
				t.Fatalf("Failed to create CompositeSearcher for %s", tt.pattern)
			}

			got := cs.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompositeSearcher_Search(t *testing.T) {
	re, _ := syntax.Parse("[a-z]+[0-9]+", syntax.Perl)
	cs := NewCompositeSearcher(re)

	tests := []struct {
		input     string
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		{"abc123", 0, 6, true},
		{"prefix abc123 suffix", 7, 13, true},
		{"123", -1, -1, false},
		{"", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			start, end, ok := cs.Search([]byte(tt.input))
			if ok != tt.wantOK {
				t.Errorf("Search(%q).ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search(%q) = (%d, %d), want (%d, %d)",
					tt.input, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestIsCompositeCharClassPattern(t *testing.T) {
	valid := []string{
		"[a-z]+[0-9]+",
		"[a-zA-Z]+[0-9]+",
		"[a-z]+[0-9]+[a-z]+",
		"[0-9]+[a-z]+",
		"[a-z]*[0-9]+",
		"[a-z]+[0-9]*",
	}

	for _, pattern := range valid {
		t.Run("valid_"+pattern, func(t *testing.T) {
			re, err := syntax.Parse(pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if !IsCompositeCharClassPattern(re) {
				t.Errorf("Expected %q to be composite char class", pattern)
			}
		})
	}

	invalid := []string{
		"[a-z]+",         // Single char class
		"abc",            // Literal
		"[a-z]+|[0-9]+",  // Alternation
		"^[a-z]+[0-9]+",  // Anchored
		"([a-z]+)[0-9]+", // With capture
	}

	for _, pattern := range invalid {
		t.Run("invalid_"+pattern, func(t *testing.T) {
			re, err := syntax.Parse(pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if IsCompositeCharClassPattern(re) {
				t.Errorf("Expected %q NOT to be composite char class", pattern)
			}
		})
	}
}

// TestCompositeSearcher_OverlappingCharClasses tests patterns where character classes overlap.
// This is a regression test for Issue #81: \w+[0-9]+ failed because \w includes digits,
// so greedy \w+ consumed all characters including digits, leaving nothing for [0-9]+.
// The fix implements backtracking: try greedy first, reduce if next parts fail.
func TestCompositeSearcher_OverlappingCharClasses(t *testing.T) {
	tests := []struct {
		pattern   string
		input     string
		wantMatch bool
		wantStart int
		wantEnd   int
	}{
		// \w includes [a-zA-Z0-9_], so \w+ and [0-9]+ overlap on digits
		{"[a-zA-Z0-9]+[0-9]+", "abc123", true, 0, 6},
		{"[a-zA-Z0-9]+[0-9]+", "123456", true, 0, 6},
		{"[a-zA-Z0-9]+[0-9]+", "a1", true, 0, 2},
		{"[a-zA-Z0-9]+[0-9]+", "abc", false, -1, -1}, // No digits for [0-9]+

		// Triple overlap: all three parts can match digits
		{"[0-9a-z]+[0-9]+[0-9a-z]+", "a1b", true, 0, 3},
		{"[0-9a-z]+[0-9]+[0-9a-z]+", "123", true, 0, 3},
		{"[0-9a-z]+[0-9]+[0-9a-z]+", "12", false, -1, -1}, // Need 3 chars minimum

		// Complex overlap with different quantifiers
		{"[0-9]+[0-9]*", "123", true, 0, 3},   // First needs 1+, second can be 0
		{"[0-9]*[0-9]+", "123", true, 0, 3},   // First can be 0, second needs 1+
		{"[0-9]+[0-9]+", "12", true, 0, 2},    // Each needs at least 1
		{"[0-9]+[0-9]+", "1", false, -1, -1},  // Can't split 1 char into two 1+ parts
		{"[0-9]+[0-9]+[0-9]+", "123", true, 0, 3}, // Three parts, need 3 chars
		{"[0-9]+[0-9]+[0-9]+", "12", false, -1, -1}, // Three parts, only 2 chars

		// Embedded in text
		{"[a-zA-Z0-9]+[0-9]+", "prefix abc123 suffix", true, 7, 13},
		{"[a-zA-Z0-9]+[0-9]+", "test99end", true, 0, 6},

		// Edge cases with star (can match 0)
		{"[0-9]*[0-9]+", "5", true, 0, 1},     // Star takes 0, plus takes 1
		{"[0-9]+[0-9]*", "5", true, 0, 1},     // Plus takes 1, star takes 0
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			cs := NewCompositeSearcher(re)
			if cs == nil {
				t.Fatalf("Failed to create CompositeSearcher for %s", tt.pattern)
			}

			// Test IsMatch
			gotMatch := cs.IsMatch([]byte(tt.input))
			if gotMatch != tt.wantMatch {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, gotMatch, tt.wantMatch)
			}

			// Test Search positions
			start, end, ok := cs.Search([]byte(tt.input))
			if ok != tt.wantMatch {
				t.Errorf("Search(%q).ok = %v, want %v", tt.input, ok, tt.wantMatch)
			}
			if ok && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search(%q) = (%d, %d), want (%d, %d)",
					tt.input, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func BenchmarkCompositeSearcher(b *testing.B) {
	re, _ := syntax.Parse("[a-zA-Z]+[0-9]+", syntax.Perl)
	cs := NewCompositeSearcher(re)
	input := []byte("The quick brown fox123 jumps over456 lazy dog789")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cs.IsMatch(input)
	}
}

func BenchmarkCompositeSearcher_Overlapping(b *testing.B) {
	re, _ := syntax.Parse("[a-zA-Z0-9]+[0-9]+", syntax.Perl)
	cs := NewCompositeSearcher(re)
	input := []byte("The quick brown fox123 jumps over456 lazy dog789")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cs.IsMatch(input)
	}
}
