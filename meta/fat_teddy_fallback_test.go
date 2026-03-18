package meta

import (
	"regexp"
	"strings"
	"testing"
)

// TestFatTeddyACPrefilter verifies that Fat Teddy (33-64 patterns)
// is replaced with Aho-Corasick prefilter at compile time (Issue #137).
//
// FatTeddy's AVX2 SIMD has known bugs with FindMatch at non-zero positions
// causing false negatives in FindAll iteration. AC provides correct matching.
func TestFatTeddyACPrefilter(t *testing.T) {
	// 50 patterns - originally Fat Teddy range (33-64), now replaced with AC
	patterns := make([]string, 50)
	for i := range 50 {
		patterns[i] = "p" + string('0'+byte(i/10)) + string('0'+byte(i%10))
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
		t.Fatalf("Strategy = %s, want UseTeddy for 50 patterns", re.Strategy())
	}

	// AC prefilter replaces FatTeddy at compile time
	if re.prefilter == nil {
		t.Fatal("prefilter is nil, expected Aho-Corasick prefilter")
	}

	t.Run("find_correctness", func(t *testing.T) {
		haystack := []byte("prefix p25 middle p42 suffix p01 end")
		match := re.Find(haystack)
		if match == nil {
			t.Fatal("Find returned nil, expected match")
		}
		if match.String() != "p25" {
			t.Errorf("Match = %q, want %q", match.String(), "p25")
		}
	})

	t.Run("find_large_haystack", func(t *testing.T) {
		haystack := make([]byte, 128)
		copy(haystack, "prefix ")
		copy(haystack[64:], "p42 suffix p01 end more padding to make it big")

		match := re.Find(haystack)
		if match == nil {
			t.Fatal("Find returned nil, expected match")
		}
	})

	t.Run("is_match", func(t *testing.T) {
		if !re.IsMatch([]byte("test p25 here")) {
			t.Error("IsMatch returned false, expected true")
		}
	})

	t.Run("find_indices", func(t *testing.T) {
		haystack := []byte("test p42 here")
		start, end, found := re.FindIndices(haystack)
		if !found {
			t.Fatal("FindIndices returned found=false")
		}
		if start != 5 || end != 8 {
			t.Errorf("FindIndices = (%d, %d), want (5, 8)", start, end)
		}
	})

	t.Run("no_match", func(t *testing.T) {
		match := re.Find([]byte("no patterns here at all"))
		if match != nil {
			t.Errorf("Find returned %q, expected nil", match.String())
		}
	})

	t.Run("find_all_iteration", func(t *testing.T) {
		// Critical: verify FindAll works correctly with AC prefilter
		haystack := []byte("p01 middle p25 end p42")
		matches := re.FindAllIndicesStreaming(haystack, -1, nil)
		if len(matches) != 3 {
			t.Errorf("FindAll count = %d, want 3", len(matches))
		}
	})
}

// TestSlimTeddyNoFallback verifies that Slim Teddy (2-32 patterns)
// does NOT get replaced with AC (SlimTeddy SIMD is correct).
func TestSlimTeddyNoFallback(t *testing.T) {
	patterns := make([]string, 20)
	for i := range 20 {
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

// TestFatTeddyCaseFoldRegression verifies correctness of (?i)get|post|put
// which produces 40 case-fold expanded patterns (FatTeddy range).
//
// Root cause: fatTeddyAVX2_2 used ANDL to combine low/high 128-bit lane masks.
// Patterns assigned only to buckets 0-7 (low lane) produced zero in the high
// lane, so AND always yielded zero = missed matches. Fix: ORL.
//
// This was the LangArena "methods" pattern bug: 11456 matches instead of 34368.
// Only POST matched (patterns spanning both lanes), GET and PUT were missed.
func TestFatTeddyCaseFoldRegression(t *testing.T) {
	re, err := Compile(`(?i)get|post|put`)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	t.Logf("Strategy: %s", re.Strategy())

	stdlibRe := regexp.MustCompile(`(?i)get|post|put`)

	// Test on generated log data similar to LangArena
	data := generateTestLogData(1000)
	haystack := []byte(data)

	// Count matches with stdlib
	stdlibMatches := stdlibRe.FindAllIndex(haystack, -1)
	stdlibCount := len(stdlibMatches)

	// Count matches with coregex
	coregexMatches := re.FindAllIndicesStreaming(haystack, -1, nil)
	coregexCount := len(coregexMatches)

	t.Logf("stdlib: %d matches, coregex: %d matches", stdlibCount, coregexCount)

	if coregexCount != stdlibCount {
		t.Errorf("Match count mismatch: coregex=%d, stdlib=%d (diff=%d)",
			coregexCount, stdlibCount, stdlibCount-coregexCount)

		// Show first few mismatches for debugging
		if len(stdlibMatches) > 0 && len(coregexMatches) > 0 {
			si, ci := 0, 0
			mismatches := 0
			for si < len(stdlibMatches) && ci < len(coregexMatches) && mismatches < 5 {
				sm := stdlibMatches[si]
				cm := coregexMatches[ci]
				switch {
				case sm[0] == cm[0] && sm[1] == cm[1]:
					si++
					ci++
				case sm[0] < cm[0]:
					t.Logf("  Missing match at [%d:%d] = %q", sm[0], sm[1],
						haystack[sm[0]:sm[1]])
					si++
					mismatches++
				default:
					t.Logf("  Extra match at [%d:%d] = %q", cm[0], cm[1],
						haystack[cm[0]:cm[1]])
					ci++
					mismatches++
				}
			}
		}
	}
}

// generateTestLogData creates log-like data with GET/POST/PUT methods.
func generateTestLogData(lines int) string {
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	var b strings.Builder
	b.Grow(lines * 100)
	for i := 0; i < lines; i++ {
		b.WriteString("192.168.1.1 - - [01/Oct/2023:12:00:00 +0000] \"")
		b.WriteString(methods[i%len(methods)])
		b.WriteString(" /path HTTP/1.1\" 200 1234\n")
	}
	return b.String()
}

// BenchmarkACPrefilter compares AC prefilter on small vs large haystacks.
func BenchmarkACPrefilter(b *testing.B) {
	patterns := make([]string, 50)
	for i := range 50 {
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
