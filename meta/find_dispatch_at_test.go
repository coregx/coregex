package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestFindAtNonZeroDispatch exercises findAtNonZero dispatch for various strategies.
// Covers: find.go findAtNonZero (53%), findDFAAt (0%), findAdaptiveAt (0%),
//
//	findBranchDispatchAt (0%), findMultilineReverseSuffixAt (0%)
func TestFindAtNonZeroDispatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		at      int
		want    string
		wantNil bool
	}{
		// NFA strategy - simple pattern
		{
			name:    "nfa_at_3",
			pattern: `ab`,
			input:   "xxxab",
			at:      2,
			want:    "ab",
		},
		{
			name:    "nfa_at_beyond",
			pattern: `ab`,
			input:   "ab",
			at:      5,
			wantNil: true,
		},
		// Patterns with literal prefixes (DFA/Both strategies)
		{
			name:    "literal_find_at",
			pattern: `hello\w+`,
			input:   "say helloworld here",
			at:      3,
			want:    "helloworld",
		},
		// Anchored pattern at>0 returns nil
		{
			name:    "anchored_at_nonzero",
			pattern: `^start`,
			input:   "startXstart",
			at:      5,
			wantNil: true,
		},
		// CharClassSearcher at non-zero position
		{
			name:    "charclass_at_5",
			pattern: `[a-z]+`,
			input:   "12345abcdef",
			at:      5,
			want:    "abcdef",
		},
		// CompositeSearcher at non-zero position
		{
			name:    "composite_at_3",
			pattern: `[a-zA-Z]+\d+`,
			input:   "...abc123...",
			at:      3,
			want:    "abc123",
		},
		// Digit prefilter at non-zero
		{
			name:    "digit_at_5",
			pattern: `\d+`,
			input:   "text 42 more",
			at:      4,
			want:    "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			match := engine.FindAt([]byte(tt.input), tt.at)
			if tt.wantNil {
				if match != nil {
					t.Errorf("expected nil, got %q at (%d,%d)", match.String(), match.Start(), match.End())
				}
				return
			}

			if match == nil {
				t.Fatalf("expected match %q, got nil (strategy=%d)", tt.want, engine.strategy)
			}
			if match.String() != tt.want {
				t.Errorf("got %q, want %q", match.String(), tt.want)
			}
		})
	}
}

// TestFindIndicesAtNonZeroDispatch exercises FindIndicesAt dispatch for various strategies.
// Covers: find_indices.go FindIndicesAt (47%), findIndicesDFAAt (0%),
//
//	findIndicesAdaptiveAt (0%), findIndicesBranchDispatchAt (0%),
//	findIndicesReverseSuffixSetAt (0%), findIndicesReverseInnerAt (0%),
//	findIndicesMultilineReverseSuffixAt (0%), findIndicesAnchoredLiteralAt (0%)
func TestFindIndicesAtNonZeroDispatch(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		at        int
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// NFA
		{
			name:      "nfa_at_0",
			pattern:   `ab`,
			input:     "xab",
			at:        0,
			wantStart: 1, wantEnd: 3, wantFound: true,
		},
		{
			name:      "nfa_at_2",
			pattern:   `ab`,
			input:     "xxab",
			at:        2,
			wantStart: 2, wantEnd: 4, wantFound: true,
		},
		// CharClassSearcher at position
		{
			name:      "charclass_at_5",
			pattern:   `[a-z]+`,
			input:     "12345hello",
			at:        5,
			wantStart: 5, wantEnd: 10, wantFound: true,
		},
		// Bounded backtracker at position
		{
			name:      "bounded_at_3",
			pattern:   `(\w+)`,
			input:     "   hello world",
			at:        3,
			wantStart: 3, wantEnd: 8, wantFound: true,
		},
		// Anchored at non-zero = no match
		{
			name:      "anchored_at_1",
			pattern:   `^start`,
			input:     "start again",
			at:        1,
			wantStart: -1, wantEnd: -1, wantFound: false,
		},
		// Digit prefilter at position
		{
			name:      "digit_at_5",
			pattern:   `\d+`,
			input:     "text 42 end",
			at:        5,
			wantStart: 5, wantEnd: 7, wantFound: true,
		},
		// Composite at position
		{
			name:      "composite_at_2",
			pattern:   `[a-zA-Z]+\d+`,
			input:     "..abc99..",
			at:        2,
			wantStart: 2, wantEnd: 7, wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			s, e, found := engine.FindIndicesAt([]byte(tt.input), tt.at)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v (strategy=%d)", found, tt.wantFound, engine.strategy)
			}
			if found {
				if s != tt.wantStart || e != tt.wantEnd {
					t.Errorf("got (%d,%d), want (%d,%d)", s, e, tt.wantStart, tt.wantEnd)
				}
			}
		})
	}
}

// TestFindAllExercisesFindAtPaths exercises FindAll which internally calls FindAt/FindIndicesAt.
// This covers the At paths for all strategies that have patterns producing multiple matches.
// Covers: find.go findAtNonZero path for Teddy, AhoCorasick
//
//	find_indices.go findIndicesTeddyAt, findIndicesAhoCorasickAt
func TestFindAllExercisesFindAtPaths(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		input     string
		wantCount int
	}{
		// Teddy: literal alternation
		{
			name:      "teddy_multi_match",
			pattern:   `foo|bar|baz`,
			input:     "foo and bar and baz",
			wantCount: 3,
		},
		// AhoCorasick: large literal alternation
		{
			name:      "aho_multi_match",
			pattern:   `alpha|beta|gamma|delta|epsilon|zeta|eta|theta|iota`,
			input:     "alpha and beta and gamma",
			wantCount: 3,
		},
		// Digit prefilter with multiple matches
		{
			name:      "digit_multi",
			pattern:   `\d+`,
			input:     "a1b22c333",
			wantCount: 3,
		},
		// CharClass with multiple matches
		{
			name:      "charclass_multi",
			pattern:   `[A-Z]+`,
			input:     "ABC def GHI jkl MNO",
			wantCount: 3,
		},
		// Composite with multiple matches
		{
			name:      "composite_multi",
			pattern:   `[a-z]+\d+`,
			input:     "ab12 cd34 ef56",
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			input := []byte(tt.input)
			indices := engine.FindAllIndicesStreaming(input, 0, nil)
			if len(indices) != tt.wantCount {
				t.Errorf("FindAll count: got %d, want %d (strategy=%d)", len(indices), tt.wantCount, engine.strategy)
				for i, idx := range indices {
					t.Logf("  match[%d] = %q at (%d,%d)", i, string(input[idx[0]:idx[1]]), idx[0], idx[1])
				}
			}

			// Verify against stdlib
			re := regexp.MustCompile(tt.pattern)
			stdMatches := re.FindAllIndex(input, -1)
			if len(indices) != len(stdMatches) {
				t.Errorf("stdlib mismatch: got %d, stdlib %d", len(indices), len(stdMatches))
			}
		})
	}
}

// TestCountExercisesStateReuse exercises Count which uses findIndicesAtWithState internally.
// Covers: find_indices.go findIndicesAtWithState (52%), Count (80%)
func TestCountExercisesStateReuse(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    int
	}{
		{"simple_literal", `abc`, "abc abc abc", 3},
		{"digit_multi", `\d+`, "1 22 333 4444", 4},
		{"charclass", `[aeiou]+`, "aeiou xx eee", 2},
		{"empty_pattern", `a*`, "bbb", 4}, // matches empty at each position + end
		{"no_match", `xyz`, "abc def ghi", 0},
		{"teddy_count", `foo|bar`, "foo bar foo", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			got := engine.Count([]byte(tt.input), -1)
			if got != tt.want {
				t.Errorf("Count(%q, %q) = %d, want %d (strategy=%d)", tt.pattern, tt.input, got, tt.want, engine.strategy)
			}
		})
	}
}

// TestCountWithLimitBranches exercises Count with n > 0 limit.
// Covers: findall.go Count limit check branch
func TestCountWithLimitBranches(t *testing.T) {
	engine, err := Compile(`\w+`)
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("one two three four five")
	got := engine.Count(input, 3)
	if got != 3 {
		t.Errorf("Count with limit 3: got %d, want 3", got)
	}

	got = engine.Count(input, 0)
	if got != 0 {
		t.Errorf("Count with limit 0: got %d, want 0", got)
	}
}

// TestFindAllStreamingNonCharClass exercises FindAllIndicesStreaming fallback.
// Covers: findall.go FindAllIndicesStreaming non-charclass path
func TestFindAllStreamingNonCharClass(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("a1b22c333")
	results := engine.FindAllIndicesStreaming(input, 0, nil)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// With limit
	results = engine.FindAllIndicesStreaming(input, 2, nil)
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit, got %d", len(results))
	}
}

// TestFindAllLargeInputCorrectness validates correctness on larger inputs.
// Covers: various At methods called in the FindAll loop.
func TestFindAllLargeInputCorrectness(t *testing.T) {
	patterns := []string{
		`\d+`,
		`[a-z]+`,
		`[a-zA-Z]+\d+`,
		`foo|bar|baz`,
	}

	// Build input with many matches
	input := strings.Repeat("foo123 bar456 baz789 abc def ", 20)

	for _, pat := range patterns {
		t.Run(pat, func(t *testing.T) {
			engine, err := Compile(pat)
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile(pat)

			inputBytes := []byte(input)
			coregexIndices := engine.FindAllIndicesStreaming(inputBytes, 0, nil)
			stdlibMatches := re.FindAllStringIndex(input, -1)

			if len(coregexIndices) != len(stdlibMatches) {
				t.Errorf("count mismatch: coregex=%d, stdlib=%d", len(coregexIndices), len(stdlibMatches))
				return
			}

			for i := range coregexIndices {
				coregexStr := string(inputBytes[coregexIndices[i][0]:coregexIndices[i][1]])
				stdlibStr := input[stdlibMatches[i][0]:stdlibMatches[i][1]]
				if coregexStr != stdlibStr {
					t.Errorf("match[%d]: coregex=%q, stdlib=%q", i, coregexStr, stdlibStr)
					break
				}
			}
		})
	}
}
