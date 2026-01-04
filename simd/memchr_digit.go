package simd

// MemchrDigit returns the index of the first ASCII digit [0-9] in haystack,
// or -1 if no digit is found.
//
// This function uses SIMD acceleration on AMD64 with AVX2, processing 32 bytes
// per iteration using parallel range comparison. Falls back to an optimized
// scalar loop on other architectures or when AVX2 is not available.
//
// The implementation uses VPCMPGTB to perform range checking:
//  1. Check if byte > 0x2F ('0' - 1)
//  2. Check if byte <= 0x39 ('9')
//  3. AND the results to get bytes in range ['0'-'9']
//
// Performance characteristics (vs byte-by-byte scanning):
//   - 64 bytes: 2-3x faster
//   - 4KB: 8-10x faster
//   - 64KB: 15-20x faster
//
// This function is particularly useful for:
//   - IP address pattern matching (scanning for first digit)
//   - Numeric field extraction from text
//   - Fast validation of alphanumeric data
//
// Example:
//
//	haystack := []byte("hello 123 world")
//	pos := simd.MemchrDigit(haystack) // returns 6 (index of '1')
//
// Example with no digits:
//
//	haystack := []byte("hello world")
//	pos := simd.MemchrDigit(haystack) // returns -1
//
// Example with IP-like pattern:
//
//	haystack := []byte("Server at 192.168.1.1 is up")
//	pos := simd.MemchrDigit(haystack) // returns 10 (index of '1' in IP)
//
// Note: This function only searches for ASCII digits 0-9 (bytes 0x30-0x39).
// It does not match Unicode digit characters.

// MemchrDigitAt returns the index of the first ASCII digit [0-9] at or after
// position 'at' in haystack, or -1 if no digit is found.
//
// This is equivalent to MemchrDigit(haystack[at:]) but returns the absolute
// index in the original haystack (not relative to 'at').
//
// Parameters:
//   - haystack: the byte slice to search
//   - at: the starting position for the search (inclusive)
//
// Returns the absolute index of the first digit found, or -1 if not found
// or if 'at' is out of bounds.
//
// Example:
//
//	haystack := []byte("abc123def456")
//	pos := simd.MemchrDigitAt(haystack, 0)  // returns 3
//	pos = simd.MemchrDigitAt(haystack, 5)   // returns 5 (the '3')
//	pos = simd.MemchrDigitAt(haystack, 6)   // returns 9 (the '4')
//	pos = simd.MemchrDigitAt(haystack, 12)  // returns -1 (out of bounds)
