//go:build amd64

package simd

// Assembly function declarations for AVX2 ASCII detection.
// These are implemented in ascii_amd64.s and use 256-bit vector operations.
//
//go:noescape
func isASCIIAVX2(data []byte) bool

// IsASCII checks if all bytes in the slice are ASCII (< 0x80).
// Returns true if all bytes have the high bit clear (values 0x00-0x7F).
//
// This function is critical for UTF-8 optimization in the regex engine.
// When input is ASCII-only, the engine can skip UTF-8 decoding overhead
// and use simplified automata (1 NFA state for '.' instead of 39).
//
// Performance characteristics (on x86-64 with AVX2):
//   - Small inputs (< 32 bytes): uses optimized SWAR (8 bytes at a time)
//   - Medium inputs (32B - 4KB): 10-15 GB/s throughput
//   - Large inputs (> 4KB): 20-30 GB/s throughput (memory bandwidth limited)
//
// Algorithm (AVX2):
//  1. Load 32 bytes into YMM register
//  2. Use VPMOVMSKB to extract high bit of each byte
//  3. If mask != 0, non-ASCII byte found
//  4. Handle tail with scalar loop
//
// Example:
//
//	data := []byte("hello world")
//	if simd.IsASCII(data) {
//	    // Use ASCII-optimized path
//	}
//
// Example with non-ASCII:
//
//	data := []byte("héllo")
//	if !simd.IsASCII(data) {
//	    // Use UTF-8 path
//	}
func IsASCII(data []byte) bool {
	// Empty slice is trivially ASCII
	if len(data) == 0 {
		return true
	}

	// Use AVX2 implementation if available and input is large enough.
	// For small inputs (< 32 bytes), the setup cost of SIMD outweighs the benefits.
	if hasAVX2 && len(data) >= 32 {
		return isASCIIAVX2(data)
	}

	// Fallback to generic implementation for small inputs or non-AVX2 CPUs
	return isASCIIGeneric(data)
}
