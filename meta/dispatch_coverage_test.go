package meta

import (
	"strings"
	"testing"
)

// TestBranchDispatchFindAndIsMatch exercises BranchDispatch strategy
// through Find, IsMatch, FindIndices, and FindAt APIs.
func TestBranchDispatchFindAndIsMatch(t *testing.T) {
	// Pattern that uses BranchDispatch: anchored alternation with distinct first bytes
	pattern := `^(\d+|UUID)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBranchDispatch {
		t.Skipf("pattern %q uses strategy %s, not BranchDispatch", pattern, engine.Strategy())
	}

	tests := []struct {
		name      string
		haystack  string
		wantMatch bool
		wantText  string
	}{
		{"digit_branch", "42 rest", true, "42"},
		{"uuid_branch", "UUID-123", true, "UUID"},
		{"no_match", "abc", false, ""},
		{"empty", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := []byte(tt.haystack)

			// IsMatch
			isMatch := engine.IsMatch(h)
			if isMatch != tt.wantMatch {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, isMatch, tt.wantMatch)
			}

			// Find
			match := engine.Find(h)
			if tt.wantMatch {
				if match == nil {
					t.Errorf("Find(%q) = nil, want %q", tt.haystack, tt.wantText)
				} else if match.String() != tt.wantText {
					t.Errorf("Find(%q) = %q, want %q", tt.haystack, match.String(), tt.wantText)
				}
			} else {
				if match != nil {
					t.Errorf("Find(%q) = %q, want nil", tt.haystack, match.String())
				}
			}

			// FindIndices
			start, end, found := engine.FindIndices(h)
			if found != tt.wantMatch {
				t.Errorf("FindIndices(%q) found=%v, want %v", tt.haystack, found, tt.wantMatch)
			}
			if found && match != nil {
				if start != match.Start() || end != match.End() {
					t.Errorf("FindIndices(%q) = (%d,%d), Find = (%d,%d)",
						tt.haystack, start, end, match.Start(), match.End())
				}
			}

			// FindAt with non-zero position (anchored pattern, should not match at != 0)
			matchAt := engine.FindAt(h, 1)
			if matchAt != nil {
				t.Errorf("FindAt(%q, 1) should return nil for anchored pattern", tt.haystack)
			}
		})
	}
}

// TestAhoCorasickFindAllApis exercises the AhoCorasick strategy through
// Find, FindAt, IsMatch, FindIndices, FindAll, and Count.
func TestAhoCorasickFindAllApis(t *testing.T) {
	// Generate 70 patterns to force AhoCorasick
	patterns := make([]string, 70)
	for i := range patterns {
		patterns[i] = strings.Repeat(string(rune('A'+(i%26))), 3) + strings.Repeat(string(rune('a'+(i%26))), 2)
	}
	pattern := strings.Join(patterns, "|")

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseAhoCorasick {
		t.Skipf("pattern uses strategy %s, not AhoCorasick", engine.Strategy())
	}

	haystack := []byte("test AAAaa and BBBbb end")

	// IsMatch
	if !engine.IsMatch(haystack) {
		t.Error("IsMatch should return true")
	}

	// Find
	match := engine.Find(haystack)
	if match == nil {
		t.Fatal("Find should return a match")
	}
	if match.String() != "AAAaa" {
		t.Errorf("Find = %q, want %q", match.String(), "AAAaa")
	}

	// FindIndices
	start, end, found := engine.FindIndices(haystack)
	if !found {
		t.Error("FindIndices should find a match")
	}
	if start != match.Start() || end != match.End() {
		t.Errorf("FindIndices (%d,%d) != Find (%d,%d)",
			start, end, match.Start(), match.End())
	}

	// FindAt
	matchAt := engine.FindAt(haystack, match.End())
	if matchAt == nil {
		t.Fatal("FindAt should find second match")
	}
	if matchAt.String() != "BBBbb" {
		t.Errorf("FindAt = %q, want %q", matchAt.String(), "BBBbb")
	}

	// Count
	count := engine.Count(haystack, -1)
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}

	// No match
	if engine.IsMatch([]byte("nothing here")) {
		t.Error("should not match 'nothing here'")
	}
}

// TestReverseSuffixSetFindAndIsMatch exercises ReverseSuffixSet strategy APIs.
func TestReverseSuffixSetFindAndIsMatch(t *testing.T) {
	pattern := `.*\.(txt|log|csv)`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffixSet {
		t.Skipf("pattern %q uses %s, not ReverseSuffixSet", pattern, engine.Strategy())
	}

	tests := []struct {
		name      string
		haystack  string
		wantMatch bool
	}{
		{"txt", "readme.txt", true},
		{"log", "error.log", true},
		{"csv", "data.csv", true},
		{"nomatch", "style.css", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := []byte(tt.haystack)

			if engine.IsMatch(h) != tt.wantMatch {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, !tt.wantMatch, tt.wantMatch)
			}

			match := engine.Find(h)
			if (match != nil) != tt.wantMatch {
				t.Errorf("Find(%q) found=%v, want %v", tt.haystack, match != nil, tt.wantMatch)
			}

			_, _, found := engine.FindIndices(h)
			if found != tt.wantMatch {
				t.Errorf("FindIndices(%q) found=%v, want %v", tt.haystack, found, tt.wantMatch)
			}
		})
	}
}

// TestMultilineReverseSuffixFindAndIsMatch exercises MultilineReverseSuffix strategy.
func TestMultilineReverseSuffixFindAndIsMatch(t *testing.T) {
	pattern := `(?m)^/.*\.php`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseMultilineReverseSuffix {
		t.Skipf("pattern %q uses %s, not MultilineReverseSuffix", pattern, engine.Strategy())
	}

	tests := []struct {
		name      string
		haystack  string
		wantMatch bool
	}{
		{"single_line", "/index.php", true},
		{"in_multi_line", "other\n/page.php\nrest", true},
		{"no_slash", "index.php", false},
		{"no_php", "/index.html", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := []byte(tt.haystack)

			if engine.IsMatch(h) != tt.wantMatch {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, !tt.wantMatch, tt.wantMatch)
			}

			match := engine.Find(h)
			if (match != nil) != tt.wantMatch {
				t.Errorf("Find(%q) found=%v, want %v", tt.haystack, match != nil, tt.wantMatch)
			}

			_, _, found := engine.FindIndices(h)
			if found != tt.wantMatch {
				t.Errorf("FindIndices(%q) found=%v, want %v", tt.haystack, found, tt.wantMatch)
			}
		})
	}
}

// TestAnchoredLiteralFindAndIsMatch exercises AnchoredLiteral strategy APIs.
func TestAnchoredLiteralFindAndIsMatch(t *testing.T) {
	pattern := `^/.*\.php$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseAnchoredLiteral {
		t.Skipf("pattern %q uses %s, not AnchoredLiteral", pattern, engine.Strategy())
	}

	tests := []struct {
		haystack  string
		wantMatch bool
	}{
		{"/index.php", true},
		{"/path/to/file.php", true},
		{"/a.php", true},
		{"index.php", false},        // no leading /
		{"/index.html", false},      // wrong suffix
		{"/index.php/extra", false}, // extra after $
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.haystack, func(t *testing.T) {
			h := []byte(tt.haystack)

			if engine.IsMatch(h) != tt.wantMatch {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, !tt.wantMatch, tt.wantMatch)
			}

			match := engine.Find(h)
			if (match != nil) != tt.wantMatch {
				t.Errorf("Find(%q) found=%v, want %v", tt.haystack, match != nil, tt.wantMatch)
			}

			_, _, found := engine.FindIndices(h)
			if found != tt.wantMatch {
				t.Errorf("FindIndices(%q) found=%v, want %v", tt.haystack, found, tt.wantMatch)
			}
		})
	}
}

// TestDigitPrefilterAllApis exercises digit prefilter through all API paths.
func TestDigitPrefilterAllApis(t *testing.T) {
	pattern := `\d+\.\d+\.\d+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseDigitPrefilter {
		t.Skipf("pattern %q uses %s, not DigitPrefilter", pattern, engine.Strategy())
	}

	haystack := []byte("version 1.2.3 and 4.5.6 end")

	// IsMatch
	if !engine.IsMatch(haystack) {
		t.Error("IsMatch should be true")
	}

	// Find
	match := engine.Find(haystack)
	if match == nil || match.String() != "1.2.3" {
		t.Errorf("Find = %v, want 1.2.3", match)
	}

	// FindIndices
	start, end, found := engine.FindIndices(haystack)
	if !found || start != 8 || end != 13 {
		t.Errorf("FindIndices = (%d,%d,%v), want (8,13,true)", start, end, found)
	}

	// FindAt at second version
	matchAt := engine.FindAt(haystack, 14)
	if matchAt == nil || matchAt.String() != "4.5.6" {
		t.Errorf("FindAt(14) = %v, want 4.5.6", matchAt)
	}

	// Count
	count := engine.Count(haystack, -1)
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}

	// FindAllIndicesStreaming
	all := engine.FindAllIndicesStreaming(haystack, -1, nil)
	if len(all) != 2 {
		t.Errorf("FindAllIndicesStreaming = %d matches, want 2", len(all))
	}

	// No match
	if engine.IsMatch([]byte("no versions")) {
		t.Error("should not match 'no versions'")
	}
}

// TestTeddyAllApis exercises Teddy strategy through all API paths.
func TestTeddyAllApis(t *testing.T) {
	pattern := "alpha|bravo|charlie|delta|echo"
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseTeddy {
		t.Skipf("pattern uses %s, not Teddy", engine.Strategy())
	}

	haystack := []byte("the bravo and echo are here")

	// IsMatch
	if !engine.IsMatch(haystack) {
		t.Error("IsMatch should be true")
	}

	// Find
	match := engine.Find(haystack)
	if match == nil || match.String() != "bravo" {
		t.Errorf("Find = %v, want bravo", match)
	}

	// FindAt
	matchAt := engine.FindAt(haystack, match.End())
	if matchAt == nil || matchAt.String() != "echo" {
		t.Errorf("FindAt = %v, want echo", matchAt)
	}

	// FindIndices
	start, end, found := engine.FindIndices(haystack)
	if !found {
		t.Error("FindIndices should find")
	}
	if start != match.Start() || end != match.End() {
		t.Errorf("FindIndices mismatch with Find")
	}

	// Count
	count := engine.Count(haystack, -1)
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}

	// No match
	if engine.IsMatch([]byte("nothing")) {
		t.Error("should not match 'nothing'")
	}
}

// TestCompositeSearcherAllApis exercises CompositeSearcher through all APIs.
func TestCompositeSearcherAllApis(t *testing.T) {
	pattern := `[a-z]+[0-9]+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseCompositeSearcher {
		t.Skipf("pattern uses %s, not CompositeSearcher", engine.Strategy())
	}

	haystack := []byte("pre abc123 mid def456 end")

	// Find
	match := engine.Find(haystack)
	if match == nil || match.String() != "abc123" {
		t.Errorf("Find = %v, want abc123", match)
	}

	// FindAt
	matchAt := engine.FindAt(haystack, match.End())
	if matchAt == nil || matchAt.String() != "def456" {
		t.Errorf("FindAt = %v, want def456", matchAt)
	}

	// FindIndices
	_, _, found := engine.FindIndices(haystack)
	if !found {
		t.Error("FindIndices should find")
	}

	// FindIndicesAt
	start, end, found := engine.FindIndicesAt(haystack, match.End())
	if !found || haystack[start] != 'd' {
		t.Errorf("FindIndicesAt(%d) = (%d,%d,%v), expected def456",
			match.End(), start, end, found)
	}

	// Count
	count := engine.Count(haystack, -1)
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}
}

// TestCharClassSearcherAllApis exercises CharClassSearcher through all APIs.
func TestCharClassSearcherAllApis(t *testing.T) {
	pattern := `\w+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseCharClassSearcher {
		t.Skipf("pattern uses %s, not CharClassSearcher", engine.Strategy())
	}

	haystack := []byte("  abc  def  ")

	// Find
	match := engine.Find(haystack)
	if match == nil || match.String() != "abc" {
		t.Errorf("Find = %v, want abc", match)
	}

	// FindAt
	matchAt := engine.FindAt(haystack, match.End())
	if matchAt == nil || matchAt.String() != "def" {
		t.Errorf("FindAt = %v, want def", matchAt)
	}

	// FindIndicesAt
	start, end, found := engine.FindIndicesAt(haystack, match.End())
	if !found || string(haystack[start:end]) != "def" {
		t.Errorf("FindIndicesAt = %q, want def", string(haystack[start:end]))
	}

	// Count
	count := engine.Count(haystack, -1)
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}

	// FindAllIndicesStreaming
	all := engine.FindAllIndicesStreaming(haystack, -1, nil)
	if len(all) != 2 {
		t.Errorf("FindAll = %d, want 2", len(all))
	}
}

// TestBoundedBacktrackerAllApis exercises BoundedBacktracker through all APIs.
func TestBoundedBacktrackerAllApis(t *testing.T) {
	pattern := `(\w)+`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseBoundedBacktracker {
		t.Skipf("pattern uses %s, not BoundedBacktracker", engine.Strategy())
	}

	haystack := []byte("  abc  def  ")

	// Find
	match := engine.Find(haystack)
	if match == nil || match.String() != "abc" {
		t.Errorf("Find = %v, want abc", match)
	}

	// FindAt
	matchAt := engine.FindAt(haystack, match.End())
	if matchAt == nil || matchAt.String() != "def" {
		t.Errorf("FindAt = %v, want def", matchAt)
	}

	// FindIndices
	start, end, found := engine.FindIndices(haystack)
	if !found || string(haystack[start:end]) != "abc" {
		t.Errorf("FindIndices = %q, want abc", string(haystack[start:end]))
	}

	// FindIndicesAt
	start2, end2, found2 := engine.FindIndicesAt(haystack, end)
	if !found2 || string(haystack[start2:end2]) != "def" {
		t.Errorf("FindIndicesAt = %q, want def", string(haystack[start2:end2]))
	}

	// FindSubmatch
	submatch := engine.FindSubmatch(haystack)
	if submatch == nil {
		t.Fatal("FindSubmatch should not be nil")
	}
	if submatch.String() != "abc" {
		t.Errorf("FindSubmatch = %q, want abc", submatch.String())
	}

	// Count
	count := engine.Count(haystack, -1)
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}
}

// TestReverseAnchoredAllApis exercises ReverseAnchored strategy through all APIs.
func TestReverseAnchoredAllApis(t *testing.T) {
	pattern := `world$`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseAnchored {
		t.Skipf("pattern uses %s, not ReverseAnchored", engine.Strategy())
	}

	haystack := []byte("hello world")

	// IsMatch
	if !engine.IsMatch(haystack) {
		t.Error("IsMatch should be true")
	}

	// Find
	match := engine.Find(haystack)
	if match == nil || match.String() != "world" {
		t.Errorf("Find = %v, want world", match)
	}

	// FindIndices
	start, end, found := engine.FindIndices(haystack)
	if !found || start != 6 || end != 11 {
		t.Errorf("FindIndices = (%d,%d,%v), want (6,11,true)", start, end, found)
	}

	// No match
	if engine.IsMatch([]byte("world hello")) {
		t.Error("should not match 'world hello'")
	}
}

// TestReverseSuffixAllApis exercises ReverseSuffix strategy through all APIs.
func TestReverseSuffixAllApis(t *testing.T) {
	pattern := `.*\.txt`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseSuffix {
		t.Skipf("pattern uses %s, not ReverseSuffix", engine.Strategy())
	}

	haystack := []byte("readme.txt")

	// IsMatch
	if !engine.IsMatch(haystack) {
		t.Error("IsMatch should be true")
	}

	// Find
	match := engine.Find(haystack)
	if match == nil || match.String() != "readme.txt" {
		t.Errorf("Find = %v, want readme.txt", match)
	}

	// FindIndices
	start, end, found := engine.FindIndices(haystack)
	if !found || start != 0 || end != 10 {
		t.Errorf("FindIndices = (%d,%d,%v), want (0,10,true)", start, end, found)
	}

	// No match
	if engine.IsMatch([]byte("readme.pdf")) {
		t.Error("should not match 'readme.pdf'")
	}
}

// TestReverseInnerAllApis exercises ReverseInner strategy through all APIs.
func TestReverseInnerAllApis(t *testing.T) {
	pattern := `.*ERROR.*`
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseReverseInner {
		t.Skipf("pattern uses %s, not ReverseInner", engine.Strategy())
	}

	haystack := []byte("prefix ERROR suffix")

	// IsMatch
	if !engine.IsMatch(haystack) {
		t.Error("IsMatch should be true")
	}

	// Find
	match := engine.Find(haystack)
	if match == nil {
		t.Fatal("Find should not be nil")
	}

	// FindIndices
	_, _, found := engine.FindIndices(haystack)
	if !found {
		t.Error("FindIndices should find")
	}

	// No match
	if engine.IsMatch([]byte("all fine here")) {
		t.Error("should not match 'all fine here'")
	}
}

// TestNFAStrategyAllApis exercises NFA strategy through all APIs.
func TestNFAStrategyAllApis(t *testing.T) {
	pattern := "xy"
	engine, err := Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}

	if engine.Strategy() != UseNFA {
		t.Skipf("pattern uses %s, not NFA", engine.Strategy())
	}

	haystack := []byte("abxycdxy")

	// Find
	match := engine.Find(haystack)
	if match == nil || match.String() != "xy" {
		t.Errorf("Find = %v, want xy", match)
	}
	if match.Start() != 2 {
		t.Errorf("Find.Start() = %d, want 2", match.Start())
	}

	// FindAt
	matchAt := engine.FindAt(haystack, 4)
	if matchAt == nil || matchAt.Start() != 6 {
		t.Errorf("FindAt(4) = %v, want match at 6", matchAt)
	}

	// FindIndicesAt
	start, end, found := engine.FindIndicesAt(haystack, 4)
	if !found || start != 6 || end != 8 {
		t.Errorf("FindIndicesAt(4) = (%d,%d,%v), want (6,8,true)", start, end, found)
	}

	// Count
	count := engine.Count(haystack, -1)
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}
}

// TestSetLongestAndMatch tests SetLongest affects matching semantics.
func TestSetLongestAndMatch(t *testing.T) {
	engine, err := Compile(`a|ab`)
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("ab")

	// Default (leftmost-first): "a" wins
	match := engine.Find(haystack)
	if match == nil || match.String() != "a" {
		t.Errorf("default: Find = %v, want 'a'", match)
	}

	// Longest: "ab" wins
	engine.SetLongest(true)
	match = engine.Find(haystack)
	if match == nil || match.String() != "ab" {
		t.Errorf("longest: Find = %v, want 'ab'", match)
	}

	// Reset
	engine.SetLongest(false)
	match = engine.Find(haystack)
	if match == nil || match.String() != "a" {
		t.Errorf("reset: Find = %v, want 'a'", match)
	}
}

// TestStatsTracking tests that stats are properly tracked across operations.
func TestStatsTracking(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	engine.ResetStats()

	// Perform some operations
	engine.IsMatch([]byte("abc 123"))
	engine.Find([]byte("abc 456"))
	engine.FindIndices([]byte("abc 789"))
	engine.Count([]byte("1 2 3"), -1)

	stats := engine.Stats()
	if stats.NFASearches == 0 {
		t.Error("expected some NFA searches")
	}
}
