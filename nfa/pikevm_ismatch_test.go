package nfa

import (
	"regexp"
	"testing"
)

// TestPikeVM_IsMatch_Basic tests basic boolean matching across pattern types.
func TestPikeVM_IsMatch_Basic(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		// Literals
		{"literal match", "foo", "hello foo world", true},
		{"literal no match", "bar", "hello world", false},
		{"literal empty pattern", "", "anything", true},
		{"literal empty both", "", "", true},
		{"literal empty haystack", "a", "", false},

		// Character classes
		{"digit class match", `\d+`, "abc123", true},
		{"digit class no match", `\d+`, "abcdef", false},
		{"word class match", `\w+`, "hello", true},
		{"word class no match", `\w+`, "   ", false},
		{"space class match", `\s+`, "hello world", true},
		{"space class no match", `\s+`, "helloworld", false},

		// Quantifiers
		{"star zero match", "a*", "bbb", true}, // zero-length match
		{"plus match", "a+", "aaa", true},
		{"plus no match", "a+", "bbb", false},
		{"optional present", "a?b", "ab", true},
		{"optional absent", "a?b", "b", true},
		{"bounded repeat", "a{3}", "aaa", true},
		{"bounded repeat too few", "a{3}", "aa", false},
		{"bounded range", "a{2,4}", "aaa", true},

		// Alternation
		{"alt first", "cat|dog", "the cat sat", true},
		{"alt second", "cat|dog", "hot dog", true},
		{"alt none", "cat|dog", "fish", false},
		{"alt three", "one|two|three", "two", true},

		// Dot
		{"dot match", "a.c", "abc", true},
		{"dot no match short", "a.c", "ac", false},
		{"dot star", "a.*c", "aXXXc", true},
		{"dot not newline", "a.c", "a\nc", false},

		// Anchors
		{"start anchor match", "^hello", "hello world", true},
		{"start anchor no match", "^hello", "say hello", false},
		{"end anchor match", "world$", "hello world", true},
		{"end anchor no match", "world$", "world hello", false},
		{"both anchors", "^exact$", "exact", true},
		{"both anchors no match", "^exact$", "not exact", false},

		// Word boundary
		{"word boundary", `\bfoo\b`, "a foo b", true},
		{"word boundary no match", `\bfoo\b`, "foobar", false},

		// Complex
		{"phone pattern", `\d{3}-\d{4}`, "call 555-1234", true},
		{"phone no match", `\d{3}-\d{4}`, "call 55-1234", false},
		{"email-like", `[a-z]+@[a-z]+\.[a-z]+`, "user@host.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestPikeVM_IsMatch_AnchoredPatterns tests IsMatch with anchored patterns
// exercising the isMatchAnchored code path.
func TestPikeVM_IsMatch_AnchoredPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"anchored start match", "^abc", "abc def", true},
		{"anchored start no match", "^abc", "xabc", false},
		{"anchored end match", "xyz$", "abc xyz", true},
		{"anchored end no match", "xyz$", "xyzabc", false},
		{"full anchored match", "^hello$", "hello", true},
		{"full anchored no match", "^hello$", "hello world", false},
		{"anchored with quantifier", "^a+b$", "aaab", true},
		{"anchored with quantifier no match", "^a+b$", "aabb", false},
		{"anchored empty", "^$", "", true},
		{"anchored empty no match", "^$", "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestPikeVM_IsMatch_Unicode tests IsMatch with Unicode patterns.
func TestPikeVM_IsMatch_Unicode(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"cyrillic match", "мир", "привет мир", true},
		{"cyrillic no match", "мир", "hello world", false},
		{"cjk match", "世界", "你好世界", true},
		{"emoji match", "😀", "test 😀 end", true},
		{"emoji no match", "😀", "no emoji", false},
		{"mixed ascii unicode", `\w+`, "café", true},
		{"unicode class", `\pL+`, "мир123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestPikeVM_IsMatch_VsStdlib verifies IsMatch correctness against stdlib for many patterns.
func TestPikeVM_IsMatch_VsStdlib(t *testing.T) {
	patterns := []string{
		"foo",
		"[a-z]+",
		"[0-9]+",
		"a+",
		"a*",
		"a?",
		"foo|bar",
		"a{2,4}",
		`\d+`,
		`\w+`,
		`\s+`,
		"^hello",
		"world$",
		"a.c",
		"a.*c",
		"(ab)+",
	}

	haystacks := []string{
		"hello foo world",
		"test123abc",
		"aaaaa",
		"",
		"foobar",
		"hello world",
		"  spaces  ",
		"abc",
		"aXXXc",
		"abababab",
	}

	for _, pattern := range patterns {
		stdRE := regexp.MustCompile(pattern)
		for _, haystack := range haystacks {
			t.Run(pattern+"/"+haystack, func(t *testing.T) {
				nfa := mustCompile(t, pattern)
				vm := NewPikeVM(nfa)

				stdResult := stdRE.MatchString(haystack)
				ourResult := vm.IsMatch([]byte(haystack))

				if stdResult != ourResult {
					t.Errorf("IsMatch mismatch: stdlib=%v, ours=%v", stdResult, ourResult)
				}
			})
		}
	}
}

// TestPikeVM_IsMatch_EmptyPatternBehavior tests edge cases around empty patterns.
func TestPikeVM_IsMatch_EmptyPatternBehavior(t *testing.T) {
	nfa := mustCompile(t, "")
	vm := NewPikeVM(nfa)

	// Empty pattern always matches (zero-length match)
	if !vm.IsMatch([]byte("")) {
		t.Error("empty pattern should match empty input")
	}
	if !vm.IsMatch([]byte("anything")) {
		t.Error("empty pattern should match any input")
	}
	if !vm.IsMatch([]byte("мир")) {
		t.Error("empty pattern should match Unicode input")
	}
}

// TestPikeVM_IsMatch_CaptureGroupsDoNotAffect tests that capture groups
// don't affect IsMatch result (captures are ignored for boolean matching).
func TestPikeVM_IsMatch_CaptureGroupsDoNotAffect(t *testing.T) {
	tests := []struct {
		pattern  string
		haystack string
		want     bool
	}{
		{"(abc)", "abc", true},
		{"(abc)", "xyz", false},
		{"(a)(b)(c)", "abc", true},
		{`(\d+)-(\d+)`, "123-456", true},
		{`(\d+)-(\d+)`, "abc-def", false},
		{`(?:abc)`, "abc", true}, // non-capturing group
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.haystack, func(t *testing.T) {
			nfa := mustCompile(t, tt.pattern)
			vm := NewPikeVM(nfa)

			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestPikeVM_SetLongest_Mode tests switching between leftmost-first and leftmost-longest modes.
func TestPikeVM_SetLongest_Mode(t *testing.T) {
	// Pattern: a|aa — leftmost-first should match "a", leftmost-longest should match "aa"
	nfa := mustCompile(t, "a+")
	vm := NewPikeVM(nfa)

	// Default (leftmost-first / greedy)
	start, end, matched := vm.Search([]byte("aaa"))
	if !matched || start != 0 || end != 3 {
		t.Errorf("default mode: got (%d, %d, %v), want (0, 3, true)", start, end, matched)
	}

	// Switch to longest mode
	vm.SetLongest(true)
	start, end, matched = vm.Search([]byte("aaa"))
	if !matched || start != 0 || end != 3 {
		t.Errorf("longest mode: got (%d, %d, %v), want (0, 3, true)", start, end, matched)
	}

	// Switch back
	vm.SetLongest(false)
	start, end, matched = vm.Search([]byte("aaa"))
	if !matched || start != 0 || end != 3 {
		t.Errorf("back to default: got (%d, %d, %v), want (0, 3, true)", start, end, matched)
	}
}

// TestPikeVM_NumStates tests NumStates accessor.
func TestPikeVM_NumStates(t *testing.T) {
	nfa := mustCompile(t, "[a-z]+")
	vm := NewPikeVM(nfa)

	if vm.NumStates() <= 0 {
		t.Errorf("NumStates() = %d, should be > 0", vm.NumStates())
	}
	if vm.NumStates() != nfa.States() {
		t.Errorf("NumStates() = %d, want %d (NFA states)", vm.NumStates(), nfa.States())
	}
}

// TestPikeVM_InitState tests that InitState properly configures external PikeVMState.
func TestPikeVM_InitState(t *testing.T) {
	nfa := mustCompile(t, "[a-z]+")
	vm := NewPikeVM(nfa)

	state := &PikeVMState{}
	vm.InitState(state)

	if state.Visited == nil {
		t.Error("InitState should initialize Visited sparse set")
	}
}
