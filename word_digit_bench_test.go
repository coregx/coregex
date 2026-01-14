package coregex

import (
	"bytes"
	"regexp"
	"testing"
)

// Generate 1MB of test data
func generateBenchData() []byte {
	var buf bytes.Buffer
	patterns := []string{
		"hello world ", "test123 ", "foo456bar ", "abc ", "xyz789 ",
		"quick brown fox ", "lazy dog ", "word42 ", "sample99text ",
	}
	for buf.Len() < 1024*1024 {
		for _, p := range patterns {
			buf.WriteString(p)
		}
	}
	return buf.Bytes()
}

var benchData = generateBenchData()

func BenchmarkWordDigit_1MB_Stdlib(b *testing.B) {
	re := regexp.MustCompile(`\w+[0-9]+`)
	b.SetBytes(int64(len(benchData)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.FindAllIndex(benchData, -1)
	}
}

func BenchmarkWordDigit_1MB_Coregex(b *testing.B) {
	re := MustCompile(`\w+[0-9]+`)
	b.SetBytes(int64(len(benchData)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.FindAllIndex(benchData, -1)
	}
}

func BenchmarkAlphaDigit_1MB_Stdlib(b *testing.B) {
	re := regexp.MustCompile(`[a-zA-Z]+[0-9]+`)
	b.SetBytes(int64(len(benchData)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.FindAllIndex(benchData, -1)
	}
}

func BenchmarkAlphaDigit_1MB_Coregex(b *testing.B) {
	re := MustCompile(`[a-zA-Z]+[0-9]+`)
	b.SetBytes(int64(len(benchData)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.FindAllIndex(benchData, -1)
	}
}

// Compact API benchmarks - zero per-match allocations
func BenchmarkWordDigit_1MB_CoregexCompact(b *testing.B) {
	re := MustCompile(`\w+[0-9]+`)
	b.SetBytes(int64(len(benchData)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.FindAllIndexCompact(benchData, -1, nil)
	}
}

func BenchmarkWordDigit_1MB_CoregexCompactReuse(b *testing.B) {
	re := MustCompile(`\w+[0-9]+`)
	results := make([][2]int, 0, 65536)
	b.SetBytes(int64(len(benchData)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results = re.FindAllIndexCompact(benchData, -1, results)
	}
}
