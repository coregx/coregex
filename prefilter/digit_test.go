package prefilter

import (
	"testing"
)

func TestDigitPrefilter_Find(t *testing.T) {
	pf := NewDigitPrefilter()

	tests := []struct {
		name     string
		haystack string
		at       int
		want     int
	}{
		// Basic tests
		{"empty haystack", "", 0, -1},
		{"no digits", "hello world", 0, -1},
		{"single digit at start", "1abc", 0, 0},
		{"single digit in middle", "abc1def", 0, 3},
		{"single digit at end", "abc1", 0, 3},
		{"all digits", "12345", 0, 0},

		// Starting position tests
		{"start at digit", "abc123", 3, 3},
		{"start after first digit", "abc123", 4, 4},
		{"start at last digit", "abc123", 5, 5},
		{"start past last digit", "abc123", 6, -1},
		{"start out of bounds", "abc123", 10, -1},

		// IP address patterns
		{"IP at start", "192.168.1.1", 0, 0},
		{"IP with prefix", "host 192.168.1.1", 0, 5},
		{"IP with prefix search from 5", "host 192.168.1.1", 5, 5},
		{"IP with prefix search from 6", "host 192.168.1.1", 6, 6},

		// Multiple digit sequences
		{"multiple sequences", "a1b2c3", 0, 1},
		{"multiple sequences skip first", "a1b2c3", 2, 3},
		{"multiple sequences skip two", "a1b2c3", 4, 5},

		// Edge cases
		{"negative start", "123", -1, -1},
		{"digit '0'", "a0b", 0, 1},
		{"digit '9'", "a9b", 0, 1},
		{"all digit chars", "0123456789", 0, 0},
		{"non-ascii", "hello\x80world1", 0, 11},

		// Boundary characters
		{"char before '0'", "a/b1c", 0, 3},     // '/' is 0x2F, just before '0'
		{"char after '9'", "a:b1c", 0, 3},      // ':' is 0x3A, just after '9'
		{"mixed boundary chars", "a/:1", 0, 3}, // '/' and ':' should be skipped
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pf.Find([]byte(tc.haystack), tc.at)
			if got != tc.want {
				t.Errorf("Find(%q, %d) = %d, want %d", tc.haystack, tc.at, got, tc.want)
			}
		})
	}
}

func TestDigitPrefilter_IsComplete(t *testing.T) {
	pf := NewDigitPrefilter()
	if pf.IsComplete() {
		t.Error("IsComplete() = true, want false")
	}
}

func TestDigitPrefilter_LiteralLen(t *testing.T) {
	pf := NewDigitPrefilter()
	if got := pf.LiteralLen(); got != 0 {
		t.Errorf("LiteralLen() = %d, want 0", got)
	}
}

func TestDigitPrefilter_HeapBytes(t *testing.T) {
	pf := NewDigitPrefilter()
	if got := pf.HeapBytes(); got != 0 {
		t.Errorf("HeapBytes() = %d, want 0", got)
	}
}

func TestDigitPrefilter_ImplementsPrefilter(t *testing.T) {
	// Compile-time check that DigitPrefilter implements Prefilter interface
	var _ Prefilter = (*DigitPrefilter)(nil)
}

func TestDigitPrefilter_LargeInput(t *testing.T) {
	pf := NewDigitPrefilter()

	// Create a large haystack with a digit near the end
	size := 10000
	haystack := make([]byte, size)
	for i := 0; i < size-10; i++ {
		haystack[i] = 'x'
	}
	haystack[size-10] = '5' // Digit near end
	for i := size - 9; i < size; i++ {
		haystack[i] = 'x'
	}

	got := pf.Find(haystack, 0)
	want := size - 10
	if got != want {
		t.Errorf("Find on large input: got %d, want %d", got, want)
	}

	// Search from position after the digit
	got = pf.Find(haystack, size-9)
	if got != -1 {
		t.Errorf("Find after digit: got %d, want -1", got)
	}
}

func TestDigitPrefilter_AllDigitPositions(t *testing.T) {
	pf := NewDigitPrefilter()
	haystack := []byte("a1b2c3d4e5f6g7h8i9j0k")

	// Find all digit positions by iterating
	var positions []int
	at := 0
	for {
		pos := pf.Find(haystack, at)
		if pos == -1 {
			break
		}
		positions = append(positions, pos)
		at = pos + 1
	}

	// Verify we found all 10 digits
	want := []int{1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	if len(positions) != len(want) {
		t.Fatalf("Found %d digits, want %d", len(positions), len(want))
	}
	for i, pos := range positions {
		if pos != want[i] {
			t.Errorf("Position %d: got %d, want %d", i, pos, want[i])
		}
	}
}

// Benchmark tests
func BenchmarkDigitPrefilter_Find_NoDigits(b *testing.B) {
	pf := NewDigitPrefilter()
	// Create haystack with no digits
	haystack := make([]byte, 4096)
	for i := range haystack {
		haystack[i] = 'a' + byte(i%26)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(haystack)))
	for i := 0; i < b.N; i++ {
		pf.Find(haystack, 0)
	}
}

func BenchmarkDigitPrefilter_Find_DigitAtEnd(b *testing.B) {
	pf := NewDigitPrefilter()
	// Create haystack with digit at end
	haystack := make([]byte, 4096)
	for i := range haystack {
		haystack[i] = 'a' + byte(i%26)
	}
	haystack[len(haystack)-1] = '5'

	b.ResetTimer()
	b.SetBytes(int64(len(haystack)))
	for i := 0; i < b.N; i++ {
		pf.Find(haystack, 0)
	}
}

func BenchmarkDigitPrefilter_Find_DigitAtStart(b *testing.B) {
	pf := NewDigitPrefilter()
	// Create haystack with digit at start
	haystack := make([]byte, 4096)
	haystack[0] = '1'
	for i := 1; i < len(haystack); i++ {
		haystack[i] = 'a' + byte(i%26)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(haystack)))
	for i := 0; i < b.N; i++ {
		pf.Find(haystack, 0)
	}
}

func BenchmarkDigitPrefilter_Find_IPPattern(b *testing.B) {
	pf := NewDigitPrefilter()
	// Simulate real-world log with IP addresses
	haystack := []byte("Server connection from client at 192.168.1.1 established successfully with port 8080 and protocol TCP")

	b.ResetTimer()
	b.SetBytes(int64(len(haystack)))
	for i := 0; i < b.N; i++ {
		pf.Find(haystack, 0)
	}
}
