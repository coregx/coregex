package nfa

// ByteClasses maps each byte value to its equivalence class.
//
// ByteClasses is an alphabet reduction technique that groups bytes into
// equivalence classes based on how the regex treats them. Instead of
// maintaining 256 transitions per DFA state, transitions are reduced to
// N classes (typically 4-64), dramatically reducing memory footprint.
//
// Key principle: Two bytes belong to the same equivalence class if they
// will never cause different transitions in any DFA state for the given regex.
//
// Example for pattern [a-z]+:
//   - Class 0: bytes 0x00-0x60 (before 'a')
//   - Class 1: bytes 0x61-0x7a ('a' to 'z')
//   - Class 2: bytes 0x7b-0xff (after 'z')
//
// Memory impact:
//   - Without ByteClasses: 256 transitions per state
//   - With ByteClasses: ~8-16 transitions per state (15-30x reduction)
type ByteClasses struct {
	// classes maps each byte (0-255) to its equivalence class
	classes [256]byte
}

// NewByteClasses creates a new ByteClasses where all bytes are in class 0.
func NewByteClasses() ByteClasses {
	return ByteClasses{} // Zero-initialized: all bytes in class 0
}

// SingletonByteClasses creates ByteClasses where each byte is its own class.
// This is equivalent to no alphabet reduction (256 classes).
func SingletonByteClasses() ByteClasses {
	var bc ByteClasses
	for i := 0; i < 256; i++ {
		bc.classes[i] = byte(i)
	}
	return bc
}

// Get returns the equivalence class for the given byte.
// This is an O(1) lookup.
func (bc *ByteClasses) Get(b byte) byte {
	return bc.classes[b]
}

// AlphabetLen returns the total number of equivalence classes.
// This is the number of distinct class values + 1 for potential EOI sentinel.
func (bc *ByteClasses) AlphabetLen() int {
	// Find the maximum class number
	maxClass := byte(0)
	for _, c := range bc.classes {
		if c > maxClass {
			maxClass = c
		}
	}
	return int(maxClass) + 1
}

// IsSingleton returns true if each byte is its own equivalence class.
// This means no alphabet reduction is possible.
func (bc *ByteClasses) IsSingleton() bool {
	return bc.AlphabetLen() == 256
}

// IsEmpty returns true if all bytes are in the same equivalence class (class 0).
// This typically means the regex matches any byte or no bytes at all.
func (bc *ByteClasses) IsEmpty() bool {
	return bc.AlphabetLen() == 1
}

// Representatives returns a slice of representative bytes, one for each class.
// Each representative can be used to compute transitions for all bytes in that class.
func (bc *ByteClasses) Representatives() []byte {
	seen := make([]bool, 256)
	var reps []byte

	for b := 0; b < 256; b++ {
		class := bc.classes[b]
		if !seen[class] {
			seen[class] = true
			reps = append(reps, byte(b))
		}
	}

	return reps
}

// Elements returns all bytes that belong to the given equivalence class.
func (bc *ByteClasses) Elements(class byte) []byte {
	var elems []byte
	for b := 0; b < 256; b++ {
		if bc.classes[b] == class {
			elems = append(elems, byte(b))
		}
	}
	return elems
}

// ByteClassSet tracks byte boundaries during NFA construction.
//
// During NFA compilation, we track which bytes are "boundary" bytes where
// equivalence classes change. For example, if pattern is [a-z], then bytes
// 'a'-1 and 'z' are boundaries.
//
// Algorithm:
//  1. For each ByteRange [lo, hi] in NFA transitions:
//     - If lo > 0: mark lo-1 as boundary
//     - Mark hi as boundary
//  2. Convert boundaries to classes by incrementing class at each boundary
type ByteClassSet struct {
	// bits is a 256-bit bitset where bit i is set if byte i is a class boundary
	bits [4]uint64
}

// NewByteClassSet creates an empty ByteClassSet with no boundaries.
func NewByteClassSet() *ByteClassSet {
	return &ByteClassSet{}
}

// SetRange marks a byte range [start, end] as having distinct transitions.
// This sets boundary bits at start-1 and end.
func (bcs *ByteClassSet) SetRange(start, end byte) {
	if start > 0 {
		bcs.setBit(start - 1)
	}
	bcs.setBit(end)
}

// SetByte marks a single byte as having a distinct transition.
// Equivalent to SetRange(b, b).
func (bcs *ByteClassSet) SetByte(b byte) {
	bcs.SetRange(b, b)
}

// setBit sets bit i in the bitset
func (bcs *ByteClassSet) setBit(b byte) {
	word := b / 64
	bit := b % 64
	bcs.bits[word] |= 1 << bit
}

// getBit returns true if bit i is set
func (bcs *ByteClassSet) getBit(b byte) bool {
	word := b / 64
	bit := b % 64
	return (bcs.bits[word] & (1 << bit)) != 0
}

// ByteClasses converts the boundary set into a ByteClasses lookup table.
//
// Algorithm: Walk through all 256 bytes, incrementing the class number
// each time we encounter a boundary byte.
func (bcs *ByteClassSet) ByteClasses() ByteClasses {
	var bc ByteClasses
	class := byte(0)

	for b := 0; b < 256; b++ {
		bc.classes[b] = class
		if bcs.getBit(byte(b)) {
			class++
		}
	}

	return bc
}

// Merge combines another ByteClassSet into this one.
// Used when building composite patterns.
func (bcs *ByteClassSet) Merge(other *ByteClassSet) {
	bcs.bits[0] |= other.bits[0]
	bcs.bits[1] |= other.bits[1]
	bcs.bits[2] |= other.bits[2]
	bcs.bits[3] |= other.bits[3]
}
