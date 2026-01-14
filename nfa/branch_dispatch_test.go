package nfa

import (
	"regexp/syntax"
	"testing"
)

func TestBranchDispatcher_Basic(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Digit branch
		{`\d+|UUID|hex32`, "12345", true},
		{`\d+|UUID|hex32`, "0", true},
		{`\d+|UUID|hex32`, "999abc", true}, // Matches digits prefix

		// Literal branches
		{`\d+|UUID|hex32`, "UUID", true},
		{`\d+|UUID|hex32`, "UUID-1234", true},
		{`\d+|UUID|hex32`, "hex32", true},
		{`\d+|UUID|hex32`, "hex32abc", true},

		// No match - first byte doesn't match any branch
		{`\d+|UUID|hex32`, "abc", false},
		{`\d+|UUID|hex32`, "XYZ", false},
		{`\d+|UUID|hex32`, "", false},

		// Edge cases
		{`\d+|UUID|hex32`, "U", false},       // "U" doesn't match "UUID"
		{`\d+|UUID|hex32`, "UUI", false},     // "UUI" doesn't match "UUID"
		{`\d+|UUID|hex32`, "hex3", false},    // "hex3" doesn't match "hex32"
		{`\d+|UUID|hex32`, "hex321", true},   // "hex321" matches "hex32" prefix

		// Simple alternations
		{`foo|bar|baz`, "foo", true},
		{`foo|bar|baz`, "bar", true},
		{`foo|bar|baz`, "baz", true},
		{`foo|bar|baz`, "foobar", true},
		{`foo|bar|baz`, "qux", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			d := NewBranchDispatcher(re)
			if d == nil {
				t.Fatalf("Failed to create BranchDispatcher")
			}

			got := d.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestBranchDispatcher_Search(t *testing.T) {
	re, _ := syntax.Parse(`\d+|UUID|hex32`, syntax.Perl)
	d := NewBranchDispatcher(re)
	if d == nil {
		t.Fatal("Failed to create BranchDispatcher")
	}

	tests := []struct {
		input     string
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		{"12345", 0, 5, true},
		{"UUID", 0, 4, true},
		{"hex32", 0, 5, true},
		{"abc", -1, -1, false},
		{"", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			start, end, ok := d.Search([]byte(tt.input))
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

func TestBranchDispatcher_Unsuitable(t *testing.T) {
	unsuitable := []string{
		`abc`,              // Not alternation
		`a|a`,              // Overlapping first bytes
		`[a-z]+|abc`,       // Overlapping (both start with a-z)
		`\d+|\d\d`,         // Overlapping (both start with digits)
		`.+|foo`,           // . matches everything
	}

	for _, pattern := range unsuitable {
		t.Run(pattern, func(t *testing.T) {
			re, err := syntax.Parse(pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			d := NewBranchDispatcher(re)
			if d != nil {
				t.Errorf("Expected nil for unsuitable pattern %q", pattern)
			}
		})
	}
}

func BenchmarkBranchDispatcher(b *testing.B) {
	re, _ := syntax.Parse(`\d+|UUID|hex32`, syntax.Perl)
	d := NewBranchDispatcher(re)
	if d == nil {
		b.Fatal("Failed to create BranchDispatcher")
	}

	inputs := [][]byte{
		[]byte("12345 some text"),
		[]byte("UUID-1234-5678"),
		[]byte("hex32deadbeef"),
		[]byte("no match here"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			d.IsMatch(input)
		}
	}
}
