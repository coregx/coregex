//go:build amd64

#include "textflag.h"

// func memchrDigitAVX2(haystack []byte) int
//
// AVX2 implementation of digit search that finds the first ASCII digit [0-9].
// Processes 32 bytes per iteration using 256-bit vector range comparison.
//
// Algorithm for range check [0-9] (bytes 0x30-0x39):
//  1. Broadcast 0x2F ('0'-1) and 0x39 ('9') to YMM registers
//  2. Load 32 bytes from haystack
//  3. VPCMPGTB: check if chunk > 0x2F (byte >= '0')
//  4. VPCMPGTB: check if 0x39 < chunk, then invert (byte <= '9')
//  5. VPAND: combine masks to get bytes in range ['0'-'9']
//  6. VPMOVMSKB: extract 32-bit mask
//  7. TZCNT/BSFL: find first set bit
//
// Parameters (FP offsets):
//   haystack_base+0(FP)  - pointer to haystack data (8 bytes)
//   haystack_len+8(FP)   - haystack length (8 bytes)
//   haystack_cap+16(FP)  - haystack capacity (8 bytes, unused but part of slice)
//   ret+24(FP)           - return value: index or -1 (8 bytes)
//
// Total argument frame size: 32 bytes (8+8+8+8)
TEXT ·memchrDigitAVX2(SB), NOSPLIT, $0-32
	// Load parameters
	MOVQ    haystack_base+0(FP), SI     // SI = haystack pointer
	MOVQ    haystack_len+8(FP), DX      // DX = haystack length

	// Empty haystack check
	TESTQ   DX, DX
	JZ      not_found

	// Prepare constants for range check ['0'-'9'] = [0x30-0x39]
	// We check: byte > 0x2F AND byte <= 0x39
	// Which is: byte > 0x2F AND NOT(byte > 0x39)
	//
	// Y0 = 0x2F repeated (low bound - 1, i.e., '0' - 1)
	// Y1 = 0x39 repeated (high bound, i.e., '9')
	MOVQ    $0x2F2F2F2F2F2F2F2F, AX
	MOVQ    AX, X0
	VPBROADCASTQ X0, Y0                  // Y0 = [0x2F x 32]

	MOVQ    $0x3939393939393939, AX
	MOVQ    AX, X1
	VPBROADCASTQ X1, Y1                  // Y1 = [0x39 x 32]

	// Save start pointer for offset calculation
	MOVQ    SI, DI                       // DI = haystack start (preserved)

	// Calculate end pointer
	LEAQ    (SI)(DX*1), R8               // R8 = SI + length (end pointer)

// Main loop: process 32 bytes per iteration
loop32:
	// Check if we have at least 32 bytes remaining
	LEAQ    32(SI), R9                   // R9 = SI + 32
	CMPQ    R9, R8                       // Compare with end pointer
	JA      handle_tail                  // If R9 > R8, less than 32 bytes left

	// Load 32 bytes from haystack (unaligned load is safe and fast with AVX2)
	VMOVDQU (SI), Y2                     // Y2 = haystack[SI:SI+32]

	// Range check: '0' <= byte <= '9'
	//
	// Step 1: Y3 = (byte > 0x2F) - bytes that are >= '0' have 0xFF, else 0x00
	// VPCMPGTB sets each byte to 0xFF if Y2[i] > Y0[i], else 0x00
	VPCMPGTB Y0, Y2, Y3                  // Y3 = (chunk > 0x2F)

	// Step 2: Y4 = (byte > 0x39) - bytes that are > '9' have 0xFF, else 0x00
	VPCMPGTB Y1, Y2, Y4                  // Y4 = (chunk > 0x39)

	// Step 3: Y5 = Y3 AND NOT(Y4)
	// This gives us: (byte > 0x2F) AND (byte <= 0x39) = digit in range
	// VPANDN: Y5 = NOT(Y4) AND Y3
	VPANDN  Y3, Y4, Y5                   // Y5 = (~Y4) & Y3 = digit mask

	// Extract mask: bit i set if byte i is a digit
	VPMOVMSKB Y5, CX                     // CX = 32-bit mask

	// Check if any digit found (mask != 0)
	TESTL   CX, CX
	JNZ     found_in_vector              // Non-zero mask means digit found

	// No digit in this chunk, advance to next 32 bytes
	ADDQ    $32, SI
	JMP     loop32

handle_tail:
	// Process remaining bytes (0-31 bytes) with scalar loop
	// This handles the case where haystack length is not a multiple of 32

	// Check if we've already processed all bytes
	CMPQ    SI, R8
	JAE     not_found                    // If SI >= end, no bytes left

tail_loop:
	// Load one byte
	MOVBLZX (SI), BX                     // BX = haystack[SI] (zero-extended)

	// Check if byte is in range ['0'-'9'] (0x30-0x39)
	CMPB    BL, $0x30                    // Compare with '0'
	JB      tail_next                    // If < '0', not a digit
	CMPB    BL, $0x39                    // Compare with '9'
	JA      tail_next                    // If > '9', not a digit

	// Found digit!
	SUBQ    DI, SI                       // SI = offset from start
	MOVQ    SI, ret+24(FP)               // Return index
	VZEROUPPER                           // Clear upper YMM bits (CRITICAL!)
	RET

tail_next:
	// Advance to next byte
	INCQ    SI
	CMPQ    SI, R8                       // Check if reached end
	JB      tail_loop                    // Continue if SI < end

not_found:
	// No digit found in entire haystack
	MOVQ    $-1, AX                      // Return -1
	MOVQ    AX, ret+24(FP)
	VZEROUPPER                           // Clear upper YMM bits (CRITICAL!)
	RET

found_in_vector:
	// Digit found in vector! CX contains 32-bit mask with set bits at digit positions.
	// Use BSFL (Bit Scan Forward Long) to find index of first set bit.
	//
	// BSFL: scans from bit 0 upward, finds first 1 bit, stores its index in destination
	// Example: CX = 0x00000010 (bit 4 set) -> BX = 4
	BSFL    CX, BX                       // BX = index of first set bit (0-31)

	// Calculate absolute index in haystack
	SUBQ    DI, SI                       // SI = offset from start to current chunk
	ADDQ    SI, BX                       // BX = absolute index (chunk_offset + bit_position)
	MOVQ    BX, ret+24(FP)               // Return index
	VZEROUPPER                           // Clear upper YMM bits
	RET
