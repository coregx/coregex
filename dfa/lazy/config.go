package lazy

// Config configures the behavior of the Lazy DFA engine.
//
// The configuration allows tuning the trade-off between memory usage and
// performance. Larger caches provide better hit rates but consume more memory.
type Config struct {
	// CacheCapacityBytes is the maximum memory (in bytes) that the DFA cache
	// may use for transition tables, state storage, and metadata.
	//
	// When MemoryUsage() exceeds this limit, the cache is considered full.
	// The DFA will then clear the cache (up to MaxCacheClears times) and
	// rebuild states on demand, or fall back to NFA.
	//
	// Default: 2MB (2 * 1024 * 1024), matching Rust regex's hybrid_cache_capacity.
	//
	// Tuning guidelines:
	//   - Simple patterns: 256KB-1MB sufficient
	//   - Complex patterns or large alphabets: 2MB-10MB
	//   - Memory-constrained environments: 256KB
	//   - Performance-critical with complex patterns: 10MB-100MB
	CacheCapacityBytes int

	// MaxStates is a legacy limit on the number of DFA states.
	// When CacheCapacityBytes > 0, MaxStates is ignored.
	// When CacheCapacityBytes == 0 and MaxStates > 0,
	// an approximate byte limit is computed from MaxStates.
	//
	// Deprecated: Use CacheCapacityBytes instead.
	MaxStates uint32

	// MaxCacheClears is the maximum number of times the DFA cache can be
	// cleared and rebuilt during a single search before falling back to NFA.
	//
	// When the cache fills up during determinization, instead of immediately
	// falling back to the NFA (PikeVM), the DFA clears the cache, re-creates
	// the start state, and continues searching from the current position.
	// This is much faster than NFA fallback for large inputs with complex
	// patterns that generate many DFA states.
	//
	// After MaxCacheClears clears, the DFA gives up and falls back to NFA
	// permanently. This prevents pathological cases where the cache thrashes
	// endlessly (clearing and refilling every few bytes).
	//
	// Default: 5
	// Set to 0 to disable cache clearing (always fall back to NFA on full cache).
	//
	// Inspired by Rust regex-automata's hybrid DFA cache clearing strategy.
	MaxCacheClears int

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

	// BreakAtMatch controls whether determinize uses Rust-style break-at-match
	// semantics. When true (default), determinize stops iterating NFA states at
	// the first Match state, preventing prefix restarts and giving leftmost-first
	// match semantics.
	//
	// Set to false for REVERSE DFAs, where the search must continue past matches
	// to find the leftmost match start. Reverse DFAs are always anchored (no prefix),
	// so break-at-match would only cut off greedy continuation states.
	//
	// Default: true
	BreakAtMatch bool
}

// DefaultCacheCapacity is the default DFA cache capacity in bytes.
// Matches Rust regex's hybrid_cache_capacity: 2 * (1 << 20) = 2MB.
const DefaultCacheCapacity = 2 * 1024 * 1024

// DefaultConfig returns a configuration with sensible defaults.
//
// These defaults are tuned for general-purpose regex matching:
//   - Cache capacity: 2MB (matches Rust regex default)
//   - Enable prefilter for maximum speedup
//   - Prevent exponential state explosion
//
// For specific use cases, tune the parameters:
//   - Memory-constrained: reduce CacheCapacityBytes to 256KB
//   - Performance-critical: increase CacheCapacityBytes to 10MB
//   - Complex patterns: increase DeterminizationLimit
func DefaultConfig() Config {
	return Config{
		CacheCapacityBytes:   DefaultCacheCapacity,
		MaxCacheClears:       5,   // Allow 5 cache clears before NFA fallback
		CacheHitThreshold:    0.0, // Disabled by default
		UsePrefilter:         true,
		MinPrefilterLen:      3,
		DeterminizationLimit: 1_000,
		BreakAtMatch:         true,
	}
}

// effectiveCapacityBytes returns the cache capacity in bytes.
// Uses CacheCapacityBytes if set, otherwise derives from legacy MaxStates.
func (c *Config) effectiveCapacityBytes() int {
	if c.CacheCapacityBytes > 0 {
		return c.CacheCapacityBytes
	}
	if c.MaxStates > 0 {
		// Legacy: approximate bytes from state count.
		// Each state uses ~100 bytes (flatTrans row + map entry + State struct).
		return int(c.MaxStates) * 100
	}
	return DefaultCacheCapacity
}

// Validate checks if the configuration is valid.
// Returns an error if any parameter is out of acceptable range.
func (c *Config) Validate() error {
	if c.CacheCapacityBytes == 0 && c.MaxStates == 0 {
		return &DFAError{
			Kind:    InvalidConfig,
			Message: "CacheCapacityBytes or MaxStates must be > 0",
		}
	}

	if c.MaxCacheClears < 0 {
		return &DFAError{
			Kind:    InvalidConfig,
			Message: "MaxCacheClears must be >= 0",
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

// WithCacheCapacity returns a new config with the specified cache capacity in bytes.
// Default is 2MB (matching Rust regex). Set to 0 to use MaxStates instead.
func (c Config) WithCacheCapacity(bytes int) Config {
	c.CacheCapacityBytes = bytes
	return c
}

// WithMaxStates returns a new config with the specified max states.
//
// Deprecated: Use WithCacheCapacity instead.
func (c Config) WithMaxStates(maxStates uint32) Config {
	c.MaxStates = maxStates
	// Clear byte limit so legacy MaxStates takes effect
	c.CacheCapacityBytes = 0
	return c
}

// WithMaxCacheClears returns a new config with the specified max cache clears.
// Set to 0 to disable cache clearing (always fall back to NFA on full cache).
func (c Config) WithMaxCacheClears(maxClears int) Config {
	c.MaxCacheClears = maxClears
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
