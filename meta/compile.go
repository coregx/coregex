// Package meta implements the meta-engine orchestrator.
//
// compile.go contains pattern compilation logic and engine builders.

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
	dfa                            *lazy.DFA
	reverseSearcher                *ReverseAnchoredSearcher
	reverseSuffixSearcher          *ReverseSuffixSearcher
	reverseSuffixSetSearcher       *ReverseSuffixSetSearcher
	reverseInnerSearcher           *ReverseInnerSearcher
	multilineReverseSuffixSearcher *MultilineReverseSuffixSearcher // Issue #97
	digitPrefilter                 *prefilter.DigitPrefilter
	digitRunSkipSafe               bool
	ahoCorasick                    *ahocorasick.Automaton
	finalStrategy                  Strategy
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
		strategy == UseMultilineReverseSuffix || strategy == UseDigitPrefilter

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
		result.digitRunSkipSafe = isDigitRunSkipSafe(re)
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

	case UseMultilineReverseSuffix:
		// Issue #97: Build multiline-aware reverse suffix searcher for (?m)^.*suffix patterns
		suffixLiterals := extractor.ExtractSuffixes(re)
		searcher, err := NewMultilineReverseSuffixSearcher(nfaEngine, suffixLiterals, dfaConfig)
		if err != nil {
			// Fallback to regular ReverseSuffix or DFA
			result.finalStrategy = UseDFA
		} else {
			// Issue #99: Extract prefix literals for fast path verification
			// For patterns like (?m)^/.*\.php, prefix is "/" - enables O(1) verification
			prefixLiterals := extractor.ExtractPrefixes(re)
			searcher.SetPrefixLiterals(prefixLiterals)
			result.multilineReverseSuffixSearcher = searcher
		}
	}

	return result
}

// charClassSearcherResult holds the result of building specialized searchers.
type charClassSearcherResult struct {
	boundedBT        *nfa.BoundedBacktracker
	charClassSrch    *nfa.CharClassSearcher
	compositeSrch    *nfa.CompositeSearcher
	compositeSeqDFA  *nfa.CompositeSequenceDFA // DFA (faster than backtracking)
	branchDispatcher *nfa.BranchDispatcher
	finalStrategy    Strategy
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

	// Compile ASCII-only NFA for patterns with '.' (V11-002 optimization).
	// This enables runtime ASCII detection: if input is all ASCII, use the faster
	// ASCII NFA which has ~2.8x fewer states for '.'-heavy patterns.
	var asciiNFAEngine *nfa.NFA
	var asciiBT *nfa.BoundedBacktracker
	if nfa.ContainsDot(re) && config.EnableASCIIOptimization {
		asciiCompiler := nfa.NewCompiler(nfa.CompilerConfig{
			UTF8:              true,
			Anchored:          false,
			DotNewline:        false,
			ASCIIOnly:         true, // Key: compile '.' as single byte range
			MaxRecursionDepth: config.MaxRecursionDepth,
		})
		asciiNFAEngine, err = asciiCompiler.CompileRegexp(re)
		if err == nil {
			asciiBT = nfa.NewBoundedBacktracker(asciiNFAEngine)
		}
		// If ASCII NFA compilation fails, we fall back to UTF-8 NFA (asciiNFAEngine stays nil)
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

	// Extract suffix literal for fully-anchored patterns (both ^ and $).
	// This enables O(1) early rejection via bytes.HasSuffix check.
	// For patterns like ^/.*\.php$, reject inputs not ending with ".php".
	// NOTE: Only works for end-anchored patterns! Non-end-anchored like ^/.*\.php
	// can match /foo.php/bar (matching /foo.php), so suffix check would be wrong.
	var anchoredSuffix []byte
	isEndAnchored := nfa.IsPatternEndAnchored(re)
	if isStartAnchored && isEndAnchored && strategy == UseBoundedBacktracker {
		suffixExtractor := literal.New(literal.ExtractorConfig{
			MaxLiterals:   config.MaxLiterals,
			MaxLiteralLen: 64,
			MaxClassSize:  10,
		})
		suffixLiterals := suffixExtractor.ExtractSuffixes(re)
		if suffixLiterals != nil && !suffixLiterals.IsEmpty() {
			lcs := suffixLiterals.LongestCommonSuffix()
			if len(lcs) >= config.MinLiteralLen {
				anchoredSuffix = lcs
			}
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

	// Extract AnchoredLiteralInfo for UseAnchoredLiteral strategy.
	// This enables O(1) specialized matching for ^prefix.*suffix$ patterns.
	// The detection was already done in SelectStrategy, but we need the info
	// for the execution path.
	// Reference: https://github.com/coregx/coregex/issues/79
	var anchoredLiteralInfo *AnchoredLiteralInfo
	if strategy == UseAnchoredLiteral {
		anchoredLiteralInfo = DetectAnchoredLiteral(re)
		// Fallback if detection fails (shouldn't happen since SelectStrategy checked)
		if anchoredLiteralInfo == nil {
			strategy = UseBoundedBacktracker
			charClassResult.boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
		}
	}

	// Initialize state pool for thread-safe concurrent searches
	numCaptures := nfaEngine.CaptureCount()

	return &Engine{
		nfa:                            nfaEngine,
		asciiNFA:                       asciiNFAEngine,
		asciiBoundedBacktracker:        asciiBT,
		dfa:                            engines.dfa,
		pikevm:                         pikevm,
		boundedBacktracker:             charClassResult.boundedBT,
		charClassSearcher:              charClassResult.charClassSrch,
		compositeSearcher:              charClassResult.compositeSrch,
		compositeSequenceDFA:           charClassResult.compositeSeqDFA,
		branchDispatcher:               charClassResult.branchDispatcher,
		anchoredFirstBytes:             anchoredFirstBytes,
		anchoredSuffix:                 anchoredSuffix,
		reverseSearcher:                engines.reverseSearcher,
		reverseSuffixSearcher:          engines.reverseSuffixSearcher,
		reverseSuffixSetSearcher:       engines.reverseSuffixSetSearcher,
		reverseInnerSearcher:           engines.reverseInnerSearcher,
		multilineReverseSuffixSearcher: engines.multilineReverseSuffixSearcher,
		digitPrefilter:                 engines.digitPrefilter,
		digitRunSkipSafe:               engines.digitRunSkipSafe,
		ahoCorasick:                    engines.ahoCorasick,
		anchoredLiteralInfo:            anchoredLiteralInfo,
		prefilter:                      pf,
		strategy:                       strategy,
		config:                         config,
		onepass:                        onePassRes,
		canMatchEmpty:                  canMatchEmpty,
		isStartAnchored:                isStartAnchored,
		fatTeddyFallback:               fatTeddyFallback,
		statePool:                      newSearchStatePool(nfaEngine, numCaptures),
		stats:                          Stats{},
	}, nil
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
