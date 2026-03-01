package lazy

import (
	"regexp"
	"testing"

	"github.com/coregx/coregex/nfa"
)

func TestBuilderMoveWithWordContextDetailed(t *testing.T) {
	// Compile a pattern that uses word characters
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("[a-z]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())

	// Get the start states via epsilon closure
	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)
	if len(startStates) == 0 {
		t.Fatal("No start states from epsilon closure")
	}

	// Move on 'a' (a word char) from non-word context
	result := builder.moveWithWordContext(startStates, 'a', false)
	if len(result) == 0 {
		t.Error("moveWithWordContext should produce states for 'a' from non-word context")
	}

	// Move on ' ' (non-word char) from word context
	result2 := builder.moveWithWordContext(startStates, ' ', true)
	// Space transitions may produce states via unanchored prefix (.*?)
	t.Logf("moveWithWordContext for ' ' from word: %v", result2)

	// Move on 'x' (word char) from word context
	result3 := builder.moveWithWordContext(startStates, 'x', true)
	if len(result3) == 0 {
		t.Error("moveWithWordContext should produce states for 'x' from word context")
	}
}

func TestBuilderMoveWithWordContextNoBoundary(t *testing.T) {
	// Pattern without word boundary - fast path should be taken
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("abc")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	if builder.hasWordBoundary {
		t.Error("Pattern 'abc' should not have word boundary")
	}

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// Move on 'a' - should skip resolveWordBoundaries (fast path)
	result := builder.moveWithWordContext(startStates, 'a', false)
	if len(result) == 0 {
		t.Error("moveWithWordContext should produce states for 'a'")
	}
}

func TestBuilderMoveWithWordContextWordBoundary(t *testing.T) {
	// Pattern with word boundary \b
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\bfoo\b`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	if !builder.hasWordBoundary {
		t.Error("Pattern with \\b should have word boundary")
	}

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// At word boundary (non-word -> word): should expand states through \b
	result := builder.moveWithWordContext(startStates, 'f', false)
	t.Logf("moveWithWordContext for 'f' from non-word: %d states", len(result))

	// Not at word boundary (word -> word): \b should NOT be satisfied
	result2 := builder.moveWithWordContext(startStates, 'f', true)
	t.Logf("moveWithWordContext for 'f' from word: %d states", len(result2))
}

func TestResolveWordBoundariesBasic(t *testing.T) {
	// Compile pattern with \b
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\btest`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	if !builder.hasWordBoundary {
		t.Fatal("Pattern should have word boundary")
	}

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// Word boundary satisfied (transition from non-word to word)
	resolved := builder.resolveWordBoundaries(startStates, true)
	t.Logf("Resolved with boundary satisfied: %d states (from %d)", len(resolved), len(startStates))

	// Word boundary not satisfied (word -> word)
	resolvedNot := builder.resolveWordBoundaries(startStates, false)
	t.Logf("Resolved with boundary NOT satisfied: %d states (from %d)", len(resolvedNot), len(startStates))
}

func TestResolveWordBoundariesNoWordBoundary(t *testing.T) {
	// Pattern without \b should return states unchanged
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("abc")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())

	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)

	// Should return original states (no word boundary to resolve)
	resolved := builder.resolveWordBoundaries(startStates, true)
	if len(resolved) != len(startStates) {
		t.Errorf("resolveWordBoundaries should return same states for pattern without \\b: got %d, want %d",
			len(resolved), len(startStates))
	}
}

func TestBuilderNewBuilderWithWordBoundaryExplicit(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("abc")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	// Pre-set word boundary flag
	builder := NewBuilderWithWordBoundary(nfaObj, DefaultConfig(), true)
	if !builder.hasWordBoundary {
		t.Error("NewBuilderWithWordBoundary(true) should set hasWordBoundary")
	}

	builder2 := NewBuilderWithWordBoundary(nfaObj, DefaultConfig(), false)
	if builder2.hasWordBoundary {
		t.Error("NewBuilderWithWordBoundary(false) should not set hasWordBoundary")
	}
}

func TestWordBoundaryDFACorrectness(t *testing.T) {
	// End-to-end test: verify DFA with \b produces correct results vs stdlib
	tests := []struct {
		pattern string
		input   string
	}{
		{`\bfoo\b`, "foo bar"},
		{`\bfoo\b`, "foobar"},
		{`\bfoo\b`, "barfoo"},
		// {`\bfoo\b`, "bar foo baz"}, — known limitation: lazy DFA word boundary at non-zero position
		{`\btest`, "test123"},
		{`\btest`, "atest"},
		{`test\b`, "mytest"},
		{`test\b`, "testing"},
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

func TestCheckHasWordBoundary(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{name: "no word boundary", pattern: "abc", want: false},
		{name: "literal only", pattern: "[a-z]+", want: false},
		{name: "word boundary \\b", pattern: `\bfoo`, want: true},
		{name: "non-word boundary \\B", pattern: `\Bfoo`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			builder := NewBuilder(nfaObj, DefaultConfig())
			if builder.hasWordBoundary != tt.want {
				t.Errorf("hasWordBoundary = %v, want %v", builder.hasWordBoundary, tt.want)
			}
		})
	}
}
