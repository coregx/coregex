package sparse

import (
	"testing"
)

// TestSparseSetSize tests the Size() method (alias for Len).
func TestSparseSetSize(t *testing.T) {
	s := NewSparseSet(10)

	if s.Size() != 0 {
		t.Errorf("expected Size()=0, got %d", s.Size())
	}

	s.Insert(1)
	s.Insert(3)
	s.Insert(5)
	if s.Size() != 3 {
		t.Errorf("expected Size()=3, got %d", s.Size())
	}
	if s.Size() != s.Len() {
		t.Errorf("Size() and Len() should agree: %d vs %d", s.Size(), s.Len())
	}
}

// TestSparseSetIter tests the Iter() method.
func TestSparseSetIter(t *testing.T) {
	s := NewSparseSet(10)
	s.Insert(7)
	s.Insert(2)
	s.Insert(5)

	var collected []uint32
	s.Iter(func(v uint32) {
		collected = append(collected, v)
	})

	if len(collected) != 3 {
		t.Fatalf("expected 3 items, got %d", len(collected))
	}
	// Insertion order: 7, 2, 5
	if collected[0] != 7 || collected[1] != 2 || collected[2] != 5 {
		t.Errorf("expected [7,2,5], got %v", collected)
	}
}

// TestSparseSetIterEmpty tests Iter on an empty set.
func TestSparseSetIterEmpty(t *testing.T) {
	s := NewSparseSet(10)

	called := false
	s.Iter(func(v uint32) {
		called = true
	})
	if called {
		t.Error("Iter should not call function on empty set")
	}
}

// TestSparseSetMemoryUsage tests the MemoryUsage() method.
func TestSparseSetMemoryUsage(t *testing.T) {
	s := NewSparseSet(100)
	expected := 100*4 + 100*4 // sparse + dense, each 100 elements * 4 bytes
	if got := s.MemoryUsage(); got != expected {
		t.Errorf("expected MemoryUsage()=%d, got %d", expected, got)
	}
}

// TestSparseSetMemoryUsageDefault tests MemoryUsage with default capacity.
func TestSparseSetMemoryUsageDefault(t *testing.T) {
	s := NewSparseSet(0) // defaults to 64
	expected := 64*4 + 64*4
	if got := s.MemoryUsage(); got != expected {
		t.Errorf("expected MemoryUsage()=%d, got %d", expected, got)
	}
}

// TestSparseSetsResize tests the Resize() method on SparseSets.
func TestSparseSetsResize(t *testing.T) {
	ss := NewSparseSets(10)
	ss.Set1.Insert(5)
	ss.Set2.Insert(8)

	// Resize smaller - should clear
	ss.Resize(10)
	if ss.Set1.Len() != 0 {
		t.Error("Set1 should be cleared after resize to same/smaller")
	}
	if ss.Set2.Len() != 0 {
		t.Error("Set2 should be cleared after resize to same/smaller")
	}

	// Resize larger
	ss.Resize(200)
	ss.Set1.Insert(150)
	if !ss.Set1.Contains(150) {
		t.Error("Set1 should contain 150 after resize to 200")
	}
}

// TestSparseSetsMemoryUsage tests the MemoryUsage() method on SparseSets.
func TestSparseSetsMemoryUsage(t *testing.T) {
	ss := NewSparseSets(50)
	expected := ss.Set1.MemoryUsage() + ss.Set2.MemoryUsage()
	if got := ss.MemoryUsage(); got != expected {
		t.Errorf("expected MemoryUsage()=%d, got %d", expected, got)
	}
}

// TestSparseSetsClear tests Clear() on SparseSets.
func TestSparseSetsClear(t *testing.T) {
	ss := NewSparseSets(20)
	ss.Set1.Insert(1)
	ss.Set1.Insert(2)
	ss.Set2.Insert(10)

	ss.Clear()
	if ss.Set1.Len() != 0 {
		t.Errorf("Set1 should be empty after Clear, got %d", ss.Set1.Len())
	}
	if ss.Set2.Len() != 0 {
		t.Errorf("Set2 should be empty after Clear, got %d", ss.Set2.Len())
	}
}

// TestSparseSetResizeGrow tests Resize() when growing the set.
func TestSparseSetResizeGrow(t *testing.T) {
	s := NewSparseSet(10)
	s.Insert(3)
	s.Insert(7)

	// Grow the set
	s.Resize(100)

	// Existing elements should survive
	if !s.Contains(3) {
		t.Error("expected 3 to survive resize")
	}
	if !s.Contains(7) {
		t.Error("expected 7 to survive resize")
	}

	// New capacity should allow larger values
	s.Insert(50)
	if !s.Contains(50) {
		t.Error("expected 50 after resize to 100")
	}
}

// TestSparseSetResizeShrink tests Resize() when shrinking clears the set.
func TestSparseSetResizeShrink(t *testing.T) {
	s := NewSparseSet(100)
	s.Insert(50)
	s.Insert(99)

	s.Resize(50) // Shrink - should clear
	if s.Len() != 0 {
		t.Errorf("expected empty set after shrink, got %d", s.Len())
	}
}

// TestSparseSetResizeZero tests Resize(0) which should default to 64.
func TestSparseSetResizeZero(t *testing.T) {
	s := NewSparseSet(10)
	s.Insert(5)
	s.Resize(0) // Should use default 64

	// Since 64 > 10, it should grow
	if s.Capacity() != 64 {
		t.Errorf("expected capacity 64, got %d", s.Capacity())
	}
}

// TestSparseSetContainsOutOfBounds tests Contains with value >= capacity.
func TestSparseSetContainsOutOfBounds(t *testing.T) {
	s := NewSparseSet(10)
	s.Insert(5)

	// Value beyond capacity should return false
	if s.Contains(10) {
		t.Error("Contains(10) should be false for capacity 10")
	}
	if s.Contains(100) {
		t.Error("Contains(100) should be false for capacity 10")
	}
}

// TestSparseSetRemoveLastElement tests removing the last element.
func TestSparseSetRemoveLastElement(t *testing.T) {
	s := NewSparseSet(10)
	s.Insert(5)

	s.Remove(5)
	if s.Len() != 0 {
		t.Errorf("expected empty set after removing last element, got %d", s.Len())
	}
	if s.Contains(5) {
		t.Error("5 should not be in set after removal")
	}
}

// TestSparseSetRemoveMiddleElement tests removing an element that isn't at the end of dense.
func TestSparseSetRemoveMiddleElement(t *testing.T) {
	s := NewSparseSet(10)
	s.Insert(1)
	s.Insert(2)
	s.Insert(3)

	s.Remove(1)
	if s.Contains(1) {
		t.Error("1 should not be in set after removal")
	}
	if !s.Contains(2) {
		t.Error("2 should still be in set")
	}
	if !s.Contains(3) {
		t.Error("3 should still be in set")
	}
	if s.Len() != 2 {
		t.Errorf("expected Len=2, got %d", s.Len())
	}
}

// TestSparseSetRemoveNonExistent tests removing a value that is not in the set.
func TestSparseSetRemoveNonExistent(t *testing.T) {
	s := NewSparseSet(10)
	s.Insert(5)

	s.Remove(3) // Not in set
	if s.Len() != 1 {
		t.Errorf("expected Len=1, got %d", s.Len())
	}
}
