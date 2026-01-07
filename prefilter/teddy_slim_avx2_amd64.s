//go:build amd64

#include "textflag.h"

// func teddySlimAVX2_1(masks *teddyMasks, haystack []byte) (pos int, bucketMask uint8)
//
// AVX2 implementation of Teddy Slim with 1-byte fingerprint.
// Processes 32 bytes per iteration (2x throughput vs SSSE3).
//
// Algorithm:
//  1. Load nibble masks for position 0 from masks struct (32 bytes for AVX2)
//  2. Main loop: process 32 bytes per iteration
//     a. Load 32 bytes from haystack (VMOVDQU)
//     b. Extract low nibbles (byte & 0x0F) and high nibbles (byte >> 4)
//     c. Use VPSHUFB to lookup bucket bits from masks
//     d. VPAND lo/hi results to find candidates
//     e. Extract bitmask with VPCMPEQB + VPMOVMSKB
//  3. If candidate found: find first set bit position, extract bucket MASK
//  4. Handle tail (<32 bytes) with SSSE3 or scalar fallback
//
// Returns bucket MASK (not bucket ID) - caller iterates through all set bits.
//
// CRITICAL: VZEROUPPER must be called before every RET to avoid AVX-SSE transition penalty.
//
// Parameters (FP offsets):
//   masks+0(FP)          - pointer to teddyMasks struct (8 bytes)
//   haystack_base+8(FP)  - pointer to haystack data (8 bytes)
//   haystack_len+16(FP)  - haystack length (8 bytes)
//   haystack_cap+24(FP)  - haystack capacity (8 bytes, unused)
//   pos+32(FP)           - return: candidate position or -1 (8 bytes)
//   bucketMask+40(FP)    - return: bucket MASK (bits for all matching buckets) or 0 (1 byte)
//
// Total argument frame size: 41 bytes (8+8+8+8+8+1)
//
// teddyMasks struct layout (offsets):
//   +0:   fingerprintLen (uint32, 4 bytes)
//   +4:   padding (4 bytes)
//   +8:   loMasks[0] (32 bytes) - for AVX2 we use all 32 bytes (duplicated for both lanes)
//   +40:  loMasks[1] (32 bytes, unused for 1-byte fingerprint)
//   +72:  loMasks[2] (32 bytes, unused)
//   +104: loMasks[3] (32 bytes, unused)
//   +136: hiMasks[0] (32 bytes)
//   +168: hiMasks[1] (32 bytes, unused)
//   +200: hiMasks[2] (32 bytes, unused)
//   +232: hiMasks[3] (32 bytes, unused)
TEXT ·teddySlimAVX2_1(SB), NOSPLIT, $0-41
	// Load parameters
	MOVQ    masks+0(FP), R8             // R8 = pointer to teddyMasks
	MOVQ    haystack_base+8(FP), SI     // SI = haystack pointer
	MOVQ    haystack_len+16(FP), DX     // DX = haystack length

	// Empty haystack check
	TESTQ   DX, DX
	JZ      not_found_1

	// Check minimum length (need at least 1 byte for fingerprint)
	CMPQ    DX, $1
	JB      not_found_1

	// Load nibble masks for position 0 (32 bytes for AVX2)
	// loMasks[0] at offset 8, hiMasks[0] at offset 136
	// For Slim Teddy AVX2: first 16 bytes are bucket masks,
	// second 16 bytes are duplicates (set during buildMasks)
	VMOVDQU 8(R8), Y0                   // Y0 = loMasks[0] (32 bytes)
	VMOVDQU 136(R8), Y1                 // Y1 = hiMasks[0] (32 bytes)

	// Create nibble extraction mask: 0x0F repeated 32 times
	MOVQ    $0x0F0F0F0F0F0F0F0F, AX
	MOVQ    AX, X2
	VPBROADCASTQ X2, Y2                 // Y2 = [0x0F x 32]

	// Save original haystack pointer for offset calculation
	MOVQ    SI, DI                      // DI = haystack start (preserved)

	// Calculate end pointer
	LEAQ    (SI)(DX*1), R9              // R9 = SI + length (end pointer)

// Main loop: process 32 bytes per iteration
loop32_1:
	// Check if we have at least 32 bytes remaining
	LEAQ    32(SI), R10                 // R10 = SI + 32
	CMPQ    R10, R9                     // Compare with end pointer
	JA      handle_tail_1               // If R10 > R9, less than 32 bytes left

	// Load 32 bytes from haystack (unaligned load)
	VMOVDQU (SI), Y3                    // Y3 = haystack[SI:SI+32]

	// Extract low nibbles: Y3 & 0x0F
	VPAND   Y2, Y3, Y4                  // Y4 = low nibbles (byte & 0x0F)

	// Extract high nibbles: (Y3 >> 4) & 0x0F
	VPSRLW  $4, Y3, Y5                  // Shift right 4 bits (works on 16-bit words)
	VPAND   Y2, Y5, Y5                  // Y5 = high nibbles (mask to 4 bits)

	// VPSHUFB lookups: use nibbles as indices into mask tables
	// For AVX2: VPSHUFB operates independently on each 128-bit lane
	// Since masks are duplicated in both lanes, this works correctly
	VPSHUFB Y4, Y0, Y6                  // Y6 = lo_mask lookup results
	VPSHUFB Y5, Y1, Y7                  // Y7 = hi_mask lookup results

	// Combine results: candidate must match BOTH low and high nibbles
	VPAND   Y7, Y6, Y6                  // Y6 = lo_mask & hi_mask (final bucket candidates)

	// Detect non-zero bytes in result vector
	// VPCMPEQB with zero, then VPMOVMSKB, then invert
	VPXOR   Y8, Y8, Y8                  // Y8 = zero vector
	VPCMPEQB Y8, Y6, Y7                 // Y7[i] = 0xFF if bucket_bits==0, else 0x00
	VPMOVMSKB Y7, CX                    // CX = bitmask where bytes were ZERO (32 bits)
	NOTL    CX                          // Invert: CX = bitmask where bytes were NON-ZERO

	// Check if any candidates found (mask != 0)
	TESTL   CX, CX
	JNZ     found_candidate_1           // Non-zero mask means candidate found

	// No candidates in this chunk, advance to next 32 bytes
	ADDQ    $32, SI
	JMP     loop32_1

handle_tail_1:
	// Process remaining bytes (0-31 bytes)
	// Check if we have at least 16 bytes for SSSE3-style processing
	LEAQ    16(SI), R10
	CMPQ    R10, R9
	JA      scalar_tail_1               // Less than 16 bytes, use scalar

	// 16-31 bytes remaining: process 16 bytes with AVX2 (using only low lane)
	// Load 16 bytes and broadcast to both lanes
	VBROADCASTI128 (SI), Y3             // Y3 = 16 bytes duplicated

	// Extract nibbles
	VPAND   Y2, Y3, Y4                  // Y4 = low nibbles
	VPSRLW  $4, Y3, Y5                  // Shift right 4
	VPAND   Y2, Y5, Y5                  // Y5 = high nibbles

	// VPSHUFB lookups
	VPSHUFB Y4, Y0, Y6
	VPSHUFB Y5, Y1, Y7
	VPAND   Y7, Y6, Y6

	// Check non-zero (only care about low 16 bits)
	VPXOR   Y8, Y8, Y8
	VPCMPEQB Y8, Y6, Y7
	VPMOVMSKB Y7, CX
	NOTL    CX
	ANDL    $0xFFFF, CX                 // Only low 16 bits are valid

	TESTL   CX, CX
	JNZ     found_candidate_16_1

	// Advance and check if more bytes remain
	ADDQ    $16, SI
	CMPQ    SI, R9
	JAE     not_found_1

scalar_tail_1:
	// Process remaining bytes with scalar loop
	CMPQ    SI, R9
	JAE     not_found_1

tail_loop_1:
	// Load one byte
	MOVBLZX (SI), AX                    // AX = haystack[SI] (zero-extended)

	// Extract nibbles
	MOVL    AX, BX
	ANDL    $0x0F, BX                   // BX = low nibble

	MOVL    AX, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble

	// Lookup in masks (scalar access to mask arrays)
	// loMasks[0][BX] at offset 8 + BX
	// hiMasks[0][CX] at offset 136 + CX
	MOVBLZX 8(R8)(BX*1), AX             // AX = loMasks[0][lowNibble]
	MOVBLZX 136(R8)(CX*1), CX           // CX = hiMasks[0][highNibble]

	// AND together (must match both nibbles)
	ANDL    CX, AX                      // AX = bucket candidate bits

	// Check if any bucket matched
	TESTL   AX, AX
	JNZ     found_scalar_1              // Non-zero means candidate found

	// Advance to next byte
	INCQ    SI
	CMPQ    SI, R9
	JB      tail_loop_1                 // Continue if SI < end

not_found_1:
	// No candidate found in entire haystack
	MOVQ    $-1, AX
	MOVQ    AX, pos+32(FP)
	MOVB    $0, bucketMask+40(FP)
	VZEROUPPER
	RET

found_candidate_1:
	// Candidate found in 32-byte vector! CX contains 32-bit mask.
	// Find position of first set bit
	BSFL    CX, AX                      // AX = position of first set bit (0-31)

	// Save chunk start pointer
	MOVQ    SI, R10                     // R10 = chunk start pointer

	// Calculate absolute position in haystack
	SUBQ    DI, SI                      // SI = offset from haystack start
	ADDQ    SI, AX                      // AX = absolute position

	// Get chunk offset for byte lookup
	MOVQ    AX, R11                     // R11 = absolute position
	SUBQ    SI, R11                     // R11 = chunk offset (0-31)

	// Load the candidate byte
	MOVBLZX (R10)(R11*1), BX            // BX = haystack[chunk_start + chunk_offset]

	// Extract nibbles
	MOVL    BX, CX
	ANDL    $0x0F, CX                   // CX = low nibble

	SHRL    $4, BX
	ANDL    $0x0F, BX                   // BX = high nibble

	// Lookup bucket bits
	MOVBLZX 8(R8)(CX*1), CX             // CX = loMasks[0][lowNibble]
	MOVBLZX 136(R8)(BX*1), BX           // BX = hiMasks[0][highNibble]

	// AND together to get bucket bits
	ANDL    BX, CX                      // CX = bucket bits

	// Return results
	MOVQ    AX, pos+32(FP)              // Return position
	MOVB    CL, bucketMask+40(FP)       // Return bucket MASK
	VZEROUPPER
	RET

found_candidate_16_1:
	// Candidate found in 16-byte portion of tail
	BSFL    CX, AX                      // AX = position of first set bit (0-15)

	// Calculate absolute position
	MOVQ    SI, R10                     // R10 = chunk start
	SUBQ    DI, SI                      // SI = offset from haystack start
	ADDQ    SI, AX                      // AX = absolute position

	// Get chunk offset
	MOVQ    AX, R11
	SUBQ    SI, R11                     // R11 = chunk offset (0-15)

	// Load candidate byte and lookup bucket mask
	MOVBLZX (R10)(R11*1), BX
	MOVL    BX, CX
	ANDL    $0x0F, CX
	SHRL    $4, BX
	ANDL    $0x0F, BX

	MOVBLZX 8(R8)(CX*1), CX
	MOVBLZX 136(R8)(BX*1), BX
	ANDL    BX, CX

	MOVQ    AX, pos+32(FP)
	MOVB    CL, bucketMask+40(FP)
	VZEROUPPER
	RET

found_scalar_1:
	// Candidate found in tail scalar loop
	SUBQ    DI, SI                      // SI = offset from haystack start

	MOVQ    SI, pos+32(FP)
	MOVB    AL, bucketMask+40(FP)       // Return bucket MASK
	VZEROUPPER
	RET

// func teddySlimAVX2_2(masks *teddyMasks, haystack []byte) (pos int, bucketMask uint8)
//
// AVX2 implementation of Teddy Slim with 2-byte fingerprint.
// Processes 32 bytes per iteration (2x throughput vs SSSE3).
// Reduces false positives by ~90% compared to 1-byte fingerprint.
//
// Algorithm:
//  1. Load nibble masks for positions 0 and 1 (32 bytes each)
//  2. Main loop: process 32 bytes per iteration
//     a. Load haystack[i:i+32] for position 0
//     b. Load haystack[i+1:i+33] for position 1 (overlapping)
//     c. For each position: extract nibbles, VPSHUFB lookup, VPAND lo/hi
//     d. VPAND results from both positions
//     e. Non-zero result = candidate
//
// Returns bucket MASK (not bucket ID) - caller iterates through all set bits.
//
// CRITICAL: VZEROUPPER before every RET.
//
// teddyMasks struct layout:
//   +0:   fingerprintLen (4 bytes)
//   +4:   padding (4 bytes)
//   +8:   loMasks[0] (32 bytes)
//   +40:  loMasks[1] (32 bytes)
//   +72:  loMasks[2] (32 bytes, unused)
//   +104: loMasks[3] (32 bytes, unused)
//   +136: hiMasks[0] (32 bytes)
//   +168: hiMasks[1] (32 bytes)
//   +200: hiMasks[2] (32 bytes, unused)
//   +232: hiMasks[3] (32 bytes, unused)
TEXT ·teddySlimAVX2_2(SB), NOSPLIT, $0-41
	// Load parameters
	MOVQ    masks+0(FP), R8             // R8 = pointer to teddyMasks
	MOVQ    haystack_base+8(FP), SI     // SI = haystack pointer
	MOVQ    haystack_len+16(FP), DX     // DX = haystack length

	// Empty haystack check
	TESTQ   DX, DX
	JZ      not_found_2

	// Check minimum length (need at least 2 bytes for 2-byte fingerprint)
	CMPQ    DX, $2
	JB      not_found_2

	// Load nibble masks for positions 0 and 1 (32 bytes each for AVX2)
	// Position 0: loMasks[0] at +8, hiMasks[0] at +136
	// Position 1: loMasks[1] at +40, hiMasks[1] at +168
	VMOVDQU 8(R8), Y0                   // Y0 = loMasks[0]
	VMOVDQU 136(R8), Y1                 // Y1 = hiMasks[0]
	VMOVDQU 40(R8), Y8                  // Y8 = loMasks[1]
	VMOVDQU 168(R8), Y9                 // Y9 = hiMasks[1]

	// Create nibble extraction mask: 0x0F repeated 32 times
	MOVQ    $0x0F0F0F0F0F0F0F0F, AX
	MOVQ    AX, X2
	VPBROADCASTQ X2, Y2                 // Y2 = [0x0F x 32]

	// Save original haystack pointer for offset calculation
	MOVQ    SI, DI                      // DI = haystack start (preserved)

	// Calculate end pointer (need 1 extra byte for overlapping load)
	LEAQ    (SI)(DX*1), R9              // R9 = SI + length (end pointer)
	SUBQ    $1, R9                      // Adjust for 2-byte fingerprint overlap

loop32_2:
	// Check if we have at least 32 bytes remaining
	LEAQ    32(SI), R10                 // R10 = SI + 32
	CMPQ    R10, R9                     // Compare with adjusted end pointer
	JA      handle_tail_2               // If R10 > R9, less than 32 bytes left

	// Load 32 bytes from haystack for position 0
	VMOVDQU (SI), Y3                    // Y3 = haystack[SI:SI+32]
	// Load 32 bytes from haystack for position 1 (offset by 1)
	VMOVDQU 1(SI), Y10                  // Y10 = haystack[SI+1:SI+33]

	// === Process position 0 ===
	// Extract low nibbles
	VPAND   Y2, Y3, Y4                  // Y4 = low nibbles

	// Extract high nibbles
	VPSRLW  $4, Y3, Y5                  // Shift right 4 bits
	VPAND   Y2, Y5, Y5                  // Y5 = high nibbles

	// VPSHUFB lookups for position 0
	VPSHUFB Y4, Y0, Y6                  // Y6 = loMasks[0] lookup
	VPSHUFB Y5, Y1, Y7                  // Y7 = hiMasks[0] lookup
	VPAND   Y7, Y6, Y6                  // Y6 = position 0 result

	// === Process position 1 ===
	// Extract low nibbles
	VPAND   Y2, Y10, Y4                 // Y4 = low nibbles

	// Extract high nibbles
	VPSRLW  $4, Y10, Y5                 // Shift right 4 bits
	VPAND   Y2, Y5, Y5                  // Y5 = high nibbles

	// VPSHUFB lookups for position 1
	VPSHUFB Y4, Y8, Y11                 // Y11 = loMasks[1] lookup
	VPSHUFB Y5, Y9, Y12                 // Y12 = hiMasks[1] lookup
	VPAND   Y12, Y11, Y11               // Y11 = position 1 result

	// === Combine both positions ===
	VPAND   Y11, Y6, Y6                 // Y6 = pos0 & pos1 (final result)

	// Detect non-zero bytes
	VPXOR   Y13, Y13, Y13               // Y13 = zero vector
	VPCMPEQB Y13, Y6, Y7                // Y7[i] = 0xFF if zero, else 0x00
	VPMOVMSKB Y7, CX                    // CX = bitmask where bytes were ZERO
	NOTL    CX                          // Invert: CX = bitmask where bytes were NON-ZERO

	// Check if any candidates found
	TESTL   CX, CX
	JNZ     found_candidate_2

	// No candidates, advance to next 32 bytes
	ADDQ    $32, SI
	JMP     loop32_2

handle_tail_2:
	// Add back the 1 we subtracted for overlap check
	ADDQ    $1, R9

	// Check if we have at least 17 bytes for 16-byte SIMD processing
	// (16 bytes + 1 for overlapping load)
	LEAQ    17(SI), R10
	CMPQ    R10, R9
	JA      scalar_tail_2               // Less than 17 bytes, use scalar

	// 17-32 bytes remaining: process 16 bytes with AVX2
	VBROADCASTI128 (SI), Y3             // Load 16 bytes, duplicate to both lanes
	VBROADCASTI128 1(SI), Y10           // Load 16 bytes at +1

	// Process position 0
	VPAND   Y2, Y3, Y4
	VPSRLW  $4, Y3, Y5
	VPAND   Y2, Y5, Y5
	VPSHUFB Y4, Y0, Y6
	VPSHUFB Y5, Y1, Y7
	VPAND   Y7, Y6, Y6

	// Process position 1
	VPAND   Y2, Y10, Y4
	VPSRLW  $4, Y10, Y5
	VPAND   Y2, Y5, Y5
	VPSHUFB Y4, Y8, Y11
	VPSHUFB Y5, Y9, Y12
	VPAND   Y12, Y11, Y11

	// Combine
	VPAND   Y11, Y6, Y6

	// Check non-zero (only care about low 16 bits)
	VPXOR   Y13, Y13, Y13
	VPCMPEQB Y13, Y6, Y7
	VPMOVMSKB Y7, CX
	NOTL    CX
	ANDL    $0xFFFF, CX                 // Only low 16 bits are valid

	TESTL   CX, CX
	JNZ     found_candidate_16_2

	// Advance
	ADDQ    $16, SI

scalar_tail_2:
	// Process remaining bytes with scalar loop
	CMPQ    SI, R9
	JAE     not_found_2

	// Need at least 2 bytes for fingerprint
	LEAQ    1(SI), R10
	CMPQ    R10, R9
	JAE     not_found_2

tail_loop_2:
	// Load two consecutive bytes
	MOVBLZX (SI), AX                    // AX = byte at position 0
	MOVBLZX 1(SI), R10                  // R10 = byte at position 1

	// === Position 0 lookup ===
	MOVL    AX, BX
	ANDL    $0x0F, BX                   // BX = low nibble pos0
	MOVL    AX, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble pos0

	MOVBLZX 8(R8)(BX*1), AX             // AX = loMasks[0][lowNibble]
	MOVBLZX 136(R8)(CX*1), CX           // CX = hiMasks[0][highNibble]
	ANDL    CX, AX                      // AX = pos0 bucket bits

	// === Position 1 lookup ===
	MOVL    R10, BX
	ANDL    $0x0F, BX                   // BX = low nibble pos1
	MOVL    R10, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble pos1

	MOVBLZX 40(R8)(BX*1), BX            // BX = loMasks[1][lowNibble]
	MOVBLZX 168(R8)(CX*1), CX           // CX = hiMasks[1][highNibble]
	ANDL    CX, BX                      // BX = pos1 bucket bits

	// === Combine ===
	ANDL    BX, AX                      // AX = pos0 & pos1

	// Check if any bucket matched
	TESTL   AX, AX
	JNZ     found_scalar_2

	// Advance to next byte
	INCQ    SI
	LEAQ    1(SI), R10
	CMPQ    R10, R9
	JB      tail_loop_2

not_found_2:
	MOVQ    $-1, AX
	MOVQ    AX, pos+32(FP)
	MOVB    $0, bucketMask+40(FP)
	VZEROUPPER
	RET

found_candidate_2:
	// Find first set bit in 32-bit mask
	BSFL    CX, AX                      // AX = position of first set bit (0-31)

	// Save chunk start
	MOVQ    SI, R10

	// Calculate absolute position
	SUBQ    DI, SI                      // SI = offset from haystack start
	ADDQ    SI, AX                      // AX = absolute position

	// Get chunk offset for byte lookup
	MOVQ    AX, R11
	SUBQ    SI, R11                     // R11 = chunk offset (0-31)

	// Load two consecutive bytes at candidate position
	MOVBLZX (R10)(R11*1), BX            // BX = byte at pos0
	MOVBLZX 1(R10)(R11*1), R12          // R12 = byte at pos1

	// === Position 0 nibble lookup ===
	MOVL    BX, CX
	ANDL    $0x0F, CX                   // CX = low nibble
	SHRL    $4, BX                      // BX = high nibble

	MOVBLZX 8(R8)(CX*1), CX             // CX = loMasks[0][low]
	MOVBLZX 136(R8)(BX*1), BX           // BX = hiMasks[0][high]
	ANDL    BX, CX                      // CX = pos0 bucket bits

	// === Position 1 nibble lookup ===
	MOVL    R12, BX
	ANDL    $0x0F, BX                   // BX = low nibble
	MOVL    R12, R13
	SHRL    $4, R13                     // R13 = high nibble

	MOVBLZX 40(R8)(BX*1), BX            // BX = loMasks[1][low]
	MOVBLZX 168(R8)(R13*1), R13         // R13 = hiMasks[1][high]
	ANDL    R13, BX                     // BX = pos1 bucket bits

	// === Combine bucket masks ===
	ANDL    BX, CX                      // CX = final bucket MASK

	// Return results
	MOVQ    AX, pos+32(FP)
	MOVB    CL, bucketMask+40(FP)       // Return bucket MASK
	VZEROUPPER
	RET

found_candidate_16_2:
	// Candidate found in 16-byte tail portion
	BSFL    CX, AX                      // AX = position of first set bit (0-15)

	// Calculate absolute position
	MOVQ    SI, R10
	SUBQ    DI, SI
	ADDQ    SI, AX

	// Get chunk offset
	MOVQ    AX, R11
	SUBQ    SI, R11

	// Load two bytes and lookup
	MOVBLZX (R10)(R11*1), BX
	MOVBLZX 1(R10)(R11*1), R12

	MOVL    BX, CX
	ANDL    $0x0F, CX
	SHRL    $4, BX
	MOVBLZX 8(R8)(CX*1), CX
	MOVBLZX 136(R8)(BX*1), BX
	ANDL    BX, CX

	MOVL    R12, BX
	ANDL    $0x0F, BX
	MOVL    R12, R13
	SHRL    $4, R13
	MOVBLZX 40(R8)(BX*1), BX
	MOVBLZX 168(R8)(R13*1), R13
	ANDL    R13, BX

	ANDL    BX, CX

	MOVQ    AX, pos+32(FP)
	MOVB    CL, bucketMask+40(FP)
	VZEROUPPER
	RET

found_scalar_2:
	// Calculate position
	SUBQ    DI, SI

	// Return results
	MOVQ    SI, pos+32(FP)
	MOVB    AL, bucketMask+40(FP)       // Return bucket MASK
	VZEROUPPER
	RET
