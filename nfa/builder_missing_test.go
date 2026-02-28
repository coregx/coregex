package nfa

import (
	"testing"
)

func TestBuilder_AddFail(t *testing.T) {
	b := NewBuilder()

	failID := b.AddFail()
	match := b.AddMatch()
	b.SetStart(match)

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	s := n.State(failID)
	if s == nil {
		t.Fatal("Fail state is nil")
	}
	if s.Kind() != StateFail {
		t.Errorf("Kind() = %v, want StateFail", s.Kind())
	}
	if s.ID() != failID {
		t.Errorf("ID() = %d, want %d", s.ID(), failID)
	}
}

func TestBuilder_AddRuneAny(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	runeAnyID := b.AddRuneAny(match)
	b.SetStart(runeAnyID)

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	s := n.State(runeAnyID)
	if s == nil {
		t.Fatal("RuneAny state is nil")
	}
	if s.Kind() != StateRuneAny {
		t.Errorf("Kind() = %v, want StateRuneAny", s.Kind())
	}
	if next := s.RuneAny(); next != match {
		t.Errorf("RuneAny() = %d, want %d", next, match)
	}

	// Verify matching via PikeVM
	vm := NewPikeVM(n)
	_, _, matched := vm.Search([]byte("x"))
	if !matched {
		t.Error("RuneAny state should match any byte")
	}
}

func TestBuilder_AddRuneAnyNotNL(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	runeAnyNotNLID := b.AddRuneAnyNotNL(match)
	b.SetStart(runeAnyNotNLID)

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	s := n.State(runeAnyNotNLID)
	if s == nil {
		t.Fatal("RuneAnyNotNL state is nil")
	}
	if s.Kind() != StateRuneAnyNotNL {
		t.Errorf("Kind() = %v, want StateRuneAnyNotNL", s.Kind())
	}
	if next := s.RuneAnyNotNL(); next != match {
		t.Errorf("RuneAnyNotNL() = %d, want %d", next, match)
	}
}

func TestBuilder_PatchSplit(t *testing.T) {
	t.Run("successful patch", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		stateA := b.AddByteRange('a', 'a', match)
		stateB := b.AddByteRange('b', 'b', match)
		splitID := b.AddSplit(InvalidState, InvalidState)
		b.SetStart(splitID)

		// Patch the split targets
		err := b.PatchSplit(splitID, stateA, stateB)
		if err != nil {
			t.Fatalf("PatchSplit error: %v", err)
		}

		n, err := b.Build()
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		// Verify the split was patched
		s := n.State(splitID)
		left, right := s.Split()
		if left != stateA || right != stateB {
			t.Errorf("Split() = (%d, %d), want (%d, %d)", left, right, stateA, stateB)
		}

		// Verify matching
		vm := NewPikeVM(n)
		_, _, matched := vm.Search([]byte("a"))
		if !matched {
			t.Error("should match 'a' via left branch")
		}
		_, _, matched = vm.Search([]byte("b"))
		if !matched {
			t.Error("should match 'b' via right branch")
		}
	})

	t.Run("out of bounds state ID", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()

		err := b.PatchSplit(StateID(999), 0, 0)
		if err == nil {
			t.Error("PatchSplit with out-of-bounds ID should return error")
		}
	})

	t.Run("non-split state", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()

		err := b.PatchSplit(match, 0, 0)
		if err == nil {
			t.Error("PatchSplit on non-Split state should return error")
		}
	})
}

func TestBuilder_Patch(t *testing.T) {
	t.Run("patch ByteRange", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		brID := b.AddByteRange('x', 'x', InvalidState)
		b.SetStart(brID)

		err := b.Patch(brID, match)
		if err != nil {
			t.Fatalf("Patch error: %v", err)
		}

		n, err := b.Build()
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		s := n.State(brID)
		_, _, next := s.ByteRange()
		if next != match {
			t.Errorf("ByteRange next = %d, want %d", next, match)
		}
	})

	t.Run("patch Epsilon", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		epsID := b.AddEpsilon(InvalidState)
		b.SetStart(epsID)

		err := b.Patch(epsID, match)
		if err != nil {
			t.Fatalf("Patch error: %v", err)
		}

		n, err := b.Build()
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		s := n.State(epsID)
		if s.Epsilon() != match {
			t.Errorf("Epsilon() = %d, want %d", s.Epsilon(), match)
		}
	})

	t.Run("patch out of bounds", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()

		err := b.Patch(StateID(999), 0)
		if err == nil {
			t.Error("Patch with out-of-bounds ID should return error")
		}
	})

	t.Run("patch Split fails", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()
		splitID := b.AddSplit(0, 0)

		err := b.Patch(splitID, 0)
		if err == nil {
			t.Error("Patch on Split state should return error")
		}
	})

	t.Run("patch Capture", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		capID := b.AddCapture(1, true, InvalidState)
		b.SetStart(capID)

		err := b.Patch(capID, match)
		if err != nil {
			t.Fatalf("Patch error: %v", err)
		}

		n, err := b.Build()
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		s := n.State(capID)
		_, _, next := s.Capture()
		if next != match {
			t.Errorf("Capture next = %d, want %d", next, match)
		}
	})

	t.Run("patch Look", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		lookID := b.AddLook(LookStartText, InvalidState)
		b.SetStart(lookID)

		err := b.Patch(lookID, match)
		if err != nil {
			t.Fatalf("Patch error: %v", err)
		}

		n, err := b.Build()
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		s := n.State(lookID)
		_, next := s.Look()
		if next != match {
			t.Errorf("Look next = %d, want %d", next, match)
		}
	})

	t.Run("patch RuneAny", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		raID := b.AddRuneAny(InvalidState)
		b.SetStart(raID)

		err := b.Patch(raID, match)
		if err != nil {
			t.Fatalf("Patch error: %v", err)
		}

		n, err := b.Build()
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		s := n.State(raID)
		if s.RuneAny() != match {
			t.Errorf("RuneAny() = %d, want %d", s.RuneAny(), match)
		}
	})

	t.Run("patch RuneAnyNotNL", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		ranlID := b.AddRuneAnyNotNL(InvalidState)
		b.SetStart(ranlID)

		err := b.Patch(ranlID, match)
		if err != nil {
			t.Fatalf("Patch error: %v", err)
		}

		n, err := b.Build()
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		s := n.State(ranlID)
		if s.RuneAnyNotNL() != match {
			t.Errorf("RuneAnyNotNL() = %d, want %d", s.RuneAnyNotNL(), match)
		}
	})
}

func TestBuilder_AddQuantifierSplit(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	bodyA := b.AddByteRange('a', 'a', InvalidState)
	qSplit := b.AddQuantifierSplit(bodyA, match)
	b.SetStart(qSplit)

	// Patch bodyA to loop back to qSplit (a* pattern)
	err := b.Patch(bodyA, qSplit)
	if err != nil {
		t.Fatalf("Patch error: %v", err)
	}

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	s := n.State(qSplit)
	if !s.IsQuantifierSplit() {
		t.Error("IsQuantifierSplit() should be true")
	}

	left, right := s.Split()
	if left != bodyA || right != match {
		t.Errorf("Split() = (%d, %d), want (%d, %d)", left, right, bodyA, match)
	}
}

func TestBuilder_AddSparse(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	transitions := []Transition{
		{Lo: 'a', Hi: 'z', Next: match},
		{Lo: '0', Hi: '9', Next: match},
	}
	sparseID := b.AddSparse(transitions)
	b.SetStart(sparseID)

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	s := n.State(sparseID)
	if s.Kind() != StateSparse {
		t.Errorf("Kind() = %v, want StateSparse", s.Kind())
	}

	got := s.Transitions()
	if len(got) != 2 {
		t.Fatalf("Transitions() len = %d, want 2", len(got))
	}
	if got[0].Lo != 'a' || got[0].Hi != 'z' {
		t.Errorf("Transitions()[0] = [%c-%c], want [a-z]", got[0].Lo, got[0].Hi)
	}
	if got[1].Lo != '0' || got[1].Hi != '9' {
		t.Errorf("Transitions()[1] = [%c-%c], want [0-9]", got[1].Lo, got[1].Hi)
	}

	// Verify transitions were copied (not aliased)
	transitions[0].Lo = 'X'
	gotAfter := n.State(sparseID).Transitions()
	if gotAfter[0].Lo != 'a' {
		t.Error("Sparse transitions should be copied, not aliased")
	}
}

func TestBuilder_AddCapture(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()

	// Add closing capture (group 1)
	closeCapID := b.AddCapture(1, false, match)
	// Add byte range inside capture
	byteID := b.AddByteRange('a', 'a', closeCapID)
	// Add opening capture (group 1)
	openCapID := b.AddCapture(1, true, byteID)

	b.SetStart(openCapID)

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	// Verify opening capture
	openState := n.State(openCapID)
	idx, isStart, next := openState.Capture()
	if idx != 1 || !isStart || next != byteID {
		t.Errorf("Open capture: (%d, %v, %d), want (1, true, %d)", idx, isStart, next, byteID)
	}

	// Verify closing capture
	closeState := n.State(closeCapID)
	idx, isStart, next = closeState.Capture()
	if idx != 1 || isStart || next != match {
		t.Errorf("Close capture: (%d, %v, %d), want (1, false, %d)", idx, isStart, next, match)
	}
}

func TestBuilder_AddLook(t *testing.T) {
	tests := []struct {
		name string
		look Look
	}{
		{"StartText", LookStartText},
		{"EndText", LookEndText},
		{"StartLine", LookStartLine},
		{"EndLine", LookEndLine},
		{"WordBoundary", LookWordBoundary},
		{"NoWordBoundary", LookNoWordBoundary},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBuilder()
			match := b.AddMatch()
			lookID := b.AddLook(tt.look, match)
			b.SetStart(lookID)

			n, err := b.Build()
			if err != nil {
				t.Fatalf("Build error: %v", err)
			}

			s := n.State(lookID)
			if s.Kind() != StateLook {
				t.Errorf("Kind() = %v, want StateLook", s.Kind())
			}

			gotLook, gotNext := s.Look()
			if gotLook != tt.look || gotNext != match {
				t.Errorf("Look() = (%d, %d), want (%d, %d)", gotLook, gotNext, tt.look, match)
			}
		})
	}
}

func TestBuilder_SetStarts(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	anchoredStart := b.AddByteRange('a', 'a', match)
	unanchoredStart := b.AddEpsilon(anchoredStart)

	b.SetStarts(anchoredStart, unanchoredStart)

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	if n.StartAnchored() != anchoredStart {
		t.Errorf("StartAnchored() = %d, want %d", n.StartAnchored(), anchoredStart)
	}
	if n.StartUnanchored() != unanchoredStart {
		t.Errorf("StartUnanchored() = %d, want %d", n.StartUnanchored(), unanchoredStart)
	}
	if n.IsAlwaysAnchored() {
		t.Error("IsAlwaysAnchored() should be false when starts differ")
	}
}

func TestBuilder_States(t *testing.T) {
	b := NewBuilder()
	if b.States() != 0 {
		t.Errorf("States() = %d, want 0", b.States())
	}

	b.AddMatch()
	if b.States() != 1 {
		t.Errorf("States() = %d, want 1", b.States())
	}

	b.AddByteRange('a', 'a', 0)
	b.AddSplit(0, 0)
	if b.States() != 3 {
		t.Errorf("States() = %d, want 3", b.States())
	}
}

func TestBuilder_Validate(t *testing.T) {
	t.Run("no anchored start", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()
		// Don't set start
		err := b.Validate()
		if err == nil {
			t.Error("Validate() should fail when start not set")
		}
	})

	t.Run("anchored start out of bounds", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()
		b.startAnchored = StateID(999)
		b.startUnanchored = 0
		err := b.Validate()
		if err == nil {
			t.Error("Validate() should fail when anchored start out of bounds")
		}
	})

	t.Run("unanchored start not set", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()
		b.startAnchored = 0
		b.startUnanchored = InvalidState
		err := b.Validate()
		if err == nil {
			t.Error("Validate() should fail when unanchored start not set")
		}
	})

	t.Run("unanchored start out of bounds", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()
		b.startAnchored = 0
		b.startUnanchored = StateID(999)
		err := b.Validate()
		if err == nil {
			t.Error("Validate() should fail when unanchored start out of bounds")
		}
	})

	t.Run("invalid ByteRange next", func(t *testing.T) {
		b := NewBuilder()
		b.AddByteRange('a', 'a', StateID(999))
		b.SetStart(0)
		err := b.Validate()
		if err == nil {
			t.Error("Validate() should fail for invalid ByteRange next state")
		}
	})

	t.Run("invalid Split left", func(t *testing.T) {
		b := NewBuilder()
		b.AddSplit(StateID(999), 0)
		b.AddMatch()
		b.SetStart(0)
		err := b.Validate()
		if err == nil {
			t.Error("Validate() should fail for invalid Split left state")
		}
	})

	t.Run("invalid Split right", func(t *testing.T) {
		b := NewBuilder()
		b.AddMatch()
		b.AddSplit(0, StateID(999))
		b.SetStart(1)
		err := b.Validate()
		if err == nil {
			t.Error("Validate() should fail for invalid Split right state")
		}
	})

	t.Run("invalid Sparse transition next", func(t *testing.T) {
		b := NewBuilder()
		b.AddSparse([]Transition{{Lo: 'a', Hi: 'z', Next: StateID(999)}})
		b.SetStart(0)
		err := b.Validate()
		if err == nil {
			t.Error("Validate() should fail for invalid Sparse transition target")
		}
	})

	t.Run("valid NFA passes", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		byteRange := b.AddByteRange('a', 'a', match)
		b.SetStart(byteRange)
		err := b.Validate()
		if err != nil {
			t.Errorf("Validate() should pass for valid NFA, got: %v", err)
		}
	})
}

func TestBuilder_BuildOptions(t *testing.T) {
	t.Run("WithPatternCount", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		b.SetStart(match)

		n, err := b.Build(WithPatternCount(5))
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}
		if n.PatternCount() != 5 {
			t.Errorf("PatternCount() = %d, want 5", n.PatternCount())
		}
	})

	t.Run("WithCaptureCount", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		b.SetStart(match)

		n, err := b.Build(WithCaptureCount(3))
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}
		if n.CaptureCount() != 3 {
			t.Errorf("CaptureCount() = %d, want 3", n.CaptureCount())
		}
	})

	t.Run("WithCaptureNames", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		b.SetStart(match)

		names := []string{"", "year", "month", "day"}
		n, err := b.Build(WithCaptureCount(4), WithCaptureNames(names))
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		got := n.SubexpNames()
		if len(got) != len(names) {
			t.Fatalf("SubexpNames() len = %d, want %d", len(got), len(names))
		}
		for i, name := range names {
			if got[i] != name {
				t.Errorf("SubexpNames()[%d] = %q, want %q", i, got[i], name)
			}
		}

		// Verify names are copied (not aliased)
		names[1] = "MODIFIED"
		got2 := n.SubexpNames()
		if got2[1] != "year" {
			t.Error("CaptureNames should be copied, not aliased")
		}
	})

	t.Run("WithCaptureNames empty", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		b.SetStart(match)

		n, err := b.Build(WithCaptureNames(nil))
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}

		// Should not panic
		_ = n.SubexpNames()
	})

	t.Run("multiple options", func(t *testing.T) {
		b := NewBuilder()
		match := b.AddMatch()
		b.SetStart(match)

		n, err := b.Build(
			WithAnchored(true),
			WithUTF8(false),
			WithPatternCount(2),
			WithCaptureCount(1),
		)
		if err != nil {
			t.Fatalf("Build error: %v", err)
		}
		if !n.IsAnchored() {
			t.Error("IsAnchored() should be true")
		}
		if n.IsUTF8() {
			t.Error("IsUTF8() should be false")
		}
		if n.PatternCount() != 2 {
			t.Errorf("PatternCount() = %d, want 2", n.PatternCount())
		}
		if n.CaptureCount() != 1 {
			t.Errorf("CaptureCount() = %d, want 1", n.CaptureCount())
		}
	})
}

func TestBuilder_BuildCompleteNFA(t *testing.T) {
	// Build a complete NFA for the pattern: a(b|c)d
	// Structure: a -> split(b, c) -> d -> match
	b := NewBuilder()
	match := b.AddMatch()
	stateD := b.AddByteRange('d', 'd', match)
	stateB := b.AddByteRange('b', 'b', stateD)
	stateC := b.AddByteRange('c', 'c', stateD)
	split := b.AddSplit(stateB, stateC)
	stateA := b.AddByteRange('a', 'a', split)

	b.SetStart(stateA)

	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	vm := NewPikeVM(n)

	tests := []struct {
		input string
		want  bool
	}{
		{"abd", true},
		{"acd", true},
		{"abc", false},
		{"ad", false},
		{"xabdx", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, _, got := vm.Search([]byte(tt.input))
			if got != tt.want {
				t.Errorf("Search(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewBuilderWithCapacity(t *testing.T) {
	b := NewBuilderWithCapacity(100)
	if b == nil {
		t.Fatal("NewBuilderWithCapacity returned nil")
	}
	if b.States() != 0 {
		t.Errorf("States() = %d, want 0", b.States())
	}

	// Should work normally
	match := b.AddMatch()
	b.SetStart(match)
	_, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
}
