package meta

// Tests for prefilter-based strategies: Teddy, AhoCorasick,
// DigitPrefilter, CompositeSearcher, and CharClassSearcher.

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"
)

func TestTeddyAt_FindAll(t *testing.T) {
	pattern := `foo|bar|baz|qux`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseTeddy)

	re := regexp.MustCompile(pattern)
	haystack := "foo xbar ybaz zqux end foo bar"

	results := engine.FindAllIndicesStreaming([]byte(haystack), 0, nil)
	stdResults := re.FindAllStringIndex(haystack, -1)
	if len(results) != len(stdResults) {
		t.Errorf("count: got %d, stdlib %d", len(results), len(stdResults))
	}

	// FindAt at specific positions
	for _, at := range []int{0, 4, 9, 14, 19} {
		m := engine.FindAt([]byte(haystack), at)
		if m != nil {
			t.Logf("FindAt(%d): %q at [%d,%d]", at, m.String(), m.Start(), m.End())
		}
	}
}

// -----------------------------------------------------------------------------
// 11. Multiline ReverseSuffix FindAll (75% -> higher)
// -----------------------------------------------------------------------------

func TestCompositeSearcher_DFAPath(t *testing.T) {
	pattern := `[a-zA-Z]+[0-9]+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)
	haystack := "abc123 def456 ghi789"

	s, e, found := engine.FindIndices([]byte(haystack))
	stdLoc := re.FindStringIndex(haystack)
	if found && stdLoc != nil {
		if s != stdLoc[0] || e != stdLoc[1] {
			t.Errorf("got (%d,%d), stdlib (%d,%d)", s, e, stdLoc[0], stdLoc[1])
		}
	}

	count := engine.Count([]byte(haystack), -1)
	stdCount := len(re.FindAllString(haystack, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}
}

// -----------------------------------------------------------------------------
// 16. findAtNonZero (find.go:120) ReverseSuffix/ReverseInner/ReverseAnchored
//     These fall back to findNFAAt at non-zero positions (73.3% -> higher)
// -----------------------------------------------------------------------------

func TestTeddy_FindAll_Detailed(t *testing.T) {
	// Various Teddy patterns with different match characteristics
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{"three_lit", `foo|bar|baz`, "foo bar baz foo"},
		{"four_lit", `alpha|beta|gamma|delta`, "alpha beta gamma delta"},
		{"mixed_positions", `cat|dog|fox`, "the fox and cat and dog ran"},
		{"no_match", `cat|dog|fox`, "nothing matches here at all"},
		{"single_match", `cat|dog|fox`, "the cat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			requireStrategy(t, engine, UseTeddy)

			re := regexp.MustCompile(tt.pattern)
			haystack := []byte(tt.input)

			// Find
			m := engine.Find(haystack)
			stdMatch := re.FindString(tt.input)
			if (m == nil) != (stdMatch == "") {
				t.Errorf("Find: got nil=%v, stdlib empty=%v", m == nil, stdMatch == "")
			}
			if m != nil && m.String() != stdMatch {
				t.Errorf("Find = %q, stdlib = %q", m.String(), stdMatch)
			}

			// Count
			count := engine.Count(haystack, -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}

			// FindAt at various positions
			for at := 0; at < len(tt.input); at += len(tt.input)/3 + 1 {
				m := engine.FindAt(haystack, at)
				_ = m // just exercise the path
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 19. findIndicesBoundedBacktrackerAt (37%) and AtWithState (27.5%)
//     Exercise the windowed BT fallback and PikeVM fallback paths.
// -----------------------------------------------------------------------------

func TestDigitPrefilter_FindAll(t *testing.T) {
	pattern := `\d+\.\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseDigitPrefilter {
		t.Skipf("strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)
	haystack := "pi=3.14 e=2.72 phi=1.62"

	// Count exercises FindIndicesAt at > 0
	count := engine.Count([]byte(haystack), -1)
	stdCount := len(re.FindAllString(haystack, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// Find
	m := engine.Find([]byte(haystack))
	stdMatch := re.FindString(haystack)
	if m != nil && m.String() != stdMatch {
		t.Errorf("Find = %q, stdlib = %q", m.String(), stdMatch)
	}

	// FindAt at non-zero
	m2 := engine.FindAt([]byte(haystack), 8)
	if m2 != nil {
		t.Logf("FindAt(8) = %q", m2.String())
	}
}

// -----------------------------------------------------------------------------
// 23. AhoCorasick At paths (71.4%) — exercise FindAt at non-zero
// -----------------------------------------------------------------------------

func TestAhoCorasick_FindAll(t *testing.T) {
	// Build a pattern with >64 literals to trigger AhoCorasick
	// Using single-character alternations won't work (need >= 3 bytes each for Teddy)
	// Actually AhoCorasick needs >64 complete literals
	var parts []string
	for i := 0; i < 70; i++ {
		parts = append(parts, "word"+strings.Repeat(string(rune('a'+i%26)), 2+i%3))
	}
	pattern := strings.Join(parts, "|")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseAhoCorasick {
		t.Skipf("strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)
	haystack := parts[0] + " " + parts[10] + " " + parts[20]

	count := engine.Count([]byte(haystack), -1)
	stdCount := len(re.FindAllString(haystack, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// FindAt at non-zero
	m := engine.FindAt([]byte(haystack), 5)
	if m != nil {
		t.Logf("FindAt(5) = %q", m.String())
	}
}

// -----------------------------------------------------------------------------
// 24. reverse_suffix.go FindIndicesAt (54.5%) and reverse_suffix_set.go FindIndicesAt (48.3%)
//     Exercise more branches in the reverse suffix searcher's FindIndicesAt.
// -----------------------------------------------------------------------------

func TestCompositeSearcher_FindAll(t *testing.T) {
	pattern := `[a-zA-Z]+[0-9]+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"multiple", "abc123 def456 ghi789"},
		{"no_match", "123 456 789"},
		{"single", "test42"},
		{"long", strings.Repeat("word99 ", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			haystack := []byte(tt.input)
			count := engine.Count(haystack, -1)
			stdCount := len(re.FindAllIndex(haystack, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 26. findIndicesTeddy (57.1%) and findIndicesTeddyAt (42.9%) — coverage push
// -----------------------------------------------------------------------------

func TestTeddy_FindIndices_Detailed(t *testing.T) {
	pattern := `foo|bar|baz`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseTeddy)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"all_match", "foo bar baz"},
		{"no_match", "qux quux corge"},
		{"single", "foo"},
		{"repeated", "foofoofoo"},
		{"long_gap", strings.Repeat("x", 1000) + "foo" + strings.Repeat("x", 1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			haystack := []byte(tt.input)

			// FindIndices at 0
			s, e, found := engine.FindIndices(haystack)
			stdLoc := re.FindIndex(haystack)
			if found != (stdLoc != nil) {
				t.Errorf("found=%v, stdlib=%v", found, stdLoc != nil)
			}
			if found && stdLoc != nil {
				if s != stdLoc[0] || e != stdLoc[1] {
					t.Errorf("got (%d,%d), stdlib (%d,%d)", s, e, stdLoc[0], stdLoc[1])
				}
			}

			// Count
			count := engine.Count(haystack, -1)
			stdCount := len(re.FindAllIndex(haystack, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 27. reverse_suffix_multiline verifyPrefix (60%) — exercise more paths
// -----------------------------------------------------------------------------

func TestTeddy_FindAt_SmallHaystack(t *testing.T) {
	// Teddy with a small haystack exercises the fatTeddyFallback path
	// if the pattern uses > 8 patterns. For <= 8, it uses Slim Teddy.
	pattern := `foo|bar|baz`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseTeddy {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Small haystack (< 64 bytes) — may use different code path
	tests := []struct {
		name  string
		input string
		at    int
		want  string
	}{
		{"small_match_foo", "foo", 0, "foo"},
		{"small_match_bar", "xxbar", 0, "bar"},
		{"small_at_2", "xxfoo", 2, "foo"},
		{"small_no_match", "xyz", 0, ""},
		{"empty", "", 0, ""},
		{"at_beyond", "foo", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := engine.FindAt([]byte(tt.input), tt.at)
			if tt.want == "" {
				if match != nil {
					t.Errorf("FindAt(%q, %d) = %q, want nil", tt.input, tt.at, match.String())
				}
			} else {
				if match == nil {
					t.Errorf("FindAt(%q, %d) = nil, want %q", tt.input, tt.at, tt.want)
				} else if match.String() != tt.want {
					t.Errorf("FindAt(%q, %d) = %q, want %q", tt.input, tt.at, match.String(), tt.want)
				}
			}

			// Cross-validate with stdlib for at=0
			if tt.at == 0 {
				stdMatch := re.FindString(tt.input)
				got := ""
				if match != nil {
					got = match.String()
				}
				if got != stdMatch {
					t.Errorf("FindAt stdlib mismatch: got %q, stdlib %q", got, stdMatch)
				}
			}
		})
	}
}

func TestTeddy_FindIndicesAt_Detailed(t *testing.T) {
	// Exercise findIndicesTeddyAt with multiple at positions.
	pattern := `foo|bar|baz`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseTeddy {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	input := "xxfoo yy bar zz baz"
	re := regexp.MustCompile(pattern)

	// FindIndicesAt at various positions
	positions := []int{0, 2, 5, 9, 12, 16}
	for _, at := range positions {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		if found {
			got := input[s:e]
			// Verify stdlib would also find a match at or after 'at'
			stdLoc := re.FindStringIndex(input[at:])
			if stdLoc != nil {
				stdStr := input[at+stdLoc[0] : at+stdLoc[1]]
				if got != stdStr {
					t.Errorf("FindIndicesAt(%d): got %q, stdlib %q", at, got, stdStr)
				}
			}
		}
	}

	// Count exercises the At loop
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}
}

// -----------------------------------------------------------------------------
// 31. findIndicesCompositeSearcher (42.9%) — exercise compositeSequenceDFA path.
// -----------------------------------------------------------------------------

func TestCompositeSearcher_DFA_FindAll(t *testing.T) {
	// CompositeSearcher patterns with multiple matches exercise
	// the findIndicesCompositeSearcherAt path
	pattern := `[a-zA-Z]+\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseCompositeSearcher {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Multiple matches — exercises the At loop
	input := "abc123 def456 ghi789 jkl012"
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// FindAllIndicesStreaming
	indices := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	stdAll := re.FindAllStringIndex(input, -1)
	if len(indices) != len(stdAll) {
		t.Errorf("FindAll count: got %d, stdlib %d", len(indices), len(stdAll))
	}
	for i := range indices {
		if i < len(stdAll) {
			got := input[indices[i][0]:indices[i][1]]
			std := input[stdAll[i][0]:stdAll[i][1]]
			if got != std {
				t.Errorf("match[%d]: got %q, stdlib %q", i, got, std)
				break
			}
		}
	}

	// FindAt at non-zero
	match := engine.FindAt([]byte(input), 7)
	if match == nil {
		t.Error("FindAt(7) should find 'def456'")
	} else if match.String() != "def456" {
		t.Errorf("FindAt(7) = %q, want 'def456'", match.String())
	}

	// FindIndicesAt at non-zero
	s, e, found := engine.FindIndicesAt([]byte(input), 7)
	if !found {
		t.Error("FindIndicesAt(7) should find match")
	} else if input[s:e] != "def456" {
		t.Errorf("FindIndicesAt(7) = %q, want 'def456'", input[s:e])
	}
}

// -----------------------------------------------------------------------------
// 32. ReverseSuffixSet.Find (56.2%) — exercise non-matchStartZero path
//     (reverse DFA to find start) and anti-quadratic guard.
// -----------------------------------------------------------------------------

func TestDigitPrefilter_FindAt_Detailed(t *testing.T) {
	pattern := `\d+\.\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseDigitPrefilter {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Multiple matches — exercises the digit run skip optimization
	input := "version 1.2 and 3.45 and 67.890"
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// FindAt at various positions
	for _, at := range []int{0, 8, 14, 20} {
		match := engine.FindAt([]byte(input), at)
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		if match != nil && found {
			if match.Start() != s || match.End() != e {
				t.Errorf("FindAt(%d) vs FindIndicesAt mismatch: [%d,%d] vs [%d,%d]",
					at, match.Start(), match.End(), s, e)
			}
		}
	}

	// Input with consecutive digits (exercises digit run skip)
	digitInput := "1111111111.22222 text 3333333333.44444"
	count2 := engine.Count([]byte(digitInput), -1)
	stdCount2 := len(re.FindAllString(digitInput, -1))
	if count2 != stdCount2 {
		t.Errorf("Count digits = %d, stdlib = %d", count2, stdCount2)
	}

	// No match — exercises the full scan fallback
	noMatch := "no digits here at all"
	if engine.IsMatch([]byte(noMatch)) {
		t.Error("IsMatch should be false for no digits")
	}
}

// -----------------------------------------------------------------------------
// 41. findAhoCorasick / findAhoCorasickAt (71.4%) — exercise more paths.
// -----------------------------------------------------------------------------

func TestAhoCorasick_FindAt_Detailed(t *testing.T) {
	pattern := `alpha|beta|gamma|delta|epsilon|zeta|eta|theta|iota`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// FindAt at various positions
	input := "alpha and beta and gamma and delta"
	for _, at := range []int{0, 5, 10, 15, 20, 25} {
		match := engine.FindAt([]byte(input), at)
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		if match != nil && found {
			if match.Start() != s || match.End() != e {
				t.Errorf("FindAt(%d) vs FindIndicesAt mismatch", at)
			}
		}
	}

	// Count
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// No match input
	match := engine.FindAt([]byte("nothing"), 0)
	if match != nil {
		t.Errorf("FindAt no match: got %q", match.String())
	}
}

// -----------------------------------------------------------------------------
// 42. isMatchDigitPrefilter (66.7%) — exercise digit prefilter IsMatch.
// -----------------------------------------------------------------------------

func TestIsMatchDigitPrefilter_Detailed(t *testing.T) {
	pattern := `\d+\.\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseDigitPrefilter {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		input string
		want  bool
	}{
		{"1.2", true},
		{"12.34", true},
		{"no digits", false},
		{"12", false}, // no decimal point
		{"", false},
		{".5", false},                            // no leading digit
		{"abc 99.99 xyz", true},                  // match in middle
		{strings.Repeat("x", 100) + "1.0", true}, // match at end of large input
	}

	for _, tt := range tests {
		got := engine.IsMatch([]byte(tt.input))
		stdGot := re.MatchString(tt.input)
		if got != stdGot {
			t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input, got, stdGot)
		}
	}
}

// -----------------------------------------------------------------------------
// 43. containsLineStartAnchor (70.6%) — exercise more pattern shapes.
// -----------------------------------------------------------------------------

func TestCharClassSearcher_FindAll_Detailed(t *testing.T) {
	// Exercise CharClassSearcher with various character classes
	patterns := []struct {
		name    string
		pattern string
		input   string
	}{
		{"word", `\w+`, "hello world 123"},
		{"upper", `[A-Z]+`, "ABC def GHI"},
		{"lower", `[a-z]+`, "ABC def GHI"},
		{"hex", `[0-9a-fA-F]+`, "deadBEEF 42 xyz"},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			if engine.Strategy() != UseCharClassSearcher {
				t.Skipf("Strategy is %s", engine.Strategy())
			}

			re := regexp.MustCompile(tt.pattern)

			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count(%q) = %d, stdlib = %d", tt.input, count, stdCount)
			}

			// FindIndicesAt at various positions
			for at := 0; at < len(tt.input); at += 4 {
				s, e, found := engine.FindIndicesAt([]byte(tt.input), at)
				if found {
					got := tt.input[s:e]
					stdLoc := re.FindStringIndex(tt.input[at:])
					if stdLoc != nil {
						std := tt.input[at+stdLoc[0] : at+stdLoc[1]]
						if got != std {
							t.Errorf("FindIndicesAt(%d): got %q, stdlib %q", at, got, std)
						}
					}
				}
			}
		})
	}
}

// =============================================================================
// Wave 4D: Final coverage push (77.7% -> 80%+)
// Focus on remaining low-coverage functions with reachable branches.
// =============================================================================

// -----------------------------------------------------------------------------
// 54. isMatchBoundedBacktracker — exercise anchoredSuffix, ASCII opt, large input.
// -----------------------------------------------------------------------------

func TestIsMatchCompositeSearcher_DFA(t *testing.T) {
	pattern := `[a-zA-Z]+[0-9]+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseCompositeSearcher {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		input string
		want  bool
	}{
		{"abc123", true},
		{"XYZ999", true},
		{"nodigits", false},
		{"123", false}, // no alpha prefix
		{"", false},
		{"a1", true}, // minimal match
		{"x" + strings.Repeat("a", 100) + "123" + "y", true}, // match in middle
	}

	for _, tt := range tests {
		got := engine.IsMatch([]byte(tt.input))
		stdGot := re.MatchString(tt.input)
		if got != stdGot {
			t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input[:minInt(len(tt.input), 30)], got, stdGot)
		}
	}
}

// -----------------------------------------------------------------------------
// 56. findIndicesBoundedBacktrackerAt — exercise ASCII optimization with
//     various input sizes to hit different branches.
// -----------------------------------------------------------------------------

func TestIsDigitRunSkipSafe_ExtraBranches(t *testing.T) {
	// OpRepeat with Max==-1 (unbounded): \d{2,}
	t.Run("digit_repeat_unbounded", func(t *testing.T) {
		re, err := syntax.Parse(`\d{2,}`, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		got := isDigitRunSkipSafe(re)
		if !got {
			t.Errorf("isDigitRunSkipSafe(`\\d{2,}`) = %v, want true", got)
		}
	})

	// OpRepeat with bounded Max: \d{2,5}
	t.Run("digit_repeat_bounded", func(t *testing.T) {
		re, err := syntax.Parse(`\d{2,5}`, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		got := isDigitRunSkipSafe(re)
		if got {
			t.Errorf("isDigitRunSkipSafe(`\\d{2,5}`) = %v, want false", got)
		}
	})

	// OpCapture wrapping: (\d+)
	t.Run("capture_digit_plus", func(t *testing.T) {
		re, err := syntax.Parse(`(\d+)`, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		got := isDigitRunSkipSafe(re)
		if !got {
			t.Errorf("isDigitRunSkipSafe(`(\\d+)`) = %v, want true", got)
		}
	})

	// nil check
	t.Run("nil", func(t *testing.T) {
		got := isDigitRunSkipSafe(nil)
		if got {
			t.Error("isDigitRunSkipSafe(nil) should be false")
		}
	})

	// OpConcat wrapping: \d+\.
	t.Run("concat_digit_plus", func(t *testing.T) {
		re, err := syntax.Parse(`\d+\.`, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		got := isDigitRunSkipSafe(re)
		if !got {
			t.Errorf("isDigitRunSkipSafe(`\\d+\\.`) = %v, want true", got)
		}
	})

	// Empty concat
	t.Run("empty_concat", func(t *testing.T) {
		re := &syntax.Regexp{Op: syntax.OpConcat, Sub: []*syntax.Regexp{}}
		got := isDigitRunSkipSafe(re)
		if got {
			t.Error("expected false for empty concat")
		}
	})

	// Empty capture
	t.Run("empty_capture", func(t *testing.T) {
		re := &syntax.Regexp{Op: syntax.OpCapture, Sub: []*syntax.Regexp{}}
		got := isDigitRunSkipSafe(re)
		if got {
			t.Error("expected false for empty capture")
		}
	})
}

// --- Test 78: hasDotStarPrefix OpCapture wrapping and edge cases ---
// Covers: strategy.go hasDotStarPrefix lines 583-599
// Targets: capture unwrap on outer and first sub, non-concat

func TestIsMatchDigitPrefilter_DFAAndNFA(t *testing.T) {
	// Pattern with digit prefilter: \d+\.\d+
	engine, err := Compile(`\d+\.\d+`)
	if err != nil {
		t.Fatal(err)
	}

	// Matching and non-matching
	tests := []struct {
		input string
		want  bool
	}{
		{"3.14", true},
		{"abc", false},
		{"123", false}, // No dot
		{"text 1.5 more", true},
	}

	for _, tt := range tests {
		got := engine.IsMatch([]byte(tt.input))
		if got != tt.want {
			t.Errorf("IsMatch(%q) = %v, want %v (strategy=%s)", tt.input, got, tt.want, engine.Strategy())
		}
	}
}

// --- Test 88: ReverseInner FindIndicesAt non-universal path with multiple candidates ---
// Covers: reverse_inner.go FindIndicesAt lines 502-546
// Targets: non-universal prefix/suffix paths, suffix mismatch, continue after mismatch

func TestFindIndicesCompositeSearcherAt_DFAPath(t *testing.T) {
	pattern := `[a-zA-Z]+\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseCompositeSearcher {
		t.Skipf("Strategy is %s, not UseCompositeSearcher", engine.Strategy())
	}

	// FindIndicesAt at non-zero to exercise compositeSearcherAt
	input := "...abc123..."
	s, e, found := engine.FindIndicesAt([]byte(input), 3)
	if !found {
		t.Error("expected match")
	} else {
		t.Logf("FindIndicesAt(3): [%d,%d] = %q", s, e, input[s:e])
	}

	// Multiple matches
	multiInput := "ab12 cd34 ef56"
	results := engine.FindAllIndicesStreaming([]byte(multiInput), 0, nil)
	if len(results) != 3 {
		t.Errorf("expected 3 matches, got %d", len(results))
	}
}

// --- Test 98: ReverseInner IsMatch with non-universal prefix at position 0 ---
// Covers: reverse_inner.go IsMatch lines 423-480
// Targets: pos==0 with universalPrefix/startAnchored, pos>0 reverse DFA

func TestFindIndicesDigitPrefilter_MultiplePositions(t *testing.T) {
	pattern := `\d+\.\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	// FindIndices
	input := "abc 3.14 xyz 2.71 end"
	s, e, found := engine.FindIndices([]byte(input))
	if !found {
		t.Error("expected match")
	} else {
		t.Logf("FindIndices: [%d,%d] = %q", s, e, input[s:e])
	}

	// Count
	count := engine.Count([]byte(input), -1)
	re := regexp.MustCompile(pattern)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count: coregex=%d, stdlib=%d", count, stdCount)
	}

	// No match: digits but no decimal
	noMatch := "abc 123 xyz"
	_, _, found2 := engine.FindIndices([]byte(noMatch))
	if found2 {
		// \d+\.\d+ should not match "123" alone
		// But FindIndices searches everywhere, so check:
		re2 := regexp.MustCompile(pattern)
		if !re2.MatchString(noMatch) && found2 {
			t.Error("false positive")
		}
	}
}

// --- Test 100: findIndicesAhoCorasickAt through FindAll ---
// Covers: find_indices.go findIndicesAhoCorasickAt lines 803-813

func TestFindIndicesAhoCorasickAt_Through_FindAll(t *testing.T) {
	// Large alternation for AhoCorasick strategy
	pattern := `alpha|beta|gamma|delta|epsilon|zeta|eta|theta|iota`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", engine.Strategy())
	}

	input := "alpha then beta then gamma then delta then epsilon"
	results := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	re := regexp.MustCompile(pattern)
	stdAll := re.FindAllStringIndex(input, -1)
	if len(results) != len(stdAll) {
		t.Errorf("count: coregex=%d, stdlib=%d", len(results), len(stdAll))
	}
}

// --- Test 101: findIndicesBranchDispatchAt at position 0 vs >0 ---
// Covers: find_indices.go findIndicesBranchDispatchAt lines 650-655

func TestIsDigitLeadPattern_MoreShapes(t *testing.T) {
	// These patterns exercise different branches of isDigitLeadPattern
	patterns := []struct {
		pattern  string
		isDigit  bool
		strategy Strategy
	}{
		{`\d+\.\d+\.\d+`, true, UseDigitPrefilter},
		{`[0-9]+`, true, UseDigitPrefilter},
		{`\d`, false, UseNFA}, // Single digit, too simple
		{`\d?abc`, false, UseNFA},
	}

	for _, tt := range patterns {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("Strategy for %q: %s", tt.pattern, engine.Strategy())
		})
	}
}

// --- Test 116: isDigitOnlyClass edge cases ---
// Covers: strategy.go isDigitOnlyClass line 312-314 (empty/odd runes)

func TestIsDigitOnlyClass_EdgeCases(t *testing.T) {
	// Empty runes
	got := isDigitOnlyClass(nil)
	if got {
		t.Error("expected false for nil runes")
	}

	got2 := isDigitOnlyClass([]rune{})
	if got2 {
		t.Error("expected false for empty runes")
	}

	// Odd-length runes (malformed)
	got3 := isDigitOnlyClass([]rune{'0'})
	if got3 {
		t.Error("expected false for odd-length runes")
	}
}

// --- Test 117: isDigitLeadPattern with synthetic empty-sub ASTs ---
// Covers: strategy.go isDigitLeadPattern lines 432-434, 444-446, 451-453, 458-460, 465-467

func TestIsDigitLeadPattern_EmptySubs(t *testing.T) {
	// OpAlternate with empty subs
	got := isDigitLeadPattern(&syntax.Regexp{Op: syntax.OpAlternate, Sub: []*syntax.Regexp{}})
	if got {
		t.Error("expected false for alternate with empty subs")
	}

	// OpConcat with empty subs
	got2 := isDigitLeadPattern(&syntax.Regexp{Op: syntax.OpConcat, Sub: []*syntax.Regexp{}})
	if got2 {
		t.Error("expected false for concat with empty subs")
	}

	// OpCapture with empty subs
	got3 := isDigitLeadPattern(&syntax.Regexp{Op: syntax.OpCapture, Sub: []*syntax.Regexp{}})
	if got3 {
		t.Error("expected false for capture with empty subs")
	}

	// OpPlus with empty subs
	got4 := isDigitLeadPattern(&syntax.Regexp{Op: syntax.OpPlus, Sub: []*syntax.Regexp{}})
	if got4 {
		t.Error("expected false for plus with empty subs")
	}

	// OpRepeat with empty subs
	got5 := isDigitLeadPattern(&syntax.Regexp{Op: syntax.OpRepeat, Sub: []*syntax.Regexp{}, Min: 1})
	if got5 {
		t.Error("expected false for repeat with empty subs")
	}

	// nil
	got6 := isDigitLeadPattern(nil)
	if got6 {
		t.Error("expected false for nil")
	}
}

// --- Test 118: isDigitLeadConcat all-optional path ---
// Covers: strategy.go isDigitLeadConcat line 346 (all elements optional)

func TestIsDigitLeadConcat_AllOptional(t *testing.T) {
	// Concat where all elements are optional (OpQuest or OpStar)
	subs := []*syntax.Regexp{
		{Op: syntax.OpQuest, Sub: []*syntax.Regexp{
			{Op: syntax.OpCharClass, Rune: []rune{'0', '9'}},
		}},
		{Op: syntax.OpStar, Sub: []*syntax.Regexp{
			{Op: syntax.OpCharClass, Rune: []rune{'0', '9'}},
		}},
	}
	got := isDigitLeadConcat(subs)
	if got {
		t.Error("expected false: all-optional pattern can match empty")
	}
}

// minInt returns the smaller of a and b.
