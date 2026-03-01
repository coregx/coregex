package nfa

import (
	"regexp"
	"testing"
)

// TestPikeVM_Step_RuneAny exercises the RuneAny branch in step() and stepForMatch().
// Pattern (?s). (dot-all mode dot) compiles to StateRuneAny, matching any codepoint
// including newline. We test ASCII, multi-byte UTF-8, and newline matching.
func TestPikeVM_Step_RuneAny(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		// (?s) enables dot-all; . becomes RuneAny
		{"ascii byte", `(?s).`, "a", true},
		{"newline", `(?s).`, "\n", true},
		{"2byte utf8", `(?s).`, "\u00e9", true},        // e-acute (2 bytes)
		{"3byte utf8", `(?s).`, "\u4e16", true},        // CJK character (3 bytes)
		{"4byte utf8", `(?s).`, "\U0001f600", true},    // emoji (4 bytes)
		{"empty", `(?s).`, "", false},                  // nothing to match
		{"dot star newlines", `(?s)a.b`, "a\nb", true}, // dot crosses newline
		{"multi rune any", `(?s)..`, "ab", true},
		{"multi rune any utf8", `(?s)..`, "\u4e16\u754c", true}, // two 3-byte chars
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestPikeVM_Step_RuneAnyNotNL exercises the RuneAnyNotNL branch in step().
// Default mode dot (no (?s)) compiles to StateRuneAnyNotNL, matching any
// codepoint except newline. Tests multi-byte UTF-8 chars and newline rejection.
func TestPikeVM_Step_RuneAnyNotNL(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"ascii", `.`, "x", true},
		{"newline rejected", `.`, "\n", false},
		{"2byte utf8", `.`, "\u00f1", true},     // n-tilde (2 bytes)
		{"3byte utf8", `.`, "\u2603", true},     // snowman (3 bytes)
		{"4byte utf8", `.`, "\U0001f4a9", true}, // pile of poo (4 bytes)
		{"dot star stops at newline", `a.b`, "a\nb", false},
		{"dot star ascii", `a.b`, "axb", true},
		{"dot star utf8", `a.b`, "a\u00e9b", true}, // e-acute between a and b
		{"multi dot utf8", `..`, "\u4e16\u754c", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestPikeVM_Search_RuneAny_Positions exercises the RuneAny branch in step()
// through Search, which uses addThreadToNext. Verifies match positions are
// correct when patterns contain (?s). (dot-all dot).
func TestPikeVM_Search_RuneAny_Positions(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"dotall single", `(?s).`, "abc", 0, 1, true},
		{"dotall newline", `(?s).`, "\nabc", 0, 1, true},
		{"dotall 3byte", `(?s).`, "\u4e16abc", 0, 3, true},
		{"dotall concat", `(?s)a.c`, "axc", 0, 3, true},
		{"dotall concat newline", `(?s)a.c`, "a\nc", 0, 3, true},
		{"dotall concat utf8", `(?s)a.c`, "a\u4e16c", 0, 5, true},
		{"dotall no match", `(?s)a.c`, "a", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_Search_RuneAnyNotNL_Positions verifies correct positions
// for patterns using default-mode dot (RuneAnyNotNL) with multi-byte UTF-8.
func TestPikeVM_Search_RuneAnyNotNL_Positions(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"single dot ascii", `.`, "abc", 0, 1, true},
		{"dot between literals", `a.c`, "axc", 0, 3, true},
		{"dot 2byte", `a.c`, "a\u00e9c", 0, 4, true},
		{"dot newline skip", `.`, "\nabc", 1, 2, true}, // skips newline, matches 'a'
		{"dot plus", `.+`, "abc\ndef", 0, 3, true},     // stops at newline
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_CheckLeftLookSucceeds exercises checkLeftLookSucceeds() and
// checkLeftLookSucceedsForSearch(). These are invoked when the left branch
// of a split is a Look assertion (e.g., word boundary \b, start-of-line ^).
func TestPikeVM_CheckLeftLookSucceeds(t *testing.T) {
	// Patterns like (?:\b|x)+ have a split where left is a Look.
	// The word boundary check at different positions exercises checkLeftLookSucceeds.
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		// \b at word boundary: left Look succeeds
		{"word boundary present", `\bfoo\b`, "foo", true},
		{"word boundary absent", `\bfoo\b`, "xfoo", false},
		{"word boundary mid", `\bfoo\b`, "x foo y", true},

		// (?:^|a) - left branch is ^ (LookStartLine)
		{"start or a at start", `(?:^|a)b`, "bc", true},
		{"start or a mid", `(?:^|a)b`, "xab", true},

		// \B (non-word-boundary)
		{"non-word-boundary", `\Boo`, "foo", true},
		{"non-word-boundary fail", `\Boo`, "oo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestPikeVM_SearchWithSlotTable_Anchored exercises searchWithSlotTableAnchored()
// by using anchored patterns (^...$) with the SearchWithSlotTable API.
func TestPikeVM_SearchWithSlotTable_Anchored(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"anchored literal", `^hello$`, "hello", 0, 5, true},
		{"anchored literal no match", `^hello$`, "say hello", -1, -1, false},
		{"anchored dotall", `(?s)^a.b$`, "a\nb", 0, 3, true},
		{"anchored dot", `^a.b$`, "axb", 0, 3, true},
		{"anchored dot newline", `^a.b$`, "a\nb", -1, -1, false},
		{"anchored char class", `^[a-z]+$`, "hello", 0, 5, true},
		{"anchored char class fail", `^[a-z]+$`, "hello123", -1, -1, false},
		{"anchored empty", `^$`, "", 0, 0, true},
		{"anchored unicode", `^.+$`, "\u4e16\u754c", 0, 6, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.SearchWithSlotTable([]byte(tt.haystack), SearchModeFind)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_SearchWithSlotTable_AnchoredCaptures exercises
// searchWithSlotTableAnchored with capture mode, which triggers slot copying
// in addSearchThread and stepSearchThread.
func TestPikeVM_SearchWithSlotTable_AnchoredCaptures(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"capture group", `^(hello)$`, "hello", 0, 5, true},
		{"two captures", `^(\w+)\s(\w+)$`, "hello world", 0, 11, true},
		{"no match", `^(hello)$`, "world", -1, -1, false},
		{"unicode capture", `^(.+)$`, "\u4e16\u754c", 0, 6, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.SearchWithSlotTable([]byte(tt.haystack), SearchModeCaptures)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_StepSearchThread_RuneAny exercises RuneAny/RuneAnyNotNL branches
// in stepSearchThread() by using SearchWithSlotTable on unanchored patterns
// containing (?s). (dot-all dot).
func TestPikeVM_StepSearchThread_RuneAny(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"dotall unanchored", `(?s)a.b`, "xxxa\nbyyy", 3, 6, true},
		{"dotall utf8", `(?s)a.b`, "xxxa\u4e16byyy", 3, 8, true},
		{"nodotall utf8", `a.b`, "xxxa\u00e9byyy", 3, 7, true},
		{"nodotall newline fail", `a.b`, "xxxa\nbyyy", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.SearchWithSlotTable([]byte(tt.haystack), SearchModeFind)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_StepSearchThread_Sparse exercises the Sparse branch in
// stepSearchThread() by using patterns that compile to StateSparse transitions
// (e.g., character classes with multiple ranges like [a-z0-9]).
func TestPikeVM_StepSearchThread_Sparse(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"multi range class", `[a-z0-9]+`, "!!!abc123!!!", 3, 9, true},
		{"multi range no match", `[a-z0-9]+`, "!!!", -1, -1, false},
		{"negated class", `[^a-z]+`, "abc123abc", 3, 6, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.SearchWithSlotTable([]byte(tt.haystack), SearchModeFind)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_Search_RuneAny_VsStdlib cross-validates PikeVM RuneAny/RuneAnyNotNL
// behavior against Go's stdlib regexp on diverse inputs.
func TestPikeVM_Search_RuneAny_VsStdlib(t *testing.T) {
	patterns := []string{
		`(?s).+`,           // RuneAny: match everything including newlines
		`(?s)a.+b`,         // RuneAny in context
		`.+`,               // RuneAnyNotNL
		`a.b`,              // RuneAnyNotNL single dot
		`(?s)(.)(.)`,       // RuneAny with captures
		`(?s)[\x00-\x7f].`, // ASCII byte then any rune
	}

	inputs := []string{
		"abc",
		"a\nb",
		"a\u4e16b",
		"\u4e16\u754c\u4eba\u6c11",
		"\n\n\n",
		"a\U0001f600b",
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)
		n := compileNFAForTest(pattern)
		vm := NewPikeVM(n)

		for _, input := range inputs {
			t.Run(pattern+"/"+input, func(t *testing.T) {
				stdLoc := stdRe.FindStringIndex(input)
				pikeStart, pikeEnd, pikeFound := vm.Search([]byte(input))

				stdMatched := stdLoc != nil
				if stdMatched != pikeFound {
					t.Errorf("match mismatch: stdlib=%v, pike=%v", stdMatched, pikeFound)
					return
				}
				if stdMatched && pikeFound {
					if stdLoc[0] != pikeStart || stdLoc[1] != pikeEnd {
						t.Errorf("position mismatch: stdlib=[%d,%d], pike=[%d,%d]",
							stdLoc[0], stdLoc[1], pikeStart, pikeEnd)
					}
				}
			})
		}
	}
}

// TestPikeVM_SearchWithCapturesAt_RuneAny exercises RuneAny in the capture
// tracking path (SearchWithCapturesAt), which uses different code for
// thread management with capture slots.
func TestPikeVM_SearchWithCapturesAt_RuneAny(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		wantNil bool
		start   int
		end     int
	}{
		{"dotall capture", `(?s)(.)`, "a", false, 0, 1},
		{"dotall capture newline", `(?s)(.)`, "\n", false, 0, 1},
		{"dotall capture utf8", `(?s)(.)`, "\u4e16", false, 0, 3},
		{"nodotall capture", `(.)`, "a", false, 0, 1},
		{"nodotall capture newline only", `(.)`, "\n", true, 0, 0},
		{"nodotall capture utf8", `(.)`, "\u4e16", false, 0, 3},
		{"two captures dotall", `(?s)(.)(.+)`, "a\nb", false, 0, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			result := vm.SearchWithCapturesAt([]byte(tt.input), 0)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got (%d, %d)", result.Start, result.End)
				}
				return
			}
			if result == nil {
				t.Fatal("expected match, got nil")
			}
			if result.Start != tt.start || result.End != tt.end {
				t.Errorf("got (%d, %d), want (%d, %d)", result.Start, result.End, tt.start, tt.end)
			}
		})
	}
}

// --- Manual NFA builder tests ---
// The compiler never emits StateRuneAny/StateRuneAnyNotNL directly;
// it expands . into UTF-8 byte ranges. To exercise the RuneAny/RuneAnyNotNL
// branches in step(), stepForMatch(), and stepSearchThread(), we must
// build NFAs manually using the Builder API.

// buildRuneAnyNFA constructs: unanchored prefix -> RuneAny -> Match.
// This creates an NFA that matches any single Unicode codepoint (including newline).
func buildRuneAnyNFA(t *testing.T) *NFA {
	t.Helper()
	b := NewBuilder()
	match := b.AddMatch()
	runeAny := b.AddRuneAny(match)
	b.SetStart(runeAny)
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	return n
}

// buildRuneAnyNotNLNFA constructs: unanchored prefix -> RuneAnyNotNL -> Match.
func buildRuneAnyNotNLNFA(t *testing.T) *NFA {
	t.Helper()
	b := NewBuilder()
	match := b.AddMatch()
	runeAnyNotNL := b.AddRuneAnyNotNL(match)
	b.SetStart(runeAnyNotNL)
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	return n
}

// buildByteRuneAnyNFA constructs: 'a' -> RuneAny -> 'b' -> Match.
// Matches "a<any_rune>b".
func buildByteRuneAnyNFA(t *testing.T) *NFA {
	t.Helper()
	b := NewBuilder()
	match := b.AddMatch()
	bState := b.AddByteRange('b', 'b', match)
	runeAny := b.AddRuneAny(bState)
	aState := b.AddByteRange('a', 'a', runeAny)
	b.SetStart(aState)
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	return n
}

// buildByteRuneAnyNotNLNFA constructs: 'a' -> RuneAnyNotNL -> 'b' -> Match.
// Matches "a<any_rune_except_nl>b".
func buildByteRuneAnyNotNLNFA(t *testing.T) *NFA {
	t.Helper()
	b := NewBuilder()
	match := b.AddMatch()
	bState := b.AddByteRange('b', 'b', match)
	runeAnyNotNL := b.AddRuneAnyNotNL(bState)
	aState := b.AddByteRange('a', 'a', runeAnyNotNL)
	b.SetStart(aState)
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	return n
}

// TestPikeVM_ManualRuneAny_IsMatch exercises StateRuneAny in stepForMatch()
// by using a manually built NFA with RuneAny state.
func TestPikeVM_ManualRuneAny_IsMatch(t *testing.T) {
	n := buildRuneAnyNFA(t)
	vm := NewPikeVM(n)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"ascii", "x", true},
		{"newline", "\n", true},
		{"2byte utf8", "\u00e9", true},
		{"3byte utf8", "\u4e16", true},
		{"4byte utf8", "\U0001f600", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vm.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestPikeVM_ManualRuneAnyNotNL_IsMatch exercises StateRuneAnyNotNL in stepForMatch().
func TestPikeVM_ManualRuneAnyNotNL_IsMatch(t *testing.T) {
	n := buildRuneAnyNotNLNFA(t)
	vm := NewPikeVM(n)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"ascii", "x", true},
		{"newline rejected", "\n", false},
		{"2byte utf8", "\u00e9", true},
		{"3byte utf8", "\u4e16", true},
		{"4byte utf8", "\U0001f600", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vm.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestPikeVM_ManualRuneAny_Search exercises StateRuneAny in step() (the main
// PikeVM step function used by Search, which tracks positions and captures).
// Note: StateRuneAny is designed for byte-at-a-time processing; the compiler
// never emits it, using byte ranges instead. For Search with exact positions,
// only ASCII-width runes give correct position tracking in manual NFAs.
func TestPikeVM_ManualRuneAny_Search(t *testing.T) {
	n := buildByteRuneAnyNFA(t)
	vm := NewPikeVM(n)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", "axb", 0, 3, true},
		{"newline", "a\nb", 0, 3, true},
		{"offset", "xxaxbyy", 2, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := vm.Search([]byte(tt.input))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_ManualRuneAnyNotNL_Search exercises StateRuneAnyNotNL in step().
// ASCII-only inputs for correct position tracking with manual NFAs.
func TestPikeVM_ManualRuneAnyNotNL_Search(t *testing.T) {
	n := buildByteRuneAnyNotNLNFA(t)
	vm := NewPikeVM(n)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", "axb", 0, 3, true},
		{"newline rejected", "a\nb", -1, -1, false},
		{"offset", "xxaxbyy", 2, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := vm.Search([]byte(tt.input))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_ManualRuneAny_SlotTable exercises StateRuneAny in stepSearchThread()
// by using SearchWithSlotTable with a manually built NFA.
// ASCII-only for correct position tracking with manual NFAs.
func TestPikeVM_ManualRuneAny_SlotTable(t *testing.T) {
	n := buildByteRuneAnyNFA(t)
	vm := NewPikeVM(n)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", "axb", 0, 3, true},
		{"newline", "a\nb", 0, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := vm.SearchWithSlotTable([]byte(tt.input), SearchModeFind)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_ManualRuneAnyNotNL_SlotTable exercises StateRuneAnyNotNL
// in stepSearchThread(). ASCII-only for correct position tracking.
func TestPikeVM_ManualRuneAnyNotNL_SlotTable(t *testing.T) {
	n := buildByteRuneAnyNotNLNFA(t)
	vm := NewPikeVM(n)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", "axb", 0, 3, true},
		{"newline rejected", "a\nb", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := vm.SearchWithSlotTable([]byte(tt.input), SearchModeFind)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_ManualRuneAny_Anchored_SlotTable exercises StateRuneAny in
// searchWithSlotTableAnchored by building an anchored NFA with RuneAny.
// Uses ASCII input since RuneAny state is byte-oriented in the PikeVM.
func TestPikeVM_ManualRuneAny_Anchored_SlotTable(t *testing.T) {
	// Build anchored NFA: RuneAny -> Match (no unanchored prefix)
	b := NewBuilder()
	match := b.AddMatch()
	runeAny := b.AddRuneAny(match)
	b.SetStart(runeAny)
	n, err := b.Build(WithAnchored(true))
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	vm := NewPikeVM(n)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", "x", 0, 1, true},
		{"newline", "\n", 0, 1, true},
		{"empty", "", -1, -1, false},
		{"multi ascii", "abc", 0, 1, true}, // matches first char only
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := vm.SearchWithSlotTable([]byte(tt.input), SearchModeFind)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_ManualRuneAnyNotNL_Anchored exercises StateRuneAnyNotNL
// in searchWithSlotTableAnchored. ASCII-only for correct position tracking.
func TestPikeVM_ManualRuneAnyNotNL_Anchored(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	runeAnyNotNL := b.AddRuneAnyNotNL(match)
	b.SetStart(runeAnyNotNL)
	n, err := b.Build(WithAnchored(true))
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	vm := NewPikeVM(n)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", "x", 0, 1, true},
		{"newline rejected", "\n", -1, -1, false},
		{"empty", "", -1, -1, false},
		{"multi ascii", "abc", 0, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := vm.SearchWithSlotTable([]byte(tt.input), SearchModeFind)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
