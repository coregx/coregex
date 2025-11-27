package meta

import (
	"regexp/syntax"

	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/literal"
	"github.com/coregx/coregex/nfa"
	"github.com/coregx/coregex/prefilter"
)

// Engine is the meta-engine that orchestrates all regex execution strategies.
//
// The Engine:
//  1. Analyzes the pattern and extracts literals
//  2. Selects the optimal strategy (NFA, DFA, or both)
//  3. Builds prefilter (if literals available)
//  4. Coordinates search across engines
//
// Thread safety: Not thread-safe. Each goroutine should use its own Engine instance.
// The underlying NFA is immutable and can be shared, but Engine state is mutable.
//
// Example:
//
//	// Compile pattern
//	engine, err := meta.Compile("(foo|bar)\\d+")
//	if err != nil {
//	    return err
//	}
//
//	// Search
//	haystack := []byte("test foo123 end")
//	match := engine.Find(haystack)
//	if match != nil {
//	    println(match.String()) // "foo123"
//	}
type Engine struct {
	nfa       *nfa.NFA
	dfa       *lazy.DFA
	pikevm    *nfa.PikeVM
	prefilter prefilter.Prefilter
	strategy  Strategy
	config    Config

	// Statistics (useful for debugging and tuning)
	stats Stats
}

// Stats tracks execution statistics for performance analysis.
type Stats struct {
	// NFASearches counts NFA (PikeVM) searches
	NFASearches uint64

	// DFASearches counts DFA searches
	DFASearches uint64

	// PrefilterHits counts successful prefilter matches
	PrefilterHits uint64

	// PrefilterMisses counts prefilter candidates that didn't match
	PrefilterMisses uint64

	// DFACacheFull counts times DFA fell back to NFA due to cache full
	DFACacheFull uint64
}

// Compile compiles a regex pattern string into an executable Engine.
//
// Steps:
//  1. Parse pattern using regexp/syntax
//  2. Compile to NFA
//  3. Extract literals (prefixes, suffixes)
//  4. Build prefilter (if good literals exist)
//  5. Select strategy
//  6. Build DFA (if strategy requires it)
//
// Returns an error if:
//   - Pattern syntax is invalid
//   - Pattern is too complex (recursion limit exceeded)
//   - Configuration is invalid
//
// Example:
//
//	engine, err := meta.Compile("hello.*world")
//	if err != nil {
//	    log.Fatal(err)
//	}
func Compile(pattern string) (*Engine, error) {
	return CompileWithConfig(pattern, DefaultConfig())
}

// CompileWithConfig compiles a pattern with custom configuration.
//
// Example:
//
//	config := meta.DefaultConfig()
//	config.MaxDFAStates = 50000 // Increase cache
//	engine, err := meta.CompileWithConfig("(a|b|c)*", config)
func CompileWithConfig(pattern string, config Config) (*Engine, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Parse pattern
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, &CompileError{
			Pattern: pattern,
			Err:     err,
		}
	}

	return CompileRegexp(re, config)
}

// CompileRegexp compiles a parsed syntax.Regexp with default configuration.
//
// This is useful when you already have a parsed regexp from another source.
//
// Example:
//
//	re, _ := syntax.Parse("hello", syntax.Perl)
//	engine, err := meta.CompileRegexp(re, meta.DefaultConfig())
func CompileRegexp(re *syntax.Regexp, config Config) (*Engine, error) {
	// Compile to NFA
	compiler := nfa.NewCompiler(nfa.CompilerConfig{
		UTF8:              true,
		Anchored:          false,
		DotNewline:        false,
		MaxRecursionDepth: config.MaxRecursionDepth,
	})

	nfaEngine, err := compiler.CompileRegexp(re)
	if err != nil {
		return nil, &CompileError{
			Err: err,
		}
	}

	// Extract literals for prefiltering
	var literals *literal.Seq
	var pf prefilter.Prefilter
	if config.EnablePrefilter {
		extractor := literal.New(literal.ExtractorConfig{
			MaxLiterals:   config.MaxLiterals,
			MaxLiteralLen: 64,
			MaxClassSize:  10,
		})
		literals = extractor.ExtractPrefixes(re)

		// Build prefilter from literals
		if literals != nil && !literals.IsEmpty() {
			builder := prefilter.NewBuilder(literals, nil)
			pf = builder.Build()
		}
	}

	// Select strategy
	strategy := SelectStrategy(nfaEngine, literals, config)

	// Build PikeVM (always needed for fallback)
	pikevm := nfa.NewPikeVM(nfaEngine)

	// Build DFA if strategy requires it
	var dfaEngine *lazy.DFA
	if strategy == UseDFA || strategy == UseBoth {
		dfaConfig := lazy.Config{
			MaxStates:            config.MaxDFAStates,
			DeterminizationLimit: config.DeterminizationLimit,
		}

		dfaEngine, err = lazy.CompileWithConfig(nfaEngine, dfaConfig)
		if err != nil {
			// DFA compilation failed: fall back to NFA-only
			strategy = UseNFA
		}
	}

	return &Engine{
		nfa:       nfaEngine,
		dfa:       dfaEngine,
		pikevm:    pikevm,
		prefilter: pf,
		strategy:  strategy,
		config:    config,
		stats:     Stats{},
	}, nil
}

// Find returns the first match in the haystack, or nil if no match.
//
// The search algorithm depends on the selected strategy:
//
//	UseNFA:   PikeVM search directly
//	UseDFA:   Prefilter (if available) → DFA → NFA fallback
//	UseBoth:  Try DFA, fallback to NFA on cache full
//
// Example:
//
//	engine, _ := meta.Compile("hello")
//	match := engine.Find([]byte("say hello world"))
//	if match != nil {
//	    println(match.String()) // "hello"
//	}
func (e *Engine) Find(haystack []byte) *Match {
	switch e.strategy {
	case UseNFA:
		return e.findNFA(haystack)
	case UseDFA:
		return e.findDFA(haystack)
	case UseBoth:
		return e.findAdaptive(haystack)
	default:
		return e.findNFA(haystack)
	}
}

// IsMatch returns true if the pattern matches anywhere in the haystack.
//
// This is equivalent to Find(haystack) != nil but may be optimized.
//
// Example:
//
//	engine, _ := meta.Compile("hello")
//	if engine.IsMatch([]byte("say hello world")) {
//	    println("matches!")
//	}
func (e *Engine) IsMatch(haystack []byte) bool {
	return e.Find(haystack) != nil
}

// findNFA searches using NFA (PikeVM) directly.
func (e *Engine) findNFA(haystack []byte) *Match {
	e.stats.NFASearches++

	start, end, matched := e.pikevm.Search(haystack)
	if !matched {
		return nil
	}

	return NewMatch(start, end, haystack)
}

// findDFA searches using DFA with prefilter and NFA fallback.
func (e *Engine) findDFA(haystack []byte) *Match {
	e.stats.DFASearches++

	// If prefilter available and complete, use it directly
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return nil
		}
		e.stats.PrefilterHits++
		// Prefilter is complete: this IS the match
		// Need to find match end (pattern length)
		end := pos + e.estimateMatchLength()
		if end > len(haystack) {
			end = len(haystack)
		}
		return NewMatch(pos, end, haystack)
	}

	// Use DFA search
	pos := e.dfa.Find(haystack)
	if pos == -1 {
		return nil
	}

	// DFA returns end position
	// For now, assume match starts at beginning of input or at a prefilter hit
	// TODO: track match start in DFA for precise bounds
	return NewMatch(0, pos, haystack)
}

// findAdaptive tries DFA first, falls back to NFA on failure.
func (e *Engine) findAdaptive(haystack []byte) *Match {
	// Try DFA first
	if e.dfa != nil {
		e.stats.DFASearches++
		pos := e.dfa.Find(haystack)
		if pos != -1 {
			// DFA succeeded
			return NewMatch(0, pos, haystack)
		}
		// DFA failed (might be cache full) - check cache stats
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 { // 90% full
			e.stats.DFACacheFull++
		}
	}

	// Fall back to NFA
	return e.findNFA(haystack)
}

// estimateMatchLength estimates the length of a match for complete prefilters.
// This is a heuristic - in the future, the DFA should track match bounds precisely.
func (e *Engine) estimateMatchLength() int {
	// For now, return a conservative estimate based on NFA size
	// This is a placeholder - real implementation needs proper match tracking
	return 1
}

// Strategy returns the execution strategy selected for this engine.
//
// Example:
//
//	strategy := engine.Strategy()
//	println(strategy.String()) // "UseDFA"
func (e *Engine) Strategy() Strategy {
	return e.strategy
}

// Stats returns execution statistics.
//
// Useful for performance analysis and debugging.
//
// Example:
//
//	stats := engine.Stats()
//	println("NFA searches:", stats.NFASearches)
//	println("DFA searches:", stats.DFASearches)
func (e *Engine) Stats() Stats {
	return e.stats
}

// ResetStats resets execution statistics to zero.
func (e *Engine) ResetStats() {
	e.stats = Stats{}
}

// CompileError represents a pattern compilation error.
type CompileError struct {
	Pattern string
	Err     error
}

// Error implements the error interface.
func (e *CompileError) Error() string {
	if e.Pattern != "" {
		return "meta: failed to compile pattern \"" + e.Pattern + "\": " + e.Err.Error()
	}
	return "meta: failed to compile: " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *CompileError) Unwrap() error {
	return e.Err
}
