package prefilter

import (
	"testing"
)

// mockPrefilter is a simple prefilter that returns positions from a predefined list.
type mockPrefilter struct {
	positions []int
	idx       int
	complete  bool
}

func (m *mockPrefilter) Find(haystack []byte, start int) int {
	for m.idx < len(m.positions) {
		pos := m.positions[m.idx]
		m.idx++
		if pos >= start {
			return pos
		}
	}
	return -1
}

func (m *mockPrefilter) IsComplete() bool { return m.complete }
func (m *mockPrefilter) LiteralLen() int  { return 0 }
func (m *mockPrefilter) HeapBytes() int   { return 0 }
func (m *mockPrefilter) Reset()           { m.idx = 0 }

func TestTrackerBasic(t *testing.T) {
	mock := &mockPrefilter{positions: []int{5, 10, 15, 20}}
	tracker := NewTracker(mock)

	// Should be active initially
	if !tracker.IsActive() {
		t.Error("Tracker should be active initially")
	}

	// Find first candidate
	pos := tracker.Find([]byte("test input"), 0)
	if pos != 5 {
		t.Errorf("Find() = %d, want 5", pos)
	}

	// Check stats
	candidates, confirms, _, active := tracker.Stats()
	if candidates != 1 {
		t.Errorf("candidates = %d, want 1", candidates)
	}
	if confirms != 0 {
		t.Errorf("confirms = %d, want 0", confirms)
	}
	if !active {
		t.Error("Should still be active")
	}

	// Confirm the match
	tracker.ConfirmMatch()
	_, confirms, eff, _ := tracker.Stats()
	if confirms != 1 {
		t.Errorf("confirms = %d, want 1", confirms)
	}
	if eff != 1.0 {
		t.Errorf("efficiency = %f, want 1.0", eff)
	}
}

func TestTrackerDisablesOnLowEfficiency(t *testing.T) {
	// Create many positions (more than warmup period)
	positions := make([]int, 200)
	for i := range positions {
		positions[i] = i
	}

	mock := &mockPrefilter{positions: positions}
	config := TrackerConfig{
		CheckInterval: 10,
		MinEfficiency: 0.1, // 10% minimum
		WarmupPeriod:  50,
	}
	tracker := NewTrackerWithConfig(mock, config)

	haystack := make([]byte, 300)

	// Find candidates without confirming - should eventually disable
	for i := 0; i < 200; i++ {
		pos := tracker.Find(haystack, i)
		if pos == -1 {
			break
		}
		// Don't confirm any matches - 0% efficiency
	}

	// Should be disabled now
	if tracker.IsActive() {
		t.Error("Tracker should be disabled after 0% efficiency")
	}

	// Find should return -1 when disabled
	if pos := tracker.Find(haystack, 0); pos != -1 {
		t.Errorf("Find() = %d when disabled, want -1", pos)
	}
}

func TestTrackerStaysActiveOnHighEfficiency(t *testing.T) {
	positions := make([]int, 200)
	for i := range positions {
		positions[i] = i
	}

	mock := &mockPrefilter{positions: positions}
	config := TrackerConfig{
		CheckInterval: 10,
		MinEfficiency: 0.1,
		WarmupPeriod:  50,
	}
	tracker := NewTrackerWithConfig(mock, config)

	haystack := make([]byte, 300)

	// Find candidates and confirm most of them (50% efficiency)
	for i := 0; i < 200; i++ {
		pos := tracker.Find(haystack, i)
		if pos == -1 {
			break
		}
		// Confirm 50% of matches
		if i%2 == 0 {
			tracker.ConfirmMatch()
		}
	}

	// Should still be active (50% > 10%)
	if !tracker.IsActive() {
		t.Error("Tracker should still be active with 50% efficiency")
	}
}

func TestTrackerWarmupPeriod(t *testing.T) {
	positions := make([]int, 100)
	for i := range positions {
		positions[i] = i
	}

	mock := &mockPrefilter{positions: positions}
	config := TrackerConfig{
		CheckInterval: 1, // Check every candidate
		MinEfficiency: 0.5,
		WarmupPeriod:  50, // Don't check until 50 candidates
	}
	tracker := NewTrackerWithConfig(mock, config)

	haystack := make([]byte, 200)

	// Find 40 candidates without confirming - should NOT disable (warmup)
	for i := 0; i < 40; i++ {
		tracker.Find(haystack, i)
		// Don't confirm - 0% efficiency
	}

	// Should still be active (in warmup period)
	if !tracker.IsActive() {
		t.Error("Tracker should still be active during warmup")
	}

	// Find more candidates to exceed warmup
	for i := 40; i < 100; i++ {
		tracker.Find(haystack, i)
	}

	// Now should be disabled
	if tracker.IsActive() {
		t.Error("Tracker should be disabled after warmup with 0% efficiency")
	}
}

func TestTrackerReset(t *testing.T) {
	positions := make([]int, 200)
	for i := range positions {
		positions[i] = i
	}

	mock := &mockPrefilter{positions: positions}
	config := TrackerConfig{
		CheckInterval: 10,
		MinEfficiency: 0.5,
		WarmupPeriod:  50,
	}
	tracker := NewTrackerWithConfig(mock, config)

	haystack := make([]byte, 300)

	// Disable the tracker
	for i := 0; i < 200; i++ {
		tracker.Find(haystack, i)
	}

	if tracker.IsActive() {
		t.Error("Tracker should be disabled")
	}

	// Reset
	tracker.Reset()
	mock.Reset()

	// Should be active again
	if !tracker.IsActive() {
		t.Error("Tracker should be active after reset")
	}

	candidates, confirms, _, _ := tracker.Stats()
	if candidates != 0 || confirms != 0 {
		t.Errorf("Stats should be zero after reset: candidates=%d, confirms=%d", candidates, confirms)
	}
}

func TestTrackerNilPrefilter(t *testing.T) {
	tracker := NewTracker(nil)
	if tracker != nil {
		t.Error("NewTracker(nil) should return nil")
	}

	wrapped := WrapWithTracking(nil)
	if wrapped != nil {
		t.Error("WrapWithTracking(nil) should return nil")
	}
}

func TestTrackedPrefilterInterface(t *testing.T) {
	mock := &mockPrefilter{positions: []int{5, 10, 15}, complete: true}
	tracked := WrapWithTracking(mock)

	// Should implement Prefilter interface
	var _ Prefilter = tracked //nolint:staticcheck // explicit interface check

	// IsComplete should delegate
	if !tracked.IsComplete() {
		t.Error("IsComplete should delegate to inner")
	}

	// HeapBytes should delegate
	if tracked.HeapBytes() != 0 {
		t.Errorf("HeapBytes = %d, want 0", tracked.HeapBytes())
	}

	// Find should work
	pos := tracked.Find([]byte("test"), 0)
	if pos != 5 {
		t.Errorf("Find() = %d, want 5", pos)
	}

	// Type assertion to access tracking
	if tp, ok := tracked.(*TrackedPrefilter); ok {
		tp.ConfirmMatch()
		candidates, confirms, _, _ := tp.Stats()
		if candidates != 1 || confirms != 1 {
			t.Errorf("Stats = (%d, %d), want (1, 1)", candidates, confirms)
		}
	} else {
		t.Error("Should be able to type assert to *TrackedPrefilter")
	}
}

func TestDefaultTrackerConfig(t *testing.T) {
	config := DefaultTrackerConfig()

	if config.CheckInterval == 0 {
		t.Error("CheckInterval should not be 0")
	}
	if config.MinEfficiency <= 0 || config.MinEfficiency >= 1 {
		t.Errorf("MinEfficiency = %f, should be between 0 and 1", config.MinEfficiency)
	}
	if config.WarmupPeriod == 0 {
		t.Error("WarmupPeriod should not be 0")
	}
}

func TestTrackerInner(t *testing.T) {
	mock := &mockPrefilter{positions: []int{1, 2, 3}}
	tracker := NewTracker(mock)

	inner := tracker.Inner()
	if inner != mock {
		t.Error("Inner() should return the wrapped prefilter")
	}
}

func BenchmarkTrackerOverhead(b *testing.B) {
	// Create a real prefilter
	pf := newMemchrPrefilter('x', false)
	tracker := NewTracker(pf)

	haystack := make([]byte, 1000)
	for i := range haystack {
		haystack[i] = 'a'
	}
	// Place some 'x' to find
	haystack[100] = 'x'
	haystack[500] = 'x'
	haystack[900] = 'x'

	b.Run("direct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pf.Find(haystack, 0)
		}
	})

	b.Run("tracked", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tracker.Find(haystack, 0)
			tracker.ConfirmMatch() // Reset tracking
		}
	})
}
