//go:build !amd64

package prefilter

// findSIMD falls back to scalar search on non-x86-64 platforms.
//
// Fat Teddy requires AVX2 which is only available on x86-64.
// On ARM, RISC-V, etc., use scalar implementation.
//
// Future: ARM NEON implementation possible (Go 1.26+ SIMD support)
func (t *FatTeddy) findSIMD(haystack []byte) (pos int, bucketMask uint16) {
	return t.findScalarCandidate(haystack)
}
