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
		"[a-z]+",        // Single char class
		"abc",           // Literal
		"[a-z]+|[0-9]+", // Alternation
		"^[a-z]+[0-9]+", // Anchored
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

func BenchmarkCompositeSearcher(b *testing.B) {
	re, _ := syntax.Parse("[a-zA-Z]+[0-9]+", syntax.Perl)
	cs := NewCompositeSearcher(re)
	input := []byte("The quick brown fox123 jumps over456 lazy dog789")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cs.IsMatch(input)
	}
}
