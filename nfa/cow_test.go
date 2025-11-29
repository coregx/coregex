package nfa

import (
	"testing"
)

// TestCOWCaptures_RefCounting verifies correct reference counting behavior
func TestCOWCaptures_RefCounting(t *testing.T) {
	tests := []struct {
		name string
		ops  func(t *testing.T)
	}{
		{
			name: "clone increments ref count",
			ops: func(t *testing.T) {
				c1 := cowCaptures{
					shared: &sharedCaptures{
						data: []int{0, 1, 2},
						refs: 1,
					},
				}
				c2 := c1.clone()
				if c1.shared.refs != 2 {
					t.Errorf("clone() didn't increment refs: got %d, want 2", c1.shared.refs)
				}
				if c2.shared.refs != 2 {
					t.Errorf("clone() result has wrong refs: got %d, want 2", c2.shared.refs)
				}
				if c1.shared != c2.shared {
					t.Error("clone() didn't share underlying data")
				}
			},
		},
		{
			name: "clone of nil is safe",
			ops: func(t *testing.T) {
				c1 := cowCaptures{}
				c2 := c1.clone()
				if c2.shared != nil {
					t.Error("clone() of nil should return nil")
				}
			},
		},
		{
			name: "update with refs=1 modifies in place",
			ops: func(t *testing.T) {
				c := cowCaptures{
					shared: &sharedCaptures{
						data: []int{0, 1, 2},
						refs: 1,
					},
				}
				oldPtr := c.shared
				c2 := c.update(1, 99)
				if c2.shared != oldPtr {
					t.Error("update() with refs=1 should modify in place, not copy")
				}
				if c2.shared.data[1] != 99 {
					t.Errorf("update() didn't modify value: got %d, want 99", c2.shared.data[1])
				}
				if c2.shared.refs != 1 {
					t.Errorf("update() in-place changed refs: got %d, want 1", c2.shared.refs)
				}
			},
		},
		{
			name: "update with refs>1 creates copy",
			ops: func(t *testing.T) {
				c1 := cowCaptures{
					shared: &sharedCaptures{
						data: []int{0, 1, 2},
						refs: 2,
					},
				}
				oldPtr := c1.shared
				c2 := c1.update(1, 99)

				// c1 should still point to old data with decremented refs
				if c1.shared.refs != 1 {
					t.Errorf("update() didn't decrement old refs: got %d, want 1", c1.shared.refs)
				}
				if c1.shared.data[1] != 1 {
					t.Error("update() modified shared data")
				}

				// c2 should point to new data
				if c2.shared == oldPtr {
					t.Error("update() with refs>1 didn't create copy")
				}
				if c2.shared.refs != 1 {
					t.Errorf("update() new copy has wrong refs: got %d, want 1", c2.shared.refs)
				}
				if c2.shared.data[1] != 99 {
					t.Errorf("update() new copy has wrong value: got %d, want 99", c2.shared.data[1])
				}
				// Verify other values copied correctly
				if c2.shared.data[0] != 0 || c2.shared.data[2] != 2 {
					t.Error("update() didn't copy all values correctly")
				}
			},
		},
		{
			name: "update on nil is safe",
			ops: func(t *testing.T) {
				c := cowCaptures{}
				c2 := c.update(0, 99)
				if c2.shared != nil {
					t.Error("update() on nil should return nil")
				}
			},
		},
		{
			name: "update out of bounds is safe",
			ops: func(t *testing.T) {
				c := cowCaptures{
					shared: &sharedCaptures{
						data: []int{0, 1},
						refs: 1,
					},
				}
				c2 := c.update(5, 99)
				if c2.shared.data[0] != 0 || c2.shared.data[1] != 1 {
					t.Error("update() out of bounds modified data")
				}
			},
		},
		{
			name: "get on nil returns nil",
			ops: func(t *testing.T) {
				c := cowCaptures{}
				if c.get() != nil {
					t.Error("get() on nil should return nil")
				}
			},
		},
		{
			name: "copyData on nil returns nil",
			ops: func(t *testing.T) {
				c := cowCaptures{}
				if c.copyData() != nil {
					t.Error("copyData() on nil should return nil")
				}
			},
		},
		{
			name: "copyData creates independent copy",
			ops: func(t *testing.T) {
				c := cowCaptures{
					shared: &sharedCaptures{
						data: []int{0, 1, 2},
						refs: 1,
					},
				}
				copied := c.copyData()
				if len(copied) != 3 {
					t.Errorf("copyData() wrong length: got %d, want 3", len(copied))
				}
				// Modify copied - should not affect original
				copied[1] = 99
				if c.shared.data[1] != 1 {
					t.Error("copyData() didn't create independent copy")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.ops(t)
		})
	}
}

// TestCOWCaptures_ThreadSplitScenario verifies COW behavior during thread splits
func TestCOWCaptures_ThreadSplitScenario(t *testing.T) {
	// Simulate thread split in PikeVM:
	// 1. Parent thread has captures
	// 2. Split creates two child threads sharing captures
	// 3. One child modifies captures (should trigger COW)
	// 4. Other child should keep original

	parent := cowCaptures{
		shared: &sharedCaptures{
			data: []int{0, 10, 20, 30},
			refs: 1,
		},
	}

	// Simulate split - both children share data
	child1 := parent.clone()
	child2 := parent.clone()

	if parent.shared.refs != 3 {
		t.Fatalf("After 2 clones, refs should be 3, got %d", parent.shared.refs)
	}

	// child1 modifies - should trigger COW
	child1 = child1.update(2, 99)

	// Verify child1 has own copy
	if child1.shared == parent.shared {
		t.Error("child1.update() should have created new copy")
	}
	if child1.shared.refs != 1 {
		t.Errorf("child1 should have refs=1, got %d", child1.shared.refs)
	}
	if child1.shared.data[2] != 99 {
		t.Errorf("child1 should have updated value 99, got %d", child1.shared.data[2])
	}

	// Verify parent and child2 still share and have correct refs
	if parent.shared.refs != 2 {
		t.Errorf("After child1 COW, parent.refs should be 2, got %d", parent.shared.refs)
	}
	if child2.shared != parent.shared {
		t.Error("child2 should still share with parent")
	}
	if parent.shared.data[2] != 20 {
		t.Error("parent data should be unchanged")
	}
	if child2.shared.data[2] != 20 {
		t.Error("child2 data should be unchanged")
	}
}

// TestPikeVM_COW_Integration verifies COW works correctly in actual PikeVM execution
func TestPikeVM_COW_Integration(t *testing.T) {
	// Test pattern with captures and alternation (causes thread splits)
	compiler := NewDefaultCompiler()
	nfa, err := compiler.Compile(`(a+)|(b+)`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	vm := NewPikeVM(nfa)
	result := vm.SearchWithCaptures([]byte("aaa"))

	if result == nil {
		t.Fatal("expected match")
	}
	if result.Start != 0 || result.End != 3 {
		t.Errorf("wrong match bounds: got [%d,%d], want [0,3]", result.Start, result.End)
	}

	// Verify captures
	if len(result.Captures) < 2 {
		t.Fatalf("expected at least 2 capture groups, got %d", len(result.Captures))
	}
	// Group 1 should match "aaa"
	if result.Captures[1] == nil {
		t.Error("capture group 1 should be set")
	} else if result.Captures[1][0] != 0 || result.Captures[1][1] != 3 {
		t.Errorf("capture group 1 wrong: got %v, want [0,3]", result.Captures[1])
	}
}

// TestPikeVM_COW_MemorySafety tests for memory leaks or corruption
func TestPikeVM_COW_MemorySafety(t *testing.T) {
	// Run multiple searches to detect memory issues
	compiler := NewDefaultCompiler()
	nfa, err := compiler.Compile(`(a+)(b+)`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	vm := NewPikeVM(nfa)

	// Run many searches - would expose memory leaks
	for i := 0; i < 1000; i++ {
		result := vm.SearchWithCaptures([]byte("aaabbb"))
		if result == nil {
			t.Fatalf("iteration %d: expected match", i)
		}
		// Verify result is consistent
		if result.Start != 0 || result.End != 6 {
			t.Errorf("iteration %d: wrong match", i)
		}
	}
}

// TestCOWCaptures_EdgeCases tests boundary conditions
func TestCOWCaptures_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "empty data slice",
			test: func(t *testing.T) {
				c := cowCaptures{
					shared: &sharedCaptures{
						data: []int{},
						refs: 1,
					},
				}
				c2 := c.clone()
				if c2.shared.refs != 2 {
					t.Error("clone() failed on empty data")
				}
				c3 := c.update(0, 99)
				if len(c3.get()) != 0 {
					t.Error("update() changed empty data length")
				}
			},
		},
		{
			name: "negative slot index",
			test: func(t *testing.T) {
				c := cowCaptures{
					shared: &sharedCaptures{
						data: []int{0, 1, 2},
						refs: 1,
					},
				}
				// Should be handled by bounds check
				c2 := c.update(-1, 99)
				// Should not panic, data should be unchanged
				if c2.shared.data[0] != 0 {
					t.Error("negative index modified data")
				}
			},
		},
		{
			name: "zero refs initial state",
			test: func(t *testing.T) {
				// This is an invalid state (refs should never be 0 in normal operation)
				// Test that we don't panic and handle gracefully
				c := cowCaptures{
					shared: &sharedCaptures{
						data: []int{0, 1},
						refs: 0, // Invalid!
					},
				}
				c2 := c.update(0, 99)
				// With refs=0, refs > 1 is false, so we modify in place
				// Data should be updated, refs stays at 0 (invalid but no crash)
				if c2.shared.data[0] != 99 {
					t.Error("update on refs=0 should still update data")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.test(t)
		})
	}
}
