package nfa

import (
	"bytes"
	"testing"
)

// BenchmarkUnanchored_Linear verifies O(n) complexity for unanchored search
// Pattern that won't match forces full scan of input
func BenchmarkUnanchored_Linear(b *testing.B) {
	pattern := "ZZZZ$"
	nfa := mustCompileBench(b, pattern)
	vm := NewPikeVM(nfa)

	sizes := []int{1000, 2000, 4000, 8000, 16000}

	for _, size := range sizes {
		input := bytes.Repeat([]byte("X"), size)
		b.Run(string(rune(size)), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				vm.Search(input)
			}
		})
	}
}

// BenchmarkUnanchored_WorstCase tests worst case for unanchored search
func BenchmarkUnanchored_WorstCase(b *testing.B) {
	pattern := "foo"
	nfa := mustCompileBench(b, pattern)
	vm := NewPikeVM(nfa)

	sizes := []struct {
		name string
		size int
	}{
		{"16B", 16},
		{"64B", 64},
		{"256B", 256},
		{"1KB", 1024},
		{"4KB", 4096},
		{"16KB", 16384},
		{"64KB", 65536},
	}

	for _, tc := range sizes {
		// Input that doesn't match - forces full scan
		input := bytes.Repeat([]byte("X"), tc.size)
		b.Run(tc.name+"_no_match", func(b *testing.B) {
			b.SetBytes(int64(tc.size))
			for i := 0; i < b.N; i++ {
				vm.Search(input)
			}
		})

		// Input with match at end
		input = append(bytes.Repeat([]byte("X"), tc.size-3), []byte("foo")...)
		b.Run(tc.name+"_match_at_end", func(b *testing.B) {
			b.SetBytes(int64(tc.size))
			for i := 0; i < b.N; i++ {
				vm.Search(input)
			}
		})

		// Input with match at start
		input = append([]byte("foo"), bytes.Repeat([]byte("X"), tc.size-3)...)
		b.Run(tc.name+"_match_at_start", func(b *testing.B) {
			b.SetBytes(int64(tc.size))
			for i := 0; i < b.N; i++ {
				vm.Search(input)
			}
		})
	}
}

func mustCompileBench(b *testing.B, pattern string) *NFA {
	b.Helper()
	compiler := NewDefaultCompiler()
	nfa, err := compiler.Compile(pattern)
	if err != nil {
		b.Fatalf("compile %q: %v", pattern, err)
	}
	return nfa
}

// BenchmarkPikeVM_Captures tests COW captures optimization
func BenchmarkPikeVM_Captures(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
		input   string
	}{
		{"simple_1group", `(foo)`, "xxxfooyyy"},
		{"simple_3groups", `(\w+)-(\w+)-(\w+)`, "abc-def-ghi"},
		{"nested_groups", `(a(b(c)))`, "xxxabcyyy"},
		{"alternation_groups", `(foo|bar)-(baz|qux)`, "foo-baz"},
		{"quantifier_groups", `(\w+)+`, "abcdefghij"},
		{"many_splits", `(a|b)(c|d)(e|f)(g|h)`, "aceg"},
	}

	for _, tc := range benchmarks {
		nfa := mustCompileBench(b, tc.pattern)
		vm := NewPikeVM(nfa)
		input := []byte(tc.input)

		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				vm.SearchWithCaptures(input)
			}
		})
	}
}

// BenchmarkPikeVM_CapturesLargeInput tests COW with larger input and many potential match positions
func BenchmarkPikeVM_CapturesLargeInput(b *testing.B) {
	pattern := `(\d+)-(\d+)-(\d+)`
	nfa := mustCompileBench(b, pattern)
	vm := NewPikeVM(nfa)

	// Input with match near the end
	input := append(bytes.Repeat([]byte("xxx"), 1000), []byte("123-456-789")...)

	b.Run("3groups_3KB", func(b *testing.B) {
		b.SetBytes(int64(len(input)))
		for i := 0; i < b.N; i++ {
			vm.SearchWithCaptures(input)
		}
	})
}
