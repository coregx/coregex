package coregex

import (
	"reflect"
	"regexp"
	"testing"
)

// TestCompile tests basic compilation
func TestCompile(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"simple literal", "hello", false},
		{"digit", `\d`, false},
		{"word", `\w+`, false},
		{"alternation", "foo|bar", false},
		{"repetition", "a+", false},
		{"invalid", "(", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := Compile(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("Compile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && re == nil {
				t.Error("Compile() returned nil")
			}
		})
	}
}

// TestMustCompile tests panic on invalid pattern
func TestMustCompile(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCompile() did not panic on invalid pattern")
		}
	}()

	MustCompile("(") // Should panic
}

// TestMatch tests Match and MatchString
func TestMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"simple match", "hello", "hello world", true},
		{"no match", "hello", "goodbye world", false},
		{"digit match", `\d`, "age 42", true},
		{"digit no match", `\d`, "no digits here", false},
		// Note: MVP implementation treats anchors as empty matches
		// Full anchor support coming in v1.1
		// {"start anchor", "^hello", "hello world", true},
		// {"start anchor fail", "^hello", "say hello", false},
		{"alternation match", "foo|bar", "test bar end", true},
		{"alternation no match", "foo|bar", "test baz end", false},
		{"empty pattern", "", "test", true}, // Empty matches
		{"empty input", "a", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)

			// Test Match
			got := re.Match([]byte(tt.input))
			if got != tt.want {
				t.Errorf("Match() = %v, want %v", got, tt.want)
			}

			// Test MatchString
			got = re.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("MatchString() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFind tests Find and FindString
func TestFind(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    string
		wantNil bool
	}{
		{"simple find", "hello", "say hello world", "hello", false},
		{"digit find", `\d+`, "age: 42 years", "42", false},
		{"no match", "xyz", "abc def", "", true},
		{"first of many", "a", "banana", "a", false},
		{"empty pattern", "", "test", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)

			// Test Find
			got := re.Find([]byte(tt.input))
			if tt.wantNil && got != nil {
				t.Errorf("Find() = %q, want nil", got)
			}
			if !tt.wantNil {
				if got == nil {
					t.Error("Find() = nil, want match")
					return
				}
				if string(got) != tt.want {
					t.Errorf("Find() = %q, want %q", got, tt.want)
				}
			}

			// Test FindString
			gotStr := re.FindString(tt.input)
			if tt.wantNil && gotStr != "" {
				t.Errorf("FindString() = %q, want empty", gotStr)
			}
			if !tt.wantNil && gotStr != tt.want {
				t.Errorf("FindString() = %q, want %q", gotStr, tt.want)
			}
		})
	}
}

// TestFindIndex tests FindIndex and FindStringIndex
func TestFindIndex(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    []int
		wantNil bool
	}{
		{"simple", "hello", "say hello world", []int{4, 9}, false},
		{"digit", `\d+`, "age: 42", []int{5, 7}, false},
		{"no match", "xyz", "abc", nil, true},
		{"start", "hello", "hello world", []int{0, 5}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)

			// Test FindIndex
			got := re.FindIndex([]byte(tt.input))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindIndex() = %v, want %v", got, tt.want)
			}

			// Test FindStringIndex
			got = re.FindStringIndex(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindStringIndex() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFindAll tests FindAll and FindAllString
func TestFindAll(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		n       int
		want    []string
	}{
		{"find all digits", `\d`, "a1b2c3", -1, []string{"1", "2", "3"}},
		{"find limited", `\d`, "a1b2c3", 2, []string{"1", "2"}},
		{"find zero", `\d`, "a1b2c3", 0, nil},
		{"no matches", `\d`, "abc", -1, nil},
		{"find words", `\w+`, "hello world test", -1, []string{"hello", "world", "test"}},
		{"find one", `hello`, "hello world hello", 1, []string{"hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)

			// Test FindAll
			got := re.FindAll([]byte(tt.input), tt.n)
			var gotStr []string
			for _, m := range got {
				gotStr = append(gotStr, string(m))
			}
			if !reflect.DeepEqual(gotStr, tt.want) {
				t.Errorf("FindAll() = %v, want %v", gotStr, tt.want)
			}

			// Test FindAllString
			gotStrDirect := re.FindAllString(tt.input, tt.n)
			if !reflect.DeepEqual(gotStrDirect, tt.want) {
				t.Errorf("FindAllString() = %v, want %v", gotStrDirect, tt.want)
			}
		})
	}
}

// TestString tests the String method
func TestString(t *testing.T) {
	pattern := `\d+`
	re := MustCompile(pattern)

	got := re.String()
	if got != pattern {
		t.Errorf("String() = %q, want %q", got, pattern)
	}
}

// TestRealWorldPatterns tests realistic regex patterns
func TestRealWorldPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    string
	}{
		{
			name:    "email simple",
			pattern: `\w+@\w+\.\w+`,
			input:   "Contact: user@example.com for info",
			want:    "user@example.com",
		},
		{
			name:    "phone number",
			pattern: `\d{3}-\d{4}`,
			input:   "Call 555-1234 today",
			want:    "555-1234",
		},
		{
			name:    "URL protocol",
			pattern: `https?://`,
			input:   "Visit https://example.com",
			want:    "https://",
		},
		{
			name:    "hex color",
			pattern: `#[0-9a-fA-F]{6}`,
			input:   "Background: #FF5733",
			want:    "#FF5733",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			got := re.FindString(tt.input)
			if got != tt.want {
				t.Errorf("FindString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEdgeCases tests edge cases
func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"empty pattern empty input", "", "", true},
		{"empty pattern non-empty input", "", "test", true},
		{"non-empty pattern empty input", "a", "", false},
		{"unicode", "世界", "你好世界", true},
		{"very long input", "needle", string(make([]byte, 10000)) + "needle", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			got := re.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("MatchString() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEndAnchor tests patterns with $ anchor
// Regression test for issue #24: first-call bug with ReverseAnchored patterns
func TestEndAnchor(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		// Basic end anchor
		{"a$ matches ending with a", "a$", "ba", true},
		{"a$ not matches ending with b", "a$", "ab", false},
		{"a$ matches single a", "a$", "a", true},
		{"a$ not matches single b", "a$", "b", false},

		// Empty string handling
		{"^$ matches empty", "^$", "", true},
		{"^$ not matches non-empty", "^$", "a", false},
		{"$ matches at end of abc", "$", "abc", true},

		// Multiple calls should give consistent results (regression for #24)
		{"a$ on ab consistent 1", "a$", "ab", false},
		{"a$ on ab consistent 2", "a$", "ab", false},
		{"a$ on ba consistent 1", "a$", "ba", true},
		{"a$ on ba consistent 2", "a$", "ba", true},

		// Start anchor combinations
		{"^a$ full match a", "^a$", "a", true},
		{"^a$ not matches ab", "^a$", "ab", false},
		{"^a$ not matches ba", "^a$", "ba", false},

		// Alternation with anchors
		{"^a?$|^b?$ matches empty", "^a?$|^b?$", "", true},
		{"^a?$|^b?$ matches a", "^a?$|^b?$", "a", true},
		{"^a?$|^b?$ matches b", "^a?$|^b?$", "b", true},
		{"^a?$|^b?$ not matches ab", "^a?$|^b?$", "ab", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)

			// Call multiple times to catch first-call bugs
			for i := 0; i < 3; i++ {
				got := re.MatchString(tt.input)
				if got != tt.want {
					t.Errorf("MatchString() call %d = %v, want %v", i+1, got, tt.want)
				}
			}
		})
	}
}

// BenchmarkCompile benchmarks compilation
func BenchmarkCompile(b *testing.B) {
	patterns := []string{
		"hello",
		`\d+`,
		`\w+@\w+\.\w+`,
		"(foo|bar|baz)",
	}

	for _, pattern := range patterns {
		b.Run(pattern, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := Compile(pattern)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMatch benchmarks matching
func BenchmarkMatch(b *testing.B) {
	re := MustCompile(`\d+`)
	input := []byte("the year is 2024")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if !re.Match(input) {
			b.Fatal("expected match")
		}
	}
}

// BenchmarkFind benchmarks finding
func BenchmarkFind(b *testing.B) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{"literal", "hello", "this is a hello world test string with more text"},
		{"digit", `\d+`, "age: 42 years old"},
		{"alternation", "foo|bar|baz", "prefix foo middle bar suffix baz end"},
		{"word", `\w+`, "hello world test"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			re := MustCompile(tt.pattern)
			input := []byte(tt.input)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				match := re.Find(input)
				if match == nil {
					b.Fatal("expected match")
				}
			}
		})
	}
}

// BenchmarkFindAll benchmarks finding all matches
func BenchmarkFindAll(b *testing.B) {
	re := MustCompile(`\d`)
	input := []byte("1a2b3c4d5e6f7g8h9i0")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		matches := re.FindAll(input, -1)
		if len(matches) != 10 {
			b.Fatalf("expected 10 matches, got %d", len(matches))
		}
	}
}

// TestCount tests counting matches
func TestCount(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		n       int
		want    int
	}{
		{"all digits", `\d`, "1 2 3 4 5", -1, 5},
		{"limited", `\d`, "1 2 3 4 5", 3, 3},
		{"no match", `\d`, "abc", -1, 0},
		{"words", `\w+`, "hello world foo", -1, 3},
		{"zero limit", `\d`, "123", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			got := re.Count([]byte(tt.input), tt.n)
			if got != tt.want {
				t.Errorf("Count() = %d, want %d", got, tt.want)
			}

			// Also test CountString
			gotStr := re.CountString(tt.input, tt.n)
			if gotStr != tt.want {
				t.Errorf("CountString() = %d, want %d", gotStr, tt.want)
			}
		})
	}
}

// TestFindAllSubmatch tests finding all matches with capture groups
func TestFindAllSubmatch(t *testing.T) {
	re := MustCompile(`(\w+)=(\d+)`)
	input := []byte("a=1 b=2 c=3")

	matches := re.FindAllSubmatch(input, -1)
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}

	// Check first match
	if string(matches[0][0]) != "a=1" {
		t.Errorf("match[0][0] = %q, want %q", string(matches[0][0]), "a=1")
	}
	if string(matches[0][1]) != "a" {
		t.Errorf("match[0][1] = %q, want %q", string(matches[0][1]), "a")
	}
	if string(matches[0][2]) != "1" {
		t.Errorf("match[0][2] = %q, want %q", string(matches[0][2]), "1")
	}

	// Check second match
	if string(matches[1][0]) != "b=2" {
		t.Errorf("match[1][0] = %q, want %q", string(matches[1][0]), "b=2")
	}
}

// TestFindAllStringSubmatch tests finding all string matches with capture groups
func TestFindAllStringSubmatch(t *testing.T) {
	re := MustCompile(`(\w+)@(\w+)`)
	input := "a@b c@d"

	matches := re.FindAllStringSubmatch(input, -1)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	// Check first match
	if matches[0][0] != "a@b" {
		t.Errorf("match[0][0] = %q, want %q", matches[0][0], "a@b")
	}
	if matches[0][1] != "a" {
		t.Errorf("match[0][1] = %q, want %q", matches[0][1], "a")
	}

	// Check second match
	if matches[1][0] != "c@d" {
		t.Errorf("match[1][0] = %q, want %q", matches[1][0], "c@d")
	}
}

// TestFindAllSubmatchIndex tests index pairs for all matches with captures
func TestFindAllSubmatchIndex(t *testing.T) {
	re := MustCompile(`(\w+)=(\d+)`)
	input := []byte("a=1 b=2")

	indices := re.FindAllSubmatchIndex(input, -1)
	if len(indices) != 2 {
		t.Fatalf("expected 2 index slices, got %d", len(indices))
	}

	// First match "a=1" at position 0-3
	// Group 0 (entire): 0-3
	// Group 1 (a): 0-1
	// Group 2 (1): 2-3
	if indices[0][0] != 0 || indices[0][1] != 3 {
		t.Errorf("indices[0][0:2] = [%d, %d], want [0, 3]", indices[0][0], indices[0][1])
	}
}

// TestLongest tests leftmost-longest (POSIX) matching semantics.
// Verifies that Longest() changes alternation behavior from
// leftmost-first (Perl) to leftmost-longest (POSIX).
func TestLongest(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		input       string
		wantDefault string // leftmost-first (Perl)
		wantLongest string // leftmost-longest (POSIX)
	}{
		{
			name:        "alternation a|ab",
			pattern:     `(a|ab)`,
			input:       "ab",
			wantDefault: "a",
			wantLongest: "ab",
		},
		{
			name:        "alternation #|#!",
			pattern:     `(#|#!)`,
			input:       "#!a",
			wantDefault: "#",
			wantLongest: "#!",
		},
		{
			name:        "alternation cat|catalog",
			pattern:     `(cat|catalog)`,
			input:       "catalog",
			wantDefault: "cat",
			wantLongest: "catalog",
		},
		{
			name:        "no difference for simple patterns",
			pattern:     `a+`,
			input:       "aaaa",
			wantDefault: "aaaa",
			wantLongest: "aaaa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test default behavior (leftmost-first)
			re := MustCompile(tt.pattern)
			gotDefault := re.FindString(tt.input)
			if gotDefault != tt.wantDefault {
				t.Errorf("Default FindString() = %q, want %q", gotDefault, tt.wantDefault)
			}

			// Test after Longest() (leftmost-longest)
			re.Longest()
			gotLongest := re.FindString(tt.input)
			if gotLongest != tt.wantLongest {
				t.Errorf("Longest() FindString() = %q, want %q", gotLongest, tt.wantLongest)
			}
		})
	}
}

// TestLongestMatchesStdlib verifies that Longest() behavior matches stdlib regexp.
func TestLongestMatchesStdlib(t *testing.T) {
	patterns := []string{
		`(a|ab)`,
		`(#|#!)`,
		`(cat|catalog)`,
		`(foo|foobar)`,
	}
	inputs := []string{"ab", "#!a", "catalog", "foobar"}

	for i, pattern := range patterns {
		input := inputs[i]
		t.Run(pattern, func(t *testing.T) {
			// stdlib
			stdRe := regexp.MustCompile(pattern)
			stdRe.Longest()
			stdResult := stdRe.FindString(input)

			// coregex
			cgRe := MustCompile(pattern)
			cgRe.Longest()
			cgResult := cgRe.FindString(input)

			if cgResult != stdResult {
				t.Errorf("coregex Longest() = %q, stdlib = %q", cgResult, stdResult)
			}
		})
	}
}
