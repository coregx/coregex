package nfa

import (
	"regexp/syntax"
	"testing"
)

func parseRegexp(t *testing.T, pattern string) *syntax.Regexp {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("failed to parse pattern %q: %v", pattern, err)
	}
	return re
}

func TestContainsDot(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		// Patterns containing dot (any char)
		{name: "bare dot", pattern: ".", want: true},
		{name: "dot star", pattern: ".*", want: true},
		{name: "dot plus", pattern: ".+", want: true},
		{name: "dot in concat", pattern: "a.b", want: true},
		{name: "dot in alternation left", pattern: ".|b", want: true},
		{name: "dot in alternation right", pattern: "a|.", want: true},
		{name: "dot in capture", pattern: "(.*)", want: true},
		{name: "dot in repeat", pattern: ".{2,5}", want: true},
		{name: "dot in quest", pattern: ".?", want: true},
		{name: "dotall flag dot", pattern: "(?s).", want: true},
		{name: "dot in nested group", pattern: "(a(b.)c)", want: true},
		{name: "dot in complex pattern", pattern: `foo.*bar`, want: true},
		{name: "dot in star nested", pattern: `(a.)+`, want: true},

		// Patterns without dot
		{name: "literal only", pattern: "abc", want: false},
		{name: "char class", pattern: "[a-z]", want: false},
		{name: "char class range", pattern: "[a-zA-Z0-9]", want: false},
		{name: "escaped dot", pattern: `\.`, want: false},
		{name: "digit class", pattern: `\d+`, want: false},
		{name: "word class", pattern: `\w+`, want: false},
		{name: "alternation no dot", pattern: "foo|bar", want: false},
		{name: "anchors only", pattern: "^abc$", want: false},
		{name: "empty pattern", pattern: "", want: false},
		{name: "quantifier no dot", pattern: "a{2,4}", want: false},
		{name: "concat no dot", pattern: "hello", want: false},
		{name: "capture no dot", pattern: "(abc)", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := parseRegexp(t, tt.pattern)
			got := ContainsDot(re)
			if got != tt.want {
				t.Errorf("ContainsDot(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestPatternHasUTF8Dependence(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		// ASCII-only patterns (no UTF-8 dependence)
		{name: "simple literal", pattern: "abc", want: false},
		{name: "ascii char class", pattern: "[a-z]", want: false},
		{name: "digit class", pattern: `\d+`, want: false},
		{name: "dot", pattern: ".", want: false},
		{name: "dot star", pattern: ".*", want: false},
		{name: "alternation ascii", pattern: "foo|bar", want: false},
		{name: "anchors", pattern: "^abc$", want: false},
		{name: "empty", pattern: "", want: false},
		{name: "word boundary", pattern: `\bword\b`, want: false},
		{name: "non-word boundary", pattern: `\Bword\B`, want: false},
		{name: "ascii range in concat", pattern: "[a-z][0-9]", want: false},
		{name: "quantifiers", pattern: "a{2,4}", want: false},
		{name: "capture ascii", pattern: "(abc)", want: false},
		{name: "quest ascii", pattern: "[a-z]?", want: false},

		// Patterns with Unicode char classes (UTF-8 dependence)
		{name: "unicode char range", pattern: `[\x80-\xff]`, want: true},
		{name: "unicode in alternation", pattern: `abc|[\x80-\xff]`, want: true},
		{name: "unicode in concat", pattern: `a[\x80-\xff]b`, want: true},
		{name: "unicode in star", pattern: `[\x80-\xff]*`, want: true},
		{name: "unicode in plus", pattern: `[\x80-\xff]+`, want: true},
		{name: "unicode in quest", pattern: `[\x80-\xff]?`, want: true},
		{name: "unicode in repeat", pattern: `[\x80-\xff]{2}`, want: true},
		{name: "unicode in capture", pattern: `([\x80-\xff])`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := parseRegexp(t, tt.pattern)
			got := PatternHasUTF8Dependence(re)
			if got != tt.want {
				t.Errorf("PatternHasUTF8Dependence(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}
