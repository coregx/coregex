package simd

import (
	"bytes"
	"testing"
)

func TestMemchrWord(t *testing.T) {
	tests := []struct {
		name     string
		haystack string
		want     int
	}{
		{"empty", "", -1},
		{"first byte letter", "hello", 0},
		{"first byte digit", "123abc", 0},
		{"first byte underscore", "_var", 0},
		{"space then word", " hello", 1},
		{"multiple spaces", "   abc", 3},
		{"no word chars", "   !@#$%", -1},
		{"word at end", "!!!a", 3},
		{"mixed", "!@# abc123", 4},
		{"all ranges", "A Z a z 0 9 _", 0},
		{"upper case only", "HELLO", 0},
		{"lower case only", "world", 0},
		{"digits only", "12345", 0},
		{"special then upper", "...ABC", 3},
		{"special then lower", "...abc", 3},
		{"special then digit", "...123", 3},
		{"special then underscore", "..._", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MemchrWord([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("MemchrWord(%q) = %d, want %d", tt.haystack, got, tt.want)
			}
		})
	}
}

func TestMemchrWord_LargeInput(t *testing.T) {
	// Test with input larger than 32 bytes to exercise SIMD path
	tests := []struct {
		name   string
		prefix string // non-word chars
		word   string // word char(s)
		want   int
	}{
		{"word at position 0", "", "hello", 0},
		{"word at position 32", string(bytes.Repeat([]byte{' '}, 32)), "hello", 32},
		{"word at position 64", string(bytes.Repeat([]byte{' '}, 64)), "x", 64},
		{"word at position 100", string(bytes.Repeat([]byte{'-'}, 100)), "A", 100},
		{"no word in 1000 bytes", string(bytes.Repeat([]byte{'!'}, 1000)), "", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			haystack := []byte(tt.prefix + tt.word)
			got := MemchrWord(haystack)
			if got != tt.want {
				t.Errorf("MemchrWord() = %d, want %d (len=%d)", got, tt.want, len(haystack))
			}
		})
	}
}

func TestMemchrNotWord(t *testing.T) {
	tests := []struct {
		name     string
		haystack string
		want     int
	}{
		{"empty", "", -1},
		{"first byte space", " hello", 0},
		{"first byte special", "!abc", 0},
		{"word then space", "hello world", 5},
		{"all word chars", "abc123_XYZ", -1},
		{"non-word at end", "abc!", 3},
		{"mixed", "foo bar", 3},
		{"all uppercase", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", -1},
		{"all lowercase", "abcdefghijklmnopqrstuvwxyz", -1},
		{"all digits", "0123456789", -1},
		{"underscore only", "____", -1},
		{"word then newline", "hello\nworld", 5},
		{"word then tab", "foo\tbar", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MemchrNotWord([]byte(tt.haystack))
			if got != tt.want {
				t.Errorf("MemchrNotWord(%q) = %d, want %d", tt.haystack, got, tt.want)
			}
		})
	}
}

func TestMemchrNotWord_LargeInput(t *testing.T) {
	tests := []struct {
		name     string
		wordPart string // word chars
		nonWord  string // non-word char(s)
		want     int
	}{
		{"non-word at position 0", "", " rest", 0},
		{"non-word at position 32", string(bytes.Repeat([]byte{'a'}, 32)), " ", 32},
		{"non-word at position 64", string(bytes.Repeat([]byte{'X'}, 64)), "!", 64},
		{"non-word at position 100", string(bytes.Repeat([]byte{'_'}, 100)), "\n", 100},
		{"all word chars 1000", string(bytes.Repeat([]byte{'z'}, 1000)), "", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			haystack := []byte(tt.wordPart + tt.nonWord)
			got := MemchrNotWord(haystack)
			if got != tt.want {
				t.Errorf("MemchrNotWord() = %d, want %d (len=%d)", got, tt.want, len(haystack))
			}
		})
	}
}

func TestMemchrWord_AllWordChars(t *testing.T) {
	// Verify all expected word characters are found
	wordChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_"

	for i, c := range []byte(wordChars) {
		haystack := []byte{' ', ' ', ' ', c}
		got := MemchrWord(haystack)
		if got != 3 {
			t.Errorf("MemchrWord with char %q (0x%02x) = %d, want 3", c, c, got)
		}
		_ = i // silence unused
	}
}

func TestMemchrNotWord_AllNonWordChars(t *testing.T) {
	// Verify non-word characters are correctly identified
	nonWordChars := " \t\n\r!@#$%^&*()-+=[]{}|;':\",./<>?"

	for _, c := range []byte(nonWordChars) {
		haystack := []byte{'a', 'b', 'c', c}
		got := MemchrNotWord(haystack)
		if got != 3 {
			t.Errorf("MemchrNotWord with char %q (0x%02x) = %d, want 3", c, c, got)
		}
	}
}

func TestMemchrInTable(t *testing.T) {
	// Create a custom table (vowels only)
	var vowels [256]bool
	for _, c := range []byte("aeiouAEIOU") {
		vowels[c] = true
	}

	tests := []struct {
		name     string
		haystack string
		want     int
	}{
		{"empty", "", -1},
		{"first is vowel", "apple", 0},
		{"vowel in middle", "xyz_a_xyz", 4},
		{"no vowels", "rhythm", -1},
		{"upper vowel", "XYZ_A", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MemchrInTable([]byte(tt.haystack), &vowels)
			if got != tt.want {
				t.Errorf("MemchrInTable(%q) = %d, want %d", tt.haystack, got, tt.want)
			}
		})
	}
}

func TestMemchrNotInTable(t *testing.T) {
	// Create a custom table (vowels only)
	var vowels [256]bool
	for _, c := range []byte("aeiouAEIOU") {
		vowels[c] = true
	}

	tests := []struct {
		name     string
		haystack string
		want     int
	}{
		{"empty", "", -1},
		{"first is consonant", "hello", 0}, // 'h' is not a vowel
		{"all vowels", "aeiou", -1},
		{"vowels then consonant", "aeioub", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MemchrNotInTable([]byte(tt.haystack), &vowels)
			if got != tt.want {
				t.Errorf("MemchrNotInTable(%q) = %d, want %d", tt.haystack, got, tt.want)
			}
		})
	}
}

// Benchmarks

func BenchmarkMemchrWord(b *testing.B) {
	sizes := []int{32, 64, 256, 1024, 4096}

	for _, size := range sizes {
		// Haystack with word char at the end
		haystack := bytes.Repeat([]byte{' '}, size-1)
		haystack = append(haystack, 'a')

		b.Run(formatSize(size)+"_end", func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				MemchrWord(haystack)
			}
		})

		// Haystack with word char at the beginning
		haystack2 := make([]byte, size)
		haystack2[0] = 'a'
		for i := 1; i < size; i++ {
			haystack2[i] = ' '
		}

		b.Run(formatSize(size)+"_start", func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				MemchrWord(haystack2)
			}
		})
	}
}

func BenchmarkMemchrNotWord(b *testing.B) {
	sizes := []int{32, 64, 256, 1024, 4096}

	for _, size := range sizes {
		// Haystack with non-word char at the end
		haystack := bytes.Repeat([]byte{'a'}, size-1)
		haystack = append(haystack, ' ')

		b.Run(formatSize(size)+"_end", func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				MemchrNotWord(haystack)
			}
		})

		// Haystack with non-word char at the beginning
		haystack2 := make([]byte, size)
		haystack2[0] = ' '
		for i := 1; i < size; i++ {
			haystack2[i] = 'a'
		}

		b.Run(formatSize(size)+"_start", func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				MemchrNotWord(haystack2)
			}
		})
	}
}

func BenchmarkMemchrWord_vsGeneric(b *testing.B) {
	// Compare SIMD vs generic implementation
	size := 4096
	haystack := bytes.Repeat([]byte{' '}, size-1)
	haystack = append(haystack, 'a')

	b.Run("SIMD", func(b *testing.B) {
		b.SetBytes(int64(size))
		for i := 0; i < b.N; i++ {
			MemchrWord(haystack)
		}
	})

	b.Run("Generic", func(b *testing.B) {
		b.SetBytes(int64(size))
		for i := 0; i < b.N; i++ {
			memchrWordGeneric(haystack)
		}
	})
}

func formatSize(n int) string {
	if n >= 1024 {
		return string(rune('0'+n/1024)) + "KB"
	}
	return string(rune('0'+n/100)) + "00B"
}
