package meta

import (
	"errors"
	"regexp/syntax"
	"sync/atomic"

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
// Thread safety: The Engine uses a sync.Pool internally to provide thread-safe
// concurrent access. Multiple goroutines can safely call search methods (Find,
// IsMatch, FindSubmatch, etc.) on the same Engine instance concurrently.
//
// The underlying NFA, DFA, and prefilters are immutable after compilation.
// Per-search mutable state is managed via sync.Pool, following the Go stdlib
// regexp package pattern.
//
// Example:
//
//	// Compile pattern (once)
//	engine, err := meta.Compile("(foo|bar)\\d+")
//	if err != nil {
//	    return err
//	}
//
//	// Search (safe to call from multiple goroutines)
//	haystack := []byte("test foo123 end")
//	match := engine.Find(haystack)
//	if match != nil {
//	    println(match.String()) // "foo123"
//	}
type Engine struct {
	// Statistics (useful for debugging and tuning)
	// IMPORTANT: stats MUST be first field for proper 8-byte alignment on 32-bit platforms.
	// This ensures atomic operations on uint64 fields work correctly.
	stats Stats

	nfa                      *nfa.NFA
	dfa                      *lazy.DFA
	pikevm                   *nfa.PikeVM
	boundedBacktracker       *nfa.BoundedBacktracker
	charClassSearcher        *nfa.CharClassSearcher    // Specialized searcher for char_class+ patterns
	compositeSearcher        *nfa.CompositeSearcher    // For concatenated char classes like [a-zA-Z]+[0-9]+
	compositeSequenceDFA     *nfa.CompositeSequenceDFA // DFA for composite patterns (faster than backtracking)
	branchDispatcher         *nfa.BranchDispatcher  // O(1) branch dispatch for anchored alternations
	anchoredFirstBytes       *nfa.FirstByteSet      // O(1) first-byte rejection for anchored patterns
	reverseSearcher          *ReverseAnchoredSearcher
	reverseSuffixSearcher    *ReverseSuffixSearcher
	reverseSuffixSetSearcher *ReverseSuffixSetSearcher
	reverseInnerSearcher     *ReverseInnerSearcher
	digitPrefilter           *prefilter.DigitPrefilter // For digit-lead patterns like IP addresses
	ahoCorasick              *ahocorasick.Automaton    // For large literal alternations (>32 patterns)
	prefilter                prefilter.Prefilter
	strategy                 Strategy
	config                   Config

	// fatTeddyFallback is an Aho-Corasick automaton used as fallback for small haystacks
	// when the main prefilter is Fat Teddy (33-64 patterns). Fat Teddy's AVX2 SIMD setup
	// overhead makes it slower than Aho-Corasick for haystacks < 64 bytes.
	// Reference: rust-aho-corasick/src/packed/teddy/builder.rs:585 (minimum_len fallback)
	fatTeddyFallback *ahocorasick.Automaton

	// OnePass DFA for anchored patterns with captures (optional optimization)
	// This is independent of strategy - used by FindSubmatch when available
	// Note: The cache is now stored in pooled SearchState for thread-safety
	onepass *onepass.DFA

	// statePool provides thread-safe pooling of per-search mutable state.
	// This enables concurrent searches on the same Engine instance.
	statePool *searchStatePool

	// longest enables leftmost-longest (POSIX) matching semantics
	// By default (false), uses leftmost-first (Perl) semantics
	longest bool

	// canMatchEmpty is true if the pattern can match an empty string.
	// When true, BoundedBacktracker cannot be used for Find operations
	// because its greedy semantics give wrong results for patterns like (?:|a)*
	canMatchEmpty bool

	// isStartAnchored is true if the pattern is anchored at start (^).
	// Used for first-byte prefilter optimization.
	isStartAnchored bool
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

	// PrefilterAbandoned counts times prefilter was abandoned due to high FP rate
	PrefilterAbandoned uint64

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

// buildOnePassDFA tries to build a OnePass DFA for anchored patterns with captures.
// This is an optional optimization for FindSubmatch (10-20x faster).
// Note: The cache is now created per-search in pooled SearchState for thread-safety.
func buildOnePassDFA(re *syntax.Regexp, nfaEngine *nfa.NFA, config Config) *onepass.DFA {
	if !config.EnableDFA || nfaEngine.CaptureCount() <= 1 {
		return nil
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
		return nil
	}

	// Try to build one-pass DFA
	onepassDFA, err := onepass.Build(anchoredNFA)
	if err != nil {
		return nil
	}

	return onepassDFA
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

	// Build Aho-Corasick automaton for large literal alternations (>32 patterns)
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

// charClassSearcherResult holds the result of building specialized searchers.
type charClassSearcherResult struct {
	boundedBT          *nfa.BoundedBacktracker
	charClassSrch      *nfa.CharClassSearcher
	compositeSrch      *nfa.CompositeSearcher
	compositeSeqDFA    *nfa.CompositeSequenceDFA // DFA (faster than backtracking)
	branchDispatcher   *nfa.BranchDispatcher
	finalStrategy      Strategy
}

func buildCharClassSearchers(
	strategy Strategy,
	re *syntax.Regexp,
	nfaEngine *nfa.NFA,
) charClassSearcherResult {
	result := charClassSearcherResult{finalStrategy: strategy}

	if strategy == UseBoundedBacktracker {
		result.boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
	}

	if strategy == UseCharClassSearcher {
		ranges := nfa.ExtractCharClassRanges(re)
		if ranges != nil {
			// Determine minMatch: 1 for +, 0 for *
			minMatch := 1
			if re.Op == syntax.OpStar {
				minMatch = 0
			}
			result.charClassSrch = nfa.NewCharClassSearcher(ranges, minMatch)
		} else {
			// Fallback to BoundedBacktracker if extraction fails
			result.finalStrategy = UseBoundedBacktracker
			result.boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
		}
	}

	// CompositeSearcher for concatenated char classes like [a-zA-Z]+[0-9]+
	// Reference: https://github.com/coregx/coregex/issues/72
	if strategy == UseCompositeSearcher {
		result.compositeSrch = nfa.NewCompositeSearcher(re)
		if result.compositeSrch == nil {
			// Fallback to BoundedBacktracker if extraction fails
			result.finalStrategy = UseBoundedBacktracker
			result.boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
		} else {
			// Try to build faster DFA (uses subset construction for overlapping patterns)
			result.compositeSeqDFA = nfa.NewCompositeSequenceDFA(re)
		}
	}

	// BranchDispatcher for anchored alternations with distinct first bytes
	// Reference: https://github.com/coregx/coregex/issues/79
	if strategy == UseBranchDispatch {
		// Extract the alternation part (skip ^ anchor)
		altPart := re
		if re.Op == syntax.OpConcat && len(re.Sub) >= 2 {
			// Skip start anchor, get the rest
			for _, sub := range re.Sub[1:] {
				if sub.Op == syntax.OpAlternate || sub.Op == syntax.OpCapture {
					altPart = sub
					break
				}
			}
		}
		result.branchDispatcher = nfa.NewBranchDispatcher(altPart)
		if result.branchDispatcher == nil {
			// Fallback to BoundedBacktracker if dispatch not possible
			result.finalStrategy = UseBoundedBacktracker
			result.boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
		}
	}

	// For UseNFA with small NFAs, also create BoundedBacktracker as fallback.
	// BoundedBacktracker is 2-3x faster than PikeVM on small inputs due to
	// generation-based visited tracking (O(1) reset) vs PikeVM's thread queues.
	// This is similar to how stdlib uses backtracking for simple patterns.
	if result.finalStrategy == UseNFA && result.boundedBT == nil && nfaEngine.States() < 50 {
		result.boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
	}

	return result
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
	charClassResult := buildCharClassSearchers(strategy, re, nfaEngine)
	strategy = charClassResult.finalStrategy

	// Check if pattern can match empty string.
	// If true, BoundedBacktracker cannot be used for Find operations
	// because its greedy semantics give wrong results for patterns like (?:|a)*
	canMatchEmpty := pikevm.IsMatch(nil)

	// Extract first-byte prefilter for anchored patterns.
	// This enables O(1) early rejection for non-matching inputs.
	// Only useful for start-anchored patterns where we only check position 0.
	var anchoredFirstBytes *nfa.FirstByteSet
	if isStartAnchored && strategy == UseBoundedBacktracker {
		fb := nfa.ExtractFirstBytes(re)
		if fb != nil && fb.IsUseful() {
			anchoredFirstBytes = fb
		}
	}

	// Build Aho-Corasick fallback for Fat Teddy patterns.
	// Fat Teddy's AVX2 SIMD has setup overhead that makes it slower than Aho-Corasick
	// for small haystacks (< 64 bytes). This matches Rust regex's minimum_len() approach.
	var fatTeddyFallback *ahocorasick.Automaton
	if strategy == UseTeddy {
		if fatTeddy, ok := pf.(*prefilter.FatTeddy); ok {
			builder := ahocorasick.NewBuilder()
			for _, pattern := range fatTeddy.Patterns() {
				builder.AddPattern(pattern)
			}
			if auto, err := builder.Build(); err == nil {
				fatTeddyFallback = auto
			}
		}
	}

	// Initialize state pool for thread-safe concurrent searches
	numCaptures := nfaEngine.CaptureCount()

	return &Engine{
		nfa:                      nfaEngine,
		dfa:                      engines.dfa,
		pikevm:                   pikevm,
		boundedBacktracker:       charClassResult.boundedBT,
		charClassSearcher:        charClassResult.charClassSrch,
		compositeSearcher:        charClassResult.compositeSrch,
		compositeSequenceDFA:     charClassResult.compositeSeqDFA,
		branchDispatcher:         charClassResult.branchDispatcher,
		anchoredFirstBytes:       anchoredFirstBytes,
		reverseSearcher:          engines.reverseSearcher,
		reverseSuffixSearcher:    engines.reverseSuffixSearcher,
		reverseSuffixSetSearcher: engines.reverseSuffixSetSearcher,
		reverseInnerSearcher:     engines.reverseInnerSearcher,
		digitPrefilter:           engines.digitPrefilter,
		ahoCorasick:              engines.ahoCorasick,
		prefilter:                pf,
		strategy:                 strategy,
		config:                   config,
		onepass:                  onePassRes,
		canMatchEmpty:            canMatchEmpty,
		isStartAnchored:          isStartAnchored,
		fatTeddyFallback:         fatTeddyFallback,
		statePool:                newSearchStatePool(nfaEngine, numCaptures),
		stats:                    Stats{},
	}, nil
}

// getSearchState retrieves a SearchState from the pool.
// Caller must call putSearchState when done.
// The returned state contains its own PikeVM instance for thread-safe concurrent use.
func (e *Engine) getSearchState() *SearchState {
	state := e.statePool.get()

	// Initialize state for BoundedBacktracker if needed
	if e.boundedBacktracker != nil && state.backtracker != nil {
		state.backtracker.Longest = e.longest
	}

	// PikeVM is already created per-state, just set longest flag if needed
	if state.pikevm != nil {
		state.pikevm.SetLongest(e.longest)
	}

	return state
}

// putSearchState returns a SearchState to the pool.
func (e *Engine) putSearchState(state *SearchState) {
	e.statePool.put(state)
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
	case UseCompositeSearcher:
		return e.findCompositeSearcher(haystack)
	case UseBranchDispatch:
		return e.findBranchDispatch(haystack)
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
	case UseCompositeSearcher:
		return e.findCompositeSearcherAt(haystack, at)
	case UseBranchDispatch:
		return e.findBranchDispatchAt(haystack, at)
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
	case UseCompositeSearcher:
		return e.isMatchCompositeSearcher(haystack)
	case UseBranchDispatch:
		return e.isMatchBranchDispatch(haystack)
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
// Thread-safe: uses pooled state for both BoundedBacktracker and PikeVM.
func (e *Engine) isMatchNFA(haystack []byte) bool {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// BoundedBacktracker is preferred when available (supports both default and Longest modes)
	useBT := e.boundedBacktracker != nil

	// Get pooled state for thread-safe execution
	state := e.getSearchState()
	defer e.putSearchState(state)

	// Use prefilter for skip-ahead if available
	if e.prefilter != nil {
		at := 0
		for at < len(haystack) {
			// Find next candidate position via prefilter
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return false // No more candidates
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)

			// Try to match at candidate position
			// Prefer BoundedBacktracker for small inputs (2-3x faster)
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				_, _, found = e.boundedBacktracker.SearchAtWithState(haystack, pos, state.backtracker)
			} else {
				_, _, found = state.pikevm.SearchAt(haystack, pos)
			}
			if found {
				return true
			}

			// Move past this position
			atomic.AddUint64(&e.stats.PrefilterMisses, 1)
			at = pos + 1
		}
		return false
	}

	// No prefilter: use BoundedBacktracker if available, else PikeVM
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.boundedBacktracker.IsMatchWithState(haystack, state.backtracker)
	}

	// Use optimized IsMatch that returns immediately on first match
	// without computing exact match positions
	return state.pikevm.IsMatch(haystack)
}

// isMatchDFA checks for match using DFA with early termination.
func (e *Engine) isMatchDFA(haystack []byte) bool {
	atomic.AddUint64(&e.stats.DFASearches, 1)

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
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		// For complete prefilters (Teddy with literals), the find is sufficient
		if e.prefilter.IsComplete() {
			return true
		}
		// Verify with NFA for incomplete prefilters
		return e.isMatchNFA(haystack)
	}

	// Fall back to DFA
	if e.dfa != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		if e.dfa.IsMatch(haystack) {
			return true
		}
		// DFA returned false - check if cache was full
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 {
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
			// Cache nearly full, fall back to NFA
			return e.isMatchNFA(haystack)
		}
		return false
	}
	return e.isMatchNFA(haystack)
}

// isMatchBoundedBacktracker checks for match using bounded backtracker.
// 2-4x faster than PikeVM for simple character class patterns.
// Thread-safe: uses pooled state.
func (e *Engine) isMatchBoundedBacktracker(haystack []byte) bool {
	if e.boundedBacktracker == nil {
		return e.isMatchNFA(haystack)
	}

	// O(1) early rejection for anchored patterns using first-byte prefilter.
	// For ^(\d+|UUID|hex32), quickly reject inputs not starting with valid byte.
	if e.anchoredFirstBytes != nil && len(haystack) > 0 {
		if !e.anchoredFirstBytes.Contains(haystack[0]) {
			return false
		}
	}

	atomic.AddUint64(&e.stats.NFASearches, 1) // Count as NFA-family search for stats
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		// Input too large for bounded backtracker, fall back to PikeVM
		return e.pikevm.IsMatch(haystack)
	}

	// Use pooled state for thread-safety
	state := e.getSearchState()
	defer e.putSearchState(state)
	return e.boundedBacktracker.IsMatchWithState(haystack, state.backtracker)
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
// Thread-safe: uses pooled state for both OnePass cache and PikeVM.
func (e *Engine) FindSubmatchAt(haystack []byte, at int) *MatchWithCaptures {
	// Get pooled state first for thread-safe access
	state := e.getSearchState()
	defer e.putSearchState(state)

	// For position 0, try OnePass DFA if available (10-20x faster for anchored patterns)
	if at == 0 && e.onepass != nil && state.onepassCache != nil {
		atomic.AddUint64(&e.stats.OnePassSearches, 1)
		slots := e.onepass.Search(haystack, state.onepassCache)
		if slots != nil {
			// Convert flat slots [start0, end0, start1, end1, ...] to nested captures
			captures := slotsToCaptures(slots)
			return NewMatchWithCaptures(haystack, captures)
		}
		// OnePass failed (input doesn't match from position 0)
		// Fall through to PikeVM which can find match anywhere
	}

	atomic.AddUint64(&e.stats.NFASearches, 1)

	nfaMatch := state.pikevm.SearchWithCapturesAt(haystack, at)
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
	case UseCompositeSearcher:
		return e.findIndicesCompositeSearcher(haystack)
	case UseBranchDispatch:
		return e.findIndicesBranchDispatch(haystack)
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
	case UseCompositeSearcher:
		return e.findIndicesCompositeSearcherAt(haystack, at)
	case UseBranchDispatch:
		return e.findIndicesBranchDispatchAt(haystack, at)
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
// Thread-safe: uses pooled state for both BoundedBacktracker and PikeVM.
func (e *Engine) findIndicesNFA(haystack []byte) (int, int, bool) {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// BoundedBacktracker can be used for Find operations only when:
	// 1. It's available
	// 2. Pattern cannot match empty (BT has greedy semantics that break empty match handling)
	useBT := e.boundedBacktracker != nil && !e.canMatchEmpty

	// Get pooled state for thread-safe execution
	state := e.getSearchState()
	defer e.putSearchState(state)

	// Use prefilter for skip-ahead if available
	if e.prefilter != nil {
		at := 0
		for at < len(haystack) {
			// Find next candidate position via prefilter
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return -1, -1, false // No more candidates
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)

			// Try to match at candidate position
			var start, end int
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				start, end, found = e.boundedBacktracker.SearchAtWithState(haystack, pos, state.backtracker)
			} else {
				start, end, found = state.pikevm.SearchAt(haystack, pos)
			}
			if found {
				return start, end, true
			}

			// Move past this position
			atomic.AddUint64(&e.stats.PrefilterMisses, 1)
			at = pos + 1
		}
		return -1, -1, false
	}

	// No prefilter: use BoundedBacktracker if available and safe
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.boundedBacktracker.SearchWithState(haystack, state.backtracker)
	}

	return state.pikevm.Search(haystack)
}

// findIndicesNFAAt searches using NFA starting at position - zero alloc.
// Uses prefilter for skip-ahead when available (like Rust regex).
// Same BoundedBacktracker rules as findIndicesNFA.
// Thread-safe: uses pooled state for both BoundedBacktracker and PikeVM.
func (e *Engine) findIndicesNFAAt(haystack []byte, at int) (int, int, bool) {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	// BoundedBacktracker can be used for Find operations only when safe
	useBT := e.boundedBacktracker != nil && !e.canMatchEmpty

	// Get pooled state for thread-safe execution
	state := e.getSearchState()
	defer e.putSearchState(state)

	// Use prefilter for skip-ahead if available
	if e.prefilter != nil {
		for at < len(haystack) {
			// Find next candidate position via prefilter
			pos := e.prefilter.Find(haystack, at)
			if pos == -1 {
				return -1, -1, false // No more candidates
			}
			atomic.AddUint64(&e.stats.PrefilterHits, 1)

			// Try to match at candidate position
			var start, end int
			var found bool
			if useBT && e.boundedBacktracker.CanHandle(len(haystack)-pos) {
				start, end, found = e.boundedBacktracker.SearchAtWithState(haystack, pos, state.backtracker)
			} else {
				start, end, found = state.pikevm.SearchAt(haystack, pos)
			}
			if found {
				return start, end, true
			}

			// Move past this position
			atomic.AddUint64(&e.stats.PrefilterMisses, 1)
			at = pos + 1
		}
		return -1, -1, false
	}

	// No prefilter: use BoundedBacktracker if available and safe
	if useBT && e.boundedBacktracker.CanHandle(len(haystack)-at) {
		return e.boundedBacktracker.SearchAtWithState(haystack, at, state.backtracker)
	}

	return state.pikevm.SearchAt(haystack, at)
}

// findIndicesDFA searches using DFA with prefilter - zero alloc.
func (e *Engine) findIndicesDFA(haystack []byte) (int, int, bool) {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// Literal fast path
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
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
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// Literal fast path
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
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
			atomic.AddUint64(&e.stats.PrefilterHits, 1)
			atomic.AddUint64(&e.stats.DFASearches, 1)
			return start, end, true
		}

		// Standard prefilter path
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			// No candidate found - definitely no match
			return -1, -1, false
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		atomic.AddUint64(&e.stats.DFASearches, 1)

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
		atomic.AddUint64(&e.stats.DFASearches, 1)
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
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
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
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		atomic.AddUint64(&e.stats.DFASearches, 1)

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
		atomic.AddUint64(&e.stats.DFASearches, 1)
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
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
		}
	}
	return e.findIndicesNFAAt(haystack, at)
}

// findIndicesReverseAnchored searches using reverse DFA - zero alloc.
func (e *Engine) findIndicesReverseAnchored(haystack []byte) (int, int, bool) {
	if e.reverseSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
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
	atomic.AddUint64(&e.stats.DFASearches, 1)
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
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSearcher.FindIndicesAt(haystack, at)
}

// findIndicesReverseSuffixSet searches using reverse suffix SET optimization - zero alloc.
func (e *Engine) findIndicesReverseSuffixSet(haystack []byte) (int, int, bool) {
	if e.reverseSuffixSetSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
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
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSetSearcher.FindIndicesAt(haystack, at)
}

// findIndicesReverseInner searches using reverse inner optimization - zero alloc.
func (e *Engine) findIndicesReverseInner(haystack []byte) (int, int, bool) {
	if e.reverseInnerSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.DFASearches, 1)
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
	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseInnerSearcher.FindIndicesAt(haystack, at)
}

// findIndicesBoundedBacktracker searches using bounded backtracker - zero alloc.
// Thread-safe: uses pooled state.
func (e *Engine) findIndicesBoundedBacktracker(haystack []byte) (int, int, bool) {
	if e.boundedBacktracker == nil {
		return e.findIndicesNFA(haystack)
	}

	// O(1) early rejection for anchored patterns using first-byte prefilter.
	if e.anchoredFirstBytes != nil && len(haystack) > 0 {
		if !e.anchoredFirstBytes.Contains(haystack[0]) {
			return -1, -1, false
		}
	}

	atomic.AddUint64(&e.stats.NFASearches, 1)
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.pikevm.Search(haystack)
	}

	state := e.getSearchState()
	defer e.putSearchState(state)
	return e.boundedBacktracker.SearchWithState(haystack, state.backtracker)
}

// findIndicesBoundedBacktrackerAt searches using bounded backtracker at position.
// Thread-safe: uses pooled state.
func (e *Engine) findIndicesBoundedBacktrackerAt(haystack []byte, at int) (int, int, bool) {
	if e.boundedBacktracker == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.pikevm.SearchAt(haystack, at)
	}

	state := e.getSearchState()
	defer e.putSearchState(state)
	return e.boundedBacktracker.SearchAtWithState(haystack, at, state.backtracker)
}

// findBoundedBacktracker searches using bounded backtracker.
// Thread-safe: uses pooled state.
func (e *Engine) findBoundedBacktracker(haystack []byte) *Match {
	if e.boundedBacktracker == nil {
		return e.findNFA(haystack)
	}

	// O(1) early rejection for anchored patterns using first-byte prefilter.
	// For ^(\d+|UUID|hex32), quickly reject inputs not starting with valid byte.
	if e.anchoredFirstBytes != nil && len(haystack) > 0 {
		if !e.anchoredFirstBytes.Contains(haystack[0]) {
			return nil
		}
	}

	atomic.AddUint64(&e.stats.NFASearches, 1)
	if !e.boundedBacktracker.CanHandle(len(haystack)) {
		return e.findNFA(haystack)
	}

	state := e.getSearchState()
	defer e.putSearchState(state)
	start, end, found := e.boundedBacktracker.SearchWithState(haystack, state.backtracker)
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
	atomic.AddUint64(&e.stats.NFASearches, 1) // Count as NFA-family for stats
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
	atomic.AddUint64(&e.stats.NFASearches, 1)
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
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.charClassSearcher.IsMatch(haystack)
}

// findIndicesCharClassSearcher searches using char_class+ searcher - zero alloc.
func (e *Engine) findIndicesCharClassSearcher(haystack []byte) (int, int, bool) {
	if e.charClassSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.charClassSearcher.Search(haystack)
}

// findIndicesCharClassSearcherAt searches using char_class+ searcher at position - zero alloc.
func (e *Engine) findIndicesCharClassSearcherAt(haystack []byte, at int) (int, int, bool) {
	if e.charClassSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.charClassSearcher.SearchAt(haystack, at)
}

// findCompositeSearcher searches using CompositeSearcher for concatenated char classes.
func (e *Engine) findCompositeSearcher(haystack []byte) *Match {
	if e.compositeSearcher == nil {
		return e.findNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1) // Count as NFA-family for stats
	start, end, found := e.compositeSearcher.Search(haystack)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findCompositeSearcherAt searches using CompositeSearcher at position.
func (e *Engine) findCompositeSearcherAt(haystack []byte, at int) *Match {
	if e.compositeSearcher == nil {
		return e.findNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	start, end, found := e.compositeSearcher.SearchAt(haystack, at)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// isMatchCompositeSearcher checks for match using CompositeSearcher.
func (e *Engine) isMatchCompositeSearcher(haystack []byte) bool {
	// Prefer DFA over backtracking
	if e.compositeSequenceDFA != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		return e.compositeSequenceDFA.IsMatch(haystack)
	}
	if e.compositeSearcher == nil {
		return e.isMatchNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.compositeSearcher.IsMatch(haystack)
}

// findIndicesCompositeSearcher searches using CompositeSearcher - zero alloc.
func (e *Engine) findIndicesCompositeSearcher(haystack []byte) (int, int, bool) {
	// Prefer DFA over backtracking (2-4x faster for overlapping patterns)
	if e.compositeSequenceDFA != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		return e.compositeSequenceDFA.Search(haystack)
	}
	if e.compositeSearcher == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.compositeSearcher.Search(haystack)
}

// findIndicesCompositeSearcherAt searches using CompositeSearcher at position - zero alloc.
func (e *Engine) findIndicesCompositeSearcherAt(haystack []byte, at int) (int, int, bool) {
	// Prefer DFA over backtracking (2-4x faster for overlapping patterns)
	if e.compositeSequenceDFA != nil {
		atomic.AddUint64(&e.stats.DFASearches, 1)
		return e.compositeSequenceDFA.SearchAt(haystack, at)
	}
	if e.compositeSearcher == nil {
		return e.findIndicesNFAAt(haystack, at)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.compositeSearcher.SearchAt(haystack, at)
}

// findBranchDispatch searches using O(1) branch dispatch for anchored alternations.
// 2-3x faster than BoundedBacktracker on match, 10x+ on no-match.
func (e *Engine) findBranchDispatch(haystack []byte) *Match {
	if e.branchDispatcher == nil {
		return e.findBoundedBacktracker(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	start, end, found := e.branchDispatcher.Search(haystack)
	if !found {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findBranchDispatchAt searches using branch dispatch at position.
// For anchored patterns, only position 0 is meaningful.
func (e *Engine) findBranchDispatchAt(haystack []byte, at int) *Match {
	if at != 0 {
		// Anchored pattern can only match at position 0
		return nil
	}
	return e.findBranchDispatch(haystack)
}

// isMatchBranchDispatch checks for match using O(1) branch dispatch.
func (e *Engine) isMatchBranchDispatch(haystack []byte) bool {
	if e.branchDispatcher == nil {
		return e.isMatchBoundedBacktracker(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.branchDispatcher.IsMatch(haystack)
}

// findIndicesBranchDispatch searches using branch dispatch - zero alloc.
func (e *Engine) findIndicesBranchDispatch(haystack []byte) (int, int, bool) {
	if e.branchDispatcher == nil {
		return e.findIndicesBoundedBacktracker(haystack)
	}
	atomic.AddUint64(&e.stats.NFASearches, 1)
	return e.branchDispatcher.Search(haystack)
}

// findIndicesBranchDispatchAt searches using branch dispatch at position - zero alloc.
func (e *Engine) findIndicesBranchDispatchAt(haystack []byte, at int) (int, int, bool) {
	if at != 0 {
		// Anchored pattern can only match at position 0
		return -1, -1, false
	}
	return e.findIndicesBranchDispatch(haystack)
}

// FindAllIndicesStreaming returns all non-overlapping match indices using streaming algorithm.
// For CharClassSearcher strategy, this uses single-pass state machine which is significantly
// faster than repeated FindIndicesAt calls (no per-match function call overhead).
//
// Returns slice of [2]int{start, end} pairs. Limit n (0=no limit) restricts match count.
// The results slice is reused if provided (pass nil for fresh allocation).
//
// This method is optimized for patterns like \w+, \d+, [a-z]+ where matches are frequent.
func (e *Engine) FindAllIndicesStreaming(haystack []byte, n int, results [][2]int) [][2]int {
	// Only CharClassSearcher benefits from streaming - others use standard loop
	if e.strategy != UseCharClassSearcher || e.charClassSearcher == nil {
		return e.findAllIndicesLoop(haystack, n, results)
	}

	// Use streaming state machine for CharClassSearcher
	allMatches := e.charClassSearcher.FindAllIndices(haystack, results)

	// Apply limit if specified
	if n > 0 && len(allMatches) > n {
		return allMatches[:n]
	}

	return allMatches
}

// findAllIndicesLoop is the standard loop-based FindAll for non-streaming strategies.
func (e *Engine) findAllIndicesLoop(haystack []byte, n int, results [][2]int) [][2]int {
	if results == nil {
		results = make([][2]int, 0, len(haystack)/100+1)
	} else {
		results = results[:0]
	}

	pos := 0
	lastMatchEnd := -1

	for n <= 0 || len(results) < n {
		start, end, found := e.FindIndicesAt(haystack, pos)
		if !found {
			break
		}

		// Handle empty matches
		if start == end {
			if start == lastMatchEnd {
				// Skip empty match right after previous match to avoid infinite loop
				if pos < len(haystack) {
					pos++
				} else {
					break
				}
				continue
			}
			pos = end
			if pos < len(haystack) {
				pos++
			}
		} else {
			pos = end
			lastMatchEnd = end
		}

		results = append(results, [2]int{start, end})
	}

	return results
}

// fatTeddySmallHaystackThreshold is the minimum haystack size for Fat Teddy efficiency.
// For smaller haystacks, Aho-Corasick is faster due to lower setup overhead.
// Benchmarks show ~2x speedup with Aho-Corasick on 37-byte haystacks with 50 patterns.
// Reference: rust-aho-corasick/src/packed/teddy/builder.rs (minimum_len fallback)
const fatTeddySmallHaystackThreshold = 64

// findTeddy searches using Teddy multi-pattern prefilter directly.
// This is the "literal engine bypass" - for exact literal alternations like (foo|bar|baz),
// Teddy.Find() returns complete matches without needing DFA/NFA verification.
func (e *Engine) findTeddy(haystack []byte) *Match {
	if e.prefilter == nil {
		return e.findNFA(haystack)
	}

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	// Fat Teddy's AVX2 SIMD setup overhead exceeds benefit on small inputs.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		match := e.fatTeddyFallback.Find(haystack, 0)
		if match == nil {
			return nil
		}
		return NewMatch(match.Start, match.End, haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)

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

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		match := e.fatTeddyFallback.FindAt(haystack, at)
		if match == nil {
			return nil
		}
		return NewMatch(match.Start, match.End, haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)

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

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		return e.fatTeddyFallback.IsMatch(haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	return e.prefilter.Find(haystack, 0) != -1
}

// findIndicesTeddy returns indices using Teddy prefilter - zero alloc.
func (e *Engine) findIndicesTeddy(haystack []byte) (int, int, bool) {
	if e.prefilter == nil {
		return e.findIndicesNFA(haystack)
	}

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		match := e.fatTeddyFallback.Find(haystack, 0)
		if match == nil {
			return -1, -1, false
		}
		return match.Start, match.End, true
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)

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

	// For Fat Teddy with small haystacks, use Aho-Corasick fallback.
	if e.fatTeddyFallback != nil && len(haystack) < fatTeddySmallHaystackThreshold {
		atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
		match := e.fatTeddyFallback.FindAt(haystack, at)
		if match == nil {
			return -1, -1, false
		}
		return match.Start, match.End, true
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)

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
// Thread-safe: uses pooled PikeVM instance.
func (e *Engine) findNFA(haystack []byte) *Match {
	atomic.AddUint64(&e.stats.NFASearches, 1)

	state := e.getSearchState()
	defer e.putSearchState(state)

	start, end, matched := state.pikevm.Search(haystack)
	if !matched {
		return nil
	}

	return NewMatch(start, end, haystack)
}

// findDFA searches using DFA with prefilter and NFA fallback.
func (e *Engine) findDFA(haystack []byte) *Match {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// If prefilter available, use it to find candidate positions quickly
	if e.prefilter != nil {
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			return nil
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)

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
			atomic.AddUint64(&e.stats.PrefilterHits, 1)
			atomic.AddUint64(&e.stats.DFASearches, 1)
			return NewMatch(start, end, haystack)
		}

		// Standard prefilter path
		pos := e.prefilter.Find(haystack, 0)
		if pos == -1 {
			// No candidate found - definitely no match
			return nil
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
		atomic.AddUint64(&e.stats.DFASearches, 1)

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
		atomic.AddUint64(&e.stats.DFASearches, 1)
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
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
		}
	}

	// Fall back to NFA
	return e.findNFA(haystack)
}

// findNFAAt searches using NFA starting from a specific position.
// This preserves absolute positions for correct anchor handling.
func (e *Engine) findNFAAt(haystack []byte, at int) *Match {
	atomic.AddUint64(&e.stats.NFASearches, 1)
	start, end, matched := e.pikevm.SearchAt(haystack, at)
	if !matched {
		return nil
	}
	return NewMatch(start, end, haystack)
}

// findDFAAt searches using DFA starting from a specific position.
// This preserves absolute positions for correct anchor handling.
func (e *Engine) findDFAAt(haystack []byte, at int) *Match {
	atomic.AddUint64(&e.stats.DFASearches, 1)

	// If prefilter available and complete, use literal fast path
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, at)
		if pos == -1 {
			return nil
		}
		atomic.AddUint64(&e.stats.PrefilterHits, 1)
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
		atomic.AddUint64(&e.stats.DFASearches, 1)
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
			atomic.AddUint64(&e.stats.DFACacheFull, 1)
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

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSearcher.Find(haystack)
}

// isMatchReverseAnchored checks for match using reverse DFA.
func (e *Engine) isMatchReverseAnchored(haystack []byte) bool {
	if e.reverseSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSearcher.IsMatch(haystack)
}

// findReverseSuffix searches using suffix literal prefilter + reverse DFA.
func (e *Engine) findReverseSuffix(haystack []byte) *Match {
	if e.reverseSuffixSearcher == nil {
		// Fallback to NFA if reverse suffix searcher not available
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSearcher.Find(haystack)
}

// isMatchReverseSuffix checks for match using suffix prefilter + reverse DFA.
func (e *Engine) isMatchReverseSuffix(haystack []byte) bool {
	if e.reverseSuffixSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSearcher.IsMatch(haystack)
}

// findReverseSuffixSet searches using Teddy multi-suffix prefilter + reverse DFA.
func (e *Engine) findReverseSuffixSet(haystack []byte) *Match {
	if e.reverseSuffixSetSearcher == nil {
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSetSearcher.Find(haystack)
}

// isMatchReverseSuffixSet checks for match using Teddy multi-suffix prefilter.
func (e *Engine) isMatchReverseSuffixSet(haystack []byte) bool {
	if e.reverseSuffixSetSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseSuffixSetSearcher.IsMatch(haystack)
}

// findReverseInner searches using inner literal prefilter + bidirectional DFA.
func (e *Engine) findReverseInner(haystack []byte) *Match {
	if e.reverseInnerSearcher == nil {
		// Fallback to NFA if reverse inner searcher not available
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
	return e.reverseInnerSearcher.Find(haystack)
}

// isMatchReverseInner checks for match using inner prefilter + bidirectional DFA.
func (e *Engine) isMatchReverseInner(haystack []byte) bool {
	if e.reverseInnerSearcher == nil {
		return e.isMatchNFA(haystack)
	}

	atomic.AddUint64(&e.stats.DFASearches, 1)
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
		atomic.AddUint64(&e.stats.NFASearches, 1)
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
// Used for simple digit-lead patterns where literal extraction fails
// but all alternation branches must start with a digit.
//
// Note: Complex digit-lead patterns (like IP addresses with 74 NFA states) are
// handled by UseBoth/UseDFA strategies instead. See digitPrefilterMaxNFAStates.
//
// Algorithm:
//  1. Use SIMD to find next digit position in haystack
//  2. Verify match at digit position using lazy DFA + PikeVM
//  3. If no match, continue from digit position + 1
//
// Performance:
//   - Skips non-digit regions with SIMD (15-20x faster for sparse data)
//   - Total: O(n) for scan + O(k*m) for k digit candidates
func (e *Engine) findDigitPrefilter(haystack []byte) *Match {
	if e.digitPrefilter == nil {
		return e.findNFA(haystack)
	}

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := 0

	for pos < len(haystack) {
		// Use SIMD to find next digit position
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return nil // No more digits, no match possible
		}

		// Verify match at digit position using DFA
		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
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
			atomic.AddUint64(&e.stats.NFASearches, 1)
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

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := at

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return nil
		}

		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			endPos := e.dfa.FindAt(haystack, digitPos)
			if endPos != -1 {
				start, end, found := e.pikevm.SearchAt(haystack, digitPos)
				if found {
					return NewMatch(start, end, haystack)
				}
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
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

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := 0

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return false // No more digits
		}

		// Use DFA for fast boolean check if available
		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			if e.dfa.FindAt(haystack, digitPos) != -1 {
				return true
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
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

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := 0

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return -1, -1, false
		}

		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			// Use anchored search - pattern MUST start at digitPos
			// This is much faster than PikeVM for patterns that require digit start
			endPos := e.dfa.SearchAtAnchored(haystack, digitPos)
			if endPos != -1 {
				return digitPos, endPos, true
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
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

	atomic.AddUint64(&e.stats.PrefilterHits, 1)
	pos := at

	for pos < len(haystack) {
		digitPos := e.digitPrefilter.Find(haystack, pos)
		if digitPos < 0 {
			return -1, -1, false
		}

		if e.dfa != nil {
			atomic.AddUint64(&e.stats.DFASearches, 1)
			// Use anchored search - pattern MUST start at digitPos
			// This is much faster than PikeVM for patterns that require digit start
			endPos := e.dfa.SearchAtAnchored(haystack, digitPos)
			if endPos != -1 {
				return digitPos, endPos, true
			}
		} else {
			atomic.AddUint64(&e.stats.NFASearches, 1)
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
// This is the "literal engine bypass" for patterns with >32 literals.
// The automaton performs O(n) multi-pattern matching with ~1.6 GB/s throughput.
func (e *Engine) findAhoCorasick(haystack []byte) *Match {
	if e.ahoCorasick == nil {
		return e.findNFA(haystack)
	}
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)

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
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)

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
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)
	return e.ahoCorasick.IsMatch(haystack)
}

// findIndicesAhoCorasick returns indices using Aho-Corasick - zero alloc.
func (e *Engine) findIndicesAhoCorasick(haystack []byte) (int, int, bool) {
	if e.ahoCorasick == nil {
		return e.findIndicesNFA(haystack)
	}
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)

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
	atomic.AddUint64(&e.stats.AhoCorasickSearches, 1)

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
