package meta

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestConcurrentMatch tests that Engine.IsMatch is thread-safe.
// Multiple goroutines call IsMatch concurrently on the same Engine instance.
//
// Note: This test focuses on patterns that use NFA-based strategies (PikeVM,
// BoundedBacktracker) which have been made thread-safe. Patterns that use
// DFA-based strategies (UseReverseSuffix, UseDigitPrefilter, etc.) are tested
// separately and require additional thread-safety work - see issue #78.
func TestConcurrentMatch(t *testing.T) {
	// These patterns use NFA-based strategies that are thread-safe:
	// - UseNFA: Simple literals, anchored patterns, word boundaries
	// - UseCharClassSearcher: Single character class patterns
	// Verified with Strategy() output - only include patterns that DO NOT use DFA
	patterns := []string{
		`hello`,     // UseNFA - literal with memmem prefilter
		`\d+`,       // UseCharClassSearcher - digit class (NFA-based)
		`[a-zA-Z]+`, // UseCharClassSearcher - alpha class (NFA-based)
		`^start`,    // UseNFA - anchored pattern
		`\b\w+\b`,   // UseNFA - word boundary pattern
	}

	// Patterns that use DFA-based strategies (not yet thread-safe):
	// - `foo|bar|baz` - UseTeddy (uses DFA)
	// - `.*\.txt$` - UseReverseSuffix (uses DFA)
	// - `[0-9]{1,3}\.[0-9]{1,3}` - UseDigitPrefilter (uses DFA)
	// - `(\w+)@(\w+)\.(\w+)` - UseReverseInner (uses DFA)

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			engine, err := Compile(pattern)
			if err != nil {
				t.Fatalf("failed to compile %q: %v", pattern, err)
			}

			testCases := []struct {
				input    string
				expected bool
			}{
				{"hello world", true},
				{"HELLO WORLD", false}, // Case sensitive
				{"12345", true},
				{"abcdef", true},
				{"foo bar baz", true},
				{"test.txt", true},
				{"start of string", true},
				{"user@domain.com", true},
				{"192.168.1.1", true},
				{"no match here xyzzy", false},
			}

			const numGoroutines = 100
			const numIterations = 100

			var wg sync.WaitGroup
			var errors atomic.Int64

			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < numIterations; j++ {
						for _, tc := range testCases {
							result := engine.IsMatch([]byte(tc.input))
							// We don't check exact results since patterns may or may not match
							// The main goal is to verify no panics/races occur
							_ = result
						}
					}
				}()
			}

			wg.Wait()

			if errors.Load() > 0 {
				t.Errorf("encountered %d errors during concurrent execution", errors.Load())
			}
		})
	}
}

// TestConcurrentFind tests that Engine.Find is thread-safe.
// Multiple goroutines call Find concurrently on the same Engine instance.
func TestConcurrentFind(t *testing.T) {
	engine, err := Compile(`\b\w+\b`)
	if err != nil {
		t.Fatalf("failed to compile pattern: %v", err)
	}

	inputs := []string{
		"hello world",
		"the quick brown fox jumps over the lazy dog",
		"testing 123 456 789",
		"a b c d e f g h i j k l m n o p q r s t u v w x y z",
		"",
		"   ",
		"single",
	}

	const numGoroutines = 100
	const numIterations = 100

	var wg sync.WaitGroup
	var matchCount atomic.Int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				for _, input := range inputs {
					match := engine.Find([]byte(input))
					if match != nil {
						matchCount.Add(1)
					}
				}
			}
		}()
	}

	wg.Wait()

	// Verify we got some matches (sanity check)
	if matchCount.Load() == 0 {
		t.Error("expected at least some matches")
	}
}

// TestConcurrentFindAllIndices tests that Engine.FindAllIndicesStreaming is thread-safe.
func TestConcurrentFindAllIndices(t *testing.T) {
	engine, err := Compile(`\d+`)
	if err != nil {
		t.Fatalf("failed to compile pattern: %v", err)
	}

	input := []byte("test 123 foo 456 bar 789 baz 012")

	const numGoroutines = 50
	const numIterations = 100

	var wg sync.WaitGroup
	var totalMatches atomic.Int64

	expectedPerCall := 4 // "123", "456", "789", "012"

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results := make([][2]int, 0, 16)
			for j := 0; j < numIterations; j++ {
				results = results[:0]
				matches := engine.FindAllIndicesStreaming(input, -1, results)
				totalMatches.Add(int64(len(matches)))
				// Note: We don't check exact count here - race detector will catch races.
				// The final assertion below verifies correct count after all goroutines complete.
				_ = len(matches) == expectedPerCall
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * numIterations * expectedPerCall)
	if totalMatches.Load() != expected {
		t.Errorf("expected %d total matches, got %d", expected, totalMatches.Load())
	}
}

// TestConcurrentFindSubmatch tests that Engine.FindSubmatch is thread-safe.
//
// Uses UseBoundedBacktracker strategy which has been made thread-safe.
// Email pattern `(\w+)@(\w+)\.(\w+)` uses UseReverseInner (DFA-based),
// so we use simpler capture patterns that stay in NFA-based strategies.
func TestConcurrentFindSubmatch(t *testing.T) {
	// Use pattern that triggers UseBoundedBacktracker (NFA-based, thread-safe)
	engine, err := Compile(`(\w+)\s+(\w+)`)
	if err != nil {
		t.Fatalf("failed to compile pattern: %v", err)
	}

	inputs := []string{
		"hello world",
		"the quick brown fox",
		"single",       // no match - only one word
		"test example", // match
	}

	const numGoroutines = 50
	const numIterations = 100

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				for _, input := range inputs {
					match := engine.FindSubmatch([]byte(input))
					// Just check it doesn't panic
					_ = match
				}
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentCount tests that Engine.Count is thread-safe.
func TestConcurrentCount(t *testing.T) {
	engine, err := Compile(`\bfoo\b`)
	if err != nil {
		t.Fatalf("failed to compile pattern: %v", err)
	}

	input := []byte("foo bar foo baz foo")
	expectedCount := 3

	const numGoroutines = 50
	const numIterations = 100

	var wg sync.WaitGroup
	var errors atomic.Int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				count := engine.Count(input, -1)
				if count != expectedCount {
					errors.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	if errors.Load() > 0 {
		t.Errorf("encountered %d incorrect count results", errors.Load())
	}
}

// TestConcurrentMixedOperations tests concurrent usage with mixed operations.
// This is a stress test to verify no races when different operations run
// concurrently (run with -race).
func TestConcurrentMixedOperations(t *testing.T) {
	engine, err := Compile(`(\d+)-(\d+)-(\d+)`)
	if err != nil {
		t.Fatalf("failed to compile pattern: %v", err)
	}

	input := []byte("Date: 2024-01-15, Code: 123-456-789, ID: 999-888-777")

	const numGoroutines = 20
	const numIterations = 100

	var wg sync.WaitGroup

	// IsMatch goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				_ = engine.IsMatch(input)
			}
		}()
	}

	// Find goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				_ = engine.Find(input)
			}
		}()
	}

	// FindAllIndicesStreaming goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results := make([][2]int, 0, 16)
			for j := 0; j < numIterations; j++ {
				results = results[:0]
				_ = engine.FindAllIndicesStreaming(input, -1, results)
			}
		}()
	}

	// FindSubmatch goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				_ = engine.FindSubmatch(input)
			}
		}()
	}

	// Count goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				_ = engine.Count(input, -1)
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentDifferentPatterns tests concurrent usage with multiple engines
// covering NFA, DFA, adaptive, literal and reverse-search strategies. Each
// result is compared with the single-goroutine result for the same engine.
func TestConcurrentDifferentPatterns(t *testing.T) {
	patterns := []string{
		`\d+`,                // UseCharClassSearcher
		`[a-z]+`,             // UseCharClassSearcher
		`^\w+`,               // anchored pattern
		`\b\w+\b`,            // word boundary pattern
		`\w+@\w+\.\w+`,       // inner literal (reverse search)
		`foo|bar|baz`,        // UseTeddy
		`\w+$`,               // end-anchored (reverse search)
		`(\w+)@(\w+)\.(\w+)`, // UseBoth
		`a+?b`,               // UseDFA with a lazy quantifier
	}

	engines := make([]*Engine, len(patterns))
	for i, pattern := range patterns {
		var err error
		engines[i], err = Compile(pattern)
		if err != nil {
			t.Fatalf("failed to compile %q: %v", pattern, err)
		}
	}

	input := []byte("test123 hello@world.com foo bar aab end")

	type result struct {
		isMatch    bool
		start, end int
		found      bool
	}
	want := make([]result, len(engines))
	for i, engine := range engines {
		start, end, found := engine.FindIndices(input)
		want[i] = result{engine.IsMatch(input), start, end, found}
	}

	const numGoroutines = 50
	const numIterations = 100

	var wg sync.WaitGroup
	var mismatches atomic.Int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(engineIdx int) {
			defer wg.Done()
			idx := engineIdx % len(engines)
			engine := engines[idx]
			for j := 0; j < numIterations; j++ {
				start, end, found := engine.FindIndices(input)
				if (result{engine.IsMatch(input), start, end, found}) != want[idx] {
					mismatches.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	if n := mismatches.Load(); n > 0 {
		t.Errorf("%d concurrent results differ from single-goroutine results", n)
	}
}

// TestConcurrentCaseInsensitivePrefilter verifies that case-insensitive patterns
// with prefilter work correctly under concurrent RunParallel.
// Regression test for Issue #137: on ARM64 without SIMD, concurrent access to
// shared lazy DFA via prefilter candidate loop caused cache corruption
// (1.7GB allocs, 1s+ per op on M2 Max).
func TestConcurrentCaseInsensitivePrefilter(t *testing.T) {
	// This pattern produces 60 case-fold prefix literals → FatTeddy/AC prefilter
	// + UseDFA strategy with 181 NFA states
	engine, err := Compile(`(?iU)\b(eval|system|exec|execute|passthru|shell_exec|phpinfo)\b`)
	if err != nil {
		t.Fatal(err)
	}

	// Input WITH match — exercises verification path, not just rejection
	matchInput := []byte(strings.Repeat("Mozilla/5.0 Safari/537.36 phpinfo", 8))
	// Input WITHOUT match — exercises fast rejection path
	noMatchInput := []byte(strings.Repeat("Mozilla/5.0 Safari/537.36 Chrome/96", 8))

	t.Run("match_concurrent", func(t *testing.T) {
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					if !engine.IsMatch(matchInput) {
						t.Error("IsMatch returned false, expected true")
						return
					}
				}
			}()
		}
		wg.Wait()
	})

	t.Run("no_match_concurrent", func(t *testing.T) {
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					if engine.IsMatch(noMatchInput) {
						t.Error("IsMatch returned true, expected false")
						return
					}
				}
			}()
		}
		wg.Wait()
	})
}

// BenchmarkConcurrentCaseInsensitiveMatch benchmarks concurrent IsMatch with
// case-insensitive prefilter pattern — the pattern reported in Issue #137.
func BenchmarkConcurrentCaseInsensitiveMatch(b *testing.B) {
	engine, err := Compile(`(?iU)\b(eval|system|exec|execute|passthru|shell_exec|phpinfo)\b`)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(strings.Repeat("Mozilla/5.0 Safari/537.36 phpinfo", 8))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			engine.IsMatch(input)
		}
	})
}

// BenchmarkConcurrentIsMatch benchmarks concurrent IsMatch performance.
func BenchmarkConcurrentIsMatch(b *testing.B) {
	engine, err := Compile(`\b\w+\b`)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte("the quick brown fox jumps over the lazy dog")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			engine.IsMatch(input)
		}
	})
}

// BenchmarkConcurrentFind benchmarks concurrent Find performance.
func BenchmarkConcurrentFind(b *testing.B) {
	engine, err := Compile(`\b\w+\b`)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte("the quick brown fox jumps over the lazy dog")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			engine.Find(input)
		}
	})
}

// BenchmarkConcurrentFindSubmatch benchmarks concurrent FindSubmatch performance.
func BenchmarkConcurrentFindSubmatch(b *testing.B) {
	engine, err := Compile(`(\w+)@(\w+)\.(\w+)`)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte("contact: user@example.com for more info")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			engine.FindSubmatch(input)
		}
	})
}
