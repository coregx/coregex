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

// === (2) Empty capture at end of input — FindALL last match ===

func TestEmptyCaptureAtEnd_FindAll(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`()`, "abc"},
		{`(|a)*`, "aa"},
		{`()`, "日"},
		{`(x?)`, "ab"},
		{`(a*)`, "b"},
		{`(a?)`, "b"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.pattern, tt.input), func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			cgxAll := re.FindAllStringSubmatchIndex(tt.input, -1)
			stdAll := reStd.FindAllStringSubmatchIndex(tt.input, -1)

			if len(cgxAll) != len(stdAll) {
				t.Errorf("FindAllStringSubmatchIndex(%q, %q): coregex %d matches, stdlib %d matches",
					tt.pattern, tt.input, len(cgxAll), len(stdAll))
				return
			}

			for i := range stdAll {
				if fmt.Sprintf("%v", cgxAll[i]) != fmt.Sprintf("%v", stdAll[i]) {
					t.Errorf("FindAllStringSubmatchIndex(%q, %q) match[%d]:\n  coregex=%v\n  stdlib =%v",
						tt.pattern, tt.input, i, cgxAll[i], stdAll[i])
				}
			}
		})
	}
}

// === (3) AllStringIndex/AllIndex iterator duplicates zero-width ===

func TestAllStringIndex_NoDuplicates(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`\b`, "ab cd"},
		{`\B`, "ab cd"},
		{`$`, "a\n"},
		{`(?m)^`, "日\n本"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%q", tt.pattern, tt.input), func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			// Collect via iterator
			var cgxResults [][]int
			for m := range re.AllStringIndex(tt.input) {
				cgxResults = append(cgxResults, []int{m[0], m[1]})
			}

			stdResults := reStd.FindAllStringIndex(tt.input, -1)

			if fmt.Sprintf("%v", cgxResults) != fmt.Sprintf("%v", stdResults) {
				t.Errorf("AllStringIndex(%q, %q):\n  coregex=%v\n  stdlib =%v",
					tt.pattern, tt.input, cgxResults, stdResults)
			}
		})
	}
}

// === (4a) Split("","",n): nil vs []string{} ===

func TestSplitEmptyEmpty(t *testing.T) {
	re := MustCompile("")
	reStd := regexp.MustCompile("")

	for _, n := range []int{-1, 0, 1, 2} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			cgx := re.Split("", n)
			std := reStd.Split("", n)

			cgxNil := cgx == nil
			stdNil := std == nil

			if cgxNil != stdNil {
				t.Errorf("Split(\"\", \"\", %d): coregex nil=%v (len=%d), stdlib nil=%v (len=%d)",
					n, cgxNil, len(cgx), stdNil, len(std))
			}
			if len(cgx) != len(std) {
				t.Errorf("Split(\"\", \"\", %d): coregex len=%d, stdlib len=%d",
					n, len(cgx), len(std))
			}
		})
	}
}

// === (4b) All(b) iterator: cap == len ===

func TestAllIterator_ThreeIndexSlice(t *testing.T) {
	re := MustCompile(`\w+`)
	input := []byte("hello world")

	for m := range re.All(input) {
		if cap(m) != len(m) {
			t.Errorf("All() match %q: cap=%d > len=%d — append would corrupt buffer",
				m, cap(m), len(m))
		}
	}
}
