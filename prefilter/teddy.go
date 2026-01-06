// Package prefilter implements the Teddy multi-pattern SIMD matching algorithm.
//
// Teddy is a SIMD-accelerated algorithm for searching multiple patterns simultaneously.
// It uses vector shuffle instructions (PSHUFB) to perform parallel table lookups,
// identifying candidate match positions that are then verified against actual patterns.
//
// The algorithm is particularly effective for 2-32 patterns with length >= 3 bytes,
// providing 20-50x speedup over naive multi-pattern search.
//
// Algorithm Overview:
//
//  1. Build Phase (once per pattern set):
//     - Assign patterns to buckets (0-7 for Slim Teddy)
//     - Build nibble lookup masks for fingerprint bytes
//     - Each mask byte contains bucket membership bits
//
//  2. Search Phase (per 16-byte chunk with SSSE3):
//     - Load haystack chunk
//     - Extract low/high nibbles
//     - Use PSHUFB to lookup bucket membership
//     - AND results to find candidates
//     - Extract candidate positions
//
//  3. Verify Phase (per candidate):
//     - Check patterns in candidate's bucket
//     - Compare full pattern bytes
//     - Return first match
//
// Reference:
//   - BurntSushi/aho-corasick Teddy README
//   - docs/dev/research/TEDDY_IMPLEMENTATION_GUIDE.md
package prefilter

import (
	"bytes"
	"math/bits"

	"github.com/coregx/coregex/literal"
)

// Constants for Teddy configuration
const (
	// MaxTeddyPatterns is the maximum number of patterns Teddy can handle efficiently.
	// Slim Teddy uses 8 buckets with modulo distribution, so patterns are spread across buckets.
	// With 2-byte fingerprint (default), false positive rate is low enough for 32 patterns.
	// Reference: Rust aho-corasick uses 32 patterns as threshold for Fat Teddy (AVX2).
	MaxTeddyPatterns = 32

	// MinTeddyPatterns is the minimum number of patterns required for Teddy
	MinTeddyPatterns = 2

	// MinTeddyPatternLen is the minimum pattern length for effective Teddy search.
	// Patterns shorter than 3 bytes have high false positive rates.
	MinTeddyPatternLen = 3

	// MaxFingerprintLen is the maximum fingerprint length (1-4 bytes).
	// We use 2-byte fingerprint by default for better false positive rejection.
	MaxFingerprintLen = 4

	// NumBucketsSlim is the number of buckets in Slim Teddy (8 buckets, 8 bits per mask byte)
	NumBucketsSlim = 8
)

// TeddyConfig configures Teddy construction.
type TeddyConfig struct {
	// MinPatterns is the minimum patterns required (default: 2)
	MinPatterns int

	// MaxPatterns is the maximum patterns allowed (default: 8)
	MaxPatterns int

	// MinPatternLen is the minimum pattern length required (default: 3)
	MinPatternLen int

	// FingerprintLen is the number of fingerprint bytes to use (1-4, default: 1)
	// Higher values reduce false positives but increase computation cost.
	FingerprintLen int
}

// DefaultTeddyConfig returns the default Teddy configuration.
func DefaultTeddyConfig() *TeddyConfig {
	return &TeddyConfig{
		MinPatterns:    MinTeddyPatterns,
		MaxPatterns:    MaxTeddyPatterns,
		MinPatternLen:  MinTeddyPatternLen,
		FingerprintLen: 2, // 2-byte fingerprint reduces false positives by ~90%
	}
}

// Teddy is a SIMD-accelerated multi-pattern searcher.
//
// It uses bucket-based filtering to quickly identify candidate positions,
// then verifies full pattern matches. This provides dramatic speedup for
// alternation patterns like (foo|bar|baz).
//
// Thread-safety: Teddy is safe for concurrent use (all state is immutable).
type Teddy struct {
	// patterns stores the original pattern bytes
	patterns [][]byte

	// masks stores the nibble lookup tables for SIMD search
	masks *teddyMasks

	// buckets maps bucket ID to list of pattern IDs
	buckets [][]int

	// minLen is the minimum pattern length (used for verification bounds)
	minLen int

	// complete indicates if Teddy match is sufficient (no verification needed).
	// This is true when all patterns are exact complete literals.
	complete bool

	// uniformLen is the pattern length when all patterns have the same length.
	// When uniformLen > 0 and complete=true, LiteralLen() returns this value.
	uniformLen int
}

// teddyMasks stores the nibble lookup masks for SIMD search.
//
// Layout is designed to match assembly expectations:
//   - Each position has two 16-byte masks (lo nibble, hi nibble)
//   - For AVX2: masks are duplicated across both 16-byte lanes
//   - Struct must be aligned for SIMD loads
//
// Memory layout (for SSSE3, 16 bytes per mask):
//
//	fingerprintLen: 4 bytes
//	padding:        4 bytes (alignment)
//	loMasks[0-3]:   4 × 32 bytes = 128 bytes (only first 16 used for SSSE3)
//	hiMasks[0-3]:   4 × 32 bytes = 128 bytes
//
// Total: 264 bytes
type teddyMasks struct {
	// fingerprintLen is the number of fingerprint bytes (1-4)
	fingerprintLen uint32

	// Padding for alignment
	_ uint32

	// loMasks[pos] is the low-nibble lookup table for fingerprint position 'pos'.
	// Each byte at index i (0-15) contains bucket bits for patterns with low nibble i at position pos.
	// For AVX2: second 16 bytes duplicate the first 16 bytes.
	loMasks [MaxFingerprintLen][32]byte

	// hiMasks[pos] is the high-nibble lookup table for fingerprint position 'pos'.
	// Layout same as loMasks.
	hiMasks [MaxFingerprintLen][32]byte
}

// NewTeddy creates a new Teddy searcher for the given patterns.
//
// Returns nil if patterns are not suitable for Teddy:
//   - Fewer than MinPatterns (default: 2)
//   - More than MaxPatterns (default: 8)
//   - Any pattern shorter than MinPatternLen (default: 3)
//
// Parameters:
//
//	patterns - the byte patterns to search for
//	config - configuration (nil uses DefaultTeddyConfig)
//
// Example:
//
//	patterns := [][]byte{
//	    []byte("foo"),
//	    []byte("bar"),
//	    []byte("baz"),
//	}
//	teddy := prefilter.NewTeddy(patterns, nil)
//	if teddy != nil {
//	    pos := teddy.Find(haystack, 0)
//	}
func NewTeddy(patterns [][]byte, config *TeddyConfig) *Teddy {
	if config == nil {
		config = DefaultTeddyConfig()
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

	// Build masks and buckets
	masks, buckets := buildMasks(patternsCopy, fingerprintLen)

	// Teddy.Find() always verifies full pattern matches via bytes.Equal,
	// so it's always complete - a Find() match is a definitive match.
	// We set complete=true because:
	//   1. Teddy.verify() does full pattern comparison (bytes.Equal)
	//   2. No NFA verification is needed after Teddy.Find() returns a position
	complete := true

	// uniformLen is the pattern length when all patterns have the same length.
	// This allows LiteralLen() to return a value for IsComplete+LiteralLen optimization.
	uniformLen := len(patternsCopy[0])
	for _, p := range patternsCopy[1:] {
		if len(p) != uniformLen {
			uniformLen = 0 // Signal non-uniform - LiteralLen() will return 0
			break
		}
	}

	return &Teddy{
		patterns:   patternsCopy,
		masks:      masks,
		buckets:    buckets,
		minLen:     minLen,
		complete:   complete,
		uniformLen: uniformLen,
	}
}

// buildMasks constructs the nibble lookup masks for SIMD search.
//
// Algorithm:
//  1. Assign each pattern to a bucket (simple modulo distribution)
//  2. For each fingerprint position:
//     - For each pattern: extract byte at that position
//     - Split byte into low nibble (b & 0x0F) and high nibble (b >> 4)
//     - Set bucket bit in loMask[position][lowNibble]
//     - Set bucket bit in hiMask[position][highNibble]
//  3. For AVX2: duplicate first 16 bytes to second 16 bytes
//
// Returns:
//
//	masks - the nibble lookup tables
//	buckets - mapping from bucket ID to pattern IDs
func buildMasks(patterns [][]byte, fingerprintLen int) (*teddyMasks, [][]int) {
	masks := &teddyMasks{
		fingerprintLen: uint32(fingerprintLen), // #nosec G115 -- fingerprintLen is bounded by MaxFingerprintLen(4)
	}

	// Initialize buckets
	numBuckets := NumBucketsSlim
	if len(patterns) < numBuckets {
		numBuckets = len(patterns)
	}
	buckets := make([][]int, numBuckets)

	// Assign patterns to buckets and build masks
	for patternID, pattern := range patterns {
		// Simple bucket assignment: modulo distribution
		// TODO: consider hash-based distribution for better balance
		bucketID := patternID % numBuckets
		buckets[bucketID] = append(buckets[bucketID], patternID)

		// Bucket bit (1 << bucketID)
		bucketBit := byte(1 << bucketID)

		// Build masks for each fingerprint position
		for pos := 0; pos < fingerprintLen; pos++ {
			b := pattern[pos]
			loNibble := b & 0x0F        // Low 4 bits
			hiNibble := (b >> 4) & 0x0F // High 4 bits

			// Set bucket bit in both nibble masks
			masks.loMasks[pos][loNibble] |= bucketBit
			masks.hiMasks[pos][hiNibble] |= bucketBit

			// For AVX2: duplicate to second 16 bytes
			// This allows AVX2 to process both lanes identically
			masks.loMasks[pos][16+loNibble] |= bucketBit
			masks.hiMasks[pos][16+hiNibble] |= bucketBit
		}
	}

	return masks, buckets
}

// Find returns the index of the first candidate match starting at or after 'start'.
//
// This implements the Prefilter interface. It uses SIMD search to find candidates,
// then verifies full pattern matches.
//
// Returns -1 if no match is found.
//
// Algorithm:
//  1. If haystack too short: use scalar fallback
//  2. SIMD search: find candidate positions using nibble masks
//  3. For each candidate: verify full pattern match
//  4. Return first match position
//
// Thread-safety: Safe for concurrent use (all state is immutable).
func (t *Teddy) Find(haystack []byte, start int) int {
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

	// Use SIMD search (SSSE3)
	// Returns (candidate_position, bucket_mask) where bucket_mask has bits set
	// for ALL matching buckets (not just first). This matches Rust's approach.
	pos, bucketMask := t.findSIMD(haystack)

	// Track accumulated offset for continuation searches
	accumulatedOffset := 0

	// Process candidates
	for pos != -1 {
		// Iterate through all set bits in bucket mask (like Rust's verify64)
		// This ensures we check ALL potential buckets, not just the first one
		mask := bucketMask
		for mask != 0 {
			// Find lowest set bit (bucket ID)
			bucket := bits.TrailingZeros(uint(mask))
			mask &^= 1 << bucket // Clear the bit

			// Verify patterns in this specific bucket
			matchPos, _ := t.verifyBucket(haystack[accumulatedOffset:], pos, bucket)
			if matchPos != -1 {
				// Match found! Return absolute position
				return start + accumulatedOffset + matchPos
			}
		}

		// No match at this candidate in any bucket, continue searching
		// Search from position after the candidate
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
// This is more efficient than Find() when the pattern length varies,
// as it avoids the need for a separate NFA search to find the end.
//
// Returns (start, end) if found, (-1, -1) if not found.
// The matched bytes are haystack[start:end].
func (t *Teddy) FindMatch(haystack []byte, start int) (int, int) {
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

	// Use SIMD search (SSSE3)
	// Returns (candidate_position, bucket_mask) - see comment in Find()
	pos, bucketMask := t.findSIMD(haystack)

	// Track accumulated offset for continuation searches
	accumulatedOffset := 0

	// Process candidates
	for pos != -1 {
		// Iterate through all set bits in bucket mask (like Rust's verify64)
		mask := bucketMask
		for mask != 0 {
			// Find lowest set bit (bucket ID)
			bucket := bits.TrailingZeros(uint(mask))
			mask &^= 1 << bucket // Clear the bit

			// Verify patterns in this specific bucket
			matchPos, patternID := t.verifyBucket(haystack[accumulatedOffset:], pos, bucket)
			if matchPos != -1 && patternID >= 0 && patternID < len(t.patterns) {
				// Match found! Return absolute start and end
				matchStart := start + accumulatedOffset + matchPos
				matchEnd := matchStart + len(t.patterns[patternID])
				return matchStart, matchEnd
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

	return -1, -1 // No match found
}

// findMatchScalar is the scalar fallback for FindMatch.
func (t *Teddy) findMatchScalar(haystack []byte, start int) (int, int) {
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

// findScalar performs scalar search for haystacks < 16 bytes.
//
// This is a fallback when SIMD cannot be used. It simply checks each pattern
// at each position using bytes.Index.
//
// Returns absolute position in original haystack (including start offset).
func (t *Teddy) findScalar(haystack []byte, start int) int {
	// For short haystacks, just check each pattern at each position
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

// findScalarCandidate is a pure Go implementation for finding candidates.
//
// This serves as:
//  1. Fallback for platforms without SIMD
//  2. Correctness baseline for testing SIMD implementations
//  3. Build-time compatibility (code compiles everywhere)
//
// Performance: ~100x slower than SIMD, but functionally identical.
//
// Returns (position, bucketMask) or (-1, -1) if no candidate found.
// bucketMask contains bits for ALL matching buckets (not just first).
func (t *Teddy) findScalarCandidate(haystack []byte) (pos, bucketMask int) {
	// Get fingerprint length
	fpLen := int(t.masks.fingerprintLen)

	// Scan haystack byte-by-byte
	for i := 0; i+fpLen <= len(haystack); i++ {
		// Simulate SIMD: check all fingerprint bytes
		candidateMask := byte(0xFF) // Start with all buckets possible

		for pos := 0; pos < fpLen; pos++ {
			b := haystack[i+pos]
			loNibble := b & 0x0F
			hiNibble := (b >> 4) & 0x0F

			// AND together nibble masks (both must match)
			loMask := t.masks.loMasks[pos][loNibble]
			hiMask := t.masks.hiMasks[pos][hiNibble]
			candidateMask &= loMask & hiMask
		}

		// If any bucket bits remain, this is a candidate
		// Return the FULL mask - caller iterates through all set bits
		if candidateMask != 0 {
			return i, int(candidateMask)
		}
	}

	return -1, -1 // No candidate found
}

// verify checks if any pattern in any bucket matches at the given position.
//
// This is called after SIMD finds a candidate position. It performs full
// pattern comparison to eliminate false positives.
//
// CRITICAL: This recalculates the candidate mask at the position and checks
// ALL buckets (not just the first one returned by SIMD BSFL). This is necessary
// because multiple buckets may match at the same position (e.g., patterns with
// the same fingerprint: "pattern1", "pattern2", "pattern3" all start with 'p').
//
// Returns (match_position, pattern_id) or (-1, -1) if no match.
func (t *Teddy) verify(haystack []byte, pos int) (int, int) {
	// Recalculate candidate mask at this position
	// This is necessary because SIMD BSFL returns only the first bucket,
	// but multiple buckets may be candidates at the same position.
	fpLen := int(t.masks.fingerprintLen)
	if pos+fpLen > len(haystack) {
		return -1, -1
	}

	candidateMask := byte(0xFF) // Start with all buckets possible
	for i := 0; i < fpLen; i++ {
		b := haystack[pos+i]
		loNibble := b & 0x0F
		hiNibble := (b >> 4) & 0x0F
		loMask := t.masks.loMasks[i][loNibble]
		hiMask := t.masks.hiMasks[i][hiNibble]
		candidateMask &= loMask & hiMask
	}

	// Check all buckets in candidate mask
	for bucketID := 0; bucketID < len(t.buckets); bucketID++ {
		if candidateMask&(1<<bucketID) != 0 {
			// Check patterns in this bucket
			for _, patternID := range t.buckets[bucketID] {
				pattern := t.patterns[patternID]
				end := pos + len(pattern)
				if end > len(haystack) {
					continue
				}
				// Compare full pattern
				if bytes.Equal(haystack[pos:end], pattern) {
					return pos, patternID
				}
			}
		}
	}

	return -1, -1 // No match in any bucket
}

// verifyBucket checks if any pattern in the specified bucket matches at the given position.
//
// This follows Rust's aho-corasick Teddy implementation which only checks patterns
// in the SIMD-indicated bucket. SIMD uses BSFL to find the first set bit in the
// candidate mask, giving us the exact bucket to check.
//
// Reference: BurntSushi/aho-corasick src/packed/teddy/generic.rs verify_bucket()
//
// Performance: Eliminates mask recalculation overhead present in verify().
//
// Returns (match_position, pattern_id) or (-1, -1) if no match.
func (t *Teddy) verifyBucket(haystack []byte, pos int, bucket int) (int, int) {
	// Bounds check
	if pos < 0 || pos >= len(haystack) {
		return -1, -1
	}

	// Check patterns ONLY in the SIMD-indicated bucket (Rust behavior)
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
//
// Returns true if finding a Teddy match guarantees a full regex match.
// This is only true if all patterns are exact literals with no overlap.
//
// For most use cases, this returns false (verification required).
func (t *Teddy) IsComplete() bool {
	return t.complete
}

// LiteralLen implements Prefilter.LiteralLen.
//
// When all patterns have the same length and complete=true,
// returns that uniform length. Otherwise returns 0.
func (t *Teddy) LiteralLen() int {
	if t.complete && t.uniformLen > 0 {
		return t.uniformLen
	}
	return 0
}

// HeapBytes implements Prefilter.HeapBytes.
//
// Returns approximate heap memory used by Teddy:
//   - Mask tables: 264 bytes (fixed)
//   - Pattern storage: sum of pattern lengths
//   - Bucket arrays: ~8 * 8 bytes
//
// Total: typically < 1KB for 2-8 patterns.
func (t *Teddy) HeapBytes() int {
	heapBytes := 0

	// Masks struct (fixed size)
	heapBytes += 264 // sizeof(teddyMasks)

	// Pattern storage
	for _, p := range t.patterns {
		heapBytes += len(p)
	}

	// Buckets (slice of slices)
	heapBytes += len(t.buckets) * 24 // slice header (24 bytes on 64-bit)
	for _, bucket := range t.buckets {
		heapBytes += len(bucket) * 8 // int slice (8 bytes per element)
	}

	return heapBytes
}

// newTeddy creates a Teddy prefilter from extracted literal sequences.
//
// This is called by selectPrefilter when multiple literals are detected.
// It extracts pattern bytes from the literal sequence and constructs Teddy.
//
// Returns nil if literals are not suitable for Teddy.
func newTeddy(seq *literal.Seq) Prefilter {
	// Extract pattern bytes from literal sequence
	patterns := make([][]byte, seq.Len())
	for i := 0; i < seq.Len(); i++ {
		patterns[i] = seq.Get(i).Bytes
	}

	// Build Teddy with default config
	return NewTeddy(patterns, nil)
}
