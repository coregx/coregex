package simd

import (
	"bytes"
	"fmt"
	"testing"
)

// TestMemchrBasic tests basic functionality and edge cases
func TestMemchrBasic(t *testing.T) {
	tests := []struct {
		name     string
		haystack []byte
		needle   byte
		want     int
	}{
		// Empty and single byte cases
		{"empty_haystack", []byte{}, 'a', -1},
		{"single_match", []byte{'a'}, 'a', 0},
		{"single_no_match", []byte{'a'}, 'b', -1},

		// Position tests
		{"first_position", []byte("hello"), 'h', 0},
		{"middle_position", []byte("hello"), 'l', 2},
		{"last_position", []byte("hello"), 'o', 4},
		{"not_found", []byte("hello"), 'x', -1},

		// Multiple occurrences (should return first)
		{"multiple_returns_first", []byte("hello world"), 'o', 4},
		{"multiple_l", []byte("hello"), 'l', 2},

		// Special bytes
		{"null_byte_present", []byte{0, 1, 2, 3}, 0, 0},
		{"null_byte_absent", []byte{1, 2, 3, 4}, 0, -1},
		{"high_byte_0xff", []byte{1, 2, 255, 4}, 255, 2},
		{"all_same_find_first", []byte{5, 5, 5, 5}, 5, 0},

		// Longer strings
		{"longer_found", []byte("the quick brown fox jumps over the lazy dog"), 'q', 4},
		{"longer_not_found", []byte("the quick brown fox jumps over the lazy dog"), 'z', 37},
		{"longer_first_char", []byte("the quick brown fox jumps over the lazy dog"), 't', 0},
		{"longer_last_char", []byte("the quick brown fox jumps over the lazy dog"), 'g', 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Memchr(tt.haystack, tt.needle)
			if got != tt.want {
				t.Errorf("Memchr(%q, %q) = %d, want %d", tt.haystack, tt.needle, got, tt.want)
			}

			// Verify against stdlib
			stdGot := bytes.IndexByte(tt.haystack, tt.needle)
			if got != stdGot {
				t.Errorf("Memchr != stdlib: got %d, stdlib %d (haystack=%q, needle=%q)",
					got, stdGot, tt.haystack, tt.needle)
			}
		})
	}
}

// TestMemchrSizes tests various input sizes including boundary conditions
func TestMemchrSizes(t *testing.T) {
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
			// Fill with 'a', put 'X' at end
			for i := range haystack {
				haystack[i] = 'a'
			}
			haystack[size-1] = 'X'

			got := Memchr(haystack, 'X')
			want := size - 1
			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}

			// Verify vs stdlib
			stdGot := bytes.IndexByte(haystack, 'X')
			if got != stdGot {
				t.Errorf("size %d: mismatch with stdlib: got %d, stdlib %d", size, got, stdGot)
			}
		})

		t.Run(fmt.Sprintf("size_%d_at_start", size), func(t *testing.T) {
			haystack := make([]byte, size)
			// Fill with 'a', put 'X' at start
			for i := range haystack {
				haystack[i] = 'a'
			}
			haystack[0] = 'X'

			got := Memchr(haystack, 'X')
			want := 0
			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}

			// Verify vs stdlib
			stdGot := bytes.IndexByte(haystack, 'X')
			if got != stdGot {
				t.Errorf("size %d: mismatch with stdlib: got %d, stdlib %d", size, got, stdGot)
			}
		})

		t.Run(fmt.Sprintf("size_%d_not_found", size), func(t *testing.T) {
			haystack := make([]byte, size)
			// Fill with 'a', search for 'X'
			for i := range haystack {
				haystack[i] = 'a'
			}

			got := Memchr(haystack, 'X')
			want := -1
			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}

			// Verify vs stdlib
			stdGot := bytes.IndexByte(haystack, 'X')
			if got != stdGot {
				t.Errorf("size %d: mismatch with stdlib: got %d, stdlib %d", size, got, stdGot)
			}
		})
	}
}

// TestMemchrAlignment tests misaligned haystack starts (important for SIMD)
func TestMemchrAlignment(t *testing.T) {
	// Create a large buffer to allow different alignment offsets
	buf := make([]byte, 256)
	for i := range buf {
		buf[i] = 'a'
	}
	buf[128] = 'X' // needle in middle

	// Test slices starting at different offsets (0-31 covers AVX2 alignment)
	for offset := 0; offset < 32; offset++ {
		t.Run(fmt.Sprintf("offset_%d", offset), func(t *testing.T) {
			haystack := buf[offset:]
			got := Memchr(haystack, 'X')
			want := 128 - offset

			if got != want {
				t.Errorf("offset %d: got %d, want %d", offset, got, want)
			}

			// Verify vs stdlib
			stdGot := bytes.IndexByte(haystack, 'X')
			if got != stdGot {
				t.Errorf("offset %d: mismatch with stdlib: got %d, stdlib %d", offset, got, stdGot)
			}
		})
	}

	// Test not found with different alignments
	for offset := 0; offset < 32; offset++ {
		t.Run(fmt.Sprintf("offset_%d_not_found", offset), func(t *testing.T) {
			haystack := buf[offset : offset+64]
			got := Memchr(haystack, 'Z')
			want := -1

			if got != want {
				t.Errorf("offset %d: got %d, want %d", offset, got, want)
			}

			// Verify vs stdlib
			stdGot := bytes.IndexByte(haystack, 'Z')
			if got != stdGot {
				t.Errorf("offset %d: mismatch with stdlib: got %d, stdlib %d", offset, got, stdGot)
			}
		})
	}
}

// TestMemchrAllBytes tests all possible byte values (0-255) as needle
func TestMemchrAllBytes(t *testing.T) {
	// Create haystack with all bytes 0-255
	haystack := make([]byte, 256)
	for i := 0; i < 256; i++ {
		haystack[i] = byte(i)
	}

	// Test each byte value
	for needle := 0; needle < 256; needle++ {
		t.Run(fmt.Sprintf("needle_%d", needle), func(t *testing.T) {
			got := Memchr(haystack, byte(needle))
			want := needle

			if got != want {
				t.Errorf("needle %d: got %d, want %d", needle, got, want)
			}

			// Verify vs stdlib
			stdGot := bytes.IndexByte(haystack, byte(needle))
			if got != stdGot {
				t.Errorf("needle %d: mismatch with stdlib: got %d, stdlib %d", needle, got, stdGot)
			}
		})
	}

	// Test each byte when not present
	haystackNoZero := haystack[1:] // exclude byte 0
	t.Run("needle_0_not_present", func(t *testing.T) {
		got := Memchr(haystackNoZero, 0)
		want := -1

		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}

		// Verify vs stdlib
		stdGot := bytes.IndexByte(haystackNoZero, 0)
		if got != stdGot {
			t.Errorf("mismatch with stdlib: got %d, stdlib %d", got, stdGot)
		}
	})
}

// TestMemchr2Basic tests Memchr2 functionality
func TestMemchr2Basic(t *testing.T) {
	tests := []struct {
		name     string
		haystack []byte
		needle1  byte
		needle2  byte
		want     int
	}{
		{"empty", []byte{}, 'a', 'b', -1},
		{"first_needle_match", []byte("hello"), 'h', 'x', 0},
		{"second_needle_match", []byte("hello"), 'x', 'h', 0},
		{"both_present_first_wins", []byte("hello world"), 'o', 'w', 4},  // 'o' at 4, 'w' at 6
		{"both_present_second_wins", []byte("hello world"), 'w', 'o', 4}, // order doesn't matter, position matters
		{"neither_present", []byte("hello"), 'x', 'y', -1},
		{"same_needles", []byte("hello"), 'h', 'h', 0},
		{"longer_string", []byte("the quick brown fox"), 'q', 'f', 4}, // 'q' at 4, 'f' at 16
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Memchr2(tt.haystack, tt.needle1, tt.needle2)
			if got != tt.want {
				t.Errorf("Memchr2(%q, %q, %q) = %d, want %d",
					tt.haystack, tt.needle1, tt.needle2, got, tt.want)
			}

			// Verify: result should match whichever needle appears first
			pos1 := bytes.IndexByte(tt.haystack, tt.needle1)
			pos2 := bytes.IndexByte(tt.haystack, tt.needle2)

			var expected int
			switch {
			case pos1 == -1 && pos2 == -1:
				expected = -1
			case pos1 == -1:
				expected = pos2
			case pos2 == -1:
				expected = pos1
			case pos1 < pos2:
				expected = pos1
			default:
				expected = pos2
			}

			if got != expected {
				t.Errorf("Memchr2 logic error: got %d, expected %d (pos1=%d, pos2=%d)",
					got, expected, pos1, pos2)
			}
		})
	}
}

// TestMemchr2Sizes tests Memchr2 with various sizes
func TestMemchr2Sizes(t *testing.T) {
	sizes := []int{1, 16, 32, 64, 128, 1024, 4096}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			haystack := make([]byte, size)
			for i := range haystack {
				haystack[i] = 'a'
			}
			if size > 10 {
				haystack[5] = 'X'
				haystack[size-5] = 'Y'
			}

			got := Memchr2(haystack, 'X', 'Y')
			pos1 := bytes.IndexByte(haystack, 'X')
			pos2 := bytes.IndexByte(haystack, 'Y')

			var want int
			switch {
			case pos1 == -1 && pos2 == -1:
				want = -1
			case pos1 == -1:
				want = pos2
			case pos2 == -1:
				want = pos1
			case pos1 < pos2:
				want = pos1
			default:
				want = pos2
			}

			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}
		})
	}
}

// TestMemchr3Basic tests Memchr3 functionality
func TestMemchr3Basic(t *testing.T) {
	tests := []struct {
		name     string
		haystack []byte
		needle1  byte
		needle2  byte
		needle3  byte
		want     int
	}{
		{"empty", []byte{}, 'a', 'b', 'c', -1},
		{"first_needle", []byte("hello"), 'h', 'x', 'y', 0},
		{"second_needle", []byte("hello"), 'x', 'e', 'y', 1},
		{"third_needle", []byte("hello"), 'x', 'y', 'o', 4},
		{"all_present_first_wins", []byte("hello world"), 'o', 'w', 'h', 0}, // 'h' at 0
		{"none_present", []byte("hello"), 'x', 'y', 'z', -1},
		{"same_needles", []byte("hello"), 'h', 'h', 'h', 0},
		{"whitespace", []byte("hello world"), ' ', ',', '.', 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Memchr3(tt.haystack, tt.needle1, tt.needle2, tt.needle3)
			if got != tt.want {
				t.Errorf("Memchr3(%q, %q, %q, %q) = %d, want %d",
					tt.haystack, tt.needle1, tt.needle2, tt.needle3, got, tt.want)
			}

			// Verify: result should match whichever needle appears first
			pos1 := bytes.IndexByte(tt.haystack, tt.needle1)
			pos2 := bytes.IndexByte(tt.haystack, tt.needle2)
			pos3 := bytes.IndexByte(tt.haystack, tt.needle3)

			positions := []int{}
			if pos1 != -1 {
				positions = append(positions, pos1)
			}
			if pos2 != -1 {
				positions = append(positions, pos2)
			}
			if pos3 != -1 {
				positions = append(positions, pos3)
			}

			var expected int
			if len(positions) == 0 {
				expected = -1
			} else {
				expected = positions[0]
				for _, p := range positions {
					if p < expected {
						expected = p
					}
				}
			}

			if got != expected {
				t.Errorf("Memchr3 logic error: got %d, expected %d (pos1=%d, pos2=%d, pos3=%d)",
					got, expected, pos1, pos2, pos3)
			}
		})
	}
}

// TestMemchr3Sizes tests Memchr3 with various sizes
func TestMemchr3Sizes(t *testing.T) {
	sizes := []int{1, 16, 32, 64, 128, 1024, 4096}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			haystack := make([]byte, size)
			for i := range haystack {
				haystack[i] = 'a'
			}
			if size > 20 {
				haystack[5] = 'X'
				haystack[10] = 'Y'
				haystack[size-5] = 'Z'
			}

			got := Memchr3(haystack, 'X', 'Y', 'Z')
			pos1 := bytes.IndexByte(haystack, 'X')
			pos2 := bytes.IndexByte(haystack, 'Y')
			pos3 := bytes.IndexByte(haystack, 'Z')

			positions := []int{}
			if pos1 != -1 {
				positions = append(positions, pos1)
			}
			if pos2 != -1 {
				positions = append(positions, pos2)
			}
			if pos3 != -1 {
				positions = append(positions, pos3)
			}

			var want int
			if len(positions) == 0 {
				want = -1
			} else {
				want = positions[0]
				for _, p := range positions {
					if p < want {
						want = p
					}
				}
			}

			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}
		})
	}
}

// BenchmarkMemchr benchmarks Memchr against stdlib bytes.IndexByte
func BenchmarkMemchr(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{16, 32, 64, 128, 256, 512, 1024, 4096, 16384, 65536, 262144, 1048576}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}
		haystack[size-1] = 'X' // needle at end (worst case for fair comparison)

		b.Run(fmt.Sprintf("memchr_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = Memchr(haystack, 'X')
			}
		})

		b.Run(fmt.Sprintf("stdlib_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = bytes.IndexByte(haystack, 'X')
			}
		})
	}
}

// BenchmarkMemchrNotFound benchmarks case when needle is not found
func BenchmarkMemchrNotFound(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{64, 1024, 65536, 1048576}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}

		b.Run(fmt.Sprintf("memchr_not_found_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = Memchr(haystack, 'X')
			}
		})

		b.Run(fmt.Sprintf("stdlib_not_found_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = bytes.IndexByte(haystack, 'X')
			}
		})
	}
}

// BenchmarkMemchrEarlyMatch benchmarks case when needle is found early
func BenchmarkMemchrEarlyMatch(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{1024, 65536, 1048576}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}
		haystack[64] = 'X' // needle at position 64 (early match)

		b.Run(fmt.Sprintf("memchr_early_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = Memchr(haystack, 'X')
			}
		})

		b.Run(fmt.Sprintf("stdlib_early_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = bytes.IndexByte(haystack, 'X')
			}
		})
	}
}

// BenchmarkMemchr2 benchmarks Memchr2
func BenchmarkMemchr2(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{64, 1024, 65536, 1048576}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}
		haystack[size-1] = 'X' // needle at end

		b.Run(fmt.Sprintf("memchr2_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = Memchr2(haystack, 'X', 'Y')
			}
		})
	}
}

// BenchmarkMemchr3 benchmarks Memchr3
func BenchmarkMemchr3(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{64, 1024, 65536, 1048576}

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}
		haystack[size-1] = 'X' // needle at end

		b.Run(fmt.Sprintf("memchr3_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = Memchr3(haystack, 'X', 'Y', 'Z')
			}
		})
	}
}

// FuzzMemchr performs fuzz testing to find edge cases
func FuzzMemchr(f *testing.F) {
	// Seed corpus with diverse inputs
	f.Add([]byte("hello world"), byte('o'))
	f.Add([]byte(""), byte('x'))
	f.Add(make([]byte, 1000), byte(0))
	f.Add([]byte{0, 1, 2, 3, 255}, byte(255))

	f.Fuzz(func(t *testing.T, haystack []byte, needle byte) {
		got := Memchr(haystack, needle)
		want := bytes.IndexByte(haystack, needle)

		if got != want {
			t.Errorf("Memchr(%v, %v) = %d, want %d", haystack, needle, got, want)
		}
	})
}

// FuzzMemchr2 performs fuzz testing for Memchr2
func FuzzMemchr2(f *testing.F) {
	// Seed corpus
	f.Add([]byte("hello world"), byte('o'), byte('w'))
	f.Add([]byte(""), byte('x'), byte('y'))
	f.Add(make([]byte, 100), byte(0), byte(1))

	f.Fuzz(func(t *testing.T, haystack []byte, needle1, needle2 byte) {
		got := Memchr2(haystack, needle1, needle2)

		// Verify: result should match whichever needle appears first
		pos1 := bytes.IndexByte(haystack, needle1)
		pos2 := bytes.IndexByte(haystack, needle2)

		var expected int
		switch {
		case pos1 == -1 && pos2 == -1:
			expected = -1
		case pos1 == -1:
			expected = pos2
		case pos2 == -1:
			expected = pos1
		case pos1 < pos2:
			expected = pos1
		default:
			expected = pos2
		}

		if got != expected {
			t.Errorf("Memchr2(%v, %v, %v) = %d, want %d (pos1=%d, pos2=%d)",
				haystack, needle1, needle2, got, expected, pos1, pos2)
		}
	})
}

// FuzzMemchr3 performs fuzz testing for Memchr3
func FuzzMemchr3(f *testing.F) {
	// Seed corpus
	f.Add([]byte("hello world"), byte('o'), byte('w'), byte('h'))
	f.Add([]byte(""), byte('x'), byte('y'), byte('z'))
	f.Add(make([]byte, 100), byte(0), byte(1), byte(2))

	f.Fuzz(func(t *testing.T, haystack []byte, needle1, needle2, needle3 byte) {
		got := Memchr3(haystack, needle1, needle2, needle3)

		// Verify: result should match whichever needle appears first
		pos1 := bytes.IndexByte(haystack, needle1)
		pos2 := bytes.IndexByte(haystack, needle2)
		pos3 := bytes.IndexByte(haystack, needle3)

		positions := []int{}
		if pos1 != -1 {
			positions = append(positions, pos1)
		}
		if pos2 != -1 {
			positions = append(positions, pos2)
		}
		if pos3 != -1 {
			positions = append(positions, pos3)
		}

		var expected int
		if len(positions) == 0 {
			expected = -1
		} else {
			expected = positions[0]
			for _, p := range positions {
				if p < expected {
					expected = p
				}
			}
		}

		if got != expected {
			t.Errorf("Memchr3(%v, %v, %v, %v) = %d, want %d (pos1=%d, pos2=%d, pos3=%d)",
				haystack, needle1, needle2, needle3, got, expected, pos1, pos2, pos3)
		}
	})
}
