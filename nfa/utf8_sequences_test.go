package nfa

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

func seqOf(rs ...utf8ByteRange) utf8Sequence {
	s := utf8Sequence{n: len(rs)}
	copy(s.ranges[:], rs)
	return s
}

func encodeSurrogate(cp rune) []byte {
	return []byte{
		byte(cp>>12&0x0F) | 0xE0,
		byte(cp>>6&0x3F) | 0x80,
		byte(cp&0x3F) | 0x80,
	}
}

// Ported from Rust regex-syntax utf8.rs: codepoints_no_surrogates.
func TestUTF8Sequences_NoSurrogates(t *testing.T) {
	for _, r := range [][2]rune{
		{0, 0xFFFF}, {0, 0x10FFFF}, {0, 0x10FFFE}, {0x80, 0x10FFFF}, {0xD7FF, 0xE000},
	} {
		seqs := utf8Sequences(r[0], r[1])
		for cp := rune(0xD800); cp < 0xE000; cp++ {
			b := encodeSurrogate(cp)
			for _, s := range seqs {
				if s.matches(b) {
					t.Fatalf("range %X-%X: sequence %v matches surrogate %X", r[0], r[1], s.bytes(), cp)
				}
			}
		}
	}
}

// Ported from Rust regex-syntax utf8.rs: single_codepoint_one_sequence.
func TestUTF8Sequences_SingleCodepointOneSequence(t *testing.T) {
	for cp := rune(0); cp <= utf8.MaxRune; cp++ {
		if !utf8.ValidRune(cp) {
			continue
		}
		if n := len(utf8Sequences(cp, cp)); n != 1 {
			t.Fatalf("U+%04X: %d sequences, want 1", cp, n)
		}
	}
}

// Ported from Rust regex-syntax utf8.rs: bmp.
func TestUTF8Sequences_BMP(t *testing.T) {
	want := []utf8Sequence{
		seqOf(utf8ByteRange{0x00, 0x7F}),
		seqOf(utf8ByteRange{0xC2, 0xDF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xE0, 0xE0}, utf8ByteRange{0xA0, 0xBF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xE1, 0xEC}, utf8ByteRange{0x80, 0xBF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xED, 0xED}, utf8ByteRange{0x80, 0x9F}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xEE, 0xEF}, utf8ByteRange{0x80, 0xBF}, utf8ByteRange{0x80, 0xBF}),
	}
	if got := utf8Sequences(0, 0xFFFF); !reflect.DeepEqual(got, want) {
		t.Fatalf("BMP sequences:\n got %v\nwant %v", got, want)
	}
}

// The module documentation example in Rust utf8.rs.
func TestUTF8Sequences_AllUnicode(t *testing.T) {
	want := []utf8Sequence{
		seqOf(utf8ByteRange{0x00, 0x7F}),
		seqOf(utf8ByteRange{0xC2, 0xDF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xE0, 0xE0}, utf8ByteRange{0xA0, 0xBF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xE1, 0xEC}, utf8ByteRange{0x80, 0xBF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xED, 0xED}, utf8ByteRange{0x80, 0x9F}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xEE, 0xEF}, utf8ByteRange{0x80, 0xBF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xF0, 0xF0}, utf8ByteRange{0x90, 0xBF}, utf8ByteRange{0x80, 0xBF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xF1, 0xF3}, utf8ByteRange{0x80, 0xBF}, utf8ByteRange{0x80, 0xBF}, utf8ByteRange{0x80, 0xBF}),
		seqOf(utf8ByteRange{0xF4, 0xF4}, utf8ByteRange{0x80, 0x8F}, utf8ByteRange{0x80, 0xBF}, utf8ByteRange{0x80, 0xBF}),
	}
	if got := utf8Sequences(0, utf8.MaxRune); !reflect.DeepEqual(got, want) {
		t.Fatalf("all-Unicode sequences:\n got %v\nwant %v", got, want)
	}
}

// TestUTF8Sequences_Exact checks, for every scalar value, that its encoding
// is matched by exactly one sequence when it is in the range and by none
// when it is not. The ranges include supplementary-plane repros where only
// the lead byte used to be constrained.
func TestUTF8Sequences_Exact(t *testing.T) {
	ranges := [][2]rune{
		{0x20000, 0x2A6DF}, {0x10000, 0x10FFF}, {0x1F600, 0x1F64F},
		{0x1003F, 0x10040}, {0x3FFFF, 0x40000}, {0xFFFFF, 0x100000},
		{0x10FFFF, 0x10FFFF}, {0x0400, 0x052F}, {0x80, 0x10FFFF},
		{0x7FF, 0x800}, {0xD7FF, 0xE000}, {0x12345, 0x10ABCD}, {0x41, 0x5A},
	}
	var buf [utf8.UTFMax]byte
	for _, r := range ranges {
		seqs := utf8Sequences(r[0], r[1])
		for cp := rune(0); cp <= utf8.MaxRune; cp++ {
			if !utf8.ValidRune(cp) {
				continue
			}
			n := utf8.EncodeRune(buf[:], cp)
			hits := 0
			for i := range seqs {
				if seqs[i].n == n && seqs[i].matches(buf[:n]) {
					hits++
				}
			}
			want := 0
			if cp >= r[0] && cp <= r[1] {
				want = 1
			}
			if hits != want {
				t.Fatalf("range %X-%X, U+%04X: matched by %d sequences, want %d", r[0], r[1], cp, hits, want)
			}
		}
	}
}

// TestUTF8Sequences_RejectsInvalid checks that no sequence of the full range
// matches an overlong encoding, a surrogate, a value above U+10FFFF or a
// stray continuation byte.
func TestUTF8Sequences_RejectsInvalid(t *testing.T) {
	seqs := utf8Sequences(0, utf8.MaxRune)
	invalid := [][]byte{
		{0xC0, 0x80}, {0xC1, 0xBF}, // overlong 2-byte
		{0xE0, 0x80, 0x80}, {0xE0, 0x9F, 0xBF}, // overlong 3-byte
		{0xF0, 0x80, 0x80, 0x80}, {0xF0, 0x8F, 0xBF, 0xBF}, // overlong 4-byte
		{0xED, 0xA0, 0x80}, {0xED, 0xBF, 0xBF}, // surrogates
		{0xF4, 0x90, 0x80, 0x80}, {0xF5, 0x80, 0x80, 0x80}, // > U+10FFFF
		{0x80}, {0xBF}, {0xFF}, // stray bytes
	}
	for _, b := range invalid {
		for _, s := range seqs {
			if s.n == len(b) && s.matches(b) {
				t.Errorf("% X matched by %v", b, s.bytes())
			}
		}
	}
}
