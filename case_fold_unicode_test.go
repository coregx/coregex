package coregex

import (
	"fmt"
	"regexp"
	"testing"
)

// TestCaseFoldUnicode verifies that (?i) works correctly for non-ASCII runes.
// Bug: nfa/compile.go only folds ASCII letters (isASCIILetter guard).
// Non-ASCII runes with FoldCase flag are compiled as exact uppercase match.
//
// Test cases deliberately include 1-rune and 5+ rune patterns which bypass
// the Teddy multi-pattern path and exercise the NFA directly.
func TestCaseFoldUnicode(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		// Cyrillic 1 rune — goes to NFA (Teddy needs ≥3 bytes per literal)
		{`(?i)к`, "к", true},
		{`(?i)к`, "К", true},
		{`(?i)ж`, "ж", true},
		{`(?i)ж`, "Ж", true},

		// Cyrillic 5+ runes — exceeds Teddy MaxLiterals, goes to NFA
		{`(?i)привет`, "привет", true},
		{`(?i)привет`, "ПРИВЕТ", true},
		{`(?i)привет`, "Привет", true},
		{`(?i)привет`, "пРиВеТ", true},
		{`(?i)котенок`, "котенок", true},
		{`(?i)котенок`, "КОТЕНОК", true},
		{`(?i)котенок`, "Котенок", true},
		{`(?i)слово`, "слово", true},
		{`(?i)слово`, "СЛОВО", true},
		{`(?i)слово`, "Слово", true},

		// Greek sigma: orbit of 3 (Σ ↔ σ ↔ ς)
		{`(?i)Σ`, "Σ", true},
		{`(?i)Σ`, "σ", true},
		{`(?i)Σ`, "ς", true}, // final sigma
		{`(?i)σ`, "Σ", true},
		{`(?i)σ`, "ς", true},
		{`(?i)ς`, "Σ", true},
		{`(?i)ς`, "σ", true},

		// Kelvin sign: K (U+004B) ↔ k (U+006B) ↔ K (U+212A)
		{`(?i)K`, "K", true},      // ASCII K
		{`(?i)K`, "k", true},      // ASCII k
		{`(?i)K`, "\u212A", true}, // Kelvin sign

		// Long s: ſ (U+017F) ↔ s (U+0073) ↔ S (U+0053)
		{`(?i)s`, "ſ", true},
		{`(?i)ſ`, "s", true},
		{`(?i)ſ`, "S", true},
		{`(?i)S`, "ſ", true},

		// German sharp s: ß doesn't fold to SS in simple case folding
		// (unicode.SimpleFold(ß) = ß, it's a self-loop — no simple fold partner)
		// But Go stdlib handles it via SimpleFold which includes ẞ (U+1E9E)
		{`(?i)ß`, "ß", true},

		// Non-ASCII Latin: ä ↔ Ä
		{`(?i)ä`, "ä", true},
		{`(?i)ä`, "Ä", true},
		{`(?i)Ä`, "ä", true},
		{`(?i)ö`, "ö", true},
		{`(?i)ö`, "Ö", true},
		{`(?i)ü`, "ü", true},
		{`(?i)ü`, "Ü", true},

		// Mixed script patterns
		{`(?i)café`, "CAFÉ", true},
		{`(?i)café`, "café", true},
		{`(?i)café`, "Café", true},

		// Negative cases — should NOT match
		{`(?i)к`, "a", false},
		{`(?i)привет`, "hello", false},
		{`(?i)Σ`, "S", false}, // Latin S ≠ Greek Sigma
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_on_%q", tt.pattern, tt.input), func(t *testing.T) {
			re, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}

			got := re.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("MatchString(%q, %q) = %v, want %v",
					tt.pattern, tt.input, got, tt.want)
			}

			// Cross-check with stdlib
			reStd := regexp.MustCompile(tt.pattern)
			stdGot := reStd.MatchString(tt.input)
			if got != stdGot {
				t.Errorf("DIVERGENCE from stdlib: coregex=%v, stdlib=%v for MatchString(%q, %q)",
					got, stdGot, tt.pattern, tt.input)
			}
		})
	}
}

// TestCaseFoldUnicodeFindAll verifies FindAll with (?i) on non-ASCII text.
func TestCaseFoldUnicodeFindAll(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`(?i)мир`, "Мир мир МИР"},
		{`(?i)σ`, "Σσς"},
		{`(?i)привет`, "привет ПРИВЕТ Привет"},
		{`(?i)ä`, "äÄäÄ"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			cgx := re.FindAllString(tt.input, -1)
			std := reStd.FindAllString(tt.input, -1)

			if len(cgx) != len(std) {
				t.Errorf("FindAllString(%q, %q): coregex found %d, stdlib found %d\n  coregex=%v\n  stdlib =%v",
					tt.pattern, tt.input, len(cgx), len(std), cgx, std)
			}
		})
	}
}

// TestCaseFoldUnicodeReplace verifies Replace with (?i) on non-ASCII text.
func TestCaseFoldUnicodeReplace(t *testing.T) {
	re := MustCompile(`(?i)мир`)
	reStd := regexp.MustCompile(`(?i)мир`)

	input := "Мир мир МИР"
	cgx := re.ReplaceAllString(input, "world")
	std := reStd.ReplaceAllString(input, "world")

	if cgx != std {
		t.Errorf("ReplaceAllString: coregex=%q, stdlib=%q", cgx, std)
	}
}
