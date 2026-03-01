package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestIsMatchAdaptiveStrategy exercises the isMatchAdaptive path.
// Covers: ismatch.go isMatchAdaptive (44%)
func TestIsMatchAdaptiveStrategy(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		// Patterns that might route through adaptive/both
		{"complex_match", `(?:[a-z]{2,5})\d{3,6}`, "prefix abc1234 suffix", true},
		{"complex_no_match", `(?:[a-z]{2,5})\d{3,6}`, "no numbers at all", false},
		{"empty_input", `(?:[a-z]{2,5})\d{3,6}`, "", false},
		{"boundary_match", `(?:[a-z]{2,5})\d{3,6}`, "ab123", true},
		{"long_input_match", `(?:[a-z]{2,5})\d{3,6}`, strings.Repeat("x", 500) + "ab123" + strings.Repeat("y", 500), true},
		{"long_input_no_match", `(?:[a-z]{2,5})\d{3,6}`, strings.Repeat("x", 1000), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v (strategy=%d)", tt.input, got, tt.want, engine.strategy)
			}

			// Verify against stdlib
			re := regexp.MustCompile(tt.pattern)
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("stdlib mismatch: coregex=%v, stdlib=%v", got, want)
			}
		})
	}
}

// TestIsMatchAllStrategies exercises IsMatch for patterns that route through various strategies.
// This is a broad sweep to fill strategy-specific IsMatch branches.
// Covers: ismatch.go isMatchReverseSuffix, isMatchReverseSuffixSet, isMatchReverseInner,
//
//	isMatchReverseAnchored, isMatchMultilineReverseSuffix, isMatchDigitPrefilter,
//	isMatchAhoCorasick, isMatchTeddy
func TestIsMatchAllStrategies(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		// ReverseSuffix
		{"reverse_suffix_match", `.*\.xml`, "config.xml", true},
		{"reverse_suffix_no_match", `.*\.xml`, "config.json", false},

		// ReverseSuffixSet
		{"reverse_suffix_set_match", `.*\.(gif|png|jpg)`, "image.png", true},
		{"reverse_suffix_set_no_match", `.*\.(gif|png|jpg)`, "image.bmp", false},

		// ReverseInner
		{"reverse_inner_match", `.*server.*`, "web server running", true},
		{"reverse_inner_no_match", `.*server.*`, "nothing here", false},

		// ReverseAnchored
		{"reverse_anchored_match", `\w+\.go$`, "main.go", true},
		{"reverse_anchored_no_match", `\w+\.go$`, "main.go.bak", false},

		// MultilineReverseSuffix
		{"multiline_match", `(?m)^/.*\.asp`, "/test.asp\nother", true},
		{"multiline_no_match", `(?m)^/.*\.asp`, "test.asp", false},

		// DigitPrefilter
		{"digit_match", `\d{3}-\d{4}`, "call 555-1234", true},
		{"digit_no_match", `\d{3}-\d{4}`, "no digits here", false},

		// AhoCorasick
		{"aho_match", `one|two|three|four|five|six|seven|eight|nine|ten`, "the number seven", true},
		{"aho_no_match", `one|two|three|four|five|six|seven|eight|nine|ten`, "no match", false},

		// Teddy
		{"teddy_match", `cat|dog|bird`, "I have a dog", true},
		{"teddy_no_match", `cat|dog|bird`, "I have a fish", false},

		// BoundedBacktracker
		{"bounded_match", `(\w+)`, "hello", true},
		{"bounded_no_match", `(\w+)`, "   ", false},

		// CharClassSearcher
		{"charclass_match", `[a-z]+`, "hello", true},
		{"charclass_no_match", `[a-z]+`, "12345", false},

		// CompositeSearcher
		{"composite_match", `[a-z]+\d+`, "abc123", true},
		{"composite_no_match", `[a-z]+\d+`, "abc", false},

		// BranchDispatch
		{"branch_match", `^(?:GET|POST|PUT|DELETE)\b`, "GET /index", true},
		{"branch_no_match", `^(?:GET|POST|PUT|DELETE)\b`, "UNKNOWN /index", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v (strategy=%d)", tt.input, got, tt.want, engine.strategy)
			}
		})
	}
}

// TestIsMatchEmptyInputAllStrategies ensures no panics on empty input.
// Covers: all isMatch* methods empty input paths
func TestIsMatchEmptyInputAllStrategies(t *testing.T) {
	patterns := []string{
		`ab`,            // NFA
		`.*\.txt`,       // ReverseSuffix
		`.*\.(a|b|c)`,   // ReverseSuffixSet
		`.*inner.*`,     // ReverseInner
		`\w+$`,          // ReverseAnchored
		`(?m)^/.*\.php`, // MultilineReverseSuffix
		`\d+`,           // DigitPrefilter or BoundedBacktracker
		`(\w+)`,         // BoundedBacktracker
		`[a-z]+`,        // CharClassSearcher
		`[a-z]+\d+`,     // CompositeSearcher
		`foo|bar|baz`,   // Teddy
		`one|two|three|four|five|six|seven|eight|nine|ten`, // AhoCorasick
	}

	for _, pat := range patterns {
		t.Run(pat, func(t *testing.T) {
			engine, err := Compile(pat)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", pat, err)
			}

			// Should not panic
			got := engine.IsMatch([]byte(""))

			// Verify against stdlib
			re := regexp.MustCompile(pat)
			want := re.MatchString("")
			if got != want {
				t.Errorf("IsMatch(\"\") = %v, want %v (strategy=%d)", got, want, engine.strategy)
			}
		})
	}
}

// TestFindEmptyInputAllStrategies ensures no panics on empty input for Find.
// Covers: find.go findAtZero empty paths for all strategies
func TestFindEmptyInputAllStrategies(t *testing.T) {
	patterns := []string{
		`ab`,
		`.*\.txt`,
		`.*\.(a|b|c)`,
		`.*inner.*`,
		`\w+$`,
		`(?m)^/.*\.php`,
		`\d+`,
		`(\w+)`,
		`[a-z]+`,
		`[a-z]+\d+`,
		`foo|bar|baz`,
		`one|two|three|four|five|six|seven|eight|nine|ten`,
	}

	for _, pat := range patterns {
		t.Run(pat, func(t *testing.T) {
			engine, err := Compile(pat)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", pat, err)
			}

			// Should not panic
			match := engine.Find([]byte(""))

			// Verify against stdlib
			re := regexp.MustCompile(pat)
			stdMatch := re.Find([]byte(""))
			if (match == nil) != (stdMatch == nil) {
				t.Errorf("Find(\"\") nil mismatch: coregex=%v, stdlib=%v", match == nil, stdMatch == nil)
			}
		})
	}
}

// TestFindIndicesEmptyInputAllStrategies ensures no panics on empty input for FindIndices.
// Covers: find_indices.go FindIndices empty paths for all strategies
func TestFindIndicesEmptyInputAllStrategies(t *testing.T) {
	patterns := []string{
		`ab`,
		`.*\.txt`,
		`.*\.(a|b|c)`,
		`.*inner.*`,
		`\w+$`,
		`(?m)^/.*\.php`,
		`\d+`,
		`(\w+)`,
		`[a-z]+`,
		`[a-z]+\d+`,
		`foo|bar|baz`,
		`one|two|three|four|five|six|seven|eight|nine|ten`,
	}

	for _, pat := range patterns {
		t.Run(pat, func(t *testing.T) {
			engine, err := Compile(pat)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", pat, err)
			}

			// Should not panic
			_, _, found := engine.FindIndices([]byte(""))

			// Verify against stdlib
			re := regexp.MustCompile(pat)
			stdLoc := re.FindStringIndex("")
			stdFound := stdLoc != nil

			if found != stdFound {
				t.Errorf("FindIndices(\"\") mismatch: coregex found=%v, stdlib found=%v (strategy=%d)", found, stdFound, engine.strategy)
			}
		})
	}
}
