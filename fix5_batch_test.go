package coregex

import (
	"fmt"
	"regexp"
	"testing"
)

// === (a) Split(n=1) off-by-one ===

func TestSplitN1(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		n       int
	}{
		{`,`, "a,b,c", 1},
		{`,`, "a,b,c", 2},
		{`,`, "a,b,c", 3},
		{`,`, "a,b,c", 0},
		{`,`, "a,b,c", -1},
		{`,`, "abc", 1},
		{`\s+`, "a b c d", 1},
		{`\s+`, "a b c d", 2},
		{`\s+`, "a b c d", 3},
		{`x`, "abc", 1},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s/n=%d", tt.pattern, tt.input, tt.n), func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			cgx := re.Split(tt.input, tt.n)
			std := reStd.Split(tt.input, tt.n)

			if fmt.Sprintf("%v", cgx) != fmt.Sprintf("%v", std) {
				t.Errorf("Split(%q, %q, %d):\n  coregex=%v (len=%d)\n  stdlib =%v (len=%d)",
					tt.pattern, tt.input, tt.n, cgx, len(cgx), std, len(std))
			}
		})
	}
}

// === (b) Repeated capture groups ===

func TestRepeatedCaptureGroups(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`(aa)*$`, "aaa"},
		{`(a)*`, "aaa"},
		{`(a)+`, "aaa"},
		{`(a)?`, "a"},
		{`(ab)*`, "ababab"},
		{`(a)*$`, "bbb"},
		{`()`, "abc"},
		{`(|a)*$`, "b"},
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

// === (c) LiteralPrefix with FoldCase ===

func TestLiteralPrefixFoldCase(t *testing.T) {
	tests := []string{
		`(?i)abc`,
		`(?i)hello`,
		`(?i)Σ`,
		`(?i)кот`,
		`abc`,    // no fold — should still work
		`^hello`, // anchored
	}

	for _, p := range tests {
		t.Run(p, func(t *testing.T) {
			re := MustCompile(p)
			reStd := regexp.MustCompile(p)

			prefix, complete := re.LiteralPrefix()
			prefixStd, completeStd := reStd.LiteralPrefix()

			if prefix != prefixStd || complete != completeStd {
				t.Errorf("LiteralPrefix(%q): coregex=(%q,%v), stdlib=(%q,%v)",
					p, prefix, complete, prefixStd, completeStd)
			}
		})
	}
}

// === (d) CompilePOSIX rejects Perl syntax ===

func TestCompilePOSIXRejectsPerl(t *testing.T) {
	perlOnly := []string{`\d+`, `\w+`, `\s+`, `(?i)hello`, `\pL`, `(?:abc)`}

	for _, p := range perlOnly {
		t.Run(p, func(t *testing.T) {
			_, errCgx := CompilePOSIX(p)
			_, errStd := regexp.CompilePOSIX(p)

			cgxOk := errCgx == nil
			stdOk := errStd == nil

			if cgxOk != stdOk {
				t.Errorf("CompilePOSIX(%q): coregex err=%v, stdlib err=%v",
					p, errCgx, errStd)
			}
		})
	}

	// Also verify POSIX patterns still work
	posixPatterns := []string{`[[:alpha:]]+`, `[a-z]+`, `a|b`, `(abc)*`}
	for _, p := range posixPatterns {
		t.Run("valid/"+p, func(t *testing.T) {
			re, err := CompilePOSIX(p)
			if err != nil {
				t.Fatalf("CompilePOSIX(%q) should succeed: %v", p, err)
			}
			if !re.MatchString("abc") {
				t.Errorf("CompilePOSIX(%q).MatchString(\"abc\") = false", p)
			}
		})
	}
}

// === (e) Find returns three-index slices (cap == len) ===

func TestFindThreeIndexSlice(t *testing.T) {
	input := []byte("hello world 123 test 456")
	re := MustCompile(`\d+`)

	result := re.Find(input)
	if result == nil {
		t.Fatal("Find returned nil")
	}
	if cap(result) != len(result) {
		t.Errorf("Find: cap=%d > len=%d — append would corrupt original buffer",
			cap(result), len(result))
	}

	// FindAll
	results := re.FindAll(input, -1)
	for i, r := range results {
		if cap(r) != len(r) {
			t.Errorf("FindAll[%d]: cap=%d > len=%d", i, cap(r), len(r))
		}
	}

	// FindSubmatch
	reSub := MustCompile(`(\d+)`)
	subResults := reSub.FindSubmatch(input)
	for i, r := range subResults {
		if r != nil && cap(r) != len(r) {
			t.Errorf("FindSubmatch[%d]: cap=%d > len=%d", i, cap(r), len(r))
		}
	}
}
