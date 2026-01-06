package meta

import (
	"testing"
)

// TestFatTeddySmallHaystackFallback verifies that Fat Teddy (33-64 patterns)
// correctly uses Aho-Corasick fallback for small haystacks.
//
// Background:
// Fat Teddy's AVX2 SIMD has setup overhead that makes it slower than
// Aho-Corasick for small haystacks (< 64 bytes). This follows Rust regex's
// minimum_len() approach in rust-aho-corasick/src/packed/teddy/builder.rs.
//
// Benchmarks show:
//   - 37-byte haystack with 50 patterns: Fat Teddy ~267 ns, Aho-Corasick ~130 ns
//   - After fallback: ~110 ns (Aho-Corasick path used)
func TestFatTeddySmallHaystackFallback(t *testing.T) {
	// 50 patterns - uses Fat Teddy (33-64 pattern range)
	// Each pattern >= 3 bytes with unique first characters to avoid prefix sharing
	patterns := make([]string, 50)
	for i := 0; i < 50; i++ {
		// Generate pattern like "p00", "p01", ..., "p49"
		patterns[i] = "p" + string('0'+byte(i/10)) + string('0'+byte(i%10))
	}

	// Build alternation pattern
	pattern := patterns[0]
	for i := 1; i < len(patterns); i++ {
		pattern += "|" + patterns[i]
	}

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Verify strategy is UseTeddy (not UseAhoCorasick - that's for >64 patterns)
	if re.Strategy() != UseTeddy {
		t.Fatalf("Strategy = %s, want UseTeddy for 50 patterns", re.Strategy())
	}

	// Verify fatTeddyFallback is set
	if re.fatTeddyFallback == nil {
		t.Fatal("fatTeddyFallback is nil, expected Aho-Corasick fallback for Fat Teddy")
	}

	t.Run("small_haystack_uses_fallback", func(t *testing.T) {
		// Small haystack (37 bytes) - should use Aho-Corasick fallback
		haystack := []byte("prefix p25 middle p42 suffix p01 end")
		if len(haystack) >= fatTeddySmallHaystackThreshold {
			t.Fatalf("Test haystack too large: %d >= %d", len(haystack), fatTeddySmallHaystackThreshold)
		}

		// Reset stats
		re.stats = Stats{}

		match := re.Find(haystack)
		if match == nil {
			t.Fatal("Find returned nil, expected match")
		}

		// Verify Aho-Corasick was used (not PrefilterHits)
		if re.stats.AhoCorasickSearches == 0 {
			t.Error("AhoCorasickSearches = 0, expected > 0 for small haystack fallback")
		}
		if re.stats.PrefilterHits != 0 {
			t.Errorf("PrefilterHits = %d, expected 0 for small haystack fallback", re.stats.PrefilterHits)
		}

		// Verify match is correct
		want := "p25"
		if match.String() != want {
			t.Errorf("Match = %q, want %q", match.String(), want)
		}
	})

	t.Run("large_haystack_uses_fat_teddy", func(t *testing.T) {
		// Large haystack (>64 bytes) - should use Fat Teddy directly
		haystack := make([]byte, 128)
		copy(haystack, "prefix ")
		copy(haystack[64:], "p42 suffix p01 end more padding to make it big")

		if len(haystack) < fatTeddySmallHaystackThreshold {
			t.Fatalf("Test haystack too small: %d < %d", len(haystack), fatTeddySmallHaystackThreshold)
		}

		// Reset stats
		re.stats = Stats{}

		match := re.Find(haystack)
		if match == nil {
			t.Fatal("Find returned nil, expected match")
		}

		// Verify Fat Teddy was used (PrefilterHits, not AhoCorasickSearches)
		if re.stats.PrefilterHits == 0 {
			t.Error("PrefilterHits = 0, expected > 0 for large haystack")
		}
		if re.stats.AhoCorasickSearches != 0 {
			t.Errorf("AhoCorasickSearches = %d, expected 0 for large haystack", re.stats.AhoCorasickSearches)
		}
	})

	t.Run("isMatch_small_haystack", func(t *testing.T) {
		haystack := []byte("test p25 here")
		re.stats = Stats{}

		got := re.IsMatch(haystack)
		if !got {
			t.Error("IsMatch returned false, expected true")
		}

		if re.stats.AhoCorasickSearches == 0 {
			t.Error("AhoCorasickSearches = 0, expected > 0 for small haystack")
		}
	})

	t.Run("findIndices_small_haystack", func(t *testing.T) {
		haystack := []byte("test p42 here")
		re.stats = Stats{}

		start, end, found := re.FindIndices(haystack)
		if !found {
			t.Fatal("FindIndices returned found=false")
		}

		if start != 5 || end != 8 {
			t.Errorf("FindIndices = (%d, %d), want (5, 8)", start, end)
		}

		if re.stats.AhoCorasickSearches == 0 {
			t.Error("AhoCorasickSearches = 0, expected > 0 for small haystack")
		}
	})

	t.Run("no_match_small_haystack", func(t *testing.T) {
		haystack := []byte("no patterns here at all")
		re.stats = Stats{}

		match := re.Find(haystack)
		if match != nil {
			t.Errorf("Find returned %q, expected nil", match.String())
		}

		// Should still use Aho-Corasick (small haystack)
		if re.stats.AhoCorasickSearches == 0 {
			t.Error("AhoCorasickSearches = 0, expected > 0 even for no-match")
		}
	})
}

// TestSlimTeddyNoFallback verifies that Slim Teddy (2-32 patterns)
// does NOT have Aho-Corasick fallback (it's efficient enough for small haystacks).
func TestSlimTeddyNoFallback(t *testing.T) {
	// 20 patterns - uses Slim Teddy (2-32 pattern range)
	patterns := make([]string, 20)
	for i := 0; i < 20; i++ {
		patterns[i] = "pat" + string('0'+byte(i/10)) + string('0'+byte(i%10))
	}

	pattern := patterns[0]
	for i := 1; i < len(patterns); i++ {
		pattern += "|" + patterns[i]
	}

	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if re.Strategy() != UseTeddy {
		t.Fatalf("Strategy = %s, want UseTeddy for 20 patterns", re.Strategy())
	}

	// Slim Teddy should NOT have fallback
	if re.fatTeddyFallback != nil {
		t.Error("fatTeddyFallback is not nil, Slim Teddy should not have fallback")
	}
}

// BenchmarkFatTeddyFallback compares Fat Teddy performance on small vs large haystacks.
func BenchmarkFatTeddyFallback(b *testing.B) {
	// 50 patterns - uses Fat Teddy
	patterns := make([]string, 50)
	for i := 0; i < 50; i++ {
		patterns[i] = "p" + string('0'+byte(i/10)) + string('0'+byte(i%10))
	}
	pattern := patterns[0]
	for i := 1; i < len(patterns); i++ {
		pattern += "|" + patterns[i]
	}

	re, err := Compile(pattern)
	if err != nil {
		b.Fatal(err)
	}

	smallHaystack := []byte("prefix p25 middle p42 suffix p01 end")
	largeHaystack := make([]byte, 1024)
	copy(largeHaystack[500:], "p42")

	b.Run("small_haystack_37B", func(b *testing.B) {
		b.SetBytes(int64(len(smallHaystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Find(smallHaystack)
		}
	})

	b.Run("large_haystack_1KB", func(b *testing.B) {
		b.SetBytes(int64(len(largeHaystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Find(largeHaystack)
		}
	})
}
