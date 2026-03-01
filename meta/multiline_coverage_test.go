package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestMultilineReverseSuffixFindAll exercises Find and FindAt for multiline patterns.
// Covers: reverse_suffix_multiline.go Find (81%), FindAt (65%), verifyPrefix (60%)
func TestMultilineReverseSuffixFindAll(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    []string
	}{
		{
			name:    "single_line_match",
			pattern: `(?m)^/.*\.php`,
			input:   "/index.php",
			want:    []string{"/index.php"},
		},
		{
			name:    "multi_line_multiple_matches",
			pattern: `(?m)^/.*\.php`,
			input:   "/index.php\n/admin.php\n/test.html",
			want:    []string{"/index.php", "/admin.php"},
		},
		{
			name:    "no_match_prefix_fails",
			pattern: `(?m)^/.*\.php`,
			input:   "index.php\nadmin.php",
			want:    nil,
		},
		{
			name:    "empty_input",
			pattern: `(?m)^/.*\.php`,
			input:   "",
			want:    nil,
		},
		{
			name:    "match_on_first_line_only",
			pattern: `(?m)^/.*\.css`,
			input:   "/style.css\nno-match.txt",
			want:    []string{"/style.css"},
		},
		{
			name:    "match_on_second_line_only",
			pattern: `(?m)^/.*\.js`,
			input:   "readme.txt\n/app.js",
			want:    []string{"/app.js"},
		},
		{
			name:    "suffix_at_end_of_line",
			pattern: `(?m)^/.*\.htm`,
			input:   "skip\n/page.htm\nskip2",
			want:    []string{"/page.htm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			haystack := []byte(tt.input)
			indices := engine.FindAllIndicesStreaming(haystack, 0, nil)

			if len(tt.want) == 0 {
				if len(indices) != 0 {
					t.Errorf("expected no matches, got %d", len(indices))
					for i, idx := range indices {
						t.Logf("  match[%d] = %q", i, string(haystack[idx[0]:idx[1]]))
					}
				}
				return
			}

			if len(indices) != len(tt.want) {
				t.Fatalf("expected %d matches, got %d", len(tt.want), len(indices))
			}

			for i, w := range tt.want {
				got := string(haystack[indices[i][0]:indices[i][1]])
				if got != w {
					t.Errorf("match[%d]: got %q, want %q", i, got, w)
				}
			}
		})
	}
}

// TestMultilineReverseSuffixIsMatch exercises IsMatch for multiline patterns.
// Covers: reverse_suffix_multiline.go IsMatch (80%)
func TestMultilineReverseSuffixIsMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"match_first_line", `(?m)^/.*\.php`, "/index.php", true},
		{"match_second_line", `(?m)^/.*\.php`, "nope\n/admin.php", true},
		{"no_match", `(?m)^/.*\.php`, "index.php\nadmin.css", false},
		{"empty", `(?m)^/.*\.php`, "", false},
		{"long_prefix_match", `(?m)^/.*\.php`, strings.Repeat("skip\n", 50) + "/deep/path.php", true},
		{"multiple_lines_no_prefix", `(?m)^/.*\.php`, "a.php\nb.php\nc.php", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v (strategy=%d)", tt.input, got, tt.want, engine.strategy)
			}
		})
	}
}

// TestMultilineReverseSuffixCorrectnessVsStdlib validates results against stdlib.
// Covers: multiline Find, FindAt, IsMatch cross-validation
func TestMultilineReverseSuffixCorrectnessVsStdlib(t *testing.T) {
	patterns := []string{
		`(?m)^/.*\.php`,
		`(?m)^/.*\.html`,
		`(?m)^/.*\.css`,
	}

	inputs := []string{
		"/index.php\n/admin.html\n/style.css",
		"no match here\nnone at all",
		"/root.php",
		"",
		"line1\n/test.php\nline3\n/more.html",
		strings.Repeat("x\n", 30) + "/deep.css",
	}

	for _, pat := range patterns {
		re := regexp.MustCompile(pat)
		engine, err := Compile(pat)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", pat, err)
		}

		for _, input := range inputs {
			hay := []byte(input)

			// IsMatch
			gotMatch := engine.IsMatch(hay)
			wantMatch := re.Match(hay)
			if gotMatch != wantMatch {
				t.Errorf("pattern=%q input=%q: IsMatch=%v want=%v", pat, input, gotMatch, wantMatch)
			}

			// Find
			gotFind := engine.Find(hay)
			wantFind := re.Find(hay)
			if (gotFind == nil) != (wantFind == nil) {
				t.Errorf("pattern=%q input=%q: Find nil mismatch: got=%v want=%v", pat, input, gotFind == nil, wantFind == nil)
			}
			if gotFind != nil && wantFind != nil {
				if gotFind.String() != string(wantFind) {
					t.Errorf("pattern=%q input=%q: Find=%q want=%q", pat, input, gotFind.String(), string(wantFind))
				}
			}

			// FindAll via indices
			gotIndices := engine.FindAllIndicesStreaming(hay, 0, nil)
			wantAll := re.FindAllIndex(hay, -1)
			if len(gotIndices) != len(wantAll) {
				t.Errorf("pattern=%q input=%q: FindAll count=%d want=%d", pat, input, len(gotIndices), len(wantAll))
				continue
			}
			for i := range gotIndices {
				gotStr := string(hay[gotIndices[i][0]:gotIndices[i][1]])
				wantStr := string(hay[wantAll[i][0]:wantAll[i][1]])
				if gotStr != wantStr {
					t.Errorf("pattern=%q input=%q: FindAll[%d]=%q want=%q", pat, input, i, gotStr, wantStr)
					break
				}
			}
		}
	}
}

// TestMultilineReverseSuffixFindAt exercises FindAt directly.
// Covers: reverse_suffix_multiline.go FindAt (65%)
func TestMultilineReverseSuffixFindAt(t *testing.T) {
	engine, err := Compile(`(?m)^/.*\.php`)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	input := []byte("/first.php\n/second.php\nno-match")

	// FindAt from 0
	m := engine.FindAt(input, 0)
	if m == nil {
		t.Fatal("expected match at 0")
	}
	if m.String() != "/first.php" {
		t.Errorf("FindAt(0) = %q, want %q", m.String(), "/first.php")
	}

	// FindAt from after first match
	m = engine.FindAt(input, 11)
	if m == nil {
		t.Fatal("expected match at 11")
	}
	if m.String() != "/second.php" {
		t.Errorf("FindAt(11) = %q, want %q", m.String(), "/second.php")
	}

	// FindAt beyond end
	m = engine.FindAt(input, 100)
	if m != nil {
		t.Error("expected nil beyond end")
	}
}

// TestMultilineReverseSuffixDFAFallback exercises the DFA (slow) path
// for multiline patterns without prefix literals.
// Covers: reverse_suffix_multiline.go Find/IsMatch DFA path, findLineStart
func TestMultilineReverseSuffixDFAFallback(t *testing.T) {
	// Pattern with multiline anchor but complex structure triggers DFA path
	// (?m)^[a-z]+\.php - no simple prefix literal
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"dfa_match", `(?m)^[a-z]+\.cfg`, "test.cfg\nother", true},
		{"dfa_no_match", `(?m)^[a-z]+\.cfg`, "TEST.cfg\nOTHER", false},
		{"dfa_second_line", `(?m)^[a-z]+\.cfg`, "SKIP\ntest.cfg", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch=%v, want %v (strategy=%d)", got, tt.want, engine.strategy)
			}

			// Verify vs stdlib
			re := regexp.MustCompile(tt.pattern)
			want := re.MatchString(tt.input)
			if got != want {
				t.Errorf("stdlib mismatch: coregex=%v stdlib=%v", got, want)
			}
		})
	}
}
