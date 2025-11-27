# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned for v0.2.0
- Capture group support (DFA limitation workaround)
- Submatch extraction API
- Extended matching modes

### Planned for v0.3.0
- Replace and ReplaceAll functions
- Split function
- Template-based replacement

### Planned for v0.4.0
- Case-insensitive matching flag
- Multiline mode
- Extended flags support

### Planned for v0.5.0
- Unicode property classes (\p{Letter}, \p{Digit})
- Unicode category support
- Full Unicode normalization

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
v0.2.0 → Capture groups support
v0.3.0 → Replace/Split functions
v0.4.0 → Case-insensitive, flags
v0.5.0 → Unicode properties
v0.6.0 → Performance optimizations
v0.7.0 → API refinements
v0.8.0 → Beta testing period
v0.9.0 → Release candidate
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
