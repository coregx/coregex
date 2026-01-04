//go:build amd64

package simd

// Assembly function declaration for AVX2 digit search implementation.
// This is implemented in memchr_digit_amd64.s and uses 256-bit vector operations
// with range comparison to find ASCII digits [0-9].
//
//go:noescape
func memchrDigitAVX2(haystack []byte) int

// MemchrDigit returns the index of the first ASCII digit [0-9] in haystack,
// or -1 if no digit is found.
//
// On AMD64 with AVX2 support and haystack >= 32 bytes, uses SIMD acceleration.
// Otherwise falls back to optimized scalar loop.
func MemchrDigit(haystack []byte) int {
	if len(haystack) == 0 {
		return -1
	}

	// Use AVX2 implementation if available and input is large enough.
	// For small inputs (< 32 bytes), the setup cost of SIMD outweighs the benefits.
	if hasAVX2 && len(haystack) >= 32 {
		return memchrDigitAVX2(haystack)
	}

	// Fallback to generic implementation for small inputs or non-AVX2 CPUs
	return memchrDigitGeneric(haystack)
}

// MemchrDigitAt returns the index of the first ASCII digit [0-9] at or after
// position 'at' in haystack, or -1 if no digit is found.
func MemchrDigitAt(haystack []byte, at int) int {
	if at < 0 || at >= len(haystack) {
		return -1
	}

	pos := MemchrDigit(haystack[at:])
	if pos < 0 {
		return -1
	}
	return pos + at
}
