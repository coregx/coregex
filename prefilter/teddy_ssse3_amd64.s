//go:build amd64

#include "textflag.h"

// func teddySlimSSSE3_1(masks *teddyMasks, haystack []byte) (pos, bucket int)
//
// SSSE3 implementation of Teddy Slim with 1-byte fingerprint.
// Searches for multiple patterns (2-8) simultaneously using SIMD shuffle instructions.
//
// Algorithm:
//  1. Load nibble masks for position 0 from masks struct
//  2. Main loop: process 16 bytes per iteration
//     a. Load 16 bytes from haystack
//     b. Extract low nibbles (byte & 0x0F) and high nibbles (byte >> 4)
//     c. Use PSHUFB to lookup bucket bits from masks
//     d. AND lo/hi results to find candidates
//     e. Extract bitmask and check for candidates
//  3. If candidate found: find first set bit position, extract bucket ID
//  4. Handle tail (< 16 bytes) with scalar loop
//
// CRITICAL: This function does NOT call VZEROUPPER because it uses only SSE/SSSE3 (XMM registers).
// VZEROUPPER is only needed after using AVX/AVX2 (YMM registers).
//
// Parameters (FP offsets):
//   masks+0(FP)          - pointer to teddyMasks struct (8 bytes)
//   haystack_base+8(FP)  - pointer to haystack data (8 bytes)
//   haystack_len+16(FP)  - haystack length (8 bytes)
//   haystack_cap+24(FP)  - haystack capacity (8 bytes, unused)
//   pos+32(FP)           - return: candidate position or -1 (8 bytes)
//   bucket+40(FP)        - return: bucket ID or -1 (8 bytes)
//
// Total argument frame size: 48 bytes (8+8+8+8+8+8)
//
// teddyMasks struct layout (offsets):
//   +0:  fingerprintLen (uint32, 4 bytes)
//   +4:  padding (4 bytes)
//   +8:  loMasks[0] (32 bytes, we use first 16)
//   +40: loMasks[1] (32 bytes, unused for 1-byte fingerprint)
//   +72: loMasks[2] (32 bytes, unused)
//   +104: loMasks[3] (32 bytes, unused)
//   +136: hiMasks[0] (32 bytes, we use first 16)
//   +168: hiMasks[1] (32 bytes, unused)
//   +200: hiMasks[2] (32 bytes, unused)
//   +232: hiMasks[3] (32 bytes, unused)
TEXT ·teddySlimSSSE3_1(SB), NOSPLIT, $0-48
	// Load parameters
	MOVQ    masks+0(FP), R8             // R8 = pointer to teddyMasks
	MOVQ    haystack_base+8(FP), SI     // SI = haystack pointer
	MOVQ    haystack_len+16(FP), DX     // DX = haystack length

	// Empty haystack check
	TESTQ   DX, DX
	JZ      not_found

	// Check minimum length (need at least 1 byte for fingerprint)
	CMPQ    DX, $1
	JB      not_found

	// Load nibble masks for position 0
	// loMasks[0] at offset 8, hiMasks[0] at offset 136
	MOVOU   8(R8), X0                   // X0 = loMasks[0] (16 bytes)
	MOVOU   136(R8), X1                 // X1 = hiMasks[0] (16 bytes)

	// Create nibble extraction mask: 0x0F repeated 16 times
	// This will be used to mask out high bits when extracting nibbles
	MOVQ    $0x0F0F0F0F0F0F0F0F, AX
	MOVQ    AX, X2
	PUNPCKLQDQ X2, X2                   // X2 = [0x0F x 16]

	// Save original haystack pointer for offset calculation
	MOVQ    SI, DI                      // DI = haystack start (preserved)

	// Calculate end pointer
	LEAQ    (SI)(DX*1), R9              // R9 = SI + length (end pointer)

// Main loop: process 16 bytes per iteration
loop16:
	// Check if we have at least 16 bytes remaining
	LEAQ    16(SI), R10                 // R10 = SI + 16
	CMPQ    R10, R9                     // Compare with end pointer
	JA      handle_tail                 // If R10 > R9, less than 16 bytes left

	// Load 16 bytes from haystack (unaligned load)
	MOVOU   (SI), X3                    // X3 = haystack[SI:SI+16]

	// Extract low nibbles: X3 & 0x0F
	MOVOA   X3, X4                      // X4 = copy of haystack bytes
	PAND    X2, X4                      // X4 = low nibbles (byte & 0x0F)

	// Extract high nibbles: (X3 >> 4) & 0x0F
	MOVOA   X3, X5                      // X5 = copy of haystack bytes
	PSRLW   $4, X5                      // Shift right 4 bits (works on 16-bit words)
	PAND    X2, X5                      // X5 = high nibbles (mask to 4 bits)

	// PSHUFB lookups: use nibbles as indices into mask tables
	// PSHUFB: for each byte position i, result[i] = table[index[i] & 0x0F]
	// If index[i] & 0x80, result[i] = 0x00 (that's why we masked to 0x0F)
	MOVOA   X0, X6                      // X6 = copy of loMasks[0]
	PSHUFB  X4, X6                      // X6 = lo_mask lookup results (bucket bits for low nibbles)

	MOVOA   X1, X7                      // X7 = copy of hiMasks[0]
	PSHUFB  X5, X7                      // X7 = hi_mask lookup results (bucket bits for high nibbles)

	// Combine results: candidate must match BOTH low and high nibbles
	// This is the key filtering step: a byte matches only if both nibbles match
	PAND    X7, X6                      // X6 = lo_mask & hi_mask (final bucket candidates)

	// CRITICAL: Detect non-zero bytes in result vector
	// PMOVMSKB extracts bit 7 of each byte, but bucket bits are 0x01-0x80
	// So bytes with only low bucket bits (0x01-0x7F) would NOT be detected!
	// Fix: Compare with zero first, then invert the result
	//
	// Algorithm:
	//   1. PCMPEQB with zero: bytes that ARE zero become 0xFF, non-zero become 0x00
	//   2. PMOVMSKB: extract high bits (0xFF->1, 0x00->0)
	//   3. XOR with 0xFFFF to invert: now 1 means NON-ZERO (candidate found)
	PXOR    X8, X8                      // X8 = zero vector
	PCMPEQB X8, X6                      // X6[i] = 0xFF if bucket_bits==0, else 0x00
	PMOVMSKB X6, CX                     // CX = bitmask where bytes were ZERO
	XORL    $0xFFFF, CX                 // Invert: CX = bitmask where bytes were NON-ZERO

	// Check if any candidates found (mask != 0)
	TESTL   CX, CX
	JNZ     found_candidate             // Non-zero mask means candidate found

	// No candidates in this chunk, advance to next 16 bytes
	ADDQ    $16, SI
	JMP     loop16

handle_tail:
	// Process remaining bytes (0-15 bytes) with scalar loop
	// This is necessary when haystack length is not a multiple of 16

	// Check if we've already processed all bytes
	CMPQ    SI, R9
	JAE     not_found                   // If SI >= end, no bytes left

	// For tail, we could:
	// 1. Process byte-by-byte (slow but correct)
	// 2. Use overlapping 16-byte load (fast but complex)
	// We choose option 1 for correctness and simplicity

tail_loop:
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
	JNZ     found_scalar                // Non-zero means candidate found

	// Advance to next byte
	INCQ    SI
	CMPQ    SI, R9
	JB      tail_loop                   // Continue if SI < end

not_found:
	// No candidate found in entire haystack
	MOVQ    $-1, AX
	MOVQ    AX, pos+32(FP)
	MOVQ    $-1, BX
	MOVQ    BX, bucket+40(FP)
	RET                                 // No VZEROUPPER needed (SSSE3 only uses XMM)

found_candidate:
	// Candidate found in vector! CX contains 16-bit mask.
	// Each set bit corresponds to a candidate position in the 16-byte chunk.
	//
	// We need to:
	// 1. Find position of first set bit (BSF/TZCNT)
	// 2. Extract bucket ID from X6 at that position
	// 3. Return (position, bucket)

	// Find first set bit in mask (BSFL = Bit Scan Forward)
	// This gives us the byte offset within the 16-byte chunk
	BSFL    CX, AX                      // AX = position of first set bit (0-15)

	// CRITICAL: Save SI before we modify it (we need it for byte lookup)
	MOVQ    SI, R10                     // R10 = chunk start pointer (preserved)

	// Calculate absolute position in haystack
	SUBQ    DI, SI                      // SI = offset from haystack start
	ADDQ    SI, AX                      // AX = haystack offset + chunk offset (absolute position)

	// Now we need bucket ID from the candidate byte
	// R10 still points to chunk start, AX has the absolute position
	// But we need the chunk-relative offset for byte lookup
	// Chunk offset = AX - SI (because AX = SI + chunk_offset from above)
	MOVQ    AX, R11                     // R11 = absolute position
	SUBQ    SI, R11                     // R11 = chunk offset (0-15)

	// Load the candidate byte using chunk start (R10) + chunk offset (R11)
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
	ANDL    BX, CX                      // CX = bucket bits (byte with bits set for matching buckets)

	// Find first set bucket bit (0-7)
	// We use BSF to find lowest set bit
	BSFL    CX, BX                      // BX = first set bit position (bucket ID)

	// Return results
	MOVQ    AX, pos+32(FP)              // Return position (absolute offset)
	MOVQ    BX, bucket+40(FP)           // Return bucket ID (0-7)
	RET                                 // No VZEROUPPER needed (SSSE3)

found_scalar:
	// Candidate found in tail (scalar loop)
	// AX contains bucket bits, SI points to candidate byte

	// Calculate position (SI is already advanced to candidate position)
	SUBQ    DI, SI                      // SI = offset from haystack start

	// Find first set bucket bit
	BSFL    AX, BX                      // BX = bucket ID (0-7)

	// Return results
	MOVQ    SI, pos+32(FP)
	MOVQ    BX, bucket+40(FP)
	RET                                 // No VZEROUPPER needed (SSSE3)
