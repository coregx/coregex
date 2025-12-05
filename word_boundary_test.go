package coregex

import (
	"regexp"
	"testing"
)

// TestWordBoundary tests \b word boundary assertions.
// \b matches at positions where the previous and next characters
// have different word/non-word status.
func TestWordBoundary(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
		wantLoc []int // nil = don't check location
	}{
		// Basic \b at start of word
		{"word_start_match", `\bword`, "hello word", true, []int{6, 10}},
		{"word_start_at_string_start", `\bword`, "word end", true, []int{0, 4}},
		{"word_start_no_match_inside", `\bword`, "sword", false, nil},
		{"word_start_no_match_embedded", `\bword`, "password", false, nil},

		// Basic \b at end of word
		{"word_end_match", `word\b`, "word!", true, []int{0, 4}},
		{"word_end_at_string_end", `word\b`, "test word", true, []int{5, 9}},
		{"word_end_no_match_inside", `word\b`, "words", false, nil},

		// \b on both sides (whole word)
		{"whole_word_match", `\bword\b`, "a word here", true, []int{2, 6}},
		{"whole_word_at_start", `\bword\b`, "word here", true, []int{0, 4}},
		{"whole_word_at_end", `\bword\b`, "here word", true, []int{5, 9}},
		{"whole_word_alone", `\bword\b`, "word", true, []int{0, 4}},
		{"whole_word_no_match_prefix", `\bword\b`, "aword", false, nil},
		{"whole_word_no_match_suffix", `\bword\b`, "worda", false, nil},
		{"whole_word_no_match_embedded", `\bword\b`, "swords", false, nil},

		// Word characters: [a-zA-Z0-9_]
		{"underscore_is_word_char", `\b_test\b`, "a _test here", true, []int{2, 7}},
		{"digit_is_word_char", `\btest123\b`, "x test123 y", true, []int{2, 9}},
		{"mixed_word_chars", `\bA_1\b`, "x A_1 y", true, []int{2, 5}},

		// Edge cases at string boundaries
		{"at_empty_string_no_word", `\b`, "", false, nil},
		{"at_start_entering_word", `\ba`, "abc", true, []int{0, 1}},
		{"at_start_not_entering_word", `\b `, " abc", false, nil},
		{"at_end_leaving_word", `c\b`, "abc", true, []int{2, 3}},

		// Multiple matches with FindAll (testing unanchored search)
		{"multiple_words", `\bthe\b`, "the cat and the dog", true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := re.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("MatchString(%q) = %v, want %v", tt.input, got, tt.want)
			}

			if tt.wantLoc != nil && got {
				loc := re.FindStringIndex(tt.input)
				if loc == nil {
					t.Errorf("FindStringIndex(%q) = nil, want %v", tt.input, tt.wantLoc)
				} else if loc[0] != tt.wantLoc[0] || loc[1] != tt.wantLoc[1] {
					t.Errorf("FindStringIndex(%q) = %v, want %v", tt.input, loc, tt.wantLoc)
				}
			}
		})
	}
}

// TestNoWordBoundary tests \B non-word boundary assertions.
// \B matches at positions where the previous and next characters
// have the SAME word/non-word status.
func TestNoWordBoundary(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
		wantLoc []int
	}{
		// \B inside a word
		{"inside_word", `o\Br`, "word", true, []int{1, 3}},
		{"inside_word_start", `\Bord`, "word", true, []int{1, 4}},
		{"inside_word_end", `wor\B`, "word", true, []int{0, 3}},

		// \B between non-word chars
		{"between_spaces", ` \B `, "a   b", true, []int{1, 3}},
		{"between_punctuation", `!\B!`, "wow!! cool", true, []int{3, 5}},

		// \B should NOT match at word boundaries
		{"not_at_word_start", `\Bword`, "hello word", false, nil},
		{"not_at_word_end", `word\B`, "word!", false, nil},
		{"not_at_string_start_word", `\Ba`, "abc", false, nil},
		{"not_at_string_end_word", `c\B`, "abc", false, nil},

		// Edge case: \B at string boundaries with non-word
		{"string_start_non_word", `\B!`, " !", true, []int{1, 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			got := re.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("MatchString(%q) = %v, want %v", tt.input, got, tt.want)
			}

			if tt.wantLoc != nil && got {
				loc := re.FindStringIndex(tt.input)
				if loc == nil {
					t.Errorf("FindStringIndex(%q) = nil, want %v", tt.input, tt.wantLoc)
				} else if loc[0] != tt.wantLoc[0] || loc[1] != tt.wantLoc[1] {
					t.Errorf("FindStringIndex(%q) = %v, want %v", tt.input, loc, tt.wantLoc)
				}
			}
		})
	}
}

// TestWordBoundaryVsStdlib compares our word boundary implementation against Go stdlib.
// This is the critical correctness test.
func TestWordBoundaryVsStdlib(t *testing.T) {
	tests := []struct {
		pattern string
		inputs  []string
	}{
		// Basic word boundary patterns
		{`\bword\b`, []string{
			"word",
			"word!",
			"!word",
			"a word here",
			"words",
			"sword",
			"password",
			"",
		}},
		{`\btest`, []string{
			"test",
			"testing",
			"atest",
			"a test",
			"",
		}},
		{`test\b`, []string{
			"test",
			"atest",
			"testing",
			"test!",
			"",
		}},

		// Non-word boundary patterns
		{`\Bword`, []string{
			"sword",
			"password",
			"word",
			"a word",
		}},
		{`word\B`, []string{
			"words",
			"wording",
			"word",
			"word!",
		}},
		{`\Bor\B`, []string{
			"word",
			"or",
			"for",
			"ore",
		}},

		// Complex patterns
		{`\b[a-z]+\b`, []string{
			"hello",
			"hello world",
			"123hello",
			"hello123",
			"",
		}},
		{`\b\d+\b`, []string{
			"123",
			"test 123 end",
			"test123",
			"123test",
			"",
		}},

		// Edge cases
		{`^\bword`, []string{
			"word",
			"sword",
			"word!",
		}},
		{`word\b$`, []string{
			"word",
			"words",
			"test word",
		}},

		// Multiple word boundaries
		{`\bthe\b.*\bcat\b`, []string{
			"the cat",
			"the big cat",
			"atheist category",
			"the cathedral",
		}},
	}

	for _, tt := range tests {
		stdlibRe := regexp.MustCompile(tt.pattern)
		coregexRe, err := Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
		}

		for _, input := range tt.inputs {
			t.Run(tt.pattern+"_"+input, func(t *testing.T) {
				stdlibMatch := stdlibRe.MatchString(input)
				coregexMatch := coregexRe.MatchString(input)

				if stdlibMatch != coregexMatch {
					t.Errorf("pattern %q, input %q: stdlib=%v, coregex=%v",
						tt.pattern, input, stdlibMatch, coregexMatch)
				}

				// Also check FindString
				stdlibFind := stdlibRe.FindString(input)
				coregexFind := coregexRe.FindString(input)

				if stdlibFind != coregexFind {
					t.Errorf("FindString pattern %q, input %q: stdlib=%q, coregex=%q",
						tt.pattern, input, stdlibFind, coregexFind)
				}

				// Check FindStringIndex
				stdlibLoc := stdlibRe.FindStringIndex(input)
				coregexLoc := coregexRe.FindStringIndex(input)

				if (stdlibLoc == nil) != (coregexLoc == nil) {
					t.Errorf("FindStringIndex pattern %q, input %q: stdlib=%v, coregex=%v",
						tt.pattern, input, stdlibLoc, coregexLoc)
				} else if stdlibLoc != nil && coregexLoc != nil {
					if stdlibLoc[0] != coregexLoc[0] || stdlibLoc[1] != coregexLoc[1] {
						t.Errorf("FindStringIndex pattern %q, input %q: stdlib=%v, coregex=%v",
							tt.pattern, input, stdlibLoc, coregexLoc)
					}
				}
			})
		}
	}
}

// TestWordBoundaryFindAll tests that FindAllString works correctly with word boundaries.
func TestWordBoundaryFindAll(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    []string
	}{
		{`\bword\b`, "word word word", []string{"word", "word", "word"}},
		{`\b\w+\b`, "hello world", []string{"hello", "world"}},
		{`\b[0-9]+\b`, "test 123 and 456 end", []string{"123", "456"}},
		{`\bthe\b`, "the cat and the dog", []string{"the", "the"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			stdlibRe := regexp.MustCompile(tt.pattern)
			coregexRe, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			stdlibAll := stdlibRe.FindAllString(tt.input, -1)
			coregexAll := coregexRe.FindAllString(tt.input, -1)

			if len(stdlibAll) != len(coregexAll) {
				t.Errorf("FindAllString len: stdlib=%d, coregex=%d",
					len(stdlibAll), len(coregexAll))
				t.Errorf("stdlib=%v, coregex=%v", stdlibAll, coregexAll)
				return
			}

			for i := range stdlibAll {
				if stdlibAll[i] != coregexAll[i] {
					t.Errorf("FindAllString[%d]: stdlib=%q, coregex=%q",
						i, stdlibAll[i], coregexAll[i])
				}
			}
		})
	}
}

// TestIssue12_WordBoundary is the specific test case from the GitHub issue.
func TestIssue12_WordBoundary(t *testing.T) {
	// Issue #12: \B word boundary should work
	pattern := `\Bword`
	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) error: %v", pattern, err)
	}

	// Should match "word" inside "sword" (not at word boundary)
	if !re.MatchString("sword") {
		t.Errorf("pattern %q should match 'sword'", pattern)
	}

	// Should NOT match "word" at the start (at word boundary)
	if re.MatchString("word") {
		t.Errorf("pattern %q should NOT match 'word' alone", pattern)
	}

	// Compare with stdlib
	stdlibRe := regexp.MustCompile(pattern)
	inputs := []string{"sword", "password", "word", "a word", ""}
	for _, input := range inputs {
		stdlib := stdlibRe.MatchString(input)
		coregex := re.MatchString(input)
		if stdlib != coregex {
			t.Errorf("pattern %q, input %q: stdlib=%v, coregex=%v",
				pattern, input, stdlib, coregex)
		}
	}
}

// BenchmarkWordBoundary benchmarks word boundary matching.
func BenchmarkWordBoundary(b *testing.B) {
	pattern := `\bword\b`
	input := "This is a test with the word appearing here and the word again."

	coregexRe, err := Compile(pattern)
	if err != nil {
		b.Fatal(err)
	}
	stdlibRe := regexp.MustCompile(pattern)

	b.Run("coregex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			coregexRe.FindAllString(input, -1)
		}
	})

	b.Run("stdlib", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			stdlibRe.FindAllString(input, -1)
		}
	})
}
