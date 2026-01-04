package simd

import (
	"encoding/binary"
	"math/bits"
)

// memchrGeneric implements pure Go byte search using SWAR (SIMD Within A Register)
// technique. It processes 8 bytes at a time using uint64 bitwise operations.
//
// This function is used as a fallback on all platforms:
//   - On amd64: fallback for small inputs (< 32 bytes) or when AVX2 is not available
//   - On other platforms: primary implementation
//
// Algorithm:
//  1. Create a mask with needle replicated in every byte of uint64
//  2. Read 8 bytes from haystack as uint64
//  3. XOR with mask (matching bytes become 0x00)
//  4. Use zero-byte detection formula to find first zero
//  5. Extract position using trailing zero count
//
// Performance: 2-5x faster than byte-by-byte comparison on medium/large inputs.
func memchrGeneric(haystack []byte, needle byte) int {
	haystackLen := len(haystack)
	if haystackLen == 0 {
		return -1
	}

	// For small inputs, byte-by-byte is faster (no setup overhead)
	if haystackLen < 8 {
		for idx := 0; idx < haystackLen; idx++ {
			if haystack[idx] == needle {
				return idx
			}
		}
		return -1
	}

	// SWAR technique: broadcast needle to all 8 bytes of uint64
	// Example: needle=0x42 → needleMask=0x4242424242424242
	needleMask := uint64(needle) * 0x0101010101010101

	idx := 0

	// Process aligned 8-byte chunks
	for idx+8 <= haystackLen {
		// Read 8 bytes as little-endian uint64
		chunk := binary.LittleEndian.Uint64(haystack[idx:])

		// XOR makes matching bytes become 0x00
		// Example: chunk=0x4241424142414241, needleMask=0x4242424242424242
		//          xor  =0x0003000300030003
		xor := chunk ^ needleMask

		// Zero-byte detection formula (Hacker's Delight technique):
		// Detects if any byte in xor is zero (meaning original byte matched needle)
		//
		// Formula: (v - 0x0101010101010101) & ^v & 0x8080808080808080
		//   - Subtracting 0x01 from each byte causes borrow if byte was 0x00
		//   - AND with ~v isolates bytes that were originally zero
		//   - AND with 0x80 extracts high bit, marking zero-byte positions
		const lo8 = 0x0101010101010101
		const hi8 = 0x8080808080808080
		hasZero := (xor - lo8) & ^xor & hi8

		if hasZero != 0 {
			// Found match! Determine exact byte position.
			// TrailingZeros64 counts bits until first set bit.
			// Divide by 8 to convert bit position to byte position.
			//
			// Example: hasZero=0x0000000000008000 (bit 15 set)
			//          TrailingZeros64=15, position=15/8=1 (second byte)
			return idx + bits.TrailingZeros64(hasZero)/8
		}

		idx += 8
	}

	// Process remaining bytes (0-7 bytes) byte-by-byte
	for idx < haystackLen {
		if haystack[idx] == needle {
			return idx
		}
		idx++
	}

	return -1
}

// memchr2Generic implements pure Go search for two needles using SWAR technique.
// It processes 8 bytes at a time, checking both needles in parallel.
//
// Algorithm:
//  1. Create masks for both needles
//  2. For each 8-byte chunk, check both needles using zero-byte detection
//  3. Return position of first match (whichever needle appears first)
func memchr2Generic(haystack []byte, needle1, needle2 byte) int {
	haystackLen := len(haystack)
	if haystackLen == 0 {
		return -1
	}

	// For small inputs, byte-by-byte is faster
	if haystackLen < 8 {
		for idx := 0; idx < haystackLen; idx++ {
			currentByte := haystack[idx]
			if currentByte == needle1 || currentByte == needle2 {
				return idx
			}
		}
		return -1
	}

	// Broadcast both needles to uint64 masks
	needleMask1 := uint64(needle1) * 0x0101010101010101
	needleMask2 := uint64(needle2) * 0x0101010101010101

	idx := 0

	// Process 8-byte chunks
	for idx+8 <= haystackLen {
		chunk := binary.LittleEndian.Uint64(haystack[idx:])

		// Check both needles in parallel
		xor1 := chunk ^ needleMask1
		xor2 := chunk ^ needleMask2

		// Zero-byte detection for both
		const lo8 = 0x0101010101010101
		const hi8 = 0x8080808080808080
		hasZero1 := (xor1 - lo8) & ^xor1 & hi8
		hasZero2 := (xor2 - lo8) & ^xor2 & hi8

		// Combine results: match if either needle found
		hasZero := hasZero1 | hasZero2

		if hasZero != 0 {
			return idx + bits.TrailingZeros64(hasZero)/8
		}

		idx += 8
	}

	// Process remaining bytes
	for idx < haystackLen {
		currentByte := haystack[idx]
		if currentByte == needle1 || currentByte == needle2 {
			return idx
		}
		idx++
	}

	return -1
}

// memchr3Generic implements pure Go search for three needles using SWAR technique.
// It processes 8 bytes at a time, checking all three needles in parallel.
//
// Algorithm: Same as memchr2Generic but with three needle masks.
func memchr3Generic(haystack []byte, needle1, needle2, needle3 byte) int {
	haystackLen := len(haystack)
	if haystackLen == 0 {
		return -1
	}

	// For small inputs, byte-by-byte is faster
	if haystackLen < 8 {
		for idx := 0; idx < haystackLen; idx++ {
			currentByte := haystack[idx]
			if currentByte == needle1 || currentByte == needle2 || currentByte == needle3 {
				return idx
			}
		}
		return -1
	}

	// Broadcast all three needles to uint64 masks
	needleMask1 := uint64(needle1) * 0x0101010101010101
	needleMask2 := uint64(needle2) * 0x0101010101010101
	needleMask3 := uint64(needle3) * 0x0101010101010101

	idx := 0

	// Process 8-byte chunks
	for idx+8 <= haystackLen {
		chunk := binary.LittleEndian.Uint64(haystack[idx:])

		// Check all three needles in parallel
		xor1 := chunk ^ needleMask1
		xor2 := chunk ^ needleMask2
		xor3 := chunk ^ needleMask3

		// Zero-byte detection for all three
		const lo8 = 0x0101010101010101
		const hi8 = 0x8080808080808080
		hasZero1 := (xor1 - lo8) & ^xor1 & hi8
		hasZero2 := (xor2 - lo8) & ^xor2 & hi8
		hasZero3 := (xor3 - lo8) & ^xor3 & hi8

		// Combine results: match if any needle found
		hasZero := hasZero1 | hasZero2 | hasZero3

		if hasZero != 0 {
			return idx + bits.TrailingZeros64(hasZero)/8
		}

		idx += 8
	}

	// Process remaining bytes
	for idx < haystackLen {
		currentByte := haystack[idx]
		if currentByte == needle1 || currentByte == needle2 || currentByte == needle3 {
			return idx
		}
		idx++
	}

	return -1
}

// memchrPairGeneric implements pure Go paired-byte search using SWAR technique.
// It finds positions where byte1 appears at position i and byte2 appears at position i+offset.
//
// This is used for highly selective substring prefiltering - the probability of
// both bytes appearing at exactly the right distance is much lower than single-byte search.
//
// Algorithm:
//  1. Create masks for both bytes
//  2. For each 8-byte chunk at position i:
//     - Check byte1 at position i
//     - Check byte2 at position i+offset
//     - AND the results to get positions where both match
//  3. Return first position where both conditions are satisfied
func memchrPairGeneric(haystack []byte, byte1, byte2 byte, offset int) int {
	haystackLen := len(haystack)
	if haystackLen == 0 || offset < 0 || haystackLen <= offset {
		return -1
	}

	// For small inputs or small remaining space, byte-by-byte is simpler
	if haystackLen < 8+offset {
		for i := 0; i+offset < haystackLen; i++ {
			if haystack[i] == byte1 && haystack[i+offset] == byte2 {
				return i
			}
		}
		return -1
	}

	// Broadcast both bytes to uint64 masks
	needleMask1 := uint64(byte1) * 0x0101010101010101
	needleMask2 := uint64(byte2) * 0x0101010101010101

	idx := 0
	const lo8 = uint64(0x0101010101010101)
	const hi8 = uint64(0x8080808080808080)

	// Process 8-byte chunks
	// We need to ensure we have 8 bytes at position idx AND 8 bytes at position idx+offset
	for idx+8+offset <= haystackLen {
		// Read 8 bytes at position idx (for byte1)
		chunk1 := binary.LittleEndian.Uint64(haystack[idx:])
		// Read 8 bytes at position idx+offset (for byte2)
		chunk2 := binary.LittleEndian.Uint64(haystack[idx+offset:])

		// Check byte1 at position idx
		xor1 := chunk1 ^ needleMask1
		hasZero1 := (xor1 - lo8) & ^xor1 & hi8

		// Check byte2 at position idx+offset
		xor2 := chunk2 ^ needleMask2
		hasZero2 := (xor2 - lo8) & ^xor2 & hi8

		// AND the results: only positions where both bytes are found
		// Bit k is set in hasZero1 if haystack[idx+k] == byte1
		// Bit k is set in hasZero2 if haystack[idx+offset+k] == byte2
		// We want positions where haystack[idx+k] == byte1 AND haystack[idx+k+offset] == byte2
		// Since chunk2 starts at idx+offset, bit k in hasZero2 corresponds to idx+offset+k
		// So AND gives us positions where both conditions are met
		hasZero := hasZero1 & hasZero2

		if hasZero != 0 {
			return idx + bits.TrailingZeros64(hasZero)/8
		}

		idx += 8
	}

	// Process remaining positions byte-by-byte
	for idx+offset < haystackLen {
		if haystack[idx] == byte1 && haystack[idx+offset] == byte2 {
			return idx
		}
		idx++
	}

	return -1
}
