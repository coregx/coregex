package meta

import (
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
)

// helper: compileForReason compiles a pattern and returns the NFA, parsed AST, and literals.
func compileForReason(t *testing.T, pattern string, config Config) (*nfa.NFA, *syntax.Regexp, *literal.Seq) {
	t.Helper()
	compiler := nfa.NewDefaultCompiler()
	compiledNFA, err := compiler.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}

	extractor := literal.New(literal.DefaultConfig())
	literals := extractor.ExtractPrefixes(re)

	return compiledNFA, re, literals
}

// TestUseDFAStrategyFind tests Find through the UseDFA dispatch path.
// UseDFA is selected for patterns with good literals and larger NFA (>20 states),
// or NFA >100 states.
func TestUseDFAStrategyFind(t *testing.T) {
	// Pattern with >20 states AND good prefix literal that triggers UseDFA:
	// "(foo|foobar)\\d+" has good prefix "foo" and complex structure
	// We need a pattern large enough (>20 NFA states) with good literals
	pattern := `(ABXBYXCX|CDYDZYDE)\d+\w+`

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	// If pattern didn't trigger UseDFA, skip (the exact threshold may vary)
	if engine.Strategy() != UseDFA {
		t.Logf("pattern %q uses strategy %s (not UseDFA), trying alternate", pattern, engine.Strategy())

		// Try a pattern that generates >100 NFA states (no good literals)
		bigPattern := strings.Repeat("(a|b|c|d|e|f|g|h)*", 4) + "z"
		engine, err = Compile(bigPattern)
		if err != nil {
			t.Fatal(err)
		}
		if engine.Strategy() != UseDFA {
			t.Skipf("Could not trigger UseDFA strategy (got %s)", engine.Strategy())
		}

		haystack := []byte(strings.Repeat("abcdefgh", 10) + "z")
		match := engine.Find(haystack)
		if match == nil {
			t.Error("Find should return a match")
		}
		return
	}

	tests := []struct {
		name      string
		haystack  string
		wantFound bool
	}{
		{"match", "test ABXBYXCX123abc end", true},
		{"no_match", "nothing here", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := engine.Find([]byte(tt.haystack))
			found := match != nil
			if found != tt.wantFound {
				t.Errorf("Find(%q) found=%v, want %v (match=%v)", tt.haystack, found, tt.wantFound, match)
			}
		})
	}
}

// TestUseBothStrategyFind tests Find through the UseBoth dispatch path.
// UseBoth is selected for medium NFA (20-100 states) without literals or strong characteristics.
func TestUseBothStrategyFind(t *testing.T) {
	// Medium-sized NFA without good literals and not a simple char class
	// Use nested groups with multiple alternations
	pattern := `(a|b|c)(d|e|f)(g|h|i)(j|k|l)(m|n|o)(p|q|r)`

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoth {
		t.Skipf("pattern %q uses %s (not UseBoth)", pattern, engine.Strategy())
	}

	tests := []struct {
		name     string
		haystack string
		wantText string
	}{
		{"match", "xadgjmp!", "adgjmp"},
		{"no_match", "xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := engine.Find([]byte(tt.haystack))
			if tt.wantText == "" {
				if match != nil {
					t.Errorf("Find(%q) = %q, want nil", tt.haystack, match.String())
				}
			} else {
				if match == nil {
					t.Errorf("Find(%q) = nil, want %q", tt.haystack, tt.wantText)
				} else if match.String() != tt.wantText {
					t.Errorf("Find(%q) = %q, want %q", tt.haystack, match.String(), tt.wantText)
				}
			}
		})
	}
}

// TestUseBothFindAtAndFindIndicesAt tests FindAt and FindIndicesAt for UseBoth strategy.
func TestUseBothFindAtAndFindIndicesAt(t *testing.T) {
	pattern := `(a|b|c)(d|e|f)(g|h|i)(j|k|l)(m|n|o)(p|q|r)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoth {
		t.Skipf("pattern uses %s, not UseBoth", engine.Strategy())
	}

	haystack := []byte("xxadgjmpxxbehknqxx")

	// FindAt(0)
	m0 := engine.FindAt(haystack, 0)
	if m0 == nil || m0.String() != "adgjmp" {
		t.Errorf("FindAt(0) = %v, want adgjmp", m0)
	}

	// FindAt past first match
	m8 := engine.FindAt(haystack, m0.End())
	if m8 == nil || m8.String() != "behknq" {
		t.Errorf("FindAt(%d) = %v, want behknq", m0.End(), m8)
	}

	// FindIndicesAt
	start, end, found := engine.FindIndicesAt(haystack, 0)
	if !found || string(haystack[start:end]) != "adgjmp" {
		t.Errorf("FindIndicesAt(0) = %q, want adgjmp", string(haystack[start:end]))
	}

	start2, end2, found2 := engine.FindIndicesAt(haystack, end)
	if !found2 || string(haystack[start2:end2]) != "behknq" {
		t.Errorf("FindIndicesAt(%d) = %q, want behknq", end, string(haystack[start2:end2]))
	}

	// IsMatch
	if !engine.IsMatch(haystack) {
		t.Error("IsMatch should be true")
	}
	if engine.IsMatch([]byte("zzz")) {
		t.Error("IsMatch should be false for 'zzz'")
	}
}

// TestStrategyReasonFunc tests the StrategyReason function to cover its branches.
func TestStrategyReasonFunc(t *testing.T) {
	config := DefaultConfig()

	tests := []struct {
		name     string
		strategy Strategy
		pattern  string
	}{
		// Simple strategies with constant reasons
		{"both", UseBoth, `(a|b|c)(d|e|f)`},
		{"reverse_anchored", UseReverseAnchored, `test$`},
		{"reverse_suffix", UseReverseSuffix, `.*\.txt`},
		{"reverse_inner", UseReverseInner, `.*ERROR.*`},
		{"teddy", UseTeddy, "foo|bar|baz"},
		{"charclass", UseCharClassSearcher, `\w+`},
		{"composite", UseCompositeSearcher, `[a-z]+[0-9]+`},
		{"digit_prefilter", UseDigitPrefilter, `\d+\.\d+`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiledNFA, re, literals := compileForReason(t, tt.pattern, config)
			reason := StrategyReason(tt.strategy, compiledNFA, literals, config)
			_ = re // re is used by compileForReason
			if reason == "" {
				t.Error("StrategyReason returned empty string")
			}
		})
	}
}

// TestStrategyReasonComplexPaths tests strategyReasonComplex for
// context-dependent reason strings.
func TestStrategyReasonComplexPaths(t *testing.T) {
	config := DefaultConfig()

	// UseNFA with DFA disabled
	t.Run("nfa_dfa_disabled", func(t *testing.T) {
		disabledConfig := config
		disabledConfig.EnableDFA = false

		compiledNFA, _, literals := compileForReason(t, `a`, config)
		reason := StrategyReason(UseNFA, compiledNFA, literals, disabledConfig)
		if !strings.Contains(reason, "DFA disabled") {
			t.Errorf("expected 'DFA disabled' in reason, got %q", reason)
		}
	})

	// UseNFA with tiny NFA
	t.Run("nfa_tiny", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `a`, config)
		reason := StrategyReason(UseNFA, compiledNFA, literals, config)
		if !strings.Contains(reason, "tiny NFA") && !strings.Contains(reason, "no good literals") {
			t.Errorf("expected explanation for NFA choice, got %q", reason)
		}
	})

	// UseBoundedBacktracker with anchored pattern
	t.Run("bt_anchored", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `^(\d+)`, config)
		reason := StrategyReason(UseBoundedBacktracker, compiledNFA, literals, config)
		if !strings.Contains(reason, "anchored") {
			t.Errorf("expected 'anchored' in reason, got %q", reason)
		}
	})

	// UseBoundedBacktracker with non-anchored pattern
	t.Run("bt_nonanchored", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `(\w)+`, config)
		reason := StrategyReason(UseBoundedBacktracker, compiledNFA, literals, config)
		if !strings.Contains(reason, "character class") {
			t.Errorf("expected 'character class' in reason, got %q", reason)
		}
	})

	// Unknown strategy
	t.Run("unknown", func(t *testing.T) {
		compiledNFA, _, literals := compileForReason(t, `a`, config)
		reason := StrategyReason(Strategy(999), compiledNFA, literals, config)
		if reason != "unknown strategy" {
			t.Errorf("expected 'unknown strategy', got %q", reason)
		}
	})
}

// TestIsSafeForReverseSuffix tests isSafeForReverseSuffix on various pattern types.
func TestIsSafeForReverseSuffix(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`.*\.txt`, true},
		{`.*keyword.*`, true},
		{`\w+`, false},     // no dot-star
		{`^hello`, false},  // start-anchored
		{`hello$`, false},  // end-anchored
		{`foo|bar`, false}, // simple alternation
		// Note: isSafeForReverseSuffix does NOT check word boundaries
		// (that's done by hasWordBoundary in selectReverseStrategy)
		{`.*\btest\b`, true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}

			got := isSafeForReverseSuffix(re)
			if got != tt.want {
				t.Errorf("isSafeForReverseSuffix(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestHasDotStarPrefix tests hasDotStarPrefix on various patterns.
func TestHasDotStarPrefix(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`.*\.txt`, true},
		{`.*keyword.*`, true},
		{`.+\.txt`, false},
		{`\w+\.txt`, false},
		{`hello`, false},
		// ^.*\.txt: parser wraps as Concat(BeginText, Concat(.*, \.txt))
		// hasDotStarPrefix checks the top-level sub, which is BeginText, not .*
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}

			got := hasDotStarPrefix(re)
			if got != tt.want {
				t.Errorf("hasDotStarPrefix(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestContainsAnchor tests containsAnchor on various patterns.
func TestContainsAnchor(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`^hello`, true},
		{`hello$`, true},
		{`^hello$`, true},
		{`hello`, false},
		{`\d+`, false},
		{`(^hello|world)`, true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}

			got := containsAnchor(re)
			if got != tt.want {
				t.Errorf("containsAnchor(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestContainsWildcard tests containsWildcard on various patterns.
func TestContainsWildcard(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`.*\.txt`, true},
		{`.+test`, true},
		{`hello`, false},
		{`\d+`, false},
		{`[a-z]+`, false},
		// bare `.` is OpAnyCharNotNL, not OpStar/OpPlus wrapping it
		{`.`, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}

			got := containsWildcard(re)
			if got != tt.want {
				t.Errorf("containsWildcard(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestIsDigitRunSkipSafe tests isDigitRunSkipSafe on various patterns.
func TestIsDigitRunSkipSafe(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{`\d+`, true},
		{`[0-9]+`, true},
		{`\d*`, true},
		{`\d{1,3}`, false}, // bounded repetition
		{`\w+`, false},     // not digit class
		{`[a-z]+`, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatal(err)
			}

			got := isDigitRunSkipSafe(re)
			if got != tt.want {
				t.Errorf("isDigitRunSkipSafe(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}
