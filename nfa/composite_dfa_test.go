package nfa

import (
	"bytes"
	"regexp/syntax"
	"testing"
)

func TestCompositeSequenceDFA_Basic(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			dfa := NewCompositeSequenceDFA(re)
			if dfa == nil {
				t.Fatalf("Failed to create CompositeSequenceDFA for %s", tt.pattern)
			}

			got := dfa.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompositeSequenceDFA_Search(t *testing.T) {
	re, _ := syntax.Parse("[a-z]+[0-9]+", syntax.Perl)
	dfa := NewCompositeSequenceDFA(re)

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
			start, end, ok := dfa.Search([]byte(tt.input))
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

func TestCompositeSequenceDFA_OverlappingCharClasses(t *testing.T) {
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

		// Embedded in text
		{"[a-zA-Z0-9]+[0-9]+", "prefix abc123 suffix", true, 7, 13},
		{"[a-zA-Z0-9]+[0-9]+", "test99end", true, 0, 6},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			dfa := NewCompositeSequenceDFA(re)
			if dfa == nil {
				t.Fatalf("Failed to create CompositeSequenceDFA for %s", tt.pattern)
			}

			// Test IsMatch
			gotMatch := dfa.IsMatch([]byte(tt.input))
			if gotMatch != tt.wantMatch {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, gotMatch, tt.wantMatch)
			}

			// Test Search positions
			start, end, ok := dfa.Search([]byte(tt.input))
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

// Benchmark comparing CompositeSearcher vs CompositeSequenceDFA
func BenchmarkCompositeSequenceDFA(b *testing.B) {
	// Generate 1MB of test data
	var buf bytes.Buffer
	patterns := []string{
		"hello world ", "test123 ", "foo456bar ", "abc ", "xyz789 ",
		"quick brown fox ", "lazy dog ", "word42 ", "sample99text ",
	}
	for buf.Len() < 1024*1024 {
		for _, p := range patterns {
			buf.WriteString(p)
		}
	}
	input := buf.Bytes()

	b.Run("NonOverlap_CompositeSearcher", func(b *testing.B) {
		re, _ := syntax.Parse("[a-zA-Z]+[0-9]+", syntax.Perl)
		cs := NewCompositeSearcher(re)
		b.SetBytes(int64(len(input)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := 0
			for {
				_, end, ok := cs.SearchAt(input, pos)
				if !ok {
					break
				}
				pos = end
			}
		}
	})

	b.Run("NonOverlap_CompositeSequenceDFA", func(b *testing.B) {
		re, _ := syntax.Parse("[a-zA-Z]+[0-9]+", syntax.Perl)
		dfa := NewCompositeSequenceDFA(re)
		b.SetBytes(int64(len(input)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := 0
			for {
				_, end, ok := dfa.SearchAt(input, pos)
				if !ok {
					break
				}
				pos = end
			}
		}
	})

	b.Run("Overlap_CompositeSearcher", func(b *testing.B) {
		re, _ := syntax.Parse("[a-zA-Z0-9]+[0-9]+", syntax.Perl)
		cs := NewCompositeSearcher(re)
		b.SetBytes(int64(len(input)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := 0
			for {
				_, end, ok := cs.SearchAt(input, pos)
				if !ok {
					break
				}
				pos = end
			}
		}
	})

	b.Run("Overlap_CompositeSequenceDFA", func(b *testing.B) {
		re, _ := syntax.Parse("[a-zA-Z0-9]+[0-9]+", syntax.Perl)
		dfa := NewCompositeSequenceDFA(re)
		b.SetBytes(int64(len(input)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := 0
			for {
				_, end, ok := dfa.SearchAt(input, pos)
				if !ok {
					break
				}
				pos = end
			}
		}
	})
}

// Legacy benchmark
func BenchmarkCompositeSequenceDFA_Legacy(b *testing.B) {
	// Generate 1MB of test data
	var buf bytes.Buffer
	patterns := []string{
		"hello world ", "test123 ", "foo456bar ", "abc ", "xyz789 ",
		"quick brown fox ", "lazy dog ", "word42 ", "sample99text ",
	}
	for buf.Len() < 1024*1024 {
		for _, p := range patterns {
			buf.WriteString(p)
		}
	}
	input := buf.Bytes()

	b.Run("CompositeSearcher", func(b *testing.B) {
		re, _ := syntax.Parse("[a-zA-Z]+[0-9]+", syntax.Perl)
		cs := NewCompositeSearcher(re)
		b.SetBytes(int64(len(input)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := 0
			for {
				_, end, ok := cs.SearchAt(input, pos)
				if !ok {
					break
				}
				pos = end
			}
		}
	})

	b.Run("CompositeSequenceDFA", func(b *testing.B) {
		re, _ := syntax.Parse("[a-zA-Z]+[0-9]+", syntax.Perl)
		dfa := NewCompositeSequenceDFA(re)
		b.SetBytes(int64(len(input)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := 0
			for {
				_, end, ok := dfa.SearchAt(input, pos)
				if !ok {
					break
				}
				pos = end
			}
		}
	})
}
