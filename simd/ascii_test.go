package simd

import (
	"bytes"
	"testing"
)

// TestIsASCII_Basic tests basic ASCII detection functionality
func TestIsASCII_Basic(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		// Empty input
		{"empty", nil, true},
		{"empty_slice", []byte{}, true},

		// Single byte - ASCII
		{"single_ascii_zero", []byte{0x00}, true},
		{"single_ascii_a", []byte{'a'}, true},
		{"single_ascii_z", []byte{'z'}, true},
		{"single_ascii_A", []byte{'A'}, true},
		{"single_ascii_Z", []byte{'Z'}, true},
		{"single_ascii_0", []byte{'0'}, true},
		{"single_ascii_9", []byte{'9'}, true},
		{"single_ascii_space", []byte{' '}, true},
		{"single_ascii_del", []byte{0x7F}, true},

		// Single byte - non-ASCII
		{"single_non_ascii_0x80", []byte{0x80}, false},
		{"single_non_ascii_0x81", []byte{0x81}, false},
		{"single_non_ascii_0xC0", []byte{0xC0}, false},
		{"single_non_ascii_0xFF", []byte{0xFF}, false},

		// Short strings - ASCII
		{"short_hello", []byte("hello"), true},
		{"short_world", []byte("world"), true},
		{"short_with_numbers", []byte("abc123"), true},
		{"short_with_punctuation", []byte("hello, world!"), true},
		{"short_all_printable", []byte(" !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"), true},

		// Short strings - non-ASCII
		{"short_utf8_e_acute", []byte("héllo"), false},
		{"short_utf8_emoji", []byte("hello 😀"), false},
		{"short_utf8_cyrillic", []byte("привет"), false},
		{"short_utf8_chinese", []byte("你好"), false},

		// 8-byte boundary tests (SWAR chunk size)
		{"8_bytes_ascii", []byte("12345678"), true},
		{"8_bytes_non_ascii_first", append([]byte{0x80}, []byte("1234567")...), false},
		{"8_bytes_non_ascii_last", append([]byte("1234567"), 0x80), false},
		{"8_bytes_non_ascii_middle", []byte("123\x80567"), false},

		// 32-byte boundary tests (AVX2 vector size)
		{"32_bytes_ascii", []byte("12345678901234567890123456789012"), true},
		{"32_bytes_non_ascii_first", append([]byte{0x80}, bytes.Repeat([]byte{'a'}, 31)...), false},
		{"32_bytes_non_ascii_last", append(bytes.Repeat([]byte{'a'}, 31), 0x80), false},
		{"32_bytes_non_ascii_middle", append(append(bytes.Repeat([]byte{'a'}, 15), 0x80), bytes.Repeat([]byte{'b'}, 16)...), false},

		// Larger inputs
		{"64_bytes_ascii", bytes.Repeat([]byte{'x'}, 64), true},
		{"100_bytes_ascii", bytes.Repeat([]byte{'y'}, 100), true},
		{"1000_bytes_ascii", bytes.Repeat([]byte{'z'}, 1000), true},

		// Non-ASCII at various positions in larger inputs
		{"64_bytes_non_ascii_at_0", func() []byte {
			b := bytes.Repeat([]byte{'a'}, 64)
			b[0] = 0x80
			return b
		}(), false},
		{"64_bytes_non_ascii_at_31", func() []byte {
			b := bytes.Repeat([]byte{'a'}, 64)
			b[31] = 0x80
			return b
		}(), false},
		{"64_bytes_non_ascii_at_32", func() []byte {
			b := bytes.Repeat([]byte{'a'}, 64)
			b[32] = 0x80
			return b
		}(), false},
		{"64_bytes_non_ascii_at_63", func() []byte {
			b := bytes.Repeat([]byte{'a'}, 64)
			b[63] = 0x80
			return b
		}(), false},

		// Edge cases for non-ASCII values
		{"boundary_0x7F", []byte{0x7F}, true},  // Last ASCII character
		{"boundary_0x80", []byte{0x80}, false}, // First non-ASCII
		{"boundary_mixed", []byte{0x7F, 0x80}, false},

		// URL path patterns (common in regex testing)
		{"url_path_ascii", []byte("/path/to/admin/file.php"), true},
		{"url_path_utf8", []byte("/path/to/файл.php"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsASCII(tc.input)
			if result != tc.expected {
				t.Errorf("IsASCII(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

// TestIsASCII_Generic tests the generic implementation directly
func TestIsASCII_Generic(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{"empty", nil, true},
		{"ascii_short", []byte("hello"), true},
		{"non_ascii_short", []byte("héllo"), false},
		{"ascii_8bytes", []byte("12345678"), true},
		{"non_ascii_8bytes", []byte("1234567\x80"), false},
		{"ascii_long", bytes.Repeat([]byte{'a'}, 100), true},
		{"non_ascii_long_end", append(bytes.Repeat([]byte{'a'}, 99), 0xFF), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isASCIIGeneric(tc.input)
			if result != tc.expected {
				t.Errorf("isASCIIGeneric(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

// TestFirstNonASCII tests the FirstNonASCII helper function
func TestFirstNonASCII(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int
	}{
		{"empty", nil, -1},
		{"all_ascii", []byte("hello world"), -1},
		{"non_ascii_at_0", []byte{0x80, 'a', 'b'}, 0},
		{"non_ascii_at_5", []byte("hello\x80world"), 5},
		{"utf8_e_acute", []byte("h\xc3\xa9llo"), 1}, // é is \xc3\xa9 in UTF-8
		{"non_ascii_at_end", append([]byte("hello"), 0xFF), 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FirstNonASCII(tc.input)
			if result != tc.expected {
				t.Errorf("FirstNonASCII(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

// TestCountNonASCII tests the CountNonASCII helper function
func TestCountNonASCII(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int
	}{
		{"empty", nil, 0},
		{"all_ascii", []byte("hello world"), 0},
		{"one_non_ascii", []byte{0x80, 'a', 'b'}, 1},
		{"two_non_ascii", []byte{0x80, 'a', 0xFF}, 2},
		{"utf8_e_acute", []byte("h\xc3\xa9llo"), 2}, // é is 2 bytes: \xc3 and \xa9
		{"all_non_ascii", []byte{0x80, 0x81, 0xFF}, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CountNonASCII(tc.input)
			if result != tc.expected {
				t.Errorf("CountNonASCII(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

// TestIsASCII_AllBytesValues ensures correctness for all possible byte values
func TestIsASCII_AllBytesValues(t *testing.T) {
	// Test each byte value individually
	for i := 0; i <= 255; i++ {
		b := byte(i)
		result := IsASCII([]byte{b})
		expected := b < 0x80

		if result != expected {
			t.Errorf("IsASCII([%d]) = %v, want %v", i, result, expected)
		}
	}
}

// TestIsASCII_Alignment tests various alignments to catch potential SIMD alignment issues
func TestIsASCII_Alignment(t *testing.T) {
	// Create a large buffer
	buf := bytes.Repeat([]byte{'a'}, 256)

	// Test different starting offsets and lengths
	for offset := 0; offset < 64; offset++ {
		for length := 0; length < 128 && offset+length <= len(buf); length++ {
			slice := buf[offset : offset+length]

			result := IsASCII(slice)
			if !result {
				t.Errorf("IsASCII failed for offset=%d, length=%d", offset, length)
			}
		}
	}
}

// TestIsASCII_ConsistencyWithGeneric ensures AVX2 and generic produce same results
func TestIsASCII_ConsistencyWithGeneric(t *testing.T) {
	testCases := [][]byte{
		nil,
		{},
		[]byte("hello"),
		[]byte("hello world this is a longer string"),
		bytes.Repeat([]byte{'a'}, 31),
		bytes.Repeat([]byte{'a'}, 32),
		bytes.Repeat([]byte{'a'}, 33),
		bytes.Repeat([]byte{'a'}, 64),
		bytes.Repeat([]byte{'a'}, 100),
		append(bytes.Repeat([]byte{'a'}, 50), 0x80),
		[]byte("héllo"),
		[]byte("/path/to/file.php"),
	}

	for i, tc := range testCases {
		result := IsASCII(tc)
		genericResult := isASCIIGeneric(tc)

		if result != genericResult {
			t.Errorf("Test case %d: IsASCII and isASCIIGeneric disagree: IsASCII=%v, generic=%v, input=%q",
				i, result, genericResult, tc)
		}
	}
}

// Benchmarks

// BenchmarkIsASCII_32 benchmarks 32-byte input (single AVX2 vector)
func BenchmarkIsASCII_32(b *testing.B) {
	data := bytes.Repeat([]byte{'a'}, 32)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_64 benchmarks 64-byte input (two AVX2 vectors)
func BenchmarkIsASCII_64(b *testing.B) {
	data := bytes.Repeat([]byte{'a'}, 64)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_1KB benchmarks 1KB input
func BenchmarkIsASCII_1KB(b *testing.B) {
	data := bytes.Repeat([]byte{'a'}, 1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_4KB benchmarks 4KB input
func BenchmarkIsASCII_4KB(b *testing.B) {
	data := bytes.Repeat([]byte{'a'}, 4*1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_1MB benchmarks 1MB input
func BenchmarkIsASCII_1MB(b *testing.B) {
	data := bytes.Repeat([]byte{'a'}, 1024*1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_6MB benchmarks 6MB input (standard regex benchmark size)
func BenchmarkIsASCII_6MB(b *testing.B) {
	data := bytes.Repeat([]byte{'a'}, 6*1024*1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_Generic_1KB benchmarks generic implementation
func BenchmarkIsASCII_Generic_1KB(b *testing.B) {
	data := bytes.Repeat([]byte{'a'}, 1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = isASCIIGeneric(data)
	}
}

// BenchmarkIsASCII_Generic_1MB benchmarks generic implementation on large input
func BenchmarkIsASCII_Generic_1MB(b *testing.B) {
	data := bytes.Repeat([]byte{'a'}, 1024*1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = isASCIIGeneric(data)
	}
}

// BenchmarkIsASCII_Small_7 benchmarks small input (below SWAR threshold)
func BenchmarkIsASCII_Small_7(b *testing.B) {
	data := []byte("hello!!")
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_Small_15 benchmarks small input (above SWAR, below AVX2)
func BenchmarkIsASCII_Small_15(b *testing.B) {
	data := []byte("hello world!!!")
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_URLPath benchmarks typical URL path (Issue #79 pattern)
func BenchmarkIsASCII_URLPath(b *testing.B) {
	data := []byte("/path/to/admin/file.php")
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_NonASCII_Early benchmarks early bailout on non-ASCII
func BenchmarkIsASCII_NonASCII_Early(b *testing.B) {
	data := append([]byte{0x80}, bytes.Repeat([]byte{'a'}, 1023)...)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}

// BenchmarkIsASCII_NonASCII_Late benchmarks late non-ASCII detection
func BenchmarkIsASCII_NonASCII_Late(b *testing.B) {
	data := append(bytes.Repeat([]byte{'a'}, 1023), 0x80)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsASCII(data)
	}
}
