package coregex

import (
	"fmt"
	"reflect"
	"regexp"
	"testing"
)

// TestIssue175_LazyQuantifier verifies that lazy quantifiers are honored by the
// char class and composite strategies, which used to match greedily.
// See https://github.com/coregx/coregex/issues/175
func TestIssue175_LazyQuantifier(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		// Reported cases
		{`\d+?`, "11"},
		{`\w+?`, "ab"},
		{`(?i)[a-c]+?`, "abc"},
		{`\w+?\s??\w?`, "1A\nc"},
		{`a*?b`, "aab"},

		// (?U) swaps the meaning of the quantifiers
		{`(?U)\d+`, "11"},

		// Composite char class patterns
		{`[a-c]+?[a-c]+?`, "abcabc"},
		{`[a-z]{1,3}?[a-c]*?`, "abcabc"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q/%q", tt.pattern, tt.input), func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			got := re.FindAllStringIndex(tt.input, -1)
			want := reStd.FindAllStringIndex(tt.input, -1)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("FindAllStringIndex(%q, %q): coregex=%v, stdlib=%v",
					tt.pattern, tt.input, got, want)
			}

			gotIdx := re.FindStringIndex(tt.input)
			wantIdx := reStd.FindStringIndex(tt.input)
			if !reflect.DeepEqual(gotIdx, wantIdx) {
				t.Errorf("FindStringIndex(%q, %q): coregex=%v, stdlib=%v",
					tt.pattern, tt.input, gotIdx, wantIdx)
			}

			if got, want := re.MatchString(tt.input), reStd.MatchString(tt.input); got != want {
				t.Errorf("MatchString(%q, %q): coregex=%v, stdlib=%v", tt.pattern, tt.input, got, want)
			}

			gotRepl := re.ReplaceAllString(tt.input, "<$0>")
			wantRepl := reStd.ReplaceAllString(tt.input, "<$0>")
			if gotRepl != wantRepl {
				t.Errorf("ReplaceAllString(%q, %q): coregex=%q, stdlib=%q",
					tt.pattern, tt.input, gotRepl, wantRepl)
			}

			gotSub := re.FindAllStringSubmatchIndex(tt.input, -1)
			wantSub := reStd.FindAllStringSubmatchIndex(tt.input, -1)
			if !reflect.DeepEqual(gotSub, wantSub) {
				t.Errorf("FindAllStringSubmatchIndex(%q, %q): coregex=%v, stdlib=%v",
					tt.pattern, tt.input, gotSub, wantSub)
			}
		})
	}
}
