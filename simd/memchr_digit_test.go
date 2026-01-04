package simd

import (
	"fmt"
	"testing"
)

// refMemchrDigit is a reference implementation for verification.
// It scans byte-by-byte checking for ASCII digits.
func refMemchrDigit(haystack []byte) int {
	for i, b := range haystack {
		if b >= '0' && b <= '9' {
			return i
		}
	}
	return -1
}

// TestMemchrDigitBasic tests basic functionality and edge cases
func TestMemchrDigitBasic(t *testing.T) {
	tests := []struct {
		name     string
		haystack []byte
		want     int
	}{
		// Empty and single byte cases
		{"empty_haystack", []byte{}, -1},
		{"single_digit_0", []byte{'0'}, 0},
		{"single_digit_5", []byte{'5'}, 0},
		{"single_digit_9", []byte{'9'}, 0},
		{"single_non_digit_a", []byte{'a'}, -1},
		{"single_non_digit_slash", []byte{'/'}, -1}, // 0x2F, just before '0'
		{"single_non_digit_colon", []byte{':'}, -1}, // 0x3A, just after '9'

		// Position tests
		{"first_position", []byte("0hello"), 0},
		{"middle_position", []byte("hel5lo"), 3},
		{"last_position", []byte("hello9"), 5},
		{"not_found", []byte("hello"), -1},

		// Multiple digits (should return first)
		{"multiple_returns_first", []byte("hello 123 world"), 6},
		{"digits_only", []byte("12345"), 0},

		// Boundary bytes (just outside digit range)
		{"slash_before_0", []byte("/abc"), -1}, // 0x2F
		{"colon_after_9", []byte(":abc"), -1},  // 0x3A
		{"slash_then_digit", []byte("/0abc"), 1},
		{"colon_then_digit", []byte(":5abc"), 1},

		// All digits individually
		{"digit_0", []byte("abc0def"), 3},
		{"digit_1", []byte("abc1def"), 3},
		{"digit_2", []byte("abc2def"), 3},
		{"digit_3", []byte("abc3def"), 3},
		{"digit_4", []byte("abc4def"), 3},
		{"digit_5", []byte("abc5def"), 3},
		{"digit_6", []byte("abc6def"), 3},
		{"digit_7", []byte("abc7def"), 3},
		{"digit_8", []byte("abc8def"), 3},
		{"digit_9", []byte("abc9def"), 3},

		// IP-like patterns
		{"ip_pattern", []byte("Server at 192.168.1.1"), 10},
		{"ip_v4_start", []byte("192.168.1.1"), 0},

		// Mixed content
		{"mixed_alpha_digit", []byte("abc123xyz"), 3},
		{"punctuation_then_digit", []byte("...!!!???5"), 9},
		{"spaces_then_digit", []byte("          1"), 10},

		// Special bytes
		{"null_bytes_then_digit", []byte{0, 0, 0, '5'}, 3},
		{"high_bytes_then_digit", []byte{255, 254, 253, '7'}, 3},
		{"tab_newline_then_digit", []byte("\t\n\r8"), 3},

		// Longer strings
		{"longer_with_digit", []byte("the quick brown fox jumps over the lazy dog 42"), 44},
		{"longer_no_digit", []byte("the quick brown fox jumps over the lazy dog"), -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MemchrDigit(tt.haystack)
			if got != tt.want {
				t.Errorf("MemchrDigit(%q) = %d, want %d", tt.haystack, got, tt.want)
			}

			// Verify against reference implementation
			refGot := refMemchrDigit(tt.haystack)
			if got != refGot {
				t.Errorf("MemchrDigit != reference: got %d, reference %d (haystack=%q)",
					got, refGot, tt.haystack)
			}
		})
	}
}

// TestMemchrDigitSizes tests various input sizes including boundary conditions
func TestMemchrDigitSizes(t *testing.T) {
	// Critical sizes: powers of 2, multiples of 16/32, edge cases
	sizes := []int{
		1, 2, 3, 4, 5, 6, 7, 8, // Small sizes
		15, 16, 17, // 16-byte boundary
		31, 32, 33, // 32-byte boundary (AVX2)
		63, 64, 65, // 64-byte boundary
		127, 128, 129, // 128-byte boundary
		255, 256, 257, // 256-byte boundary
		1023, 1024, 1025, // 1KB boundary
		4095, 4096, 4097, // 4KB boundary
		16383, 16384, // 16KB boundary
		65535, 65536, // 64KB boundary
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d_at_end", size), func(t *testing.T) {
			haystack := make([]byte, size)
			// Fill with 'a' (non-digit)
			for i := range haystack {
				haystack[i] = 'a'
			}
			haystack[size-1] = '5' // digit at end

			got := MemchrDigit(haystack)
			want := size - 1
			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}

			// Verify vs reference
			refGot := refMemchrDigit(haystack)
			if got != refGot {
				t.Errorf("size %d: mismatch with reference: got %d, ref %d", size, got, refGot)
			}
		})

		t.Run(fmt.Sprintf("size_%d_at_start", size), func(t *testing.T) {
			haystack := make([]byte, size)
			// Fill with 'a' (non-digit)
			for i := range haystack {
				haystack[i] = 'a'
			}
			haystack[0] = '3' // digit at start

			got := MemchrDigit(haystack)
			want := 0
			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}

			// Verify vs reference
			refGot := refMemchrDigit(haystack)
			if got != refGot {
				t.Errorf("size %d: mismatch with reference: got %d, ref %d", size, got, refGot)
			}
		})

		t.Run(fmt.Sprintf("size_%d_not_found", size), func(t *testing.T) {
			haystack := make([]byte, size)
			// Fill with 'a' (non-digit)
			for i := range haystack {
				haystack[i] = 'a'
			}

			got := MemchrDigit(haystack)
			want := -1
			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}

			// Verify vs reference
			refGot := refMemchrDigit(haystack)
			if got != refGot {
				t.Errorf("size %d: mismatch with reference: got %d, ref %d", size, got, refGot)
			}
		})

		// Test digit at various positions within chunk
		if size >= 64 {
			positions := []int{0, 1, 15, 16, 17, 31, 32, 33, size / 2, size - 1}
			for _, pos := range positions {
				if pos >= size {
					continue
				}
				t.Run(fmt.Sprintf("size_%d_at_pos_%d", size, pos), func(t *testing.T) {
					haystack := make([]byte, size)
					for i := range haystack {
						haystack[i] = 'x'
					}
					haystack[pos] = '7'

					got := MemchrDigit(haystack)
					if got != pos {
						t.Errorf("size %d, pos %d: got %d, want %d", size, pos, got, pos)
					}
				})
			}
		}
	}
}

// TestMemchrDigitAlignment tests misaligned haystack starts (important for SIMD)
func TestMemchrDigitAlignment(t *testing.T) {
	// Create a large buffer to allow different alignment offsets
	buf := make([]byte, 256)
	for i := range buf {
		buf[i] = 'a'
	}
	buf[128] = '8' // digit in middle

	// Test slices starting at different offsets (0-31 covers AVX2 alignment)
	for offset := 0; offset < 32; offset++ {
		t.Run(fmt.Sprintf("offset_%d", offset), func(t *testing.T) {
			haystack := buf[offset:]
			got := MemchrDigit(haystack)
			want := 128 - offset

			if got != want {
				t.Errorf("offset %d: got %d, want %d", offset, got, want)
			}

			// Verify vs reference
			refGot := refMemchrDigit(haystack)
			if got != refGot {
				t.Errorf("offset %d: mismatch with reference: got %d, ref %d", offset, got, refGot)
			}
		})
	}

	// Test not found with different alignments
	for offset := 0; offset < 32; offset++ {
		t.Run(fmt.Sprintf("offset_%d_not_found", offset), func(t *testing.T) {
			haystack := buf[offset : offset+64]
			got := MemchrDigit(haystack)
			want := -1

			if got != want {
				t.Errorf("offset %d: got %d, want %d", offset, got, want)
			}

			// Verify vs reference
			refGot := refMemchrDigit(haystack)
			if got != refGot {
				t.Errorf("offset %d: mismatch with reference: got %d, ref %d", offset, got, refGot)
			}
		})
	}
}

// TestMemchrDigitAllBytes tests all 256 byte values to ensure only digits match
func TestMemchrDigitAllBytes(t *testing.T) {
	// Test each byte value individually
	for b := 0; b < 256; b++ {
		haystack := []byte{byte(b)}
		isDigit := b >= '0' && b <= '9'

		t.Run(fmt.Sprintf("byte_%d", b), func(t *testing.T) {
			got := MemchrDigit(haystack)

			var want int
			if isDigit {
				want = 0
			} else {
				want = -1
			}

			if got != want {
				t.Errorf("byte %d (0x%02X, %q): got %d, want %d (isDigit=%v)",
					b, b, string(rune(b)), got, want, isDigit)
			}
		})
	}

	// Create haystack with all bytes 0-255, find first digit ('0' = 0x30)
	haystack := make([]byte, 256)
	for i := 0; i < 256; i++ {
		haystack[i] = byte(i)
	}

	t.Run("all_bytes_first_digit", func(t *testing.T) {
		got := MemchrDigit(haystack)
		want := int('0') // 0x30 = 48

		if got != want {
			t.Errorf("first digit in 0-255: got %d, want %d", got, want)
		}
	})

	// Test haystack with no digits (all bytes except 0x30-0x39)
	noDigitsLen := 256 - 10
	noDigits := make([]byte, noDigitsLen)
	idx := 0
	for i := 0; i < 256; i++ {
		if i < '0' || i > '9' {
			noDigits[idx] = byte(i)
			idx++
		}
	}

	t.Run("no_digits_in_haystack", func(t *testing.T) {
		got := MemchrDigit(noDigits)
		want := -1

		if got != want {
			t.Errorf("no digits: got %d, want %d", got, want)
		}
	})
}

// TestMemchrDigitAtBasic tests MemchrDigitAt functionality
func TestMemchrDigitAtBasic(t *testing.T) {
	tests := []struct {
		name     string
		haystack []byte
		at       int
		want     int
	}{
		// Basic cases
		{"simple_from_0", []byte("abc123def"), 0, 3},
		{"simple_from_3", []byte("abc123def"), 3, 3},
		{"simple_from_4", []byte("abc123def"), 4, 4},
		{"simple_from_6", []byte("abc123def"), 6, -1},

		// Edge cases
		{"empty_haystack", []byte{}, 0, -1},
		{"at_negative", []byte("123"), -1, -1},
		{"at_out_of_bounds", []byte("123"), 10, -1},
		{"at_exact_length", []byte("123"), 3, -1},

		// Multiple digit groups
		{"multiple_groups", []byte("abc123def456"), 0, 3},
		{"multiple_groups_from_6", []byte("abc123def456"), 6, 9},
		{"multiple_groups_from_9", []byte("abc123def456"), 9, 9},

		// No digits
		{"no_digits_from_0", []byte("abcdefgh"), 0, -1},
		{"no_digits_from_5", []byte("abcdefgh"), 5, -1},

		// Single byte
		{"single_digit", []byte("5"), 0, 0},
		{"single_non_digit", []byte("a"), 0, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MemchrDigitAt(tt.haystack, tt.at)
			if got != tt.want {
				t.Errorf("MemchrDigitAt(%q, %d) = %d, want %d",
					tt.haystack, tt.at, got, tt.want)
			}
		})
	}
}

// TestMemchrDigitConsistency tests AVX2 vs generic implementation consistency
func TestMemchrDigitConsistency(t *testing.T) {
	// Various patterns to test
	patterns := [][]byte{
		[]byte("hello world this is a test 123"),
		[]byte("aaaaaaaaaaaaaaaaaaaaaaaaa"),
		[]byte("0123456789"),
		[]byte("a1b2c3d4e5f6g7h8i9j0"),
		[]byte("no digits here at all"),
		make([]byte, 1000),
		make([]byte, 10000),
	}

	// Fill large patterns
	for i := range patterns[5] {
		patterns[5][i] = byte(i%26 + 'a')
	}
	// Place digit in middle
	patterns[5][500] = '5'

	for i := range patterns[6] {
		patterns[6][i] = byte(i%26 + 'A')
	}
	// Place digit near end
	patterns[6][9990] = '9'

	for pi, pattern := range patterns {
		t.Run(fmt.Sprintf("pattern_%d", pi), func(t *testing.T) {
			got := MemchrDigit(pattern)
			want := refMemchrDigit(pattern)

			if got != want {
				t.Errorf("pattern[%d]: got %d, want %d", pi, got, want)
			}
		})
	}
}

// TestMemchrDigitGenericDirect tests the generic implementation directly
func TestMemchrDigitGenericDirect(t *testing.T) {
	tests := []struct {
		haystack []byte
		want     int
	}{
		{[]byte{}, -1},
		{[]byte("5"), 0},
		{[]byte("a5"), 1},
		{[]byte("abc"), -1},
		{[]byte("abc123"), 3},
		{make([]byte, 100), -1},
	}

	for i, tt := range tests {
		got := memchrDigitGeneric(tt.haystack)
		if got != tt.want {
			t.Errorf("test %d: memchrDigitGeneric(%q) = %d, want %d",
				i, tt.haystack, got, tt.want)
		}
	}
}

// BenchmarkMemchrDigit benchmarks digit search performance
func BenchmarkMemchrDigit(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a' // non-digit
		}
		haystack[size-1] = '5' // digit at end (worst case for fair comparison)

		b.Run(fmt.Sprintf("digit_at_end_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = MemchrDigit(haystack)
			}
		})
	}
}

// BenchmarkMemchrDigitNotFound benchmarks case when no digit is found
func BenchmarkMemchrDigitNotFound(b *testing.B) {
	sizes := []int{64, 1024, 65536, 1048576}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a' // all non-digits
		}

		b.Run(fmt.Sprintf("not_found_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = MemchrDigit(haystack)
			}
		})
	}
}

// BenchmarkMemchrDigitEarlyMatch benchmarks case when digit is found early
func BenchmarkMemchrDigitEarlyMatch(b *testing.B) {
	sizes := []int{1024, 65536, 1048576}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}
		haystack[64] = '7' // digit at position 64 (early match)

		b.Run(fmt.Sprintf("early_match_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = MemchrDigit(haystack)
			}
		})
	}
}

// BenchmarkMemchrDigitGeneric benchmarks the generic implementation
func BenchmarkMemchrDigitGeneric(b *testing.B) {
	sizes := []int{64, 1024, 65536}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}
		haystack[size-1] = '5'

		b.Run(fmt.Sprintf("generic_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = memchrDigitGeneric(haystack)
			}
		})
	}
}

// FuzzMemchrDigit performs fuzz testing to find edge cases
func FuzzMemchrDigit(f *testing.F) {
	// Seed corpus with diverse inputs
	f.Add([]byte("hello 123 world"))
	f.Add([]byte(""))
	f.Add([]byte("0"))
	f.Add([]byte("9"))
	f.Add([]byte("no digits"))
	f.Add(make([]byte, 1000))
	f.Add([]byte{0, 1, 2, 255, '5'})
	f.Add([]byte("///:;<=")) // bytes around digit range
	f.Add([]byte("0123456789"))

	f.Fuzz(func(t *testing.T, haystack []byte) {
		got := MemchrDigit(haystack)
		want := refMemchrDigit(haystack)

		if got != want {
			t.Errorf("MemchrDigit(%v) = %d, want %d", haystack, got, want)
		}

		// Additional verification: if found, byte must be a digit
		if got >= 0 && got < len(haystack) {
			b := haystack[got]
			if b < '0' || b > '9' {
				t.Errorf("MemchrDigit returned %d but haystack[%d]=%d is not a digit",
					got, got, b)
			}
		}
	})
}

// FuzzMemchrDigitAt performs fuzz testing for MemchrDigitAt
func FuzzMemchrDigitAt(f *testing.F) {
	f.Add([]byte("abc123def"), 0)
	f.Add([]byte("abc123def"), 3)
	f.Add([]byte("abc123def"), 6)
	f.Add([]byte(""), 0)
	f.Add(make([]byte, 100), 50)

	f.Fuzz(func(t *testing.T, haystack []byte, at int) {
		got := MemchrDigitAt(haystack, at)

		// Calculate expected result
		var want int
		if at < 0 || at >= len(haystack) {
			want = -1
		} else {
			pos := refMemchrDigit(haystack[at:])
			if pos < 0 {
				want = -1
			} else {
				want = pos + at
			}
		}

		if got != want {
			t.Errorf("MemchrDigitAt(%v, %d) = %d, want %d",
				haystack, at, got, want)
		}
	})
}
