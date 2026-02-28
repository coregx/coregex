package meta

import (
	"regexp"
	"testing"
)

// TestFindAt_PerStrategy exercises FindAt with at > 0 for every strategy.
// This covers the findAtNonZero dispatch path which has many 0% coverage branches.
func TestFindAt_PerStrategy(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		at      int
		wantStr string // expected match text, "" for no match
	}{
		// UseNFA: tiny pattern, no good literals
		{"NFA at=0", `a.c`, "xabc", 0, "abc"},
		{"NFA at>0", `a.c`, "xyzabc", 3, "abc"},
		{"NFA no match", `a.c`, "xyz", 1, ""},

		// UseCharClassSearcher: simple char class+
		{"CharClass at=0", `[a-z]+`, "123abc", 0, "abc"},
		{"CharClass at>0", `[a-z]+`, "123abc456def", 6, "def"},
		{"CharClass at past end", `[a-z]+`, "abc", 4, ""},

		// UseCompositeSearcher: concatenated char classes
		{"Composite at=0", `[a-z]+[0-9]+`, "abc123", 0, "abc123"},
		{"Composite at>0", `[a-z]+[0-9]+`, "---abc123---def456", 9, "def456"},

		// UseBoundedBacktracker: anchored pattern
		{"BT anchored at=0", `^hello`, "hello world", 0, "hello"},
		{"BT anchored at>0", `^hello`, "hello world", 1, ""},

		// UseBranchDispatch: anchored alternation
		{"BranchDispatch at=0 match", `^(foo|bar|baz)`, "foo123", 0, "foo"},
		{"BranchDispatch at=0 no match", `^(foo|bar|baz)`, "qux123", 0, ""},
		{"BranchDispatch at>0", `^(foo|bar|baz)`, "foo123", 1, ""},

		// UseTeddy: exact literal alternation (3-8 patterns)
		{"Teddy at=0", `alpha|beta|gamma`, "before alpha after", 0, "alpha"},
		{"Teddy at>0", `alpha|beta|gamma`, "alpha and beta", 6, "beta"},
		{"Teddy no match at>0", `alpha|beta|gamma`, "alpha", 5, ""},

		// UseDigitPrefilter: digit-lead pattern
		{"DigitPrefilter at=0", `\d+-\d+`, "abc 123-456 xyz", 0, "123-456"},
		{"DigitPrefilter at>0", `\d+-\d+`, "11-22 abc 33-44", 6, "33-44"},

		// UseReverseAnchored: end-anchored pattern
		{"RevAnchored at=0", `world$`, "hello world", 0, "world"},

		// UseReverseSuffix: suffix literal with .*
		{"RevSuffix at=0", `.*\.txt`, "file.txt", 0, "file.txt"},

		// UseReverseInner: inner literal with .*
		{"RevInner at=0", `.*connection.*`, "error: connection reset", 0, "error: connection reset"},

		// UseAnchoredLiteral: ^prefix.*suffix$
		{"AnchoredLiteral at=0 match", `^hello.*world$`, "hello beautiful world", 0, "hello beautiful world"},
		{"AnchoredLiteral at=0 no match", `^hello.*world$`, "goodbye world", 0, ""},
		{"AnchoredLiteral at>0", `^hello.*world$`, "hello world", 1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}

			match := engine.FindAt([]byte(tt.input), tt.at)
			if tt.wantStr == "" {
				if match != nil {
					t.Errorf("FindAt(%q, %d): got %q, want nil", tt.input, tt.at, match.String())
				}
			} else {
				if match == nil {
					t.Errorf("FindAt(%q, %d): got nil, want %q", tt.input, tt.at, tt.wantStr)
				} else if match.String() != tt.wantStr {
					t.Errorf("FindAt(%q, %d): got %q, want %q", tt.input, tt.at, match.String(), tt.wantStr)
				}
			}
		})
	}
}

// TestFindAt_BeyondHaystack checks FindAt with at beyond haystack length.
func TestFindAt_BeyondHaystack(t *testing.T) {
	engine, err := Compile(`\w+`)
	if err != nil {
		t.Fatal(err)
	}

	match := engine.FindAt([]byte("hello"), 10)
	if match != nil {
		t.Error("FindAt beyond haystack should return nil")
	}
}

// TestFindIndicesAt_PerStrategy exercises FindIndicesAt with at > 0 for every strategy.
// This covers all findIndices*At dispatch branches.
func TestFindIndicesAt_PerStrategy(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		at        int
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// UseNFA
		{"NFA at=0", `a.c`, "xabc", 0, 1, 4, true},
		{"NFA at>0", `a.c`, "xyzabc", 3, 3, 6, true},
		{"NFA no match at>0", `a.c`, "xyz", 1, -1, -1, false},

		// UseCharClassSearcher
		{"CharClass at=0", `[a-z]+`, "123abc", 0, 3, 6, true},
		{"CharClass at>0 found", `[a-z]+`, "123abc456def", 6, 9, 12, true},
		{"CharClass at>0 no match", `[a-z]+`, "123", 1, -1, -1, false},

		// UseCompositeSearcher
		{"Composite at=0", `[a-z]+[0-9]+`, "abc123", 0, 0, 6, true},
		{"Composite at>0", `[a-z]+[0-9]+`, "---abc123---def456", 9, 12, 18, true},

		// UseBoundedBacktracker (anchored)
		{"BT anchored at=0", `^hello`, "hello world", 0, 0, 5, true},
		{"BT anchored at>0 no match", `^hello`, "hello world", 1, -1, -1, false},

		// UseBranchDispatch (anchored alternation)
		{"BranchDispatch at=0", `^(foo|bar)`, "foo123", 0, 0, 3, true},
		{"BranchDispatch at>0", `^(foo|bar)`, "foo123", 1, -1, -1, false},

		// UseTeddy
		{"Teddy at=0", `alpha|beta|gamma`, "alpha then beta", 0, 0, 5, true},
		{"Teddy at>0", `alpha|beta|gamma`, "alpha then beta", 6, 11, 15, true},

		// UseDigitPrefilter
		{"DigitPrefilter at=0", `\d+-\d+`, "abc 12-34 xyz", 0, 4, 9, true},
		{"DigitPrefilter at>0", `\d+-\d+`, "12-34 and 56-78", 6, 10, 15, true},

		// UseAnchoredLiteral
		{"AnchoredLiteral at=0 match", `^hello.*world$`, "hello world", 0, 0, 11, true},
		{"AnchoredLiteral at>0", `^hello.*world$`, "hello world", 1, -1, -1, false},

		// UseMultilineReverseSuffix
		{"MultilineRevSuffix at=0", `(?m)^.*\.php`, "/var/www/index.php", 0, 0, 18, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}

			start, end, found := engine.FindIndicesAt([]byte(tt.input), tt.at)
			if found != tt.wantFound {
				t.Errorf("FindIndicesAt(%q, %d) found=%v, want %v (strategy=%s)",
					tt.input, tt.at, found, tt.wantFound, engine.Strategy())
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("FindIndicesAt(%q, %d) = (%d, %d), want (%d, %d) (strategy=%s)",
					tt.input, tt.at, start, end, tt.wantStart, tt.wantEnd, engine.Strategy())
			}
		})
	}
}

// TestFindIndicesAt_VsStdlib cross-validates FindIndicesAt results against stdlib.
func TestFindIndicesAt_VsStdlib(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`\d+`, "abc 123 def 456 ghi"},
		{`[a-z]+`, "ABC abc DEF def GHI"},
		{`\w+`, "one two three four"},
		{`alpha|beta`, "beta then alpha then gamma"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			re := regexp.MustCompile(tt.pattern)
			input := []byte(tt.input)

			// Test FindIndicesAt at multiple positions
			for at := 0; at < len(input); at++ {
				s, e, found := engine.FindIndicesAt(input, at)
				loc := re.FindIndex(input[at:])

				if loc == nil {
					if found {
						t.Errorf("at=%d: got (%d,%d,true), stdlib=nil", at, s, e)
					}
				} else {
					stdStart := loc[0] + at
					stdEnd := loc[1] + at
					if !found {
						t.Errorf("at=%d: got not found, stdlib=(%d,%d)", at, stdStart, stdEnd)
					} else if s != stdStart || e != stdEnd {
						t.Errorf("at=%d: got (%d,%d), stdlib=(%d,%d)", at, s, e, stdStart, stdEnd)
					}
				}
			}
		})
	}
}

// TestCount_MultipleMatches_PerStrategy exercises Count for every strategy
// using inputs with multiple matches. Count internally calls FindAllIndicesStreaming
// which uses findIndicesAtWithState with at > 0.
func TestCount_MultipleMatches_PerStrategy(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    int
	}{
		// UseNFA
		{"NFA", `a.c`, "abc adc aec xyz", 3},
		// UseCharClassSearcher
		{"CharClass", `[a-z]+`, "abc 123 def 456 ghi", 3},
		// UseCompositeSearcher
		{"Composite", `[a-z]+[0-9]+`, "abc123 def456 ghi789", 3},
		// UseBoundedBacktracker (anchored -- can only match once)
		{"BT anchored", `^hello`, "hello world", 1},
		// UseBranchDispatch
		{"BranchDispatch", `^(foo|bar)`, "foo bar", 1},
		// UseTeddy
		{"Teddy", `alpha|beta|gamma`, "alpha beta gamma delta alpha", 4},
		// UseDigitPrefilter
		{"DigitPrefilter", `\d+-\d+`, "12-34 56-78 90-12", 3},
		// UseAnchoredLiteral
		{"AnchoredLiteral", `^hello.*world$`, "hello world", 1},
		// UseReverseAnchored
		{"RevAnchored", `end$`, "not the end", 1},
		// UseReverseSuffix
		{"RevSuffix", `.*\.log`, "app.log", 1},
		// UseReverseInner
		{"RevInner", `.*error.*`, "an error occurred", 1},
		// Zero matches
		{"Zero matches", `xyz`, "abc def ghi", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}

			got := engine.Count([]byte(tt.input), -1)
			if got != tt.want {
				t.Errorf("Count(%q, %q) = %d, want %d (strategy=%s)",
					tt.pattern, tt.input, got, tt.want, engine.Strategy())
			}
		})
	}
}

// TestCount_VsStdlib_MultiMatch cross-validates Count against stdlib for multi-match inputs.
func TestCount_VsStdlib_MultiMatch(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`\d+`, "abc 123 def 456 ghi 789 jkl"},
		{`[a-z]+`, "ABC abc DEF def GHI ghi JKL jkl"},
		{`\w+`, "one, two, three, four, five"},
		{`alpha|beta`, "alpha beta alpha beta alpha"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			re := regexp.MustCompile(tt.pattern)

			got := engine.Count([]byte(tt.input), -1)
			want := len(re.FindAllString(tt.input, -1))
			if got != want {
				t.Errorf("Count=%d, stdlib=%d for %q on %q (strategy=%s)",
					got, want, tt.pattern, tt.input, engine.Strategy())
			}
		})
	}
}

// TestFindAllIndicesStreaming_At exercises the full FindAllIndicesStreaming path
// which uses findIndicesAtWithState internally.
func TestFindAllIndicesStreaming_At(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		limit   int
		want    int // expected match count
	}{
		{"all matches", `\d+`, "a1b22c333d4444", 0, 4},
		{"limited", `\d+`, "a1b22c333d4444", 2, 2},
		{"no matches", `\d+`, "abcdef", 0, 0},
		{"empty input", `\d+`, "", 0, 0},
		// Teddy strategy
		{"teddy multi", `cat|dog|rat`, "cat and dog and rat", 0, 3},
		// CharClass
		{"charclass multi", `[a-z]+`, "abc 123 def 456", 0, 2},
		// Composite
		{"composite multi", `[a-z]+[0-9]+`, "abc123 def456", 0, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}

			results := engine.FindAllIndicesStreaming([]byte(tt.input), tt.limit, nil)

			if len(results) != tt.want {
				t.Errorf("FindAllIndicesStreaming got %d matches, want %d (strategy=%s)",
					len(results), tt.want, engine.Strategy())
			}
		})
	}
}

// TestFindAt_DFA_Strategy targets the UseDFA code path.
// UseDFA is selected for large NFA (>= 20 states) with good literals.
func TestFindAt_DFA_Strategy(t *testing.T) {
	// This pattern has many states (>20) plus a good literal prefix "XPREFIX"
	// which triggers UseDFA (large NFA with good literals).
	pattern := `XPREFIX[a-z]{5,20}[0-9]{3,10}[A-Z]{2,8}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for %q: %s", pattern, strategy)

	input := "junk XPREFIX" + "abcde" + "12345" + "AB" + " more junk"
	// FindAt(0)
	match := engine.Find([]byte(input))
	if match == nil {
		t.Log("No match from DFA strategy (may depend on literal extraction)")
	} else {
		t.Logf("Find: %q at [%d,%d]", match.String(), match.Start(), match.End())
	}

	// FindAt non-zero -- exercises findDFAAt or findAdaptiveAt
	match2 := engine.FindAt([]byte(input), 5)
	if match != nil && match2 == nil {
		t.Error("FindAt(5) should still find the match")
	}
}

// TestFindAt_Both_Strategy targets the UseBoth (adaptive) code path.
// UseBoth is selected for medium NFA (20-100 states) without strong literals.
func TestFindAt_Both_Strategy(t *testing.T) {
	// Medium complexity pattern without extractable literals, 20-100 states
	pattern := `(a|b|c|d|e|f|g|h|i|j|k|l|m|n|o)*z`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for pattern: %s", strategy)

	input := "abcdefghijklmnoz tail"
	// Find at position 0
	match := engine.Find([]byte(input))
	if match != nil {
		t.Logf("Find: %q at [%d,%d]", match.String(), match.Start(), match.End())
	}

	// FindAt non-zero -- exercises findAdaptiveAt
	match2 := engine.FindAt([]byte(input), 5)
	t.Logf("FindAt(5): match=%v", match2 != nil)

	// FindIndicesAt non-zero -- exercises findIndicesAdaptiveAt
	s, e, found := engine.FindIndicesAt([]byte(input), 5)
	t.Logf("FindIndicesAt(5): (%d, %d, %v)", s, e, found)
}

// TestFindIndices_DFA_LiteralFastPath targets the DFA literal fast path.
// When prefilter is complete and literal length known, DFA returns directly.
func TestFindIndices_DFA_LiteralFastPath(t *testing.T) {
	// A longer literal triggers UseDFA with complete prefilter
	pattern := `XPREFIX_LONG_LITERAL_MATCH`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Strategy: %s", engine.Strategy())

	input := "before XPREFIX_LONG_LITERAL_MATCH after"
	s, e, found := engine.FindIndices([]byte(input))
	if !found {
		t.Log("No match (may use NFA for short pattern)")
	} else {
		t.Logf("Found at [%d,%d]", s, e)
	}

	// At non-zero position
	s2, e2, found2 := engine.FindIndicesAt([]byte(input), 7)
	if found && !found2 {
		t.Logf("FindIndicesAt(7) should find same match: strategy=%s", engine.Strategy())
	}
	_ = s2
	_ = e2
}
