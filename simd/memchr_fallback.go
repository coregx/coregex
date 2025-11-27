//go:build !amd64

package simd

// Memchr returns the index of the first instance of needle in haystack,
// or -1 if needle is not present in haystack.
//
// On non-AMD64 platforms, this function uses an optimized pure Go implementation
// with SWAR (SIMD Within A Register) technique, which processes 8 bytes at a time
// using uint64 bitwise operations.
//
// Performance characteristics (pure Go SWAR):
//   - Small inputs (< 8 bytes): byte-by-byte comparison
//   - Medium/large inputs: 2-5x faster than naive byte-by-byte
//   - Not as fast as AVX2, but significantly better than simple loops
//
// See memchrGeneric for implementation details.
func Memchr(haystack []byte, needle byte) int {
	return memchrGeneric(haystack, needle)
}

// Memchr2 returns the index of the first instance of either needle1 or needle2
// in haystack, or -1 if neither is present.
//
// On non-AMD64 platforms, this function uses pure Go SWAR technique to check
// both needles in parallel within 8-byte chunks.
//
// The function returns the position of whichever needle appears first in haystack.
func Memchr2(haystack []byte, needle1, needle2 byte) int {
	return memchr2Generic(haystack, needle1, needle2)
}

// Memchr3 returns the index of the first instance of needle1, needle2, or needle3
// in haystack, or -1 if none are present.
//
// On non-AMD64 platforms, this function uses pure Go SWAR technique to check
// all three needles in parallel within 8-byte chunks.
//
// The function returns the position of whichever needle appears first in haystack.
func Memchr3(haystack []byte, needle1, needle2, needle3 byte) int {
	return memchr3Generic(haystack, needle1, needle2, needle3)
}
