package meta

import (
	"strings"
	"testing"
)

// TestFindAllDFACacheOverflow_ReverseFallback verifies that FindAllIndicesStreaming
// returns correct results when the DFA cache overflows and nfaFallbackReverse
// is invoked. Bug: nfaFallbackReverse runs reverse NFA's PikeVM forward,
// producing no matches for multi-byte UTF-8 patterns.
//
// Root cause: dfa/lazy/lazy.go:2161 — d.pikevm.Search(haystack[start:end])
// runs reverse NFA forward instead of backward.
func TestFindAllDFACacheOverflow_ReverseFallback(t *testing.T) {
	// \pL+ compiles to 5844 NFA states. BoundedBacktracker handles inputs
	// up to ~5741 bytes. Beyond that, bidirectional DFA is used. When DFA
	// cache overflows, nfaFallbackReverse is called — and fails.
	pattern := `\pL+`

	// Build input large enough to exceed BoundedBacktracker threshold (~5741 bytes)
	// but structured so we know the exact expected match count.
	base := "hello world "
	var sb strings.Builder
	for sb.Len() < 10000 {
		sb.WriteString(base)
	}
	input := sb.String()

	config := DefaultConfig()
	engine, err := CompileWithConfig(pattern, config)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pattern, err)
	}

	haystack := []byte(input)

	// Count expected matches: "hello" and "world" per repetition
	wordsPerRepeat := 2
	repeats := len(input) / len(base)
	expectedMin := repeats * wordsPerRepeat

	results := engine.FindAllIndicesStreaming(haystack, -1, nil)
	if len(results) < expectedMin {
		t.Errorf("FindAllIndicesStreaming(%q) on %dB input: got %d matches, want >= %d",
			pattern, len(input), len(results), expectedMin)
	}

	// Count must agree
	count := engine.Count(haystack, -1)
	if count != len(results) {
		t.Errorf("Count disagrees with FindAllIndicesStreaming: Count=%d, FindAll=%d", count, len(results))
	}
}

// TestFindAllDFACacheOverflow_SmallCache forces DFA cache overflow with
// a small MaxDFAStates on a simple ASCII pattern to verify the invariant:
// cache-full → either flush+continue or NFA fallback, never "no matches".
func TestFindAllDFACacheOverflow_SmallCache(t *testing.T) {
	// Pattern that uses DFA: alternation with enough states to fill a tiny cache.
	pattern := `[a-z]+`

	// Large input with many matches
	input := strings.Repeat("hello world foo bar ", 500)

	config := DefaultConfig()
	config.MaxDFAStates = 3 // Force tiny cache → frequent overflow

	engine, err := CompileWithConfig(pattern, config)
	if err != nil {
		t.Fatalf("CompileWithConfig: %v", err)
	}

	haystack := []byte(input)

	results := engine.FindAllIndicesStreaming(haystack, -1, nil)
	if len(results) == 0 {
		t.Fatal("FindAllIndicesStreaming returned 0 matches on input that clearly matches")
	}

	// Verify against a default-config engine
	defaultEngine, _ := Compile(pattern)
	defaultResults := defaultEngine.FindAllIndicesStreaming(haystack, -1, nil)
	if len(results) != len(defaultResults) {
		t.Errorf("Small cache: %d matches, default cache: %d matches",
			len(results), len(defaultResults))
	}

	// Verify match positions
	for i := 0; i < len(results) && i < len(defaultResults); i++ {
		if results[i] != defaultResults[i] {
			t.Errorf("Match %d: small cache=%v, default=%v", i, results[i], defaultResults[i])
			break
		}
	}
}

// TestFindAllDFACacheOverflow_Cyrillic tests the original reported bug:
// \pL+ on Cyrillic-heavy input returns 1 instead of correct match count.
func TestFindAllDFACacheOverflow_Cyrillic(t *testing.T) {
	pattern := `\pL+`
	base := "Hello мир world тест data проверка text αβγ δεζ "

	for _, size := range []int{1000, 5000, 10000, 20000, 50000} {
		var sb strings.Builder
		for sb.Len() < size {
			sb.WriteString(base)
		}
		input := sb.String()
		haystack := []byte(input)

		engine, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pattern, err)
		}

		results := engine.FindAllIndicesStreaming(haystack, -1, nil)
		count := engine.Count(haystack, -1)
		isMatch := engine.IsMatch(haystack)

		// The pattern matches the input (IsMatch=true), so FindAll must return >0
		if isMatch && len(results) <= 1 {
			t.Errorf("\\pL+ on %dB: IsMatch=true but FindAllIndicesStreaming returned only %d matches (Count=%d)",
				size, len(results), count)
		}

		// Count must agree with FindAll
		if count != len(results) {
			t.Errorf("\\pL+ on %dB: Count=%d but FindAllIndicesStreaming=%d", size, count, len(results))
		}

		// Verify match count scales roughly linearly with input size
		if size >= 10000 {
			wordsPerBase := 10 // approximate words per base string
			repeats := size / len(base)
			expectedMin := repeats * wordsPerBase * 80 / 100 // 80% tolerance
			if len(results) < expectedMin {
				t.Errorf("\\pL+ on %dB: only %d matches, expected at least %d",
					size, len(results), expectedMin)
			}
		}
	}
}

// TestFindAllDFACacheOverflow_CountConsistency ensures Count and FindAllIndicesStreaming
// always agree, even under DFA cache pressure.
func TestFindAllDFACacheOverflow_CountConsistency(t *testing.T) {
	patterns := []string{
		`\pL+`,
		`\p{Cyrillic}+`,
		`\w+`,
		`[a-zA-Z]+`,
	}

	input := strings.Repeat("Hello мир world тест ", 300)
	haystack := []byte(input)

	for _, pattern := range patterns {
		engine, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pattern, err)
		}

		results := engine.FindAllIndicesStreaming(haystack, -1, nil)
		count := engine.Count(haystack, -1)

		if count != len(results) {
			t.Errorf("Pattern %q: Count=%d, FindAllIndicesStreaming=%d", pattern, count, len(results))
		}
	}
}
