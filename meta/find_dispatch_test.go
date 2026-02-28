package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestFindDispatchAllStrategies tests Find through patterns that trigger
// each known execution strategy.
func TestFindDispatchAllStrategies(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantText string // empty means no match
	}{
		// UseNFA
		{"nfa_literal", "xy", "abxycd", "xy"},
		{"nfa_nomatch", "xy", "abcd", ""},

		// UseCharClassSearcher
		{"ccs_word", `\w+`, "  hello  ", "hello"},
		{"ccs_digit", `\d+`, "abc123def", "123"},
		{"ccs_nomatch", `\d+`, "no digits", ""},
		{"ccs_lowercase", `[a-z]+`, "ABC abc", "abc"},

		// UseBoundedBacktracker
		{"bt_capture_word", `(\w)+`, "abc", "abc"},
		{"bt_capture_digit", `(\d)+`, "99", "99"},
		{"bt_nomatch", `(\d)+`, "abc", ""},

		// UseReverseSuffix
		{"rsuffix_txt", `.*\.txt`, "readme.txt", "readme.txt"},
		{"rsuffix_css", `.*\.css`, "style.css", "style.css"},
		{"rsuffix_nomatch", `.*\.txt`, "readme.pdf", ""},

		// UseReverseInner
		{"rinner_keyword", `.*keyword.*`, "has keyword here", "has keyword here"},
		{"rinner_error", `.*ERROR.*`, "line ERROR!", "line ERROR!"},
		{"rinner_nomatch", `.*keyword.*`, "nothing", ""},

		// UseReverseAnchored
		{"ranchored_dollar", `end$`, "the end", "end"},
		{"ranchored_nomatch", `end$`, "end of line", ""},

		// UseCompositeSearcher
		{"composite_alpha_digit", `[a-zA-Z]+[0-9]+`, "test123", "test123"},
		{"composite_in_text", `[a-z]+[0-9]+`, "  ab42 ", "ab42"},
		{"composite_nomatch", `[a-zA-Z]+[0-9]+`, "test", ""},

		// UseDigitPrefilter
		{"digit_version", `\d+\.\d+\.\d+`, "v1.2.3 release", "1.2.3"},
		{"digit_pair", `\d+\.\d+`, "ver 3.14", "3.14"},
		{"digit_nomatch", `\d+\.\d+`, "no dots", ""},

		// UseTeddy
		{"teddy_first", "foo|bar|baz", "prefix foo suffix", "foo"},
		{"teddy_second", "foo|bar|baz", "prefix bar suffix", "bar"},
		{"teddy_third", "foo|bar|baz", "prefix baz suffix", "baz"},
		{"teddy_nomatch", "foo|bar|baz", "prefix qux suffix", ""},

		// UseAnchoredLiteral
		{"anchored_lit", `^/.*\.php$`, "/index.php", "/index.php"},
		{"anchored_lit_nomatch", `^/.*\.php$`, "/index.html", ""},

		// Start-anchored
		{"start_anchor", `^hello`, "hello world", "hello"},
		{"start_anchor_nomatch", `^hello`, "say hello", ""},

		// Empty pattern
		{"empty_pattern", "", "test", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			match := engine.Find([]byte(tt.haystack))
			if tt.wantText == "" {
				if match != nil && match.Len() > 0 {
					t.Errorf("Find(%q) = %q, want no match (strategy: %s)",
						tt.haystack, match.String(), engine.Strategy())
				}
			} else {
				if match == nil {
					t.Errorf("Find(%q) = nil, want %q (strategy: %s)",
						tt.haystack, tt.wantText, engine.Strategy())
				} else if match.String() != tt.wantText {
					t.Errorf("Find(%q) = %q, want %q (strategy: %s)",
						tt.haystack, match.String(), tt.wantText, engine.Strategy())
				}
			}
		})
	}
}

// TestFindAtNonZeroPositions tests FindAt for various strategies at non-zero positions.
func TestFindAtNonZeroPositions(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		at       int
		wantText string // empty means no match
	}{
		// CharClassSearcher
		{"ccs_at_4", `\w+`, "abc def", 4, "def"},
		{"ccs_at_end", `\w+`, "abc", 3, ""},

		// BoundedBacktracker
		{"bt_at_3", `(\d)+`, "12 34", 3, "34"},

		// NFA
		{"nfa_at_2", "x", "axbxc", 2, "x"},
		{"nfa_at_end", "x", "ax", 2, ""},

		// Composite
		{"composite_at_4", `[a-z]+[0-9]+`, "ab1 cd2", 4, "cd2"},

		// Anchored at non-zero (should not match)
		{"anchored_at_1", `^hello`, "hello", 1, ""},

		// At 0 for anchored (should match)
		{"anchored_at_0", `^hello`, "hello world", 0, "hello"},

		// At past all data
		{"at_beyond", `\w+`, "hello", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			match := engine.FindAt([]byte(tt.haystack), tt.at)
			if tt.wantText == "" {
				if match != nil {
					t.Errorf("FindAt(%q, %d) = %q, want nil (strategy: %s)",
						tt.haystack, tt.at, match.String(), engine.Strategy())
				}
			} else {
				if match == nil {
					t.Errorf("FindAt(%q, %d) = nil, want %q (strategy: %s)",
						tt.haystack, tt.at, tt.wantText, engine.Strategy())
				} else if match.String() != tt.wantText {
					t.Errorf("FindAt(%q, %d) = %q, want %q (strategy: %s)",
						tt.haystack, tt.at, match.String(), tt.wantText, engine.Strategy())
				}
			}
		})
	}
}

// TestFindVsStdlib compares Find results against Go stdlib regexp.
func TestFindVsStdlib(t *testing.T) {
	patterns := []string{
		`\d+`,
		`\w+`,
		`[a-z]+`,
		"hello",
		`[a-zA-Z]+[0-9]+`,
	}

	haystacks := []string{
		"hello world 123",
		"abc def 42",
		"test123",
		"   ",
		"",
		"HELLO",
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}
		re := regexp.MustCompile(pattern)

		for _, haystack := range haystacks {
			h := []byte(haystack)
			match := engine.Find(h)
			stdlibLoc := re.FindIndex(h)

			coregexFound := match != nil
			stdlibFound := stdlibLoc != nil

			if coregexFound != stdlibFound {
				t.Errorf("pattern %q, haystack %q: coregex found=%v, stdlib found=%v",
					pattern, haystack, coregexFound, stdlibFound)
				continue
			}

			if coregexFound {
				if match.Start() != stdlibLoc[0] || match.End() != stdlibLoc[1] {
					t.Errorf("pattern %q, haystack %q: coregex (%d,%d), stdlib (%d,%d)",
						pattern, haystack, match.Start(), match.End(), stdlibLoc[0], stdlibLoc[1])
				}
			}
		}
	}
}

// TestFindMatchProperties tests that Match object properties are correct.
func TestFindMatchProperties(t *testing.T) {
	engine, err := Compile(`\w+`)
	if err != nil {
		t.Fatal(err)
	}

	match := engine.Find([]byte("  hello  "))
	if match == nil {
		t.Fatal("expected match")
	}

	if match.Start() != 2 {
		t.Errorf("Start() = %d, want 2", match.Start())
	}
	if match.End() != 7 {
		t.Errorf("End() = %d, want 7", match.End())
	}
	if match.Len() != 5 {
		t.Errorf("Len() = %d, want 5", match.Len())
	}
	if match.String() != "hello" {
		t.Errorf("String() = %q, want %q", match.String(), "hello")
	}
	if string(match.Bytes()) != "hello" {
		t.Errorf("Bytes() = %q, want %q", string(match.Bytes()), "hello")
	}
	if match.IsEmpty() {
		t.Error("IsEmpty() should be false")
	}
	if !match.Contains(3) {
		t.Error("Contains(3) should be true")
	}
	if match.Contains(7) {
		t.Error("Contains(7) should be false (exclusive end)")
	}
}

// TestFindMultipleCallsSameEngine tests repeated Find calls on the same engine.
func TestFindMultipleCallsSameEngine(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	// 100 iterations to stress the sync.Pool
	for i := 0; i < 100; i++ {
		match := engine.Find([]byte("abc 42 def"))
		if match == nil {
			t.Fatalf("iteration %d: Find returned nil", i)
		}
		if match.String() != "42" {
			t.Fatalf("iteration %d: Find returned %q, want %q", i, match.String(), "42")
		}
	}

	for i := 0; i < 100; i++ {
		match := engine.Find([]byte("no digits"))
		if match != nil {
			t.Fatalf("iteration %d: Find returned %q, want nil", i, match.String())
		}
	}
}

// TestFindLargeInput tests Find on large inputs for different strategies.
func TestFindLargeInput(t *testing.T) {
	const size = 100 * 1024

	tests := []struct {
		name      string
		pattern   string
		input     func() []byte
		wantFound bool
	}{
		{
			"literal_at_end",
			"needle",
			func() []byte { return []byte(strings.Repeat("x", size) + "needle") },
			true,
		},
		{
			"suffix_at_end",
			`.*\.cfg`,
			func() []byte { return []byte(strings.Repeat("x", size) + ".cfg") },
			true,
		},
		{
			"inner_at_end",
			`.*FATAL.*`,
			func() []byte {
				return []byte(strings.Repeat("x", size) + "FATAL" + strings.Repeat("y", 100))
			},
			true,
		},
		{
			"charclass_at_end",
			`\d+`,
			func() []byte { return []byte(strings.Repeat("abc ", size/4) + "42") },
			true,
		},
		{
			"no_match",
			"needle",
			func() []byte { return []byte(strings.Repeat("hay ", size/4)) },
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			match := engine.Find(tt.input())
			found := match != nil
			if found != tt.wantFound {
				t.Errorf("Find on large input: found=%v, want %v (strategy: %s)",
					found, tt.wantFound, engine.Strategy())
			}
		})
	}
}

// TestFindAtAllStrategiesConsistency verifies that Find and FindAt(0) return
// identical results for patterns triggering each strategy.
func TestFindAtAllStrategiesConsistency(t *testing.T) {
	patterns := []string{
		"x",            // UseNFA
		`\w+`,          // UseCharClassSearcher
		`(\w)+`,        // UseBoundedBacktracker
		`.*\.txt`,      // UseReverseSuffix
		`.*ERROR.*`,    // UseReverseInner
		`end$`,         // UseReverseAnchored
		`[a-z]+[0-9]+`, // UseCompositeSearcher
		`\d+\.\d+`,     // UseDigitPrefilter
		"foo|bar|baz",  // UseTeddy
	}

	haystacks := []string{
		"x",
		"hello world",
		"abc 123",
		"readme.txt",
		"ERROR here",
		"the end",
		"test42",
		"version 1.0",
		"say foo",
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}

		for _, haystack := range haystacks {
			h := []byte(haystack)
			m1 := engine.Find(h)
			m2 := engine.FindAt(h, 0)

			found1 := m1 != nil
			found2 := m2 != nil

			if found1 != found2 {
				t.Errorf("pattern %q, haystack %q: Find=%v, FindAt(0)=%v (strategy: %s)",
					pattern, haystack, found1, found2, engine.Strategy())
				continue
			}

			if found1 && found2 {
				if m1.Start() != m2.Start() || m1.End() != m2.End() {
					t.Errorf("pattern %q, haystack %q: Find=(%d,%d), FindAt(0)=(%d,%d)",
						pattern, haystack, m1.Start(), m1.End(), m2.Start(), m2.End())
				}
			}
		}
	}
}

// TestFindAtReturnsNilBeyondHaystack tests that FindAt returns nil when at > len.
func TestFindAtReturnsNilBeyondHaystack(t *testing.T) {
	engine, err := Compile(`\w+`)
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("hello")
	match := engine.FindAt(haystack, 100)
	if match != nil {
		t.Errorf("FindAt beyond haystack returned %q, want nil", match.String())
	}
}

// TestFindAtReverseStrategyFallback tests that reverse strategies fall back
// correctly at non-zero positions.
func TestFindAtReverseStrategyFallback(t *testing.T) {
	// Patterns with reverse strategies
	tests := []struct {
		name     string
		pattern  string
		haystack string
		at       int
		wantText string
	}{
		{"rsuffix_at_0", `.*\.txt`, "a.txt b.txt", 0, "a.txt b.txt"},
		{"rinner_at_0", `.*ERROR.*`, "ok ERROR now", 0, "ok ERROR now"},
		{"ranchored_at_0", `end$`, "the end", 0, "end"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			match := engine.FindAt([]byte(tt.haystack), tt.at)
			if match == nil {
				t.Errorf("FindAt(%q, %d) = nil, want %q", tt.haystack, tt.at, tt.wantText)
			} else if match.String() != tt.wantText {
				t.Errorf("FindAt(%q, %d) = %q, want %q",
					tt.haystack, tt.at, match.String(), tt.wantText)
			}
		})
	}
}

// TestFindEmptyMatch tests that Find handles patterns matching empty strings.
func TestFindEmptyMatch(t *testing.T) {
	engine, err := Compile("")
	if err != nil {
		t.Fatal(err)
	}

	match := engine.Find([]byte("abc"))
	if match == nil {
		t.Fatal("empty pattern should match")
	}
	if !match.IsEmpty() {
		t.Errorf("empty pattern match should be empty, got len %d", match.Len())
	}
}

// TestFindDigitPrefilterCorrectness tests that digit prefilter returns
// correct match positions for various numeric patterns.
func TestFindDigitPrefilterCorrectness(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
		want     string
	}{
		{`\d+\.\d+\.\d+`, "version 1.2.3 here", "1.2.3"},
		{`\d+\.\d+`, "val=3.14", "3.14"},
		{`\d+:\d+:\d+`, "time 12:30:45!", "12:30:45"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			match := engine.Find([]byte(tt.haystack))
			if match == nil {
				t.Fatalf("Find(%q) returned nil, want %q", tt.haystack, tt.want)
			}
			if match.String() != tt.want {
				t.Errorf("Find(%q) = %q, want %q", tt.haystack, match.String(), tt.want)
			}
		})
	}
}
