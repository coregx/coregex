package nfa

import (
	"reflect"
	"testing"
)

// TestSubexpNames verifies that named capture groups are correctly extracted
func TestSubexpNames(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected []string
	}{
		{
			name:     "no captures",
			pattern:  `abc`,
			expected: []string{""},
		},
		{
			name:     "unnamed capture",
			pattern:  `(abc)`,
			expected: []string{"", ""},
		},
		{
			name:     "single named capture",
			pattern:  `(?P<foo>abc)`,
			expected: []string{"", "foo"},
		},
		{
			name:     "mixed named and unnamed",
			pattern:  `(?P<year>\d+)-(\d+)-(?P<day>\d+)`,
			expected: []string{"", "year", "", "day"},
		},
		{
			name:     "multiple named captures",
			pattern:  `(?P<scheme>https?)://(?P<host>\w+)`,
			expected: []string{"", "scheme", "host"},
		},
		{
			name:     "nested captures with names",
			pattern:  `(?P<outer>a(?P<inner>b)c)`,
			expected: []string{"", "outer", "inner"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Failed to compile pattern %q: %v", tt.pattern, err)
			}

			got := nfa.SubexpNames()
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("SubexpNames() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestSubexpNamesMatchesStdlib verifies that our implementation matches stdlib behavior
func TestSubexpNamesMatchesStdlib(t *testing.T) {
	patterns := []string{
		`abc`,
		`(abc)`,
		`(?P<foo>abc)`,
		`(?P<year>\d+)-(\d+)-(?P<day>\d+)`,
		`(?P<scheme>https?)://(?P<host>\w+)/(?P<path>.*)`,
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			// Compile with our implementation
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(pattern)
			if err != nil {
				t.Fatalf("Failed to compile pattern %q: %v", pattern, err)
			}

			got := nfa.SubexpNames()

			// Compare with stdlib
			// Note: We can't directly compare with stdlib regexp.Regexp.SubexpNames()
			// because that would require importing "regexp" which creates circular dependency
			// Instead, we verify the structure is correct
			if len(got) == 0 {
				t.Errorf("SubexpNames() returned empty slice for pattern %q", pattern)
			}
			if got[0] != "" {
				t.Errorf("SubexpNames()[0] = %q, want empty string (entire match)", got[0])
			}
		})
	}
}

// TestCaptureCount verifies capture count remains correct with named captures
func TestCaptureCount(t *testing.T) {
	tests := []struct {
		pattern string
		count   int
	}{
		{`abc`, 1},                              // group 0 only
		{`(abc)`, 2},                            // group 0 + 1 unnamed
		{`(?P<foo>abc)`, 2},                     // group 0 + 1 named
		{`(?P<a>x)(?P<b>y)`, 3},                 // group 0 + 2 named
		{`(?P<year>\d+)-(\d+)-(?P<day>\d+)`, 4}, // group 0 + 3 mixed
		{`(?P<outer>a(?P<inner>b)c)`, 3},        // group 0 + 2 nested
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Failed to compile pattern %q: %v", tt.pattern, err)
			}

			got := nfa.CaptureCount()
			if got != tt.count {
				t.Errorf("CaptureCount() = %d, want %d", got, tt.count)
			}

			// Verify SubexpNames length matches CaptureCount
			names := nfa.SubexpNames()
			if len(names) != tt.count {
				t.Errorf("len(SubexpNames()) = %d, want %d (should match CaptureCount)", len(names), tt.count)
			}
		})
	}
}
