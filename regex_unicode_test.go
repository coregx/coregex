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

// TestEmptyCharacterClass tests that empty character classes like [^\S\s] never match.
// This is a regression test for issue #88.
// The bug: empty char classes were compiled as compileEmptyMatch() which matches empty string,
// but they should use compileNoMatch() to never match.
func TestEmptyCharacterClass(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{"negated_all_1", `[^\S\s]`, "abc"},
		{"negated_all_2", `[^\D\d]`, "abc123"},
		{"negated_all_3", `[^\W\w]`, "abc_123"},
		{"negated_all_unicode", `[^\S\s]`, "日本語"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)

			// Empty character class should never match
			if re.MatchString(tt.input) {
				t.Errorf("coregex.MatchString(%q, %q) = true, want false (empty class should never match)",
					tt.pattern, tt.input)
			}

			// Verify against stdlib
			reStd := regexp.MustCompile(tt.pattern)
			if reStd.MatchString(tt.input) != re.MatchString(tt.input) {
				t.Errorf("coregex vs stdlib mismatch for %q on %q", tt.pattern, tt.input)
			}
		})
	}
}

// TestNegatedUnicodePropertyClass tests that negated Unicode property classes like \P{Han}
// match complete UTF-8 codepoints, not individual bytes.
// This is a regression test for issue #91.
// The bug: \P{Han}+ on "中" (3-byte UTF-8) was returning 3 matches (bytes) instead of 0.
func TestNegatedUnicodePropertyClass(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    int // expected number of matches for FindAllString
	}{
		// \P{Han} matches any codepoint NOT in Han script
		// "中" is Han, so should NOT match
		{"han_char_no_match", `\P{Han}`, "中", 0},
		{"han_plus_no_match", `\P{Han}+`, "中", 0},

		// "abc" are ASCII, not Han, so should match
		{"ascii_matches", `\P{Han}`, "abc", 3},
		{"ascii_plus_matches", `\P{Han}+`, "abc", 1},

		// Mixed: "abc中文def" - should match "abc" and "def" but not "中文"
		{"mixed_han_ascii", `\P{Han}+`, "abc中文def", 2},

		// \P{Latin} matches non-Latin characters
		// "日本語" are not Latin, so should match
		{"non_latin_matches", `\P{Latin}`, "日本語", 3},
		{"non_latin_plus_matches", `\P{Latin}+`, "日本語", 1},

		// Latin text should not match \P{Latin}
		{"latin_no_match", `\P{Latin}+`, "abc", 0},

		// Emoji (4-byte UTF-8) with negated class
		{"emoji_not_latin", `\P{Latin}`, "😀", 1},
		{"emoji_not_han", `\P{Han}`, "😀", 1},

		// Cyrillic (2-byte UTF-8) with negated class
		{"cyrillic_not_latin", `\P{Latin}`, "Привет", 6},
		{"cyrillic_not_han", `\P{Han}+`, "Привет", 1},
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
				t.Errorf("coregex vs stdlib mismatch: coregex=%d matches %v, stdlib=%d matches %v",
					got, matches, gotStd, matchesStd)
			}
		})
	}
}
