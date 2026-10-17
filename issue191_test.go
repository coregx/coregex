package coregex

import (
	"fmt"
	"reflect"
	"regexp"
	"testing"
)

// TestIssue191_DotAllPrefixReverseSuffix verifies that a (?s).* prefix is not
// clipped to the suffix's line by the ReverseSuffix and ReverseSuffixSet
// strategies. Without (?s), .* cannot cross \n (#146); with it, it can.
// See https://github.com/coregx/coregex/issues/191
func TestIssue191_DotAllPrefixReverseSuffix(t *testing.T) {
	patterns := []string{
		// Reported cases (ReverseSuffix)
		`(?s).*\.txt`,
		`(?s).*@example\.com`,
		`(?s)(.*)@example\.com`,
		`(?s).*(foo|bar)baz`,
		`(?s:.*)\.txt`,
		// ReverseSuffixSet
		`(?s).*\.(txt|log)`,
		// Without (?s): .* must stop at \n (#146), must stay correct
		`.*\.txt`,
		`.*@example\.com`,
		`(.*)@example\.com`,
		`.*\.(txt|log)`,
		// ReverseSuffixSet Find returned the rightmost match for any prefix
		`\w+\.(txt|log)`,
		`.+\.(txt|log)`,
		// Reverse scan stopped by the anti-quadratic guard must not return a
		// clipped start (SearchReverseLimited)
		`(?s).*x.*\.(txt|log)`,
		`(?i)\w+\.(txt|log)`,
	}
	inputs := []string{
		"a\nb.txt",
		"a\nb.log",
		"q.txt\nz\nb.txt",
		"x\ny@example.com",
		"x\ny@example.com\nz@example.com",
		"x\nybarbaz",
		"a.txt\n",
		"\n.txt",
		"a.txt\nb.log\nc",
		"a.txt b.log",
		" bar.txtx.logexample.coma",
		"bazafoobaz",
		"11@.logx.txtbar.txtt",
		".txtl.txt.txtaXbbl",
		"no match here",
		"",
	}

	for _, p := range patterns {
		re := MustCompile(p)
		std := regexp.MustCompile(p)
		for _, in := range inputs {
			t.Run(fmt.Sprintf("%q/%q", p, in), func(t *testing.T) {
				if got, want := re.MatchString(in), std.MatchString(in); got != want {
					t.Errorf("MatchString: coregex=%v, stdlib=%v", got, want)
				}
				if got, want := re.FindStringIndex(in), std.FindStringIndex(in); !reflect.DeepEqual(got, want) {
					t.Errorf("FindStringIndex: coregex=%v, stdlib=%v", got, want)
				}
				if got, want := re.FindAllStringIndex(in, -1), std.FindAllStringIndex(in, -1); !reflect.DeepEqual(got, want) {
					t.Errorf("FindAllStringIndex: coregex=%v, stdlib=%v", got, want)
				}
				if got, want := re.FindStringSubmatchIndex(in), std.FindStringSubmatchIndex(in); !reflect.DeepEqual(got, want) {
					t.Errorf("FindStringSubmatchIndex: coregex=%v, stdlib=%v", got, want)
				}
				if got, want := re.FindAllStringSubmatchIndex(in, -1), std.FindAllStringSubmatchIndex(in, -1); !reflect.DeepEqual(got, want) {
					t.Errorf("FindAllStringSubmatchIndex: coregex=%v, stdlib=%v", got, want)
				}
				if got, want := re.ReplaceAllString(in, "<$0>"), std.ReplaceAllString(in, "<$0>"); got != want {
					t.Errorf("ReplaceAllString: coregex=%q, stdlib=%q", got, want)
				}
				if got, want := re.Split(in, -1), std.Split(in, -1); !reflect.DeepEqual(got, want) {
					t.Errorf("Split: coregex=%q, stdlib=%q", got, want)
				}
			})
		}
	}
}
