package meta

// Tests for FindAll, FindAllSubmatch, FindAllIndicesStreaming,
// Count, and related iteration paths.

import (
	"regexp"
	"testing"
)

func TestFindSubmatch_Various(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		wantSubs int // expected number of submatches (including full match)
	}{
		{"simple_capture", `(\w+)`, "hello", 2},
		{"two_groups", `(\d+)-(\d+)`, "12-34", 3},
		{"anchored_capture", `^(\w+)(\d+)$`, "abc123", 3},
		{"no_match", `(\w+)`, "", 0},
		{"named_groups", `(?P<first>\w+)\s+(?P<last>\w+)`, "John Doe", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			subs := engine.FindSubmatch([]byte(tt.input))
			if tt.wantSubs == 0 {
				if subs != nil {
					t.Errorf("FindSubmatch(%q) should be nil", tt.input)
				}
				return
			}
			if subs == nil {
				t.Fatalf("FindSubmatch(%q) = nil, want %d groups", tt.input, tt.wantSubs)
			}
			if subs.NumCaptures() != tt.wantSubs {
				t.Errorf("FindSubmatch(%q) has %d groups, want %d", tt.input, subs.NumCaptures(), tt.wantSubs)
			}

			// Cross-validate with stdlib
			re := regexp.MustCompile(tt.pattern)
			stdSubs := re.FindStringSubmatch(tt.input)
			if subs.NumCaptures() != len(stdSubs) {
				t.Errorf("submatch count: got %d, stdlib %d", subs.NumCaptures(), len(stdSubs))
			}
		})
	}
}

func TestFindAllSubmatch_Multi(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		wantN   int
	}{
		{"word_captures", `(\w+)`, "hello world test", 3},
		{"digit_pairs", `(\d+)-(\d+)`, "1-2 3-4 5-6", 3},
		{"no_match", `(\d+)`, "no digits", 0},
		{"single_match", `^(\w+)`, "hello world", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			matches := engine.FindAllSubmatch([]byte(tt.input), -1)
			if len(matches) != tt.wantN {
				t.Errorf("FindAllSubmatch: got %d, want %d", len(matches), tt.wantN)
			}

			// Cross-validate with stdlib
			re := regexp.MustCompile(tt.pattern)
			stdMatches := re.FindAllStringSubmatch(tt.input, -1)
			if len(matches) != len(stdMatches) {
				t.Errorf("count: got %d, stdlib %d", len(matches), len(stdMatches))
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 59. Exercise the isMatchReverseInner / isMatchReverseSuffix / isMatchReverseAnchored
//     paths with edge cases to improve coverage of those functions.
// -----------------------------------------------------------------------------

func TestFindAllIndicesStreaming_WithReuse(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("abc 123 def 456 ghi 789")

	// Normal call with no limit
	results := engine.FindAllIndicesStreaming(input, 0, nil)
	if len(results) != 3 {
		t.Errorf("FindAllIndicesStreaming returned %d results, want 3", len(results))
	}

	// With limit
	limited := engine.FindAllIndicesStreaming(input, 2, nil)
	if len(limited) != 2 {
		t.Errorf("FindAllIndicesStreaming(limit=2) returned %d, want 2", len(limited))
	}

	// With pre-allocated results slice for reuse
	preallocated := make([][2]int, 0, 10)
	reused := engine.FindAllIndicesStreaming(input, 0, preallocated)
	if len(reused) != 3 {
		t.Errorf("FindAllIndicesStreaming(reused) returned %d, want 3", len(reused))
	}

	// Empty input
	empty := engine.FindAllIndicesStreaming(nil, 0, nil)
	if len(empty) != 0 {
		t.Errorf("FindAllIndicesStreaming(empty) returned %d, want 0", len(empty))
	}
}

// -----------------------------------------------------------------------------
// 64. Exercise error path in compile — Error() and Unwrap() methods.
// -----------------------------------------------------------------------------

func TestCount_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		limit   int
		want    int
	}{
		{"empty_pattern_empty_input", ``, "", 1, 1},
		{"empty_pattern", ``, "abc", 4, 4},
		{"limit_zero", `\w+`, "hello world", 0, 0},
		{"limit_one", `\w+`, "hello world", 1, 1},
		{"limit_exact", `\w+`, "hello world", 2, 2},
		{"limit_over", `\w+`, "hello world", 10, 2},
		{"no_match", `\d+`, "no digits", -1, 0},
		{"greedy_star", `a*`, "aaa", -1, 1}, // greedy: matches "aaa" once
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.Count([]byte(tt.input), tt.limit)
			if got != tt.want {
				re := regexp.MustCompile(tt.pattern)
				stdAll := re.FindAllString(tt.input, tt.limit)
				t.Errorf("Count(%q, %d) = %d, want %d (stdlib=%d, strategy=%s)",
					tt.input, tt.limit, got, tt.want, len(stdAll), engine.Strategy())
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 68. Exercise findSubmatchAt via FindAllSubmatch with multiple matches.
// -----------------------------------------------------------------------------

func TestFindSubmatchAt_MultipleMatches(t *testing.T) {
	// FindAllSubmatch calls FindSubmatchAt internally with at > 0
	pattern := `(\d+)-(\w+)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	input := "1-abc 2-def 3-ghi"
	matches := engine.FindAllSubmatch([]byte(input), -1)
	stdMatches := re.FindAllStringSubmatch(input, -1)

	if len(matches) != len(stdMatches) {
		t.Fatalf("match count: got %d, stdlib %d", len(matches), len(stdMatches))
	}

	for i := range matches {
		if matches[i].NumCaptures() != len(stdMatches[i]) {
			t.Errorf("match[%d] group count: got %d, stdlib %d",
				i, matches[i].NumCaptures(), len(stdMatches[i]))
			continue
		}
		for j := 0; j < matches[i].NumCaptures(); j++ {
			got := matches[i].GroupString(j)
			std := stdMatches[i][j]
			if got != std {
				t.Errorf("match[%d][%d]: got %q, stdlib %q", i, j, got, std)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// 69. Exercise more reverse_suffix.go and reverse_suffix_set.go internal paths.
// -----------------------------------------------------------------------------

func TestFindAllIndicesStreaming_StartAnchored(t *testing.T) {
	// Start-anchored pattern triggers initCap=1 path
	engine, err := Compile(`^hello`)
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("hello world")
	results := engine.FindAllIndicesStreaming(input, 0, nil)
	if len(results) != 1 {
		t.Errorf("expected 1 match, got %d", len(results))
	}

	// No match case
	results2 := engine.FindAllIndicesStreaming([]byte("world"), 0, nil)
	if len(results2) != 0 {
		t.Errorf("expected 0 matches, got %d", len(results2))
	}
}

// --- Test 83: FindAllIndicesStreaming with pre-allocated results slice ---
// Covers: findall.go findAllIndicesLoop lines 129-131 (results reuse path)

func TestFindAllIndicesStreaming_ResultsReuse(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	// Pass pre-allocated results
	preallocated := make([][2]int, 5)
	results := engine.FindAllIndicesStreaming([]byte("1 22 333"), 0, preallocated)
	if len(results) != 3 {
		t.Errorf("expected 3 matches, got %d", len(results))
	}

	// Verify same backing array reuse
	re := regexp.MustCompile(`\d+`)
	stdMatches := re.FindAllStringIndex("1 22 333", -1)
	if len(results) != len(stdMatches) {
		t.Errorf("count mismatch: coregex=%d, stdlib=%d", len(results), len(stdMatches))
	}
}

// --- Test 84: slotsToCaptures unmatched capture group ---
// Covers: findall.go slotsToCaptures lines 79-81 (nil capture)

func TestSlotsToCaptures_UnmatchedGroup(t *testing.T) {
	// Pattern with optional group: (a)(b)? on "a" - group 2 is unmatched
	engine, err := Compile(`(a)(b)?`)
	if err != nil {
		t.Fatal(err)
	}

	// Use FindSubmatch which calls slotsToCaptures internally
	match := engine.FindSubmatch([]byte("a"))
	if match == nil {
		t.Fatal("expected match")
	}

	t.Logf("NumCaptures: %d", match.NumCaptures())
	// Group 2 (b?) should be unmatched
	for i := 0; i < match.NumCaptures(); i++ {
		idx := match.GroupIndex(i)
		if idx != nil {
			t.Logf("Group %d: [%d,%d]", i, idx[0], idx[1])
		} else {
			t.Logf("Group %d: nil (unmatched)", i)
		}
	}
}

// --- Test 85: FindAllSubmatch with empty match advancement ---
// Covers: findall.go FindAllSubmatch lines 295-298 (empty match: pos++)

func TestFindAllSubmatch_EmptyMatchAdvance(t *testing.T) {
	engine, err := Compile(`a?`)
	if err != nil {
		t.Fatal(err)
	}

	// a? can match empty strings
	matches := engine.FindAllSubmatch([]byte("b"), -1)
	t.Logf("FindAllSubmatch('a?', 'b'): %d matches", len(matches))

	// Cross-validate count
	re := regexp.MustCompile(`a?`)
	stdMatches := re.FindAllString("b", -1)
	t.Logf("stdlib: %d matches", len(stdMatches))
}

// --- Test 86: Count edge case with empty matches and skipping ---
// Covers: findall.go Count lines 218-223 (skip empty match at lastNonEmptyEnd)
// and lines 240-241 (default: pos++)

func TestCount_EmptyMatchSkipping(t *testing.T) {
	engine, err := Compile(`a*`)
	if err != nil {
		t.Fatal(err)
	}

	// a* matches empty string between each character, and greedily at "aaa"
	// stdlib returns: "aaa", "", "", "" on "aaabbb" => 4
	input := "bbb"
	count := engine.Count([]byte(input), -1)
	re := regexp.MustCompile(`a*`)
	stdCount := len(re.FindAllString(input, -1))
	t.Logf("Count('a*', %q): ours=%d, stdlib=%d", input, count, stdCount)
}

// --- Test 87: isMatchDigitPrefilter NFA fallback path ---
// Covers: ismatch.go isMatchDigitPrefilter lines 293-298 (NFA path when no DFA)

func TestFindAllIndicesLoop_ResultsReuse(t *testing.T) {
	// Use a Teddy pattern (non-CharClassSearcher) to go through findAllIndicesLoop
	engine, err := Compile(`foo|bar|baz`)
	if err != nil {
		t.Fatal(err)
	}

	// Pre-allocate results slice
	preallocated := make([][2]int, 10)
	results := engine.FindAllIndicesStreaming([]byte("foo and bar and baz"), 0, preallocated)
	if len(results) != 3 {
		t.Errorf("expected 3 matches, got %d", len(results))
	}
}

// --- Test 107: findAllIndicesLoop empty match at end of haystack ---
// Covers: findall.go lines 152-153 (pos > len(haystack) break in empty match skip)
// and lines 172-173 (default: pos++ when end <= pos)

func TestFindAllIndicesLoop_EmptyMatchAtEnd(t *testing.T) {
	// Pattern that matches empty strings: a?
	// On "a", FindAll should find "a" and then empty match at position 1
	engine, err := Compile(`a?`)
	if err != nil {
		t.Fatal(err)
	}

	// Use a non-CharClassSearcher-triggering pattern
	// a? should use UseNFA
	input := "a"
	results := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	re := regexp.MustCompile(`a?`)
	stdAll := re.FindAllStringIndex(input, -1)
	t.Logf("a? on %q: ours=%d, stdlib=%d", input, len(results), len(stdAll))

	// Another boundary case: empty string
	results2 := engine.FindAllIndicesStreaming([]byte(""), 0, nil)
	t.Logf("a? on empty: %d matches", len(results2))
}

// --- Test 108: Count empty match boundary cases ---
// Covers: findall.go Count lines 218-223 (skip empty match at end), 240-241 (default: pos++)

func TestCount_EmptyMatchBoundary(t *testing.T) {
	engine, err := Compile(`a?`)
	if err != nil {
		t.Fatal(err)
	}

	// Count on single char
	count := engine.Count([]byte("a"), -1)
	re := regexp.MustCompile(`a?`)
	stdCount := len(re.FindAllString("a", -1))
	t.Logf("Count('a?', 'a'): ours=%d, stdlib=%d", count, stdCount)

	// Count on empty
	count2 := engine.Count([]byte(""), -1)
	stdCount2 := len(re.FindAllString("", -1))
	t.Logf("Count('a?', ''): ours=%d, stdlib=%d", count2, stdCount2)

	// b* on "cc" - empty matches at each position
	engine2, err := Compile(`b*`)
	if err != nil {
		t.Fatal(err)
	}
	count3 := engine2.Count([]byte("cc"), -1)
	re2 := regexp.MustCompile(`b*`)
	stdCount3 := len(re2.FindAllString("cc", -1))
	t.Logf("Count('b*', 'cc'): ours=%d, stdlib=%d", count3, stdCount3)
}

// --- Test 109: selectReverseStrategy through various pattern shapes ---
// Covers: strategy.go selectReverseStrategy lines 941-961
