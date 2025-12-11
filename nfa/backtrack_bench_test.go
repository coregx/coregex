package nfa

import (
	"regexp"
	"testing"
)

func BenchmarkBacktracker_VsStdlib(b *testing.B) {
	patterns := []struct {
		name    string
		pattern string
	}{
		{"digit", `\d+`},
		{"word", `\w+`},
		{"alpha", `[a-z]+`},
	}

	input := []byte("the quick brown fox jumps over 12345 lazy dogs")

	for _, p := range patterns {
		// stdlib
		stdRe := regexp.MustCompile(p.pattern)
		b.Run(p.name+"/stdlib", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				stdRe.Match(input)
			}
		})

		// BoundedBacktracker
		nfa := compileNFAForTest(p.pattern)
		bt := NewBoundedBacktracker(nfa)
		b.Run(p.name+"/backtracker", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bt.IsMatch(input)
			}
		})

		// PikeVM for reference
		vm := NewPikeVM(nfa)
		b.Run(p.name+"/pikevm", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				vm.IsMatch(input)
			}
		})
	}
}
