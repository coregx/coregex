package coregex

import (
	"fmt"
	"regexp"
	"testing"
)

// TestIssue166_BranchDispatch tests anchored alternation patterns that were
// broken by BranchDispatch ignoring prefix/suffix around the alternation,
// and by "assume true" fallbacks for non-literal branches.
// See https://github.com/coregx/coregex/issues/166
func TestIssue166_BranchDispatch(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// False negatives: prefix before alternation ignored
		{`^x(a|bc)`, "xbc"},               // original report
		{`^HTTP/(1\.0|1\.1|2)`, "HTTP/2"}, // HTTP version
		{`^/api/(users|posts)`, "/api/users"},

		// False positives: suffix after alternation ignored
		{`^(GET|POST) /`, "GET x"},
		{`^(a|bc)$`, "bcd"},

		// False positives: non-literal branch "assume true"
		{`^(a\d|b)`, "a"},
		{`^(ab+|c)`, "a"},

		// FoldCase in literal branch — bytewise comparison
		{`^(?i:uuid|hex)`, "Hex"},

		// Unicode class branch — bytes not runes
		{`^(\pL+|\d+)`, "Привет"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.pattern, tt.input), func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			cgx := re.MatchString(tt.input)
			std := reStd.MatchString(tt.input)

			if cgx != std {
				t.Errorf("MatchString(%q, %q): coregex=%v, stdlib=%v",
					tt.pattern, tt.input, cgx, std)
			}

			cgxIdx := re.FindStringIndex(tt.input)
			stdIdx := reStd.FindStringIndex(tt.input)

			cgxFound := cgxIdx != nil
			stdFound := stdIdx != nil
			if cgxFound != stdFound {
				t.Errorf("FindStringIndex(%q, %q): coregex=%v, stdlib=%v",
					tt.pattern, tt.input, cgxIdx, stdIdx)
			}
		})
	}
}

// TestIssue166_FirstBytesFoldCase tests first-byte extraction with FoldCase
// and non-ASCII runes.
func TestIssue166_FirstBytesFoldCase(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`^[Vv]\d+`, "v1"},
		{`^(?:v|V)(\d+)`, "v1"},
		{`^(é|x)`, "é"},
		{`^(?i:é)x`, "Éx"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.pattern, tt.input), func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			cgx := re.MatchString(tt.input)
			std := reStd.MatchString(tt.input)

			if cgx != std {
				t.Errorf("MatchString(%q, %q): coregex=%v, stdlib=%v",
					tt.pattern, tt.input, cgx, std)
			}
		})
	}
}

// TestIssue166_AnchoredInvariant verifies the cross-API invariant:
// MatchString(s) == (FindStringIndex(s) != nil) == (FindStringSubmatchIndex(s) != nil)
// and that FindStringIndex matches the first two elements of FindStringSubmatchIndex.
func TestIssue166_AnchoredInvariant(t *testing.T) {
	patterns := []string{
		`^x(a|bc)`,
		`^HTTP/(1\.0|1\.1|2)`,
		`^/api/(users|posts)`,
		`^(GET|POST) /`,
		`^(a|bc)$`,
		`^(a\d|b)`,
		`^[Vv]\d+`,
		`^(é|x)`,
		`^(?i:é)x`,
		`^(?i:uuid|hex)`,
		`^(\pL+|\d+)`,
		`^(\d+|UUID|hex32)`,
		`^(a|bc)`,
	}

	inputs := []string{
		"xbc", "xa", "HTTP/2", "HTTP/1.1", "/api/users", "/api/posts",
		"GET /index", "GET x", "POST /", "bcd", "bc", "a", "a1", "b",
		"v1", "V1", "é", "Éx", "Hex", "UUID", "hex32",
		"Привет", "123", "abc",
	}

	for _, p := range patterns {
		re := MustCompile(p)
		for _, in := range inputs {
			match := re.MatchString(in)
			findIdx := re.FindStringIndex(in)
			subIdx := re.FindStringSubmatchIndex(in)

			findFound := findIdx != nil
			subFound := subIdx != nil

			if match != findFound {
				t.Errorf("%q on %q: MatchString=%v but FindStringIndex=%v",
					p, in, match, findIdx)
			}
			if match != subFound {
				t.Errorf("%q on %q: MatchString=%v but FindStringSubmatchIndex=%v",
					p, in, match, subIdx)
			}
			if findFound && subFound {
				if findIdx[0] != subIdx[0] || findIdx[1] != subIdx[1] {
					t.Errorf("%q on %q: FindStringIndex=%v but FindStringSubmatchIndex[0:2]=%v",
						p, in, findIdx, subIdx[:2])
				}
			}
		}
	}
}
