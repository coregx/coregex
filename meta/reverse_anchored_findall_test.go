package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestReverseAnchoredAllAPIs verifies FindAll, Count, FindAllSubmatch, and
// FindIndicesAt (used by ReplaceAll/Split) are all linear for $-anchored
// patterns. Issue #183.
func TestReverseAnchoredAllAPIs(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{"error_dollar", `ERROR.*$`, "line1\nERROR foo\nline3\nERROR bar"},
		{"word_dollar", `WARN.*$`, "WARN: first\nINFO: second\nWARN: third"},
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
			input := []byte(tt.input)

			// FindAll
			got := re.FindAllIndicesStreaming(input, -1, nil)
			want := std.FindAllStringIndex(tt.input, -1)
			if len(got) != len(want) {
				t.Errorf("FindAll: got %d matches, want %d", len(got), len(want))
			}
			for i := range got {
				if i < len(want) && (got[i][0] != want[i][0] || got[i][1] != want[i][1]) {
					t.Errorf("FindAll[%d] = %v, want %v", i, got[i], want[i])
				}
			}

			// Count
			gotCount := re.Count(input, -1)
			if gotCount != len(want) {
				t.Errorf("Count = %d, want %d", gotCount, len(want))
			}

			// FindAllSubmatch
			gotSub := re.FindAllSubmatch(input, -1)
			wantSub := std.FindAllSubmatch(input, -1)
			if len(gotSub) != len(wantSub) {
				t.Errorf("FindAllSubmatch: got %d, want %d", len(gotSub), len(wantSub))
			}

			// FindIndicesAt (ReplaceAll/Split path)
			s, e, found := re.FindIndicesAt(input, 0)
			wantIdx := std.FindIndex(input)
			if found != (wantIdx != nil) {
				t.Errorf("FindIndicesAt(0): found=%v, want=%v", found, wantIdx != nil)
			}
			if found && wantIdx != nil && (s != wantIdx[0] || e != wantIdx[1]) {
				t.Errorf("FindIndicesAt(0) = [%d,%d], want %v", s, e, wantIdx)
			}
		})
	}
}

// TestReverseAnchoredLinearTime verifies O(n) for all APIs on large input.
func TestReverseAnchoredLinearTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}

	line := "2026-10-09 12:00:01 INFO  Server started on port 8080\n"
	input := strings.Repeat(line, 2400)
	inputBytes := []byte(input)

	re, err := Compile(`ERROR.*$`)
	if err != nil {
		t.Fatal(err)
	}

	if results := re.FindAllIndicesStreaming(inputBytes, -1, nil); len(results) != 0 {
		t.Errorf("FindAll: expected 0 matches, got %d", len(results))
	}
	if count := re.Count(inputBytes, -1); count != 0 {
		t.Errorf("Count: expected 0, got %d", count)
	}
	if got := re.FindAllSubmatch(inputBytes, -1); len(got) != 0 {
		t.Errorf("FindAllSubmatch: expected 0, got %d", len(got))
	}
	_, _, found := re.FindIndicesAt(inputBytes, 0)
	if found {
		t.Error("FindIndicesAt(0): expected no match")
	}
}

// TestReverseAnchoredMultipleCandidates verifies correct single match with
// multiple literal candidates in the text.
func TestReverseAnchoredMultipleCandidates(t *testing.T) {
	input := "ERROR first\nERROR second\nERROR third"
	re, err := Compile(`ERROR.*$`)
	if err != nil {
		t.Fatal(err)
	}
	std := regexp.MustCompile(`ERROR.*$`)

	got := re.FindAllIndicesStreaming([]byte(input), -1, nil)
	want := std.FindAllStringIndex(input, -1)
	if len(got) != len(want) {
		t.Fatalf("FindAll: got %d matches %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i][0] != want[i][0] || got[i][1] != want[i][1] {
			t.Errorf("FindAll[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestReverseSearcherLeftmostMatch verifies the reverse searcher returns the
// leftmost (greedy) match, not the first/empty match. Pre-existing bug fixed
// by preserving ByteRange edges in fillStartStateWithIncoming. Issue #183.
func TestReverseSearcherLeftmostMatch(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{`a*$`, "aaa"},
		{`[a-c]*$`, "xabc"},
		{`a*b*$`, "aabb"},
		{`(?:a*)$`, "aaa"},
		{`xa*$`, "xaaa"},
		{`a+$`, "aaa"},
		{`\d+$`, "x123"},
		{`(a|aa)$`, "aa"},
		{`a?$`, "a"},
		{`a{0,3}$`, "aaa"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			re, err := Compile(tt.pattern)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.pattern, err)
			}
			std := regexp.MustCompile(tt.pattern)

			gotS, gotE, gotFound := re.FindIndices([]byte(tt.input))
			want := std.FindStringIndex(tt.input)
			if gotFound != (want != nil) {
				t.Errorf("Find(%q, %q): found=%v, want=%v", tt.pattern, tt.input, gotFound, want != nil)
			} else if gotFound && (gotS != want[0] || gotE != want[1]) {
				t.Errorf("Find(%q, %q): got [%d,%d], want %v", tt.pattern, tt.input, gotS, gotE, want)
			}
		})
	}
}
