package literal

import (
	"regexp/syntax"
	"strings"
	"testing"
)

// TestAllComplete tests the Seq.AllComplete method which checks whether
// every literal in the sequence has Complete=true. This enables the
// "literal engine bypass" optimization: skip DFA, use only prefilter.
func TestAllComplete(t *testing.T) {
	tests := []struct {
		name     string
		seq      *Seq
		expected bool
	}{
		{
			name:     "nil sequence returns false",
			seq:      nil,
			expected: false,
		},
		{
			name:     "empty sequence returns false",
			seq:      NewSeq(),
			expected: false,
		},
		{
			name: "single complete literal",
			seq: NewSeq(
				NewLiteral([]byte("hello"), true),
			),
			expected: true,
		},
		{
			name: "single incomplete literal",
			seq: NewSeq(
				NewLiteral([]byte("hello"), false),
			),
			expected: false,
		},
		{
			name: "all complete multiple literals",
			seq: NewSeq(
				NewLiteral([]byte("foo"), true),
				NewLiteral([]byte("bar"), true),
				NewLiteral([]byte("baz"), true),
			),
			expected: true,
		},
		{
			name: "first incomplete rest complete",
			seq: NewSeq(
				NewLiteral([]byte("foo"), false),
				NewLiteral([]byte("bar"), true),
				NewLiteral([]byte("baz"), true),
			),
			expected: false,
		},
		{
			name: "last incomplete rest complete",
			seq: NewSeq(
				NewLiteral([]byte("foo"), true),
				NewLiteral([]byte("bar"), true),
				NewLiteral([]byte("baz"), false),
			),
			expected: false,
		},
		{
			name: "middle incomplete rest complete",
			seq: NewSeq(
				NewLiteral([]byte("foo"), true),
				NewLiteral([]byte("bar"), false),
				NewLiteral([]byte("baz"), true),
			),
			expected: false,
		},
		{
			name: "all incomplete",
			seq: NewSeq(
				NewLiteral([]byte("x"), false),
				NewLiteral([]byte("y"), false),
			),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.seq.AllComplete()
			if got != tt.expected {
				t.Errorf("AllComplete() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestAllCompleteFromExtraction verifies AllComplete via actual regex extraction.
// Patterns that produce only exact literals (no wildcards after) should yield AllComplete=true.
func TestAllCompleteFromExtraction(t *testing.T) {
	extractor := New(DefaultConfig())

	tests := []struct {
		name     string
		pattern  string
		expected bool
	}{
		{
			name:     "simple literal is all complete",
			pattern:  "hello",
			expected: true,
		},
		{
			name:     "alternation of literals is all complete",
			pattern:  "(foo|bar|baz)",
			expected: true,
		},
		{
			name:     "char class concat is all complete",
			pattern:  "[abc]test",
			expected: true,
		},
		{
			name:     "wildcard suffix makes incomplete",
			pattern:  "hello.*",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			seq := extractor.ExtractPrefixes(re)
			got := seq.AllComplete()
			if got != tt.expected {
				t.Errorf("AllComplete() for %q = %v, want %v", tt.pattern, got, tt.expected)
			}
		})
	}
}

// TestEnforceMaxLiteralLenTruncation verifies the enforceMaxLiteralLen method
// truncates literals exceeding MaxLiteralLen and marks them as incomplete.
// The 50% coverage gap is the truncation branch (len > MaxLiteralLen).
func TestEnforceMaxLiteralLenTruncation(t *testing.T) {
	t.Run("concat with long literal triggers truncation", func(t *testing.T) {
		config := DefaultConfig()
		config.MaxLiteralLen = 4
		extractor := New(config)

		// "abcdefgh" is 8 bytes, MaxLiteralLen=4 should truncate to "abcd"
		re, err := syntax.Parse("abcdefgh", syntax.Perl)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		seq := extractor.ExtractPrefixes(re)
		if seq.IsEmpty() {
			t.Fatal("Expected non-empty seq")
		}
		if seq.Len() != 1 {
			t.Fatalf("Expected 1 literal, got %d", seq.Len())
		}

		lit := seq.Get(0)
		if string(lit.Bytes) != "abcd" {
			t.Errorf("Expected truncated to %q, got %q", "abcd", lit.Bytes)
		}
	})

	t.Run("cross-product result exceeds MaxLiteralLen", func(t *testing.T) {
		config := DefaultConfig()
		config.MaxLiteralLen = 5
		extractor := New(config)

		// "abc[de]fghij" in concat: "abc" + [de] + "fghij"
		// Cross products: "abcdfghij" (9 bytes) and "abcefghij" (9 bytes)
		// Both exceed MaxLiteralLen=5, so truncated to 5 bytes
		re, err := syntax.Parse("abc[de]fghij", syntax.Perl)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		seq := extractor.ExtractPrefixes(re)
		if seq.IsEmpty() {
			t.Fatal("Expected non-empty seq")
		}

		for i := 0; i < seq.Len(); i++ {
			lit := seq.Get(i)
			if len(lit.Bytes) > 5 {
				t.Errorf("Literal %d %q exceeds MaxLiteralLen=5 (len=%d)",
					i, lit.Bytes, len(lit.Bytes))
			}
		}
	})

	t.Run("enforceMaxLiteralLen in concat loop", func(t *testing.T) {
		// Use a very short MaxLiteralLen so that the intermediate cross-product
		// result gets truncated during the concat walk (inside extractPrefixesConcat).
		config := DefaultConfig()
		config.MaxLiteralLen = 3
		extractor := New(config)

		// Pattern: "abcdef[xy]" -- "abcdef" is 6 bytes, already truncated to 3 at OpLiteral.
		// Then [xy] cross-product cannot extend (already inexact).
		re, err := syntax.Parse("abcdef[xy]", syntax.Perl)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		seq := extractor.ExtractPrefixes(re)
		if seq.IsEmpty() {
			t.Fatal("Expected non-empty seq")
		}

		for i := 0; i < seq.Len(); i++ {
			lit := seq.Get(i)
			if len(lit.Bytes) > 3 {
				t.Errorf("Literal %d %q exceeds MaxLiteralLen=3", i, lit.Bytes)
			}
		}
	})

	t.Run("enforceMaxLiteralLen direct method", func(t *testing.T) {
		extractor := New(ExtractorConfig{MaxLiteralLen: 3})

		seq := NewSeq(
			NewLiteral([]byte("ab"), true),       // 2 bytes, under limit
			NewLiteral([]byte("abcdef"), true),   // 6 bytes, exceeds limit
			NewLiteral([]byte("xyz"), true),      // 3 bytes, exactly at limit
			NewLiteral([]byte("toolong"), false), // 7 bytes, exceeds limit
		)

		extractor.enforceMaxLiteralLen(seq)

		// "ab" -- unchanged
		if string(seq.Get(0).Bytes) != "ab" || !seq.Get(0).Complete {
			t.Errorf("Literal 0: expected {ab, complete=true}, got {%s, complete=%v}",
				seq.Get(0).Bytes, seq.Get(0).Complete)
		}
		// "abcdef" -> "abc", marked incomplete
		if string(seq.Get(1).Bytes) != "abc" || seq.Get(1).Complete {
			t.Errorf("Literal 1: expected {abc, complete=false}, got {%s, complete=%v}",
				seq.Get(1).Bytes, seq.Get(1).Complete)
		}
		// "xyz" -- unchanged (exactly at limit, not exceeding)
		if string(seq.Get(2).Bytes) != "xyz" || !seq.Get(2).Complete {
			t.Errorf("Literal 2: expected {xyz, complete=true}, got {%s, complete=%v}",
				seq.Get(2).Bytes, seq.Get(2).Complete)
		}
		// "toolong" -> "too", already incomplete
		if string(seq.Get(3).Bytes) != "too" || seq.Get(3).Complete {
			t.Errorf("Literal 3: expected {too, complete=false}, got {%s, complete=%v}",
				seq.Get(3).Bytes, seq.Get(3).Complete)
		}
	})
}

// TestEnforceMaxLiteralLenViaConcatCrossProduct exercises the enforceMaxLiteralLen
// call inside extractPrefixesConcat's loop. This happens when a cross-product
// step produces literals longer than MaxLiteralLen.
func TestEnforceMaxLiteralLenViaConcatCrossProduct(t *testing.T) {
	config := DefaultConfig()
	config.MaxLiteralLen = 6
	extractor := New(config)

	// Pattern "abcd[ef]gh" produces cross-product:
	//   "abcde" + "gh" = "abcdegh" (7 bytes) -- exceeds limit of 6
	//   "abcdf" + "gh" = "abcdfgh" (7 bytes) -- exceeds limit of 6
	// After enforceMaxLiteralLen: truncated to 6 bytes, marked incomplete
	re, err := syntax.Parse("abcd[ef]gh", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractPrefixes(re)
	if seq.IsEmpty() {
		t.Fatal("Expected non-empty seq")
	}

	for i := 0; i < seq.Len(); i++ {
		lit := seq.Get(i)
		if len(lit.Bytes) > 6 {
			t.Errorf("Literal %d %q exceeds MaxLiteralLen=6 (len=%d)",
				i, lit.Bytes, len(lit.Bytes))
		}
	}
}

// TestSuffixLongLiteralTruncation verifies suffix extraction truncation
// from the END (keeps last MaxLiteralLen bytes).
func TestSuffixLongLiteralTruncation(t *testing.T) {
	config := DefaultConfig()
	config.MaxLiteralLen = 5
	extractor := New(config)

	// Generate a 20-char literal: "abcdefghijklmnopqrst"
	re, err := syntax.Parse("abcdefghijklmnopqrst", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractSuffixes(re)
	if seq.IsEmpty() {
		t.Fatal("Expected non-empty suffix seq")
	}

	lit := seq.Get(0)
	// Suffix truncation keeps the LAST 5 bytes: "pqrst"
	if string(lit.Bytes) != "pqrst" {
		t.Errorf("Expected suffix %q, got %q", "pqrst", lit.Bytes)
	}
}

// TestExpandAlternateContributionNilBranch tests the case where one branch
// of an alternation inside a concat has no extractable literals, causing
// expandAlternateContribution to return nil.
func TestExpandAlternateContributionNilBranch(t *testing.T) {
	extractor := New(DefaultConfig())

	// Pattern: "prefix(foo|.*)" -- second alternative .* has no prefix,
	// so expandAlternateContribution returns nil for the alternation.
	// The concat should stop extending at the alternation.
	re, err := syntax.Parse("prefix(foo|.*)", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractPrefixes(re)
	// The (foo|.*) branch has .* which yields empty seq for one alternative,
	// so the alternation returns empty in extractPrefixes.
	// In the concat context, concatSubContribution returns nil for the alternation.
	// Result: "prefix" marked as incomplete.
	if seq.IsEmpty() {
		t.Fatal("Expected non-empty seq (should have 'prefix')")
	}

	lit := seq.Get(0)
	if string(lit.Bytes) != "prefix" {
		t.Errorf("Expected %q, got %q", "prefix", lit.Bytes)
	}
	if lit.Complete {
		t.Errorf("Expected incomplete (alternation stopped expansion)")
	}
}

// TestExpandAlternateContributionTooMany tests the case where alternation
// inside a concat exceeds MaxLiterals, causing expandAlternateContribution
// to return nil. We use a manually constructed AST to avoid Go's parser
// optimization that factors (a|b|c) into [a-c].
func TestExpandAlternateContributionTooMany(t *testing.T) {
	config := DefaultConfig()
	config.MaxLiterals = 2
	extractor := New(config)

	// Build AST manually: OpConcat(OpLiteral("pre"), OpAlternate(a,b,c), OpLiteral("suf"))
	// This avoids parser factoring (a|b|c) into a char class.
	re := &syntax.Regexp{
		Op: syntax.OpConcat,
		Sub: []*syntax.Regexp{
			{Op: syntax.OpLiteral, Rune: []rune("pre")},
			{
				Op: syntax.OpAlternate,
				Sub: []*syntax.Regexp{
					{Op: syntax.OpLiteral, Rune: []rune("a")},
					{Op: syntax.OpLiteral, Rune: []rune("b")},
					{Op: syntax.OpLiteral, Rune: []rune("c")},
				},
			},
			{Op: syntax.OpLiteral, Rune: []rune("suf")},
		},
	}

	seq := extractor.ExtractPrefixes(re)
	if seq.IsEmpty() {
		t.Fatal("Expected non-empty seq")
	}

	// The alternation contributes 3 literals > MaxLiterals=2,
	// expandAlternateContribution returns nil, concat stops extending.
	// Result: "pre" marked as incomplete.
	lit := seq.Get(0)
	if string(lit.Bytes) != "pre" {
		t.Errorf("Expected %q, got %q", "pre", lit.Bytes)
	}
	if lit.Complete {
		t.Errorf("Expected incomplete since alternation blocked expansion")
	}
}

// TestExtractPrefixesCaseInsensitive verifies that case-insensitive patterns
// (FoldCase flag) are skipped during prefix extraction.
func TestExtractPrefixesCaseInsensitive(t *testing.T) {
	extractor := New(DefaultConfig())

	re, err := syntax.Parse("(?i)hello", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractPrefixes(re)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq for case-insensitive pattern, got %d literals", seq.Len())
	}
}

// TestConcatSubContributionFoldCase verifies that a case-insensitive sub-expression
// inside a concat is treated as non-expandable.
func TestConcatSubContributionFoldCase(t *testing.T) {
	extractor := New(DefaultConfig())

	// "abc(?i:def)ghi" -- the (?i:def) part has FoldCase, so it cannot be expanded.
	// concatSubContribution returns nil for it, stopping concat extension.
	re, err := syntax.Parse("abc(?i:def)ghi", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractPrefixes(re)
	if seq.IsEmpty() {
		t.Fatal("Expected at least the 'abc' prefix")
	}

	lit := seq.Get(0)
	if !strings.HasPrefix(string(lit.Bytes), "abc") {
		t.Errorf("Expected prefix starting with 'abc', got %q", lit.Bytes)
	}
}

// TestExtractPrefixesConcatEmptySub verifies that an empty concat is handled.
func TestExtractPrefixesConcatEmptySub(t *testing.T) {
	extractor := New(DefaultConfig())

	// Manually construct an OpConcat with no sub-expressions
	re := &syntax.Regexp{
		Op: syntax.OpConcat,
	}

	seq := extractor.ExtractPrefixes(re)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq for empty concat, got %d literals", seq.Len())
	}
}

// TestExtractPrefixesConcatOnlyAnchors verifies handling of concat with only anchors.
func TestExtractPrefixesConcatOnlyAnchors(t *testing.T) {
	extractor := New(DefaultConfig())

	// Construct concat of only anchors: ^$
	re := &syntax.Regexp{
		Op: syntax.OpConcat,
		Sub: []*syntax.Regexp{
			{Op: syntax.OpBeginLine},
			{Op: syntax.OpEndLine},
		},
	}

	seq := extractor.ExtractPrefixes(re)
	if !seq.IsEmpty() {
		t.Errorf("Expected empty seq for anchor-only concat, got %d literals", seq.Len())
	}
}

// TestExtractPrefixesConcatCrossProductLimitZero verifies that a zero
// CrossProductLimit falls back to the default of 250.
func TestExtractPrefixesConcatCrossProductLimitZero(t *testing.T) {
	config := DefaultConfig()
	config.CrossProductLimit = 0
	extractor := New(config)

	re, err := syntax.Parse("ab[cd]ef", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractPrefixes(re)
	if seq.IsEmpty() {
		t.Fatal("Expected non-empty seq with fallback CrossProductLimit")
	}

	expected := []string{"abcef", "abdef"}
	if seq.Len() != len(expected) {
		t.Fatalf("Expected %d literals, got %d", len(expected), seq.Len())
	}
	for i, exp := range expected {
		if string(seq.Get(i).Bytes) != exp {
			t.Errorf("Literal %d: expected %q, got %q", i, exp, seq.Get(i).Bytes)
		}
	}
}

// TestConcatSubContributionOpRepeat verifies that OpRepeat with min >= 1
// in a concat contributes inexact literals from the inner expression.
func TestConcatSubContributionOpRepeat(t *testing.T) {
	extractor := New(DefaultConfig())

	// Pattern: "abc[xy]{2,5}def" -- the [xy]{2,5} is OpRepeat with min=2.
	// concatSubContribution should extract [xy] as inexact literals for cross-product.
	re, err := syntax.Parse("abc[xy]{2,5}def", syntax.Perl)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	seq := extractor.ExtractPrefixes(re)
	if seq.IsEmpty() {
		t.Fatal("Expected non-empty seq")
	}

	// Should extract "abcx" and "abcy" (abc + [xy] from repeat), both incomplete
	for i := 0; i < seq.Len(); i++ {
		lit := seq.Get(i)
		if !strings.HasPrefix(string(lit.Bytes), "abc") {
			t.Errorf("Literal %d %q should start with 'abc'", i, lit.Bytes)
		}
		if lit.Complete {
			t.Errorf("Literal %d %q should be incomplete (from repeat)", i, lit.Bytes)
		}
	}
}
