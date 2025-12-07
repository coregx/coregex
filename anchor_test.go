package coregex

import (
	"regexp"
	"testing"
)

// TestAnchorInFindAll tests that ^ anchor only matches at position 0
// This is a regression test for issue #14
func TestAnchorInFindAll(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{"^", "12345"},
		{"(^)", "12345"},
		{"(^)|($)", "12345"},
		{"($)|(^)", "12345"},
		{"^test", "test hello test"},
		{"^[a-z]+", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			// stdlib reference
			re := regexp.MustCompile(tt.pattern)
			expected := re.FindAllStringIndex(tt.input, -1)

			// coregex
			cre := MustCompile(tt.pattern)
			got := cre.FindAllStringIndex(tt.input, -1)

			// Compare
			if len(got) != len(expected) {
				t.Errorf("FindAllStringIndex(%q, %q): got %d matches, want %d matches\n  got: %v\n  want: %v",
					tt.pattern, tt.input, len(got), len(expected), got, expected)
				return
			}

			for i := range got {
				if got[i][0] != expected[i][0] || got[i][1] != expected[i][1] {
					t.Errorf("FindAllStringIndex(%q, %q)[%d]: got %v, want %v",
						tt.pattern, tt.input, i, got[i], expected[i])
				}
			}
		})
	}
}

// TestAnchorInReplaceAll tests that ^ anchor works correctly in ReplaceAll
func TestAnchorInReplaceAll(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		repl    string
	}{
		{"^", "12345", "x"},
		{"(^)", "12345", "x"},
		{"(^)|($)", "12345", "x"},
		{"^test", "test hello test", "START"},
		{"^[a-z]+", "hello world", "WORD"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			// stdlib reference
			re := regexp.MustCompile(tt.pattern)
			expected := re.ReplaceAllString(tt.input, tt.repl)

			// coregex
			cre := MustCompile(tt.pattern)
			got := cre.ReplaceAllString(tt.input, tt.repl)

			if got != expected {
				t.Errorf("ReplaceAllString(%q, %q, %q):\n  got:  %q\n  want: %q",
					tt.pattern, tt.input, tt.repl, got, expected)
			}
		})
	}
}

// TestAnchorWithCaptures tests ^ anchor with capture groups in ReplaceAll
func TestAnchorWithCaptures(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		repl    string
	}{
		{"(^)(.+)", "hello", "[$1$2]"},
		{"^([a-z]+)", "hello world", "WORD:$1"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			// stdlib reference
			re := regexp.MustCompile(tt.pattern)
			expected := re.ReplaceAllString(tt.input, tt.repl)

			// coregex
			cre := MustCompile(tt.pattern)
			got := cre.ReplaceAllString(tt.input, tt.repl)

			if got != expected {
				t.Errorf("ReplaceAllString(%q, %q, %q):\n  got:  %q\n  want: %q",
					tt.pattern, tt.input, tt.repl, got, expected)
			}
		})
	}
}
