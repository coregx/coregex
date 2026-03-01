package lazy

import (
	"regexp"
	"testing"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// buildMemmemPrefilter creates a memmem prefilter for a literal string using the public API.
func buildMemmemPrefilter(needle string) prefilter.Prefilter {
	seq := literal.NewSeq(literal.NewLiteral([]byte(needle), true))
	return prefilter.NewBuilder(seq, nil).Build()
}

// buildMemchrPrefilter creates a memchr prefilter for a single byte using the public API.
func buildMemchrPrefilter(b byte) prefilter.Prefilter {
	seq := literal.NewSeq(literal.NewLiteral([]byte{b}, false))
	return prefilter.NewBuilder(seq, nil).Build()
}

// TestDFAWithPrefilterFind tests DFA search with an external prefilter.
// Note: For a complete prefilter, DFA.Find returns the start position of the match
// (as returned by the prefilter), not the end position.
func TestDFAWithPrefilterFind(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		wantPos int
	}{
		{
			name:    "literal prefilter match",
			pattern: "hello",
			input:   "say hello world",
			wantPos: 4, // start of "hello" (complete prefilter returns start)
		},
		{
			name:    "literal prefilter no match",
			pattern: "xyz",
			input:   "say hello world",
			wantPos: -1,
		},
		{
			name:    "literal at start",
			pattern: "say",
			input:   "say hello world",
			wantPos: 0, // start of "say" (complete prefilter returns start)
		},
		{
			name:    "literal at end",
			pattern: "world",
			input:   "say hello world",
			wantPos: 10, // start of "world" (complete prefilter returns start)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			pf := buildMemmemPrefilter(tt.pattern)
			if pf == nil {
				t.Skip("could not build prefilter")
			}

			config := DefaultConfig()
			dfa, err := CompileWithPrefilter(nfaObj, config, pf)
			if err != nil {
				t.Fatalf("CompileWithPrefilter error: %v", err)
			}

			got := dfa.Find([]byte(tt.input))
			if got != tt.wantPos {
				t.Errorf("Find(%q) = %d, want %d", tt.input, got, tt.wantPos)
			}
		})
	}
}

// TestDFAWithPrefilterIsMatch tests boolean match with prefilter.
func TestDFAWithPrefilterIsMatch(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		wantMatch bool
	}{
		{"match found", "hello", "say hello world", true},
		{"no match", "xyz", "say hello world", false},
		{"empty input", "hello", "", false},
		{"match at start", "say", "say hello world", true},
		{"match at end", "world", "say hello world", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			pf := buildMemmemPrefilter(tt.pattern)
			if pf == nil {
				t.Skip("could not build prefilter")
			}

			config := DefaultConfig()
			dfa, err := CompileWithPrefilter(nfaObj, config, pf)
			if err != nil {
				t.Fatalf("CompileWithPrefilter error: %v", err)
			}

			got := dfa.IsMatch([]byte(tt.input))
			if got != tt.wantMatch {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.wantMatch)
			}
		})
	}
}

// TestDFAWithPrefilterCorrectnessVsStdlib compares prefilter-assisted DFA
// search results against stdlib regexp to validate correctness.
func TestDFAWithPrefilterCorrectnessVsStdlib(t *testing.T) {
	patterns := []string{
		"hello",
		"test",
		"world",
		"foo",
	}

	inputs := []string{
		"hello world",
		"test foo bar",
		"say hello test world",
		"no match for foo here is foo",
		"xyz",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			compiler := nfa.NewDefaultCompiler()
			nfaObj, err := compiler.Compile(pattern)
			if err != nil {
				t.Fatalf("NFA compile error: %v", err)
			}

			pf := buildMemmemPrefilter(pattern)
			if pf == nil {
				t.Skip("could not build prefilter")
			}

			config := DefaultConfig()
			dfa, err := CompileWithPrefilter(nfaObj, config, pf)
			if err != nil {
				t.Fatalf("CompileWithPrefilter error: %v", err)
			}

			re := regexp.MustCompile(pattern)

			for _, input := range inputs {
				// IsMatch
				dfaMatch := dfa.IsMatch([]byte(input))
				stdlibMatch := re.MatchString(input)
				if dfaMatch != stdlibMatch {
					t.Errorf("IsMatch(%q, %q): DFA=%v, stdlib=%v",
						pattern, input, dfaMatch, stdlibMatch)
				}
			}
		})
	}
}

// TestDFAWithCompletePrefilter tests behavior when prefilter.IsComplete() returns true.
func TestDFAWithCompletePrefilter(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("hello")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	pf := buildMemmemPrefilter("hello")
	if pf == nil {
		t.Skip("could not build prefilter")
	}

	config := DefaultConfig()
	dfa, err := CompileWithPrefilter(nfaObj, config, pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	// Complete prefilter: Find uses prefilter directly
	input := []byte("say hello there hello again")
	got := dfa.Find(input)
	if got < 0 {
		t.Errorf("Find with complete prefilter returned %d, want match", got)
	}
}

// TestDFAWithPrefilterFindAt tests FindAt with prefilter starting from non-zero position.
func TestDFAWithPrefilterFindAt(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("hello")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	pf := buildMemmemPrefilter("hello")
	if pf == nil {
		t.Skip("could not build prefilter")
	}

	config := DefaultConfig()
	dfa, err := CompileWithPrefilter(nfaObj, config, pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	input := []byte("hello hello")

	// FindAt from position 0 should find first "hello"
	got := dfa.FindAt(input, 0)
	if got < 0 {
		t.Errorf("FindAt(0) = %d, want match", got)
	}

	// FindAt from position 6 should find second "hello"
	got = dfa.FindAt(input, 6)
	if got < 0 {
		t.Errorf("FindAt(6) = %d, want match", got)
	}

	// FindAt past all matches
	got = dfa.FindAt(input, 11)
	if got != -1 {
		t.Errorf("FindAt(11) = %d, want -1", got)
	}
}

// TestDFAWithPrefilterMultipleCandidates tests that the prefilter-assisted search
// correctly handles multiple candidate positions where only some verify.
func TestDFAWithPrefilterMultipleCandidates(t *testing.T) {
	// Pattern "a+b" with prefilter for "a" (memchr 'a')
	// Prefilter finds 'a' candidates, then DFA verifies a+b from there
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("a+b")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	// Use memmem for "ab" as prefilter — an incomplete prefilter for "a+b"
	pf := buildMemmemPrefilter("ab")
	if pf == nil {
		t.Skip("could not build prefilter")
	}

	config := DefaultConfig()
	dfa, err := CompileWithPrefilter(nfaObj, config, pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	tests := []struct {
		name      string
		input     string
		wantFound bool
	}{
		{"match after a's", "xxaaabxx", true},
		{"no b at all", "xaaax", false},
		{"ab at start", "abxxx", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dfa.Find([]byte(tt.input))
			gotFound := got >= 0
			if gotFound != tt.wantFound {
				t.Errorf("Find(%q) = %d, wantFound = %v", tt.input, got, tt.wantFound)
			}
		})
	}
}

// TestDFAWithPrefilterCacheClearing tests that prefilter-assisted search
// works correctly even when DFA cache is cleared during verification.
func TestDFAWithPrefilterCacheClearing(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("a+b+c+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	// Use memchr for 'a' as prefilter
	pf := buildMemchrPrefilter('a')
	if pf == nil {
		t.Skip("could not build prefilter")
	}

	config := DefaultConfig().WithMaxStates(5).WithMaxCacheClears(3)
	dfa, err := CompileWithPrefilter(nfaObj, config, pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	// This input should trigger cache pressure during DFA verification
	input := []byte("xxxaaabbbcccxxx")
	got := dfa.Find(input)
	if got == -1 {
		t.Error("Find should find match even with cache clearing")
	}

	// Also verify IsMatch
	if !dfa.IsMatch(input) {
		t.Error("IsMatch should return true even with cache clearing")
	}
}
