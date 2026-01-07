//go:build amd64

package prefilter

import (
	"testing"
)

// TestSlimTeddyAVX2_Regression reproduces the CI regression.
// On AMD EPYC, AVX2 Slim Teddy was 6x slower than SSSE3.
//
// This test verifies that AVX2 and SSSE3 produce identical results
// for the exact patterns and haystack from the failing benchmark.
func TestSlimTeddyAVX2_Regression(t *testing.T) {
	if !hasAVX2 || !hasSSSE3 {
		t.Skip("Need both AVX2 and SSSE3")
	}

	// Exact patterns from BenchmarkAhoCorasickLargeInput (15 patterns)
	patterns := [][]byte{
		[]byte("error"), []byte("warning"), []byte("critical"),
		[]byte("fatal"), []byte("debug"), []byte("info"),
		[]byte("trace"), []byte("notice"), []byte("alert"),
		[]byte("emergency"), []byte("panic"), []byte("exception"),
		[]byte("failure"), []byte("timeout"), []byte("refused"),
	}

	config := &TeddyConfig{
		MinPatterns:    2,
		MaxPatterns:    32,
		MinPatternLen:  3,
		FingerprintLen: 2, // Default
	}

	teddy := NewTeddy(patterns, config)
	if teddy == nil {
		t.Fatal("Failed to create Teddy")
	}

	// Create exact haystack from benchmark (64KB with sparse matches)
	base := "This is a line of log output without any matching keywords. Just normal text. "
	haystack := make([]byte, 0, 64*1024)
	for len(haystack) < 60*1024 {
		haystack = append(haystack, base...)
	}
	haystack = append(haystack, "error occurred. warning issued. critical alert."...)

	t.Logf("Haystack size: %d bytes", len(haystack))
	t.Logf("FingerprintLen: %d", teddy.masks.fingerprintLen)

	// Run multiple iterations to catch any inconsistencies
	for iter := 0; iter < 100; iter++ {
		// Call both implementations
		posAVX2, maskAVX2 := teddySlimAVX2_2(teddy.masks, haystack)
		posSSSE3, maskSSSE3 := teddySlimSSSE3_2(teddy.masks, haystack)

		if posAVX2 != posSSSE3 {
			t.Fatalf("Iteration %d: Position mismatch AVX2=%d SSSE3=%d",
				iter, posAVX2, posSSSE3)
		}
		if maskAVX2 != maskSSSE3 {
			t.Fatalf("Iteration %d: Mask mismatch at pos %d: AVX2=0x%02x SSSE3=0x%02x",
				iter, posAVX2, maskAVX2, maskSSSE3)
		}
	}

	pos, mask := teddySlimAVX2_2(teddy.masks, haystack)
	t.Logf("First candidate: pos=%d, mask=0x%02x", pos, mask)
}

// TestSlimTeddyAVX2_NoMatchRegression tests no-match scenario
func TestSlimTeddyAVX2_NoMatchRegression(t *testing.T) {
	if !hasAVX2 || !hasSSSE3 {
		t.Skip("Need both AVX2 and SSSE3")
	}

	patterns := [][]byte{
		[]byte("error"), []byte("warning"), []byte("critical"),
	}

	config := &TeddyConfig{
		MinPatterns:    2,
		MaxPatterns:    32,
		MinPatternLen:  3,
		FingerprintLen: 2,
	}

	teddy := NewTeddy(patterns, config)
	if teddy == nil {
		t.Fatal("Failed to create Teddy")
	}

	// Haystack with NO matches
	haystack := make([]byte, 64*1024)
	for i := range haystack {
		haystack[i] = byte('a' + (i % 26)) // letters a-z repeating
	}

	for iter := 0; iter < 100; iter++ {
		posAVX2, maskAVX2 := teddySlimAVX2_2(teddy.masks, haystack)
		posSSSE3, maskSSSE3 := teddySlimSSSE3_2(teddy.masks, haystack)

		if posAVX2 != posSSSE3 {
			t.Fatalf("No-match iteration %d: Position mismatch AVX2=%d SSSE3=%d",
				iter, posAVX2, posSSSE3)
		}
		if maskAVX2 != maskSSSE3 {
			t.Fatalf("No-match iteration %d: Mask mismatch: AVX2=0x%02x SSSE3=0x%02x",
				iter, maskAVX2, maskSSSE3)
		}
	}

	posNoMatch, _ := teddySlimAVX2_2(teddy.masks, haystack)
	t.Logf("No-match test: pos=%d (expected -1)", posNoMatch)
}

// BenchmarkSlimTeddyDirect benchmarks raw SIMD functions without Find overhead
func BenchmarkSlimTeddyDirect(b *testing.B) {
	patterns := [][]byte{
		[]byte("error"), []byte("warning"), []byte("critical"),
		[]byte("fatal"), []byte("debug"), []byte("info"),
		[]byte("trace"), []byte("notice"), []byte("alert"),
		[]byte("emergency"), []byte("panic"), []byte("exception"),
		[]byte("failure"), []byte("timeout"), []byte("refused"),
	}

	config := &TeddyConfig{
		MinPatterns:    2,
		MaxPatterns:    32,
		MinPatternLen:  3,
		FingerprintLen: 2,
	}

	teddy := NewTeddy(patterns, config)
	if teddy == nil {
		b.Fatal("Failed to create Teddy")
	}

	// No-match haystack (worst case - must scan entire buffer)
	haystack := make([]byte, 64*1024)
	for i := range haystack {
		haystack[i] = 'x'
	}

	b.Run("AVX2_NoMatch_64KB", func(b *testing.B) {
		if !hasAVX2 {
			b.Skip("AVX2 not available")
		}
		b.SetBytes(int64(len(haystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			teddySlimAVX2_2(teddy.masks, haystack)
		}
	})

	b.Run("SSSE3_NoMatch_64KB", func(b *testing.B) {
		if !hasSSSE3 {
			b.Skip("SSSE3 not available")
		}
		b.SetBytes(int64(len(haystack)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			teddySlimSSSE3_2(teddy.masks, haystack)
		}
	})
}
