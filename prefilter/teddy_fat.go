// Package prefilter provides the Fat Teddy multi-pattern SIMD matching algorithm.
//
// Fat Teddy is an AVX2-based extension of Teddy that uses 16 buckets (vs 8 in Slim Teddy).
// It processes 256-bit vectors and can efficiently handle 33-64 patterns.
//
// Architecture:
//   - Slim Teddy: 8 buckets, SSSE3 (128-bit), 2-32 patterns
//   - Fat Teddy: 16 buckets, AVX2 (256-bit), 33-64 patterns
//   - Fallback: Aho-Corasick for >64 patterns or no AVX2
//
// Mask Layout (256-bit / 32 bytes per mask):
//   - Low 128-bit lane (bytes 0-15): buckets 0-7
//   - High 128-bit lane (bytes 16-31): buckets 8-15
//
// Reference:
//   - Rust aho-corasick: src/packed/teddy/generic.rs
//   - docs/dev/kanban/done/V010-FAT-001-research.md
package prefilter

import (
	"bytes"
	"math/bits"
)

// FatTeddyConfig configures Fat Teddy construction.
type FatTeddyConfig struct {
	// MinPatterns is the minimum patterns required (default: 2)
	MinPatterns int

	// MaxPatterns is the maximum patterns allowed (default: 64)
	MaxPatterns int

	// MinPatternLen is the minimum pattern length required (default: 3)
	MinPatternLen int

	// FingerprintLen is the number of fingerprint bytes to use (1-4, default: 2)
	FingerprintLen int
}

// DefaultFatTeddyConfig returns the default Fat Teddy configuration.
func DefaultFatTeddyConfig() *FatTeddyConfig {
	return &FatTeddyConfig{
		MinPatterns:    MinTeddyPatterns,
		MaxPatterns:    MaxFatTeddyPatterns,
		MinPatternLen:  MinTeddyPatternLen,
		FingerprintLen: 2, // 2-byte fingerprint reduces false positives by ~90%
	}
}

// FatTeddy is an AVX2-accelerated multi-pattern searcher using 16 buckets.
//
// It uses the same algorithm as Slim Teddy but with 256-bit vectors,
// enabling efficient search for 33-64 patterns.
//
// Thread-safety: FatTeddy is safe for concurrent use (all state is immutable).
type FatTeddy struct {
	// patterns stores the original pattern bytes
	patterns [][]byte

	// masks stores the nibble lookup tables for AVX2 search
	masks *fatTeddyMasks

	// buckets maps bucket ID (0-15) to list of pattern IDs
	buckets [][]int

	// minLen is the minimum pattern length (used for verification bounds)
	minLen int

	// complete indicates if FatTeddy match is sufficient (no verification needed)
	complete bool

	// uniformLen is the pattern length when all patterns have the same length
	uniformLen int
}

// fatTeddyMasks stores the nibble lookup masks for AVX2 search.
//
// Layout for 256-bit vectors:
//   - Bytes 0-15: bucket bits for buckets 0-7
//   - Bytes 16-31: bucket bits for buckets 8-15
//
// During search:
//  1. VBROADCASTI128 loads 16 bytes and duplicates to both lanes
//  2. VPSHUFB on low lane → candidates for buckets 0-7
//  3. VPSHUFB on high lane → candidates for buckets 8-15
//  4. VPERM2I128 + VPUNPCKLBW interleaves results
//  5. VPMOVMSKB extracts 16-bit bucket mask
type fatTeddyMasks struct {
	// fingerprintLen is the number of fingerprint bytes (1-4)
	fingerprintLen uint32

	// Padding for alignment
	_ uint32

	// loMasks[pos] is the low-nibble lookup table for fingerprint position 'pos'.
	// Layout: bytes 0-15 = buckets 0-7, bytes 16-31 = buckets 8-15
	loMasks [MaxFingerprintLen][32]byte

	// hiMasks[pos] is the high-nibble lookup table for fingerprint position 'pos'.
	// Layout same as loMasks.
	hiMasks [MaxFingerprintLen][32]byte
}

// NewFatTeddy creates a new Fat Teddy searcher for the given patterns.
//
// Returns nil if patterns are not suitable for Fat Teddy:
//   - Fewer than MinPatterns (default: 2)
//   - More than MaxPatterns (default: 64)
//   - Any pattern shorter than MinPatternLen (default: 3)
//
// Note: For 2-32 patterns, prefer Slim Teddy (SSSE3) as it has lower overhead.
// Fat Teddy is optimal for 33-64 patterns on AVX2-capable CPUs.
//
// Example:
//
//	patterns := make([][]byte, 50)
//	for i := range patterns {
//	    patterns[i] = []byte(fmt.Sprintf("pattern%02d", i))
//	}
//	fatTeddy := prefilter.NewFatTeddy(patterns, nil)
//	if fatTeddy != nil {
//	    pos := fatTeddy.Find(haystack, 0)
//	}
//
//nolint:dupl // Intentional duplication - FatTeddy (16 buckets) vs Teddy (8 buckets) have similar constructors
func NewFatTeddy(patterns [][]byte, config *FatTeddyConfig) *FatTeddy {
	if config == nil {
		config = DefaultFatTeddyConfig()
	}

	// Validate pattern count
	if len(patterns) < config.MinPatterns || len(patterns) > config.MaxPatterns {
		return nil
	}

	// Validate pattern lengths and find minimum
	minLen := len(patterns[0])
	for _, p := range patterns {
		if len(p) < config.MinPatternLen {
			return nil // Pattern too short
		}
		if len(p) < minLen {
			minLen = len(p)
		}
	}

	// Determine fingerprint length (min of config and shortest pattern)
	fingerprintLen := config.FingerprintLen
	if fingerprintLen > minLen {
		fingerprintLen = minLen
	}
	if fingerprintLen > MaxFingerprintLen {
		fingerprintLen = MaxFingerprintLen
	}

	// Copy patterns to prevent aliasing
	patternsCopy := make([][]byte, len(patterns))
	for i, p := range patterns {
		patternsCopy[i] = make([]byte, len(p))
		copy(patternsCopy[i], p)
	}

	// Build masks and buckets for 16 buckets
	masks, buckets := buildFatMasks(patternsCopy, fingerprintLen)

	// FatTeddy.Find() always verifies full pattern matches
	complete := true

	// uniformLen is the pattern length when all patterns have the same length
	uniformLen := len(patternsCopy[0])
	for _, p := range patternsCopy[1:] {
		if len(p) != uniformLen {
			uniformLen = 0
			break
		}
	}

	return &FatTeddy{
		patterns:   patternsCopy,
		masks:      masks,
		buckets:    buckets,
		minLen:     minLen,
		complete:   complete,
		uniformLen: uniformLen,
	}
}

// buildFatMasks constructs the nibble lookup masks for AVX2 search (16 buckets).
//
// Algorithm:
//  1. Assign each pattern to a bucket (modulo 16 distribution)
//  2. For each fingerprint position:
//     - For each pattern: extract byte at that position
//     - Split byte into low nibble (b & 0x0F) and high nibble (b >> 4)
//     - For buckets 0-7: set bit in bytes 0-15
//     - For buckets 8-15: set bit in bytes 16-31
//
// Mask layout matches Rust aho-corasick generic.rs FatMaskBuilder.
func buildFatMasks(patterns [][]byte, fingerprintLen int) (*fatTeddyMasks, [][]int) {
	masks := &fatTeddyMasks{
		fingerprintLen: uint32(fingerprintLen), // #nosec G115 -- fingerprintLen is bounded by MaxFingerprintLen(4)
	}

	// Initialize 16 buckets
	buckets := make([][]int, NumBucketsFat)

	// Assign patterns to buckets and build masks
	for patternID, pattern := range patterns {
		// Bucket assignment: modulo 16 distribution
		// This matches Rust's simple bucket assignment
		bucketID := patternID % NumBucketsFat
		buckets[bucketID] = append(buckets[bucketID], patternID)

		// Build masks for each fingerprint position
		for pos := 0; pos < fingerprintLen; pos++ {
			b := pattern[pos]
			loNibble := b & 0x0F        // Low 4 bits
			hiNibble := (b >> 4) & 0x0F // High 4 bits

			if bucketID < 8 {
				// Buckets 0-7: low 128-bit lane (bytes 0-15)
				bucketBit := byte(1 << bucketID)
				masks.loMasks[pos][loNibble] |= bucketBit
				masks.hiMasks[pos][hiNibble] |= bucketBit
			} else {
				// Buckets 8-15: high 128-bit lane (bytes 16-31)
				bucketBit := byte(1 << (bucketID - 8))
				masks.loMasks[pos][16+loNibble] |= bucketBit
				masks.hiMasks[pos][16+hiNibble] |= bucketBit
			}
		}
	}

	return masks, buckets
}

// Find returns the index of the first match starting at or after 'start'.
//
// This implements the Prefilter interface. It uses AVX2 SIMD search to find candidates,
// then verifies full pattern matches.
//
// Returns -1 if no match is found.
func (t *FatTeddy) Find(haystack []byte, start int) int {
	// Bounds check
	if start < 0 || start >= len(haystack) {
		return -1
	}

	// Slice haystack from start position
	haystack = haystack[start:]

	// If haystack is too short for SIMD (< 16 bytes), use scalar search
	if len(haystack) < 16 {
		return t.findScalar(haystack, start)
	}

	// Use SIMD search
	pos, bucketMask := t.findSIMD(haystack)

	// Track accumulated offset for continuation searches
	accumulatedOffset := 0

	// Process candidates
	for pos != -1 {
		// Iterate through all set bits in bucket mask (16-bit for Fat Teddy)
		for bucketMask != 0 {
			// Find lowest set bit (bucket ID)
			bucket := bits.TrailingZeros16(bucketMask)
			bucketMask &^= 1 << bucket // Clear the bit

			// Verify patterns in this specific bucket
			matchPos, _ := t.verifyBucket(haystack[accumulatedOffset:], pos, bucket)
			if matchPos != -1 {
				// Match found! Return absolute position
				return start + accumulatedOffset + matchPos
			}
		}

		// No match at this candidate in any bucket, continue searching
		nextSearchStart := accumulatedOffset + pos + 1
		if nextSearchStart >= len(haystack) {
			break
		}

		// Update accumulated offset
		accumulatedOffset = nextSearchStart

		// Search in remaining haystack
		pos, bucketMask = t.findSIMD(haystack[accumulatedOffset:])
	}

	return -1 // No match found
}

// FindMatch returns the start and end positions of the first match.
//
//nolint:dupl // Intentional duplication - FatTeddy uses uint16 bucketMask, Teddy uses uint8
func (t *FatTeddy) FindMatch(haystack []byte, start int) (int, int) {
	// Bounds check
	if start < 0 || start >= len(haystack) {
		return -1, -1
	}

	// Slice haystack from start position
	haystack = haystack[start:]

	// If haystack is too short for SIMD (< 16 bytes), use scalar search
	if len(haystack) < 16 {
		return t.findMatchScalar(haystack, start)
	}

	// Use SIMD search
	pos, bucketMask := t.findSIMD(haystack)

	// Track accumulated offset for continuation searches
	accumulatedOffset := 0

	// Process candidates
	for pos != -1 {
		// Iterate through all set bits in bucket mask
		for bucketMask != 0 {
			bucket := bits.TrailingZeros16(bucketMask)
			bucketMask &^= 1 << bucket

			matchPos, patternID := t.verifyBucket(haystack[accumulatedOffset:], pos, bucket)
			if matchPos != -1 && patternID >= 0 && patternID < len(t.patterns) {
				matchStart := start + accumulatedOffset + matchPos
				matchEnd := matchStart + len(t.patterns[patternID])
				return matchStart, matchEnd
			}
		}

		nextSearchStart := accumulatedOffset + pos + 1
		if nextSearchStart >= len(haystack) {
			break
		}

		accumulatedOffset = nextSearchStart
		pos, bucketMask = t.findSIMD(haystack[accumulatedOffset:])
	}

	return -1, -1
}

// findScalar performs scalar search for haystacks < 16 bytes.
func (t *FatTeddy) findScalar(haystack []byte, start int) int {
	for i := 0; i < len(haystack)-t.minLen+1; i++ {
		for _, pattern := range t.patterns {
			if i+len(pattern) <= len(haystack) {
				if bytes.Equal(haystack[i:i+len(pattern)], pattern) {
					return start + i
				}
			}
		}
	}
	return -1
}

// findMatchScalar is the scalar fallback for FindMatch.
func (t *FatTeddy) findMatchScalar(haystack []byte, start int) (int, int) {
	for i := 0; i < len(haystack)-t.minLen+1; i++ {
		for _, pattern := range t.patterns {
			if i+len(pattern) <= len(haystack) {
				if bytes.Equal(haystack[i:i+len(pattern)], pattern) {
					return start + i, start + i + len(pattern)
				}
			}
		}
	}
	return -1, -1
}

// findScalarCandidate is a pure Go implementation for finding candidates.
// Returns (position, bucketMask) or (-1, 0) if no candidate found.
// bucketMask is 16-bit for Fat Teddy (16 buckets).
func (t *FatTeddy) findScalarCandidate(haystack []byte) (pos int, bucketMask uint16) {
	fpLen := int(t.masks.fingerprintLen)

	for i := 0; i+fpLen <= len(haystack); i++ {
		// Start with all 16 buckets possible
		candidateMaskLo := byte(0xFF) // Buckets 0-7
		candidateMaskHi := byte(0xFF) // Buckets 8-15

		for pos := 0; pos < fpLen; pos++ {
			b := haystack[i+pos]
			loNibble := b & 0x0F
			hiNibble := (b >> 4) & 0x0F

			// Low 128-bit lane: buckets 0-7
			loMaskLo := t.masks.loMasks[pos][loNibble]
			hiMaskLo := t.masks.hiMasks[pos][hiNibble]
			candidateMaskLo &= loMaskLo & hiMaskLo

			// High 128-bit lane: buckets 8-15
			loMaskHi := t.masks.loMasks[pos][16+loNibble]
			hiMaskHi := t.masks.hiMasks[pos][16+hiNibble]
			candidateMaskHi &= loMaskHi & hiMaskHi
		}

		// Combine into 16-bit mask
		combined := uint16(candidateMaskLo) | (uint16(candidateMaskHi) << 8)
		if combined != 0 {
			return i, combined
		}
	}

	return -1, 0
}

// verifyBucket checks if any pattern in the specified bucket matches.
func (t *FatTeddy) verifyBucket(haystack []byte, pos int, bucket int) (int, int) {
	if pos < 0 || pos >= len(haystack) {
		return -1, -1
	}

	if bucket >= 0 && bucket < len(t.buckets) {
		for _, patternID := range t.buckets[bucket] {
			pattern := t.patterns[patternID]
			end := pos + len(pattern)
			if end <= len(haystack) && bytes.Equal(haystack[pos:end], pattern) {
				return pos, patternID
			}
		}
	}

	return -1, -1
}

// IsComplete implements Prefilter.IsComplete.
func (t *FatTeddy) IsComplete() bool {
	return t.complete
}

// LiteralLen implements Prefilter.LiteralLen.
func (t *FatTeddy) LiteralLen() int {
	if t.complete && t.uniformLen > 0 {
		return t.uniformLen
	}
	return 0
}

// HeapBytes implements Prefilter.HeapBytes.
func (t *FatTeddy) HeapBytes() int {
	heapBytes := 264 // sizeof(fatTeddyMasks)

	for _, p := range t.patterns {
		heapBytes += len(p)
	}

	heapBytes += len(t.buckets) * 24
	for _, bucket := range t.buckets {
		heapBytes += len(bucket) * 8
	}

	return heapBytes
}

// MinimumLen returns the minimum haystack length for efficient SIMD search.
//
// For haystacks smaller than this, the SIMD setup overhead exceeds the benefit.
// Callers should use a fallback strategy (like Aho-Corasick) for small inputs.
//
// This follows Rust regex's minimum_len() approach:
//   - AVX2 processes 32 bytes per iteration
//   - With 2-byte fingerprint, minimum is 32 + 1 = 33 bytes
//   - We use 64 as conservative threshold based on benchmarks showing
//     Aho-Corasick is ~2x faster than Fat Teddy on ~37 byte inputs
//
// Reference: rust-aho-corasick/src/packed/teddy/builder.rs:585
func (t *FatTeddy) MinimumLen() int {
	// Conservative threshold: Aho-Corasick beats Fat Teddy below this size.
	// Benchmarks: 37-byte haystack with 50 patterns:
	//   Fat Teddy: ~267 ns, Aho-Corasick: ~130 ns
	return 64
}

// PatternCount returns the number of patterns in this Fat Teddy searcher.
// Used by meta-engine to build appropriate fallback strategy.
func (t *FatTeddy) PatternCount() int {
	return len(t.patterns)
}

// Patterns returns the patterns stored in this Fat Teddy searcher.
// Used by meta-engine to build Aho-Corasick fallback for small haystacks.
func (t *FatTeddy) Patterns() [][]byte {
	return t.patterns
}
