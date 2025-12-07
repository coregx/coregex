package coregex

import (
	"reflect"
	"regexp"
	"testing"
)

// Edge case tests based on gap analysis vs stdlib and Rust regex-automata.
// These tests catch subtle bugs in anchor handling, empty matches, and capture groups.

// compareWithStdlib is a helper that compares coregex with stdlib for all operations.
func compareWithStdlib(t *testing.T, pattern, input string) {
	t.Helper()

	stdRe := regexp.MustCompile(pattern)
	coRe := MustCompile(pattern)

	// Compare Match
	stdMatch := stdRe.MatchString(input)
	coMatch := coRe.MatchString(input)
	if stdMatch != coMatch {
		t.Errorf("MatchString mismatch: std=%v, co=%v", stdMatch, coMatch)
	}

	// Compare Find
	stdFind := stdRe.FindString(input)
	coFind := coRe.FindString(input)
	if stdFind != coFind {
		t.Errorf("FindString mismatch: std=%q, co=%q", stdFind, coFind)
	}

	// Compare FindAllString
	stdAll := stdRe.FindAllString(input, -1)
	coAll := coRe.FindAllString(input, -1)
	if !reflect.DeepEqual(stdAll, coAll) {
		t.Errorf("FindAllString mismatch:\n  std=%v\n  co=%v", stdAll, coAll)
	}

	// Compare FindAllStringIndex
	stdIdx := stdRe.FindAllStringIndex(input, -1)
	coIdx := coRe.FindAllStringIndex(input, -1)
	if !reflect.DeepEqual(stdIdx, coIdx) {
		t.Errorf("FindAllStringIndex mismatch:\n  std=%v\n  co=%v", stdIdx, coIdx)
	}
}

// =============================================================================
// HIGH PRIORITY: Empty Match Patterns
// =============================================================================

func TestEmptyMatchPatterns(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Empty alternation at start
		{"|b", "abc"},
		{"|b", ""},
		{"|b", "b"},
		// Empty alternation at end
		{"b|", "abc"},
		{"b|", "b"},
		{"b|", ""},
		// Multiple empty alternations
		{"||", "abc"},
		{"||", ""},
		// Non-capturing empty group
		{"(?:)|b", "abc"},
		{"(?:)|b", "b"},
		// Empty match followed by literal
		{"(?:)+|b", "abc"},
		// Star of empty-or-char
		{"(?:|a)*", "aaa"},
		{"(?:|a)*", ""},
		// Plus of empty-or-char
		{"(?:|a)+", "aaa"},
		{"(?:|a)+", ""},
		// Question of empty group
		{"(?:)?", "abc"},
		// Empty pattern
		{"", "abc"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// HIGH PRIORITY: Multiline Mode
// =============================================================================

func TestMultilineMode(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Basic multiline
		{"(?m)^[a-z]+$", "abc\ndef\nxyz"},
		{"(?m)^$", "abc\ndef\nxyz"},
		{"(?m)^", "abc\ndef\nxyz"},
		{"(?m)$", "abc\ndef\nxyz"},
		{"(?m)[a-z]$", "abc\ndef\nxyz"},
		// Multiline at empty positions
		{"(?m)^$", ""},
		{"(?m)^$", "\n"},
		{"(?m)^$", "\n\n"},
		{"(?m)^$", "a\n\nb"},
		// Multiline with repetition
		{"(?m)(?:^$)*", "a\nb\nc"},
		{"(?m)(?:^|a)+", "a\naaa\n"},
		// Multiline caret and dollar together
		{"(?m)^.*$", "abc\ndef"},
		{"(?m)^.+$", "abc\ndef"},
		// Mixed with non-multiline
		{"(?m)^abc", "abc\nabc"},
		{"(?m)abc$", "abc\nabc"},
		// Multiline with CRLF
		{"(?m)^abc$", "abc\r\nabc"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// HIGH PRIORITY: FindAll Iteration Semantics
// =============================================================================

func TestFindAllIterationSemantics(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Non-greedy with empty match possible
		{"abc|.*?", "abczzz"},
		{"abc|.*?", "abczabc"},
		{".*?", "abc"},
		{".*?", ""},
		// Greedy patterns
		{".*", "abc"},
		{".*", ""},
		{".+", "abc"},
		{".?", "abc"},
		// Empty match at each position
		{"a*", "bbb"},
		{"a?", "bbb"},
		{"(?:a*)?", "bbb"},
		// Overlapping possibility
		{"aa?", "aaa"},
		{"a+?", "aaa"},
		// Anchor with iteration
		{"^a", "aa"},
		{"a$", "aa"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// MEDIUM PRIORITY: Capture Group Zero Quantifier
// =============================================================================

func TestCaptureGroupZeroQuantifier(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// {0} makes group never participate
		{"(a){0}", ""},
		{"(a){0}", "a"},
		{"(a){0}b", "b"},
		{"(a){0}(b)", "b"},
		{"(a)(b){0}(c)", "ac"},
		// {0,0} is same as {0}
		{"(a){0,0}", "a"},
		{"(a){0,0}b", "b"},
		// Complex nesting with {0}
		{"(a)(((b))){0}c", "ac"},
		// Optional group that doesn't match
		{"(a)?(b)", "b"},
		{"(a)*(b)", "b"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			re := regexp.MustCompile(tt.pattern)
			expected := re.FindAllStringSubmatchIndex(tt.input, -1)

			cre := MustCompile(tt.pattern)
			got := cre.FindAllStringSubmatchIndex(tt.input, -1)

			if !reflect.DeepEqual(got, expected) {
				t.Errorf("FindAllStringSubmatchIndex mismatch:\n  got:  %v\n  want: %v",
					got, expected)
			}
		})
	}
}

// =============================================================================
// MEDIUM PRIORITY: Word Boundary Corner Cases
// =============================================================================

func TestWordBoundaryCornerCases(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Double boundaries
		{`\b\b`, ""},
		{`\b\b`, "a"},
		{`\b\b`, "ab"},
		{`\b\b`, " "},
		{`\B\B`, ""},
		{`\B\B`, "x"},
		{`\B\B`, "xx"},
		{`\B\B`, " "},
		// Boundary with anchors
		{`^\b$`, ""},
		{`^\b$`, "x"},
		{`^\B$`, ""},
		{`^\B$`, "x"},
		{`\b^`, "ab"},
		{`$\b`, "ab"},
		{`^\b`, "ab"},
		{`\b$`, "ab"},
		// Boundary after non-word at start
		{`^\B`, ""},
		{`^\B`, "x"},
		{`^\B`, " x"},
		{`^\B`, "  "},
		// Boundary patterns with text
		{`\bword\b`, "a word here"},
		{`\bword\b`, "word"},
		{`\bword\b`, "wording"},
		{`\Bword\B`, "swordfish"},
		{`\Bword\B`, "word"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// MEDIUM PRIORITY: Alternation with Anchors
// =============================================================================

func TestAlternationWithAnchors(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Partial anchor in alternation
		{"^a|b", "ba"},
		{"^a|b", "ab"},
		{"^a|b", "b"},
		{"^a|z", "yyyyya"},
		{"a$|z", "ayyyyy"},
		{"a$|z", "za"},
		{"ab?|$", "az"},
		{"ab?|$", ""},
		// Dollar in capture followed by literal
		{"(a$)b$", "ab"},
		{"(a$)|b$", "ab"},
		{"(a$)|b$", "b"},
		// Anchored alternation with different lengths
		{"^(a|ab)$", "a"},
		{"^(a|ab)$", "ab"},
		{"^(ab|a)$", "a"},
		{"^(ab|a)$", "ab"},
		// Complex alternation with anchors
		{"^a|^b", "ab"},
		{"a$|b$", "ab"},
		{"^a$|^b$", "a"},
		{"^a$|^b$", "b"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// LOW PRIORITY: Rust Regex Regressions
// =============================================================================

func TestRustRegressions(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		// https://github.com/rust-lang/regex/issues/98
		{"many-repeat", "^.{1,100}", "a"},
		// https://github.com/rust-lang/regex/issues/169
		{"leftmost-first-prefix", "z*azb", "azb"},
		// https://github.com/rust-lang/regex/issues/191
		{"many-alternates", "1|2|3|4|5|6|7|8|9|10|int", "int"},
		// https://github.com/rust-lang/regex/issues/268
		{"partial-anchor", "^a|b", "ba"},
		// https://github.com/rust-lang/regex/issues/579
		{"word-boundary-weird", `\b..\b`, "I have 12, he has 2!"},
		// Timestamp pattern (common real-world)
		{"timestamp", `(?:([0-9][0-9][0-9]):)?([0-9][0-9]):([0-9][0-9])`, "102:12:39"},
		{"timestamp-short", `([0-9][0-9]):([0-9][0-9])`, "12:39"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// REGRESSION TESTS: Previously Fixed Bugs
// =============================================================================

func TestPreviouslyFixedBugs(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		// Issue #10: ^ anchor not working
		{"issue10-caret", "^abc", "abc"},
		{"issue10-caret-no-match", "^abc", "xabc"},
		// Issue #12: Word boundaries
		{"issue12-word-boundary", `\bword\b`, "a word here"},
		{"issue12-non-boundary", `\Bword`, "swordfish"},
		// Issue #14: ^ in FindAllIndex
		{"issue14-caret-findall", "^", "12345"},
		{"issue14-caret-group-findall", "(^)", "12345"},
		{"issue14-caret-dollar", "(^)|($)", "12345"},
		{"issue14-caret-or-char", "(^)|2", "12345"},
		// Issue #15: DFA.IsMatch with capture groups
		{"issue15-capture-email", `\w+@([[:alnum:]]+\.)+[[:alnum:]]+[[:blank:]]+`, "bleble@foo1.bh.pl       deny"},
		{"issue15-capture-simple", `(abc)+`, "abcabc"},
		{"issue15-nested-capture", `((ab)+c)+`, "ababcababc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// SPECIAL PATTERNS: Common Real-World Edge Cases
// =============================================================================

func TestRealWorldEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		// Log parsing
		{"log-level", `\b(DEBUG|INFO|WARN|ERROR)\b`, "[INFO] Starting application"},
		{"log-timestamp", `\d{4}-\d{2}-\d{2}`, "2025-12-07 10:30:00"},
		// Email-like patterns
		{"email-simple", `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, "test@example.com"},
		// URL patterns
		{"url-protocol", `https?://`, "https://example.com"},
		// IP address
		{"ip-address", `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`, "192.168.1.1"},
		// File extensions
		{"file-ext", `\.(txt|log|json)$`, "file.json"},
		// Whitespace handling
		{"trim-whitespace", `^\s+|\s+$`, "  hello world  "},
		{"split-whitespace", `\s+`, "hello   world"},
		// Quoted strings
		{"quoted-string", `"[^"]*"`, `say "hello" to "world"`},
		// Version numbers
		{"semver", `v?\d+\.\d+\.\d+`, "v1.2.3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// UNICODE EDGE CASES
// =============================================================================

func TestUnicodeEdgeCases(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Word boundary with unicode
		{`\bслово\b`, "это слово тут"},
		{`\b\w+\b`, "hello мир world"},
		// Character classes with unicode
		{`[а-я]+`, "привет мир"},
		{`[A-Za-zА-Яа-я]+`, "Hello Мир"},
		// Dot with unicode
		{`.+`, "hello мир"},
		{`.{3}`, "абв"},
		// Case insensitive with unicode
		{"(?i)hello", "HELLO"},
		{"(?i)привет", "ПРИВЕТ"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// GREEDY VS NON-GREEDY
// =============================================================================

func TestGreedyVsNonGreedy(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Basic greedy
		{"a.*b", "aXXbYYb"},
		{"a.+b", "aXXbYYb"},
		{"a.?b", "aXb"},
		// Non-greedy
		{"a.*?b", "aXXbYYb"},
		{"a.+?b", "aXXbYYb"},
		{"a.??b", "aXb"},
		// Repetition bounds
		{"a{2,4}", "aaaaa"},
		{"a{2,4}?", "aaaaa"},
		// With groups
		{"(a+)(a+)", "aaaa"},
		{"(a+?)(a+?)", "aaaa"},
		{"(a+)(a+?)", "aaaa"},
		{"(a+?)(a+)", "aaaa"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}

// =============================================================================
// BOUNDARY CONDITIONS
// =============================================================================

func TestBoundaryConditions(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Empty input
		{"a*", ""},
		{"a+", ""},
		{"a?", ""},
		{"^$", ""},
		{"^.*$", ""},
		// Single character input
		{"a*", "a"},
		{"a+", "a"},
		{"a?", "a"},
		{"^a$", "a"},
		// Pattern longer than input
		{"abcdef", "abc"},
		// Input longer than reasonable pattern match
		{"a", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		// Many anchors
		{"^^^^abc$$$$", "abc"},
		{"^+abc$+", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			compareWithStdlib(t, tt.pattern, tt.input)
		})
	}
}
