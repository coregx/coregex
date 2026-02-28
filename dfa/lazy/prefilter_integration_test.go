package lazy

import (
	"testing"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// buildDFAWithPrefilter creates a DFA with a prefilter from a single literal prefix.
func buildDFAWithPrefilter(t *testing.T, pattern string, prefix string) *DFA {
	t.Helper()
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(pattern)
	if err != nil {
		t.Fatalf("NFA compile %q error: %v", pattern, err)
	}

	// Build prefilter from prefix literal
	seq := literal.NewSeq(literal.NewLiteral([]byte(prefix), len(prefix) == len(pattern)))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skipf("no prefilter built for prefix %q", prefix)
	}

	dfa, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}
	return dfa
}

func TestIsMatchWithPrefilterComplete(t *testing.T) {
	// When prefilter IsComplete(), a prefilter hit is sufficient
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("hello")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	// Create a complete prefilter (exact literal match)
	seq := literal.NewSeq(literal.NewLiteral([]byte("hello"), true))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	dfa, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "match found", input: "say hello world", want: true},
		{name: "no match", input: "goodbye world", want: false},
		{name: "empty input", input: "", want: false},
		{name: "exact match", input: "hello", want: true},
		{name: "multiple occurrences", input: "hello hello hello", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dfa.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) with complete prefilter = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsMatchWithPrefilterIncomplete(t *testing.T) {
	// Incomplete prefilter needs DFA verification after candidate found
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("hello[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	// Create incomplete prefilter (only matches "h" prefix)
	seq := literal.NewSeq(literal.NewLiteral([]byte("h"), false))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	dfa, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "full match", input: "hello123", want: true},
		{name: "prefilter hit but no full match", input: "happy days", want: false},
		{name: "no prefilter hit", input: "goodbye", want: false},
		{name: "match in middle", input: "say hello456 end", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dfa.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) with incomplete prefilter = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFindWithPrefilterComplete(t *testing.T) {
	// Complete prefilter: Find returns prefilter position directly
	dfa := buildDFAWithPrefilter(t, "hello", "hello")

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "match at start", input: "hello world", want: 0},
		{name: "match in middle", input: "say hello", want: 4},
		{name: "no match", input: "goodbye world", want: -1},
		{name: "empty input", input: "", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dfa.Find([]byte(tt.input))
			// Find returns end position, but with complete prefilter it returns prefilter.Find
			// which returns start position of the needle
			if tt.want == -1 && got != -1 {
				t.Errorf("Find(%q) = %d, want no match (-1)", tt.input, got)
			}
			if tt.want != -1 && got == -1 {
				t.Errorf("Find(%q) = -1, want match", tt.input)
			}
		})
	}
}

func TestFindWithPrefilterIncomplete(t *testing.T) {
	// Incomplete prefilter needs DFA verification
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("foo[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	seq := literal.NewSeq(literal.NewLiteral([]byte("f"), false))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	dfa, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{name: "full match", input: "foo123", wantMatch: true},
		{name: "prefilter hit no full match", input: "fancy bar", wantMatch: false},
		{name: "no prefilter hit", input: "bar baz", wantMatch: false},
		{name: "match after false start", input: "fizz foo456 end", wantMatch: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dfa.Find([]byte(tt.input))
			gotMatch := got != -1
			if gotMatch != tt.wantMatch {
				t.Errorf("Find(%q) match = %v (pos=%d), want match = %v",
					tt.input, gotMatch, got, tt.wantMatch)
			}
		})
	}
}

func TestFindWithPrefilterMultipleCandidates(t *testing.T) {
	// Test that prefilter correctly skips false candidates
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("ab[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	seq := literal.NewSeq(literal.NewLiteral([]byte("a"), false))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	dfa, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	// Multiple 'a' positions but only one leads to full match
	input := []byte("ax ay az ab123 end")
	got := dfa.Find(input)
	if got == -1 {
		t.Error("Find should match 'ab123'")
	}

	// No full match among candidates
	input2 := []byte("ax ay az")
	got2 := dfa.Find(input2)
	if got2 != -1 {
		t.Errorf("Find should return -1 for no full match, got %d", got2)
	}
}

func TestDFAWithPrefilterNoPrefilter(t *testing.T) {
	// DFA without prefilter should still work correctly
	dfa, err := CompilePattern("hello")
	if err != nil {
		t.Fatalf("CompilePattern error: %v", err)
	}

	// IsMatch and Find should work without prefilter
	if !dfa.IsMatch([]byte("hello world")) {
		t.Error("IsMatch should be true")
	}
	if dfa.IsMatch([]byte("goodbye")) {
		t.Error("IsMatch should be false")
	}

	got := dfa.Find([]byte("say hello"))
	if got == -1 {
		t.Error("Find should match")
	}
}

func TestBuildPrefilterFromLiteralsIntegration(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		wantNil bool
	}{
		{name: "single byte prefix", prefix: "x", wantNil: false},
		{name: "multi byte prefix", prefix: "hello", wantNil: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := literal.NewSeq(literal.NewLiteral([]byte(tt.prefix), true))
			pf := BuildPrefilterFromLiterals(seq, nil)
			if tt.wantNil && pf != nil {
				t.Error("Expected nil prefilter")
			}
			if !tt.wantNil && pf == nil {
				t.Error("Expected non-nil prefilter")
			}
		})
	}
}

func TestExtractPrefilterIntegration(t *testing.T) {
	// ExtractPrefilter currently returns nil (not implemented yet)
	pf, err := ExtractPrefilter("hello")
	if err != nil {
		t.Errorf("ExtractPrefilter should not error: %v", err)
	}
	// Currently returns nil (documented behavior)
	if pf != nil {
		t.Log("ExtractPrefilter returned non-nil prefilter (implementation may have changed)")
	}
}

func TestExtractPrefilterInvalidPattern(t *testing.T) {
	_, err := ExtractPrefilter("[invalid")
	if err == nil {
		t.Error("ExtractPrefilter should error on invalid pattern")
	}
}
