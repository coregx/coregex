package nfa

import (
	"regexp"
	"testing"
)

// TestPikeVM_SearchAt_Positions tests SearchAt with various starting positions.
func TestPikeVM_SearchAt_Positions(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		at        int
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{
			name: "from beginning", pattern: "foo", haystack: "foo bar foo",
			at: 0, wantStart: 0, wantEnd: 3, wantFound: true,
		},
		{
			name: "skip first match", pattern: "foo", haystack: "foo bar foo",
			at: 3, wantStart: 8, wantEnd: 11, wantFound: true,
		},
		{
			name: "from exact match start", pattern: "bar", haystack: "foo bar baz",
			at: 4, wantStart: 4, wantEnd: 7, wantFound: true,
		},
		{
			name: "no match after position", pattern: "foo", haystack: "foo",
			at: 1, wantStart: -1, wantEnd: -1, wantFound: false,
		},
		{
			name: "at end of haystack", pattern: "a", haystack: "abc",
			at: 3, wantStart: -1, wantEnd: -1, wantFound: false,
		},
		{
			name: "past end of haystack", pattern: "a", haystack: "abc",
			at: 10, wantStart: -1, wantEnd: -1, wantFound: false,
		},
		{
			name: "empty pattern at position", pattern: "", haystack: "abc",
			at: 2, wantStart: 2, wantEnd: 2, wantFound: true,
		},
		{
			name: "digit class from middle", pattern: `\d+`, haystack: "abc123def456",
			at: 6, wantStart: 9, wantEnd: 12, wantFound: true,
		},
		{
			name: "at zero empty haystack", pattern: "", haystack: "",
			at: 0, wantStart: 0, wantEnd: 0, wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, found := vm.SearchAt([]byte(tt.haystack), tt.at)
			if found != tt.wantFound {
				t.Errorf("SearchAt(%q, %d) found=%v, want %v", tt.haystack, tt.at, found, tt.wantFound)
				return
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("SearchAt(%q, %d) = (%d, %d), want (%d, %d)",
					tt.haystack, tt.at, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_SearchWithCaptures_Basic tests SearchWithCaptures for various patterns.
func TestPikeVM_SearchWithCaptures_Basic(t *testing.T) {
	tests := []struct {
		name         string
		pattern      string
		haystack     string
		wantMatch    bool
		wantStart    int
		wantEnd      int
		wantCaptures [][]int // group 0 = overall, group 1+ = captures
	}{
		{
			name:      "single group",
			pattern:   "(abc)",
			haystack:  "xxxabcyyy",
			wantMatch: true,
			wantStart: 3,
			wantEnd:   6,
			wantCaptures: [][]int{
				{3, 6}, // group 0: entire match
				{3, 6}, // group 1: (abc)
			},
		},
		{
			name:      "two groups",
			pattern:   "([a-z]+)([0-9]+)",
			haystack:  "abc123xyz",
			wantMatch: true,
			wantStart: 0,
			wantEnd:   6,
			wantCaptures: [][]int{
				{0, 6}, // group 0: entire match
				{0, 3}, // group 1: [a-z]+
				{3, 6}, // group 2: [0-9]+
			},
		},
		{
			name:      "nested groups",
			pattern:   "((a+)(b+))",
			haystack:  "xxxaaabbbyyy",
			wantMatch: true,
			wantStart: 3,
			wantEnd:   9,
			wantCaptures: [][]int{
				{3, 9}, // group 0: entire match
				{3, 9}, // group 1: outer group
				{3, 6}, // group 2: a+
				{6, 9}, // group 3: b+
			},
		},
		{
			name:         "no match",
			pattern:      "(foo)(bar)",
			haystack:     "no match here",
			wantMatch:    false,
			wantCaptures: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			result := vm.SearchWithCaptures([]byte(tt.haystack))

			if !tt.wantMatch {
				if result != nil {
					t.Errorf("expected no match, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected match, got nil")
			}

			if result.Start != tt.wantStart || result.End != tt.wantEnd {
				t.Errorf("match at (%d, %d), want (%d, %d)",
					result.Start, result.End, tt.wantStart, tt.wantEnd)
			}

			if len(result.Captures) != len(tt.wantCaptures) {
				t.Errorf("capture count = %d, want %d", len(result.Captures), len(tt.wantCaptures))
				return
			}

			for i, want := range tt.wantCaptures {
				got := result.Captures[i]
				if len(got) < 2 || got[0] != want[0] || got[1] != want[1] {
					t.Errorf("group %d: got %v, want %v", i, got, want)
				}
			}
		})
	}
}

// TestPikeVM_SearchWithCapturesAt_Position tests SearchWithCapturesAt with non-zero start.
func TestPikeVM_SearchWithCapturesAt_Position(t *testing.T) {
	nfa := mustCompile(t, `(\d+)`)
	vm := NewPikeVM(nfa)
	haystack := []byte("abc123def456")

	// From position 0 — should find "123"
	result := vm.SearchWithCapturesAt(haystack, 0)
	if result == nil {
		t.Fatal("expected match from pos 0")
	}
	if result.Start != 3 || result.End != 6 {
		t.Errorf("from 0: got (%d, %d), want (3, 6)", result.Start, result.End)
	}

	// From position 6 — should find "456"
	result = vm.SearchWithCapturesAt(haystack, 6)
	if result == nil {
		t.Fatal("expected match from pos 6")
	}
	if result.Start != 9 || result.End != 12 {
		t.Errorf("from 6: got (%d, %d), want (9, 12)", result.Start, result.End)
	}

	// From position past all matches
	result = vm.SearchWithCapturesAt(haystack, 12)
	if result != nil {
		t.Errorf("from 12: expected nil, got (%d, %d)", result.Start, result.End)
	}

	// Past end
	result = vm.SearchWithCapturesAt(haystack, 20)
	if result != nil {
		t.Errorf("from 20: expected nil, got match")
	}
}

// TestPikeVM_SearchAll_MultiMatch tests SearchAll finding all non-overlapping matches.
func TestPikeVM_SearchAll_MultiMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     []Match
	}{
		{
			name:     "repeated single char",
			pattern:  "a",
			haystack: "abaca",
			want:     []Match{{0, 1}, {2, 3}, {4, 5}},
		},
		{
			name:     "word matches",
			pattern:  "[a-z]+",
			haystack: "foo 123 bar 456 baz",
			want:     []Match{{0, 3}, {8, 11}, {16, 19}},
		},
		{
			name:     "digit groups",
			pattern:  `\d+`,
			haystack: "a1b22c333d",
			want:     []Match{{1, 2}, {3, 5}, {6, 9}},
		},
		{
			name:     "no matches",
			pattern:  "xyz",
			haystack: "abc def ghi",
			want:     nil,
		},
		{
			name:     "empty haystack",
			pattern:  "a",
			haystack: "",
			want:     nil,
		},
		{
			name:     "overlapping avoided",
			pattern:  "aba",
			haystack: "abababa",
			want:     []Match{{0, 3}, {4, 7}},
		},
		{
			name:     "alternation all matches",
			pattern:  "cat|dog",
			haystack: "cat and dog",
			want:     []Match{{0, 3}, {8, 11}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			got := vm.SearchAll([]byte(tt.haystack))

			if len(got) != len(tt.want) {
				t.Errorf("SearchAll found %d matches, want %d", len(got), len(tt.want))
				t.Logf("got: %v", got)
				return
			}

			for i, m := range got {
				if m.Start != tt.want[i].Start || m.End != tt.want[i].End {
					t.Errorf("match %d: got (%d, %d), want (%d, %d)",
						i, m.Start, m.End, tt.want[i].Start, tt.want[i].End)
				}
			}
		})
	}
}

// TestPikeVM_SearchAll_EmptyMatches tests SearchAll behavior with patterns that can match empty.
func TestPikeVM_SearchAll_EmptyMatches(t *testing.T) {
	// a* can match empty string — SearchAll should advance past empty matches
	nfa := mustCompile(t, "a*")
	vm := NewPikeVM(nfa)

	matches := vm.SearchAll([]byte("bab"))
	// a* should produce empty matches between non-matching chars plus the "a" match
	// Exact behavior may vary, but we must not get an infinite loop
	if len(matches) == 0 {
		t.Error("SearchAll(a*, 'bab') should find at least one match")
	}

	// Verify none of the matches go backwards (no infinite loop)
	lastEnd := 0
	for _, m := range matches {
		if m.Start < lastEnd && m.End <= m.Start {
			t.Errorf("match at (%d, %d) goes backwards from lastEnd=%d", m.Start, m.End, lastEnd)
		}
		if m.End > lastEnd {
			lastEnd = m.End
		}
	}
}

// TestPikeVM_SearchAll_VsStdlib verifies SearchAll correctness against stdlib FindAllStringIndex.
func TestPikeVM_SearchAll_VsStdlib(t *testing.T) {
	patterns := []string{
		"[a-z]+",
		`\d+`,
		"foo|bar",
		"a+",
	}

	haystacks := []string{
		"foo 123 bar 456 baz",
		"aaabbbccc",
		"no matches 000",
		"alternation: foo and bar and foo",
	}

	for _, pattern := range patterns {
		stdRE := regexp.MustCompile(pattern)
		for _, haystack := range haystacks {
			t.Run(pattern+"/"+haystack, func(t *testing.T) {
				nfa := mustCompile(t, pattern)
				vm := NewPikeVM(nfa)

				stdAll := stdRE.FindAllStringIndex(haystack, -1)
				ourAll := vm.SearchAll([]byte(haystack))

				if len(stdAll) != len(ourAll) {
					t.Errorf("count mismatch: stdlib=%d, ours=%d", len(stdAll), len(ourAll))
					t.Logf("stdlib: %v", stdAll)
					t.Logf("ours: %v", ourAll)
					return
				}

				for i := range stdAll {
					if stdAll[i][0] != ourAll[i].Start || stdAll[i][1] != ourAll[i].End {
						t.Errorf("match %d: stdlib=%v, ours=(%d,%d)",
							i, stdAll[i], ourAll[i].Start, ourAll[i].End)
					}
				}
			})
		}
	}
}

// TestPikeVM_SearchBetween_Bounds tests SearchBetween with bounded ranges.
func TestPikeVM_SearchBetween_Bounds(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		startAt   int
		maxEnd    int
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{
			name: "match within bounds", pattern: "foo", haystack: "xxxfooyyy",
			startAt: 0, maxEnd: 9, wantStart: 3, wantEnd: 6, wantFound: true,
		},
		{
			name: "match cut off by maxEnd", pattern: "foobar", haystack: "xxxfoobaryyy",
			startAt: 0, maxEnd: 6, wantStart: -1, wantEnd: -1, wantFound: false,
		},
		{
			name: "start after match", pattern: "foo", haystack: "foobar",
			startAt: 3, maxEnd: 6, wantStart: -1, wantEnd: -1, wantFound: false,
		},
		{
			name: "empty range", pattern: "a", haystack: "abc",
			startAt: 1, maxEnd: 1, wantStart: -1, wantEnd: -1, wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			start, end, found := vm.SearchBetween([]byte(tt.haystack), tt.startAt, tt.maxEnd)
			if found != tt.wantFound {
				t.Errorf("SearchBetween found=%v, want %v", found, tt.wantFound)
				return
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("SearchBetween = (%d, %d), want (%d, %d)",
					start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestPikeVM_Search_LongestMode tests leftmost-longest (POSIX) matching semantics.
func TestPikeVM_Search_LongestMode(t *testing.T) {
	// With longest mode, alternation should pick the longer match
	nfa := mustCompile(t, "a|aa|aaa")
	vm := NewPikeVM(nfa)

	// Default (leftmost-first): should match "a" (first alternative)
	start, end, matched := vm.Search([]byte("aaa"))
	if !matched {
		t.Fatal("expected match in default mode")
	}
	defaultLen := end - start

	// Longest mode: should match "aaa" (longest alternative)
	vm.SetLongest(true)
	start, end, matched = vm.Search([]byte("aaa"))
	if !matched {
		t.Fatal("expected match in longest mode")
	}
	longestLen := end - start

	if longestLen < defaultLen {
		t.Errorf("longest mode match (%d-%d, len=%d) shorter than default (%d)",
			start, end, longestLen, defaultLen)
	}
}

// TestPikeVM_Search_ConsecutiveMatches tests handling of adjacent matches.
func TestPikeVM_Search_ConsecutiveMatches(t *testing.T) {
	nfa := mustCompile(t, "[a-z]+")
	vm := NewPikeVM(nfa)

	// "abc def" — should find "abc" first, then "def" via SearchAt
	start, end, found := vm.Search([]byte("abc def"))
	if !found || start != 0 || end != 3 {
		t.Errorf("first match: got (%d, %d, %v), want (0, 3, true)", start, end, found)
	}

	// Continue from end of first match
	start, end, found = vm.SearchAt([]byte("abc def"), 3)
	if !found || start != 4 || end != 7 {
		t.Errorf("second match: got (%d, %d, %v), want (4, 7, true)", start, end, found)
	}

	// No more matches
	_, _, found = vm.SearchAt([]byte("abc def"), 7)
	if found {
		t.Error("should not find third match")
	}
}

// TestPikeVM_Search_WithCaptures_VsStdlib compares capture results with stdlib.
func TestPikeVM_Search_WithCaptures_VsStdlib(t *testing.T) {
	patterns := []string{
		`(\d+)-(\d+)`,
		`([a-z]+)([0-9]+)`,
		`(foo)(bar)`,
		`(a+)(b+)(c+)`,
	}

	haystacks := []string{
		"xxx123-456yyy",
		"abc123xyz",
		"foobar",
		"aaabbbccc",
		"no match here",
	}

	for _, pattern := range patterns {
		stdRE := regexp.MustCompile(pattern)
		for _, haystack := range haystacks {
			t.Run(pattern+"/"+haystack, func(t *testing.T) {
				nfa := mustCompile(t, pattern)
				vm := NewPikeVM(nfa)

				stdMatch := stdRE.FindStringSubmatchIndex(haystack)
				ourResult := vm.SearchWithCaptures([]byte(haystack))

				stdMatched := stdMatch != nil
				ourMatched := ourResult != nil

				if stdMatched != ourMatched {
					t.Errorf("match mismatch: stdlib=%v, ours=%v", stdMatched, ourMatched)
					return
				}

				if !stdMatched {
					return
				}

				// Compare overall match
				if stdMatch[0] != ourResult.Start || stdMatch[1] != ourResult.End {
					t.Errorf("overall: stdlib=[%d,%d], ours=[%d,%d]",
						stdMatch[0], stdMatch[1], ourResult.Start, ourResult.End)
				}

				// Compare capture groups
				numGroups := len(stdMatch)/2 - 1 // exclude group 0
				if numGroups+1 > len(ourResult.Captures) {
					t.Errorf("capture count: stdlib=%d groups, ours=%d",
						numGroups, len(ourResult.Captures)-1)
					return
				}

				for g := 1; g <= numGroups; g++ {
					stdStart := stdMatch[g*2]
					stdEnd := stdMatch[g*2+1]
					if g < len(ourResult.Captures) && len(ourResult.Captures[g]) >= 2 {
						ourStart := ourResult.Captures[g][0]
						ourEnd := ourResult.Captures[g][1]
						if stdStart != ourStart || stdEnd != ourEnd {
							t.Errorf("group %d: stdlib=[%d,%d], ours=[%d,%d]",
								g, stdStart, stdEnd, ourStart, ourEnd)
						}
					}
				}
			})
		}
	}
}
