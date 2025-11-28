package nfa

import (
	"testing"
)

// TestReverse_Simple tests reverse NFA for simple literal "abc"
func TestReverse_Simple(t *testing.T) {
	// Compile forward NFA for "abc"
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("abc")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	// Build reverse NFA
	reverse := Reverse(forward)

	// Verify basic properties
	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	if reverse.States() == 0 {
		t.Error("Reverse NFA has no states")
	}

	// Verify start state is valid
	startAnchored := reverse.StartAnchored()
	if startAnchored == InvalidState {
		t.Error("Reverse NFA has invalid anchored start state")
	}

	// Verify we have at least one match state
	hasMatch := false
	for it := reverse.Iter(); it.HasNext(); {
		state := it.Next()
		if state.IsMatch() {
			hasMatch = true
			break
		}
	}
	if !hasMatch {
		t.Error("Reverse NFA has no match states")
	}

	t.Logf("Forward NFA: %d states", forward.States())
	t.Logf("Reverse NFA: %d states", reverse.States())
}

// TestReverse_Alternation tests reverse NFA for "foo|bar"
func TestReverse_Alternation(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("foo|bar")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	if reverse.States() == 0 {
		t.Error("Reverse NFA has no states")
	}

	// Verify we have match state
	matchCount := 0
	for it := reverse.Iter(); it.HasNext(); {
		state := it.Next()
		if state.IsMatch() {
			matchCount++
		}
	}
	if matchCount == 0 {
		t.Error("Reverse NFA has no match states")
	}

	t.Logf("Forward NFA: %d states", forward.States())
	t.Logf("Reverse NFA: %d states, %d match states", reverse.States(), matchCount)
}

// TestReverse_CharClass tests reverse NFA for "[abc]"
func TestReverse_CharClass(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("[abc]")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	if reverse.States() == 0 {
		t.Error("Reverse NFA has no states")
	}

	// Count state types
	stateKinds := make(map[StateKind]int)
	for it := reverse.Iter(); it.HasNext(); {
		state := it.Next()
		stateKinds[state.Kind()]++
	}

	t.Logf("Forward NFA: %d states", forward.States())
	t.Logf("Reverse NFA: %d states", reverse.States())
	for kind, count := range stateKinds {
		t.Logf("  %s: %d", kind, count)
	}

	if stateKinds[StateMatch] == 0 {
		t.Error("Reverse NFA has no match states")
	}
}

// TestReverse_Quantifier tests reverse NFA for "a+"
func TestReverse_Quantifier(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("a+")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	if reverse.States() == 0 {
		t.Error("Reverse NFA has no states")
	}

	// Verify structure - at minimum we need match and byte range states
	hasMatch := false
	hasByteRange := false
	stateKinds := make(map[StateKind]int)
	for it := reverse.Iter(); it.HasNext(); {
		state := it.Next()
		stateKinds[state.Kind()]++
		switch state.Kind() {
		case StateMatch:
			hasMatch = true
		case StateByteRange:
			hasByteRange = true
		}
	}

	if !hasMatch {
		t.Error("Reverse NFA missing match state")
	}
	if !hasByteRange {
		t.Error("Reverse NFA missing byte range state")
	}

	t.Logf("Forward NFA: %d states", forward.States())
	t.Logf("Reverse NFA: %d states", reverse.States())
	for kind, count := range stateKinds {
		t.Logf("  %s: %d", kind, count)
	}
}

// TestReverse_Star tests reverse NFA for "a*"
func TestReverse_Star(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("a*")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	if reverse.States() == 0 {
		t.Error("Reverse NFA has no states")
	}

	t.Logf("Forward NFA: %d states", forward.States())
	t.Logf("Reverse NFA: %d states", reverse.States())
}

// TestReverse_Concat tests reverse NFA for concatenation "abcd"
func TestReverse_Concat(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("abcd")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	// Count byte range states (should have 4 for a,b,c,d)
	byteRangeCount := 0
	for it := reverse.Iter(); it.HasNext(); {
		state := it.Next()
		if state.Kind() == StateByteRange {
			byteRangeCount++
		}
	}

	// Should have byte ranges for 'a', 'b', 'c', 'd'
	if byteRangeCount < 4 {
		t.Errorf("Expected at least 4 byte range states, got %d", byteRangeCount)
	}

	t.Logf("Forward NFA: %d states", forward.States())
	t.Logf("Reverse NFA: %d states, %d byte ranges", reverse.States(), byteRangeCount)
}

// TestReverse_EmptyPattern tests reverse NFA for empty pattern
func TestReverse_EmptyPattern(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	if reverse.States() == 0 {
		t.Error("Reverse NFA has no states")
	}

	t.Logf("Forward NFA: %d states", forward.States())
	t.Logf("Reverse NFA: %d states", reverse.States())
}

// TestReverse_ComplexPattern tests reverse NFA for complex pattern
func TestReverse_ComplexPattern(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("(foo|bar)+baz")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	if reverse.States() == 0 {
		t.Error("Reverse NFA has no states")
	}

	// Verify we have all necessary state types
	stateKinds := make(map[StateKind]int)
	for it := reverse.Iter(); it.HasNext(); {
		state := it.Next()
		stateKinds[state.Kind()]++
	}

	if stateKinds[StateMatch] == 0 {
		t.Error("Reverse NFA missing match state")
	}

	t.Logf("Forward NFA: %d states", forward.States())
	t.Logf("Reverse NFA: %d states", reverse.States())
	for kind, count := range stateKinds {
		t.Logf("  %s: %d", kind, count)
	}
}

// TestReverse_Anchored tests reverse NFA for anchored pattern "^abc"
func TestReverse_Anchored(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("^abc")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	// Reverse NFA should preserve anchored flag
	if forward.IsAnchored() && !reverse.IsAnchored() {
		t.Error("Reverse NFA lost anchored flag")
	}

	t.Logf("Forward NFA: %d states, anchored=%v", forward.States(), forward.IsAnchored())
	t.Logf("Reverse NFA: %d states, anchored=%v", reverse.States(), reverse.IsAnchored())
}

// TestReverse_CaptureGroups tests that captures are stripped in reverse NFA
func TestReverse_CaptureGroups(t *testing.T) {
	compiler := NewDefaultCompiler()
	forward, err := compiler.Compile("(a)(b)(c)")
	if err != nil {
		t.Fatalf("failed to compile forward NFA: %v", err)
	}

	reverse := Reverse(forward)

	if reverse == nil {
		t.Fatal("Reverse returned nil")
	}

	// Reverse NFA should have no captures
	if reverse.CaptureCount() != 0 {
		t.Errorf("Reverse NFA should have 0 captures, got %d", reverse.CaptureCount())
	}

	// Should have no capture states
	captureCount := 0
	for it := reverse.Iter(); it.HasNext(); {
		state := it.Next()
		if state.Kind() == StateCapture {
			captureCount++
		}
	}
	if captureCount > 0 {
		t.Errorf("Reverse NFA should have no capture states, got %d", captureCount)
	}

	t.Logf("Forward NFA: %d captures", forward.CaptureCount())
	t.Logf("Reverse NFA: %d captures (expected 0)", reverse.CaptureCount())
}

// TestReverse_Properties tests that properties are preserved
func TestReverse_Properties(t *testing.T) {
	tests := []struct {
		pattern string
		utf8    bool
	}{
		{"abc", true},
		{"[a-z]+", true},
		{"foo|bar", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			compiler := NewCompiler(CompilerConfig{
				UTF8:     tt.utf8,
				Anchored: false,
			})
			forward, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("failed to compile: %v", err)
			}

			reverse := Reverse(forward)

			if reverse.IsUTF8() != forward.IsUTF8() {
				t.Errorf("UTF8 flag mismatch: forward=%v, reverse=%v",
					forward.IsUTF8(), reverse.IsUTF8())
			}

			if reverse.PatternCount() != forward.PatternCount() {
				t.Errorf("Pattern count mismatch: forward=%d, reverse=%d",
					forward.PatternCount(), reverse.PatternCount())
			}
		})
	}
}

// TestReverse_StateCount tests that reverse NFA doesn't explode in size
func TestReverse_StateCount(t *testing.T) {
	patterns := []string{
		"a",
		"ab",
		"abc",
		"a+",
		"a*",
		"a|b",
		"(a|b)+",
		"[a-z]",
		"[a-z]+",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			forward, err := compiler.Compile(pattern)
			if err != nil {
				t.Fatalf("failed to compile: %v", err)
			}

			reverse := Reverse(forward)

			// Reverse NFA should not be dramatically larger
			// Allow up to 2x size (some extra states for structure)
			maxAllowed := forward.States() * 2
			if reverse.States() > maxAllowed {
				t.Errorf("Reverse NFA too large: forward=%d, reverse=%d (max allowed=%d)",
					forward.States(), reverse.States(), maxAllowed)
			}

			t.Logf("Pattern: %s, Forward: %d states, Reverse: %d states",
				pattern, forward.States(), reverse.States())
		})
	}
}
