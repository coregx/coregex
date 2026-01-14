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

// TestDotMatchesUTF8Codepoints tests that '.' matches UTF-8 codepoints, not bytes.
// This is a regression test for issue #85.
// The bug: '.' was matching individual bytes (0x00-0xFF) instead of full UTF-8
// codepoints, causing FindAllString(`.`, "日本語") to return 9 matches (bytes)
// instead of 3 matches (codepoints).
func TestDotMatchesUTF8Codepoints(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    int // expected number of matches
	}{
		// Japanese characters (3 bytes each in UTF-8)
		{"japanese_dot", `.`, "日本語", 3},
		{"japanese_dot_plus", `.+`, "日本語", 1},

		// Emoji (4 bytes each in UTF-8)
		{"emoji_dot", `.`, "😀😁", 2},
		{"emoji_dot_plus", `.+`, "😀😁", 1},

		// Mixed ASCII and multibyte
		{"mixed_dot", `.`, "a日b", 3},
		{"mixed_dot_plus", `.+`, "a日b", 1},

		// Cyrillic (2 bytes each in UTF-8)
		{"cyrillic_dot", `.`, "Привет", 6},
		{"cyrillic_dot_plus", `.+`, "Привет", 1},

		// German umlauts (2 bytes each in UTF-8)
		{"umlaut_dot", `.`, "äöü", 3},
		{"umlaut_dot_plus", `.+`, "äöü", 1},

		// Newline handling: '.' should NOT match newline
		{"dot_no_newline", `.`, "a\nb", 2},
		{"dot_no_newline_unicode", `.`, "日\n本", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			matches := re.FindAllString(tt.input, -1)
			got := len(matches)

			if got != tt.want {
				t.Errorf("coregex.FindAllString(%q, %q) returned %d matches, want %d (matches: %v)",
					tt.pattern, tt.input, got, tt.want, matches)
			}

			// Verify against stdlib
			reStd := regexp.MustCompile(tt.pattern)
			matchesStd := reStd.FindAllString(tt.input, -1)
			gotStd := len(matchesStd)

			if got != gotStd {
				t.Errorf("coregex vs stdlib mismatch: coregex=%d matches, stdlib=%d matches",
					got, gotStd)
			}
		})
	}
}

// TestDotSMatchesAll tests that (?s). (dotall mode) matches everything including newlines.
func TestDotSMatchesAll(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    int
	}{
		{"dotall_newline", `(?s).`, "a\nb", 3},
		{"dotall_unicode_newline", `(?s).`, "日\n本", 3},
		{"dotall_plus", `(?s).+`, "a\nb\nc", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			matches := re.FindAllString(tt.input, -1)
			got := len(matches)

			if got != tt.want {
				t.Errorf("coregex.FindAllString(%q, %q) returned %d matches, want %d",
					tt.pattern, tt.input, got, tt.want)
			}

			// Verify against stdlib
			reStd := regexp.MustCompile(tt.pattern)
			matchesStd := reStd.FindAllString(tt.input, -1)
			gotStd := len(matchesStd)

			if got != gotStd {
				t.Errorf("coregex vs stdlib mismatch: coregex=%d, stdlib=%d",
					got, gotStd)
			}
		})
	}
}
