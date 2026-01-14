package meta

import (
	"regexp"
	"testing"
)

// CompositeSearcher benchmarks: concatenated char classes like [a-zA-Z]+[0-9]+
// These patterns benefit from sequential lookup table optimization.

var compositePatterns = []struct {
	name    string
	pattern string
	input   string
}{
	{"AlphaDigit", `[a-zA-Z]+\d+`, "abc123def456"},
	{"AlphaDigitAlpha", `[a-z]+[0-9]+[a-z]+`, "abc123def"},
	{"WordDigit", `\w+[0-9]+`, "hello123world"},
	{"HexPattern", `[a-fA-F]+[0-9]+`, "deadbeef42"},
}

func BenchmarkComposite_Stdlib(b *testing.B) {
	for _, tc := range compositePatterns {
		b.Run(tc.name, func(b *testing.B) {
			re := regexp.MustCompile(tc.pattern)
			input := []byte(tc.input)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				re.Find(input)
			}
		})
	}
}

func BenchmarkComposite_Coregex(b *testing.B) {
	for _, tc := range compositePatterns {
		b.Run(tc.name, func(b *testing.B) {
			re, err := Compile(tc.pattern)
			if err != nil {
				b.Fatal(err)
			}
			input := []byte(tc.input)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				re.Find(input)
			}
		})
	}
}

func BenchmarkComposite_NoMatch_Stdlib(b *testing.B) {
	re := regexp.MustCompile(`[a-zA-Z]+\d+`)
	input := []byte("no digits here at all")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Find(input)
	}
}

func BenchmarkComposite_NoMatch_Coregex(b *testing.B) {
	re, err := Compile(`[a-zA-Z]+\d+`)
	if err != nil {
		b.Fatal(err)
	}
	input := []byte("no digits here at all")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Find(input)
	}
}
