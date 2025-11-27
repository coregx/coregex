package prefilter

// Tracker wraps a Prefilter with effectiveness tracking.
//
// The tracker monitors the ratio of confirmed matches to candidates found.
// When effectiveness drops below a threshold (too many false positives),
// the prefilter is automatically disabled to prevent catastrophic slowdown.
//
// This implements the rust-regex pattern where prefilters can be "retired"
// when they become ineffective for certain inputs.
//
// Algorithm:
//  1. Track candidates (prefilter finds) and confirms (actual matches)
//  2. Every N candidates, check effectiveness ratio
//  3. If ratio < threshold, disable prefilter
//  4. Once disabled, never re-enable (for this search)
//
// Example usage:
//
//	pf := prefilter.NewBuilder(prefixes, nil).Build()
//	tracker := prefilter.NewTracker(pf)
//	for {
//	    if !tracker.IsActive() {
//	        // Prefilter disabled, use full scan
//	        break
//	    }
//	    pos := tracker.Find(haystack, start)
//	    if pos == -1 {
//	        break
//	    }
//	    if fullRegexMatches(haystack, pos) {
//	        tracker.ConfirmMatch()
//	        return pos
//	    }
//	    start = pos + 1
//	}
type Tracker struct {
	inner Prefilter

	// Statistics
	candidates uint64 // Total candidate positions found
	confirms   uint64 // Confirmed matches (actual regex matches)

	// Configuration
	checkInterval  uint64  // Check effectiveness every N candidates
	minEfficiency  float64 // Minimum required efficiency (0.0 to 1.0)
	warmupPeriod   uint64  // Don't disable until this many candidates
	lastCheckpoint uint64  // Candidates at last checkpoint

	// State
	active bool // Whether prefilter is still active
}

// TrackerConfig holds configuration for the effectiveness tracker.
type TrackerConfig struct {
	// CheckInterval is how often to check effectiveness (in candidates).
	// Default: 64
	CheckInterval uint64

	// MinEfficiency is the minimum acceptable ratio of confirms/candidates.
	// If efficiency drops below this, prefilter is disabled.
	// Default: 0.1 (10%)
	MinEfficiency float64

	// WarmupPeriod is the minimum number of candidates before checking effectiveness.
	// This prevents premature disabling on small samples.
	// Default: 128
	WarmupPeriod uint64
}

// DefaultTrackerConfig returns the default tracker configuration.
//
// Default values are tuned based on rust-regex heuristics:
//   - CheckInterval: 64 (check frequently but not every candidate)
//   - MinEfficiency: 0.1 (10% - disable if >90% false positives)
//   - WarmupPeriod: 128 (need enough samples for statistical significance)
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		CheckInterval: 64,
		MinEfficiency: 0.1,
		WarmupPeriod:  128,
	}
}

// NewTracker creates a new tracker for the given prefilter with default config.
//
// Returns nil if the inner prefilter is nil.
func NewTracker(inner Prefilter) *Tracker {
	return NewTrackerWithConfig(inner, DefaultTrackerConfig())
}

// NewTrackerWithConfig creates a new tracker with custom configuration.
//
// Returns nil if the inner prefilter is nil.
func NewTrackerWithConfig(inner Prefilter, config TrackerConfig) *Tracker {
	if inner == nil {
		return nil
	}

	return &Tracker{
		inner:         inner,
		checkInterval: config.CheckInterval,
		minEfficiency: config.MinEfficiency,
		warmupPeriod:  config.WarmupPeriod,
		active:        true,
	}
}

// Find returns the next candidate position, or -1 if none found or disabled.
//
// This method:
//  1. Checks if prefilter is still active
//  2. Calls inner prefilter's Find
//  3. Increments candidate counter if found
//  4. Checks effectiveness at intervals
//
// Unlike the inner prefilter, this may return -1 even when candidates exist
// if the prefilter has been disabled due to low effectiveness.
func (t *Tracker) Find(haystack []byte, start int) int {
	if !t.active {
		return -1
	}

	pos := t.inner.Find(haystack, start)
	if pos >= 0 {
		t.candidates++
		t.checkEffectiveness()
	}
	return pos
}

// ConfirmMatch should be called when a candidate actually matches.
//
// This increments the confirm counter, improving the effectiveness ratio.
// Always call this after verifying a candidate is a real match.
func (t *Tracker) ConfirmMatch() {
	t.confirms++
}

// IsActive returns true if the prefilter is still being used.
//
// When false, the caller should fall back to full regex search
// instead of using the prefilter.
func (t *Tracker) IsActive() bool {
	return t.active
}

// IsComplete delegates to the inner prefilter's IsComplete.
func (t *Tracker) IsComplete() bool {
	return t.inner.IsComplete()
}

// HeapBytes returns the memory used by the inner prefilter.
func (t *Tracker) HeapBytes() int {
	return t.inner.HeapBytes()
}

// Stats returns the current tracking statistics.
//
// Returns (candidates, confirms, efficiency, active).
func (t *Tracker) Stats() (candidates, confirms uint64, efficiency float64, active bool) {
	candidates = t.candidates
	confirms = t.confirms
	if candidates > 0 {
		efficiency = float64(confirms) / float64(candidates)
	}
	active = t.active
	return
}

// Reset clears statistics and re-enables the prefilter.
//
// Useful for reusing the tracker across multiple searches.
func (t *Tracker) Reset() {
	t.candidates = 0
	t.confirms = 0
	t.lastCheckpoint = 0
	t.active = true
}

// Inner returns the underlying prefilter.
//
// Useful for accessing type-specific methods or bypassing tracking.
func (t *Tracker) Inner() Prefilter {
	return t.inner
}

// checkEffectiveness evaluates whether to disable the prefilter.
//
// Called after each candidate is found. Only performs the actual check
// at configured intervals to minimize overhead.
func (t *Tracker) checkEffectiveness() {
	// Still in warmup period
	if t.candidates < t.warmupPeriod {
		return
	}

	// Check at intervals
	if t.candidates-t.lastCheckpoint < t.checkInterval {
		return
	}
	t.lastCheckpoint = t.candidates

	// Calculate efficiency
	efficiency := float64(t.confirms) / float64(t.candidates)

	// Disable if below threshold
	if efficiency < t.minEfficiency {
		t.active = false
	}
}

// TrackedPrefilter wraps a Prefilter to implement the Prefilter interface
// while providing tracking capabilities.
//
// This allows using a tracked prefilter anywhere a regular Prefilter is expected.
type TrackedPrefilter struct {
	*Tracker
}

// WrapWithTracking creates a Prefilter that includes effectiveness tracking.
//
// The returned prefilter can be used anywhere a regular Prefilter is expected,
// but also supports tracking methods via type assertion:
//
//	pf := prefilter.WrapWithTracking(innerPf)
//	// Use as regular prefilter
//	pos := pf.Find(haystack, 0)
//	// Access tracking via type assertion
//	if tracked, ok := pf.(*prefilter.TrackedPrefilter); ok {
//	    tracked.ConfirmMatch()
//	    _, _, eff, _ := tracked.Stats()
//	}
func WrapWithTracking(inner Prefilter) Prefilter {
	if inner == nil {
		return nil
	}
	return &TrackedPrefilter{
		Tracker: NewTracker(inner),
	}
}

// Find implements Prefilter.Find with tracking.
func (tp *TrackedPrefilter) Find(haystack []byte, start int) int {
	return tp.Tracker.Find(haystack, start)
}

// IsComplete implements Prefilter.IsComplete.
func (tp *TrackedPrefilter) IsComplete() bool {
	return tp.Tracker.IsComplete()
}

// HeapBytes implements Prefilter.HeapBytes.
func (tp *TrackedPrefilter) HeapBytes() int {
	return tp.Tracker.HeapBytes()
}
