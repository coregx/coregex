package prefilter

import (
	"bytes"
	"testing"
)

// Naive multi-pattern search for baseline comparison
func naiveMultiPattern(haystack []byte, patterns [][]byte) int {
	minPos := -1
	for _, pattern := range patterns {
		pos := bytes.Index(haystack, pattern)
		if pos != -1 && (minPos == -1 || pos < minPos) {
			minPos = pos
		}
	}
	return minPos
}

// BenchmarkTeddy_vs_Naive benchmarks Teddy against naive multi-pattern search
func BenchmarkTeddy_vs_Naive(b *testing.B) {
	b.ReportAllocs()

	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	sizes := []int{256, 1024, 4096, 65536}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'x'
		}
		// Place pattern near the end
		copy(haystack[size-50:], "some foo here")

		// Benchmark Teddy
		b.Run("Teddy_"+string(rune(size/1024))+"KB", func(b *testing.B) {
			teddy := NewTeddy(patterns, nil)
			if teddy == nil {
				b.Skip("Teddy not available")
			}
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = teddy.Find(haystack, 0)
			}
		})

		// Benchmark Naive
		b.Run("Naive_"+string(rune(size/1024))+"KB", func(b *testing.B) {
			b.SetBytes(int64(size))

			for i := 0; i < b.N; i++ {
				_ = naiveMultiPattern(haystack, patterns)
			}
		})
	}
}

// BenchmarkTeddy_PatternCount benchmarks Teddy with different pattern counts
func BenchmarkTeddy_PatternCount(b *testing.B) {
	b.ReportAllocs()

	haystack := make([]byte, 4096)
	for i := range haystack {
		haystack[i] = 'x'
	}
	copy(haystack[2000:], "target")

	patternCounts := []int{2, 4, 6, 8}

	for _, count := range patternCounts {
		patterns := make([][]byte, count)
		for i := 0; i < count-1; i++ {
			patterns[i] = []byte("nop" + string(rune('a'+i)))
		}
		patterns[count-1] = []byte("target") // Last pattern is the match

		b.Run("Patterns_"+string(rune(count+'0')), func(b *testing.B) {
			teddy := NewTeddy(patterns, nil)
			if teddy == nil {
				b.Skip("Teddy not available")
			}
			b.SetBytes(4096)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = teddy.Find(haystack, 0)
			}
		})
	}
}

// BenchmarkTeddy_MatchPosition benchmarks with match at different positions
func BenchmarkTeddy_MatchPosition(b *testing.B) {
	b.ReportAllocs()

	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
	}

	haystack := make([]byte, 4096)
	for i := range haystack {
		haystack[i] = 'x'
	}

	positions := []struct {
		name string
		pos  int
	}{
		{"Start", 0},
		{"Early", 100},
		{"Middle", 2048},
		{"Late", 4000},
	}

	for _, pos := range positions {
		haystackCopy := make([]byte, len(haystack))
		copy(haystackCopy, haystack)
		copy(haystackCopy[pos.pos:], "foo")

		b.Run("Match_"+pos.name, func(b *testing.B) {
			b.ReportAllocs()
			teddy := NewTeddy(patterns, nil)
			if teddy == nil {
				b.Skip("Teddy not available")
			}
			b.SetBytes(4096)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = teddy.Find(haystackCopy, 0)
			}
		})
	}
}

// BenchmarkTeddy_NoMatch benchmarks the no-match case
func BenchmarkTeddy_NoMatch(b *testing.B) {
	b.ReportAllocs()

	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}

	haystack := make([]byte, 4096)
	for i := range haystack {
		haystack[i] = 'x'
	}

	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		b.Skip("Teddy not available")
	}

	b.SetBytes(4096)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = teddy.Find(haystack, 0)
	}
}
