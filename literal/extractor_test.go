package literal

import (
	"regexp/syntax"
	"testing"
)

// Helper function to parse regex and extract prefixes
func extractPrefixes(t *testing.T, pattern string) *Seq {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("Failed to parse regex %q: %v", pattern, err)
	}

	extractor := New(DefaultConfig())
	return extractor.ExtractPrefixes(re)
}

// Helper function to parse regex and extract suffixes
func extractSuffixes(t *testing.T, pattern string) *Seq {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("Failed to parse regex %q: %v", pattern, err)
	}

	extractor := New(DefaultConfig())
	return extractor.ExtractSuffixes(re)
}

// Helper function to parse regex and extract inner literals
func extractInner(t *testing.T, pattern string) *Seq {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("Failed to parse regex %q: %v", pattern, err)
	}

	extractor := New(DefaultConfig())
	return extractor.ExtractInner(re)
}

// Helper to check if sequence contains expected literals
func checkLiterals(t *testing.T, seq *Seq, expected []string) {
	t.Helper()
	if seq.Len() != len(expected) {
		t.Errorf("Expected %d literals, got %d", len(expected), seq.Len())
		for i := 0; i < seq.Len(); i++ {
			t.Logf("  Got: %q", string(seq.Get(i).Bytes))
		}
		return
	}

	for i, exp := range expected {
		if i >= seq.Len() {
			break
		}
		got := string(seq.Get(i).Bytes)
		if got != exp {
			t.Errorf("Literal %d: expected %q, got %q", i, exp, got)
		}
	}
}

// TestExtractPrefixesLiteral tests extraction from simple literal patterns
func TestExtractPrefixesLiteral(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
	}{
		{"hello", []string{"hello"}},
		{"foo", []string{"foo"}},
		{"a", []string{"a"}},
		{"test123", []string{"test123"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			seq := extractPrefixes(t, tt.pattern)
			checkLiterals(t, seq, tt.expected)
		})
	}
}

// TestExtractPrefixesConcat tests concatenation patterns
func TestExtractPrefixesConcat(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
	}{
		{"abc", []string{"abc"}},
		{"hello.*world", []string{"hello"}},
		{"prefix.*", []string{"prefix"}},
		// test[0-9] expands to all complete literals (better for Teddy prefilter)
		{"test[0-9]", []string{"test0", "test1", "test2", "test3", "test4", "test5", "test6", "test7", "test8", "test9"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			seq := extractPrefixes(t, tt.pattern)
			checkLiterals(t, seq, tt.expected)
		})
	}
}

// TestExtractPrefixesAlternate tests alternation patterns
func TestExtractPrefixesAlternate(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
	}{
		{"(foo|bar)", []string{"foo", "bar"}},
		{"(a|b|c)", []string{"a", "b", "c"}},
		{"(hello|world)", []string{"hello", "world"}},
		{"(x)", []string{"x"}}, // Single alternative (just a capture group)
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			seq := extractPrefixes(t, tt.pattern)
			checkLiterals(t, seq, tt.expected)
		})
	}
}

// TestExtractPrefixesCharClass tests character class patterns
func TestExtractPrefixesCharClass(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
		desc     string
	}{
		{"[abc]", []string{"a", "b", "c"}, "simple class"},
		{"[a-c]", []string{"a", "b", "c"}, "range class"},
		{"[xyz]test", []string{"xtest", "ytest", "ztest"}, "class + literal (cross-product)"},
		{"[0-9]", []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}, "class at limit (10 == 10 limit)"},
		{"[a-z]", []string{}, "class too large (26 > 10 limit)"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			seq := extractPrefixes(t, tt.pattern)
			if len(tt.expected) == 0 {
				if !seq.IsEmpty() {
					t.Errorf("Expected empty sequence for %q, got %d literals", tt.pattern, seq.Len())
				}
			} else {
				checkLiterals(t, seq, tt.expected)
			}
		})
	}
}

// TestExtractPrefixesCharClassConcat tests character class followed by literal
func TestExtractPrefixesCharClassConcat(t *testing.T) {
	// [abc]test should now produce full cross-product: ["atest", "btest", "ctest"]
	// because cross-product expansion walks through the entire concat chain
	pattern := "[abc]test"
	seq := extractPrefixes(t, pattern)

	expected := []string{"atest", "btest", "ctest"}
	checkLiterals(t, seq, expected)
}

// TestExtractPrefixesAnchors tests anchor patterns
func TestExtractPrefixesAnchors(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
		desc     string
	}{
		{"^foo", []string{"foo"}, "begin anchor"},
		{"^(hello|world)", []string{"hello", "world"}, "begin anchor + alternation"},
		{".*foo", []string{}, "wildcard prefix (no reliable prefix)"},
		{".+bar", []string{}, "wildcard prefix (no reliable prefix)"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			seq := extractPrefixes(t, tt.pattern)
			if len(tt.expected) == 0 {
				if !seq.IsEmpty() {
					t.Errorf("Expected empty sequence for %q, got %d literals", tt.pattern, seq.Len())
				}
			} else {
				checkLiterals(t, seq, tt.expected)
			}
		})
	}
}

// TestExtractPrefixesRepetition tests repetition operators
func TestExtractPrefixesRepetition(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
		desc     string
	}{
		{"a*bc", []string{}, "star makes prefix optional"},
		{"a?bc", []string{}, "quest makes prefix optional"},
		{"a+bc", []string{}, "plus (conservative: no prefix)"},
		{"(foo)*bar", []string{}, "star on group"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			seq := extractPrefixes(t, tt.pattern)
			if !seq.IsEmpty() {
				t.Errorf("Expected empty sequence for %q (repetition), got %d literals",
					tt.pattern, seq.Len())
			}
		})
	}
}

// TestExtractSuffixes tests suffix extraction
func TestExtractSuffixes(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
		desc     string
	}{
		{"world", []string{"world"}, "simple literal"},
		{"(foo|bar)", []string{"foo", "bar"}, "alternation"},
		{"test[xyz]", []string{"testx", "testy", "testz"}, "literal + char class"},
		{"hello.*world", []string{"world"}, "prefix + wildcard + suffix"},
		{"foo.*", []string{}, "wildcard suffix (no reliable suffix)"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			seq := extractSuffixes(t, tt.pattern)
			if len(tt.expected) == 0 {
				if !seq.IsEmpty() {
					t.Errorf("Expected empty sequence for %q, got %d literals", tt.pattern, seq.Len())
				}
			} else {
				checkLiterals(t, seq, tt.expected)
			}
		})
	}
}

// TestExtractSuffixesConcat tests suffix extraction from concatenations
func TestExtractSuffixesConcat(t *testing.T) {
	pattern := "prefix.*suffix"
	seq := extractSuffixes(t, pattern)
	expected := []string{"suffix"}
	checkLiterals(t, seq, expected)
}

// TestExtractInner tests inner literal extraction
func TestExtractInner(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
		desc     string
	}{
		{".*foo.*", []string{"foo"}, "inner literal"},
		{".*(hello|world).*", []string{"hello", "world"}, "inner alternation"},
		{"prefix.*middle.*suffix", []string{"prefix"}, "first literal found"},
		{".*test", []string{"test"}, "inner literal at end"},
		{"start.*", []string{"start"}, "inner literal at start"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			seq := extractInner(t, tt.pattern)
			if len(tt.expected) == 0 {
				if !seq.IsEmpty() {
					t.Errorf("Expected empty sequence for %q, got %d literals", tt.pattern, seq.Len())
				}
			} else {
				checkLiterals(t, seq, tt.expected)
			}
		})
	}
}

// TestExtractorConfig tests configuration limits
func TestExtractorConfig(t *testing.T) {
	t.Run("MaxLiterals limit", func(t *testing.T) {
		config := DefaultConfig()
		config.MaxLiterals = 2
		extractor := New(config)

		pattern := "(a|b|c|d|e)" // 5 alternatives
		re, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		seq := extractor.ExtractPrefixes(re)
		if seq.Len() > 2 {
			t.Errorf("Expected at most 2 literals (MaxLiterals=2), got %d", seq.Len())
		}
	})

	t.Run("MaxLiteralLen limit", func(t *testing.T) {
		config := DefaultConfig()
		config.MaxLiteralLen = 5
		extractor := New(config)

		pattern := "verylongprefix" // 14 chars
		re, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		seq := extractor.ExtractPrefixes(re)
		if seq.Len() != 1 {
			t.Fatalf("Expected 1 literal, got %d", seq.Len())
		}

		lit := seq.Get(0)
		if len(lit.Bytes) != 5 {
			t.Errorf("Expected literal length 5 (MaxLiteralLen=5), got %d: %q",
				len(lit.Bytes), string(lit.Bytes))
		}
	})

	t.Run("MaxClassSize limit", func(t *testing.T) {
		config := DefaultConfig()
		config.MaxClassSize = 3
		extractor := New(config)

		// [a-d] has 4 characters, exceeds limit of 3
		pattern := "[a-d]"
		re, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		seq := extractor.ExtractPrefixes(re)
		if !seq.IsEmpty() {
			t.Errorf("Expected empty sequence (char class too large), got %d literals", seq.Len())
		}
	})
}

// TestRealWorldPatterns tests extraction from real-world regex patterns
func TestRealWorldPatterns(t *testing.T) {
	tests := []struct {
		pattern  string
		prefixes []string
		suffixes []string
		desc     string
	}{
		{
			pattern:  `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
			prefixes: []string{}, // Char class too large
			suffixes: []string{}, // Char class too large
			desc:     "email regex",
		},
		{
			pattern:  `https?://[^\s]+`,
			prefixes: []string{"http"}, // Common prefix "http"
			suffixes: []string{},
			desc:     "URL regex",
		},
		{
			pattern:  `\d{4}-\d{2}-\d{2}`,
			prefixes: []string{}, // \d is a char class (too large)
			suffixes: []string{},
			desc:     "date pattern YYYY-MM-DD",
		},
		{
			// Parser optimizes POST|PUT to P(OST|UT), but we now expand it back
			pattern:  `(GET|POST|PUT|DELETE)\s+`,
			prefixes: []string{"GET", "POST", "PUT", "DELETE"}, // Properly expanded from factored prefix
			suffixes: []string{},
			desc:     "HTTP method (with parser optimization)",
		},
		{
			pattern:  `.*error.*`,
			prefixes: []string{},
			suffixes: []string{},
			desc:     "log search (no prefix/suffix)",
		},
		{
			pattern:  `function\s+\w+`,
			prefixes: []string{"function"},
			suffixes: []string{},
			desc:     "JS function declaration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// Test prefixes
			prefixSeq := extractPrefixes(t, tt.pattern)
			if len(tt.prefixes) == 0 {
				if !prefixSeq.IsEmpty() {
					t.Logf("Pattern: %s", tt.pattern)
					t.Logf("Expected no prefixes, but got %d:", prefixSeq.Len())
					for i := 0; i < prefixSeq.Len(); i++ {
						t.Logf("  - %q", string(prefixSeq.Get(i).Bytes))
					}
				}
			} else {
				checkLiterals(t, prefixSeq, tt.prefixes)
			}

			// Test suffixes
			suffixSeq := extractSuffixes(t, tt.pattern)
			if len(tt.suffixes) == 0 {
				if !suffixSeq.IsEmpty() {
					t.Logf("Pattern: %s", tt.pattern)
					t.Logf("Expected no suffixes, but got %d:", suffixSeq.Len())
					for i := 0; i < suffixSeq.Len(); i++ {
						t.Logf("  - %q", string(suffixSeq.Get(i).Bytes))
					}
				}
			} else {
				checkLiterals(t, suffixSeq, tt.suffixes)
			}
		})
	}
}

// TestExtractPrefixesEdgeCases tests edge cases
func TestExtractPrefixesEdgeCases(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
		desc     string
	}{
		{"", []string{}, "empty pattern (OpEmptyMatch, no literals)"},
		{".", []string{}, "single dot (wildcard)"},
		{".*", []string{}, "wildcard star"},
		{".+", []string{}, "wildcard plus"},
		{"^$", []string{}, "begin + end anchors only"},
		{"()", []string{}, "empty capture group (OpEmptyMatch, no literals)"},
		{"a{3}", []string{}, "repetition count (not handled as literal)"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			seq := extractPrefixes(t, tt.pattern)
			if len(tt.expected) == 0 {
				if !seq.IsEmpty() {
					t.Errorf("Expected empty sequence for %q, got %d literals", tt.pattern, seq.Len())
					for i := 0; i < seq.Len(); i++ {
						t.Logf("  Got: %q", string(seq.Get(i).Bytes))
					}
				}
			} else {
				checkLiterals(t, seq, tt.expected)
			}
		})
	}
}

// TestExtractPrefixesUnicode tests Unicode handling
func TestExtractPrefixesUnicode(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
		desc     string
	}{
		{"hello世界", []string{"hello世界"}, "mixed ASCII and Unicode"},
		{"(你好|世界)", []string{"你好", "世界"}, "Unicode alternation"},
		{"[äöü]", []string{"ä", "ö", "ü"}, "Unicode char class"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			seq := extractPrefixes(t, tt.pattern)
			checkLiterals(t, seq, tt.expected)
		})
	}
}

// TestExtractPrefixesRecursionLimit tests recursion depth limit
func TestExtractPrefixesRecursionLimit(t *testing.T) {
	// Build a deeply nested pattern: (((((...a)))))
	pattern := "a"
	for i := 0; i < 150; i++ { // Exceed depth limit of 100
		pattern = "(" + pattern + ")"
	}

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	extractor := New(DefaultConfig())
	seq := extractor.ExtractPrefixes(re)

	// Should return empty due to recursion limit
	if !seq.IsEmpty() {
		t.Errorf("Expected empty sequence due to recursion limit, got %d literals", seq.Len())
	}
}

// TestExtractInnerForReverseSearch tests inner literal extraction for ReverseInner strategy
func TestExtractInnerForReverseSearch(t *testing.T) {
	tests := []struct {
		name            string
		pattern         string
		expectNil       bool
		expectedLiteral string
	}{
		{
			name:            "inner literal with wildcards before and after",
			pattern:         `.*connection.*`,
			expectNil:       false,
			expectedLiteral: "connection",
		},
		{
			name:            "ERROR prefix inner suffix pattern",
			pattern:         `ERROR.*connection.*timeout`,
			expectNil:       false,
			expectedLiteral: "connection",
		},
		{
			name:            "func inner return pattern",
			pattern:         `func.*Error.*return`,
			expectNil:       false,
			expectedLiteral: "Error",
		},
		{
			name:            "prefix middle suffix pattern",
			pattern:         `prefix.*middle.*suffix`,
			expectNil:       false,
			expectedLiteral: "middle",
		},
		{
			name:      "prefix only pattern (no inner)",
			pattern:   `hello.*`,
			expectNil: true,
		},
		{
			name:      "suffix only pattern (no inner)",
			pattern:   `.*world`,
			expectNil: true,
		},
		{
			name:      "simple literal (no wildcards)",
			pattern:   `hello`,
			expectNil: true,
		},
		{
			name:      "no wildcards before",
			pattern:   `helloconnectionworld`,
			expectNil: true,
		},
		{
			name:      "wildcard only before, not after",
			pattern:   `.*connection`,
			expectNil: true,
		},
		{
			name:      "wildcard only after, not before",
			pattern:   `connection.*`,
			expectNil: true,
		},
		{
			name:            "alternation with inner",
			pattern:         `.*(foo|bar).*`,
			expectNil:       false,
			expectedLiteral: "foo", // first alternative
		},
		{
			name:      "too short concat (< 3 parts)",
			pattern:   `a.*b`,
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Failed to parse regex %q: %v", tt.pattern, err)
			}

			extractor := New(DefaultConfig())
			innerInfo := extractor.ExtractInnerForReverseSearch(re)

			//nolint:nestif // Test validation logic requires nested conditions
			if tt.expectNil {
				if innerInfo != nil {
					t.Errorf("Expected nil, got inner info with literal %q",
						string(innerInfo.Literals.Get(0).Bytes))
				}
			} else {
				if innerInfo == nil {
					t.Errorf("Expected inner info, got nil")
					return
				}
				if innerInfo.Literals.IsEmpty() {
					t.Errorf("Expected non-empty literals")
					return
				}
				got := string(innerInfo.Literals.Get(0).Bytes)
				if got != tt.expectedLiteral {
					t.Errorf("Expected literal %q, got %q", tt.expectedLiteral, got)
				}
			}
		})
	}
}

// TestExtractInnerForReverseSearchEdgeCases tests edge cases
func TestExtractInnerForReverseSearchEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		expectNil bool
	}{
		{
			name:      "empty pattern",
			pattern:   ``,
			expectNil: true,
		},
		{
			name:      "only wildcard",
			pattern:   `.*`,
			expectNil: true,
		},
		{
			name:      "nested captures with inner",
			pattern:   `.*(a(b)c).*`,
			expectNil: false,
		},
		{
			name:      "multiple wildcards before and after",
			pattern:   `.*.*connection.*.*`,
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Failed to parse regex %q: %v", tt.pattern, err)
			}

			extractor := New(DefaultConfig())
			innerInfo := extractor.ExtractInnerForReverseSearch(re)

			if tt.expectNil && innerInfo != nil {
				t.Errorf("Expected nil, got inner info")
			} else if !tt.expectNil && innerInfo == nil {
				t.Errorf("Expected inner info, got nil")
			}
		})
	}
}

// BenchmarkExtractPrefixes benchmarks prefix extraction performance
func BenchmarkExtractPrefixes(b *testing.B) {
	patterns := []string{
		"hello",
		"(foo|bar|baz)",
		"[abc]test",
		"prefix.*suffix",
		"(GET|POST|PUT|DELETE)",
	}

	extractor := New(DefaultConfig())

	for _, pattern := range patterns {
		re, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			b.Fatalf("Parse failed for %q: %v", pattern, err)
		}

		b.Run(pattern, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = extractor.ExtractPrefixes(re)
			}
		})
	}
}

// BenchmarkExtractInnerForReverseSearch benchmarks inner literal extraction
func BenchmarkExtractInnerForReverseSearch(b *testing.B) {
	patterns := []string{
		".*connection.*",
		"ERROR.*connection.*timeout",
		"prefix.*middle.*suffix",
	}

	extractor := New(DefaultConfig())

	for _, pattern := range patterns {
		re, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			b.Fatalf("Parse failed for %q: %v", pattern, err)
		}

		b.Run(pattern, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = extractor.ExtractInnerForReverseSearch(re)
			}
		})
	}
}

// TestExtractPrefixesFactoredPrefix tests extraction of prefixes when the regex parser
// factors common prefixes. For example: (Wanderlust|Weltanschauung) becomes W(anderlust|eltanschauung).
// We should expand this back to ["Wanderlust", "Weltanschauung"].
func TestExtractPrefixesFactoredPrefix(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected []string
	}{
		{
			name:     "two words with common prefix W",
			pattern:  "(Wanderlust|Weltanschauung)",
			expected: []string{"Wanderlust", "Weltanschauung"},
		},
		{
			name:     "three words with common prefix",
			pattern:  "(test1|test2|test3)",
			expected: []string{"test1", "test2", "test3"},
		},
		{
			name:     "no common prefix",
			pattern:  "(foo|bar|baz)",
			expected: []string{"foo", "bar", "baz"},
		},
		{
			name:     "nested factored prefix",
			pattern:  "(abc1|abc2|abd1|abd2)",
			expected: []string{"abc1", "abc2", "abd1", "abd2"},
		},
		{
			name:     "german words benchmark pattern",
			pattern:  "(Bildungsroman|Doppelganger|Gestalt|Kindergarten)",
			expected: []string{"Bildungsroman", "Doppelganger", "Gestalt", "Kindergarten"},
		},
	}

	extractor := New(DefaultConfig())

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			re, err := syntax.Parse(tc.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse pattern: %v", err)
			}

			seq := extractor.ExtractPrefixes(re)

			if seq.Len() != len(tc.expected) {
				t.Errorf("expected %d literals, got %d", len(tc.expected), seq.Len())
				t.Logf("Got literals:")
				for i := 0; i < seq.Len(); i++ {
					t.Logf("  [%d] %q complete=%v", i, seq.Get(i).Bytes, seq.Get(i).Complete)
				}
				return
			}

			// Check each literal
			gotSet := make(map[string]bool)
			for i := 0; i < seq.Len(); i++ {
				lit := seq.Get(i)
				gotSet[string(lit.Bytes)] = true
				if !lit.Complete {
					t.Errorf("literal %q should be complete", lit.Bytes)
				}
			}

			for _, exp := range tc.expected {
				if !gotSet[exp] {
					t.Errorf("expected literal %q not found", exp)
				}
			}
		})
	}
}

// TestCrossProductExpansion tests cross-product literal expansion for char classes
// in the middle of concatenations.
func TestCrossProductExpansion(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected []string
		complete bool // Whether all literals should be complete
	}{
		{
			name:     "simple concat with middle char class",
			pattern:  "ab[cd]ef",
			expected: []string{"abcef", "abdef"},
			complete: true,
		},
		{
			name:     "DNA pattern: ag[act]gtaaa",
			pattern:  "ag[act]gtaaa",
			expected: []string{"agagtaaa", "agcgtaaa", "agtgtaaa"},
			complete: true,
		},
		{
			name:     "DNA pattern: tttac[agt]ct",
			pattern:  "tttac[agt]ct",
			expected: []string{"tttacact", "tttacgct", "tttactct"},
			complete: true,
		},
		{
			name:     "two char classes: [ab][cd]",
			pattern:  "[ab][cd]",
			expected: []string{"ac", "ad", "bc", "bd"},
			complete: true,
		},
		{
			name:     "char class at start: [abc]test",
			pattern:  "[abc]test",
			expected: []string{"atest", "btest", "ctest"},
			complete: true,
		},
		{
			name:     "char class at end: test[abc]",
			pattern:  "test[abc]",
			expected: []string{"testa", "testb", "testc"},
			complete: true,
		},
		{
			name:     "char class too large stops expansion",
			pattern:  "[a-z]test",
			expected: []string{}, // [a-z] has 26 chars > MaxClassSize=10
			complete: false,
		},
		{
			name:     "wildcard stops expansion",
			pattern:  "ab.*cd",
			expected: []string{"ab"},
			complete: false,
		},
		{
			name:     "repetition stops expansion",
			pattern:  "ab[cd]+ef",
			expected: []string{"ab"},
			complete: false,
		},
	}

	extractor := New(DefaultConfig())

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			re, err := syntax.Parse(tc.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse pattern: %v", err)
			}

			seq := extractor.ExtractPrefixes(re)

			if len(tc.expected) == 0 {
				if !seq.IsEmpty() {
					t.Errorf("expected empty seq, got %d literals:", seq.Len())
					for i := 0; i < seq.Len(); i++ {
						t.Logf("  [%d] %q complete=%v", i, seq.Get(i).Bytes, seq.Get(i).Complete)
					}
				}
				return
			}

			if seq.Len() != len(tc.expected) {
				t.Errorf("expected %d literals, got %d", len(tc.expected), seq.Len())
				for i := 0; i < seq.Len(); i++ {
					t.Logf("  [%d] %q complete=%v", i, seq.Get(i).Bytes, seq.Get(i).Complete)
				}
				return
			}

			for i, exp := range tc.expected {
				got := string(seq.Get(i).Bytes)
				if got != exp {
					t.Errorf("literal %d: expected %q, got %q", i, exp, got)
				}
				if seq.Get(i).Complete != tc.complete {
					t.Errorf("literal %d %q: expected complete=%v, got complete=%v",
						i, got, tc.complete, seq.Get(i).Complete)
				}
			}
		})
	}
}

// TestCrossProductDNAAlternation tests the full DNA alternation pattern
// that motivated this feature.
func TestCrossProductDNAAlternation(t *testing.T) {
	pattern := "ag[act]gtaaa|tttac[agt]ct"

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("failed to parse pattern: %v", err)
	}

	extractor := New(DefaultConfig())
	seq := extractor.ExtractPrefixes(re)

	// Should produce 6 literals: 3 from each branch
	expected := map[string]bool{
		"agagtaaa": true,
		"agcgtaaa": true,
		"agtgtaaa": true,
		"tttacact": true,
		"tttacgct": true,
		"tttactct": true,
	}

	if seq.Len() != 6 {
		t.Errorf("expected 6 literals, got %d", seq.Len())
		for i := 0; i < seq.Len(); i++ {
			t.Logf("  [%d] %q complete=%v", i, seq.Get(i).Bytes, seq.Get(i).Complete)
		}
		return
	}

	for i := 0; i < seq.Len(); i++ {
		lit := seq.Get(i)
		key := string(lit.Bytes)
		if !expected[key] {
			t.Errorf("unexpected literal: %q", key)
		}
		if !lit.Complete {
			t.Errorf("literal %q should be complete", key)
		}
	}
}

// TestCrossProductOverflowHandling tests that overflow is handled gracefully
// when cross-product expansion would produce too many literals.
func TestCrossProductOverflowHandling(t *testing.T) {
	// Pattern with multiple char classes: [0-9][0-9][0-9] = 10*10*10 = 1000 literals
	// Should trigger overflow handling since 1000 > CrossProductLimit=250
	pattern := "[0-9][0-9][0-9]"

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("failed to parse pattern: %v", err)
	}

	extractor := New(DefaultConfig())
	seq := extractor.ExtractPrefixes(re)

	// After overflow: truncated to 4 bytes, deduplicated, marked inexact
	// [0-9][0-9] = 100 literals, each 2 bytes. 100 > MaxLiterals=64 → overflow triggers
	// (overflow check: acc.Len() > crossLimit || acc.Len() > MaxLiterals)
	// KeepFirstBytes(4): 2-byte literals < 4, no truncation
	// Dedup: all 100 are distinct → no removal
	// Final cap: 100 > MaxLiterals=64 → truncated to exactly 64
	// Third [0-9] is never reached because overflow breaks out of the concat loop.

	if seq.IsEmpty() {
		t.Error("expected non-empty seq after overflow handling")
		return
	}

	if seq.Len() != 64 {
		t.Errorf("expected exactly 64 literals after overflow, got %d", seq.Len())
	}

	// Each literal should be exactly 2 bytes (one digit from each of the first two [0-9] classes;
	// the third class is never reached because overflow triggers after the second cross-product)
	for i := 0; i < seq.Len(); i++ {
		if len(seq.Get(i).Bytes) != 2 {
			t.Errorf("literal %d: expected 2 bytes, got %d bytes (%q)",
				i, len(seq.Get(i).Bytes), seq.Get(i).Bytes)
		}
		if seq.Get(i).Complete {
			t.Errorf("literal %q should be inexact after overflow", seq.Get(i).Bytes)
		}
	}
}

// TestCrossProductMaxLiteralsLimit tests that MaxLiterals is respected.
func TestCrossProductMaxLiteralsLimit(t *testing.T) {
	config := DefaultConfig()
	config.MaxLiterals = 4

	extractor := New(config)

	// [0-9]test would produce 10 literals, but MaxLiterals=4 limits the output
	pattern := "[0-9]test"
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("failed to parse pattern: %v", err)
	}

	seq := extractor.ExtractPrefixes(re)

	// Cross-product of 10 class chars * 1 literal = 10, exceeds MaxLiterals=4
	// Overflow handling: truncate to 4 bytes, dedup, cap at MaxLiterals
	if seq.Len() > 4 {
		t.Errorf("expected at most 4 literals (MaxLiterals=4), got %d", seq.Len())
	}
}

// TestCrossProductCompleteFlag tests that Complete flag is correctly propagated.
func TestCrossProductCompleteFlag(t *testing.T) {
	extractor := New(DefaultConfig())

	// Pattern where entire concat is captured: ab[cd]
	// All resulting literals should be complete
	t.Run("all_complete", func(t *testing.T) {
		re, _ := syntax.Parse("ab[cd]", syntax.Perl)
		seq := extractor.ExtractPrefixes(re)
		for i := 0; i < seq.Len(); i++ {
			if !seq.Get(i).Complete {
				t.Errorf("literal %q should be complete", seq.Get(i).Bytes)
			}
		}
	})

	// Pattern with trailing wildcard: ab[cd].*
	// All resulting literals should be incomplete
	t.Run("incomplete_with_wildcard", func(t *testing.T) {
		re, _ := syntax.Parse("ab[cd].*", syntax.Perl)
		seq := extractor.ExtractPrefixes(re)
		for i := 0; i < seq.Len(); i++ {
			if seq.Get(i).Complete {
				t.Errorf("literal %q should be incomplete (wildcard follows)", seq.Get(i).Bytes)
			}
		}
	})
}
