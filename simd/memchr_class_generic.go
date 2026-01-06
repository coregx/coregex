package simd

// isWordChar returns true if b is a word character [A-Za-z0-9_].
func isWordChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

// memchrWordGeneric is the scalar implementation of MemchrWord.
// Used as fallback when SIMD is not available or for small inputs.
func memchrWordGeneric(haystack []byte) int {
	for i, b := range haystack {
		if isWordChar(b) {
			return i
		}
	}
	return -1
}

// memchrNotWordGeneric is the scalar implementation of MemchrNotWord.
func memchrNotWordGeneric(haystack []byte) int {
	for i, b := range haystack {
		if !isWordChar(b) {
			return i
		}
	}
	return -1
}

// memchrInTableGeneric is the scalar implementation of MemchrInTable.
func memchrInTableGeneric(haystack []byte, table *[256]bool) int {
	for i, b := range haystack {
		if table[b] {
			return i
		}
	}
	return -1
}

// memchrNotInTableGeneric is the scalar implementation of MemchrNotInTable.
func memchrNotInTableGeneric(haystack []byte, table *[256]bool) int {
	for i, b := range haystack {
		if !table[b] {
			return i
		}
	}
	return -1
}
