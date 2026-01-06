//go:build !amd64

package prefilter

// findSIMD performs candidate search using pure Go implementation.
//
// On non-AMD64 platforms, we don't have SSSE3 assembly, so we use the scalar
// fallback implementation. This is slower (~100x) but functionally identical.
//
// Future: Implement NEON version for ARM64 platforms.
//
// Returns (position, bucketMask) or (-1, 0) if no candidate found.
// bucketMask contains bits for ALL matching buckets (not just first).
func (t *Teddy) findSIMD(haystack []byte) (pos int, bucketMask uint8) {
	// No SIMD available on this platform, use scalar fallback
	return t.findScalarCandidate(haystack)
}
