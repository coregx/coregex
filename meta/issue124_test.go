package meta

import (
	"regexp"
	"strings"
	"testing"
)

// Issue #124: Two correctness bugs reported by @kostya via regexdna benchmark.
//
// Bug 1: ReverseSuffix strategy dropped epsilon edges in reverse NFA for patterns
// with 2+ variable-length groups (e.g., \d+\.\d+\.\d+\.35). Fix: guard in
// isSafeForReverseSuffix + forward DFA verification after reverse match.
//
// Bug 2: PikeVM non-greedy quantifiers behaved greedily due to tookLeft flag
// leaking from UTF-8 alternation chains. Fix: removed tookLeft/priority system,
// replaced with Rust's DFS-ordering + break-on-first-match approach.
//
// See: https://github.com/coregx/coregex/issues/124

// ---------------------------------------------------------------------------
// Bug 2: Non-greedy PikeVM — core regression tests
// ---------------------------------------------------------------------------

// TestIssue124_NonGreedy_TemplateDelimiters tests the original bug pattern from
// @kostya: non-greedy matching of template delimiters like {{ ... }}.
// Before fix: \{\{.*?\s*\}\} on "{{ a }} {{ b }}" returned "{{ a }} {{ b }}"
// After fix: correctly returns "{{ a }}" (first delimited block).
func TestIssue124_NonGreedy_TemplateDelimiters(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantFind  string
		wantCount int
	}{
		{
			name:      "mustache_basic",
			pattern:   `\{\{.*?\}\}`,
			haystack:  "{{ a }} {{ b }}",
			wantFind:  "{{ a }}",
			wantCount: 2,
		},
		{
			name:      "mustache_with_whitespace_quantifier",
			pattern:   `\{\{.*?\s*\}\}`,
			haystack:  "{{ a }} {{ b }}",
			wantFind:  "{{ a }}",
			wantCount: 2,
		},
		{
			name:      "mustache_with_capture",
			pattern:   `\{\{\s*(.*?)\s*\}\}`,
			haystack:  "{{ a }} {{ b }}",
			wantFind:  "{{ a }}",
			wantCount: 2,
		},
		{
			name:      "mustache_adjacent",
			pattern:   `\{\{.*?\}\}`,
			haystack:  "{{a}}{{b}}{{c}}",
			wantFind:  "{{a}}",
			wantCount: 3,
		},
		{
			name:      "mustache_nested_content",
			pattern:   `\{\{.*?\}\}`,
			haystack:  "before {{hello world}} middle {{foo bar}} after",
			wantFind:  "{{hello world}}",
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			haystack := []byte(tt.haystack)

			// Verify Find returns first match
			m := engine.Find(haystack)
			if m == nil {
				t.Fatalf("Find(%q) = nil, want %q", tt.haystack, tt.wantFind)
			}
			if got := m.String(); got != tt.wantFind {
				t.Errorf("Find(%q) = %q, want %q", tt.haystack, got, tt.wantFind)
			}

			// Verify Count matches stdlib
			stdRe := regexp.MustCompile(tt.pattern)
			stdCount := len(stdRe.FindAllStringIndex(tt.haystack, -1))
			coreCount := engine.Count(haystack, -1)
			if coreCount != stdCount {
				t.Errorf("Count = %d, stdlib = %d", coreCount, stdCount)
			}
			if coreCount != tt.wantCount {
				t.Errorf("Count = %d, want %d", coreCount, tt.wantCount)
			}
		})
	}
}

// TestIssue124_NonGreedy_CommonPatterns tests widely-used non-greedy patterns
// from real-world codebases: HTML tags, quoted strings, comments.
func TestIssue124_NonGreedy_CommonPatterns(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantCount int
	}{
		// Quoted strings
		{"double_quoted", `"(.*?)"`, `"a" "b" "c"`, 3},
		{"single_quoted", `'(.*?)'`, `'hello' 'world'`, 2},
		{"backtick", "`(.*?)`", "`code1` text `code2`", 2},

		// HTML/XML tags
		{"html_tags", `<(.*?)>`, "<a> <b> <c>", 3},
		{"html_attrs", `<[^>]*?>`, `<div class="x"> <span id="y">`, 2},

		// C-style comments
		{"c_comments", `/\*.*?\*/`, "/* a */ code /* b */", 2},

		// Parenthesized groups
		{"parens", `\(.*?\)`, "(x) (y) (z)", 3},

		// Bracket groups
		{"brackets", `\[.*?\]`, "[1] [2] [3]", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			haystack := []byte(tt.haystack)

			// Compare count with stdlib
			stdRe := regexp.MustCompile(tt.pattern)
			stdCount := len(stdRe.FindAllStringIndex(tt.haystack, -1))
			coreCount := engine.Count(haystack, -1)

			if coreCount != stdCount {
				t.Errorf("Count = %d, stdlib = %d", coreCount, stdCount)
			}
			if coreCount != tt.wantCount {
				t.Errorf("Count = %d, want %d", coreCount, tt.wantCount)
			}
		})
	}
}

// TestIssue124_NonGreedy_QuantifierVariants tests all non-greedy quantifier
// forms: *?, +?, ??, {n,m}? — ensuring none exhibit greedy behavior.
func TestIssue124_NonGreedy_QuantifierVariants(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
	}{
		// .*? (star non-greedy)
		{"star_nongreedy", `a.*?b`, "axbxxb", 0, 3},
		{"star_nongreedy_empty", `a.*?a`, "axa", 0, 3},

		// .+? (plus non-greedy)
		{"plus_nongreedy", `a.+?b`, "axybxxb", 0, 4},
		{"plus_nongreedy_min", `a.+?b`, "axb", 0, 3},

		// ?? (quest non-greedy)
		{"quest_nongreedy", `ab??`, "ab", 0, 1},
		{"quest_nongreedy_needed", `ab??c`, "abc", 0, 3},

		// {n,m}? (repeat non-greedy)
		{"repeat_nongreedy", `a{2,4}?`, "aaaa", 0, 2},
		{"repeat_nongreedy_3", `a{2,4}?b`, "aaaab", 0, 5},

		// Mixed greedy and non-greedy in same pattern
		{"mixed_greedy_first", `a.*b.*?c`, "axbycxbzc", 0, 9},

		// Non-greedy with character classes
		{"charclass_nongreedy", `[a-z]+?x`, "abcx", 0, 4},
		{"digit_nongreedy", `\d+?\.`, "123.", 0, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			start, end, found := engine.FindIndices([]byte(tt.haystack))
			if !found {
				t.Fatalf("FindIndices(%q) = not found, want (%d, %d)", tt.haystack, tt.wantStart, tt.wantEnd)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("FindIndices(%q) = (%d, %d), want (%d, %d)",
					tt.haystack, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestIssue124_NonGreedy_VsGreedy verifies that greedy and non-greedy variants
// produce different (correct) results on the same input.
func TestIssue124_NonGreedy_VsGreedy(t *testing.T) {
	tests := []struct {
		name       string
		greedy     string
		nonGreedy  string
		haystack   string
		wantGreedy [2]int
		wantLazy   [2]int
	}{
		{
			name: "star", greedy: `a.*b`, nonGreedy: `a.*?b`,
			haystack: "axbxxb", wantGreedy: [2]int{0, 6}, wantLazy: [2]int{0, 3},
		},
		{
			name: "plus", greedy: `a.+b`, nonGreedy: `a.+?b`,
			haystack: "axybxxb", wantGreedy: [2]int{0, 7}, wantLazy: [2]int{0, 4},
		},
		{
			name: "quest", greedy: `ab?`, nonGreedy: `ab??`,
			haystack: "ab", wantGreedy: [2]int{0, 2}, wantLazy: [2]int{0, 1},
		},
		{
			name: "repeat", greedy: `a{2,5}`, nonGreedy: `a{2,5}?`,
			haystack: "aaaaa", wantGreedy: [2]int{0, 5}, wantLazy: [2]int{0, 2},
		},
		{
			name: "template", greedy: `\{\{.*\}\}`, nonGreedy: `\{\{.*?\}\}`,
			haystack: "{{a}} {{b}}", wantGreedy: [2]int{0, 11}, wantLazy: [2]int{0, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			greedyEngine, err := Compile(tt.greedy)
			if err != nil {
				t.Fatalf("Compile greedy %q: %v", tt.greedy, err)
			}
			lazyEngine, err := Compile(tt.nonGreedy)
			if err != nil {
				t.Fatalf("Compile lazy %q: %v", tt.nonGreedy, err)
			}

			haystack := []byte(tt.haystack)

			gs, ge, gf := greedyEngine.FindIndices(haystack)
			if !gf {
				t.Fatalf("greedy: no match, want %v", tt.wantGreedy)
			}
			if gs != tt.wantGreedy[0] || ge != tt.wantGreedy[1] {
				t.Errorf("greedy = (%d, %d), want %v", gs, ge, tt.wantGreedy)
			}

			ls, le, lf := lazyEngine.FindIndices(haystack)
			if !lf {
				t.Fatalf("lazy: no match, want %v", tt.wantLazy)
			}
			if ls != tt.wantLazy[0] || le != tt.wantLazy[1] {
				t.Errorf("lazy = (%d, %d), want %v", ls, le, tt.wantLazy)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Bug 1: ReverseSuffix strategy — core regression tests
// ---------------------------------------------------------------------------

// TestIssue124_ReverseSuffix_MultiVarLenGroups tests patterns with multiple
// variable-length groups. These now correctly use ReverseSuffix after the
// reverse NFA mixed-edge fix (fillMixedState, v0.12.9). Previously these
// were blocked by wildcardCount >= 2 guard due to a reverse NFA bug.
func TestIssue124_ReverseSuffix_MultiVarLenGroups(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantFind string
	}{
		// @kostya's original pattern — LangArena ips
		{
			name:     "ip_suffix_35",
			pattern:  `\d+\.\d+\.\d+\.35`,
			haystack: "192.168.1.35",
			wantFind: "192.168.1.35",
		},
		{
			name:     "two_digit_groups",
			pattern:  `\d+\.\d+\.35`,
			haystack: "12.34.35",
			wantFind: "12.34.35",
		},
		{
			name:     "alpha_dot_com",
			pattern:  `[a-z]+\.[a-z]+\.com`,
			haystack: "foo.bar.com",
			wantFind: "foo.bar.com",
		},
		{
			name:     "mixed_separators",
			pattern:  `\w+@\w+\.org`,
			haystack: "user@example.org",
			wantFind: "user@example.org",
		},
		{
			name:     "ip_embedded",
			pattern:  `\d+\.\d+\.\d+\.35`,
			haystack: "server at 10.0.0.35 is down",
			wantFind: "10.0.0.35",
		},
		// FindAll correctness — multiple matches
		{
			name:     "ip_multiple",
			pattern:  `\d+\.\d+\.\d+\.35`,
			haystack: "192.168.1.35 and 10.0.0.35",
			wantFind: "192.168.1.35",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			// Multi-wildcard patterns now use ReverseSuffix
			if engine.Strategy() != UseReverseSuffix {
				t.Errorf("pattern %q uses %v, want UseReverseSuffix", tt.pattern, engine.Strategy())
			}

			m := engine.Find([]byte(tt.haystack))
			if m == nil {
				t.Fatalf("Find(%q) = nil, want %q", tt.haystack, tt.wantFind)
			}
			if got := m.String(); got != tt.wantFind {
				t.Errorf("Find(%q) = %q, want %q", tt.haystack, got, tt.wantFind)
			}
		})
	}

	// FindAll correctness: verify match count matches stdlib
	t.Run("find_all_vs_stdlib", func(t *testing.T) {
		multiMatchTests := []struct {
			pattern string
			input   string
			want    int
		}{
			{`\d+\.\d+\.\d+\.35`, "192.168.1.35 and 10.0.0.35 end", 2},
			{`\d+\.\d+\.35`, "1.2.35 and 3.4.35 and 5.6.35", 3},
			{`[a-z]+\.[a-z]+\.com`, "a.b.com x.y.com foo.bar.com", 3},
			{`\w+@\w+\.org`, "a@b.org c@d.org", 2},
		}
		for _, tt := range multiMatchTests {
			engine, _ := Compile(tt.pattern)
			matches := engine.FindAllIndicesStreaming([]byte(tt.input), -1, nil)
			if len(matches) != tt.want {
				t.Errorf("FindAll(%q, %q) = %d matches, want %d", tt.pattern, tt.input, len(matches), tt.want)
			}
		}
	})
}

// TestIssue124_ReverseSuffix_SingleVarLen tests patterns with exactly ONE
// variable-length group — these should still use ReverseSuffix.
func TestIssue124_ReverseSuffix_SingleVarLen(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		wantFind string
	}{
		{"dotstar_txt", `.*\.txt`, "file.txt", "file.txt"},
		{"dotplus_log", `.+\.log`, "app.log", "app.log"},
		{"charclass_conf", `[a-z]+\.conf`, "nginx.conf", "nginx.conf"},
		{"single_digit", `\d+\.35`, "192.35", "192.35"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			m := engine.Find([]byte(tt.haystack))
			if m == nil {
				t.Fatalf("Find(%q) = nil, want %q", tt.haystack, tt.wantFind)
			}
			if got := m.String(); got != tt.wantFind {
				t.Errorf("Find(%q) = %q, want %q", tt.haystack, got, tt.wantFind)
			}
		})
	}
}

// TestIssue124_ReverseSuffix_ForwardVerification tests that forward DFA
// verification produces correct greedy match boundaries.
func TestIssue124_ReverseSuffix_ForwardVerification(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
	}{
		{
			name:      "dotstar_greedy_last_suffix",
			pattern:   `.*\.txt`,
			haystack:  "a.txt.txt",
			wantStart: 0,
			wantEnd:   9,
		},
		{
			name:      "dotstar_greedy_spaces",
			pattern:   `.*\.txt`,
			haystack:  "a.txt b.txt",
			wantStart: 0,
			wantEnd:   11,
		},
		{
			name:      "dotstar_single",
			pattern:   `.*\.log`,
			haystack:  "app.log",
			wantStart: 0,
			wantEnd:   7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			start, end, found := engine.FindIndices([]byte(tt.haystack))
			if !found {
				t.Fatalf("FindIndices(%q) = not found", tt.haystack)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("FindIndices(%q) = (%d, %d), want (%d, %d)",
					tt.haystack, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Comprehensive stdlib comparison
// ---------------------------------------------------------------------------

// TestIssue124_StdlibComparison_Find tests coregex vs stdlib for all Issue #124
// patterns using FindIndices (equivalent to FindStringIndex).
func TestIssue124_StdlibComparison_Find(t *testing.T) {
	type testCase struct {
		pattern  string
		haystack string
	}

	tests := []testCase{
		// Bug 2: Non-greedy patterns
		{`\{\{.*?\}\}`, "{{ a }} {{ b }}"},
		{`\{\{.*?\s*\}\}`, "{{ a }} {{ b }}"},
		{`\{\{\s*(.*?)\s*\}\}`, "{{ a }} {{ b }}"},
		{`"(.*?)"`, `"a" "b" "c"`},
		{`'(.*?)'`, `'hello' 'world'`},
		{`<(.*?)>`, "<a> <b> <c>"},
		{`/\*.*?\*/`, "/* a */ /* b */"},
		{`a.*?b`, "axbxxb"},
		{`a.+?b`, "axybxxb"},
		{`ab??`, "ab"},
		{`a{2,4}?`, "aaaa"},

		// Bug 1: ReverseSuffix patterns
		{`\d+\.\d+\.\d+\.35`, "192.168.1.35"},
		{`\d+\.\d+\.35`, "12.34.35"},
		{`[a-z]+\.[a-z]+\.com`, "foo.bar.com"},
		{`\w+@\w+\.org`, "user@example.org"},

		// Edge cases: empty match, no match
		{`a*?`, "aaa"},
		{`(a|b)*?`, "abab"},
		{`x.*?y`, "no match here"},
		{`\d+\.\d+\.\d+\.99`, "192.168.1.35"},

		// Regression: patterns that must still work
		{`.*\.txt`, "file.txt"},
		{`.*\.txt`, "a.txt.txt"},
		{`.+\.log`, "app.log data.log"},
		{`[a-z]+`, "hello123"},
		{`\d+`, "abc123def456"},
		{`(foo|bar)+`, "foobarfoo"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			stdRe := regexp.MustCompile(tt.pattern)
			stdLoc := stdRe.FindStringIndex(tt.haystack)

			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			start, end, found := engine.FindIndices([]byte(tt.haystack))

			switch {
			case stdLoc == nil && !found:
				// Both no match — correct
			case stdLoc == nil && found:
				t.Errorf("stdlib=nil, coregex=(%d,%d)", start, end)
			case stdLoc != nil && !found:
				t.Errorf("stdlib=%v, coregex=nil", stdLoc)
			case start != stdLoc[0] || end != stdLoc[1]:
				t.Errorf("stdlib=%v, coregex=(%d,%d)", stdLoc, start, end)
			}
		})
	}
}

// TestIssue124_StdlibComparison_Count tests match count consistency with stdlib.
// Count exercises FindAll iteration logic — where non-greedy bugs are most visible.
func TestIssue124_StdlibComparison_Count(t *testing.T) {
	type testCase struct {
		pattern  string
		haystack string
	}

	tests := []testCase{
		// Non-greedy — must produce multiple matches, not one greedy match
		{`\{\{.*?\}\}`, "{{a}} {{b}} {{c}}"},
		{`"(.*?)"`, `"x" "y" "z"`},
		{`<.*?>`, "<a><b><c>"},
		{`/\*.*?\*/`, "/*1*/ /*2*/ /*3*/"},
		{`\(.*?\)`, "(a) (b) (c)"},

		// Greedy — should produce fewer, longer matches
		{`".*"`, `"x" "y" "z"`},
		{`<.*>`, "<a><b><c>"},

		// Multi-var-len (now excluded from ReverseSuffix)
		{`\d+\.\d+\.\d+\.35`, "10.0.0.35 and 192.168.1.35"},

		// Suffix patterns
		{`.*\.txt`, "a.txt b.txt"},
		{`.+\.log`, "x.log y.log z.log"},

		// Simple patterns
		{`\d+`, "a1b22c333d"},
		{`[a-z]+`, "Hello World Test"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			stdRe := regexp.MustCompile(tt.pattern)
			stdCount := len(stdRe.FindAllStringIndex(tt.haystack, -1))

			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			coreCount := engine.Count([]byte(tt.haystack), -1)

			if stdCount != coreCount {
				t.Errorf("count: stdlib=%d, coregex=%d", stdCount, coreCount)
			}
		})
	}
}

// TestIssue124_StdlibComparison_IsMatch tests boolean match consistency.
func TestIssue124_StdlibComparison_IsMatch(t *testing.T) {
	type testCase struct {
		pattern  string
		haystack string
	}

	tests := []testCase{
		{`\{\{.*?\}\}`, "{{ a }}"},
		{`\{\{.*?\}\}`, "no delimiters"},
		{`"(.*?)"`, `has "quotes"`},
		{`"(.*?)"`, `no quotes`},
		{`\d+\.\d+\.\d+\.35`, "192.168.1.35"},
		{`\d+\.\d+\.\d+\.35`, "192.168.1.36"},
		{`.*\.txt`, "file.txt"},
		{`.*\.txt`, "file.log"},
		{`a.*?b`, "ab"},
		{`a.*?b`, "ac"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			stdRe := regexp.MustCompile(tt.pattern)
			stdMatch := stdRe.MatchString(tt.haystack)

			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			coreMatch := engine.IsMatch([]byte(tt.haystack))

			if stdMatch != coreMatch {
				t.Errorf("stdlib=%v, coregex=%v", stdMatch, coreMatch)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Strategy guard regression tests
// ---------------------------------------------------------------------------

// TestIssue124_StrategyGuard_MultiVarLen verifies that isSafeForReverseSuffix
// accepts patterns with multiple variable-length groups (guard removed in v0.12.10
// after reverse NFA mixed-edge fix).
func TestIssue124_StrategyGuard_MultiVarLen(t *testing.T) {
	// All wildcard+suffix patterns should now use ReverseSuffix
	allowed := []struct {
		name    string
		pattern string
	}{
		{"dotstar_txt", `.*\.txt`},
		{"dotplus_log", `.+\.log`},
		{"charclass_conf", `[a-z]+\.conf`},
		{"single_digit", `\d+\.35`},
		{"three_digit_groups", `\d+\.\d+\.\d+\.35`},
		{"two_digit_groups", `\d+\.\d+\.35`},
		{"alpha_groups", `[a-z]+\.[a-z]+\.com`},
		{"word_at_word", `\w+@\w+\.org`},
		{"digit_dash_digit", `\d+-\d+\.35`},
	}

	for _, tt := range allowed {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			if engine.Strategy() != UseReverseSuffix {
				t.Errorf("pattern %q uses %v, want UseReverseSuffix", tt.pattern, engine.Strategy())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Edge cases and boundary conditions
// ---------------------------------------------------------------------------

// TestIssue124_EmptyMatchNonGreedy tests non-greedy quantifiers that can
// match the empty string — the (x+)? transformation edge case.
func TestIssue124_EmptyMatchNonGreedy(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
	}{
		{"star_nongreedy_empty_input", `a*?`, ""},
		{"star_nongreedy_nonempty", `a*?`, "aaa"},
		{"alternation_with_empty", `(|a)*`, "aa"},
		{"optional_group", `(a)?`, "a"},
		{"optional_group_nomatch", `(a)?`, "b"},
		{"nested_optional", `(a*)?`, "aaa"},
		{"empty_alternation_star", `(|x)*`, "xxx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdRe := regexp.MustCompile(tt.pattern)
			stdLoc := stdRe.FindStringIndex(tt.haystack)

			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			start, end, found := engine.FindIndices([]byte(tt.haystack))

			switch {
			case stdLoc == nil && !found:
				// ok
			case stdLoc == nil && found:
				t.Errorf("stdlib=nil, coregex=(%d,%d)", start, end)
			case stdLoc != nil && !found:
				t.Errorf("stdlib=%v, coregex=nil", stdLoc)
			case start != stdLoc[0] || end != stdLoc[1]:
				t.Errorf("stdlib=%v, coregex=(%d,%d)", stdLoc, start, end)
			}
		})
	}
}

// TestIssue124_NonGreedy_LargeInput tests non-greedy matching on larger inputs
// to catch any O(n^2) regressions in the new DFS-ordering implementation.
func TestIssue124_NonGreedy_LargeInput(t *testing.T) {
	pattern := `"(.*?)"`
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString(`"value_`)
		sb.WriteString(strings.Repeat("x", 10))
		sb.WriteString(`" `)
	}
	haystack := sb.String()

	stdRe := regexp.MustCompile(pattern)
	stdCount := len(stdRe.FindAllStringIndex(haystack, -1))

	engine, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error: %v", pattern, err)
	}
	coreCount := engine.Count([]byte(haystack), -1)

	if stdCount != coreCount {
		t.Fatalf("large input: count stdlib=%d, coregex=%d", stdCount, coreCount)
	}
	if coreCount != 500 {
		t.Errorf("expected 500 matches, got %d", coreCount)
	}
}

// TestIssue124_NonGreedy_Unicode tests non-greedy matching with Unicode content.
func TestIssue124_NonGreedy_Unicode(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
	}{
		{"cyrillic_quotes", `"(.*?)"`, `"привет" "мир"`},
		{"chinese_tags", `<(.*?)>`, "<你好> <世界>"},
		{"emoji_brackets", `\[(.*?)\]`, "[a] [b]"},
		{"mixed_nongreedy", `\{(.*?)\}`, "{abc} {def} {ghi}"},
		{"utf8_delimiters", `«(.*?)»`, "«один» «два»"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdRe := regexp.MustCompile(tt.pattern)
			stdCount := len(stdRe.FindAllStringIndex(tt.haystack, -1))

			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			coreCount := engine.Count([]byte(tt.haystack), -1)

			if stdCount != coreCount {
				t.Errorf("count: stdlib=%d, coregex=%d", stdCount, coreCount)
			}

			// Also verify first match position
			stdLoc := stdRe.FindStringIndex(tt.haystack)
			start, end, found := engine.FindIndices([]byte(tt.haystack))

			switch {
			case stdLoc == nil && found:
				t.Errorf("stdlib=nil, coregex=(%d,%d)", start, end)
			case stdLoc != nil && !found:
				t.Errorf("stdlib=%v, coregex=nil", stdLoc)
			case len(stdLoc) >= 2 && (start != stdLoc[0] || end != stdLoc[1]):
				t.Errorf("stdlib=%v, coregex=(%d,%d)", stdLoc, start, end)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// @kostya's regexdna patterns (the original report)
// ---------------------------------------------------------------------------

// TestIssue124_RegexDNA_Patterns tests the specific patterns from the regexdna
// benchmark that @kostya used when discovering the bugs.
func TestIssue124_RegexDNA_Patterns(t *testing.T) {
	patterns := []string{
		`agggtaaa|tttaccct`,
		`[cgt]gggtaaa|tttaccc[acg]`,
		`a[act]ggtaaa|tttacc[agt]t`,
		`ag[act]gtaaa|tttac[agt]ct`,
		`agg[act]taaa|ttta[agt]cct`,
		`aggg[acg]aaa|ttt[cgt]ccct`,
		`agggt[cgt]aa|tt[acg]accct`,
		`agggta[cgt]a|t[acg]taccct`,
		`agggtaa[cgt]|[acg]ttaccct`,
	}

	haystack := "agggtaaaXtttaccctXcgggtaaaXtttacccgX" +
		"aactggtaaaXtttaccagttX" +
		"agactgtaaaXtttacagtctX"

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			stdRe := regexp.MustCompile(pattern)
			stdCount := len(stdRe.FindAllStringIndex(haystack, -1))

			engine, err := Compile(pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", pattern, err)
			}
			coreCount := engine.Count([]byte(haystack), -1)

			if stdCount != coreCount {
				t.Errorf("count: stdlib=%d, coregex=%d", stdCount, coreCount)
			}

			// Verify first match
			stdLoc := stdRe.FindStringIndex(haystack)
			start, end, found := engine.FindIndices([]byte(haystack))

			switch {
			case stdLoc == nil && found:
				t.Errorf("stdlib=nil, coregex=(%d,%d)", start, end)
			case stdLoc != nil && !found:
				t.Errorf("stdlib=%v, coregex=nil", stdLoc)
			case len(stdLoc) >= 2 && (start != stdLoc[0] || end != stdLoc[1]):
				t.Errorf("stdlib=%v, coregex=(%d,%d)", stdLoc, start, end)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FindIndicesAt iteration tests (FindAll via manual loop)
// ---------------------------------------------------------------------------

// TestIssue124_FindIndicesAt_NonGreedyIteration tests that FindIndicesAt
// correctly iterates non-greedy matches like FindAll would.
func TestIssue124_FindIndicesAt_NonGreedyIteration(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     [][2]int
	}{
		{
			name: "mustache_blocks", pattern: `\{\{.*?\}\}`,
			haystack: "{{a}} {{b}} {{c}}",
			want:     [][2]int{{0, 5}, {6, 11}, {12, 17}},
		},
		{
			name: "quoted_strings", pattern: `".*?"`,
			haystack: `"a" "bb" "ccc"`,
			want:     [][2]int{{0, 3}, {4, 8}, {9, 14}},
		},
		{
			name: "html_tags", pattern: `<.*?>`,
			haystack: "<a><b><c>",
			want:     [][2]int{{0, 3}, {3, 6}, {6, 9}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			haystack := []byte(tt.haystack)
			var results [][2]int
			at := 0
			for {
				start, end, found := engine.FindIndicesAt(haystack, at)
				if !found {
					break
				}
				results = append(results, [2]int{start, end})
				if end == at {
					at = end + 1
				} else {
					at = end
				}
				if at >= len(haystack) {
					break
				}
			}

			if len(results) != len(tt.want) {
				t.Fatalf("got %d matches %v, want %d %v",
					len(results), results, len(tt.want), tt.want)
			}
			for i, want := range tt.want {
				if results[i] != want {
					t.Errorf("match[%d] = %v, want %v", i, results[i], want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkIssue124_NonGreedyTemplates benchmarks non-greedy template matching.
func BenchmarkIssue124_NonGreedyTemplates(b *testing.B) {
	engine, _ := Compile(`\{\{.*?\}\}`)
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("{{ value_")
		sb.WriteString(strings.Repeat("x", 20))
		sb.WriteString(" }} text ")
	}
	haystack := []byte(sb.String())

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		engine.Count(haystack, -1)
	}
}

// BenchmarkIssue124_ReverseSuffix_ForwardVerification benchmarks the overhead
// of forward DFA verification in ReverseSuffix.
func BenchmarkIssue124_ReverseSuffix_ForwardVerification(b *testing.B) {
	engine, _ := Compile(`.*\.txt`)
	haystack := []byte(strings.Repeat("word ", 1000) + "file.txt")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		engine.FindIndices(haystack)
	}
}
