//go:build amd64

package simd

// Assembly function declarations for character class search.
// These are implemented in memchr_class_amd64.s using AVX2.

// memchrWordAVX2 finds the first word character [A-Za-z0-9_] in haystack.
// Returns position or -1 if not found.
//
//go:noescape
func memchrWordAVX2(haystack []byte) int

// memchrNotWordAVX2 finds the first non-word character in haystack.
// Returns position or -1 if all bytes are word characters.
//
//go:noescape
func memchrNotWordAVX2(haystack []byte) int

// MemchrWord finds the first word character [A-Za-z0-9_] in haystack.
// Returns position or -1 if not found.
//
// This function uses AVX2 SIMD instructions to process 32 bytes per iteration
// when available, achieving ~10x speedup over scalar search on large inputs.
//
// Performance characteristics:
//   - Small inputs (< 32 bytes): scalar fallback
//   - Large inputs (>= 32 bytes): AVX2 processes 32 bytes/iteration
//
// Example:
//
//	haystack := []byte("   hello123")
//	pos := simd.MemchrWord(haystack)
//	// pos == 3 (position of 'h')
func MemchrWord(haystack []byte) int {
	if len(haystack) == 0 {
		return -1
	}

	if hasAVX2 && len(haystack) >= 32 {
		return memchrWordAVX2(haystack)
	}

	return memchrWordGeneric(haystack)
}

// MemchrNotWord finds the first non-word character in haystack.
// Returns position or -1 if all bytes are word characters.
//
// This is the complement of MemchrWord: it finds the first byte that is
// NOT in [A-Za-z0-9_].
//
// Example:
//
//	haystack := []byte("hello world")
//	pos := simd.MemchrNotWord(haystack)
//	// pos == 5 (position of space)
func MemchrNotWord(haystack []byte) int {
	if len(haystack) == 0 {
		return -1
	}

	if hasAVX2 && len(haystack) >= 32 {
		return memchrNotWordAVX2(haystack)
	}

	return memchrNotWordGeneric(haystack)
}

// MemchrInTable finds the first byte where table[byte] is true.
// This is a general-purpose character class search using a 256-byte lookup table.
//
// For specific classes like \w, use MemchrWord instead for better performance.
//
// Returns position or -1 if not found.
func MemchrInTable(haystack []byte, table *[256]bool) int {
	if len(haystack) == 0 || table == nil {
		return -1
	}

	// For now, use scalar. SIMD version would require VPGATHERDD or
	// pre-computed nibble tables which adds complexity.
	return memchrInTableGeneric(haystack, table)
}

// MemchrNotInTable finds the first byte where table[byte] is false.
// This is the complement of MemchrInTable.
//
// Returns position or -1 if all bytes have table[byte] == true.
func MemchrNotInTable(haystack []byte, table *[256]bool) int {
	if len(haystack) == 0 || table == nil {
		return -1
	}

	return memchrNotInTableGeneric(haystack, table)
}
