//go:build amd64

#include "textflag.h"

// func fatTeddyAVX2_2(masks *fatTeddyMasks, haystack []byte) (pos int, bucketMask uint16)
//
// AVX2 implementation of Fat Teddy with 2-byte fingerprint and 16 buckets.
// Based on Rust aho-corasick generic.rs Fat<V, 2> implementation.
//
// Algorithm:
//  1. Start at position 1 (cur = start + 1)
//  2. Initialize prev0 = 0xFF (all bits set)
//  3. Main loop: process 16 bytes per iteration
//     a. Load 16 bytes and broadcast to both lanes (VBROADCASTI128)
//     b. Compute res0 = masks[0] lookup (first fingerprint byte)
//     c. Compute res1 = masks[1] lookup (second fingerprint byte)
//     d. Shift res0 right by 1 byte within each lane, bringing in prev0 (VPALIGNR)
//     e. AND res0_shifted with res1
//     f. Check for non-zero bytes (candidates)
//  4. If candidate found: extract position and 16-bit bucket mask
//
// CRITICAL: VZEROUPPER before every RET to avoid AVX-SSE transition penalty.
//
// Parameters (FP offsets):
//   masks+0(FP)          - pointer to fatTeddyMasks struct (8 bytes)
//   haystack_base+8(FP)  - pointer to haystack data (8 bytes)
//   haystack_len+16(FP)  - haystack length (8 bytes)
//   haystack_cap+24(FP)  - haystack capacity (8 bytes, unused)
//   pos+32(FP)           - return: candidate position or -1 (8 bytes)
//   bucketMask+40(FP)    - return: 16-bit bucket mask or 0 (2 bytes)
//
// fatTeddyMasks struct layout (offsets):
//   +0:   fingerprintLen (uint32, 4 bytes)
//   +4:   padding (4 bytes)
//   +8:   loMasks[0] (32 bytes) - low 16 = buckets 0-7, high 16 = buckets 8-15
//   +40:  loMasks[1] (32 bytes)
//   +72:  loMasks[2] (32 bytes, unused for 2-byte fingerprint)
//   +104: loMasks[3] (32 bytes, unused)
//   +136: hiMasks[0] (32 bytes)
//   +168: hiMasks[1] (32 bytes)
//   +200: hiMasks[2] (32 bytes, unused)
//   +232: hiMasks[3] (32 bytes, unused)
TEXT ·fatTeddyAVX2_2(SB), NOSPLIT, $0-42
	// Load parameters
	MOVQ    masks+0(FP), R8             // R8 = pointer to fatTeddyMasks
	MOVQ    haystack_base+8(FP), SI     // SI = haystack pointer
	MOVQ    haystack_len+16(FP), DX     // DX = haystack length

	// Empty haystack check
	TESTQ   DX, DX
	JZ      not_found

	// Check minimum length (need at least 2 bytes for 2-byte fingerprint)
	CMPQ    DX, $2
	JB      not_found

	// Load nibble masks for positions 0 and 1 (32 bytes each for AVX2)
	// Position 0: loMasks[0] at +8, hiMasks[0] at +136
	// Position 1: loMasks[1] at +40, hiMasks[1] at +168
	VMOVDQU 8(R8), Y0                   // Y0 = loMasks[0] (32 bytes)
	VMOVDQU 136(R8), Y1                 // Y1 = hiMasks[0] (32 bytes)
	VMOVDQU 40(R8), Y8                  // Y8 = loMasks[1] (32 bytes)
	VMOVDQU 168(R8), Y9                 // Y9 = hiMasks[1] (32 bytes)

	// Create nibble extraction mask: 0x0F repeated 32 times
	MOVQ    $0x0F0F0F0F0F0F0F0F, AX
	MOVQ    AX, X2
	VPBROADCASTQ X2, Y2                 // Y2 = [0x0F x 32]

	// Initialize prev0 = 0xFF (all bits set)
	VPCMPEQD Y7, Y7, Y7                 // Y7 = prev0 = all 0xFF

	// Save original haystack pointer for offset calculation
	MOVQ    SI, DI                      // DI = haystack start (preserved)

	// Start at position 1 (per Rust algorithm)
	INCQ    SI                          // cur = start + 1

	// Calculate end pointer for 16-byte chunks
	LEAQ    (DI)(DX*1), R9              // R9 = haystack end pointer

loop16:
	// Check if we have at least 16 bytes remaining from cur
	LEAQ    16(SI), R10                 // R10 = cur + 16
	CMPQ    R10, R9                     // Compare with end pointer
	JA      handle_tail                 // If R10 > R9, less than 16 bytes left

	// Load 16 bytes from cur and broadcast to both 128-bit lanes
	VBROADCASTI128 (SI), Y3             // Y3 = chunk (16 bytes duplicated)

	// === Compute res0 = masks[0] lookup ===
	// Extract low nibbles: chunk & 0x0F
	VPAND   Y2, Y3, Y4                  // Y4 = low nibbles

	// Extract high nibbles: (chunk >> 4) & 0x0F
	VPSRLW  $4, Y3, Y5                  // Shift right 4 bits (within 16-bit words)
	VPAND   Y2, Y5, Y5                  // Y5 = high nibbles

	// VPSHUFB lookups for masks[0]
	VPSHUFB Y4, Y0, Y6                  // Y6 = loMasks[0][low_nibbles]
	VPSHUFB Y5, Y1, Y10                 // Y10 = hiMasks[0][high_nibbles]
	VPAND   Y10, Y6, Y6                 // Y6 = res0 (position 0 candidates)

	// === Compute res1 = masks[1] lookup ===
	// Use same nibbles (Y4, Y5) since same chunk
	VPSHUFB Y4, Y8, Y10                 // Y10 = loMasks[1][low_nibbles]
	VPSHUFB Y5, Y9, Y11                 // Y11 = hiMasks[1][high_nibbles]
	VPAND   Y11, Y10, Y10               // Y10 = res1 (position 1 candidates)

	// === half_shift_in_one_byte: VPALIGNR(res0, prev0, 15) ===
	// This shifts res0 right by 1 byte within each 128-bit lane,
	// bringing in the last byte from prev0
	VPALIGNR $15, Y7, Y6, Y11           // Y11 = res0 shifted right by 1 with prev0

	// Update prev0 for next iteration
	VMOVDQA Y6, Y7                      // prev0 = res0

	// === Combine: res = res0_shifted AND res1 ===
	VPAND   Y10, Y11, Y6                // Y6 = final candidates

	// === Check for non-zero bytes ===
	// In Fat Teddy, each byte position has an 8-bit bucket mask
	// Low lane (bytes 0-15): buckets 0-7
	// High lane (bytes 16-31): buckets 8-15 (same position, different buckets)
	//
	// We need to find first position where BOTH lanes have non-zero bytes
	// A match at position i means: Y6[i] != 0 AND Y6[16+i] != 0

	// Check if any byte is non-zero in low lane
	VPXOR   Y12, Y12, Y12               // Y12 = zero
	VPCMPEQB Y12, Y6, Y13               // Y13[i] = 0xFF if Y6[i] == 0, else 0
	VPMOVMSKB Y13, CX                   // CX = byte mask (1 = zero, 0 = non-zero)
	NOTL    CX                          // CX = inverted (1 = non-zero)

	// Low 16 bits = positions 0-15 in low lane
	// High 16 bits = positions 0-15 in high lane (buckets 8-15)
	MOVL    CX, AX
	ANDL    $0xFFFF, AX                 // AX = low lane mask
	SHRL    $16, CX                     // CX = high lane mask

	// Both lanes must have non-zero for a valid candidate
	ANDL    CX, AX                      // AX = positions with candidates in BOTH lanes

	TESTL   AX, AX
	JNZ     found_candidate

	// No candidates in this chunk, advance
	ADDQ    $16, SI
	JMP     loop16

handle_tail:
	// Process remaining bytes one at a time using scalar code
	// Reset prev0 logic doesn't matter for scalar

tail_check:
	// Need at least 2 bytes
	LEAQ    1(SI), R10
	CMPQ    R10, R9
	JAE     not_found

tail_loop:
	// Load two consecutive bytes
	MOVBLZX (SI), AX                    // AX = byte at position i
	MOVBLZX 1(SI), R10                  // R10 = byte at position i+1

	// === Position 0 lookup ===
	// Buckets 0-7 (loMasks[0][0:16], hiMasks[0][0:16])
	MOVL    AX, BX
	ANDL    $0x0F, BX                   // BX = low nibble
	MOVL    AX, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble

	MOVBLZX 8(R8)(BX*1), R11            // R11 = loMasks[0][low]
	MOVBLZX 136(R8)(CX*1), R12          // R12 = hiMasks[0][high]
	ANDL    R12, R11                    // R11 = pos0 buckets 0-7

	// Buckets 8-15 (loMasks[0][16:32], hiMasks[0][16:32])
	MOVBLZX 24(R8)(BX*1), R12           // R12 = loMasks[0][16+low]
	MOVBLZX 152(R8)(CX*1), R13          // R13 = hiMasks[0][16+high]
	ANDL    R13, R12                    // R12 = pos0 buckets 8-15
	SHLL    $8, R12
	ORL     R12, R11                    // R11 = 16-bit pos0 mask

	// === Position 1 lookup ===
	MOVL    R10, BX
	ANDL    $0x0F, BX                   // BX = low nibble
	MOVL    R10, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble

	// Buckets 0-7
	MOVBLZX 40(R8)(BX*1), R12           // R12 = loMasks[1][low]
	MOVBLZX 168(R8)(CX*1), R13          // R13 = hiMasks[1][high]
	ANDL    R13, R12                    // R12 = pos1 buckets 0-7

	// Buckets 8-15
	MOVBLZX 56(R8)(BX*1), R13           // R13 = loMasks[1][16+low]
	MOVBLZX 184(R8)(CX*1), R14          // R14 = hiMasks[1][16+high]
	ANDL    R14, R13                    // R13 = pos1 buckets 8-15
	SHLL    $8, R13
	ORL     R13, R12                    // R12 = 16-bit pos1 mask

	// Combine pos0 and pos1
	ANDL    R12, R11                    // R11 = 16-bit combined mask

	TESTL   R11, R11
	JNZ     found_scalar

	// Next position
	INCQ    SI
	LEAQ    1(SI), R10
	CMPQ    R10, R9
	JB      tail_loop

not_found:
	MOVQ    $-1, pos+32(FP)
	MOVW    $0, bucketMask+40(FP)
	VZEROUPPER
	RET

found_candidate:
	// AX contains mask of positions with candidates
	// Find first set bit
	BSFL    AX, BX                      // BX = first position with candidate

	// Calculate absolute position: (cur - start - 1) + BX
	// Note: we started at start+1, so actual match position is cur-1+BX relative to start
	MOVQ    SI, AX
	SUBQ    DI, AX                      // AX = cur - start
	DECQ    AX                          // AX = cur - start - 1 (because we're at position after the match byte)
	ADDQ    BX, AX                      // AX = absolute position

	// Extract 16-bit bucket mask at position BX
	// Need to read Y6[BX] (buckets 0-7) and Y6[16+BX] (buckets 8-15)
	// Use scalar reload from haystack
	ADDQ    DI, BX                      // BX = absolute pointer to match position
	SUBQ    $1, BX                      // Adjust for the -1 in position calc
	ADDQ    AX, DI                      // DI = pointer to match position

	// Reload bytes at match position for bucket mask calculation
	MOVBLZX (DI), R10                   // R10 = first byte
	MOVBLZX 1(DI), R11                  // R11 = second byte

	// Position 0 bucket mask
	MOVL    R10, BX
	ANDL    $0x0F, BX
	MOVL    R10, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX

	MOVBLZX 8(R8)(BX*1), R12
	MOVBLZX 136(R8)(CX*1), R13
	ANDL    R13, R12                    // R12 = pos0 buckets 0-7

	MOVBLZX 24(R8)(BX*1), R13
	MOVBLZX 152(R8)(CX*1), R14
	ANDL    R14, R13
	SHLL    $8, R13
	ORL     R13, R12                    // R12 = 16-bit pos0 mask

	// Position 1 bucket mask
	MOVL    R11, BX
	ANDL    $0x0F, BX
	MOVL    R11, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX

	MOVBLZX 40(R8)(BX*1), R13
	MOVBLZX 168(R8)(CX*1), R14
	ANDL    R14, R13                    // R13 = pos1 buckets 0-7

	MOVBLZX 56(R8)(BX*1), R14
	MOVBLZX 184(R8)(CX*1), R15
	ANDL    R15, R14
	SHLL    $8, R14
	ORL     R14, R13                    // R13 = 16-bit pos1 mask

	// Combine
	ANDL    R13, R12                    // R12 = final 16-bit bucket mask

	MOVQ    AX, pos+32(FP)
	MOVW    R12, bucketMask+40(FP)
	VZEROUPPER
	RET

found_scalar:
	// Calculate position
	SUBQ    DI, SI                      // SI = position relative to start

	MOVQ    SI, pos+32(FP)
	MOVW    R11, bucketMask+40(FP)
	VZEROUPPER
	RET
