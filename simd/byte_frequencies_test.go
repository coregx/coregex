package simd

import (
	"testing"
)

func TestByteFrequencies_TableSize(t *testing.T) {
	if len(ByteFrequencies) != 256 {
		t.Errorf("ByteFrequencies should have 256 entries, got %d", len(ByteFrequencies))
	}
}

func TestByteFrequencies_CommonBytes(t *testing.T) {
	// Space should be the most common (rank 255)
	if ByteFrequencies[' '] != 255 {
		t.Errorf("Space should have rank 255, got %d", ByteFrequencies[' '])
	}

	// 'e' should be very common (high rank)
	if ByteFrequencies['e'] < 200 {
		t.Errorf("'e' should have high rank (>200), got %d", ByteFrequencies['e'])
	}

	// 't' should be common
	if ByteFrequencies['t'] < 200 {
		t.Errorf("'t' should have high rank (>200), got %d", ByteFrequencies['t'])
	}
}

func TestByteFrequencies_RareBytes(t *testing.T) {
	// '@' should be rare (low rank)
	if ByteFrequencies['@'] > 50 {
		t.Errorf("'@' should have low rank (<50), got %d", ByteFrequencies['@'])
	}

	// 'Q' should be rare
	if ByteFrequencies['Q'] > 50 {
		t.Errorf("'Q' should have low rank (<50), got %d", ByteFrequencies['Q'])
	}

	// 'Z' should be very rare
	if ByteFrequencies['Z'] > 20 {
		t.Errorf("'Z' should have very low rank (<20), got %d", ByteFrequencies['Z'])
	}

	// 'z' should be rare
	if ByteFrequencies['z'] > 50 {
		t.Errorf("'z' should have low rank (<50), got %d", ByteFrequencies['z'])
	}
}

func TestByteRank(t *testing.T) {
	tests := []struct {
		b    byte
		want byte
	}{
		{' ', 255},
		{'@', 25},
		{'e', 245},
	}

	for _, tt := range tests {
		got := ByteRank(tt.b)
		if got != tt.want {
			t.Errorf("ByteRank(%q) = %d, want %d", tt.b, got, tt.want)
		}
	}
}

func TestSelectRareBytes_Empty(t *testing.T) {
	info := SelectRareBytes(nil)
	if info.Byte1 != 0 || info.Index1 != 0 {
		t.Errorf("SelectRareBytes(nil) should return zero values")
	}
}

func TestSelectRareBytes_SingleByte(t *testing.T) {
	info := SelectRareBytes([]byte{'x'})
	if info.Byte1 != 'x' || info.Index1 != 0 {
		t.Errorf("SelectRareBytes single byte failed")
	}
	if info.Byte2 != 'x' || info.Index2 != 0 {
		t.Errorf("SelectRareBytes single byte: Byte2 should equal Byte1")
	}
}

func TestSelectRareBytes_TwoBytes(t *testing.T) {
	// '@' (rank 25) is rarer than 'e' (rank 245)
	info := SelectRareBytes([]byte{'e', '@'})
	if info.Byte1 != '@' {
		t.Errorf("Byte1 should be '@' (rarest), got %q", info.Byte1)
	}
	if info.Index1 != 1 {
		t.Errorf("Index1 should be 1, got %d", info.Index1)
	}
	if info.Byte2 != 'e' {
		t.Errorf("Byte2 should be 'e', got %q", info.Byte2)
	}
}

func TestSelectRareBytes_Email(t *testing.T) {
	// In "@example.com", '@' should be selected as rarest
	needle := []byte("@example.com")
	info := SelectRareBytes(needle)

	if info.Byte1 != '@' {
		t.Errorf("Byte1 should be '@', got %q (rank %d)", info.Byte1, ByteFrequencies[info.Byte1])
	}
	if info.Index1 != 0 {
		t.Errorf("Index1 should be 0, got %d", info.Index1)
	}
}

func TestSelectRareBytes_CommonPattern(t *testing.T) {
	// In "the", all bytes are common but 'h' is slightly rarer
	needle := []byte("the")
	info := SelectRareBytes(needle)

	// 'h' (rank 150) < 't' (rank 215) < 'e' (rank 245)
	if info.Byte1 != 'h' {
		t.Errorf("Byte1 should be 'h' (rarest in 'the'), got %q", info.Byte1)
	}
}

func TestSelectRareBytes_DifferentBytes(t *testing.T) {
	// Ensure Byte1 and Byte2 are different when possible
	needle := []byte("abcdef")
	info := SelectRareBytes(needle)

	if info.Byte1 == info.Byte2 && len(needle) > 1 {
		// Only acceptable if all bytes are the same
		allSame := true
		for i := 1; i < len(needle); i++ {
			if needle[i] != needle[0] {
				allSame = false
				break
			}
		}
		if !allSame {
			t.Errorf("Byte1 and Byte2 should be different: both are %q", info.Byte1)
		}
	}
}

func TestSelectRareBytes_RepeatedBytes(t *testing.T) {
	// All same bytes
	needle := []byte("aaaa")
	info := SelectRareBytes(needle)

	if info.Byte1 != 'a' || info.Byte2 != 'a' {
		t.Errorf("With all same bytes, both should be 'a'")
	}
}

func TestSelectRareByteOptimized_Basic(t *testing.T) {
	tests := []struct {
		needle   string
		wantByte byte
	}{
		{"@example.com", '@'},
		{"hello", 'h'}, // 'h' (150) < 'o' (205) < 'l' (175) < 'e' (245)
		{"test", 's'},  // 's' (200) vs 't' (215) vs 'e' (245) - actually 's' is 200, wait let me check
	}

	for _, tt := range tests {
		gotByte, _ := selectRareByteOptimized([]byte(tt.needle))
		if gotByte != tt.wantByte {
			t.Errorf("selectRareByteOptimized(%q) = %q (rank %d), want %q (rank %d)",
				tt.needle, gotByte, ByteFrequencies[gotByte], tt.wantByte, ByteFrequencies[tt.wantByte])
		}
	}
}

func TestSelectRareByteOptimized_Empty(t *testing.T) {
	b, idx := selectRareByteOptimized(nil)
	if b != 0 || idx != -1 {
		t.Errorf("selectRareByteOptimized(nil) = (%d, %d), want (0, -1)", b, idx)
	}
}

// Benchmark rare byte selection
func BenchmarkSelectRareBytes(b *testing.B) {
	needles := [][]byte{
		[]byte("@example.com"),
		[]byte("hello world"),
		[]byte("the quick brown fox"),
		[]byte("SELECT * FROM users WHERE id = 1"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, needle := range needles {
			SelectRareBytes(needle)
		}
	}
}

func BenchmarkSelectRareByteOptimized(b *testing.B) {
	needles := [][]byte{
		[]byte("@example.com"),
		[]byte("hello world"),
		[]byte("the quick brown fox"),
		[]byte("SELECT * FROM users WHERE id = 1"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, needle := range needles {
			selectRareByteOptimized(needle)
		}
	}
}
