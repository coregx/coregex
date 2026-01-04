package meta

import (
	"errors"
	"regexp/syntax"

	"github.com/coregx/ahocorasick"
	"github.com/coregx/coregex/dfa/lazy"
	"github.com/coregx/coregex/dfa/onepass"
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
	nfa                      *nfa.NFA
	dfa                      *lazy.DFA
	pikevm                   *nfa.PikeVM
	boundedBacktracker       *nfa.BoundedBacktracker
	charClassSearcher        *nfa.CharClassSearcher // Specialized searcher for char_class+ patterns
	reverseSearcher          *ReverseAnchoredSearcher
	reverseSuffixSearcher    *ReverseSuffixSearcher
	reverseSuffixSetSearcher *ReverseSuffixSetSearcher
	reverseInnerSearcher     *ReverseInnerSearcher
	digitPrefilter           *prefilter.DigitPrefilter // For digit-lead patterns like IP addresses
	ahoCorasick              *ahocorasick.Automaton    // For large literal alternations (>8 patterns)
	prefilter                prefilter.Prefilter
	strategy                 Strategy
	config                   Config

	// OnePass DFA for anchored patterns with captures (optional optimization)
	// This is independent of strategy - used by FindSubmatch when available
	onepass      *onepass.DFA
	onepassCache *onepass.Cache

	// longest enables leftmost-longest (POSIX) matching semantics
	// By default (false), uses leftmost-first (Perl) semantics
	longest bool

	// canMatchEmpty is true if the pattern can match an empty string.
	// When true, BoundedBacktracker cannot be used for Find operations
	// because its greedy semantics give wrong results for patterns like (?:|a)*
	canMatchEmpty bool

	// Statistics (useful for debugging and tuning)
	stats Stats
}

// Stats tracks execution statistics for performance analysis.
type Stats struct {
	// NFASearches counts NFA (PikeVM) searches
	NFASearches uint64

	// DFASearches counts DFA searches
	DFASearches uint64

	// OnePassSearches counts OnePass DFA searches (for FindSubmatch)
	OnePassSearches uint64

	// AhoCorasickSearches counts Aho-Corasick automaton searches
	AhoCorasickSearches uint64

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

// buildCharClassSearchers creates specialized searchers for character class patterns.
// Returns (BoundedBacktracker, CharClassSearcher, updatedStrategy).
// If CharClassSearcher extraction fails, falls back to BoundedBacktracker.
// onePassResult holds the result of buildOnePassDFA.
type onePassResult struct {
	dfa   *onepass.DFA
	cache *onepass.Cache
}

// buildOnePassDFA tries to build a OnePass DFA for anchored patterns with captures.
// This is an optional optimization for FindSubmatch (10-20x faster).
func buildOnePassDFA(re *syntax.Regexp, nfaEngine *nfa.NFA, config Config) onePassResult {
	if !config.EnableDFA || nfaEngine.CaptureCount() <= 1 {
		return onePassResult{}
	}

	// Compile anchored NFA for OnePass (requires Anchored: true)
	anchoredCompiler := nfa.NewCompiler(nfa.CompilerConfig{
		UTF8:              true,
		Anchored:          true,
		DotNewline:        false,
		MaxRecursionDepth: config.MaxRecursionDepth,
	})
	anchoredNFA, err := anchoredCompiler.CompileRegexp(re)
	if err != nil {
		return onePassResult{}
	}

	// Try to build one-pass DFA
	onepassDFA, err := onepass.Build(anchoredNFA)
	if err != nil {
		return onePassResult{}
	}

	return onePassResult{
		dfa:   onepassDFA,
		cache: onepass.NewCache(onepassDFA.NumCaptures()),
	}
}

// strategyEngines holds all strategy-specific engines built by buildStrategyEngines.
type strategyEngines struct {
	dfa                      *lazy.DFA
	reverseSearcher          *ReverseAnchoredSearcher
	reverseSuffixSearcher    *ReverseSuffixSearcher
	reverseSuffixSetSearcher *ReverseSuffixSetSearcher
	reverseInnerSearcher     *ReverseInnerSearcher
	digitPrefilter           *prefilter.DigitPrefilter
	ahoCorasick              *ahocorasick.Automaton
	finalStrategy            Strategy
}

// buildStrategyEngines builds all strategy-specific engines based on the selected strategy.
// Returns the engines and potentially updated strategy (if building fails and fallback is needed).
func buildStrategyEngines(
	strategy Strategy,
	re *syntax.Regexp,
	nfaEngine *nfa.NFA,
	literals *literal.Seq,
	pf prefilter.Prefilter,
	config Config,
) strategyEngines {
	result := strategyEngines{finalStrategy: strategy}

	// Build Aho-Corasick automaton for large literal alternations (>8 patterns)
	if strategy == UseAhoCorasick && literals != nil && !literals.IsEmpty() {
		builder := ahocorasick.NewBuilder()
		litCount := literals.Len()
		for i := 0; i < litCount; i++ {
			lit := literals.Get(i)
			builder.AddPattern(lit.Bytes)
		}
		auto, err := builder.Build()
		if err != nil {
			result.finalStrategy = UseNFA
		} else {
			result.ahoCorasick = auto
		}
		return result
	}

	// Check if DFA-based strategy is needed
	needsDFA := strategy == UseDFA || strategy == UseBoth ||
		strategy == UseReverseAnchored || strategy == UseReverseSuffix ||
		strategy == UseReverseSuffixSet || strategy == UseReverseInner ||
		strategy == UseDigitPrefilter

	if !needsDFA {
		return result
	}

	dfaConfig := lazy.Config{
		MaxStates:            config.MaxDFAStates,
		DeterminizationLimit: config.DeterminizationLimit,
	}

	result = buildReverseSearchers(result, strategy, re, nfaEngine, dfaConfig, config)

	// Build forward DFA for non-reverse strategies
	if result.finalStrategy == UseDFA || result.finalStrategy == UseBoth || result.finalStrategy == UseDigitPrefilter {
		dfa, err := lazy.CompileWithPrefilter(nfaEngine, dfaConfig, pf)
		if err != nil {
			result.finalStrategy = UseNFA
		} else {
			result.dfa = dfa
		}
	}

	// For digit prefilter strategy, create the digit prefilter
	if result.finalStrategy == UseDigitPrefilter {
		result.digitPrefilter = prefilter.NewDigitPrefilter()
	}

	return result
}

// buildReverseSearchers builds reverse searchers for reverse strategies.
func buildReverseSearchers(
	result strategyEngines,
	strategy Strategy,
	re *syntax.Regexp,
	nfaEngine *nfa.NFA,
	dfaConfig lazy.Config,
	config Config,
) strategyEngines {
	extractor := literal.New(literal.ExtractorConfig{
		MaxLiterals:   config.MaxLiterals,
		MaxLiteralLen: 64,
		MaxClassSize:  10,
	})

	switch strategy {
	case UseReverseAnchored:
		searcher, err := NewReverseAnchoredSearcher(nfaEngine, dfaConfig)
		if err != nil {
			result.finalStrategy = UseDFA
		} else {
			result.reverseSearcher = searcher
		}

	case UseReverseSuffix:
		suffixLiterals := extractor.ExtractSuffixes(re)
		searcher, err := NewReverseSuffixSearcher(nfaEngine, suffixLiterals, dfaConfig)
		if err != nil {
			result.finalStrategy = UseDFA
		} else {
			result.reverseSuffixSearcher = searcher
		}

	case UseReverseSuffixSet:
		suffixLiterals := extractor.ExtractSuffixes(re)
		searcher, err := NewReverseSuffixSetSearcher(nfaEngine, suffixLiterals, dfaConfig)
		if err != nil {
			result.finalStrategy = UseBoth
		} else {
			result.reverseSuffixSetSearcher = searcher
		}

	case UseReverseInner:
		innerInfo := extractor.ExtractInnerForReverseSearch(re)
		if innerInfo == nil {
			result.finalStrategy = UseDFA
		} else {
			searcher, err := NewReverseInnerSearcher(nfaEngine, innerInfo, dfaConfig)
			if err != nil {
				result.finalStrategy = UseDFA
			} else {
				result.reverseInnerSearcher = searcher
			}
		}
	}

	return result
}

func buildCharClassSearchers(
	strategy Strategy,
	re *syntax.Regexp,
	nfaEngine *nfa.NFA,
) (*nfa.BoundedBacktracker, *nfa.CharClassSearcher, Strategy) {
	var boundedBT *nfa.BoundedBacktracker
	var charClassSrch *nfa.CharClassSearcher

	if strategy == UseBoundedBacktracker {
		boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
	}

	if strategy == UseCharClassSearcher {
		ranges := nfa.ExtractCharClassRanges(re)
		if ranges != nil {
			// Determine minMatch: 1 for +, 0 for *
			minMatch := 1
			if re.Op == syntax.OpStar {
				minMatch = 0
			}
			charClassSrch = nfa.NewCharClassSearcher(ranges, minMatch)
		} else {
			// Fallback to BoundedBacktracker if extraction fails
			strategy = UseBoundedBacktracker
			boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
		}
	}

	// For UseNFA with small NFAs, also create BoundedBacktracker as fallback.
	// BoundedBacktracker is 2-3x faster than PikeVM on small inputs due to
	// generation-based visited tracking (O(1) reset) vs PikeVM's thread queues.
	// This is similar to how stdlib uses backtracking for simple patterns.
	if strategy == UseNFA && boundedBT == nil && nfaEngine.States() < 50 {
		boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
	}

	return boundedBT, charClassSrch, strategy
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
	// NOTE: Don't build prefilter for start-anchored patterns (^...).
	// A prefilter for "^abc" would find "abc" anywhere in input, bypassing the anchor.
	// The prefilter's IsComplete() would return true, causing false positives.
	var literals *literal.Seq
	var pf prefilter.Prefilter
	isStartAnchored := nfaEngine.IsAlwaysAnchored()
	if config.EnablePrefilter && !isStartAnchored {
		extractor := literal.New(literal.ExtractorConfig{
			MaxLiterals:   config.MaxLiterals,
			MaxLiteralLen: 64,
			MaxClassSize:  10,
		})
		literals = extractor.ExtractPrefixes(re)

		// Build prefilter from prefix literals
		if literals != nil && !literals.IsEmpty() {
			builder := prefilter.NewBuilder(literals, nil)
			pf = builder.Build()
		}
	}

	// Select strategy (pass re for anchor detection)
	strategy := SelectStrategy(nfaEngine, re, literals, config)

	// Build PikeVM (always needed for fallback)
	pikevm := nfa.NewPikeVM(nfaEngine)

	// Build OnePass DFA for anchored patterns with captures (optional optimization)
	onePassRes := buildOnePassDFA(re, nfaEngine, config)

	// Build strategy-specific engines (DFA, reverse searchers, Aho-Corasick, etc.)
	engines := buildStrategyEngines(strategy, re, nfaEngine, literals, pf, config)
	strategy = engines.finalStrategy

	// Build specialized searchers for character class patterns
	boundedBT, charClassSrch, strategy := buildCharClassSearchers(strategy, re, nfaEngine)

	// Check if pattern can match empty string.
	// If true, BoundedBacktracker cannot be used for Find operations
	// because its greedy semantics give wrong results for patterns like (?:|a)*
	canMatchEmpty := pikevm.IsMatch(nil)

	return &Engine{
		nfa:                      nfaEngine,
		dfa:                      engines.dfa,
		pikevm:                   pikevm,
		boundedBacktracker:       boundedBT,
		charClassSearcher:        charClassSrch,
		reverseSearcher:          engines.reverseSearcher,
		reverseSuffixSearcher:    engines.reverseSuffixSearcher,
		reverseSuffixSetSearcher: engines.reverseSuffixSetSearcher,
		reverseInnerSearcher:     engines.reverseInnerSearcher,
		digitPrefilter:           engines.digitPrefilter,
		ahoCorasick:              engines.ahoCorasick,
		prefilter:                pf,
		strategy:                 strategy,
		config:                   config,
		onepass:                  onePassRes.dfa,
		onepassCache:             onePassRes.cache,
		canMatchEmpty:            canMatchEmpty,
		stats:                    Stats{},
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
	return e.FindAt(haystack, 0)
}

// FindAt finds the first match starting from position 'at' in the haystack.
// Returns nil if no match is found.
//
// This method is used by FindAll* operations to correctly handle anchors like ^.
// Unlike Find, it takes the FULL haystack and a starting position, so assertions
// like ^ correctly check against the original input start, not a sliced position.
//
// Example:
//
//	engine, _ := meta.Compile("^test")
//	match := engine.FindAt([]byte("hello test"), 0)  // matches at 0
//	match := engine.FindAt([]byte("hello test"), 6)  // no match (^ won't match at pos 6)
func (e *Engine) FindAt(haystack []byte, at int) *Match {
	if at > len(haystack) {
		return nil
	}

	// Early impossibility check: anchored pattern can only match at position 0
	if at > 0 && e.nfa.IsAlwaysAnchored() {
		return nil
	}

	// For position 0, use the optimized strategy-specific paths
	if at == 0 {
		return e.findAtZero(haystack)
	}

	// For non-zero positions, use FindAt variants that preserve absolute positions
	return e.findAtNonZero(haystack, at)
}

// findAtZero dispatches to the appropriate strategy for position 0.
// This is a helper function to reduce cyclomatic complexity in FindAt.
func (e *Engine) findAtZero(haystack []byte) *Match {
	switch e.strategy {
	case UseNFA:
		return e.findNFA(haystack)
	case UseDFA:
		return e.findDFA(haystack)
	case UseBoth:
		return e.findAdaptive(haystack)
	case UseReverseAnchored:
		return e.findReverseAnchored(haystack)
	case UseReverseSuffix:
		return e.findReverseSuffix(haystack)
	case UseReverseSuffixSet:
		return e.findReverseSuffixSet(haystack)
	case UseReverseInner:
		return e.findReverseInner(haystack)
	case UseBoundedBacktracker:
		return e.findBoundedBacktracker(haystack)
	case UseCharClassSearcher:
		return e.findCharClassSearcher(haystack)
	case UseTeddy:
		return e.findTeddy(haystack)
	case UseDigitPrefilter:
		return e.findDigitPrefilter(haystack)
	case UseAhoCorasick:
		return e.findAhoCorasick(haystack)
	default:
		return e.findNFA(haystack)
	}
}

// findAtNonZero dispatches to the appropriate strategy for non-zero positions.
// This is a helper function to reduce cyclomatic complexity in FindAt.
func (e *Engine) findAtNonZero(haystack []byte, at int) *Match {
	switch e.strategy {
	case UseNFA:
		return e.findNFAAt(haystack, at)
	case UseDFA:
		return e.findDFAAt(haystack, at)
	case UseBoth:
		return e.findAdaptiveAt(haystack, at)
	case UseReverseAnchored, UseReverseSuffix, UseReverseInner:
		// Reverse strategies should work correctly with slicing
		// since they operate on specific ranges
		return e.findNFAAt(haystack, at)
	case UseBoundedBacktracker:
		return e.findBoundedBacktrackerAt(haystack, at)
	case UseCharClassSearcher:
		return e.findCharClassSearcherAt(haystack, at)
	case UseTeddy:
		return e.findTeddyAt(haystack, at)
	case UseDigitPrefilter:
		return e.findDigitPrefilterAt(haystack, at)
	case UseAhoCorasick:
		return e.findAhoCorasickAt(haystack, at)
	default:
		return e.findNFAAt(haystack, at)
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
	case UseReverseSuffix:
		return e.isMatchReverseSuffix(haystack)
	case UseReverseSuffixSet:
		return e.isMatchReverseSuffixSet(haystack)
	case UseReverseInner:
		return e.isMatchReverseInner(haystack)
	case UseBoundedBacktracker:
		return e.isMatchBoundedBacktracker(haystack)
	case UseCharClassSearcher:
		return e.isMatchCharClassSearcher(haystack)
	case UseTeddy:
		return e.isMatchTeddy(haystack)
	case UseDigitPrefilter:
		return e.isMatchDigitPrefilter(haystack)
	case UseAhoCorasick:
		return e.isMatchAhoCorasick(haystack)
	default:
		return e.isMatchNFA(haystack)
	}
}

// isMatchNFA checks for match using NFA (PikeVM or BoundedBacktracker) with early termination.
// Uses prefilter for skip-ahead when available (like Rust regex).
// For small NFAs, prefers BoundedBacktracker (2-3x faster than PikeVM on small inputs).
func (e *Engine) isMatchNFA(haystack []byte) bool {
	e.stats.NFASearches++

	// BoundedBacktracker is preferred when available (supports both default and Longest modes)
	useBT := e.boundedBacktracker != nil

	// Use prefilter for skip-ahead if available
	if e.prefilter != nil {
		at := 0
		for at < len(haystack) {
			// Find next candidate position via prefilter
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return false // No more candidates
			}
			e.stats.PrefilterHits++

			// Try to match at candidate position
			// Prefer BoundedBacktracker for small inputs (2-3x faster)
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				_, _, found = e.boundedBacktracker.SearchAt(haystack, pos)
			} else {
				_, _, found = e.pikevm.SearchAt(haystack, pos)
			}
			if found {
				return true
			}

			// Move past this position
			e.stats.PrefilterMisses++
			at = pos + 1
		}
		return false
	}

	// No prefilter: use BoundedBacktracker if available, else PikeVM
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.boundedBacktracker.IsMatch(haystack)
	}

	// Use optimized IsMatch that returns immediately on first match
	// without computing exact match positions
	return e.pikevm.IsMatch(haystack)
}

// isMatchDFA checks for match using DFA with early termination.
func (e *Engine) isMatchDFA(haystack []byte) bool {
	e.stats.DFASearches++

	// Use DFA.IsMatch which has early termination optimization
	return e.dfa.IsMatch(haystack)
}

// isMatchAdaptive tries prefilter/DFA first, falls back to NFA.
func (e *Engine) isMatchAdaptive(haystack []byte) bool {
	// Use prefilter if available for fast boolean matching
	if e.prefilter != nil {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return false // Prefilter says no match
		}
		e.stats.PrefilterHits++
		// For complete prefilters (Teddy with literals), the find is sufficient
		if e.prefilter.IsComplete() {
			return true
		}
		// Verify with NFA for incomplete prefilters
		return e.isMatchNFA(haystack)
	}

	// Fall back to DFA
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

// isMatchBoundedBacktracker checks for match using bounded backtracker.
// 2-4x faster than PikeVM for simple character class patterns.
func (e *Engine) isMatchBoundedBacktracker(haystack []byte) bool {
	if e.boundedBacktracker == nil {
		return e.isMatchNFA(haystack)
	}
	e.stats.NFASearches++ // Count as NFA-family search for stats
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		// Input too large for bounded backtracker, fall back to PikeVM
		return e.pikevm.IsMatch(haystack)
	}
	return e.boundedBacktracker.IsMatch(haystack)
}

// FindSubmatch returns the first match with capture group information.
// Returns nil if no match is found.
//
// Group 0 is always the entire match. Groups 1+ are explicit capture groups.
// Unmatched optional groups will have nil values.
//
// When a one-pass DFA is available (for anchored patterns), this method
// is 10-20x faster than PikeVM for capture group extraction.
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
	return e.FindSubmatchAt(haystack, 0)
}

// FindSubmatchAt returns the first match with capture group information,
// starting from position 'at' in the haystack.
// Returns nil if no match is found.
//
// This method is used by ReplaceAll* operations to correctly handle anchors like ^.
// Unlike FindSubmatch, it takes the FULL haystack and a starting position.
func (e *Engine) FindSubmatchAt(haystack []byte, at int) *MatchWithCaptures {
	// For position 0, try OnePass DFA if available (10-20x faster for anchored patterns)
	if at == 0 && e.onepass != nil && e.onepassCache != nil {
		e.stats.OnePassSearches++
		slots := e.onepass.Search(haystack, e.onepassCache)
		if slots != nil {
			// Convert flat slots [start0, end0, start1, end1, ...] to nested captures
			captures := slotsToCaptures(slots)
			return NewMatchWithCaptures(haystack, captures)
		}
		// OnePass failed (input doesn't match from position 0)
		// Fall through to PikeVM which can find match anywhere
	}

	e.stats.NFASearches++

	// Use PikeVM for capture group extraction
	nfaMatch := e.pikevm.SearchWithCapturesAt(haystack, at)
	if nfaMatch == nil {
		return nil
	}

	return NewMatchWithCaptures(haystack, nfaMatch.Captures)
}

// slotsToCaptures converts flat slots [start0, end0, start1, end1, ...]
// to nested captures [[start0, end0], [start1, end1], ...].
func slotsToCaptures(slots []int) [][]int {
	numCaptures := len(slots) / 2
	captures := make([][]int, numCaptures)
	for i := 0; i < numCaptures; i++ {
		start := slots[i*2]
		end := slots[i*2+1]
		if start >= 0 && end >= 0 {
			captures[i] = []int{start, end}
		} else {
			captures[i] = nil // Unmatched capture
		}
	}
	return captures
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

// SetLongest enables or disables leftmost-longest (POSIX) matching semantics.
// By default, the engine uses leftmost-first (Perl) semantics where the first
// alternative in an alternation wins. With longest=true, the longest match wins.
//
// This affects how alternations like `(a|ab)` match:
//   - longest=false (default): "a" wins (first branch)
//   - longest=true: "ab" wins (longest match)
func (e *Engine) SetLongest(longest bool) {
	e.longest = longest
	e.pikevm.SetLongest(longest)
	if e.boundedBacktracker != nil {
		e.boundedBacktracker.SetLongest(longest)
	}
}

// FindIndices returns the start and end indices of the first match.
// Returns (-1, -1, false) if no match is found.
//
// This is a zero-allocation alternative to Find() - it returns indices
// directly instead of creating a Match object.
func (e *Engine) FindIndices(haystack []byte) (start, end int, found bool) {
	switch e.strategy {
	case UseNFA:
		return e.findIndicesNFA(haystack)
	case UseDFA:
		return e.findIndicesDFA(haystack)
	case UseBoth:
		return e.findIndicesAdaptive(haystack)
	case UseReverseAnchored:
		return e.findIndicesReverseAnchored(haystack)
	case UseReverseSuffix:
		return e.findIndicesReverseSuffix(haystack)
	case UseReverseSuffixSet:
		return e.findIndicesReverseSuffixSet(haystack)
	case UseReverseInner:
		return e.findIndicesReverseInner(haystack)
	case UseBoundedBacktracker:
		return e.findIndicesBoundedBacktracker(haystack)
	case UseCharClassSearcher:
		return e.findIndicesCharClassSearcher(haystack)
	case UseTeddy:
		return e.findIndicesTeddy(haystack)
	case UseDigitPrefilter:
		return e.findIndicesDigitPrefilter(haystack)
	case UseAhoCorasick:
		return e.findIndicesAhoCorasick(haystack)
	default:
		return e.findIndicesNFA(haystack)
	}
}

// FindIndicesAt returns the start and end indices of the first match starting at position 'at'.
// Returns (-1, -1, false) if no match is found.
func (e *Engine) FindIndicesAt(haystack []byte, at int) (start, end int, found bool) {
	// Early impossibility check: anchored pattern can only match at position 0
	if at > 0 && e.nfa.IsAlwaysAnchored() {
		return -1, -1, false
	}

	switch e.strategy {
	case UseNFA:
		return e.findIndicesNFAAt(haystack, at)
	case UseDFA:
		return e.findIndicesDFAAt(haystack, at)
	case UseBoth:
		return e.findIndicesAdaptiveAt(haystack, at)
	case UseReverseSuffix:
		return e.findIndicesReverseSuffixAt(haystack, at)
	case UseReverseSuffixSet:
		return e.findIndicesReverseSuffixSetAt(haystack, at)
	case UseReverseInner:
		return e.findIndicesReverseInnerAt(haystack, at)
	case UseBoundedBacktracker:
		return e.findIndicesBoundedBacktrackerAt(haystack, at)
	case UseCharClassSearcher:
		return e.findIndicesCharClassSearcherAt(haystack, at)
	case UseTeddy:
		return e.findIndicesTeddyAt(haystack, at)
	case UseDigitPrefilter:
		return e.findIndicesDigitPrefilterAt(haystack, at)
	case UseAhoCorasick:
		return e.findIndicesAhoCorasickAt(haystack, at)
	default:
		return e.findIndicesNFAAt(haystack, at)
	}
}

// findIndicesNFA searches using NFA (PikeVM) directly - zero alloc.
// Uses prefilter for skip-ahead when available (like Rust regex).
//
// BoundedBacktracker can be used for patterns that cannot match empty.
// For patterns like (?:|a)*, its greedy semantics give wrong results,
// so we must use PikeVM which correctly implements leftmost-first semantics.
func (e *Engine) findIndicesNFA(haystack []byte) (int, int, bool) {
	e.stats.NFASearches++

	// BoundedBacktracker can be used for Find operations only when:
	// 1. It's available
	// 2. Pattern cannot match empty (BT has greedy semantics that break empty match handling)
	useBT := e.boundedBacktracker != nil && !e.canMatchEmpty

	// Use prefilter for skip-ahead if available
	if e.prefilter != nil {
		at := 0
		for at < len(haystack) {
			// Find next candidate position via prefilter
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return -1, -1, false // No more candidates
			}
			e.stats.PrefilterHits++

			// Try to match at candidate position
			var start, end int
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				start, end, found = e.boundedBacktracker.SearchAt(haystack, pos)
			} else {
				start, end, found = e.pikevm.SearchAt(haystack, pos)
			}
			if found {
				return start, end, true
			}

			// Move past this position
			e.stats.PrefilterMisses++
			at = pos + 1
		}
		return -1, -1, false
	}

	// No prefilter: use BoundedBacktracker if available and safe
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.boundedBacktracker.Search(haystack)
	}

	return e.pikevm.Search(haystack)
}

// findIndicesNFAAt searches using NFA starting at position - zero alloc.
// Uses prefilter for skip-ahead when available (like Rust regex).
// Same BoundedBacktracker rules as findIndicesNFA.
func (e *Engine) findIndicesNFAAt(haystack []byte, at int) (int, int, bool) {
	e.stats.NFASearches++

	// BoundedBacktracker can be used for Find operations only when safe
	useBT := e.boundedBacktracker != nil && !e.canMatchEmpty

	// Use prefilter for skip-ahead if available
	if e.prefilter != nil {
		for at < len(haystack) {
			// Find next candidate position via prefilter
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return -1, -1, false // No more candidates
			}
			e.stats.PrefilterHits++

			// Try to match at candidate position
			var start, end int
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				start, end, found = e.boundedBacktracker.SearchAt(haystack, pos)
			} else {
				start, end, found = e.pikevm.SearchAt(haystack, pos)
			}
			if found {
				return start, end, true
			}

			// Move past this position
			e.stats.PrefilterMisses++
			at = pos + 1
		}
		return -1, -1, false
	}

	// No prefilter: use BoundedBacktracker if available and safe
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)-at) {
		return e.boundedBacktracker.SearchAt(haystack, at)
	}

	return e.pikevm.SearchAt(haystack, at)
}

// findIndicesDFA searches using DFA with prefilter - zero alloc.
func (e *Engine) findIndicesDFA(haystack []byte) (int, int, bool) {
	e.stats.DFASearches++

	// Literal fast path
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return -1, -1, false
		}
		e.stats.PrefilterHits++
		literalLen := e.prefilter.LiteralLen()
		if literalLen > 0 {
			return pos, pos + literalLen, true
		}
		return e.pikevm.Search(haystack)
	}

	// Use DFA search to check if there's a match
	pos := e.dfa.Find(haystack)
	if pos == -1 {
		return -1, -1, false
	}

	// DFA found a match - use PikeVM for exact bounds (leftmost-first semantics)
	// NOTE: Bidirectional search (reverse DFA) doesn't work correctly here because
	// DFA.Find returns the END of LONGEST match, not FIRST match. For patterns like
	// (?m)abc$ on "abc\nabc", DFA returns 7 but correct first match ends at 3.
	return e.pikevm.Search(haystack)
}

// findIndicesDFAAt searches using DFA starting at position - zero alloc.
func (e *Engine) findIndicesDFAAt(haystack []byte, at int) (int, int, bool) {
	e.stats.DFASearches++

	// Literal fast path
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return -1, -1, false
		}
		e.stats.PrefilterHits++
		literalLen := e.prefilter.LiteralLen()
		if literalLen > 0 {
			return pos, pos + literalLen, true
		}
		return e.pikevm.SearchAt(haystack, at)
	}

	// Use DFA search to check if there's a match
	pos := e.dfa.FindAt(haystack, at)
	if pos == -1 {
		return -1, -1, false
	}

	// DFA found a match - use PikeVM for exact bounds (leftmost-first semantics)
	return e.pikevm.SearchAt(haystack, at)
}

// findIndicesAdaptive tries prefilter+DFA first, falls back to NFA - zero alloc.
func (e *Engine) findIndicesAdaptive(haystack []byte) (int, int, bool) {
	// Use prefilter if available for fast candidate finding
	if e.prefilter != nil && e.dfa != nil {
		// Check if prefilter can return match bounds directly (e.g., Teddy)
		if mf, ok := e.prefilter.(prefilter.MatchFinder); ok {
			start, end := mf.FindMatch(haystack, 0)
			if start == -1 {
				return -1, -1, false
			}
			e.stats.PrefilterHits++
			e.stats.DFASearches++
			return start, end, true
		}

		// Standard prefilter path
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			// No candidate found - definitely no match
			return -1, -1, false
		}
		e.stats.PrefilterHits++
		e.stats.DFASearches++

		// Literal fast path
		if e.prefilter.IsComplete() {
			literalLen := e.prefilter.LiteralLen()
			if literalLen > 0 {
				return pos, pos + literalLen, true
			}
		}

		// Search from prefilter position - O(m) not O(n)
		return e.pikevm.SearchAt(haystack, pos)
	}

	// Try DFA without prefilter
	if e.dfa != nil {
		e.stats.DFASearches++
		endPos := e.dfa.Find(haystack)
		if endPos != -1 {
			// Use estimated start position for O(m) search instead of O(n)
			estimatedStart := 0
			if endPos > 100 {
				estimatedStart = endPos - 100
			}
			return e.pikevm.SearchAt(haystack, estimatedStart)
		}
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 {
			e.stats.DFACacheFull++
		}
	}
	return e.findIndicesNFA(haystack)
}

// findIndicesAdaptiveAt tries prefilter+DFA first at position, falls back to NFA - zero alloc.
func (e *Engine) findIndicesAdaptiveAt(haystack []byte, at int) (int, int, bool) {
	// Use prefilter if available for fast candidate finding
	if e.prefilter != nil && e.dfa != nil {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return -1, -1, false
		}
		e.stats.PrefilterHits++
		e.stats.DFASearches++

		// Literal fast path
		if e.prefilter.IsComplete() {
			literalLen := e.prefilter.LiteralLen()
			if literalLen > 0 {
				return pos, pos + literalLen, true
			}
		}

		// Search from prefilter position - O(m) not O(n)
		return e.pikevm.SearchAt(haystack, pos)
	}

	// Try DFA without prefilter
	if e.dfa != nil {
		e.stats.DFASearches++
		endPos := e.dfa.FindAt(haystack, at)
		if endPos != -1 {
			// Use estimated start for O(m) search
			estimatedStart := at
			if endPos > at+100 {
				estimatedStart = endPos - 100
			}
			return e.pikevm.SearchAt(haystack, estimatedStart)
		}
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 {
			e.stats.DFACacheFull++
		}
	}
	return e.findIndicesNFAAt(haystack, at)
}

// findIndicesReverseAnchored searches using reverse DFA - zero alloc.
func (e *Engine) findIndicesReverseAnchored(haystack []byte) (int, int, bool) {
	if e.reverseSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	e.stats.DFASearches++
	match := e.reverseSearcher.Find(haystack)
	if match == nil {
		return -1, -1, false
	}
	return match.Start(), match.End(), true
}

// findIndicesReverseSuffix searches using reverse suffix optimization - zero alloc.
func (e *Engine) findIndicesReverseSuffix(haystack []byte) (int, int, bool) {
	if e.reverseSuffixSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	e.stats.DFASearches++
	match := e.reverseSuffixSearcher.Find(haystack)
	if match == nil {
		return -1, -1, false
	}
	return match.Start(), match.End(), true
}

// findIndicesReverseSuffixAt searches using reverse suffix optimization from position - zero alloc.
func (e *Engine) findIndicesReverseSuffixAt(haystack []byte, at int) (int, int, bool) {
	if e.reverseSuffixSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	e.stats.DFASearches++
	return e.reverseSuffixSearcher.FindIndicesAt(haystack, at)
}

// findIndicesReverseSuffixSet searches using reverse suffix SET optimization - zero alloc.
func (e *Engine) findIndicesReverseSuffixSet(haystack []byte) (int, int, bool) {
	if e.reverseSuffixSetSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	e.stats.DFASearches++
	match := e.reverseSuffixSetSearcher.Find(haystack)
	if match == nil {
		return -1, -1, false
	}
	return match.Start(), match.End(), true
}

// findIndicesReverseSuffixSetAt searches using reverse suffix SET optimization from position - zero alloc.
func (e *Engine) findIndicesReverseSuffixSetAt(haystack []byte, at int) (int, int, bool) {
	if e.reverseSuffixSetSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	e.stats.DFASearches++
	return e.reverseSuffixSetSearcher.FindIndicesAt(haystack, at)
}

// findIndicesReverseInner searches using reverse inner optimization - zero alloc.
func (e *Engine) findIndicesReverseInner(haystack []byte) (int, int, bool) {
	if e.reverseInnerSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	e.stats.DFASearches++
	match := e.reverseInnerSearcher.Find(haystack)
	if match == nil {
		return -1, -1, false
	}
	return match.Start(), match.End(), true
}

// findIndicesReverseInnerAt searches using reverse inner optimization from position - zero alloc.
func (e *Engine) findIndicesReverseInnerAt(haystack []byte, at int) (int, int, bool) {
	if e.reverseInnerSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	e.stats.DFASearches++
	return e.reverseInnerSearcher.FindIndicesAt(haystack, at)
}

// findIndicesBoundedBacktracker searches using bounded backtracker - zero alloc.
func (e *Engine) findIndicesBoundedBacktracker(haystack []byte) (int, int, bool) {
	if e.boundedBacktracker == nil {
		return e.findIndicesNFA(haystack)
	}
	e.stats.NFASearches++
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.pikevm.Search(haystack)
	}
	return e.boundedBacktracker.Search(haystack)
}

// findIndicesBoundedBacktrackerAt searches using bounded backtracker at position.
func (e *Engine) findIndicesBoundedBacktrackerAt(haystack []byte, at int) (int, int, bool) {
	if e.boundedBacktracker == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	e.stats.NFASearches++
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.pikevm.SearchAt(haystack, at)
	}
	return e.boundedBacktracker.SearchAt(haystack, at)
}

// findBoundedBacktracker searches using bounded backtracker.
func (e *Engine) findBoundedBacktracker(haystack []byte) *Match {
	if e.boundedBacktracker == nil {
		return e.findNFA(haystack)
	}
	e.stats.NFASearches++
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.findNFA(haystack)
	}
	start, end, found := e.boundedBacktracker.Search(haystack)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findBoundedBacktrackerAt searches using bounded backtracker at position.
func (e *Engine) findBoundedBacktrackerAt(haystack []byte, at int) *Match {
	// For now, fall back to NFA for non-zero positions
	return e.findNFAAt(haystack, at)
}

// findCharClassSearcher searches using specialized char_class+ searcher.
// 14-17x faster than BoundedBacktracker for simple char_class+ patterns.
func (e *Engine) findCharClassSearcher(haystack []byte) *Match {
	if e.charClassSearcher == nil {
		return e.findNFA(haystack)
	}
	e.stats.NFASearches++ // Count as NFA-family for stats
	start, end, found := e.charClassSearcher.Search(haystack)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findCharClassSearcherAt searches using specialized char_class+ searcher at position.
func (e *Engine) findCharClassSearcherAt(haystack []byte, at int) *Match {
	if e.charClassSearcher == nil {
		return e.findNFAAt(haystack, at)
	}
	e.stats.NFASearches++
	start, end, found := e.charClassSearcher.SearchAt(haystack, at)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// isMatchCharClassSearcher checks for match using specialized char_class+ searcher.
func (e *Engine) isMatchCharClassSearcher(haystack []byte) bool {
	if e.charClassSearcher == nil {
		return e.isMatchNFA(haystack)
	}
	e.stats.NFASearches++
	return e.charClassSearcher.IsMatch(haystack)
}

// findIndicesCharClassSearcher searches using char_class+ searcher - zero alloc.
func (e *Engine) findIndicesCharClassSearcher(haystack []byte) (int, int, bool) {
	if e.charClassSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	e.stats.NFASearches++
	return e.charClassSearcher.Search(haystack)
}

// findIndicesCharClassSearcherAt searches using char_class+ searcher at position - zero alloc.
func (e *Engine) findIndicesCharClassSearcherAt(haystack []byte, at int) (int, int, bool) {
	if e.charClassSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	e.stats.NFASearches++
	return e.charClassSearcher.SearchAt(haystack, at)
}

// findTeddy searches using Teddy multi-pattern prefilter directly.
// This is the "literal engine bypass" - for exact literal alternations like (foo|bar|baz),
// Teddy.Find() returns complete matches without needing DFA/NFA verification.
func (e *Engine) findTeddy(haystack []byte) *Match {
	if e.prefilter == nil {
		return e.findNFA(haystack)
	}
	e.stats.PrefilterHits++

	// Use FindMatch which returns both start and end positions
	if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
		start, end := matcher.FindMatch(haystack, 0)
		if start == -1 {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Fallback: use Find + LiteralLen
	pos := e.prefilter.Find(haystack, 0)
	if pos == -1 {
		return nil
	}
	literalLen := e.prefilter.LiteralLen()
	if literalLen > 0 {
		return NewMatch(pos, pos+literalLen, haystack)
	}
	// If no uniform length, fall back to NFA for verification
	return e.findNFAAt(haystack, pos)
}

// findTeddyAt searches using Teddy at a specific position.
func (e *Engine) findTeddyAt(haystack []byte, at int) *Match {
	if e.prefilter == nil || at >= len(haystack) {
		return e.findNFAAt(haystack, at)
	}
	e.stats.PrefilterHits++

	// Use FindMatch which returns both start and end positions
	if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
		start, end := matcher.FindMatch(haystack, at)
		if start == -1 {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Fallback: use Find + LiteralLen
	pos := e.prefilter.Find(haystack, at)
	if pos == -1 {
		return nil
	}
	literalLen := e.prefilter.LiteralLen()
	if literalLen > 0 {
		return NewMatch(pos, pos+literalLen, haystack)
	}
	return e.findNFAAt(haystack, pos)
}

// isMatchTeddy checks for match using Teddy prefilter.
func (e *Engine) isMatchTeddy(haystack []byte) bool {
	if e.prefilter == nil {
		return e.isMatchNFA(haystack)
	}
	e.stats.PrefilterHits++
	return e.prefilter.Find(haystack, 0) != -1
}

// findIndicesTeddy returns indices using Teddy prefilter - zero alloc.
func (e *Engine) findIndicesTeddy(haystack []byte) (int, int, bool) {
	if e.prefilter == nil {
		return e.findIndicesNFA(haystack)
	}
	e.stats.PrefilterHits++

	// Use FindMatch which returns both start and end positions
	if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
		start, end := matcher.FindMatch(haystack, 0)
		if start == -1 {
			return -1, -1, false
		}
		return start, end, true
	}

	// Fallback: use Find + LiteralLen
	pos := e.prefilter.Find(haystack, 0)
	if pos == -1 {
		return -1, -1, false
	}
	literalLen := e.prefilter.LiteralLen()
	if literalLen > 0 {
		return pos, pos + literalLen, true
	}
	return e.findIndicesNFAAt(haystack, pos)
}

// findIndicesTeddyAt returns indices using Teddy at position - zero alloc.
func (e *Engine) findIndicesTeddyAt(haystack []byte, at int) (int, int, bool) {
	if e.prefilter == nil || at >= len(haystack) {
		return e.findIndicesNFAAt(haystack, at)
	}
	e.stats.PrefilterHits++

	// Use FindMatch which returns both start and end positions
	if matcher, ok := e.prefilter.(interface{ FindMatch([]byte, int) (int, int) }); ok {
		start, end := matcher.FindMatch(haystack, at)
		if start == -1 {
			return -1, -1, false
		}
		return start, end, true
	}

	// Fallback: use Find + LiteralLen
	pos := e.prefilter.Find(haystack, at)
	if pos == -1 {
		return -1, -1, false
	}
	literalLen := e.prefilter.LiteralLen()
	if literalLen > 0 {
		return pos, pos + literalLen, true
	}
	return e.findIndicesNFAAt(haystack, pos)
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

	// If prefilter available, use it to find candidate positions quickly
	if e.prefilter != nil {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return nil
		}
		e.stats.PrefilterHits++

		// Literal fast path: if prefilter is complete and we know literal length
		if e.prefilter.IsComplete() {
			literalLen := e.prefilter.LiteralLen()
			if literalLen > 0 {
				// Direct return without PikeVM - prefilter found exact match
				return NewMatch(pos, pos+literalLen, haystack)
			}
		}

		// Use anchored search from prefilter position - O(m) not O(n)!
		// This is much faster than searching the entire haystack
		start, end, matched := e.pikevm.SearchAt(haystack, pos)
		if !matched {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Use DFA search
	endPos := e.dfa.Find(haystack)
	if endPos == -1 {
		return nil
	}

	// DFA found match ending at endPos - use reverse search to find start
	// This is O(m) where m = match length, not O(n)
	// For patterns without prefilter, estimate start position
	// and search from there
	estimatedStart := 0
	if endPos > 100 {
		// For long haystacks, start search closer to the match end
		estimatedStart = endPos - 100
	}
	start, end, matched := e.pikevm.SearchAt(haystack, estimatedStart)
	if !matched {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findAdaptive tries prefilter+DFA first, falls back to NFA on failure.
func (e *Engine) findAdaptive(haystack []byte) *Match {
	// Use prefilter if available for fast candidate finding
	if e.prefilter != nil && e.dfa != nil {
		// Check if prefilter can return match bounds directly (e.g., Teddy)
		if mf, ok := e.prefilter.(prefilter.MatchFinder); ok {
			start, end := mf.FindMatch(haystack, 0)
			if start == -1 {
				return nil
			}
			e.stats.PrefilterHits++
			e.stats.DFASearches++
			return NewMatch(start, end, haystack)
		}

		// Standard prefilter path
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			// No candidate found - definitely no match
			return nil
		}
		e.stats.PrefilterHits++
		e.stats.DFASearches++

		// Literal fast path: if prefilter is complete and we know literal length
		if e.prefilter.IsComplete() {
			literalLen := e.prefilter.LiteralLen()
			if literalLen > 0 {
				// Direct return without PikeVM - prefilter found exact match
				return NewMatch(pos, pos+literalLen, haystack)
			}
		}

		// Use anchored search from prefilter position - O(m) not O(n)!
		start, end, matched := e.pikevm.SearchAt(haystack, pos)
		if !matched {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Try DFA without prefilter
	if e.dfa != nil {
		e.stats.DFASearches++
		endPos := e.dfa.Find(haystack)
		if endPos != -1 {
			// DFA succeeded - get exact match bounds from NFA
			// Use estimated start position for O(m) search instead of O(n)
			estimatedStart := 0
			if endPos > 100 {
				estimatedStart = endPos - 100
			}
			start, end, matched := e.pikevm.SearchAt(haystack, estimatedStart)
			if !matched {
				return nil
			}
			return NewMatch(start, end, haystack)
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

// findNFAAt searches using NFA starting from a specific position.
// This preserves absolute positions for correct anchor handling.
func (e *Engine) findNFAAt(haystack []byte, at int) *Match {
	e.stats.NFASearches++
	start, end, matched := e.pikevm.SearchAt(haystack, at)
	if !matched {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findDFAAt searches using DFA starting from a specific position.
// This preserves absolute positions for correct anchor handling.
func (e *Engine) findDFAAt(haystack []byte, at int) *Match {
	e.stats.DFASearches++

	// If prefilter available and complete, use literal fast path
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return nil
		}
		e.stats.PrefilterHits++
		// Literal fast path: prefilter already found exact match
		// Use LiteralLen() to calculate end position directly
		literalLen := e.prefilter.LiteralLen()
		if literalLen > 0 {
			// Direct return without PikeVM
			return NewMatch(pos, pos+literalLen, haystack)
		}
		// Fallback to NFA if LiteralLen not available (e.g., Teddy multi-pattern)
		start, end, matched := e.pikevm.SearchAt(haystack, at)
		if !matched {
			return nil
		}
		return NewMatch(start, end, haystack)
	}

	// Use DFA search with FindAt
	pos := e.dfa.FindAt(haystack, at)
	if pos == -1 {
		return nil
	}

	// DFA returns end position, but doesn't track start position
	// Fall back to NFA to get exact match bounds
	start, end, matched := e.pikevm.SearchAt(haystack, at)
	if !matched {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findAdaptiveAt tries DFA first at a specific position, falls back to NFA on failure.
func (e *Engine) findAdaptiveAt(haystack []byte, at int) *Match {
	// Try DFA first
	if e.dfa != nil {
		e.stats.DFASearches++
		pos := e.dfa.FindAt(haystack, at)
		if pos != -1 {
			// DFA succeeded - need to find start position from NFA
			start, end, matched := e.pikevm.SearchAt(haystack, at)
			if matched {
				return NewMatch(start, end, haystack)
			}
		}
		// DFA failed (might be cache full) - check cache stats
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 { // 90% full
			e.stats.DFACacheFull++
		}
	}

	// Fall back to NFA
	return e.findNFAAt(haystack, at)
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

// findReverseSuffix searches using suffix literal prefilter + reverse DFA.
func (e *Engine) findReverseSuffix(haystack []byte) *Match {
	if e.reverseSuffixSearcher == nil {
		// Fallback to NFA if reverse suffix searcher not available
		return e.findNFA(haystack)
	}

	e.stats.DFASearches++
	return e.reverseSuffixSearcher.Find(haystack)
}

// isMatchReverseSuffix checks for match using suffix prefilter + reverse DFA.
func (e *Engine) isMatchReverseSuffix(haystack []byte) bool {
	if e.reverseSuffixSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	e.stats.DFASearches++
	return e.reverseSuffixSearcher.IsMatch(haystack)
}

// findReverseSuffixSet searches using Teddy multi-suffix prefilter + reverse DFA.
func (e *Engine) findReverseSuffixSet(haystack []byte) *Match {
	if e.reverseSuffixSetSearcher == nil {
		return e.findNFA(haystack)
	}

	e.stats.DFASearches++
	return e.reverseSuffixSetSearcher.Find(haystack)
}

// isMatchReverseSuffixSet checks for match using Teddy multi-suffix prefilter.
func (e *Engine) isMatchReverseSuffixSet(haystack []byte) bool {
	if e.reverseSuffixSetSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	e.stats.DFASearches++
	return e.reverseSuffixSetSearcher.IsMatch(haystack)
}

// findReverseInner searches using inner literal prefilter + bidirectional DFA.
func (e *Engine) findReverseInner(haystack []byte) *Match {
	if e.reverseInnerSearcher == nil {
		// Fallback to NFA if reverse inner searcher not available
		return e.findNFA(haystack)
	}

	e.stats.DFASearches++
	return e.reverseInnerSearcher.Find(haystack)
}

// isMatchReverseInner checks for match using inner prefilter + bidirectional DFA.
func (e *Engine) isMatchReverseInner(haystack []byte) bool {
	if e.reverseInnerSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	e.stats.DFASearches++
	return e.reverseInnerSearcher.IsMatch(haystack)
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
	lastNonEmptyEnd := -1

	for pos <= len(haystack) {
		// Use zero-allocation FindIndicesAt
		start, end, found := e.FindIndicesAt(haystack, pos)
		if !found {
			break
		}

		// Skip empty matches at lastNonEmptyEnd (stdlib behavior)
		//nolint:gocritic // badCond: intentional - checking empty match (start==end) at lastNonEmptyEnd
		if start == end && start == lastNonEmptyEnd {
			pos++
			if pos > len(haystack) {
				break
			}
			continue
		}

		count++

		// Track non-empty match ends
		if start != end {
			lastNonEmptyEnd = end
		}

		// Move position past this match
		switch {
		case start == end:
			// Empty match: advance by 1 to avoid infinite loop
			pos = end + 1
		case end > pos:
			pos = end
		default:
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

// findDigitPrefilter searches using SIMD digit scanning + DFA verification.
// Used for digit-lead patterns like IP addresses where literal extraction fails
// but all alternation branches must start with a digit.
//
// Algorithm:
//  1. Use SIMD to find next digit position in haystack
//  2. Verify match at digit position using lazy DFA + PikeVM
//  3. If no match, continue from digit position + 1
//
// Performance:
//   - Skips non-digit regions with SIMD (15-20x faster than byte-by-byte)
//   - DFA verification is O(m) where m is pattern length
//   - Total: O(n) for scan + O(k*m) for k digit candidates
func (e *Engine) findDigitPrefilter(haystack []byte) *Match {
	if e.digitPrefilter == nil {
		return e.findNFA(haystack)
	}

	e.stats.PrefilterHits++ // Count prefilter usage
	pos := 0

	for pos < len(haystack) {
		// Use SIMD to find next digit position
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return nil // No more digits, no match possible
		}

		// Verify match at digit position using DFA
		if e.dfa != nil {
			e.stats.DFASearches++
			endPos := e.dfa.FindAt(haystack, digitPos)
			if endPos != -1 {
				// DFA found potential match - get exact bounds from NFA
				start, end, found := e.pikevm.SearchAt(haystack, digitPos)
				if found {
					return NewMatch(start, end, haystack)
				}
			}
		} else {
			// No DFA - use PikeVM directly
			e.stats.NFASearches++
			start, end, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return NewMatch(start, end, haystack)
			}
		}

		// No match at this digit position, continue searching
		pos = digitPos + 1
	}

	return nil
}

// findDigitPrefilterAt searches using digit prefilter starting at position 'at'.
func (e *Engine) findDigitPrefilterAt(haystack []byte, at int) *Match {
	if e.digitPrefilter == nil || at >= len(haystack) {
		return e.findNFAAt(haystack, at)
	}

	e.stats.PrefilterHits++
	pos := at

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return nil
		}

		if e.dfa != nil {
			e.stats.DFASearches++
			endPos := e.dfa.FindAt(haystack, digitPos)
			if endPos != -1 {
				start, end, found := e.pikevm.SearchAt(haystack, digitPos)
				if found {
					return NewMatch(start, end, haystack)
				}
			}
		} else {
			e.stats.NFASearches++
			start, end, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return NewMatch(start, end, haystack)
			}
		}

		pos = digitPos + 1
	}

	return nil
}

// isMatchDigitPrefilter checks for match using digit prefilter.
// Optimized for boolean matching with early termination.
func (e *Engine) isMatchDigitPrefilter(haystack []byte) bool {
	if e.digitPrefilter == nil {
		return e.isMatchNFA(haystack)
	}

	e.stats.PrefilterHits++
	pos := 0

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return false // No more digits
		}

		// Use DFA for fast boolean check if available
		if e.dfa != nil {
			e.stats.DFASearches++
			// DFA.FindAt returns end position if match, -1 otherwise
			if e.dfa.FindAt(haystack, digitPos) != -1 {
				return true
			}
		} else {
			e.stats.NFASearches++
			_, _, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return true
			}
		}

		pos = digitPos + 1
	}

	return false
}

// findIndicesDigitPrefilter returns indices using digit prefilter - zero alloc.
func (e *Engine) findIndicesDigitPrefilter(haystack []byte) (int, int, bool) {
	if e.digitPrefilter == nil {
		return e.findIndicesNFA(haystack)
	}

	e.stats.PrefilterHits++
	pos := 0

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return -1, -1, false
		}

		if e.dfa != nil {
			e.stats.DFASearches++
			endPos := e.dfa.FindAt(haystack, digitPos)
			if endPos != -1 {
				start, end, found := e.pikevm.SearchAt(haystack, digitPos)
				if found {
					return start, end, true
				}
			}
		} else {
			e.stats.NFASearches++
			start, end, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return start, end, true
			}
		}

		pos = digitPos + 1
	}

	return -1, -1, false
}

// findIndicesDigitPrefilterAt returns indices starting at position 'at' - zero alloc.
func (e *Engine) findIndicesDigitPrefilterAt(haystack []byte, at int) (int, int, bool) {
	if e.digitPrefilter == nil || at >= len(haystack) {
		return e.findIndicesNFAAt(haystack, at)
	}

	e.stats.PrefilterHits++
	pos := at

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return -1, -1, false
		}

		if e.dfa != nil {
			e.stats.DFASearches++
			endPos := e.dfa.FindAt(haystack, digitPos)
			if endPos != -1 {
				start, end, found := e.pikevm.SearchAt(haystack, digitPos)
				if found {
					return start, end, true
				}
			}
		} else {
			e.stats.NFASearches++
			start, end, found := e.pikevm.SearchAt(haystack, digitPos)
			if found {
				return start, end, true
			}
		}

		pos = digitPos + 1
	}

	return -1, -1, false
}

// findAhoCorasick searches using Aho-Corasick automaton for large literal alternations.
// This is the "literal engine bypass" for patterns with >8 literals.
// The automaton performs O(n) multi-pattern matching with ~1.6 GB/s throughput.
func (e *Engine) findAhoCorasick(haystack []byte) *Match {
	if e.ahoCorasick == nil {
		return e.findNFA(haystack)
	}
	e.stats.AhoCorasickSearches++

	m := e.ahoCorasick.Find(haystack, 0)
	if m == nil {
		return nil
	}
	return NewMatch(m.Start, m.End, haystack)
}

// findAhoCorasickAt searches using Aho-Corasick starting at position 'at'.
func (e *Engine) findAhoCorasickAt(haystack []byte, at int) *Match {
	if e.ahoCorasick == nil || at >= len(haystack) {
		return e.findNFAAt(haystack, at)
	}
	e.stats.AhoCorasickSearches++

	m := e.ahoCorasick.Find(haystack, at)
	if m == nil {
		return nil
	}
	return NewMatch(m.Start, m.End, haystack)
}

// isMatchAhoCorasick checks for match using Aho-Corasick automaton.
// Optimized for boolean matching with zero allocations.
func (e *Engine) isMatchAhoCorasick(haystack []byte) bool {
	if e.ahoCorasick == nil {
		return e.isMatchNFA(haystack)
	}
	e.stats.AhoCorasickSearches++
	return e.ahoCorasick.IsMatch(haystack)
}

// findIndicesAhoCorasick returns indices using Aho-Corasick - zero alloc.
func (e *Engine) findIndicesAhoCorasick(haystack []byte) (int, int, bool) {
	if e.ahoCorasick == nil {
		return e.findIndicesNFA(haystack)
	}
	e.stats.AhoCorasickSearches++

	m := e.ahoCorasick.Find(haystack, 0)
	if m == nil {
		return -1, -1, false
	}
	return m.Start, m.End, true
}

// findIndicesAhoCorasickAt returns indices using Aho-Corasick starting at position 'at' - zero alloc.
func (e *Engine) findIndicesAhoCorasickAt(haystack []byte, at int) (int, int, bool) {
	if e.ahoCorasick == nil || at >= len(haystack) {
		return e.findIndicesNFAAt(haystack, at)
	}
	e.stats.AhoCorasickSearches++

	m := e.ahoCorasick.Find(haystack, at)
	if m == nil {
		return -1, -1, false
	}
	return m.Start, m.End, true
}

// CompileError represents a pattern compilation error.
type CompileError struct {
	Pattern string
	Err     error
}

// Error implements the error interface.
// For syntax errors, returns the error directly to match stdlib behavior.
func (e *CompileError) Error() string {
	// If the underlying error is from regexp/syntax, return it directly
	// to match stdlib behavior (no extra prefix)
	var syntaxErr *syntax.Error
	if errors.As(e.Err, &syntaxErr) {
		return e.Err.Error()
	}
	// For other errors, add the regexp: prefix
	return "regexp: " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *CompileError) Unwrap() error {
	return e.Err
}
