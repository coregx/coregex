package nfa

import (
	"regexp/syntax"
	"testing"
)

func TestExtractCharClassRanges(t *testing.T) {
	tests := []struct {
		pattern    string
		wantRanges int // -1 means nil (not supported)
	}{
		// Supported patterns
		{`[a-z]+`, 1},
		{`[A-Z]+`, 1},
		{`[0-9]+`, 1},
		{`[\w]+`, 4}, // 0-9, A-Z, _, a-z (sorted by Go's syntax)
		{`[\d]+`, 1}, // 0-9
		{`[a-zA-Z]+`, 2},
		{`[a-zA-Z0-9]+`, 3},
		{`[a-zA-Z0-9_]+`, 4},
		{`[abc]+`, 1}, // Go optimizes consecutive chars [a-c] to single range

		// Not supported - no quantifier
		{`abc`, -1},    // No quantifier
		{`[a-z]`, -1},  // No quantifier (need + or *)
		{`[a-z]?`, -1}, // ? not supported
		{`a+`, -1},     // Single char, not char class

		// Not supported - complex patterns
		{`[a-z]+[0-9]+`, -1}, // Concatenation
		{`^[a-z]+`, -1},      // Anchor
		{`[a-z]+$`, -1},      // Anchor

		// Not supported - Unicode (rune > 127 requires multi-byte UTF-8)
		// This is critical: ö has code point 246 but UTF-8 encoding is 0xC3 0xB6 (2 bytes)
		// CharClassSearcher's 256-byte lookup table can't handle multi-byte UTF-8
		{`[föd]+`, -1}, // Mixed ASCII + Unicode
		{`[äöü]+`, -1}, // All Unicode (Latin-1 Supplement)
		{`[α-ω]+`, -1}, // Greek letters
		{`[日本語]+`, -1}, // CJK characters
		{`[ö]+`, -1},   // Single Unicode char
	}

	for _, tt := range tests {
		re, err := syntax.Parse(tt.pattern, syntax.Perl)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", tt.pattern, err)
		}

		ranges := ExtractCharClassRanges(re)
		gotRanges := -1
		if ranges != nil {
			gotRanges = len(ranges)
		}

		if gotRanges != tt.wantRanges {
			t.Errorf("ExtractCharClassRanges(%q) = %d ranges, want %d",
				tt.pattern, gotRanges, tt.wantRanges)
		}
	}
}

func TestIsSimpleCharClassPlus(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`[\w]+`, true},
		{`[a-z]+`, true},
		{`\d+`, true},
		{`abc`, false},
		{`a+`, false},
		{`(a|b)+`, false},
	}

	for _, tt := range tests {
		re, _ := syntax.Parse(tt.pattern, syntax.Perl)
		got := IsSimpleCharClassPlus(re)
		if got != tt.want {
			t.Errorf("IsSimpleCharClassPlus(%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}
