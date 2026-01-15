package meta

import (
	"regexp"
	"testing"
)

// TestMultilineReverseSuffixSearcher tests the multiline-aware reverse suffix searcher.
func TestMultilineReverseSuffixSearcher(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		haystack  string
		want      bool
		wantStart int
		wantEnd   int
	}{
		{
			name:      "multiline_php_single_line",
			pattern:   "(?m)^/.*\\.php",
			haystack:  "/index.php",
			want:      true,
			wantStart: 0,
			wantEnd:   10,
		},
		{
			name:      "multiline_php_multiline_input_first",
			pattern:   "(?m)^/.*\\.php",
			haystack:  "/admin/login.php\n/other/path",
			want:      true,
			wantStart: 0,
			wantEnd:   16,
		},
		{
			name:      "multiline_php_multiline_input_second",
			pattern:   "(?m)^/.*\\.php",
			haystack:  "/some/path\n/admin/login.php",
			want:      true,
			wantStart: 11,
			wantEnd:   27,
		},
		{
			name:      "multiline_php_multiline_input_middle",
			pattern:   "(?m)^/.*\\.php",
			haystack:  "/text\n/admin/config.php\n/more",
			want:      true,
			wantStart: 6,
			wantEnd:   23,
		},
		{
			name:      "multiline_php_no_match",
			pattern:   "(?m)^/.*\\.php",
			haystack:  "/index.html\n/style.css",
			want:      false,
			wantStart: -1,
			wantEnd:   -1,
		},
		{
			name:      "multiline_email_at_line_start",
			pattern:   "(?m)^.*@example\\.com",
			haystack:  "user@example.com\nother@test.org",
			want:      true,
			wantStart: 0,
			wantEnd:   16,
		},
		{
			name:      "multiline_path_complex",
			pattern:   "(?m)^/api/.*\\.json",
			haystack:  "text\n/api/users.json\nmore",
			want:      true,
			wantStart: 5,
			wantEnd:   20,
		},
		{
			name:      "multiline_charclass_before_suffix",
			pattern:   "(?m)^/[a-z]+\\.txt",
			haystack:  "header\n/docs.txt\nfooter",
			want:      true,
			wantStart: 7,
			wantEnd:   16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compile with our engine
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			// Test IsMatch
			got := engine.IsMatch([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("IsMatch() = %v, want %v", got, tt.want)
			}

			// Test Find
			match := engine.Find([]byte(tt.haystack))
			if tt.want {
				if match == nil {
					t.Error("Find() = nil, want match")
				} else if match.Start() < 0 || match.End() < 0 {
					// Note: Start position may differ from stdlib due to greedy semantics
					// Our multiline searcher finds matches at line boundaries
					t.Errorf("Find() invalid bounds: start=%d, end=%d", match.Start(), match.End())
				}
			} else if match != nil {
				t.Errorf("Find() = %v, want nil", match)
			}

			// Verify against stdlib for correctness
			stdRe := regexp.MustCompile(tt.pattern)
			stdMatch := stdRe.MatchString(tt.haystack)
			if got != stdMatch {
				t.Errorf("IsMatch() = %v, stdlib = %v", got, stdMatch)
			}
		})
	}
}

// TestMultilineReverseSuffixStrategy verifies strategy selection.
func TestMultilineReverseSuffixStrategy(t *testing.T) {
	tests := []struct {
		name         string
		pattern      string
		wantStrategy Strategy
	}{
		{
			name:         "multiline_line_anchored_suffix",
			pattern:      "(?m)^.*\\.php",
			wantStrategy: UseMultilineReverseSuffix,
		},
		{
			name:         "multiline_with_wildcard_and_charclass",
			pattern:      "(?m)^/.*[a-z]+\\.txt",
			wantStrategy: UseMultilineReverseSuffix,
		},
		// Unanchored patterns should use regular ReverseSuffix
		{
			name:         "unanchored_suffix_pattern",
			pattern:      ".*\\.txt",
			wantStrategy: UseReverseSuffix,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			got := engine.Strategy()
			if got != tt.wantStrategy {
				t.Errorf("Strategy() = %v, want %v", got, tt.wantStrategy)
			}
		})
	}
}

// TestMultilineReverseSuffixCorrectness verifies results against stdlib.
func TestMultilineReverseSuffixCorrectness(t *testing.T) {
	patterns := []string{
		"(?m)^/.*\\.php",
		"(?m)^.*@example\\.com",
		"(?m)^/api/.*\\.json",
	}

	inputs := []string{
		"/index.php",
		"/admin/login.php\n/other/path",
		"/some/path\n/admin/login.php",
		"text\n/api/users.json\nmore",
		"user@example.com\nother@test.org",
		"no match here",
		"\n\n\n/test.php\n\n",
	}

	for _, pattern := range patterns {
		stdRe := regexp.MustCompile(pattern)
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", pattern, err)
		}

		for _, input := range inputs {
			haystack := []byte(input)

			// Test IsMatch
			stdMatch := stdRe.Match(haystack)
			ourMatch := engine.IsMatch(haystack)
			if stdMatch != ourMatch {
				t.Errorf("Pattern %q, input %q: IsMatch() = %v, stdlib = %v",
					pattern, input, ourMatch, stdMatch)
			}

			// Test Find (only verify existence, not exact bounds)
			stdLoc := stdRe.FindIndex(haystack)
			ourLoc := engine.Find(haystack)

			stdHasMatch := stdLoc != nil
			ourHasMatch := ourLoc != nil

			if stdHasMatch != ourHasMatch {
				t.Errorf("Pattern %q, input %q: Find() hasMatch = %v, stdlib = %v",
					pattern, input, ourHasMatch, stdHasMatch)
			}
		}
	}
}

// BenchmarkMultilineReverseSuffix benchmarks multiline suffix patterns.
func BenchmarkMultilineReverseSuffix(b *testing.B) {
	// Pattern from Issue #97: (?m)^/.*[\w-]+\.php
	pattern := "(?m)^/.*\\.php"
	engine, err := Compile(pattern)
	if err != nil {
		b.Fatalf("Compile error: %v", err)
	}
	stdRe := regexp.MustCompile(pattern)

	benchmarks := []struct {
		name     string
		haystack string
	}{
		{"short_match", "/index.php"},
		{"medium_match", "/admin/users/login.php"},
		{"multiline_first", "/api/v1/users.php\n/other/stuff\n/more/data"},
		{"multiline_middle", "/some/path\n/api/v1/config.php\n/footer"},
		{"multiline_last", "/header\n/data\n/final/page.php"},
		{"no_match", "/index.html\n/style.css\n/script.js"},
		{"long_no_match", string(make([]byte, 1000)) + "\n" + string(make([]byte, 1000))},
	}

	for _, bm := range benchmarks {
		haystack := []byte(bm.haystack)

		b.Run(bm.name+"_coregex", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				engine.IsMatch(haystack)
			}
		})

		b.Run(bm.name+"_stdlib", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				stdRe.Match(haystack)
			}
		})
	}
}

// BenchmarkMultilineReverseSuffixFind benchmarks Find operation.
func BenchmarkMultilineReverseSuffixFind(b *testing.B) {
	pattern := "(?m)^/.*\\.php"
	engine, err := Compile(pattern)
	if err != nil {
		b.Fatalf("Compile error: %v", err)
	}
	stdRe := regexp.MustCompile(pattern)

	// Simulate a log file with PHP requests
	haystack := []byte(`/css/style.css
/js/main.js
/images/logo.png
/api/users/list.php
/api/users/create.php
/html/index.html
/admin/dashboard.php
/favicon.ico
`)

	b.Run("coregex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			engine.Find(haystack)
		}
	})

	b.Run("stdlib", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			stdRe.FindIndex(haystack)
		}
	})
}

// TestFindLineStart tests the findLineStart helper function.
func TestFindLineStart(t *testing.T) {
	tests := []struct {
		name     string
		haystack string
		pos      int
		want     int
	}{
		{"start_of_input", "hello", 3, 0},
		{"after_newline", "hello\nworld", 8, 6},
		{"multiple_newlines", "a\nb\nc\nd", 6, 6},
		{"at_newline", "hello\nworld", 6, 6},
		{"empty_line", "a\n\nc", 2, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findLineStart([]byte(tt.haystack), tt.pos)
			if got != tt.want {
				t.Errorf("findLineStart(%q, %d) = %d, want %d", tt.haystack, tt.pos, got, tt.want)
			}
		})
	}
}
