package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestFindIndicesCompositeSearcher_DFAPath exercises the CompositeSequenceDFA path.
// findIndicesCompositeSearcher is at 42.9% -- the DFA path (compositeSequenceDFA != nil)
// and the backtracking path (compositeSearcher) are separate branches.
func TestFindIndicesCompositeSearcher_DFAPath(t *testing.T) {
	// [a-z]+[0-9]+ triggers UseCompositeSearcher with CompositeSequenceDFA
	pattern := `[a-z]+[0-9]+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseCompositeSearcher {
		t.Skipf("Strategy is %s, not UseCompositeSearcher", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"single match", "abc123"},
		{"match in middle", "---abc123---"},
		{"multiple matches", "abc123 def456 ghi789"},
		{"no match", "abc def 123"},
		{"empty", ""},
		{"only alpha", "abcdef"},
		{"only digits", "123456"},
		{"at boundary", "a1"},
		{"long match", strings.Repeat("a", 50) + strings.Repeat("1", 50)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)

			// FindIndices (zero-alloc)
			s, e, found := engine.FindIndices(input)
			stdLoc := re.FindStringIndex(tt.input)
			if stdLoc == nil {
				if found {
					t.Errorf("FindIndices: got (%d,%d), want not found", s, e)
				}
			} else {
				if !found || s != stdLoc[0] || e != stdLoc[1] {
					t.Errorf("FindIndices: got (%d,%d,%v), want (%d,%d,true)",
						s, e, found, stdLoc[0], stdLoc[1])
				}
			}

			// Find (Match object)
			match := engine.Find(input)
			if stdLoc == nil {
				if match != nil {
					t.Errorf("Find: got %q, want nil", match.String())
				}
			} else if match == nil {
				t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
			}

			// IsMatch
			got := engine.IsMatch(input)
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v", got, want)
			}

			// Count (exercises FindAll loop)
			count := engine.Count(input, -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}
		})
	}
}

// TestFindIndicesCompositeSearcherAt_DFAPath exercises findIndicesCompositeSearcherAt
// with the CompositeSequenceDFA path.
func TestFindIndicesCompositeSearcherAt_DFAPathExtended(t *testing.T) {
	pattern := `[a-z]+[0-9]+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseCompositeSearcher {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "---abc123---def456---ghi789---"

	// FindIndicesAt at various positions
	for at := 0; at < len(input); at += 3 {
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

// TestFindIndicesBoundedBacktrackerAt_ASCIIOpt exercises the ASCII optimization
// path in findIndicesBoundedBacktrackerAt. This path is at 44.4%.
// The ASCII optimization uses asciiBoundedBacktracker when input is ASCII-only.
func TestFindIndicesBoundedBacktrackerAt_ASCIIOpt(t *testing.T) {
	// Pattern with '.' triggers ASCII optimization (dot compiles to fewer states in ASCII mode)
	pattern := `^/.*[\w-]+\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for %q: %s", pattern, strategy)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"match", "/var/www/index.php"},
		{"match nested", "/app/controllers/user-profile.php"},
		{"no match", "/var/www/index.html"},
		{"no match no slash", "index.php"},
		{"empty", ""},
		{"long path", "/" + strings.Repeat("subdir/", 50) + "page.php"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)

			// IsMatch exercises isMatchBoundedBacktracker (with ASCII optimization)
			got := engine.IsMatch(input)
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v (strategy=%s)", got, want, engine.Strategy())
			}

			// Find exercises findBoundedBacktracker
			match := engine.Find(input)
			stdLoc := re.FindStringIndex(tt.input)
			if stdLoc == nil {
				if match != nil {
					t.Errorf("Find: got %q, want nil", match.String())
				}
			} else if match == nil {
				t.Errorf("Find: got nil, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
			}

			// FindIndices
			s, e, found := engine.FindIndices(input)
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

// TestBoundedBacktrackerAt_BidirectionalDFA exercises the bidirectional DFA
// fallback path when BoundedBacktracker.CanHandle returns false for large inputs.
// findIndicesBidirectionalDFA is at 70.0%.
func TestBoundedBacktrackerAt_BidirectionalDFA(t *testing.T) {
	// Pattern that triggers UseBoundedBacktracker
	pattern := `(\w{2,8})+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for %q: %s", pattern, strategy)

	re := regexp.MustCompile(pattern)

	// Test with normal-sized input first
	input := "hello world testing"
	s, e, found := engine.FindIndices([]byte(input))
	stdLoc := re.FindStringIndex(input)
	if stdLoc == nil {
		if found {
			t.Errorf("FindIndices: unexpected (%d,%d)", s, e)
		}
	} else if !found {
		t.Errorf("FindIndices: not found, stdlib [%d,%d]", stdLoc[0], stdLoc[1])
	}

	// Count with multiple matches
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// Medium input exercises FindIndicesAt with at > 0
	medInput := strings.Repeat("abcd ", 100)
	count2 := engine.Count([]byte(medInput), -1)
	stdCount2 := len(re.FindAllString(medInput, -1))
	if count2 != stdCount2 {
		t.Errorf("Count(med) = %d, stdlib = %d", count2, stdCount2)
	}
}

// TestBoundedBacktrackerAt_WithState exercises findIndicesBoundedBacktrackerAtWithState.
// This function is at 50.0% and is used by the FindAll loop.
func TestBoundedBacktrackerAt_WithState(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		wantN   int
	}{
		// Simple BT pattern with multiple matches
		{"digit groups", `\d+`, "a1b22c333d4444", 4},
		// Word pattern
		{"words", `\w+`, "one two three", 3},
		// Anchored BT (single match only)
		{"anchored", `^\w+`, "hello world", 1},
		// Empty match handling
		{"no match", `\d+`, "abcdef", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile(tt.pattern)

			// Count exercises the full FindAll path with findIndicesAtWithState
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d (strategy=%s)", count, stdCount, engine.Strategy())
			}

			// FindAll also exercises the at > 0 path
			results := engine.FindAllIndicesStreaming([]byte(tt.input), 0, nil)
			if len(results) != tt.wantN {
				t.Errorf("FindAll got %d matches, want %d", len(results), tt.wantN)
			}
		})
	}
}

// TestBoundedBacktrackerAt_AnchoredFirstBytes exercises the O(1) early rejection
// path in findIndicesBoundedBacktracker when anchoredFirstBytes is set.
func TestBoundedBacktrackerAt_AnchoredFirstBytes(t *testing.T) {
	// Anchored pattern with specific first bytes
	pattern := `^(\d+|[A-F]+)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for %q: %s", pattern, strategy)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"digit start", "123abc", true},
		{"hex start", "ABCDEF", true},
		{"lower start", "abcdef", false},
		{"space start", " 123", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// IsMatch exercises the first-byte prefilter
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch = %v, stdlib = %v (strategy=%s)", got, want, engine.Strategy())
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

			// FindIndices
			_, _, found := engine.FindIndices([]byte(tt.input))
			if found != tt.want {
				t.Errorf("FindIndices found=%v, want %v", found, tt.want)
			}
		})
	}
}

// TestBoundedBacktrackerAt_AnchoredSuffix exercises the suffix rejection
// path in isMatchBoundedBacktracker when anchoredSuffix is set.
func TestBoundedBacktrackerAt_AnchoredSuffix(t *testing.T) {
	// Pattern with anchored suffix -- triggers suffix rejection optimization
	pattern := `^/.*\.php$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Strategy: %s", engine.Strategy())

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"match", "/index.php"},
		{"match nested", "/app/user.php"},
		{"no suffix", "/index.html"},
		{"no prefix", "index.php"},
		{"empty", ""},
		{"suffix only", ".php"},
		{"long", "/" + strings.Repeat("dir/", 20) + "file.php"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.IsMatch([]byte(tt.input))
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("IsMatch(%q) = %v, stdlib = %v", tt.input, got, want)
			}
		})
	}
}

// TestFindIndicesTeddy_FindMatch exercises the Teddy FindMatch interface path
// in findIndicesTeddy. findIndicesTeddy is at 57.1%.
func TestFindIndicesTeddy_FindMatch(t *testing.T) {
	// 3-8 exact literals trigger UseTeddy
	pattern := `delta|echo|foxtrot`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseTeddy {
		t.Skipf("Strategy is %s, not UseTeddy", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"single match", "before delta after"},
		{"multiple", "delta echo foxtrot"},
		{"no match", "alpha bravo charlie"},
		{"empty", ""},
		{"at end", "data foxtrot"},
		{"at start", "echo data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// FindIndices exercises findIndicesTeddy
			s, e, found := engine.FindIndices([]byte(tt.input))
			stdLoc := re.FindStringIndex(tt.input)
			if stdLoc == nil {
				if found {
					t.Errorf("FindIndices: got (%d,%d), want not found", s, e)
				}
			} else if !found || s != stdLoc[0] || e != stdLoc[1] {
				t.Errorf("FindIndices: got (%d,%d,%v), want (%d,%d,true)",
					s, e, found, stdLoc[0], stdLoc[1])
			}

			// Count (exercises FindAll at>0 path)
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}
		})
	}
}

// TestFindIndicesTeddyAt_FallbackPaths exercises findIndicesTeddyAt fallback paths.
// findIndicesTeddyAt is at 42.9%.
func TestFindIndicesTeddyAt_FallbackPaths(t *testing.T) {
	pattern := `hotel|india|juliet`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseTeddy {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "prefix hotel middle india suffix juliet end"

	// FindIndicesAt at various positions (exercises findIndicesTeddyAt)
	for at := 0; at < len(input); at += 5 {
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("FindIndicesAt(%d): unexpected (%d,%d)", at, s, e)
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

// TestFindIndicesDigitPrefilterAt_AllPaths exercises findIndicesDigitPrefilterAt.
// findIndicesDigitPrefilterAt is at 59.1%.
func TestFindIndicesDigitPrefilterAt_AllPaths(t *testing.T) {
	pattern := `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	strategy := engine.Strategy()
	t.Logf("Strategy for IP pattern: %s", strategy)

	re := regexp.MustCompile(pattern)

	tests := []struct {
		name  string
		input string
	}{
		{"single IP", "server 192.168.1.1 port"},
		{"two IPs", "src 10.0.0.1 dst 172.16.0.1"},
		{"no match", "text without ips"},
		{"digits but no IP", "123 456 789"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Count exercises FindAll with at > 0 (findIndicesDigitPrefilterAt path)
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d", count, stdCount)
			}

			// FindIndicesAt at non-zero position
			if len(tt.input) > 10 {
				s, e, found := engine.FindIndicesAt([]byte(tt.input), 5)
				stdLoc := re.FindIndex([]byte(tt.input)[5:])
				if stdLoc == nil {
					if found {
						t.Errorf("FindIndicesAt(5): unexpected (%d,%d)", s, e)
					}
				} else {
					stdStart := stdLoc[0] + 5
					stdEnd := stdLoc[1] + 5
					if !found {
						t.Errorf("FindIndicesAt(5): not found, stdlib (%d,%d)", stdStart, stdEnd)
					}
				}
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

// TestFindIndicesBidirectionalDFA_EmptyMatch exercises the empty match case
// in findIndicesBidirectionalDFA (end == at path).
func TestFindIndicesBidirectionalDFA_EmptyMatch(t *testing.T) {
	// Pattern that can match empty: \w* (zero or more word chars)
	// Note: this may not trigger bidirectional DFA, but exercises BT with
	// various match sizes.
	pattern := `\w*`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(pattern)

	// Find on input where first match could be empty
	input := " hello "
	match := engine.Find([]byte(input))
	stdLoc := re.FindStringIndex(input)
	if stdLoc != nil && match != nil {
		if match.Start() != stdLoc[0] || match.End() != stdLoc[1] {
			t.Errorf("Find: got [%d,%d], stdlib [%d,%d]",
				match.Start(), match.End(), stdLoc[0], stdLoc[1])
		}
	}
}

// TestCompositeSearcher_VsStdlib_MultiMatch cross-validates CompositeSearcher
// FindAll results against stdlib for various patterns.
func TestCompositeSearcher_VsStdlib_MultiMatch(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`[a-z]+[0-9]+`, "abc123 def456 ghi789 jkl"},
		{`[A-Z]+[a-z]+`, "Hello World Foo Bar"},
		{`[0-9]+[a-z]+`, "123abc 456def 789ghi"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile(tt.pattern)

			// FindAll via Count
			count := engine.Count([]byte(tt.input), -1)
			stdCount := len(re.FindAllString(tt.input, -1))
			if count != stdCount {
				t.Errorf("Count = %d, stdlib = %d (strategy=%s)", count, stdCount, engine.Strategy())
			}

			// FindAllIndicesStreaming
			results := engine.FindAllIndicesStreaming([]byte(tt.input), 0, nil)
			stdResults := re.FindAllStringIndex(tt.input, -1)
			if len(results) != len(stdResults) {
				t.Errorf("FindAll: %d matches, stdlib %d", len(results), len(stdResults))
			}
			for i := 0; i < len(results) && i < len(stdResults); i++ {
				if results[i][0] != stdResults[i][0] || results[i][1] != stdResults[i][1] {
					t.Errorf("match[%d]: got [%d,%d], stdlib [%d,%d]",
						i, results[i][0], results[i][1], stdResults[i][0], stdResults[i][1])
				}
			}
		})
	}
}

// TestReverseInner_FindIndicesAt_NonZero exercises findIndicesReverseInnerAt.
// findIndicesReverseInnerAt is at 75.0%.
func TestReverseInner_FindIndicesAt_NonZero(t *testing.T) {
	pattern := `.*connection.*`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseInner {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "error: connection refused; retry: connection timeout"

	// Count exercises the FindAll loop with FindIndicesAt at > 0
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// FindIndicesAt at various positions
	for _, at := range []int{0, 10, 27, 35, 50} {
		if at >= len(input) {
			continue
		}
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		if stdLoc == nil {
			if found {
				t.Errorf("FindIndicesAt(%d): unexpected (%d,%d)", at, s, e)
			}
		} else if !found {
			t.Errorf("FindIndicesAt(%d): not found, stdlib (%d,%d)", at, stdLoc[0]+at, stdLoc[1]+at)
		}
	}
}

// TestMultilineReverseSuffix_FindIndicesAt exercises findIndicesMultilineReverseSuffixAt.
// findIndicesMultilineReverseSuffixAt is at 75.0%.
func TestMultilineReverseSuffix_FindIndicesAt_Multiline(t *testing.T) {
	pattern := `(?m)^.*\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseMultilineReverseSuffix {
		t.Skipf("Strategy is %s", engine.Strategy())
	}

	re := regexp.MustCompile(pattern)

	input := "/index.php\n/admin/dashboard.php\n/api/users.php"

	// Count
	count := engine.Count([]byte(input), -1)
	stdCount := len(re.FindAllString(input, -1))
	if count != stdCount {
		t.Errorf("Count = %d, stdlib = %d", count, stdCount)
	}

	// FindAll
	results := engine.FindAllIndicesStreaming([]byte(input), 0, nil)
	stdResults := re.FindAllStringIndex(input, -1)
	if len(results) != len(stdResults) {
		t.Errorf("FindAll: %d matches, stdlib %d", len(results), len(stdResults))
	}

	// FindIndicesAt at non-zero positions
	for _, at := range []int{0, 11, 32} {
		if at >= len(input) {
			continue
		}
		s, e, found := engine.FindIndicesAt([]byte(input), at)
		stdLoc := re.FindIndex([]byte(input)[at:])
		switch {
		case stdLoc == nil && found:
			t.Errorf("FindIndicesAt(%d): unexpected (%d,%d)", at, s, e)
		case stdLoc != nil && !found:
			t.Errorf("FindIndicesAt(%d): not found", at)
		case stdLoc != nil && found:
			t.Logf("FindIndicesAt(%d): [%d,%d] = %q", at, s, e, input[s:e])
		}
	}
}
