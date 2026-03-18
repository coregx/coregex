//go:build amd64

package prefilter

import (
	"fmt"
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

// TestFatTeddyAVX2_SingleLaneBuckets reproduces the AND vs OR bug.
//
// Root cause: fatTeddyAVX2_2 used ANDL to combine low/high lane masks.
// In Fat Teddy, each bucket maps to exactly ONE lane: buckets 0-7 in the low
// 128-bit lane, buckets 8-15 in the high lane. Patterns with bucket IDs only
// in 0-7 produce zero in the high lane, so AND always yields zero = missed.
// Fix: use ORL (candidates exist in EITHER lane).
//
// Reproduction: 40 patterns (case-folded GET/POST/PUT). GET has 8 variants
// at indices 0-7, all in buckets 0-7 (low lane only). AND zeroed them out.
func TestFatTeddyAVX2_SingleLaneBuckets(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	// Create 40 patterns where some are ONLY in low-lane buckets (0-7)
	// and some span both lanes. This simulates (?i)get|post|put.
	//
	// Patterns 0-7 -> buckets 0-7 (low lane only)
	// Patterns 8-15 -> buckets 8-15 (high lane only)
	// Patterns 16-23 -> buckets 0-7 (low lane)
	// Patterns 24-31 -> buckets 8-15 (high lane)
	// Patterns 32-39 -> buckets 0-7 (low lane)

	// Use recognizable 3-byte patterns
	patterns := make([][]byte, 40)
	// GET case-fold variants (8 patterns, indices 0-7, ALL in low lane)
	patterns[0] = []byte("GET")
	patterns[1] = []byte("GEt")
	patterns[2] = []byte("GeT")
	patterns[3] = []byte("Get")
	patterns[4] = []byte("gET")
	patterns[5] = []byte("gEt")
	patterns[6] = []byte("geT")
	patterns[7] = []byte("get")
	// POST case-fold variants (16 patterns, indices 8-23, span BOTH lanes)
	caseVariants := []string{
		"POST", "POSt", "POsT", "POst",
		"PoST", "PoSt", "PosT", "Post",
		"pOST", "pOSt", "pOsT", "pOst",
		"poST", "poSt", "posT", "post",
	}
	for i, v := range caseVariants {
		patterns[8+i] = []byte(v)
	}
	// PUT case-fold variants (8 patterns, indices 24-31)
	patterns[24] = []byte("PUT")
	patterns[25] = []byte("PUt")
	patterns[26] = []byte("PuT")
	patterns[27] = []byte("Put")
	patterns[28] = []byte("pUT")
	patterns[29] = []byte("pUt")
	patterns[30] = []byte("puT")
	patterns[31] = []byte("put")
	// Fill remaining with dummy patterns (indices 32-39)
	for i := 32; i < 40; i++ {
		patterns[i] = []byte(fmt.Sprintf("xx%d", i))
	}

	ft := NewFatTeddy(patterns, nil)
	if ft == nil {
		t.Fatal("NewFatTeddy returned nil")
	}

	// Verify bucket assignments
	t.Logf("Bucket assignments:")
	for i, bucket := range ft.buckets {
		if len(bucket) > 0 {
			t.Logf("  Bucket %d (lane %s): patterns %v", i,
				map[bool]string{true: "LOW", false: "HIGH"}[i < 8], bucket)
		}
	}

	// Test: GET must be found (was missed with ANDL bug)
	haystack := []byte("xxxxxxxxxxxxxxxxxxGETxxxxxxxxxxxxxxxxxx")
	pos := ft.Find(haystack, 0)
	if pos != 18 {
		t.Errorf("GET: expected pos=18, got %d", pos)
	}

	// Test: get must be found
	haystack2 := []byte("xxxxxxxxxxxxxxxxxxgetxxxxxxxxxxxxxxxxxx")
	pos2 := ft.Find(haystack2, 0)
	if pos2 != 18 {
		t.Errorf("get: expected pos=18, got %d", pos2)
	}

	// Test: POST must be found
	haystack3 := []byte("xxxxxxxxxxxxxxxxxxPOSTxxxxxxxxxxxxxxxx")
	pos3 := ft.Find(haystack3, 0)
	if pos3 != 18 {
		t.Errorf("POST: expected pos=18, got %d", pos3)
	}

	// Test: PUT must be found
	haystack4 := []byte("xxxxxxxxxxxxxxxxxxPUTxxxxxxxxxxxxxxxxxx")
	pos4 := ft.Find(haystack4, 0)
	if pos4 != 18 {
		t.Errorf("PUT: expected pos=18, got %d", pos4)
	}

	// Test FindAll-style iteration: count all matches
	data := []byte("GET some POST data PUT more get stuff post here put there")
	count := 0
	at := 0
	for at < len(data) {
		p := ft.Find(data, at)
		if p == -1 {
			break
		}
		count++
		at = p + 1
	}
	// Expected: GET, POST, PUT, get, post, put = 6
	if count != 6 {
		t.Errorf("FindAll count: expected 6, got %d", count)
	}

	// Test with SIMD directly: patterns only in low lane
	lowOnlyHaystack := []byte("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxGETxxxxx")
	pos5, mask5 := fatTeddyAVX2_2(ft.masks, lowOnlyHaystack)
	t.Logf("Direct SIMD for GET: pos=%d, mask=0x%04x", pos5, mask5)
	if pos5 == -1 {
		t.Error("Direct SIMD failed to find GET (low-lane-only pattern)")
	}
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
