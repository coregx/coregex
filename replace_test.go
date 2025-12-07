package coregex

import (
	"strconv"
	"testing"
)

func TestFindAllIndex(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		n       int
		want    [][]int
	}{
		{`\d+`, "1 2 3", -1, [][]int{{0, 1}, {2, 3}, {4, 5}}},
		{`\d+`, "1 2 3", 2, [][]int{{0, 1}, {2, 3}}},
		{`\d+`, "1 2 3", 0, nil},
		{`\d+`, "abc", -1, nil},
		{`a`, "aaa", -1, [][]int{{0, 1}, {1, 2}, {2, 3}}},
		{`a*`, "aaa", -1, [][]int{{0, 3}}}, // No empty match at end after non-empty match (stdlib behavior)
	}

	for _, tt := range tests {
		re := MustCompile(tt.pattern)
		got := re.FindAllIndex([]byte(tt.input), tt.n)
		if !equalIntSlices(got, tt.want) {
			t.Errorf("FindAllIndex(%q, %q, %d) = %v, want %v",
				tt.pattern, tt.input, tt.n, got, tt.want)
		}
	}
}

func TestFindAllStringIndex(t *testing.T) {
	re := MustCompile(`\d+`)
	got := re.FindAllStringIndex("1 2 3", -1)
	want := [][]int{{0, 1}, {2, 3}, {4, 5}}
	if !equalIntSlices(got, want) {
		t.Errorf("FindAllStringIndex = %v, want %v", got, want)
	}
}

func TestReplaceAllLiteral(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		repl    string
		want    string
	}{
		{`\d+`, "age: 42", "XX", "age: XX"},
		{`\d+`, "1 2 3", "X", "X X X"},
		{`\d+`, "abc", "X", "abc"},
		{`a`, "aaa", "b", "bbb"},
		{`\s+`, "a  b   c", " ", "a b c"},
	}

	for _, tt := range tests {
		re := MustCompile(tt.pattern)
		got := string(re.ReplaceAllLiteral([]byte(tt.input), []byte(tt.repl)))
		if got != tt.want {
			t.Errorf("ReplaceAllLiteral(%q, %q, %q) = %q, want %q",
				tt.pattern, tt.input, tt.repl, got, tt.want)
		}
	}
}

func TestReplaceAllLiteralString(t *testing.T) {
	re := MustCompile(`\d+`)
	got := re.ReplaceAllLiteralString("age: 42", "XX")
	want := "age: XX"
	if got != want {
		t.Errorf("ReplaceAllLiteralString = %q, want %q", got, want)
	}
}

func TestReplaceAll(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		repl    string
		want    string
	}{
		// Literal replacement (no $ variables)
		{`\d+`, "age: 42", "XX", "age: XX"},
		// Capture group replacement
		{`(\w+)@(\w+)\.(\w+)`, "user@example.com", "$1 at $2 dot $3", "user at example dot com"},
		// $0 (entire match)
		{`\d+`, "age: 42", "[$0]", "age: [42]"},
		// Multiple replacements
		{`(\d+)`, "1 2 3", "($1)", "(1) (2) (3)"},
		// $$ escape
		{`\d+`, "price: 10", "$$", "price: $"},
		// No capture groups with $
		{`\d+`, "age: 42", "$1", "age: "},
	}

	for _, tt := range tests {
		re := MustCompile(tt.pattern)
		got := string(re.ReplaceAll([]byte(tt.input), []byte(tt.repl)))
		if got != tt.want {
			t.Errorf("ReplaceAll(%q, %q, %q) = %q, want %q",
				tt.pattern, tt.input, tt.repl, got, tt.want)
		}
	}
}

func TestReplaceAllString(t *testing.T) {
	re := MustCompile(`(\w+)@(\w+)\.(\w+)`)
	got := re.ReplaceAllString("user@example.com", "$1 at $2 dot $3")
	want := "user at example dot com"
	if got != want {
		t.Errorf("ReplaceAllString = %q, want %q", got, want)
	}
}

func TestReplaceAllFunc(t *testing.T) {
	re := MustCompile(`\d+`)
	got := re.ReplaceAllFunc([]byte("1 2 3"), func(s []byte) []byte {
		n, _ := strconv.Atoi(string(s))
		return []byte(strconv.Itoa(n * 2))
	})
	want := "2 4 6"
	if string(got) != want {
		t.Errorf("ReplaceAllFunc = %q, want %q", string(got), want)
	}

	// Test with no matches
	re2 := MustCompile(`\d+`)
	got2 := re2.ReplaceAllFunc([]byte("abc"), func(s []byte) []byte {
		return []byte("X")
	})
	want2 := "abc"
	if string(got2) != want2 {
		t.Errorf("ReplaceAllFunc (no match) = %q, want %q", string(got2), want2)
	}
}

func TestReplaceAllStringFunc(t *testing.T) {
	re := MustCompile(`\d+`)
	got := re.ReplaceAllStringFunc("1 2 3", func(s string) string {
		n, _ := strconv.Atoi(s)
		return strconv.Itoa(n * 2)
	})
	want := "2 4 6"
	if got != want {
		t.Errorf("ReplaceAllStringFunc = %q, want %q", got, want)
	}

	// Test with no matches
	re2 := MustCompile(`\d+`)
	got2 := re2.ReplaceAllStringFunc("abc", func(s string) string {
		return "X"
	})
	want2 := "abc"
	if got2 != want2 {
		t.Errorf("ReplaceAllStringFunc (no match) = %q, want %q", got2, want2)
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		n       int
		want    []string
	}{
		{`,`, "a,b,c", -1, []string{"a", "b", "c"}},
		{`,`, "a,b,c", 2, []string{"a", "b,c"}},
		{`,`, "a,b,c", 0, nil},
		{`,`, "abc", -1, []string{"abc"}},
		{`\s+`, "a  b   c", -1, []string{"a", "b", "c"}},
		{`\s+`, "  a  b  ", -1, []string{"", "a", "b", ""}},
		{`,`, "a,b,c,d,e", 3, []string{"a", "b", "c,d,e"}},
		{`a`, "aaa", -1, []string{"", "", "", ""}},
	}

	for _, tt := range tests {
		re := MustCompile(tt.pattern)
		got := re.Split(tt.input, tt.n)
		if !equalStringSlices(got, tt.want) {
			t.Errorf("Split(%q, %q, %d) = %#v (len=%d), want %#v (len=%d)",
				tt.pattern, tt.input, tt.n, got, len(got), tt.want, len(tt.want))
			// Debug output
			for i := 0; i < len(got) || i < len(tt.want); i++ {
				var g, w string
				if i < len(got) {
					g = got[i]
				}
				if i < len(tt.want) {
					w = tt.want[i]
				}
				t.Logf("  [%d] got=%q want=%q", i, g, w)
			}
		}
	}
}

func TestExpandEdgeCases(t *testing.T) {
	re := MustCompile(`(\d+)`)
	match := re.FindSubmatchIndex([]byte("test 123 end"))

	tests := []struct {
		template string
		want     string
	}{
		{"$0", "123"},
		{"$1", "123"},
		{"$$", "$"},
		{"$${foo}", "${foo}"},
		{"before $1 after", "before 123 after"},
		{"$", "$"},                   // Lone $ at end
		{"${", "${"},                 // Incomplete ${
		{"$9", ""},                   // Non-existent group
		{"text", "text"},             // No $ at all
		{"$0$0", "123123"},           // Multiple $0
		{"$1 and $1", "123 and 123"}, // Multiple $1
	}

	for _, tt := range tests {
		dst := re.expand(nil, []byte(tt.template), []byte("test 123 end"), match)
		got := string(dst)
		if got != tt.want {
			t.Errorf("expand(%q) = %q, want %q", tt.template, got, tt.want)
		}
	}
}

// Helper functions
func equalIntSlices(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	if a == nil && b == nil {
		return true
	}
	if (a == nil) != (b == nil) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if a == nil && b == nil {
		return true
	}
	if (a == nil) != (b == nil) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
