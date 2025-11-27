package lazy

// Config configures the behavior of the Lazy DFA engine.
//
// The configuration allows tuning the trade-off between memory usage and
// performance. Larger caches provide better hit rates but consume more memory.
type Config struct {
	// MaxStates is the maximum number of DFA states to cache.
	// When this limit is reached, the DFA falls back to NFA execution.
	//
	// Default: 10,000 states (~1MB with 256-byte transition tables)
	// Memory usage: ~100-200 bytes per state (depending on transitions)
	//
	// Tuning guidelines:
	//   - Simple patterns: 100-1,000 states sufficient
	//   - Complex patterns: 10,000-100,000 states
	//   - Memory-constrained: 1,000 states (~100KB)
	MaxStates uint32

	// CacheHitThreshold is the minimum cache hit rate (0.0-1.0) to continue
	// using DFA. If hit rate falls below this, fall back to NFA.
	//
	// Default: 0.0 (disabled - always use DFA until cache full)
	//
	// This prevents thrashing when the working set exceeds cache size.
	// Set to 0.5-0.7 for adaptive fallback.
	CacheHitThreshold float64

	// UsePrefilter enables prefilter-based candidate search.
	// When true, the DFA will use extracted literals to find candidates
	// before running the full DFA.
	//
	// Default: true (highly recommended - provides 10-100x speedup)
	UsePrefilter bool

	// MinPrefilterLen is the minimum literal length to use prefilter.
	// Literals shorter than this are ignored for prefiltering.
	//
	// Default: 3 bytes
	// Rationale: Short literals have high false positive rate
	MinPrefilterLen int

	// DeterminizationLimit is the maximum number of NFA states in a single
	// DFA state before giving up on determinization.
	//
	// Default: 1,000 NFA states
	//
	// This prevents exponential blowup for patterns like (a|b)*c.
	// When exceeded, fall back to NFA for that transition.
	DeterminizationLimit int
}

// DefaultConfig returns a configuration with sensible defaults.
//
// These defaults are tuned for general-purpose regex matching:
//   - Balance memory usage (~1MB) with performance
//   - Enable prefilter for maximum speedup
//   - Prevent exponential state explosion
//
// For specific use cases, tune the parameters:
//   - Memory-constrained: reduce MaxStates to 1,000
//   - Performance-critical: increase MaxStates to 100,000
//   - Complex patterns: increase DeterminizationLimit
func DefaultConfig() Config {
	return Config{
		MaxStates:            10_000,
		CacheHitThreshold:    0.0, // Disabled by default
		UsePrefilter:         true,
		MinPrefilterLen:      3,
		DeterminizationLimit: 1_000,
	}
}

// Validate checks if the configuration is valid.
// Returns an error if any parameter is out of acceptable range.
func (c *Config) Validate() error {
	if c.MaxStates == 0 {
		return &DFAError{
			Kind:    InvalidConfig,
			Message: "MaxStates must be > 0",
		}
	}

	if c.CacheHitThreshold < 0.0 || c.CacheHitThreshold > 1.0 {
		return &DFAError{
			Kind:    InvalidConfig,
			Message: "CacheHitThreshold must be in range [0.0, 1.0]",
		}
	}

	if c.MinPrefilterLen < 0 {
		return &DFAError{
			Kind:    InvalidConfig,
			Message: "MinPrefilterLen must be >= 0",
		}
	}

	if c.DeterminizationLimit <= 0 {
		return &DFAError{
			Kind:    InvalidConfig,
			Message: "DeterminizationLimit must be > 0",
		}
	}

	return nil
}

// WithMaxStates returns a new config with the specified max states
func (c Config) WithMaxStates(maxStates uint32) Config {
	c.MaxStates = maxStates
	return c
}

// WithCacheHitThreshold returns a new config with the specified cache hit threshold
func (c Config) WithCacheHitThreshold(threshold float64) Config {
	c.CacheHitThreshold = threshold
	return c
}

// WithPrefilter returns a new config with prefilter enabled/disabled
func (c Config) WithPrefilter(enabled bool) Config {
	c.UsePrefilter = enabled
	return c
}

// WithMinPrefilterLen returns a new config with the specified min prefilter length
func (c Config) WithMinPrefilterLen(minLen int) Config {
	c.MinPrefilterLen = minLen
	return c
}

// WithDeterminizationLimit returns a new config with the specified limit
func (c Config) WithDeterminizationLimit(limit int) Config {
	c.DeterminizationLimit = limit
	return c
}
