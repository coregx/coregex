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
