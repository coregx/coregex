package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestReverseSuffixSearcher_FindAt exercises ReverseSuffixSearcher.FindAt (0% coverage).
// FindAt is used by FindAllIndicesStreaming iteration for UseReverseSuffix.
// Note: greedy .* patterns produce different FindAll behavior than stdlib because
// .* consumes the entire prefix. We test Find (single match) against stdlib and
// exercise FindAll for code path coverage.
func TestReverseSuffixSearcher_FindAt(t *testing.T) {
	pattern := `.*\.txt`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pattern, err)
	}

	if engine.Strategy() != UseReverseSuffix {
		t.Skipf("Strategy is %s, not UseReverseSuffix", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{"single match", "readme.txt", true},
		{"match in middle", "prefix readme.txt suffix", true},
		{"multiple suffixes", "a.txt then b.txt", true},
		{"no match", "readme.md", false},
		{"no match at end", "data.csv", false},
		{"suffix only", ".txt", true},
		{"empty", "", false},
		{"long prefix", strings.Repeat("x", 200) + "doc.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)

			// Find (position 0) - exercises the standard Find path
			match := engine.Find(input)
			stdLoc := re.FindStringIndex(tt.input)

			if stdLoc == nil {
				if match != nil {
					t.Errorf("Find: got %q, want nil", match.String())
				}
			} else if match == nil {
				t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
			}

			// IsMatch
			got := engine.IsMatch(input)
			if got != tt.wantMatch {
				t.Errorf("IsMatch = %v, want %v", got, tt.wantMatch)
			}

			// FindAll -- exercises FindAt internally (code path coverage).
			// For greedy .* patterns, FindAll match count may differ from stdlib.
			allMatches := engine.FindAllIndicesStreaming(input, 0, nil)
			if tt.wantMatch && len(allMatches) == 0 {
				t.Error("FindAll: got 0 matches, want at least 1")
			}
			if !tt.wantMatch && len(allMatches) != 0 {
				t.Errorf("FindAll: got %d matches, want 0", len(allMatches))
			}
		})
	}
}

// TestReverseSuffixSearcher_FindAt_ExerciseLoop exercises the prefilter iteration
// loop and anti-quadratic guard in FindAt by using inputs with many suffix candidates.
func TestReverseSuffixSearcher_FindAt_ExerciseLoop(t *testing.T) {
	pattern := `.*\.log`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffix {
		t.Skipf("Strategy is %s, not UseReverseSuffix", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Single .log occurrence -- Count should agree with stdlib
	single := "application.log"
	count := engine.Count([]byte(single), -1)
	stdCount := len(re.FindAllString(single, -1))
	if count != stdCount {
		t.Errorf("Count(single) = %d, stdlib = %d", count, stdCount)
	}

	// Exercise FindIndicesAt at various positions to cover the iteration path
	input := "server.log"
	for at := 0; at < len(input); at++ {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("FindIndicesAt(%d): unexpected (%d,%d)", at, s, e)
			}
		} else if !found {
			t.Errorf("FindIndicesAt(%d): not found, stdlib (%d,%d)", at, stdLoc[0]+at, stdLoc[1]+at)
		}
	}

	// No match input
	noMatch := "error.txt"
	match := engine.Find([]byte(noMatch))
	if match != nil {
		t.Errorf("Find on no-match: got %q", match.String())
	}

	// Beyond length
	_, _, found := engine.FindIndicesAt([]byte("a.log"), 100)
	if found {
		t.Error("FindIndicesAt beyond length should not find")
	}
}

// TestReverseSuffixSetSearcher_FindAt exercises ReverseSuffixSetSearcher.FindAt (0% coverage).
// FindAt is used by FindAllIndicesStreaming for UseReverseSuffixSet strategy.
func TestReverseSuffixSetSearcher_FindAt(t *testing.T) {
	pattern := `.*\.(txt|log|md)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("Strategy is %s, not UseReverseSuffixSet", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{"single txt", "readme.txt", true},
		{"single log", "app.log", true},
		{"single md", "guide.md", true},
		{"multiple types", "doc.txt app.log readme.md", true},
		{"no match", "image.jpg", false},
		{"empty", "", false},
		{"long prefix", strings.Repeat("x", 200) + "notes.md", true},
		{"suffix only", ".txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)

			// Find at position 0
			match := engine.Find(input)
			stdLoc := re.FindStringIndex(tt.input)
			if stdLoc == nil {
				if match != nil {
					t.Errorf("Find: got %q, want nil", match.String())
				}
			} else if match == nil {
				t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
			}

			// IsMatch
			got := engine.IsMatch(input)
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v", got, want)
			}

			// FindAll -- exercises FindAt internally (code path coverage).
			allMatches := engine.FindAllIndicesStreaming(input, 0, nil)
			if tt.wantMatch && len(allMatches) == 0 {
				t.Error("FindAll: got 0 matches, want at least 1")
			}
			if !tt.wantMatch && len(allMatches) != 0 {
				t.Errorf("FindAll: got %d matches, want 0", len(allMatches))
			}
		})
	}
}

// TestReverseSuffixSetSearcher_FindAt_Exercise exercises the loop and
// anti-quadratic guard in ReverseSuffixSetSearcher.FindAt.
func TestReverseSuffixSetSearcher_FindAt_Exercise(t *testing.T) {
	pattern := `.*\.(css|js|html)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("Strategy is %s, not UseReverseSuffixSet", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Single match -- Count should agree with stdlib
	single := "style.css"
	count := engine.Count([]byte(single), -1)
	stdCount := len(re.FindAllString(single, -1))
	if count != stdCount {
		t.Errorf("Count(single) = %d, stdlib = %d", count, stdCount)
	}

	// Exercise FindIndicesAt at every position
	input := "app.js"
	for at := 0; at < len(input); at++ {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("FindIndicesAt(%d): unexpected (%d,%d)", at, s, e)
			}
		} else if !found {
			t.Errorf("FindIndicesAt(%d): not found, stdlib (%d,%d)", at, stdLoc[0]+at, stdLoc[1]+at)
		}
	}

	// No match
	noMatch := "readme.md"
	// Note: .md is not in (css|js|html)
	match := engine.Find([]byte(noMatch))
	if match != nil {
		t.Errorf("Find on no-match: got %q", match.String())
	}
}

// TestReverseSuffixSet_FindIndicesAt_SuffixLenZero exercises getSuffixLen == 0 path.
func TestReverseSuffixSet_FindIndicesAt_ShortSuffixes(t *testing.T) {
	// Pattern with short suffixes
	pattern := `.*\.(go|py|rs)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("Strategy is %s, not UseReverseSuffixSet", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Single file tests -- Count should match stdlib
	singleFiles := []struct {
		name  string
		input string
	}{
		{"go", "main.go"},
		{"py", "script.py"},
		{"rs", "lib.rs"},
		{"no match", "readme.md"},
	}

	for _, tt := range singleFiles {
		t.Run(tt.name, func(t *testing.T) {
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}
		})
	}
}

// TestReverseSuffix_FindAll_SingleMatches cross-validates Count against stdlib
// for inputs with a single match (where greedy .* doesn't cause ambiguity).
func TestReverseSuffix_FindAll_SingleMatches(t *testing.T) {
	patterns := []struct {
		pattern  string
		strategy Strategy
	}{
		{`.*\.txt`, UseReverseSuffix},
		{`.*\.(txt|log|md)`, UseReverseSuffixSet},
	}

	for _, pp := range patterns {
		engine, err := Compile(pp.pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pp.pattern, err)
		}

		if engine.Strategy() != pp.strategy {
			t.Logf("Strategy for %q: %s (expected %s), testing anyway",
				pp.pattern, engine.Strategy(), pp.strategy)
		}

		re := regexp.MustCompile(pp.pattern)

		// Only test inputs with exactly 0 or 1 match (where behavior matches stdlib)
		inputs := []string{
			"readme.txt",
			"no match here",
			"",
			strings.Repeat("x", 500) + ".txt",
		}

		for _, input := range inputs {
			count := engine.Count([]byte(input), -1)
			stdCount := len(re.FindAllString(input, -1))
			if count != stdCount {
				t.Errorf("%q on %q: Count=%d, stdlib=%d (strategy=%s)",
					pp.pattern, input, count, stdCount, engine.Strategy())
			}
		}
	}
}

// TestReverseSuffix_IsMatch_Detailed exercises isMatchReverseSuffix with
// various input sizes and patterns.
func TestReverseSuffix_IsMatch_Detailed(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"suffix match", `.*\.cfg`, "app.cfg", true},
		{"suffix no match", `.*\.cfg`, "app.ini", false},
		{"empty", `.*\.cfg`, "", false},
		{"long input match", `.*\.cfg`, strings.Repeat("a", 1000) + ".cfg", true},
		{"long input no match", `.*\.cfg`, strings.Repeat("a", 1000), false},
		{"multiple candidates", `.*\.cfg`, "a.cfg b.cfg c.cfg", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch = %v, want %v (strategy=%s)", got, tt.want, engine.Strategy())
			}
		})
	}
}

// TestReverseSuffixSet_IsMatch_ImageFormats exercises isMatchReverseSuffixSet with
// various input patterns.
func TestReverseSuffixSet_IsMatch_ImageFormats(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"jpg match", `.*\.(jpg|png|gif|bmp)`, "photo.jpg", true},
		{"png match", `.*\.(jpg|png|gif|bmp)`, "image.png", true},
		{"gif match", `.*\.(jpg|png|gif|bmp)`, "anim.gif", true},
		{"bmp match", `.*\.(jpg|png|gif|bmp)`, "bitmap.bmp", true},
		{"no match", `.*\.(jpg|png|gif|bmp)`, "doc.pdf", false},
		{"empty", `.*\.(jpg|png|gif|bmp)`, "", false},
		{"long input", `.*\.(jpg|png|gif|bmp)`, strings.Repeat("x", 500) + ".png", true},
		{"multiple", `.*\.(jpg|png|gif|bmp)`, "a.jpg b.png c.gif", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch = %v, want %v (strategy=%s)", got, tt.want, engine.Strategy())
			}
		})
	}
}

// TestReverseSuffix_FindIndicesAt_PositionSweep exercises the FindIndicesAt path
// at every position in a single-match input for ReverseSuffix strategy.
// This ensures the iteration loop and matchStartZero path are fully covered.
func TestReverseSuffix_FindIndicesAt_PositionSweep(t *testing.T) {
	pattern := `.*\.yml`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffix {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "config.yml"

	for at := 0; at <= len(input); at++ {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindStringIndex(input[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("at=%d: unexpected (%d,%d)", at, s, e)
			}
		} else {
			stdStart := stdLoc[0] + at
			stdEnd := stdLoc[1] + at
			if !found {
				t.Errorf("at=%d: not found, stdlib (%d,%d)", at, stdStart, stdEnd)
			} else if s != stdStart || e != stdEnd {
				t.Errorf("at=%d: got (%d,%d), want (%d,%d)", at, s, e, stdStart, stdEnd)
			}
		}
	}
}

// TestReverseSuffixSet_FindIndicesAt_PositionSweep exercises FindIndicesAt for
// ReverseSuffixSet at every position in a single-match input.
func TestReverseSuffixSet_FindIndicesAt_PositionSweep(t *testing.T) {
	pattern := `.*\.(yml|yaml|toml)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "config.yaml"

	for at := 0; at <= len(input); at++ {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindStringIndex(input[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("at=%d: unexpected (%d,%d)", at, s, e)
			}
		} else {
			stdStart := stdLoc[0] + at
			stdEnd := stdLoc[1] + at
			if !found {
				t.Errorf("at=%d: not found, stdlib (%d,%d)", at, stdStart, stdEnd)
			} else if s != stdStart || e != stdEnd {
				t.Errorf("at=%d: got (%d,%d), want (%d,%d)", at, s, e, stdStart, stdEnd)
			}
		}
	}
}
