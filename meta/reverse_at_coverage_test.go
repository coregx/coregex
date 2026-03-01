package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestReverseSuffix_FindIndicesAt exercises the ReverseSuffix FindIndicesAt path.
func TestReverseSuffix_FindIndicesAt(t *testing.T) {
	engine, err := Compile(`.*\.txt`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffix {
		t.Skipf("Strategy is %s, not UseReverseSuffix", engine.Strategy())
	}

	tests := []struct {
		input     string
		at        int
		wantFound bool
	}{
		{"file.txt", 0, true},
		{"abc file.txt", 0, true},
		{"no match", 0, false},
	}

	for _, tt := range tests {
		_, _, found := engine.FindIndicesAt([]byte(tt.input), tt.at)
		if found != tt.wantFound {
			t.Errorf("FindIndicesAt(%q, %d) found=%v, want %v",
				tt.input, tt.at, found, tt.wantFound)
		}
	}
}

// TestReverseSuffixSet_FindIndicesAt exercises the ReverseSuffixSet FindIndicesAt path.
func TestReverseSuffixSet_FindIndicesAt(t *testing.T) {
	engine, err := Compile(`.*\.(txt|log|md)`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("Strategy is %s, not UseReverseSuffixSet", engine.Strategy())
	}

	tests := []struct {
		input     string
		at        int
		wantFound bool
	}{
		{"file.txt", 0, true},
		{"app.log", 0, true},
		{"readme.md", 0, true},
		{"no match", 0, false},
	}

	for _, tt := range tests {
		_, _, found := engine.FindIndicesAt([]byte(tt.input), tt.at)
		if found != tt.wantFound {
			t.Errorf("FindIndicesAt(%q, %d) found=%v, want %v",
				tt.input, tt.at, found, tt.wantFound)
		}
	}
}

// TestReverseInner_FindIndicesAt exercises the ReverseInner FindIndicesAt path.
func TestReverseInner_FindIndicesAt(t *testing.T) {
	engine, err := Compile(`.*connection.*`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseInner {
		t.Skipf("Strategy is %s, not UseReverseInner", engine.Strategy())
	}

	tests := []struct {
		input     string
		at        int
		wantFound bool
	}{
		{"error: connection refused", 0, true},
		{"no match here", 0, false},
	}

	for _, tt := range tests {
		_, _, found := engine.FindIndicesAt([]byte(tt.input), tt.at)
		if found != tt.wantFound {
			t.Errorf("FindIndicesAt(%q, %d) found=%v, want %v",
				tt.input, tt.at, found, tt.wantFound)
		}
	}
}

// TestMultilineReverseSuffix_FindIndicesAt exercises the multiline suffix *At path.
func TestMultilineReverseSuffix_FindIndicesAt(t *testing.T) {
	engine, err := Compile(`(?m)^.*\.php`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseMultilineReverseSuffix {
		t.Skipf("Strategy is %s, not UseMultilineReverseSuffix", engine.Strategy())
	}

	input := "/var/www/index.php\n/var/www/admin.php"
	// Find from position 0
	s, e, found := engine.FindIndicesAt([]byte(input), 0)
	if !found {
		t.Error("Expected match at position 0")
	} else {
		t.Logf("Match at [%d,%d]: %q", s, e, input[s:e])
	}

	// Find from after first line
	s2, e2, found2 := engine.FindIndicesAt([]byte(input), 19)
	if !found2 {
		t.Error("Expected match from position 19")
	} else {
		t.Logf("Match at [%d,%d]: %q", s2, e2, input[s2:e2])
	}

	// Find at position beyond all matches
	_, _, found3 := engine.FindIndicesAt([]byte(input), len(input))
	if found3 {
		t.Error("Should not find match at end of input")
	}
}

// TestMultilineReverseSuffix_FindAt exercises the multiline suffix Find path at > 0.
func TestMultilineReverseSuffix_FindAt(t *testing.T) {
	engine, err := Compile(`(?m)^.*\.php`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseMultilineReverseSuffix {
		t.Skipf("Strategy is %s, not UseMultilineReverseSuffix", engine.Strategy())
	}

	input := "/var/www/index.php\n/var/www/admin.php"
	match := engine.FindAt([]byte(input), 19)
	if match != nil {
		t.Logf("FindAt(19): %q at [%d,%d]", match.String(), match.Start(), match.End())
	}
}

// TestAhoCorasick_FindAt exercises the AhoCorasick *At paths.
func TestAhoCorasick_FindAt(t *testing.T) {
	// Build a pattern with many alternatives to trigger AhoCorasick
	words := make([]string, 70)
	for i := range words {
		words[i] = strings.Repeat(string(rune('a'+(i%26))), 3) + strings.Repeat(string(rune('A'+(i%26))), 2)
	}
	pattern := strings.Join(words, "|")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", engine.Strategy())
	}

	input := "prefix " + words[0] + " middle " + words[50] + " suffix"

	// FindAt position 0
	match := engine.Find([]byte(input))
	if match == nil {
		t.Error("Find should match")
	}

	// FindAt non-zero
	match2 := engine.FindAt([]byte(input), 8)
	t.Logf("FindAt(8): match=%v", match2 != nil)

	// FindIndicesAt non-zero
	s, e, found := engine.FindIndicesAt([]byte(input), 8)
	t.Logf("FindIndicesAt(8): (%d, %d, %v)", s, e, found)

	// Count
	count := engine.Count([]byte(input), -1)
	if count < 2 {
		t.Errorf("Count = %d, want >= 2", count)
	}
}

// TestTeddy_FindAt exercises the Teddy *At paths with at > 0.
func TestTeddy_FindAt(t *testing.T) {
	engine, err := Compile(`alpha|beta|gamma|delta|epsilon`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseTeddy {
		t.Skipf("Strategy is %s, not UseTeddy", engine.Strategy())
	}

	input := "alpha and beta and gamma and delta and epsilon"

	// FindAt at various positions
	for _, at := range []int{0, 6, 15, 24, 33} {
		match := engine.FindAt([]byte(input), at)
		if match == nil {
			t.Errorf("FindAt(%d) should find a match", at)
		} else {
			t.Logf("FindAt(%d): %q at [%d,%d]", at, match.String(), match.Start(), match.End())
		}
	}

	// FindIndicesAt
	for _, at := range []int{0, 6, 15, 24, 33} {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		if !found {
			t.Errorf("FindIndicesAt(%d) should find a match", at)
		} else {
			t.Logf("FindIndicesAt(%d): [%d,%d]", at, s, e)
		}
	}

	// Past all matches
	match := engine.FindAt([]byte(input), 46)
	if match != nil {
		t.Error("FindAt(46) should not find match past end")
	}
}

// TestDigitPrefilter_FindAt exercises the digit prefilter *At path.
func TestDigitPrefilter_FindAt(t *testing.T) {
	engine, err := Compile(`\d+-\d+`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseDigitPrefilter {
		t.Skipf("Strategy is %s, not UseDigitPrefilter", engine.Strategy())
	}

	input := "abc 12-34 def 56-78 ghi"

	// FindAt from various positions
	match1 := engine.FindAt([]byte(input), 0)
	if match1 == nil || match1.String() != "12-34" {
		t.Errorf("FindAt(0): got %v, want 12-34", match1)
	}

	match2 := engine.FindAt([]byte(input), 10)
	if match2 == nil || match2.String() != "56-78" {
		t.Errorf("FindAt(10): got %v, want 56-78", match2)
	}

	match3 := engine.FindAt([]byte(input), 20)
	if match3 != nil {
		t.Errorf("FindAt(20): got %v, want nil", match3)
	}

	// FindIndicesAt
	s, e, found := engine.FindIndicesAt([]byte(input), 10)
	if !found || s != 14 || e != 19 {
		t.Errorf("FindIndicesAt(10): got (%d,%d,%v), want (14,19,true)", s, e, found)
	}
}

// TestFindAll_VsStdlib_MultiStrategy cross-validates FindAll results across strategies.
func TestFindAll_VsStdlib_MultiStrategy(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// NFA
		{`a.c`, "abc adc aec xyz abc"},
		// CharClass
		{`[a-z]+`, "abc 123 DEF def 456 ghi"},
		// Teddy
		{`cat|dog|rat`, "cat and dog and rat and cat"},
		// DigitPrefilter
		{`\d+-\d+`, "11-22 33-44 55-66"},
		// BoundedBacktracker
		{`^hello`, "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile(tt.pattern)

			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))

			if count != stdCount {
				t.Errorf("Count=%d, stdlib=%d for %q (strategy=%s)",
					count, stdCount, tt.input, engine.Strategy())
			}
		})
	}
}

// TestFindAllIndicesStreaming_Positions verifies exact match positions across strategies.
func TestFindAllIndicesStreaming_Positions(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{"digits", `\d+`, "abc 123 def 456 ghi 789"},
		{"words", `[a-z]+`, "abc DEF ghi JKL mno"},
		{"alternation", `foo|bar|baz`, "foo bar baz foo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile(tt.pattern)

			input := []byte(tt.input)
			stdAll := re.FindAllIndex(input, -1)

			ourAll := engine.FindAllIndicesStreaming(input, 0, nil)

			if len(ourAll) != len(stdAll) {
				t.Errorf("count: ours=%d, stdlib=%d", len(ourAll), len(stdAll))
				return
			}

			for i := range stdAll {
				if ourAll[i][0] != stdAll[i][0] || ourAll[i][1] != stdAll[i][1] {
					t.Errorf("match %d: ours=[%d,%d], stdlib=[%d,%d]",
						i, ourAll[i][0], ourAll[i][1], stdAll[i][0], stdAll[i][1])
				}
			}
		})
	}
}

// TestBoundedBacktracker_FindIndicesAt_Large exercises the BT overflow path
// which falls through to bidirectional DFA or PikeVM.
func TestBoundedBacktracker_FindIndicesAt_Large(t *testing.T) {
	// Use a pattern that triggers UseBoundedBacktracker
	engine, err := Compile(`^[a-z]+`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s, not UseBoundedBacktracker", engine.Strategy())
	}

	// Small input -- normal BT path
	s, e, found := engine.FindIndices([]byte("hello world"))
	if !found || s != 0 || e != 5 {
		t.Errorf("Small input: got (%d,%d,%v), want (0,5,true)", s, e, found)
	}

	// At > 0 for anchored pattern should return no match
	_, _, found2 := engine.FindIndicesAt([]byte("hello world"), 1)
	if found2 {
		t.Error("Anchored pattern at > 0 should not match")
	}
}

// TestCompositeSearcher_FindAt exercises CompositeSearcher *At paths.
func TestCompositeSearcher_FindAt(t *testing.T) {
	engine, err := Compile(`[a-z]+[0-9]+`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseCompositeSearcher {
		t.Skipf("Strategy is %s, not UseCompositeSearcher", engine.Strategy())
	}

	input := "---abc123---def456---ghi789---"

	// FindAt from position 0
	match := engine.Find([]byte(input))
	if match == nil || match.String() != "abc123" {
		t.Errorf("Find: got %v, want abc123", match)
	}

	// FindAt from position past first match
	match2 := engine.FindAt([]byte(input), 9)
	if match2 == nil || match2.String() != "def456" {
		t.Errorf("FindAt(9): got %v, want def456", match2)
	}

	// FindIndicesAt from position past second match
	s, e, found := engine.FindIndicesAt([]byte(input), 18)
	if !found || input[s:e] != "ghi789" {
		t.Errorf("FindIndicesAt(18): got (%d,%d,%v), want ghi789", s, e, found)
	}

	// Count all matches
	count := engine.Count([]byte(input), -1)
	if count != 3 {
		t.Errorf("Count = %d, want 3", count)
	}
}

// TestCharClassSearcher_FindAt exercises CharClassSearcher *At paths.
func TestCharClassSearcher_FindAt(t *testing.T) {
	engine, err := Compile(`[a-z]+`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseCharClassSearcher {
		t.Skipf("Strategy is %s, not UseCharClassSearcher", engine.Strategy())
	}

	input := "ABC abc DEF def GHI ghi"

	// FindAt from various positions
	match := engine.FindAt([]byte(input), 0)
	if match == nil || match.String() != "abc" {
		t.Errorf("FindAt(0): got %v, want abc", match)
	}

	match2 := engine.FindAt([]byte(input), 8)
	if match2 == nil || match2.String() != "def" {
		t.Errorf("FindAt(8): got %v, want def", match2)
	}

	match3 := engine.FindAt([]byte(input), 16)
	if match3 == nil || match3.String() != "ghi" {
		t.Errorf("FindAt(16): got %v, want ghi", match3)
	}

	// FindIndicesAt
	s, e, found := engine.FindIndicesAt([]byte(input), 8)
	if !found || input[s:e] != "def" {
		t.Errorf("FindIndicesAt(8): got (%d,%d,%v), want def", s, e, found)
	}
}
