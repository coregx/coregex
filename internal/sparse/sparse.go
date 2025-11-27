// Package sparse provides a sparse set data structure for efficient membership testing.
//
// A sparse set is a data structure that supports O(1) insertion, deletion, and membership
// testing while maintaining a dense list of elements. It's particularly useful for NFA
// simulation where we need to track visited states.
package sparse

// SparseSet is a set of uint32 values that supports O(1) operations.
// It maintains both a sparse array (for membership testing) and a dense array
// (for iteration). The sparse array maps values to indices in the dense array.
//
// This implementation is optimized for cases where the universe of possible
// values is known and relatively small (e.g., NFA state IDs).
type SparseSet struct {
	sparse []uint32 // Maps value -> index in dense
	dense  []uint32 // Contains the actual values
	size   uint32   // Current number of elements
}

// NewSparseSet creates a new sparse set with the given capacity.
// The capacity represents the maximum value that can be stored (exclusive).
func NewSparseSet(capacity uint32) *SparseSet {
	return &SparseSet{
		sparse: make([]uint32, capacity),
		dense:  make([]uint32, 0, capacity),
		size:   0,
	}
}

// Insert adds a value to the set.
// If the value is already present, this is a no-op.
// Panics if value >= capacity.
func (s *SparseSet) Insert(value uint32) {
	if s.Contains(value) {
		return
	}

	// Add to dense array
	s.dense = append(s.dense, value)
	// Map value to its index in dense
	s.sparse[value] = s.size
	s.size++
}

// Contains returns true if the value is in the set
func (s *SparseSet) Contains(value uint32) bool {
	// Bounds check: value must be within sparse array bounds
	// Check for potential overflow when converting len to uint32
	if len(s.sparse) > 0x7FFFFFFF {
		return false // len too large for safe conversion
	}
	//nolint:gosec // G115: len is checked above for safe conversion to uint32
	sparseLen := uint32(len(s.sparse))
	if value >= sparseLen {
		return false
	}
	idx := s.sparse[value]
	return idx < s.size && s.dense[idx] == value
}

// Remove removes a value from the set.
// If the value is not present, this is a no-op.
func (s *SparseSet) Remove(value uint32) {
	if !s.Contains(value) {
		return
	}

	// Get index of value in dense array
	idx := s.sparse[value]

	// Move last element to this position (swap and pop)
	lastValue := s.dense[s.size-1]
	s.dense[idx] = lastValue
	s.sparse[lastValue] = idx

	s.size--
	s.dense = s.dense[:s.size]
}

// Clear removes all elements from the set in O(1) time
func (s *SparseSet) Clear() {
	s.size = 0
	s.dense = s.dense[:0]
}

// Size returns the number of elements in the set
func (s *SparseSet) Size() int {
	return int(s.size)
}

// IsEmpty returns true if the set contains no elements
func (s *SparseSet) IsEmpty() bool {
	return s.size == 0
}

// Values returns a slice of all values in the set.
// The returned slice is valid until the next mutation.
func (s *SparseSet) Values() []uint32 {
	return s.dense[:s.size]
}

// Iter calls the given function for each value in the set.
// The iteration order is unspecified.
func (s *SparseSet) Iter(f func(uint32)) {
	for i := uint32(0); i < s.size; i++ {
		f(s.dense[i])
	}
}
