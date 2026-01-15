// Package meta implements the meta-engine orchestrator for coregex.
//
// The meta package provides the core Engine type that orchestrates all regex
// execution strategies. It analyzes patterns, selects optimal strategies,
// and coordinates search across multiple engines (NFA, DFA, prefilters).
//
// # Architecture
//
// The meta-engine uses a multi-strategy approach:
//
//   - Pattern analysis extracts literals for prefiltering
//   - Strategy selection chooses the optimal execution path
//   - Engine orchestration coordinates NFA, DFA, and specialized searchers
//
// # Strategies
//
// Available strategies (selected automatically based on pattern):
//
//   - UseNFA: Direct NFA (PikeVM) search for small patterns
//   - UseDFA: Lazy DFA for patterns with good literals
//   - UseBoth: Adaptive DFA→NFA with cache eviction handling
//   - UseReverseAnchored: Reverse DFA for end-anchored patterns ($)
//   - UseReverseSuffix: Suffix literal + reverse DFA (1124x speedup)
//   - UseReverseInner: Bidirectional search from inner literal (909x)
//   - UseReverseSuffixSet: Teddy multi-suffix prefilter (260x)
//   - UseTeddy: SIMD multi-pattern for exact alternations (242x)
//   - UseAhoCorasick: Aho-Corasick for large alternations (113x)
//   - UseCharClassSearcher: Specialized char class searcher (23x)
//   - UseDigitPrefilter: SIMD digit prefilter for digit-lead patterns
//   - UseOnePass: OnePass DFA for anchored patterns with captures
//   - UseBoundedBacktracker: Bounded backtracker for char class patterns
//   - UseBranchDispatch: O(1) branch dispatch for anchored alternations
//   - UseCompositeSearcher: For concatenated char classes
//   - UseAnchoredLiteral: O(1) matching for ^prefix.*suffix$ patterns (32-133x)
//
// # Thread Safety
//
// The Engine type is safe for concurrent use from multiple goroutines.
// Per-search mutable state is managed via sync.Pool.
//
// # Usage
//
//	engine, err := meta.Compile(`\w+@\w+\.\w+`)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Find first match
//	match := engine.Find([]byte("user@example.com"))
//	if match != nil {
//	    fmt.Println(match.String())
//	}
//
//	// Check if matches
//	if engine.IsMatch([]byte("test@test.com")) {
//	    fmt.Println("matches!")
//	}
//
//	// Find with captures
//	match := engine.FindSubmatch([]byte("user@example.com"))
//	if match != nil {
//	    fmt.Println(match.Group(1)) // first capture group
//	}
//
// # Files
//
// The meta package is organized into these files:
//
//   - engine.go: Engine struct definition and core API
//   - compile.go: Pattern compilation and engine builders
//   - find.go: Find methods returning *Match
//   - find_indices.go: FindIndices methods (zero-allocation)
//   - ismatch.go: IsMatch methods for boolean matching
//   - findall.go: FindAll*, Count, and FindSubmatch methods
//   - strategy.go: Strategy constants and selection logic
//   - config.go: Configuration options
//   - match.go: Match and MatchWithCaptures types
//   - search_state.go: Thread-safe state pooling
//   - anchored_literal.go: UseAnchoredLiteral implementation
//   - reverse_*.go: Reverse search implementations
package meta
