package nfa

import "unicode/utf8"

// utf8ByteRange is an inclusive range of bytes [lo, hi].
type utf8ByteRange struct {
	lo, hi byte
}

// utf8Sequence is a sequence of 1 to 4 byte ranges. A byte string is in the
// sequence when its i-th byte falls in the i-th range, for every range.
type utf8Sequence struct {
	n      int
	ranges [utf8.UTFMax]utf8ByteRange
}

// bytes returns the byte ranges of the sequence, lead byte first.
func (s *utf8Sequence) bytes() []utf8ByteRange {
	return s.ranges[:s.n]
}

// matches reports whether a prefix of b matches the sequence.
func (s *utf8Sequence) matches(b []byte) bool {
	if len(b) < s.n {
		return false
	}
	for i, r := range s.bytes() {
		if b[i] < r.lo || b[i] > r.hi {
			return false
		}
	}
	return true
}

// utf8Sequences splits the scalar value range [lo, hi] into sequences of
// UTF-8 byte ranges. The sequences together match exactly the UTF-8
// encodings of the scalar values in [lo, hi]: no surrogates, no overlong or
// otherwise invalid encodings, and no two sequences match the same bytes.
//
// For example, [U+0400, U+052F] gives
//
//	[D0-D3][80-BF]  U+0400-U+04FF
//	[D4][80-AF]     U+0500-U+052F
//
// This is a port of Utf8Sequences from Rust regex-syntax (src/utf8.rs).
// The range is first split where the encoded length changes, then at
// continuation-byte boundaries, until the encodings of both ends differ only
// in byte values, never in which bytes are free. Each such range is then one
// sequence: byte i ranges from start's byte i to end's byte i.
func utf8Sequences(lo, hi rune) []utf8Sequence {
	if lo < 0 {
		lo = 0
	}
	if hi > utf8.MaxRune {
		hi = utf8.MaxRune
	}
	var out []utf8Sequence
	// The lower part of a split is pushed last, so it is processed first and
	// the sequences come out in ascending order, as in Rust.
	stack := [][2]rune{{lo, hi}}
	for len(stack) > 0 {
		start, end := stack[len(stack)-1][0], stack[len(stack)-1][1]
		stack = stack[:len(stack)-1]
		if start > end {
			continue
		}
		if firstEnd, secondStart, ok := utf8SplitRange(start, end); ok {
			stack = append(stack, [2]rune{secondStart, end}, [2]rune{start, firstEnd})
			continue
		}
		var bs, be [utf8.UTFMax]byte
		n := utf8.EncodeRune(bs[:], start)
		utf8.EncodeRune(be[:], end)
		seq := utf8Sequence{n: n}
		for i := range n {
			seq.ranges[i] = utf8ByteRange{bs[i], be[i]}
		}
		out = append(out, seq)
	}
	return out
}

// utf8SplitRange reports whether the non-empty range [start, end] must be
// split before it can be one sequence, and if so where: the parts are
// [start, firstEnd] and [secondStart, end].
func utf8SplitRange(start, end rune) (firstEnd, secondStart rune, ok bool) {
	// Surrogates have no UTF-8 encoding: cut them out.
	if start < 0xE000 && end > 0xD7FF {
		return 0xD7FF, 0xE000, true
	}
	// Split where the encoded length changes.
	for _, maxScalar := range [...]rune{0x7F, 0x7FF, 0xFFFF} {
		if start <= maxScalar && maxScalar < end {
			return maxScalar, maxScalar + 1, true
		}
	}
	if end <= 0x7F {
		return 0, 0, false
	}
	// Split at continuation-byte boundaries: the low 6*i bits of start must
	// be all zeros and those of end all ones, whenever start and end differ
	// above them.
	for i := 1; i < utf8.UTFMax; i++ {
		m := rune(1)<<(6*i) - 1
		if start&^m != end&^m {
			if start&m != 0 {
				return start | m, (start | m) + 1, true
			}
			if end&m != m {
				return end&^m - 1, end &^ m, true
			}
		}
	}
	return 0, 0, false
}
