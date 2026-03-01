package meta

// Tests for BoundedBacktracker paths including ASCII optimization,
// windowed fallback, large input fallback, and bidirectional DFA.

import (
	"regexp"
	"strings"
	"testing"
)

func TestFindIndicesBidirectionalDFA(t *testing.T) {
	// (\w{2,8})+ uses BoundedBacktracker which overflows on large input,
	// triggering the bidirectional DFA fallback path.
	// BT can handle up to ~958K, so we need >1MB to trigger overflow.
	pattern := `(\w{2,8})+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// Small input: within BT capacity
	small := []byte("hello world")
	s, e, found := engine.FindIndices(small)
	stdLoc := re.FindIndex(small)
	if found && stdLoc != nil {
		if s != stdLoc[0] || e != stdLoc[1] {
			t.Errorf("small: got (%d,%d), stdlib (%d,%d)", s, e, stdLoc[0], stdLoc[1])
		}
	}

	// Large input >1MB to exceed BT capacity and trigger bidirectional DFA fallback
	largeSize := 2_000_000 // 2MB exceeds BT's ~958K limit
	largeInput := []byte(strings.Repeat("abcdefgh", largeSize/8))

	s2, e2, found2 := engine.FindIndices(largeInput)
	if !found2 {
		t.Fatal("FindIndices on large input should find a match")
	}
	if s2 != 0 {
		t.Errorf("large: start=%d, want 0", s2)
	}
	if e2 != len(largeInput) {
		t.Errorf("large: end=%d, want %d", e2, len(largeInput))
	}

	// FindIndicesAt at non-zero on large input
	s3, e3, found3 := engine.FindIndicesAt(largeInput, 10)
	if found3 {
		t.Logf("FindIndicesAt(10) = (%d,%d)", s3, e3)
	}
}

// TestWave4_BidirectionalDFA_MultipleMatches exercises the bidirectional DFA
// during FindAll where BT can't handle the full input.

func TestBidirectionalDFA_MultipleMatches(t *testing.T) {
	pattern := `(\w{2,8})+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// Multiple large words separated by spaces
	word := strings.Repeat("abcd", 50000)
	input := word + " " + word + " " + word
	inputBytes := []byte(input)

	ourCount := engine.Count(inputBytes, -1)
	stdCount := len(re.FindAllIndex(inputBytes, -1))

	if ourCount != stdCount {
		t.Errorf("Count = %d, stdlib = %d", ourCount, stdCount)
	}
}

// -----------------------------------------------------------------------------
// 5. ReverseSuffix.FindAt (reverse_suffix.go:211) — FindAt at non-zero position
//    Exercised through FindAll which uses FindIndicesAt (not FindAt directly).
//    To cover reverse_suffix.go:211, we use Count which exercises the internal loop.
// -----------------------------------------------------------------------------

func TestBoundedBacktrackerAt_ASCIIOptimization(t *testing.T) {
	// Pattern with dot (.) triggers ASCII optimization when available
	pattern := `^(.+)@(.+)\.(.+)$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)
	tests := []struct {
		name  string
		input string
	}{
		{"ascii_email", "user@example.com"},
		{"no_match", "not an email"},
		{"empty", ""},
		{"short", "@"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.input))
			stdGot := re.MatchString(tt.input)
			if got != stdGot {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input, got, stdGot)
			}
		})
	}
}

func TestBoundedBacktrackerAt_LargeInputFallback(t *testing.T) {
	pattern := `(\w+)\s+(\w+)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// Small input
	small := []byte("hello world")
	s, e, found := engine.FindIndices(small)
	stdLoc := re.FindIndex(small)
	if found && stdLoc != nil {
		if s != stdLoc[0] || e != stdLoc[1] {
			t.Errorf("small: got (%d,%d), stdlib (%d,%d)", s, e, stdLoc[0], stdLoc[1])
		}
	}

	// Large input
	largeInput := []byte(strings.Repeat("word ", 100000))
	s2, e2, found2 := engine.FindIndices(largeInput)
	if !found2 {
		t.Fatal("should find match on large input")
	}
	t.Logf("large: first match [%d,%d]", s2, e2)

	// Count limited to avoid slow test
	count := engine.Count(largeInput, 10)
	if count < 1 {
		t.Error("should find at least 1 match")
	}
}

func TestBoundedBacktrackerAtWithState_WindowedFallback(t *testing.T) {
	pattern := `(\w{2,8})+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	largeInput := []byte(strings.Repeat("abcdef ", 100000))

	count := engine.Count(largeInput, 50)
	stdMatches := re.FindAllIndex(largeInput, 50)
	if count != len(stdMatches) {
		t.Errorf("Count(50) = %d, stdlib = %d", count, len(stdMatches))
	}
}

// -----------------------------------------------------------------------------
// 10. Teddy FindAt (find.go:614) — at 38.1% coverage
// -----------------------------------------------------------------------------

func TestBoundedBacktrackerAt_Detailed(t *testing.T) {
	// BoundedBacktracker with captures at various positions
	pattern := `^(\w+)\s+(\w+)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)
	tests := []struct {
		name  string
		input string
	}{
		{"simple", "hello world"},
		{"with_extra", "hello world extra"},
		{"no_match", "123"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// FindIndices
			s, e, found := engine.FindIndices([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if found != (stdLoc != nil) {
				t.Errorf("found=%v, stdlib=%v", found, stdLoc != nil)
			}
			if found && stdLoc != nil {
				if s != stdLoc[0] || e != stdLoc[1] {
					t.Errorf("got (%d,%d), stdlib (%d,%d)", s, e, stdLoc[0], stdLoc[1])
				}
			}

			// FindSubmatch
			sm := engine.FindSubmatch([]byte(tt.input))
			if sm != nil {
				t.Logf("FindSubmatch: %q groups=%d", sm.String(), sm.NumCaptures())
			}
		})
	}
}

// TestWave4_BoundedBacktracker_FindSubmatch_Large exercises BT overflow in FindSubmatch.

func TestBoundedBacktracker_FindSubmatch_Large(t *testing.T) {
	pattern := `(\w{2,8})+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	// Small input: within BT capacity
	small := []byte("hello world")
	sm := engine.FindSubmatch(small)
	if sm == nil {
		t.Fatal("FindSubmatch should find match on small input")
	}
	t.Logf("small: %q, captures=%d", sm.String(), sm.NumCaptures())

	// Large input: exceeds BT capacity, triggers fallback
	largeInput := []byte(strings.Repeat("abcdef", 100000))
	sm2 := engine.FindSubmatch(largeInput)
	if sm2 == nil {
		t.Fatal("FindSubmatch should find match on large input")
	}
	t.Logf("large: len=%d", sm2.End()-sm2.Start())
}

// -----------------------------------------------------------------------------
// 20. findIndicesDFA (57.1%) and findIndicesAdaptive (36.7%)
//     Exercise prefilter+DFA and DFA-only paths in FindIndices.
// -----------------------------------------------------------------------------

func TestBoundedBacktrackerAtWithState_AnchoredFirstBytes(t *testing.T) {
	// Pattern: ^/.*\.php — start-anchored with first byte '/'
	// The anchoredFirstBytes optimization rejects inputs not starting with '/'
	engine, err := Compile(`^/.*\.php`)
	if err != nil {
		t.Fatal(err)
	}

	// Count exercises findIndicesBoundedBacktrackerAtWithState via FindAll loop
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"starts_with_slash", "/index.php", 1},
		{"no_slash", "index.php", 0},          // rejected by anchoredFirstBytes
		{"empty", "", 0},                      // empty input
		{"slash_no_php", "/index.html", 0},    // starts with / but no .php
		{"multiple_slashes", "//test.php", 1}, // starts with /
	}

	re := regexp.MustCompile(`^/.*\.php`)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Count([]byte(tt.input), -1)
			stdGot := len(re.FindAllString(tt.input, -1))
			if got != stdGot {
				t.Errorf("Count(%q) = %d, stdlib = %d", tt.input, got, stdGot)
			}
		})
	}
}

func TestBoundedBacktrackerAtWithState_WindowedFallback_Extended(t *testing.T) {
	// Exercise the V12 windowed BoundedBacktracker fallback path.
	// This triggers when BT cannot handle the remaining input but DFA+reverseDFA are unavailable.
	// Pattern: (\w)+ — uses BoundedBacktracker, has captures so no DFA.
	pattern := `(\w)+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s, not UseBoundedBacktracker", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Small input — BT handles directly
	small := "hello world test"
	got := engine.Count([]byte(small), -1)
	stdGot := len(re.FindAllString(small, -1))
	if got != stdGot {
		t.Errorf("Count small: got %d, stdlib %d", got, stdGot)
	}

	// Medium input with many matches — exercises the state-reusing loop
	medium := strings.Repeat("abc ", 500)
	got2 := engine.Count([]byte(medium), -1)
	stdGot2 := len(re.FindAllString(medium, -1))
	if got2 != stdGot2 {
		t.Errorf("Count medium: got %d, stdlib %d", got2, stdGot2)
	}

	// FindAll with limit
	indices := engine.FindAllIndicesStreaming([]byte(medium), 10, nil)
	if len(indices) != 10 {
		t.Errorf("FindAll with limit 10: got %d", len(indices))
	}
}

func TestBoundedBacktrackerAtWithState_ASCIIPath(t *testing.T) {
	// Exercise the ASCII optimization path in findIndicesBoundedBacktrackerAtWithState.
	// Pattern with '.' — triggers ASCII BT creation.
	pattern := `^/.*\.txt`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// ASCII input — should use ASCII BT
	asciiInput := "/path/to/file.txt"
	got := engine.IsMatch([]byte(asciiInput))
	stdGot := re.MatchString(asciiInput)
	if got != stdGot {
		t.Errorf("IsMatch ASCII %q: got %v, stdlib %v", asciiInput, got, stdGot)
	}

	// Count exercises the state loop
	count := engine.Count([]byte(asciiInput), -1)
	stdCount := len(re.FindAllString(asciiInput, -1))
	if count != stdCount {
		t.Errorf("Count ASCII: got %d, stdlib %d", count, stdCount)
	}
}

// -----------------------------------------------------------------------------
// 29. isMatchAdaptive (44.4%) — exercise DFA and cache-full branches.
//     isMatchAdaptive is called for UseBoth strategy.
// -----------------------------------------------------------------------------

func TestBoundedBacktracker_Find_Various(t *testing.T) {
	pattern := `^([a-z]+)(\d+)$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"match", "hello123", true},
		{"no_match_upper", "Hello123", false},
		{"no_match_no_digits", "hello", false},
		{"empty", "", false},
		{"only_digits", "123", false},
		{"long_match", strings.Repeat("x", 100) + "999", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.input))
			stdGot := re.MatchString(tt.input)
			if got != stdGot {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input[:minInt(len(tt.input), 20)], got, stdGot)
			}

			match := engine.Find([]byte(tt.input))
			stdMatch := re.FindString(tt.input)
			gotStr := ""
			if match != nil {
				gotStr = match.String()
			}
			if gotStr != stdMatch {
				t.Errorf("Find(%q) = %q, stdlib = %q", tt.input[:minInt(len(tt.input), 20)], gotStr, stdMatch)
			}

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
// 51. Exercise findAdaptive (36.1%) via DFA match + NFA fallback.
//     The DFA-only path (no prefilter) with match found.
// -----------------------------------------------------------------------------

func TestIsMatchBoundedBacktracker_AnchoredSuffix(t *testing.T) {
	// Pattern: ^/.*\.php — has anchoredSuffix ".php"
	pattern := `^/.*\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		input string
		want  bool
	}{
		{"/index.php", true},
		{"/admin/page.php", true},
		{"/index.html", false},                          // rejected by anchoredSuffix
		{"index.php", false},                            // rejected by anchoredFirstBytes (no /)
		{"", false},                                     // empty
		{"/test.PHP", false},                            // case sensitive
		{"/" + strings.Repeat("x", 500) + ".php", true}, // medium input
	}

	for _, tt := range tests {
		got := engine.IsMatch([]byte(tt.input))
		stdGot := re.MatchString(tt.input)
		if got != stdGot {
			t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input[:minInt(len(tt.input), 30)], got, stdGot)
		}
	}
}

func TestIsMatchBoundedBacktracker_ASCIIOptimization(t *testing.T) {
	// Pattern with '.' — triggers ASCII BT creation
	pattern := `^/.+\.txt`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// ASCII input — uses ASCII BT
	asciiTests := []struct {
		input string
		want  bool
	}{
		{"/file.txt", true},
		{"/path/to/file.txt", true},
		{"/no-ext", false},
		{"", false},
		{"/a.txt", true},
	}

	for _, tt := range asciiTests {
		got := engine.IsMatch([]byte(tt.input))
		stdGot := re.MatchString(tt.input)
		if got != stdGot {
			t.Errorf("IsMatch ASCII(%q) = %v, stdlib = %v", tt.input, got, stdGot)
		}
	}
}

// -----------------------------------------------------------------------------
// 55. isMatchCompositeSearcher — exercise DFA path.
// -----------------------------------------------------------------------------

func TestFindIndicesBoundedBacktrackerAt_ASCII_Various(t *testing.T) {
	// Pattern that creates ASCII BT: has '.' wildcard
	pattern := `(\w)+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	// Exercise the At path via Count on various sized ASCII inputs
	sizes := []int{10, 100, 1000, 5000}
	for _, size := range sizes {
		input := strings.Repeat("abc ", size/4)
		count := engine.Count([]byte(input), -1)
		stdCount := len(re.FindAllString(input, -1))
		if count != stdCount {
			t.Errorf("Count (size=%d): got %d, stdlib %d", size, count, stdCount)
		}
	}
}

func TestFindIndicesBoundedBacktrackerAt_AnchoredPattern(t *testing.T) {
	// Start-anchored pattern exercises the isStartAnchored branch
	// in findIndicesBoundedBacktrackerAtWithState
	pattern := `^(\d+)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"match_start", "12345abc", 1},
		{"no_match", "abc123", 0},
		{"empty", "", 0},
		{"only_digits", "99999", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 57. isMatchReverseSuffix / isMatchReverseAnchored / isMatchMultiline
//     exercise IsMatch on various reverse strategies to increase coverage.
// -----------------------------------------------------------------------------

func TestFindIndicesBoundedBacktrackerAt_ASCIIPaths(t *testing.T) {
	// Pattern with BoundedBacktracker: (\w+)
	pattern := `(\w+)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("Strategy is %s, not UseBoundedBacktracker", engine.Strategy())
	}

	// ASCII input at non-zero position
	input := "   hello world"
	s, e, found := engine.FindIndicesAt([]byte(input), 3)
	if !found {
		t.Error("expected match")
	} else if string(input[s:e]) != "hello" {
		t.Errorf("got %q, want 'hello'", input[s:e])
	}

	// Multiple matches using FindAll to exercise findIndicesBoundedBacktrackerAt
	results := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	re := regexp.MustCompile(pattern)
	stdAll := re.FindAllStringIndex(input, -1)
	if len(results) != len(stdAll) {
		t.Errorf("count: coregex=%d, stdlib=%d", len(results), len(stdAll))
	}
}

// --- Test 97: findIndicesCompositeSearcherAt with compositeSequenceDFA ---
// Covers: find_indices.go findIndicesCompositeSearcherAt lines 626-637
// Targets: compositeSequenceDFA != nil path, compositeSearcher fallback
