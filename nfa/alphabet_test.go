package nfa

import (
	"testing"
)

func TestByteClasses_Empty(t *testing.T) {
	bc := NewByteClasses()

	// All bytes should be in class 0
	for b := 0; b < 256; b++ {
		if class := bc.Get(byte(b)); class != 0 {
			t.Errorf("Get(%d) = %d, want 0", b, class)
		}
	}

	if !bc.IsEmpty() {
		t.Error("IsEmpty() = false, want true")
	}

	if bc.AlphabetLen() != 1 {
		t.Errorf("AlphabetLen() = %d, want 1", bc.AlphabetLen())
	}
}

func TestByteClasses_Singleton(t *testing.T) {
	bc := SingletonByteClasses()

	// Each byte should be its own class
	for b := 0; b < 256; b++ {
		if class := bc.Get(byte(b)); class != byte(b) {
			t.Errorf("Get(%d) = %d, want %d", b, class, b)
		}
	}

	if !bc.IsSingleton() {
		t.Error("IsSingleton() = false, want true")
	}

	if bc.AlphabetLen() != 256 {
		t.Errorf("AlphabetLen() = %d, want 256", bc.AlphabetLen())
	}
}

func TestByteClassSet_SimpleRange(t *testing.T) {
	bcs := NewByteClassSet()

	// Pattern [a-z] - bytes 'a' to 'z' form one class
	bcs.SetRange('a', 'z')

	bc := bcs.ByteClasses()

	// Should have 3 classes:
	// Class 0: bytes before 'a' (0x00-0x60)
	// Class 1: bytes 'a'-'z' (0x61-0x7a)
	// Class 2: bytes after 'z' (0x7b-0xff)

	// All bytes before 'a' should be class 0
	for b := byte(0); b < 'a'; b++ {
		if class := bc.Get(b); class != 0 {
			t.Errorf("Get(%d) = %d, want 0 (before 'a')", b, class)
		}
	}

	// All bytes 'a'-'z' should be class 1
	for b := byte('a'); b <= 'z'; b++ {
		if class := bc.Get(b); class != 1 {
			t.Errorf("Get(%d '%c') = %d, want 1", b, b, class)
		}
	}

	// All bytes after 'z' should be class 2
	for b := byte('z') + 1; b > 0; b++ { // Loop until overflow
		if class := bc.Get(b); class != 2 {
			t.Errorf("Get(%d) = %d, want 2 (after 'z')", b, class)
		}
		if b == 255 {
			break
		}
	}

	if bc.AlphabetLen() != 3 {
		t.Errorf("AlphabetLen() = %d, want 3", bc.AlphabetLen())
	}
}

func TestByteClassSet_MultipleRanges(t *testing.T) {
	bcs := NewByteClassSet()

	// Pattern [a-z]|[0-9]
	bcs.SetRange('a', 'z')
	bcs.SetRange('0', '9')

	bc := bcs.ByteClasses()

	// Should have 5 classes:
	// Class 0: bytes 0x00-0x2f (before '0')
	// Class 1: bytes '0'-'9'
	// Class 2: bytes 0x3a-0x60 (between '9' and 'a')
	// Class 3: bytes 'a'-'z'
	// Class 4: bytes 0x7b-0xff (after 'z')

	// All '0'-'9' should be same class
	class0 := bc.Get('0')
	for b := byte('0'); b <= '9'; b++ {
		if class := bc.Get(b); class != class0 {
			t.Errorf("Get('%c') = %d, want %d (same as '0')", b, class, class0)
		}
	}

	// All 'a'-'z' should be same class
	classA := bc.Get('a')
	for b := byte('a'); b <= 'z'; b++ {
		if class := bc.Get(b); class != classA {
			t.Errorf("Get('%c') = %d, want %d (same as 'a')", b, class, classA)
		}
	}

	// '0' and 'a' should be different classes
	if class0 == classA {
		t.Errorf("'0' and 'a' should be different classes, both got %d", class0)
	}

	if bc.AlphabetLen() != 5 {
		t.Errorf("AlphabetLen() = %d, want 5", bc.AlphabetLen())
	}
}

func TestByteClassSet_SingleByte(t *testing.T) {
	bcs := NewByteClassSet()

	// Pattern 'x' - single byte
	bcs.SetByte('x')

	bc := bcs.ByteClasses()

	// Should have 3 classes:
	// Class 0: bytes before 'x'
	// Class 1: byte 'x'
	// Class 2: bytes after 'x'

	// Bytes before 'x' should be class 0
	if class := bc.Get('a'); class != 0 {
		t.Errorf("Get('a') = %d, want 0", class)
	}

	// 'x' should be class 1
	if class := bc.Get('x'); class != 1 {
		t.Errorf("Get('x') = %d, want 1", class)
	}

	// Bytes after 'x' should be class 2
	if class := bc.Get('y'); class != 2 {
		t.Errorf("Get('y') = %d, want 2", class)
	}

	if bc.AlphabetLen() != 3 {
		t.Errorf("AlphabetLen() = %d, want 3", bc.AlphabetLen())
	}
}

func TestByteClasses_Representatives(t *testing.T) {
	bcs := NewByteClassSet()
	bcs.SetRange('a', 'z')

	bc := bcs.ByteClasses()
	reps := bc.Representatives()

	// Should have 3 representatives (one per class)
	if len(reps) != 3 {
		t.Errorf("len(Representatives()) = %d, want 3", len(reps))
	}

	// Check that each representative maps to a unique class
	classes := make(map[byte]bool)
	for _, rep := range reps {
		class := bc.Get(rep)
		if classes[class] {
			t.Errorf("Duplicate class %d for representative %d", class, rep)
		}
		classes[class] = true
	}
}

func TestByteClasses_Elements(t *testing.T) {
	bcs := NewByteClassSet()
	bcs.SetRange('a', 'z')

	bc := bcs.ByteClasses()

	// Get elements of class 1 (should be a-z)
	elements := bc.Elements(1)

	if len(elements) != 26 {
		t.Errorf("len(Elements(1)) = %d, want 26", len(elements))
	}

	// Verify all elements are in the range 'a'-'z'
	for _, elem := range elements {
		if elem < 'a' || elem > 'z' {
			t.Errorf("Element %d not in range 'a'-'z'", elem)
		}
	}
}

func TestByteClassSet_Merge(t *testing.T) {
	bcs1 := NewByteClassSet()
	bcs1.SetRange('a', 'z')

	bcs2 := NewByteClassSet()
	bcs2.SetRange('0', '9')

	bcs1.Merge(bcs2)

	bc := bcs1.ByteClasses()

	// Should have 5 classes after merge
	if bc.AlphabetLen() != 5 {
		t.Errorf("AlphabetLen() after merge = %d, want 5", bc.AlphabetLen())
	}

	// Verify both ranges are distinct
	if bc.Get('a') == bc.Get('0') {
		t.Error("'a' and '0' should be in different classes after merge")
	}
}

func TestByteClassSet_AdjacentRanges(t *testing.T) {
	bcs := NewByteClassSet()

	// Pattern [a-m][n-z] - adjacent ranges
	bcs.SetRange('a', 'm')
	bcs.SetRange('n', 'z')

	bc := bcs.ByteClasses()

	// Should have 4 classes:
	// Class 0: before 'a'
	// Class 1: 'a'-'m'
	// Class 2: 'n'-'z'
	// Class 3: after 'z'

	if bc.Get('a') != bc.Get('m') {
		t.Error("'a' and 'm' should be same class")
	}

	if bc.Get('n') != bc.Get('z') {
		t.Error("'n' and 'z' should be same class")
	}

	if bc.Get('m') == bc.Get('n') {
		t.Error("'m' and 'n' should be different classes")
	}

	if bc.AlphabetLen() != 4 {
		t.Errorf("AlphabetLen() = %d, want 4", bc.AlphabetLen())
	}
}

func BenchmarkByteClasses_Get(b *testing.B) {
	bcs := NewByteClassSet()
	bcs.SetRange('a', 'z')
	bcs.SetRange('0', '9')
	bc := bcs.ByteClasses()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bc.Get(byte(i % 256))
	}
}

func BenchmarkByteClasses_Representatives(b *testing.B) {
	bcs := NewByteClassSet()
	bcs.SetRange('a', 'z')
	bcs.SetRange('0', '9')
	bc := bcs.ByteClasses()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bc.Representatives()
	}
}
