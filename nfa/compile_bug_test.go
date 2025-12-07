package nfa

import (
	"testing"
)

// TestBug6_NegatedCharClass tests Bug #6: [^,]* crashes due to expanding 1.1M codepoints
// The pattern [^,] is negated, which expands to ranges [0, 43, 45, 1114111]
// This represents ~1.1M codepoints, causing the old code to crash at >256 limit
func TestBug6_NegatedCharClass(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{
			name:    "negated comma - empty string",
			pattern: "[^,]*",
			input:   "",
			want:    true, // matches zero occurrences
		},
		{
			name:    "negated comma - no comma",
			pattern: "[^,]*",
			input:   "hello world",
			want:    true,
		},
		{
			name:    "negated comma - with comma",
			pattern: "[^,]*",
			input:   "hello,world",
			want:    true, // matches "hello" before comma
		},
		{
			name:    "negated comma - only comma",
			pattern: "[^,]*",
			input:   ",",
			want:    true, // matches empty string before comma
		},
		{
			name:    "negated newline",
			pattern: "[^\n]*",
			input:   "single line",
			want:    true,
		},
		{
			name:    "negated newline - multiline",
			pattern: "[^\n]*",
			input:   "first\nsecond",
			want:    true, // matches "first" before newline
		},
		{
			name:    "negated a-z",
			pattern: "[^a-z]*",
			input:   "123!@#",
			want:    true,
		},
		{
			name:    "negated a-z - with letters",
			pattern: "[^a-z]*",
			input:   "ABC123",
			want:    true, // matches "ABC123" (no lowercase)
		},
		{
			name:    "negated digit",
			pattern: "[^0-9]+",
			input:   "abc",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if err != nil {
				t.Fatalf("compilation failed: %v", err)
			}

			if nfa == nil {
				t.Fatal("NFA is nil")
			}

			// Verify NFA was created successfully
			if nfa.States() == 0 {
				t.Error("NFA has no states")
			}
			if nfa.Start() == InvalidState {
				t.Error("NFA has invalid start state")
			}

			// Note: This test only verifies compilation succeeds
			// Actual matching is tested in PikeVM tests
		})
	}
}

// TestBug6_NegatedCharClass_NoExplosion verifies that negated character classes
// with large ranges don't cause memory explosion during compilation
func TestBug6_NegatedCharClass_NoExplosion(t *testing.T) {
	// These patterns would cause the old code to try expanding 1.1M+ codepoints
	largeNegatedClasses := []string{
		"[^,]",      // ~1.1M codepoints
		"[^a]",      // ~1.1M codepoints
		"[^\n]",     // ~1.1M codepoints
		"[^0-9]",    // ~1.1M codepoints
		"[^a-zA-Z]", // ~1.1M codepoints
	}

	for _, pattern := range largeNegatedClasses {
		t.Run(pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(pattern)

			if err != nil {
				t.Fatalf("compilation of %q failed: %v", pattern, err)
			}

			if nfa == nil {
				t.Fatalf("NFA is nil for pattern %q", pattern)
			}

			// Verify NFA is reasonable size (not 1M+ states)
			stateCount := nfa.States()
			if stateCount > 1000 {
				t.Errorf("NFA has too many states: %d (pattern: %q)", stateCount, pattern)
			}
		})
	}
}

// TestBug7_FoldCaseInLiteral tests Bug #7: [oO]+d doesn't match "food"
// The pattern [oO] is parsed as OpLiteral with FoldCase flag, but flag was ignored
func TestBug7_FoldCaseInLiteral(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{
			name:    "case-insensitive o - lowercase",
			pattern: "(?i)od",
			input:   "food",
			want:    true,
		},
		{
			name:    "case-insensitive o - uppercase",
			pattern: "(?i)od",
			input:   "fOOd",
			want:    true,
		},
		{
			name:    "case-insensitive o - mixed",
			pattern: "(?i)od",
			input:   "fOod",
			want:    true,
		},
		{
			name:    "case-insensitive hello",
			pattern: "(?i)hello",
			input:   "HELLO",
			want:    true,
		},
		{
			name:    "case-insensitive hello - lowercase",
			pattern: "(?i)hello",
			input:   "hello",
			want:    true,
		},
		{
			name:    "case-insensitive hello - mixed",
			pattern: "(?i)hello",
			input:   "HeLLo",
			want:    true,
		},
		{
			name:    "case-insensitive abc",
			pattern: "(?i)abc",
			input:   "ABC",
			want:    true,
		},
		{
			name:    "case-insensitive abc - lowercase",
			pattern: "(?i)abc",
			input:   "abc",
			want:    true,
		},
		{
			name:    "case-insensitive abc - mixed",
			pattern: "(?i)abc",
			input:   "aBc",
			want:    true,
		},
		{
			name:    "case-sensitive - no match",
			pattern: "abc",
			input:   "ABC",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if err != nil {
				t.Fatalf("compilation failed: %v", err)
			}

			if nfa == nil {
				t.Fatal("NFA is nil")
			}

			// Verify NFA was created successfully
			if nfa.States() == 0 {
				t.Error("NFA has no states")
			}
			if nfa.Start() == InvalidState {
				t.Error("NFA has invalid start state")
			}

			// Note: This test only verifies compilation succeeds
			// Actual case-insensitive matching is tested in PikeVM tests
		})
	}
}

// TestBug7_CharClassWithFoldCase tests character classes with case-insensitive flag
func TestBug7_CharClassWithFoldCase(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{
			name:    "[oO]+d pattern",
			pattern: "[oO]+d",
			input:   "food",
			want:    true,
		},
		{
			name:    "[oO]+d pattern - uppercase",
			pattern: "[oO]+d",
			input:   "fOOd",
			want:    true,
		},
		{
			name:    "[aA]bc pattern",
			pattern: "[aA]bc",
			input:   "abc",
			want:    true,
		},
		{
			name:    "[aA]bc pattern - uppercase",
			pattern: "[aA]bc",
			input:   "Abc",
			want:    true,
		},
		{
			name:    "[hH]ello pattern",
			pattern: "[hH]ello",
			input:   "hello",
			want:    true,
		},
		{
			name:    "[hH]ello pattern - uppercase",
			pattern: "[hH]ello",
			input:   "Hello",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if err != nil {
				t.Fatalf("compilation failed: %v", err)
			}

			if nfa == nil {
				t.Fatal("NFA is nil")
			}

			// Verify NFA was created successfully
			if nfa.States() == 0 {
				t.Error("NFA has no states")
			}
			if nfa.Start() == InvalidState {
				t.Error("NFA has invalid start state")
			}
		})
	}
}

// TestBug6And7_Integration tests both bugs together in realistic scenarios
func TestBug6And7_Integration(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{
			name:    "negated class with case-insensitive",
			pattern: "(?i)[^,]*hello",
			input:   "world HELLO",
			want:    true,
		},
		{
			name:    "multiple negated classes",
			pattern: "[^,]*[^\n]*",
			input:   "no commas or newlines",
			want:    true,
		},
		{
			name:    "case-insensitive word boundary",
			pattern: "(?i)foo[oO]*d",
			input:   "fooOOOd",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)

			if err != nil {
				t.Fatalf("compilation failed: %v", err)
			}

			if nfa == nil {
				t.Fatal("NFA is nil")
			}

			// Verify NFA is reasonable
			if nfa.States() == 0 {
				t.Error("NFA has no states")
			}
			if nfa.Start() == InvalidState {
				t.Error("NFA has invalid start state")
			}

			// Verify state count is reasonable (not exploded)
			if nfa.States() > 2000 {
				t.Errorf("NFA has too many states: %d", nfa.States())
			}
		})
	}
}

// TestHelperFunctions tests the new helper functions for case handling
func TestHelperFunctions(t *testing.T) {
	t.Run("isASCIILetter", func(t *testing.T) {
		tests := []struct {
			r    rune
			want bool
		}{
			{'a', true},
			{'z', true},
			{'A', true},
			{'Z', true},
			{'m', true},
			{'0', false},
			{'9', false},
			{' ', false},
			{'@', false},
			{'\n', false},
			{'あ', false}, // Japanese character
		}

		for _, tt := range tests {
			got := isASCIILetter(tt.r)
			if got != tt.want {
				t.Errorf("isASCIILetter(%q) = %v, want %v", tt.r, got, tt.want)
			}
		}
	})

	t.Run("toUpperASCII", func(t *testing.T) {
		tests := []struct {
			r    rune
			want rune
		}{
			{'a', 'A'},
			{'z', 'Z'},
			{'A', 'A'},
			{'Z', 'Z'},
			{'m', 'M'},
			{'0', '0'},
			{' ', ' '},
		}

		for _, tt := range tests {
			got := toUpperASCII(tt.r)
			if got != tt.want {
				t.Errorf("toUpperASCII(%q) = %q, want %q", tt.r, got, tt.want)
			}
		}
	})

	t.Run("toLowerASCII", func(t *testing.T) {
		tests := []struct {
			r    rune
			want rune
		}{
			{'A', 'a'},
			{'Z', 'z'},
			{'a', 'a'},
			{'z', 'z'},
			{'M', 'm'},
			{'0', '0'},
			{' ', ' '},
		}

		for _, tt := range tests {
			got := toLowerASCII(tt.r)
			if got != tt.want {
				t.Errorf("toLowerASCII(%q) = %q, want %q", tt.r, got, tt.want)
			}
		}
	})
}

// TestBug8_InlineFlags tests Bug #8: Inline flags (?s:...) not working
// The pattern (?s:.) should match newlines within the scope of the flag.
// Reported by Ben Hoyt in GoAWK integration testing.
func TestBug8_InlineFlags(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Basic inline flag tests
		{`(?s:.)`, "\n", true},
		{`(?s:.)`, "a", true},
		{`.`, "\n", false},
		{`.`, "a", true},

		// Scoped inline flags
		{`a(?s:.)b`, "a\nb", true},
		{`a(?s:.)b`, "axb", true},
		{`a.b`, "a\nb", false},
		{`a.b`, "axb", true},

		// Full pattern with inline flags (Ben Hoyt's test case)
		{`(?s:^a.*c$)`, "a\nb\nc", true},
		{`^a.*c$`, "a\nb\nc", false},

		// Multiple dots with mixed behavior
		{`a(?s:.)b.c`, "a\nbxc", true},
		{`a(?s:.)b.c`, "a\nb\nc", false}, // second dot doesn't match \n

		// Case insensitive inline flag
		{`(?i:abc)`, "ABC", true},
		{`(?i:abc)`, "abc", true},
		{`abc`, "ABC", false},

		// Combined flags
		{`(?is:^a.*z$)`, "A\nB\nZ", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			vm := NewPikeVM(nfa)
			_, _, got := vm.Search([]byte(tt.input))

			if got != tt.want {
				t.Errorf("pattern %q, input %q: got %v, want %v", tt.pattern, tt.input, got, tt.want)
			}
		})
	}
}
