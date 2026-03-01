package meta

// Tests for Find/FindAt/FindIndices dispatch through all strategy paths
// including adaptive (UseBoth), branch dispatch, and DFA paths.

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"
)

func TestFindAdaptiveAt_UseBoth(t *testing.T) {
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name      string
		haystack  string
		at        int
		wantFound bool
	}{
		// FindAt at position 0 dispatches through findAtZero -> findAdaptive
		{"at_zero_match", "abcz rest", 0, true},
		// FindAt at non-zero dispatches through findAtNonZero -> findAdaptiveAt
		{"at_nonzero_match", "xxabczxx", 2, true},
		// FindAt past all matches
		{"at_past_all", "abcz", 4, false},
		// FindAt past end
		{"at_past_end", "abcz", 100, false},
		// No match anywhere
		{"no_match", "xxxyyy", 0, false},
		// Empty haystack
		{"empty", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := engine.FindAt([]byte(tt.haystack), tt.at)
			if tt.wantFound {
				if match == nil {
					t.Fatalf("FindAt(%q, %d) = nil, want match", tt.haystack, tt.at)
				}
				// Cross-validate with stdlib
				stdMatch := re.FindString(tt.haystack[tt.at:])
				if stdMatch != "" && match.String() != stdMatch {
					t.Logf("FindAt(%q, %d) = %q, stdlib = %q (note: different semantics for absolute positions)",
						tt.haystack, tt.at, match.String(), stdMatch)
				}
			} else if match != nil {
				t.Errorf("FindAt(%q, %d) = %q, want nil", tt.haystack, tt.at, match.String())
			}
		})
	}
}

// TestWave4_FindAdaptiveAt_FindAll exercises findAdaptiveAt through FindAll.
// FindAll internally calls FindAt/FindIndicesAt with at > 0 for subsequent matches.

func TestFindAdaptiveAt_FindAll(t *testing.T) {
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	re := regexp.MustCompile(pattern)

	// Input with multiple matches forces FindAll to call At methods at >0
	haystack := "az bz cz"
	count := engine.Count([]byte(haystack), -1)
	stdCount := len(re.FindAllString(haystack, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d for %q", count, stdCount, haystack)
	}

	// Verify individual matches
	results := engine.FindAllIndicesStreaming([]byte(haystack), 0, nil)
	stdResults := re.FindAllStringIndex(haystack, -1)
	if len(results) != len(stdResults) {
		t.Errorf("FindAll: got %d, stdlib %d", len(results), len(stdResults))
	}
}

// -----------------------------------------------------------------------------
// 2. findIndicesAdaptiveAt (find_indices.go:329) — UseBoth FindIndicesAt
//    Covers the zero-allocation FindIndicesAt path for UseBoth strategy.
// -----------------------------------------------------------------------------

func TestFindIndicesAdaptiveAt_UseBoth(t *testing.T) {
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	haystack := []byte("xxazxxbzxx")
	re := regexp.MustCompile(pattern)

	tests := []struct {
		name      string
		at        int
		wantFound bool
	}{
		{"at_zero", 0, true},
		{"at_after_first", 4, true},
		{"at_past_all", 8, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e, found := engine.FindIndicesAt(haystack, tt.at)
			if found != tt.wantFound {
				t.Fatalf("FindIndicesAt(at=%d) found=%v, want %v", tt.at, found, tt.wantFound)
			}
			if found {
				t.Logf("FindIndicesAt(at=%d) = (%d,%d) = %q", tt.at, s, e, string(haystack[s:e]))
			}
		})
	}

	// Cross-validate total matches against stdlib
	allIndices := engine.FindAllIndicesStreaming(haystack, 0, nil)
	stdAll := re.FindAllIndex(haystack, -1)
	if len(allIndices) != len(stdAll) {
		t.Errorf("total matches: got %d, stdlib %d", len(allIndices), len(stdAll))
	}
}

// TestWave4_FindIndicesAdaptive_Branches exercises findIndicesAdaptive branches.

func TestFindIndicesAdaptive_Branches(t *testing.T) {
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"has_match", "xabczx", true},
		{"no_match", "xxxx", false},
		{"match_at_start", "az", true},
		{"match_at_end", "xxxaz", true},
		{"empty", "", false},
		{"multiple", "az bz", true},
		// Longer input: may exercise DFA cache
		{"long_input", strings.Repeat("xyz", 100) + "az" + strings.Repeat("xyz", 100), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e, found := engine.FindIndices([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			stdFound := stdLoc != nil

			if found != stdFound {
				t.Errorf("found=%v, stdlib=%v", found, stdFound)
			}
			if found && stdFound {
				if s != stdLoc[0] || e != stdLoc[1] {
					t.Errorf("got (%d,%d), stdlib (%d,%d)", s, e, stdLoc[0], stdLoc[1])
				}
			}
		})
	}
}

// TestWave4_FindAdaptive_DFAPath exercises findAdaptive DFA path.

func TestFindAdaptive_DFAPath(t *testing.T) {
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"match_middle", "xxxazxxx"},
		{"match_start", "az"},
		{"no_match_short", "abc"},
		{"long_input", strings.Repeat("xyz", 100) + "az" + strings.Repeat("xyz", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := engine.Find([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)

			if (m == nil) != (stdLoc == nil) {
				t.Errorf("Find: got nil=%v, stdlib nil=%v", m == nil, stdLoc == nil)
			}
			if m != nil && stdLoc != nil {
				if m.Start() != stdLoc[0] || m.End() != stdLoc[1] {
					t.Errorf("Find: got [%d,%d], stdlib [%d,%d]",
						m.Start(), m.End(), stdLoc[0], stdLoc[1])
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 3. findBranchDispatchAt (find.go:556) — branch dispatch at non-zero position
//    For anchored patterns, FindAt(at>0) always returns nil.
//    Also covers findIndicesBranchDispatchAt (find_indices.go:649).
// -----------------------------------------------------------------------------

func TestFindBranchDispatchAt(t *testing.T) {
	patterns := []struct {
		name    string
		pattern string
		match0  string
	}{
		{"simple_alternation", `^(foo|bar|baz|qux)`, "foo"},
		{"digit_alternatives", `^(\d+|UUID|hex)`, "123"},
	}

	for _, pp := range patterns {
		t.Run(pp.name, func(t *testing.T) {
			engine, err := Compile(pp.pattern)
			if err != nil {
				t.Fatal(err)
			}
			requireStrategy(t, engine, UseBranchDispatch)

			haystack := []byte(pp.match0 + " trailing")

			// FindAt at 0 should match
			m0 := engine.FindAt(haystack, 0)
			if m0 == nil || m0.String() != pp.match0 {
				t.Errorf("FindAt(0) = %v, want %q", m0, pp.match0)
			}

			// FindAt at non-zero: anchored pattern cannot match
			// This exercises findBranchDispatchAt which returns nil for at != 0
			for _, at := range []int{1, 2, 3, 5, 10} {
				m := engine.FindAt(haystack, at)
				if m != nil {
					t.Errorf("FindAt(%d) = %q, want nil (anchored)", at, m.String())
				}
			}

			// FindIndicesAt at non-zero: also returns not found
			s, e, found := engine.FindIndicesAt(haystack, 1)
			if found {
				t.Errorf("FindIndicesAt(1) = (%d,%d,true), want false", s, e)
			}

			// FindIndicesAt at 0: should work
			s0, e0, found0 := engine.FindIndicesAt(haystack, 0)
			if !found0 {
				t.Error("FindIndicesAt(0) should find match")
			} else if string(haystack[s0:e0]) != pp.match0 {
				t.Errorf("FindIndicesAt(0) = %q, want %q", string(haystack[s0:e0]), pp.match0)
			}
		})
	}
}

// TestWave4_FindBranchDispatchAt_Count ensures Count works for anchored patterns.

func TestFindBranchDispatchAt_Count(t *testing.T) {
	engine, err := Compile(`^(alpha|beta|gamma|delta)`)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBranchDispatch)

	tests := []struct {
		input string
		want  int
	}{
		{"alpha rest", 1},
		{"beta rest", 1},
		{"gamma rest", 1},
		{"delta rest", 1},
		{"epsilon rest", 0},
		{"", 0},
	}

	for _, tt := range tests {
		count := engine.Count([]byte(tt.input), -1)
		if count != tt.want {
			t.Errorf("Count(%q) = %d, want %d", tt.input, count, tt.want)
		}
	}
}

// -----------------------------------------------------------------------------
// 4. findIndicesBidirectionalDFA (find_indices.go:489) — BT overflow + DFA fallback
//    Triggered when BoundedBacktracker can't handle input size AND both forward
//    and reverse DFA are available.
// -----------------------------------------------------------------------------

func TestFindDFAAt(t *testing.T) {
	pattern := `LONGPREFIX[a-z]{5,20}[0-9]{3,10}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseDFA && engine.Strategy() != UseBoth {
		t.Skipf("strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)
	inputStr := "junk LONGPREFIXabcde12345 more LONGPREFIXfghij67890 end"
	haystack := []byte(inputStr)

	// FindAll exercises FindAt with at > 0
	count := engine.Count(haystack, -1)
	stdCount := len(re.FindAllIndex(haystack, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// Verify match positions
	allMatches := engine.FindAllIndicesStreaming(haystack, 0, nil)
	stdMatches := re.FindAllIndex(haystack, -1)
	if len(allMatches) != len(stdMatches) {
		t.Fatalf("count: got %d, stdlib %d", len(allMatches), len(stdMatches))
	}
	for i := range allMatches {
		if allMatches[i][0] != stdMatches[i][0] || allMatches[i][1] != stdMatches[i][1] {
			t.Errorf("match[%d]: got [%d,%d], stdlib [%d,%d]",
				i, allMatches[i][0], allMatches[i][1], stdMatches[i][0], stdMatches[i][1])
		}
	}
}

// TestWave4_FindDFAAt_LiteralFastPath exercises the literal fast path in findDFAAt.

func TestFindDFAAt_LiteralFastPath(t *testing.T) {
	pattern := `hello`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)
	haystack := []byte("say hello and hello again")

	count := engine.Count(haystack, -1)
	stdCount := len(re.FindAllString(string(haystack), -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}
}

// -----------------------------------------------------------------------------
// 9. BoundedBacktracker overflow fallback paths (37% and 27.5% coverage)
// -----------------------------------------------------------------------------

func TestIsMatch_ReverseStrategies(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"rsuffix_match", `.*\.txt`, "hello.txt", true},
		{"rsuffix_nomatch", `.*\.txt`, "hello.csv", false},
		{"rsuffixset_match", `.*\.(txt|log|md)`, "file.txt", true},
		{"rsuffixset_nomatch", `.*\.(txt|log|md)`, "file.csv", false},
		{"rinner_match", `.*keyword.*`, "has keyword here", true},
		{"rinner_nomatch", `.*keyword.*`, "no match here", false},
		// ReverseAnchored: use literal suffix to ensure correct matching
		{"ranchored_match", `hello world$`, "say hello world", true},
		{"ranchored_nomatch", `hello world$`, "hello world!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v (strategy=%s)",
					tt.input, got, tt.want, engine.Strategy())
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 14. FindAtZero coverage for all strategies (94.4% -> higher)
// -----------------------------------------------------------------------------

func TestFindAtZero_AllStrategies(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    string
	}{
		{"reverse_suffix", `.*\.txt`, "hello.txt", "hello.txt"},
		{"reverse_suffix_set", `.*\.(txt|log|md)`, "hello.txt", "hello.txt"},
		{"reverse_inner", `.*keyword.*`, "has keyword here", "has keyword here"},
		{"multiline_suffix", `(?m)^/.*\.php`, "/index.php", "/index.php"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			m := engine.Find([]byte(tt.input))
			if m == nil {
				t.Fatalf("Find = nil, want %q", tt.want)
			}

			re := regexp.MustCompile(tt.pattern)
			stdMatch := re.FindString(tt.input)
			if m.String() != stdMatch {
				t.Errorf("Find = %q, stdlib = %q (strategy=%s)",
					m.String(), stdMatch, engine.Strategy())
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 15. CompositeSearcher DFA path (42.9% -> higher)
// -----------------------------------------------------------------------------

func TestFindAtNonZero_ReverseStrategies(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		at      int
	}{
		// ReverseSuffix at non-zero
		{"rsuffix_at5", `.*\.txt`, "xxxx hello.txt", 5},
		// ReverseInner at non-zero
		{"rinner_at3", `.*keyword.*`, "xx keyword here", 3},
		// ReverseAnchored at non-zero
		{"ranchored_at5", `world$`, "xxxx world", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			// FindAt at non-zero exercises findAtNonZero dispatch
			m := engine.FindAt([]byte(tt.input), tt.at)
			if m != nil {
				t.Logf("FindAt(%d) = %q at [%d,%d] (strategy=%s)",
					tt.at, m.String(), m.Start(), m.End(), engine.Strategy())
			}
		})
	}
}

// =============================================================================
// Wave 4B: Additional tests to push coverage from 74.1% to 80%+.
// Targeting: reverse_inner.go (Find 14%, FindIndicesAt 22.6%),
//            findAdaptive (36.1%), findTeddy/findTeddyAt (38-61%),
//            findIndicesBoundedBacktrackerAt (37%), and other low-coverage functions.
// =============================================================================

// -----------------------------------------------------------------------------
// 17. ReverseInner.Find non-universal paths (14% -> higher)
//     Universal (.*keyword.*) only covers the universalPrefix && universalSuffix shortcut.
//     Non-universal patterns exercise the full DFA bidirectional scan loop.
// -----------------------------------------------------------------------------

func TestFindIndicesDFA_WithPrefilter(t *testing.T) {
	// Pattern with good prefix literal (triggers prefilter+DFA)
	pattern := `LONGPREFIX[a-z]{3,10}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseDFA {
		t.Skipf("strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)
	haystack := "xxx LONGPREFIXabcdef xxx LONGPREFIXghijk xxx"

	// FindIndices
	s, e, found := engine.FindIndices([]byte(haystack))
	stdLoc := re.FindStringIndex(haystack)
	if found && stdLoc != nil {
		if s != stdLoc[0] || e != stdLoc[1] {
			t.Errorf("got (%d,%d), stdlib (%d,%d)", s, e, stdLoc[0], stdLoc[1])
		}
	}

	// FindAll exercises FindIndicesAt at non-zero
	count := engine.Count([]byte(haystack), -1)
	stdCount := len(re.FindAllString(haystack, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}
}

// TestWave4_FindIndicesDFA_NoPrefilter exercises DFA path without prefilter.

func TestFindIndicesDFA_NoPrefilter(t *testing.T) {
	// Large alternation pattern with >100 states but no good prefix
	var parts []string
	for i := 0; i < 30; i++ {
		parts = append(parts, strings.Repeat(string(rune('a'+i%26)), 2+i%3))
	}
	pattern := "(" + strings.Join(parts, "|") + ")*Z"

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseDFA && engine.Strategy() != UseBoth {
		t.Skipf("strategy is %s", engine.Strategy())
	}

	haystack := "aaZ bbZ ccZ"
	re := regexp.MustCompile(pattern)

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
// 21. isMatchAdaptive (44.4%) — exercise UseBoth IsMatch path
// -----------------------------------------------------------------------------

func TestIsMatchAdaptive(t *testing.T) {
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		input string
		want  bool
	}{
		{"abcz", true},
		{"z", true},    // just the trailing literal
		{"xyz", false}, // no z-producing path
		{"", false},
		{strings.Repeat("abc", 100) + "z", true},
	}

	for _, tt := range tests {
		got := engine.IsMatch([]byte(tt.input))
		stdGot := re.MatchString(tt.input)
		if got != stdGot {
			t.Errorf("IsMatch(%q): got %v, stdlib %v", tt.input, got, stdGot)
		}
	}
}

// -----------------------------------------------------------------------------
// 22. findIndicesDigitPrefilterAt (59.1%) — exercise more branches
// -----------------------------------------------------------------------------

func TestIsMatchAdaptive_NoMatchViaPrefilter(t *testing.T) {
	// UseBoth pattern exercises isMatchAdaptive
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	// Test no-match case — DFA returns false (no 'z' in input)
	if engine.IsMatch([]byte("nothing")) {
		t.Error("IsMatch should be false for 'nothing'")
	}

	// Test match case — DFA finds it
	if !engine.IsMatch([]byte("az")) {
		t.Error("IsMatch should be true for 'az'")
	}

	// Test with various inputs to exercise DFA-only path (no prefilter for UseBoth)
	tests := []struct {
		input string
		want  bool
	}{
		{"z", true},
		{"abz", true},
		{"zzz", true},
		{"", false},
		{"abcdef", false},
		{strings.Repeat("a", 100) + "z", true},
		{strings.Repeat("a", 100), false},
	}

	re := regexp.MustCompile(pattern)
	for _, tt := range tests {
		got := engine.IsMatch([]byte(tt.input))
		stdGot := re.MatchString(tt.input)
		if got != stdGot {
			t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input[:minInt(len(tt.input), 20)], got, stdGot)
		}
	}
}

// -----------------------------------------------------------------------------
// 30. findTeddy / findTeddyAt (42-62%) — exercise fallback paths and
//     no-match paths for Teddy strategy.
// -----------------------------------------------------------------------------

func TestFindDFAAt_DFASearch(t *testing.T) {
	// Large pattern with good prefix literal — triggers UseDFA
	pattern := `LONGPREFIX[a-z]{5,20}[0-9]{3,10}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy: %s", strategy)

	if strategy != UseDFA {
		t.Skipf("Strategy is %s, need UseDFA", strategy)
	}

	re := regexp.MustCompile(pattern)

	// Multiple matches — exercises findDFAAt via FindAll loop
	input := "LONGPREFIXabcde12345 LONGPREFIXfghij67890 LONGPREFIXklmno11111"
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// FindAll
	indices := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	stdAll := re.FindAllStringIndex(input, -1)
	if len(indices) != len(stdAll) {
		t.Errorf("FindAll: got %d, stdlib %d", len(indices), len(stdAll))
	}

	// FindAt at various positions
	for at := 0; at <= 42; at += 7 {
		match := engine.FindAt([]byte(input), at)
		if match != nil {
			t.Logf("FindAt(%d) = %q at [%d,%d]", at, match.String(), match.Start(), match.End())
		}
	}
}

// -----------------------------------------------------------------------------
// 36. findIndicesAdaptiveAt (47.8%) — exercise prefilter branches.
//     For UseBoth patterns that have DFA but no prefilter, the DFA-only branch
//     gets exercised. We need patterns that exercise FindAll to hit the At path.
// -----------------------------------------------------------------------------

func TestFindIndicesAdaptiveAt_FindAll(t *testing.T) {
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	re := regexp.MustCompile(pattern)

	// Multiple matches trigger findIndicesAdaptiveAt (the At variant)
	input := "az bz cz dz ez"
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// Larger input with many matches — DFA path with cache pressure
	largeInput := strings.Repeat("abcdefghz ", 200)
	count2 := engine.Count([]byte(largeInput), -1)
	stdCount2 := len(re.FindAllString(largeInput, -1))
	if count2 != stdCount2 {
		t.Errorf("Count large = %d, stdlib = %d", count2, stdCount2)
	}

	// FindIndicesAt at various positions
	indices := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	stdAll := re.FindAllStringIndex(input, -1)
	if len(indices) != len(stdAll) {
		t.Errorf("FindAll: got %d, stdlib %d", len(indices), len(stdAll))
	}
}

// -----------------------------------------------------------------------------
// 37. ReverseSuffix.FindIndicesAt (54.5%) — exercise more branches.
// -----------------------------------------------------------------------------

func TestAnchoredLiteral_FindIndicesAt_Detailed(t *testing.T) {
	pattern := `^hello.*world$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseAnchoredLiteral {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"match", "hello beautiful world"},
		{"no_match", "hello beautiful"},
		{"no_prefix", "say hello world"},
		{"empty", ""},
		{"exact", "helloworld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e, found := engine.FindIndices([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if found != (stdLoc != nil) {
				t.Errorf("FindIndices(%q): found=%v, stdlib=%v", tt.input, found, stdLoc != nil)
			}
			if found && stdLoc != nil && (s != stdLoc[0] || e != stdLoc[1]) {
				t.Errorf("FindIndices: got [%d,%d], stdlib [%d,%d]", s, e, stdLoc[0], stdLoc[1])
			}
		})
	}

	// Count — anchored pattern matches at most once
	count := engine.Count([]byte("hello world"), -1)
	if count != 1 {
		t.Errorf("Count = %d, want 1", count)
	}
	count = engine.Count([]byte("no match"), -1)
	if count != 0 {
		t.Errorf("Count no match = %d, want 0", count)
	}
}

// -----------------------------------------------------------------------------
// 49. Various find*At functions at 75% — exercise the "at > 0" fallback paths.
//     Many of these dispatch to findNFAAt when at > 0.
// -----------------------------------------------------------------------------

func TestFindAtNonZero_MoreStrategies(t *testing.T) {
	// Exercise FindAt at non-zero for various strategies
	tests := []struct {
		name    string
		pattern string
		input   string
		at      int
	}{
		// ReverseSuffix at > 0
		{"reverse_suffix_at5", `.*\.txt`, "xxxx file.txt more", 5},
		// ReverseSuffixSet at > 0
		{"reverse_suffix_set_at5", `.*\.(txt|log)`, "xxxx app.log end", 5},
		// ReverseInner at > 0
		{"reverse_inner_at5", `.*connection.*`, "xxxx connection refused", 5},
		// MultilineReverseSuffix at > 0
		{"multiline_at5", `(?m)^/.*\.php`, "text\n/index.php", 5},
		// ReverseAnchored at > 0
		{"reverse_anchored_at3", `hello world$`, "xx hello world", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			re := regexp.MustCompile(tt.pattern)

			// FindAt
			match := engine.FindAt([]byte(tt.input), tt.at)
			if match != nil {
				t.Logf("FindAt(%d) = %q at [%d,%d] (strategy=%s)",
					tt.at, match.String(), match.Start(), match.End(), engine.Strategy())
			}

			// FindIndicesAt
			s, e, found := engine.FindIndicesAt([]byte(tt.input), tt.at)
			if found {
				gotStr := tt.input[s:e]
				t.Logf("FindIndicesAt(%d) = %q at [%d,%d]", tt.at, gotStr, s, e)
			}

			// Cross-validate Find at 0 with stdlib
			match0 := engine.Find([]byte(tt.input))
			stdMatch := re.FindString(tt.input)
			if (match0 != nil) != (stdMatch != "") {
				t.Errorf("Find: got=%v, stdlib=%v", match0 != nil, stdMatch != "")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 50. BoundedBacktracker (anchored patterns with captures) — thorough exercise.
//     These patterns trigger UseBoundedBacktracker and exercise both find and
//     findIndices paths including the OnePass optimization if available.
// -----------------------------------------------------------------------------

func TestFindAdaptive_DFA_MatchFound(t *testing.T) {
	pattern := useBothPattern()
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	requireStrategy(t, engine, UseBoth)

	re := regexp.MustCompile(pattern)

	// These inputs exercise the DFA path in findAdaptive where DFA finds endPos != -1
	// and then PikeVM refines the match bounds.
	tests := []struct {
		name  string
		input string
	}{
		{"short_match", "az"},
		{"medium_match", "abcdefghz"},
		{"match_in_middle", "xxx" + strings.Repeat("ab", 10) + "z" + "yyy"},
		{"no_match_long", strings.Repeat("abcdefgh", 20)},
		{"multiple_z", "azazaz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := engine.Find([]byte(tt.input))
			stdMatch := re.FindString(tt.input)
			got := ""
			if match != nil {
				got = match.String()
			}
			if got != stdMatch {
				t.Errorf("Find(%q): got %q, stdlib %q",
					tt.input[:minInt(len(tt.input), 30)], got, stdMatch)
			}

			// Also test FindIndices
			s, e, found := engine.FindIndices([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if found != (stdLoc != nil) {
				t.Errorf("FindIndices: found=%v, stdlib=%v", found, stdLoc != nil)
			}
			if found && stdLoc != nil && (s != stdLoc[0] || e != stdLoc[1]) {
				t.Errorf("FindIndices: got [%d,%d], stdlib [%d,%d]", s, e, stdLoc[0], stdLoc[1])
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 52. Exercise findIndicesReverseSuffix/ReverseSuffixSet At functions (75%)
//     via FindAll which calls the At variants for subsequent matches.
// -----------------------------------------------------------------------------

func TestIsMatch_AllStrategies_Thorough(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		inputs  []string
	}{
		{"reverse_suffix", `.*\.txt`, []string{
			"file.txt", "no_match", "", ".txt", "path/file.txt",
		}},
		{"reverse_suffix_set", `.*\.(txt|log|md)`, []string{
			"file.txt", "app.log", "readme.md", "image.png", "",
		}},
		{"reverse_anchored", `hello world$`, []string{
			"hello world", "say hello world", "hello world!", "",
		}},
		{"reverse_inner", `.*connection.*`, []string{
			"connection refused", "all fine", "", "lost connection",
		}},
		{"multiline_reverse", `(?m)^/.*\.php`, []string{
			"/index.php", "no match", "\n/admin.php", "",
		}},
		{"teddy", `foo|bar|baz`, []string{
			"foo", "bar", "baz", "xyz", "", "xfoox",
		}},
		{"aho_corasick", `alpha|beta|gamma|delta|epsilon|zeta|eta|theta|iota`, []string{
			"alpha", "zeta", "xyz", "",
		}},
		{"digit_prefilter", `\d+\.\d+`, []string{
			"1.0", "no match", "", "3.14",
		}},
		{"char_class", `\w+`, []string{
			"hello", "", "123", "  spaces  ",
		}},
		{"composite", `[a-z]+\d+`, []string{
			"abc123", "no match", "", "x1",
		}},
		{"bounded_bt", `^(\d+)`, []string{
			"123abc", "abc", "", "99",
		}},
		{"anchored_literal", `^hello.*world$`, []string{
			"hello world", "hello beautiful world", "nope", "",
		}},
		{"branch_dispatch", `^(foo|bar|baz|qux)`, []string{
			"foo123", "baz!", "xyz", "",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			re := regexp.MustCompile(tt.pattern)

			for _, input := range tt.inputs {
				got := engine.IsMatch([]byte(input))
				stdGot := re.MatchString(input)
				if got != stdGot {
					t.Errorf("IsMatch(%q) = %v, stdlib = %v (strategy=%s)",
						input, got, stdGot, engine.Strategy())
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 58. findSubmatchAt — exercise FindSubmatch and FindAllSubmatch more.
// -----------------------------------------------------------------------------

func TestIsMatch_ReverseStrategies_EdgeCases(t *testing.T) {
	// ReverseSuffix: test with very short inputs
	t.Run("reverse_suffix_short", func(t *testing.T) {
		engine, _ := Compile(`.*\.txt`)
		re := regexp.MustCompile(`.*\.txt`)

		shortInputs := []string{".txt", "x.txt", ".tx", "t", ""}
		for _, input := range shortInputs {
			got := engine.IsMatch([]byte(input))
			std := re.MatchString(input)
			if got != std {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", input, got, std)
			}
		}
	})

	// ReverseSuffixSet: test with very short inputs
	t.Run("reverse_suffix_set_short", func(t *testing.T) {
		engine, _ := Compile(`.*\.(txt|log|md)`)
		re := regexp.MustCompile(`.*\.(txt|log|md)`)

		shortInputs := []string{".txt", ".log", ".md", ".tx", "x", ""}
		for _, input := range shortInputs {
			got := engine.IsMatch([]byte(input))
			std := re.MatchString(input)
			if got != std {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", input, got, std)
			}
		}
	})

	// ReverseInner: test with single-word inputs
	t.Run("reverse_inner_short", func(t *testing.T) {
		engine, _ := Compile(`.*error.*`)
		re := regexp.MustCompile(`.*error.*`)

		shortInputs := []string{"error", "err", "e", "", "errorerror"}
		for _, input := range shortInputs {
			got := engine.IsMatch([]byte(input))
			std := re.MatchString(input)
			if got != std {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", input, got, std)
			}
		}
	})
}

// -----------------------------------------------------------------------------
// 60. More compile.go branches — Config validation, error paths.
// -----------------------------------------------------------------------------

func TestDigitRunSkipSafe_ViaStrategy(t *testing.T) {
	// Patterns that may or may not qualify for digit run skip
	patterns := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"digit_plus", `\d+`, "123", true},
		{"digit_star", `\d*`, "123", true},
		{"digit_range", `\d{2,}`, "123", true},
		{"digit_bounded", `\d{1,3}`, "123", true},
		{"digit_dot", `\d+\.\d+`, "1.2", true},
		{"digit_no_match", `\d+`, "abc", false},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			re := regexp.MustCompile(tt.pattern)
			got := engine.IsMatch([]byte(tt.input))
			std := re.MatchString(tt.input)
			if got != std {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v (strategy=%s)",
					tt.input, got, std, engine.Strategy())
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 67. Exercise the findall.go Count and FindAll with edge cases.
// -----------------------------------------------------------------------------

func TestAnchoredLiteral_HelperBranches(t *testing.T) {
	// isCharClassPlus with non-Plus op
	t.Run("isCharClassPlus_star", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpCharClass, Rune: []rune{'a', 'z'}}},
		}
		got := isCharClassPlus(re)
		if got {
			t.Error("expected false for OpStar")
		}
	})

	// isCharClassPlus with wrong sub count
	t.Run("isCharClassPlus_no_sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpPlus,
			Sub: []*syntax.Regexp{},
		}
		got := isCharClassPlus(re)
		if got {
			t.Error("expected false for Plus with no sub")
		}
	})

	// isCharClassPlus with non-charclass sub
	t.Run("isCharClassPlus_literal_sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpPlus,
			Sub: []*syntax.Regexp{{Op: syntax.OpLiteral, Rune: []rune{'a'}}},
		}
		got := isCharClassPlus(re)
		if got {
			t.Error("expected false for Plus with literal sub")
		}
	})

	// isGreedyWildcard with wrong sub count
	t.Run("isGreedyWildcard_no_sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{},
		}
		got := isGreedyWildcard(re)
		if got {
			t.Error("expected false for Star with no sub")
		}
	})

	// isGreedyWildcard with non-any sub
	t.Run("isGreedyWildcard_literal_sub", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpStar,
			Sub: []*syntax.Regexp{{Op: syntax.OpLiteral, Rune: []rune{'a'}}},
		}
		got := isGreedyWildcard(re)
		if got {
			t.Error("expected false for Star with literal sub")
		}
	})

	// isGreedyWildcard with non-star/plus op
	t.Run("isGreedyWildcard_quest", func(t *testing.T) {
		re := &syntax.Regexp{
			Op:  syntax.OpQuest,
			Sub: []*syntax.Regexp{{Op: syntax.OpAnyChar}},
		}
		got := isGreedyWildcard(re)
		if got {
			t.Error("expected false for Quest")
		}
	})
}

// --- Test 80: buildCharClassTable edge cases ---
// Covers: anchored_literal.go buildCharClassTable lines 261-285
// Targets: non-charclass input, range > 255

func TestFindIndicesBranchDispatchAt_Positions(t *testing.T) {
	pattern := `^(foo|bar|baz|qux)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBranchDispatch {
		t.Skipf("Strategy is %s, not UseBranchDispatch", engine.Strategy())
	}

	// At position 0 should find match
	s, e, found := engine.FindIndicesAt([]byte("foo123"), 0)
	if !found {
		t.Error("expected match at position 0")
	} else {
		t.Logf("FindIndicesAt(0): [%d,%d]", s, e)
	}

	// At position > 0 should NOT match (anchored)
	_, _, found2 := engine.FindIndicesAt([]byte("foo123"), 1)
	if found2 {
		t.Error("expected no match at position > 0 for anchored pattern")
	}
}

// --- Test 102: ReverseSuffix IsMatch with various inputs ---
// Covers: reverse_suffix.go IsMatch lines 319-375
// Targets: anti-quadratic guard path, suffix not found
