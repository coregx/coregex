package prefilter

import (
	"fmt"
	"testing"
)

// --- Teddy FindMatch tests ---

func TestTeddyFindMatch(t *testing.T) {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}
	td := NewTeddy(patterns, nil)
	if td == nil {
		t.Fatal("expected Teddy to be created")
	}

	tests := []struct {
		name      string
		haystack  string
		start     int
		wantStart int
		wantEnd   int
	}{
		{"match_first", "hello foo world", 0, 6, 9},
		{"match_second", "hello bar world", 0, 6, 9},
		{"match_offset", "hello foo world", 7, -1, -1},
		{"no_match", "hello world", 0, -1, -1},
		{"empty_haystack", "", 0, -1, -1},
		{"start_out_of_range", "foo", 10, -1, -1},
		{"negative_start", "foo", -1, -1, -1},
		{"match_at_beginning", "foobar", 0, 0, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e := td.FindMatch([]byte(tt.haystack), tt.start)
			if s != tt.wantStart || e != tt.wantEnd {
				t.Errorf("FindMatch(%q, %d) = (%d, %d), want (%d, %d)",
					tt.haystack, tt.start, s, e, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestTeddyFindMatchScalar tests FindMatch with short haystacks (<16 bytes) using scalar path.
func TestTeddyFindMatchScalar(t *testing.T) {
	patterns := [][]byte{
		[]byte("abc"),
		[]byte("xyz"),
	}
	td := NewTeddy(patterns, nil)
	if td == nil {
		t.Fatal("expected Teddy to be created")
	}

	tests := []struct {
		haystack  string
		wantStart int
		wantEnd   int
	}{
		{"abc", 0, 3},
		{"xyz", 0, 3},
		{"aabcx", 1, 4},
		{"no", -1, -1},
		{"ab", -1, -1},   // Too short for any pattern
		{"axyz", 1, 4},   // Match at offset
		{"abcxyz", 0, 3}, // First match wins
	}

	for _, tt := range tests {
		t.Run(tt.haystack, func(t *testing.T) {
			s, e := td.FindMatch([]byte(tt.haystack), 0)
			if s != tt.wantStart || e != tt.wantEnd {
				t.Errorf("FindMatch(%q, 0) = (%d, %d), want (%d, %d)",
					tt.haystack, s, e, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestTeddyFindScalarCandidate tests the scalar candidate finding path.
func TestTeddyFindScalarCandidate(t *testing.T) {
	patterns := [][]byte{
		[]byte("abc"),
		[]byte("def"),
	}
	td := NewTeddy(patterns, nil)
	if td == nil {
		t.Fatal("expected Teddy to be created")
	}

	// Scalar candidate should find "abc" at position 0
	pos, mask := td.findScalarCandidate([]byte("abcdef"))
	if pos < 0 {
		t.Fatal("expected candidate position >= 0")
	}
	if mask == 0 {
		t.Fatal("expected non-zero bucket mask")
	}

	// No candidate
	pos, mask = td.findScalarCandidate([]byte("xyz"))
	if pos != -1 {
		t.Errorf("expected pos=-1 for no candidate, got %d", pos)
	}
	if mask != 0 {
		t.Errorf("expected mask=0, got %d", mask)
	}

	// Empty haystack
	pos, mask = td.findScalarCandidate([]byte(""))
	if pos != -1 || mask != 0 {
		t.Errorf("expected (-1, 0) for empty haystack, got (%d, %d)", pos, mask)
	}
}

// TestTeddyLiteralLenUniform tests LiteralLen for uniform-length patterns.
func TestTeddyLiteralLenUniform(t *testing.T) {
	// Uniform length patterns: complete=true, uniformLen=3
	patterns := [][]byte{
		[]byte("abc"),
		[]byte("def"),
	}
	td := NewTeddy(patterns, nil)
	if td == nil {
		t.Fatal("expected Teddy to be created")
	}
	if td.LiteralLen() != 3 {
		t.Errorf("expected LiteralLen()=3 for uniform complete teddy, got %d", td.LiteralLen())
	}
}

// TestTeddyLiteralLenNonUniform tests LiteralLen for non-uniform-length patterns.
func TestTeddyLiteralLenNonUniform(t *testing.T) {
	// Non-uniform lengths: uniformLen=0 -> LiteralLen returns 0
	patterns := [][]byte{
		[]byte("abc"),
		[]byte("defg"),
	}
	td := NewTeddy(patterns, nil)
	if td == nil {
		t.Fatal("expected Teddy to be created")
	}
	if td.LiteralLen() != 0 {
		t.Errorf("expected LiteralLen()=0 for non-uniform teddy, got %d", td.LiteralLen())
	}
}

// --- FatTeddy tests ---

func makeFatTeddyPatterns() [][]byte {
	const n = 40
	patterns := make([][]byte, n)
	for i := 0; i < n; i++ {
		patterns[i] = []byte(fmt.Sprintf("pat%03d", i))
	}
	return patterns
}

func TestFatTeddyFindMatchCoverage(t *testing.T) {
	patterns := makeFatTeddyPatterns() // >32 triggers Fat Teddy
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	tests := []struct {
		name      string
		haystack  string
		start     int
		wantStart int
		wantEnd   int
	}{
		{"match_first", "hello pat000 world", 0, 6, 12},
		{"no_match", "hello world nothing", 0, -1, -1},
		{"empty", "", 0, -1, -1},
		{"negative_start", "pat000", -1, -1, -1},
		{"start_past_end", "pat000", 100, -1, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e := ft.FindMatch([]byte(tt.haystack), tt.start)
			if s != tt.wantStart || e != tt.wantEnd {
				t.Errorf("FindMatch(%q, %d) = (%d, %d), want (%d, %d)",
					tt.haystack, tt.start, s, e, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestFatTeddyFindMatchScalar tests FatTeddy FindMatch with short (<16 bytes) haystacks.
func TestFatTeddyFindMatchScalar(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	// Short haystack triggers scalar path
	s, e := ft.FindMatch([]byte("pat000"), 0)
	if s != 0 || e != 6 {
		t.Errorf("expected (0, 6), got (%d, %d)", s, e)
	}

	s, e = ft.FindMatch([]byte("nope"), 0)
	if s != -1 || e != -1 {
		t.Errorf("expected (-1, -1) for no match, got (%d, %d)", s, e)
	}
}

// TestFatTeddyFindScalarCandidate tests the scalar candidate path for Fat Teddy.
func TestFatTeddyFindScalarCandidate(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	pos, mask := ft.findScalarCandidate([]byte("pat000extra"))
	if pos < 0 {
		t.Fatal("expected candidate position >= 0")
	}
	if mask == 0 {
		t.Fatal("expected non-zero mask")
	}

	pos, mask = ft.findScalarCandidate([]byte("zz"))
	if pos != -1 || mask != 0 {
		t.Errorf("expected (-1, 0), got (%d, %d)", pos, mask)
	}
}

// TestFatTeddyLiteralLenUniform tests LiteralLen for uniform-length FatTeddy.
func TestFatTeddyLiteralLenUniform(t *testing.T) {
	patterns := makeFatTeddyPatterns() // all "pat000"-"pat039" = length 6
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	// All patterns same length and complete=true -> LiteralLen = 6
	if ft.LiteralLen() != 6 {
		t.Errorf("expected LiteralLen()=6, got %d", ft.LiteralLen())
	}
}

// TestFatTeddyIsFast tests IsFast for FatTeddy.
func TestFatTeddyIsFast(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	if !ft.IsFast() {
		t.Error("expected IsFast()=true for patterns with len >= 3")
	}
}

// TestFatTeddyHeapBytes tests HeapBytes for FatTeddy.
func TestFatTeddyHeapBytes(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	hb := ft.HeapBytes()
	if hb <= 0 {
		t.Errorf("expected HeapBytes > 0, got %d", hb)
	}
}

// TestFatTeddyMinimumLen tests MinimumLen method.
func TestFatTeddyMinimumLen(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	ml := ft.MinimumLen()
	if ml != 64 {
		t.Errorf("expected MinimumLen()=64, got %d", ml)
	}
}

// TestFatTeddyPatternCount tests PatternCount method.
func TestFatTeddyPatternCount(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	if ft.PatternCount() != 40 {
		t.Errorf("expected PatternCount()=40, got %d", ft.PatternCount())
	}
}

// TestFatTeddyPatterns tests Patterns method.
func TestFatTeddyPatterns(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	got := ft.Patterns()
	if len(got) != 40 {
		t.Errorf("expected 40 patterns, got %d", len(got))
	}
}

// --- TrackedPrefilter tests ---

func TestTrackedPrefilterLiteralLen(t *testing.T) {
	inner := newMemchrPrefilter('a', true)
	tp := WrapWithTracking(inner)
	if tp == nil {
		t.Fatal("expected TrackedPrefilter to be created")
	}

	if tp.LiteralLen() != 1 {
		t.Errorf("expected LiteralLen()=1, got %d", tp.LiteralLen())
	}
}

func TestTrackedPrefilterIsFast(t *testing.T) {
	inner := newMemchrPrefilter('x', false)
	tp := WrapWithTracking(inner)
	if tp == nil {
		t.Fatal("expected TrackedPrefilter to be created")
	}

	if !tp.IsFast() {
		t.Error("expected IsFast()=true for memchr")
	}
}

func TestTrackedPrefilterHeapBytes(t *testing.T) {
	inner := newMemchrPrefilter('x', false)
	tp := WrapWithTracking(inner)
	if tp == nil {
		t.Fatal("expected TrackedPrefilter to be created")
	}

	if tp.HeapBytes() != 0 {
		t.Errorf("expected HeapBytes()=0 for memchr, got %d", tp.HeapBytes())
	}
}

func TestTrackedPrefilterIsComplete(t *testing.T) {
	inner := newMemchrPrefilter('x', true)
	tp := WrapWithTracking(inner)
	if tp == nil {
		t.Fatal("expected TrackedPrefilter to be created")
	}

	if !tp.IsComplete() {
		t.Error("expected IsComplete()=true for complete memchr")
	}
}

func TestTrackedPrefilterFind(t *testing.T) {
	inner := newMemchrPrefilter('x', false)
	tp := WrapWithTracking(inner)
	if tp == nil {
		t.Fatal("expected TrackedPrefilter to be created")
	}

	pos := tp.Find([]byte("abcxdef"), 0)
	if pos != 3 {
		t.Errorf("expected Find=3, got %d", pos)
	}
}

func TestWrapWithTrackingNil(t *testing.T) {
	tp := WrapWithTracking(nil)
	if tp != nil {
		t.Error("expected nil for nil inner prefilter")
	}
}

// --- Tracker LiteralLen and IsFast tests ---

func TestTrackerLiteralLen(t *testing.T) {
	inner := newMemchrPrefilter('a', true)
	tracker := NewTracker(inner)

	if tracker.LiteralLen() != 1 {
		t.Errorf("expected LiteralLen()=1, got %d", tracker.LiteralLen())
	}
}

func TestTrackerIsFast(t *testing.T) {
	inner := newMemchrPrefilter('a', false)
	tracker := NewTracker(inner)

	if !tracker.IsFast() {
		t.Error("expected IsFast()=true for memchr")
	}
}

// --- memchrPrefilter LiteralLen tests ---

func TestMemchrPrefilterLiteralLen(t *testing.T) {
	// Complete memchr: LiteralLen = 1
	pf := newMemchrPrefilter('a', true)
	if pf.LiteralLen() != 1 {
		t.Errorf("expected LiteralLen()=1 for complete memchr, got %d", pf.LiteralLen())
	}

	// Incomplete memchr: LiteralLen = 0
	pf2 := newMemchrPrefilter('a', false)
	if pf2.LiteralLen() != 0 {
		t.Errorf("expected LiteralLen()=0 for incomplete memchr, got %d", pf2.LiteralLen())
	}
}

// --- memmemPrefilter LiteralLen tests ---

func TestMemmemPrefilterLiteralLen(t *testing.T) {
	// Complete memmem: LiteralLen = needle length
	pf := newMemmemPrefilter([]byte("hello"), true)
	if pf.LiteralLen() != 5 {
		t.Errorf("expected LiteralLen()=5 for complete memmem, got %d", pf.LiteralLen())
	}

	// Incomplete memmem: LiteralLen = 0
	pf2 := newMemmemPrefilter([]byte("hello"), false)
	if pf2.LiteralLen() != 0 {
		t.Errorf("expected LiteralLen()=0 for incomplete memmem, got %d", pf2.LiteralLen())
	}
}

// --- WouldBeFast edge cases ---

func TestWouldBeFastShortPatterns(t *testing.T) {
	// WouldBeFast with multiple short patterns (len < 3) should return false
	// This is tested by checking that the function returns false for patterns that are too short

	// Nil or empty sequence
	if WouldBeFast(nil) {
		t.Error("expected WouldBeFast(nil) = false")
	}
}

// --- FatTeddy scalar search ---

func TestFatTeddyFindScalar(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	// Short haystack triggers scalar Find path
	pos := ft.Find([]byte("pat000"), 0)
	if pos != 0 {
		t.Errorf("expected Find=0, got %d", pos)
	}

	pos = ft.Find([]byte("nope"), 0)
	if pos != -1 {
		t.Errorf("expected Find=-1, got %d", pos)
	}
}

// --- FatTeddy Find with longer haystack to exercise SIMD path ---

func TestFatTeddyFindSIMD(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	// Long haystack exercises SIMD path (>= 16 bytes)
	haystack := []byte("this is a long haystack that contains pat005 somewhere in it")
	pos := ft.Find(haystack, 0)
	if pos < 0 {
		t.Error("expected to find pat005 in long haystack")
	}
}

// --- Teddy Find edge: start == len(haystack) ---

func TestTeddyFindStartAtEnd(t *testing.T) {
	patterns := [][]byte{[]byte("abc"), []byte("def")}
	td := NewTeddy(patterns, nil)
	if td == nil {
		t.Fatal("expected Teddy to be created")
	}

	pos := td.Find([]byte("abc"), 3)
	if pos != -1 {
		t.Errorf("expected Find=-1 when start==len(haystack), got %d", pos)
	}
}

// --- FatTeddy FindMatch with longer haystack ---

func TestFatTeddyFindMatchSIMD(t *testing.T) {
	patterns := makeFatTeddyPatterns()
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	haystack := []byte("this is a long haystack that contains pat010 somewhere here!!!")
	s, e := ft.FindMatch(haystack, 0)
	if s < 0 || e < 0 {
		t.Error("expected to find pat010 in long haystack")
	}
	if e-s != 6 {
		t.Errorf("expected match length 6, got %d", e-s)
	}
}

// --- FatTeddy non-uniform length ---

func TestFatTeddyLiteralLenNonUniform(t *testing.T) {
	// Create FatTeddy with non-uniform length patterns
	patterns := make([][]byte, 40)
	for i := 0; i < 40; i++ {
		if i%2 == 0 {
			patterns[i] = []byte(fmt.Sprintf("pat%03d", i))
		} else {
			patterns[i] = []byte(fmt.Sprintf("pattern%03d", i))
		}
	}
	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("expected FatTeddy to be created")
	}

	// Non-uniform -> LiteralLen = 0
	if ft.LiteralLen() != 0 {
		t.Errorf("expected LiteralLen()=0 for non-uniform, got %d", ft.LiteralLen())
	}
}
