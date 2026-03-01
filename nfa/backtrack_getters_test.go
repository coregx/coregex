package nfa

import (
	"testing"
)

func TestNewBacktrackerState(t *testing.T) {
	state := NewBacktrackerState()
	if state == nil {
		t.Fatal("NewBacktrackerState() returned nil")
	}
	if state.Visited != nil {
		t.Error("Visited should be nil initially")
	}
	if state.Generation != 0 {
		t.Errorf("Generation = %d, want 0", state.Generation)
	}
	if state.NumStates != 0 {
		t.Errorf("NumStates = %d, want 0", state.NumStates)
	}
	if state.InputLen != 0 {
		t.Errorf("InputLen = %d, want 0", state.InputLen)
	}
	if state.Longest {
		t.Error("Longest should be false initially")
	}
}

func TestBoundedBacktracker_SetLongest(t *testing.T) {
	nfa := compileNFAForTest(`\w+`)
	bt := NewBoundedBacktracker(nfa)

	// Default: not longest
	if bt.internalState.Longest {
		t.Error("Longest should be false by default")
	}

	// Enable longest
	bt.SetLongest(true)
	if !bt.internalState.Longest {
		t.Error("Longest should be true after SetLongest(true)")
	}

	// Disable longest
	bt.SetLongest(false)
	if bt.internalState.Longest {
		t.Error("Longest should be false after SetLongest(false)")
	}
}

func TestBoundedBacktracker_NumStates(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{"simple literal", "abc"},
		{"char class", `\d+`},
		{"alternation", "foo|bar|baz"},
		{"quantifier", "a{2,5}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(nfa)

			numStates := bt.NumStates()
			if numStates <= 0 {
				t.Errorf("NumStates() = %d, should be > 0", numStates)
			}
			if numStates != nfa.States() {
				t.Errorf("NumStates() = %d, want %d (NFA states)", numStates, nfa.States())
			}
		})
	}
}

func TestBoundedBacktracker_MaxVisitedSize(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)

	maxVisited := bt.MaxVisitedSize()
	if maxVisited != 32*1024*1024 {
		t.Errorf("MaxVisitedSize() = %d, want %d", maxVisited, 32*1024*1024)
	}
}

func TestBoundedBacktracker_MaxInputSize(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)

	maxInput := bt.MaxInputSize()
	numStates := bt.NumStates()

	// maxInputSize = maxVisitedSize / numStates - 1
	expected := bt.MaxVisitedSize()/numStates - 1
	if maxInput != expected {
		t.Errorf("MaxInputSize() = %d, want %d", maxInput, expected)
	}

	// Should be a large number (millions)
	if maxInput < 100_000 {
		t.Errorf("MaxInputSize() = %d, expected much larger for simple pattern", maxInput)
	}
}

func TestBoundedBacktracker_MaxInputSize_ZeroStates(t *testing.T) {
	// Create a backtracker with zero states via direct construction
	bt := &BoundedBacktracker{
		nfa:            nil,
		numStates:      0,
		maxVisitedSize: 32 * 1024 * 1024,
	}

	maxInput := bt.MaxInputSize()
	if maxInput != 0 {
		t.Errorf("MaxInputSize() with 0 states = %d, want 0", maxInput)
	}
}

func TestBoundedBacktracker_CanHandle_Boundaries(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)
	numStates := bt.NumStates()

	tests := []struct {
		name          string
		haystackLen   int
		wantCanHandle bool
	}{
		{"empty input", 0, true},
		{"1 byte", 1, true},
		{"small input 1KB", 1024, true},
		{"medium input 1MB", 1_000_000, true},
		{"at limit", bt.MaxVisitedSize()/numStates - 1, true},
		{"over limit", bt.MaxVisitedSize()/numStates + 1, false},
		{"very large", 100_000_000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bt.CanHandle(tt.haystackLen)
			if got != tt.wantCanHandle {
				t.Errorf("CanHandle(%d) = %v, want %v (numStates=%d, maxVisited=%d)",
					tt.haystackLen, got, tt.wantCanHandle, numStates, bt.MaxVisitedSize())
			}
		})
	}
}

func TestBoundedBacktracker_SearchAt(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		at        int
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{
			name: "search from beginning", pattern: `\d+`, input: "abc123def",
			at: 0, wantStart: 3, wantEnd: 6, wantFound: true,
		},
		{
			name: "search past first match", pattern: `\d+`, input: "12abc34",
			at: 3, wantStart: 5, wantEnd: 7, wantFound: true,
		},
		{
			name: "search from exact match start", pattern: "foo", input: "xxxfoo",
			at: 3, wantStart: 3, wantEnd: 6, wantFound: true,
		},
		{
			name: "search past all matches", pattern: "foo", input: "foo",
			at: 1, wantStart: -1, wantEnd: -1, wantFound: false,
		},
		{
			name: "search at end", pattern: "a", input: "bca",
			at: 3, wantStart: -1, wantEnd: -1, wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(nfa)

			start, end, found := bt.SearchAt([]byte(tt.input), tt.at)
			if found != tt.wantFound {
				t.Errorf("SearchAt found = %v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("SearchAt = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestBoundedBacktracker_IsMatchAnchored(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"match at start", "hello", "hello world", true},
		{"no match at start", "world", "hello world", false},
		{"empty pattern", "", "anything", true},
		{"empty input", "a", "", false},
		{"full match", "abc", "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(nfa)

			got := bt.IsMatchAnchored([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatchAnchored(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
			}
		})
	}
}

func TestBoundedBacktracker_WithState_ThreadSafety(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)

	state1 := NewBacktrackerState()
	state2 := NewBacktrackerState()

	input := []byte("abc123def456")

	// Both states should produce the same result independently
	got1 := bt.IsMatchWithState(input, state1)
	got2 := bt.IsMatchWithState(input, state2)

	if got1 != got2 {
		t.Errorf("IsMatchWithState results differ: state1=%v, state2=%v", got1, got2)
	}

	// Search with separate states
	s1start, s1end, s1found := bt.SearchWithState(input, state1)
	s2start, s2end, s2found := bt.SearchWithState(input, state2)

	if s1found != s2found || s1start != s2start || s1end != s2end {
		t.Errorf("SearchWithState results differ: state1=(%d,%d,%v), state2=(%d,%d,%v)",
			s1start, s1end, s1found, s2start, s2end, s2found)
	}
}

func TestBoundedBacktracker_IsMatchAnchoredWithState(t *testing.T) {
	nfa := compileNFAForTest("hello")
	bt := NewBoundedBacktracker(nfa)
	state := NewBacktrackerState()

	if !bt.IsMatchAnchoredWithState([]byte("hello world"), state) {
		t.Error("IsMatchAnchoredWithState should match 'hello' at start of 'hello world'")
	}
	if bt.IsMatchAnchoredWithState([]byte("say hello"), state) {
		t.Error("IsMatchAnchoredWithState should not match 'hello' at start of 'say hello'")
	}
}

func TestBoundedBacktracker_SearchAtWithState(t *testing.T) {
	nfa := compileNFAForTest(`\d+`)
	bt := NewBoundedBacktracker(nfa)
	state := NewBacktrackerState()

	start, end, found := bt.SearchAtWithState([]byte("abc123def"), 0, state)
	if !found || start != 3 || end != 6 {
		t.Errorf("SearchAtWithState = (%d, %d, %v), want (3, 6, true)", start, end, found)
	}
}

func TestBoundedBacktracker_LargeInputNotHandled(t *testing.T) {
	nfa := compileNFAForTest(`\w+`)
	bt := NewBoundedBacktracker(nfa)

	// Generate input larger than what CanHandle allows
	maxInput := bt.MaxInputSize()
	largeInput := make([]byte, maxInput+100)
	for i := range largeInput {
		largeInput[i] = 'a'
	}

	// Should return false/not found
	if bt.IsMatch(largeInput) {
		t.Error("IsMatch should return false for input exceeding MaxInputSize")
	}

	start, end, found := bt.Search(largeInput)
	if found {
		t.Errorf("Search should return not found for large input, got (%d, %d)", start, end)
	}
}
