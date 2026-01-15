//go:build amd64

#include "textflag.h"

// func isASCIIAVX2(data []byte) bool
//
// AVX2 implementation of ASCII detection that checks if all bytes are < 0x80.
// Processes 32 bytes per iteration using 256-bit vector operations.
//
// Algorithm:
//  1. Main loop: load 32 bytes, use VPMOVMSKB to extract high bits
//  2. If any high bit is set (mask != 0), return false (non-ASCII found)
//  3. Handle tail (< 32 bytes) with scalar loop
//  4. Always call VZEROUPPER before returning (critical for performance)
//
// The key insight: ASCII bytes have the high bit (bit 7) clear (0x00-0x7F).
// VPMOVMSKB extracts bit 7 from each byte into a 32-bit mask.
// If mask == 0, all 32 bytes are ASCII.
//
// Parameters (FP offsets):
//   data_base+0(FP)  - pointer to data (8 bytes)
//   data_len+8(FP)   - data length (8 bytes)
//   data_cap+16(FP)  - data capacity (8 bytes, unused but part of slice)
//   ret+24(FP)       - return value: bool (1 byte, but uses 8 bytes for alignment)
//
// Total argument frame size: 32 bytes (8+8+8+8)
TEXT ·isASCIIAVX2(SB), NOSPLIT, $0-32
	// Load parameters
	MOVQ    data_base+0(FP), SI     // SI = data pointer
	MOVQ    data_len+8(FP), DX      // DX = data length

	// Empty data check
	TESTQ   DX, DX
	JZ      all_ascii

	// Save start pointer
	MOVQ    SI, DI                  // DI = data start (preserved)

	// Calculate end pointer
	LEAQ    (SI)(DX*1), R8          // R8 = SI + length (end pointer)

	// Main loop: process 32 bytes per iteration
loop32:
	// Check if we have at least 32 bytes remaining
	LEAQ    32(SI), R9              // R9 = SI + 32
	CMPQ    R9, R8                  // Compare with end pointer
	JA      handle_tail             // If R9 > R8, less than 32 bytes left

	// Load 32 bytes from data (unaligned load is safe and fast with AVX2)
	VMOVDQU (SI), Y0                // Y0 = data[SI:SI+32]

	// Extract high bit of each byte into 32-bit mask
	// VPMOVMSKB: bit i in result = bit 7 of byte i in source vector
	// If any byte >= 0x80, its high bit is 1, so mask != 0
	VPMOVMSKB Y0, AX                // AX = 32-bit mask of high bits

	// Check if any non-ASCII byte found (mask != 0)
	TESTL   AX, AX
	JNZ     found_non_ascii         // Non-zero mask means non-ASCII found

	// All 32 bytes are ASCII, advance to next chunk
	ADDQ    $32, SI
	JMP     loop32

handle_tail:
	// Process remaining bytes (0-31 bytes) with scalar loop
	// Check if we've already processed all bytes
	CMPQ    SI, R8
	JAE     all_ascii               // If SI >= end, all bytes processed

tail_loop:
	// Load one byte and check high bit
	MOVBLZX (SI), AX                // AX = data[SI] (zero-extended)
	TESTB   $0x80, AL               // Check if high bit is set
	JNZ     found_non_ascii_scalar  // If set, non-ASCII found

	// Advance to next byte
	INCQ    SI
	CMPQ    SI, R8                  // Check if reached end
	JB      tail_loop               // Continue if SI < end

all_ascii:
	// All bytes are ASCII
	MOVB    $1, ret+24(FP)          // Return true
	VZEROUPPER                      // Clear upper YMM bits (CRITICAL!)
	RET

found_non_ascii:
	// Non-ASCII byte found in vector
	MOVB    $0, ret+24(FP)          // Return false
	VZEROUPPER                      // Clear upper YMM bits
	RET

found_non_ascii_scalar:
	// Non-ASCII byte found in scalar loop
	MOVB    $0, ret+24(FP)          // Return false
	VZEROUPPER                      // Clear upper YMM bits
	RET
