package nfa

import (
	"regexp"
	"regexp/syntax"
	"testing"
)

// Issue #124: PikeVM non-greedy correctness tests.
//
// Root cause: tookLeft flag leaked from UTF-8 alternation chains into quantifier
// priority resets, causing non-greedy .*? to behave greedily.
//
// Fix: Removed tookLeft/priority system entirely. Replaced with Rust's DFS-ordering
// approach: greedy/non-greedy semantics determined solely by branch order in splits
// + break-on-first-match in the combined match-check/step loop.
//
// See: https://github.com/coregx/coregex/issues/124

// TestIssue124_PikeVM_NonGreedyStar tests .*? at the PikeVM level.
func TestIssue124_PikeVM_NonGreedyStar(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantMatch bool
	}{
		// Core non-greedy: should match shortest
		{"basic", `a.*?b`, "axbxxb", 0, 3, true},
		{"adjacent", `a.*?a`, "axa", 0, 3, true},
		{"empty_middle", `a.*?b`, "ab", 0, 2, true},
		{"no_match", `a.*?b`, "axx", -1, -1, false},

		// Template delimiters (original bug pattern)
		{"mustache", `\{\{.*?\}\}`, "{{ a }} {{ b }}", 0, 7, true},
		{"mustache_ws", `\{\{.*?\s*\}\}`, "{{ a }} {{ b }}", 0, 7, true},

		// Quoted strings
		{"dquote", `".*?"`, `"a" "b"`, 0, 3, true},
		{"squote", `'.*?'`, `'x' 'y'`, 0, 3, true},

		// With character classes
		{"digit_lazy", `\d+?\.`, "123.", 0, 4, true},
		{"alpha_lazy", `[a-z]+?x`, "abcx", 0, 4, true},

		// Non-greedy with captures
		{"capture_lazy", `(a.*?b)`, "axbxxb", 0, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.Search([]byte(tt.haystack))

			if match != tt.wantMatch {
				t.Fatalf("match = %v, want %v", match, tt.wantMatch)
			}
			if !tt.wantMatch {
				return
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestIssue124_PikeVM_NonGreedyPlus tests .+? at the PikeVM level.
func TestIssue124_PikeVM_NonGreedyPlus(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
	}{
		{"basic", `a.+?b`, "axyb", 0, 4},
		{"minimal", `a.+?b`, "axb", 0, 3},
		{"multiple", `a.+?b`, "axybxxb", 0, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.Search([]byte(tt.haystack))

			if !match {
				t.Fatalf("no match, want (%d, %d)", tt.wantStart, tt.wantEnd)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestIssue124_PikeVM_NonGreedyQuest tests ?? at the PikeVM level.
func TestIssue124_PikeVM_NonGreedyQuest(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
	}{
		{"quest_nongreedy", `ab??`, "ab", 0, 1},       // prefer NOT consuming 'b'
		{"quest_greedy", `ab?`, "ab", 0, 2},           // prefer consuming 'b'
		{"quest_nongreedy_ctx", `ab??c`, "abc", 0, 3}, // must consume 'b' for 'c' to match
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.Search([]byte(tt.haystack))

			if !match {
				t.Fatalf("no match, want (%d, %d)", tt.wantStart, tt.wantEnd)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestIssue124_PikeVM_NonGreedyRepeat tests {n,m}? at the PikeVM level.
func TestIssue124_PikeVM_NonGreedyRepeat(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
	}{
		{"repeat_2_4_lazy", `a{2,4}?`, "aaaa", 0, 2},
		{"repeat_2_4_greedy", `a{2,4}`, "aaaa", 0, 4},
		{"repeat_1_3_lazy", `a{1,3}?`, "aaa", 0, 1},
		{"repeat_1_3_greedy", `a{1,3}`, "aaa", 0, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.Search([]byte(tt.haystack))

			if !match {
				t.Fatalf("no match, want (%d, %d)", tt.wantStart, tt.wantEnd)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestIssue124_PikeVM_GreedyStillWorks verifies that greedy quantifiers are
// unaffected by the non-greedy fix — they must still match longest.
func TestIssue124_PikeVM_GreedyStillWorks(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
	}{
		{"star_greedy", `a.*b`, "axbxxb", 0, 6},
		{"plus_greedy", `a.+b`, "axybxxb", 0, 7},
		{"quest_greedy", `ab?`, "ab", 0, 2},
		{"repeat_greedy", `a{2,4}`, "aaaa", 0, 4},
		{"charclass_greedy", `[a-z]+`, "hello", 0, 5},
		{"digit_greedy", `\d+`, "12345", 0, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.Search([]byte(tt.haystack))

			if !match {
				t.Fatalf("no match, want (%d, %d)", tt.wantStart, tt.wantEnd)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestIssue124_PikeVM_EmptyMatchableBody tests the (x+)? transformation.
// When x* body can match empty (like (|a)*), standard Thompson construction
// creates incorrect DFS preference. Fix: compile x* as (x+)? to preserve
// correct ordering. Reference: Rust regex issue #779.
func TestIssue124_PikeVM_EmptyMatchableBody(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
	}{
		{"empty_alt_star", `(|a)*`, "aa"},
		{"empty_alt_star_b", `(|b)*`, "bb"},
		{"empty_alt_plus_in_star", `(|a+)*`, "aaa"},
		{"nested_optional_star", `(a?)*`, "aaa"},
		{"optional_in_star", `(a*)*`, "aa"},
		{"empty_alt_three", `(|a|b)*`, "aba"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdRe := regexp.MustCompile(tt.pattern)
			stdLoc := stdRe.FindStringIndex(tt.haystack)

			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.Search([]byte(tt.haystack))

			if stdLoc == nil {
				if match {
					t.Errorf("stdlib=nil, pikevm=(%d,%d)", start, end)
				}
				return
			}
			if !match {
				t.Fatalf("stdlib=%v, pikevm=no match", stdLoc)
			}
			if start != stdLoc[0] || end != stdLoc[1] {
				t.Errorf("stdlib=%v, pikevm=(%d,%d)", stdLoc, start, end)
			}
		})
	}
}

// TestIssue124_PikeVM_UTF8NonGreedy tests non-greedy matching with multi-byte
// UTF-8 content. This exercises the UTF-8 alternation chain code paths that
// were the root cause of the tookLeft bug.
func TestIssue124_PikeVM_UTF8NonGreedy(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
	}{
		// Dot matches multi-byte UTF-8 characters through alternation chains
		{"cyrillic_lazy", `".*?"`, `"привет" "мир"`},
		{"chinese_lazy", `<.*?>`, "<你好> <世界>"},
		{"mixed_lazy", `\{.*?\}`, "{abc} {def}"},

		// Character classes with UTF-8
		{"utf8_charclass", `[а-я]+?x`, "абвx"},

		// Non-greedy with UTF-8 delimiters
		{"utf8_delimiters", `«.*?»`, "«один» «два»"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdRe := regexp.MustCompile(tt.pattern)
			stdLoc := stdRe.FindStringIndex(tt.haystack)

			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.Search([]byte(tt.haystack))

			switch {
			case stdLoc == nil && match:
				t.Errorf("stdlib=nil, pikevm=(%d,%d)", start, end)
			case stdLoc != nil && !match:
				t.Errorf("stdlib=%v, pikevm=no match", stdLoc)
			case stdLoc != nil && match && (start != stdLoc[0] || end != stdLoc[1]):
				t.Errorf("stdlib=%v, pikevm=(%d,%d)", stdLoc, start, end)
			}
		})
	}
}

// TestIssue124_PikeVM_StdlibComprehensive runs a comprehensive comparison
// against Go stdlib for patterns that exercise the fixed code paths.
func TestIssue124_PikeVM_StdlibComprehensive(t *testing.T) {
	patterns := []string{
		// Non-greedy quantifiers
		`a.*?b`, `a.+?b`, `ab??`, `a{2,4}?`,
		// Greedy quantifiers (must still work)
		`a.*b`, `a.+b`, `ab?`, `a{2,4}`,
		// Mixed
		`a.*?b.*c`, `a.*b.*?c`,
		// Alternation (exercises DFS ordering)
		`(foo|bar)`, `(a|b|c)+`,
		// Captures with non-greedy
		`(a.*?b)`, `(a)(.*?)(b)`,
		// Character classes
		`[a-z]+?x`, `[0-9]+?\.`,
	}

	haystacks := []string{
		"axbxxbxxc",
		"axyb",
		"ab",
		"aaaa",
		"foobar",
		"abcabc",
		"abcx",
		"123.456.",
		"",
		"no match",
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)

		compiler := NewDefaultCompiler()
		nfa, err := compiler.Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", pattern, err)
		}
		pikevm := NewPikeVM(nfa)

		for _, haystack := range haystacks {
			t.Run(pattern+"/"+haystack, func(t *testing.T) {
				stdLoc := stdRe.FindStringIndex(haystack)
				start, end, match := pikevm.Search([]byte(haystack))

				switch {
				case stdLoc == nil && match:
					t.Errorf("stdlib=nil, pikevm=(%d,%d)", start, end)
				case stdLoc != nil && !match:
					t.Errorf("stdlib=%v, pikevm=no match", stdLoc)
				case stdLoc != nil && match && (start != stdLoc[0] || end != stdLoc[1]):
					t.Errorf("stdlib=%v, pikevm=(%d,%d)", stdLoc, start, end)
				}
			})
		}
	}
}

// TestIssue124_PikeVM_SearchAt_NonGreedy tests non-greedy with SearchAt offset.
func TestIssue124_PikeVM_SearchAt_NonGreedy(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		at        int
		wantStart int
		wantEnd   int
		wantMatch bool
	}{
		{"from_0", `".*?"`, `"a" "b"`, 0, 0, 3, true},
		{"from_3", `".*?"`, `"a" "b"`, 3, 4, 7, true},
		{"from_end", `".*?"`, `"a" "b"`, 7, -1, -1, false},
		{"lazy_from_offset", `a.*?b`, "xxaxbxxaxxyb", 5, 7, 12, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewDefaultCompiler()
			nfa, err := compiler.Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			pikevm := NewPikeVM(nfa)
			start, end, match := pikevm.SearchAt([]byte(tt.haystack), tt.at)

			if match != tt.wantMatch {
				t.Fatalf("match = %v, want %v", match, tt.wantMatch)
			}
			if !tt.wantMatch {
				return
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestIssue124_Compile_CanMatchEmpty tests the canMatchEmpty helper function
// used by the (x+)? transformation.
func TestIssue124_Compile_CanMatchEmpty(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		// Can match empty
		{"a*", true},
		{"a?", true},
		{"(|a)", true},
		{"(a*)", true},
		{"(a?b?)", true},
		{"(|a)(|b)", true},

		// Cannot match empty
		{"a", false},
		{"a+", false},
		{"ab", false},
		{"[a-z]", false},
		{"[a-z]+", false},
		{".", false},
		{".+", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := syntax.Parse(tt.pattern, syntax.Perl)
			if err != nil {
				t.Fatalf("syntax.Parse(%q) error: %v", tt.pattern, err)
			}
			re = re.Simplify()

			got := canMatchEmpty(re)
			if got != tt.want {
				t.Errorf("canMatchEmpty(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}
