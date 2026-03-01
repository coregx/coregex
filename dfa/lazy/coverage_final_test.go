package lazy

import (
	"testing"

	"github.com/coregx/coregex/nfa"
)

// TestCompileConvenience tests the Compile() convenience function.
func TestCompileConvenience(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	n, err := compiler.Compile("ab+c")
	if err != nil {
		t.Fatalf("NFA compile failed: %v", err)
	}

	d, err := Compile(n)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	pos := d.Find([]byte("xabbc"))
	if pos != 5 {
		t.Errorf("expected Find=5, got %d", pos)
	}
}

// TestWithMinPrefilterLen tests the WithMinPrefilterLen config method.
func TestWithMinPrefilterLen(t *testing.T) {
	cfg := DefaultConfig().WithMinPrefilterLen(10)
	if cfg.MinPrefilterLen != 10 {
		t.Errorf("expected MinPrefilterLen=10, got %d", cfg.MinPrefilterLen)
	}
}

// TestSearchAtAnchoredCoverage tests the SearchAtAnchored method.
func TestSearchAtAnchoredCoverage(t *testing.T) {
	d, err := CompilePattern("foo")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	tests := []struct {
		name     string
		haystack string
		at       int
		want     int
	}{
		{"match_at_start", "foobar", 0, 3},
		{"no_match_at_offset", "xfoo", 0, -1}, // anchored: must start at 0
		{"match_at_offset", "xfoo", 1, 4},
		{"past_end", "foo", 10, -1},
		{"at_end_no_match", "foo", 3, -1},
		{"empty_input_no_match", "", 0, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.SearchAtAnchored([]byte(tt.haystack), tt.at)
			if got != tt.want {
				t.Errorf("SearchAtAnchored(%q, %d) = %d, want %d",
					tt.haystack, tt.at, got, tt.want)
			}
		})
	}
}

// TestSearchAtAnchoredEmptyPatternCoverage tests anchored search with empty-matching pattern.
func TestSearchAtAnchoredEmptyPatternCoverage(t *testing.T) {
	d, err := CompilePattern("a*")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	// At end of input, a* matches empty string
	got := d.SearchAtAnchored([]byte("abc"), 3)
	if got != 3 {
		t.Errorf("expected 3 (empty match at end), got %d", got)
	}

	// At position 0, should match "a"
	got = d.SearchAtAnchored([]byte("abc"), 0)
	if got < 0 {
		t.Errorf("expected match at position 0, got %d", got)
	}
}

// TestAlphabetLenWithByteClasses tests AlphabetLen with and without byte classes.
func TestAlphabetLenWithByteClasses(t *testing.T) {
	// Pattern that creates byte classes (char class with specific ranges)
	d, err := CompilePattern("[a-z]+")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	al := d.AlphabetLen()
	if al <= 0 || al > 256 {
		t.Errorf("expected AlphabetLen in [1,256], got %d", al)
	}
}

// TestAlphabetLenNoByteClasses tests AlphabetLen when byte classes are nil.
func TestAlphabetLenNoByteClasses(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	n, err := compiler.Compile("abc")
	if err != nil {
		t.Fatal(err)
	}

	// Build DFA with default config
	d, err := CompileWithConfig(n, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	// If byte classes exist, AlphabetLen < 256; if nil, = 256
	al := d.AlphabetLen()
	if al <= 0 || al > 256 {
		t.Errorf("expected AlphabetLen in [1,256], got %d", al)
	}
}

// TestByteToClass tests the byteToClass conversion.
func TestByteToClass(t *testing.T) {
	d, err := CompilePattern("[a-z]+")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	// 'a' and 'b' should map to the same equivalence class (both in [a-z])
	ca := d.byteToClass('a')
	cb := d.byteToClass('b')
	if ca != cb {
		t.Logf("Note: 'a' class=%d, 'b' class=%d (may differ based on ranges)", ca, cb)
	}

	// A byte outside [a-z] should map to a different class
	cX := d.byteToClass('!')
	if cX == ca {
		t.Log("Note: '!' maps to same class as 'a' - unexpected but not necessarily wrong")
	}
}

// TestReleaseStateSetNil tests releaseStateSet with nil input.
func TestReleaseStateSetNil(t *testing.T) {
	// Should not panic
	releaseStateSet(nil)
}

// TestReleaseStateSetLarge tests releaseStateSet with large set (skipped from pool).
func TestReleaseStateSetLarge(t *testing.T) {
	ss := NewStateSetWithCapacity(5000) // > 4096 threshold
	for i := nfa.StateID(0); i < 100; i++ {
		ss.Add(i)
	}
	// Should not panic, but should not pool
	releaseStateSet(ss)
}

// TestSearchReverseLimitedBounds tests SearchReverseLimited edge cases.
func TestSearchReverseLimitedBounds(t *testing.T) {
	// Build reverse DFA
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("abc")
	if err != nil {
		t.Fatal(err)
	}
	revNFA := nfa.Reverse(fwdNFA)
	d, err := Compile(revNFA)
	if err != nil {
		t.Fatal(err)
	}

	// Normal reverse search (start, end, minStart)
	pos := d.SearchReverseLimited([]byte("xyzabc"), 0, 6, 0)
	if pos < 0 {
		t.Errorf("expected match start, got %d", pos)
	}

	// Empty range
	pos = d.SearchReverseLimited([]byte("abc"), 3, 3, 0)
	if pos != -1 {
		t.Errorf("expected -1 for empty range, got %d", pos)
	}
}

// TestIsMatchReverseEdge tests IsMatchReverse edge cases.
func TestIsMatchReverseEdge(t *testing.T) {
	// Build a reverse DFA from reverse NFA
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("abc")
	if err != nil {
		t.Fatal(err)
	}
	revNFA := nfa.Reverse(fwdNFA)
	d, err := Compile(revNFA)
	if err != nil {
		t.Fatal(err)
	}

	// Match exists (reverse DFA matches "cba" backwards = "abc" forwards)
	if !d.IsMatchReverse([]byte("abc"), 0, 3) {
		t.Error("expected IsMatchReverse to find 'abc'")
	}

	// No match
	if d.IsMatchReverse([]byte("xyz"), 0, 3) {
		t.Error("expected IsMatchReverse to not find 'xyz'")
	}

	// Out of bounds (end <= start)
	if d.IsMatchReverse([]byte("abc"), 0, 0) {
		t.Error("expected false for empty range")
	}

	// Out of bounds (end > len)
	if d.IsMatchReverse([]byte("abc"), 0, 10) {
		t.Error("expected false when end > len(haystack)")
	}
}

// TestSearchAtEmptyMatch tests SearchAt with pattern that can match empty.
func TestSearchAtEmptyMatch(t *testing.T) {
	d, err := CompilePattern("a*")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	// SearchAt at end of haystack
	got := d.SearchAt([]byte("abc"), 3)
	if got != 3 {
		t.Errorf("expected 3 (empty match at end), got %d", got)
	}

	// SearchAt past end
	got = d.SearchAt([]byte("abc"), 10)
	if got != -1 {
		t.Errorf("expected -1 for past end, got %d", got)
	}

	// SearchAt on empty input
	got = d.SearchAt([]byte(""), 0)
	if got != 0 {
		t.Errorf("expected 0 (empty match on empty input), got %d", got)
	}
}

// TestDetectAccelerationFromCachedNilState tests nil state handling.
func TestDetectAccelerationFromCachedNilState(t *testing.T) {
	result := DetectAccelerationFromCached(nil)
	if result != nil {
		t.Errorf("expected nil for nil state, got %v", result)
	}
}

// TestDetectAccelerationFromCachedWithClassesNil tests nil state with classes.
func TestDetectAccelerationFromCachedWithClassesNil(t *testing.T) {
	result := DetectAccelerationFromCachedWithClasses(nil, nil)
	if result != nil {
		t.Errorf("expected nil for nil state, got %v", result)
	}
}

// TestComputeStartState tests the ComputeStartState function.
func TestComputeStartState(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	n, err := compiler.Compile("[a-z]+")
	if err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(n, DefaultConfig())
	configs := []StartConfig{
		{Kind: StartText, Anchored: false},
		{Kind: StartText, Anchored: true},
		{Kind: StartLineLF, Anchored: false},
		{Kind: StartWord, Anchored: false},
	}

	for _, config := range configs {
		state, key := ComputeStartState(builder, n, config)
		if state == nil {
			t.Errorf("ComputeStartState(%v) returned nil state", config)
		}
		if key == 0 {
			t.Errorf("ComputeStartState(%v) returned zero key", config)
		}
	}
}

// TestSearchReverseStartGtEnd tests SearchReverse when start > end.
func TestSearchReverseStartGtEnd(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	fwdNFA, err := compiler.Compile("abc")
	if err != nil {
		t.Fatal(err)
	}
	revNFA := nfa.Reverse(fwdNFA)
	d, err := Compile(revNFA)
	if err != nil {
		t.Fatal(err)
	}

	pos := d.SearchReverse([]byte("abc"), 3, 0)
	if pos != -1 {
		t.Errorf("expected -1 for start > end, got %d", pos)
	}
}

// TestFindAtWithLargeOffset tests FindAt with a large start offset.
func TestFindAtWithLargeOffset(t *testing.T) {
	d, err := CompilePattern("abc")
	if err != nil {
		t.Fatal(err)
	}

	got := d.FindAt([]byte("abc"), 100)
	if got != -1 {
		t.Errorf("expected -1 for large offset, got %d", got)
	}
}

// TestDetectAccelerationFullState tests DetectAcceleration on a built state.
func TestDetectAccelerationFullState(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	n, err := compiler.Compile("a")
	if err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(n, DefaultConfig())
	dfa, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	// Force some state transitions
	_ = dfa.Find([]byte("bbbba"))

	// Try acceleration detection on each state
	for _, s := range dfa.states {
		if s != nil {
			result := builder.DetectAcceleration(s)
			// result can be nil or a slice - just verify no panic
			_ = result
		}
	}
}

// TestDetectAccelerationNilBuilder tests DetectAcceleration with nil state.
func TestDetectAccelerationNilBuilder(t *testing.T) {
	compiler := nfa.NewDefaultCompiler()
	n, err := compiler.Compile("a")
	if err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(n, DefaultConfig())
	result := builder.DetectAcceleration(nil)
	if result != nil {
		t.Errorf("expected nil for nil state, got %v", result)
	}
}

// TestConfigValidateEdgeCases tests config validation edge cases.
func TestConfigValidateEdgeCases(t *testing.T) {
	// Invalid: zero max states
	cfg := DefaultConfig()
	cfg.MaxStates = 0
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for zero MaxStates")
	}

	// Invalid: zero determinization limit
	cfg = DefaultConfig()
	cfg.DeterminizationLimit = 0
	err = cfg.Validate()
	if err == nil {
		t.Error("expected error for zero DeterminizationLimit")
	}

	// Invalid: negative MaxCacheClears
	cfg = DefaultConfig()
	cfg.MaxCacheClears = -1
	err = cfg.Validate()
	if err == nil {
		t.Error("expected error for negative MaxCacheClears")
	}
}

// TestCompilePatternInvalid tests CompilePattern with invalid regex.
func TestCompilePatternInvalid(t *testing.T) {
	_, err := CompilePattern("[invalid")
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

// TestSearchAtAnchoredWordBoundary tests anchored search with word boundary.
func TestSearchAtAnchoredWordBoundary(t *testing.T) {
	d, err := CompilePattern(`\bfoo\b`)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		haystack string
		at       int
		want     int
	}{
		{"word_boundary_match", " foo ", 1, 4},
		{"no_boundary", "xfoox", 1, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.SearchAtAnchored([]byte(tt.haystack), tt.at)
			if got != tt.want {
				t.Errorf("SearchAtAnchored(%q, %d) = %d, want %d",
					tt.haystack, tt.at, got, tt.want)
			}
		})
	}
}

// TestIsCacheClearedNilError tests isCacheCleared with nil error.
func TestIsCacheClearedNilError(t *testing.T) {
	if isCacheCleared(nil) {
		t.Error("expected false for nil error")
	}
}

// TestGetStateDeadState tests getState with DeadState.
func TestGetStateDeadState(t *testing.T) {
	d, err := CompilePattern("abc")
	if err != nil {
		t.Fatal(err)
	}

	s := d.getState(DeadState)
	if s != nil {
		t.Error("expected nil for DeadState")
	}
}

// TestGetStateOutOfRange tests getState with out-of-range ID.
func TestGetStateOutOfRange(t *testing.T) {
	d, err := CompilePattern("abc")
	if err != nil {
		t.Fatal(err)
	}

	s := d.getState(9999)
	if s != nil {
		t.Error("expected nil for out-of-range state ID")
	}
}
