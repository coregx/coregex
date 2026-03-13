//go:build goexperiment.simd && amd64

package prefilter

import (
	"math/bits"
	"simd/archsimd"
	"unsafe"
)

// findSIMD performs SIMD search for candidate positions using Go 1.26+ archsimd
// compiler intrinsics, eliminating the Go/assembly boundary crossing overhead.
//
// With hand-written assembly, each findSIMD() call crosses the Go→ASM→Go
// boundary (~50-65 cycles overhead). Over a 6MB scan with ~375K calls, this
// adds significant overhead. archsimd intrinsics compile to native SIMD
// instructions inline, keeping the entire search loop in Go with zero
// boundary crossings.
//
// The algorithm is identical to the SSSE3 assembly (teddySlimSSSE3_1/2):
//
//  1. Load 16-byte nibble masks (lo/hi) for each fingerprint position
//  2. Main loop: process 16 bytes per iteration
//     - Load 16 bytes from haystack
//     - Extract low nibbles (AND 0x0F) and high nibbles (SHR 4, AND 0x0F)
//     - PSHUFB lookup in lo/hi mask tables
//     - AND lo/hi results to get candidate mask
//     - PMOVMSKB to extract bitmask
//  3. For 2-byte fingerprint: AND results from positions 0 and 1
//  4. Find first set bit, extract bucket mask for that candidate byte
//
// Returns (position, bucketMask) or (-1, 0) if no candidate found.
// bucketMask contains bits for ALL matching buckets (not just first).
func (t *Teddy) findSIMD(haystack []byte) (pos int, bucketMask uint8) {
	fpLen := int(t.masks.fingerprintLen)

	// AVX2 path: 32 bytes/iteration (2x throughput over SSSE3).
	// With archsimd intrinsics the compiler manages AVX/SSE transitions
	// automatically — no VZEROUPPER overhead that made the ASM AVX2 path
	// 4x slower on AMD EPYC.
	if hasAVX2 {
		switch fpLen {
		case 1:
			return t.findSIMDArchsimdAVX2_1(haystack)
		case 2:
			return t.findSIMDArchsimdAVX2_2(haystack)
		}
	}

	// SSSE3 fallback: 16 bytes/iteration.
	switch fpLen {
	case 1:
		return t.findSIMDArchsimd1(haystack)
	case 2:
		return t.findSIMDArchsimd2(haystack)
	default:
		return t.findScalarCandidate(haystack)
	}
}

// findSIMDArchsimd1 performs Teddy search with 1-byte fingerprint using archsimd.
//
// Algorithm per 16-byte chunk:
//
//	chunk = load(haystack[at:at+16])
//	loNibbles = chunk AND 0x0F
//	hiNibbles = (chunk >> 4) AND 0x0F        // unsigned shift via Uint16x8
//	loResult  = PSHUFB(loTable, loNibbles)    // PermuteOrZero
//	hiResult  = PSHUFB(hiTable, hiNibbles)    // PermuteOrZero
//	combined  = loResult AND hiResult
//	bits      = PMOVMSKB(combined != zero)    // NotEqual + ToBits
//
// Returns (candidate_position, bucket_mask) or (-1, 0).
func (t *Teddy) findSIMDArchsimd1(haystack []byte) (int, uint8) {
	if len(haystack) < 1 {
		return -1, 0
	}

	// Broadcast the nibble extraction mask: 0x0F = 15 as int8
	nibbleMask := archsimd.BroadcastInt8x16(0x0F)
	zero := archsimd.Int8x16{}

	// Load the fingerprint position 0 lookup tables.
	// loMasks[0] and hiMasks[0] are [32]byte arrays; we only need the first 16 bytes
	// for SSSE3-width (128-bit) operation.
	loTable := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&t.masks.loMasks[0][0])))
	hiTable := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&t.masks.hiMasks[0][0])))

	at := 0
	for at+16 <= len(haystack) {
		// Load 16 bytes from haystack
		chunk := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&haystack[at])))

		// Extract low nibbles: chunk & 0x0F
		loNibbles := chunk.And(nibbleMask)

		// Extract high nibbles: (chunk >> 4) & 0x0F
		// Use Uint16x8.ShiftAllRight (VPSRLW = unsigned logical shift) to avoid
		// sign extension. Int16x8.ShiftAllRight would use VPSRAW (arithmetic shift),
		// which propagates the sign bit and corrupts high nibble extraction for bytes
		// with bit 7 set (0x80-0xFF).
		hiNibbles := chunk.AsUint16x8().ShiftAllRight(4).AsInt8x16().And(nibbleMask)

		// PSHUFB lookups: each byte in the result contains bucket membership bits
		// for the pattern(s) whose nibble matches the corresponding haystack byte's nibble.
		// PermuteOrZero zeroes output bytes where the index has its high bit set,
		// which is exactly what PSHUFB does.
		loResult := loTable.PermuteOrZero(loNibbles)
		hiResult := hiTable.PermuteOrZero(hiNibbles)

		// AND: a byte is a candidate only if BOTH its low and high nibbles
		// match some pattern's fingerprint byte nibbles.
		combined := loResult.And(hiResult)

		// Extract a bitmask of non-zero bytes (candidate positions within this chunk).
		// NotEqual returns a mask where each lane is set if combined[i] != 0.
		// ToBits converts the mask to a uint16 bitmask.
		candidateBits := combined.NotEqual(zero).ToBits()

		if candidateBits != 0 {
			// Find the first candidate position within this 16-byte chunk.
			bitPos := bits.TrailingZeros16(candidateBits)

			// Re-extract the accurate bucket mask for this specific candidate byte.
			// The SIMD result byte at bitPos contains the correct mask, but it's
			// faster and simpler to re-lookup from the scalar tables (same as ASM).
			b := haystack[at+bitPos]
			bucketBits := t.masks.loMasks[0][b&0x0F] & t.masks.hiMasks[0][(b>>4)&0x0F]

			return at + bitPos, bucketBits
		}

		at += 16
	}

	// Scalar tail for remaining < 16 bytes.
	// Cannot use SIMD load without risking out-of-bounds read.
	for ; at < len(haystack); at++ {
		b := haystack[at]
		bucketBits := t.masks.loMasks[0][b&0x0F] & t.masks.hiMasks[0][(b>>4)&0x0F]
		if bucketBits != 0 {
			return at, bucketBits
		}
	}

	return -1, 0
}

// findSIMDArchsimd2 performs Teddy search with 2-byte fingerprint using archsimd.
//
// The 2-byte fingerprint processes two overlapping 16-byte chunks per iteration:
//
//	chunk0 = haystack[at:at+16]      (fingerprint byte position 0)
//	chunk1 = haystack[at+1:at+17]    (fingerprint byte position 1)
//
// Each chunk is processed through its own pair of mask tables, and the results
// are ANDed together. A candidate must match BOTH fingerprint positions, reducing
// false positives by ~90% compared to 1-byte fingerprint.
//
// Returns (candidate_position, bucket_mask) or (-1, 0).
func (t *Teddy) findSIMDArchsimd2(haystack []byte) (int, uint8) {
	// Need at least 2 bytes for the 2-byte fingerprint
	if len(haystack) < 2 {
		return -1, 0
	}

	nibbleMask := archsimd.BroadcastInt8x16(0x0F)
	zero := archsimd.Int8x16{}

	// Load mask tables for both fingerprint positions.
	// Position 0: checks first byte of each pattern's fingerprint.
	// Position 1: checks second byte of each pattern's fingerprint.
	loTable0 := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&t.masks.loMasks[0][0])))
	hiTable0 := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&t.masks.hiMasks[0][0])))
	loTable1 := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&t.masks.loMasks[1][0])))
	hiTable1 := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&t.masks.hiMasks[1][0])))

	at := 0
	// The overlapping load for position 1 reads haystack[at+1:at+17],
	// so we need at+17 <= len(haystack), i.e., at+16 < len(haystack).
	for at+16 < len(haystack) {
		// Position 0: process haystack[at:at+16]
		chunk0 := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&haystack[at])))
		loNibbles0 := chunk0.And(nibbleMask)
		hiNibbles0 := chunk0.AsUint16x8().ShiftAllRight(4).AsInt8x16().And(nibbleMask)
		loResult0 := loTable0.PermuteOrZero(loNibbles0)
		hiResult0 := hiTable0.PermuteOrZero(hiNibbles0)
		result0 := loResult0.And(hiResult0)

		// Position 1: process haystack[at+1:at+17] (overlapping by 15 bytes)
		chunk1 := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&haystack[at+1])))
		loNibbles1 := chunk1.And(nibbleMask)
		hiNibbles1 := chunk1.AsUint16x8().ShiftAllRight(4).AsInt8x16().And(nibbleMask)
		loResult1 := loTable1.PermuteOrZero(loNibbles1)
		hiResult1 := hiTable1.PermuteOrZero(hiNibbles1)
		result1 := loResult1.And(hiResult1)

		// AND results from both positions: candidate must match at both
		// fingerprint byte positions.
		combined := result0.And(result1)

		candidateBits := combined.NotEqual(zero).ToBits()

		if candidateBits != 0 {
			bitPos := bits.TrailingZeros16(candidateBits)

			// Re-extract bucket mask from scalar tables for accuracy.
			// Check both fingerprint positions for the candidate.
			b0 := haystack[at+bitPos]
			b1 := haystack[at+bitPos+1]
			bucketBits := t.masks.loMasks[0][b0&0x0F] & t.masks.hiMasks[0][(b0>>4)&0x0F] &
				t.masks.loMasks[1][b1&0x0F] & t.masks.hiMasks[1][(b1>>4)&0x0F]

			return at + bitPos, bucketBits
		}

		at += 16
	}

	// Scalar tail: need at least 2 consecutive bytes for 2-byte fingerprint.
	for ; at+1 < len(haystack); at++ {
		b0 := haystack[at]
		b1 := haystack[at+1]
		bucketBits := t.masks.loMasks[0][b0&0x0F] & t.masks.hiMasks[0][(b0>>4)&0x0F] &
			t.masks.loMasks[1][b1&0x0F] & t.masks.hiMasks[1][(b1>>4)&0x0F]
		if bucketBits != 0 {
			return at, bucketBits
		}
	}

	return -1, 0
}

// findSIMDArchsimdAVX2_1 performs Teddy search with 1-byte fingerprint using
// 256-bit AVX2 intrinsics via archsimd, processing 32 bytes per iteration.
//
// This is 2x throughput over the SSSE3 path (findSIMDArchsimd1) because it
// processes 32 bytes per iteration instead of 16. With archsimd intrinsics,
// the compiler handles AVX/SSE transitions automatically — no VZEROUPPER
// overhead that made the hand-written AVX2 assembly 4x slower on AMD EPYC.
//
// The mask tables are already duplicated across both 16-byte halves by
// buildMasks(), so we load the full 32-byte array directly. VPSHUFB
// (PermuteOrZeroGrouped) operates per 128-bit lane, which is exactly
// correct for Teddy: each lane processes its 16 bytes independently
// using the same nibble lookup table.
//
// Algorithm per 32-byte chunk:
//
//	chunk    = load256(haystack[at:at+32])
//	loNibs   = chunk AND 0x0F
//	hiNibs   = (chunk >> 4) AND 0x0F             // unsigned shift
//	loResult = VPSHUFB(loTable256, loNibs)        // PermuteOrZeroGrouped
//	hiResult = VPSHUFB(hiTable256, hiNibs)        // PermuteOrZeroGrouped
//	combined = loResult AND hiResult
//	bits     = VPMOVMSKB(combined != zero)        // uint32 bitmask
//
// Returns (candidate_position, bucket_mask) or (-1, 0).
func (t *Teddy) findSIMDArchsimdAVX2_1(haystack []byte) (int, uint8) {
	if len(haystack) < 1 {
		return -1, 0
	}

	// 256-bit broadcast: 0x0F into all 32 lanes.
	nibbleMask := archsimd.BroadcastInt8x32(0x0F)
	zero := archsimd.Int8x32{}

	// Load the full 32-byte mask arrays. buildMasks() already duplicates the
	// 16-byte nibble tables into both halves of the [32]byte arrays, so this
	// single load is sufficient for both AVX2 lanes.
	loTable := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&t.masks.loMasks[0][0])))
	hiTable := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&t.masks.hiMasks[0][0])))

	at := 0
	for at+32 <= len(haystack) {
		// Load 32 bytes from haystack.
		chunk := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&haystack[at])))

		// Extract low nibbles: chunk & 0x0F
		loNibbles := chunk.And(nibbleMask)

		// Extract high nibbles: (chunk >> 4) & 0x0F
		// Uint16x16.ShiftAllRight = VPSRLW (unsigned logical shift), avoids
		// sign extension for bytes 0x80-0xFF.
		hiNibbles := chunk.AsUint16x16().ShiftAllRight(4).AsInt8x32().And(nibbleMask)

		// VPSHUFB lookups: PermuteOrZeroGrouped operates per 128-bit lane,
		// which is exactly what Teddy needs — each lane uses the same nibble
		// table independently.
		loResult := loTable.PermuteOrZeroGrouped(loNibbles)
		hiResult := hiTable.PermuteOrZeroGrouped(hiNibbles)

		// AND: candidate only if both nibbles match.
		combined := loResult.And(hiResult)

		// VPMOVMSKB: extract 32-bit bitmask of non-zero bytes.
		candidateBits := combined.NotEqual(zero).ToBits()

		if candidateBits != 0 {
			bitPos := bits.TrailingZeros32(candidateBits)

			// Re-extract bucket mask from scalar tables.
			b := haystack[at+bitPos]
			bucketBits := t.masks.loMasks[0][b&0x0F] & t.masks.hiMasks[0][(b>>4)&0x0F]

			return at + bitPos, bucketBits
		}

		at += 32
	}

	// Tail: fall back to SSSE3 for remaining 16-31 bytes, then scalar for <16.
	if at+16 <= len(haystack) {
		nibbleMask128 := archsimd.BroadcastInt8x16(0x0F)
		zero128 := archsimd.Int8x16{}
		loTable128 := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&t.masks.loMasks[0][0])))
		hiTable128 := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&t.masks.hiMasks[0][0])))

		chunk := archsimd.LoadInt8x16((*[16]int8)(unsafe.Pointer(&haystack[at])))
		loNibbles := chunk.And(nibbleMask128)
		hiNibbles := chunk.AsUint16x8().ShiftAllRight(4).AsInt8x16().And(nibbleMask128)
		loResult := loTable128.PermuteOrZero(loNibbles)
		hiResult := hiTable128.PermuteOrZero(hiNibbles)
		combined := loResult.And(hiResult)
		candidateBits := combined.NotEqual(zero128).ToBits()

		if candidateBits != 0 {
			bitPos := bits.TrailingZeros16(candidateBits)
			b := haystack[at+bitPos]
			bucketBits := t.masks.loMasks[0][b&0x0F] & t.masks.hiMasks[0][(b>>4)&0x0F]
			return at + bitPos, bucketBits
		}

		at += 16
	}

	// Scalar tail for remaining < 16 bytes.
	for ; at < len(haystack); at++ {
		b := haystack[at]
		bucketBits := t.masks.loMasks[0][b&0x0F] & t.masks.hiMasks[0][(b>>4)&0x0F]
		if bucketBits != 0 {
			return at, bucketBits
		}
	}

	return -1, 0
}

// findSIMDArchsimdAVX2_2 performs Teddy search with 2-byte fingerprint using
// 256-bit AVX2 intrinsics, processing 32 bytes per iteration.
//
// The 2-byte fingerprint uses two overlapping 32-byte loads per iteration:
//
//	chunk0 = haystack[at:at+32]      (fingerprint byte position 0)
//	chunk1 = haystack[at+1:at+33]    (fingerprint byte position 1)
//
// Each chunk is processed through its own pair of mask tables. Results are
// ANDed together — a candidate must match BOTH fingerprint positions, reducing
// false positives by ~90% compared to 1-byte fingerprint.
//
// The overlapping load approach avoids the need for VPALIGNR (which is not
// available in archsimd) and is the same strategy used in the SSSE3 path.
//
// Returns (candidate_position, bucket_mask) or (-1, 0).
func (t *Teddy) findSIMDArchsimdAVX2_2(haystack []byte) (int, uint8) {
	// Need at least 2 bytes for the 2-byte fingerprint.
	if len(haystack) < 2 {
		return -1, 0
	}

	nibbleMask := archsimd.BroadcastInt8x32(0x0F)
	zero := archsimd.Int8x32{}

	// Load 32-byte mask tables for both fingerprint positions.
	// buildMasks() duplicates nibble tables across both 16-byte halves.
	loTable0 := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&t.masks.loMasks[0][0])))
	hiTable0 := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&t.masks.hiMasks[0][0])))
	loTable1 := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&t.masks.loMasks[1][0])))
	hiTable1 := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&t.masks.hiMasks[1][0])))

	at := 0
	// The overlapping load for position 1 reads haystack[at+1:at+33],
	// so we need at+33 <= len(haystack).
	for at+33 <= len(haystack) {
		// Position 0: process haystack[at:at+32]
		chunk0 := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&haystack[at])))
		loNibbles0 := chunk0.And(nibbleMask)
		hiNibbles0 := chunk0.AsUint16x16().ShiftAllRight(4).AsInt8x32().And(nibbleMask)
		loResult0 := loTable0.PermuteOrZeroGrouped(loNibbles0)
		hiResult0 := hiTable0.PermuteOrZeroGrouped(hiNibbles0)
		result0 := loResult0.And(hiResult0)

		// Position 1: process haystack[at+1:at+33] (overlapping by 31 bytes)
		chunk1 := archsimd.LoadInt8x32((*[32]int8)(unsafe.Pointer(&haystack[at+1])))
		loNibbles1 := chunk1.And(nibbleMask)
		hiNibbles1 := chunk1.AsUint16x16().ShiftAllRight(4).AsInt8x32().And(nibbleMask)
		loResult1 := loTable1.PermuteOrZeroGrouped(loNibbles1)
		hiResult1 := hiTable1.PermuteOrZeroGrouped(hiNibbles1)
		result1 := loResult1.And(hiResult1)

		// AND results from both positions: candidate must match at both
		// fingerprint byte positions.
		combined := result0.And(result1)

		candidateBits := combined.NotEqual(zero).ToBits()

		if candidateBits != 0 {
			bitPos := bits.TrailingZeros32(candidateBits)

			// Re-extract bucket mask from scalar tables for accuracy.
			b0 := haystack[at+bitPos]
			b1 := haystack[at+bitPos+1]
			bucketBits := t.masks.loMasks[0][b0&0x0F] & t.masks.hiMasks[0][(b0>>4)&0x0F] &
				t.masks.loMasks[1][b1&0x0F] & t.masks.hiMasks[1][(b1>>4)&0x0F]

			return at + bitPos, bucketBits
		}

		at += 32
	}

	// Tail: fall back to SSSE3 for remaining bytes.
	// The SSSE3 2-byte path handles 16 < len correctly and has its own scalar tail.
	if at < len(haystack) {
		pos, mask := t.findSIMDArchsimd2(haystack[at:])
		if pos != -1 {
			return at + pos, mask
		}
	}

	return -1, 0
}
