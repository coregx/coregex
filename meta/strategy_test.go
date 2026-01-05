package meta

import (
	"regexp/syntax"
	"testing"
)

// TestIsDigitOnlyClass verifies that isDigitOnlyClass correctly identifies
// character classes containing only digits [0-9].
func TestIsDigitOnlyClass(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
		desc    string
	}{
		// Digit-only classes (should return true)
		// Note: Single-char classes like [0] are optimized to Literal by Go's parser
		{"[0-9]", true, "full digit range"},
		{"[0-5]", true, "partial digit range low"},
		{"[5-9]", true, "partial digit range high"},
		{"[0-4]", true, "0-4 range"},
		{"[0-35-9]", true, "multiple digit ranges (0-3 and 5-9)"},
		{"[135]", true, "specific digits (1, 3, 5)"},
		{"[02468]", true, "even digits"},
		{"[13579]", true, "odd digits"},

		// Non-digit classes (should return false)
		{"[a-z]", false, "lowercase letters only"},
		{"[A-Z]", false, "uppercase letters only"},
		{"[0-9a-z]", false, "digits and letters"},
		{"[a-z0-9]", false, "letters and digits"},
		{"[0-9_]", false, "digits and underscore"},
		{"[\\w]", false, "word class (includes letters)"},
		{"[\\s]", false, "whitespace class"},
		{"[0-9-]", false, "digits and hyphen"},
		{"[0-9.]", false, "digits and dot"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			// isDigitOnlyClass expects OpCharClass
			// Note: Go's regex parser optimizes single-char classes to Literal,
			// so we skip those cases here
			if re.Op != syntax.OpCharClass {
				t.Skipf("pattern %q optimized to %v (not OpCharClass)", tt.pattern, re.Op)
			}

			got := isDigitOnlyClass(re.Rune)
			if got != tt.want {
				t.Errorf("isDigitOnlyClass(%q) = %v, want %v (%s)", tt.pattern, got, tt.want, tt.desc)
			}
		})
	}
}

// TestIsDigitLeadPattern verifies that isDigitLeadPattern correctly identifies
// patterns that must start with a digit [0-9].
func TestIsDigitLeadPattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
		desc    string
	}{
		// === Patterns that MUST start with digit (should return true) ===

		// Basic digit patterns
		{`\d+`, true, "digit class with plus"},
		{`[0-9]+`, true, "explicit digit range with plus"},
		{`[0-9]`, true, "single digit required"},
		{`[0-5]`, true, "partial digit range"},

		// Literals starting with digit
		{`1`, true, "single digit literal"},
		{`123`, true, "multi-digit literal"},
		{`1abc`, true, "digit followed by letters"},
		{`25[0-5]`, true, "literal 25 followed by digit class"},

		// Alternations where ALL branches start with digit
		{`1|2|3`, true, "alternation of single digits"},
		{`10|20|30`, true, "alternation of two-digit numbers"},
		{`25[0-5]|2[0-4][0-9]`, true, "IP octet high range pattern"},
		// Note: `[1-9]?[0-9]` has a ? which means [1-9] can match zero,
		// but the remaining [0-9] still requires a digit, so it's digit-lead
		{`1[0-9][0-9]|[0-9]`, true, "IP octet pattern (all branches digit-lead)"},
		{`25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[0-9]`, true, "IP octet all branches"},
		// The actual IP octet pattern with optional [1-9]?
		{`[1-9]?[0-9]`, true, "optional digit prefix followed by required digit"},
		{`1[0-9][0-9]|[1-9]?[0-9]`, true, "IP octet pattern with optional prefix"},
		{`25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9]`, true, "full IP octet pattern"},

		// With capture groups
		{`(\d+)`, true, "capture group wrapping digit class"},
		{`([0-9]+)`, true, "capture group wrapping explicit digit range"},
		{`(1|2|3)`, true, "capture group with digit alternation"},

		// Non-capturing groups
		{`(?:25[0-5]|2[0-4][0-9])`, true, "non-capturing group with digit alternation"},
		{`(?:[0-9]+)`, true, "non-capturing group with digit class"},

		// Concatenations starting with digit
		{`[0-9]+[a-z]+`, true, "digit concat with letters (starts with digit)"},
		{`[0-5][0-9]`, true, "two digit classes concatenated"},
		{`1[0-9]{2}`, true, "literal then repeated digit class"},

		// Repeat with min >= 1
		{`[0-9]{1,3}`, true, "digit class with bounded repeat min=1"},
		{`\d{2,4}`, true, "digit class with bounded repeat min=2"},

		// === Patterns that may NOT start with digit (should return false) ===

		// Mixed character classes
		{`[a-z0-9]+`, false, "may start with letter"},
		{`[0-9a-z]+`, false, "may start with digit or letter"},
		{`[\w]+`, false, "word class includes letters"},
		{`[a-zA-Z0-9]+`, false, "alphanumeric may start with letter"},

		// Literals not starting with digit
		{`a\d+`, false, "starts with literal 'a'"},
		{`abc123`, false, "starts with letters"},
		{`foo`, false, "all letters"},
		{`hello`, false, "literal word"},

		// Zero-or-more patterns (can match empty)
		{`\d*foo`, false, "star can match zero - may start with 'f'"},
		{`[0-9]*bar`, false, "star can match zero"},
		{`\d*`, false, "star alone can match empty"},
		{`[0-9]*`, false, "digit star can match empty"},

		// Optional patterns (can match zero)
		{`\d?foo`, false, "quest can match zero - may start with 'f'"},
		{`[0-9]?abc`, false, "quest can match zero"},
		{`\d?`, false, "quest alone can match empty"},

		// Zero-min repeat
		{`[0-9]{0,3}`, false, "bounded repeat with min=0"},
		{`\d{0,5}`, false, "min=0 can match empty"},

		// Dot patterns
		{`.*\d+`, false, "dot-star matches anything"},
		{`.+\d+`, false, "dot-plus may start with non-digit"},
		{`.`, false, "single dot matches any char"},

		// Alternations with non-digit branch
		{`\d+|abc`, false, "alternation has non-digit branch"},
		{`123|abc`, false, "one branch is letters"},
		{`[0-9]+|[a-z]+`, false, "one branch is letters"},

		// Anchors
		{`^\d+`, false, "anchor at start (start anchor doesn't consume, but pattern goes to concat)"},
		// Note: `\d+$` - the pattern still starts with \d+, which IS digit-lead
		// The end anchor doesn't affect what the pattern starts with
		{`\d+$`, true, "anchor at end - still starts with digit"},

		// Word boundaries
		{`\b\d+`, false, "word boundary before digit"},

		// Empty and special
		{``, false, "empty pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			if tt.pattern == "" {
				// Empty pattern case
				if got := isDigitLeadPattern(nil); got != tt.want {
					t.Errorf("isDigitLeadPattern(nil) = %v, want %v", got, tt.want)
				}
				return
			}

			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			got := isDigitLeadPattern(re)
			if got != tt.want {
				t.Errorf("isDigitLeadPattern(%q) = %v, want %v (%s)", tt.pattern, got, tt.want, tt.desc)
			}
		})
	}
}

// TestIPPatternDigitLeadDetection tests that the full IP address pattern
// from Issue #50 is correctly detected as a digit-lead pattern.
func TestIPPatternDigitLeadDetection(t *testing.T) {
	// The actual IP address validation pattern from Issue #50
	ipPattern := `(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])`

	re, err := syntax.Parse(ipPattern, syntax.Perl)
	if err != nil {
		t.Fatalf("failed to parse IP pattern: %v", err)
	}

	if !isDigitLeadPattern(re) {
		t.Errorf("IP pattern should be detected as digit-lead pattern")
	}
}

// TestDigitPrefilterStrategySelection verifies that UseDigitPrefilter strategy
// is correctly selected for patterns that:
// 1. Must start with a digit
// 2. Have no extractable prefix literals (due to alternation structure)
// 3. Are not simple char_class+ patterns (those use CharClassSearcher)
func TestDigitPrefilterStrategySelection(t *testing.T) {
	tests := []struct {
		pattern string
		want    Strategy
		desc    string
	}{
		// IP address patterns should use UseDigitPrefilter
		// Note: The full IP pattern has complex alternation that produces no good literals
		{`25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9]`, UseDigitPrefilter, "IP octet pattern"},

		// Simple digit patterns use CharClassSearcher (more efficient)
		{`[0-9]+`, UseCharClassSearcher, "simple digit class uses CharClassSearcher"},
		{`\d+`, UseCharClassSearcher, "simple \\d+ uses CharClassSearcher"},

		// Patterns with good prefix literals use UseDFA
		{`123\d+`, UseNFA, "literal prefix uses NFA (tiny pattern with literals)"},

		// Non-digit patterns should NOT use UseDigitPrefilter
		{`[a-z]+`, UseCharClassSearcher, "letter class uses CharClassSearcher"},
		{`\w+`, UseCharClassSearcher, "word class uses CharClassSearcher"},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
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

// TestCaptureGroupStrategySelection verifies strategy selection for character class patterns.
// - Patterns WITH capture groups use UseBoundedBacktracker (3-7x faster than PikeVM)
// - Patterns WITHOUT captures use UseCharClassSearcher (14-17x faster than BoundedBacktracker)
func TestCaptureGroupStrategySelection(t *testing.T) {
	tests := []struct {
		pattern string
		want    Strategy
	}{
		// WITH capture groups: use BoundedBacktracker (3-7x faster than PikeVM)
		{"(a|b|c)+", UseBoundedBacktracker},
		{"([0-9])+", UseBoundedBacktracker},
		{"([a-z])*", UseBoundedBacktracker},
		{"(\\d)+", UseBoundedBacktracker},

		// WITHOUT capture groups: use CharClassSearcher (14-17x faster than BoundedBacktracker!)
		{"[abc]+", UseCharClassSearcher},
		{"[0-9]+", UseCharClassSearcher},
		{"[\\w]+", UseCharClassSearcher},
		{"[a-z]+", UseCharClassSearcher},

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

// TestEmailPatternStrategy is a REGRESSION test to ensure email patterns
// use UseReverseInner strategy. The "@" symbol (1 byte) must trigger ReverseInner
// because it provides 11-42x speedup for email patterns.
//
// IMPORTANT: Do not change minInnerLen threshold without updating this test!
// v0.8.20 regression: minInnerLen was accidentally changed from 1 to 3, breaking
// email pattern performance (from 11-42x faster to 3x slower than stdlib).
func TestEmailPatternStrategy(t *testing.T) {
	tests := []struct {
		pattern string
		want    Strategy
		desc    string
	}{
		// Email patterns MUST use ReverseInner (via "@" inner literal)
		{`[\w.+-]+@[\w.-]+\.[\w.-]+`, UseReverseInner, "email with @ inner literal"},
		{`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, UseReverseInner, "strict email pattern"},

		// This uses ReverseSuffix because ".com" is a good suffix literal
		{`.*@example\.com`, UseReverseSuffix, "email with suffix literal uses ReverseSuffix"},

		// Single-character inner literals MUST also use ReverseInner
		{`.*:.*`, UseReverseInner, "colon as inner literal"},
		{`.*#.*`, UseReverseInner, "hash as inner literal"},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("failed to compile %q: %v", tt.pattern, err)
			}

			if engine.Strategy() != tt.want {
				t.Errorf("REGRESSION: pattern %q: got strategy %v, want %v\n"+
					"Email patterns need minInnerLen=1 for ReverseInner with single-char literals!",
					tt.pattern, engine.Strategy(), tt.want)
			}
		})
	}
}

// TestIPPatternStrategy verifies that complex IP-like patterns do NOT use DigitPrefilter.
//
// Issue: IP patterns like `(?:(?:25[0-5]|2[0-4][0-9]|...)\.){3}...` have:
//   - 74 NFA states (high verification cost)
//   - 16+ alternation branches (high false positive rate)
//   - 9.2x more digit positions than actual matches
//
// Using DigitPrefilter on such patterns causes 1.3x REGRESSION vs stdlib.
// These patterns should use UseBoth (lazy DFA without prefilter) instead.
//
// Reference: Rust regex avoids prefiltering for patterns with high false positive rates.
// See: regex-automata/src/util/prefilter/mod.rs lines 12-18.
func TestIPPatternStrategy(t *testing.T) {
	// Full IP validation pattern - MUST NOT use DigitPrefilter
	ipPattern := `(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])`

	config := DefaultConfig()
	engine, err := CompileWithConfig(ipPattern, config)
	if err != nil {
		t.Fatalf("failed to compile IP pattern: %v", err)
	}

	strategy := engine.Strategy()

	// IP pattern should NOT use DigitPrefilter due to complexity
	if strategy == UseDigitPrefilter {
		t.Errorf("REGRESSION: IP pattern should NOT use UseDigitPrefilter\n"+
			"Got: %v\n"+
			"Complex patterns with many alternations have high false positive rate.\n"+
			"DigitPrefilter causes 1.3x regression vs stdlib for IP patterns.",
			strategy)
	}

	// Expected: UseBoth or UseDFA (lazy DFA without prefilter overhead)
	if strategy != UseBoth && strategy != UseDFA {
		t.Logf("IP pattern using strategy: %v (expected UseBoth or UseDFA)", strategy)
	}
}

// TestDigitPrefilterComplexityAnalysis verifies the complexity analysis for DigitPrefilter.
//
// Note: Go's regex parser aggressively simplifies simple alternations to char classes:
//   - `a|b|c` becomes CharClass [a-c] (no OpAlternate!)
//   - `(a|b)|(c|d)` has only 2 OpAlternate branches (inner ones become CharClass)
//
// This means we test with patterns that can't be simplified.
func TestDigitPrefilterComplexityAnalysis(t *testing.T) {
	tests := []struct {
		pattern              string
		wantBranches         int
		wantMaxDepth         int
		wantNestedRepetition bool
		desc                 string
	}{
		// Simple patterns - no alternation
		{`\d+`, 0, 0, false, "simple digit+"},
		{`[0-9]+`, 0, 0, false, "char class digit+"},

		// Simple alternations get optimized to char class by Go's parser
		// `a|b|c` → CharClass [a-c], no OpAlternate
		{`a|b|c`, 0, 0, false, "simple alternation optimized to char class"},

		// Alternation with captures preserves OpAlternate at top level
		// `(a|b)|(c|d)` → Alternate with 2 branches (Capture(CharClass))
		{`(a|b)|(c|d)`, 2, 1, false, "2-branch alternation with captures"},

		// Nested alternation with captures
		// `(a|(b|c))` → Capture → Alternate(Literal, Capture(CharClass))
		{`(a|(b|c))`, 2, 1, false, "nested alternation (inner optimized to char class)"},

		// Repetition inside alternation
		{`(a+|b+)`, 2, 1, true, "repetition inside alternation"},
		{`(a*|b)`, 2, 1, true, "star inside alternation"},

		// Note: Go's parser is very aggressive at optimizing alternations.
		// `(foo|bar|baz)` becomes Alternate(Literal("foo"), Concat(Literal("ba"), CharClass([rz])))
		// So it has 2 branches at OpAlternate level, not 3.
		{`(foo|bar|baz)`, 2, 1, false, "3 different literals (Go optimizes to 2 branches)"},
		{`(abc|def|ghi|jkl)`, 4, 1, false, "4 different literals"},

		// IP pattern complexity - should have many branches
		// Each octet has 4 branches: 25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9]
		// Repeated {3} creates more complexity
		{`(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])`,
			0, 0, false, "IP pattern - complex (checked separately)"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			result := analyzeDigitPrefilterComplexity(re)

			// For IP pattern, we just verify it has high complexity
			if tt.desc == "IP pattern - complex (checked separately)" {
				// IP pattern - check it's detected as complex
				if result.alternationBranches < 8 {
					t.Errorf("IP pattern should have >= 8 alternation branches, got %d",
						result.alternationBranches)
				}
				if !result.hasNestedRepetition {
					t.Errorf("IP pattern should have nested repetition")
				}
				return
			}

			if result.alternationBranches != tt.wantBranches {
				t.Errorf("alternationBranches: got %d, want %d",
					result.alternationBranches, tt.wantBranches)
			}

			if result.maxNestingDepth != tt.wantMaxDepth {
				t.Errorf("maxNestingDepth: got %d, want %d",
					result.maxNestingDepth, tt.wantMaxDepth)
			}

			if result.hasNestedRepetition != tt.wantNestedRepetition {
				t.Errorf("hasNestedRepetition: got %v, want %v",
					result.hasNestedRepetition, tt.wantNestedRepetition)
			}
		})
	}
}

// TestIsDigitPrefilterBeneficial verifies that complex patterns are correctly excluded.
func TestIsDigitPrefilterBeneficial(t *testing.T) {
	tests := []struct {
		pattern string
		nfaSize int
		want    bool
		desc    string
	}{
		// Simple digit patterns - should be beneficial
		{`\d+`, 10, true, "simple digit+ with small NFA"},
		{`[0-9]+`, 10, true, "char class digit+ with small NFA"},
		{`\d{3}`, 15, true, "fixed digit repetition"},

		// Too large NFA - not beneficial
		{`\d+`, 60, false, "digit+ with large NFA (>50 states)"},
		{`[0-9]+`, 100, false, "digit+ with very large NFA"},

		// Note: Go's parser optimizes `(0|1|2|3|4|5|6|7|8|9)+` to CharClass [0-9]+
		// So it has 0 alternation branches. Use a pattern that preserves alternations.
		// IP-like patterns with mixed literal prefixes preserve alternation structure.
		{`(10|20|30|40|50|60|70|80|90)+`, 30, false, "9 branches with different prefixes"},

		// Complex IP pattern - not beneficial
		{`(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])`,
			74, false, "IP pattern - too complex"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			got := isDigitPrefilterBeneficial(re, tt.nfaSize)

			if got != tt.want {
				t.Errorf("isDigitPrefilterBeneficial(%q, nfaSize=%d): got %v, want %v",
					tt.pattern, tt.nfaSize, got, tt.want)
			}
		})
	}
}
