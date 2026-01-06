//go:build amd64

package prefilter

import (
	"golang.org/x/sys/cpu"
)

// CPU feature detection
var (
	// hasSSSE3 indicates if CPU supports SSSE3 instruction set (2006+)
	// Required for Slim Teddy (8 buckets, 128-bit vectors)
	hasSSSE3 = cpu.X86.HasSSSE3

	// hasAVX2 indicates if CPU supports AVX2 instruction set (2013+)
	// Required for Fat Teddy (16 buckets, 256-bit vectors)
	hasAVX2 = cpu.X86.HasAVX2
)

// teddySlimSSSE3_1 is the SSSE3 assembly implementation for 1-byte fingerprint.
//
// This is implemented in teddy_ssse3_amd64.s and provides ~20-50x speedup
// over naive multi-pattern search on CPUs with SSSE3 support (2006+).
//
// Parameters:
//
//	masks - pointer to teddyMasks struct containing nibble lookup tables
//	haystack - the byte slice to search
//
// Returns:
//
//	pos - position of first candidate (relative to haystack start), or -1
//	bucketMask - bitmask of matching buckets (bits 0-7), or 0 if not found
//	            Caller should iterate through all set bits using bits.TrailingZeros8.
//	            This matches Rust's aho-corasick verify64() approach.
//
//go:noescape
func teddySlimSSSE3_1(masks *teddyMasks, haystack []byte) (pos int, bucketMask uint8)

// teddySlimSSSE3_2 is the SSSE3 assembly implementation for 2-byte fingerprint.
//
// This reduces false positives by ~90% compared to 1-byte fingerprint because
// it checks two consecutive bytes instead of one.
//
// Algorithm:
//  1. Load masks for position 0 and position 1
//  2. For each 16-byte chunk:
//     - Process position 0: lookup nibbles in loMasks[0]/hiMasks[0]
//     - Process position 1: lookup nibbles in loMasks[1]/hiMasks[1] (overlapping load)
//     - AND results from both positions
//     - Non-zero result = candidate
//
// Returns bucketMask (bitmask of all matching buckets), not bucket ID.
//
//go:noescape
func teddySlimSSSE3_2(masks *teddyMasks, haystack []byte) (pos int, bucketMask uint8)

// findSIMD performs SIMD search for candidate positions.
//
// This method overrides the generic implementation in teddy.go when SSSE3 is available.
// It dispatches to the appropriate SIMD implementation based on fingerprint length
// and CPU capabilities.
//
// Platform support:
//   - x86-64 with SSSE3: use teddySlimSSSE3_1 (16 bytes/iteration)
//   - x86-64 without SSSE3: fallback to findScalarCandidate
//   - Other platforms: fallback (via build tags)
//
// Returns (position, bucketMask) or (-1, 0) if no candidate found.
// bucketMask contains bits for ALL matching buckets (not just first).
// Caller should iterate through all set bits using bits.TrailingZeros8.
func (t *Teddy) findSIMD(haystack []byte) (pos int, bucketMask uint8) {
	// Check CPU support
	if !hasSSSE3 {
		// CPU doesn't support SSSE3, use scalar fallback
		return t.findScalarCandidate(haystack)
	}

	// Check fingerprint length
	fpLen := int(t.masks.fingerprintLen)

	switch fpLen {
	case 1:
		// Use SSSE3 implementation for 1-byte fingerprint
		return teddySlimSSSE3_1(t.masks, haystack)

	case 2:
		// Use SSSE3 implementation for 2-byte fingerprint
		// This reduces false positives by ~90% compared to 1-byte
		return teddySlimSSSE3_2(t.masks, haystack)

	default:
		// 3-4 byte fingerprints not yet implemented in SSSE3
		// Fall back to scalar
		return t.findScalarCandidate(haystack)
	}
}
