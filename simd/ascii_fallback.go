//go:build !amd64

package simd

// IsASCII checks if all bytes in the slice are ASCII (< 0x80).
// Returns true if all bytes have the high bit clear (values 0x00-0x7F).
//
// On non-AMD64 platforms, this function uses an optimized pure Go implementation
// with SWAR (SIMD Within A Register) technique, which processes 8 bytes at a time
// using uint64 bitwise operations.
//
// Performance characteristics (pure Go SWAR):
//   - Small inputs (< 8 bytes): byte-by-byte comparison
//   - Medium/large inputs: ~10 GB/s throughput (memory bandwidth limited)
//
// This function is critical for UTF-8 optimization in the regex engine.
// When input is ASCII-only, the engine can skip UTF-8 decoding overhead
// and use simplified automata (1 NFA state for '.' instead of 39).
//
// See isASCIIGeneric for implementation details.
func IsASCII(data []byte) bool {
	return isASCIIGeneric(data)
}
