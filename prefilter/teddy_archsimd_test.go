//go:build goexperiment.simd && amd64

package prefilter

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// TestArchsimd1_CorrectnessVsScalar verifies that the archsimd 1-byte fingerprint
// implementation produces identical results to the scalar candidate finder.
func TestArchsimd1_CorrectnessVsScalar(t *testing.T) {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	cfg := DefaultTeddyConfig()
	cfg.FingerprintLen = 1
	teddy := NewTeddy(patterns, cfg)
	if teddy == nil {
		t.Fatal("NewTeddy returned nil")
	}

	haystacks := []string{
		"hello foo world bar",
		"no match here at all",
		"foobarbazqux",
		"xxxxxxxxxxxxxxxxxfoo",
		"xxxxxxxxxxxxxxxxxbar",
		"barxxxxxxxxxxxxxxxxx",
		"exactly16bytesXX",
		"exactly17bytesXXX",
		"a]baz[c",
		string(make([]byte, 100)) + "qux",
		string(make([]byte, 1000)) + "foo" + string(make([]byte, 1000)),
	}

	for _, hs := range haystacks {
		h := []byte(hs)
		simdPos, simdMask := teddy.findSIMDArchsimd1(h)
		scalarPos, scalarMask := teddy.findScalarCandidate(h)

		if simdPos != scalarPos || simdMask != scalarMask {
			t.Errorf("haystack %q (len=%d):\n  archsimd1: pos=%d mask=0x%02x\n  scalar:    pos=%d mask=0x%02x",
				truncate(hs, 40), len(h), simdPos, simdMask, scalarPos, scalarMask)
		}
	}
}

// TestArchsimd2_CorrectnessVsScalar verifies that the archsimd 2-byte fingerprint
// implementation produces identical results to the scalar candidate finder.
func TestArchsimd2_CorrectnessVsScalar(t *testing.T) {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	teddy := NewTeddy(patterns, nil) // default: 2-byte fingerprint
	if teddy == nil {
		t.Fatal("NewTeddy returned nil")
	}
	if teddy.masks.fingerprintLen != 2 {
		t.Fatalf("expected 2-byte fingerprint, got %d", teddy.masks.fingerprintLen)
	}

	haystacks := []string{
		"hello foo world bar",
		"no match here at all",
		"foobarbazqux",
		"xxxxxxxxxxxxxxxxxfoo",
		"xxxxxxxxxxxxxxxxxbar",
		"barxxxxxxxxxxxxxxxxx",
		"exactly16bytesXX",
		"exactly17bytesXXX",
		"a]baz[c",
		string(make([]byte, 100)) + "qux",
		string(make([]byte, 1000)) + "foo" + string(make([]byte, 1000)),
	}

	for _, hs := range haystacks {
		h := []byte(hs)
		simdPos, simdMask := teddy.findSIMDArchsimd2(h)
		scalarPos, scalarMask := teddy.findScalarCandidate(h)

		if simdPos != scalarPos || simdMask != scalarMask {
			t.Errorf("haystack %q (len=%d):\n  archsimd2: pos=%d mask=0x%02x\n  scalar:    pos=%d mask=0x%02x",
				truncate(hs, 40), len(h), simdPos, simdMask, scalarPos, scalarMask)
		}
	}
}

// TestArchsimd_EdgeCaseSizes tests boundary haystack sizes that stress the
// SIMD main loop and scalar tail transitions.
func TestArchsimd_EdgeCaseSizes(t *testing.T) {
	patterns := [][]byte{[]byte("abc"), []byte("xyz")}

	for _, fpLen := range []int{1, 2} {
		cfg := DefaultTeddyConfig()
		cfg.FingerprintLen = fpLen
		teddy := NewTeddy(patterns, cfg)
		if teddy == nil {
			t.Fatal("NewTeddy returned nil")
		}

		// Test various sizes around SIMD boundaries
		sizes := []int{0, 1, 2, 3, 15, 16, 17, 31, 32, 33, 48, 63, 64, 100, 255, 256, 1024}

		for _, size := range sizes {
			t.Run(fmt.Sprintf("fp%d/size%d/no_match", fpLen, size), func(t *testing.T) {
				haystack := make([]byte, size)
				for i := range haystack {
					haystack[i] = '.'
				}
				simdPos, simdMask := teddy.findSIMD(haystack)
				scalarPos, scalarMask := teddy.findScalarCandidate(haystack)

				if simdPos != scalarPos || simdMask != scalarMask {
					t.Errorf("no_match size=%d: archsimd pos=%d mask=0x%02x, scalar pos=%d mask=0x%02x",
						size, simdPos, simdMask, scalarPos, scalarMask)
				}
			})

			// Place pattern at various positions within the haystack
			for _, patIdx := range []int{0, 1} {
				pat := patterns[patIdx]
				if size < len(pat) {
					continue
				}
				// Pattern at start
				t.Run(fmt.Sprintf("fp%d/size%d/pat%d_start", fpLen, size, patIdx), func(t *testing.T) {
					haystack := make([]byte, size)
					for i := range haystack {
						haystack[i] = '.'
					}
					copy(haystack[0:], pat)
					simdPos, _ := teddy.findSIMD(haystack)
					scalarPos, _ := teddy.findScalarCandidate(haystack)
					if simdPos != scalarPos {
						t.Errorf("start: archsimd pos=%d, scalar pos=%d", simdPos, scalarPos)
					}
				})
				// Pattern at end
				t.Run(fmt.Sprintf("fp%d/size%d/pat%d_end", fpLen, size, patIdx), func(t *testing.T) {
					haystack := make([]byte, size)
					for i := range haystack {
						haystack[i] = '.'
					}
					copy(haystack[size-len(pat):], pat)
					simdPos, _ := teddy.findSIMD(haystack)
					scalarPos, _ := teddy.findScalarCandidate(haystack)
					if simdPos != scalarPos {
						t.Errorf("end: archsimd pos=%d, scalar pos=%d", simdPos, scalarPos)
					}
				})
			}
		}
	}
}

// TestArchsimd_LargeHaystack tests search in a large (64KB) haystack with
// patterns placed at various positions.
func TestArchsimd_LargeHaystack(t *testing.T) {
	patterns := [][]byte{
		[]byte("ERROR"),
		[]byte("WARN!"),
		[]byte("FATAL"),
	}

	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy returned nil")
	}

	const size = 65536
	positions := []int{0, 1, 15, 16, 17, 100, 1000, 32768, 65530}

	for _, pos := range positions {
		for _, pat := range patterns {
			if pos+len(pat) > size {
				continue
			}
			t.Run(fmt.Sprintf("%s_at_%d", pat, pos), func(t *testing.T) {
				haystack := make([]byte, size)
				for i := range haystack {
					haystack[i] = '.'
				}
				copy(haystack[pos:], pat)

				result := teddy.Find(haystack, 0)
				if result != pos {
					t.Errorf("Find() = %d, want %d", result, pos)
				}
			})
		}
	}
}

// TestArchsimd_HighBitBytes verifies correct nibble extraction for bytes with
// the high bit set (0x80-0xFF). This exercises the unsigned shift path:
// Uint16x8.ShiftAllRight (VPSRLW) vs Int16x8.ShiftAllRight (VPSRAW).
func TestArchsimd_HighBitBytes(t *testing.T) {
	// Patterns containing bytes with high bit set
	patterns := [][]byte{
		{0xC3, 0xA9, 0x63},       // e-acute in UTF-8 + 'c'
		{0xE2, 0x80, 0x99},       // right single quotation mark UTF-8
		{0xF0, 0x9F, 0x98, 0x80}, // emoji: grinning face
	}

	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy returned nil")
	}

	// Build haystack with the pattern embedded
	for i, pat := range patterns {
		t.Run(fmt.Sprintf("pattern_%d", i), func(t *testing.T) {
			haystack := make([]byte, 100)
			for j := range haystack {
				haystack[j] = 0x20 // space
			}
			copy(haystack[50:], pat)

			result := teddy.Find(haystack, 0)
			if result != 50 {
				t.Errorf("Find() = %d, want 50 for high-bit pattern %x", result, pat)
			}
		})
	}
}

// TestArchsimd_FindMatchIntegration tests the full Find+verify pipeline
// to ensure archsimd findSIMD integrates correctly with the verification layer.
func TestArchsimd_FindMatchIntegration(t *testing.T) {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy returned nil")
	}

	tests := []struct {
		haystack string
		wantPos  int
		wantEnd  int
	}{
		{"hello foo world", 6, 9},
		{"test bar test", 5, 8},
		{"baz at start", 0, 3},
		{"end with qux", 9, 12},
		{"no match here", -1, -1},
		{"foobarbazqux", 0, 3},
	}

	for _, tt := range tests {
		t.Run(tt.haystack, func(t *testing.T) {
			start, end := teddy.FindMatch([]byte(tt.haystack), 0)
			if start != tt.wantPos || end != tt.wantEnd {
				t.Errorf("FindMatch(%q) = (%d, %d), want (%d, %d)",
					tt.haystack, start, end, tt.wantPos, tt.wantEnd)
			}
		})
	}
}

// TestArchsimd_FuzzCorrectnessVsScalar runs randomized testing to verify
// archsimd results match the scalar implementation across diverse inputs.
func TestArchsimd_FuzzCorrectnessVsScalar(t *testing.T) {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}

	for _, fpLen := range []int{1, 2} {
		cfg := DefaultTeddyConfig()
		cfg.FingerprintLen = fpLen
		teddy := NewTeddy(patterns, cfg)
		if teddy == nil {
			t.Fatal("NewTeddy returned nil")
		}

		rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic for reproducibility
		const iterations = 5000

		t.Run(fmt.Sprintf("fp%d", fpLen), func(t *testing.T) {
			for i := 0; i < iterations; i++ {
				// Random haystack size: 0-512 bytes
				size := rng.Intn(513)
				haystack := make([]byte, size)
				for j := range haystack {
					haystack[j] = byte(rng.Intn(256))
				}

				// 30% chance: inject a pattern at random position
				if size >= 3 && rng.Float32() < 0.3 {
					pat := patterns[rng.Intn(len(patterns))]
					pos := rng.Intn(size - len(pat) + 1)
					copy(haystack[pos:], pat)
				}

				simdPos, simdMask := teddy.findSIMD(haystack)
				scalarPos, scalarMask := teddy.findScalarCandidate(haystack)

				if simdPos != scalarPos || simdMask != scalarMask {
					t.Fatalf("iteration %d, size=%d:\n  archsimd: pos=%d mask=0x%02x\n  scalar:   pos=%d mask=0x%02x\n  haystack prefix: %x",
						i, size, simdPos, simdMask, scalarPos, scalarMask, truncateBytes(haystack, 64))
				}
			}
		})
	}
}

// TestArchsimd_ManyPatterns tests with larger pattern sets up to the Slim Teddy maximum.
func TestArchsimd_ManyPatterns(t *testing.T) {
	// Build 8 patterns (max per default config)
	patterns := [][]byte{
		[]byte("alpha"),
		[]byte("bravo"),
		[]byte("charm"),
		[]byte("delta"),
		[]byte("eagle"),
		[]byte("foxtx"),
		[]byte("gamma"),
		[]byte("hotel"),
	}

	teddy := NewTeddy(patterns, nil)
	if teddy == nil {
		t.Fatal("NewTeddy returned nil")
	}

	haystack := make([]byte, 4096)
	for i := range haystack {
		haystack[i] = '-'
	}

	// Place each pattern at a different position and verify detection
	for i, pat := range patterns {
		pos := 100 + i*200
		h := make([]byte, len(haystack))
		copy(h, haystack)
		copy(h[pos:], pat)

		result := teddy.Find(h, 0)
		if result != pos {
			t.Errorf("pattern %q at pos %d: Find()=%d", pat, pos, result)
		}
	}
}

// truncate returns a string truncated to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// truncateBytes returns a byte slice truncated to maxLen bytes.
func truncateBytes(b []byte, maxLen int) []byte {
	if len(b) <= maxLen {
		return b
	}
	return b[:maxLen]
}

// TestArchsimdAVX2_1byte_CorrectnessVsScalar verifies that the AVX2 1-byte
// fingerprint produces identical results to the scalar candidate finder.
func TestArchsimdAVX2_1byte_CorrectnessVsScalar(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	cfg := DefaultTeddyConfig()
	cfg.FingerprintLen = 1
	teddy := NewTeddy(patterns, cfg)
	if teddy == nil {
		t.Fatal("NewTeddy returned nil")
	}

	haystacks := []string{
		"hello foo world bar",
		"no match here at all",
		"foobarbazqux",
		"xxxxxxxxxxxxxxxxxfoo",
		"xxxxxxxxxxxxxxxxxbar",
		"barxxxxxxxxxxxxxxxxx",
		"exactly16bytesXX",
		"exactly17bytesXXX",
		"a]baz[c",
		string(make([]byte, 100)) + "qux",
		string(make([]byte, 1000)) + "foo" + string(make([]byte, 1000)),
		// AVX2-specific: match at 32-byte boundaries
		string(bytes.Repeat([]byte{'.'}, 31)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 32)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 33)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 63)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 64)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 65)) + "foo",
	}

	for _, hs := range haystacks {
		h := []byte(hs)
		avxPos, avxMask := teddy.findSIMDArchsimdAVX2_1(h)
		scalarPos, scalarMask := teddy.findScalarCandidate(h)

		if avxPos != scalarPos || avxMask != scalarMask {
			t.Errorf("haystack len=%d:\n  AVX2_1: pos=%d mask=0x%02x\n  scalar: pos=%d mask=0x%02x",
				len(h), avxPos, avxMask, scalarPos, scalarMask)
		}
	}
}

// TestArchsimdAVX2_2byte_CorrectnessVsScalar verifies that the AVX2 2-byte
// fingerprint produces identical results to the scalar candidate finder.
func TestArchsimdAVX2_2byte_CorrectnessVsScalar(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	teddy := NewTeddy(patterns, nil) // default: 2-byte fingerprint
	if teddy == nil {
		t.Fatal("NewTeddy returned nil")
	}

	haystacks := []string{
		"hello foo world bar",
		"no match here at all",
		"foobarbazqux",
		"xxxxxxxxxxxxxxxxxfoo",
		"xxxxxxxxxxxxxxxxxbar",
		"barxxxxxxxxxxxxxxxxx",
		"exactly16bytesXX",
		"exactly17bytesXXX",
		"a]baz[c",
		string(make([]byte, 100)) + "qux",
		string(make([]byte, 1000)) + "foo" + string(make([]byte, 1000)),
		// AVX2-specific: match at 32-byte boundaries
		string(bytes.Repeat([]byte{'.'}, 31)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 32)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 33)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 63)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 64)) + "foo",
		string(bytes.Repeat([]byte{'.'}, 65)) + "foo",
	}

	for _, hs := range haystacks {
		h := []byte(hs)
		avxPos, avxMask := teddy.findSIMDArchsimdAVX2_2(h)
		scalarPos, scalarMask := teddy.findScalarCandidate(h)

		if avxPos != scalarPos || avxMask != scalarMask {
			t.Errorf("haystack len=%d:\n  AVX2_2: pos=%d mask=0x%02x\n  scalar: pos=%d mask=0x%02x",
				len(h), avxPos, avxMask, scalarPos, scalarMask)
		}
	}
}

// TestArchsimdAVX2_EdgeCases tests AVX2 with sizes that stress the 32-byte
// main loop, 16-byte SSSE3 tail, and scalar tail transitions.
func TestArchsimdAVX2_EdgeCases(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	patterns := [][]byte{[]byte("abc"), []byte("xyz")}

	for _, fpLen := range []int{1, 2} {
		cfg := DefaultTeddyConfig()
		cfg.FingerprintLen = fpLen
		teddy := NewTeddy(patterns, cfg)
		if teddy == nil {
			t.Fatal("NewTeddy returned nil")
		}

		// Sizes chosen to stress AVX2 boundaries (32-byte aligned)
		sizes := []int{0, 1, 2, 3, 15, 16, 17, 31, 32, 33, 47, 48, 49, 63, 64, 65, 95, 96, 97, 128}

		for _, size := range sizes {
			t.Run(fmt.Sprintf("fp%d/size%d/no_match", fpLen, size), func(t *testing.T) {
				haystack := bytes.Repeat([]byte{'.'}, size)
				simdPos, simdMask := teddy.findSIMD(haystack)
				scalarPos, scalarMask := teddy.findScalarCandidate(haystack)

				if simdPos != scalarPos || simdMask != scalarMask {
					t.Errorf("no_match size=%d: SIMD pos=%d mask=0x%02x, scalar pos=%d mask=0x%02x",
						size, simdPos, simdMask, scalarPos, scalarMask)
				}
			})

			for _, patIdx := range []int{0, 1} {
				pat := patterns[patIdx]
				if size < len(pat) {
					continue
				}
				// Pattern at end (stresses tail handling)
				t.Run(fmt.Sprintf("fp%d/size%d/pat%d_end", fpLen, size, patIdx), func(t *testing.T) {
					haystack := bytes.Repeat([]byte{'.'}, size)
					copy(haystack[size-len(pat):], pat)
					simdPos, _ := teddy.findSIMD(haystack)
					scalarPos, _ := teddy.findScalarCandidate(haystack)
					if simdPos != scalarPos {
						t.Errorf("end: SIMD pos=%d, scalar pos=%d", simdPos, scalarPos)
					}
				})
			}
		}
	}
}

// TestArchsimdAVX2_LargeHaystack tests AVX2 search in a 64KB haystack with
// patterns at positions that span multiple 32-byte chunks.
func TestArchsimdAVX2_LargeHaystack(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	patterns := [][]byte{
		[]byte("ERROR"),
		[]byte("WARN!"),
		[]byte("FATAL"),
	}

	for _, fpLen := range []int{1, 2} {
		cfg := DefaultTeddyConfig()
		cfg.FingerprintLen = fpLen
		teddy := NewTeddy(patterns, cfg)
		if teddy == nil {
			t.Fatal("NewTeddy returned nil")
		}

		const size = 65536
		// Test positions at 32-byte boundaries and off-by-one
		positions := []int{0, 1, 15, 16, 17, 31, 32, 33, 63, 64, 65, 1000, 32768, 65530}

		for _, pos := range positions {
			for _, pat := range patterns {
				if pos+len(pat) > size {
					continue
				}
				t.Run(fmt.Sprintf("fp%d/%s_at_%d", fpLen, pat, pos), func(t *testing.T) {
					haystack := bytes.Repeat([]byte{'.'}, size)
					copy(haystack[pos:], pat)

					result := teddy.Find(haystack, 0)
					if result != pos {
						t.Errorf("Find() = %d, want %d", result, pos)
					}
				})
			}
		}
	}
}

// TestArchsimdAVX2_HighBitBytes verifies AVX2 unsigned shift correctness
// for bytes 0x80-0xFF. The VPSRLW (unsigned) vs VPSRAW (signed) distinction
// is critical for correct high nibble extraction in the AVX2 path.
func TestArchsimdAVX2_HighBitBytes(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	// Patterns with bytes >= 0x80 in various positions
	patterns := [][]byte{
		{0xC3, 0xA9, 0x63},       // UTF-8 e-acute + 'c'
		{0xE2, 0x80, 0x99},       // UTF-8 right single quotation mark
		{0xF0, 0x9F, 0x98, 0x80}, // UTF-8 grinning face emoji
	}

	for _, fpLen := range []int{1, 2} {
		cfg := DefaultTeddyConfig()
		cfg.FingerprintLen = fpLen
		teddy := NewTeddy(patterns, cfg)
		if teddy == nil {
			t.Fatal("NewTeddy returned nil")
		}

		for i, pat := range patterns {
			// Place pattern in second 32-byte chunk to exercise AVX2 specifically
			t.Run(fmt.Sprintf("fp%d/pattern_%d", fpLen, i), func(t *testing.T) {
				haystack := bytes.Repeat([]byte{0x20}, 100)
				copy(haystack[50:], pat)

				result := teddy.Find(haystack, 0)
				if result != 50 {
					t.Errorf("Find() = %d, want 50 for high-bit pattern %x", result, pat)
				}
			})
		}
	}
}

// TestArchsimdAVX2_vs_SSSE3 verifies that AVX2 and SSSE3 produce identical
// results across randomized inputs.
func TestArchsimdAVX2_vs_SSSE3(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}

	rng := rand.New(rand.NewSource(99)) //nolint:gosec // deterministic for reproducibility

	for _, fpLen := range []int{1, 2} {
		cfg := DefaultTeddyConfig()
		cfg.FingerprintLen = fpLen
		teddy := NewTeddy(patterns, cfg)
		if teddy == nil {
			t.Fatal("NewTeddy returned nil")
		}

		t.Run(fmt.Sprintf("fp%d", fpLen), func(t *testing.T) {
			for i := 0; i < 5000; i++ {
				size := rng.Intn(513)
				haystack := make([]byte, size)
				for j := range haystack {
					haystack[j] = byte(rng.Intn(256))
				}

				// 30% chance: inject a pattern
				if size >= 3 && rng.Float32() < 0.3 {
					pat := patterns[rng.Intn(len(patterns))]
					pos := rng.Intn(size - len(pat) + 1)
					copy(haystack[pos:], pat)
				}

				var avxPos int
				var avxMask uint8
				var ssse3Pos int
				var ssse3Mask uint8

				if fpLen == 1 {
					avxPos, avxMask = teddy.findSIMDArchsimdAVX2_1(haystack)
					ssse3Pos, ssse3Mask = teddy.findSIMDArchsimd1(haystack)
				} else {
					avxPos, avxMask = teddy.findSIMDArchsimdAVX2_2(haystack)
					ssse3Pos, ssse3Mask = teddy.findSIMDArchsimd2(haystack)
				}

				if avxPos != ssse3Pos || avxMask != ssse3Mask {
					t.Fatalf("iteration %d, size=%d:\n  AVX2:  pos=%d mask=0x%02x\n  SSSE3: pos=%d mask=0x%02x",
						i, size, avxPos, avxMask, ssse3Pos, ssse3Mask)
				}
			}
		})
	}
}

// BenchmarkTeddy_Archsimd_AVX2_vs_SSSE3 compares AVX2 32-byte/iter vs SSSE3
// 16-byte/iter throughput for both fingerprint lengths.
func BenchmarkTeddy_Archsimd_AVX2_vs_SSSE3(b *testing.B) {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
		[]byte("qux"),
	}

	sizes := []int{256, 1024, 4096, 65536}

	for _, fpLen := range []int{1, 2} {
		cfg := DefaultTeddyConfig()
		cfg.FingerprintLen = fpLen
		teddy := NewTeddy(patterns, cfg)
		if teddy == nil {
			b.Fatal("NewTeddy returned nil")
		}

		for _, size := range sizes {
			// No-match haystack: exercises full scan
			haystack := bytes.Repeat([]byte{'x'}, size)

			b.Run(fmt.Sprintf("fp%d/AVX2/%dB", fpLen, size), func(b *testing.B) {
				if !hasAVX2 {
					b.Skip("AVX2 not available")
				}
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if fpLen == 1 {
						teddy.findSIMDArchsimdAVX2_1(haystack)
					} else {
						teddy.findSIMDArchsimdAVX2_2(haystack)
					}
				}
			})

			b.Run(fmt.Sprintf("fp%d/SSSE3/%dB", fpLen, size), func(b *testing.B) {
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if fpLen == 1 {
						teddy.findSIMDArchsimd1(haystack)
					} else {
						teddy.findSIMDArchsimd2(haystack)
					}
				}
			})
		}
	}
}

// FuzzArchsimd_CorrectnessVsScalar is a Go fuzz target for continuous
// correctness validation of archsimd against scalar.
// Tests both 1-byte and 2-byte fingerprint within a single f.Fuzz call.
func FuzzArchsimd_CorrectnessVsScalar(f *testing.F) {
	patterns := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}

	cfg1 := DefaultTeddyConfig()
	cfg1.FingerprintLen = 1
	teddy1 := NewTeddy(patterns, cfg1)

	cfg2 := DefaultTeddyConfig()
	cfg2.FingerprintLen = 2
	teddy2 := NewTeddy(patterns, cfg2)

	if teddy1 == nil || teddy2 == nil {
		f.Fatal("NewTeddy returned nil")
	}

	// Seed corpus
	f.Add([]byte("hello foo world"))
	f.Add([]byte("no match"))
	f.Add([]byte(""))
	f.Add([]byte("f"))
	f.Add([]byte("fo"))
	f.Add([]byte("foobarfoobar"))
	f.Add(make([]byte, 16))
	f.Add(make([]byte, 17))
	f.Add(make([]byte, 32))
	f.Add(bytes.Repeat([]byte{0xFF}, 64))

	f.Fuzz(func(t *testing.T, haystack []byte) {
		// Test 1-byte fingerprint
		simdPos1, simdMask1 := teddy1.findSIMD(haystack)
		scalarPos1, scalarMask1 := teddy1.findScalarCandidate(haystack)
		if simdPos1 != scalarPos1 || simdMask1 != scalarMask1 {
			t.Errorf("fp1 haystack len=%d:\n  archsimd: pos=%d mask=0x%02x\n  scalar:   pos=%d mask=0x%02x",
				len(haystack), simdPos1, simdMask1, scalarPos1, scalarMask1)
		}

		// Test 2-byte fingerprint
		simdPos2, simdMask2 := teddy2.findSIMD(haystack)
		scalarPos2, scalarMask2 := teddy2.findScalarCandidate(haystack)
		if simdPos2 != scalarPos2 || simdMask2 != scalarMask2 {
			t.Errorf("fp2 haystack len=%d:\n  archsimd: pos=%d mask=0x%02x\n  scalar:   pos=%d mask=0x%02x",
				len(haystack), simdPos2, simdMask2, scalarPos2, scalarMask2)
		}
	})
}
