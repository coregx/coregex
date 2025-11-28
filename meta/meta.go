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
	nfa             *nfa.NFA
	dfa             *lazy.DFA
	pikevm          *nfa.PikeVM
	reverseSearcher *ReverseAnchoredSearcher
	prefilter       prefilter.Prefilter
	strategy        Strategy
	config          Config

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

	// Select strategy (pass re for anchor detection)
	strategy := SelectStrategy(nfaEngine, re, literals, config)

	// Build PikeVM (always needed for fallback)
	pikevm := nfa.NewPikeVM(nfaEngine)

	// Build DFA if strategy requires it
	var dfaEngine *lazy.DFA
	var reverseSearcher *ReverseAnchoredSearcher

	if strategy == UseDFA || strategy == UseBoth || strategy == UseReverseAnchored {
		dfaConfig := lazy.Config{
			MaxStates:            config.MaxDFAStates,
			DeterminizationLimit: config.DeterminizationLimit,
		}

		// For reverse search, build reverse searcher
		if strategy == UseReverseAnchored {
			reverseSearcher, err = NewReverseAnchoredSearcher(nfaEngine, dfaConfig)
			if err != nil {
				// Reverse DFA compilation failed: fall back to forward DFA
				strategy = UseDFA
			}
		}

		// Build forward DFA for non-reverse strategies
		if strategy == UseDFA || strategy == UseBoth {
			// Pass prefilter to DFA for start-state skip optimization
			dfaEngine, err = lazy.CompileWithPrefilter(nfaEngine, dfaConfig, pf)
			if err != nil {
				// DFA compilation failed: fall back to NFA-only
				strategy = UseNFA
			}
		}
	}

	return &Engine{
		nfa:             nfaEngine,
		dfa:             dfaEngine,
		pikevm:          pikevm,
		reverseSearcher: reverseSearcher,
		prefilter:       pf,
		strategy:        strategy,
		config:          config,
		stats:           Stats{},
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
	case UseReverseAnchored:
		return e.findReverseAnchored(haystack)
	default:
		return e.findNFA(haystack)
	}
}

// IsMatch returns true if the pattern matches anywhere in the haystack.
//
// This is optimized for boolean matching:
//   - Uses early termination (returns immediately on first match)
//   - Avoids Match object creation
//   - Uses DFA.IsMatch when available (2-10x faster than Find)
//
// Example:
//
//	engine, _ := meta.Compile("hello")
//	if engine.IsMatch([]byte("say hello world")) {
//	    println("matches!")
//	}
func (e *Engine) IsMatch(haystack []byte) bool {
	switch e.strategy {
	case UseNFA:
		return e.isMatchNFA(haystack)
	case UseDFA:
		return e.isMatchDFA(haystack)
	case UseBoth:
		return e.isMatchAdaptive(haystack)
	case UseReverseAnchored:
		return e.isMatchReverseAnchored(haystack)
	default:
		return e.isMatchNFA(haystack)
	}
}

// isMatchNFA checks for match using NFA (PikeVM) with early termination.
func (e *Engine) isMatchNFA(haystack []byte) bool {
	e.stats.NFASearches++
	_, _, matched := e.pikevm.Search(haystack)
	return matched
}

// isMatchDFA checks for match using DFA with early termination.
func (e *Engine) isMatchDFA(haystack []byte) bool {
	e.stats.DFASearches++

	// Use DFA.IsMatch which has early termination optimization
	return e.dfa.IsMatch(haystack)
}

// isMatchAdaptive tries DFA first, falls back to NFA.
func (e *Engine) isMatchAdaptive(haystack []byte) bool {
	if e.dfa != nil {
		e.stats.DFASearches++
		if e.dfa.IsMatch(haystack) {
			return true
		}
		// DFA returned false - check if cache was full
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 {
			e.stats.DFACacheFull++
			// Cache nearly full, fall back to NFA
			return e.isMatchNFA(haystack)
		}
		return false
	}
	return e.isMatchNFA(haystack)
}

// FindSubmatch returns the first match with capture group information.
// Returns nil if no match is found.
//
// Group 0 is always the entire match. Groups 1+ are explicit capture groups.
// Unmatched optional groups will have nil values.
//
// Example:
//
//	engine, _ := meta.Compile(`(\w+)@(\w+)\.(\w+)`)
//	match := engine.FindSubmatch([]byte("user@example.com"))
//	if match != nil {
//	    fmt.Println(match.Group(0)) // "user@example.com"
//	    fmt.Println(match.Group(1)) // "user"
//	    fmt.Println(match.Group(2)) // "example"
//	    fmt.Println(match.Group(3)) // "com"
//	}
func (e *Engine) FindSubmatch(haystack []byte) *MatchWithCaptures {
	e.stats.NFASearches++

	// Always use PikeVM for capture group extraction
	nfaMatch := e.pikevm.SearchWithCaptures(haystack)
	if nfaMatch == nil {
		return nil
	}

	return NewMatchWithCaptures(haystack, nfaMatch.Captures)
}

// NumCaptures returns the number of capture groups in the pattern.
// Group 0 is the entire match, groups 1+ are explicit captures.
func (e *Engine) NumCaptures() int {
	return e.nfa.CaptureCount()
}

// SubexpNames returns the names of capture groups in the pattern.
// Index 0 is always "" (entire match). Named groups return their names, unnamed groups return "".
// This matches stdlib regexp.Regexp.SubexpNames() behavior.
func (e *Engine) SubexpNames() []string {
	return e.nfa.SubexpNames()
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

	// If prefilter available and complete, use it to find candidates quickly
	// then verify with NFA to get exact match bounds
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return nil
		}
		e.stats.PrefilterHits++
		// Prefilter found the literal, now get exact match bounds from NFA
		// (NFA will start from the unanchored prefix and find the match)
		start, end, matched := e.pikevm.Search(haystack)
		if !matched {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Use DFA search
	pos := e.dfa.Find(haystack)
	if pos == -1 {
		return nil
	}

	// DFA returns end position, but doesn't track start position
	// Fall back to NFA to get exact match bounds
	// TODO: optimize by tracking match start in DFA
	start, end, matched := e.pikevm.Search(haystack)
	if !matched {
		return nil
	}
	return NewMatch(start, end, haystack)
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

// findReverseAnchored searches using reverse DFA for end-anchored patterns.
func (e *Engine) findReverseAnchored(haystack []byte) *Match {
	if e.reverseSearcher == nil {
		// Fallback to NFA if reverse searcher not available
		return e.findNFA(haystack)
	}

	e.stats.DFASearches++
	return e.reverseSearcher.Find(haystack)
}

// isMatchReverseAnchored checks for match using reverse DFA.
func (e *Engine) isMatchReverseAnchored(haystack []byte) bool {
	if e.reverseSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	e.stats.DFASearches++
	return e.reverseSearcher.IsMatch(haystack)
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

// Count returns the number of non-overlapping matches in the haystack.
//
// This is optimized for counting without allocating result slices.
// Uses early termination for boolean checks at each step.
// If n > 0, counts at most n matches. If n <= 0, counts all matches.
//
// Example:
//
//	engine, _ := meta.Compile(`\d+`)
//	count := engine.Count([]byte("1 2 3 4 5"), -1)
//	// count == 5
func (e *Engine) Count(haystack []byte, n int) int {
	if n == 0 {
		return 0
	}

	count := 0
	pos := 0

	for pos <= len(haystack) {
		// Search from current position
		match := e.Find(haystack[pos:])
		if match == nil {
			break
		}

		count++

		// Move position past this match
		end := match.End()
		if end > 0 {
			pos += end
		} else {
			// Empty match: advance by 1 to avoid infinite loop
			pos++
		}

		// Check limit
		if n > 0 && count >= n {
			break
		}
	}

	return count
}

// FindAllSubmatch returns all successive matches with capture group information.
// If n > 0, returns at most n matches. If n <= 0, returns all matches.
//
// Example:
//
//	engine, _ := meta.Compile(`(\w+)@(\w+)\.(\w+)`)
//	matches := engine.FindAllSubmatch([]byte("a@b.c x@y.z"), -1)
//	// len(matches) == 2
func (e *Engine) FindAllSubmatch(haystack []byte, n int) []*MatchWithCaptures {
	if n == 0 {
		return nil
	}

	var matches []*MatchWithCaptures
	pos := 0

	for pos <= len(haystack) {
		// Use PikeVM for capture extraction
		e.stats.NFASearches++
		nfaMatch := e.pikevm.SearchWithCaptures(haystack[pos:])
		if nfaMatch == nil {
			break
		}

		// Adjust captures to absolute positions
		// Captures is [][]int where each element is [start, end] for a group
		adjustedCaptures := make([][]int, len(nfaMatch.Captures))
		for i, cap := range nfaMatch.Captures {
			if len(cap) >= 2 && cap[0] >= 0 {
				adjustedCaptures[i] = []int{pos + cap[0], pos + cap[1]}
			} else {
				adjustedCaptures[i] = nil // Unmatched group
			}
		}

		match := NewMatchWithCaptures(haystack, adjustedCaptures)
		matches = append(matches, match)

		// Move position past this match
		end := nfaMatch.End
		if end > 0 {
			pos += end
		} else {
			// Empty match: advance by 1 to avoid infinite loop
			pos++
		}

		// Check limit
		if n > 0 && len(matches) >= n {
			break
		}
	}

	return matches
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
