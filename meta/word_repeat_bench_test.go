package meta

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestWordRepeatNFAStates checks how many NFA states word repeat patterns generate.
func TestWordRepeatNFAStates(t *testing.T) {
	patterns := []string{
		`(\w{2,8})+`,
		`(?:\w{2,8})+`,
		`\w{2,8}`,
		`\w+`,
		`[\w]+`,
		`[a-zA-Z]+`,
		`\d+`,
	}

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Errorf("Compile(%q) failed: %v", pattern, err)
			continue
		}

		nfaStates := engine.nfa.States()
		btMaxSize := 0
		maxInputKB := 0
		if engine.boundedBacktracker != nil {
			btMaxSize = engine.boundedBacktracker.MaxVisitedSize()
			maxInputKB = btMaxSize / (nfaStates + 1) / 1024
		}

		t.Logf("Pattern: %-20s States: %3d MaxInput: %5dKB BT.MaxSize: %d Strategy: %s",
			pattern, nfaStates, maxInputKB, btMaxSize, engine.Strategy())
	}
}

// TestWordRepeatCorrectness verifies that word_repeat patterns produce correct matches.
func TestWordRepeatCorrectness(t *testing.T) {
	pattern := `(\w{2,8})+`
	engine, _ := Compile(pattern)
	reStd := regexp.MustCompile(pattern)

	tests := []string{
		"hello",
		"hello world test",
		"a", // min is 2, so no match
		"ab",
		"abc def ghi",
	}

	for _, input := range tests {
		stdMatches := reStd.FindAllString(input, -1)
		cgxMatches := engine.FindAllIndicesStreaming([]byte(input), -1, nil)

		if len(stdMatches) != len(cgxMatches) {
			t.Errorf("Pattern %q on %q: stdlib found %d, coregex found %d",
				pattern, input, len(stdMatches), len(cgxMatches))
			continue
		}

		for i, stdMatch := range stdMatches {
			cgxStart, cgxEnd := cgxMatches[i][0], cgxMatches[i][1]
			cgxMatch := input[cgxStart:cgxEnd]
			if stdMatch != cgxMatch {
				t.Errorf("Pattern %q on %q match %d: stdlib=%q coregex=%q",
					pattern, input, i, stdMatch, cgxMatch)
			}
		}
	}
}

// BenchmarkWordRepeat compares coregex vs stdlib for word_repeat pattern.
// Expectations:
// - Input ≤100KB: coregex should be 20-50% faster (uses BoundedBacktracker)
// - Input >200KB: coregex may be 1.5-3x slower (falls back to PikeVM)
func BenchmarkWordRepeat(b *testing.B) {
	pattern := `(\w{2,8})+`

	engineCgx, _ := Compile(pattern)
	reStd := regexp.MustCompile(pattern)

	// 50KB input - should use BoundedBacktracker and be faster
	input := []byte(strings.Repeat("hello world test data foo bar 123 ", 1500))
	b.Logf("Input size: %dKB, NFA states: %d", len(input)/1024, engineCgx.nfa.States())

	b.Run("coregex_50KB", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = engineCgx.FindAllIndicesStreaming(input, -1, nil)
		}
	})

	b.Run("stdlib_50KB", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = reStd.FindAllIndex(input, -1)
		}
	})
}

// BenchmarkWordRepeatBySize tests different input sizes.
func BenchmarkWordRepeatBySize(b *testing.B) {
	pattern := `(\w{2,8})+`

	engineCgx, _ := Compile(pattern)
	reStd := regexp.MustCompile(pattern)

	for _, sizeKB := range []int{10, 50, 100, 332, 1024, 6144} {
		input := []byte(strings.Repeat("hello world test data foo bar 123 ", sizeKB*30))

		b.Run(fmt.Sprintf("coregex_%dKB", sizeKB), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = engineCgx.FindAllIndicesStreaming(input, -1, nil)
			}
		})

		b.Run(fmt.Sprintf("stdlib_%dKB", sizeKB), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = reStd.FindAllIndex(input, -1)
			}
		})
	}
}
