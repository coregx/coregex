//go:build amd64

package prefilter

// fatTeddyAVX2_2 is the AVX2 assembly implementation for 2-byte fingerprint.
//
// This is implemented in teddy_avx2_amd64.s and provides optimal performance
// for 33-64 patterns on AVX2-capable CPUs.
//
// Algorithm:
//  1. VBROADCASTI128: Load 16 bytes, duplicate to both 128-bit lanes
//  2. Extract low/high nibbles with VPAND
//  3. VPSHUFB: Lookup bucket bits in masks
//  4. VPAND: Combine results from both fingerprint positions
//  5. VPERM2I128 + VPUNPCKLBW: Interleave to get 16-bit bucket mask
//  6. VPMOVMSKB: Extract candidate mask
//
// Returns:
//
//	pos - position of first candidate (relative to haystack start), or -1
//	bucketMask - 16-bit bitmask of matching buckets, or 0 if not found
//
//go:noescape
func fatTeddyAVX2_2(masks *fatTeddyMasks, haystack []byte) (pos int, bucketMask uint16)

// findSIMD performs AVX2 search for candidate positions.
//
// This method dispatches to the appropriate SIMD implementation:
//   - AVX2 available: use fatTeddyAVX2_2 (256-bit vectors)
//   - No AVX2: fallback to findScalarCandidate
//
// Returns (position, bucketMask) or (-1, 0) if no candidate found.
// bucketMask is 16-bit (16 buckets).
func (t *FatTeddy) findSIMD(haystack []byte) (pos int, bucketMask uint16) {
	// Check CPU support (hasAVX2 is defined in teddy_ssse3_amd64.go)
	if !hasAVX2 {
		return t.findScalarCandidate(haystack)
	}

	// Check fingerprint length
	fpLen := int(t.masks.fingerprintLen)

	switch fpLen {
	case 2:
		// Use AVX2 implementation for 2-byte fingerprint
		return fatTeddyAVX2_2(t.masks, haystack)

	default:
		// 1, 3, 4 byte fingerprints not yet implemented in AVX2
		// Fall back to scalar
		return t.findScalarCandidate(haystack)
	}
}
