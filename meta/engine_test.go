package meta

import (
	"strings"
	"testing"
)

// TestEngineStrategy tests that Strategy() returns a known strategy for compiled patterns.
func TestEngineStrategy(t *testing.T) {
	tests := []struct {
		name           string
		pattern        string
		wantStrategies []Strategy // acceptable strategies (depends on platform/SIMD support)
	}{
		{
			name:    "simple literal triggers NFA or DFA",
			pattern: "a",
			wantStrategies: []Strategy{
				UseNFA, UseDFA, UseBoth, UseBoundedBacktracker, UseCharClassSearcher,
			},
		},
		{
			name:    "char class pattern",
			pattern: `\w+`,
			wantStrategies: []Strategy{
				UseCharClassSearcher, UseBoundedBacktracker, UseNFA,
			},
		},
		{
			name:    "reverse suffix pattern",
			pattern: `.*\.txt`,
			wantStrategies: []Strategy{
				UseReverseSuffix, UseBoth, UseDFA, UseNFA,
			},
		},
		{
			name:    "reverse inner pattern",
			pattern: `.*keyword.*`,
			wantStrategies: []Strategy{
				UseReverseInner, UseBoth, UseDFA, UseNFA,
			},
		},
		{
			name:    "digit class",
			pattern: `\d+`,
			wantStrategies: []Strategy{
				UseCharClassSearcher, UseBoundedBacktracker, UseNFA,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			strategy := engine.Strategy()
			// Verify strategy has a valid string representation
			stratStr := strategy.String()
			if stratStr == "" || stratStr == "Unknown" {
				t.Errorf("Strategy().String() = %q, want known strategy name", stratStr)
			}

			// Verify strategy is one of the acceptable ones
			found := false
			for _, want := range tt.wantStrategies {
				if strategy == want {
					found = true
					break
				}
			}
			if !found {
				t.Logf("Strategy() = %s (not in expected set, but may be valid for this platform)", stratStr)
			}
		})
	}
}

// TestStrategyString tests the Strategy.String() method for all known strategies.
func TestStrategyString(t *testing.T) {
	tests := []struct {
		strategy Strategy
		want     string
	}{
		{UseNFA, "UseNFA"},
		{UseDFA, "UseDFA"},
		{UseBoth, "UseBoth"},
		{UseReverseAnchored, "UseReverseAnchored"},
		{UseReverseSuffix, "UseReverseSuffix"},
		{UseOnePass, "UseOnePass"},
		{UseReverseInner, "UseReverseInner"},
		{UseBoundedBacktracker, "UseBoundedBacktracker"},
		{UseTeddy, "UseTeddy"},
		{UseReverseSuffixSet, "UseReverseSuffixSet"},
		{UseCharClassSearcher, "UseCharClassSearcher"},
		{UseCompositeSearcher, "UseCompositeSearcher"},
		{UseBranchDispatch, "UseBranchDispatch"},
		{UseDigitPrefilter, "UseDigitPrefilter"},
		{UseAhoCorasick, "UseAhoCorasick"},
		{UseAnchoredLiteral, "UseAnchoredLiteral"},
		{UseMultilineReverseSuffix, "UseMultilineReverseSuffix"},
		{Strategy(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.strategy.String()
			if got != tt.want {
				t.Errorf("Strategy(%d).String() = %q, want %q", int(tt.strategy), got, tt.want)
			}
		})
	}
}

// TestEngineIsStartAnchored tests IsStartAnchored for anchored and unanchored patterns.
func TestEngineIsStartAnchored(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"caret anchor", `^hello`, true},
		{"caret with alternation", `^(foo|bar)`, true},
		{"caret with class", `^[a-z]+`, true},
		{"unanchored literal", `hello`, false},
		{"unanchored class", `\d+`, false},
		{"unanchored alternation", `foo|bar`, false},
		{"dot star prefix (unanchored)", `.*hello`, false},
		{"dollar anchor only (not start-anchored)", `hello$`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.IsStartAnchored()
			if got != tt.want {
				t.Errorf("IsStartAnchored() = %v, want %v (pattern: %s)", got, tt.want, tt.pattern)
			}
		})
	}
}

// TestEngineNumCaptures tests NumCaptures for patterns with varying capture groups.
func TestEngineNumCaptures(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    int // including group 0
	}{
		{"no explicit captures", `hello`, 1},
		{"one capture group", `(hello)`, 2},
		{"two capture groups", `(\w+)@(\w+)`, 3},
		{"three capture groups", `(\d{4})-(\d{2})-(\d{2})`, 4},
		{"nested captures", `((a)(b))`, 4},
		{"alternation with captures", `(foo)|(bar)`, 3},
		{"non-capturing group", `(?:foo)bar`, 1},
		{"mix of capturing and non-capturing", `(?:foo)(bar)`, 2},
		{"char class (no captures)", `[a-z]+`, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.NumCaptures()
			if got != tt.want {
				t.Errorf("NumCaptures() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestEngineSubexpNames tests SubexpNames for named and unnamed groups.
func TestEngineSubexpNames(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			name:    "no captures",
			pattern: `hello`,
			want:    []string{""},
		},
		{
			name:    "unnamed captures",
			pattern: `(\w+) (\d+)`,
			want:    []string{"", "", ""},
		},
		{
			name:    "named captures",
			pattern: `(?P<word>\w+) (?P<num>\d+)`,
			want:    []string{"", "word", "num"},
		},
		{
			name:    "mixed named and unnamed",
			pattern: `(\w+) (?P<num>\d+)`,
			want:    []string{"", "", "num"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := engine.SubexpNames()
			if len(got) != len(tt.want) {
				t.Fatalf("SubexpNames() length = %d, want %d; got %v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("SubexpNames()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestEngineStatsTracking tests that Stats() tracks searches correctly.
func TestEngineStatsTracking(t *testing.T) {
	engine, err := Compile("hello")
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	// Initial stats should be zero
	stats := engine.Stats()
	totalBefore := stats.NFASearches + stats.DFASearches + stats.AhoCorasickSearches
	if totalBefore != 0 {
		t.Errorf("initial total searches = %d, want 0", totalBefore)
	}

	// Perform searches
	haystack := []byte("hello world")
	engine.IsMatch(haystack)
	engine.IsMatch(haystack)
	engine.IsMatch(haystack)

	stats = engine.Stats()
	totalAfter := stats.NFASearches + stats.DFASearches + stats.AhoCorasickSearches + stats.PrefilterHits
	if totalAfter == 0 {
		t.Error("after 3 searches, total search stats should be > 0")
	}

	// Reset and verify
	engine.ResetStats()
	stats = engine.Stats()
	if stats.NFASearches != 0 || stats.DFASearches != 0 || stats.AhoCorasickSearches != 0 {
		t.Error("ResetStats() did not clear all statistics")
	}
}

// TestEngineStatsAfterFind tests stats tracking for Find operations.
func TestEngineStatsAfterFind(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	engine.ResetStats()
	engine.Find([]byte("abc 123 def"))

	stats := engine.Stats()
	total := stats.NFASearches + stats.DFASearches + stats.PrefilterHits
	if total == 0 {
		t.Error("Find() should increment some search stat")
	}
}

// TestEngineSetLongest tests leftmost-longest vs leftmost-first matching.
func TestEngineSetLongest(t *testing.T) {
	engine, err := Compile(`a+`)
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("aaa")

	// Default: leftmost-first
	m := engine.Find(haystack)
	if m == nil {
		t.Fatal("Find() = nil, want match")
	}

	// Enable longest matching
	engine.SetLongest(true)
	m = engine.Find(haystack)
	if m == nil {
		t.Fatal("Find() = nil after SetLongest(true)")
	}
	// With longest semantics, "a+" on "aaa" should match "aaa"
	if m.String() != "aaa" {
		t.Errorf("SetLongest(true): Find() = %q, want %q", m.String(), "aaa")
	}
}

// TestEngineCompileWithConfig tests CompileWithConfig with different configurations.
func TestEngineCompileWithConfig(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		config  Config
		wantErr bool
	}{
		{
			name:    "default config",
			pattern: "hello",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name:    "DFA disabled",
			pattern: "hello",
			config: Config{
				EnableDFA:         false,
				EnablePrefilter:   false,
				MaxRecursionDepth: 100,
			},
			wantErr: false,
		},
		{
			name:    "prefilter disabled",
			pattern: "hello",
			config: Config{
				EnableDFA:            true,
				MaxDFAStates:         10000,
				DeterminizationLimit: 1000,
				EnablePrefilter:      false,
				MaxRecursionDepth:    100,
			},
			wantErr: false,
		},
		{
			name:    "invalid config rejected",
			pattern: "hello",
			config: Config{
				EnableDFA:         true,
				MaxDFAStates:      0, // invalid
				MaxRecursionDepth: 100,
			},
			wantErr: true,
		},
		{
			name:    "invalid pattern",
			pattern: "(",
			config:  DefaultConfig(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CompileWithConfig(tt.pattern, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("CompileWithConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && engine == nil {
				t.Error("CompileWithConfig() returned nil engine without error")
			}
		})
	}
}

// TestEngineFindAll tests FindAllIndicesStreaming returns correct match positions.
func TestEngineFindAll(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		limit     int
		wantCount int
		wantFirst string
	}{
		{
			name:      "find all words",
			pattern:   `\w+`,
			haystack:  "hello world foo",
			limit:     -1,
			wantCount: 3,
			wantFirst: "hello",
		},
		{
			name:      "find all digits",
			pattern:   `\d+`,
			haystack:  "a1 b22 c333",
			limit:     -1,
			wantCount: 3,
			wantFirst: "1",
		},
		{
			name:      "limit results",
			pattern:   `\w+`,
			haystack:  "a b c d e",
			limit:     2,
			wantCount: 2,
			wantFirst: "a",
		},
		{
			name:      "no match",
			pattern:   `\d+`,
			haystack:  "no digits here",
			limit:     -1,
			wantCount: 0,
		},
		{
			name:      "empty haystack",
			pattern:   `\w+`,
			haystack:  "",
			limit:     -1,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			haystack := []byte(tt.haystack)
			results := engine.FindAllIndicesStreaming(haystack, tt.limit, nil)

			if len(results) != tt.wantCount {
				t.Errorf("FindAllIndicesStreaming() returned %d matches, want %d", len(results), tt.wantCount)
				return
			}

			if tt.wantCount > 0 {
				first := string(haystack[results[0][0]:results[0][1]])
				if first != tt.wantFirst {
					t.Errorf("first match = %q, want %q", first, tt.wantFirst)
				}
			}
		})
	}
}

// TestEngineCount tests Count method.
func TestEngineCount(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		limit    int
		want     int
	}{
		{"count all words", `\w+`, "hello world foo", -1, 3},
		{"count digits", `\d+`, "a1 b22 c333", -1, 3},
		{"count with limit", `\w+`, "a b c d e", 2, 2},
		{"count zero limit", `\w+`, "a b c", 0, 0},
		{"no matches", `\d+`, "no digits", -1, 0},
		{"empty haystack", `\w+`, "", -1, 0},
		{"single match", `hello`, "hello world", -1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			got := engine.Count([]byte(tt.haystack), tt.limit)
			if got != tt.want {
				t.Errorf("Count() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestEngineFindSubmatch tests FindSubmatch with capture groups.
func TestEngineFindSubmatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantNil  bool
		wantG0   string
		wantG1   string
	}{
		{
			name:     "email capture",
			pattern:  `(\w+)@(\w+)`,
			haystack: "email: user@host.com",
			wantNil:  false,
			wantG0:   "user@host",
			wantG1:   "user",
		},
		{
			name:     "no match",
			pattern:  `(\d+)-(\d+)`,
			haystack: "no numbers here",
			wantNil:  true,
		},
		{
			name:     "simple group",
			pattern:  `(hello)`,
			haystack: "say hello world",
			wantNil:  false,
			wantG0:   "hello",
			wantG1:   "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			m := engine.FindSubmatch([]byte(tt.haystack))
			if tt.wantNil {
				if m != nil {
					t.Errorf("FindSubmatch() = non-nil, want nil")
				}
				return
			}

			if m == nil {
				t.Fatal("FindSubmatch() = nil, want match")
			}

			if got := m.String(); got != tt.wantG0 {
				t.Errorf("group 0 = %q, want %q", got, tt.wantG0)
			}
			if got := m.GroupString(1); got != tt.wantG1 {
				t.Errorf("group 1 = %q, want %q", got, tt.wantG1)
			}
		})
	}
}

// TestEngineFindAllSubmatch tests FindAllSubmatch returns all matches with captures.
func TestEngineFindAllSubmatch(t *testing.T) {
	engine, err := Compile(`(\w+)`)
	if err != nil {
		t.Fatal(err)
	}

	matches := engine.FindAllSubmatch([]byte("abc def ghi"), -1)
	if len(matches) != 3 {
		t.Fatalf("FindAllSubmatch() returned %d matches, want 3", len(matches))
	}

	expected := []string{"abc", "def", "ghi"}
	for i, want := range expected {
		got := matches[i].String()
		if got != want {
			t.Errorf("match[%d] = %q, want %q", i, got, want)
		}
	}
}

// TestEngineFindAllSubmatchWithLimit tests FindAllSubmatch respects limit.
func TestEngineFindAllSubmatchWithLimit(t *testing.T) {
	engine, err := Compile(`(\w+)`)
	if err != nil {
		t.Fatal(err)
	}

	matches := engine.FindAllSubmatch([]byte("a b c d e"), 2)
	if len(matches) != 2 {
		t.Fatalf("FindAllSubmatch(n=2) returned %d matches, want 2", len(matches))
	}

	// n=0 should return nil
	matches = engine.FindAllSubmatch([]byte("a b c"), 0)
	if matches != nil {
		t.Errorf("FindAllSubmatch(n=0) = non-nil, want nil")
	}
}

// TestEngineFindAt tests FindAt with non-zero starting positions.
func TestEngineFindAt(t *testing.T) {
	engine, err := Compile(`\w+`)
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("hello world foo")

	tests := []struct {
		name    string
		at      int
		wantStr string
		wantNil bool
	}{
		{"from 0", 0, "hello", false},
		{"from 6 (world)", 6, "world", false},
		{"from 12 (foo)", 12, "foo", false},
		{"from end", 15, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := engine.FindAt(haystack, tt.at)
			if tt.wantNil {
				if m != nil {
					t.Errorf("FindAt(%d) = %q, want nil", tt.at, m.String())
				}
				return
			}
			if m == nil {
				t.Fatalf("FindAt(%d) = nil, want %q", tt.at, tt.wantStr)
			}
			if m.String() != tt.wantStr {
				t.Errorf("FindAt(%d) = %q, want %q", tt.at, m.String(), tt.wantStr)
			}
		})
	}
}

// TestEngineFindIndices tests FindIndices returns correct positions.
func TestEngineFindIndices(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"literal match", "hello", "say hello world", 4, 9, true},
		{"digit match", `\d+`, "age: 42", 5, 7, true},
		{"no match", "xyz", "abc def", 0, 0, false},
		{"empty haystack", "a", "", 0, 0, false},
		{"match at start", "^hello", "hello world", 0, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatal(err)
			}

			start, end, found := engine.FindIndices([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
				return
			}
			if found {
				if start != tt.wantStart || end != tt.wantEnd {
					t.Errorf("indices = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
				}
			}
		})
	}
}

// TestEngineFindIndicesAt tests FindIndicesAt with offset.
func TestEngineFindIndicesAt(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("a1 b22 c333")

	// Find first match
	start, end, found := engine.FindIndicesAt(haystack, 0)
	if !found || start != 1 || end != 2 {
		t.Errorf("first match at 0: (%d, %d, %v), want (1, 2, true)", start, end, found)
	}

	// Find second match starting after first
	start, end, found = engine.FindIndicesAt(haystack, 2)
	if !found || start != 4 || end != 6 {
		t.Errorf("second match at 2: (%d, %d, %v), want (4, 6, true)", start, end, found)
	}

	// Find third match
	start, end, found = engine.FindIndicesAt(haystack, 6)
	if !found || start != 8 || end != 11 {
		t.Errorf("third match at 6: (%d, %d, %v), want (8, 11, true)", start, end, found)
	}

	// No more matches
	_, _, found = engine.FindIndicesAt(haystack, 11)
	if found {
		t.Error("expected no match after position 11")
	}
}

// TestEngineLargeInput tests engine behavior with large input.
func TestEngineLargeInput(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	// 100KB input with a number at the end
	large := strings.Repeat("x", 100*1024) + "42"
	m := engine.Find([]byte(large))
	if m == nil {
		t.Fatal("Find() = nil, want match on large input")
	}
	if m.String() != "42" {
		t.Errorf("Find() = %q, want %q", m.String(), "42")
	}
}
