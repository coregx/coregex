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
TEXT ·fatTeddyAVX2_2(SB), NOSPLIT, $32-42
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

	// === Check for any non-zero byte in candidate vector ===
	// VPTEST sets ZF if (Y6 AND Y6) == 0, i.e., all zero.
	// This is 1 instruction vs 8 (VPCMPEQB+VPMOVMSKB+NOTL+ORL+TESTL).
	VPTEST  Y6, Y6
	JNZ     found_candidate

	// No candidates in this chunk, advance
	ADDQ    $16, SI
	JMP     loop16

handle_tail:
	// Cover the prev0 carry-over position (k*16) that the SIMD loop missed.
	// The 2-byte fingerprint algorithm uses VPALIGNR to carry prev0 between
	// SIMD iterations. When exiting to tail, position k*16 hasn't been checked
	// because the SIMD iteration that would use prev0 never ran.
	DECQ    SI

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
	// Y6 is non-zero (VPTEST confirmed). Extract position + bucket mask.
	// Y6[0:15] = buckets 0-7, Y6[16:31] = buckets 8-15 for same 16 positions.
	//
	// Extract position mask: find which bytes are non-zero (either lane).
	VPXOR   Y12, Y12, Y12               // Y12 = zero
	VPCMPEQB Y12, Y6, Y13              // Y13[i] = 0xFF if Y6[i]==0
	VPMOVMSKB Y13, CX                  // CX = zero mask (1=zero)
	NOTL    CX                          // CX = non-zero mask
	// Combine lanes: position has candidate if either lane non-zero
	MOVL    CX, AX
	SHRL    $16, CX
	ORL     CX, AX                      // AX = 16-bit position mask
	// Find first candidate position
	BSFL    AX, BX                      // BX = first position (0-15)

	// Calculate absolute position: (cur - start - 1) + BX
	// Note: we started at start+1, so actual match position is cur-1+BX relative to start
	MOVQ    SI, AX
	SUBQ    DI, AX                      // AX = cur - start
	DECQ    AX                          // AX = cur - start - 1
	ADDQ    BX, AX                      // AX = absolute position

	// Extract 16-bit bucket mask directly from Y6 (the candidate vector).
	// Spill Y6 to stack and read Y6[BX] (buckets 0-7) and Y6[16+BX] (buckets 8-15).
	// This avoids the broken scalar re-derivation from haystack bytes which gives
	// wrong results after VPALIGNR shift.
	VMOVDQU Y6, (SP)                    // Spill Y6 to stack (32 bytes at SP)

	// Read bucket masks for position BX directly from spilled vector
	MOVBLZX (SP)(BX*1), R12            // R12 = Y6[BX] = buckets 0-7 for this position
	ADDQ    $16, BX
	MOVBLZX (SP)(BX*1), R13            // R13 = Y6[16+BX_orig] = buckets 8-15
	SHLL    $8, R13
	ORL     R13, R12                    // R12 = 16-bit bucket mask

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

// func fatTeddyAVX2_2_batch(masks *fatTeddyMasks, haystack []byte, buf []uint32) int
//
// Batch version: scans entire haystack, writes ALL candidates to buf.
// Each candidate = (position << 16) | bucketMask packed in uint32.
// Returns number of candidates written.
// Masks loaded ONCE, prev0 maintained throughout — no round-trip overhead.
//
// FP layout:
//   masks+0(FP)           ptr (8)
//   haystack_base+8(FP)   ptr (8)
//   haystack_len+16(FP)   int (8)
//   haystack_cap+24(FP)   int (8)
//   buf_base+32(FP)       ptr (8)
//   buf_len+40(FP)        int (8)
//   buf_cap+48(FP)        int (8)
//   ret+56(FP)            int (8)
TEXT ·fatTeddyAVX2_2_batch(SB), NOSPLIT, $32-64
	MOVQ    masks+0(FP), R8
	MOVQ    haystack_base+8(FP), SI
	MOVQ    haystack_len+16(FP), DX
	MOVQ    buf_base+32(FP), R14        // R14 = output buffer pointer
	MOVQ    buf_len+40(FP), R15         // R15 = buffer capacity
	XORQ    R13, R13                    // R13 = candidate count = 0

	TESTQ   DX, DX
	JZ      batch_done
	CMPQ    DX, $2
	JL      batch_done

	// Load masks
	VMOVDQU 8(R8), Y0
	VMOVDQU 136(R8), Y1
	VMOVDQU 40(R8), Y8
	VMOVDQU 168(R8), Y9
	MOVQ    $0x0F0F0F0F0F0F0F0F, AX
	MOVQ    AX, X2
	VPBROADCASTQ X2, Y2
	VPCMPEQD Y7, Y7, Y7                // prev0 = 0xFF

	MOVQ    SI, DI                      // DI = start
	INCQ    SI                          // cur = start + 1
	LEAQ    (DI)(DX*1), R9              // R9 = end

batch_loop16:
	LEAQ    16(SI), R10
	CMPQ    R10, R9
	JA      batch_tail

	VBROADCASTI128 (SI), Y3
	VPAND   Y2, Y3, Y4
	VPSRLW  $4, Y3, Y5
	VPAND   Y2, Y5, Y5
	VPSHUFB Y4, Y0, Y6
	VPSHUFB Y5, Y1, Y10
	VPAND   Y10, Y6, Y6
	VPSHUFB Y4, Y8, Y10
	VPSHUFB Y5, Y9, Y11
	VPAND   Y11, Y10, Y10
	VPALIGNR $15, Y7, Y6, Y11
	VMOVDQA Y6, Y7
	VPAND   Y10, Y11, Y6

	VPTEST  Y6, Y6
	JNZ     batch_found

	ADDQ    $16, SI
	JMP     batch_loop16

batch_found:
	// Extract position mask (ORL of lanes) and iterate all candidates
	VPXOR   Y12, Y12, Y12
	VPCMPEQB Y12, Y6, Y12
	VPMOVMSKB Y12, CX
	NOTL    CX
	MOVL    CX, AX
	SHRL    $16, CX
	ORL     CX, AX                      // AX = 16-bit position mask
	// Spill Y6 for bucket extraction
	VMOVDQU Y6, (SP)

batch_iterate_bits:
	TESTL   AX, AX
	JZ      batch_next_chunk

	BSFL    AX, BX                      // BX = bit position (0-15)
	BTRL    BX, AX                      // Clear bit in AX

	// Calculate absolute position
	MOVQ    SI, CX
	SUBQ    DI, CX                      // CX = cur - start
	DECQ    CX                          // CX = cur - start - 1
	ADDQ    BX, CX                      // CX = absolute position

	// Extract bucket mask from spilled Y6
	MOVBLZX (SP)(BX*1), R10             // R10 = buckets 0-7
	LEAQ    16(BX), R11
	MOVBLZX (SP)(R11*1), R11            // R11 = buckets 8-15
	SHLL    $8, R11
	ORL     R11, R10                    // R10 = 16-bit bucket mask

	// Pack: (position << 16) | bucketMask into uint64
	SHLQ    $16, CX
	ORQ     R10, CX

	// Write to buffer if space available
	CMPQ    R13, R15
	JAE     batch_done_vzeroupper        // Buffer full
	MOVQ    CX, (R14)(R13*8)            // buf[count] = packed candidate (uint64)
	INCQ    R13

	JMP     batch_iterate_bits

batch_next_chunk:
	ADDQ    $16, SI
	JMP     batch_loop16

batch_tail:
	// Scalar tail — same as original but writes to buffer
	DECQ    SI                          // Cover prev0 position

batch_tail_check:
	LEAQ    1(SI), R10
	CMPQ    R10, R9
	JAE     batch_done_vzeroupper

	// Scalar 2-byte fingerprint check
	MOVBLZX (SI), AX
	MOVBLZX 1(SI), R10

	MOVL    AX, BX
	ANDL    $0x0F, BX
	MOVL    AX, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX
	MOVBLZX 8(R8)(BX*1), R11
	MOVBLZX 136(R8)(CX*1), R12
	ANDL    R12, R11                    // pos0 lo
	MOVBLZX 24(R8)(BX*1), R12
	MOVBLZX 152(R8)(CX*1), AX
	ANDL    AX, R12                     // pos0 hi

	MOVL    R10, BX
	ANDL    $0x0F, BX
	MOVL    R10, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX
	MOVBLZX 40(R8)(BX*1), AX
	MOVBLZX 168(R8)(CX*1), R10
	ANDL    R10, AX                     // pos1 lo
	MOVBLZX 56(R8)(BX*1), R10
	MOVBLZX 184(R8)(CX*1), CX
	ANDL    CX, R10                     // pos1 hi

	ANDL    AX, R11                     // combine lo
	ANDL    R10, R12                    // combine hi
	MOVL    R11, AX
	SHLL    $8, R12
	ORL     R12, AX                     // 16-bit bucket mask

	TESTL   AX, AX
	JZ      batch_tail_next

	// Write scalar candidate (uint64)
	CMPQ    R13, R15
	JAE     batch_done_vzeroupper
	MOVQ    SI, CX
	SUBQ    DI, CX                      // position
	SHLQ    $16, CX
	ORQ     AX, CX
	MOVQ    CX, (R14)(R13*8)
	INCQ    R13

batch_tail_next:
	INCQ    SI
	JMP     batch_tail_check

batch_done_vzeroupper:
	VZEROUPPER

batch_done:
	MOVQ    R13, ret+56(FP)
	RET
