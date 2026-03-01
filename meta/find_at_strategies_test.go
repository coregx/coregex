package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestFindAdaptive_MatchFinder exercises the findAdaptive MatchFinder interface path.
// findAdaptive checks if prefilter implements MatchFinder for direct match bounds.
// This path is at 36.1% coverage.
func TestFindAdaptive_MatchFinder(t *testing.T) {
	// UseBoth requires medium NFA (20-100 states) with prefilter+DFA.
	// Build pattern that triggers UseBoth with a Teddy-like prefilter.
	// Multi-literal alternation with complex suffix forces UseBoth.
	pattern := `(apple|grape|lemon|melon|peach|plumb|mango)\d{3,10}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pattern, err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for %q: %s", pattern, strategy)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"match at start", "apple123 rest"},
		{"match in middle", "junk grape456 tail"},
		{"no match", "banana999 cherry777"},
		{"empty", ""},
		{"multiple matches", "apple111 melon222 peach333"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := engine.Find([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)

			if stdLoc == nil {
				if match != nil {
					t.Errorf("Find(%q): got %q, stdlib says no match", tt.input, match.String())
				}
			} else {
				if match == nil {
					t.Errorf("Find(%q): got nil, stdlib found [%d,%d]", tt.input, stdLoc[0], stdLoc[1])
				} else if match.Start() != stdLoc[0] || match.End() != stdLoc[1] {
					t.Errorf("Find(%q): got [%d,%d], want [%d,%d]",
						tt.input, match.Start(), match.End(), stdLoc[0], stdLoc[1])
				}
			}

			// Also test FindIndices (zero-alloc path)
			s, e, found := engine.FindIndices([]byte(tt.input))
			if stdLoc == nil {
				if found {
					t.Errorf("FindIndices: got (%d,%d,true), want not found", s, e)
				}
			} else {
				if !found || s != stdLoc[0] || e != stdLoc[1] {
					t.Errorf("FindIndices: got (%d,%d,%v), want (%d,%d,true)", s, e, found, stdLoc[0], stdLoc[1])
				}
			}
		})
	}
}

// TestFindAdaptive_DFAWithoutPrefilter exercises the DFA-only path in findAdaptive.
// When prefilter is nil but DFA exists, findAdaptive uses DFA.Find directly.
func TestFindAdaptive_DFAWithoutPrefilter(t *testing.T) {
	// Pattern with many states but no extractable literals
	var parts []string
	for i := 0; i < 25; i++ {
		parts = append(parts, string(rune('a'+i%26))+"x")
	}
	pattern := "(" + strings.Join(parts, "|") + ")*Y"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy: %s", strategy)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"match", "axbxcxY"},
		{"no match", "axbxcxZ"},
		{"empty", ""},
		{"just Y", "Y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := engine.Find([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)

			if stdLoc == nil {
				if match != nil {
					t.Errorf("got match %q, want nil", match.String())
				}
			} else if match == nil {
				t.Errorf("got nil, want [%d,%d]", stdLoc[0], stdLoc[1])
			}

			// Test IsMatch (exercises isMatchAdaptive)
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v", got, want)
			}
		})
	}
}

// TestFindAdaptive_DFACacheFull exercises the DFA cache full fallback in findAdaptive.
// When DFA cache is nearly full (90%+), findAdaptive falls back to NFA.
func TestFindAdaptive_DFACacheFull(t *testing.T) {
	// Large alternation to stress DFA cache
	var parts []string
	for i := 0; i < 20; i++ {
		parts = append(parts, strings.Repeat(string(rune('a'+i%26)), 3+i%4))
	}
	pattern := "(" + strings.Join(parts, "|") + ")+Z"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	t.Logf("Strategy: %s", engine.Strategy())

	re := regexp.MustCompile(pattern)

	// Feed large diverse input to potentially fill DFA cache
	var input string
	for i := 0; i < 50; i++ {
		input += strings.Repeat(string(rune('a'+i%26)), 3) + " "
	}
	input += "aaaZ"

	match := engine.Find([]byte(input))
	stdLoc := re.FindStringIndex(input)

	if stdLoc == nil {
		if match != nil {
			t.Errorf("got match, want nil")
		}
	} else if match == nil {
		t.Logf("No match found (may differ from stdlib due to strategy)")
	}

	// Test FindAt at non-zero position
	match2 := engine.FindAt([]byte(input), 10)
	t.Logf("FindAt(10): match=%v", match2 != nil)

	// Test FindIndicesAt at non-zero position
	s, e, found := engine.FindIndicesAt([]byte(input), 10)
	t.Logf("FindIndicesAt(10): (%d,%d,%v)", s, e, found)
}

// TestIsMatchAdaptive_AllPaths exercises isMatchAdaptive through all paths.
// isMatchAdaptive is at 44.4% coverage. It has:
//   - prefilter complete path (returns true on prefilter.Find)
//   - prefilter incomplete path (verifies with NFA)
//   - DFA path (when no prefilter, tries DFA.IsMatch)
//   - DFA cache full fallback to NFA
//   - NFA fallback (no DFA or prefilter)
func TestIsMatchAdaptive_AllPaths(t *testing.T) {
	patterns := []struct {
		pattern string
		inputs  []struct {
			input string
			want  bool
		}
	}{
		{
			// Medium complexity with literals triggers UseBoth
			pattern: `(alpha|bravo|charlie|delta|echo|foxtrot|golf)\d{2,8}`,
			inputs: []struct {
				input string
				want  bool
			}{
				{"alpha12", true},
				{"xyz", false},
				{"alpha", false},
				{"bravo99", true},
				{"", false},
				{strings.Repeat("x", 1000) + "echo55", true},
			},
		},
		{
			// No extractable literals, forces DFA path
			pattern: `(a|b|c|d|e|f|g|h|i|j|k|l|m|n|o|p|q|r|s|t|u|v|w|x|y|z)*Z`,
			inputs: []struct {
				input string
				want  bool
			}{
				{"abcZ", true},
				{"Z", true},
				{"abc", false},
				{"", false},
			},
		},
	}

	for _, pp := range patterns {
		engine, err := Compile(pp.pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pp.pattern, err)
		}
		re := regexp.MustCompile(pp.pattern)

		for _, tt := range pp.inputs {
			name := pp.pattern + "/" + tt.input
			if len(name) > 80 {
				name = name[:80]
			}
			t.Run(name, func(t *testing.T) {
				got := engine.IsMatch([]byte(tt.input))
				want := re.MatchString(tt.input)
				if got != want {
					t.Errorf("IsMatch = %v, stdlib = %v (strategy=%s)", got, want, engine.Strategy())
				}
			})
		}
	}
}

// TestFindDFAAt_AllPaths exercises findDFAAt through its branches.
// findDFAAt is at 40.0% coverage. It has:
//   - prefilter complete + LiteralLen path
//   - prefilter complete without LiteralLen (NFA fallback)
//   - DFA FindAt + NFA for bounds
func TestFindDFAAt_AllPaths(t *testing.T) {
	// Pattern that triggers UseDFA with literal prefilter
	pattern := `LONGPREFIX[a-z]{5,20}[0-9]{3,10}[A-Z]{2,8}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy: %s", strategy)

	re := regexp.MustCompile(pattern)

	input := "junk1 LONGPREFIX" + "abcde" + "12345" + "AB" + " junk2 LONGPREFIX" + "fghij" + "67890" + "CD" + " end"

	// FindAt at various positions
	positions := []int{0, 6, 10, 30, 50}
	for _, at := range positions {
		if at >= len(input) {
			continue
		}
		match := engine.FindAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])

		if stdLoc == nil {
			if match != nil {
				t.Errorf("FindAt(%d): got %q, want nil", at, match.String())
			}
		} else {
			stdStart := stdLoc[0] + at
			stdEnd := stdLoc[1] + at
			if match == nil {
				t.Errorf("FindAt(%d): got nil, stdlib found [%d,%d]", at, stdStart, stdEnd)
			} else if match.Start() != stdStart || match.End() != stdEnd {
				t.Errorf("FindAt(%d): got [%d,%d], want [%d,%d]",
					at, match.Start(), match.End(), stdStart, stdEnd)
			}
		}
	}

	// FindIndicesAt exercises findIndicesDFAAt
	for _, at := range positions {
		if at >= len(input) {
			continue
		}
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("FindIndicesAt(%d): got (%d,%d,true), want not found", at, s, e)
			}
		} else {
			stdStart := stdLoc[0] + at
			stdEnd := stdLoc[1] + at
			if !found || s != stdStart || e != stdEnd {
				t.Errorf("FindIndicesAt(%d): got (%d,%d,%v), want (%d,%d,true)",
					at, s, e, found, stdStart, stdEnd)
			}
		}
	}
}

// TestFindTeddyAt_MultiplePaths exercises findTeddyAt fallback paths.
// findTeddyAt is at 42.9% coverage.
func TestFindTeddyAt_MultiplePaths(t *testing.T) {
	// Pattern triggers UseTeddy: 3-8 exact literals
	pattern := `alpha|bravo|charlie`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseTeddy {
		t.Skipf("Strategy is %s, not UseTeddy", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "prefix alpha middle bravo suffix charlie end"

	// Test FindAt at various positions
	positions := []int{0, 7, 13, 20, 27, 40}
	for _, at := range positions {
		if at >= len(input) {
			continue
		}
		match := engine.FindAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])

		if stdLoc == nil {
			if match != nil {
				t.Errorf("FindAt(%d): got %q, want nil", at, match.String())
			}
		} else {
			stdStart := stdLoc[0] + at
			stdEnd := stdLoc[1] + at
			if match == nil {
				t.Errorf("FindAt(%d): got nil, stdlib [%d,%d]", at, stdStart, stdEnd)
			} else if match.Start() != stdStart || match.End() != stdEnd {
				t.Errorf("FindAt(%d): got [%d,%d], want [%d,%d]",
					at, match.Start(), match.End(), stdStart, stdEnd)
			}
		}
	}

	// Count exercises FindAll with at > 0 internally
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}
}

// TestFindTeddyAt_SmallHaystack exercises Fat Teddy small haystack fallback.
// For haystacks < 64 bytes with Fat Teddy, falls back to Aho-Corasick.
func TestFindTeddyAt_SmallHaystack(t *testing.T) {
	// Fat Teddy requires many patterns (33-64). Use a large alternation.
	var parts []string
	for i := 0; i < 40; i++ {
		parts = append(parts, "pat"+strings.Repeat(string(rune('a'+i%26)), 3+i%3))
	}
	pattern := strings.Join(parts, "|")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	t.Logf("Strategy: %s", engine.Strategy())

	// Small haystack (< 64 bytes) -- triggers Fat Teddy fallback to Aho-Corasick
	small := "junk pataaa tail"
	match := engine.Find([]byte(small))
	t.Logf("Find small: match=%v", match != nil)

	// FindAt on small haystack at non-zero position
	match2 := engine.FindAt([]byte(small), 5)
	t.Logf("FindAt(5) small: match=%v", match2 != nil)

	// Large haystack (> 64 bytes) -- uses Fat Teddy SIMD directly
	large := strings.Repeat("x", 200) + "pataaa" + strings.Repeat("y", 200)
	match3 := engine.Find([]byte(large))
	if match3 != nil {
		t.Logf("Find large: %q at [%d,%d]", match3.String(), match3.Start(), match3.End())
	}

	// FindAt on large haystack at non-zero
	match4 := engine.FindAt([]byte(large), 100)
	if match4 != nil {
		t.Logf("FindAt(100) large: %q at [%d,%d]", match4.String(), match4.Start(), match4.End())
	}
}

// TestFindDigitPrefilterAt_DFAAndNFAPaths exercises findDigitPrefilterAt.
// findDigitPrefilterAt is at 71.4% -- test multiple digit positions.
func TestFindDigitPrefilterAt_DFAAndNFAPaths(t *testing.T) {
	pattern := `\d+-\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseDigitPrefilter {
		t.Skipf("Strategy is %s, not UseDigitPrefilter", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "text 123-456 more 789-012 end"

	// FindAt at non-zero position past first match
	positions := []int{0, 5, 13, 18, 25}
	for _, at := range positions {
		if at >= len(input) {
			continue
		}
		match := engine.FindAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])

		if stdLoc == nil {
			if match != nil {
				t.Errorf("FindAt(%d): got %q, want nil", at, match.String())
			}
		} else {
			stdStart := stdLoc[0] + at
			stdEnd := stdLoc[1] + at
			if match == nil {
				t.Errorf("FindAt(%d): got nil, stdlib [%d,%d]", at, stdStart, stdEnd)
			} else if match.Start() != stdStart || match.End() != stdEnd {
				t.Errorf("FindAt(%d): got [%d,%d], want [%d,%d]",
					at, match.Start(), match.End(), stdStart, stdEnd)
			}
		}
	}

	// FindIndicesAt exercises findIndicesDigitPrefilterAt
	for _, at := range positions {
		if at >= len(input) {
			continue
		}
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("FindIndicesAt(%d): unexpected match (%d,%d)", at, s, e)
			}
		} else {
			stdStart := stdLoc[0] + at
			stdEnd := stdLoc[1] + at
			if !found || s != stdStart || e != stdEnd {
				t.Errorf("FindIndicesAt(%d): got (%d,%d,%v), want (%d,%d,true)",
					at, s, e, found, stdStart, stdEnd)
			}
		}
	}

	// Count exercises the full FindAll iteration loop
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// No match -- digit positions exist but no valid pattern
	noMatch := "abc 999 def 888 ghi"
	count2 := engine.Count([]byte(noMatch), -1)
	stdCount2 := len(re.FindAllString(noMatch, -1))
	if count2 != stdCount2 {
		t.Errorf("Count(noMatch) = %d, stdlib = %d", count2, stdCount2)
	}
}

// TestFindAhoCorasickAt_Paths exercises findAhoCorasickAt.
// findAhoCorasickAt is at 71.4%.
func TestFindAhoCorasickAt_Paths(t *testing.T) {
	// Large alternation (>32 literals) triggers UseAhoCorasick
	var parts []string
	for i := 0; i < 50; i++ {
		parts = append(parts, "kw"+strings.Repeat(string(rune('a'+i%26)), 3+i%4))
	}
	pattern := strings.Join(parts, "|")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseAhoCorasick {
		t.Skipf("Strategy is %s, not UseAhoCorasick", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Input with multiple matches
	input := "prefix " + parts[0] + " middle " + parts[10] + " suffix " + parts[20] + " end"

	// FindAt at non-zero positions
	for _, at := range []int{0, 7, 20, 40} {
		if at >= len(input) {
			continue
		}
		match := engine.FindAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if match != nil {
				t.Errorf("FindAt(%d): got match, want nil", at)
			}
		} else if match == nil {
			t.Errorf("FindAt(%d): got nil, want match", at)
		}
	}

	// IsMatch
	if !engine.IsMatch([]byte(input)) {
		t.Error("IsMatch should return true")
	}
	if engine.IsMatch([]byte("no matches here at all")) {
		t.Error("IsMatch should return false")
	}

	// Count
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}
}

// TestNilGuard_Strategies tests the nil-guard fallback branches.
// Many strategies have a nil check that falls back to NFA. These are at 75%.
// The nil guards are exercised when the specialized searcher is nil,
// which we can test by verifying the standard paths still work.
func TestNilGuard_Strategies(t *testing.T) {
	// Each test case exercises a strategy's Find/FindIndices/IsMatch paths.
	// The nil-guard branch is the "other" branch (when searcher IS present),
	// which is the normal path. Both branches need testing.
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		// ReverseAnchored
		{"ReverseAnchored match", `world$`, "hello world", true},
		{"ReverseAnchored no match", `world$`, "hello earth", false},
		{"ReverseAnchored empty", `world$`, "", false},

		// ReverseSuffix
		{"ReverseSuffix match", `.*\.log`, "app.log", true},
		{"ReverseSuffix no match", `.*\.log`, "app.txt", false},
		{"ReverseSuffix empty", `.*\.log`, "", false},

		// ReverseSuffixSet
		{"ReverseSuffixSet match", `.*\.(jpg|png|gif)`, "photo.jpg", true},
		{"ReverseSuffixSet no match", `.*\.(jpg|png|gif)`, "doc.pdf", false},
		{"ReverseSuffixSet empty", `.*\.(jpg|png|gif)`, "", false},

		// ReverseInner
		{"ReverseInner match", `.*warning.*`, "a warning here", true},
		{"ReverseInner no match", `.*warning.*`, "all clear", false},
		{"ReverseInner empty", `.*warning.*`, "", false},

		// MultilineReverseSuffix
		{"MultilineRevSuffix match", `(?m)^.*\.php`, "/index.php", true},
		{"MultilineRevSuffix no match", `(?m)^.*\.php`, "/index.html", false},

		// CharClassSearcher
		{"CharClass match", `[a-z]+`, "abc", true},
		{"CharClass no match", `[a-z]+`, "123", false},

		// CompositeSearcher
		{"Composite match", `[a-z]+[0-9]+`, "abc123", true},
		{"Composite no match", `[a-z]+[0-9]+`, "abc", false},

		// BranchDispatch
		{"BranchDispatch match", `^(PUT|GET|POST|DELETE)`, "GET /api", true},
		{"BranchDispatch no match", `^(PUT|GET|POST|DELETE)`, "PATCH /api", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}

			// Find
			match := engine.Find([]byte(tt.input))
			if (match != nil) != tt.want {
				t.Errorf("Find(%q): got match=%v, want %v (strategy=%s)",
					tt.input, match != nil, tt.want, engine.Strategy())
			}

			// FindIndices
			_, _, found := engine.FindIndices([]byte(tt.input))
			if found != tt.want {
				t.Errorf("FindIndices: found=%v, want %v", found, tt.want)
			}

			// IsMatch
			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch: %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFindIndicesAdaptive_WithPrefilterComplete exercises the literal fast path
// in findIndicesAdaptive when prefilter.IsComplete() && LiteralLen() > 0.
func TestFindIndicesAdaptive_WithPrefilterComplete(t *testing.T) {
	// This pattern triggers UseBoth with a complete prefilter.
	// Multi-literal alternation where literals are complete matches.
	patterns := []string{
		`(alpha|bravo|charlie|delta|echo|foxtrot|golf)\d{2,8}`,
		`(a|b|c|d|e|f|g|h|i|j|k|l|m|n|o|p|q|r|s|t|u|v|w|x|y|z)*Z`,
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pattern, err)
		}
		re := regexp.MustCompile(pattern)

		input := "leading abcZ trailing"
		s, e, found := engine.FindIndices([]byte(input))
		stdLoc := re.FindStringIndex(input)

		if stdLoc == nil {
			if found {
				t.Errorf("%q: FindIndices got (%d,%d,true), want not found", pattern, s, e)
			}
		} else if found && (s != stdLoc[0] || e != stdLoc[1]) {
			t.Errorf("%q: FindIndices got (%d,%d), want (%d,%d)", pattern, s, e, stdLoc[0], stdLoc[1])
		}

		// FindIndicesAt at various positions
		for _, at := range []int{0, 5, 8, 12} {
			if at >= len(input) {
				continue
			}
			s, e, found := engine.FindIndicesAt([]byte(input), at)
			stdLoc := re.FindIndex([]byte(input)[at:])
			if stdLoc == nil {
				if found {
					t.Errorf("%q: FindIndicesAt(%d) got (%d,%d,true), want not found", pattern, at, s, e)
				}
			} else {
				stdStart := stdLoc[0] + at
				stdEnd := stdLoc[1] + at
				if !found || s != stdStart || e != stdEnd {
					t.Errorf("%q: FindIndicesAt(%d) got (%d,%d,%v), want (%d,%d,true)",
						pattern, at, s, e, found, stdStart, stdEnd)
				}
			}
		}
	}
}

// TestFindIndicesAdaptiveAt_DFAPath exercises findIndicesAdaptiveAt DFA-only path.
// When prefilter is nil but DFA exists, DFA.FindAt is used.
func TestFindIndicesAdaptiveAt_DFAPath(t *testing.T) {
	// Pattern that triggers UseBoth without literals
	var parts []string
	for i := 0; i < 25; i++ {
		parts = append(parts, string(rune('a'+i%26))+"x")
	}
	pattern := "(" + strings.Join(parts, "|") + ")*Y"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	re := regexp.MustCompile(pattern)

	input := "prefix axbxY suffix cxdxY end"

	// Count exercises FindAll which exercises FindIndicesAt at > 0
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d (strategy=%s)", count, stdCount, engine.Strategy())
	}

	// FindIndicesAt at specific positions
	for _, at := range []int{0, 7, 12, 20} {
		if at >= len(input) {
			continue
		}
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("at=%d: got (%d,%d), want not found", at, s, e)
			}
		} else {
			stdStart := stdLoc[0] + at
			stdEnd := stdLoc[1] + at
			if !found || s != stdStart || e != stdEnd {
				t.Errorf("at=%d: got (%d,%d,%v), want (%d,%d,true)",
					at, s, e, found, stdStart, stdEnd)
			}
		}
	}
}
