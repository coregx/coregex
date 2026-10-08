package coregex

import (
	"fmt"
	"regexp"
	"testing"
)

// === (1) CompilePOSIX: ^ is multiline, [^a] doesn't match \n ===

func TestCompilePOSIX_Semantics(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{"^ is multiline", `^b`, "a\nb"},
		{"$ is multiline", `a$`, "a\nb"},
		{"[^a] vs newline", `[^a]`, "\n"},
		{"dot vs newline", `.`, "\n"},
		{"POSIX longest a|ab", `a|ab`, "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := CompilePOSIX(tt.pattern)
			if err != nil {
				t.Skipf("CompilePOSIX(%q) failed: %v", tt.pattern, err)
				return
			}
			reStd, _ := regexp.CompilePOSIX(tt.pattern)

			cgx := re.FindString(tt.input)
			std := reStd.FindString(tt.input)

			if cgx != std {
				t.Errorf("CompilePOSIX(%q).FindString(%q): coregex=%q, stdlib=%q",
					tt.pattern, tt.input, cgx, std)
			}
		})
	}
}

// === (2) Empty capture at end of input ===

func TestEmptyCaptureAtEnd(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`(x?)`, "ab"},
		{`(a*)`, "b"},
		{`()`, "abc"},
		{`(a?)`, "b"},
		{`(z*)`, "abc"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.pattern, tt.input), func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			cgx := re.FindStringSubmatchIndex(tt.input)
			std := reStd.FindStringSubmatchIndex(tt.input)

			if fmt.Sprintf("%v", cgx) != fmt.Sprintf("%v", std) {
				t.Errorf("FindStringSubmatchIndex(%q, %q):\n  coregex=%v\n  stdlib =%v",
					tt.pattern, tt.input, cgx, std)
			}
		})
	}
}

// === (3) AllIndex duplicates zero-width matches ===

func TestAllIndexNoDuplicateZeroWidth(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`\b`, "ab cd"},
		{`\b`, "hello"},
		{`^`, "abc"},
		{`$`, "abc"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.pattern, tt.input), func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			cgx := re.FindAllStringIndex(tt.input, -1)
			std := reStd.FindAllStringIndex(tt.input, -1)

			if fmt.Sprintf("%v", cgx) != fmt.Sprintf("%v", std) {
				t.Errorf("FindAllStringIndex(%q, %q):\n  coregex=%v\n  stdlib =%v",
					tt.pattern, tt.input, cgx, std)
			}
		})
	}
}

// === (4) Trivial: Split("","",n) ===

func TestSplitEmptyEmpty(t *testing.T) {
	tests := []struct {
		input string
		n     int
	}{
		{"", -1},
		{"", 0},
		{"", 1},
		{"", 2},
	}

	re := MustCompile("")
	reStd := regexp.MustCompile("")

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			cgx := re.Split(tt.input, tt.n)
			std := reStd.Split(tt.input, tt.n)

			if fmt.Sprintf("%v", cgx) != fmt.Sprintf("%v", std) {
				t.Errorf("Split(\"\", \"\", %d):\n  coregex=%v (len=%d)\n  stdlib =%v (len=%d)",
					tt.n, cgx, len(cgx), std, len(std))
			}
		})
	}
}
