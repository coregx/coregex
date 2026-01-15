// Package meta implements the meta-engine orchestrator that automatically
// selects the optimal regex execution strategy.
//
// The meta-engine coordinates three engines:
//   - Prefilter: fast literal-based candidate finding (optional)
//   - Lazy DFA: deterministic finite automaton with on-demand state construction
//   - NFA (PikeVM): nondeterministic fallback for complex patterns or DFA cache overflow
//
// Strategy selection is based on:
//   - Pattern complexity (NFA size, literal quality)
//   - Prefilter availability (good literals enable fast filtering)
//   - DFA suitability (patterns without alternations benefit most)
//
// The meta-engine provides the public API for pattern compilation and matching,
// hiding the complexity of multi-engine coordination from users.
package meta

// Config controls meta-engine behavior and performance characteristics.
//
// Configuration options affect:
//   - Strategy selection (which engine to use)
//   - Cache sizes (DFA state cache)
//   - Limits (determinization, recursion)
//   - Prefilter enablement
//
// Example:
//
//	config := meta.DefaultConfig()
//	config.EnableDFA = false // Force NFA-only execution
//	engine := meta.NewEngine(nfa, config)
type Config struct {
	// EnableDFA enables the Lazy DFA engine.
	// When false, only NFA (PikeVM) is used.
	// Default: true
	EnableDFA bool

	// EnablePrefilter enables literal-based prefiltering.
	// When false, no prefilter is used even if literals are available.
	// Default: true
	EnablePrefilter bool

	// MaxDFAStates sets the maximum number of DFA states to cache.
	// Larger values use more memory but reduce NFA fallback frequency.
	// Default: 10000
	MaxDFAStates uint32

	// DeterminizationLimit caps the number of NFA states per DFA state.
	// This prevents exponential blowup in patterns like (a*)*b.
	// Default: 1000
	DeterminizationLimit int

	// MinLiteralLen is the minimum length for prefilter literals.
	// Shorter literals may have too many false positives.
	// Default: 2
	MinLiteralLen int

	// MaxLiterals limits the number of literals to extract for prefiltering.
	// Must be > 64 to properly detect patterns that exceed Teddy's capacity.
	// Default: 256 (allows detecting patterns with >64 literals for Aho-Corasick)
	MaxLiterals int

	// MaxRecursionDepth limits recursion during NFA compilation.
	// Default: 100
	MaxRecursionDepth int

	// EnableASCIIOptimization enables ASCII runtime detection (V11-002 optimization).
	// When true and the pattern contains '.', two NFAs are compiled:
	//   - UTF-8 NFA: handles all valid UTF-8 codepoints (~28 states per '.')
	//   - ASCII NFA: optimized for ASCII-only input (1-2 states per '.')
	//
	// At runtime, input is checked using SIMD (AVX2 on x86-64) to determine if
	// all bytes are ASCII. If so, the faster ASCII NFA is used.
	//
	// Performance impact for Issue #79 pattern ^/.*[\w-]+\.php:
	//   - Compile time: ~1.5x longer (compiling two NFAs)
	//   - Match time: up to 1.6x faster on ASCII input
	//   - Memory: ~1.4x more (two NFAs stored)
	//
	// The check adds ~3-4ns overhead per search but saves significantly more
	// on patterns with '.' when input is ASCII-only.
	//
	// Default: true
	EnableASCIIOptimization bool
}

// DefaultConfig returns a configuration with sensible defaults.
//
// Defaults are tuned for typical regex patterns with a balance between
// performance and memory usage:
//   - DFA enabled with 10K state cache (moderate memory usage)
//   - Prefilter enabled (5-50x speedup on patterns with literals)
//   - Conservative determinization limit (prevents exponential blowup)
//   - Reasonable recursion depth (handles nested patterns)
//
// Example:
//
//	config := meta.DefaultConfig()
//	// Use as-is or customize specific options
//	config.MaxDFAStates = 50000 // Increase cache for better hit rate
func DefaultConfig() Config {
	return Config{
		EnableDFA:               true,
		EnablePrefilter:         true,
		MaxDFAStates:            10000,
		DeterminizationLimit:    1000,
		MinLiteralLen:           1,   // Allow single-byte prefilters (memchr) like Rust
		MaxLiterals:             256, // Allow detecting >64 literals for Aho-Corasick
		MaxRecursionDepth:       100,
		EnableASCIIOptimization: true, // V11-002: ASCII runtime detection for '.' patterns
	}
}

// Validate checks if the configuration is valid.
// Returns an error if any parameter is out of range.
//
// Valid ranges:
//   - MaxDFAStates: 1 to 1,000,000
//   - DeterminizationLimit: 10 to 100,000
//   - MinLiteralLen: 1 to 64
//   - MaxLiterals: 1 to 1,000
//   - MaxRecursionDepth: 10 to 1,000
//
// Example:
//
//	config := meta.Config{MaxDFAStates: 0} // Invalid!
//	if err := config.Validate(); err != nil {
//	    log.Fatal(err)
//	}
func (c Config) Validate() error {
	if c.EnableDFA {
		if c.MaxDFAStates < 1 || c.MaxDFAStates > 1_000_000 {
			return &ConfigError{
				Field:   "MaxDFAStates",
				Message: "must be between 1 and 1,000,000",
			}
		}
		if c.DeterminizationLimit < 10 || c.DeterminizationLimit > 100_000 {
			return &ConfigError{
				Field:   "DeterminizationLimit",
				Message: "must be between 10 and 100,000",
			}
		}
	}

	if c.EnablePrefilter {
		if c.MinLiteralLen < 1 || c.MinLiteralLen > 64 {
			return &ConfigError{
				Field:   "MinLiteralLen",
				Message: "must be between 1 and 64",
			}
		}
		if c.MaxLiterals < 1 || c.MaxLiterals > 1_000 {
			return &ConfigError{
				Field:   "MaxLiterals",
				Message: "must be between 1 and 1,000",
			}
		}
	}

	if c.MaxRecursionDepth < 10 || c.MaxRecursionDepth > 1_000 {
		return &ConfigError{
			Field:   "MaxRecursionDepth",
			Message: "must be between 10 and 1,000",
		}
	}

	return nil
}

// ConfigError represents an invalid configuration parameter.
type ConfigError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ConfigError) Error() string {
	return "regexp: invalid config: " + e.Field + ": " + e.Message
}
