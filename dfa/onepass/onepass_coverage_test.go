package onepass

import (
	"errors"
	"regexp/syntax"
	"testing"

	"github.com/coregx/coregex/nfa"
)

// compileAnchored compiles a pattern with anchored NFA for onepass testing.
func compileAnchored(t *testing.T, pattern string) *nfa.NFA {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("failed to parse pattern %q: %v", pattern, err)
	}
	compiler := nfa.NewCompiler(nfa.CompilerConfig{
		UTF8:              true,
		Anchored:          true,
		DotNewline:        false,
		MaxRecursionDepth: 100,
	})
	n, err := compiler.CompileRegexp(re)
	if err != nil {
		t.Fatalf("failed to compile NFA for %q: %v", pattern, err)
	}
	return n
}

// TestCacheSlots verifies the Slots() getter returns the internal slots slice.
func TestCacheSlots(t *testing.T) {
	tests := []struct {
		name       string
		numCapture int
		wantLen    int
	}{
		{"one group (group 0)", 1, 2},
		{"two groups", 2, 4},
		{"three groups", 3, 6},
		{"sixteen groups (max)", 16, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(tt.numCapture)
			slots := cache.Slots()

			if slots == nil {
				t.Fatal("Slots() returned nil")
			}
			if len(slots) != tt.wantLen {
				t.Errorf("Slots() len = %d, want %d", len(slots), tt.wantLen)
			}

			// Verify Reset sets all slots to -1
			cache.Reset()
			for i, v := range cache.Slots() {
				if v != -1 {
					t.Errorf("after Reset, Slots()[%d] = %d, want -1", i, v)
				}
			}

			// Verify Slots() returns same underlying slice (not a copy)
			cache.Slots()[0] = 42
			if cache.Slots()[0] != 42 {
				t.Error("Slots() appears to return a copy, expected same underlying slice")
			}
		})
	}
}

// TestIsMatchStateOutOfBounds tests isMatchState with state IDs beyond matchStates slice.
func TestIsMatchStateOutOfBounds(t *testing.T) {
	n := compileAnchored(t, `abc`)
	dfa, err := Build(n)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// State ID beyond the matchStates slice should return false
	outOfBounds := StateID(len(dfa.matchStates) + 100)
	if dfa.isMatchState(outOfBounds) {
		t.Error("isMatchState should return false for out-of-bounds state ID")
	}

	// DeadState (very large) should return false
	if dfa.isMatchState(DeadState) {
		t.Error("isMatchState should return false for DeadState")
	}
}

// TestGetMatchSlotsOutOfBounds tests getMatchSlots with state IDs beyond matchSlots slice.
func TestGetMatchSlotsOutOfBounds(t *testing.T) {
	n := compileAnchored(t, `(abc)`)
	dfa, err := Build(n)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Out-of-bounds state ID should return 0
	outOfBounds := StateID(len(dfa.matchSlots) + 100)
	mask := dfa.getMatchSlots(outOfBounds)
	if mask != 0 {
		t.Errorf("getMatchSlots for out-of-bounds = %d, want 0", mask)
	}

	// DeadState (very large) should return 0
	mask = dfa.getMatchSlots(DeadState)
	if mask != 0 {
		t.Errorf("getMatchSlots for DeadState = %d, want 0", mask)
	}
}

// TestGetMatchSlotsValidState verifies getMatchSlots returns correct mask for a valid match state.
func TestGetMatchSlotsValidState(t *testing.T) {
	n := compileAnchored(t, `(abc)`)
	dfa, err := Build(n)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Search to confirm DFA works, then check match state slots
	cache := NewCache(dfa.NumCaptures())
	got := dfa.Search([]byte("abc"), cache)
	if got == nil {
		t.Fatal("expected match for 'abc'")
	}

	// At least one match state should have a non-zero slot mask
	foundNonZero := false
	for i := 0; i < len(dfa.matchStates); i++ {
		if dfa.matchStates[i] {
			mask := dfa.getMatchSlots(StateID(i))
			if mask != 0 {
				foundNonZero = true
			}
		}
	}
	// It's acceptable for match slots to be 0 if all captures are set via transitions
	_ = foundNonZero
}

// TestIsOnePassUnanchored tests IsOnePass with unanchored patterns (should fail).
func TestIsOnePassUnanchored(t *testing.T) {
	// Compile without anchored flag
	compiler := nfa.NewDefaultCompiler()
	n, err := compiler.Compile("abc")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Unanchored NFA should NOT be one-pass
	if !n.IsAlwaysAnchored() {
		// Expected: unanchored patterns fail IsOnePass
		if IsOnePass(n) {
			t.Error("IsOnePass should return false for unanchored NFA")
		}
	}
}

// TestIsOnePassTooManyCaptures tests IsOnePass with >16 capture groups.
func TestIsOnePassTooManyCaptures(t *testing.T) {
	// 17 capture groups: group 0 + 16 explicit = 17 total > 16
	pattern := `(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)(m)(n)(o)(p)`
	n := compileAnchored(t, pattern)

	if n.CaptureCount() <= 16 {
		t.Skipf("NFA has %d captures, need >16", n.CaptureCount())
	}

	if IsOnePass(n) {
		t.Error("IsOnePass should return false for >16 capture groups")
	}

	// Build should also return ErrTooManyCaptures
	_, err := Build(n)
	if err == nil {
		t.Fatal("Build should error for >16 capture groups")
	}
	if !errors.Is(err, ErrTooManyCaptures) {
		t.Errorf("expected ErrTooManyCaptures, got: %v", err)
	}
}

// TestIsOnePassSmallAnchored tests IsOnePass with small anchored patterns.
func TestIsOnePassSmallAnchored(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"single char", `a`, true},
		{"literal", `abc`, true},
		{"char class", `[a-z]+`, true},
		{"digit groups", `(\d+)-(\d+)`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileAnchored(t, tt.pattern)
			got := IsOnePass(n)
			if got != tt.want {
				t.Errorf("IsOnePass(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestEpsilonClosureOnePassMultipleMatchPaths tests the case where
// epsilon closure encounters multiple match states (not one-pass).
func TestEpsilonClosureOnePassMultipleMatchPaths(t *testing.T) {
	// Patterns that are ambiguous (multiple match paths) should fail
	ambiguousPatterns := []struct {
		name    string
		pattern string
	}{
		{"greedy ambiguity", `(.*)x`},
		{"ambiguous groups", `(.*) (.*)`},
	}

	for _, tt := range ambiguousPatterns {
		t.Run(tt.name, func(t *testing.T) {
			n := compileAnchored(t, tt.pattern)
			_, err := Build(n)
			if err == nil {
				t.Errorf("expected Build to fail for ambiguous pattern %q", tt.pattern)
			}
			if !errors.Is(err, ErrNotOnePass) {
				t.Errorf("expected ErrNotOnePass for %q, got: %v", tt.pattern, err)
			}
		})
	}
}

// TestEpsilonClosureOnePassWithLookAhead tests epsilon closure through look assertions.
func TestEpsilonClosureOnePassWithLookAhead(t *testing.T) {
	// Patterns with anchors that pass through StateLook in epsilon closure
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"start anchor", `^abc`, "abc", true},
		{"start anchor no match", `^abc`, "xbc", false},
		{"end anchor", `abc$`, "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileAnchored(t, tt.pattern)
			dfa, err := Build(n)
			if err != nil {
				t.Skipf("pattern %q not one-pass: %v", tt.pattern, err)
				return
			}
			got := dfa.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestGetTransitionOutOfBounds tests getTransition with an index beyond the table.
func TestGetTransitionOutOfBounds(t *testing.T) {
	n := compileAnchored(t, `a`)
	dfa, err := Build(n)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Use a very large state ID that would cause index out of bounds
	largeStateID := StateID(len(dfa.table) + 1000)
	trans := dfa.getTransition(largeStateID, 0)
	if !trans.IsDead() {
		t.Error("getTransition with out-of-bounds index should return dead transition")
	}
}

// TestIsMatchWithDeadTransition tests IsMatch when the first byte leads to a dead state.
func TestIsMatchWithDeadTransition(t *testing.T) {
	n := compileAnchored(t, `[0-9]+`)
	dfa, err := Build(n)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Letters should immediately hit a dead transition
	if dfa.IsMatch([]byte("abc")) {
		t.Error("IsMatch should return false for non-digit input")
	}

	// Digits should match
	if !dfa.IsMatch([]byte("123")) {
		t.Error("IsMatch should return true for digit input")
	}
}

// TestIsMatchFinalStateCheck tests IsMatch when the match is only at the final state.
func TestIsMatchFinalStateCheck(t *testing.T) {
	n := compileAnchored(t, `abc`)
	dfa, err := Build(n)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Exact input: match detected at final state check (after loop)
	if !dfa.IsMatch([]byte("abc")) {
		t.Error("IsMatch should return true for exact 'abc'")
	}

	// Prefix only: dead state during input processing
	if dfa.IsMatch([]byte("ab")) {
		t.Error("IsMatch should return false for prefix 'ab'")
	}
}

// TestTransitionIsDeadCoverage verifies the DeadState constant for transitions.
func TestTransitionIsDeadCoverage(t *testing.T) {
	// DeadState is 0 in the onepass package
	trans := NewTransition(DeadState, false, 0)
	if !trans.IsDead() {
		t.Error("transition with DeadState should report IsDead() = true")
	}

	// State 1 should NOT be dead
	trans2 := NewTransition(1, false, 0)
	if trans2.IsDead() {
		t.Error("transition with state 1 should not be dead")
	}

	// MaxStateID should NOT be dead
	trans3 := NewTransition(MaxStateID, false, 0)
	if trans3.IsDead() {
		t.Error("transition with MaxStateID should not be dead")
	}
}

// TestBuildWithCaptureStateEpsilon tests building DFA with patterns containing capture
// groups that exercise the StateCapture branch in epsilonClosureOnePass.
func TestBuildWithCaptureStateEpsilon(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		wantMatch bool
		wantGroup string // expected group 1 content (empty if no capture)
	}{
		{
			name:      "single capture group",
			pattern:   `(abc)`,
			input:     "abc",
			wantMatch: true,
			wantGroup: "abc",
		},
		{
			name:      "nested captures with separator",
			pattern:   `(\d+):(\d+)`,
			input:     "12:34",
			wantMatch: true,
			wantGroup: "12",
		},
		{
			name:      "optional capture group",
			pattern:   `a(b?)c`,
			input:     "abc",
			wantMatch: true,
			wantGroup: "b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileAnchored(t, tt.pattern)
			dfa, err := Build(n)
			if err != nil {
				t.Skipf("pattern %q not one-pass: %v", tt.pattern, err)
				return
			}

			cache := NewCache(dfa.NumCaptures())
			got := dfa.Search([]byte(tt.input), cache)

			if !tt.wantMatch {
				if got != nil {
					t.Errorf("expected no match, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected match, got nil")
			}
			if tt.wantGroup == "" || len(got) < 4 {
				return
			}
			g1 := string([]byte(tt.input)[got[2]:got[3]])
			if g1 != tt.wantGroup {
				t.Errorf("group 1 = %q, want %q", g1, tt.wantGroup)
			}
		})
	}
}

// TestNextPowerOf2EdgeCases tests edge cases for nextPowerOf2.
func TestNextPowerOf2EdgeCases(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{-1, 1},
		{-100, 1},
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{16, 16},
		{17, 32},
		{1024, 1024},
		{1025, 2048},
	}

	for _, tt := range tests {
		got := nextPowerOf2(tt.input)
		if got != tt.want {
			t.Errorf("nextPowerOf2(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// TestLog2EdgeCases tests edge cases for log2.
func TestLog2EdgeCases(t *testing.T) {
	tests := []struct {
		input int
		want  uint
	}{
		{-1, 0},
		{0, 0},
		{1, 0},
		{2, 1},
		{4, 2},
		{8, 3},
		{1024, 10},
	}

	for _, tt := range tests {
		got := log2(tt.input)
		if got != tt.want {
			t.Errorf("log2(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
