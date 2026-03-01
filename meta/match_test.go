package meta

import (
	"testing"
)

// TestNewMatch tests Match construction via NewMatch.
func TestNewMatch(t *testing.T) {
	haystack := []byte("hello world foo bar")

	tests := []struct {
		name      string
		start     int
		end       int
		wantStart int
		wantEnd   int
		wantLen   int
		wantStr   string
		wantEmpty bool
	}{
		{
			name:      "normal match in middle",
			start:     6,
			end:       11,
			wantStart: 6,
			wantEnd:   11,
			wantLen:   5,
			wantStr:   "world",
			wantEmpty: false,
		},
		{
			name:      "match at beginning",
			start:     0,
			end:       5,
			wantStart: 0,
			wantEnd:   5,
			wantLen:   5,
			wantStr:   "hello",
			wantEmpty: false,
		},
		{
			name:      "match at end",
			start:     16,
			end:       19,
			wantStart: 16,
			wantEnd:   19,
			wantLen:   3,
			wantStr:   "bar",
			wantEmpty: false,
		},
		{
			name:      "single byte match",
			start:     0,
			end:       1,
			wantStart: 0,
			wantEnd:   1,
			wantLen:   1,
			wantStr:   "h",
			wantEmpty: false,
		},
		{
			name:      "empty match (zero length)",
			start:     5,
			end:       5,
			wantStart: 5,
			wantEnd:   5,
			wantLen:   0,
			wantStr:   "",
			wantEmpty: true,
		},
		{
			name:      "empty match at position 0",
			start:     0,
			end:       0,
			wantStart: 0,
			wantEnd:   0,
			wantLen:   0,
			wantStr:   "",
			wantEmpty: true,
		},
		{
			name:      "full haystack match",
			start:     0,
			end:       len(haystack),
			wantStart: 0,
			wantEnd:   len(haystack),
			wantLen:   len(haystack),
			wantStr:   "hello world foo bar",
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMatch(tt.start, tt.end, haystack)

			if m.Start() != tt.wantStart {
				t.Errorf("Start() = %d, want %d", m.Start(), tt.wantStart)
			}
			if m.End() != tt.wantEnd {
				t.Errorf("End() = %d, want %d", m.End(), tt.wantEnd)
			}
			if m.Len() != tt.wantLen {
				t.Errorf("Len() = %d, want %d", m.Len(), tt.wantLen)
			}
			if m.String() != tt.wantStr {
				t.Errorf("String() = %q, want %q", m.String(), tt.wantStr)
			}
			if m.IsEmpty() != tt.wantEmpty {
				t.Errorf("IsEmpty() = %v, want %v", m.IsEmpty(), tt.wantEmpty)
			}
		})
	}
}

// TestMatchBytes tests that Bytes returns a view into the original haystack.
func TestMatchBytes(t *testing.T) {
	haystack := []byte("abcdef")

	tests := []struct {
		name    string
		start   int
		end     int
		wantNil bool
		wantStr string
	}{
		{"valid range", 2, 4, false, "cd"},
		{"full range", 0, 6, false, "abcdef"},
		{"empty range at start", 0, 0, false, ""},
		{"negative start returns nil", -1, 3, true, ""},
		{"end beyond haystack returns nil", 0, 100, true, ""},
		{"start > end returns nil", 4, 2, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMatch(tt.start, tt.end, haystack)
			got := m.Bytes()

			if tt.wantNil {
				if got != nil {
					t.Errorf("Bytes() = %q, want nil", got)
				}
				return
			}

			if string(got) != tt.wantStr {
				t.Errorf("Bytes() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

// TestMatchBytesSharesMemory verifies Bytes returns a slice into the original haystack,
// not a copy.
func TestMatchBytesSharesMemory(t *testing.T) {
	haystack := []byte("hello world")
	m := NewMatch(0, 5, haystack)

	b := m.Bytes()
	if string(b) != "hello" {
		t.Fatalf("unexpected bytes: %q", b)
	}

	// Modifying haystack should affect the returned bytes
	haystack[0] = 'H'
	if b[0] != 'H' {
		t.Error("Bytes() should return a view into the original haystack, not a copy")
	}
}

// TestMatchContains tests the Contains method for various positions.
func TestMatchContains(t *testing.T) {
	m := NewMatch(5, 10, []byte("0123456789abcdef"))

	tests := []struct {
		name string
		pos  int
		want bool
	}{
		{"before range", 3, false},
		{"at start (inclusive)", 5, true},
		{"in middle", 7, true},
		{"at end minus one", 9, true},
		{"at end (exclusive)", 10, false},
		{"after range", 12, false},
		{"position 0", 0, false},
		{"negative position", -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.Contains(tt.pos)
			if got != tt.want {
				t.Errorf("Contains(%d) = %v, want %v", tt.pos, got, tt.want)
			}
		})
	}
}

// TestMatchContainsEmptyMatch tests Contains on an empty match (start == end).
func TestMatchContainsEmptyMatch(t *testing.T) {
	m := NewMatch(5, 5, []byte("0123456789"))

	// An empty match contains no positions
	if m.Contains(5) {
		t.Error("empty match should not contain its position")
	}
	if m.Contains(4) {
		t.Error("empty match should not contain position before it")
	}
}

// TestMatchStringAllocates verifies that String() returns a new string,
// independent of the haystack.
func TestMatchStringAllocates(t *testing.T) {
	haystack := []byte("hello world")
	m := NewMatch(0, 5, haystack)

	s := m.String()
	if s != "hello" {
		t.Fatalf("String() = %q, want %q", s, "hello")
	}

	// Modifying the haystack should NOT affect the string
	haystack[0] = 'X'
	if s != "hello" {
		t.Error("String() should return an independent copy, not a reference to haystack")
	}
}

// TestMatchWithCapturesCreation tests MatchWithCaptures construction and accessors.
func TestMatchWithCapturesCreation(t *testing.T) {
	haystack := []byte("user@example.com")

	tests := []struct {
		name          string
		captures      [][]int
		wantStart     int
		wantEnd       int
		wantStr       string
		wantNumGroups int
	}{
		{
			name:          "email pattern with 3 groups",
			captures:      [][]int{{0, 16}, {0, 4}, {5, 12}, {13, 16}},
			wantStart:     0,
			wantEnd:       16,
			wantStr:       "user@example.com",
			wantNumGroups: 4,
		},
		{
			name:          "single group (whole match only)",
			captures:      [][]int{{0, 4}},
			wantStart:     0,
			wantEnd:       4,
			wantStr:       "user",
			wantNumGroups: 1,
		},
		{
			name:          "empty captures",
			captures:      [][]int{},
			wantStart:     -1,
			wantEnd:       -1,
			wantStr:       "",
			wantNumGroups: 0,
		},
		{
			name:          "nil group 0",
			captures:      [][]int{nil, {5, 12}},
			wantStart:     -1,
			wantEnd:       -1,
			wantStr:       "",
			wantNumGroups: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMatchWithCaptures(haystack, tt.captures)

			if m.Start() != tt.wantStart {
				t.Errorf("Start() = %d, want %d", m.Start(), tt.wantStart)
			}
			if m.End() != tt.wantEnd {
				t.Errorf("End() = %d, want %d", m.End(), tt.wantEnd)
			}
			if m.String() != tt.wantStr {
				t.Errorf("String() = %q, want %q", m.String(), tt.wantStr)
			}
			if m.NumCaptures() != tt.wantNumGroups {
				t.Errorf("NumCaptures() = %d, want %d", m.NumCaptures(), tt.wantNumGroups)
			}
		})
	}
}

// TestMatchWithCapturesGroup tests individual capture group access.
func TestMatchWithCapturesGroup(t *testing.T) {
	haystack := []byte("2024-01-15 info: hello world")
	// Pattern: (\d{4})-(\d{2})-(\d{2}) (\w+): (.*)
	captures := [][]int{
		{0, 28},  // group 0: whole match
		{0, 4},   // group 1: year
		{5, 7},   // group 2: month
		{8, 10},  // group 3: day
		{11, 15}, // group 4: level
		{17, 28}, // group 5: message
	}

	m := NewMatchWithCaptures(haystack, captures)

	tests := []struct {
		name      string
		index     int
		wantStr   string
		wantNil   bool
		wantIndex []int
	}{
		{"group 0 (whole)", 0, "2024-01-15 info: hello world", false, []int{0, 28}},
		{"group 1 (year)", 1, "2024", false, []int{0, 4}},
		{"group 2 (month)", 2, "01", false, []int{5, 7}},
		{"group 3 (day)", 3, "15", false, []int{8, 10}},
		{"group 4 (level)", 4, "info", false, []int{11, 15}},
		{"group 5 (message)", 5, "hello world", false, []int{17, 28}},
		{"negative index", -1, "", true, nil},
		{"index out of range", 10, "", true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.Group(tt.index)
			if tt.wantNil {
				if got != nil {
					t.Errorf("Group(%d) = %q, want nil", tt.index, got)
				}
			} else {
				if string(got) != tt.wantStr {
					t.Errorf("Group(%d) = %q, want %q", tt.index, got, tt.wantStr)
				}
			}

			gotStr := m.GroupString(tt.index)
			if gotStr != tt.wantStr {
				t.Errorf("GroupString(%d) = %q, want %q", tt.index, gotStr, tt.wantStr)
			}

			gotIdx := m.GroupIndex(tt.index)
			if tt.wantIndex == nil {
				if gotIdx != nil {
					t.Errorf("GroupIndex(%d) = %v, want nil", tt.index, gotIdx)
				}
			} else {
				if len(gotIdx) != 2 || gotIdx[0] != tt.wantIndex[0] || gotIdx[1] != tt.wantIndex[1] {
					t.Errorf("GroupIndex(%d) = %v, want %v", tt.index, gotIdx, tt.wantIndex)
				}
			}
		})
	}
}

// TestMatchWithCapturesNilGroup tests behavior with nil (unmatched) capture groups.
func TestMatchWithCapturesNilGroup(t *testing.T) {
	haystack := []byte("abc")
	// Simulate optional group: (a)(b)?(c) where group 2 is unmatched
	captures := [][]int{
		{0, 3}, // group 0: "abc"
		{0, 1}, // group 1: "a"
		nil,    // group 2: unmatched
		{2, 3}, // group 3: "c"
	}

	m := NewMatchWithCaptures(haystack, captures)

	if got := m.Group(2); got != nil {
		t.Errorf("Group(2) = %q, want nil for unmatched group", got)
	}
	if got := m.GroupString(2); got != "" {
		t.Errorf("GroupString(2) = %q, want empty string for unmatched group", got)
	}
	if got := m.GroupIndex(2); got != nil {
		t.Errorf("GroupIndex(2) = %v, want nil for unmatched group", got)
	}

	// Matched groups should still work
	if got := m.GroupString(1); got != "a" {
		t.Errorf("GroupString(1) = %q, want %q", got, "a")
	}
	if got := m.GroupString(3); got != "c" {
		t.Errorf("GroupString(3) = %q, want %q", got, "c")
	}
}

// TestMatchWithCapturesAllGroups tests AllGroups and AllGroupStrings.
func TestMatchWithCapturesAllGroups(t *testing.T) {
	haystack := []byte("foo bar baz")
	captures := [][]int{
		{0, 11}, // group 0
		{0, 3},  // group 1: "foo"
		{4, 7},  // group 2: "bar"
		nil,     // group 3: unmatched
	}

	m := NewMatchWithCaptures(haystack, captures)

	groups := m.AllGroups()
	if len(groups) != 4 {
		t.Fatalf("AllGroups() returned %d groups, want 4", len(groups))
	}
	if string(groups[0]) != "foo bar baz" {
		t.Errorf("AllGroups()[0] = %q, want %q", groups[0], "foo bar baz")
	}
	if string(groups[1]) != "foo" {
		t.Errorf("AllGroups()[1] = %q, want %q", groups[1], "foo")
	}
	if string(groups[2]) != "bar" {
		t.Errorf("AllGroups()[2] = %q, want %q", groups[2], "bar")
	}
	if groups[3] != nil {
		t.Errorf("AllGroups()[3] = %q, want nil", groups[3])
	}

	strs := m.AllGroupStrings()
	if len(strs) != 4 {
		t.Fatalf("AllGroupStrings() returned %d strings, want 4", len(strs))
	}
	if strs[1] != "foo" {
		t.Errorf("AllGroupStrings()[1] = %q, want %q", strs[1], "foo")
	}
	if strs[3] != "" {
		t.Errorf("AllGroupStrings()[3] = %q, want empty", strs[3])
	}
}

// TestMatchWithCapturesBytes tests Bytes method boundary checks.
func TestMatchWithCapturesBytes(t *testing.T) {
	haystack := []byte("abcdef")

	tests := []struct {
		name     string
		captures [][]int
		wantNil  bool
		wantStr  string
	}{
		{
			name:     "valid group 0",
			captures: [][]int{{1, 4}},
			wantNil:  false,
			wantStr:  "bcd",
		},
		{
			name:     "empty captures",
			captures: [][]int{},
			wantNil:  true,
		},
		{
			name:     "nil group 0",
			captures: [][]int{nil},
			wantNil:  true,
		},
		{
			name:     "negative start in group 0",
			captures: [][]int{{-1, 3}},
			wantNil:  true,
		},
		{
			name:     "end beyond haystack",
			captures: [][]int{{0, 100}},
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMatchWithCaptures(haystack, tt.captures)
			got := m.Bytes()
			if tt.wantNil {
				if got != nil {
					t.Errorf("Bytes() = %q, want nil", got)
				}
			} else {
				if string(got) != tt.wantStr {
					t.Errorf("Bytes() = %q, want %q", got, tt.wantStr)
				}
			}
		})
	}
}

// TestMatchWithCapturesGroupBoundaryValidation tests Group method
// with out-of-bounds capture indices.
func TestMatchWithCapturesGroupBoundaryValidation(t *testing.T) {
	haystack := []byte("test data here")
	captures := [][]int{
		{0, 14},  // group 0: whole match
		{0, 100}, // group 1: end beyond haystack (invalid)
		{-5, 4},  // group 2: negative start (invalid)
	}

	m := NewMatchWithCaptures(haystack, captures)

	// Group 1: end beyond haystack should return nil
	if got := m.Group(1); got != nil {
		t.Errorf("Group(1) with end beyond haystack = %q, want nil", got)
	}

	// Group 2: negative start should return nil
	if got := m.Group(2); got != nil {
		t.Errorf("Group(2) with negative start = %q, want nil", got)
	}
}

// TestMatchNilHaystack tests Match behavior with nil haystack.
func TestMatchNilHaystack(t *testing.T) {
	m := NewMatch(0, 0, nil)
	if m.Start() != 0 {
		t.Errorf("Start() = %d, want 0", m.Start())
	}
	if m.End() != 0 {
		t.Errorf("End() = %d, want 0", m.End())
	}
	if m.Len() != 0 {
		t.Errorf("Len() = %d, want 0", m.Len())
	}
	if !m.IsEmpty() {
		t.Error("IsEmpty() = false, want true")
	}
}
