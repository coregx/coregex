package nfa

import (
	"regexp"
	"testing"
)

// TestBacktracker_IsMatch_RuneAny exercises the StateRuneAny branch in
// backtrackWithState(). Pattern (?s). (dot-all mode) compiles to StateRuneAny.
func TestBacktracker_IsMatch_RuneAny(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"ascii", `(?s).`, "x", true},
		{"newline", `(?s).`, "\n", true},
		{"2byte utf8", `(?s).`, "\u00e9", true},
		{"3byte utf8", `(?s).`, "\u4e16", true},
		{"4byte utf8", `(?s).`, "\U0001f600", true},
		{"empty", `(?s).`, "", false},
		{"dotall star", `(?s)a.*b`, "a\n\nb", true},
		{"dotall concat utf8", `(?s)a.b`, "a\u4e16b", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			got := bt.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestBacktracker_IsMatch_RuneAnyNotNL exercises the StateRuneAnyNotNL branch
// in backtrackWithState(). Default-mode dot matches any codepoint except newline.
func TestBacktracker_IsMatch_RuneAnyNotNL(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"ascii", `.`, "x", true},
		{"newline rejected", `.`, "\n", false},
		{"2byte utf8", `.`, "\u00f1", true},
		{"3byte utf8", `.`, "\u2603", true},
		{"4byte utf8", `.`, "\U0001f4a9", true},
		{"dot concat", `a.b`, "axb", true},
		{"dot concat newline", `a.b`, "a\nb", false},
		{"dot concat utf8", `a.b`, "a\u00e9b", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			got := bt.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestBacktracker_IsMatch_Look exercises the StateLook branch in
// backtrackWithState() for word boundary (\b, \B) and anchor assertions.
func TestBacktracker_IsMatch_Look(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"word boundary start", `\bfoo`, "foo bar", true},
		{"word boundary end", `foo\b`, "foo bar", true},
		{"word boundary both", `\bfoo\b`, "foo", true},
		{"word boundary fail", `\bfoo\b`, "xfoo", false},
		{"non-word boundary", `\Boo`, "foo", true},
		{"non-word boundary fail", `\Boo`, "oo bar", false},
		{"start text", `\Ahello`, "hello world", true},
		{"start text fail", `\Ahello`, "say hello", false},
		{"end text", `world\z`, "hello world", true},
		{"end text fail", `world\z`, "world hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			got := bt.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestBacktracker_Find_RuneAny exercises the StateRuneAny branch in
// backtrackFindWithState(). Verifies correct match end positions for
// dot-all patterns with multi-byte UTF-8 input.
func TestBacktracker_Find_RuneAny(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"dotall ascii", `(?s)a.b`, "axb", 0, 3, true},
		{"dotall newline", `(?s)a.b`, "a\nb", 0, 3, true},
		{"dotall utf8 2byte", `(?s)a.b`, "a\u00e9b", 0, 4, true},
		{"dotall utf8 3byte", `(?s)a.b`, "a\u4e16b", 0, 5, true},
		{"dotall offset", `(?s)a.b`, "xxxa\nbyyy", 3, 6, true},
		{"dotall no match", `(?s)a.b`, "ab", -1, -1, false},
		{"dotall star", `(?s)a.+b`, "a\n\n\nb", 0, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			start, end, found := bt.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_Find_RuneAnyNotNL exercises the StateRuneAnyNotNL branch
// in backtrackFindWithState(). Default-mode dot with multi-byte UTF-8.
func TestBacktracker_Find_RuneAnyNotNL(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", `a.b`, "axb", 0, 3, true},
		{"utf8 2byte", `a.b`, "a\u00e9b", 0, 4, true},
		{"utf8 3byte", `a.b`, "a\u4e16b", 0, 5, true},
		{"newline fail", `a.b`, "a\nb", -1, -1, false},
		{"offset", `a.b`, "xxxa\u00e9byyy", 3, 7, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			start, end, found := bt.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_Find_Look exercises the StateLook branch in
// backtrackFindWithState(). Word boundary patterns return correct positions.
func TestBacktracker_Find_Look(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"word boundary", `\bfoo\b`, "say foo bar", 4, 7, true},
		{"word boundary fail", `\bfoo\b`, "foobar", -1, -1, false},
		{"start anchor", `\Ahello`, "hello world", 0, 5, true},
		{"end anchor", `world\z`, "hello world", 6, 11, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			start, end, found := bt.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_FindLongest_RuneAny exercises the StateRuneAny branch in
// backtrackFindLongestWithState(). SetLongest(true) forces exploration of
// all split branches to find the longest possible match.
func TestBacktracker_FindLongest_RuneAny(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// With longest mode, a|ab should match "ab" (length 2) not "a" (length 1)
		{"longest alt", `a|ab`, "ab", 0, 2, true},
		{"longest dotall", `(?s)a.+`, "a\nb\nc", 0, 5, true},
		{"longest dot", `a.+`, "abcde", 0, 5, true},
		{"longest utf8 dotall", `(?s)a.+b`, "a\u4e16\u754cb", 0, 8, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			bt.SetLongest(true)
			start, end, found := bt.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_FindLongest_RuneAnyNotNL exercises the StateRuneAnyNotNL
// branch in backtrackFindLongestWithState() with SetLongest(true).
func TestBacktracker_FindLongest_RuneAnyNotNL(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"longest dot plus", `.+`, "abcdef", 0, 6, true},
		{"longest dot plus newline", `.+`, "abc\ndef", 0, 3, true}, // stops at newline
		{"longest utf8", `.+`, "\u4e16\u754c\u4eba", 0, 9, true},
		{"longest alt dot", `a.|ab`, "ab", 0, 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			bt.SetLongest(true)
			start, end, found := bt.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_FindLongest_Look exercises the StateLook branch in
// backtrackFindLongestWithState() with SetLongest(true).
func TestBacktracker_FindLongest_Look(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"longest word boundary", `\b\w+\b`, "hello world", 0, 5, true},
		{"longest start anchor", `\A\w+`, "hello world", 0, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			bt.SetLongest(true)
			start, end, found := bt.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_FindLongest_Sparse exercises the StateSparse branch in
// backtrackFindLongestWithState() with multi-range character classes.
func TestBacktracker_FindLongest_Sparse(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"sparse multi range", `[a-z0-9]+`, "!!!abc123!!!", 3, 9, true},
		{"sparse longest", `[a-z]+|[a-z0-9]+`, "abc123", 0, 6, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			bt := NewBoundedBacktracker(n)
			bt.SetLongest(true)
			start, end, found := bt.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("Search found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("Search = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_RuneAny_VsStdlib cross-validates backtracker RuneAny behavior
// against Go stdlib regexp on diverse inputs with multi-byte UTF-8.
func TestBacktracker_RuneAny_VsStdlib(t *testing.T) {
	patterns := []string{
		`(?s).+`,
		`(?s)a.+b`,
		`.+`,
		`a.b`,
		`(?s).`,
		`\b\w+\b`,
	}

	inputs := []string{
		"abc",
		"a\nb",
		"a\u4e16b",
		"\u4e16\u754c",
		"\n\n\n",
		"hello world",
		"a\U0001f600b",
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)
		n := compileNFAForTest(pattern)
		bt := NewBoundedBacktracker(n)

		for _, input := range inputs {
			t.Run(pattern+"/"+input, func(t *testing.T) {
				stdLoc := stdRe.FindStringIndex(input)
				btStart, btEnd, btFound := bt.Search([]byte(input))

				stdMatched := stdLoc != nil
				if stdMatched != btFound {
					t.Errorf("match mismatch: stdlib=%v, bt=%v", stdMatched, btFound)
					return
				}
				if stdMatched && btFound {
					if stdLoc[0] != btStart || stdLoc[1] != btEnd {
						t.Errorf("position mismatch: stdlib=[%d,%d], bt=[%d,%d]",
							stdLoc[0], stdLoc[1], btStart, btEnd)
					}
				}
			})
		}
	}
}

// --- Manual NFA builder tests for Backtracker ---
// The compiler expands . into UTF-8 byte ranges, never emitting StateRuneAny
// or StateRuneAnyNotNL. To exercise these branches in backtrackWithState(),
// backtrackFindWithState(), and backtrackFindLongestWithState(), we build
// NFAs manually.

// TestBacktracker_ManualRuneAny_IsMatch exercises StateRuneAny branch in
// backtrackWithState() using a manually built NFA.
func TestBacktracker_ManualRuneAny_IsMatch(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	runeAny := b.AddRuneAny(match)
	b.SetStart(runeAny)
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	bt := NewBoundedBacktracker(n)

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
			got := bt.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestBacktracker_ManualRuneAnyNotNL_IsMatch exercises StateRuneAnyNotNL
// in backtrackWithState().
func TestBacktracker_ManualRuneAnyNotNL_IsMatch(t *testing.T) {
	b := NewBuilder()
	match := b.AddMatch()
	runeAnyNotNL := b.AddRuneAnyNotNL(match)
	b.SetStart(runeAnyNotNL)
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	bt := NewBoundedBacktracker(n)

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
			got := bt.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestBacktracker_ManualRuneAny_Search exercises StateRuneAny in
// backtrackFindWithState() using a manually built NFA: 'a' -> RuneAny -> 'b' -> Match.
func TestBacktracker_ManualRuneAny_Search(t *testing.T) {
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

	bt := NewBoundedBacktracker(n)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", "axb", 0, 3, true},
		{"newline", "a\nb", 0, 3, true},
		{"2byte utf8", "a\u00e9b", 0, 4, true},
		{"3byte utf8", "a\u4e16b", 0, 5, true},
		{"4byte utf8", "a\U0001f600b", 0, 6, true},
		{"no match", "ab", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := bt.Search([]byte(tt.input))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_ManualRuneAnyNotNL_Search exercises StateRuneAnyNotNL
// in backtrackFindWithState().
func TestBacktracker_ManualRuneAnyNotNL_Search(t *testing.T) {
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

	bt := NewBoundedBacktracker(n)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii", "axb", 0, 3, true},
		{"newline rejected", "a\nb", -1, -1, false},
		{"3byte utf8", "a\u4e16b", 0, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := bt.Search([]byte(tt.input))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_ManualRuneAny_FindLongest exercises StateRuneAny in
// backtrackFindLongestWithState() with SetLongest(true).
func TestBacktracker_ManualRuneAny_FindLongest(t *testing.T) {
	// Build: RuneAny+ -> Match (matches 1+ codepoints including newline)
	b := NewBuilder()
	match := b.AddMatch()
	runeAny := b.AddRuneAny(InvalidState) // next patched below
	// Loop: split -> [runeAny, match]
	split := b.AddQuantifierSplit(runeAny, match)
	if err := b.Patch(runeAny, split); err != nil {
		// If patch fails, use epsilon
		eps := b.AddEpsilon(split)
		if err := b.Patch(runeAny, eps); err != nil {
			t.Fatalf("Patch error: %v", err)
		}
	}
	b.SetStart(runeAny)
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	bt := NewBoundedBacktracker(n)
	bt.SetLongest(true)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii multi", "abc", 0, 3, true},
		{"with newline", "a\nb", 0, 3, true},
		{"utf8", "\u4e16\u754c", 0, 6, true},
		{"empty", "", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := bt.Search([]byte(tt.input))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestBacktracker_ManualRuneAnyNotNL_FindLongest exercises StateRuneAnyNotNL
// in backtrackFindLongestWithState() with SetLongest(true).
func TestBacktracker_ManualRuneAnyNotNL_FindLongest(t *testing.T) {
	// Build: RuneAnyNotNL+ -> Match (matches 1+ codepoints except newline)
	b := NewBuilder()
	match := b.AddMatch()
	runeAnyNotNL := b.AddRuneAnyNotNL(InvalidState)
	split := b.AddQuantifierSplit(runeAnyNotNL, match)
	if err := b.Patch(runeAnyNotNL, split); err != nil {
		eps := b.AddEpsilon(split)
		if err := b.Patch(runeAnyNotNL, eps); err != nil {
			t.Fatalf("Patch error: %v", err)
		}
	}
	b.SetStart(runeAnyNotNL)
	n, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	bt := NewBoundedBacktracker(n)
	bt.SetLongest(true)

	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"ascii multi", "abc", 0, 3, true},
		{"stops at newline", "ab\ncd", 0, 2, true}, // longest stops before newline
		{"utf8", "\u4e16\u754c", 0, 6, true},
		{"empty", "", -1, -1, false},
		{"newline only", "\n", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, found := bt.Search([]byte(tt.input))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
