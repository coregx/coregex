package meta

import (
	"regexp"
	"testing"
)

// BenchmarkReverseSuffixSet_Find benchmarks coregex vs stdlib for multi-suffix patterns
// Pattern: .*\.(txt|log|md) - suffix alternation where LCS is empty
// This optimization is NOT present in rust-regex (they fall back to Core strategy)
func BenchmarkReverseSuffixSet_Find(b *testing.B) {
	makeHaystack := func(size int, suffix string) []byte {
		result := make([]byte, size)
		for i := range result {
			result[i] = 'x'
		}
		// Place suffix near the end (greedy .* will match from start)
		pos := size - len(suffix) - 10
		if pos > 0 && pos+len(suffix) <= size {
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
		{"suffix_alt_1KB", `.*\.(txt|log|md)`, ".txt", 1024},
		{"suffix_alt_32KB", `.*\.(txt|log|md)`, ".log", 32 * 1024},
		{"suffix_alt_1MB", `.*\.(txt|log|md)`, ".md", 1024 * 1024},
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

// BenchmarkReverseSuffixSet_IsMatch benchmarks IsMatch for multi-suffix patterns
func BenchmarkReverseSuffixSet_IsMatch(b *testing.B) {
	makeHaystack := func(size int, suffix string) []byte {
		result := make([]byte, size)
		for i := range result {
			result[i] = 'x'
		}
		pos := size - len(suffix) - 10
		if pos > 0 && pos+len(suffix) <= size {
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
		{"suffix_alt_1KB", `.*\.(txt|log|md)`, ".txt", 1024},
		{"suffix_alt_32KB", `.*\.(txt|log|md)`, ".log", 32 * 1024},
		{"suffix_alt_1MB", `.*\.(txt|log|md)`, ".md", 1024 * 1024},
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

// BenchmarkReverseSuffixSet_FindAll benchmarks FindAll for multi-suffix patterns
// Note: This test uses public Regex API since Engine doesn't expose FindAll directly
func BenchmarkReverseSuffixSet_FindAll(b *testing.B) {
	b.Skip("FindAll not available on Engine - use public API benchmarks in root package")
}

// BenchmarkReverseSuffixSet_Variations tests different suffix alternation patterns
func BenchmarkReverseSuffixSet_Variations(b *testing.B) {
	haystack := make([]byte, 1024)
	for i := range haystack {
		haystack[i] = 'a'
	}
	// Add suffix near end
	copy(haystack[len(haystack)-20:], "config.json")

	patterns := []struct {
		name    string
		pattern string
	}{
		{"2_suffixes", `.*\.(json|yaml)`},
		{"3_suffixes", `.*\.(json|yaml|toml)`},
		{"4_suffixes", `.*\.(json|yaml|toml|xml)`},
		{"extensions", `.*\.(txt|log|md|rst)`},
		{"configs", `.*\.(json|yaml|yml|toml|ini)`},
	}

	for _, p := range patterns {
		// Stdlib
		b.Run("stdlib_"+p.name, func(b *testing.B) {
			re := regexp.MustCompile(p.pattern)
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				re.Match(haystack)
			}
		})

		// Coregex
		b.Run("coregex_"+p.name, func(b *testing.B) {
			engine, err := Compile(p.pattern)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				engine.IsMatch(haystack)
			}
		})
	}
}
