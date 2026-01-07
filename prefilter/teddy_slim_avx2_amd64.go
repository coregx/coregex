//go:build amd64

package prefilter

// teddySlimAVX2_1 is the AVX2 assembly implementation for 1-byte fingerprint.
//
// This is implemented in teddy_slim_avx2_amd64.s and processes 32 bytes per iteration,
// providing 2x throughput compared to SSSE3 teddySlimSSSE3_1 (16 bytes/iteration).
//
// Algorithm:
//  1. Load 32-byte nibble masks (duplicated across lanes for Slim Teddy)
//  2. Main loop: process 32 bytes per iteration
//     - Load 32 bytes from haystack (VMOVDQU)
//     - Extract low/high nibbles with VPAND
//     - VPSHUFB: lookup bucket bits in masks (operates per 128-bit lane)
//     - VPAND: combine lo/hi results
//     - VPMOVMSKB: extract 32-bit candidate mask
//  3. Find first set bit position, extract bucket mask
//  4. Handle tail with VBROADCASTI128 or scalar fallback
//
// Performance: ~2x faster than SSSE3 on AVX2-capable CPUs for large haystacks.
//
// Parameters:
//
//	masks - pointer to teddyMasks struct containing nibble lookup tables
//	haystack - the byte slice to search
//
// Returns:
//
//	pos - position of first candidate (relative to haystack start), or -1
//	bucketMask - bitmask of matching buckets (bits 0-7), or 0 if not found
//
//go:noescape
//nolint:unused // Available for direct use; not enabled in integrated prefilter (see #74)
func teddySlimAVX2_1(masks *teddyMasks, haystack []byte) (pos int, bucketMask uint8)

// teddySlimAVX2_2 is the AVX2 assembly implementation for 2-byte fingerprint.
//
// This is implemented in teddy_slim_avx2_amd64.s and processes 32 bytes per iteration,
// providing 2x throughput compared to SSSE3 teddySlimSSSE3_2 (16 bytes/iteration).
//
// Using 2-byte fingerprint reduces false positives by ~90% compared to 1-byte fingerprint
// because it checks two consecutive bytes instead of one.
//
// Algorithm:
//  1. Load 32-byte nibble masks for positions 0 and 1
//  2. Main loop: process 32 bytes per iteration
//     - Load haystack[i:i+32] for position 0 (VMOVDQU)
//     - Load haystack[i+1:i+33] for position 1 (overlapping load)
//     - For each position: extract nibbles, VPSHUFB lookup, VPAND lo/hi
//     - VPAND results from both positions
//     - VPMOVMSKB: extract 32-bit candidate mask
//  3. Find first set bit, extract bucket mask
//  4. Handle tail with VBROADCASTI128 or scalar fallback
//
// Performance: ~2x faster than SSSE3 on AVX2-capable CPUs.
// Expected to close the 5.9x gap vs Rust regex on literal_alt pattern.
//
// Returns:
//
//	pos - position of first candidate (relative to haystack start), or -1
//	bucketMask - bitmask of matching buckets (bits 0-7), or 0 if not found
//
//go:noescape
//nolint:unused // Available for direct use; not enabled in integrated prefilter (see #74)
func teddySlimAVX2_2(masks *teddyMasks, haystack []byte) (pos int, bucketMask uint8)
