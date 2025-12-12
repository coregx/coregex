package meta

import (
	"regexp/syntax"
	"testing"
)

// TestIsSimpleCharClass verifies that isSimpleCharClass correctly identifies
// patterns that can use BoundedBacktracker for improved performance.
func TestIsSimpleCharClass(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
		desc    string
	}{
		// Basic character classes
		{"[abc]", true, "simple char class"},
		{"[0-9]", true, "digit range"},
		{"[a-zA-Z]", true, "letter ranges"},

		// Repeated character classes
		{"[abc]+", true, "plus quantifier"},
		{"[abc]*", true, "star quantifier"},
		{"[abc]?", true, "quest quantifier"},
		{"[abc]{2,5}", true, "bounded repeat"},

		// With capture groups - the key optimization
		{"(a|b|c)", true, "alternation as capture (Go optimizes to CharClass)"},
		{"(a|b|c)+", true, "repeated alternation with capture"},
		{"([0-9])+", true, "digit class with capture"},
		{"([a-z])*", true, "letter class with capture"},

		// Non-capturing groups
		{"(?:a|b|c)+", true, "non-capturing alternation (no OpCapture in AST)"},

		// Concatenations of char classes
		{"[a-z][0-9]", true, "concat of two classes"},
		{"[a-z]+[0-9]+", true, "concat of repeated classes"},

		// NOT simple char class patterns
		{"abc", false, "literal - not char class"},
		{"a.b", false, "contains wildcard"},
		{"a|bc", false, "alternation of different-length strings"},
		{"(foo|bar)", false, "alternation of multi-char literals"},
		{"[abc]d", false, "char class followed by literal"},
		{"a[bc]", false, "literal followed by char class"},
		{".*", false, "wildcard - not char class"},
		{"a+b+", false, "two different literals with quantifiers"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			got := isSimpleCharClass(re)
			if got != tt.want {
				t.Errorf("isSimpleCharClass(%q) = %v, want %v (%s)", tt.pattern, got, tt.want, tt.desc)
			}
		})
	}
}

// TestCaptureGroupStrategySelection verifies that patterns with capture groups
// around character classes correctly select UseBoundedBacktracker.
func TestCaptureGroupStrategySelection(t *testing.T) {
	tests := []struct {
		pattern string
		want    Strategy
	}{
		// Should use BoundedBacktracker (3-7x faster than PikeVM)
		{"(a|b|c)+", UseBoundedBacktracker},
		{"([0-9])+", UseBoundedBacktracker},
		{"([a-z])*", UseBoundedBacktracker},
		{"(\\d)+", UseBoundedBacktracker},

		// Without capture should also use BoundedBacktracker
		{"[abc]+", UseBoundedBacktracker},
		{"[0-9]+", UseBoundedBacktracker},

		// These should NOT use BoundedBacktracker (multi-char alternations)
		// Note: actual strategy depends on NFA size, but definitely not BoundedBacktracker
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("failed to compile %q: %v", tt.pattern, err)
			}

			if engine.Strategy() != tt.want {
				t.Errorf("pattern %q: got strategy %v, want %v",
					tt.pattern, engine.Strategy(), tt.want)
			}
		})
	}
}
