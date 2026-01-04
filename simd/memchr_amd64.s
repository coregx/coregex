//go:build amd64

#include "textflag.h"

// func memchrAVX2(haystack []byte, needle byte) int
//
// AVX2 implementation of memchr that searches for a single byte in a byte slice.
// Processes 32 bytes per iteration using 256-bit vector operations.
//
// Algorithm:
//  1. Broadcast needle to all 32 bytes of YMM0 register
//  2. Main loop: load 32 bytes, compare with VPCMPEQB, extract mask, check for match
//  3. If match found: use BSFL to find position of first set bit
//  4. Handle tail (< 32 bytes) with scalar loop
//  5. Always call VZEROUPPER before returning (critical for performance)
//
// Parameters (FP offsets):
//   haystack_base+0(FP)  - pointer to haystack data (8 bytes)
//   haystack_len+8(FP)   - haystack length (8 bytes)
//   haystack_cap+16(FP)  - haystack capacity (8 bytes, unused but part of slice)
//   needle+24(FP)        - needle byte (8 bytes, byte parameters use full word)
//   ret+32(FP)           - return value: index or -1 (8 bytes)
//
// Total argument frame size: 40 bytes (8+8+8+8+8)
TEXT ·memchrAVX2(SB), NOSPLIT, $0-40
	// Load parameters
	MOVQ    haystack_base+0(FP), SI     // SI = haystack pointer
	MOVQ    haystack_len+8(FP), DX      // DX = haystack length
	MOVBQZX needle+24(FP), AX           // AX = needle (zero-extend byte to qword)

	// Empty haystack check
	TESTQ   DX, DX
	JZ      not_found

	// Broadcast needle to all 32 bytes of YMM0
	// VPBROADCASTB replicates the low byte of source to all 32 positions
	MOVD    AX, X0                       // X0[0:31] = needle (rest zeros)
	VPBROADCASTB X0, Y0                  // Y0 = [needle × 32]

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
	VMOVDQU (SI), Y1                     // Y1 = haystack[SI:SI+32]

	// Compare each byte with needle
	// VPCMPEQB: for each byte position, set 0xFF if equal, 0x00 if not equal
	VPCMPEQB Y0, Y1, Y2                  // Y2 = comparison result mask

	// Convert vector comparison result to 32-bit integer mask
	// VPMOVMSKB: extract the high bit of each byte into a 32-bit mask
	// Bit i in result corresponds to byte i in source vector
	VPMOVMSKB Y2, CX                     // CX = 32-bit mask

	// Check if any byte matched (mask != 0)
	TESTL   CX, CX
	JNZ     found_in_vector              // Non-zero mask means match found

	// No match in this chunk, advance to next 32 bytes
	ADDQ    $32, SI
	JMP     loop32

handle_tail:
	// Process remaining bytes (0-31 bytes) with scalar loop
	// This handles the case where haystack length is not a multiple of 32

	// Check if we've already processed all bytes
	CMPQ    SI, R8
	JAE     not_found                    // If SI >= end, no bytes left

tail_loop:
	// Load one byte and compare
	MOVBLZX (SI), BX                     // BX = haystack[SI] (zero-extended)
	CMPB    BL, AL                       // Compare with needle
	JE      found_scalar                 // Match found

	// Advance to next byte
	INCQ    SI
	CMPQ    SI, R8                       // Check if reached end
	JB      tail_loop                    // Continue if SI < end

not_found:
	// No match found in entire haystack
	MOVQ    $-1, AX                      // Return -1
	MOVQ    AX, ret+32(FP)
	VZEROUPPER                           // Clear upper YMM bits (CRITICAL!)
	RET

found_in_vector:
	// Match found in vector! CX contains 32-bit mask with set bits at match positions.
	// Use BSFL (Bit Scan Forward Long) to find index of first set bit.
	//
	// BSFL: scans from bit 0 upward, finds first 1 bit, stores its index in destination
	// Example: CX = 0x00000010 (bit 4 set) → BX = 4
	BSFL    CX, BX                       // BX = index of first set bit (0-31)

	// Calculate absolute index in haystack
	SUBQ    DI, SI                       // SI = offset from start to current chunk
	ADDQ    SI, BX                       // BX = absolute index (chunk_offset + bit_position)
	MOVQ    BX, ret+32(FP)               // Return index
	VZEROUPPER                           // Clear upper YMM bits
	RET

found_scalar:
	// Match found in tail loop
	SUBQ    DI, SI                       // SI = offset from start
	MOVQ    SI, ret+32(FP)               // Return index
	VZEROUPPER                           // Clear upper YMM bits
	RET

// func memchr2AVX2(haystack []byte, needle1, needle2 byte) int
//
// AVX2 implementation of memchr2 that searches for either of two bytes.
// Uses parallel comparison with two broadcast vectors and combines results with VOR.
//
// Algorithm:
//  1. Broadcast needle1 to YMM0, needle2 to YMM1
//  2. For each 32-byte chunk: compare with both needles, OR the results
//  3. Extract mask and find first match position
//
// Parameters:
//   haystack_base+0(FP)  - pointer (8 bytes)
//   haystack_len+8(FP)   - length (8 bytes)
//   haystack_cap+16(FP)  - capacity (8 bytes, unused)
//   needle1+24(FP)       - first needle (8 bytes)
//   needle2+25(FP)       - second needle (8 bytes) - note: overlaps with needle1, Go packs byte params
//   ret+32(FP)           - return value (8 bytes)
//
// Total: 40 bytes (8+8+8+8+8)
TEXT ·memchr2AVX2(SB), NOSPLIT, $0-40
	// Load parameters
	MOVQ    haystack_base+0(FP), SI
	MOVQ    haystack_len+8(FP), DX
	MOVBQZX needle1+24(FP), AX
	MOVBQZX needle2+25(FP), BX

	// Empty check
	TESTQ   DX, DX
	JZ      not_found2

	// Broadcast both needles
	MOVD    AX, X0
	VPBROADCASTB X0, Y0                  // Y0 = [needle1 × 32]
	MOVD    BX, X1
	VPBROADCASTB X1, Y1                  // Y1 = [needle2 × 32]

	// Save start pointer
	MOVQ    SI, DI

	// Calculate end
	LEAQ    (SI)(DX*1), R8

loop32_2:
	LEAQ    32(SI), R9
	CMPQ    R9, R8
	JA      handle_tail2

	// Load chunk
	VMOVDQU (SI), Y2

	// Compare with both needles
	VPCMPEQB Y0, Y2, Y3                  // Y3 = matches with needle1
	VPCMPEQB Y1, Y2, Y4                  // Y4 = matches with needle2

	// Combine results with OR (match if either needle found)
	VPOR    Y3, Y4, Y3                   // Y3 = needle1_matches | needle2_matches

	// Extract mask
	VPMOVMSKB Y3, CX
	TESTL   CX, CX
	JNZ     found_in_vector2

	ADDQ    $32, SI
	JMP     loop32_2

handle_tail2:
	CMPQ    SI, R8
	JAE     not_found2

tail_loop2:
	MOVBLZX (SI), CX
	CMPB    CL, AL                       // Compare with needle1
	JE      found_scalar2
	CMPB    CL, BL                       // Compare with needle2
	JE      found_scalar2

	INCQ    SI
	CMPQ    SI, R8
	JB      tail_loop2

not_found2:
	MOVQ    $-1, AX
	MOVQ    AX, ret+32(FP)
	VZEROUPPER
	RET

found_in_vector2:
	BSFL    CX, CX
	SUBQ    DI, SI
	ADDQ    SI, CX
	MOVQ    CX, ret+32(FP)
	VZEROUPPER
	RET

found_scalar2:
	SUBQ    DI, SI
	MOVQ    SI, ret+32(FP)
	VZEROUPPER
	RET

// func memchr3AVX2(haystack []byte, needle1, needle2, needle3 byte) int
//
// AVX2 implementation of memchr3 that searches for any of three bytes.
// Uses parallel comparison with three broadcast vectors and combines results with VOR.
//
// Algorithm:
//  1. Broadcast needle1 to YMM0, needle2 to YMM1, needle3 to YMM2
//  2. For each 32-byte chunk: compare with all three needles, OR the results
//  3. Extract mask and find first match position
//
// Parameters:
//   haystack_base+0(FP)  - pointer (8 bytes)
//   haystack_len+8(FP)   - length (8 bytes)
//   haystack_cap+16(FP)  - capacity (8 bytes, unused)
//   needle1+24(FP)       - first needle (8 bytes)
//   needle2+25(FP)       - second needle (8 bytes)
//   needle3+26(FP)       - third needle (8 bytes)
//   ret+32(FP)           - return value (8 bytes)
//
// Total: 40 bytes (8+8+8+8+8)
TEXT ·memchr3AVX2(SB), NOSPLIT, $0-40
	// Load parameters
	MOVQ    haystack_base+0(FP), SI
	MOVQ    haystack_len+8(FP), DX
	MOVBQZX needle1+24(FP), AX
	MOVBQZX needle2+25(FP), BX
	MOVBQZX needle3+26(FP), R10

	// Empty check
	TESTQ   DX, DX
	JZ      not_found3

	// Broadcast all three needles
	MOVD    AX, X0
	VPBROADCASTB X0, Y0                  // Y0 = [needle1 × 32]
	MOVD    BX, X1
	VPBROADCASTB X1, Y1                  // Y1 = [needle2 × 32]
	MOVD    R10, X2
	VPBROADCASTB X2, Y2                  // Y2 = [needle3 × 32]

	// Save start pointer
	MOVQ    SI, DI

	// Calculate end
	LEAQ    (SI)(DX*1), R8

loop32_3:
	LEAQ    32(SI), R9
	CMPQ    R9, R8
	JA      handle_tail3

	// Load chunk
	VMOVDQU (SI), Y3

	// Compare with all three needles
	VPCMPEQB Y0, Y3, Y4                  // Y4 = matches with needle1
	VPCMPEQB Y1, Y3, Y5                  // Y5 = matches with needle2
	VPCMPEQB Y2, Y3, Y6                  // Y6 = matches with needle3

	// Combine results: match if any needle found
	VPOR    Y4, Y5, Y4                   // Y4 = needle1 | needle2
	VPOR    Y4, Y6, Y4                   // Y4 = (needle1 | needle2) | needle3

	// Extract mask
	VPMOVMSKB Y4, CX
	TESTL   CX, CX
	JNZ     found_in_vector3

	ADDQ    $32, SI
	JMP     loop32_3

handle_tail3:
	CMPQ    SI, R8
	JAE     not_found3

tail_loop3:
	MOVBLZX (SI), CX
	CMPB    CL, AL                       // Compare with needle1
	JE      found_scalar3
	CMPB    CL, BL                       // Compare with needle2
	JE      found_scalar3
	CMPB    CL, R10B                     // Compare with needle3
	JE      found_scalar3

	INCQ    SI
	CMPQ    SI, R8
	JB      tail_loop3

not_found3:
	MOVQ    $-1, AX
	MOVQ    AX, ret+32(FP)
	VZEROUPPER
	RET

found_in_vector3:
	BSFL    CX, CX
	SUBQ    DI, SI
	ADDQ    SI, CX
	MOVQ    CX, ret+32(FP)
	VZEROUPPER
	RET

found_scalar3:
	SUBQ    DI, SI
	MOVQ    SI, ret+32(FP)
	VZEROUPPER
	RET

// func memchrPairAVX2(haystack []byte, byte1, byte2 byte, offset int) int
//
// AVX2 implementation of paired-byte search. Finds positions where byte1 appears
// at position i and byte2 appears at position i+offset.
//
// This is crucial for efficient substring searching - by verifying two bytes at
// their correct relative positions, we dramatically reduce false positives.
//
// Algorithm:
//  1. Broadcast byte1 to YMM0, byte2 to YMM1
//  2. For each 32-byte chunk at position p:
//     - Load haystack[p:p+32], compare with byte1 → mask1
//     - Load haystack[p+offset:p+offset+32], compare with byte2 → mask2
//     - AND mask1, mask2 → combined mask (only where both bytes match at correct distance)
//  3. First set bit in combined mask is the answer
//
// Parameters:
//   haystack_base+0(FP)  - pointer (8 bytes)
//   haystack_len+8(FP)   - length (8 bytes)
//   haystack_cap+16(FP)  - capacity (8 bytes, unused)
//   byte1+24(FP)         - first byte to find (8 bytes)
//   byte2+25(FP)         - second byte to find (8 bytes)
//   offset+32(FP)        - distance between byte1 and byte2 (8 bytes)
//   ret+40(FP)           - return value (8 bytes)
//
// Total: 48 bytes (8+8+8+8+8+8)
TEXT ·memchrPairAVX2(SB), NOSPLIT, $0-48
	// Load parameters
	MOVQ    haystack_base+0(FP), SI      // SI = haystack pointer
	MOVQ    haystack_len+8(FP), DX       // DX = haystack length
	MOVBQZX byte1+24(FP), AX             // AX = byte1
	MOVBQZX byte2+25(FP), BX             // BX = byte2
	MOVQ    offset+32(FP), R10           // R10 = offset

	// Empty check and bounds check
	TESTQ   DX, DX
	JZ      not_found_pair
	CMPQ    R10, DX                      // offset >= length?
	JAE     not_found_pair

	// Broadcast both bytes
	MOVD    AX, X0
	VPBROADCASTB X0, Y0                  // Y0 = [byte1 × 32]
	MOVD    BX, X1
	VPBROADCASTB X1, Y1                  // Y1 = [byte2 × 32]

	// Save start pointer
	MOVQ    SI, DI                       // DI = haystack start

	// Calculate limits:
	// - We need 32 bytes at position p (for byte1 check)
	// - We need 32 bytes at position p+offset (for byte2 check)
	// - So we need p + offset + 32 <= length
	// - Which means p <= length - offset - 32
	MOVQ    DX, R8                       // R8 = length
	SUBQ    R10, R8                      // R8 = length - offset
	SUBQ    $32, R8                      // R8 = length - offset - 32 (last valid p)
	CMPQ    R8, $0
	JL      handle_tail_pair             // If negative, can't do vector loop

	// R8 now contains the last valid position for vector loop
	ADDQ    SI, R8                       // R8 = absolute last valid pointer

loop32_pair:
	// Check if SI <= R8 (still have room for full vector)
	CMPQ    SI, R8
	JA      handle_tail_pair

	// Load 32 bytes at position p (for byte1)
	VMOVDQU (SI), Y2                     // Y2 = haystack[p:p+32]

	// Load 32 bytes at position p+offset (for byte2)
	VMOVDQU (SI)(R10*1), Y3              // Y3 = haystack[p+offset:p+offset+32]

	// Compare byte1
	VPCMPEQB Y0, Y2, Y4                  // Y4 = positions where haystack[p+k] == byte1

	// Compare byte2
	VPCMPEQB Y1, Y3, Y5                  // Y5 = positions where haystack[p+offset+k] == byte2

	// AND the results: bit k is set only if:
	// - haystack[p+k] == byte1
	// - haystack[p+offset+k] == byte2
	// This means the pair (byte1, byte2) appears at (p+k, p+k+offset)
	VPAND   Y4, Y5, Y4                   // Y4 = combined mask

	// Extract mask
	VPMOVMSKB Y4, CX
	TESTL   CX, CX
	JNZ     found_in_vector_pair

	// No match in this chunk, advance by 32 bytes
	ADDQ    $32, SI
	JMP     loop32_pair

handle_tail_pair:
	// Process remaining positions byte-by-byte
	// We need SI + offset < end (DI + DX)
	LEAQ    (DI)(DX*1), R8               // R8 = end pointer

tail_loop_pair:
	// Check if SI + offset < end
	LEAQ    (SI)(R10*1), R9              // R9 = SI + offset
	CMPQ    R9, R8
	JAE     not_found_pair               // If SI + offset >= end, we're done

	// Load and compare both bytes
	MOVBLZX (SI), CX                     // CX = haystack[SI]
	CMPB    CL, AL                       // Compare with byte1
	JNE     tail_next_pair               // Not byte1, skip

	// byte1 matches, check byte2
	MOVBLZX (SI)(R10*1), CX              // CX = haystack[SI + offset]
	CMPB    CL, BL                       // Compare with byte2
	JE      found_scalar_pair            // Both match!

tail_next_pair:
	INCQ    SI
	JMP     tail_loop_pair

not_found_pair:
	MOVQ    $-1, AX
	MOVQ    AX, ret+40(FP)
	VZEROUPPER
	RET

found_in_vector_pair:
	// Match found in vector! CX contains mask
	BSFL    CX, CX                       // CX = index of first set bit (0-31)
	SUBQ    DI, SI                       // SI = offset from start to current chunk
	ADDQ    SI, CX                       // CX = absolute index
	MOVQ    CX, ret+40(FP)
	VZEROUPPER
	RET

found_scalar_pair:
	SUBQ    DI, SI
	MOVQ    SI, ret+40(FP)
	VZEROUPPER
	RET
