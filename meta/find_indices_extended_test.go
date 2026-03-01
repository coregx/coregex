package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestFindIndicesAllStrategies exercises FindIndices for patterns that trigger
// every known strategy, verifying correct (start, end) positions.
func TestFindIndicesAllStrategies(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// UseReverseSuffix
		{"rsuffix_match", `.*\.css`, "style.css", 0, 9, true},
		{"rsuffix_nomatch", `.*\.css`, "style.less", -1, -1, false},
		{"rsuffix_in_middle", `.*\.css`, "file.css is good", 0, 8, true},

		// UseReverseSuffixSet
		{"rssuffix_txt", `.*\.(txt|log|csv)`, "data.csv", 0, 8, true},
		{"rssuffix_log", `.*\.(txt|log|csv)`, "error.log", 0, 9, true},
		{"rssuffix_nomatch", `.*\.(txt|log|csv)`, "data.xml", -1, -1, false},

		// UseReverseInner
		{"rinner_match", `.*ERROR.*`, "line ERROR here", 0, 15, true},
		{"rinner_nomatch", `.*ERROR.*`, "all fine", -1, -1, false},

		// UseReverseAnchored
		{"ranchored_match", `test$`, "run test", 4, 8, true},
		{"ranchored_nomatch", `test$`, "test run", -1, -1, false},

		// UseCharClassSearcher
		{"ccs_alpha", `[a-z]+`, "ABC abc DEF", 4, 7, true},
		{"ccs_spaces", `\s+`, "abc   def", 3, 6, true},
		{"ccs_nomatch", `\d+`, "no-digits", -1, -1, false},

		// UseBoundedBacktracker
		{"bt_repeat_capture", `(\d)+`, "abc 42 def", 4, 6, true},
		{"bt_nomatch", `(\d)+`, "no digits", -1, -1, false},

		// UseCompositeSearcher
		{"composite_match", `[a-z]+[0-9]+`, "hello42world", 0, 7, true},
		{"composite_nomatch", `[a-z]+[0-9]+`, "hello world", -1, -1, false},

		// UseDigitPrefilter
		{"digit_version", `\d+\.\d+`, "version 2.0 release", 8, 11, true},
		{"digit_nomatch", `\d+\.\d+`, "no version", -1, -1, false},

		// Teddy
		{"teddy_match", "foo|bar|baz", "xyzbarqux", 3, 6, true},
		{"teddy_nomatch", "foo|bar|baz", "nothing", -1, -1, false},

		// UseAnchoredLiteral
		{"anchored_lit", `^/.*\.php$`, "/index.php", 0, 10, true},
		{"anchored_lit_nomatch", `^/.*\.php$`, "/index.html", -1, -1, false},

		// Start-anchored
		{"start_anchored", `^abc`, "abcdef", 0, 3, true},
		{"start_anchored_nomatch", `^abc`, "xabc", -1, -1, false},

		// Empty pattern matches at position 0
		{"empty_pattern", "", "test", 0, 0, true},
		{"empty_pattern_empty_input", "", "", 0, 0, true},

		// Empty haystack with non-empty pattern
		{"empty_haystack", `\w+`, "", -1, -1, false},

		// NFA direct
		{"nfa_literal", "xy", "abxycd", 2, 4, true},
		{"nfa_nomatch", "xy", "abcd", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			start, end, found := engine.FindIndices([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Errorf("FindIndices(%q) found=%v, want %v (strategy: %s)",
					tt.haystack, found, tt.wantFound, engine.Strategy())
				return
			}
			if found {
				if start != tt.wantStart || end != tt.wantEnd {
					t.Errorf("FindIndices(%q) = (%d,%d), want (%d,%d) (strategy: %s)",
						tt.haystack, start, end, tt.wantStart, tt.wantEnd, engine.Strategy())
				}
			}
		})
	}
}

// TestFindIndicesAtBoundaryConditions tests FindIndicesAt with edge-case positions.
func TestFindIndicesAtBoundaryConditions(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		at        int
		wantFound bool
	}{
		// at == 0 should work normally
		{"at_zero", `\w+`, "hello", 0, true},

		// at == len(haystack) - for patterns that can match empty
		{"at_end_empty_pattern", "", "hello", 5, true},

		// at past match start should skip it
		{"at_past_first_match", `\w+`, "abc def", 4, true},

		// at at very end with non-empty pattern
		{"at_end_nonmatch", `\w+`, "abc", 3, false},

		// Anchored pattern at non-zero position
		{"anchored_at_1", `^hello`, "hello", 1, false},

		// at == 0 for anchored
		{"anchored_at_0", `^hello`, "hello world", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			_, _, found := engine.FindIndicesAt([]byte(tt.haystack), tt.at)
			if found != tt.wantFound {
				t.Errorf("FindIndicesAt(%q, %d) found=%v, want %v (strategy: %s)",
					tt.haystack, tt.at, found, tt.wantFound, engine.Strategy())
			}
		})
	}
}

// TestFindIndicesAtProgressiveSearch uses FindIndicesAt to progressively find
// all matches, verifying each step returns the correct next match.
func TestFindIndicesAtProgressiveSearch(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantTexts []string
	}{
		{
			"words_in_sentence",
			`\w+`,
			"the quick brown fox",
			[]string{"the", "quick", "brown", "fox"},
		},
		{
			"digits_separated",
			`\d+`,
			"a1 b22 c333 d4444",
			[]string{"1", "22", "333", "4444"},
		},
		{
			"literals",
			"ab",
			"abcabcab",
			[]string{"ab", "ab", "ab"},
		},
		{
			"alternation",
			"cat|dog",
			"the cat and the dog",
			[]string{"cat", "dog"},
		},
		{
			"composite_alpha_digit",
			`[a-z]+[0-9]+`,
			"a1 bb22 ccc333",
			[]string{"a1", "bb22", "ccc333"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			h := []byte(tt.haystack)
			var got []string
			at := 0
			for {
				start, end, found := engine.FindIndicesAt(h, at)
				if !found {
					break
				}
				got = append(got, string(h[start:end]))
				if end > at {
					at = end
				} else {
					at++
				}
				if at > len(h) {
					break
				}
			}

			if len(got) != len(tt.wantTexts) {
				t.Errorf("got %d matches %v, want %d matches %v",
					len(got), got, len(tt.wantTexts), tt.wantTexts)
				return
			}
			for i := range got {
				if got[i] != tt.wantTexts[i] {
					t.Errorf("match[%d] = %q, want %q", i, got[i], tt.wantTexts[i])
				}
			}
		})
	}
}

// TestFindIndicesVsStdlib compares FindIndices results against Go stdlib regexp.
func TestFindIndicesVsStdlib(t *testing.T) {
	patterns := []string{
		`\d+`,
		`\w+`,
		`[a-z]+`,
		"hello",
		`\d+\.\d+`,
		`[a-zA-Z]+[0-9]+`,
	}

	haystacks := []string{
		"abc 123 def 456",
		"hello world test",
		"version 1.23 here",
		"abc123 def456",
		"no match here!!!",
		"",
		"   ",
	}

	for _, pattern := range patterns {
		for _, haystack := range haystacks {
			name := pattern + "/" + haystack
			if len(name) > 40 {
				name = name[:40]
			}
			t.Run(name, func(t *testing.T) {
				engine, err := Compile(pattern)
				if err != nil {
					t.Fatal(err)
				}
				re := regexp.MustCompile(pattern)

				h := []byte(haystack)
				start, end, found := engine.FindIndices(h)
				loc := re.FindIndex(h)

				switch {
				case found && loc == nil:
					t.Errorf("coregex found match at (%d,%d) but stdlib found none", start, end)
				case !found && loc != nil:
					t.Errorf("coregex found no match but stdlib found at (%d,%d)", loc[0], loc[1])
				case found && loc != nil && (start != loc[0] || end != loc[1]):
					t.Errorf("coregex (%d,%d) != stdlib (%d,%d)",
						start, end, loc[0], loc[1])
				}
			})
		}
	}
}

// TestFindIndicesLargeInputAllStrategies tests FindIndices on large inputs
// for patterns triggering different strategies.
func TestFindIndicesLargeInputAllStrategies(t *testing.T) {
	const size = 100 * 1024 // 100KB

	tests := []struct {
		name       string
		pattern    string
		buildInput func() []byte
		wantFound  bool
	}{
		{
			"charclass_100kb",
			`\w+`,
			func() []byte { return []byte(strings.Repeat("   ", size/3) + "word") },
			true,
		},
		{
			"suffix_100kb",
			`.*\.log`,
			func() []byte { return []byte(strings.Repeat("x", size) + ".log") },
			true,
		},
		{
			"inner_100kb",
			`.*CRITICAL.*`,
			func() []byte { return []byte(strings.Repeat("x", size) + "CRITICAL" + strings.Repeat("y", 100)) },
			true,
		},
		{
			"composite_100kb",
			`[a-z]+[0-9]+`,
			func() []byte { return []byte(strings.Repeat("   ", size/3) + "abc123") },
			true,
		},
		{
			"digit_prefilter_100kb",
			`\d+\.\d+\.\d+`,
			func() []byte { return []byte(strings.Repeat("abc ", size/4) + "1.2.3") },
			true,
		},
		{
			"teddy_100kb",
			"apple|banana|cherry",
			func() []byte { return []byte(strings.Repeat("x", size) + "banana") },
			true,
		},
		{
			"no_match_100kb",
			"needle",
			func() []byte { return []byte(strings.Repeat("haystack ", size/9)) },
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			input := tt.buildInput()
			_, _, found := engine.FindIndices(input)
			if found != tt.wantFound {
				t.Errorf("FindIndices on %dB input: found=%v, want %v (strategy: %s)",
					len(input), found, tt.wantFound, engine.Strategy())
			}
		})
	}
}

// TestFindIndicesAtAnchoredPatterns verifies that anchored patterns correctly
// reject non-zero start positions.
func TestFindIndicesAtAnchoredPatterns(t *testing.T) {
	anchoredPatterns := []string{
		`^hello`,
		`^\w+`,
	}

	haystack := []byte("hello world 123")

	for _, pattern := range anchoredPatterns {
		t.Run(pattern, func(t *testing.T) {
			engine, err := Compile(pattern)
			if err != nil {
				t.Fatal(err)
			}

			// At position 0 should work
			_, _, found := engine.FindIndicesAt(haystack, 0)
			if !found {
				t.Errorf("FindIndicesAt(%q, 0) should find match", pattern)
			}

			// At non-zero position should not match
			_, _, found = engine.FindIndicesAt(haystack, 1)
			if found {
				t.Errorf("FindIndicesAt(%q, 1) should not find match for anchored pattern", pattern)
			}
		})
	}
}

// TestFindIndicesConsistencyFindVsIndices verifies Find and FindIndices return
// identical positions for a large set of patterns and inputs.
func TestFindIndicesConsistencyFindVsIndices(t *testing.T) {
	patterns := []string{
		"a",
		`\d+`,
		`\w+`,
		`[a-z]+`,
		"foo|bar|baz",
		`^hello`,
		`world$`,
		`.*\.txt`,
		`.*keyword.*`,
		`[a-zA-Z]+[0-9]+`,
		`(\w)+`,
		`\d+\.\d+`,
	}

	haystacks := []string{
		"hello world",
		"abc 123 def",
		"foo bar baz",
		"document.txt",
		"abc123",
		"has keyword here",
		"",
		"   ",
		"version 1.23",
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", pattern, err)
		}

		for _, haystack := range haystacks {
			h := []byte(haystack)
			idxStart, idxEnd, idxFound := engine.FindIndices(h)
			match := engine.Find(h)
			findFound := match != nil

			if idxFound != findFound {
				t.Errorf("pattern %q, input %q: FindIndices found=%v, Find found=%v (strategy: %s)",
					pattern, haystack, idxFound, findFound, engine.Strategy())
				continue
			}
			if findFound && (idxStart != match.Start() || idxEnd != match.End()) {
				t.Errorf("pattern %q, input %q: FindIndices=(%d,%d), Find=(%d,%d) (strategy: %s)",
					pattern, haystack, idxStart, idxEnd, match.Start(), match.End(), engine.Strategy())
			}
		}
	}
}
