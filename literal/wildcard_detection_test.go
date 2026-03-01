package literal

import (
	"regexp/syntax"
	"testing"
)

// TestIsWildcardOrRepetition tests the isWildcardOrRepetition function which
// checks whether a regex sub-expression represents variable-length matching.
// This function is critical for ExtractInnerForReverseSearch to determine
// whether wildcards exist before/after inner literals.
func TestIsWildcardOrRepetition(t *testing.T) {
	t.Run("direct wildcard ops", func(t *testing.T) {
		tests := []struct {
			name string
			op   syntax.Op
			want bool
		}{
			{"OpStar is wildcard", syntax.OpStar, true},
			{"OpPlus is wildcard", syntax.OpPlus, true},
			{"OpQuest is wildcard", syntax.OpQuest, true},
			{"OpRepeat is wildcard", syntax.OpRepeat, true},
			{"OpAnyChar is wildcard", syntax.OpAnyChar, true},
			{"OpAnyCharNotNL is wildcard", syntax.OpAnyCharNotNL, true},
			{"OpLiteral is not wildcard", syntax.OpLiteral, false},
			{"OpCharClass is not wildcard", syntax.OpCharClass, false},
			{"OpBeginLine is not wildcard", syntax.OpBeginLine, false},
			{"OpEndLine is not wildcard", syntax.OpEndLine, false},
			{"OpEmptyMatch is not wildcard", syntax.OpEmptyMatch, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				re := &syntax.Regexp{Op: tt.op}
				got := isWildcardOrRepetition(re)
				if got != tt.want {
					t.Errorf("isWildcardOrRepetition(Op=%v) = %v, want %v", tt.op, got, tt.want)
				}
			})
		}
	})

	t.Run("OpConcat with wildcard sub", func(t *testing.T) {
		// Concat containing a wildcard sub-expression
		re := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("abc")},
				{Op: syntax.OpStar},
			},
		}
		if !isWildcardOrRepetition(re) {
			t.Error("Expected OpConcat with OpStar sub to be wildcard")
		}
	})

	t.Run("OpConcat without wildcard sub", func(t *testing.T) {
		// Concat of only literals
		re := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("abc")},
				{Op: syntax.OpLiteral, Rune: []rune("def")},
			},
		}
		if isWildcardOrRepetition(re) {
			t.Error("Expected OpConcat with only literals to not be wildcard")
		}
	})

	t.Run("OpConcat with empty sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpConcat,
			Sub: []*syntax.Regexp{},
		}
		if isWildcardOrRepetition(re) {
			t.Error("Expected empty OpConcat to not be wildcard")
		}
	})

	t.Run("OpAlternate with wildcard sub", func(t *testing.T) {
		// Alternation where one branch contains a wildcard
		re := &syntax.Regexp{
			Op: syntax.OpAlternate,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("abc")},
				{Op: syntax.OpAnyChar},
			},
		}
		if !isWildcardOrRepetition(re) {
			t.Error("Expected OpAlternate with OpAnyChar sub to be wildcard")
		}
	})

	t.Run("OpAlternate without wildcard sub", func(t *testing.T) {
		// Alternation of only literals
		re := &syntax.Regexp{
			Op: syntax.OpAlternate,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("abc")},
				{Op: syntax.OpLiteral, Rune: []rune("def")},
			},
		}
		if isWildcardOrRepetition(re) {
			t.Error("Expected OpAlternate with only literals to not be wildcard")
		}
	})

	t.Run("OpAlternate with empty sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpAlternate,
			Sub: []*syntax.Regexp{},
		}
		if isWildcardOrRepetition(re) {
			t.Error("Expected empty OpAlternate to not be wildcard")
		}
	})

	t.Run("OpCapture with wildcard content", func(t *testing.T) {
		re := &syntax.Regexp{
			Op: syntax.OpCapture,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpStar},
			},
		}
		if !isWildcardOrRepetition(re) {
			t.Error("Expected OpCapture wrapping OpStar to be wildcard")
		}
	})

	t.Run("OpCapture with non-wildcard content", func(t *testing.T) {
		re := &syntax.Regexp{
			Op: syntax.OpCapture,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("abc")},
			},
		}
		if isWildcardOrRepetition(re) {
			t.Error("Expected OpCapture wrapping literal to not be wildcard")
		}
	})

	t.Run("OpCapture with empty sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{},
		}
		if isWildcardOrRepetition(re) {
			t.Error("Expected OpCapture with no sub to not be wildcard")
		}
	})

	t.Run("nested concat with deep wildcard", func(t *testing.T) {
		// OpConcat -> OpConcat -> OpStar
		re := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{
					Op: syntax.OpConcat,
					Sub: []*syntax.Regexp{
						{Op: syntax.OpLiteral, Rune: []rune("x")},
						{Op: syntax.OpPlus},
					},
				},
			},
		}
		if !isWildcardOrRepetition(re) {
			t.Error("Expected nested concat with deep wildcard to be detected")
		}
	})

	t.Run("alternation with nested capture containing wildcard", func(t *testing.T) {
		// OpAlternate -> OpCapture -> OpAnyCharNotNL
		re := &syntax.Regexp{
			Op: syntax.OpAlternate,
			Sub: []*syntax.Regexp{
				{
					Op: syntax.OpCapture,
					Sub: []*syntax.Regexp{
						{Op: syntax.OpAnyCharNotNL},
					},
				},
			},
		}
		if !isWildcardOrRepetition(re) {
			t.Error("Expected alternation with capture(AnyCharNotNL) to be wildcard")
		}
	})
}

// TestIsWildcardOrRepetitionFromParsedPatterns tests with real parsed patterns
// to validate behavior on actual regex ASTs.
func TestIsWildcardOrRepetitionFromParsedPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"dot star", ".*", true},
		{"dot plus", ".+", true},
		{"dot quest", ".?", true},
		{"plain literal", "abc", false},
		{"char class", "[abc]", false},
		{"repeat count", "a{3,5}", true},
		{"anchor begin", "^", false},
		{"anchor end", "$", false},
		{"capture with star", "(a*)", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			got := isWildcardOrRepetition(re)
			if got != tt.want {
				t.Errorf("isWildcardOrRepetition(%q) = %v, want %v (Op=%v)",
					tt.pattern, got, tt.want, re.Op)
			}
		})
	}
}

// TestExtractInnerOpAlternate verifies inner literal extraction from alternation
// patterns. The extractInner function should return the union of all alternatives,
// or empty if any alternative has no inner literal.
func TestExtractInnerOpAlternate(t *testing.T) {
	extractor := New(DefaultConfig())

	tests := []struct {
		name     string
		pattern  string
		expected []string
		isEmpty  bool
	}{
		{
			// Go parser factors bar|baz into ba[rz], so we get 2 inner literals
			name:     "alternation of literals",
			pattern:  "(foo|bar|baz)",
			expected: []string{"foo", "ba"},
		},
		{
			name:    "alternation with wildcard branch",
			pattern: "(foo|.*)",
			isEmpty: true, // .* branch has no inner literal
		},
		{
			name:     "alternation inside concat",
			pattern:  ".*(foo|bar).*",
			expected: []string{"foo", "bar"},
		},
		{
			name:    "alternation with empty branch",
			pattern: "(foo|)",
			isEmpty: true, // empty branch has no inner literal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			seq := extractor.ExtractInner(re)

			if tt.isEmpty {
				if !seq.IsEmpty() {
					t.Errorf("Expected empty seq, got %d literals", seq.Len())
					for i := 0; i < seq.Len(); i++ {
						t.Logf("  [%d] %q", i, seq.Get(i).Bytes)
					}
				}
				return
			}

			if seq.IsEmpty() {
				t.Fatal("Expected non-empty seq")
			}

			if seq.Len() != len(tt.expected) {
				t.Errorf("Expected %d literals, got %d", len(tt.expected), seq.Len())
				for i := 0; i < seq.Len(); i++ {
					t.Logf("  [%d] %q", i, seq.Get(i).Bytes)
				}
				return
			}

			for i, exp := range tt.expected {
				got := string(seq.Get(i).Bytes)
				if got != exp {
					t.Errorf("Literal %d: expected %q, got %q", i, exp, got)
				}
			}
		})
	}
}

// TestExtractInnerOpCharClass verifies inner extraction from character class patterns.
func TestExtractInnerOpCharClass(t *testing.T) {
	extractor := New(DefaultConfig())

	tests := []struct {
		name     string
		pattern  string
		expected []string
		isEmpty  bool
	}{
		{
			name:     "small char class",
			pattern:  "[abc]",
			expected: []string{"a", "b", "c"},
		},
		{
			name:    "large char class exceeds MaxClassSize",
			pattern: "[a-z]",
			isEmpty: true,
		},
		{
			name:     "char class inside concat",
			pattern:  ".*[abc].*",
			expected: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			seq := extractor.ExtractInner(re)

			if tt.isEmpty {
				if !seq.IsEmpty() {
					t.Errorf("Expected empty seq, got %d literals", seq.Len())
				}
				return
			}

			if seq.Len() != len(tt.expected) {
				t.Errorf("Expected %d literals, got %d", len(tt.expected), seq.Len())
				return
			}

			for i, exp := range tt.expected {
				got := string(seq.Get(i).Bytes)
				if got != exp {
					t.Errorf("Literal %d: expected %q, got %q", i, exp, got)
				}
			}
		})
	}
}

// TestExtractInnerIncompleteness verifies that inner literals are always
// marked as incomplete (Complete=false), since inner literals are never
// sufficient for a full match.
func TestExtractInnerIncompleteness(t *testing.T) {
	extractor := New(DefaultConfig())

	re, err := syntax.Parse(".*hello.*", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractInner(re)
	if seq.IsEmpty() {
		t.Fatal("Expected non-empty seq")
	}

	for i := 0; i < seq.Len(); i++ {
		if seq.Get(i).Complete {
			t.Errorf("Inner literal %d %q should be incomplete",
				i, seq.Get(i).Bytes)
		}
	}
}

// TestExtractInnerCaseInsensitive verifies that case-insensitive patterns
// are skipped during inner literal extraction.
func TestExtractInnerCaseInsensitive(t *testing.T) {
	extractor := New(DefaultConfig())

	re, err := syntax.Parse("(?i)error", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractInner(re)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq for case-insensitive inner, got %d literals", seq.Len())
	}
}

// TestExtractInnerDepthLimit verifies that deep recursion is handled safely.
func TestExtractInnerDepthLimit(t *testing.T) {
	extractor := New(DefaultConfig())

	// Build a deeply nested pattern via captures
	pattern := "x"
	for i := 0; i < 150; i++ {
		pattern = "(" + pattern + ")"
	}

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractInner(re)
	// Should return empty due to recursion limit (depth > 100)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq due to depth limit, got %d literals", seq.Len())
	}
}

// TestExtractInnerWildcardOps verifies that wildcard/repetition ops return empty.
func TestExtractInnerWildcardOps(t *testing.T) {
	extractor := New(DefaultConfig())

	patterns := []string{".*", ".+", ".?", ".", "a*", "a+", "a?"}
	for _, pattern := range patterns {
		re, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			t.Fatalf("Parse %q failed: %v", pattern, err)
		}

		seq := extractor.ExtractInner(re)
		if !seq.IsEmpty() {
			t.Errorf("Expected empty seq for inner extraction of %q, got %d literals",
				pattern, seq.Len())
		}
	}
}

// TestExtractInnerAnchors verifies that anchors contribute no inner literals.
func TestExtractInnerAnchors(t *testing.T) {
	extractor := New(DefaultConfig())

	// Manually construct anchor nodes to test the extractInner switch branches directly
	anchorOps := []syntax.Op{
		syntax.OpBeginLine,
		syntax.OpBeginText,
		syntax.OpEndLine,
		syntax.OpEndText,
	}

	for _, op := range anchorOps {
		re := &syntax.Regexp{Op: op}
		seq := extractor.extractInner(re, 0)
		if !seq.IsEmpty() {
			t.Errorf("Expected empty seq for anchor Op=%v, got %d literals", op, seq.Len())
		}
	}
}

// TestExtractInnerCapture verifies that capture groups are unwrapped.
func TestExtractInnerCapture(t *testing.T) {
	extractor := New(DefaultConfig())

	t.Run("capture with literal", func(t *testing.T) {
		re, err := syntax.Parse("(hello)", syntax.Perl)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		seq := extractor.ExtractInner(re)
		if seq.IsEmpty() {
			t.Fatal("Expected non-empty seq for (hello)")
		}
		if string(seq.Get(0).Bytes) != "hello" {
			t.Errorf("Expected %q, got %q", "hello", seq.Get(0).Bytes)
		}
	})

	t.Run("capture with empty sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{},
		}
		seq := extractor.extractInner(re, 0)
		if !seq.IsEmpty() {
			t.Errorf("Expected empty seq for empty capture, got %d literals", seq.Len())
		}
	})
}

// TestExtractInnerConcat verifies inner extraction walks concat sub-expressions
// and returns the first non-empty literal found.
func TestExtractInnerConcat(t *testing.T) {
	extractor := New(DefaultConfig())

	tests := []struct {
		name     string
		pattern  string
		expected string
	}{
		{
			name:     "concat with leading literal",
			pattern:  "hello.*world",
			expected: "hello",
		},
		{
			name:     "concat with wildcard then literal",
			pattern:  ".*world",
			expected: "world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			seq := extractor.ExtractInner(re)
			if seq.IsEmpty() {
				t.Fatal("Expected non-empty inner seq")
			}

			got := string(seq.Get(0).Bytes)
			if got != tt.expected {
				t.Errorf("Expected inner literal %q, got %q", tt.expected, got)
			}
		})
	}
}

// TestExtractInnerMaxLiteralLen verifies that inner literals are truncated
// when they exceed MaxLiteralLen.
func TestExtractInnerMaxLiteralLen(t *testing.T) {
	config := DefaultConfig()
	config.MaxLiteralLen = 3
	extractor := New(config)

	re, err := syntax.Parse("abcdef", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractInner(re)
	if seq.IsEmpty() {
		t.Fatal("Expected non-empty seq")
	}

	lit := seq.Get(0)
	if len(lit.Bytes) > 3 {
		t.Errorf("Expected inner literal truncated to 3 bytes, got %d: %q",
			len(lit.Bytes), lit.Bytes)
	}
}

// TestBuildSuffixASTEdgeCases tests buildSuffixAST with edge cases
// to cover the remaining <= 0 branch and single-element branch.
func TestBuildSuffixASTEdgeCases(t *testing.T) {
	t.Run("splitIdx at end returns empty match", func(t *testing.T) {
		// Build a concat with 3 elements, split at index 3 (past the end)
		concat := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("a")},
				{Op: syntax.OpLiteral, Rune: []rune("b")},
				{Op: syntax.OpLiteral, Rune: []rune("c")},
			},
		}

		suffix := buildSuffixAST(concat, 3)
		if suffix.Op != syntax.OpEmptyMatch {
			t.Errorf("Expected OpEmptyMatch for splitIdx=len(Sub), got Op=%v", suffix.Op)
		}
	})

	t.Run("splitIdx beyond end returns empty match", func(t *testing.T) {
		concat := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("a")},
			},
		}

		suffix := buildSuffixAST(concat, 5)
		if suffix.Op != syntax.OpEmptyMatch {
			t.Errorf("Expected OpEmptyMatch for splitIdx > len(Sub), got Op=%v", suffix.Op)
		}
	})

	t.Run("single remaining element returns clone of that element", func(t *testing.T) {
		concat := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("a")},
				{Op: syntax.OpLiteral, Rune: []rune("b")},
				{Op: syntax.OpLiteral, Rune: []rune("c")},
			},
		}

		// splitIdx=2, remaining=1 (only "c")
		suffix := buildSuffixAST(concat, 2)
		if suffix.Op != syntax.OpLiteral {
			t.Errorf("Expected OpLiteral for single remaining, got Op=%v", suffix.Op)
		}
		if string(suffix.Rune) != "c" {
			t.Errorf("Expected rune 'c', got %q", string(suffix.Rune))
		}
	})

	t.Run("multiple remaining elements returns new concat", func(t *testing.T) {
		concat := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("a")},
				{Op: syntax.OpLiteral, Rune: []rune("b")},
				{Op: syntax.OpLiteral, Rune: []rune("c")},
				{Op: syntax.OpLiteral, Rune: []rune("d")},
			},
		}

		// splitIdx=1, remaining=3 (b, c, d)
		suffix := buildSuffixAST(concat, 1)
		if suffix.Op != syntax.OpConcat {
			t.Errorf("Expected OpConcat, got Op=%v", suffix.Op)
		}
		if len(suffix.Sub) != 3 {
			t.Errorf("Expected 3 sub-expressions, got %d", len(suffix.Sub))
		}
	})
}

// TestBuildPrefixASTEdgeCases tests buildPrefixAST with edge cases.
func TestBuildPrefixASTEdgeCases(t *testing.T) {
	t.Run("splitIdx zero returns empty match", func(t *testing.T) {
		concat := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("a")},
			},
		}

		prefix := buildPrefixAST(concat, 0)
		if prefix.Op != syntax.OpEmptyMatch {
			t.Errorf("Expected OpEmptyMatch for splitIdx=0, got Op=%v", prefix.Op)
		}
	})

	t.Run("splitIdx one returns single element clone", func(t *testing.T) {
		concat := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune("a")},
				{Op: syntax.OpLiteral, Rune: []rune("b")},
			},
		}

		prefix := buildPrefixAST(concat, 1)
		if prefix.Op != syntax.OpLiteral {
			t.Errorf("Expected OpLiteral, got Op=%v", prefix.Op)
		}
		if string(prefix.Rune) != "a" {
			t.Errorf("Expected rune 'a', got %q", string(prefix.Rune))
		}
	})
}

// TestExtractSuffixesCaseInsensitive verifies that case-insensitive suffix patterns
// are skipped.
func TestExtractSuffixesCaseInsensitive(t *testing.T) {
	extractor := New(DefaultConfig())

	re, err := syntax.Parse("(?i)world", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractSuffixes(re)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq for case-insensitive suffix, got %d", seq.Len())
	}
}

// TestExtractSuffixesDepthLimit verifies that deeply nested patterns for suffix
// extraction respect the recursion depth limit.
func TestExtractSuffixesDepthLimit(t *testing.T) {
	extractor := New(DefaultConfig())

	pattern := "x"
	for i := 0; i < 150; i++ {
		pattern = "(" + pattern + ")"
	}

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractSuffixes(re)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq due to depth limit, got %d", seq.Len())
	}
}

// TestExtractSuffixesAnchors verifies suffix extraction with trailing anchors.
func TestExtractSuffixesAnchors(t *testing.T) {
	extractor := New(DefaultConfig())

	tests := []struct {
		name     string
		pattern  string
		expected []string
	}{
		{
			name:     "suffix with dollar anchor",
			pattern:  `\.txt$`,
			expected: []string{".txt"},
		},
		{
			name:     "alternation suffix with anchor",
			pattern:  `\.(txt|log|md)$`,
			expected: []string{".txt", ".log", ".md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			seq := extractor.ExtractSuffixes(re)
			if seq.Len() != len(tt.expected) {
				t.Errorf("Expected %d suffixes, got %d", len(tt.expected), seq.Len())
				for i := 0; i < seq.Len(); i++ {
					t.Logf("  [%d] %q", i, seq.Get(i).Bytes)
				}
				return
			}

			for i, exp := range tt.expected {
				got := string(seq.Get(i).Bytes)
				if got != exp {
					t.Errorf("Suffix %d: expected %q, got %q", i, exp, got)
				}
			}
		})
	}
}

// TestExtractSuffixesCapture verifies that capture groups are unwrapped
// during suffix extraction.
func TestExtractSuffixesCapture(t *testing.T) {
	extractor := New(DefaultConfig())

	t.Run("capture wrapping literal", func(t *testing.T) {
		re, err := syntax.Parse("(world)", syntax.Perl)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		seq := extractor.ExtractSuffixes(re)
		if seq.IsEmpty() {
			t.Fatal("Expected non-empty suffix seq for (world)")
		}
		if string(seq.Get(0).Bytes) != "world" {
			t.Errorf("Expected %q, got %q", "world", seq.Get(0).Bytes)
		}
	})

	t.Run("capture with empty sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{},
		}
		seq := extractor.extractSuffixes(re, 0)
		if !seq.IsEmpty() {
			t.Errorf("Expected empty seq for empty capture suffix")
		}
	})
}

// TestExtractSuffixesAnchorOnlyConcat verifies that a concat of only anchors
// returns empty during suffix extraction.
func TestExtractSuffixesAnchorOnlyConcat(t *testing.T) {
	extractor := New(DefaultConfig())

	// Construct: $\z (end anchors only)
	re := &syntax.Regexp{
		Op: syntax.OpConcat,
		Sub: []*syntax.Regexp{
			{Op: syntax.OpEndLine},
			{Op: syntax.OpEndText},
		},
	}

	seq := extractor.extractSuffixes(re, 0)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq for anchor-only concat suffix, got %d", seq.Len())
	}
}

// TestExtractSuffixesAlternateWithEmptyBranch verifies that an alternation
// where one branch has no suffix returns empty.
func TestExtractSuffixesAlternateWithEmptyBranch(t *testing.T) {
	extractor := New(DefaultConfig())

	// (world|.*) -- .* has no suffix
	re, err := syntax.Parse("(world|.*)", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractSuffixes(re)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq (one branch has no suffix), got %d", seq.Len())
	}
}

// TestCloneRegexpNil verifies that cloneRegexp handles nil input.
func TestCloneRegexpNil(t *testing.T) {
	result := cloneRegexp(nil)
	if result != nil {
		t.Errorf("Expected nil for cloneRegexp(nil), got %v", result)
	}
}

// TestExpandCharClassNotCharClass verifies expandCharClass returns empty for
// non-OpCharClass nodes.
func TestExpandCharClassNotCharClass(t *testing.T) {
	extractor := New(DefaultConfig())

	re := &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune("abc")}
	seq := extractor.expandCharClass(re)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq for non-OpCharClass, got %d literals", seq.Len())
	}
}
