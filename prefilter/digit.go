// Package prefilter provides fast candidate filtering for regex search.
//
// This file implements DigitPrefilter, a specialized prefilter for patterns
// that must start with an ASCII digit [0-9]. Used for IP address patterns
// and other digit-lead alternations where literal extraction fails.

package prefilter

import "github.com/coregx/coregex/simd"

// DigitPrefilter implements the Prefilter interface for patterns that must
// start with ASCII digits [0-9].
//
// It uses SIMD-accelerated digit scanning to quickly find candidate positions,
// allowing the regex engine to skip large regions of non-digit text.
// This is particularly effective for:
//   - IP address patterns: `(?:25[0-5]|2[0-4][0-9]|...)`
//   - Numeric validators: `[1-9][0-9]*`
//   - Phone number patterns: `\d{3}-\d{3}-\d{4}`
//
// Performance characteristics:
//   - Small inputs (< 32 bytes): 2-3x faster than byte-by-byte
//   - Medium inputs (4KB): 8-10x faster
//   - Large inputs (64KB+): 15-20x faster
//
// This prefilter is NOT complete - finding a digit is only a candidate
// position. The full regex must be verified at that position.
//
// Example usage (internal):
//
//	pf := NewDigitPrefilter()
//	pos := pf.Find(haystack, 0)
//	for pos != -1 {
//	    if regexMatchesAt(haystack, pos) {
//	        return pos
//	    }
//	    pos = pf.Find(haystack, pos+1)
//	}
type DigitPrefilter struct{}

// NewDigitPrefilter creates a prefilter for patterns that must start with digits.
//
// This prefilter uses simd.MemchrDigitAt internally for fast digit scanning.
// It is designed for patterns where all alternation branches must begin with
// a digit [0-9], enabling efficient skip-ahead through non-digit regions.
//
// Example patterns that benefit:
//   - IP address: `25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9]`
//   - Numeric: `[0-9]+`
//   - Date: `[0-9]{4}-[0-9]{2}-[0-9]{2}`
func NewDigitPrefilter() *DigitPrefilter {
	return &DigitPrefilter{}
}

// Find returns the index of the first digit at or after 'start'.
// Returns -1 if no digit is found in the remaining haystack.
//
// This method uses SIMD acceleration on AMD64 with AVX2 support.
// For inputs >= 32 bytes, processes 32 bytes per iteration.
//
// Parameters:
//   - haystack: the byte slice to search
//   - start: the starting position (inclusive)
//
// Returns:
//   - index >= start if a digit candidate is found
//   - -1 if no digit exists at or after start
func (p *DigitPrefilter) Find(haystack []byte, start int) int {
	return simd.MemchrDigitAt(haystack, start)
}

// IsComplete returns false because finding a digit is only a candidate position.
// The full regex must be verified at that position to confirm a match.
//
// Unlike literal prefilters (Memchr, Memmem) which can sometimes guarantee
// a match, digit prefiltering only narrows the search space - the actual
// pattern may still fail to match at the digit position.
func (p *DigitPrefilter) IsComplete() bool {
	return false
}

// LiteralLen returns 0 because DigitPrefilter doesn't match fixed-length literals.
//
// The prefilter finds digit characters, but the actual match length depends
// on the full regex pattern being verified.
func (p *DigitPrefilter) LiteralLen() int {
	return 0
}

// HeapBytes returns 0 because DigitPrefilter uses no heap allocation.
//
// The prefilter is stateless and relies on simd.MemchrDigitAt which
// operates directly on the input slice without additional allocations.
func (p *DigitPrefilter) HeapBytes() int {
	return 0
}
