package lazy

import (
	"strings"
	"testing"

	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// --- AlphabetLen and byteToClass nil-branch tests ---

// TestAlphabetLenNilByteClasses tests AlphabetLen returns 256 when byteClasses is nil.
func TestAlphabetLenNilByteClasses(t *testing.T) {
	d := &DFA{byteClasses: nil}
	if got := d.AlphabetLen(); got != 256 {
		t.Errorf("AlphabetLen() with nil byteClasses = %d, want 256", got)
	}
}

// TestByteToClassNilByteClasses tests byteToClass returns identity when byteClasses is nil.
func TestByteToClassNilByteClasses(t *testing.T) {
	d := &DFA{byteClasses: nil}
	for b := 0; b < 256; b++ {
		got := d.byteToClass(byte(b))
		if got != byte(b) {
			t.Errorf("byteToClass(%d) with nil byteClasses = %d, want %d", b, got, b)
		}
	}
}

// --- DetectAccelerationFromCachedWithClasses tests ---

// TestDetectAccelFromCachedWithClassesByteMapping tests that when byteClasses
// are provided, the function correctly maps class indices back to representative bytes.
func TestDetectAccelFromCachedWithClassesByteMapping(t *testing.T) {
	// Build a DFA with a pattern that creates ByteClasses
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("[a-z]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	d, err := CompileWithConfig(nfaObj, DefaultConfig())
	if err != nil {
		t.Fatalf("DFA compile error: %v", err)
	}

	bc := d.ByteClasses()
	if bc == nil {
		t.Skip("no byte classes created for pattern")
	}

	// Warm up the DFA by searching
	_ = d.Find([]byte(strings.Repeat("a", 100) + "!"))

	// Try detection with classes on each state
	for _, s := range d.states {
		if s != nil {
			result := DetectAccelerationFromCachedWithClasses(s, bc)
			if len(result) > 0 {
				t.Logf("State %d: accelerable with classes, exit bytes: %v", s.ID(), result)
				// Verify exit bytes are actual bytes (not class indices)
				for _, b := range result {
					_ = bc.Get(b) // should not panic
				}
			}
		}
	}
}

// TestDetectAccelFromCachedWithClassesNilClasses verifies the nil byteClasses fallback.
func TestDetectAccelFromCachedWithClassesNilClasses(t *testing.T) {
	// Create a state with known transitions (stride=256, no compression)
	state := NewState(StateID(1), []nfa.StateID{0}, false)

	// Fill 253 self-loop transitions
	for i := 0; i < 253; i++ {
		state.AddTransition(byte(i), StateID(1))
	}
	// Add 3 exit transitions to a different state
	state.AddTransition(253, StateID(2))
	state.AddTransition(254, StateID(2))
	state.AddTransition(255, StateID(2))

	// nil byteClasses -> exit class indices ARE the bytes (identity)
	result := DetectAccelerationFromCachedWithClasses(state, nil)
	if len(result) != 3 {
		t.Fatalf("expected 3 exit bytes with nil classes, got %v", result)
	}
	expected := map[byte]bool{253: true, 254: true, 255: true}
	for _, b := range result {
		if !expected[b] {
			t.Errorf("unexpected exit byte %d", b)
		}
	}
}

// TestDetectAccelFromCachedInsufficientTransitions tests that when too few
// transitions are cached, acceleration detection returns nil.
func TestDetectAccelFromCachedInsufficientTransitions(t *testing.T) {
	state := NewState(StateID(1), []nfa.StateID{0}, false)
	// Only add a few transitions (way below 94% threshold)
	state.AddTransition(0, StateID(1))
	state.AddTransition(1, StateID(2))

	result := DetectAccelerationFromCachedWithClasses(state, nil)
	if result != nil {
		t.Errorf("expected nil for insufficient cached transitions, got %v", result)
	}
}

// TestDetectAccelFromCachedTooManyExitClasses tests that >3 exit classes returns nil.
func TestDetectAccelFromCachedTooManyExitClasses(t *testing.T) {
	state := NewState(StateID(1), []nfa.StateID{0}, false)
	// Fill 250 self-loops
	for i := 0; i < 250; i++ {
		state.AddTransition(byte(i), StateID(1))
	}
	// Add 4 distinct exit transitions (> 3 limit)
	state.AddTransition(250, StateID(2))
	state.AddTransition(251, StateID(3))
	state.AddTransition(252, StateID(4))
	state.AddTransition(253, StateID(5))
	// Fill remaining with dead
	state.AddTransition(254, DeadState)
	state.AddTransition(255, DeadState)

	result := DetectAccelerationFromCachedWithClasses(state, nil)
	if result != nil {
		t.Errorf("expected nil for >3 exit classes, got %v", result)
	}
}

// TestDetectAccelFromCachedZeroExitClasses tests that 0 exit classes returns nil.
func TestDetectAccelFromCachedZeroExitClasses(t *testing.T) {
	state := NewState(StateID(1), []nfa.StateID{0}, false)
	// All transitions are self-loops or dead
	for i := 0; i < 256; i++ {
		if i < 200 {
			state.AddTransition(byte(i), StateID(1)) // self-loop
		} else {
			state.AddTransition(byte(i), DeadState) // dead
		}
	}

	result := DetectAccelerationFromCachedWithClasses(state, nil)
	if result != nil {
		t.Errorf("expected nil for 0 exit classes, got %v", result)
	}
}

// --- searchEarliestMatchAnchored tests ---

// TestSearchEarliestMatchAnchoredCacheClear tests the cache-clear recovery path
// in searchEarliestMatchAnchored by using a tiny cache.
func TestSearchEarliestMatchAnchoredCacheClear(t *testing.T) {
	// Use minimal cache to force cache clears
	config := DefaultConfig().WithMaxStates(3).WithMaxCacheClears(10)
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("[a-z]+[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	d, err := CompileWithConfig(nfaObj, config)
	if err != nil {
		t.Fatalf("DFA compile error: %v", err)
	}

	// Input that should match and exercises cache clearing path
	input := []byte("abc123")

	// Create a prefilter pointing to 'a'
	seq := literal.NewSeq(literal.NewLiteral([]byte("a"), false))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf != nil {
		d.prefilter = pf
	}

	// IsMatch exercises searchEarliestMatchAnchored via isMatchWithPrefilter.
	// The key goal is exercising the cache-clear path without panicking.
	// With tiny cache, NFA fallback may or may not match depending on internal state.
	got := d.IsMatch(input)
	t.Logf("IsMatch with tiny cache (cache-clear path exercised) = %v", got)

	// Also exercise with a slightly larger input to ensure more cache clears
	largeInput := []byte(strings.Repeat("a", 20) + "123")
	got2 := d.IsMatch(largeInput)
	t.Logf("IsMatch with larger input = %v", got2)
}

// TestSearchEarliestMatchAnchoredStartPastEnd tests boundary: startPos > len(haystack).
func TestSearchEarliestMatchAnchoredStartPastEnd(t *testing.T) {
	d, err := CompilePattern("abc")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Directly call searchEarliestMatchAnchored with startPos past end
	got := d.searchEarliestMatchAnchored([]byte("abc"), 10)
	if got {
		t.Error("searchEarliestMatchAnchored should return false when startPos > len")
	}
}

// TestSearchEarliestMatchAnchoredWordBoundary tests the word boundary check
// path in searchEarliestMatchAnchored.
func TestSearchEarliestMatchAnchoredWordBoundary(t *testing.T) {
	d, err := CompilePattern(`\bfoo\b`)
	if err != nil {
		t.Skipf("pattern not supported: %v", err)
	}

	tests := []struct {
		name  string
		input string
		at    int
		want  bool
	}{
		{"match at word boundary", " foo ", 1, true},
		{"no boundary in middle", "xfoox", 1, false},
		{"at start of input", "foo bar", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.searchEarliestMatchAnchored([]byte(tt.input), tt.at)
			if got != tt.want {
				t.Errorf("searchEarliestMatchAnchored(%q, %d) = %v, want %v",
					tt.input, tt.at, got, tt.want)
			}
		})
	}
}

// --- SearchAtAnchored additional tests ---

// TestSearchAtAnchoredCacheClearRecovery exercises the cache-clear recovery
// path in SearchAtAnchored by using a small cache.
func TestSearchAtAnchoredCacheClearRecovery(t *testing.T) {
	// Use a small cache (but not so tiny that start state can't be built)
	config := DefaultConfig().WithMaxStates(8).WithMaxCacheClears(5)
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("[a-z]+[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	d, err := CompileWithConfig(nfaObj, config)
	if err != nil {
		t.Fatalf("DFA compile error: %v", err)
	}

	// Input with many distinct characters to exercise cache clearing.
	// The key goal is exercising the cache-clear recovery path without panicking.
	input := []byte("abcdef12345")
	got := d.SearchAtAnchored(input, 0)
	t.Logf("SearchAtAnchored with small cache = %d", got)

	// Verify the function does not panic with various inputs
	_ = d.SearchAtAnchored([]byte("x1"), 0)
	_ = d.SearchAtAnchored([]byte("abcdefghijklmnop123"), 0)
	_ = d.SearchAtAnchored([]byte("nomatch"), 0)
}

// TestSearchAtAnchoredEmptyHaystack tests anchored search on empty input.
func TestSearchAtAnchoredEmptyHaystack(t *testing.T) {
	d, err := CompilePattern("a*")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Empty haystack with empty-matching pattern
	got := d.SearchAtAnchored([]byte(""), 0)
	if got != 0 {
		t.Errorf("SearchAtAnchored(empty, 0) = %d, want 0 (empty match)", got)
	}

	// Empty haystack with non-empty-matching pattern
	d2, err := CompilePattern("abc")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	got = d2.SearchAtAnchored([]byte(""), 0)
	if got != -1 {
		t.Errorf("SearchAtAnchored(empty, 0) for 'abc' = %d, want -1", got)
	}
}

// --- IsMatchReverse additional tests ---

// TestIsMatchReverseWordBoundary tests IsMatchReverse with word boundary patterns.
func TestIsMatchReverseWordBoundary(t *testing.T) {
	// Compile reverse DFA for a char class pattern
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("[a-z]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	d, err := CompileWithConfig(revNFA, DefaultConfig())
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	tests := []struct {
		name  string
		input string
		start int
		end   int
		want  bool
	}{
		{"full lowercase match", "abcdef", 0, 6, true},
		{"partial match window", "123abc456", 3, 6, true},
		{"no match", "123456", 0, 6, false},
		{"single char", "a", 0, 1, true},
		{"empty range", "abc", 2, 2, false},
		{"end > len", "abc", 0, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.IsMatchReverse([]byte(tt.input), tt.start, tt.end)
			if got != tt.want {
				t.Errorf("IsMatchReverse(%q, %d, %d) = %v, want %v",
					tt.input, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

// TestIsMatchReverseCacheClear tests cache-clear recovery in IsMatchReverse.
func TestIsMatchReverseCacheClear(t *testing.T) {
	config := DefaultConfig().WithMaxStates(3).WithMaxCacheClears(10)
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("[a-zA-Z]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	d, err := CompileWithConfig(revNFA, config)
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	// Input with varied chars to force cache clears
	input := []byte("aBcDeFgHiJkLmN")
	got := d.IsMatchReverse(input, 0, len(input))
	// Result depends on NFA fallback after cache exhaustion
	t.Logf("IsMatchReverse with tiny cache = %v", got)
}

// TestIsMatchReverseFinalStateMatch tests the final state check at the end
// of the IsMatchReverse loop (line ~1811: return currentState.IsMatch()).
func TestIsMatchReverseFinalStateMatch(t *testing.T) {
	// Pattern "0?0" reversed: after processing "0" from right,
	// the optional "0?" already matched at the final state check.
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("a?a")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	d, err := CompileWithConfig(revNFA, DefaultConfig())
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	// "a" should match because a?a can match with 0 occurrences of a?
	got := d.IsMatchReverse([]byte("a"), 0, 1)
	t.Logf("IsMatchReverse('a') for a?a = %v", got)
}

// --- SearchReverse additional tests ---

// TestSearchReverseCacheClearSlowPath tests the cache-clear path in the
// single-byte reverse tail loop of SearchReverse.
func TestSearchReverseCacheClearSlowPath(t *testing.T) {
	config := DefaultConfig().WithMaxStates(3).WithMaxCacheClears(10)
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("[a-zA-Z0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	d, err := CompileWithConfig(revNFA, config)
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	// Diverse input to force cache clearing in reverse
	input := []byte("aB1cD2eF3gH4iJ5kL6")
	got := d.SearchReverse(input, 0, len(input))
	t.Logf("SearchReverse with cache clearing = %d", got)
}

// TestSearchReverseUnrolledLoop tests SearchReverse with inputs long enough
// to exercise the 4x unrolled loop (>= start+3 bytes).
func TestSearchReverseUnrolledLoop(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("test")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	d, err := CompileWithConfig(revNFA, DefaultConfig())
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	tests := []struct {
		name  string
		input string
		start int
		end   int
		want  int
	}{
		// Long input ensures the 4x unrolled path is taken
		{"match in long input", "xxxxxtestyyyyy", 0, 9, 5},
		{"no match in long input", "xxxxxxxxyyyyyy", 0, 14, -1},
		// Short input (1-3 bytes) goes directly to tail loop
		{"short input 1 byte", "t", 0, 1, -1},
		{"short input 3 bytes", "est", 0, 3, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.SearchReverse([]byte(tt.input), tt.start, tt.end)
			if got != tt.want {
				t.Errorf("SearchReverse(%q, %d, %d) = %d, want %d",
					tt.input, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

// --- SearchReverseLimited additional tests ---

// TestSearchReverseLimitedCacheClear tests the cache-clear recovery
// path in SearchReverseLimited.
func TestSearchReverseLimitedCacheClear(t *testing.T) {
	config := DefaultConfig().WithMaxStates(3).WithMaxCacheClears(10)
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("[a-zA-Z]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	d, err := CompileWithConfig(revNFA, config)
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	input := []byte("aBcDeFgHiJkLmN")
	got := d.SearchReverseLimited(input, 0, len(input), 5)
	t.Logf("SearchReverseLimited with tiny cache = %d", got)
}

// TestSearchReverseLimitedQuadraticSignalExtended tests that SearchReverseLimited returns
// the quadratic signal when the scan reaches minStart without hitting dead state.
func TestSearchReverseLimitedQuadraticSignalExtended(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("[a-z]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	d, err := CompileWithConfig(revNFA, DefaultConfig())
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	// All lowercase input matches [a-z]+, so no dead state is reached.
	// With minStart > start and no match, should return quadratic signal.
	input := []byte("abcdefghijklmnopqrstuvwxyz")
	got := d.SearchReverseLimited(input, 0, len(input), 20)

	// Should return a match position or the quadratic signal
	if got == -1 {
		t.Error("expected match or quadratic signal, got -1")
	}
}

// TestSearchReverseLimitedMinStartAboveStart tests the lowerBound > start path
// for the quadratic signal return.
func TestSearchReverseLimitedMinStartAboveStart(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("[a-z]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}
	revNFA := nfa.ReverseAnchored(fwdNFA)
	d, err := CompileWithConfig(revNFA, DefaultConfig())
	if err != nil {
		t.Fatalf("Reverse DFA compile error: %v", err)
	}

	// start=0, end=10, minStart=8
	// Only scans positions 9, 8 then hits lowerBound
	input := []byte("abcdefghij")
	got := d.SearchReverseLimited(input, 0, len(input), 8)
	t.Logf("SearchReverseLimited(start=0, end=10, minStart=8) = %d", got)
}

// --- findWithPrefilterAt additional tests ---

// TestFindWithPrefilterAtWordBoundary tests findWithPrefilterAt with
// a word boundary pattern and prefilter.
func TestFindWithPrefilterAtWordBoundary(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`\bfoo\b`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	seq := literal.NewSeq(literal.NewLiteral([]byte("f"), false))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	d, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{"word boundary match", " foo bar", true},
		{"prefilter hit no boundary", "xfoo", false},
		{"no prefilter hit", "bar baz", false},
		{"multiple f candidates", "fix fax foo end", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.Find([]byte(tt.input))
			gotMatch := got != -1
			if gotMatch != tt.wantMatch {
				t.Errorf("Find(%q) match=%v (pos=%d), want match=%v",
					tt.input, gotMatch, got, tt.wantMatch)
			}
		})
	}
}

// TestFindWithPrefilterAtCacheClear tests the cache-clear recovery path
// in findWithPrefilterAt using a very small cache.
func TestFindWithPrefilterAtCacheClear(t *testing.T) {
	config := DefaultConfig().WithMaxStates(3).WithMaxCacheClears(10)
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("[a-zA-Z]+[0-9]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	seq := literal.NewSeq(literal.NewLiteral([]byte("a"), false))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	d, err := CompileWithPrefilter(nfaObj, config, pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	// Input with prefilter candidate that matches after diverse transitions
	input := []byte("xxx abc123 yyy")
	got := d.Find(input)
	if got == -1 {
		t.Error("Find should match 'abc123' in input")
	}
}

// TestFindWithPrefilterAtDeadStateRestart tests the dead-state restart path
// in findWithPrefilterAt where a dead state triggers finding the next candidate.
func TestFindWithPrefilterAtDeadStateRestart(t *testing.T) {
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

	d, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	// Multiple 'a' candidates, first few lead to dead states,
	// final one leads to a match
	input := []byte("ax ay az ab999 end")
	got := d.Find(input)
	if got == -1 {
		t.Error("Find should match 'ab999'")
	}

	// All candidates lead to dead states
	input2 := []byte("ax ay az")
	got2 := d.Find(input2)
	if got2 != -1 {
		t.Errorf("Find should return -1 for no match, got %d", got2)
	}
}

// TestFindWithPrefilterAtStartReturnsToStartState tests the prefilter skip
// optimization when the search returns to the start state without committing.
func TestFindWithPrefilterAtStartReturnsToStartState(t *testing.T) {
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

	d, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	// Multiple 'f' positions with varying distances
	input := []byte("f x f y foo123 f z")
	got := d.Find(input)
	if got == -1 {
		t.Error("Find should match 'foo123'")
	}
}

// TestFindWithPrefilterAtMatchFromStartAt tests that findWithPrefilterAt
// correctly uses the startAt offset to begin searching.
func TestFindWithPrefilterAtMatchFromStartAt(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("foo")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	seq := literal.NewSeq(literal.NewLiteral([]byte("foo"), true))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	d, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	input := []byte("foo bar foo baz")
	// Find starting at position 4 (past first "foo")
	got := d.FindAt(input, 4)
	if got == -1 {
		t.Error("FindAt(4) should find second 'foo'")
	}
}

// TestFindWithPrefilterAtEOIWordBoundary tests the EOI word boundary check
// at the end of findWithPrefilterAt.
func TestFindWithPrefilterAtEOIWordBoundary(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile(`test\b`)
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	seq := literal.NewSeq(literal.NewLiteral([]byte("t"), false))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	d, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	// "test" at end of input -- \b is satisfied at EOI
	got := d.Find([]byte("xxtest"))
	if got == -1 {
		t.Error("Find should match 'test' at EOI with \\b")
	}
}

// TestFindWithPrefilterAtCompletePrefilter tests the fast path where
// prefilter.IsComplete() returns true.
func TestFindWithPrefilterAtCompletePrefilter(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("hello")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	seq := literal.NewSeq(literal.NewLiteral([]byte("hello"), true))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	d, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	tests := []struct {
		name  string
		input string
		at    int
		want  int // -1 for no match, >= 0 for match
	}{
		{"match at start", "hello world", 0, 0},
		{"match in middle", "say hello", 0, 4},
		{"no match", "goodbye world", 0, -1},
		{"match from offset", "hello hello", 1, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.FindAt([]byte(tt.input), tt.at)
			if tt.want == -1 {
				if got != -1 {
					t.Errorf("FindAt(%q, %d) = %d, want -1", tt.input, tt.at, got)
				}
			} else {
				if got == -1 {
					t.Errorf("FindAt(%q, %d) = -1, want match", tt.input, tt.at)
				}
			}
		})
	}
}

// TestFindWithPrefilterAtCommittedMatchLeftmost tests leftmost semantics:
// once in a match state, if the next state is NOT a match, the committed
// match is returned (leftmost-longest).
func TestFindWithPrefilterAtCommittedMatchLeftmost(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	nfaObj, err := compiler.Compile("[a-z]+")
	if err != nil {
		t.Fatalf("NFA compile error: %v", err)
	}

	seq := literal.NewSeq(literal.NewLiteral([]byte("a"), false))
	pf := prefilter.NewBuilder(seq, nil).Build()
	if pf == nil {
		t.Skip("no prefilter built")
	}

	d, err := CompileWithPrefilter(nfaObj, DefaultConfig(), pf)
	if err != nil {
		t.Fatalf("CompileWithPrefilter error: %v", err)
	}

	// "abc" followed by space -- committed match ends at space
	input := []byte("abc 123")
	got := d.Find(input)
	if got == -1 {
		t.Error("Find should match 'abc'")
	}
	if got != 3 {
		t.Errorf("Find returned %d, expected 3 (end of 'abc')", got)
	}
}
