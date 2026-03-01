package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestCountVariousPatterns tests Count for various pattern types and match counts.
func TestCountVariousPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		n        int
		want     int
	}{
		// Zero matches
		{"zero_digit_no_input", `\d+`, "", -1, 0},
		{"zero_digit_no_match", `\d+`, "no digits here", -1, 0},
		{"zero_literal_no_match", "xyz", "abc def", -1, 0},

		// Single match
		{"one_literal", "hello", "say hello world", -1, 1},
		{"one_digit", `\d+`, "test42end", -1, 1},
		{"one_suffix", `.*\.txt`, "readme.txt", -1, 1},

		// Multiple matches
		{"multi_word", `\w+`, "the quick brown fox", -1, 4},
		{"multi_digit", `\d+`, "1 22 333 4444", -1, 4},
		{"multi_literal", "ab", "ababab", -1, 3},
		{"multi_alternation", "cat|dog", "cat dog cat", -1, 3},

		// With limit
		{"limit_0", `\w+`, "a b c d", 0, 0},
		{"limit_1", `\w+`, "a b c d", 1, 1},
		{"limit_2", `\w+`, "a b c d", 2, 2},
		{"limit_exceeds", `\w+`, "a b", 10, 2},

		// Edge cases
		{"empty_pattern", "", "abc", -1, 4}, // empty pattern matches at every position + end
		{"empty_input_empty_pattern", "", "", -1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.Count([]byte(tt.haystack), tt.n)
			if got != tt.want {
				t.Errorf("Count(%q, %d) = %d, want %d (strategy: %s)",
					tt.haystack, tt.n, got, tt.want, engine.Strategy())
			}
		})
	}
}

// TestCountVsStdlib compares Count results against Go stdlib.
func TestCountVsStdlib(t *testing.T) {
	patterns := []string{
		`\d+`,
		`\w+`,
		`[a-z]+`,
		"test",
		"a",
	}

	haystacks := []string{
		"hello world 123 456",
		"test test test",
		"abc def ghi",
		"1 22 333",
		"no match at all!!!",
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}
		re := regexp.MustCompile(pattern)

		for _, haystack := range haystacks {
			h := []byte(haystack)
			coregexCount := engine.Count(h, -1)
			stdlibMatches := re.FindAllIndex(h, -1)
			stdlibCount := len(stdlibMatches)

			if coregexCount != stdlibCount {
				t.Errorf("pattern %q, haystack %q: coregex Count=%d, stdlib=%d",
					pattern, haystack, coregexCount, stdlibCount)
			}
		}
	}
}

// TestCountConsistencyWithFindAllIndicesStreaming verifies Count and
// FindAllIndicesStreaming agree on match counts for various strategies.
func TestCountConsistencyWithFindAllIndicesStreaming(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
	}{
		{`\w+`, "hello world foo bar"},
		{`\d+`, "1 22 333 4444 55555"},
		{`[a-z]+`, "ABC abc DEF def"},
		{"foo|bar|baz", "foo bar baz foo"},
		{`[a-z]+[0-9]+`, "a1 bb22 ccc333"},
		{`.*\.txt`, "a.txt"},
		{`(\w)+`, "abc def"},
		{`\d+\.\d+`, "1.2 3.4 5.6"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			h := []byte(tt.haystack)
			count := engine.Count(h, -1)
			all := engine.FindAllIndicesStreaming(h, -1, nil)

			if count != len(all) {
				t.Errorf("pattern %q: Count=%d, FindAllIndicesStreaming=%d (strategy: %s)",
					tt.pattern, count, len(all), engine.Strategy())
			}
		})
	}
}

// TestFindAllSubmatchBasic tests FindAllSubmatch with capture group patterns.
func TestFindAllSubmatchBasic(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		haystack   string
		n          int
		wantCount  int
		wantGroups [][]string // [match][group] = text
	}{
		{
			"email_captures",
			`(\w+)@(\w+)`,
			"user@host admin@server",
			-1,
			2,
			[][]string{
				{"user@host", "user", "host"},
				{"admin@server", "admin", "server"},
			},
		},
		{
			"digit_groups",
			`(\d+)-(\d+)`,
			"123-456 789-012",
			-1,
			2,
			[][]string{
				{"123-456", "123", "456"},
				{"789-012", "789", "012"},
			},
		},
		{
			"single_capture",
			`(\d+)`,
			"abc 42 def 99",
			-1,
			2,
			[][]string{
				{"42", "42"},
				{"99", "99"},
			},
		},
		{
			"with_limit",
			`(\w+)`,
			"a b c d",
			2,
			2,
			[][]string{
				{"a", "a"},
				{"b", "b"},
			},
		},
		{
			"no_match",
			`(\d+)`,
			"no digits",
			-1,
			0,
			nil,
		},
		{
			"limit_zero",
			`(\d+)`,
			"123 456",
			0,
			0,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			matches := engine.FindAllSubmatch([]byte(tt.haystack), tt.n)
			if len(matches) != tt.wantCount {
				t.Fatalf("got %d matches, want %d", len(matches), tt.wantCount)
			}

			for i, wantGroups := range tt.wantGroups {
				if i >= len(matches) {
					break
				}
				m := matches[i]

				// Check group 0 (entire match)
				if m.String() != wantGroups[0] {
					t.Errorf("match[%d] group 0 = %q, want %q", i, m.String(), wantGroups[0])
				}

				// Check capture groups
				for g := 1; g < len(wantGroups); g++ {
					got := m.GroupString(g)
					if got != wantGroups[g] {
						t.Errorf("match[%d] group %d = %q, want %q", i, g, got, wantGroups[g])
					}
				}
			}
		})
	}
}

// TestFindAllSubmatchOptionalCaptures tests FindAllSubmatch with optional capture groups.
func TestFindAllSubmatchOptionalCaptures(t *testing.T) {
	engine, err := Compile(`(\d+)(?:\.(\d+))?`)
	if err != nil {
		t.Fatal(err)
	}

	matches := engine.FindAllSubmatch([]byte("42 1.23 99"), -1)
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3", len(matches))
	}

	// First match: "42" - group 2 not captured
	if matches[0].String() != "42" {
		t.Errorf("match 0 = %q, want %q", matches[0].String(), "42")
	}
	if matches[0].GroupString(1) != "42" {
		t.Errorf("match 0 group 1 = %q, want %q", matches[0].GroupString(1), "42")
	}

	// Second match: "1.23" - both groups captured
	if matches[1].String() != "1.23" {
		t.Errorf("match 1 = %q, want %q", matches[1].String(), "1.23")
	}
	if matches[1].GroupString(1) != "1" {
		t.Errorf("match 1 group 1 = %q, want %q", matches[1].GroupString(1), "1")
	}
	if matches[1].GroupString(2) != "23" {
		t.Errorf("match 1 group 2 = %q, want %q", matches[1].GroupString(2), "23")
	}

	// Third match: "99"
	if matches[2].String() != "99" {
		t.Errorf("match 2 = %q, want %q", matches[2].String(), "99")
	}
}

// TestFindAllIndicesStreamingCallbackBehavior tests FindAllIndicesStreaming
// with various limits and result reuse.
func TestFindAllIndicesStreamingCallbackBehavior(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("1 22 333 4444 55555")

	// No limit
	all := engine.FindAllIndicesStreaming(haystack, 0, nil)
	if len(all) != 5 {
		t.Errorf("no limit: got %d matches, want 5", len(all))
	}

	// With limit
	limited := engine.FindAllIndicesStreaming(haystack, 3, nil)
	if len(limited) != 3 {
		t.Errorf("limit 3: got %d matches, want 3", len(limited))
	}

	// Reuse slice
	preallocated := make([][2]int, 0, 10)
	reused := engine.FindAllIndicesStreaming(haystack, -1, preallocated)
	if len(reused) != 5 {
		t.Errorf("reuse: got %d matches, want 5", len(reused))
	}
}

// TestFindAllIndicesStreamingPositions verifies exact positions returned.
func TestFindAllIndicesStreamingPositions(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     [][2]int
	}{
		{
			"words",
			`\w+`,
			"the fox",
			[][2]int{{0, 3}, {4, 7}},
		},
		{
			"digits",
			`\d+`,
			"a1b22c333",
			[][2]int{{1, 2}, {3, 5}, {6, 9}},
		},
		{
			"literal",
			"ab",
			"xababx",
			[][2]int{{1, 3}, {3, 5}},
		},
		{
			"alternation",
			"cat|dog",
			"a cat and dog",
			[][2]int{{2, 5}, {10, 13}},
		},
		{
			"no_matches",
			`\d+`,
			"no digits",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.FindAllIndicesStreaming([]byte(tt.haystack), -1, nil)

			if len(got) != len(tt.want) {
				t.Fatalf("got %d matches %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("match[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestFindAllIndicesStreamingVsStdlib compares all-match results against stdlib.
func TestFindAllIndicesStreamingVsStdlib(t *testing.T) {
	patterns := []string{
		`\d+`,
		`\w+`,
		`[a-z]+`,
		"test",
	}

	haystacks := []string{
		"hello world 123 456",
		"test test test",
		"1 22 333 4444",
		"no match !!!",
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}
		re := regexp.MustCompile(pattern)

		for _, haystack := range haystacks {
			h := []byte(haystack)
			coregex := engine.FindAllIndicesStreaming(h, -1, nil)
			stdlib := re.FindAllIndex(h, -1)

			if len(coregex) != len(stdlib) {
				t.Errorf("pattern %q, haystack %q: coregex %d matches, stdlib %d",
					pattern, haystack, len(coregex), len(stdlib))
				continue
			}

			for i := range coregex {
				if coregex[i][0] != stdlib[i][0] || coregex[i][1] != stdlib[i][1] {
					t.Errorf("pattern %q, haystack %q, match[%d]: coregex (%d,%d), stdlib (%d,%d)",
						pattern, haystack, i,
						coregex[i][0], coregex[i][1],
						stdlib[i][0], stdlib[i][1])
				}
			}
		}
	}
}

// TestFindSubmatchBasic tests FindSubmatch with various capture patterns.
func TestFindSubmatchBasic(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		haystack   string
		wantMatch  bool
		wantGroups []string // group 0, 1, 2, ...
	}{
		{
			"email",
			`(\w+)@(\w+)\.(\w+)`,
			"user@example.com",
			true,
			[]string{"user@example.com", "user", "example", "com"},
		},
		{
			"date",
			`(\d{4})-(\d{2})-(\d{2})`,
			"date: 2024-01-15",
			true,
			[]string{"2024-01-15", "2024", "01", "15"},
		},
		{
			"no_match",
			`(\d+)-(\d+)`,
			"no numbers",
			false,
			nil,
		},
		{
			"simple_group",
			`(\w+)`,
			"hello world",
			true,
			[]string{"hello", "hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			match := engine.FindSubmatch([]byte(tt.haystack))
			if tt.wantMatch {
				if match == nil {
					t.Fatal("expected match, got nil")
				}
				for i, want := range tt.wantGroups {
					got := match.GroupString(i)
					if got != want {
						t.Errorf("group %d = %q, want %q", i, got, want)
					}
				}
			} else if match != nil {
				t.Errorf("expected no match, got %q", match.String())
			}
		})
	}
}

// TestFindSubmatchAt tests FindSubmatchAt with various starting positions.
func TestFindSubmatchAt(t *testing.T) {
	engine, err := Compile(`(\w+)`)
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("abc def ghi")

	// At position 0 -> "abc"
	m0 := engine.FindSubmatchAt(haystack, 0)
	if m0 == nil || m0.GroupString(1) != "abc" {
		t.Errorf("at 0: got %v, want 'abc'", m0)
	}

	// At position 4 -> "def"
	m4 := engine.FindSubmatchAt(haystack, 4)
	if m4 == nil || m4.GroupString(1) != "def" {
		t.Errorf("at 4: got %v, want 'def'", m4)
	}

	// At position 8 -> "ghi"
	m8 := engine.FindSubmatchAt(haystack, 8)
	if m8 == nil || m8.GroupString(1) != "ghi" {
		t.Errorf("at 8: got %v, want 'ghi'", m8)
	}

	// Past end -> nil
	m12 := engine.FindSubmatchAt(haystack, 12)
	if m12 != nil {
		t.Errorf("at 12: expected nil, got %q", m12.String())
	}
}

// TestFindSubmatchNumCaptures tests NumCaptures on the match.
func TestFindSubmatchNumCaptures(t *testing.T) {
	tests := []struct {
		pattern      string
		wantCaptures int
	}{
		{`\w+`, 1},         // group 0 only
		{`(\w+)`, 2},       // group 0 + group 1
		{`(\w+)(\d+)`, 3},  // group 0 + 2 captures
		{`((\w+))`, 3},     // group 0 + nested captures
		{`(?:(\w+))`, 2},   // non-capturing wrapper
		{`(\w+)?(\d+)`, 3}, // optional group
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			if engine.NumCaptures() != tt.wantCaptures {
				t.Errorf("NumCaptures() = %d, want %d", engine.NumCaptures(), tt.wantCaptures)
			}
		})
	}
}

// TestCountLargeInput tests Count on large inputs.
func TestCountLargeInput(t *testing.T) {
	// 10000 words
	words := make([]string, 10000)
	for i := range words {
		words[i] = "word"
	}
	haystack := []byte(strings.Join(words, " "))

	engine, err := Compile(`\w+`)
	if err != nil {
		t.Fatal(err)
	}

	count := engine.Count(haystack, -1)
	if count != 10000 {
		t.Errorf("Count on 10000 words = %d, want 10000", count)
	}

	// With limit
	limitCount := engine.Count(haystack, 100)
	if limitCount != 100 {
		t.Errorf("Count with limit 100 = %d, want 100", limitCount)
	}
}

// TestFindAllIndicesStreamingEmptyMatches tests handling of empty matches.
func TestFindAllIndicesStreamingEmptyMatches(t *testing.T) {
	// Pattern that can match empty: a*
	engine, err := Compile("a*")
	if err != nil {
		t.Fatal(err)
	}

	// Compare with stdlib
	re := regexp.MustCompile("a*")
	haystack := []byte("ab")

	coregex := engine.FindAllIndicesStreaming(haystack, -1, nil)
	stdlib := re.FindAllIndex(haystack, -1)

	if len(coregex) != len(stdlib) {
		t.Errorf("a* on 'ab': coregex %d matches, stdlib %d matches\ncoregex: %v\nstdlib: %v",
			len(coregex), len(stdlib), coregex, stdlib)
		return
	}

	for i := range coregex {
		if coregex[i][0] != stdlib[i][0] || coregex[i][1] != stdlib[i][1] {
			t.Errorf("match[%d]: coregex %v, stdlib %v", i, coregex[i], stdlib[i])
		}
	}
}

// TestCountEmptyPatternMatchesEverywhere verifies that empty pattern
// matches at every position (consistent with stdlib).
func TestCountEmptyPatternMatchesEverywhere(t *testing.T) {
	engine, err := Compile("")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile("")

	haystacks := []string{"", "a", "ab", "abc"}

	for _, h := range haystacks {
		coregexCount := engine.Count([]byte(h), -1)
		stdlibCount := len(re.FindAllStringIndex(h, -1))

		if coregexCount != stdlibCount {
			t.Errorf("empty pattern on %q: coregex=%d, stdlib=%d", h, coregexCount, stdlibCount)
		}
	}
}

// TestFindAllSubmatchNoCaptures tests FindAllSubmatch with a pattern that has no captures.
func TestFindAllSubmatchNoCaptures(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	matches := engine.FindAllSubmatch([]byte("1 22 333"), -1)
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3", len(matches))
	}

	// Group 0 should still work
	want := []string{"1", "22", "333"}
	for i, m := range matches {
		if m.String() != want[i] {
			t.Errorf("match[%d] = %q, want %q", i, m.String(), want[i])
		}
	}
}
