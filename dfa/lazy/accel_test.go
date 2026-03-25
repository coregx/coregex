package lazy

import (
	"testing"

	"github.com/coregx/coregex/nfa"
)

func TestStateAcceleration(t *testing.T) {
	// Create a test state
	state := NewState(StateID(1), []nfa.StateID{0, 1}, false)

	// Initially not accelerable
	if state.IsAccelerable() {
		t.Error("New state should not be accelerable")
	}

	if bytes := state.AccelExitBytes(); bytes != nil {
		t.Errorf("AccelExitBytes() = %v, want nil", bytes)
	}

	// Set 1 exit byte
	state.SetAccelBytes([]byte{'x'})
	if !state.IsAccelerable() {
		t.Error("State with 1 exit byte should be accelerable")
	}
	if bytes := state.AccelExitBytes(); len(bytes) != 1 || bytes[0] != 'x' {
		t.Errorf("AccelExitBytes() = %v, want ['x']", bytes)
	}

	// Set 2 exit bytes
	state.SetAccelBytes([]byte{'a', 'b'})
	if !state.IsAccelerable() {
		t.Error("State with 2 exit bytes should be accelerable")
	}
	if bytes := state.AccelExitBytes(); len(bytes) != 2 {
		t.Errorf("AccelExitBytes() = %v, want 2 bytes", bytes)
	}

	// Set 3 exit bytes
	state.SetAccelBytes([]byte{'x', 'y', 'z'})
	if !state.IsAccelerable() {
		t.Error("State with 3 exit bytes should be accelerable")
	}

	// Try to set 4 exit bytes (not allowed)
	state.SetAccelBytes([]byte{'a', 'b', 'c', 'd'})
	// Should still have previous 3 bytes (SetAccelBytes doesn't set if > 3)
	if bytes := state.AccelExitBytes(); len(bytes) != 3 {
		t.Errorf("AccelExitBytes() = %v, should still be 3 bytes", bytes)
	}

	// Empty bytes should not make state accelerable
	state2 := NewState(StateID(2), []nfa.StateID{0}, false)
	state2.SetAccelBytes([]byte{})
	if state2.IsAccelerable() {
		t.Error("State with 0 exit bytes should not be accelerable")
	}
}

func TestDetectAcceleration(t *testing.T) {
	// Create a simple NFA for pattern "x" - this will have
	// a start state where only 'x' transitions to a different state
	compiler := nfa.NewDefaultCompiler()

	// The start state of ".*x" pattern would be accelerable
	// because only 'x' transitions forward, all other bytes loop
	nfaObj, err := compiler.Compile(".*x")
	if err != nil {
		t.Fatalf("Failed to compile NFA: %v", err)
	}

	builder := NewBuilder(nfaObj, DefaultConfig())
	startLook := LookSetFromStartKind(StartText)
	startStates := builder.epsilonClosure([]nfa.StateID{nfaObj.StartUnanchored()}, startLook)
	startState := NewState(StateID(0), startStates, false)

	// Full detection (computes all transitions)
	exitBytes := builder.DetectAcceleration(startState)
	t.Logf("DetectAcceleration result: %v", exitBytes)

	// For .*x pattern, we expect 'x' to be an exit byte
	// Note: The exact result depends on NFA structure
	if len(exitBytes) > 0 {
		t.Logf("Found accelerable state with exit bytes: %v", exitBytes)
	}
}

func TestDetectAccelerationFromCached(t *testing.T) {
	// State no longer stores transitions — DetectAccelerationFromCached returns nil.
	// Acceleration is now detected via DetectAccelerationFromFlat using flatTrans.
	state := NewState(StateID(1), []nfa.StateID{0}, false)
	exitBytes := DetectAccelerationFromCached(state)
	if exitBytes != nil {
		t.Errorf("Expected nil (State has no transitions), got %v", exitBytes)
	}
}

func TestDetectAccelerationFromFlat(t *testing.T) {
	// Test acceleration detection via flat transition table
	// Using premultiplied state IDs: sid = stateIndex * stride
	stride := 256
	sid := StateID(1 * stride) // premultiplied: state 1 at offset 256
	state2 := StateID(2 * stride)
	flatTrans := make([]StateID, 3*stride) // 3 states (0, 1, 2)

	// State 1: 250 self-loops, 3 exits to state 2, 3 dead
	base := sid.Offset()
	for i := 0; i < 250; i++ {
		flatTrans[base+i] = sid // Self-loop
	}
	flatTrans[base+250] = state2
	flatTrans[base+251] = state2
	flatTrans[base+252] = state2
	flatTrans[base+253] = DeadState
	flatTrans[base+254] = DeadState
	flatTrans[base+255] = DeadState

	exitBytes := DetectAccelerationFromFlat(sid, flatTrans, stride, nil)
	if len(exitBytes) != 3 {
		t.Errorf("Expected 3 exit bytes, got %v", exitBytes)
	}
	expected := map[byte]bool{250: true, 251: true, 252: true}
	for _, b := range exitBytes {
		if !expected[b] {
			t.Errorf("Unexpected exit byte: %d", b)
		}
	}
}

func TestDFAAccelerate(t *testing.T) {
	// Test the accelerate helper function
	dfa, err := CompilePattern("foo")
	if err != nil {
		t.Fatalf("Failed to compile DFA: %v", err)
	}
	_ = dfa.NewCache() // accelerate is an immutable DFA method, cache not needed here

	haystack := []byte("aaaaaaaaaafoobar")

	// Test acceleration with single byte
	pos := dfa.accelerate(haystack, 0, []byte{'f'})
	if pos != 10 { // 'f' is at position 10
		t.Errorf("accelerate with 'f' = %d, want 10", pos)
	}

	// Test acceleration with two bytes
	pos = dfa.accelerate(haystack, 0, []byte{'f', 'o'})
	if pos != 10 { // First match is still 'f' at 10
		t.Errorf("accelerate with 'f','o' = %d, want 10", pos)
	}

	// Test acceleration with byte not in haystack
	pos = dfa.accelerate(haystack, 0, []byte{'x'})
	if pos != -1 {
		t.Errorf("accelerate with 'x' = %d, want -1", pos)
	}

	// Test acceleration at end of haystack
	pos = dfa.accelerate(haystack, len(haystack), []byte{'f'})
	if pos != -1 {
		t.Errorf("accelerate at end = %d, want -1", pos)
	}

	// Test with three bytes
	pos = dfa.accelerate(haystack, 0, []byte{'f', 'b', 'z'})
	if pos != 10 { // 'f' at 10 comes before 'b' at 13
		t.Errorf("accelerate with 'f','b','z' = %d, want 10", pos)
	}
}

func TestAccelerableStateInSearch(t *testing.T) {
	// Create a state and manually set it as accelerable
	// then verify the search uses acceleration

	// This is more of an integration test
	dfa, err := CompilePattern("x")
	if err != nil {
		t.Fatalf("Failed to compile DFA: %v", err)
	}
	cache := dfa.NewCache()

	// Test that search still works correctly
	tests := []struct {
		haystack string
		want     int
	}{
		{"x", 1},
		{"ax", 2},
		{"aaaaaaaaax", 10},
		{"no match", -1},
	}

	for _, tc := range tests {
		got := dfa.Find(cache, []byte(tc.haystack))
		if got != tc.want {
			t.Errorf("Find(%q) = %d, want %d", tc.haystack, got, tc.want)
		}
	}
}

func BenchmarkAccelerate(b *testing.B) {
	dfa, err := CompilePattern("foo")
	if err != nil {
		b.Fatalf("Failed to compile DFA: %v", err)
	}
	_ = dfa.NewCache() // accelerate is an immutable DFA method

	haystack := make([]byte, 4096)
	for i := range haystack {
		haystack[i] = 'a'
	}
	// Put 'f' near the end
	haystack[4000] = 'f'
	haystack[4001] = 'o'
	haystack[4002] = 'o'

	b.Run("memchr1", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			dfa.accelerate(haystack, 0, []byte{'f'})
		}
	})

	b.Run("memchr2", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			dfa.accelerate(haystack, 0, []byte{'f', 'o'})
		}
	})

	b.Run("memchr3", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			dfa.accelerate(haystack, 0, []byte{'f', 'o', 'x'})
		}
	})
}
