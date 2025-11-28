package meta

import (
	"regexp"
	"testing"
)

// BenchmarkReverseSuffix_Comparison benchmarks coregex vs stdlib for ReverseSuffix patterns
func BenchmarkReverseSuffix_Comparison(b *testing.B) {
	// Create haystacks of different sizes
	makeHaystack := func(size int, suffix string) []byte {
		result := make([]byte, size)
		for i := range result {
			result[i] = 'x'
		}
		// Place suffix near the middle
		pos := size / 2
		if pos+len(suffix) <= size {
			copy(result[pos:], suffix)
		}
		return result
	}

	benchmarks := []struct {
		name    string
		pattern string
		suffix  string
		size    int
	}{
		{"txt_1KB", `.*\.txt`, ".txt", 1024},
		{"txt_32KB", `.*\.txt`, ".txt", 32 * 1024},
		{"txt_1MB", `.*\.txt`, ".txt", 1024 * 1024},
	}

	for _, bm := range benchmarks {
		haystack := makeHaystack(bm.size, bm.suffix)

		// Stdlib benchmark
		b.Run("stdlib_"+bm.name, func(b *testing.B) {
			re := regexp.MustCompile(bm.pattern)
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				match := re.Find(haystack)
				if match == nil {
					b.Fatal("no match")
				}
			}
		})

		// Coregex benchmark
		b.Run("coregex_"+bm.name, func(b *testing.B) {
			engine, err := Compile(bm.pattern)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				match := engine.Find(haystack)
				if match == nil {
					b.Fatal("no match")
				}
			}
		})
	}
}

// BenchmarkReverseSuffix_IsMatch_Comparison benchmarks IsMatch for coregex vs stdlib
func BenchmarkReverseSuffix_IsMatch_Comparison(b *testing.B) {
	makeHaystack := func(size int, suffix string) []byte {
		result := make([]byte, size)
		for i := range result {
			result[i] = 'x'
		}
		pos := size / 2
		if pos+len(suffix) <= size {
			copy(result[pos:], suffix)
		}
		return result
	}

	benchmarks := []struct {
		name    string
		pattern string
		suffix  string
		size    int
	}{
		{"txt_1KB", `.*\.txt`, ".txt", 1024},
		{"txt_32KB", `.*\.txt`, ".txt", 32 * 1024},
		{"txt_1MB", `.*\.txt`, ".txt", 1024 * 1024},
	}

	for _, bm := range benchmarks {
		haystack := makeHaystack(bm.size, bm.suffix)

		// Stdlib benchmark
		b.Run("stdlib_"+bm.name, func(b *testing.B) {
			re := regexp.MustCompile(bm.pattern)
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if !re.Match(haystack) {
					b.Fatal("no match")
				}
			}
		})

		// Coregex benchmark
		b.Run("coregex_"+bm.name, func(b *testing.B) {
			engine, err := Compile(bm.pattern)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if !engine.IsMatch(haystack) {
					b.Fatal("no match")
				}
			}
		})
	}
}
