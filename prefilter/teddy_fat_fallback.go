//go:build !amd64

package prefilter

// hasAVX2 is always false on non-x86-64 platforms.
var hasAVX2 = false

// findSIMD falls back to scalar search on non-x86-64 platforms.
func (t *FatTeddy) findSIMD(haystack []byte) (pos int, bucketMask uint16) {
	return t.findScalarCandidate(haystack)
}

// fatTeddyAVX2_2Batch is unavailable on non-x86-64 platforms.
func fatTeddyAVX2_2Batch(_ *fatTeddyMasks, _ []byte, _ []uint64) int {
	return 0
}
