package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestReverseSuffixFindAll exercises ReverseSuffix FindAt path used by FindAll.
// Covers: reverse_suffix.go FindAt (0%), FindIndicesAt (50%)
func TestReverseSuffixFindAll(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		wantAll []string
	}{
		{
			name:    "suffix_single_match",
			pattern: `.*\.txt`,
			input:   "file.txt",
			wantAll: []string{"file.txt"},
		},
		{
			name:    "suffix_another_extension",
			pattern: `.*\.yaml`,
			input:   "config.yaml",
			wantAll: []string{"config.yaml"},
		},
		{
			name:    "suffix_no_match",
			pattern: `.*\.csv`,
			input:   "data.txt and more.log",
			wantAll: nil,
		},
		{
			name:    "suffix_empty_input",
			pattern: `.*\.xml`,
			input:   "",
			wantAll: nil,
		},
		{
			name:    "suffix_match_at_start",
			pattern: `.*\.go`,
			input:   ".go",
			wantAll: []string{".go"},
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

			if len(tt.wantAll) == 0 {
				if len(indices) != 0 {
					t.Errorf("expected no matches, got %d", len(indices))
				}
				return
			}

			if len(indices) != len(tt.wantAll) {
				t.Fatalf("expected %d matches, got %d", len(tt.wantAll), len(indices))
			}

			for i, want := range tt.wantAll {
				got := string(haystack[indices[i][0]:indices[i][1]])
				if got != want {
					t.Errorf("match[%d]: got %q, want %q", i, got, want)
				}
			}
		})
	}
}

// TestReverseSuffixIsMatch exercises IsMatch path for ReverseSuffix patterns.
// Covers: reverse_suffix.go IsMatch anti-quadratic guard paths.
func TestReverseSuffixIsMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"match_suffix", `.*\.html`, "index.html", true},
		{"no_match", `.*\.html`, "index.css", false},
		{"empty_input", `.*\.html`, "", false},
		{"long_input_match", `.*\.json`, strings.Repeat("x", 1000) + ".json", true},
		{"long_input_no_match", `.*\.json`, strings.Repeat("x", 1000) + ".xml", false},
		{"multiple_suffix_occurrences", `.*\.txt`, "a.txt.b.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestReverseSuffixFindIndicesAt exercises FindIndicesAt for ReverseSuffix.
// Covers: reverse_suffix.go FindIndicesAt (50%)
func TestReverseSuffixFindIndicesAt(t *testing.T) {
	engine, err := Compile(`.*\.cfg`)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	haystack := []byte("app.cfg")

	// FindIndicesAt from position 0
	s, e, found := engine.FindIndicesAt(haystack, 0)
	if !found {
		t.Fatal("expected match at position 0")
	}
	if s != 0 || e != 7 {
		t.Errorf("FindIndicesAt(0) = (%d,%d), want (0,7)", s, e)
	}

	// FindIndicesAt from beyond end
	_, _, found = engine.FindIndicesAt(haystack, 100)
	if found {
		t.Error("expected no match beyond end")
	}
}

// TestReverseInnerFindAll exercises ReverseInner Find and FindIndicesAt paths.
// Covers: reverse_inner.go Find (14%), FindIndicesAt (0%)
func TestReverseInnerFindAll(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    string
		wantNil bool
	}{
		{
			name:    "inner_match",
			pattern: `.*connection.*`,
			input:   "ERROR: connection timeout",
			want:    "ERROR: connection timeout",
		},
		{
			name:    "inner_no_match",
			pattern: `.*connection.*`,
			input:   "no match here",
			wantNil: true,
		},
		{
			name:    "inner_empty_input",
			pattern: `.*connection.*`,
			input:   "",
			wantNil: true,
		},
		{
			name:    "inner_keyword_at_start",
			pattern: `.*error.*`,
			input:   "error happened",
			want:    "error happened",
		},
		{
			name:    "inner_keyword_at_end",
			pattern: `.*timeout.*`,
			input:   "request timeout",
			want:    "request timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			match := engine.Find([]byte(tt.input))
			if tt.wantNil {
				if match != nil {
					t.Errorf("expected nil, got %q", match.String())
				}
				return
			}

			if match == nil {
				t.Fatal("expected match, got nil")
			}

			if match.String() != tt.want {
				t.Errorf("got %q, want %q", match.String(), tt.want)
			}
		})
	}
}

// TestReverseInnerIsMatch exercises IsMatch for ReverseInner patterns.
// Covers: reverse_inner.go IsMatch (73%)
func TestReverseInnerIsMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"match", `.*keyword.*`, "has keyword here", true},
		{"no_match", `.*keyword.*`, "nothing here", false},
		{"empty", `.*keyword.*`, "", false},
		{"keyword_only", `.*keyword.*`, "keyword", true},
		{"long_prefix", `.*keyword.*`, strings.Repeat("a", 500) + "keyword" + strings.Repeat("b", 500), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestReverseInnerFindIndicesAt exercises FindIndicesAt for ReverseInner.
// Covers: reverse_inner.go FindIndicesAt (0%), Find anti-quadratic paths
func TestReverseInnerFindIndicesAt(t *testing.T) {
	engine, err := Compile(`.*token.*`)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	haystack := []byte("invalid token detected")

	// FindIndicesAt from 0
	s, e, found := engine.FindIndicesAt(haystack, 0)
	if !found {
		t.Fatal("expected match at 0")
	}
	if s != 0 || e != len(haystack) {
		t.Errorf("FindIndicesAt(0) = (%d,%d), want (0,%d)", s, e, len(haystack))
	}

	// FindIndicesAt beyond end
	_, _, found = engine.FindIndicesAt(haystack, 100)
	if found {
		t.Error("expected no match beyond end")
	}
}

// TestReverseAnchoredFindAll exercises FindAll for end-anchored patterns.
// Covers: reverse_anchored.go Find paths
func TestReverseAnchoredFindAll(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    string
		wantNil bool
	}{
		{
			name:    "dollar_anchor_match",
			pattern: `\w+\.txt$`,
			input:   "path/to/file.txt",
			want:    "file.txt",
		},
		{
			name:    "dollar_anchor_no_match",
			pattern: `\w+\.txt$`,
			input:   "file.txt.bak",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}

			match := engine.Find([]byte(tt.input))
			if tt.wantNil {
				if match != nil {
					t.Errorf("expected nil, got %q", match.String())
				}
				return
			}

			if match == nil {
				t.Fatal("expected match, got nil")
			}
			if match.String() != tt.want {
				t.Errorf("got %q, want %q", match.String(), tt.want)
			}
		})
	}
}

// TestReverseSuffixSetFindAll exercises ReverseSuffixSet Find and FindAt paths.
// Covers: reverse_suffix_set.go Find (56%), FindAt (0%), FindIndicesAt (0%)
func TestReverseSuffixSetFindAll(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    []string
	}{
		{
			name:    "multi_suffix_single",
			pattern: `.*\.(txt|log|csv)`,
			input:   "data.csv",
			want:    []string{"data.csv"},
		},
		{
			name:    "multi_suffix_different_extensions",
			pattern: `.*\.(txt|log|csv)`,
			input:   "report.csv",
			want:    []string{"report.csv"},
		},
		{
			name:    "multi_suffix_no_match",
			pattern: `.*\.(txt|log|csv)`,
			input:   "file.xml",
			want:    nil,
		},
		{
			name:    "multi_suffix_empty",
			pattern: `.*\.(txt|log|csv)`,
			input:   "",
			want:    nil,
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

// TestReverseSuffixSetIsMatch exercises IsMatch for ReverseSuffixSet.
// Covers: reverse_suffix_set.go IsMatch (71%)
func TestReverseSuffixSetIsMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"match_txt", `.*\.(txt|log|md)`, "readme.txt", true},
		{"match_log", `.*\.(txt|log|md)`, "app.log", true},
		{"match_md", `.*\.(txt|log|md)`, "notes.md", true},
		{"no_match", `.*\.(txt|log|md)`, "image.png", false},
		{"empty", `.*\.(txt|log|md)`, "", false},
		{"long_match", `.*\.(txt|log|md)`, strings.Repeat("a/", 100) + "file.log", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
			}
			got := engine.IsMatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsMatch(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestReverseSuffixSetFindIndicesAt exercises FindIndicesAt for ReverseSuffixSet.
// Covers: reverse_suffix_set.go FindIndicesAt (0%)
func TestReverseSuffixSetFindIndicesAt(t *testing.T) {
	engine, err := Compile(`.*\.(htm|php|asp)`)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	haystack := []byte("index.htm")

	// From position 0
	s, e, found := engine.FindIndicesAt(haystack, 0)
	if !found {
		t.Fatal("expected match")
	}
	if s != 0 || e != 9 {
		t.Errorf("got (%d,%d), want (0,9)", s, e)
	}

	// Beyond end
	_, _, found = engine.FindIndicesAt(haystack, 100)
	if found {
		t.Error("expected no match beyond end")
	}
}

// TestReverseStrategiesCorrectnessVsStdlib validates reverse strategy results against stdlib.
func TestReverseStrategiesCorrectnessVsStdlib(t *testing.T) {
	tests := []struct {
		pattern string
		inputs  []string
	}{
		{`.*\.txt`, []string{"file.txt", "a.txt.txt", "no-match", "", "x.txt/y.txt"}},
		{`.*\.log`, []string{"app.log", "test", ""}},
		{`.*\.(txt|log|csv)`, []string{"data.csv", "test.log", "file.xml", "a.txt.log"}},
		{`.*connection.*`, []string{"lost connection here", "no match", "connection", "xconnection"}},
	}

	for _, tt := range tests {
		re := regexp.MustCompile(tt.pattern)
		engine, err := Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) error: %v", tt.pattern, err)
		}

		for _, input := range tt.inputs {
			hay := []byte(input)

			// IsMatch
			gotMatch := engine.IsMatch(hay)
			wantMatch := re.Match(hay)
			if gotMatch != wantMatch {
				t.Errorf("pattern %q, input %q: IsMatch=%v, want %v", tt.pattern, input, gotMatch, wantMatch)
			}

			// Find
			gotFind := engine.Find(hay)
			wantFind := re.Find(hay)
			if (gotFind == nil) != (wantFind == nil) {
				t.Errorf("pattern %q, input %q: Find nil mismatch: got %v, want %v", tt.pattern, input, gotFind, wantFind)
			}
			if gotFind != nil && wantFind != nil {
				if gotFind.String() != string(wantFind) {
					t.Errorf("pattern %q, input %q: Find=%q, want %q", tt.pattern, input, gotFind.String(), string(wantFind))
				}
			}
		}
	}
}
