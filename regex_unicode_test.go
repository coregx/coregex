package coregex

import (
	"regexp"
	"testing"
)

// TestUnicodeCharClass tests that Unicode character classes work correctly.
// This is a regression test for the bug where CharClassSearcher was incorrectly
// used for patterns with runes > 127 (like ö = code point 246).
// The issue: ö has code point 246 which is < 255, but UTF-8 encoding is
// 0xC3 0xB6 (2 bytes), so byte lookup table doesn't work.
func TestUnicodeCharClass(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    string // expected match, "" for no match
	}{
		// Mixed ASCII + Unicode
		{`[föd]+`, "fööd", "fööd"},
		{`[föd]+`, "food", "f"},     // 'o' is not in [föd], so only 'f' matches
		{`[food]+`, "food", "food"}, // ASCII-only class for comparison
		{`[föd]+`, "hello fööd world", "fööd"},

		// All Unicode
		{`[äöü]+`, "äöü", "äöü"},
		{`[äöü]+`, "hello äöü world", "äöü"},
		{`[äöü]+`, "abc", ""}, // no match

		// Unicode literal (should work via different code path)
		{`ö+`, "öööö", "öööö"},
		{`ö+`, "xöööy", "ööö"},

		// Alternation with Unicode (different code path)
		{`(ö|a)+`, "öaöa", "öaöa"},
		{`(ä|ö|ü)+`, "äöü", "äöü"},

		// ASCII patterns should still work
		{`[a-z]+`, "hello", "hello"},
		{`[a-z]+`, "HELLO", ""}, // no match
		{`[\w]+`, "hello123", "hello123"},

		// Edge case: ASCII text with Unicode pattern
		{`[äöü]+`, "hello", ""}, // no match

		// Edge case: Unicode text with ASCII pattern
		{`[a-z]+`, "café", "caf"}, // matches only ASCII part
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.text, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			got := re.FindString(tt.text)
			if got != tt.want {
				t.Errorf("coregex.FindString(%q, %q) = %q, want %q",
					tt.pattern, tt.text, got, tt.want)
			}

			// Verify against stdlib
			reStd := regexp.MustCompile(tt.pattern)
			gotStd := reStd.FindString(tt.text)
			if got != gotStd {
				t.Errorf("coregex.FindString(%q, %q) = %q, stdlib = %q (mismatch!)",
					tt.pattern, tt.text, got, gotStd)
			}
		})
	}
}

// TestUnicodeCharClassFindIndex tests that match positions are correct for Unicode.
func TestUnicodeCharClassFindIndex(t *testing.T) {
	tests := []struct {
		pattern   string
		text      string
		wantStart int
		wantEnd   int
	}{
		// "絵 fööd y" - 絵 is 3 bytes, space is 1, fööd is 6 bytes (f=1, ö=2, ö=2, d=1)
		{`[föd]+`, "絵 fööd y", 4, 10}, // start=4 (after "絵 "), end=10 (length 6)
		{`[äöü]+`, "test äöü end", 5, 11},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			idx := re.FindStringIndex(tt.text)
			if idx == nil {
				t.Fatalf("coregex.FindStringIndex(%q, %q) = nil, want [%d, %d]",
					tt.pattern, tt.text, tt.wantStart, tt.wantEnd)
			}
			if idx[0] != tt.wantStart || idx[1] != tt.wantEnd {
				t.Errorf("coregex.FindStringIndex(%q, %q) = [%d, %d], want [%d, %d]",
					tt.pattern, tt.text, idx[0], idx[1], tt.wantStart, tt.wantEnd)
			}

			// Verify against stdlib
			reStd := regexp.MustCompile(tt.pattern)
			idxStd := reStd.FindStringIndex(tt.text)
			if len(idxStd) != 2 || idx[0] != idxStd[0] || idx[1] != idxStd[1] {
				t.Errorf("coregex vs stdlib mismatch: coregex=[%d,%d], stdlib=%v",
					idx[0], idx[1], idxStd)
			}
		})
	}
}
