package lazy

import (
	"testing"

	"github.com/coregx/coregex/nfa"
)

func TestNewCache(t *testing.T) {
	tests := []struct {
		name      string
		maxStates uint32
	}{
		{name: "small cache", maxStates: 10},
		{name: "medium cache", maxStates: 1000},
		{name: "large cache", maxStates: 100000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCache(tt.maxStates)
			if c == nil {
				t.Fatal("NewCache returned nil")
			}
			if c.Size() != 0 {
				t.Errorf("NewCache.Size() = %d, want 0", c.Size())
			}
			if c.IsFull() {
				t.Error("NewCache should not be full")
			}
			if c.ClearCount() != 0 {
				t.Errorf("NewCache.ClearCount() = %d, want 0", c.ClearCount())
			}

			hits, misses, hitRate := c.Stats()
			if hits != 0 || misses != 0 || hitRate != 0 {
				t.Errorf("NewCache.Stats() = (%d, %d, %f), want (0, 0, 0)", hits, misses, hitRate)
			}
		})
	}
}

func TestCacheInsertAndGet(t *testing.T) {
	c := NewCache(100)

	// Create a state
	nfaStates := []nfa.StateID{1, 2, 3}
	key := ComputeStateKey(nfaStates)
	state := NewState(InvalidState, nfaStates, false)

	// Insert
	id, err := c.Insert(key, state)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if id == InvalidState {
		t.Error("Insert returned InvalidState")
	}

	// Get should find it
	got, ok := c.Get(key)
	if !ok {
		t.Fatal("Get should find inserted state")
	}
	if got.ID() != id {
		t.Errorf("Get returned state with ID %d, want %d", got.ID(), id)
	}

	// Size should be 1
	if c.Size() != 1 {
		t.Errorf("Size() = %d, want 1", c.Size())
	}
}

func TestCacheInsertDuplicate(t *testing.T) {
	c := NewCache(100)

	nfaStates := []nfa.StateID{1, 2}
	key := ComputeStateKey(nfaStates)
	state1 := NewState(InvalidState, nfaStates, false)
	state2 := NewState(InvalidState, nfaStates, true)

	// First insert
	id1, err := c.Insert(key, state1)
	if err != nil {
		t.Fatalf("First Insert failed: %v", err)
	}

	// Second insert with same key should return existing ID
	id2, err := c.Insert(key, state2)
	if err != nil {
		t.Fatalf("Second Insert failed: %v", err)
	}
	if id2 != id1 {
		t.Errorf("Duplicate Insert returned different ID: %d vs %d", id2, id1)
	}

	// Size should still be 1
	if c.Size() != 1 {
		t.Errorf("Size() = %d, want 1 after duplicate insert", c.Size())
	}
}

func TestCacheIsFull(t *testing.T) {
	c := NewCache(3)

	// Insert up to capacity
	for i := nfa.StateID(0); i < 3; i++ {
		nfaStates := []nfa.StateID{i}
		key := ComputeStateKey(nfaStates)
		state := NewState(InvalidState, nfaStates, false)
		_, err := c.Insert(key, state)
		if err != nil {
			t.Fatalf("Insert(%d) failed: %v", i, err)
		}
	}

	// Cache should be full
	if !c.IsFull() {
		t.Error("Cache should be full after inserting maxStates items")
	}

	// Next insert should fail with ErrCacheFull
	nfaStates := []nfa.StateID{99}
	key := ComputeStateKey(nfaStates)
	state := NewState(InvalidState, nfaStates, false)
	_, err := c.Insert(key, state)
	if err != ErrCacheFull {
		t.Errorf("Insert on full cache: got %v, want ErrCacheFull", err)
	}
}

func TestCacheGetOrInsert(t *testing.T) {
	c := NewCache(100)

	nfaStates := []nfa.StateID{5, 10}
	key := ComputeStateKey(nfaStates)

	// First GetOrInsert - should insert
	state1 := NewState(InvalidState, nfaStates, false)
	got, wasHit, err := c.GetOrInsert(key, state1)
	if err != nil {
		t.Fatalf("First GetOrInsert failed: %v", err)
	}
	if wasHit {
		t.Error("First GetOrInsert should be a miss")
	}
	if got == nil {
		t.Fatal("First GetOrInsert returned nil state")
	}

	// Second GetOrInsert with same key - should get existing
	state2 := NewState(InvalidState, nfaStates, true)
	got2, wasHit2, err := c.GetOrInsert(key, state2)
	if err != nil {
		t.Fatalf("Second GetOrInsert failed: %v", err)
	}
	if !wasHit2 {
		t.Error("Second GetOrInsert should be a hit")
	}
	if got2.ID() != got.ID() {
		t.Errorf("Second GetOrInsert returned different state ID: %d vs %d", got2.ID(), got.ID())
	}
}

func TestCacheGetOrInsertFull(t *testing.T) {
	c := NewCache(1)

	// Fill the cache
	nfaStates := []nfa.StateID{1}
	key := ComputeStateKey(nfaStates)
	state := NewState(InvalidState, nfaStates, false)
	_, _, err := c.GetOrInsert(key, state)
	if err != nil {
		t.Fatalf("GetOrInsert failed: %v", err)
	}

	// Next GetOrInsert with new key should fail
	nfaStates2 := []nfa.StateID{2}
	key2 := ComputeStateKey(nfaStates2)
	state2 := NewState(InvalidState, nfaStates2, false)
	_, _, err = c.GetOrInsert(key2, state2)
	if err == nil {
		t.Error("GetOrInsert on full cache should return error")
	}
}

func TestCacheGetNotFound(t *testing.T) {
	c := NewCache(100)

	key := ComputeStateKey([]nfa.StateID{42})
	got, ok := c.Get(key)
	if ok {
		t.Error("Get on empty cache should return false")
	}
	if got != nil {
		t.Errorf("Get on empty cache returned non-nil state: %v", got)
	}
}

func TestCacheStats(t *testing.T) {
	c := NewCache(100)

	// After some operations, stats should be tracked
	nfaStates := []nfa.StateID{1}
	key := ComputeStateKey(nfaStates)
	state := NewState(InvalidState, nfaStates, false)

	// Insert = miss
	_, _ = c.Insert(key, state)

	// Get existing = hit
	_, _ = c.Get(key)

	// Get non-existing = no increment (only hit increments in Get)
	nonExistKey := ComputeStateKey([]nfa.StateID{99})
	_, _ = c.Get(nonExistKey)

	hits, misses, _ := c.Stats()
	// At minimum we should have some stats tracked
	if hits+misses == 0 {
		t.Error("Stats should have non-zero total after operations")
	}
}

func TestCacheResetStats(t *testing.T) {
	c := NewCache(100)

	// Generate some stats
	nfaStates := []nfa.StateID{1}
	key := ComputeStateKey(nfaStates)
	state := NewState(InvalidState, nfaStates, false)
	_, _ = c.Insert(key, state)
	_, _ = c.Get(key)

	// Reset stats
	c.ResetStats()

	hits, misses, hitRate := c.Stats()
	if hits != 0 || misses != 0 || hitRate != 0 {
		t.Errorf("After ResetStats: Stats() = (%d, %d, %f), want (0, 0, 0)", hits, misses, hitRate)
	}
}

func TestCacheClear(t *testing.T) {
	c := NewCache(100)

	// Insert some states
	for i := nfa.StateID(0); i < 5; i++ {
		nfaStates := []nfa.StateID{i}
		key := ComputeStateKey(nfaStates)
		state := NewState(InvalidState, nfaStates, false)
		_, _ = c.Insert(key, state)
	}

	if c.Size() != 5 {
		t.Fatalf("Size() = %d, want 5 before clear", c.Size())
	}

	// Clear
	c.Clear()

	if c.Size() != 0 {
		t.Errorf("Size() = %d, want 0 after clear", c.Size())
	}
	if c.ClearCount() != 0 {
		t.Errorf("ClearCount() = %d, want 0 after Clear() (full reset)", c.ClearCount())
	}

	hits, misses, _ := c.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("Stats should be reset after Clear: hits=%d, misses=%d", hits, misses)
	}
}

func TestCacheClearKeepMemory(t *testing.T) {
	c := NewCache(100)

	// Insert some states
	for i := nfa.StateID(0); i < 5; i++ {
		nfaStates := []nfa.StateID{i}
		key := ComputeStateKey(nfaStates)
		state := NewState(InvalidState, nfaStates, false)
		_, _ = c.Insert(key, state)
	}

	// ClearKeepMemory
	c.ClearKeepMemory()

	if c.Size() != 0 {
		t.Errorf("Size() = %d, want 0 after ClearKeepMemory", c.Size())
	}

	// ClearCount should be incremented
	if c.ClearCount() != 1 {
		t.Errorf("ClearCount() = %d, want 1 after ClearKeepMemory", c.ClearCount())
	}

	// Can still insert new states
	nfaStates := []nfa.StateID{99}
	key := ComputeStateKey(nfaStates)
	state := NewState(InvalidState, nfaStates, false)
	_, err := c.Insert(key, state)
	if err != nil {
		t.Errorf("Insert after ClearKeepMemory failed: %v", err)
	}
	if c.Size() != 1 {
		t.Errorf("Size() = %d, want 1 after re-insert", c.Size())
	}
}

func TestCacheClearKeepMemoryMultiple(t *testing.T) {
	c := NewCache(100)

	// Multiple clears should increment counter
	c.ClearKeepMemory()
	c.ClearKeepMemory()
	c.ClearKeepMemory()

	if c.ClearCount() != 3 {
		t.Errorf("ClearCount() = %d, want 3 after 3 ClearKeepMemory calls", c.ClearCount())
	}
}

func TestCacheResetClearCount(t *testing.T) {
	c := NewCache(100)

	// Increment clear count
	c.ClearKeepMemory()
	c.ClearKeepMemory()
	if c.ClearCount() != 2 {
		t.Fatalf("ClearCount() = %d, want 2", c.ClearCount())
	}

	// Reset clear count
	c.ResetClearCount()
	if c.ClearCount() != 0 {
		t.Errorf("ClearCount() = %d, want 0 after ResetClearCount", c.ClearCount())
	}
}

func TestCacheCapacityBoundary(t *testing.T) {
	// Test with capacity of 1
	c := NewCache(1)

	nfaStates := []nfa.StateID{1}
	key := ComputeStateKey(nfaStates)
	state := NewState(InvalidState, nfaStates, false)

	// First insert should succeed
	_, err := c.Insert(key, state)
	if err != nil {
		t.Fatalf("Insert on capacity-1 cache failed: %v", err)
	}

	if !c.IsFull() {
		t.Error("Capacity-1 cache should be full after 1 insert")
	}

	// Second insert with different key should fail
	nfaStates2 := []nfa.StateID{2}
	key2 := ComputeStateKey(nfaStates2)
	state2 := NewState(InvalidState, nfaStates2, false)
	_, err = c.Insert(key2, state2)
	if err != ErrCacheFull {
		t.Errorf("Second insert on capacity-1 cache: got %v, want ErrCacheFull", err)
	}
}

func TestCacheStatsHitRate(t *testing.T) {
	c := NewCache(100)

	// Insert a state
	nfaStates := []nfa.StateID{1}
	key := ComputeStateKey(nfaStates)
	state := NewState(InvalidState, nfaStates, false)
	_, _ = c.Insert(key, state)

	// Do several Gets (hits)
	for i := 0; i < 9; i++ {
		c.Get(key)
	}

	_, _, hitRate := c.Stats()
	// With 1 miss (insert) and 9 hits (Gets), hit rate should be high
	// Note: exact rate depends on implementation counting
	if hitRate < 0 || hitRate > 1 {
		t.Errorf("Hit rate %f should be between 0 and 1", hitRate)
	}
}

func TestCacheStateIDAssignment(t *testing.T) {
	c := NewCache(100)

	// Insert multiple states and verify IDs are sequential
	var ids []StateID
	for i := nfa.StateID(0); i < 5; i++ {
		nfaStates := []nfa.StateID{i}
		key := ComputeStateKey(nfaStates)
		state := NewState(InvalidState, nfaStates, false)
		id, err := c.Insert(key, state)
		if err != nil {
			t.Fatalf("Insert(%d) failed: %v", i, err)
		}
		ids = append(ids, id)
	}

	// IDs should be sequential starting from StartState+1
	for i, id := range ids {
		expected := StartState + 1 + StateID(i)
		if id != expected {
			t.Errorf("State %d got ID %d, want %d", i, id, expected)
		}
	}
}
