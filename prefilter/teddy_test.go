package prefilter

import (
	"bytes"
	"testing"
)

// TestNewTeddy_Validation tests pattern validation during construction
func TestNewTeddy_Validation(t *testing.T) {
	tests := []struct {
		name     string
		patterns [][]byte
		config   *TeddyConfig
		wantNil  bool
		reason   string
	}{
		{
			name:     "valid 2 patterns",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			config:   nil,
			wantNil:  false,
			reason:   "2 patterns of length 3 should be accepted",
		},
		{
			name: "valid 8 patterns",
			patterns: [][]byte{
				[]byte("foo"), []byte("bar"), []byte("baz"), []byte("qux"),
				[]byte("one"), []byte("two"), []byte("six"), []byte("ten"),
			},
			config:  nil,
			wantNil: false,
			reason:  "8 patterns is the maximum for Teddy",
		},
		{
			name:     "too few patterns",
			patterns: [][]byte{[]byte("foo")},
			config:   nil,
			wantNil:  true,
			reason:   "need at least 2 patterns",
		},
		{
			name: "too many patterns",
			patterns: [][]byte{
				[]byte("p1"), []byte("p2"), []byte("p3"), []byte("p4"),
				[]byte("p5"), []byte("p6"), []byte("p7"), []byte("p8"),
				[]byte("p9"),
			},
			config:  nil,
			wantNil: true,
			reason:  "more than 8 patterns not supported",
		},
		{
			name:     "pattern too short",
			patterns: [][]byte{[]byte("foo"), []byte("ab")},
			config:   nil,
			wantNil:  true,
			reason:   "pattern 'ab' is only 2 bytes (need >= 3)",
		},
		{
			name:     "all patterns too short",
			patterns: [][]byte{[]byte("ab"), []byte("cd")},
			config:   nil,
			wantNil:  true,
			reason:   "all patterns < 3 bytes",
		},
		{
			name:     "empty patterns list",
			patterns: [][]byte{},
			config:   nil,
			wantNil:  true,
			reason:   "no patterns provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teddy := NewTeddy(tt.patterns, tt.config)
			if (teddy == nil) != tt.wantNil {
				t.Errorf("NewTeddy() = %v, want nil=%v; reason: %s",
					teddy, tt.wantNil, tt.reason)
			}
		})
	}
}

// TestTeddy_Find_Basic tests basic search functionality
func TestTeddy_Find_Basic(t *testing.T) {
	tests := []struct {
		name     string
		patterns [][]byte
		haystack string
		start    int
		want     int
	}{
		{
			name:     "find first pattern",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "hello foo world",
			start:    0,
			want:     6,
		},
		{
			name:     "find second pattern",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "hello bar world",
			start:    0,
			want:     6,
		},
		{
			name:     "find at start",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "foo bar baz",
			start:    0,
			want:     0,
		},
		{
			name:     "find at end",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "hello world foo",
			start:    0,
			want:     12,
		},
		{
			name:     "not found",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "hello world",
			start:    0,
			want:     -1,
		},
		{
			name:     "find with start offset",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "foo bar foo",
			start:    1,
			want:     4, // Leftmost match after start: "bar" at position 4
		},
		{
			name:     "multiple occurrences (find first)",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "foo bar foo bar",
			start:    0,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teddy := NewTeddy(tt.patterns, nil)
			if teddy == nil {
				t.Fatal("NewTeddy() returned nil")
			}

			got := teddy.Find([]byte(tt.haystack), tt.start)
			if got != tt.want {
				t.Errorf("Find() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestTeddy_Find_MultiplePatterns tests search with various pattern counts
func TestTeddy_Find_MultiplePatterns(t *testing.T) {
	tests := []struct {
		name     string
		patterns [][]byte
		haystack string
		want     int
		wantAny  bool // true if we want any of the patterns (first match)
	}{
		{
			name:     "3 patterns - find first",
			patterns: [][]byte{[]byte("foo"), []byte("bar"), []byte("baz")},
			haystack: "hello baz world",
			want:     6,
		},
		{
			name:     "4 patterns - find middle",
			patterns: [][]byte{[]byte("one"), []byte("two"), []byte("six"), []byte("ten")},
			haystack: "count six items",
			want:     6,
		},
		{
			name: "8 patterns - find last",
			patterns: [][]byte{
				[]byte("aaa"), []byte("bbb"), []byte("ccc"), []byte("ddd"),
				[]byte("eee"), []byte("fff"), []byte("ggg"), []byte("hhh"),
			},
			haystack: "where is hhh here",
			want:     9,
		},
		{
			name: "6 patterns - none found",
			patterns: [][]byte{
				[]byte("foo"), []byte("bar"), []byte("baz"),
				[]byte("qux"), []byte("one"), []byte("two"),
			},
			haystack: "hello world",
			want:     -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teddy := NewTeddy(tt.patterns, nil)
			if teddy == nil {
				t.Fatal("NewTeddy() returned nil")
			}

			got := teddy.Find([]byte(tt.haystack), 0)
			if got != tt.want {
				t.Errorf("Find() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestTeddy_Find_OverlappingPatterns tests patterns that share prefixes
func TestTeddy_Find_OverlappingPatterns(t *testing.T) {
	tests := []struct {
		name     string
		patterns [][]byte
		haystack string
		want     int
	}{
		{
			name:     "prefix overlap (foo, foobar)",
			patterns: [][]byte{[]byte("foo"), []byte("foobar")},
			haystack: "hello foobar world",
			want:     6, // finds "foo" first (shorter)
		},
		{
			name:     "same first bytes (bar, baz)",
			patterns: [][]byte{[]byte("bar"), []byte("baz")},
			haystack: "hello baz world",
			want:     6,
		},
		{
			name:     "shared middle bytes",
			patterns: [][]byte{[]byte("hello"), []byte("jello")},
			haystack: "say jello world",
			want:     4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teddy := NewTeddy(tt.patterns, nil)
			if teddy == nil {
				t.Fatal("NewTeddy() returned nil")
			}

			got := teddy.Find([]byte(tt.haystack), 0)
			if got != tt.want {
				t.Errorf("Find() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestTeddy_Find_EdgeCases tests edge cases
func TestTeddy_Find_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		patterns [][]byte
		haystack string
		start    int
		want     int
	}{
		{
			name:     "empty haystack",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "",
			start:    0,
			want:     -1,
		},
		{
			name:     "haystack shorter than pattern",
			patterns: [][]byte{[]byte("foobar"), []byte("bazqux")},
			haystack: "foo",
			start:    0,
			want:     -1,
		},
		{
			name:     "exact match (haystack == pattern)",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "foo",
			start:    0,
			want:     0,
		},
		{
			name:     "start beyond haystack",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "hello",
			start:    100,
			want:     -1,
		},
		{
			name:     "start at last possible position",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "abcfoo",
			start:    3,
			want:     3,
		},
		{
			name:     "pattern at multiple positions (leftmost)",
			patterns: [][]byte{[]byte("foo"), []byte("bar")},
			haystack: "foofoofoo",
			start:    0,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teddy := NewTeddy(tt.patterns, nil)
			if teddy == nil {
				t.Fatal("NewTeddy() returned nil")
			}

			got := teddy.Find([]byte(tt.haystack), tt.start)
			if got != tt.want {
				t.Errorf("Find() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestTeddy_Find_LargeHaystack tests search in larger inputs
func TestTeddy_Find_LargeHaystack(t *testing.T) {
	patterns := [][]byte{
		[]byte("ERROR"),
		[]byte("WARNING"),
		[]byte("FATAL"),
	}

	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy() returned nil")
	}

	// Build large haystack with pattern near the end
	haystack := make([]byte, 10000)
	for i := range haystack {
		haystack[i] = 'x'
	}
	copy(haystack[9500:], "some ERROR message")

	got := teddy.Find(haystack, 0)
	if got != 9505 {
		t.Errorf("Find() = %d, want 9505", got)
	}
}

// TestTeddy_Find_ShortHaystack tests scalar fallback for haystacks < 16 bytes
func TestTeddy_Find_ShortHaystack(t *testing.T) {
	patterns := [][]byte{[]byte("foo"), []byte("bar")}
	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy() returned nil")
	}

	tests := []struct {
		haystack string
		want     int
	}{
		{"foo", 0},
		{"bar", 0},
		{"abcfoo", 3},
		{"xbarx", 1},
		{"nope", -1},
		{"fo", -1},          // too short to match
		{"fooba", 0},        // matches "foo"
		{"12345678901", -1}, // exactly 11 bytes, no match
	}

	for _, tt := range tests {
		t.Run(tt.haystack, func(t *testing.T) {
			got := teddy.Find([]byte(tt.haystack), 0)
			if got != tt.want {
				t.Errorf("Find(%q) = %d, want %d", tt.haystack, got, tt.want)
			}
		})
	}
}

// TestTeddy_Correctness_vs_Naive tests Teddy correctness against naive search
func TestTeddy_Correctness_vs_Naive(t *testing.T) {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy() returned nil")
	}

	// Naive multi-pattern search (for correctness baseline)
	naiveFind := func(haystack []byte, patterns [][]byte) int {
		minPos := -1
		for _, pattern := range patterns {
			pos := bytes.Index(haystack, pattern)
			if pos != -1 && (minPos == -1 || pos < minPos) {
				minPos = pos
			}
		}
		return minPos
	}

	haystacks := []string{
		"hello foo world",
		"test bar test",
		"baz at start",
		"qux at end",
		"no match here",
		"multiple foo bar foo",
		"edge case qux",
		string(make([]byte, 1000)) + "foo",
		"repeated foobarfoobarfoobar",
	}

	for _, haystack := range haystacks {
		t.Run(haystack[:minInt(len(haystack), 20)], func(t *testing.T) {
			teddyResult := teddy.Find([]byte(haystack), 0)
			naiveResult := naiveFind([]byte(haystack), patterns)

			if teddyResult != naiveResult {
				t.Errorf("Teddy=%d, Naive=%d, haystack=%q",
					teddyResult, naiveResult, haystack)
			}
		})
	}
}

// TestTeddy_IsComplete tests completeness flag
func TestTeddy_IsComplete(t *testing.T) {
	patterns := [][]byte{[]byte("foo"), []byte("bar")}
	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy() returned nil")
	}

	// Teddy is always complete because Find() verifies full pattern matches
	if !teddy.IsComplete() {
		t.Error("IsComplete() = false, want true (Teddy always verifies)")
	}

	// LiteralLen returns the uniform length for same-length patterns
	if teddy.LiteralLen() != 3 {
		t.Errorf("LiteralLen() = %d, want 3", teddy.LiteralLen())
	}

	// Test non-uniform length patterns - IsComplete still true, but LiteralLen=0
	nonUniformPatterns := [][]byte{[]byte("foo"), []byte("bar"), []byte("hello")}
	nonUniformTeddy := NewTeddy(nonUniformPatterns, nil)
	if nonUniformTeddy == nil {
		t.Fatal("NewTeddy(nonUniform) returned nil")
	}
	// IsComplete is still true because Teddy.Find() verifies full pattern
	if !nonUniformTeddy.IsComplete() {
		t.Error("IsComplete() = false for non-uniform, want true")
	}
	// But LiteralLen is 0 for non-uniform lengths
	if nonUniformTeddy.LiteralLen() != 0 {
		t.Errorf("LiteralLen() = %d for non-uniform, want 0", nonUniformTeddy.LiteralLen())
	}
}

// TestTeddy_HeapBytes tests memory usage reporting
func TestTeddy_HeapBytes(t *testing.T) {
	patterns := [][]byte{[]byte("foo"), []byte("bar"), []byte("baz")}
	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy() returned nil")
	}

	heapBytes := teddy.HeapBytes()

	// Should be reasonable: masks (264) + patterns (9) + buckets (~100)
	// Total should be < 1KB
	if heapBytes <= 0 || heapBytes > 1024 {
		t.Errorf("HeapBytes() = %d, want in range (0, 1024]", heapBytes)
	}

	t.Logf("HeapBytes = %d bytes (masks + patterns + buckets)", heapBytes)
}

// TestTeddy_MaskConstruction tests internal mask building
func TestTeddy_MaskConstruction(t *testing.T) {
	patterns := [][]byte{
		[]byte("foo"), // 0x66, 0x6F, 0x6F
		[]byte("bar"), // 0x62, 0x61, 0x72
	}

	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy() returned nil")
	}

	// Check that masks were built
	if teddy.masks == nil {
		t.Fatal("masks is nil")
	}

	// Check fingerprint length
	if teddy.masks.fingerprintLen != 1 {
		t.Errorf("fingerprintLen = %d, want 1", teddy.masks.fingerprintLen)
	}

	// Check buckets were assigned
	if len(teddy.buckets) == 0 {
		t.Error("buckets is empty")
	}

	// Verify patterns were copied (not aliased)
	patterns[0][0] = 'x'
	if teddy.patterns[0][0] == 'x' {
		t.Error("patterns were aliased (not copied)")
	}
}

// minInt returns the minimum of two integers
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
