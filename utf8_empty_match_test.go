package coregex

import (
	"fmt"
	"regexp"
	"testing"
	"unicode/utf8"
)

// TestEmptyMatchUTF8Advancement verifies that empty matches advance by rune,
// not by byte, on multibyte UTF-8 input. This is the stdlib contract.
func TestEmptyMatchUTF8Advancement(t *testing.T) {
	patterns := []string{"a*", "[a-c]*", "x?", "(?:z)*", ""}

	inputs := []struct {
		name  string
		value string
	}{
		{"CJK", "日"},
		{"Cyrillic", "ж"},
		{"Emoji", "😀"},
		{"2byte", "é"},
		{"3byte", "₿"},
		{"mixed", "aé日"},
		{"CJK_multi", "日本語"},
		{"Cyrillic_word", "мир"},
	}

	for _, p := range patterns {
		reStd := regexp.MustCompile(p)
		reCgx := MustCompile(p)

		for _, in := range inputs {
			t.Run(fmt.Sprintf("FindAllIndex/%s/%s", p, in.name), func(t *testing.T) {
				std := reStd.FindAllStringIndex(in.value, -1)
				cgx := reCgx.FindAllStringIndex(in.value, -1)
				if fmt.Sprintf("%v", cgx) != fmt.Sprintf("%v", std) {
					t.Errorf("FindAllStringIndex(%q, %q):\n  coregex=%v\n  stdlib =%v",
						p, in.value, cgx, std)
				}
			})
		}
	}
}

// TestReplaceAllUTF8ProducesValidOutput verifies that ReplaceAll on multibyte
// input never produces invalid UTF-8.
func TestReplaceAllUTF8ProducesValidOutput(t *testing.T) {
	patterns := []string{"a*", "[x]*", ""}

	inputs := []string{"日", "日本語", "мир", "😀🎉", "aéb", "xжy"}

	for _, p := range patterns {
		reStd := regexp.MustCompile(p)
		reCgx := MustCompile(p)

		for _, in := range inputs {
			t.Run(fmt.Sprintf("%s/%s", p, in), func(t *testing.T) {
				stdResult := reStd.ReplaceAllString(in, "-")
				cgxResult := reCgx.ReplaceAllString(in, "-")

				if cgxResult != stdResult {
					t.Errorf("ReplaceAllString(%q, %q, \"-\"):\n  coregex=%q\n  stdlib =%q",
						p, in, cgxResult, stdResult)
				}

				if !utf8.ValidString(cgxResult) {
					t.Errorf("ReplaceAllString(%q, %q, \"-\") produced invalid UTF-8: %q",
						p, in, cgxResult)
				}
			})
		}
	}
}

// TestReplaceAllLiteralUTF8 verifies ReplaceAllLiteral on multibyte input.
func TestReplaceAllLiteralUTF8(t *testing.T) {
	patterns := []string{"a*", ""}

	inputs := []string{"日本語", "мир", "aéb"}

	for _, p := range patterns {
		reStd := regexp.MustCompile(p)
		reCgx := MustCompile(p)

		for _, in := range inputs {
			t.Run(fmt.Sprintf("%s/%s", p, in), func(t *testing.T) {
				stdResult := reStd.ReplaceAllLiteralString(in, "X")
				cgxResult := reCgx.ReplaceAllLiteralString(in, "X")

				if cgxResult != stdResult {
					t.Errorf("ReplaceAllLiteralString(%q, %q, \"X\"):\n  coregex=%q\n  stdlib =%q",
						p, in, cgxResult, stdResult)
				}
			})
		}
	}
}

// TestReplaceAllFuncUTF8 verifies ReplaceAllStringFunc on multibyte input.
func TestReplaceAllFuncUTF8(t *testing.T) {
	re := MustCompile("a*")
	reStd := regexp.MustCompile("a*")

	input := "日本語"
	upper := func(s string) string { return "[" + s + "]" }

	cgxResult := re.ReplaceAllStringFunc(input, upper)
	stdResult := reStd.ReplaceAllStringFunc(input, upper)

	if cgxResult != stdResult {
		t.Errorf("ReplaceAllStringFunc(\"a*\", %q):\n  coregex=%q\n  stdlib =%q",
			input, cgxResult, stdResult)
	}
}

// TestSplitUTF8 verifies Split on multibyte input with empty-matching patterns.
func TestSplitUTF8(t *testing.T) {
	re := MustCompile("a*")
	reStd := regexp.MustCompile("a*")

	inputs := []string{"日本語", "мир", "aéb日"}

	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			cgx := re.Split(in, -1)
			std := reStd.Split(in, -1)

			if fmt.Sprintf("%v", cgx) != fmt.Sprintf("%v", std) {
				t.Errorf("Split(%q, -1):\n  coregex=%v (len=%d)\n  stdlib =%v (len=%d)",
					in, cgx, len(cgx), std, len(std))
			}
		})
	}
}

// TestFindAllSubmatchIndexUTF8 verifies submatch iteration on multibyte input.
func TestFindAllSubmatchIndexUTF8(t *testing.T) {
	re := MustCompile("(a)*")
	reStd := regexp.MustCompile("(a)*")

	input := "日本"

	cgx := re.FindAllStringSubmatchIndex(input, -1)
	std := reStd.FindAllStringSubmatchIndex(input, -1)

	if fmt.Sprintf("%v", cgx) != fmt.Sprintf("%v", std) {
		t.Errorf("FindAllStringSubmatchIndex(\"(a)*\", %q):\n  coregex=%v\n  stdlib =%v",
			input, cgx, std)
	}
}
