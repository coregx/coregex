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

// MatchWithCaptures represents a match with capture group information.
// Group 0 is always the entire match, groups 1+ are explicit capture groups.
type MatchWithCaptures struct {
	haystack []byte
	// captures[i] = [start, end] for group i, or nil if not captured
	captures [][]int
}

// NewMatchWithCaptures creates a MatchWithCaptures from capture positions.
func NewMatchWithCaptures(haystack []byte, captures [][]int) *MatchWithCaptures {
	return &MatchWithCaptures{
		haystack: haystack,
		captures: captures,
	}
}

// Start returns the start position of the entire match (group 0).
func (m *MatchWithCaptures) Start() int {
	if len(m.captures) > 0 && m.captures[0] != nil {
		return m.captures[0][0]
	}
	return -1
}

// End returns the end position of the entire match (group 0).
func (m *MatchWithCaptures) End() int {
	if len(m.captures) > 0 && m.captures[0] != nil {
		return m.captures[0][1]
	}
	return -1
}

// Bytes returns the matched bytes for group 0 (entire match).
func (m *MatchWithCaptures) Bytes() []byte {
	if len(m.captures) > 0 && m.captures[0] != nil {
		start, end := m.captures[0][0], m.captures[0][1]
		if start >= 0 && end >= start && end <= len(m.haystack) {
			return m.haystack[start:end]
		}
	}
	return nil
}

// String returns the matched text for group 0 (entire match).
func (m *MatchWithCaptures) String() string {
	return string(m.Bytes())
}

// NumCaptures returns the number of capture groups (including group 0).
func (m *MatchWithCaptures) NumCaptures() int {
	return len(m.captures)
}

// Group returns the captured text for the specified group.
// Group 0 is the entire match. Returns nil if group was not captured.
func (m *MatchWithCaptures) Group(index int) []byte {
	if index < 0 || index >= len(m.captures) {
		return nil
	}
	if m.captures[index] == nil {
		return nil
	}
	start, end := m.captures[index][0], m.captures[index][1]
	if start >= 0 && end >= start && end <= len(m.haystack) {
		return m.haystack[start:end]
	}
	return nil
}

// GroupString returns the captured text as string for the specified group.
func (m *MatchWithCaptures) GroupString(index int) string {
	return string(m.Group(index))
}

// GroupIndex returns [start, end] positions for the specified group.
// Returns nil if group was not captured.
func (m *MatchWithCaptures) GroupIndex(index int) []int {
	if index < 0 || index >= len(m.captures) {
		return nil
	}
	return m.captures[index]
}

// AllGroups returns all captured groups as byte slices.
// Result[0] is the entire match, result[i] may be nil if group i was not captured.
func (m *MatchWithCaptures) AllGroups() [][]byte {
	result := make([][]byte, len(m.captures))
	for i := range m.captures {
		result[i] = m.Group(i)
	}
	return result
}

// AllGroupStrings returns all captured groups as strings.
func (m *MatchWithCaptures) AllGroupStrings() []string {
	result := make([]string, len(m.captures))
	for i := range m.captures {
		result[i] = m.GroupString(i)
	}
	return result
}
