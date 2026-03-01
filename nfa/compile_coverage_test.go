package nfa

import (
	"regexp"
	"regexp/syntax"
	"testing"
)

// TestCompile_UTF81ByteRange exercises compileUTF81ByteRange (0% coverage).
// Single-byte UTF-8 is plain ASCII (U+0000 - U+007F). Character classes
// restricted to ASCII ranges trigger this path.
func TestCompile_UTF81ByteRange(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		// [A-Z] is a pure ASCII byte range
		{"upper ascii range", `[A-Z]`, "Hello", true},
		{"upper ascii no match", `[A-Z]`, "hello", false},
		{"single ascii byte", `[x]`, "x", true},
		{"single ascii byte no match", `[x]`, "y", false},
		{"null byte range", `[\x00-\x1f]`, "\x01", true},
		{"full ascii range", `[\x00-\x7f]`, "a", true},
		{"low ascii range", `[a-f]`, "c", true},
		{"low ascii no match", `[a-f]`, "z", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestCompile_NonGreedyStar exercises the non-greedy branch of compileStar.
// Pattern `a*?` (non-greedy star) uses AddSplit instead of AddQuantifierSplit,
// preferring the exit path (shorter match).
func TestCompile_NonGreedyStar(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// a*? prefers zero-length match
		{"non-greedy star empty", `a*?`, "aaa", 0, 0, true},
		// a*?b finds shortest path to b
		{"non-greedy star then literal", `a*?b`, "aaab", 0, 4, true},
		{"non-greedy star then literal short", `a*?b`, "b", 0, 1, true},
		// Verify vs greedy: a*b on "aaab" gives same result (leftmost)
		{"greedy star then literal", `a*b`, "aaab", 0, 4, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestCompile_NonGreedyPlus exercises the non-greedy branch of compilePlus.
// Pattern `a+?` (non-greedy plus) prefers single-character match.
func TestCompile_NonGreedyPlus(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// a+? prefers single 'a'
		{"non-greedy plus single", `a+?`, "aaa", 0, 1, true},
		// a+?b finds shortest path (one a then b)
		{"non-greedy plus then literal", `a+?b`, "aaab", 0, 4, true},
		{"non-greedy plus no match", `a+?`, "bbb", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestCompile_NonGreedyQuest exercises the non-greedy branch of compileQuest.
// Pattern `a??` (non-greedy optional) prefers skipping the match.
func TestCompile_NonGreedyQuest(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		// a?? prefers empty match
		{"non-greedy quest empty", `a??`, "a", 0, 0, true},
		// a??b prefers skipping a, so matches just "b"
		{"non-greedy quest then literal", `a??b`, "ab", 0, 2, true},
		{"non-greedy quest then literal no a", `a??b`, "b", 0, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestCompile_NonGreedy_VsStdlib cross-validates all non-greedy quantifiers
// against Go stdlib to ensure identical leftmost semantics.
func TestCompile_NonGreedy_VsStdlib(t *testing.T) {
	patterns := []string{
		`a*?b`,
		`a+?b`,
		`a??b`,
		`\d*?x`,
		`\d+?x`,
		`[a-z]*?[0-9]`,
		`[a-z]+?[0-9]`,
	}

	inputs := []string{
		"aaab",
		"b",
		"ab",
		"123x",
		"x",
		"abc1",
		"a1",
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)
		n := compileNFAForTest(pattern)
		vm := NewPikeVM(n)

		for _, input := range inputs {
			t.Run(pattern+"/"+input, func(t *testing.T) {
				stdLoc := stdRe.FindStringIndex(input)
				pikeStart, pikeEnd, pikeFound := vm.Search([]byte(input))

				stdMatched := stdLoc != nil
				if stdMatched != pikeFound {
					t.Errorf("match mismatch: stdlib=%v, pike=%v", stdMatched, pikeFound)
					return
				}
				if stdMatched && pikeFound {
					if stdLoc[0] != pikeStart || stdLoc[1] != pikeEnd {
						t.Errorf("position mismatch: stdlib=[%d,%d], pike=[%d,%d]",
							stdLoc[0], stdLoc[1], pikeStart, pikeEnd)
					}
				}
			})
		}
	}
}

// TestHasInternalEndAnchor exercises hasInternalEndAnchor() at 46.2% coverage.
// Tests patterns where $ appears in non-terminal positions within concatenation,
// alternation, and capture groups.
func TestHasInternalEndAnchor(t *testing.T) {
	tests := []struct {
		name string
		re   string
		want bool
	}{
		// No internal anchor - $ at the very end
		{"end only", `abc$`, false},
		{"end in capture", `(abc$)`, false},
		{"end in alt all", `(a$|b$)`, false},

		// Internal anchor - $ NOT at the end
		{"internal dollar concat", `a$b`, true},
		// $ inside capture followed by more content
		{"internal dollar in capture then more", `(a$)b$`, true},

		// Alternation with internal anchor in one branch
		{"alt one branch internal", `(a$b|c$)`, true},

		// Clean pattern without $ at all
		{"no anchor at all", `abc`, false},
		{"no anchor capture", `(abc)`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.re, syntax.Perl)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := hasInternalEndAnchor(re)
			if got != tt.want {
				t.Errorf("hasInternalEndAnchor(%q) = %v, want %v", tt.re, got, tt.want)
			}
		})
	}
}

// TestIsPatternEndAnchored_InternalAnchor tests IsPatternEndAnchored with
// patterns that have both end anchors AND internal anchors, which should
// return false.
func TestIsPatternEndAnchored_InternalAnchor(t *testing.T) {
	tests := []struct {
		name string
		re   string
		want bool
	}{
		{"simple end anchored", `abc$`, true},
		{"internal and end", `a$b$`, false}, // internal $ makes it false
		{"capture end anchored", `(abc$)`, true},
		{"alt all end anchored", `(a$|b$)`, true},
		{"not end anchored", `abc`, false},
		{"alt partial end anchored", `(a$|b)`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.re, syntax.Perl)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := IsPatternEndAnchored(re)
			if got != tt.want {
				t.Errorf("IsPatternEndAnchored(%q) = %v, want %v", tt.re, got, tt.want)
			}
		})
	}
}

// TestIsValidCompositePart exercises isValidCompositePart() at 45.5% coverage.
// Tests edge cases: OpRepeat, OpQuest, bare OpCharClass, and invalid parts.
func TestIsValidCompositePart(t *testing.T) {
	tests := []struct {
		name string
		re   string
		want bool
	}{
		// OpPlus with char class
		{"plus charclass", `[a-z]+`, true},
		// OpStar with char class
		{"star charclass", `[0-9]*`, true},
		// OpQuest with char class
		{"quest charclass", `[a-z]?`, true},
		// OpRepeat with char class
		{"repeat charclass", `[a-z]{2,4}`, true},
		// Bare char class (no quantifier)
		{"bare charclass", `[a-z]`, true},
		// Invalid: literal (not a char class)
		{"literal", `a`, false},
		// Invalid: dot (not a char class)
		{"dot", `.`, false},
		// Invalid: OpPlus with non-charclass sub
		{"plus literal", `a+`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := syntax.Parse(tt.re, syntax.Perl)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			// syntax.Parse wraps in OpCapture, unwrap it
			inner := re
			if re.Op == syntax.OpCapture && len(re.Sub) > 0 {
				inner = re.Sub[0]
			}
			got := isValidCompositePart(inner)
			if got != tt.want {
				t.Errorf("isValidCompositePart(%q [Op=%v]) = %v, want %v",
					tt.re, inner.Op, got, tt.want)
			}
		})
	}
}

// TestIsValidCompositePart_Nil tests nil input to isValidCompositePart.
func TestIsValidCompositePart_Nil(t *testing.T) {
	if isValidCompositePart(nil) {
		t.Error("isValidCompositePart(nil) = true, want false")
	}
}

// TestCompile_UTF83ByteRange exercises compileUTF83ByteRange at 43.8% coverage.
// 3-byte UTF-8 encodes U+0800 - U+FFFF (BMP). Character classes spanning
// this range trigger surrogate gap handling (U+D800-U+DFFF must be excluded).
// Go regex uses actual Unicode chars or \p{} categories, not \uXXXX escapes.
func TestCompile_UTF83ByteRange(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		// CJK Unified Ideographs range [一-龥] - common 3-byte range (U+4E00-U+9FA5)
		{"cjk range match", "[一-龥]", "\u4e16", true},  // 世
		{"cjk range match2", "[一-龥]", "\u754c", true}, // 界
		{"cjk range no match", "[一-龥]", "a", false},
		{"cjk range multi", "[一-龥]+", "世界人民", true},

		// Korean Hangul syllable range [가-힣] (U+AC00-U+D7A3)
		{"korean match", "[가-힣]", "가", true},
		{"korean match2", "[가-힣]", "힣", true},
		{"korean no match", "[가-힣]", "a", false},

		// Wider BMP range spanning multiple lead bytes
		{"wide bmp range", "[Ā-龥]+", "世界", true}, // U+0100-U+9FA5 (2-byte and 3-byte)
		{"wide bmp ascii fail", "[Ā-龥]+", "abc", false},

		// \p{Han} triggers CJK character class (3-byte UTF-8 compilation)
		{"pHan match", `\p{Han}`, "世", true},
		{"pHan no match", `\p{Han}`, "a", false},
		{"pHan multi", `\p{Han}+`, "世界", true},

		// Hiragana range (U+3040-U+309F) - 3-byte range
		{"hiragana match", "[ぁ-ゟ]", "あ", true},
		{"hiragana no match", "[ぁ-ゟ]", "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestCompile_UTF83ByteRange_VsStdlib cross-validates 3-byte UTF-8 character
// class patterns against stdlib regexp.
func TestCompile_UTF83ByteRange_VsStdlib(t *testing.T) {
	patterns := []string{
		"[一-龥]+",   // CJK Ideographs
		"[가-힣]+",   // Korean Hangul
		`\p{Han}+`, // Unicode Han property
		"[ぁ-ゟ]+",   // Hiragana
	}

	inputs := []string{
		"世界",    // CJK
		"가나다",   // Korean
		"あいう",   // Hiragana
		"abc",   // ASCII only
		"世abc界", // mixed
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)
		n := compileNFAForTest(pattern)
		vm := NewPikeVM(n)

		for _, input := range inputs {
			t.Run(pattern+"/"+input, func(t *testing.T) {
				stdMatch := stdRe.MatchString(input)
				pikeMatch := vm.IsMatch([]byte(input))
				if stdMatch != pikeMatch {
					t.Errorf("mismatch: stdlib=%v, pike=%v", stdMatch, pikeMatch)
				}
			})
		}
	}
}

// TestCompile_FoldCaseRune exercises compileFoldCaseRune at 73.7% coverage.
// Case-insensitive patterns (?i) trigger case folding for ASCII letters.
func TestCompile_FoldCaseRune(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		{"lower matches upper", `(?i)hello`, "HELLO", true},
		{"upper matches lower", `(?i)HELLO`, "hello", true},
		{"mixed case", `(?i)Hello`, "hElLo", true},
		{"non-letter unchanged", `(?i)test123`, "TEST123", true},
		{"case insensitive no match", `(?i)hello`, "world", false},
		{"single char fold", `(?i)a`, "A", true},
		{"single char fold lower", `(?i)A`, "a", true},
		{"digit no fold", `(?i)1`, "1", true},
		// Multi-character with mix of letters and non-letters
		{"mixed alphanum", `(?i)abc123def`, "ABC123DEF", true},
		{"partial match", `(?i)ab`, "xABy", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q, %q) = %v, want %v", tt.pattern, tt.haystack, got, tt.want)
			}
		})
	}
}

// TestCompile_FoldCaseRune_Search exercises compileFoldCaseRune through Search
// to verify correct position tracking with case-folded patterns.
func TestCompile_FoldCaseRune_Search(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{"fold at offset", `(?i)abc`, "xxxABCyyy", 3, 6, true},
		{"fold no match", `(?i)abc`, "xxxyyy", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			start, end, found := vm.Search([]byte(tt.haystack))
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && (start != tt.wantStart || end != tt.wantEnd) {
				t.Errorf("got (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestExtractSinglePart_EdgeCases exercises extractSinglePart at 52.5% coverage.
// Tests OpRepeat, nil input, and invalid sub-expressions.
func TestExtractSinglePart_EdgeCases(t *testing.T) {
	// nil input
	if extractSinglePart(nil) != nil {
		t.Error("extractSinglePart(nil) should be nil")
	}

	// OpRepeat with char class: [a-z]{2,4}
	re, err := syntax.Parse(`[a-z]{2,4}`, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}
	inner := re
	if re.Op == syntax.OpCapture && len(re.Sub) > 0 {
		inner = re.Sub[0]
	}
	part := extractSinglePart(inner)
	if part == nil {
		t.Fatal("extractSinglePart([a-z]{2,4}) should not be nil")
	}
	if part.minMatch != 2 || part.maxMatch != 4 {
		t.Errorf("got min=%d, max=%d, want min=2, max=4", part.minMatch, part.maxMatch)
	}

	// OpStar with char class: [a-z]*
	re2, err := syntax.Parse(`[a-z]*`, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}
	inner2 := re2
	if re2.Op == syntax.OpCapture && len(re2.Sub) > 0 {
		inner2 = re2.Sub[0]
	}
	part2 := extractSinglePart(inner2)
	if part2 == nil {
		t.Fatal("extractSinglePart([a-z]*) should not be nil")
	}
	if part2.minMatch != 0 || part2.maxMatch != 0 {
		t.Errorf("star: got min=%d, max=%d, want min=0, max=0", part2.minMatch, part2.maxMatch)
	}

	// OpQuest with char class: [a-z]?
	re3, err := syntax.Parse(`[a-z]?`, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}
	inner3 := re3
	if re3.Op == syntax.OpCapture && len(re3.Sub) > 0 {
		inner3 = re3.Sub[0]
	}
	part3 := extractSinglePart(inner3)
	if part3 == nil {
		t.Fatal("extractSinglePart([a-z]?) should not be nil")
	}
	if part3.minMatch != 0 || part3.maxMatch != 1 {
		t.Errorf("quest: got min=%d, max=%d, want min=0, max=1", part3.minMatch, part3.maxMatch)
	}
}

// TestExtractSinglePart_InvalidSub tests that extractSinglePart returns nil
// for sub-expressions where the inner node is not a char class.
func TestExtractSinglePart_InvalidSub(t *testing.T) {
	// OpPlus with non-charclass (literal group)
	re, err := syntax.Parse(`(abc)+`, syntax.Perl)
	if err != nil {
		t.Fatal(err)
	}
	// Unwrap capture to get the OpPlus
	inner := re
	if re.Op == syntax.OpCapture && len(re.Sub) > 0 {
		inner = re.Sub[0]
	}
	// inner might be OpPlus(OpCapture(OpConcat(...))) or similar
	part := extractSinglePart(inner)
	if part != nil {
		t.Errorf("extractSinglePart((abc)+) should be nil, got min=%d max=%d",
			part.minMatch, part.maxMatch)
	}
}

// TestCompile_UTF83ByteRange_SurrogateEdge tests edge cases around the
// surrogate gap in compileUTF83ByteRange. The gap U+D800-U+DFFF is invalid
// in UTF-8, so ranges must be split around it. We use \p{} properties
// and actual Unicode characters since Go regex does not support \uXXXX.
func TestCompile_UTF83ByteRange_SurrogateEdge(t *testing.T) {
	// A wide range spanning from CJK (3-byte) to Korean (3-byte) exercises
	// the surrogate gap splitting in compileUTF83ByteRange.
	// [一-힣] spans U+4E00 to U+D7A3, which is entirely before surrogates.
	// \p{Co} matches private use area (U+E000+), which is after surrogates.
	tests := []struct {
		name     string
		pattern  string
		haystack string
		want     bool
	}{
		// Range spanning multiple 3-byte lead byte groups
		{"wide 3byte range", "[一-힣]+", "世界가나", true},
		{"wide 3byte no match", "[一-힣]+", "abc", false},
		// Private use area (U+E000-U+F8FF) - after surrogate gap
		{"private use area", `\p{Co}`, "\ue000", true},
		{"private use area no match", `\p{Co}`, "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := compileNFAForTest(tt.pattern)
			vm := NewPikeVM(n)
			got := vm.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.haystack, got, tt.want)
			}
		})
	}
}
