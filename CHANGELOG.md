# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned
- Look-around assertions
- ARM NEON SIMD support (waiting for Go 1.26 native SIMD)
- UTF-8 Automata optimization

---

## [0.8.2] - 2025-12-03

### Fixed
- **Critical: Infinite loop in `onepass.Build()` for patterns like `(.*)`**
  - Bug: byte overflow when iterating ranges with hi=255 caused hang
  - Affected patterns: `(.*)`, `^(.*)$`, `([_a-zA-Z][_a-zA-Z0-9]*)=(.*)`
  - Thanks to Ben Hoyt (GoAWK) for reporting!

### Added
- **`Longest()` method**: API compatibility with stdlib `regexp.Regexp`
- **`QuoteMeta()` function**: Escape regex metacharacters in strings

---

## [0.8.1] - 2025-12-03

### Added
- **Type alias `Regexp`**: Drop-in compatibility with stdlib `regexp` package
  - `type Regexp = Regex` allows using `*regexp.Regexp` in existing code
  - Simply replace `import "regexp"` with `import regexp "github.com/coregx/coregex"`
  - Resolves [#5](https://github.com/coregx/coregex/issues/5)

---

## [0.8.0] - 2025-11-29

### Added
- **ReverseInner Strategy (OPT-010, OPT-012)**: Bidirectional DFA for `.*keyword.*` patterns
  - AST splitting: separate prefix/suffix NFAs for bidirectional search
  - Universal match detection: skip DFA scans for `.*` prefix/suffix
  - Early return on first confirmed match (leftmost-first semantics)
  - Prefilter finds inner literal, reverse DFA confirms prefix, forward DFA confirms suffix

### Performance
- **IsMatch speedup** (inner literal patterns):
  - `.*connection.*` 250KB: **3,154x faster** than stdlib (12.6ms → 4µs)
  - `.*database.*` 120KB: **1,174x faster** than stdlib
  - Many candidates (100 occurrences): **25x faster** than stdlib
- **Find speedup** (inner literal patterns):
  - `.*connection.*` 250KB: **1,894x faster** than stdlib (15.2ms → 8µs)
  - `.*database.*` 120KB: **2,857x faster** than stdlib (5.7ms → 2µs)
  - Many candidates (100 occurrences): **13x faster** than stdlib
- **Zero heap allocations** in hot path

### Technical
- New files: `meta/reverse_inner.go`, `meta/reverse_inner_test.go`
- Modified: `literal/extractor.go` (AST splitting), `meta/strategy.go`, `meta/meta.go`
- Code: +1,348 lines for ReverseInner implementation
- Tests: 7 new test suites, all passing
- Linter: 0 issues

---

## [0.7.0] - 2025-11-28

### Added
- **OnePass DFA (OPT-011)**: Zero-allocation captures for simple patterns
  - Automatically selected for anchored patterns with linear structure
  - 10x faster than PikeVM for capture group extraction
  - Zero allocations (vs PikeVM's 2-4 allocs per match)
  - Implemented using onepass compiler from stdlib

### Performance
- **FindSubmatch speedup**: ~700ns → 70ns (**10x faster**)
- **Zero allocations** vs 2-4 allocs with PikeVM
- Applicable to patterns like `^(prefix)([a-z]+)(suffix)$`

### Technical
- New package: `dfa/onepass/` with compiler and executor
- Modified: `meta/strategy.go`, `meta/meta.go`
- Tests: Comprehensive onepass test suite
- Linter: 0 issues

---

## [0.6.0] - 2025-11-28

### Added
- **ReverseSuffix Strategy (OPT-009)**: Zero-allocation reverse DFA for suffix patterns
  - Suffix literal prefilter + reverse DFA for patterns like `.*\.txt`
  - `SearchReverse()` / `IsMatchReverse()` - backward scanning without byte reversal
  - Greedy (leftmost-longest) matching semantics
  - Smart strategy selection: prefers prefix literals when available

### Performance
- **IsMatch speedup** (suffix patterns):
  - `.*\.txt` 1KB: **131x faster** than stdlib
  - `.*\.txt` 32KB: **1,549x faster** than stdlib
  - `.*\.txt` 1MB: **1,314x faster** than stdlib
- **Find speedup**: up to 1.6x faster than stdlib
- **Zero heap allocations** in hot path

### Technical
- New files: `meta/reverse_suffix.go`, `meta/reverse_suffix_test.go`, `meta/reverse_suffix_bench_test.go`
- Modified: `dfa/lazy/lazy.go` (SearchReverse/IsMatchReverse), `meta/strategy.go`, `meta/meta.go`
- Code: +1,058 lines for ReverseSuffix implementation
- Tests: 8 new tests, all passing
- Linter: 0 issues

---

## [0.5.0] - 2025-11-28

### Added
- **Named Capture Groups**: Full support for `(?P<name>...)` syntax
  - `SubexpNames()` API in `Regex`, `Engine`, and `NFA`
  - Compatible with stdlib `regexp.Regexp.SubexpNames()` behavior
  - Returns slice of capture group names (index 0 = entire match)
- **NFA Compiler Enhancement**: `collectCaptureInfo()` collects capture names during compilation
  - Two-pass algorithm: count captures, then collect names
  - Stores names from `syntax.Regexp.Name` field
- **Builder Enhancement**: `WithCaptureNames()` BuildOption for passing names to NFA

### Technical
- New files: `nfa/named_captures_test.go`, `example_subexpnames_test.go`
- Modified files: `nfa/nfa.go`, `nfa/compile.go`, `nfa/builder.go`, `meta/meta.go`, `regex.go`
- Code: +200 lines for named captures implementation
- Tests: 18 new tests for named captures, all passing
- Examples: 2 integration examples demonstrating SubexpNames() usage

---

## [0.4.0] - 2025-11-28

### Added
- **Reverse Search Engine**: Complete reverse NFA/DFA construction
  - `nfa.Reverse()` - Build reverse NFA from forward NFA
  - `nfa.ReverseAnchored()` - Build anchored reverse NFA for `$` patterns
  - Two-pass algorithm for correct state ordering
  - Comprehensive test suite (12 tests)

- **ReverseAnchored Strategy**: Optimized search for `$` anchor patterns
  - New `UseReverseAnchored` strategy in meta-engine
  - `ReverseAnchoredSearcher` with reversed haystack search
  - `IsPatternEndAnchored()` for `$` and `\z` detection
  - Automatic strategy selection for end-anchored patterns

- **Core Optimizations (OPT-001..006)**:
  - **OPT-001**: Start State Caching - 6 start configurations with StartByteMap
  - **OPT-002**: Prefilter Effectiveness Tracking - dynamic disabling at >90% false positives
  - **OPT-003**: Early Match Termination - `searchEarliestMatch()` for IsMatch
  - **OPT-004**: State Acceleration - memchr/memchr2/memchr3 in DFA loop
  - **OPT-005**: ByteClasses - alphabet compression for reduced DFA states
  - **OPT-006**: Specialized Search Functions - optimized Count/FindAllSubmatch

### Fixed
- **FIX-001**: PikeVM visited check in `addThreadToNext`
  - Prevents exponential thread explosion for patterns with character classes
  - Added visited check per rust-regex pikevm.rs:1683 pattern
  - Fixed `visited.Clear()` timing in searchAt/searchAtWithCaptures

- **FIX-002**: ReverseAnchored unanchored prefix bug
  - Critical bug: Reverse NFA incorrectly included `.*?` prefix loop
  - Caused O(n*m) instead of O(m) for end-anchored patterns
  - Fixed by skipping unanchored prefix states in `ReverseAnchored()`
  - Result: Easy1 1MB: 340 sec → 1.6 ms (**205,000x faster**)

### Performance
- **Easy1 `$` anchor 1MB**: 340 sec → 1.6 ms (**205,000x faster** - critical bug fix)
- Case-insensitive patterns: Still **233x faster** than stdlib
- Hard1 multi-alternation: **5.2x faster** than stdlib

### Technical
- New files: `nfa/reverse.go`, `nfa/reverse_test.go`
- New files: `meta/reverse_anchored.go`, `meta/reverse_anchored_test.go`
- Modified: `meta/strategy.go`, `meta/meta.go`, `nfa/compile.go`
- Code: +1000 lines for reverse search implementation
- Tests: All passing, 0 linter issues

---

## [0.3.0] - 2025-11-27

### Added
- **Replace functions**: Full stdlib-compatible replacement API
  - `ReplaceAll()` / `ReplaceAllString()` - replace with template expansion
  - `ReplaceAllLiteral()` / `ReplaceAllLiteralString()` - literal replacement (no $ expansion)
  - `ReplaceAllFunc()` / `ReplaceAllStringFunc()` - replace with function callback
- **Split function**: `Split(s string, n int)` - split string by regex
- **Template expansion**: `$0`-`$9` backreference support in replacement templates
- **FindAllIndex**: `FindAllIndex()` / `FindAllStringIndex()` for batch index retrieval

### Technical
- Pre-allocation optimization for replacement buffers
- Proper `$$` escape handling (literal `$`)
- Empty match handling to prevent infinite loops

---

## [0.2.1] - 2025-11-27

### Fixed
- Documentation: Updated README.md with v0.2.0 features and performance numbers
- Updated performance claims from 143x to 263x (accurate benchmark results)
- Added capture groups to feature table

---

## [0.2.0] - 2025-11-27

### Added
- **Capture groups support**: Full submatch extraction via PikeVM
- `FindSubmatch()` / `FindStringSubmatch()` - returns all capture groups
- `FindSubmatchIndex()` / `FindStringSubmatchIndex()` - returns group positions
- `NumSubexp()` - returns number of capture groups
- NFA `StateCapture` state type for group boundaries
- Thread-local capture tracking in PikeVM with copy-on-write semantics

### Performance
- Case-insensitive 32KB: **263x faster** than stdlib
- Case-insensitive 1KB: **92x faster** than stdlib
- Case-sensitive 1KB: **3.5x faster** than stdlib
- Small inputs (16B): ~4x overhead due to multi-engine architecture (acceptable trade-off)

### Technical
- Captures follow Thompson's construction as epsilon transitions
- DFA path unchanged - captures only allocated when requested via FindSubmatch

---

### Planned for v0.7.0
- OnePass DFA for simple patterns
- ReverseInner strategy for `prefix.*keyword.*suffix`

### Planned for v0.8.0
- ARM NEON SIMD support
- Aho-Corasick for large multi-pattern sets
- Memory layout optimizations

### Planned for v1.0.0
- API stability guarantee
- Backward compatibility promise
- Production-ready designation
- Performance benchmarks vs stdlib (official)

---

## [0.1.4] - 2025-11-27

### Fixed
- Documentation: Fixed broken benchmark link in README (`benchmarks/` → `benchmark/`)
- Documentation: Updated CHANGELOG with v0.1.2 and v0.1.3 release notes
- Documentation: Updated current version references in README

---

## [0.1.3] - 2025-11-27

### Fixed
- **Critical DFA cache bug**: Start state ID was being overwritten by cache, causing every DFA search to fall back to slow NFA (200x performance regression)
- **Leftmost-longest semantics**: Fixed DFA search to properly return first match position with greedy extension

### Performance
- DFA with prefilter: 887,129 ns → 4,375 ns (**202x faster**)
- Case-insensitive patterns: **143x faster** than stdlib (5,883 ns vs 842,422 ns)

---

## [0.1.2] - 2025-11-27

### Fixed
- **Strategy selection order**: Patterns with good literals now correctly use DFA+prefilter instead of NFA
- **Match bounds**: Complete prefilter matches now return correct bounds using PikeVM
- **DFA match start position**: Fixed start position calculation for unanchored patterns

### Changed
- Removed unused `estimateMatchLength()` function
- Converted if-else chains to switch statements (linter compliance)

---

## [0.1.1] - 2025-11-27

### Fixed
- **O(n²) complexity bug**: Fixed PikeVM unanchored search that caused quadratic performance
- **Lazy DFA unanchored search**: Added dual start states for O(n) unanchored matching

---

## [0.1.0] - 2025-01-26

### Added

#### Phase 1: SIMD Primitives
- **SIMD Memchr** - Fast byte search primitives
  - `simd.Memchr()` - Single byte search with AVX2/SSE4.2 (1.7x faster @ 1MB)
  - `simd.Memchr2()` - Two-byte search
  - `simd.Memchr3()` - Three-byte search
  - Platform support: AMD64 (AVX2/SSE4.2) + fallback for other platforms
  - Zero allocations in hot paths

- **SIMD Memmem** - Fast substring search
  - `simd.Memmem()` - Substring search (6.8x - 87.4x faster than `bytes.Index`)
  - Rare byte heuristic optimization
  - Throughput: 57-63 GB/s on typical patterns
  - Zero allocations

#### Phase 2: Literal Extraction
- **Literal Types** - Core types for literal sequences
  - `literal.Literal` - Single literal with completeness flag
  - `literal.Seq` - Sequence of literals with optimization operations
  - Operations: `Minimize()`, `LongestCommonPrefix()`, `LongestCommonSuffix()`

- **Literal Extractor** - Extract literals from regex patterns
  - `literal.ExtractPrefixes()` - Extract prefix literals
  - `literal.ExtractSuffixes()` - Extract suffix literals
  - `literal.ExtractInner()` - Extract inner literals
  - Supports 8 syntax.Op types (OpLiteral, OpConcat, OpAlternate, OpCapture, OpCharClass, etc.)
  - Configurable limits (MaxLiterals: 64, MaxLiteralLen: 64)

#### Phase 3: Prefilter System
- **Prefilter Interface** - Automatic prefilter selection
  - `prefilter.Prefilter` interface - Universal prefilter API
  - `prefilter.Builder` - Automatic strategy selection
  - `MemchrPrefilter` - Single byte search (11-60 GB/s)
  - `MemmemPrefilter` - Single substring search (4-79 GB/s)
  - Zero allocations in hot paths

- **Teddy Multi-Pattern SIMD** - Fast multi-pattern search
  - Slim Teddy algorithm (8 buckets, 1-byte fingerprint)
  - SSSE3 assembly (16 bytes/iteration)
  - Supports 2-8 patterns
  - Expected 20-50x speedup vs naive multi-pattern search

#### Phase 4: NFA & Lazy DFA Engines
- **NFA Thompson's Construction** - Non-deterministic finite automaton
  - `nfa.Compile()` - Thompson's construction compiler
  - `nfa.PikeVM` - NFA execution engine with capture support
  - `nfa.Builder` - Programmatic NFA construction API
  - `sparse.SparseSet` - O(1) state tracking data structure
  - Zero allocations in state tracking

- **Lazy DFA Engine** - On-demand determinization
  - `lazy.DFA` - Main DFA search engine
  - `lazy.Find()` - Find first match
  - `lazy.IsMatch()` - Boolean matching
  - On-demand state construction during search
  - Thread-safe caching with statistics
  - NFA fallback when cache full
  - O(n) time complexity (linear in input)
  - Expected 10-100x speedup vs pure NFA

- **Meta Engine & Public API** - Intelligent orchestration
  - **Public API** in root package:
    - `Compile(pattern string) (*Regex, error)` - Compile regex pattern
    - `MustCompile(pattern string) *Regex` - Compile or panic
    - `CompileWithConfig(pattern, config) (*Regex, error)` - With custom config
  - **Matching methods**:
    - `Match([]byte) bool` - Boolean matching
    - `MatchString(string) bool` - String matching
  - **Finding methods**:
    - `Find([]byte) []byte` - Find first match bytes
    - `FindString(string) string` - Find first match string
    - `FindIndex([]byte) []int` - Find match position
    - `FindStringIndex(string) []int` - Find string match position
    - `FindAll([]byte, n int) [][]byte` - Find all matches
    - `FindAllString(string, n int) []string` - Find all string matches
  - **Meta Engine**:
    - Intelligent strategy selection (UseNFA/UseDFA/UseBoth)
    - Automatic prefilter integration
    - Full pipeline: Pattern → NFA → Literals → Prefilter → DFA → Search
  - **Strategy selection heuristics**:
    - Tiny patterns (< 20 states) → NFA only
    - Good literals (LCP ≥ 3) → DFA + Prefilter (5-50x speedup)
    - Large patterns (> 100 states) → DFA
    - Medium patterns → Adaptive (try DFA, fallback to NFA)

### Documentation
- Comprehensive Godoc documentation for all public APIs
- 54 runnable examples across all packages
- Implementation guides for Teddy SIMD algorithm
- Reference documentation from Rust regex crate

### Performance
- 5-50x faster than stdlib `regexp` for patterns with literals
- Zero allocations in steady state (after warm-up)
- O(n) time complexity for DFA search
- Thread-safe implementation

### Testing
- 77.0% average test coverage across all packages
- Public API: 94.5% coverage
- 400+ test cases covering edge cases
- Fuzz testing for correctness
- Comparison tests vs stdlib regexp
- Zero linter issues across ALL 13 tasks

### Changed
- N/A (initial release)

### Deprecated
- N/A (initial release)

### Removed
- N/A (initial release)

### Fixed
- N/A (initial release)

### Security
- No known security issues
- All inputs validated
- No unsafe operations outside of SIMD assembly

---

## Project Statistics (v0.1.0)

**Code**:
- 46 Go files
- 13,294 total lines (8,500 implementation + 4,800 tests)
- 7 packages (simd, literal, prefilter, nfa, dfa/lazy, meta, root)

**Quality**:
- 77.0% average test coverage
- 0 linter issues (13/13 tasks clean!)
- Production-quality code

**Performance**:
- Memchr: 1.7x speedup @ 1MB
- Memmem: 6.8x - 87.4x speedup vs stdlib
- Prefilter throughput: 4-79 GB/s
- Expected total: 5-50x faster than stdlib for patterns with literals

**Development**:
- Completed in one day (2025-01-26)
- ~8-10 hours from zero to release-ready
- All phases completed (Phase 1-4)

---

## Roadmap to v1.0.0

```
v0.1.0 → Initial release (DONE ✅)
v0.2.0 → Capture groups support (DONE ✅)
v0.3.0 → Replace/Split functions (DONE ✅)
v0.4.0 → Reverse Search + Core Optimizations (DONE ✅)
v0.5.0 → Named captures (DONE ✅)
v0.6.0 → ReverseSuffix optimization (DONE ✅) ← YOU ARE HERE
v0.7.0 → OnePass DFA, ReverseInner strategy
v0.8.0 → ARM NEON SIMD, Aho-Corasick
v0.9.0 → Beta testing period
v1.0.0 → Stable release (API frozen)
```

---

## Notes

### What's Included in v0.1.0
✅ Multi-engine regex architecture
✅ SIMD-accelerated primitives (AVX2/SSE4.2)
✅ Literal extraction and prefiltering
✅ NFA (Thompson's) + Lazy DFA engines
✅ Intelligent strategy selection
✅ stdlib-compatible basic API
✅ Comprehensive test suite
✅ Full documentation + examples

### What's NOT Included (Future Versions)
❌ Capture group support (DFA limitation)
❌ Replace/Split functions
❌ Case-insensitive matching
❌ Unicode property classes
❌ API stability guarantee (v1.0+ only)

### Important
**v0.1.0 is experimental**. API may change in v0.2+. While code quality is production-ready, use in production systems with caution until v1.0.0 release with API stability guarantee.

---

[Unreleased]: https://github.com/coregx/coregex/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/coregx/coregex/releases/tag/v0.1.0
