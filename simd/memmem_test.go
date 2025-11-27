package simd

import (
	"bytes"
	"fmt"
	"testing"
)

// TestMemmemBasic tests basic functionality and edge cases
func TestMemmemBasic(t *testing.T) {
	tests := []struct {
		name     string
		haystack []byte
		needle   []byte
		want     int
	}{
		// Empty cases
		{"empty_needle", []byte("hello"), []byte{}, 0},
		{"empty_haystack", []byte{}, []byte("x"), -1},
		{"both_empty", []byte{}, []byte{}, 0},

		// Single byte (delegates to Memchr)
		{"single_found", []byte("hello"), []byte("e"), 1},
		{"single_not_found", []byte("hello"), []byte("x"), -1},

		// Position tests
		{"at_start", []byte("hello world"), []byte("hello"), 0},
		{"at_end", []byte("hello world"), []byte("world"), 6},
		{"in_middle", []byte("hello world"), []byte("lo wo"), 3},
		{"not_found", []byte("hello world"), []byte("xyz"), -1},

		// Exact match
		{"exact_match", []byte("hello"), []byte("hello"), 0},

		// Needle longer than haystack
		{"needle_too_long", []byte("hi"), []byte("hello"), -1},

		// Multiple occurrences (should return first)
		{"multiple_returns_first", []byte("hello hello"), []byte("hello"), 0},
		{"overlapping_pattern", []byte("aaaa"), []byte("aa"), 0},

		// Special characters
		{"with_null_bytes", []byte{0, 1, 2, 3, 4}, []byte{2, 3}, 2},
		{"high_bytes", []byte{1, 2, 255, 254, 5}, []byte{255, 254}, 2},

		// Real-world examples
		{"http_method", []byte("GET /index.html HTTP/1.1"), []byte("HTTP"), 16},
		{"json_key", []byte(`{"name":"John","age":30}`), []byte(`"age"`), 15},
		{"url_protocol", []byte("https://example.com/path"), []byte("://"), 5},

		// Repeated characters
		{"repeated_in_needle", []byte("hello"), []byte("ll"), 2},
		{"repeated_in_haystack", []byte("aaaaabaaaa"), []byte("ab"), 4},
		{"all_same", []byte("aaaa"), []byte("aa"), 0},

		// Longer patterns
		{"longer_found", []byte("the quick brown fox jumps"), []byte("brown fox"), 10},
		{"longer_not_found", []byte("the quick brown fox jumps"), []byte("lazy dog"), -1},

		// Boundary cases
		{"needle_at_last_position", []byte("hello!"), []byte("!"), 5},
		{"two_byte_needle", []byte("hello world"), []byte("wo"), 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Memmem(tt.haystack, tt.needle)
			if got != tt.want {
				t.Errorf("Memmem(%q, %q) = %d, want %d", tt.haystack, tt.needle, got, tt.want)
			}

			// Verify against stdlib
			stdGot := bytes.Index(tt.haystack, tt.needle)
			if got != stdGot {
				t.Errorf("Memmem != stdlib: got %d, stdlib %d (haystack=%q, needle=%q)",
					got, stdGot, tt.haystack, tt.needle)
			}
		})
	}
}

// TestMemmemSizes tests various needle sizes (critical for performance branches)
func TestMemmemSizes(t *testing.T) {
	// Test different needle sizes: 2, 4, 8, 16, 32, 64, 128 bytes
	sizes := []int{2, 4, 8, 16, 32, 64, 128}

	for _, needleSize := range sizes {
		t.Run(fmt.Sprintf("needle_size_%d_found_at_end", needleSize), func(t *testing.T) {
			// Create haystack with pattern at end
			haystackSize := 1024
			haystack := make([]byte, haystackSize)
			for i := range haystack {
				haystack[i] = 'a'
			}

			// Create needle with unique marker at end
			needle := make([]byte, needleSize)
			for i := range needle {
				if i == needleSize-1 {
					needle[i] = 'X' // Rare byte at end
				} else {
					needle[i] = 'a'
				}
			}

			// Place needle at end of haystack
			copy(haystack[haystackSize-needleSize:], needle)

			got := Memmem(haystack, needle)
			want := haystackSize - needleSize

			if got != want {
				t.Errorf("size %d: got %d, want %d", needleSize, got, want)
			}

			// Verify vs stdlib
			stdGot := bytes.Index(haystack, needle)
			if got != stdGot {
				t.Errorf("size %d: mismatch with stdlib: got %d, stdlib %d", needleSize, got, stdGot)
			}
		})

		t.Run(fmt.Sprintf("needle_size_%d_found_at_start", needleSize), func(t *testing.T) {
			haystackSize := 1024
			haystack := make([]byte, haystackSize)
			for i := range haystack {
				haystack[i] = 'a'
			}

			needle := make([]byte, needleSize)
			for i := range needle {
				if i == 0 {
					needle[i] = 'X' // Rare byte at start
				} else {
					needle[i] = 'a'
				}
			}

			copy(haystack[0:], needle)

			got := Memmem(haystack, needle)
			want := 0

			if got != want {
				t.Errorf("size %d: got %d, want %d", needleSize, got, want)
			}

			stdGot := bytes.Index(haystack, needle)
			if got != stdGot {
				t.Errorf("size %d: mismatch with stdlib: got %d, stdlib %d", needleSize, got, stdGot)
			}
		})

		t.Run(fmt.Sprintf("needle_size_%d_not_found", needleSize), func(t *testing.T) {
			haystackSize := 1024
			haystack := make([]byte, haystackSize)
			for i := range haystack {
				haystack[i] = 'a'
			}

			needle := make([]byte, needleSize)
			for i := range needle {
				needle[i] = 'X' // All different from haystack
			}

			got := Memmem(haystack, needle)
			want := -1

			if got != want {
				t.Errorf("size %d: got %d, want %d", needleSize, got, want)
			}

			stdGot := bytes.Index(haystack, needle)
			if got != stdGot {
				t.Errorf("size %d: mismatch with stdlib: got %d, stdlib %d", needleSize, got, stdGot)
			}
		})
	}
}

// TestMemmemPositions tests needle found at various positions in haystack
func TestMemmemPositions(t *testing.T) {
	haystackSize := 1024
	needleSize := 8
	needle := []byte("PATTERN!")

	positions := []int{0, 1, 7, 15, 31, 32, 33, 63, 64, 127, 128, 256, 512, 1016}

	for _, pos := range positions {
		if pos+needleSize > haystackSize {
			continue
		}

		t.Run(fmt.Sprintf("position_%d", pos), func(t *testing.T) {
			haystack := make([]byte, haystackSize)
			for i := range haystack {
				haystack[i] = 'a'
			}
			copy(haystack[pos:], needle)

			got := Memmem(haystack, needle)
			want := pos

			if got != want {
				t.Errorf("position %d: got %d, want %d", pos, got, want)
			}

			stdGot := bytes.Index(haystack, needle)
			if got != stdGot {
				t.Errorf("position %d: mismatch with stdlib: got %d, stdlib %d", pos, got, stdGot)
			}
		})
	}
}

// TestMemmemRepeated tests patterns with repeated characters
func TestMemmemRepeated(t *testing.T) {
	tests := []struct {
		name     string
		haystack string
		needle   string
		want     int
	}{
		{"simple_repeat", "aaaa", "aa", 0},
		{"repeat_with_marker", "aaaaaabaaaa", "aab", 4},
		{"all_same_longer", "aaaaaaaaaa", "aaaaa", 0},
		{"dna_pattern", "ATATATATATAT", "ATAT", 0},
		{"number_repeat", "1111211111", "112", 2},
		{"space_repeat", "    x    ", "   x", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			haystack := []byte(tt.haystack)
			needle := []byte(tt.needle)

			got := Memmem(haystack, needle)
			if got != tt.want {
				t.Errorf("Memmem(%q, %q) = %d, want %d", haystack, needle, got, tt.want)
			}

			stdGot := bytes.Index(haystack, needle)
			if got != stdGot {
				t.Errorf("mismatch with stdlib: got %d, stdlib %d", got, stdGot)
			}
		})
	}
}

// TestMemmemLarge tests large haystack sizes (important for SIMD performance)
func TestMemmemLarge(t *testing.T) {
	sizes := []int{1024, 4096, 16384, 65536, 262144, 1048576}
	needle := []byte("FIND_ME_NOW!")

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d_found", size), func(t *testing.T) {
			haystack := make([]byte, size)
			for i := range haystack {
				haystack[i] = byte('a' + (i % 26))
			}

			// Place needle at 75% position
			pos := (size * 3) / 4
			if pos+len(needle) <= size {
				copy(haystack[pos:], needle)
			} else {
				pos = size - len(needle)
				copy(haystack[pos:], needle)
			}

			got := Memmem(haystack, needle)
			want := pos

			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}

			stdGot := bytes.Index(haystack, needle)
			if got != stdGot {
				t.Errorf("size %d: mismatch with stdlib: got %d, stdlib %d", size, got, stdGot)
			}
		})

		t.Run(fmt.Sprintf("size_%d_not_found", size), func(t *testing.T) {
			haystack := make([]byte, size)
			for i := range haystack {
				haystack[i] = byte('a' + (i % 26))
			}

			// Needle with characters not in haystack
			notFoundNeedle := []byte("XYZ_NOT_FOUND")

			got := Memmem(haystack, notFoundNeedle)
			want := -1

			if got != want {
				t.Errorf("size %d: got %d, want %d", size, got, want)
			}

			stdGot := bytes.Index(haystack, notFoundNeedle)
			if got != stdGot {
				t.Errorf("size %d: mismatch with stdlib: got %d, stdlib %d", size, got, stdGot)
			}
		})
	}
}

// TestMemmemAlignment tests different alignment offsets (important for SIMD)
func TestMemmemAlignment(t *testing.T) {
	needle := []byte("PATTERN")
	baseHaystack := make([]byte, 256)
	for i := range baseHaystack {
		baseHaystack[i] = 'a'
	}
	copy(baseHaystack[128:], needle)

	// Test different alignment offsets (0-31 for AVX2)
	for offset := 0; offset < 32; offset++ {
		t.Run(fmt.Sprintf("offset_%d", offset), func(t *testing.T) {
			haystack := baseHaystack[offset:]
			got := Memmem(haystack, needle)
			want := 128 - offset

			if got != want {
				t.Errorf("offset %d: got %d, want %d", offset, got, want)
			}

			stdGot := bytes.Index(haystack, needle)
			if got != stdGot {
				t.Errorf("offset %d: mismatch with stdlib: got %d, stdlib %d", offset, got, stdGot)
			}
		})
	}
}

// TestMemmemBoundaries tests boundary conditions
func TestMemmemBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		haystack []byte
		needle   []byte
		want     int
	}{
		// Exact size matches
		{"exact_size_2", []byte("ab"), []byte("ab"), 0},
		{"exact_size_8", []byte("12345678"), []byte("12345678"), 0},
		{"exact_size_32", make([]byte, 32), make([]byte, 32), 0},

		// Off-by-one scenarios
		{"needle_one_longer", []byte("hello"), []byte("helloo"), -1},
		{"haystack_one_longer", []byte("hello!"), []byte("hello"), 0},

		// At exact boundaries
		{"at_32_byte_boundary", append(make([]byte, 32), []byte("XX")...), []byte("XX"), 32},
		{"at_64_byte_boundary", append(make([]byte, 64), []byte("XX")...), []byte("XX"), 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Memmem(tt.haystack, tt.needle)
			if got != tt.want {
				t.Errorf("Memmem(%q, %q) = %d, want %d", tt.haystack, tt.needle, got, tt.want)
			}

			stdGot := bytes.Index(tt.haystack, tt.needle)
			if got != stdGot {
				t.Errorf("mismatch with stdlib: got %d, stdlib %d", got, stdGot)
			}
		})
	}
}

// BenchmarkMemmem benchmarks Memmem against stdlib bytes.Index
func BenchmarkMemmem(b *testing.B) {
	b.ReportAllocs()

	// Test different haystack sizes
	haystackSizes := []int{1024, 4096, 16384, 65536, 262144, 1048576}

	// Test different needle sizes
	needleSizes := []int{2, 4, 8, 16, 32, 64}

	for _, hSize := range haystackSizes {
		for _, nSize := range needleSizes {
			if nSize > hSize {
				continue
			}

			// Create haystack
			haystack := make([]byte, hSize)
			for i := range haystack {
				haystack[i] = 'a'
			}

			// Create needle with rare byte at end
			needle := make([]byte, nSize)
			for i := range needle {
				if i == nSize-1 {
					needle[i] = 'X'
				} else {
					needle[i] = 'a'
				}
			}

			// Place needle at end (worst case)
			copy(haystack[hSize-nSize:], needle)

			b.Run(fmt.Sprintf("memmem_h%d_n%d", hSize, nSize), func(b *testing.B) {
				b.SetBytes(int64(hSize))
				for i := 0; i < b.N; i++ {
					_ = Memmem(haystack, needle)
				}
			})

			b.Run(fmt.Sprintf("stdlib_h%d_n%d", hSize, nSize), func(b *testing.B) {
				b.SetBytes(int64(hSize))
				for i := 0; i < b.N; i++ {
					_ = bytes.Index(haystack, needle)
				}
			})
		}
	}
}

// BenchmarkMemmemNotFound benchmarks case when needle is not found
func BenchmarkMemmemNotFound(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{4096, 65536, 1048576}
	needle := []byte("NOT_FOUND_PATTERN")

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}

		b.Run(fmt.Sprintf("memmem_not_found_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = Memmem(haystack, needle)
			}
		})

		b.Run(fmt.Sprintf("stdlib_not_found_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = bytes.Index(haystack, needle)
			}
		})
	}
}

// BenchmarkMemmemEarlyMatch benchmarks case when needle is found early
func BenchmarkMemmemEarlyMatch(b *testing.B) {
	b.ReportAllocs()

	sizes := []int{4096, 65536, 1048576}
	needle := []byte("EARLY_MATCH")

	for _, size := range sizes {
		haystack := make([]byte, size)
		for i := range haystack {
			haystack[i] = 'a'
		}

		// Place needle at position 100 (early match)
		copy(haystack[100:], needle)

		b.Run(fmt.Sprintf("memmem_early_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = Memmem(haystack, needle)
			}
		})

		b.Run(fmt.Sprintf("stdlib_early_%d", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = bytes.Index(haystack, needle)
			}
		})
	}
}

// BenchmarkMemmemShortNeedle benchmarks short needles (2-8 bytes)
func BenchmarkMemmemShortNeedle(b *testing.B) {
	b.ReportAllocs()

	haystackSize := 65536
	haystack := make([]byte, haystackSize)
	for i := range haystack {
		haystack[i] = byte('a' + (i % 26))
	}

	needles := [][]byte{
		[]byte("ab"),
		[]byte("abcd"),
		[]byte("pattern"),
		[]byte("findme!!"),
	}

	for _, needle := range needles {
		copy(haystack[haystackSize-len(needle):], needle)

		b.Run(fmt.Sprintf("memmem_len_%d", len(needle)), func(b *testing.B) {
			b.SetBytes(int64(haystackSize))
			for i := 0; i < b.N; i++ {
				_ = Memmem(haystack, needle)
			}
		})

		b.Run(fmt.Sprintf("stdlib_len_%d", len(needle)), func(b *testing.B) {
			b.SetBytes(int64(haystackSize))
			for i := 0; i < b.N; i++ {
				_ = bytes.Index(haystack, needle)
			}
		})
	}
}

// FuzzMemmem performs fuzz testing to find edge cases
func FuzzMemmem(f *testing.F) {
	// Seed corpus with diverse inputs
	f.Add([]byte("hello world"), []byte("world"))
	f.Add([]byte(""), []byte("x"))
	f.Add([]byte("x"), []byte(""))
	f.Add([]byte("aaaa"), []byte("aa"))
	f.Add(make([]byte, 100), []byte("pattern"))
	f.Add([]byte{0, 1, 2, 3, 255}, []byte{2, 3})

	f.Fuzz(func(t *testing.T, haystack, needle []byte) {
		got := Memmem(haystack, needle)
		want := bytes.Index(haystack, needle)

		if got != want {
			t.Errorf("Memmem(%v, %v) = %d, want %d", haystack, needle, got, want)
		}
	})
}
