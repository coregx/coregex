package coregex

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// TestPackageLevelMatch tests the package-level Match function.
func TestPackageLevelMatch(t *testing.T) {
	tests := []struct {
		pattern string
		input   []byte
		want    bool
	}{
		{`\d+`, []byte("hello 123"), true},
		{`\d+`, []byte("hello"), false},
		{`^hello`, []byte("hello world"), true},
		{`^hello`, []byte("say hello"), false},
	}

	for _, tt := range tests {
		got, err := Match(tt.pattern, tt.input)
		if err != nil {
			t.Errorf("Match(%q, %q) error: %v", tt.pattern, tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
		}

		// Compare with stdlib
		stdGot, _ := regexp.Match(tt.pattern, tt.input)
		if got != stdGot {
			t.Errorf("Match(%q, %q) = %v, stdlib = %v", tt.pattern, tt.input, got, stdGot)
		}
	}
}

// TestPackageLevelMatchString tests the package-level MatchString function.
func TestPackageLevelMatchString(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{`\d+`, "hello 123", true},
		{`\d+`, "hello", false},
		{`^hello`, "hello world", true},
		{`^hello`, "say hello", false},
	}

	for _, tt := range tests {
		got, err := MatchString(tt.pattern, tt.input)
		if err != nil {
			t.Errorf("MatchString(%q, %q) error: %v", tt.pattern, tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("MatchString(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
		}

		// Compare with stdlib
		stdGot, _ := regexp.MatchString(tt.pattern, tt.input)
		if got != stdGot {
			t.Errorf("MatchString(%q, %q) = %v, stdlib = %v", tt.pattern, tt.input, got, stdGot)
		}
	}
}

// TestCompilePOSIX tests POSIX compilation with leftmost-longest semantics.
func TestCompilePOSIX(t *testing.T) {
	// Test that CompilePOSIX sets longest mode
	re, err := CompilePOSIX(`(a|ab)`)
	if err != nil {
		t.Fatalf("CompilePOSIX error: %v", err)
	}

	// With POSIX semantics, should match "ab" (longest) not "a" (leftmost-first)
	stdRe := regexp.MustCompilePOSIX(`(a|ab)`)
	stdMatch := stdRe.FindString("ab")
	ourMatch := re.FindString("ab")

	if ourMatch != stdMatch {
		t.Errorf("CompilePOSIX match = %q, stdlib = %q", ourMatch, stdMatch)
	}
}

// TestMustCompilePOSIXPanic tests that MustCompilePOSIX panics on invalid pattern.
func TestMustCompilePOSIXPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompilePOSIX did not panic on invalid pattern")
		}
	}()
	MustCompilePOSIX(`[invalid`)
}

// TestSubexpIndex tests the SubexpIndex method.
func TestSubexpIndex(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    int
	}{
		{`(?P<year>\d+)-(?P<month>\d+)`, "year", 1},
		{`(?P<year>\d+)-(?P<month>\d+)`, "month", 2},
		{`(?P<year>\d+)-(?P<month>\d+)`, "day", -1},
		{`(?P<first>\w+)(?P<second>\w+)`, "first", 1},
		{`(?P<first>\w+)(?P<second>\w+)`, "second", 2},
		{`(\d+)`, "unnamed", -1},
		{`(?P<foo>a)`, "", -1}, // empty name returns -1
	}

	for _, tt := range tests {
		re := MustCompile(tt.pattern)
		got := re.SubexpIndex(tt.name)
		if got != tt.want {
			t.Errorf("SubexpIndex(%q, %q) = %d, want %d", tt.pattern, tt.name, got, tt.want)
		}

		// Compare with stdlib
		stdRe := regexp.MustCompile(tt.pattern)
		stdGot := stdRe.SubexpIndex(tt.name)
		if got != stdGot {
			t.Errorf("SubexpIndex(%q, %q) = %d, stdlib = %d", tt.pattern, tt.name, got, stdGot)
		}
	}
}

// TestLiteralPrefix tests the LiteralPrefix method.
func TestLiteralPrefix(t *testing.T) {
	tests := []struct {
		pattern        string
		wantPrefix     string
		wantComplete   bool
	}{
		{`hello`, "hello", true},
		{`hello.*`, "hello", false},
		{`Hello, \w+`, "Hello, ", false},
		{`abc`, "abc", true},
		{`\d+`, "", false},
		{`.*`, "", false},
		{`^hello`, "", false}, // anchors are not literal
	}

	for _, tt := range tests {
		re := MustCompile(tt.pattern)
		gotPrefix, gotComplete := re.LiteralPrefix()

		// Compare with stdlib
		stdRe := regexp.MustCompile(tt.pattern)
		stdPrefix, stdComplete := stdRe.LiteralPrefix()

		if gotPrefix != stdPrefix {
			t.Errorf("LiteralPrefix(%q) prefix = %q, stdlib = %q", tt.pattern, gotPrefix, stdPrefix)
		}
		if gotComplete != stdComplete {
			t.Errorf("LiteralPrefix(%q) complete = %v, stdlib = %v", tt.pattern, gotComplete, stdComplete)
		}
	}
}

// TestCopy tests the Copy method.
func TestCopy(t *testing.T) {
	re := MustCompile(`\d+`)
	copyRe := re.Copy()

	if copyRe == nil {
		t.Fatal("Copy returned nil")
	}

	// Both should match the same
	input := "test 123"
	if re.FindString(input) != copyRe.FindString(input) {
		t.Error("Copy produces different results")
	}

	// Calling Longest on one shouldn't affect the other
	copyRe.Longest()
	// They may now produce different results for some patterns (tested separately)
}

// TestCopyWithLongest tests that Copy preserves Longest setting.
func TestCopyWithLongest(t *testing.T) {
	re := MustCompile(`(a|ab)`)
	re.Longest()
	copyRe := re.Copy()

	input := "ab"
	if re.FindString(input) != copyRe.FindString(input) {
		t.Error("Copy did not preserve Longest setting")
	}
}

// TestMarshalText tests the MarshalText method.
func TestMarshalText(t *testing.T) {
	patterns := []string{`\d+`, `hello.*world`, `[a-z]+`, `(?P<name>\w+)`}

	for _, pattern := range patterns {
		re := MustCompile(pattern)
		data, err := re.MarshalText()
		if err != nil {
			t.Errorf("MarshalText(%q) error: %v", pattern, err)
			continue
		}

		if string(data) != pattern {
			t.Errorf("MarshalText(%q) = %q", pattern, string(data))
		}
	}
}

// TestUnmarshalText tests the UnmarshalText method.
func TestUnmarshalText(t *testing.T) {
	patterns := []string{`\d+`, `hello.*world`, `[a-z]+`}

	for _, pattern := range patterns {
		var re Regex
		err := re.UnmarshalText([]byte(pattern))
		if err != nil {
			t.Errorf("UnmarshalText(%q) error: %v", pattern, err)
			continue
		}

		// Should work like a compiled regex
		if re.String() != pattern {
			t.Errorf("UnmarshalText(%q) String() = %q", pattern, re.String())
		}

		// Should be able to match
		if pattern == `\d+` && !re.MatchString("test 123") {
			t.Errorf("UnmarshalText(%q) failed to match", pattern)
		}
	}
}

// TestUnmarshalTextInvalid tests UnmarshalText with invalid pattern.
func TestUnmarshalTextInvalid(t *testing.T) {
	var re Regex
	err := re.UnmarshalText([]byte(`[invalid`))
	if err == nil {
		t.Error("UnmarshalText with invalid pattern should return error")
	}
}

// TestMatchReader tests the MatchReader method.
func TestMatchReader(t *testing.T) {
	re := MustCompile(`\d+`)

	// Test with matching input
	reader := strings.NewReader("hello 123 world")
	if !re.MatchReader(reader) {
		t.Error("MatchReader failed to match digits")
	}

	// Test with non-matching input
	reader = strings.NewReader("hello world")
	if re.MatchReader(reader) {
		t.Error("MatchReader incorrectly matched")
	}
}

// TestFindReaderIndex tests the FindReaderIndex method.
func TestFindReaderIndex(t *testing.T) {
	re := MustCompile(`\d+`)

	reader := strings.NewReader("hello 123 world")
	idx := re.FindReaderIndex(reader)

	if idx == nil {
		t.Fatal("FindReaderIndex returned nil")
	}

	// "123" starts at position 6
	if idx[0] != 6 || idx[1] != 9 {
		t.Errorf("FindReaderIndex = %v, want [6 9]", idx)
	}
}

// TestFindReaderSubmatchIndex tests the FindReaderSubmatchIndex method.
func TestFindReaderSubmatchIndex(t *testing.T) {
	re := MustCompile(`(\w+)@(\w+)`)

	reader := strings.NewReader("user@domain")
	idx := re.FindReaderSubmatchIndex(reader)

	if idx == nil {
		t.Fatal("FindReaderSubmatchIndex returned nil")
	}

	// Full match [0:11], group1 [0:4], group2 [5:11]
	if len(idx) < 6 {
		t.Fatalf("FindReaderSubmatchIndex = %v, expected at least 6 elements", idx)
	}
}

// TestPackageLevelMatchReader tests the package-level MatchReader function.
func TestPackageLevelMatchReader(t *testing.T) {
	reader := strings.NewReader("hello 123")
	matched, err := MatchReader(`\d+`, reader)
	if err != nil {
		t.Fatalf("MatchReader error: %v", err)
	}
	if !matched {
		t.Error("MatchReader should have matched")
	}

	// Test with invalid pattern
	reader = strings.NewReader("test")
	_, err = MatchReader(`[invalid`, reader)
	if err == nil {
		t.Error("MatchReader with invalid pattern should return error")
	}
}

// TestExpandStdlibCompat tests Expand compatibility with stdlib.
func TestExpandStdlibCompat(t *testing.T) {
	tests := []struct {
		pattern  string
		input    string
		template string
	}{
		{`(\w+)@(\w+)`, "user@domain", "$1 at $2"},
		{`(\d+)-(\d+)`, "123-456", "$2-$1"},
		{`(a)(b)(c)`, "abc", "$3$2$1"},
		{`\d+`, "123", "number: $0"},
	}

	for _, tt := range tests {
		re := MustCompile(tt.pattern)
		stdRe := regexp.MustCompile(tt.pattern)

		match := re.FindStringSubmatchIndex(tt.input)
		stdMatch := stdRe.FindStringSubmatchIndex(tt.input)

		if match == nil || stdMatch == nil {
			continue
		}

		got := re.ExpandString(nil, tt.template, tt.input, match)
		want := stdRe.ExpandString(nil, tt.template, tt.input, stdMatch)

		if !bytes.Equal(got, want) {
			t.Errorf("ExpandString(%q, %q, %q) = %q, stdlib = %q",
				tt.pattern, tt.template, tt.input, got, want)
		}
	}
}
