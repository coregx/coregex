package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestBoundedBTAtWithState_AnchoredFirstByte exercises the anchoredFirstBytes
// O(1) early rejection in findIndicesBoundedBacktrackerAtWithState.
// This path is at 50% coverage.
func TestBoundedBTAtWithState_AnchoredFirstByte(t *testing.T) {
	// Anchored pattern with limited first bytes
	pattern := `^/[a-z]+\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"slash start match", "/index.php", true},
		{"slash start deep", "/admin/user.php", true},
		{"no slash start", "index.php", false},
		{"digit start", "123.php", false},
		{"empty", "", false},
		{"just slash", "/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v", got, want)
			}

			// Find
			match := engine.Find([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if stdLoc == nil {
				if match != nil {
					t.Errorf("Find: got %q, want nil", match.String())
				}
			} else if match == nil {
				t.Errorf("Find: got nil, want match")
			}

			// Count (exercises FindAll with WithState path)
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}
		})
	}
}

// TestBoundedBTAtWithState_ASCIIPath exercises the ASCII optimization
// in findIndicesBoundedBacktrackerAtWithState. This uses asciiBoundedBacktracker
// when input is ASCII-only and the pattern contains '.'.
func TestBoundedBTAtWithState_ASCIIPath(t *testing.T) {
	// Pattern with '.' triggers ASCII NFA optimization
	pattern := `^/.*\.html$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// All ASCII inputs exercise the ASCII BT path
	tests := []struct {
		name  string
		input string
	}{
		{"match short", "/index.html"},
		{"match deep", "/a/b/c/page.html"},
		{"no match ext", "/index.php"},
		{"no match prefix", "index.html"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v (strategy=%s)",
					tt.input, got, want, engine.Strategy())
			}

			// Count exercises the full WithState loop path
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}
		})
	}
}

// TestBoundedBTAt_UTF8Input exercises the non-ASCII path in findIndicesBoundedBacktrackerAt.
// When input contains non-ASCII bytes, the ASCII optimization is skipped.
func TestBoundedBTAt_UTF8Input(t *testing.T) {
	// Pattern with '.' that has ASCII optimization, but input is UTF-8
	pattern := `^/.+\.html$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// Non-ASCII inputs -- the asciiBoundedBacktracker path is skipped
	tests := []struct {
		name  string
		input string
	}{
		{"utf8 match", "/caf\u00e9.html"},
		{"utf8 no match", "/caf\u00e9.php"},
		{"ascii match", "/page.html"},
		{"chinese", "/\u4e2d\u6587.html"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input, got, want)
			}

			// FindIndices
			s, e, found := engine.FindIndices([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if stdLoc == nil {
				if found {
					t.Errorf("FindIndices: unexpected (%d,%d)", s, e)
				}
			} else if !found {
				t.Errorf("FindIndices: not found, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
			}
		})
	}
}

// TestBidirectionalDFA_LargeInput exercises findIndicesBidirectionalDFA
// by using an input large enough to exceed BoundedBacktracker's capacity.
// findIndicesBidirectionalDFA is at 70.0%.
func TestBidirectionalDFA_LargeInput(t *testing.T) {
	// Pattern that uses BoundedBacktracker but has DFA+reverseDFA available
	pattern := `(\w{2,8})+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)
	t.Logf("Strategy: %s", engine.Strategy())

	// Create input larger than BT's maxVisitedSize to trigger bidirectional DFA
	// Typical maxVisitedSize is ~32MB but depends on pattern states.
	// Use moderate input with many matches to exercise the At path.
	size := 10000
	input := strings.Repeat("abcdefgh ", size)

	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count(large) = %d, stdlib = %d", count, stdCount)
	}

	// FindAll on moderately large input
	results := engine.FindAllIndicesStreaming([]byte(input), 10, nil)
	if len(results) != 10 {
		t.Errorf("FindAll(limit=10): got %d, want 10", len(results))
	}
}

// TestBidirectionalDFA_FindIndices_EndAtBoundary exercises the end == at boundary
// case in findIndicesBidirectionalDFA (line where forward DFA returns at position).
func TestBidirectionalDFA_FindIndices_EndAtBoundary(t *testing.T) {
	// Pattern that uses BT strategy
	pattern := `\w+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// Input with matches at different positions
	input := "one two three four five"

	// Test FindIndicesAt at various positions
	for at := 0; at < len(input); at++ {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("at=%d: unexpected (%d,%d)", at, s, e)
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

// TestBTAt_LargeInputCannotHandle exercises the BT overflow path where
// CanHandle returns false and we fall back to bidirectional DFA or PikeVM.
func TestBTAt_LargeInputCannotHandle(t *testing.T) {
	// Pattern with BT strategy and dot (for ASCII optimization)
	pattern := `^/.*\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)
	t.Logf("Strategy: %s", engine.Strategy())

	// Generate a large input that may exceed BT capacity
	// BT capacity depends on pattern states * input length.
	// With ~14-39 states, inputs > ~1MB may overflow.
	longPath := "/" + strings.Repeat("subdir/", 200) + "index.php"
	got := engine.IsMatch([]byte(longPath))
	want := re.MatchString(longPath)
	if got != want {
		t.Errorf("IsMatch(long) = %v, stdlib = %v", got, want)
	}

	// FindIndices on large input
	s, e, found := engine.FindIndices([]byte(longPath))
	stdLoc := re.FindStringIndex(longPath)
	if stdLoc == nil {
		if found {
			t.Errorf("FindIndices: unexpected (%d,%d)", s, e)
		}
	} else if !found {
		t.Errorf("FindIndices: not found, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
	}
}

// TestBTAt_IsMatchDigitPrefilter_DFAPath exercises isMatchDigitPrefilter's DFA
// verification path. isMatchDigitPrefilter is at 72.2%.
func TestBTAt_IsMatchDigitPrefilter_DFAPath(t *testing.T) {
	pattern := `\d+-\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseDigitPrefilter {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"match", "abc 123-456 xyz"},
		{"no match digits only", "123 456 789"},
		{"no match", "abcdef"},
		{"empty", ""},
		{"multiple matches", "1-2 3-4 5-6"},
		{"long input", strings.Repeat("abc ", 500) + "99-11"},
		{"digits no hyphen", strings.Repeat("9", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v", got, want)
			}
		})
	}
}

// TestFindTeddy_LiteralLenFallback exercises the LiteralLen == 0 fallback
// in findTeddy (line 605-610). When prefilter has no uniform literal length,
// it falls back to NFA for verification.
func TestFindTeddy_LiteralLenFallback(t *testing.T) {
	// Use patterns with mixed-length literals to get LiteralLen() == 0
	// 3 patterns of equal length triggers FindMatch interface, so we need
	// patterns where FindMatch is NOT available but LiteralLen is 0.
	patterns := []string{
		`alpha|bravo|charlie|delta|echo`,
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pattern, err)
		}

		re := regexp.MustCompile(pattern)
		t.Logf("Strategy for %q: %s", pattern, engine.Strategy())

		tests := []struct {
			name  string
			input string
		}{
			{"match first", "alpha rest"},
			{"match last", "rest echo"},
			{"no match", "kilo lima"},
			{"empty", ""},
			{"multiple", "alpha bravo charlie"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				match := engine.Find([]byte(tt.input))
				stdLoc := re.FindStringIndex(tt.input)
				switch {
				case stdLoc == nil && match != nil:
					t.Errorf("Find: got %q, want nil", match.String())
				case stdLoc != nil && match == nil:
					t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
				case stdLoc != nil && match != nil && (match.Start() != stdLoc[0] || match.End() != stdLoc[1]):
					t.Errorf("Find: got [%d,%d], want [%d,%d]",
						match.Start(), match.End(), stdLoc[0], stdLoc[1])
				}

				// FindIndices
				s, e, found := engine.FindIndices([]byte(tt.input))
				if stdLoc == nil {
					if found {
						t.Errorf("FindIndices: unexpected (%d,%d)", s, e)
					}
				} else if !found || s != stdLoc[0] || e != stdLoc[1] {
					t.Errorf("FindIndices: got (%d,%d,%v), want (%d,%d,true)",
						s, e, found, stdLoc[0], stdLoc[1])
				}

				// Count
				count := engine.Count([]byte(tt.input), -1)
				stdCount := len(re.FindAllString(tt.input, -1))
				if count != stdCount {
					t.Errorf("Count = %d, stdlib = %d", count, stdCount)
				}
			})
		}
	}
}

// TestReverseInner_PrefixSuffixFail exercises the ReverseInner strategy
// when suffix or prefix DFA verification fails, covering the continue paths.
// reverse_inner.go Find is at 74.4% and FindIndicesAt at 74.2%.
func TestReverseInner_PrefixSuffixFail(t *testing.T) {
	// Pattern where inner literal exists but prefix/suffix may fail
	pattern := `.*error.*timeout.*`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseInner {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"full match", "an error caused timeout here"},
		{"no timeout", "an error occurred but recovered"},
		{"no error", "a timeout happened without warning"},
		{"both present", "error: connection timeout"},
		{"multiple errors", "error one then error timeout finally"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find
			match := engine.Find([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if stdLoc == nil {
				if match != nil {
					t.Errorf("Find: got %q, want nil", match.String())
				}
			} else if match == nil {
				t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
			}

			// IsMatch
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v", got, want)
			}
		})
	}
}

// TestReverseInner_FindIndicesAt_Loop exercises FindIndicesAt at various positions
// for ReverseInner, covering the DFA search loop with multiple candidates.
func TestReverseInner_FindIndicesAt_Loop(t *testing.T) {
	pattern := `.*warning.*`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseInner {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "no warning here then another warning later"

	// Exercise FindIndicesAt at every position
	for at := 0; at < len(input); at += 3 {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("at=%d: unexpected (%d,%d)", at, s, e)
			}
		} else if !found {
			t.Errorf("at=%d: not found, stdlib (%d,%d)", at, stdLoc[0]+at, stdLoc[1]+at)
		}
	}

	// Count
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}
}

// TestMultilineReverseSuffix_VerifyPrefix exercises the verifyPrefix path.
// verifyPrefix is at 60.0%.
func TestMultilineReverseSuffix_VerifyPrefix_Extended(t *testing.T) {
	// Pattern with prefix literal that enables fast-path verification
	pattern := `(?m)^/.*\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseMultilineReverseSuffix {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"single line match", "/index.php"},
		{"multi line first", "/index.php\n/admin.php\n/api.php"},
		{"multi line middle", "readme.txt\n/page.php\nother.txt"},
		{"no match", "readme.txt\nindex.html"},
		{"empty", ""},
		{"prefix fail", "index.php\npage.php"},
		{"long lines", strings.Repeat("x", 200) + "\n/" + strings.Repeat("y", 100) + ".php"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find
			match := engine.Find([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if stdLoc == nil {
				if match != nil {
					t.Errorf("Find: got %q, want nil", match.String())
				}
			} else if match == nil {
				t.Errorf("Find: got nil, stdlib [%d,%d]=%q",
					stdLoc[0], stdLoc[1], tt.input[stdLoc[0]:stdLoc[1]])
			}

			// IsMatch
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v", got, want)
			}

			// Count
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}

			// FindIndicesAt at various positions
			for at := 0; at < len(tt.input) && at < 30; at += 5 {
				s, e, found := engine.FindIndicesAt([]byte(tt.input), at)
				stdLoc := re.FindIndex([]byte(tt.input)[at:])
				if stdLoc == nil {
					if found {
						t.Errorf("FindIndicesAt(%d): unexpected (%d,%d)", at, s, e)
					}
				} else if !found {
					t.Errorf("FindIndicesAt(%d): not found", at)
				}
			}
		})
	}
}

// TestFindIndicesBoundedBacktrackerAt_PikeVMFallback exercises the PikeVM
// fallback when BT can't handle the input and no bidirectional DFA is available.
func TestFindIndicesBoundedBacktrackerAt_PikeVMFallback(t *testing.T) {
	// Simple BT patterns -- test with FindIndicesAt at various positions
	patterns := []struct {
		pattern string
		input   string
	}{
		{`\d+`, "abc 123 def 456"},
		{`\w+`, "hello world foo bar"},
		{`[a-z]+`, "ABC abc DEF def"},
	}

	for _, pp := range patterns {
		engine, err := Compile(pp.pattern)
		if err != nil {
			t.Fatal(err)
		}
		re := regexp.MustCompile(pp.pattern)

		// FindIndicesAt at every position
		input := []byte(pp.input)
		for at := 0; at < len(input); at++ {
			s, e, found := engine.FindIndicesAt(input, at)
			stdLoc := re.FindIndex(input[at:])
			if stdLoc == nil {
				if found {
					t.Errorf("%s at=%d: unexpected (%d,%d)", pp.pattern, at, s, e)
				}
			} else {
				stdStart := stdLoc[0] + at
				stdEnd := stdLoc[1] + at
				if !found || s != stdStart || e != stdEnd {
					t.Errorf("%s at=%d: got (%d,%d,%v), want (%d,%d,true)",
						pp.pattern, at, s, e, found, stdStart, stdEnd)
				}
			}
		}
	}
}
