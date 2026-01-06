//go:build !amd64

package simd

// MemchrWord finds the first word character [A-Za-z0-9_] in haystack.
// Returns position or -1 if not found.
//
// This is the fallback implementation for non-AMD64 platforms.
func MemchrWord(haystack []byte) int {
	return memchrWordGeneric(haystack)
}

// MemchrNotWord finds the first non-word character in haystack.
// Returns position or -1 if all bytes are word characters.
//
// This is the fallback implementation for non-AMD64 platforms.
func MemchrNotWord(haystack []byte) int {
	return memchrNotWordGeneric(haystack)
}

// MemchrInTable finds the first byte where table[byte] is true.
// Returns position or -1 if not found.
func MemchrInTable(haystack []byte, table *[256]bool) int {
	if len(haystack) == 0 || table == nil {
		return -1
	}
	return memchrInTableGeneric(haystack, table)
}

// MemchrNotInTable finds the first byte where table[byte] is false.
// Returns position or -1 if all bytes have table[byte] == true.
func MemchrNotInTable(haystack []byte, table *[256]bool) int {
	if len(haystack) == 0 || table == nil {
		return -1
	}
	return memchrNotInTableGeneric(haystack, table)
}
