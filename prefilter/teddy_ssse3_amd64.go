//go:build amd64

package prefilter

import (
	"golang.org/x/sys/cpu"
)

// CPU feature detection
var (
	// hasSSSE3 indicates if CPU supports SSSE3 instruction set
	hasSSSE3 = cpu.X86.HasSSSE3
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
//	bucket - bucket ID (0-7) containing matching patterns, or -1
//
//go:noescape
func teddySlimSSSE3_1(masks *teddyMasks, haystack []byte) (pos, bucket int)

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
// Returns (position, bucket_id) or (-1, -1) if no candidate found.
func (t *Teddy) findSIMD(haystack []byte) (pos, bucket int) {
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

	default:
		// Multi-byte fingerprints not yet implemented in SSSE3
		// Fall back to scalar (TODO: implement 2-4 byte fingerprints)
		return t.findScalarCandidate(haystack)
	}
}
