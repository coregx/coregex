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
	reverseDFA                     *lazy.DFA // Reverse DFA for bidirectional search fallback
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
		strategy == UseMultilineReverseSuffix || strategy == UseDigitPrefilter ||
		strategy == UseBoundedBacktracker

	if !needsDFA {
		return result
	}

	dfaConfig := lazy.DefaultConfig()
	dfaConfig.MaxStates = config.MaxDFAStates
	dfaConfig.DeterminizationLimit = config.DeterminizationLimit

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

	// Build reverse DFA for bidirectional search (UseDFA and BoundedBacktracker).
	// Forward DFA → match end, reverse DFA → match start. O(n) total.
	result = buildReverseDFA(result, re, nfaEngine, dfaConfig, pf)

	// For digit prefilter strategy, create the digit prefilter
	if result.finalStrategy == UseDigitPrefilter {
		result.digitPrefilter = prefilter.NewDigitPrefilter()
		result.digitRunSkipSafe = isDigitRunSkipSafe(re)
	}

	return result
}

// buildReverseDFA builds reverse DFA for bidirectional search.
// Used by UseDFA (replaces PikeVM second pass) and BoundedBacktracker (large input fallback).
func buildReverseDFA(
	result strategyEngines,
	re *syntax.Regexp,
	nfaEngine *nfa.NFA,
	dfaConfig lazy.Config,
	pf prefilter.Prefilter,
) strategyEngines {
	// Reverse DFA config: disable break-at-match so the reverse search continues
	// past matches to find the leftmost match start (greedy continuation).
	revDFAConfig := dfaConfig
	revDFAConfig.BreakAtMatch = false

	switch result.finalStrategy {
	case UseDFA:
		// Skip for non-greedy patterns: forward DFA always finds leftmost-longest,
		// which is incompatible with non-greedy semantics.
		if result.dfa != nil && !hasNonGreedyQuantifier(re) {
			reverseNFA := nfa.ReverseAnchored(nfaEngine)
			revDFA, err := lazy.CompileWithConfig(reverseNFA, revDFAConfig)
			if err == nil {
				result.reverseDFA = revDFA
			}
		}
	case UseBoundedBacktracker:
		fwdDFA, err := lazy.CompileWithPrefilter(nfaEngine, dfaConfig, pf)
		if err == nil {
			result.dfa = fwdDFA
			reverseNFA := nfa.ReverseAnchored(nfaEngine)
			revDFA, revErr := lazy.CompileWithConfig(reverseNFA, revDFAConfig)
			if revErr == nil {
				result.reverseDFA = revDFA
			}
		}
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
		searcher, err := NewReverseSuffixSearcher(nfaEngine, suffixLiterals, dfaConfig, hasDotStarPrefix(re))
		if err != nil {
			result.finalStrategy = UseDFA
		} else {
			result.reverseSuffixSearcher = searcher
		}

	case UseReverseSuffixSet:
		suffixLiterals := extractor.ExtractSuffixes(re)
		searcher, err := NewReverseSuffixSetSearcher(nfaEngine, suffixLiterals, dfaConfig, hasDotStarPrefix(re))
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
	btNFA *nfa.NFA, // NFA for BoundedBacktracker (runeNFA when available, else nfaEngine)
) charClassSearcherResult {
	result := charClassSearcherResult{finalStrategy: strategy}

	if strategy == UseBoundedBacktracker {
		result.boundedBT = nfa.NewBoundedBacktracker(btNFA)
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
			result.boundedBT = nfa.NewBoundedBacktracker(btNFA)
		}
	}

	// CompositeSearcher for concatenated char classes like [a-zA-Z]+[0-9]+
	// Reference: https://github.com/coregx/coregex/issues/72
	if strategy == UseCompositeSearcher {
		result.compositeSrch = nfa.NewCompositeSearcher(re)
		if result.compositeSrch == nil {
			// Fallback to BoundedBacktracker if extraction fails
			result.finalStrategy = UseBoundedBacktracker
			result.boundedBT = nfa.NewBoundedBacktracker(btNFA)
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
			result.boundedBT = nfa.NewBoundedBacktracker(btNFA)
		}
	}

	// For UseNFA with small NFAs, also create BoundedBacktracker as fallback.
	// BoundedBacktracker is 2-3x faster than PikeVM on small inputs due to
	// generation-based visited tracking (O(1) reset) vs PikeVM's thread queues.
	// Use small capacity (256KB like Rust) — for UseNFA, BT is optional;
	// PikeVM handles large inputs correctly. This prevents 37MB+ visited allocations.
	if result.finalStrategy == UseNFA && result.boundedBT == nil && nfaEngine.States() < 50 {
		result.boundedBT = nfa.NewBoundedBacktrackerSmall(btNFA)
	}

	return result
}

// buildDotOptimizedNFAs compiles optimized NFA variants for patterns with '.'.
// Returns:
//   - asciiNFA: NFA with '.' compiled as single ASCII byte range (for ASCII-only input)
//   - asciiBT: BoundedBacktracker for asciiNFA
//   - runeNFA: NFA with '.' compiled as sparse dispatch (fewer split states for PikeVM)
func buildDotOptimizedNFAs(
	re *syntax.Regexp, config Config,
) (*nfa.NFA, *nfa.BoundedBacktracker, *nfa.NFA) {
	if !nfa.ContainsDot(re) {
		return nil, nil, nil
	}

	// ASCII-only NFA (V11-002 optimization):
	// compile '.' as single byte range [0x00-0x7F] for ASCII-only inputs.
	var asciiNFAEngine *nfa.NFA
	var asciiBT *nfa.BoundedBacktracker
	if config.EnableASCIIOptimization {
		asciiCompiler := nfa.NewCompiler(nfa.CompilerConfig{
			UTF8:              true,
			Anchored:          false,
			DotNewline:        false,
			ASCIIOnly:         true,
			MaxRecursionDepth: config.MaxRecursionDepth,
		})
		var err error
		asciiNFAEngine, err = asciiCompiler.CompileRegexp(re)
		if err == nil {
			asciiBT = nfa.NewBoundedBacktracker(asciiNFAEngine)
		}
	}

	// Sparse-dispatch NFA: compile '.' as a single sparse state mapping each
	// leading byte range to the correct continuation chain. This eliminates
	// ~9 split states per dot, giving PikeVM O(1) dispatch instead of
	// O(branches) split-chain DFS. Measured 2.8-4.8x PikeVM speedup.
	var runeNFAEngine *nfa.NFA
	runeCompiler := nfa.NewCompiler(nfa.CompilerConfig{
		UTF8:              true,
		Anchored:          false,
		DotNewline:        false,
		UseRuneStates:     true,
		MaxRecursionDepth: config.MaxRecursionDepth,
	})
	runeNFAEngine, err := runeCompiler.CompileRegexp(re)
	if err != nil {
		runeNFAEngine = nil
	}

	return asciiNFAEngine, asciiBT, runeNFAEngine
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

	// Compile optimized NFA variants for patterns with '.'
	asciiNFAEngine, asciiBT, runeNFAEngine := buildDotOptimizedNFAs(re, config)

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

	// Debug: log extracted literals (prefixes + suffixes)
	debugLiterals("prefixes", literals)
	debugSuffixes(re, config, isStartAnchored)

	// Select strategy (pass re for anchor detection)
	strategy := SelectStrategy(nfaEngine, re, literals, config)

	pf, strategy = adjustForAnchors(pf, strategy, re)

	// Build PikeVM (always needed for fallback).
	// NOTE: hasMultilineLineAnchor and hasAnchorAssertions are defined in strategy.go
	// Use runeNFA when available — sparse dispatch replaces ~9 split states
	// with a single sparse state, giving PikeVM O(1) byte dispatch per '.'.
	pikevmNFA := nfaEngine
	if runeNFAEngine != nil {
		pikevmNFA = runeNFAEngine
	}
	pikevm := nfa.NewPikeVM(pikevmNFA)

	// Set prefilter as skip-ahead inside PikeVM (Rust approach: pikevm.rs:1293).
	// When NFA has no active threads, PikeVM skips to next candidate position.
	// Safe for partial-coverage prefilters — NFA processes all branches.
	configurePikeVMSkipAhead(pikevm, pf, isStartAnchored)

	// Build OnePass DFA for anchored patterns with captures (optional optimization)
	onePassRes := buildOnePassDFA(re, nfaEngine, config)

	// Build strategy-specific engines (DFA, reverse searchers, Aho-Corasick, etc.)
	engines := buildStrategyEngines(strategy, re, nfaEngine, literals, pf, config)
	strategy = engines.finalStrategy

	// Build specialized searchers for character class patterns.
	// Pass pikevmNFA so BoundedBacktrackers benefit from rune states.
	charClassResult := buildCharClassSearchers(strategy, re, nfaEngine, pikevmNFA)
	strategy = charClassResult.finalStrategy

	// Debug: log engines built
	debugEngine("PikeVM", true, "")
	debugEngine("OnePass DFA", onePassRes != nil, "not worth it or not anchored")
	debugEngine("lazy DFA", engines.dfa != nil, "strategy does not need DFA")
	debugEngine("reverse DFA", engines.reverseDFA != nil, "")

	// Debug: log final strategy selection
	debugStrategy(re.String(), strategy, nfaEngine.States(), literals, "")

	// Prefilter selection: Slim Teddy (2-32 patterns), AC DFA (>32 patterns).
	// FatTeddy replaced by AC for >32 patterns (130x faster, zero false positives).

	// Debug: log prefilter selection
	debugPrefilter(pf)

	// Check if pattern can match empty string.
	// If true, BoundedBacktracker cannot be used for Find operations
	// because its greedy semantics give wrong results for patterns like (?:|a)*
	canMatchEmpty := pikevm.IsMatch(nil)

	// Check if Phase 3 (SearchAtAnchored) is needed in bidirectional DFA search.
	// Phase 3 re-scans from confirmed start with greedy semantics. Only needed when
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
			charClassResult.boundedBT = nfa.NewBoundedBacktracker(pikevmNFA)
		}
	}

	// Initialize state pool for thread-safe concurrent searches
	numCaptures := nfaEngine.CaptureCount()

	return &Engine{
		nfa:                            nfaEngine,
		runeNFA:                        runeNFAEngine,
		asciiNFA:                       asciiNFAEngine,
		asciiBoundedBacktracker:        asciiBT,
		dfa:                            engines.dfa,
		reverseDFA:                     engines.reverseDFA,
		nfaStateCount:                  nfaEngine.States(),
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
		prefilterPartialCoverage:       literals != nil && literals.IsPartialCoverage(),
		strategy:                       strategy,
		config:                         config,
		onepass:                        onePassRes,
		canMatchEmpty:                  canMatchEmpty,
		isStartAnchored:                isStartAnchored,
		fatTeddyFallback:               fatTeddyFallback,
		statePool: newSearchStatePool(buildSearchStateConfig(
			pikevmNFA, numCaptures, engines, strategy,
		)),
		stats: Stats{},
	}, nil
}

// adjustForAnchors fixes prefilter for patterns with anchors.
// Anchors (^, $, \b) require verification that Teddy/AC prefilter can't provide.
//
// Note: the lazy DFA correctly handles (?m)^ via StartByteMap — after \n it
// selects StartLineLF which includes LookStartLine in the epsilon closure.
// Verified with direct DFA tests and Rust source analysis (identical approach).
// See docs/dev/research/v01216-arm64-regression.md for details.
func adjustForAnchors(pf prefilter.Prefilter, strategy Strategy, re *syntax.Regexp) (prefilter.Prefilter, Strategy) {
	if !hasAnchorAssertions(re) {
		return pf, strategy
	}

	hasMultilineAnchor := hasMultilineLineAnchor(re)

	if pf != nil && pf.IsComplete() {
		if hasMultilineAnchor && !hasNonLineAnchors(re) {
			// (?m)^ with complete literals and NO other anchors (\b, $):
			// Use line-anchor wrapper — O(1) line-start check per candidate.
			// This keeps IsComplete()=true so Teddy can return matches directly
			// without expensive NFA verification.
			pf = prefilter.WrapLineAnchor(pf)
		} else {
			// Other anchors (\b, $) or mixed anchors:
			// Mark incomplete — engine must verify with NFA/DFA.
			pf = prefilter.WrapIncomplete(pf)
		}
	}

	return pf, strategy
}

// hasNonLineAnchors checks if the pattern has anchors other than (?m)^ line start.
// Returns true for \b, $, \A, \z, or non-multiline ^.
func hasNonLineAnchors(re *syntax.Regexp) bool {
	if re == nil {
		return false
	}
	switch re.Op {
	case syntax.OpBeginLine:
		return false // (?m)^ is fine
	case syntax.OpEndLine, syntax.OpEndText, syntax.OpBeginText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	}
	for _, sub := range re.Sub {
		if hasNonLineAnchors(sub) {
			return true
		}
	}
	return false
}

// configurePikeVMSkipAhead sets prefilter as skip-ahead inside PikeVM.
func configurePikeVMSkipAhead(pikevm *nfa.PikeVM, pf prefilter.Prefilter, isStartAnchored bool) {
	if pf != nil && !isStartAnchored {
		pikevm.SetSkipAhead(pf)
	}
}

// buildSearchStateConfig extracts all DFA references needed for per-search caches.
// Strategy-specific DFAs come from reverse searchers (which have their own DFAs).
func buildSearchStateConfig(nfaEngine *nfa.NFA, numCaptures int, engines strategyEngines, strategy Strategy) searchStateConfig {
	cfg := searchStateConfig{
		nfaEngine:   nfaEngine,
		numCaptures: numCaptures,
		forwardDFA:  engines.dfa,
		reverseDFA:  engines.reverseDFA,
	}

	// Extract strategy-specific DFAs from reverse searchers
	switch strategy {
	case UseReverseSuffix:
		if s := engines.reverseSuffixSearcher; s != nil {
			cfg.stratFwdDFA = s.forwardDFA
			cfg.stratRevDFA = s.reverseDFA
		}
	case UseReverseInner:
		if s := engines.reverseInnerSearcher; s != nil {
			cfg.stratFwdDFA = s.forwardDFA
			cfg.stratRevDFA = s.reverseDFA
		}
	case UseReverseSuffixSet:
		if s := engines.reverseSuffixSetSearcher; s != nil {
			cfg.stratFwdDFA = s.forwardDFA
			cfg.stratRevDFA = s.reverseDFA
		}
	case UseReverseAnchored:
		if s := engines.reverseSearcher; s != nil {
			cfg.stratRevDFA = s.reverseDFA
		}
	case UseMultilineReverseSuffix:
		if s := engines.multilineReverseSuffixSearcher; s != nil {
			cfg.stratFwdDFA = s.forwardDFA
		}
	}

	return cfg
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
