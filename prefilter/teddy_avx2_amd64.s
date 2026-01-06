//go:build amd64

#include "textflag.h"

// func fatTeddyAVX2_2(masks *fatTeddyMasks, haystack []byte) (pos int, bucketMask uint16)
//
// AVX2 implementation of Fat Teddy with 2-byte fingerprint and 16 buckets.
// This is optimal for 33-64 patterns on AVX2-capable CPUs.
//
// Algorithm (based on Rust aho-corasick generic.rs:447-512):
//  1. Load nibble masks (32 bytes each, covering all 16 buckets)
//  2. Main loop: process 16 bytes per iteration
//     a. VBROADCASTI128: Load 16 bytes, duplicate to both 128-bit lanes
//     b. Extract low/high nibbles with VPAND
//     c. VPSHUFB: Lookup bucket bits in masks
//        - Low lane (bytes 0-15) → buckets 0-7
//        - High lane (bytes 16-31) → buckets 8-15
//     d. VPAND: Combine lo/hi results for each position
//     e. VPAND: Combine results from both fingerprint positions
//     f. VPERM2I128 + VPUNPCKLBW: Interleave to create 16-bit bucket masks
//     g. VPMOVMSKB: Extract candidate mask
//  3. If candidate found: extract position and 16-bit bucket mask
//  4. Handle tail (< 16 bytes) with scalar loop
//
// CRITICAL: VZEROUPPER must be called before every RET to avoid AVX-SSE transition penalty.
//
// Parameters (FP offsets):
//   masks+0(FP)          - pointer to fatTeddyMasks struct (8 bytes)
//   haystack_base+8(FP)  - pointer to haystack data (8 bytes)
//   haystack_len+16(FP)  - haystack length (8 bytes)
//   haystack_cap+24(FP)  - haystack capacity (8 bytes, unused)
//   pos+32(FP)           - return: candidate position or -1 (8 bytes)
//   bucketMask+40(FP)    - return: 16-bit bucket mask or 0 (2 bytes)
//
// Total argument frame size: 42 bytes (8+8+8+8+8+2)
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

	// Save original haystack pointer for offset calculation
	MOVQ    SI, DI                      // DI = haystack start (preserved)

	// Calculate end pointer (need 1 extra byte for overlapping load)
	LEAQ    (SI)(DX*1), R9              // R9 = SI + length (end pointer)
	SUBQ    $1, R9                      // Adjust for 2-byte fingerprint overlap

loop16:
	// Check if we have at least 16 bytes remaining
	LEAQ    16(SI), R10                 // R10 = SI + 16
	CMPQ    R10, R9                     // Compare with adjusted end pointer
	JA      handle_tail                 // If R10 > R9, less than 16 bytes left

	// Load 16 bytes from haystack and broadcast to both lanes
	// VBROADCASTI128: loads 16 bytes and duplicates to both 128-bit lanes
	VBROADCASTI128 (SI), Y3             // Y3 = haystack[SI:SI+16] in both lanes
	VBROADCASTI128 1(SI), Y10           // Y10 = haystack[SI+1:SI+17] in both lanes

	// === Process position 0 ===
	// Extract low nibbles: Y3 & 0x0F
	VPAND   Y2, Y3, Y4                  // Y4 = low nibbles

	// Extract high nibbles: (Y3 >> 4) & 0x0F
	VPSRLW  $4, Y3, Y5                  // Shift right 4 bits
	VPAND   Y2, Y5, Y5                  // Y5 = high nibbles

	// VPSHUFB lookups for position 0
	// Low lane uses loMasks bytes 0-15, high lane uses bytes 16-31
	VPSHUFB Y4, Y0, Y6                  // Y6 = lo lookup results
	VPSHUFB Y5, Y1, Y7                  // Y7 = hi lookup results
	VPAND   Y7, Y6, Y6                  // Y6 = position 0 candidate mask

	// === Process position 1 ===
	// Extract low nibbles
	VPAND   Y2, Y10, Y4                 // Y4 = low nibbles

	// Extract high nibbles
	VPSRLW  $4, Y10, Y5                 // Shift right 4 bits
	VPAND   Y2, Y5, Y5                  // Y5 = high nibbles

	// VPSHUFB lookups for position 1
	VPSHUFB Y4, Y8, Y11                 // Y11 = lo lookup results
	VPSHUFB Y5, Y9, Y12                 // Y12 = hi lookup results
	VPAND   Y12, Y11, Y11               // Y11 = position 1 candidate mask

	// === Combine both positions ===
	VPAND   Y11, Y6, Y6                 // Y6 = pos0 & pos1 (both positions must match)

	// Now Y6 has the combined results:
	// - Low lane (Y6[0:16]): candidate bits for buckets 0-7
	// - High lane (Y6[16:32]): candidate bits for buckets 8-15
	//
	// We need to interleave these to create 16-bit bucket masks per position.
	// For position i in 0..15:
	//   bucket_mask[i] = (Y6[16+i] << 8) | Y6[i]
	//
	// Use VPERM2I128 to get lanes in order, then VPUNPCKLBW to interleave.

	// Swap lanes: Y13[0:16] = Y6[16:32], Y13[16:32] = Y6[0:16]
	VPERM2I128 $0x01, Y6, Y6, Y13       // Y13 = swapped lanes

	// Interleave low bytes: result[2i] = Y6[i], result[2i+1] = Y13[i]
	// This creates 16-bit values where low byte is from buckets 0-7, high byte from 8-15
	VPUNPCKLBW Y13, Y6, Y14             // Y14 = interleaved (first 16 positions)

	// Check if any 16-bit words in Y14 are non-zero
	// Extract mask from low 128 bits (covers positions 0-7)
	VPXOR   Y15, Y15, Y15               // Y15 = zero
	VPCMPEQW Y15, Y14, Y15              // Y15[i] = 0xFFFF if Y14[i]==0, else 0
	VPMOVMSKB Y15, CX                   // CX = byte mask (2 bits per position)
	XORL    $0xFFFFFFFF, CX             // Invert: 1 means NON-ZERO

	// Each 16-bit word produces 2 bits in the mask
	// Positions 0-7 are in bits 0-15 of CX
	// We need to check if any word is non-zero
	ANDL    $0xFFFF, CX                 // Keep only low 16 bits (positions 0-7)

	// Check if any candidates found
	TESTL   CX, CX
	JNZ     found_candidate

	// No candidates in this chunk, advance to next 16 bytes
	ADDQ    $16, SI
	JMP     loop16

handle_tail:
	// Add back the 1 we subtracted for overlap check
	ADDQ    $1, R9

	// Process remaining bytes with scalar loop
	CMPQ    SI, R9
	JAE     not_found

	// Need at least 2 bytes for fingerprint
	LEAQ    1(SI), R10
	CMPQ    R10, R9
	JAE     not_found

tail_loop:
	// Load two consecutive bytes
	MOVBLZX (SI), AX                    // AX = byte at position 0
	MOVBLZX 1(SI), R10                  // R10 = byte at position 1

	// === Position 0 lookup (buckets 0-7) ===
	MOVL    AX, BX
	ANDL    $0x0F, BX                   // BX = low nibble
	MOVL    AX, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble

	// Lookup in buckets 0-7 (loMasks[0][0:16] and hiMasks[0][0:16])
	MOVBLZX 8(R8)(BX*1), AX             // AX = loMasks[0][low] (buckets 0-7)
	MOVBLZX 136(R8)(CX*1), CX           // CX = hiMasks[0][high] (buckets 0-7)
	ANDL    CX, AX                      // AX = pos0 bucket bits (low 8)

	// === Position 0 lookup (buckets 8-15) ===
	MOVBLZX (SI), R11                   // Reload byte
	MOVL    R11, BX
	ANDL    $0x0F, BX                   // BX = low nibble
	MOVL    R11, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble

	MOVBLZX 24(R8)(BX*1), BX            // BX = loMasks[0][16+low] (buckets 8-15)
	MOVBLZX 152(R8)(CX*1), CX           // CX = hiMasks[0][16+high] (buckets 8-15)
	ANDL    CX, BX                      // BX = pos0 bucket bits (high 8)
	SHLL    $8, BX                      // Shift to high byte
	ORL     BX, AX                      // AX = 16-bit pos0 bucket mask

	// === Position 1 lookup (buckets 0-7) ===
	MOVL    R10, BX
	ANDL    $0x0F, BX                   // BX = low nibble
	MOVL    R10, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble

	MOVBLZX 40(R8)(BX*1), R11           // R11 = loMasks[1][low] (buckets 0-7)
	MOVBLZX 168(R8)(CX*1), CX           // CX = hiMasks[1][high] (buckets 0-7)
	ANDL    CX, R11                     // R11 = pos1 bucket bits (low 8)

	// === Position 1 lookup (buckets 8-15) ===
	MOVBLZX 1(SI), R12                  // Reload byte
	MOVL    R12, BX
	ANDL    $0x0F, BX                   // BX = low nibble
	MOVL    R12, CX
	SHRL    $4, CX
	ANDL    $0x0F, CX                   // CX = high nibble

	MOVBLZX 56(R8)(BX*1), BX            // BX = loMasks[1][16+low] (buckets 8-15)
	MOVBLZX 184(R8)(CX*1), CX           // CX = hiMasks[1][16+high] (buckets 8-15)
	ANDL    CX, BX                      // BX = pos1 bucket bits (high 8)
	SHLL    $8, BX                      // Shift to high byte
	ORL     BX, R11                     // R11 = 16-bit pos1 bucket mask

	// === Combine ===
	ANDL    R11, AX                     // AX = pos0 & pos1 (16-bit)

	// Check if any bucket matched
	TESTL   AX, AX
	JNZ     found_scalar

	// Advance to next byte
	INCQ    SI
	LEAQ    1(SI), R10
	CMPQ    R10, R9
	JB      tail_loop

not_found:
	// No candidate found
	MOVQ    $-1, AX
	MOVQ    AX, pos+32(FP)
	MOVW    $0, bucketMask+40(FP)
	VZEROUPPER                          // CRITICAL: Required before RET after AVX2 usage
	RET

found_candidate:
	// Candidate found! CX contains mask where pairs of bits indicate non-zero words.
	// Each position uses 2 bits in the mask (because VPMOVMSKB extracts byte masks).
	// Position i is non-zero if bits 2i and 2i+1 are set.
	//
	// Find first position with non-zero word:
	// - BSFL finds lowest set bit
	// - Divide by 2 to get word position

	BSFL    CX, AX                      // AX = lowest set bit position
	SHRL    $1, AX                      // AX = word position (0-7)

	// Save chunk start for byte lookup
	MOVQ    SI, R10

	// Calculate absolute position in haystack
	SUBQ    DI, SI                      // SI = chunk offset from haystack start
	ADDQ    SI, AX                      // AX = absolute position

	// Extract the 16-bit bucket mask at the found position
	// The bucket mask is in Y14 at word position (AX - SI) = original word index
	MOVQ    AX, R11
	SUBQ    SI, R11                     // R11 = word position within chunk (0-7)

	// Load 16-bit bucket mask from the result vector
	// Each word in Y14 is a 16-bit bucket mask
	// Position R11 * 2 bytes into the vector
	SHLL    $1, R11                     // R11 = byte offset (word * 2)

	// Extract word from Y14 - need to do scalar reload since we can't easily index YMM
	// Reload and compute bucket mask directly from haystack bytes
	MOVBLZX (R10)(AX*1), BX             // BX = byte at position 0 (relative to chunk)
	// Correction: AX now has absolute position, need chunk-relative offset
	MOVQ    AX, R12
	ADDQ    DI, R12                     // R12 = absolute pointer
	SUBQ    R10, R12                    // R12 = offset from chunk start
	MOVBLZX (R10)(R12*1), BX            // BX = byte at pos0

	// Position 0 bucket mask (buckets 0-7)
	MOVL    BX, CX
	ANDL    $0x0F, CX                   // CX = low nibble
	MOVL    BX, R13
	SHRL    $4, R13
	ANDL    $0x0F, R13                  // R13 = high nibble

	MOVBLZX 8(R8)(CX*1), CX             // CX = loMasks[0][low]
	MOVBLZX 136(R8)(R13*1), R13         // R13 = hiMasks[0][high]
	ANDL    R13, CX                     // CX = pos0 buckets 0-7

	// Position 0 bucket mask (buckets 8-15)
	MOVL    BX, R13
	ANDL    $0x0F, R13                  // R13 = low nibble
	MOVL    BX, R14
	SHRL    $4, R14
	ANDL    $0x0F, R14                  // R14 = high nibble

	MOVBLZX 24(R8)(R13*1), R13          // R13 = loMasks[0][16+low]
	MOVBLZX 152(R8)(R14*1), R14         // R14 = hiMasks[0][16+high]
	ANDL    R14, R13                    // R13 = pos0 buckets 8-15
	SHLL    $8, R13                     // Shift to high byte
	ORL     R13, CX                     // CX = 16-bit pos0 mask

	// Position 1 byte
	MOVBLZX 1(R10)(R12*1), BX           // BX = byte at pos1

	// Position 1 bucket mask (buckets 0-7)
	MOVL    BX, R13
	ANDL    $0x0F, R13                  // R13 = low nibble
	MOVL    BX, R14
	SHRL    $4, R14
	ANDL    $0x0F, R14                  // R14 = high nibble

	MOVBLZX 40(R8)(R13*1), R13          // R13 = loMasks[1][low]
	MOVBLZX 168(R8)(R14*1), R14         // R14 = hiMasks[1][high]
	ANDL    R14, R13                    // R13 = pos1 buckets 0-7

	// Position 1 bucket mask (buckets 8-15)
	MOVL    BX, R14
	ANDL    $0x0F, R14                  // R14 = low nibble
	MOVL    BX, R15
	SHRL    $4, R15
	ANDL    $0x0F, R15                  // R15 = high nibble

	MOVBLZX 56(R8)(R14*1), R14          // R14 = loMasks[1][16+low]
	MOVBLZX 184(R8)(R15*1), R15         // R15 = hiMasks[1][16+high]
	ANDL    R15, R14                    // R14 = pos1 buckets 8-15
	SHLL    $8, R14                     // Shift to high byte
	ORL     R14, R13                    // R13 = 16-bit pos1 mask

	// Combine pos0 and pos1
	ANDL    R13, CX                     // CX = final 16-bit bucket mask

	// Return results
	MOVQ    AX, pos+32(FP)              // Return position
	MOVW    CX, bucketMask+40(FP)       // Return 16-bit bucket mask
	VZEROUPPER                          // CRITICAL: Required before RET
	RET

found_scalar:
	// Calculate position
	SUBQ    DI, SI

	// Return results
	MOVQ    SI, pos+32(FP)
	MOVW    AX, bucketMask+40(FP)       // Return 16-bit bucket mask
	VZEROUPPER                          // CRITICAL: Required before RET
	RET
