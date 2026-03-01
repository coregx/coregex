package nfa

import (
	"strings"
	"testing"
)

func TestStateKind_String(t *testing.T) {
	tests := []struct {
		kind StateKind
		want string
	}{
		{StateMatch, "Match"},
		{StateByteRange, "ByteRange"},
		{StateSparse, "Sparse"},
		{StateSplit, "Split"},
		{StateEpsilon, "Epsilon"},
		{StateCapture, "Capture"},
		{StateFail, "Fail"},
		{StateLook, "Look"},
		{StateRuneAny, "RuneAny"},
		{StateRuneAnyNotNL, "RuneAnyNotNL"},
		{StateKind(255), "Unknown(255)"},
		{StateKind(42), "Unknown(42)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.kind.String()
			if got != tt.want {
				t.Errorf("StateKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestState_String_AllKinds(t *testing.T) {
	tests := []struct {
		name    string
		state   State
		wantSub string
	}{
		{
			name:    "Match",
			state:   State{id: 0, kind: StateMatch},
			wantSub: "State(0, Match)",
		},
		{
			name:    "ByteRange single char",
			state:   State{id: 1, kind: StateByteRange, lo: 'a', hi: 'a', next: 2},
			wantSub: "State(1, ByteRange 'a' -> 2)",
		},
		{
			name:    "ByteRange range",
			state:   State{id: 2, kind: StateByteRange, lo: 'a', hi: 'z', next: 3},
			wantSub: "State(2, ByteRange ['a'-'z'] -> 3)",
		},
		{
			name: "Sparse",
			state: State{
				id:   3,
				kind: StateSparse,
				transitions: []Transition{
					{Lo: 'a', Hi: 'z', Next: 4},
					{Lo: '0', Hi: '9', Next: 5},
				},
			},
			wantSub: "State(3, Sparse 2 transitions)",
		},
		{
			name:    "Split",
			state:   State{id: 4, kind: StateSplit, left: 5, right: 6},
			wantSub: "State(4, Split -> [5, 6])",
		},
		{
			name:    "Epsilon",
			state:   State{id: 5, kind: StateEpsilon, next: 6},
			wantSub: "State(5, Epsilon -> 6)",
		},
		{
			name:    "Fail",
			state:   State{id: 6, kind: StateFail},
			wantSub: "State(6, Fail)",
		},
		{
			name:    "Look StartText",
			state:   State{id: 7, kind: StateLook, look: LookStartText, next: 8},
			wantSub: "State(7, Look(StartText) -> 8)",
		},
		{
			name:    "Look EndText",
			state:   State{id: 8, kind: StateLook, look: LookEndText, next: 9},
			wantSub: "State(8, Look(EndText) -> 9)",
		},
		{
			name:    "Look StartLine",
			state:   State{id: 9, kind: StateLook, look: LookStartLine, next: 10},
			wantSub: "State(9, Look(StartLine) -> 10)",
		},
		{
			name:    "Look EndLine",
			state:   State{id: 10, kind: StateLook, look: LookEndLine, next: 11},
			wantSub: "State(10, Look(EndLine) -> 11)",
		},
		{
			name:    "Look unknown",
			state:   State{id: 11, kind: StateLook, look: Look(99), next: 12},
			wantSub: "State(11, Look(Unknown) -> 12)",
		},
		{
			name:    "RuneAny",
			state:   State{id: 12, kind: StateRuneAny, next: 13},
			wantSub: "State(12, RuneAny -> 13)",
		},
		{
			name:    "RuneAnyNotNL",
			state:   State{id: 13, kind: StateRuneAnyNotNL, next: 14},
			wantSub: "State(13, RuneAnyNotNL -> 14)",
		},
		{
			name:    "Unknown kind",
			state:   State{id: 14, kind: StateKind(200)},
			wantSub: "State(14, Unknown)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.String()
			if got != tt.wantSub {
				t.Errorf("State.String() = %q, want %q", got, tt.wantSub)
			}
		})
	}
}

func TestState_Accessors_ByteRange(t *testing.T) {
	s := &State{id: 1, kind: StateByteRange, lo: 'a', hi: 'z', next: 2}

	lo, hi, next := s.ByteRange()
	if lo != 'a' || hi != 'z' || next != 2 {
		t.Errorf("ByteRange() = (%c, %c, %d), want (a, z, 2)", lo, hi, next)
	}

	// Non-ByteRange state returns zeros
	nonBR := &State{id: 2, kind: StateMatch}
	lo, hi, next = nonBR.ByteRange()
	if lo != 0 || hi != 0 || next != InvalidState {
		t.Errorf("ByteRange() on non-ByteRange = (%d, %d, %d), want (0, 0, InvalidState)", lo, hi, next)
	}
}

func TestState_Accessors_Split(t *testing.T) {
	s := &State{id: 1, kind: StateSplit, left: 2, right: 3}

	left, right := s.Split()
	if left != 2 || right != 3 {
		t.Errorf("Split() = (%d, %d), want (2, 3)", left, right)
	}

	// Non-Split state returns InvalidState
	nonSplit := &State{id: 2, kind: StateMatch}
	left, right = nonSplit.Split()
	if left != InvalidState || right != InvalidState {
		t.Errorf("Split() on non-Split = (%d, %d), want (InvalidState, InvalidState)", left, right)
	}
}

func TestState_Accessors_Epsilon(t *testing.T) {
	s := &State{id: 1, kind: StateEpsilon, next: 5}

	next := s.Epsilon()
	if next != 5 {
		t.Errorf("Epsilon() = %d, want 5", next)
	}

	// Non-Epsilon state returns InvalidState
	nonEps := &State{id: 2, kind: StateMatch}
	next = nonEps.Epsilon()
	if next != InvalidState {
		t.Errorf("Epsilon() on non-Epsilon = %d, want InvalidState", next)
	}
}

func TestState_Accessors_Transitions(t *testing.T) {
	trans := []Transition{
		{Lo: 'a', Hi: 'z', Next: 1},
		{Lo: '0', Hi: '9', Next: 2},
	}
	s := &State{id: 1, kind: StateSparse, transitions: trans}

	got := s.Transitions()
	if len(got) != 2 {
		t.Fatalf("Transitions() returned %d transitions, want 2", len(got))
	}
	if got[0].Lo != 'a' || got[0].Hi != 'z' || got[0].Next != 1 {
		t.Errorf("Transitions()[0] = %+v, want {a, z, 1}", got[0])
	}

	// Non-Sparse state returns nil
	nonSparse := &State{id: 2, kind: StateMatch}
	if nonSparse.Transitions() != nil {
		t.Error("Transitions() on non-Sparse should return nil")
	}
}

func TestState_Accessors_Capture(t *testing.T) {
	s := &State{id: 1, kind: StateCapture, captureIndex: 2, captureStart: true, next: 5}

	idx, isStart, next := s.Capture()
	if idx != 2 || !isStart || next != 5 {
		t.Errorf("Capture() = (%d, %v, %d), want (2, true, 5)", idx, isStart, next)
	}

	// Closing capture
	sClose := &State{id: 2, kind: StateCapture, captureIndex: 2, captureStart: false, next: 6}
	idx, isStart, next = sClose.Capture()
	if idx != 2 || isStart || next != 6 {
		t.Errorf("Capture() = (%d, %v, %d), want (2, false, 6)", idx, isStart, next)
	}

	// Non-Capture state
	nonCap := &State{id: 3, kind: StateMatch}
	idx, isStart, next = nonCap.Capture()
	if idx != 0 || isStart || next != InvalidState {
		t.Errorf("Capture() on non-Capture = (%d, %v, %d), want (0, false, InvalidState)", idx, isStart, next)
	}
}

func TestState_Accessors_Look(t *testing.T) {
	tests := []struct {
		name     string
		look     Look
		next     StateID
		wantLook Look
		wantNext StateID
	}{
		{"StartText", LookStartText, 1, LookStartText, 1},
		{"EndText", LookEndText, 2, LookEndText, 2},
		{"StartLine", LookStartLine, 3, LookStartLine, 3},
		{"EndLine", LookEndLine, 4, LookEndLine, 4},
		{"WordBoundary", LookWordBoundary, 5, LookWordBoundary, 5},
		{"NoWordBoundary", LookNoWordBoundary, 6, LookNoWordBoundary, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{id: 0, kind: StateLook, look: tt.look, next: tt.next}
			gotLook, gotNext := s.Look()
			if gotLook != tt.wantLook || gotNext != tt.wantNext {
				t.Errorf("Look() = (%d, %d), want (%d, %d)", gotLook, gotNext, tt.wantLook, tt.wantNext)
			}
		})
	}

	// Non-Look state
	nonLook := &State{id: 0, kind: StateMatch}
	look, next := nonLook.Look()
	if look != 0 || next != InvalidState {
		t.Errorf("Look() on non-Look = (%d, %d), want (0, InvalidState)", look, next)
	}
}

func TestState_Accessors_RuneAny(t *testing.T) {
	s := &State{id: 1, kind: StateRuneAny, next: 5}
	got := s.RuneAny()
	if got != 5 {
		t.Errorf("RuneAny() = %d, want 5", got)
	}

	// Non-RuneAny state
	nonRA := &State{id: 2, kind: StateMatch}
	got = nonRA.RuneAny()
	if got != InvalidState {
		t.Errorf("RuneAny() on non-RuneAny = %d, want InvalidState", got)
	}
}

func TestState_Accessors_RuneAnyNotNL(t *testing.T) {
	s := &State{id: 1, kind: StateRuneAnyNotNL, next: 5}
	got := s.RuneAnyNotNL()
	if got != 5 {
		t.Errorf("RuneAnyNotNL() = %d, want 5", got)
	}

	// Non-RuneAnyNotNL state
	nonRANL := &State{id: 2, kind: StateMatch}
	got = nonRANL.RuneAnyNotNL()
	if got != InvalidState {
		t.Errorf("RuneAnyNotNL() on non-RuneAnyNotNL = %d, want InvalidState", got)
	}
}

func TestState_IsMatch(t *testing.T) {
	matchState := &State{id: 0, kind: StateMatch}
	if !matchState.IsMatch() {
		t.Error("IsMatch() should return true for Match state")
	}

	nonMatchState := &State{id: 1, kind: StateByteRange}
	if nonMatchState.IsMatch() {
		t.Error("IsMatch() should return false for non-Match state")
	}
}

func TestState_IsQuantifierSplit(t *testing.T) {
	qSplit := &State{id: 0, kind: StateSplit, left: 1, right: 2, isQuantifierSplit: true}
	if !qSplit.IsQuantifierSplit() {
		t.Error("IsQuantifierSplit() should return true for quantifier split")
	}

	altSplit := &State{id: 1, kind: StateSplit, left: 2, right: 3, isQuantifierSplit: false}
	if altSplit.IsQuantifierSplit() {
		t.Error("IsQuantifierSplit() should return false for alternation split")
	}

	nonSplit := &State{id: 2, kind: StateByteRange}
	if nonSplit.IsQuantifierSplit() {
		t.Error("IsQuantifierSplit() should return false for non-Split state")
	}
}

func TestNFA_IsAlwaysAnchored(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{name: "anchored start", pattern: "^abc", want: true},
		{name: "anchored both", pattern: "^abc$", want: true},
		{name: "unanchored", pattern: "abc", want: false},
		{name: "unanchored with star", pattern: ".*abc", want: false},
		{name: "anchored end only", pattern: "abc$", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			n, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := n.IsAlwaysAnchored()
			if got != tt.want {
				t.Errorf("IsAlwaysAnchored() = %v, want %v (startAnchored=%d, startUnanchored=%d)",
					got, tt.want, n.StartAnchored(), n.StartUnanchored())
			}
		})
	}
}

func TestNFA_State_InvalidID(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("abc")
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	// InvalidState constant
	if n.State(InvalidState) != nil {
		t.Error("State(InvalidState) should return nil")
	}

	// Out of bounds
	if n.State(StateID(n.States()+10)) != nil {
		t.Error("State(out of bounds) should return nil")
	}

	// Valid state
	if n.State(n.StartAnchored()) == nil {
		t.Error("State(startAnchored) should not return nil")
	}
}

func TestNFA_IsMatch_ID(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	byteRange := b.AddByteRange('a', 'a', match)
	b.SetStart(byteRange)

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	if !n.IsMatch(match) {
		t.Error("IsMatch(match) should return true")
	}
	if n.IsMatch(byteRange) {
		t.Error("IsMatch(byteRange) should return false")
	}
	if n.IsMatch(InvalidState) {
		t.Error("IsMatch(InvalidState) should return false")
	}
}

func TestNFA_Properties(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile(`(\w+)`)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	if n.States() == 0 {
		t.Error("States() should be > 0")
	}

	if n.PatternCount() != 1 {
		t.Errorf("PatternCount() = %d, want 1", n.PatternCount())
	}

	// Should have at least group 0 (entire match) + group 1
	if n.CaptureCount() < 2 {
		t.Errorf("CaptureCount() = %d, want >= 2", n.CaptureCount())
	}

	if n.ByteClasses() == nil {
		t.Error("ByteClasses() should not be nil")
	}
}

func TestNFA_SubexpNames(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			name:    "no captures",
			pattern: "abc",
			want:    nil,
		},
		{
			name:    "unnamed capture",
			pattern: "(abc)",
			want:    []string{"", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			n, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := n.SubexpNames()
			if tt.want == nil {
				// Just verify it doesn't panic and returns valid slice
				if got == nil && n.CaptureCount() > 0 {
					t.Errorf("SubexpNames() returned nil but CaptureCount = %d", n.CaptureCount())
				}
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("SubexpNames() len = %d, want %d", len(got), len(tt.want))
			}
		})
	}
}

func TestNFA_String(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("abc")
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	s := n.String()
	if !strings.Contains(s, "NFA{") {
		t.Errorf("String() = %q, should contain 'NFA{'", s)
	}
	if !strings.Contains(s, "states:") {
		t.Errorf("String() = %q, should contain 'states:'", s)
	}
}

func TestNFA_Iter_CompleteCoverage(t *testing.T) {
	compiler := NewDefaultCompiler()
	n, err := compiler.Compile("[a-z]+")
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	iter := n.Iter()

	// HasNext should be true initially
	if !iter.HasNext() {
		t.Fatal("HasNext() should be true initially")
	}

	// Iterate all states
	count := 0
	for iter.HasNext() {
		s := iter.Next()
		if s == nil {
			t.Fatalf("Next() returned nil at position %d", count)
		}
		count++
	}

	if count != n.States() {
		t.Errorf("iterated %d states, NFA has %d", count, n.States())
	}

	// After iteration, HasNext should be false
	if iter.HasNext() {
		t.Error("HasNext() should be false after full iteration")
	}

	// Next after exhaustion should return nil
	if iter.Next() != nil {
		t.Error("Next() should return nil after exhaustion")
	}
}

func TestNFA_IsUTF8(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	b.SetStart(match)

	// Default should be UTF-8
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !n.IsUTF8() {
		t.Error("IsUTF8() should be true by default")
	}

	// With UTF-8 disabled
	b2 := NewBuilder()
	match2 := b2.AddMatch()
	b2.SetStart(match2)
	n2, err := b2.Build(WithUTF8(false))
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if n2.IsUTF8() {
		t.Error("IsUTF8() should be false when WithUTF8(false)")
	}
}

func TestNFA_IsAnchored(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	b.SetStart(match)

	// Default not anchored
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if n.IsAnchored() {
		t.Error("IsAnchored() should be false by default")
	}

	// With anchored
	b2 := NewBuilder()
	match2 := b2.AddMatch()
	b2.SetStart(match2)
	n2, err := b2.Build(WithAnchored(true))
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !n2.IsAnchored() {
		t.Error("IsAnchored() should be true when WithAnchored(true)")
	}
}
