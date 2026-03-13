//go:build amd64 && !goexperiment.simd

package prefilter

// findSIMD performs SIMD search for candidate positions using hand-written assembly.
//
// This dispatches to the appropriate SSSE3 assembly implementation based on
// fingerprint length and CPU capabilities.
//
// Platform support (in order of preference):
//   - x86-64 with SSSE3: use teddySlimSSSE3_1/2 (16 bytes/iteration)
//   - x86-64 without SSSE3: fallback to findScalarCandidate
//
// Note: AVX2 Slim Teddy (teddySlimAVX2_1/2) processes 32 bytes/iteration
// (2x throughput vs SSSE3) but is 4x SLOWER on AMD EPYC in regex-bench due
// to VZEROUPPER overhead. Each findSIMD() call crosses Go/assembly boundary
// and pays ~35 cycles for VZEROUPPER on return. With frequent verification
// restarts (literal_alt: 18.09ms AVX2 vs 4.32ms SSSE3), the per-call
// overhead dominates. Rust avoids this by inlining the entire find+verify
// loop, eliminating function call boundaries.
//
// The archsimd path (teddy_archsimd_amd64.go) eliminates this overhead
// by keeping the entire search loop in Go with compiler intrinsics.
//
// Returns (position, bucketMask) or (-1, 0) if no candidate found.
// bucketMask contains bits for ALL matching buckets (not just first).
// Caller should iterate through all set bits using bits.TrailingZeros8.
func (t *Teddy) findSIMD(haystack []byte) (pos int, bucketMask uint8) {
	fpLen := int(t.masks.fingerprintLen)

	if hasSSSE3 {
		switch fpLen {
		case 1:
			return teddySlimSSSE3_1(t.masks, haystack)
		case 2:
			return teddySlimSSSE3_2(t.masks, haystack)
		}
	}

	// No SIMD support, use scalar fallback
	return t.findScalarCandidate(haystack)
}
