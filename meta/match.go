package meta

// Match represents a successful regex match with position information.
//
// A Match contains:
//   - Start position (inclusive)
//   - End position (exclusive)
//   - Reference to the original haystack
//
// Note: This is a simple match without capture group support.
// Capture groups will be added in a future version.
//
// Example:
//
//	match := &Match{start: 5, end: 11, haystack: []byte("test foo123 end")}
//	println(match.String()) // "foo123"
//	println(match.Start(), match.End()) // 5, 11
type Match struct {
	start    int
	end      int
	haystack []byte
}

// NewMatch creates a new Match from start and end positions.
//
// Parameters:
//   - start: inclusive start position in haystack
//   - end: exclusive end position in haystack
//   - haystack: the original byte buffer that was searched
//
// The haystack is stored by reference (not copied) for efficiency.
// Callers must ensure the haystack remains valid for the lifetime of the Match.
//
// Example:
//
//	haystack := []byte("hello world")
//	match := meta.NewMatch(0, 5, haystack) // "hello"
func NewMatch(start, end int, haystack []byte) *Match {
	return &Match{
		start:    start,
		end:      end,
		haystack: haystack,
	}
}

// Start returns the inclusive start position of the match.
//
// Example:
//
//	match := meta.NewMatch(5, 11, []byte("test foo123 end"))
//	println(match.Start()) // 5
func (m *Match) Start() int {
	return m.start
}

// End returns the exclusive end position of the match.
//
// Example:
//
//	match := meta.NewMatch(5, 11, []byte("test foo123 end"))
//	println(match.End()) // 11
func (m *Match) End() int {
	return m.end
}

// Len returns the length of the match in bytes.
//
// Example:
//
//	match := meta.NewMatch(5, 11, []byte("test foo123 end"))
//	println(match.Len()) // 6 (11 - 5)
func (m *Match) Len() int {
	return m.end - m.start
}

// Bytes returns the matched bytes as a slice.
//
// The returned slice is a view into the original haystack (not a copy).
// Callers should copy the bytes if they need to retain them after the
// haystack is modified or deallocated.
//
// Example:
//
//	match := meta.NewMatch(5, 11, []byte("test foo123 end"))
//	println(string(match.Bytes())) // "foo123"
func (m *Match) Bytes() []byte {
	if m.start < 0 || m.end > len(m.haystack) || m.start > m.end {
		return nil
	}
	return m.haystack[m.start:m.end]
}

// String returns the matched text as a string.
//
// This allocates a new string by copying the matched bytes.
// For zero-allocation access, use Bytes() instead.
//
// Example:
//
//	match := meta.NewMatch(5, 11, []byte("test foo123 end"))
//	println(match.String()) // "foo123"
func (m *Match) String() string {
	return string(m.Bytes())
}

// IsEmpty returns true if the match has zero length.
//
// Empty matches can occur with patterns like "" or "(?:)" that match
// without consuming input.
//
// Example:
//
//	match := meta.NewMatch(5, 5, []byte("test"))
//	println(match.IsEmpty()) // true
func (m *Match) IsEmpty() bool {
	return m.start == m.end
}

// Contains returns true if the given position is within the match range.
//
// Parameters:
//   - pos: position to check (must be >= 0)
//
// Returns true if start <= pos < end.
//
// Example:
//
//	match := meta.NewMatch(5, 11, []byte("test foo123 end"))
//	println(match.Contains(7))  // true
//	println(match.Contains(11)) // false (exclusive end)
func (m *Match) Contains(pos int) bool {
	return pos >= m.start && pos < m.end
}
