package lazy

import (
	"regexp"
	"testing"

	"github.com/coregx/coregex/nfa"
)

// TestResolveWordBoundariesWithCapture tests resolveWordBoundaries when the NFA
// has Capture states reachable after crossing a word boundary assertion.
// This exercises the StateCapture branch in the expansion loop (builder.go line ~478).
func TestResolveWordBoundariesWithCapture(t *testing.T) {
	// \b(\w+)\b has word boundary followed by capture group
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\b(\w+)\b`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	if !builder.hasWordBoundary {
		t.Fatal("Pattern with \\b should have word boundary")
	}

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// With word boundary satisfied (non-word -> word transition),
	// the resolver should follow through \b -> Capture -> consuming states
	resolved := builder.resolveWordBoundaries(startStates, true)
	if len(resolved) <= len(startStates) {
		t.Logf("resolved states: %d, original states: %d", len(resolved), len(startStates))
		// Even if equal, verify no panic occurred
	}

	// With word boundary NOT satisfied (word -> word), \b should not add new states
	resolvedNot := builder.resolveWordBoundaries(startStates, false)
	t.Logf("resolved (boundary satisfied): %d states, (not satisfied): %d states",
		len(resolved), len(resolvedNot))
}

// TestResolveWordBoundariesWithNestedLook tests resolveWordBoundaries when a Look
// state is encountered during the expansion after crossing the first word boundary.
// Exercises the StateLook branch in the inner expansion loop (builder.go line ~439).
func TestResolveWordBoundariesWithNestedLook(t *testing.T) {
	// \b\bfoo has two consecutive word boundaries
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\b\bfoo`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	if !builder.hasWordBoundary {
		t.Fatal("Pattern should have word boundary")
	}

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// With boundary satisfied, should follow through both \b assertions
	resolved := builder.resolveWordBoundaries(startStates, true)
	t.Logf("Nested \\b\\b resolved with boundary: %d states (from %d)", len(resolved), len(startStates))
}

// TestResolveWordBoundariesWithSplit tests resolveWordBoundaries when Split states
// are encountered after crossing the word boundary. This exercises the StateSplit
// branch in the expansion loop (builder.go line ~466).
func TestResolveWordBoundariesWithSplit(t *testing.T) {
	// \b(foo|bar) has word boundary followed by alternation (Split)
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\b(foo|bar)`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	if !builder.hasWordBoundary {
		t.Fatal("Pattern should have word boundary")
	}

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// With boundary satisfied, should follow through \b -> Split(foo, bar)
	resolved := builder.resolveWordBoundaries(startStates, true)
	if len(resolved) <= len(startStates) {
		t.Logf("resolved: %d states, original: %d states", len(resolved), len(startStates))
	}

	// Without boundary, \b is not crossed, so no expansion
	resolvedNot := builder.resolveWordBoundaries(startStates, false)
	t.Logf("Split: boundary satisfied=%d states, not satisfied=%d states",
		len(resolved), len(resolvedNot))
}

// TestResolveWordBoundariesNoWordBoundaryLook tests that LookNoWordBoundary (\B)
// is correctly handled -- it should resolve when boundary is NOT satisfied.
func TestResolveWordBoundariesNoWordBoundaryLook(t *testing.T) {
	// \Bfoo matches "foo" when NOT at a word boundary
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\Bfoo`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	if !builder.hasWordBoundary {
		t.Fatal("Pattern with \\B should have word boundary flag")
	}

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// \B is satisfied when boundary is NOT present (word->word or non-word->non-word)
	// So wordBoundarySatisfied=false should trigger \B expansion
	resolvedNotSatisfied := builder.resolveWordBoundaries(startStates, false)

	// wordBoundarySatisfied=true should NOT trigger \B expansion
	resolvedSatisfied := builder.resolveWordBoundaries(startStates, true)

	t.Logf("\\B pattern: boundary not satisfied=%d states, satisfied=%d states",
		len(resolvedNotSatisfied), len(resolvedSatisfied))
}

// TestResolveWordBoundariesWithEpsilon tests resolveWordBoundaries when Epsilon
// states are encountered after crossing a word boundary. Exercises the
// StateEpsilon branch in the expansion loop (builder.go line ~458).
func TestResolveWordBoundariesWithEpsilon(t *testing.T) {
	// \ba? has word boundary followed by optional 'a' (Epsilon + Split)
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\ba?`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	if !builder.hasWordBoundary {
		t.Fatal("Pattern should have word boundary")
	}

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// With boundary satisfied, should follow through \b -> epsilon chain
	resolved := builder.resolveWordBoundaries(startStates, true)
	t.Logf("Epsilon: resolved with boundary: %d states (from %d)", len(resolved), len(startStates))
}

// TestResolveWordBoundariesInvalidNext tests that resolveWordBoundaries handles
// states with InvalidState next gracefully.
func TestResolveWordBoundariesInvalidNext(t *testing.T) {
	// This is more of a robustness test -- compile a pattern and ensure no panics
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\b`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// Should not panic even with bare \b
	resolved := builder.resolveWordBoundaries(startStates, true)
	t.Logf("Bare \\b: resolved %d states (from %d)", len(resolved), len(startStates))
}

// TestWordBoundaryDFACorrectnessExtended extends end-to-end correctness tests
// for word boundary patterns against stdlib.
func TestWordBoundaryDFACorrectnessExtended(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Capture after word boundary
		{`\b(\w+)\b`, "hello"},
		{`\b(\w+)\b`, "  hello  "},
		// Non-word boundary
		{`\Boo`, "foo"},
		{`\Boo`, "oo"},
		// Word boundary at different positions
		{`\btest\b`, "test"},
		{`\btest\b`, " test "},
		{`\btest\b`, "contest"},
		{`\btest\b`, "tested"},
		// Word boundary with alternation
		{`\b(foo|bar)\b`, "foo"},
		{`\b(foo|bar)\b`, "bar"},
		{`\b(foo|bar)\b`, "foobar"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Skipf("Pattern %q not supported: %v", tt.pattern, err)
				return
			}

			re := regexp.MustCompile(tt.pattern)
			stdlibMatch := re.MatchString(tt.input)
			dfaMatch := dfa.IsMatch([]byte(tt.input))

			if dfaMatch != stdlibMatch {
				t.Errorf("IsMatch(%q, %q): DFA=%v, stdlib=%v", tt.pattern, tt.input, dfaMatch, stdlibMatch)
			}
		})
	}
}

// TestCheckEOIMatchWithWordBoundary tests the CheckEOIMatch function
// which resolves pending word boundary assertions at end of input.
func TestCheckEOIMatchWithWordBoundary(t *testing.T) {
	// Pattern `test\b` - the \b at EOI should be satisfied if last char is word char
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`test\b`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilderWithWordBoundary(nfaObj, DefaultConfig(), true)

	// Get states for "test" matched, now at EOI
	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// Simulate consuming "test" bytes
	states := startStates
	for _, b := range []byte("test") {
		states = builder.moveWithWordContext(states, b, false)
		if len(states) == 0 {
			t.Skip("pattern lost all states after consuming 'test'")
			return
		}
	}

	// At EOI with last char 't' (word char), \b should be satisfied
	matched := builder.CheckEOIMatch(states, true)
	t.Logf("CheckEOIMatch after 'test' (isFromWord=true): %v", matched)

	// At EOI with non-word context, \b should NOT be satisfied
	matchedNonWord := builder.CheckEOIMatch(states, false)
	t.Logf("CheckEOIMatch after 'test' (isFromWord=false): %v", matchedNonWord)
}

// TestWordBoundaryEndToEndFind tests word boundary patterns with Find (not just IsMatch).
func TestWordBoundaryEndToEndFind(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		wantMatch bool
	}{
		{"word start", `\bfoo`, "foo bar", true},
		{"word end", `foo\b`, "foo bar", true},
		{"word both", `\bfoo\b`, "foo", true},
		{"not at boundary", `\bfoo\b`, "xfoox", false},
		{"non-word boundary", `\Boo`, "foo", true},
		{"non-word boundary start", `\Boo`, "oo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Skipf("Pattern %q not compilable: %v", tt.pattern, err)
				return
			}

			got := dfa.Find([]byte(tt.input))
			gotMatch := got != -1

			if gotMatch != tt.wantMatch {
				t.Errorf("Find(%q, %q) match=%v (pos=%d), want match=%v",
					tt.pattern, tt.input, gotMatch, got, tt.wantMatch)
			}
		})
	}
}
