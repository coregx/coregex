package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestReverseAnchoredFindAllQuadratic verifies that FindAll/Count for $-anchored patterns
// use the reverse searcher (one scan) instead of PikeVM loop (O(n²)). Issue #183.
func TestReverseAnchoredFindAllQuadratic(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{"error_dotstar_dollar", `ERROR.*$`, "line1\nERROR foo\nline3\nERROR bar"},
		{"word_dotstar_dollar", `WARN.*$`, "WARN: first\nINFO: second\nWARN: third"},
		{"case_insensitive", `(?i)error.*$`, "Error: test\nInfo: ok\nERROR: fail"},
		{"no_match", `MISSING.*$`, "line1\nline2\nline3"},
		{"single_line", `test.*$`, "this is a test case"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}
			std := regexp.MustCompile(tt.pattern)

			// FindAll
			got := re.FindAllIndicesStreaming([]byte(tt.input), -1, nil)
			want := std.FindAllStringIndex(tt.input, -1)
			if len(got) != len(want) {
				t.Errorf("FindAll(%q, %q): got %d matches, want %d",
					tt.pattern, tt.input, len(got), len(want))
				t.Logf("  got:  %v", got)
				t.Logf("  want: %v", want)
				return
			}
			for i := range got {
				if got[i][0] != want[i][0] || got[i][1] != want[i][1] {
					t.Errorf("FindAll(%q, %q)[%d] = %v, want %v",
						tt.pattern, tt.input, i, got[i], want[i])
				}
			}

			// Count
			gotCount := re.Count([]byte(tt.input), -1)
			wantCount := len(want)
			if gotCount != wantCount {
				t.Errorf("Count(%q, %q) = %d, want %d",
					tt.pattern, tt.input, gotCount, wantCount)
			}
		})
	}
}

// TestReverseAnchoredFindAllQuadraticLinearTime verifies O(n) not O(n²).
// If quadratic, 2400 lines would take ~3 seconds; linear should be < 50ms.
func TestReverseAnchoredFindAllQuadraticLinearTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}

	line := "2026-10-09 12:00:01 INFO  Server started on port 8080\n"
	input := []byte(strings.Repeat(line, 2400))

	re, err := Compile(`ERROR.*$`)
	if err != nil {
		t.Fatal(err)
	}

	// Should be nearly instant (one reverse scan, no match)
	results := re.FindAllIndicesStreaming(input, -1, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 matches, got %d", len(results))
	}

	count := re.Count(input, -1)
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

// TestReverseAnchoredMultipleCandidates verifies that with multiple literal
// candidates, the reverse searcher returns the correct single match (the last
// occurrence's match to end of text), not the first candidate.
func TestReverseAnchoredMultipleCandidates(t *testing.T) {
	// "ERROR" appears 3 times, but ERROR.*$ without (?m) matches only the
	// last one that extends to end of text.
	input := "ERROR first\nERROR second\nERROR third"

	re, err := Compile(`ERROR.*$`)
	if err != nil {
		t.Fatal(err)
	}
	std := regexp.MustCompile(`ERROR.*$`)

	got := re.FindAllIndicesStreaming([]byte(input), -1, nil)
	want := std.FindAllStringIndex(input, -1)

	if len(got) != len(want) {
		t.Fatalf("FindAll: got %d matches %v, want %d %v",
			len(got), got, len(want), want)
	}
	for i := range got {
		if got[i][0] != want[i][0] || got[i][1] != want[i][1] {
			t.Errorf("FindAll[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
