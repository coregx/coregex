package meta

import (
	"regexp/syntax"
	"strings"
	"testing"
)

// TestStrategySelectionComprehensive tests that meta.Compile selects the correct
// execution strategy for a wide variety of pattern classes.
func TestStrategySelectionComprehensive(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    Strategy
	}{
		// ========== UseNFA: tiny patterns without useful literals ==========
		{"nfa_single_char", "a", UseNFA},
		{"nfa_single_char_b", "b", UseNFA},
		{"nfa_two_char_literal", "ab", UseNFA},

		// ========== UseReverseSuffix: .*suffix patterns ==========
		{"rsuffix_dot_star_txt", `.*\.txt`, UseReverseSuffix},
		{"rsuffix_dot_star_log", `.*\.log`, UseReverseSuffix},
		{"rsuffix_dot_star_json", `.*\.json`, UseReverseSuffix},
		{"rsuffix_dot_star_xml", `.*\.xml`, UseReverseSuffix},
		{"rsuffix_dot_star_html", `.*\.html`, UseReverseSuffix},
		{"rsuffix_dot_star_css", `.*\.css`, UseReverseSuffix},

		// ========== UseReverseInner: .*inner.* patterns ==========
		{"rinner_keyword", `.*keyword.*`, UseReverseInner},
		{"rinner_error", `.*ERROR.*`, UseReverseInner},
		{"rinner_colon", `.*:.*`, UseReverseInner},
		{"rinner_at_sign", `.*@.*`, UseReverseInner},

		// ========== UseReverseAnchored: end-anchored patterns ==========
		{"ranchored_end_dollar", `hello$`, UseReverseAnchored},
		{"ranchored_end_world", `world$`, UseReverseAnchored},
		{"ranchored_end_suffix", `\.txt$`, UseReverseAnchored},

		// ========== UseCharClassSearcher: simple char class+ patterns ==========
		{"ccs_word_plus", `\w+`, UseCharClassSearcher},
		{"ccs_digit_plus", `\d+`, UseCharClassSearcher},
		{"ccs_lower_plus", `[a-z]+`, UseCharClassSearcher},
		{"ccs_upper_plus", `[A-Z]+`, UseCharClassSearcher},
		{"ccs_hex_plus", `[0-9a-f]+`, UseCharClassSearcher},
		{"ccs_alnum_plus", `[a-zA-Z0-9]+`, UseCharClassSearcher},

		// ========== UseBoundedBacktracker: char class with captures ==========
		{"bt_word_capture", `(\w)+`, UseBoundedBacktracker},
		{"bt_digit_capture", `(\d)+`, UseBoundedBacktracker},
		{"bt_letter_capture", `([a-z])+`, UseBoundedBacktracker},
		{"bt_alnum_capture", `([0-9])+`, UseBoundedBacktracker},

		// ========== UseCompositeSearcher: concatenated char classes ==========
		{"composite_alpha_digit", `[a-zA-Z]+[0-9]+`, UseCompositeSearcher},
		{"composite_word_digit", `\w+[0-9]+`, UseCompositeSearcher},
		{"composite_lower_upper", `[a-z]+[A-Z]+`, UseCompositeSearcher},

		// ========== UseDigitPrefilter: digit-lead patterns ==========
		{"digit_semver", `\d+\.\d+\.\d+`, UseDigitPrefilter},
		{"digit_version_pair", `\d+\.\d+`, UseDigitPrefilter},
		{"digit_time", `\d+:\d+:\d+`, UseDigitPrefilter},
		{"digit_ip_octet", `25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9]`, UseDigitPrefilter},

		// ========== UseAhoCorasick: large literal alternations (>64 patterns) ==========
		// Teddy handles up to ~64 patterns; AhoCorasick kicks in above that
		{
			"ahocorasick_many_literals",
			strings.Join([]string{
				"alpha", "bravo", "charlie", "delta", "echo",
				"foxtrot", "golf", "hotel", "india", "juliet",
				"kilo", "lima", "mike", "november", "oscar",
				"papa", "quebec", "romeo", "sierra", "tango",
				"uniform", "victor", "whiskey", "xray", "yankee",
				"zulu", "apple", "banana", "cherry", "durian",
				"elderberry", "fig", "grape", "honeydew", "jackfruit",
				"kumquat", "lemon", "mango", "nectarine", "orange",
				"papaya", "quince", "raspberry", "strawberry", "tangerine",
				"ugli", "vanilla", "watermelon", "ximenia", "yuzu",
				"zucchini", "avocado", "blueberry", "coconut", "dragonfruit",
				"eggplant", "fennel", "guava", "hazelnut", "imbe",
				"jujube", "kiwi", "lychee", "mulberry", "nutmeg",
				"olive", "pear", "raisin", "saffron", "thyme",
			}, "|"),
			UseAhoCorasick,
		},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.Strategy()
			if got != tt.want {
				t.Errorf("pattern %q: got strategy %s, want %s",
					tt.pattern, got, tt.want)
			}
		})
	}
}

// TestStrategySelectionWithDFADisabled verifies that disabling DFA forces NFA strategy.
func TestStrategySelectionWithDFADisabled(t *testing.T) {
	config := Config{
		EnableDFA:         false,
		EnablePrefilter:   false,
		MaxRecursionDepth: 100,
	}

	patterns := []string{
		"hello",
		`\d+`,
		`\w+`,
		"foo|bar|baz",
		`[a-z]+[0-9]+`,
		`.*\.txt`,
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			engine, err := CompileWithConfig(pattern, config)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", pattern, err)
			}

			// With DFA disabled, strategy should be NFA-based or specialized searcher
			strategy := engine.Strategy()
			if strategy == UseDFA || strategy == UseBoth {
				t.Errorf("pattern %q: strategy %s should not use DFA when DFA is disabled",
					pattern, strategy)
			}
		})
	}
}

// TestHasWordBoundary tests that hasWordBoundary correctly detects word boundary assertions.
func TestHasWordBoundary(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`\bword\b`, true},
		{`\btest`, true},
		{`hello\b`, true},
		{`\Btest`, true},
		{`hello`, false},
		{`\d+`, false},
		{`.*\.txt`, false},
		{`foo|bar`, false},
		{`(\bx)`, true},
		{`(a|\bx)`, true},
		{`[a-z]+`, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			got := hasWordBoundary(re)
			if got != tt.want {
				t.Errorf("hasWordBoundary(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestHasWordBoundaryNilInput tests that hasWordBoundary handles nil input.
func TestHasWordBoundaryNilInput(t *testing.T) {
	if hasWordBoundary(nil) {
		t.Error("hasWordBoundary(nil) should return false")
	}
}

// TestIsOptionalElement tests isOptionalElement for correct identification.
func TestIsOptionalElement(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`a?`, true},
		{`a*`, true},
		{`a{0,5}`, true},
		{`a+`, false},
		{`a{1,5}`, false},
		{`a{2,5}`, false},
		{`a`, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			got := isOptionalElement(re)
			if got != tt.want {
				t.Errorf("isOptionalElement(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestIsOptionalElementNilInput tests that isOptionalElement handles nil input.
func TestIsOptionalElementNilInput(t *testing.T) {
	if isOptionalElement(nil) {
		t.Error("isOptionalElement(nil) should return false")
	}
}

// TestIsOptionalDigitOnly tests isOptionalDigitOnly for optional digit classes.
func TestIsOptionalDigitOnly(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`[0-9]?`, true},
		{`[1-9]?`, true},
		{`[a-z]?`, false}, // letters, not digits
		{`[0-9a-z]?`, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			got := isOptionalDigitOnly(re)
			if got != tt.want {
				t.Errorf("isOptionalDigitOnly(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestIsOptionalDigitOnlyNilInput tests that isOptionalDigitOnly handles nil.
func TestIsOptionalDigitOnlyNilInput(t *testing.T) {
	if isOptionalDigitOnly(nil) {
		t.Error("isOptionalDigitOnly(nil) should return false")
	}
}

// TestStrategyStringCoverage tests that Strategy.String() returns correct names.
func TestStrategyStringCoverage(t *testing.T) {
	tests := []struct {
		strategy Strategy
		want     string
	}{
		{UseNFA, "UseNFA"},
		{UseDFA, "UseDFA"},
		{UseBoth, "UseBoth"},
		{UseReverseAnchored, "UseReverseAnchored"},
		{UseReverseSuffix, "UseReverseSuffix"},
		{UseOnePass, "UseOnePass"},
		{UseReverseInner, "UseReverseInner"},
		{UseBoundedBacktracker, "UseBoundedBacktracker"},
		{UseTeddy, "UseTeddy"},
		{UseReverseSuffixSet, "UseReverseSuffixSet"},
		{UseCharClassSearcher, "UseCharClassSearcher"},
		{UseCompositeSearcher, "UseCompositeSearcher"},
		{UseBranchDispatch, "UseBranchDispatch"},
		{UseDigitPrefilter, "UseDigitPrefilter"},
		{UseAhoCorasick, "UseAhoCorasick"},
		{UseAnchoredLiteral, "UseAnchoredLiteral"},
		{UseMultilineReverseSuffix, "UseMultilineReverseSuffix"},
		{Strategy(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.strategy.String()
			if got != tt.want {
				t.Errorf("Strategy(%d).String() = %q, want %q", tt.strategy, got, tt.want)
			}
		})
	}
}

// TestStrategySelectionReverseSuffixSet verifies ReverseSuffixSet for multi-suffix alternations.
func TestStrategySelectionReverseSuffixSet(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    Strategy
	}{
		{"multi_ext_3", `.*\.(txt|log|csv)`, UseReverseSuffixSet},
		{"multi_ext_4", `.*\.(txt|log|csv|xml)`, UseReverseSuffixSet},
		{"multi_ext_5", `.*\.(html|json|yaml|toml|conf)`, UseReverseSuffixSet},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.Strategy()
			if got != tt.want {
				t.Errorf("pattern %q: got strategy %s, want %s",
					tt.pattern, got, tt.want)
			}
		})
	}
}

// TestStrategySelectionTeddy verifies Teddy for small literal alternations.
func TestStrategySelectionTeddy(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    Strategy
	}{
		{"teddy_3", "foo|bar|baz", UseTeddy},
		{"teddy_4", "alpha|bravo|charlie|delta", UseTeddy},
		{"teddy_5", "one|two|three|four|five", UseTeddy},
		{"teddy_6", "aaa|bbb|ccc|ddd|eee|fff", UseTeddy},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.Strategy()
			if got != tt.want {
				t.Errorf("pattern %q: got strategy %s, want %s",
					tt.pattern, got, tt.want)
			}
		})
	}
}

// TestStrategySelectionAnchoredLiteral verifies AnchoredLiteral for ^prefix.*suffix$ patterns.
func TestStrategySelectionAnchoredLiteral(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    Strategy
	}{
		{"anchored_prefix_suffix", `^/.*\.php$`, UseAnchoredLiteral},
		{"anchored_suffix_only", `^.*\.txt$`, UseAnchoredLiteral},
		{"anchored_api_json", `^api/.*\.json$`, UseAnchoredLiteral},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.Strategy()
			if got != tt.want {
				t.Errorf("pattern %q: got strategy %s, want %s",
					tt.pattern, got, tt.want)
			}
		})
	}
}

// TestStrategySelectionMultilineReverseSuffix verifies multiline suffix patterns.
func TestStrategySelectionMultilineReverseSuffix(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    Strategy
	}{
		{"multiline_php", `(?m)^/.*\.php`, UseMultilineReverseSuffix},
		{"multiline_log", `(?m)^.*\.log`, UseMultilineReverseSuffix},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.Strategy()
			if got != tt.want {
				t.Errorf("pattern %q: got strategy %s, want %s",
					tt.pattern, got, tt.want)
			}
		})
	}
}

// TestIsDigitLeadConcatWithOptionalPrefix tests digit-lead detection for
// concatenation patterns that have optional digit prefixes.
func TestIsDigitLeadConcatWithOptionalPrefix(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`[1-9]?[0-9]`, true},
		{`[0-9]?[0-9]`, true},
		{`[a-z]?[0-9]`, false},
		{`[0-9]+[a-z]+`, true},
		{`[a-z]+[0-9]+`, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("failed to parse %q: %v", tt.pattern, err)
			}

			got := isDigitLeadPattern(re)
			if got != tt.want {
				t.Errorf("isDigitLeadPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestStrategyCorrectnessAcrossAllStrategies compiles various patterns, checks the
// strategy, and verifies correctness of Find/IsMatch against known results.
func TestStrategyCorrectnessAcrossAllStrategies(t *testing.T) {
	type testCase struct {
		pattern  string
		haystack string
		want     bool
		wantText string // expected match text (empty if no match)
	}

	tests := []testCase{
		// UseNFA
		{"a", "bac", true, "a"},
		{"a", "xyz", false, ""},

		// UseCharClassSearcher
		{`\w+`, "hello world", true, "hello"},
		{`\d+`, "no digits", false, ""},

		// UseBoundedBacktracker
		{`(\w)+`, "abc", true, "abc"},
		{`(\d)+`, "letters", false, ""},

		// UseReverseSuffix
		{`.*\.txt`, "readme.txt", true, "readme.txt"},
		{`.*\.txt`, "readme.pdf", false, ""},

		// UseReverseInner
		{`.*keyword.*`, "this has keyword here", true, "this has keyword here"},
		{`.*keyword.*`, "nothing", false, ""},

		// UseReverseAnchored
		{`end$`, "the end", true, "end"},
		{`end$`, "end of", false, ""},

		// UseCompositeSearcher
		{`[a-zA-Z]+[0-9]+`, "test123rest", true, "test123"},
		{`[a-zA-Z]+[0-9]+`, "test", false, ""},

		// UseDigitPrefilter
		{`\d+\.\d+\.\d+`, "version 1.2.3 here", true, "1.2.3"},
		{`\d+\.\d+\.\d+`, "no version", false, ""},

		// Teddy (literal alternation)
		{"foo|bar|baz", "has bar!", true, "bar"},
		{"foo|bar|baz", "nothing", false, ""},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		name := tt.pattern + "_in_" + tt.haystack
		if len(name) > 60 {
			name = name[:60]
		}
		t.Run(name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			// Check IsMatch
			isMatch := engine.IsMatch([]byte(tt.haystack))
			if isMatch != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v (strategy: %s)",
					tt.haystack, isMatch, tt.want, engine.Strategy())
			}

			// Check Find
			match := engine.Find([]byte(tt.haystack))
			if tt.want {
				if match == nil {
					t.Errorf("Find(%q) = nil, want match %q (strategy: %s)",
						tt.haystack, tt.wantText, engine.Strategy())
				} else if match.String() != tt.wantText {
					t.Errorf("Find(%q) = %q, want %q (strategy: %s)",
						tt.haystack, match.String(), tt.wantText, engine.Strategy())
				}
			} else {
				if match != nil {
					t.Errorf("Find(%q) = %q, want nil (strategy: %s)",
						tt.haystack, match.String(), engine.Strategy())
				}
			}
		})
	}
}

// TestStrategySelectionBranchDispatch verifies that anchored alternations with
// distinct first bytes use BranchDispatch strategy.
func TestStrategySelectionBranchDispatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    Strategy
	}{
		{"branch_digit_uuid", `^(\d+|UUID)`, UseBranchDispatch},
	}

	config := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, config)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.Strategy()
			if got != tt.want {
				t.Errorf("pattern %q: got strategy %s, want %s",
					tt.pattern, got, tt.want)
			}
		})
	}
}
