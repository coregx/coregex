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

// TestVersionPatternStrategy verifies digit-lead patterns use DigitPrefilter.
// Issue #75: Version patterns like `\d+\.\d+\.\d+` should use DigitPrefilter,
// NOT ReverseInner with "." as inner literal.
//
// Benchmark data (regex-bench, 6MB input):
//   - DigitPrefilter: 2.15ms
//   - ReverseInner:   8.21ms (3.8x slower!)
//
// The "." character has high frequency in typical text, making it a poor
// prefilter choice. DigitPrefilter scans for digits which are rarer.
func TestVersionPatternStrategy(t *testing.T) {
	tests := []struct {
		pattern string
		want    Strategy
		desc    string
	}{
		// Digit-lead patterns with single-char inner literal → DigitPrefilter
		{`\d+\.\d+\.\d+`, UseDigitPrefilter, "semver pattern uses DigitPrefilter"},
		{`\d+\.\d+`, UseDigitPrefilter, "version pair uses DigitPrefilter"},
		{`\d+:\d+:\d+`, UseDigitPrefilter, "time pattern uses DigitPrefilter"},

		// IP patterns use DigitPrefilter (no extractable inner literal)
		{`25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9]`, UseDigitPrefilter, "IP octet uses DigitPrefilter"},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("failed to compile %q: %v", tt.pattern, err)
			}

			if engine.Strategy() != tt.want {
				t.Errorf("Issue #75: pattern %q: got strategy %v, want %v\n"+
					"Digit-lead patterns with single-char inner should use DigitPrefilter!",
					tt.pattern, engine.Strategy(), tt.want)
			}
		})
	}
}
