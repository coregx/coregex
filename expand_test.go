package coregex

import (
	"regexp"
	"testing"
)

// TestExpandTemplates verifies that expand() handles all template forms
// matching Go stdlib behavior: $N, ${N}, $name, ${name}, $$, multi-digit.
func TestExpandTemplates(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		template string
	}{
		// Basic $N (single digit)
		{"$1", `(\w+)\s+(\w+)`, "hello world", "$1"},
		{"$2", `(\w+)\s+(\w+)`, "hello world", "$2"},
		{"$0", `(\w+)\s+(\w+)`, "hello world", "$0"},
		{"$2 $1", `(\w+)\s+(\w+)`, "hello world", "$2 $1"},

		// ${N} braced numeric
		{"${1}", `(\w+)\s+(\w+)`, "hello world", "${1}"},
		{"${2}", `(\w+)\s+(\w+)`, "hello world", "${2}"},
		{"${2} ${1}", `(\w+)\s+(\w+)`, "hello world", "${2} ${1}"},
		{"${0}", `(\w+)\s+(\w+)`, "hello world", "${0}"},

		// Multi-digit $N
		{"$10 ten groups", `(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)`, "abcdefghij", "$10"},
		{"${10}", `(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)`, "abcdefghij", "${10}"},
		{"$1 then literal 0", `(a)(b)`, "ab", "$10"},

		// Named groups
		{"$name", `(?P<first>\w+)\s+(?P<second>\w+)`, "hello world", "$second $first"},
		{"${name}", `(?P<first>\w+)\s+(?P<second>\w+)`, "hello world", "${second} ${first}"},

		// $$ escape
		{"$$", `(\w+)`, "hello", "$$"},
		{"$1$$", `(\w+)`, "hello", "$1$$"},

		// $N followed by non-identifier char
		{"$1!", `(\w+)`, "hello", "$1!"},
		{"$1-$2", `(\w+)-(\w+)`, "a-b", "$1-$2"},

		// Invalid/missing group — silent drop (stdlib behavior)
		{"$9 missing", `(\w+)`, "hello", "$9"},
		{"${99}", `(\w+)`, "hello", "${99}"},

		// Literal $ at end
		{"trailing $", `(\w+)`, "hello", "$1 costs $"},

		// No groups pattern
		{"no groups", `\w+`, "hello", "$0"},

		// Mixed
		{"mixed", `(?P<x>\d+)-(\w+)`, "42-foo", "${x}=$2"},

		// Empty match
		{"empty template", `(\w+)`, "hello", ""},
		{"no dollars", `(\w+)`, "hello", "replaced"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			reStd := regexp.MustCompile(tt.pattern)

			cgx := re.ReplaceAllString(tt.input, tt.template)
			std := reStd.ReplaceAllString(tt.input, tt.template)

			if cgx != std {
				t.Errorf("ReplaceAllString(%q, %q, %q):\n  coregex=%q\n  stdlib =%q",
					tt.pattern, tt.input, tt.template, cgx, std)
			}
		})
	}
}

// TestExpandExpand verifies the Expand/ExpandString methods directly.
func TestExpandExpand(t *testing.T) {
	re := MustCompile(`(?P<first>\w+)\s+(?P<second>\w+)`)
	reStd := regexp.MustCompile(`(?P<first>\w+)\s+(?P<second>\w+)`)

	src := "hello world"
	match := re.FindStringSubmatchIndex(src)
	matchStd := reStd.FindStringSubmatchIndex(src)

	templates := []string{
		"${first} ${second}",
		"${second}, ${first}!",
		"$1 $2",
		"${1} ${2}",
		"$0",
		"$$",
	}

	for _, tmpl := range templates {
		var cgxBuf, stdBuf []byte
		cgxResult := re.ExpandString(cgxBuf, tmpl, src, match)
		stdResult := reStd.ExpandString(stdBuf, tmpl, src, matchStd)

		if string(cgxResult) != string(stdResult) {
			t.Errorf("ExpandString(%q):\n  coregex=%q\n  stdlib =%q",
				tmpl, cgxResult, stdResult)
		}
	}
}
