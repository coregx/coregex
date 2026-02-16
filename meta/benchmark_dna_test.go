package meta

import (
	"fmt"
	"regexp"
	"testing"
)

// BenchmarkDNA benchmarks all 9 regexdna patterns on 1MB DNA input.
// This is the primary performance regression benchmark for DNA workloads.
//
// Each sub-benchmark measures FindAllIndicesStreaming throughput for one pattern.
// Use benchstat to compare across releases:
//
//	go test -bench=BenchmarkDNA/dna_ -benchmem -count=5 ./meta/... > new.txt
//	benchstat old.txt new.txt
func BenchmarkDNA(b *testing.B) {
	data := generateDNA(1024 * 1024) // 1MB

	for _, p := range dnaPatterns {
		b.Run(p.name, func(b *testing.B) {
			engine, err := Compile(p.pattern)
			if err != nil {
				b.Fatalf("Compile(%q) failed: %v", p.pattern, err)
			}

			// Log strategy for diagnostics
			b.Logf("strategy=%s", engine.Strategy())

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = engine.FindAllIndicesStreaming(data, -1, nil)
			}
		})
	}
}

// BenchmarkDNA_SizeScaling benchmarks the worst-case DNA pattern (dna_4) across
// multiple input sizes to detect non-linear scaling regressions.
//
// dna_4 (`ag[act]gtaaa|tttac[agt]ct`) is selected because it exercises the
// DFA/NFA boundary most heavily with its character class + alternation structure.
func BenchmarkDNA_SizeScaling(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"64KB", 64 * 1024},
		{"1MB", 1024 * 1024},
		{"4MB", 4 * 1024 * 1024},
	}

	pattern := `ag[act]gtaaa|tttac[agt]ct` // dna_4, worst case

	engine, err := Compile(pattern)
	if err != nil {
		b.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	b.Logf("pattern=%s strategy=%s", pattern, engine.Strategy())

	for _, sz := range sizes {
		data := generateDNA(sz.size)

		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = engine.FindAllIndicesStreaming(data, -1, nil)
			}
		})
	}
}

// BenchmarkDNA_VsStdlib provides direct comparison between coregex and stdlib
// for all 9 DNA patterns on 1MB input. Use benchstat to analyze:
//
//	go test -bench=BenchmarkDNA_VsStdlib -benchmem -count=5 ./meta/... > dna.txt
//	benchstat -filter '.name:/coregex/' -filter '.name:/stdlib/' dna.txt
func BenchmarkDNA_VsStdlib(b *testing.B) {
	data := generateDNA(1024 * 1024) // 1MB

	for _, p := range dnaPatterns {
		b.Run(fmt.Sprintf("coregex/%s", p.name), func(b *testing.B) {
			engine, err := Compile(p.pattern)
			if err != nil {
				b.Fatalf("Compile(%q) failed: %v", p.pattern, err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = engine.FindAllIndicesStreaming(data, -1, nil)
			}
		})

		b.Run(fmt.Sprintf("stdlib/%s", p.name), func(b *testing.B) {
			re := regexp.MustCompile(p.pattern)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = re.FindAllIndex(data, -1)
			}
		})
	}
}
