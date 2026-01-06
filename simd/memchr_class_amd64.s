//go:build amd64

#include "textflag.h"

// func memchrWordAVX2(haystack []byte) int
//
// AVX2 implementation to find first word character [A-Za-z0-9_].
// Processes 32 bytes per iteration using range comparisons.
//
// Algorithm for checking if byte B is in range [lo, hi]:
//   clamped = max(min(B, hi), lo)
//   inRange = (B == clamped) ? 0xFF : 0x00
//
// Word character = [A-Z] | [a-z] | [0-9] | '_'
//
// Parameters:
//   haystack_base+0(FP)  - pointer to haystack (8 bytes)
//   haystack_len+8(FP)   - length (8 bytes)
//   haystack_cap+16(FP)  - capacity (8 bytes, unused)
//   ret+24(FP)           - return value (8 bytes)
//
TEXT ·memchrWordAVX2(SB), NOSPLIT, $0-32
	// Load parameters
	MOVQ    haystack_base+0(FP), SI    // SI = haystack pointer
	MOVQ    haystack_len+8(FP), DX     // DX = length

	// Empty check
	TESTQ   DX, DX
	JZ      word_not_found

	// Save start for offset calculation
	MOVQ    SI, DI                     // DI = start pointer

	// Calculate end pointer
	LEAQ    (SI)(DX*1), R8             // R8 = end pointer

	// Broadcast range constants to YMM registers
	// [A-Z]: 65-90
	MOVQ    $65, AX
	MOVD    AX, X8
	VPBROADCASTB X8, Y8                // Y8 = [65, 65, ...] = 'A'

	MOVQ    $90, AX
	MOVD    AX, X9
	VPBROADCASTB X9, Y9                // Y9 = [90, 90, ...] = 'Z'

	// [a-z]: 97-122
	MOVQ    $97, AX
	MOVD    AX, X10
	VPBROADCASTB X10, Y10              // Y10 = [97, 97, ...] = 'a'

	MOVQ    $122, AX
	MOVD    AX, X11
	VPBROADCASTB X11, Y11              // Y11 = [122, 122, ...] = 'z'

	// [0-9]: 48-57
	MOVQ    $48, AX
	MOVD    AX, X12
	VPBROADCASTB X12, Y12              // Y12 = [48, 48, ...] = '0'

	MOVQ    $57, AX
	MOVD    AX, X13
	VPBROADCASTB X13, Y13              // Y13 = [57, 57, ...] = '9'

	// '_': 95
	MOVQ    $95, AX
	MOVD    AX, X14
	VPBROADCASTB X14, Y14              // Y14 = [95, 95, ...] = '_'

word_loop32:
	// Check if we have at least 32 bytes remaining
	LEAQ    32(SI), R9
	CMPQ    R9, R8
	JA      word_handle_tail

	// Load 32 bytes
	VMOVDQU (SI), Y0                   // Y0 = data

	// Check [A-Z]: clamp to [65, 90], compare
	VPMINUB Y0, Y9, Y1                 // Y1 = min(data, 90)
	VPMAXUB Y1, Y8, Y1                 // Y1 = max(min(data, 90), 65)
	VPCMPEQB Y0, Y1, Y2                // Y2 = (data == clamped) for [A-Z]

	// Check [a-z]: clamp to [97, 122], compare
	VPMINUB Y0, Y11, Y1                // Y1 = min(data, 122)
	VPMAXUB Y1, Y10, Y1                // Y1 = max(min(data, 122), 97)
	VPCMPEQB Y0, Y1, Y3                // Y3 = (data == clamped) for [a-z]

	// Check [0-9]: clamp to [48, 57], compare
	VPMINUB Y0, Y13, Y1                // Y1 = min(data, 57)
	VPMAXUB Y1, Y12, Y1                // Y1 = max(min(data, 57), 48)
	VPCMPEQB Y0, Y1, Y4                // Y4 = (data == clamped) for [0-9]

	// Check '_'
	VPCMPEQB Y0, Y14, Y5               // Y5 = (data == '_')

	// Combine: Y2 | Y3 | Y4 | Y5
	VPOR    Y2, Y3, Y6
	VPOR    Y4, Y5, Y7
	VPOR    Y6, Y7, Y6                 // Y6 = final result mask

	// Extract mask
	VPMOVMSKB Y6, CX

	// Check if any match
	TESTL   CX, CX
	JNZ     word_found_in_vector

	// Advance to next chunk
	ADDQ    $32, SI
	JMP     word_loop32

word_handle_tail:
	// Process remaining bytes with scalar loop
	CMPQ    SI, R8
	JAE     word_not_found

word_tail_loop:
	MOVBLZX (SI), AX                   // AX = byte

	// Check [A-Z]: 65 <= b <= 90
	CMPB    AL, $65
	JB      word_check_lower
	CMPB    AL, $90
	JBE     word_found_scalar

word_check_lower:
	// Check [a-z]: 97 <= b <= 122
	CMPB    AL, $97
	JB      word_check_digit
	CMPB    AL, $122
	JBE     word_found_scalar

word_check_digit:
	// Check [0-9]: 48 <= b <= 57
	CMPB    AL, $48
	JB      word_check_underscore
	CMPB    AL, $57
	JBE     word_found_scalar

word_check_underscore:
	// Check '_': 95
	CMPB    AL, $95
	JE      word_found_scalar

	// Not a word char, continue
	INCQ    SI
	CMPQ    SI, R8
	JB      word_tail_loop

word_not_found:
	MOVQ    $-1, AX
	MOVQ    AX, ret+24(FP)
	VZEROUPPER
	RET

word_found_in_vector:
	// Find first set bit
	BSFL    CX, BX
	SUBQ    DI, SI                     // SI = offset to chunk start
	ADDQ    SI, BX                     // BX = absolute position
	MOVQ    BX, ret+24(FP)
	VZEROUPPER
	RET

word_found_scalar:
	SUBQ    DI, SI
	MOVQ    SI, ret+24(FP)
	VZEROUPPER
	RET


// func memchrNotWordAVX2(haystack []byte) int
//
// AVX2 implementation to find first non-word character.
// This is the complement of memchrWordAVX2.
//
// Non-word = NOT([A-Z] | [a-z] | [0-9] | '_')
//
TEXT ·memchrNotWordAVX2(SB), NOSPLIT, $0-32
	// Load parameters
	MOVQ    haystack_base+0(FP), SI
	MOVQ    haystack_len+8(FP), DX

	// Empty check
	TESTQ   DX, DX
	JZ      notword_not_found

	// Save start for offset calculation
	MOVQ    SI, DI

	// Calculate end pointer
	LEAQ    (SI)(DX*1), R8

	// Broadcast range constants
	// [A-Z]: 65-90
	MOVQ    $65, AX
	MOVD    AX, X8
	VPBROADCASTB X8, Y8

	MOVQ    $90, AX
	MOVD    AX, X9
	VPBROADCASTB X9, Y9

	// [a-z]: 97-122
	MOVQ    $97, AX
	MOVD    AX, X10
	VPBROADCASTB X10, Y10

	MOVQ    $122, AX
	MOVD    AX, X11
	VPBROADCASTB X11, Y11

	// [0-9]: 48-57
	MOVQ    $48, AX
	MOVD    AX, X12
	VPBROADCASTB X12, Y12

	MOVQ    $57, AX
	MOVD    AX, X13
	VPBROADCASTB X13, Y13

	// '_': 95
	MOVQ    $95, AX
	MOVD    AX, X14
	VPBROADCASTB X14, Y14

	// All ones for NOT operation
	VPCMPEQB Y15, Y15, Y15             // Y15 = [0xFF, 0xFF, ...]

notword_loop32:
	// Check if we have at least 32 bytes remaining
	LEAQ    32(SI), R9
	CMPQ    R9, R8
	JA      notword_handle_tail

	// Load 32 bytes
	VMOVDQU (SI), Y0

	// Check [A-Z]
	VPMINUB Y0, Y9, Y1
	VPMAXUB Y1, Y8, Y1
	VPCMPEQB Y0, Y1, Y2

	// Check [a-z]
	VPMINUB Y0, Y11, Y1
	VPMAXUB Y1, Y10, Y1
	VPCMPEQB Y0, Y1, Y3

	// Check [0-9]
	VPMINUB Y0, Y13, Y1
	VPMAXUB Y1, Y12, Y1
	VPCMPEQB Y0, Y1, Y4

	// Check '_'
	VPCMPEQB Y0, Y14, Y5

	// Combine: isWord = Y2 | Y3 | Y4 | Y5
	VPOR    Y2, Y3, Y6
	VPOR    Y4, Y5, Y7
	VPOR    Y6, Y7, Y6

	// NOT: isNotWord = ~isWord
	VPXOR   Y6, Y15, Y6                // Y6 = NOT(isWord)

	// Extract mask
	VPMOVMSKB Y6, CX

	// Check if any match
	TESTL   CX, CX
	JNZ     notword_found_in_vector

	// Advance to next chunk
	ADDQ    $32, SI
	JMP     notword_loop32

notword_handle_tail:
	// Process remaining bytes with scalar loop
	CMPQ    SI, R8
	JAE     notword_not_found

notword_tail_loop:
	MOVBLZX (SI), AX

	// Check if NOT a word char (inverse of word_tail_loop logic)
	// Word chars: [A-Z], [a-z], [0-9], '_'
	// If it's a word char, skip; if not, found

	// Check [A-Z]: 65 <= b <= 90
	CMPB    AL, $65
	JB      notword_check_lower_gap
	CMPB    AL, $90
	JBE     notword_is_word           // It's [A-Z], skip

notword_check_lower_gap:
	// Check [a-z]: 97 <= b <= 122
	CMPB    AL, $97
	JB      notword_check_digit_gap
	CMPB    AL, $122
	JBE     notword_is_word           // It's [a-z], skip

notword_check_digit_gap:
	// Check [0-9]: 48 <= b <= 57
	CMPB    AL, $48
	JB      notword_check_underscore_gap
	CMPB    AL, $57
	JBE     notword_is_word           // It's [0-9], skip

notword_check_underscore_gap:
	// Check '_': 95
	CMPB    AL, $95
	JE      notword_is_word           // It's '_', skip

	// Not a word char - found!
	JMP     notword_found_scalar

notword_is_word:
	// It's a word char, continue searching
	INCQ    SI
	CMPQ    SI, R8
	JB      notword_tail_loop

notword_not_found:
	MOVQ    $-1, AX
	MOVQ    AX, ret+24(FP)
	VZEROUPPER
	RET

notword_found_in_vector:
	BSFL    CX, BX
	SUBQ    DI, SI
	ADDQ    SI, BX
	MOVQ    BX, ret+24(FP)
	VZEROUPPER
	RET

notword_found_scalar:
	SUBQ    DI, SI
	MOVQ    SI, ret+24(FP)
	VZEROUPPER
	RET
