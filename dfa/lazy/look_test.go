package lazy

import (
	"testing"

	"github.com/coregx/coregex/nfa"
)

func TestLookSetInsertAndContains(t *testing.T) {
	tests := []struct {
		name    string
		look    nfa.Look
		lookSet LookSet
	}{
		{name: "StartText", look: nfa.LookStartText, lookSet: LookStartText},
		{name: "EndText", look: nfa.LookEndText, lookSet: LookEndText},
		{name: "StartLine", look: nfa.LookStartLine, lookSet: LookStartLine},
		{name: "EndLine", look: nfa.LookEndLine, lookSet: LookEndLine},
		{name: "WordBoundary", look: nfa.LookWordBoundary, lookSet: LookWordBoundary},
		{name: "NoWordBoundary", look: nfa.LookNoWordBoundary, lookSet: LookNoWordBoundary},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s LookSet

			// Initially empty - should not contain any look
			if s.Contains(tt.look) {
				t.Errorf("Empty LookSet should not contain %v", tt.name)
			}

			// Insert and verify
			s = s.Insert(tt.look)
			if !s.Contains(tt.look) {
				t.Errorf("After Insert(%v), Contains() should return true", tt.name)
			}

			// Verify the underlying bits match expected
			if s&tt.lookSet == 0 {
				t.Errorf("After Insert(%v), expected bit %v to be set", tt.name, tt.lookSet)
			}
		})
	}
}

func TestLookSetEmptyBehavior(t *testing.T) {
	s := LookNone

	// Empty set should not contain any look assertion
	allLooks := []nfa.Look{
		nfa.LookStartText,
		nfa.LookEndText,
		nfa.LookStartLine,
		nfa.LookEndLine,
		nfa.LookWordBoundary,
		nfa.LookNoWordBoundary,
	}

	for _, look := range allLooks {
		if s.Contains(look) {
			t.Errorf("LookNone should not contain %v", look)
		}
	}

	// LookNone value should be 0
	if s != 0 {
		t.Errorf("LookNone = %d, want 0", s)
	}
}

func TestLookSetMultipleInsertions(t *testing.T) {
	// Insert multiple assertions
	s := LookNone
	s = s.Insert(nfa.LookStartText)
	s = s.Insert(nfa.LookEndText)
	s = s.Insert(nfa.LookWordBoundary)

	// Verify all inserted are present
	if !s.Contains(nfa.LookStartText) {
		t.Error("Should contain LookStartText")
	}
	if !s.Contains(nfa.LookEndText) {
		t.Error("Should contain LookEndText")
	}
	if !s.Contains(nfa.LookWordBoundary) {
		t.Error("Should contain LookWordBoundary")
	}

	// Verify non-inserted are absent
	if s.Contains(nfa.LookStartLine) {
		t.Error("Should not contain LookStartLine")
	}
	if s.Contains(nfa.LookEndLine) {
		t.Error("Should not contain LookEndLine")
	}
	if s.Contains(nfa.LookNoWordBoundary) {
		t.Error("Should not contain LookNoWordBoundary")
	}
}

func TestLookSetDuplicateInsert(t *testing.T) {
	s := LookNone
	s = s.Insert(nfa.LookStartText)
	before := s
	s = s.Insert(nfa.LookStartText) // duplicate
	if s != before {
		t.Errorf("Duplicate Insert should not change set: got %v, want %v", s, before)
	}
}

func TestLookSetInsertUnknownLook(t *testing.T) {
	s := LookNone
	// Use a Look value outside the known range
	s = s.Insert(nfa.Look(200))
	if s != LookNone {
		t.Errorf("Inserting unknown Look should not change set: got %v, want 0", s)
	}
}

func TestLookSetContainsUnknownLook(t *testing.T) {
	s := LookStartText | LookEndText | LookStartLine | LookEndLine | LookWordBoundary | LookNoWordBoundary
	// Unknown Look should return false even with all bits set
	if s.Contains(nfa.Look(200)) {
		t.Error("Contains with unknown Look should return false")
	}
}

func TestLookSetFromStartKind(t *testing.T) {
	tests := []struct {
		name          string
		kind          StartKind
		wantStartText bool
		wantStartLine bool
	}{
		{
			name:          "StartText has both StartText and StartLine",
			kind:          StartText,
			wantStartText: true,
			wantStartLine: true,
		},
		{
			name:          "StartLineLF has only StartLine",
			kind:          StartLineLF,
			wantStartText: false,
			wantStartLine: true,
		},
		{
			name:          "StartLineCR has no assertions",
			kind:          StartLineCR,
			wantStartText: false,
			wantStartLine: false,
		},
		{
			name:          "StartWord has no assertions",
			kind:          StartWord,
			wantStartText: false,
			wantStartLine: false,
		},
		{
			name:          "StartNonWord has no assertions",
			kind:          StartNonWord,
			wantStartText: false,
			wantStartLine: false,
		},
		{
			name:          "unknown StartKind has no assertions",
			kind:          StartKind(200),
			wantStartText: false,
			wantStartLine: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ls := LookSetFromStartKind(tt.kind)

			if got := ls.Contains(nfa.LookStartText); got != tt.wantStartText {
				t.Errorf("Contains(StartText) = %v, want %v", got, tt.wantStartText)
			}
			if got := ls.Contains(nfa.LookStartLine); got != tt.wantStartLine {
				t.Errorf("Contains(StartLine) = %v, want %v", got, tt.wantStartLine)
			}

			// None of the start kinds should set EndText or EndLine
			if ls.Contains(nfa.LookEndText) {
				t.Error("LookSetFromStartKind should never set EndText")
			}
			if ls.Contains(nfa.LookEndLine) {
				t.Error("LookSetFromStartKind should never set EndLine")
			}
		})
	}
}

func TestLookSetFromStartKindValues(t *testing.T) {
	// Verify exact values
	tests := []struct {
		kind StartKind
		want LookSet
	}{
		{StartText, LookStartText | LookStartLine},
		{StartLineLF, LookStartLine},
		{StartLineCR, LookNone},
		{StartWord, LookNone},
		{StartNonWord, LookNone},
	}

	for _, tt := range tests {
		got := LookSetFromStartKind(tt.kind)
		if got != tt.want {
			t.Errorf("LookSetFromStartKind(%v) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestLookSetForEOI(t *testing.T) {
	ls := LookSetForEOI()

	// Should contain EndText and EndLine
	if !ls.Contains(nfa.LookEndText) {
		t.Error("LookSetForEOI should contain EndText")
	}
	if !ls.Contains(nfa.LookEndLine) {
		t.Error("LookSetForEOI should contain EndLine")
	}

	// Should not contain StartText or StartLine
	if ls.Contains(nfa.LookStartText) {
		t.Error("LookSetForEOI should not contain StartText")
	}
	if ls.Contains(nfa.LookStartLine) {
		t.Error("LookSetForEOI should not contain StartLine")
	}

	// Verify exact value
	want := LookEndText | LookEndLine
	if ls != want {
		t.Errorf("LookSetForEOI() = %v, want %v", ls, want)
	}
}

func TestLookSetBitIndependence(t *testing.T) {
	// Verify that each Look assertion maps to a distinct bit
	allLooks := []struct {
		look nfa.Look
		name string
	}{
		{nfa.LookStartText, "StartText"},
		{nfa.LookEndText, "EndText"},
		{nfa.LookStartLine, "StartLine"},
		{nfa.LookEndLine, "EndLine"},
		{nfa.LookWordBoundary, "WordBoundary"},
		{nfa.LookNoWordBoundary, "NoWordBoundary"},
	}

	for i := 0; i < len(allLooks); i++ {
		for j := i + 1; j < len(allLooks); j++ {
			// Insert only the first look
			s := LookNone.Insert(allLooks[i].look)

			// The second look should not be present
			if s.Contains(allLooks[j].look) {
				t.Errorf("Insert(%s) should not set %s", allLooks[i].name, allLooks[j].name)
			}
		}
	}
}

func TestLookSetAllAssertions(t *testing.T) {
	// Insert all assertions
	s := LookNone
	s = s.Insert(nfa.LookStartText)
	s = s.Insert(nfa.LookEndText)
	s = s.Insert(nfa.LookStartLine)
	s = s.Insert(nfa.LookEndLine)
	s = s.Insert(nfa.LookWordBoundary)
	s = s.Insert(nfa.LookNoWordBoundary)

	// All should be present
	allLooks := []nfa.Look{
		nfa.LookStartText,
		nfa.LookEndText,
		nfa.LookStartLine,
		nfa.LookEndLine,
		nfa.LookWordBoundary,
		nfa.LookNoWordBoundary,
	}

	for _, look := range allLooks {
		if !s.Contains(look) {
			t.Errorf("Set with all assertions should contain %v", look)
		}
	}
}
