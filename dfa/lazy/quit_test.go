package lazy

import "testing"

// TestQuitStateFallback verifies that QuitState transitions cause the DFA to
// fall back to PikeVM and produce correct results. QuitState is currently dead
// code (no NFA emits LookInvalidUTF8 yet), so we exercise it by manually
// injecting QuitState transitions into the cache.
func TestQuitStateFallback(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		quitByte byte
		wantFind int
	}{
		{
			name:     "literal match with quit on unrelated byte",
			pattern:  "foo",
			haystack: "xxfooxx",
			quitByte: 'x',
			wantFind: 5,
		},
		{
			name:     "alternation with quit on first byte",
			pattern:  "bar",
			haystack: "bazbar",
			quitByte: 'b',
			wantFind: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dfa, err := CompilePattern(tt.pattern)
			if err != nil {
				t.Fatalf("CompilePattern(%q): %v", tt.pattern, err)
			}

			// Baseline: DFA without quit gives correct result
			cache := dfa.NewCache()
			baseline := dfa.Find(cache, []byte(tt.haystack))
			if baseline != tt.wantFind {
				t.Fatalf("baseline Find = %d, want %d", baseline, tt.wantFind)
			}

			// Now inject QuitState: compile fresh, do a search to populate
			// the start state, then overwrite the quitByte transition.
			cache2 := dfa.NewCache()
			hay := []byte(tt.haystack)

			// Warm up: trigger determinize for start state
			startState := dfa.getStartStateForUnanchored(cache2, hay, 0)
			if startState == nil {
				t.Fatal("failed to get start state")
			}

			// Overwrite the transition for quitByte from start state
			classIdx := int(dfa.byteToClass(tt.quitByte))
			cache2.SetFlatTransition(startState.id, classIdx, QuitState)

			// Search should fall back to PikeVM via QuitState and still
			// produce correct result
			got := dfa.Find(cache2, hay)
			if got != tt.wantFind {
				t.Errorf("Find with QuitState on %q = %d, want %d (PikeVM fallback)",
					string(tt.quitByte), got, tt.wantFind)
			}
		})
	}
}

// TestQuitStateIsMatch verifies QuitState fallback in IsMatch path.
func TestQuitStateIsMatch(t *testing.T) {
	dfa, err := CompilePattern("world")
	if err != nil {
		t.Fatalf("CompilePattern: %v", err)
	}

	hay := []byte("hello world")

	// Baseline
	cache := dfa.NewCache()
	if !dfa.IsMatch(cache, hay) {
		t.Fatal("baseline IsMatch = false, want true")
	}

	// Inject QuitState on 'h' from start state
	cache2 := dfa.NewCache()
	startState := dfa.getStartStateForUnanchored(cache2, hay, 0)
	if startState == nil {
		t.Fatal("failed to get start state")
	}
	classIdx := int(dfa.byteToClass('h'))
	cache2.SetFlatTransition(startState.id, classIdx, QuitState)

	if !dfa.IsMatch(cache2, hay) {
		t.Error("IsMatch with QuitState = false, want true (PikeVM fallback)")
	}
}

// TestQuitStateConstants verifies tag bit semantics.
func TestQuitStateConstants(t *testing.T) {
	if !QuitState.IsTagged() {
		t.Error("QuitState.IsTagged() = false, want true")
	}
	if !QuitState.IsQuitTag() {
		t.Error("QuitState.IsQuitTag() = false, want true")
	}
	if QuitState.IsDeadTag() {
		t.Error("QuitState.IsDeadTag() = true, want false")
	}
	if QuitState.IsInvalidTag() {
		t.Error("QuitState.IsInvalidTag() = true, want false")
	}
	if QuitState == DeadState {
		t.Error("QuitState == DeadState, must be distinct")
	}
	if QuitState == InvalidState {
		t.Error("QuitState == InvalidState, must be distinct")
	}
}
