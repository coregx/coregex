//go:build !amd64

package simd

// MemchrDigit returns the index of the first ASCII digit [0-9] in haystack,
// or -1 if no digit is found.
//
// On non-AMD64 platforms, this function uses an optimized pure Go implementation
// that checks if each byte is in the range ['0'-'9'].
//
// Performance characteristics (pure Go):
//   - Small inputs (< 8 bytes): byte-by-byte comparison
//   - Medium/large inputs: uses SWAR technique for better performance
func MemchrDigit(haystack []byte) int {
	return memchrDigitGeneric(haystack)
}

// MemchrDigitAt returns the index of the first ASCII digit [0-9] at or after
// position 'at' in haystack, or -1 if no digit is found.
func MemchrDigitAt(haystack []byte, at int) int {
	if at < 0 || at >= len(haystack) {
		return -1
	}

	pos := memchrDigitGeneric(haystack[at:])
	if pos < 0 {
		return -1
	}
	return pos + at
}
