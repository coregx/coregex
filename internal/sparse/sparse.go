// Package sparse provides a sparse set data structure for efficient state tracking.
//
// A sparse set achieves O(1) insertion, membership testing, and clearing by exploiting
// the "uninitialized memory trick" described by Briggs & Torczon (1993) and popularized
// by Russ Cox for regex engines (https://research.swtch.com/sparse).
//
// This is the preferred data structure for NFA state tracking because:
//   - O(1) clear: just reset the size counter, no memory zeroing needed
//   - O(1) membership test: cross-validation between sparse and dense arrays
//   - Insertion order preserved: enables deterministic NFA simulation
//   - Cache-friendly iteration: sequential access to dense array
//
// Trade-off: Uses 8 bytes per potential element (4 bytes sparse + 4 bytes dense)
// vs 1 bit for bitsets. But the O(1) clear makes this worthwhile for regex engines
// where clear is called per-position during NFA simulation.
//
// Reference implementations:
//   - Rust regex-automata: regex-automata/src/util/sparse_set.rs
//   - RE2: re2/sparse_set.h
//   - Go regexp: uses bitsets (different trade-off for small patterns)
package sparse

// SparseSet is a set of uint32 values with O(1) membership, insertion, and clear.
//
// The data structure maintains two arrays:
//   - sparse[value] = index in dense where value is stored
//   - dense[index] = the actual value
//
// Membership test: sparse[v] < size AND dense[sparse[v]] == v
// The cross-validation handles garbage values in uninitialized sparse slots.
//
// Example layout for set {2, 5, 1}:
//
//	sparse[]                        dense[]
//	+---+---+---+---+---+---+       +---+---+---+---+
//	| ? | 2 | 0 | ? | ? | 1 |       | 2 | 5 | 1 | ? |
//	+---+---+---+---+---+---+       +---+---+---+---+
//	  0   1   2   3   4   5           0   1   2
//	                                        ^ size=3
//
// Clear() simply sets size=0, making all elements "unreachable" in O(1).
type SparseSet struct {
	sparse []uint32 // Maps value -> index in dense (capacity = max value + 1)
	dense  []uint32 // Stores values in insertion order (capacity = max value + 1)
	size   uint32   // Number of elements currently in the set
}

// NewSparseSet creates a new sparse set with the given capacity.
// Capacity must be at least max(values)+1 that will be stored.
// For NFA state tracking, use nfa.States() as capacity.
func NewSparseSet(capacity uint32) *SparseSet {
	if capacity == 0 {
		capacity = 64 // Reasonable default
	}
	return &SparseSet{
		sparse: make([]uint32, capacity),
		dense:  make([]uint32, capacity),
		size:   0,
	}
}

// Insert adds a value to the set. Returns true if the value was newly added.
// If the value is already present, returns false (no-op).
// Panics if value >= capacity.
func (s *SparseSet) Insert(value uint32) bool {
	if s.Contains(value) {
		return false
	}
	// Direct assignment to pre-allocated array (no append overhead)
	s.dense[s.size] = value
	s.sparse[value] = s.size
	s.size++
	return true
}

// Contains returns true if the value is in the set.
// This is the key operation - O(1) with cross-validation to handle garbage.
func (s *SparseSet) Contains(value uint32) bool {
	// Bounds check: value must be within sparse array capacity
	if value >= uint32(len(s.sparse)) {
		return false
	}
	idx := s.sparse[value]
	// Cross-validation: sparse[value] must point to valid dense index
	// AND dense[idx] must equal value (handles garbage in sparse)
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
	// Swap-and-pop: move last element to fill the gap
	s.size--
	if idx < s.size {
		lastValue := s.dense[s.size]
		s.dense[idx] = lastValue
		s.sparse[lastValue] = idx
	}
}

// Clear removes all elements in O(1) time.
// This is the magic of sparse sets - no memory zeroing needed.
// All previous elements become "unreachable" because idx >= size.
func (s *SparseSet) Clear() {
	s.size = 0
}

// Len returns the number of elements in the set.
func (s *SparseSet) Len() int {
	return int(s.size)
}

// Size returns the number of elements in the set.
// Alias for Len() for compatibility.
func (s *SparseSet) Size() int {
	return int(s.size)
}

// IsEmpty returns true if the set contains no elements.
func (s *SparseSet) IsEmpty() bool {
	return s.size == 0
}

// Capacity returns the maximum value that can be stored + 1.
func (s *SparseSet) Capacity() uint32 {
	return uint32(len(s.sparse))
}

// Values returns a slice of all values in the set.
// The returned slice is valid until the next mutation.
// Values are in insertion order (first inserted = first in slice).
func (s *SparseSet) Values() []uint32 {
	return s.dense[:s.size]
}

// Iter calls the given function for each value in the set.
// Values are iterated in insertion order.
func (s *SparseSet) Iter(f func(uint32)) {
	for i := uint32(0); i < s.size; i++ {
		f(s.dense[i])
	}
}

// Resize changes the capacity of the set.
// If newCapacity < current capacity, the set is cleared.
// If newCapacity > current capacity, arrays are reallocated.
func (s *SparseSet) Resize(newCapacity uint32) {
	if newCapacity == 0 {
		newCapacity = 64
	}
	currentCap := uint32(len(s.sparse))
	if newCapacity <= currentCap {
		// Shrinking or same size - just clear
		s.size = 0
		return
	}
	// Growing - reallocate arrays
	newSparse := make([]uint32, newCapacity)
	newDense := make([]uint32, newCapacity)
	// Copy existing elements
	copy(newSparse, s.sparse)
	copy(newDense[:s.size], s.dense[:s.size])
	s.sparse = newSparse
	s.dense = newDense
}

// MemoryUsage returns the approximate memory usage in bytes.
// Formula: 2 * capacity * 4 bytes (for sparse and dense arrays)
func (s *SparseSet) MemoryUsage() int {
	return len(s.sparse)*4 + len(s.dense)*4
}

// Clone creates a deep copy of the sparse set.
func (s *SparseSet) Clone() *SparseSet {
	clone := NewSparseSet(s.Capacity())
	copy(clone.sparse, s.sparse)
	copy(clone.dense[:s.size], s.dense[:s.size])
	clone.size = s.size
	return clone
}

// SparseSets holds a pair of sparse sets for double-buffering.
// This is the Rust regex-automata pattern for NFA simulation:
//   - Set1 holds current generation states
//   - Set2 accumulates next generation states
//   - After each byte: Swap() and Clear(Set2)
type SparseSets struct {
	Set1 *SparseSet // Current generation
	Set2 *SparseSet // Next generation
}

// NewSparseSets creates a pair of sparse sets with given capacity.
func NewSparseSets(capacity uint32) *SparseSets {
	return &SparseSets{
		Set1: NewSparseSet(capacity),
		Set2: NewSparseSet(capacity),
	}
}

// Swap exchanges Set1 and Set2 in O(1) time (pointer swap).
func (ss *SparseSets) Swap() {
	ss.Set1, ss.Set2 = ss.Set2, ss.Set1
}

// Clear clears both sets.
func (ss *SparseSets) Clear() {
	ss.Set1.Clear()
	ss.Set2.Clear()
}

// Resize resizes both sets to the new capacity.
func (ss *SparseSets) Resize(newCapacity uint32) {
	ss.Set1.Resize(newCapacity)
	ss.Set2.Resize(newCapacity)
}

// MemoryUsage returns total memory usage of both sets.
func (ss *SparseSets) MemoryUsage() int {
	return ss.Set1.MemoryUsage() + ss.Set2.MemoryUsage()
}
