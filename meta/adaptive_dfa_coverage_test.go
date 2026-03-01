package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestAdaptive_UseBoth exercises the UseBoth (adaptive) strategy paths.
// UseBoth is selected for medium NFA (20-100 states) without strong literals.
func TestAdaptive_UseBoth(t *testing.T) {
	// Build a pattern with 20-100 NFA states and no good literals.
	// Character class alternation without extractable literals.
	// (a|b|c|d|...|z)*X where X forces enough states.
	letters := "a|b|c|d|e|f|g|h|i|j|k|l|m|n|o|p|q|r|s|t|u|v|w|x|y|z"
	pattern := "(" + letters + ")*X"
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for (%s)*X: %s", letters, strategy)

	// If this doesn't trigger UseBoth, try another approach
	if strategy != UseBoth {
		// Try a pattern with repeated character classes
		pattern2 := `[a-m]+[n-z]+[a-m]+[n-z]+[a-m]+`
		engine2, err := Compile(pattern2)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		strategy = engine2.Strategy()
		t.Logf("Strategy for %q: %s", pattern2, strategy)
		if strategy == UseBoth {
			engine = engine2
			pattern = pattern2
		}
	}

	// Regardless of which strategy was selected, test Find/FindAt/FindIndicesAt
	input := "abcdefghijklmnopqrstuvwxyzX trailing"
	re := regexp.MustCompile(pattern)

	// Find
	match := engine.Find([]byte(input))
	stdLoc := re.FindStringIndex(input)
	if match != nil && stdLoc != nil {
		if match.Start() != stdLoc[0] || match.End() != stdLoc[1] {
			t.Errorf("Find: got [%d,%d], stdlib [%d,%d]",
				match.Start(), match.End(), stdLoc[0], stdLoc[1])
		}
	}

	// FindAt at non-zero position
	match2 := engine.FindAt([]byte(input), 5)
	if match2 != nil {
		t.Logf("FindAt(5): %q at [%d,%d]", match2.String(), match2.Start(), match2.End())
	}

	// FindIndices
	s, e, found := engine.FindIndices([]byte(input))
	if found && stdLoc != nil {
		if s != stdLoc[0] || e != stdLoc[1] {
			t.Errorf("FindIndices: got [%d,%d], stdlib [%d,%d]", s, e, stdLoc[0], stdLoc[1])
		}
	}

	// FindIndicesAt at non-zero
	s2, e2, found2 := engine.FindIndicesAt([]byte(input), 5)
	if found2 {
		t.Logf("FindIndicesAt(5): [%d,%d]", s2, e2)
	}

	// Count with multiple matches
	multiInput := "aX bX cX dX eX"
	count := engine.Count([]byte(multiInput), -1)
	stdCount := len(re.FindAllString(multiInput, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d for %q", count, stdCount, multiInput)
	}
}

// TestUseDFA_Find exercises the UseDFA strategy in detail.
// UseDFA is selected for large NFA (>= 20 states) with good literals.
func TestUseDFA_Find(t *testing.T) {
	// Pattern with >20 NFA states and extractable literal
	pattern := `LONGPREFIX[a-z]{5,20}[0-9]{3,10}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy: %s", strategy)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"match", "junk LONGPREFIX" + "abcde" + "12345" + " more", true},
		{"no match", "junk SHORTPFX" + "abc" + "12" + " more", false},
		{"empty", "", false},
		{"only prefix", "LONGPREFIX", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := engine.Find([]byte(tt.input))
			got := match != nil
			if got != tt.want {
				t.Errorf("Find(%q) = %v, want %v (strategy=%s)",
					tt.input, got, tt.want, strategy)
			}
		})
	}

	// Test FindAt at non-zero positions
	input := "junk LONGPREFIX" + "abcde" + "12345" + " more"
	match := engine.FindAt([]byte(input), 5)
	if match == nil {
		t.Error("FindAt(5) should find the match")
	} else {
		t.Logf("FindAt(5): %q at [%d,%d]", match.String(), match.Start(), match.End())
	}

	// FindIndicesAt at non-zero
	s, e, found := engine.FindIndicesAt([]byte(input), 5)
	if !found {
		t.Error("FindIndicesAt(5) should find the match")
	} else {
		t.Logf("FindIndicesAt(5): [%d,%d]", s, e)
	}
}

// TestUseDFA_Count exercises UseDFA with multiple matches (exercises *At paths).
func TestUseDFA_Count(t *testing.T) {
	pattern := `LONGPREFIX[a-z]{3,10}[0-9]{2,5}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(pattern)

	input := "LONGPREFIXabc12 LONGPREFIXdef345 LONGPREFIXghi6789"
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d (strategy=%s)", count, stdCount, engine.Strategy())
	}
}

// TestUseDFA_LargeNFA_NoLiterals exercises UseDFA for patterns with >100 NFA states.
func TestUseDFA_LargeNFA_NoLiterals(t *testing.T) {
	// Build a large alternation pattern (>100 states) without extractable literals
	var parts []string
	for i := 0; i < 30; i++ {
		parts = append(parts, strings.Repeat(string(rune('a'+i%26)), 2+i%3))
	}
	pattern := "(" + strings.Join(parts, "|") + ")*Z"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for large pattern: %s", strategy)

	input := "aaZ"
	match := engine.Find([]byte(input))
	if match != nil {
		t.Logf("Match: %q at [%d,%d]", match.String(), match.Start(), match.End())
	}

	// FindAt at non-zero
	input2 := "xxx aaZ yyy"
	match2 := engine.FindAt([]byte(input2), 4)
	if match2 != nil {
		t.Logf("FindAt(4): %q", match2.String())
	}
}

// TestBranchDispatch_FindAt_NonZero exercises findBranchDispatchAt with at > 0.
// For anchored patterns, FindAt(at>0) should always return nil.
func TestBranchDispatch_FindAt_NonZero(t *testing.T) {
	engine, err := Compile(`^(foo|bar|baz|qux)`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBranchDispatch {
		t.Skipf("Strategy is %s, not UseBranchDispatch", engine.Strategy())
	}

	// At position 0
	match := engine.FindAt([]byte("foo123"), 0)
	if match == nil || match.String() != "foo" {
		t.Errorf("FindAt(0): got %v, want foo", match)
	}

	// At position > 0 should always return nil (anchored)
	for _, at := range []int{1, 2, 3, 4, 5} {
		match := engine.FindAt([]byte("foo123"), at)
		if match != nil {
			t.Errorf("FindAt(%d): got %q, want nil (anchored pattern)", at, match.String())
		}
	}

	// FindIndicesAt at > 0
	s, e, found := engine.FindIndicesAt([]byte("foo123"), 1)
	if found {
		t.Errorf("FindIndicesAt(1): got (%d,%d,true), want not found (anchored)", s, e)
	}

	// Count should be exactly 1 for anchored patterns
	count := engine.Count([]byte("foo123"), -1)
	if count != 1 {
		t.Errorf("Count = %d, want 1 (anchored pattern)", count)
	}
}

// TestAnchoredLiteral_FindAt_NonZero exercises findAnchoredLiteral at > 0.
func TestAnchoredLiteral_FindAt_NonZero(t *testing.T) {
	engine, err := Compile(`^hello.*world$`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseAnchoredLiteral {
		t.Skipf("Strategy is %s, not UseAnchoredLiteral", engine.Strategy())
	}

	// At position 0
	match := engine.FindAt([]byte("hello beautiful world"), 0)
	if match == nil {
		t.Error("FindAt(0): want match")
	}

	// At position > 0 should return nil (start-anchored)
	match2 := engine.FindAt([]byte("hello beautiful world"), 1)
	if match2 != nil {
		t.Errorf("FindAt(1): got %q, want nil", match2.String())
	}

	// FindIndicesAt at > 0
	_, _, found := engine.FindIndicesAt([]byte("hello world"), 1)
	if found {
		t.Error("FindIndicesAt(1): want not found (anchored)")
	}
}

// TestReverseInner_Find_Detailed exercises the ReverseInner Find method in detail.
// This covers the bidirectional DFA search path which was at 14% coverage.
func TestReverseInner_Find_Detailed(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		// Basic inner literal
		{"connection found", `.*connection.*`, "error: connection refused", true},
		{"connection not found", `.*connection.*`, "everything is fine", false},
		{"empty input", `.*connection.*`, "", false},

		// Inner literal with non-universal prefix
		{"keyword with prefix", `.*error.*timeout.*`, "error: connection timeout", true},
		{"keyword no match", `.*error.*timeout.*`, "warning: connection refused", false},

		// Multiple candidates
		{"first match wins", `.*test.*`, "test and test again", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			if engine.Strategy() != UseReverseInner {
				t.Skipf("Strategy is %s", engine.Strategy())
			}

			match := engine.Find([]byte(tt.input))
			got := match != nil
			if got != tt.want {
				t.Errorf("Find(%q) = %v, want %v", tt.input, got, tt.want)
			}

			// Cross-validate with stdlib
			re := regexp.MustCompile(tt.pattern)
			stdMatch := re.MatchString(tt.input)
			if got != stdMatch {
				t.Errorf("stdlib=%v, ours=%v for %q on %q", stdMatch, got, tt.pattern, tt.input)
			}
		})
	}
}

// TestReverseInner_FindIndicesAt_Coverage exercises ReverseInner FindIndicesAt.
func TestReverseInner_FindIndicesAt_Coverage(t *testing.T) {
	engine, err := Compile(`.*connection.*`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseInner {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	input := "error: connection refused"

	// FindIndicesAt(0) -- should find match
	s, e, found := engine.FindIndicesAt([]byte(input), 0)
	if !found {
		t.Error("FindIndicesAt(0) should find match")
	} else {
		t.Logf("FindIndicesAt(0): [%d,%d] = %q", s, e, input[s:e])
	}

	// Count
	count := engine.Count([]byte(input), -1)
	if count != 1 {
		t.Errorf("Count = %d, want 1", count)
	}
}

// TestReverseSuffix_FindAt_Direct exercises ReverseSuffix FindAt (Match-returning version).
func TestReverseSuffix_FindAt_Direct(t *testing.T) {
	// Use a pattern that generates ReverseSuffix strategy
	engine, err := Compile(`.*\.txt`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffix {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	// The reverse searcher dispatches FindAt through the meta-engine
	// For non-zero positions, reverse strategies fall back to NFA
	input := "first.txt second.txt"
	match := engine.FindAt([]byte(input), 0)
	if match == nil {
		t.Error("FindAt(0) should find match")
	} else {
		t.Logf("FindAt(0): %q at [%d,%d]", match.String(), match.Start(), match.End())
	}

	// Non-zero position: reverse strategies fall back to NFA at non-zero positions
	match2 := engine.FindAt([]byte(input), 10)
	if match2 != nil {
		t.Logf("FindAt(10): %q at [%d,%d]", match2.String(), match2.Start(), match2.End())
	}

	// Count -- exercises the internal FindAllIndicesStreaming loop
	count := engine.Count([]byte(input), -1)
	if count < 1 {
		t.Errorf("Count = %d, expected at least 1", count)
	}
}

// TestReverseSuffixSet_FindAt_Direct exercises ReverseSuffixSet FindAt.
func TestReverseSuffixSet_FindAt_Direct(t *testing.T) {
	engine, err := Compile(`.*\.(txt|log|md)`)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	input := "file.txt app.log readme.md"
	match := engine.Find([]byte(input))
	if match == nil {
		t.Error("Find should find match")
	}

	// Count
	count := engine.Count([]byte(input), -1)
	if count < 1 {
		t.Errorf("Count = %d, want >= 1", count)
	}
}

// TestFindAllSubmatch_MultiStrategy exercises FindAllSubmatch across strategies.
// This calls FindSubmatchAt internally with at > 0.
func TestFindAllSubmatch_MultiStrategy(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		wantN   int // expected number of matches
	}{
		{"NFA captures", `(a)(b)(c)`, "abc abc abc", 3},
		{"BT captures", `^(\w+)`, "hello", 1},
		{"digit captures", `(\d+)-(\d+)`, "12-34 56-78", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			matches := engine.FindAllSubmatch([]byte(tt.input), -1)
			if len(matches) != tt.wantN {
				t.Errorf("FindAllSubmatch got %d matches, want %d (strategy=%s)",
					len(matches), tt.wantN, engine.Strategy())
			}
		})
	}
}

// TestIsMatch_WithLargeInput exercises various strategies on large input.
func TestIsMatch_WithLargeInput(t *testing.T) {
	size := 100_000
	input := strings.Repeat("x", size)
	inputWithMatch := input + "hello"

	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"NFA large match", `hello`, inputWithMatch, true},
		{"NFA large no match", `hello`, input, false},
		{"CharClass large", `[a-z]+`, input, true},
		{"Teddy large no match", `alpha|beta|gamma`, input, false},
		{"Teddy large match", `alpha|beta|gamma`, input + "alpha", true},
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
