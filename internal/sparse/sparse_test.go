package sparse

import (
	"testing"
)

func TestSparseSet_Basic(t *testing.T) {
	s := NewSparseSet(100)

	// Empty set
	if !s.IsEmpty() {
		t.Error("new set should be empty")
	}
	if s.Contains(0) {
		t.Error("empty set should not contain 0")
	}

	// Insert and contain
	if !s.Insert(5) {
		t.Error("first insert should return true")
	}
	if !s.Contains(5) {
		t.Error("set should contain 5 after insert")
	}
	if s.Insert(5) {
		t.Error("duplicate insert should return false")
	}
	if s.Len() != 1 {
		t.Errorf("len should be 1, got %d", s.Len())
	}

	// Multiple inserts
	s.Insert(10)
	s.Insert(3)
	s.Insert(7)
	if s.Len() != 4 {
		t.Errorf("len should be 4, got %d", s.Len())
	}

	// Clear
	s.Clear()
	if !s.IsEmpty() {
		t.Error("set should be empty after clear")
	}
	if s.Contains(5) {
		t.Error("cleared set should not contain 5")
	}
}

func TestSparseSet_InsertionOrder(t *testing.T) {
	s := NewSparseSet(100)
	s.Insert(5)
	s.Insert(2)
	s.Insert(8)
	s.Insert(1)

	expected := []uint32{5, 2, 8, 1}
	values := s.Values()
	if len(values) != len(expected) {
		t.Fatalf("expected %d values, got %d", len(expected), len(values))
	}
	for i, v := range values {
		if v != expected[i] {
			t.Errorf("at index %d: expected %d, got %d", i, expected[i], v)
		}
	}
}

func TestSparseSet_Remove(t *testing.T) {
	s := NewSparseSet(100)
	s.Insert(1)
	s.Insert(2)
	s.Insert(3)

	s.Remove(2)
	if s.Contains(2) {
		t.Error("set should not contain 2 after remove")
	}
	if s.Len() != 2 {
		t.Errorf("len should be 2 after remove, got %d", s.Len())
	}
	if !s.Contains(1) || !s.Contains(3) {
		t.Error("set should still contain 1 and 3")
	}
}

func TestSparseSet_ClearPreservesCapacity(t *testing.T) {
	s := NewSparseSet(100)
	for i := uint32(0); i < 50; i++ {
		s.Insert(i)
	}
	s.Clear()

	// Should be able to insert again without issues
	for i := uint32(0); i < 50; i++ {
		s.Insert(i)
	}
	if s.Len() != 50 {
		t.Errorf("len should be 50, got %d", s.Len())
	}
}

func TestSparseSet_CrossValidation(t *testing.T) {
	// Test that garbage values in sparse don't cause false positives
	s := NewSparseSet(100)
	s.Insert(5)
	s.Insert(10)
	s.Clear()

	// After clear, contains should return false even though
	// sparse[5] and sparse[10] still have old values
	if s.Contains(5) || s.Contains(10) {
		t.Error("cleared set should not contain old values")
	}

	// Insert new values
	s.Insert(3)
	if !s.Contains(3) {
		t.Error("should contain 3")
	}
	if s.Contains(5) || s.Contains(10) {
		t.Error("should not contain old values")
	}
}

func TestSparseSet_Resize(t *testing.T) {
	s := NewSparseSet(10)
	s.Insert(5)
	s.Insert(7)

	// Grow
	s.Resize(100)
	if s.Capacity() != 100 {
		t.Errorf("capacity should be 100, got %d", s.Capacity())
	}
	if !s.Contains(5) || !s.Contains(7) {
		t.Error("should preserve elements after grow")
	}

	// Shrink clears
	s.Resize(50)
	if s.Len() != 0 {
		t.Errorf("shrink should clear, len=%d", s.Len())
	}
}

func TestSparseSet_Clone(t *testing.T) {
	s := NewSparseSet(100)
	s.Insert(1)
	s.Insert(2)
	s.Insert(3)

	clone := s.Clone()
	if clone.Len() != s.Len() {
		t.Error("clone should have same length")
	}
	for _, v := range s.Values() {
		if !clone.Contains(v) {
			t.Errorf("clone should contain %d", v)
		}
	}

	// Modify clone shouldn't affect original
	clone.Insert(99)
	if s.Contains(99) {
		t.Error("modifying clone should not affect original")
	}
}

func TestSparseSets_Swap(t *testing.T) {
	ss := NewSparseSets(100)
	ss.Set1.Insert(1)
	ss.Set1.Insert(2)
	ss.Set2.Insert(10)

	ss.Swap()

	if !ss.Set1.Contains(10) {
		t.Error("after swap, Set1 should contain 10")
	}
	if !ss.Set2.Contains(1) || !ss.Set2.Contains(2) {
		t.Error("after swap, Set2 should contain 1 and 2")
	}
}

func BenchmarkSparseSet_Insert(b *testing.B) {
	s := NewSparseSet(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Clear()
		for j := uint32(0); j < 100; j++ {
			s.Insert(j)
		}
	}
}

func BenchmarkSparseSet_Contains(b *testing.B) {
	s := NewSparseSet(1000)
	for j := uint32(0); j < 100; j++ {
		s.Insert(j)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := uint32(0); j < 100; j++ {
			s.Contains(j)
		}
	}
}

func BenchmarkSparseSet_Clear(b *testing.B) {
	s := NewSparseSet(1000)
	for j := uint32(0); j < 1000; j++ {
		s.Insert(j)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Clear()
		s.Insert(0) // Re-add one element so Clear has work to "undo"
	}
}
