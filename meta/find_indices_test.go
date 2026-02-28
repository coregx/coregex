package meta

import (
	"strings"
	"testing"
)

// TestFindIndicesStrategyDispatch tests FindIndices through patterns that trigger
// different execution strategies. This exercises the zero-allocation path.
func TestFindIndicesStrategyDispatch(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// NFA strategy (simple patterns)
		{"nfa_literal", "a", "xax", 1, 2, true},
		{"nfa_no_match", "z", "abc", 0, 0, false},

		// CharClassSearcher
		{"charclass_word", `\w+`, "  hello  ", 2, 7, true},
		{"charclass_digit", `\d+`, "abc123def", 3, 6, true},
		{"charclass_no_match", `\d+`, "no digits", 0, 0, false},

		// BoundedBacktracker (char class with capture)
		{"bt_capture", `(\w)+`, "abc", 0, 3, true},
		{"bt_no_match", `(\d)+`, "abc", 0, 0, false},

		// ReverseSuffix
		{"rsuffix_txt", `.*\.txt`, "readme.txt", 0, 10, true},
		{"rsuffix_no_match", `.*\.txt`, "readme.pdf", 0, 0, false},

		// ReverseInner
		{"rinner_keyword", `.*keyword.*`, "before keyword after", 0, 20, true},
		{"rinner_no_match", `.*keyword.*`, "nothing", 0, 0, false},

		// End-anchored (ReverseAnchored)
		{"ranchored_end", `hello$`, "say hello", 4, 9, true},
		{"ranchored_no_match", `hello$`, "hello world", 0, 0, false},

		// Alternation (may trigger Teddy or NFA)
		{"alternation_first", "foo|bar|baz", "  foo  ", 2, 5, true},
		{"alternation_second", "foo|bar|baz", "  bar  ", 2, 5, true},
		{"alternation_no_match", "foo|bar|baz", "  qux  ", 0, 0, false},

		// Start-anchored
		{"start_anchored", `^hello`, "hello world", 0, 5, true},
		{"start_anchored_no_match", `^hello`, "say hello", 0, 0, false},

		// Composite pattern
		{"composite_alpha_digit", `[a-zA-Z]+[0-9]+`, "abc123", 0, 6, true},
		{"composite_no_match", `[a-zA-Z]+[0-9]+`, "abc", 0, 0, false},

		// Empty pattern
		{"empty_pattern", "", "test", 0, 0, true},

		// Empty haystack
		{"empty_haystack", `\w+`, "", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			start, end, found := engine.FindIndices([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Errorf("FindIndices() found = %v, want %v (strategy: %s)",
					found, tt.wantFound, engine.Strategy())
				return
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("FindIndices() = (%d, %d), want (%d, %d) (strategy: %s)",
					start, end, tt.wantStart, tt.wantEnd, engine.Strategy())
			}
		})
	}
}

// TestFindIndicesAtStrategyDispatch tests FindIndicesAt with non-zero offset,
// exercising the *At dispatch variants for each strategy.
func TestFindIndicesAtStrategyDispatch(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		at        int
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// CharClassSearcher At
		{"charclass_at_0", `\w+`, "abc def", 0, 0, 3, true},
		{"charclass_at_4", `\w+`, "abc def", 4, 4, 7, true},
		{"charclass_at_end", `\w+`, "abc", 3, 0, 0, false},

		// BoundedBacktracker At
		{"bt_at_0", `(\d)+`, "12 34", 0, 0, 2, true},
		{"bt_at_3", `(\d)+`, "12 34", 3, 3, 5, true},

		// NFA At
		{"nfa_at_0", "x", "axbxc", 0, 1, 2, true},
		{"nfa_at_2", "x", "axbxc", 2, 3, 4, true},
		{"nfa_at_4", "x", "axbxc", 4, 0, 0, false},

		// Reverse suffix At
		{"rsuffix_at_0", `.*\.txt`, "a.txt b.txt", 0, 0, 5, true},

		// Composite At
		{"composite_at_0", `[a-z]+[0-9]+`, "ab1 cd2", 0, 0, 3, true},
		{"composite_at_4", `[a-z]+[0-9]+`, "ab1 cd2", 4, 4, 7, true},

		// Anchored pattern at non-zero offset (should not match)
		{"anchored_at_nonzero", `^hello`, "hello world", 1, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			start, end, found := engine.FindIndicesAt([]byte(tt.haystack), tt.at)
			if found != tt.wantFound {
				t.Errorf("FindIndicesAt(%d) found = %v, want %v (strategy: %s)",
					tt.at, found, tt.wantFound, engine.Strategy())
				return
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("FindIndicesAt(%d) = (%d, %d), want (%d, %d) (strategy: %s)",
					tt.at, start, end, tt.wantStart, tt.wantEnd, engine.Strategy())
			}
		})
	}
}

// TestFindIndicesConsistentWithFind verifies FindIndices and Find return the same positions.
func TestFindIndicesConsistentWithFind(t *testing.T) {
	patterns := []string{
		"hello",
		`\d+`,
		`\w+`,
		`[a-z]+`,
		"foo|bar|baz",
		`^hello`,
		`world$`,
		`.*\.txt`,
		`[a-zA-Z]+[0-9]+`,
	}

	haystacks := []string{
		"hello world",
		"123 abc 456",
		"foo bar baz",
		"document.txt",
		"abc123",
		"",
		"   ",
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", pattern, err)
		}

		for _, haystack := range haystacks {
			h := []byte(haystack)

			// FindIndices
			idxStart, idxEnd, idxFound := engine.FindIndices(h)

			// Find
			match := engine.Find(h)
			findFound := match != nil

			if idxFound != findFound {
				t.Errorf("pattern %q, haystack %q: FindIndices found=%v, Find found=%v",
					pattern, haystack, idxFound, findFound)
				continue
			}

			if findFound {
				if idxStart != match.Start() || idxEnd != match.End() {
					t.Errorf("pattern %q, haystack %q: FindIndices=(%d,%d), Find=(%d,%d)",
						pattern, haystack, idxStart, idxEnd, match.Start(), match.End())
				}
			}
		}
	}
}

// TestFindIndicesAtIteration tests using FindIndicesAt in a loop to find all matches.
func TestFindIndicesAtIteration(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantCount int
		wantTexts []string
	}{
		{
			name:      "find all words",
			pattern:   `\w+`,
			haystack:  "hello world foo",
			wantCount: 3,
			wantTexts: []string{"hello", "world", "foo"},
		},
		{
			name:      "find all digits",
			pattern:   `\d+`,
			haystack:  "a1 b22 c333",
			wantCount: 3,
			wantTexts: []string{"1", "22", "333"},
		},
		{
			name:      "find all literals",
			pattern:   "ab",
			haystack:  "abcabc",
			wantCount: 2,
			wantTexts: []string{"ab", "ab"},
		},
		{
			name:      "no matches",
			pattern:   `\d+`,
			haystack:  "no digits",
			wantCount: 0,
			wantTexts: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			h := []byte(tt.haystack)
			var matches []string
			at := 0
			for {
				start, end, found := engine.FindIndicesAt(h, at)
				if !found {
					break
				}
				matches = append(matches, string(h[start:end]))
				if end > at {
					at = end
				} else {
					at++
				}
			}

			if len(matches) != tt.wantCount {
				t.Errorf("found %d matches, want %d: %v", len(matches), tt.wantCount, matches)
				return
			}

			for i, want := range tt.wantTexts {
				if matches[i] != want {
					t.Errorf("match[%d] = %q, want %q", i, matches[i], want)
				}
			}
		})
	}
}

// TestFindIndicesLargeInput tests FindIndices on large inputs for various strategies.
func TestFindIndicesLargeInput(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		buildInput func() string
		wantFound  bool
	}{
		{
			name:    "literal in 100KB",
			pattern: "needle",
			buildInput: func() string {
				return strings.Repeat("x", 100*1024) + "needle"
			},
			wantFound: true,
		},
		{
			name:    "digit in 100KB",
			pattern: `\d+`,
			buildInput: func() string {
				return strings.Repeat("abc ", 25*1024) + "42"
			},
			wantFound: true,
		},
		{
			name:    "suffix in 100KB",
			pattern: `.*\.log`,
			buildInput: func() string {
				return strings.Repeat("x", 100*1024) + ".log"
			},
			wantFound: true,
		},
		{
			name:    "no match in 100KB",
			pattern: "needle",
			buildInput: func() string {
				return strings.Repeat("haystack ", 12*1024)
			},
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			input := []byte(tt.buildInput())
			_, _, found := engine.FindIndices(input)
			if found != tt.wantFound {
				t.Errorf("FindIndices() found = %v, want %v (strategy: %s)",
					found, tt.wantFound, engine.Strategy())
			}
		})
	}
}

// TestFindAllIndicesStreamingReuse tests that passing a pre-allocated results slice works.
func TestFindAllIndicesStreamingReuse(t *testing.T) {
	engine, err := Compile(`\w+`)
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("abc def ghi")

	// First call with nil (fresh allocation)
	results := engine.FindAllIndicesStreaming(haystack, -1, nil)
	if len(results) != 3 {
		t.Fatalf("first call: got %d results, want 3", len(results))
	}

	// Second call reusing the slice
	results2 := engine.FindAllIndicesStreaming(haystack, -1, results)
	if len(results2) != 3 {
		t.Fatalf("reuse call: got %d results, want 3", len(results2))
	}

	// Verify results are correct
	expected := [][2]int{{0, 3}, {4, 7}, {8, 11}}
	for i, want := range expected {
		if results2[i] != want {
			t.Errorf("result[%d] = %v, want %v", i, results2[i], want)
		}
	}
}

// TestCountConsistentWithFindAll tests that Count and FindAllIndicesStreaming agree.
func TestCountConsistentWithFindAll(t *testing.T) {
	patterns := []string{`\w+`, `\d+`, `[a-z]+`, "foo|bar"}
	haystacks := []string{
		"hello world 123",
		"foo bar baz qux",
		"a1 b2 c3",
		"",
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}

		for _, haystack := range haystacks {
			h := []byte(haystack)
			count := engine.Count(h, -1)
			all := engine.FindAllIndicesStreaming(h, -1, nil)

			if count != len(all) {
				t.Errorf("pattern %q, haystack %q: Count=%d, FindAll=%d",
					pattern, haystack, count, len(all))
			}
		}
	}
}
