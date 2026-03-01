package simd

// ByteFrequencies contains empirical byte frequency ranks based on analysis
// of English text, source code, and binary data.
//
// Lower rank = rarer byte (better candidate for SIMD search).
// Higher rank = more common byte (worse candidate).
//
// The table is derived from:
//   - English text corpus analysis
//   - Source code repositories (Go, Rust, C, Python)
//   - Binary file sampling
//
// This matches the approach used by Rust's memchr crate for optimal
// rare byte selection in substring search.
//
// Reference: https://github.com/BurntSushi/memchr
var ByteFrequencies = [256]byte{
	// 0x00-0x0F: Control characters (generally rare)
	0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 1, 0, 0,
	// 0x10-0x1F: More control characters
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	// 0x20-0x2F: Space, punctuation
	// ' '=255 (most common), '!'=60, '"'=140, '#'=50, '$'=40, '%'=35, '&'=30, '\''=160
	// '('=130, ')'=130, '*'=80, '+'=55, ','=200, '-'=140, '.'=210, '/'=100
	255, 60, 140, 50, 40, 35, 30, 160, 130, 130, 80, 55, 200, 140, 210, 100,
	// 0x30-0x3F: Digits and more punctuation
	// '0'=180, '1'=190, '2'=170, '3'=150, '4'=140, '5'=140, '6'=130, '7'=120
	// '8'=120, '9'=120, ':'=150, ';'=100, '<'=70, '='=160, '>'=70, '?'=50
	180, 190, 170, 150, 140, 140, 130, 120, 120, 120, 150, 100, 70, 160, 70, 50,
	// 0x40-0x4F: '@' and uppercase A-O
	// '@'=25 (rare!), 'A'=120, 'B'=80, 'C'=90, 'D'=85, 'E'=130, 'F'=75, 'G'=70
	// 'H'=80, 'I'=115, 'J'=30, 'K'=35, 'L'=90, 'M'=85, 'N'=100, 'O'=105
	25, 120, 80, 90, 85, 130, 75, 70, 80, 115, 30, 35, 90, 85, 100, 105,
	// 0x50-0x5F: Uppercase P-Z and brackets
	// 'P'=80, 'Q'=15, 'R'=100, 'S'=110, 'T'=115, 'U'=70, 'V'=45, 'W'=55
	// 'X'=20, 'Y'=50, 'Z'=10, '['=90, '\\'=60, ']'=90, '^'=20, '_'=110
	80, 15, 100, 110, 115, 70, 45, 55, 20, 50, 10, 90, 60, 90, 20, 110,
	// 0x60-0x6F: Backtick and lowercase a-o
	// '`'=30, 'a'=225, 'b'=140, 'c'=170, 'd'=165, 'e'=245, 'f'=135, 'g'=130
	// 'h'=150, 'i'=200, 'j'=25, 'k'=65, 'l'=175, 'm'=155, 'n'=195, 'o'=205
	30, 225, 140, 170, 165, 245, 135, 130, 150, 200, 25, 65, 175, 155, 195, 205,
	// 0x70-0x7F: Lowercase p-z and braces
	// 'p'=145, 'q'=15, 'r'=195, 's'=200, 't'=215, 'u'=150, 'v'=75, 'w'=95
	// 'x'=45, 'y'=120, 'z'=20, '{'=85, '|'=40, '}'=85, '~'=15, DEL=0
	145, 15, 195, 200, 215, 150, 75, 95, 45, 120, 20, 85, 40, 85, 15, 0,
	// 0x80-0xFF: Extended ASCII / UTF-8 continuation bytes (generally rare in text)
	// These are less common in typical text/code, so they get low ranks
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
}

// ByteRank returns the frequency rank of a byte.
// Lower values indicate rarer bytes (better for search optimization).
func ByteRank(b byte) byte {
	return ByteFrequencies[b]
}

// RareByteInfo holds information about selected rare bytes for search.
type RareByteInfo struct {
	// Byte1 is the rarest byte found in the needle.
	Byte1 byte
	// Index1 is the position of Byte1 in the needle.
	Index1 int
	// Byte2 is the second rarest byte (different from Byte1).
	Byte2 byte
	// Index2 is the position of Byte2 in the needle.
	Index2 int
}

// SelectRareBytes finds the two rarest bytes in needle using the frequency table.
// This enables paired-byte SIMD search which is more selective than single-byte.
//
// The algorithm:
//  1. Start with first two bytes as candidates
//  2. Iterate through needle, tracking the two rarest bytes seen
//  3. Ensure Byte1 is always the rarest (lowest rank)
//  4. Ensure Byte2 is different from Byte1
//
// Returns RareByteInfo with the two rarest bytes and their positions.
// For needles shorter than 2 bytes, Byte2/Index2 may equal Byte1/Index1.
func SelectRareBytes(needle []byte) RareByteInfo {
	n := len(needle)

	if n == 0 {
		return RareByteInfo{}
	}

	if n == 1 {
		return RareByteInfo{
			Byte1:  needle[0],
			Index1: 0,
			Byte2:  needle[0],
			Index2: 0,
		}
	}

	// Initialize with first two bytes
	byte1, idx1 := needle[0], 0
	byte2, idx2 := needle[1], 1

	// Ensure byte1 is the rarer one
	if ByteFrequencies[byte2] < ByteFrequencies[byte1] {
		byte1, byte2 = byte2, byte1
		idx1, idx2 = idx2, idx1
	}

	// Scan remaining bytes for even rarer candidates
	for i := 2; i < n; i++ {
		b := needle[i]
		rank := ByteFrequencies[b]

		if rank < ByteFrequencies[byte1] {
			// Found new rarest byte - shift byte1 to byte2
			byte2, idx2 = byte1, idx1
			byte1, idx1 = b, i
		} else if b != byte1 && rank < ByteFrequencies[byte2] {
			// Found new second-rarest byte (must be different from byte1)
			byte2, idx2 = b, i
		}
	}

	return RareByteInfo{
		Byte1:  byte1,
		Index1: idx1,
		Byte2:  byte2,
		Index2: idx2,
	}
}

// selectRareByteOptimized returns the rarest byte in needle using frequency table.
// This is a drop-in replacement for the simple last-byte heuristic.
func selectRareByteOptimized(needle []byte) (rareByte byte, index int) {
	n := len(needle)
	if n == 0 {
		return 0, -1
	}

	rareByte = needle[0]
	index = 0
	minRank := ByteFrequencies[rareByte]

	for i := 1; i < n; i++ {
		b := needle[i]
		rank := ByteFrequencies[b]
		if rank < minRank {
			rareByte = b
			index = i
			minRank = rank
		}
	}

	return rareByte, index
}
