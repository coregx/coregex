package meta

import (
	"errors"
	"regexp/syntax"

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
	nfa                   *nfa.NFA
	dfa                   *lazy.DFA
	pikevm                *nfa.PikeVM
	boundedBacktracker    *nfa.BoundedBacktracker
	reverseSearcher       *ReverseAnchoredSearcher
	reverseSuffixSearcher *ReverseSuffixSearcher
	reverseInnerSearcher  *ReverseInnerSearcher
	prefilter             prefilter.Prefilter
	strategy              Strategy
	config                Config

	// OnePass DFA for anchored patterns with captures (optional optimization)
	// This is independent of strategy - used by FindSubmatch when available
	onepass      *onepass.DFA
	onepassCache *onepass.Cache

	// longest enables leftmost-longest (POSIX) matching semantics
	// By default (false), uses leftmost-first (Perl) semantics
	longest bool

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
//
//nolint:cyclop // complexity is inherent to multi-strategy compilation
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

	// Try to build OnePass DFA for anchored patterns with capture groups
	// This is an optional optimization for FindSubmatch (10-20x faster)
	var onepassDFA *onepass.DFA
	var onepassCache *onepass.Cache
	if config.EnableDFA && nfaEngine.CaptureCount() > 1 {
		// Compile anchored NFA for OnePass (requires Anchored: true)
		anchoredCompiler := nfa.NewCompiler(nfa.CompilerConfig{
			UTF8:              true,
			Anchored:          true, // Required for one-pass
			DotNewline:        false,
			MaxRecursionDepth: config.MaxRecursionDepth,
		})
		anchoredNFA, err := anchoredCompiler.CompileRegexp(re)
		if err == nil {
			// Try to build one-pass DFA
			onepassDFA, err = onepass.Build(anchoredNFA)
			if err == nil {
				// Success! Create cache for reuse
				onepassCache = onepass.NewCache(onepassDFA.NumCaptures())
			}
			// If onepass.Build fails (ErrNotOnePass), silently fall back to PikeVM
		}
	}

	// Build DFA if strategy requires it
	var dfaEngine *lazy.DFA
	var reverseSearcher *ReverseAnchoredSearcher
	var reverseSuffixSearcher *ReverseSuffixSearcher
	var reverseInnerSearcher *ReverseInnerSearcher

	if strategy == UseDFA || strategy == UseBoth || strategy == UseReverseAnchored || strategy == UseReverseSuffix || strategy == UseReverseInner {
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

		// For reverse suffix search, build reverse suffix searcher
		if strategy == UseReverseSuffix {
			// Extract suffix literals
			extractor := literal.New(literal.ExtractorConfig{
				MaxLiterals:   config.MaxLiterals,
				MaxLiteralLen: 64,
				MaxClassSize:  10,
			})
			suffixLiterals := extractor.ExtractSuffixes(re)

			reverseSuffixSearcher, err = NewReverseSuffixSearcher(nfaEngine, suffixLiterals, dfaConfig)
			if err != nil {
				// ReverseSuffix compilation failed: fall back to forward DFA
				strategy = UseDFA
			}
		}

		// For reverse inner search, build reverse inner searcher
		if strategy == UseReverseInner {
			// Extract inner literals
			extractor := literal.New(literal.ExtractorConfig{
				MaxLiterals:   config.MaxLiterals,
				MaxLiteralLen: 64,
				MaxClassSize:  10,
			})
			innerInfo := extractor.ExtractInnerForReverseSearch(re)
			if innerInfo != nil {
				reverseInnerSearcher, err = NewReverseInnerSearcher(nfaEngine, innerInfo, dfaConfig)
				if err != nil {
					// ReverseInner compilation failed: fall back to forward DFA
					strategy = UseDFA
				}
			} else {
				// No inner literals available: fall back to forward DFA
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

	// Build BoundedBacktracker for character class patterns
	var boundedBT *nfa.BoundedBacktracker
	if strategy == UseBoundedBacktracker {
		boundedBT = nfa.NewBoundedBacktracker(nfaEngine)
	}

	return &Engine{
		nfa:                   nfaEngine,
		dfa:                   dfaEngine,
		pikevm:                pikevm,
		boundedBacktracker:    boundedBT,
		reverseSearcher:       reverseSearcher,
		reverseSuffixSearcher: reverseSuffixSearcher,
		reverseInnerSearcher:  reverseInnerSearcher,
		prefilter:             pf,
		strategy:              strategy,
		config:                config,
		onepass:               onepassDFA,
		onepassCache:          onepassCache,
		stats:                 Stats{},
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

	// For position 0, use the optimized strategy-specific paths
	if at == 0 {
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
		case UseReverseInner:
			return e.findReverseInner(haystack)
		case UseBoundedBacktracker:
			return e.findBoundedBacktracker(haystack)
		default:
			return e.findNFA(haystack)
		}
	}

	// For non-zero positions, use FindAt variants that preserve absolute positions
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
	case UseReverseInner:
		return e.isMatchReverseInner(haystack)
	case UseBoundedBacktracker:
		return e.isMatchBoundedBacktracker(haystack)
	default:
		return e.isMatchNFA(haystack)
	}
}

// isMatchNFA checks for match using NFA (PikeVM) with early termination.
func (e *Engine) isMatchNFA(haystack []byte) bool {
	e.stats.NFASearches++
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
	case UseReverseInner:
		return e.findIndicesReverseInner(haystack)
	case UseBoundedBacktracker:
		return e.findIndicesBoundedBacktracker(haystack)
	default:
		return e.findIndicesNFA(haystack)
	}
}

// FindIndicesAt returns the start and end indices of the first match starting at position 'at'.
// Returns (-1, -1, false) if no match is found.
func (e *Engine) FindIndicesAt(haystack []byte, at int) (start, end int, found bool) {
	switch e.strategy {
	case UseNFA:
		return e.findIndicesNFAAt(haystack, at)
	case UseDFA:
		return e.findIndicesDFAAt(haystack, at)
	case UseBoth:
		return e.findIndicesAdaptiveAt(haystack, at)
	case UseBoundedBacktracker:
		return e.findIndicesBoundedBacktrackerAt(haystack, at)
	default:
		return e.findIndicesNFAAt(haystack, at)
	}
}

// findIndicesNFA searches using NFA (PikeVM) directly - zero alloc.
func (e *Engine) findIndicesNFA(haystack []byte) (int, int, bool) {
	e.stats.NFASearches++
	return e.pikevm.Search(haystack)
}

// findIndicesNFAAt searches using NFA starting at position - zero alloc.
func (e *Engine) findIndicesNFAAt(haystack []byte, at int) (int, int, bool) {
	e.stats.NFASearches++
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

	// Use DFA search
	pos := e.dfa.Find(haystack)
	if pos == -1 {
		return -1, -1, false
	}

	// DFA returns end position, need NFA for start
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

	pos := e.dfa.FindAt(haystack, at)
	if pos == -1 {
		return -1, -1, false
	}
	return e.pikevm.SearchAt(haystack, at)
}

// findIndicesAdaptive tries DFA first, falls back to NFA - zero alloc.
func (e *Engine) findIndicesAdaptive(haystack []byte) (int, int, bool) {
	if e.dfa != nil {
		e.stats.DFASearches++
		pos := e.dfa.Find(haystack)
		if pos != -1 {
			return e.pikevm.Search(haystack)
		}
		size, capacity, _, _, _ := e.dfa.CacheStats()
		if size >= int(capacity)*9/10 {
			e.stats.DFACacheFull++
		}
	}
	return e.findIndicesNFA(haystack)
}

// findIndicesAdaptiveAt tries DFA first at position, falls back to NFA - zero alloc.
func (e *Engine) findIndicesAdaptiveAt(haystack []byte, at int) (int, int, bool) {
	if e.dfa != nil {
		e.stats.DFASearches++
		pos := e.dfa.FindAt(haystack, at)
		if pos != -1 {
			return e.pikevm.SearchAt(haystack, at)
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
	// For now, fall back to NFA for non-zero positions
	// BoundedBacktracker doesn't have SearchAt yet
	return e.findIndicesNFAAt(haystack, at)
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

	// If prefilter available and complete, use literal fast path
	// This bypasses PikeVM entirely for exact literal matches
	if e.prefilter != nil && e.prefilter.IsComplete() {
		pos := e.prefilter.Find(haystack, 0)
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
			// DFA succeeded - get exact match bounds from NFA
			// DFA only returns end position, not start position
			start, end, matched := e.pikevm.Search(haystack)
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
