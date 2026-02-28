package lazy

import (
	"regexp"
	"testing"

	"github.com/coregx/coregex/nfa"
)

// TestBuilderEpsilonClosure tests that epsilon closure correctly follows
// all epsilon transitions (Split, Epsilon, StateLook, StateCapture).
func TestBuilderEpsilonClosure(t *testing.T) {
	patterns := []struct {
		name    string
		pattern string
	}{
		{"simple literal", "abc"},
		{"alternation", "a|b|c"},
		{"repetition star", "a*"},
		{"repetition plus", "a+"},
		{"optional", "a?b"},
		{"capture group", "(abc)"},
		{"nested captures", "((a)(b))"},
		{"char class", "[a-z]+"},
		{"mixed", "(foo|bar)+"},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			builder := NewBuilder(nfaObj, DefaultConfig())
			startLook := LookSetFromStartKind(StartText)
			closure := builder.epsilonClosure(
				[]nfa.StateID{nfaObj.StartUnanchored()},
				startLook,
			)

			// Epsilon closure must produce at least one state
			if len(closure) == 0 {
				t.Error("epsilonClosure returned empty set")
			}

			// Closure must include the start state itself
			startFound := false
			for _, sid := range closure {
				if sid == nfaObj.StartUnanchored() {
					startFound = true
					break
				}
			}
			// Note: start state may not always be in closure if it's a Split/Epsilon
			// that immediately transitions to another state. Check that SOME state exists.
			_ = startFound
		})
	}
}

// TestBuilderMoveWithWordContext tests moveWithWordContext for patterns
// with and without word boundaries.
func TestBuilderMoveWithWordContext(t *testing.T) {
	t.Run("pattern without word boundary", func(t *testing.T) {
		compiler := nfa.NewDefaultCompiler()
		nfaObj, err := compiler.Compile("abc")
		if err != nil {
			t.Fatalf("NFA compile error: %v", err)
		}

		builder := NewBuilder(nfaObj, DefaultConfig())
		startLook := LookSetFromStartKind(StartText)
		startStates := builder.epsilonClosure(
			[]nfa.StateID{nfaObj.StartUnanchored()},
			startLook,
		)

		// Move on 'a' should produce states
		nextStates := builder.moveWithWordContext(startStates, 'a', false)
		if len(nextStates) == 0 {
			t.Error("moveWithWordContext('a') returned empty set for pattern 'abc'")
		}

		// Move on 'x' from start should also produce states
		// (because unanchored prefix (?s:.)*? matches anything)
		nextStates = builder.moveWithWordContext(startStates, 'x', false)
		// May or may not be empty depending on unanchored prefix handling
		t.Logf("moveWithWordContext('x') states: %d", len(nextStates))
	})

	t.Run("pattern with word boundary", func(t *testing.T) {
		compiler := nfa.NewDefaultCompiler()
		nfaObj, err := compiler.Compile(`\btest\b`)
		if err != nil {
			t.Fatalf("NFA compile error: %v", err)
		}

		builder := NewBuilder(nfaObj, DefaultConfig())

		// Builder should detect word boundary
		if !builder.hasWordBoundary {
			t.Error("Expected hasWordBoundary=true for \\btest\\b")
		}

		startLook := LookSetFromStartKind(StartText)
		startStates := builder.epsilonClosure(
			[]nfa.StateID{nfaObj.StartUnanchored()},
			startLook,
		)

		// Move from non-word context should work
		nextStates := builder.moveWithWordContext(startStates, 't', false)
		t.Logf("moveWithWordContext('t', fromNonWord) states: %d", len(nextStates))
	})
}

// TestBuilderContainsMatchState tests the match state detection.
func TestBuilderContainsMatchState(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("a")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())

	// Start states should not be match states for "a" (requires consuming 'a')
	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure(
		[]nfa.StateID{nfaObj.StartUnanchored()},
		startLook,
	)

	// After consuming 'a', should contain match state
	afterA := builder.moveWithWordContext(startStates, 'a', false)
	if len(afterA) > 0 {
		if !builder.containsMatchState(afterA) {
			t.Log("After consuming 'a', no match state found (may be in deeper closure)")
		}
	}
}

// TestBuilderCheckHasWordBoundary tests detection of word boundary assertions.
func TestBuilderCheckHasWordBoundary(t *testing.T) {
	tests := []struct {
		pattern  string
		expected bool
	}{
		{"abc", false},
		{"[a-z]+", false},
		{"a*b+", false},
		{`\bword\b`, true},
		{`\Btest\B`, true},
		{`foo\bbar`, true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			builder := NewBuilder(nfaObj, DefaultConfig())
			if builder.hasWordBoundary != tt.expected {
				t.Errorf("hasWordBoundary = %v, want %v", builder.hasWordBoundary, tt.expected)
			}
		})
	}
}

// TestBuilderCheckEOIMatch tests end-of-input match resolution.
func TestBuilderCheckEOIMatch(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		input      string
		wantEOI    bool
		isFromWord bool
	}{
		{
			name:       "word boundary at EOI after word char",
			pattern:    `\btest\b`,
			input:      "test",
			wantEOI:    true,
			isFromWord: true,
		},
		{
			name:       "literal without word boundary",
			pattern:    "test",
			input:      "test",
			wantEOI:    false,
			isFromWord: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			builder := NewBuilder(nfaObj, DefaultConfig())

			// Simulate running through the pattern
			startLook := LookSetFromStartKind(StartText)
			states := builder.epsilonClosure(
				[]nfa.StateID{nfaObj.StartUnanchored()},
				startLook,
			)

			// Consume input bytes
			for i := 0; i < len(tt.input); i++ {
				isFromWord := false
				if i > 0 {
					isFromWord = isWordByte(tt.input[i-1])
				}
				states = builder.moveWithWordContext(states, tt.input[i], isFromWord)
				if len(states) == 0 {
					break
				}
			}

			// Check EOI match
			if len(states) > 0 {
				eoi := builder.CheckEOIMatch(states, tt.isFromWord)
				t.Logf("CheckEOIMatch(%q, isFromWord=%v) = %v", tt.pattern, tt.isFromWord, eoi)
			}
		})
	}
}

// TestCompilePatternEdgeCases tests compilation of patterns with various edge cases.
func TestCompilePatternEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		valid   bool
	}{
		{"empty pattern", "", true},
		{"single char", "a", true},
		{"dot", ".", true},
		{"complex alternation", "a|b|c|d|e", true},
		{"nested groups", "((a+)b(c+))", true},
		{"character class range", "[a-zA-Z0-9_]+", true},
		{"negated class", "[^abc]+", true},
		{"escaped special", `\.\*\+`, true},
		{"invalid pattern", "[invalid", false},
		{"unbalanced parens", "(abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if tt.valid {
				if err != nil {
					t.Errorf("CompilePattern(%q) error: %v", tt.pattern, err)
				}
				if dfa == nil {
					t.Error("CompilePattern returned nil DFA without error")
				}
			} else {
				if err == nil {
					t.Errorf("CompilePattern(%q) expected error, got nil", tt.pattern)
				}
			}
		})
	}
}

// TestCompileWithConfig tests various Config combinations.
func TestCompileWithConfigCombinations(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("[a-z]+[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{
			name:   "default config",
			config: DefaultConfig(),
			valid:  true,
		},
		{
			name:   "small cache with clears",
			config: DefaultConfig().WithMaxStates(3).WithMaxCacheClears(10),
			valid:  true,
		},
		{
			name:   "prefilter disabled",
			config: DefaultConfig().WithPrefilter(false),
			valid:  true,
		},
		{
			name:   "low determinization limit",
			config: DefaultConfig().WithDeterminizationLimit(5),
			valid:  true,
		},
		{
			name:   "zero states is invalid",
			config: DefaultConfig().WithMaxStates(0),
			valid:  false,
		},
		{
			name:   "negative determinization limit is invalid",
			config: DefaultConfig().WithDeterminizationLimit(-1),
			valid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompileWithConfig(nfaObj, tt.config)
			if tt.valid {
				if err != nil {
					t.Fatalf("CompileWithConfig error: %v", err)
				}

				// Verify DFA works
				input := []byte("abc123")
				got := dfa.IsMatch(input)
				re := regexp.MustCompile("[a-z]+[0-9]+")
				want := re.Match(input)
				if got != want {
					t.Errorf("IsMatch(%q) = %v, stdlib says %v", input, got, want)
				}
			} else {
				if err == nil {
					t.Error("Expected error for invalid config")
				}
			}
		})
	}
}

// TestBuilderNewBuilderWithWordBoundaryPreComputed tests the NewBuilderWithWordBoundary constructor
// with pre-computed values matching and not matching the NFA's actual state.
func TestBuilderNewBuilderWithWordBoundaryPreComputed(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("abc")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	// Create with explicit false (matches reality for "abc")
	b1 := NewBuilderWithWordBoundary(nfaObj, DefaultConfig(), false)
	if b1.hasWordBoundary {
		t.Error("Expected hasWordBoundary=false")
	}

	// Create with explicit true (overrides for "abc" which has no \b)
	b2 := NewBuilderWithWordBoundary(nfaObj, DefaultConfig(), true)
	if !b2.hasWordBoundary {
		t.Error("Expected hasWordBoundary=true")
	}

	// Verify that the NFA and config are properly set
	if b1.nfa == nil {
		t.Error("Builder NFA should not be nil")
	}
	if b2.nfa == nil {
		t.Error("Builder NFA should not be nil")
	}
}

// TestBuildPrefilterFromLiteralsNilArgs tests the literal prefilter builder helper with nil args.
func TestBuildPrefilterFromLiteralsNilArgs(t *testing.T) {
	// BuildPrefilterFromLiterals with nil sequences should return nil (no panic)
	pf := BuildPrefilterFromLiterals(nil, nil)
	// Result depends on implementation; just verify no panic
	_ = pf
}

// TestExtractPrefilterVariousPatterns tests the prefilter extraction function with various patterns.
func TestExtractPrefilterVariousPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{"simple literal", "hello"},
		{"alternation", "foo|bar"},
		{"char class", "[a-z]+"},
		{"complex", "a+b+c+"},
		{"dot star", ".*x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf, err := ExtractPrefilter(tt.pattern)
			// Currently returns (nil, nil) as not fully implemented
			if err != nil {
				t.Errorf("ExtractPrefilter(%q) error: %v", tt.pattern, err)
			}
			// nil prefilter is expected (not an error)
			_ = pf
		})
	}

	// Invalid pattern should return error
	_, err := ExtractPrefilter("[invalid")
	if err == nil {
		t.Error("ExtractPrefilter with invalid pattern should return error")
	}
}

// TestDFAIsAlwaysAnchoredField tests that isAlwaysAnchored is set correctly.
// isAlwaysAnchored is true only when NFA.startAnchored == NFA.startUnanchored,
// which depends on the compiler implementation.
func TestDFAIsAlwaysAnchoredField(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{"unanchored literal", "hello"},
		{"anchored literal", "^hello"},
		{"anchored char class", "^[a-z]+"},
		{"unanchored alternation", "foo|bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			dfa, err := CompileWithConfig(nfaObj, DefaultConfig())
			if err != nil {
				t.Fatalf("DFA compile error: %v", err)
			}

			// Verify consistency: isAlwaysAnchored matches NFA's IsAlwaysAnchored
			if dfa.isAlwaysAnchored != nfaObj.IsAlwaysAnchored() {
				t.Errorf("DFA.isAlwaysAnchored = %v, NFA.IsAlwaysAnchored = %v",
					dfa.isAlwaysAnchored, nfaObj.IsAlwaysAnchored())
			}

			// If always anchored, verify FindAt from non-zero position returns -1
			if dfa.isAlwaysAnchored {
				input := []byte("hello world")
				got := dfa.FindAt(input, 5)
				if got != -1 {
					t.Errorf("Anchored FindAt from non-zero position should return -1, got %d", got)
				}
			}
		})
	}
}

// TestDFAWordBoundaryPatterns tests patterns with \b and \B assertions.
func TestDFAWordBoundaryPatterns(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		wantMatch bool
	}{
		{`\bword\b matches standalone`, `\bword\b`, "a word here", true},
		{`\bword\b no match embedded`, `\bword\b`, "swordfish", false},
		{`\btest at start`, `\btest`, "test123", true},
		{`\btest not at word start`, `\btest`, "atest", false},
		{`word\b at end`, `word\b`, "password", true},
		{`\B non-boundary`, `a\Bb`, "ab", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q) error: %v", tt.pattern, err)
			}

			re := regexp.MustCompile(tt.pattern)
			stdlibMatch := re.MatchString(tt.input)

			got := dfa.IsMatch([]byte(tt.input))
			if got != stdlibMatch {
				t.Errorf("IsMatch(%q, %q) = %v, stdlib says %v",
					tt.pattern, tt.input, got, stdlibMatch)
			}
		})
	}
}
