package prefilter

import (
	"bytes"
	"fmt"
	"testing"
)

func TestFatTeddyBasic(t *testing.T) {
	// Create 40 patterns (requires Fat Teddy with 16 buckets)
	patterns := make([][]byte, 40)
	for i := 0; i < 40; i++ {
		patterns[i] = []byte(fmt.Sprintf("pattern%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("NewFatTeddy returned nil")
	}

	// Test finding each pattern
	for i, pattern := range patterns {
		haystack := []byte("some text with " + string(pattern) + " in it")
		pos := ft.Find(haystack, 0)
		if pos == -1 {
			t.Errorf("pattern[%d] %q not found", i, pattern)
		}
		if pos != 15 { // "some text with " = 15 chars
			t.Errorf("pattern[%d] found at %d, expected 15", i, pos)
		}
	}
}

func TestFatTeddyNoMatch(t *testing.T) {
	patterns := make([][]byte, 50)
	for i := 0; i < 50; i++ {
		patterns[i] = []byte(fmt.Sprintf("pattern%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("NewFatTeddy returned nil")
	}

	// Search in text that doesn't contain any pattern
	haystack := []byte("this text has no matching content at all")
	pos := ft.Find(haystack, 0)
	if pos != -1 {
		t.Errorf("expected -1, got %d", pos)
	}
}

func TestFatTeddyMultipleMatches(t *testing.T) {
	patterns := make([][]byte, 35)
	for i := 0; i < 35; i++ {
		patterns[i] = []byte(fmt.Sprintf("pat%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("NewFatTeddy returned nil")
	}

	// Haystack with multiple patterns
	haystack := []byte("start pat00 middle pat15 and pat34 end")

	// First match should be pat00 at position 6
	pos := ft.Find(haystack, 0)
	if pos != 6 {
		t.Errorf("first match at %d, expected 6", pos)
	}

	// Continue from after first match
	pos = ft.Find(haystack, 11) // after "pat00"
	if pos != 19 {              // "start pat00 middle " = 19
		t.Errorf("second match at %d, expected 19", pos)
	}
}

func TestFatTeddyFindMatch(t *testing.T) {
	patterns := make([][]byte, 40)
	for i := 0; i < 40; i++ {
		patterns[i] = []byte(fmt.Sprintf("word%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("NewFatTeddy returned nil")
	}

	haystack := []byte("prefix word25 suffix")
	start, end := ft.FindMatch(haystack, 0)

	if start != 7 || end != 13 {
		t.Errorf("FindMatch returned (%d, %d), expected (7, 13)", start, end)
	}

	// Verify the match
	if string(haystack[start:end]) != "word25" {
		t.Errorf("match is %q, expected word25", haystack[start:end])
	}
}

func TestFatTeddyCorrectnessVsScalar(t *testing.T) {
	// Create 50 patterns
	patterns := make([][]byte, 50)
	for i := 0; i < 50; i++ {
		patterns[i] = []byte(fmt.Sprintf("test%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("NewFatTeddy returned nil")
	}

	// Test with various haystacks
	testCases := []string{
		"no matches here",
		"test00 at start",
		"end with test49",
		"middle test25 middle",
		"test00 test25 test49 multiple",
		"",
		"short",
	}

	for _, tc := range testCases {
		haystack := []byte(tc)

		// Find using Fat Teddy
		pos := ft.Find(haystack, 0)

		// Find using scalar reference
		expectedPos := -1
		for i := 0; i < len(haystack); i++ {
			for _, pattern := range patterns {
				if i+len(pattern) <= len(haystack) {
					if bytes.Equal(haystack[i:i+len(pattern)], pattern) {
						expectedPos = i
						break
					}
				}
			}
			if expectedPos != -1 {
				break
			}
		}

		if pos != expectedPos {
			t.Errorf("haystack %q: got %d, expected %d", tc, pos, expectedPos)
		}
	}
}

func TestFatTeddyTooFewPatterns(t *testing.T) {
	// Less than 2 patterns should return nil
	patterns := [][]byte{[]byte("only one")}
	ft := NewFatTeddy(patterns, nil)
	if ft != nil {
		t.Error("expected nil for 1 pattern")
	}
}

func TestFatTeddyTooManyPatterns(t *testing.T) {
	// More than 64 patterns should return nil
	patterns := make([][]byte, 100)
	for i := 0; i < 100; i++ {
		patterns[i] = []byte(fmt.Sprintf("pattern%03d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft != nil {
		t.Error("expected nil for 100 patterns")
	}
}

func TestFatTeddyShortPatterns(t *testing.T) {
	// Patterns shorter than 3 bytes should return nil
	patterns := [][]byte{[]byte("ab"), []byte("cd")}
	ft := NewFatTeddy(patterns, nil)
	if ft != nil {
		t.Error("expected nil for 2-byte patterns")
	}
}

func TestFatTeddyBucketDistribution(t *testing.T) {
	// Verify patterns are distributed across all 16 buckets
	patterns := make([][]byte, 64)
	for i := 0; i < 64; i++ {
		patterns[i] = []byte(fmt.Sprintf("patt%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("NewFatTeddy returned nil")
	}

	// Check all 16 buckets have patterns
	totalPatterns := 0
	for i, bucket := range ft.buckets {
		if len(bucket) == 0 {
			t.Errorf("bucket %d is empty", i)
		}
		totalPatterns += len(bucket)
	}

	if totalPatterns != 64 {
		t.Errorf("total patterns in buckets: %d, expected 64", totalPatterns)
	}

	if len(ft.buckets) != NumBucketsFat {
		t.Errorf("bucket count: %d, expected %d", len(ft.buckets), NumBucketsFat)
	}
}

func TestFatTeddyIsComplete(t *testing.T) {
	patterns := make([][]byte, 40)
	for i := 0; i < 40; i++ {
		patterns[i] = []byte(fmt.Sprintf("word%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("NewFatTeddy returned nil")
	}

	// All patterns are exact literals, so IsComplete should be true
	if !ft.IsComplete() {
		t.Error("expected IsComplete() to be true")
	}

	// All patterns have same length (6), so LiteralLen should return 6
	if ft.LiteralLen() != 6 {
		t.Errorf("LiteralLen() = %d, expected 6", ft.LiteralLen())
	}
}

func BenchmarkFatTeddy50Patterns(b *testing.B) {
	patterns := make([][]byte, 50)
	for i := 0; i < 50; i++ {
		patterns[i] = []byte(fmt.Sprintf("pattern%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		b.Fatal("NewFatTeddy returned nil")
	}

	// Create 1MB haystack with one pattern in the middle
	haystack := make([]byte, 1024*1024)
	for i := range haystack {
		haystack[i] = byte('a' + i%26)
	}
	copy(haystack[512*1024:], patterns[25]) // Put pattern25 in the middle

	b.ResetTimer()
	b.SetBytes(int64(len(haystack)))

	for i := 0; i < b.N; i++ {
		ft.Find(haystack, 0)
	}
}

func BenchmarkFatTeddy64Patterns(b *testing.B) {
	patterns := make([][]byte, 64)
	for i := 0; i < 64; i++ {
		patterns[i] = []byte(fmt.Sprintf("pattern%02d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		b.Fatal("NewFatTeddy returned nil")
	}

	// Create 1MB haystack with no matches
	haystack := make([]byte, 1024*1024)
	for i := range haystack {
		haystack[i] = byte('a' + i%26)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(haystack)))

	for i := 0; i < b.N; i++ {
		ft.Find(haystack, 0)
	}
}

func BenchmarkFatTeddyVsSlimTeddy(b *testing.B) {
	for _, n := range []int{8, 16, 24, 32, 40, 48, 56, 64} {
		b.Run(fmt.Sprintf("patterns_%d", n), func(b *testing.B) {
			patterns := make([][]byte, n)
			for i := 0; i < n; i++ {
				patterns[i] = []byte(fmt.Sprintf("patt%02d", i))
			}

			var finder interface {
				Find([]byte, int) int
			}

			if n <= 32 {
				finder = NewTeddy(patterns, nil)
			} else {
				finder = NewFatTeddy(patterns, nil)
			}

			if finder == nil {
				b.Fatalf("failed to create finder for %d patterns", n)
			}

			haystack := make([]byte, 64*1024)
			for i := range haystack {
				haystack[i] = byte('x' + i%3)
			}

			b.ResetTimer()
			b.SetBytes(int64(len(haystack)))

			for i := 0; i < b.N; i++ {
				finder.Find(haystack, 0)
			}
		})
	}
}
