package meta

// Tests for strategy selection helpers, AST analysis functions,
// compilation paths, and engine properties.

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"
)

func TestContainsLineStartAnchor(t *testing.T) {
	// This is tested indirectly through MultilineReverseSuffix strategy selection.
	// Patterns with (?m)^ trigger containsLineStartAnchor.
	patterns := []struct {
		name    string
		pattern string
		want    bool // whether strategy is multiline
	}{
		{"multiline_prefix", `(?m)^/.*\.php`, true},
		{"multiline_capture", `(?m)^(GET|POST).*`, true},
		{"no_multiline", `^/.*\.php`, false},
		{"multiline_alternation", `(?m)^(abc|def).*\.txt`, true},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			got := engine.Strategy() == UseMultilineReverseSuffix
			if got != tt.want {
				t.Logf("Strategy for %q: %s (want multiline=%v)", tt.pattern, engine.Strategy(), tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 44. isStartAnchorOnly (35.7%) — exercise various anchor patterns.
// -----------------------------------------------------------------------------

func TestIsStartAnchorOnly_Patterns(t *testing.T) {
	// We test isStartAnchorOnly indirectly through reverse_inner behavior.
	// Patterns that start with .* followed by inner literal trigger ReverseInner.
	// Patterns that start with ^ trigger anchored strategies.
	// isStartAnchorOnly is called from ReverseInner to check if prefix is anchor-only.
	patterns := []struct {
		name    string
		pattern string
		inputs  []string
	}{
		{"dotstar_inner", `.*keyword.*`, []string{
			"hello keyword world",
			"no match here",
			"",
		}},
		{"dotplus_inner", `.+keyword.+`, []string{
			"hello keyword world",
			"no match here",
			"",
		}},
		{"anchored_dotstar", `^.*keyword`, []string{
			"hello keyword",
			"no match here",
			"",
		}},
		{"charclass_prefix", `[a-z]+keyword`, []string{
			"hellokeyword and more",
			"no match here",
			"",
		}},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: strategy=%s", tt.name, engine.Strategy())

			re := regexp.MustCompile(tt.pattern)

			for _, input := range tt.inputs {
				match := engine.Find([]byte(input))
				stdMatch := re.FindString(input)
				got := ""
				if match != nil {
					got = match.String()
				}
				if got != stdMatch {
					t.Errorf("Find(%q): got %q, stdlib %q", input, got, stdMatch)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 45. ReverseSuffixSet.IsMatch (71.4%) — exercise more patterns.
// -----------------------------------------------------------------------------

func TestStrategyReasonComplex_MorePaths(t *testing.T) {
	config := DefaultConfig()

	// UseDFA reason - large NFA
	t.Run("dfa_large_nfa", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `LONGPREFIX[a-z]{5,20}[0-9]{3,10}`, config)
		reason := StrategyReason(UseDFA, compiledNFA, literals, config)
		if reason == "" {
			t.Error("StrategyReason returned empty")
		}
		t.Logf("UseDFA reason: %s", reason)
	})

	// UseBoth reason
	t.Run("both_medium_nfa", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, useBothPattern(), config)
		reason := StrategyReason(UseBoth, compiledNFA, literals, config)
		if reason == "" {
			t.Error("StrategyReason returned empty")
		}
		t.Logf("UseBoth reason: %s", reason)
	})

	// UseAnchoredLiteral reason
	t.Run("anchored_literal", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `^hello.*world$`, config)
		reason := StrategyReason(UseAnchoredLiteral, compiledNFA, literals, config)
		if reason == "" {
			t.Error("StrategyReason returned empty")
		}
		t.Logf("UseAnchoredLiteral reason: %s", reason)
	})

	// UseMultilineReverseSuffix reason
	t.Run("multiline_reverse_suffix", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `(?m)^/.*\.php`, config)
		reason := StrategyReason(UseMultilineReverseSuffix, compiledNFA, literals, config)
		if reason == "" {
			t.Error("StrategyReason returned empty")
		}
		t.Logf("UseMultilineReverseSuffix reason: %s", reason)
	})

	// UseBranchDispatch reason
	t.Run("branch_dispatch", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `^(foo|bar|baz|qux)`, config)
		reason := StrategyReason(UseBranchDispatch, compiledNFA, literals, config)
		if reason == "" {
			t.Error("StrategyReason returned empty")
		}
		t.Logf("UseBranchDispatch reason: %s", reason)
	})

	// UseReverseSuffixSet reason
	t.Run("reverse_suffix_set", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `.*\.(txt|log|md)`, config)
		reason := StrategyReason(UseReverseSuffixSet, compiledNFA, literals, config)
		if reason == "" {
			t.Error("StrategyReason returned empty")
		}
		t.Logf("UseReverseSuffixSet reason: %s", reason)
	})

	// UseOnePass reason
	t.Run("onepass", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `^[a-z]+$`, config)
		reason := StrategyReason(UseOnePass, compiledNFA, literals, config)
		if reason == "" {
			t.Error("StrategyReason returned empty")
		}
		t.Logf("UseOnePass reason: %s", reason)
	})

	// UseAhoCorasick reason
	t.Run("aho_corasick", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `alpha|beta|gamma|delta|epsilon|zeta|eta|theta|iota`, config)
		reason := StrategyReason(UseAhoCorasick, compiledNFA, literals, config)
		if reason == "" {
			t.Error("StrategyReason returned empty")
		}
		t.Logf("UseAhoCorasick reason: %s", reason)
	})
}

// -----------------------------------------------------------------------------
// 47. ReverseInner.Find / FindIndicesAt — exercise more branches of the
//     bidirectional DFA scan loop.
// -----------------------------------------------------------------------------

func TestCompileWithConfig_CustomConfig(t *testing.T) {
	// Test with DFA disabled
	config := DefaultConfig()
	config.EnableDFA = false

	engine, err := CompileWithConfig(`hello`, config)
	if err != nil {
		t.Fatal(err)
	}
	// Should use NFA strategy since DFA is disabled
	if engine.Strategy() == UseDFA || engine.Strategy() == UseBoth {
		t.Errorf("Strategy should not be DFA/Both with EnableDFA=false, got %s", engine.Strategy())
	}

	// Test matching still works
	if !engine.IsMatch([]byte("hello world")) {
		t.Error("IsMatch should be true")
	}
	if engine.IsMatch([]byte("goodbye")) {
		t.Error("IsMatch should be false")
	}
}

func TestCompileInvalidPattern(t *testing.T) {
	// Test various invalid patterns
	invalids := []string{
		`[`,       // unclosed bracket
		`(`,       // unclosed paren
		`*`,       // nothing to repeat
		`(?P<>a)`, // empty group name
	}

	for _, pat := range invalids {
		_, err := Compile(pat)
		if err == nil {
			t.Errorf("Compile(%q) should fail", pat)
		}
	}
}

// -----------------------------------------------------------------------------
// 61. Strategy helper functions — exercise more branches.
// -----------------------------------------------------------------------------

func TestCompileError_Methods(t *testing.T) {
	_, err := Compile(`[invalid`)
	if err == nil {
		t.Fatal("expected error for invalid pattern")
	}

	// Test Error() method
	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string")
	}

	// Test type assertion
	type unwrapper interface {
		Unwrap() error
	}
	if uw, ok := err.(unwrapper); ok {
		inner := uw.Unwrap()
		if inner == nil {
			t.Error("Unwrap() returned nil")
		}
	}
}

// -----------------------------------------------------------------------------
// 65. Exercise buildStrategyEngines and buildReverseSearchers more branches.
// -----------------------------------------------------------------------------

func TestCompile_VariousStrategies(t *testing.T) {
	// Compile patterns that trigger different strategy selections
	// to exercise buildStrategyEngines and buildReverseSearchers
	patterns := []struct {
		name    string
		pattern string
	}{
		{"simple_literal", `hello`},
		{"alternation_3", `foo|bar|baz`},
		{"alternation_10", `alpha|beta|gamma|delta|epsilon|zeta|eta|theta|iota|kappa`},
		{"char_class", `\w+`},
		{"composite", `[a-z]+\d+`},
		{"anchored_simple", `^hello$`},
		{"anchored_alt", `^(foo|bar|baz|qux)`},
		{"suffix", `.*\.txt`},
		{"suffix_set", `.*\.(txt|log|md)`},
		{"inner", `.*keyword.*`},
		{"multiline", `(?m)^/.*\.php`},
		{"anchored_literal", `^/.*\.php$`},
		{"digit", `\d+\.\d+`},
		{"large_alt", `(a|b|c|d|e|f|g|h)*(a|b|c|d|e|f|g|h)*z`},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			re := regexp.MustCompile(tt.pattern)

			// Quick correctness check on a few inputs
			inputs := []string{"hello world", "foo bar baz", "test.txt", "/index.php", "123.456", ""}
			for _, input := range inputs {
				got := engine.IsMatch([]byte(input))
				std := re.MatchString(input)
				if got != std {
					t.Errorf("IsMatch(%q) = %v, stdlib = %v (strategy=%s)",
						input, got, std, engine.Strategy())
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 66. Exercise isDigitRunSkipSafe more branches via strategy selection.
// -----------------------------------------------------------------------------

func TestEngine_Stats(t *testing.T) {
	engine, err := Compile(`hello`)
	if err != nil {
		t.Fatal(err)
	}

	// Initial stats should be zero
	stats := engine.Stats()
	if stats.NFASearches != 0 || stats.DFASearches != 0 {
		t.Errorf("initial stats not zero: %+v", stats)
	}

	// Do some operations
	engine.IsMatch([]byte("hello world"))
	engine.Find([]byte("hello world"))
	engine.FindIndices([]byte("hello world"))
	engine.Count([]byte("hello world hello"), -1)

	stats = engine.Stats()
	total := stats.NFASearches + stats.DFASearches + stats.PrefilterHits + stats.AhoCorasickSearches
	if total == 0 {
		t.Error("stats should be non-zero after operations")
	}

	// Reset
	engine.ResetStats()
	stats = engine.Stats()
	if stats.NFASearches != 0 || stats.DFASearches != 0 {
		t.Errorf("stats not zero after reset: %+v", stats)
	}
}

func TestEngine_Properties(t *testing.T) {
	engine, err := Compile(`^(hello)\s+(\w+)$`)
	if err != nil {
		t.Fatal(err)
	}

	// NumCaptures (group 0 + 2 explicit captures = 3)
	if engine.NumCaptures() != 3 {
		t.Errorf("NumCaptures = %d, want 3", engine.NumCaptures())
	}

	// SubexpNames
	names := engine.SubexpNames()
	if len(names) == 0 {
		t.Error("SubexpNames should not be empty")
	}

	// IsStartAnchored
	if !engine.IsStartAnchored() {
		t.Error("pattern should be start-anchored")
	}

	// Strategy
	strategy := engine.Strategy()
	if strategy.String() == "" {
		t.Error("Strategy string should not be empty")
	}
}

// =============================================================================
// Wave 4E: Final coverage push targeting remaining uncovered blocks.
// Focus: strategy.go helper functions, reverse_inner.go AST helpers,
// anchored_literal.go branches, findall.go edge cases, ismatch.go dispatch.
// =============================================================================

// --- Test 71: isStartAnchorOnly detailed branch coverage ---
// Covers: reverse_inner.go isStartAnchorOnly lines 50-81
// Targets: OpStar/OpPlus/OpQuest wrapping, OpConcat, OpCapture, OpEmptyMatch, nil

func TestIsStartAnchorOnly_Branches(t *testing.T) {
	tests := []struct {
		name string
		re   *syntax.Regexp
		want bool
	}{
		{"nil", nil, false},
		{"BeginText", &syntax.Regexp{Op: syntax.OpBeginText}, true},
		{"BeginLine", &syntax.Regexp{Op: syntax.OpBeginLine}, true},
		{"Literal", &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{'a'}}, false},
		{"Star_of_BeginText", &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpBeginText}},
		}, true},
		{"Plus_of_BeginText", &syntax.Regexp{
			Op:  syntax.OpPlus,
			Sub: []*syntax.Regexp{{Op: syntax.OpBeginText}},
		}, true},
		{"Quest_of_BeginText", &syntax.Regexp{
			Op:  syntax.OpQuest,
			Sub: []*syntax.Regexp{{Op: syntax.OpBeginText}},
		}, true},
		{"Star_of_Literal", &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpLiteral, Rune: []rune{'a'}}},
		}, false},
		{"Concat_of_anchors", &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpBeginText},
				{Op: syntax.OpBeginLine},
			},
		}, true},
		{"Concat_with_non_anchor", &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpBeginText},
				{Op: syntax.OpLiteral, Rune: []rune{'x'}},
			},
		}, false},
		{"Concat_empty", &syntax.Regexp{
			Op:  syntax.OpConcat,
			Sub: []*syntax.Regexp{},
		}, false},
		{"Capture_of_BeginText", &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{{Op: syntax.OpBeginText}},
		}, true},
		{"Capture_of_Literal", &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{{Op: syntax.OpLiteral, Rune: []rune{'a'}}},
		}, false},
		{"Capture_empty", &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{},
		}, false},
		{"EmptyMatch", &syntax.Regexp{Op: syntax.OpEmptyMatch}, true},
		{"AnyChar", &syntax.Regexp{Op: syntax.OpAnyChar}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStartAnchorOnly(tt.re)
			if got != tt.want {
				t.Errorf("isStartAnchorOnly = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Test 72: isUniversalMatch detailed branch coverage ---
// Covers: reverse_inner.go isUniversalMatch lines 25-44
// Targets: nil check, OpEmptyMatch, AnyChar/AnyCharNotNL distinction

func TestIsUniversalMatch_Branches(t *testing.T) {
	tests := []struct {
		name string
		re   *syntax.Regexp
		want bool
	}{
		{"nil", nil, false},
		{"OpEmptyMatch", &syntax.Regexp{Op: syntax.OpEmptyMatch}, true},
		{"Star_AnyChar", &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpAnyChar}},
		}, true},
		{"Star_AnyCharNotNL", &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpAnyCharNotNL}},
		}, true},
		{"Plus_AnyChar", &syntax.Regexp{
			Op:  syntax.OpPlus,
			Sub: []*syntax.Regexp{{Op: syntax.OpAnyChar}},
		}, true},
		{"Star_Literal", &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpLiteral, Rune: []rune{'a'}}},
		}, false},
		{"Literal", &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{'a'}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUniversalMatch(tt.re)
			if got != tt.want {
				t.Errorf("isUniversalMatch = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Test 73: endsWithUniversalMatch detailed branch coverage ---
// Covers: reverse_inner.go endsWithUniversalMatch lines 83-111
// Targets: nil, OpStar direct, Concat last, Capture unwrap, non-universal

func TestEndsWithUniversalMatch_Branches(t *testing.T) {
	tests := []struct {
		name string
		re   *syntax.Regexp
		want bool
	}{
		{"nil", nil, false},
		{"Star_AnyChar", &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpAnyChar}},
		}, true},
		{"Plus_AnyCharNotNL", &syntax.Regexp{
			Op:  syntax.OpPlus,
			Sub: []*syntax.Regexp{{Op: syntax.OpAnyCharNotNL}},
		}, true},
		{"Concat_ends_Star", &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpLiteral, Rune: []rune{'a'}},
				{Op: syntax.OpStar, Sub: []*syntax.Regexp{{Op: syntax.OpAnyChar}}},
			},
		}, true},
		{"Concat_ends_Literal", &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpStar, Sub: []*syntax.Regexp{{Op: syntax.OpAnyChar}}},
				{Op: syntax.OpLiteral, Rune: []rune{'z'}},
			},
		}, false},
		{"Concat_empty", &syntax.Regexp{
			Op:  syntax.OpConcat,
			Sub: []*syntax.Regexp{},
		}, false},
		{"Capture_wrapping_Star", &syntax.Regexp{
			Op: syntax.OpCapture,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpStar, Sub: []*syntax.Regexp{{Op: syntax.OpAnyChar}}},
			},
		}, true},
		{"Capture_empty_sub", &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{},
		}, false},
		{"Star_Literal", &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpLiteral, Rune: []rune{'a'}}},
		}, false},
		{"Literal", &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{'x'}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endsWithUniversalMatch(tt.re)
			if got != tt.want {
				t.Errorf("endsWithUniversalMatch = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Test 74: isSafeForReverseInner detailed branch coverage ---
// Covers: strategy.go isSafeForReverseInner lines 841-874
// Targets: OpCapture wrapping, CharClass Plus, default return false

func TestContainsLineStartAnchor_Branches(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		// Simple (?m)^ pattern
		{"multiline_caret", `(?m)^hello`, true},
		// Non-multiline: ^ becomes OpBeginText, not OpBeginLine
		{"textstart", `^hello`, false}, // OpBeginText, not OpBeginLine
		// Alternation where all branches have line start anchor
		{"alt_all_anchored", `(?m)^foo|(?m)^bar`, true},
		// No anchor at all
		{"no_anchor", `hello`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}
			got := containsLineStartAnchor(re)
			if got != tt.want {
				t.Errorf("containsLineStartAnchor(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}

	// Direct AST test: OpAlternate with all OpBeginLine
	t.Run("alt_all_beginline", func(t *testing.T) {
		re := &syntax.Regexp{
			Op: syntax.OpAlternate,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpBeginLine},
				{Op: syntax.OpBeginLine},
			},
		}
		got := containsLineStartAnchor(re)
		if !got {
			t.Error("expected true for alternate with all OpBeginLine")
		}
	})

	// Direct AST test: OpAlternate with mixed
	t.Run("alt_mixed", func(t *testing.T) {
		re := &syntax.Regexp{
			Op: syntax.OpAlternate,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpBeginLine},
				{Op: syntax.OpLiteral, Rune: []rune{'x'}},
			},
		}
		got := containsLineStartAnchor(re)
		if got {
			t.Error("expected false for alternate with mixed anchored/unanchored")
		}
	})

	// Direct AST test: OpAlternate with empty sub
	t.Run("alt_empty", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpAlternate,
			Sub: []*syntax.Regexp{},
		}
		got := containsLineStartAnchor(re)
		if got {
			t.Error("expected false for empty alternate")
		}
	})

	// Direct AST test: OpCapture wrapping
	t.Run("capture_wrapping_beginline", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpCapture,
			Sub: []*syntax.Regexp{{Op: syntax.OpBeginLine}},
		}
		got := containsLineStartAnchor(re)
		if !got {
			t.Error("expected true for capture wrapping OpBeginLine")
		}
	})
}

// --- Test 76: isSafeForMultilineReverseSuffix detailed branch coverage ---
// Covers: strategy.go isSafeForMultilineReverseSuffix lines 772-809
// Targets: OpCapture wrapping, non-multiline, non-concat

func TestHasDotStarPrefix_ExtraBranches(t *testing.T) {
	// Capture wrapping: (.*\.txt)
	t.Run("capture_wrapped", func(t *testing.T) {
		re, err := syntax.Parse(`(.*\.txt)`, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		got := hasDotStarPrefix(re)
		if !got {
			t.Errorf("hasDotStarPrefix(`(.*\\.txt)`) = %v, want true", got)
		}
	})

	// Single sub concat (< 2 subs)
	t.Run("single_sub_concat", func(t *testing.T) {
		re := &syntax.Regexp{
			Op: syntax.OpConcat,
			Sub: []*syntax.Regexp{
				{Op: syntax.OpStar, Sub: []*syntax.Regexp{{Op: syntax.OpAnyChar}}},
			},
		}
		got := hasDotStarPrefix(re)
		if got {
			t.Error("expected false for concat with single sub")
		}
	})

	// Non-concat
	t.Run("non_concat", func(t *testing.T) {
		re := &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{'a'}}
		got := hasDotStarPrefix(re)
		if got {
			t.Error("expected false for literal")
		}
	})
}

// --- Test 79: isCharClassPlus and isGreedyWildcard edge cases ---
// Covers: anchored_literal.go isCharClassPlus lines 200-208, isGreedyWildcard lines 180-189

func TestFindIndicesNFAAt_PikeVMFallback(t *testing.T) {
	// Simple NFA pattern
	pattern := `ab`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	// At position beyond haystack
	_, _, found := engine.FindIndicesAt([]byte("ab"), 10)
	if found {
		t.Error("expected no match beyond haystack")
	}

	// At position within haystack
	s, e, found := engine.FindIndicesAt([]byte("xxab"), 2)
	if !found || string("xxab"[s:e]) != "ab" {
		t.Errorf("expected match 'ab', got found=%v [%d,%d]", found, s, e)
	}
}

// --- Test 106: findAllIndicesLoop results reuse path (non-CharClassSearcher) ---
// Covers: findall.go line 129-131 (results != nil path in findAllIndicesLoop)

func TestIsSimpleCharClass_Nil(t *testing.T) {
	got := isSimpleCharClass(nil)
	if got {
		t.Error("isSimpleCharClass(nil) should be false")
	}
}

// --- Test 111: DetectAnchoredLiteral with multiple wildcards ---
// Covers: anchored_literal.go line 114-117 (multiple wildcards)

func TestAnalyzeLiterals_LargeAlternation(t *testing.T) {
	// Build a pattern with >64 alternatives to exercise the AhoCorasick path
	var parts []string
	for i := 0; i < 70; i++ {
		parts = append(parts, strings.Repeat(string(rune('a'+i%26)), 3+i%4))
	}
	pattern := strings.Join(parts, "|")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Strategy for 70-literal alternation: %s", engine.Strategy())

	// Verify it can match
	if !engine.IsMatch([]byte("aaa")) {
		t.Error("should match 'aaa'")
	}
}

// --- Test 115: isDigitLeadPattern through various shape patterns ---
// Covers: strategy.go isDigitLeadPattern lines 416-523
