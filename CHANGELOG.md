# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned
- Look-around assertions
- ARM NEON SIMD support (waiting for Go 1.26 native SIMD)
- SIMD prefilter for CompositeSequenceDFA (#83)

## [0.12.1] - 2026-02-06

### Performance
- **DFA bidirectional fallback for BoundedBacktracker** — When BoundedBacktracker
  can't handle large inputs (exceeds 32M entry limit), use forward DFA + reverse
  DFA instead of PikeVM. Forward DFA finds match end, reverse DFA finds match
  start. O(n) total vs PikeVM's O(n*states). ~3x speedup on `(\w{2,8})+` at 6MB.
- **Digit-run skip optimization** — For `\d+`-leading patterns (IP addresses,
  version numbers), skip entire digit run on DFA failure instead of advancing
  one byte at a time. Only enabled when the leading digit class has a greedy
  unbounded quantifier.

### Fixed
- **CompositeSequenceDFA overmatching for bounded patterns** — Bare character
  classes like `\w` (maxMatch=1) were treated as unbounded by the DFA, causing
  `\w\w` on "000" to return "000" instead of "00". Now rejects patterns with
  bounded maxMatch, falling back to CompositeSearcher backtracking.
- **AVX2 Teddy assembly correctness** — Fixed `teddySlimAVX2_2` returning
  position -1 (not-found sentinel) for valid candidates in short haystacks,
  caused by `DECQ SI` executing when there was no prior chunk boundary to cover.
  AVX2 dispatch remains disabled by default (SSSE3 is 4x faster on AMD EPYC
  due to VZEROUPPER overhead on frequent verification restarts).

## [0.12.0] - 2026-02-06

### Performance
- **Anti-quadratic guard for reverse searches** — Track `minStart` position in
  ReverseSuffix, ReverseInner, and ReverseSuffixSet searchers to prevent O(n²)
  degradation on high false-positive suffix workloads. Falls back to PikeVM
  when quadratic behavior detected. Inspired by Rust regex `meta/limited.rs`.
- **Lazy DFA 4x loop unrolling** — Process 4 state transitions per inner loop
  iteration in forward and reverse DFA search. Check special states only between
  batches. Direct field access for minimal per-transition overhead.
  Expected 15-40% throughput on DFA-heavy patterns (alpha_digit, word_digit).
- **Prefilter `IsFast()` gate** — Skip reverse search optimizations when a fast
  SIMD-backed prefix prefilter already exists. Heuristic: Memchr/Memmem always
  fast, Teddy fast when `minLen >= 3`. Inspired by Rust regex `is_fast()`.
- **DFA cache clear & continue** — On cache overflow, clear and fall back to
  PikeVM for current search instead of permanently disabling DFA. Configurable
  `MaxCacheClears` limit (default 5). Inspired by Rust regex `try_clear_cache`.

### Fixed
- **OnePass DFA capture limit** — Tighten from 17 to 16 capture groups.
  `uint32` slot mask (32 bits) can only track 16 groups (slots 0..31).
  Group 17 silently dropped end position due to `slotIdx < 32` guard.

---

## [0.11.9] - 2026-02-02

### Fixed
- **Missing first-byte prefilter in FindAll state-reusing path** (Issue #107)
  - `findIndicesBoundedBacktrackerAtWithState` was missing `anchoredFirstBytes` check
  - Pattern `^/.*[\w-]+\.php` (without `$`) took 377ms instead of 40µs on 6MB input
  - Added O(1) early rejection for anchored patterns not starting with required byte
  - Fix: **377ms → 40µs** (9000x improvement for non-matching anchored patterns)

---

## [0.11.8] - 2026-02-01

### Fixed
- **Critical regression in UseAnchoredLiteral strategy** (Issue #107)
  - `FindIndices*` and `findIndicesAtWithState` were missing `UseAnchoredLiteral` case
  - Pattern `^/.*[\w-]+\.php$` fell through to slow NFA path: 0.01ms → 408ms (40,000x slower)
  - Added `findIndicesAnchoredLiteral` and `findIndicesAnchoredLiteralAt` methods
  - Now correctly uses O(1) anchored literal matching: **408ms → 0.5ms**

---

## [0.11.7] - 2026-02-01

### Fixed
- **FindAll now uses optimized state-reusing path** (Issue #107)
  - FindAll was using slow per-match loop instead of optimized findAllIndicesStreaming
  - Results for `(\w{2,8})+` on 6MB: 2179ms → 834ms (**2.6x faster**)
  - Now **1.08x faster than stdlib** (was 2.4x slower in regex-bench)

---

## [0.11.6] - 2026-02-01

### Performance
- **Windowed BoundedBacktracker for large inputs** (Issue #107)
  - V12 optimization: When input exceeds BoundedBacktracker's maxInput (~914KB),
    search in a window of maxInput bytes first before falling back to PikeVM
  - Most patterns produce short matches found within the first window
  - **6MB now 1.68x FASTER than stdlib** (was 2.2x slower!)
  - Benchmark for `(\w{2,8})+` on 6MB: 1900ms → 628ms (3x improvement)
- **SlotTable architecture** (Rust-style)
  - Per-state slot storage instead of per-thread COW captures
  - Dynamic slot sizing: 0 (IsMatch), 2 (Find), full (Captures)
  - Lightweight searchThread: 16 bytes (was 40+ bytes)
- **BoundedBacktracker optimizations for word_repeat patterns** (Issue #107)
  - Switch from uint32 to uint16 generation tracking (2x memory savings)
  - Cache-friendly memory layout: `pos * numStates + state`
  - Slice haystack to remaining portion in `findIndicesBoundedBacktrackerAt`
- **PikeVM visited state tracking optimization**
  - Consolidate Contains+Insert to single Insert call
  - Saves ~8% of SparseSet operations in hot path
- **FindAll/Count sync.Pool overhead elimination**
  - Acquire SearchState once, reuse for all iterations
  - Allocations reduced from 1.29M to 49 for 6MB input

### Results for `(\w{2,8})+` pattern vs stdlib
| Size | Speedup |
|------|---------|
| 10KB | **1.68x faster** |
| 50KB | **1.88x faster** |
| 100KB | **2.04x faster** |
| 1MB | **1.67x faster** |
| 6MB | **1.68x faster** |

---

## [0.11.5] - 2026-02-01

### Fixed
- **checkHasWordBoundary catastrophic slowdown** (Issue #105)
  - Patterns with `\w{n,m}` quantifiers were **7,000,000x slower** than stdlib
  - Root cause: O(N*M) complexity from scanning all NFA states per byte
  - Fix: Use `NewBuilderWithWordBoundary()`, add `hasWordBoundary` guards, anchored prefilter verification
  - **Result: 3m22s → 3.6µs** (56,000,000x faster, **7.9x faster than stdlib**)

### Performance
- **DFA state lookup: map → slice** — 42% CPU time eliminated
  - State IDs are sequential, so direct slice indexing beats hash lookups
- **Literal extraction from capture/repeat groups** — better prefilters
  - `=(\$\w...){2}` now extracts `=$` (2 bytes) instead of just `=` (1 byte)
  - Reduces false positives in prefilter, massive speedup on selective patterns

### Technical Details
- Added `searchEarliestMatchAnchored()` for O(1) prefilter verification
- Replaced `stateByID map[StateID]*State` with `states []*State`
- Extended `tryExpandConcatSuffix()` to unwrap OpRepeat/OpCapture
- Credits: @danslo for root cause analysis and fix suggestions

---

## [0.11.4] - 2026-01-16

### Fixed
- **FindAll/FindAllIndex now use UseMultilineReverseSuffix strategy** (Issue #102)
  - Previously `FindIndicesAt()` was missing dispatch for `UseMultilineReverseSuffix`
  - `IsMatch`/`Find` were fast (1µs), but `FindAll` was slow (78ms) - **100x gap vs Rust**
  - After fix: `FindAll` on 6MB with 2000 matches: **~1ms** (was 78ms)

### Changed
- Updated `golang.org/x/sys` dependency v0.39.0 → v0.40.0

---

## [0.11.3] - 2026-01-16

### Changed
- **UseMultilineReverseSuffix: Prefix verification fast path** (Issue #99)
  - Pattern `(?m)^/.*\.php` now **319-552x faster** than stdlib (was 3.5-5.7x)
  - Algorithm: suffix prefilter → SIMD backward scan → O(1) prefix byte check
  - Skip-to-next-line optimization: avoids O(n²) worst case
  - DFA fallback for complex patterns without extractable prefix

### Performance (pattern `(?m)^/.*\.php` on 1KB log data)

| Operation | coregex | stdlib | Speedup |
|-----------|---------|--------|---------|
| IsMatch | 182 ns | 100 µs | **552x** |
| Find | 240 ns | 81 µs | **338x** |
| CountAll | 58 µs | 18.7 ms | **319x** |

### Technical Details
- `MultilineReverseSuffixSearcher.prefixBytes` for O(1) verification
- `SetPrefixLiterals()` extracts prefix from pattern
- `findLineStart()` uses SIMD `bytes.LastIndexByte`
- Skip-to-next-line: on prefix mismatch, jump to next `\n` position

---

## [0.11.2] - 2026-01-16

### Changed
- **UseMultilineReverseSuffix: DFA verification instead of PikeVM** (Issue #99)
  - Replaced O(n*m) PikeVM verification with O(n) DFA verification
  - Uses `lazy.DFA.SearchAtAnchored()` for linear-time anchored matching

---

## [0.11.1] - 2026-01-16

### Added
- **UseMultilineReverseSuffix Strategy** - Line-aware suffix search for multiline patterns (Fixes #97)
  - Pattern `(?m)^/.*\.php`: **3.5-5.7x faster** than stdlib (was 24% slower)
  - Algorithm: suffix prefilter → backward scan to line start → forward PikeVM verification
  - No-match cases: **12-130x faster** due to efficient suffix prefilter rejection
  - New 18th strategy in meta-engine
  - Files: `meta/reverse_suffix_multiline.go`, `meta/reverse_suffix_multiline_test.go`

### Performance (Issue #97 pattern `(?m)^/.*\.php` on 0.5MB log file)

| Operation | coregex | stdlib | Speedup |
|-----------|---------|--------|---------|
| IsMatch | 20.6 µs | 72.2 µs | **3.5x** |
| Find | 15.3 µs | 68.7 µs | **4.5x** |
| CountAll (200 matches) | 2.56 ms | 14.6 ms | **5.7x** |

---

## [0.11.0] - 2026-01-15

### Added
- **UseAnchoredLiteral Strategy** - O(1) specialized matching for `^prefix.*suffix$` patterns (Fixes #79)
  - Pattern `^/.*[\w-]+\.php$`: **32-133x faster** than stdlib (was 5.3x slower)
  - Algorithm: O(1) length check → O(k) prefix match → O(k) suffix match → O(m) charclass bridge
  - Supports patterns with: prefix literals, `.*/+` wildcards, charclass+ bridge, suffix literals
  - New 17th strategy in meta-engine
  - Files: `meta/anchored_literal.go` (350 lines), `meta/anchored_literal_test.go` (495 lines)

- **V11-002 ASCII Runtime Detection** - SIMD-accelerated input classification
  - Dual NFA compilation: UTF-8 NFA (28 states) + ASCII NFA (2 states) for patterns with `.`
  - Runtime ASCII detection using AVX2/SSE2/SWAR (3-4ns overhead)
  - ASCII input: up to 1.6x faster match time
  - Config option: `EnableASCIIOptimization` (default: true)

### Fixed
- **OnePass DFA handles StateLook anchors** - `^`, `$`, `\A`, `\z` transitions now work correctly
- **Suffix extraction skips trailing anchors** - O(1) rejection for suffix patterns improved

### Changed
- **meta.go refactored into 6 focused files** (internal, no API changes)
  - `engine.go` (230): Engine struct, Stats, core API
  - `compile.go` (526): Compilation, builders
  - `find.go` (749): Find methods returning *Match
  - `find_indices.go` (733): Zero-alloc FindIndices methods
  - `ismatch.go` (353): IsMatch boolean methods
  - `findall.go` (285): FindAll*, Count, FindSubmatch
  - `meta.go` (81): Package documentation only

### Performance (Issue #79 pattern `^/.*[\w-]+\.php$`)

| Input | coregex | stdlib | Speedup |
|-------|---------|--------|---------|
| Short (24B) | 7.6 ns | 241 ns | **32x** |
| Medium (45B) | 7.8 ns | 347 ns | **44x** |
| Long (78B) | 7.9 ns | 516 ns | **65x** |
| No match (45B) | 4.4 ns | 590 ns | **133x** |

---

## [0.10.10] - 2026-01-15

### Fixed
- **ReverseSuffix whitelist includes CharClass Plus** - Performance regression fix
  - Bug: `[^\s]+\.txt` pattern caused extreme slowdown (266ms/MB instead of µs)
  - Root cause: `isSafeForReverseSuffix` only recognized `.*` and `.+` wildcards
  - Fix: CharClass Plus patterns (`[^\s]+`, `[\w]+`) now qualify for reverse suffix optimization
  - Result: `suffix_find` pattern now completes in 398µs (was timing out)

---

## [0.10.9] - 2026-01-15

### Added
- **UTF-8 suffix sharing for dot NFA** - Performance optimization (#79)
  - Dot metacharacter NFA states reduced from 39 to 28
  - Based on Rust regex-automata approach

- **Anchored suffix prefilter** - O(1) rejection for suffix patterns (#79)
  - Patterns ending with literal suffix get O(1) rejection when suffix not found

### Fixed
- **CharClassSearcher now excludes `*` patterns** - Zero-width match bug fix
  - Bug: `[0-9]*` on "A" returned false instead of true (zero-width match)
  - Root cause: CharClassSearcher doesn't support zero-width matches
  - Fix: Only use CharClassSearcher for `+` patterns, `*` uses BoundedBacktracker

- **Invalid UTF-8 handling for negated char classes** - stdlib compatibility
  - `\D`, `\S`, `\W`, `[^x]` now match invalid UTF-8 bytes (0x80-0xFF)
  - Multi-byte paths take precedence for valid UTF-8 (longer match wins)
  - Note: Partial Unicode classes like `\P{Han}` don't get this fix to avoid
    incorrectly matching bytes of valid UTF-8 sequences

- **ReverseInner whitelist** - Strategy safety
  - Bug: `A*20*` on "2" returned false instead of true
  - Root cause: ReverseInner strategy unsafe for patterns with Star of Literal
  - Fix: Only allow AnyChar wildcards (`.*`) or CharClass Plus (`[\w]+`) prefixes

- **ReverseSuffix whitelist** - Strategy safety
  - Bug: `0?0` on "0", `0?^0` on "0" returned wrong results
  - Root cause: Reverse NFA bug with optional elements and internal anchors
  - Fix: Only enable ReverseSuffix for patterns with AnyChar wildcards

---

## [0.10.8] - 2026-01-15

### Fixed
- **Performance: FindAll 600x faster for anchored patterns on large inputs** (#92)
  - Bug: `FindAll("^HTTP/[12]\.[01]", 6MB)` took 346µs instead of <1µs
  - Root cause: Allocation heuristic `make([][2]int, 0, len(haystack)/100+1)` created 1MB buffer for 1 match
  - Fix: Smart allocation - anchored patterns use cap=1, others capped at 256
  - Result: Now matches stdlib performance (~500ns for anchored patterns)

### Added
- `Engine.IsStartAnchored()` method for allocation optimization

---

## [0.10.7] - 2026-01-15

### Added
- **100% stdlib regexp API compatibility** - All remaining methods now implemented:
  - `CompilePOSIX`, `MustCompilePOSIX` - POSIX ERE compilation with leftmost-longest semantics
  - `Match(pattern, b)`, `MatchString(pattern, s)` - Package-level matching functions
  - `MatchReader(pattern, r)` - Package-level reader matching
  - `SubexpIndex(name)` - Find named capture group index
  - `LiteralPrefix()` - Extract literal prefix from pattern
  - `Expand`, `ExpandString` - Template expansion with `$n` substitution
  - `Copy()` - Regex duplication (deprecated since Go 1.12)
  - `MarshalText`, `UnmarshalText` - encoding.TextMarshaler/TextUnmarshaler interface
  - `MatchReader`, `FindReaderIndex`, `FindReaderSubmatchIndex` - io.RuneReader methods

- **New stdlib compatibility tests** (`regex_stdlib_compat_test.go`):
  - Tests for all newly added methods
  - Direct comparison with stdlib behavior
  - Edge case coverage for LiteralPrefix with anchors

### Fixed
- **Critical: Dot metacharacter matched UTF-8 bytes instead of codepoints** (#85)
  - Bug: `FindAllString(".", "日本語")` returned 9 matches (bytes) instead of 3 (codepoints)
  - Root cause: `compileAnyCharNotNL` used byte range `[0x00-0x09, 0x0B-0xFF]` instead of UTF-8 sequences
  - Fix: New `compileUTF8Any()` builds proper 1/2/3/4-byte UTF-8 automata
  - Handles all valid UTF-8: ASCII, 2-byte (U+0080-U+07FF), 3-byte (U+0800-U+FFFF), 4-byte (U+10000-U+10FFFF)
  - Correctly excludes surrogates (U+D800-U+DFFF) in 3-byte sequences

- **Critical: Negated Unicode property classes matched bytes instead of codepoints** (#91)
  - Bug: `\P{Han}` on "中" (3-byte UTF-8) returned 3 matches instead of 0
  - Root cause: `compileUnicodeClassLarge` matched individual bytes `[0x80-0xFF]`
  - Fix: Proper UTF-8 range compilation via `compileUTF8Range()` family of functions
  - Optimization: Simple negated ASCII classes (e.g., `[^,]`) use efficient "any UTF-8" path
  - Complex classes (e.g., `\P{Han}`) use precise UTF-8 automata for each range

- **Empty character classes incorrectly matched empty string** (#88)
  - Bug: `[^\S\s]` (logically empty) matched empty strings
  - Fix: Use `compileNoMatch()` instead of `compileEmptyMatch()` for empty ranges

- **Case-insensitive patterns used incorrect literal prefilters** (#87)
  - Bug: `(?i)hello` with literal prefilter searched for exact "hello" bytes
  - Fix: Skip literal extraction when `FoldCase` flag is set in `extractPrefixes/Suffixes/Inner`

- **Empty pattern Split returned incorrect results** (#90)
  - Bug: `Split("", "abc")` behavior differed from stdlib
  - Fix: Handle zero-width matches at string boundaries correctly

### Changed
- `compileUnicodeClassLarge` now builds proper UTF-8 automata instead of byte approximations
- Added 6 new UTF-8 compilation helper functions for precise range handling
- Improved surrogate gap handling in 3-byte UTF-8 compilation

### Added
- `TestDotMatchesUTF8Codepoints` - regression tests for dot metacharacter
- `TestNegatedUnicodePropertyClass` - regression tests for `\P{Han}`, `\P{Latin}`, etc.
- `TestEmptyCharacterClass` - regression tests for `[^\S\s]`, `[^\D\d]`

---

## [0.10.6] - 2026-01-14

### Added
- **CompositeSequenceDFA: 5x speedup for overlapping char class patterns** (#83)
  - Patterns like `\w+[0-9]+`, `[a-zA-Z0-9]+[0-9]+` where char classes overlap
  - Uses NFA subset construction for correct overlap handling
  - Byte class reduction: 256 bytes → 3-8 equivalence classes
  - First-part skip optimization: O(1) check if position can start match
  - Loop unrolling: 4 bytes per iteration (Rust-inspired)
  - Internal: 125 MB/s raw DFA (vs 45 MB/s backtracking CompositeSearcher)

- **FindAllIndexCompact API: zero per-match allocations**
  - `FindAllIndexCompact(b []byte, n int, results [][2]int) [][2]int`
  - Returns `[][2]int` instead of `[][]int` (single allocation vs N allocations)
  - Supports buffer reuse for repeated searches
  - 60974 → 9 allocations for 1MB input with many matches

### Performance
- CompositeSequenceDFA: **5-7x faster than stdlib** for overlapping patterns (raw DFA: 3x faster than backtracking)
- FindAllIndexCompact: **99.98% allocation reduction** for FindAllIndex

---

## [0.10.5] - 2026-01-14

### Fixed
- **Critical: CompositeSearcher failed on overlapping character classes** (#81)
  - Bug: Patterns like `\w+[0-9]+` returned 0 matches (`\w` includes digits)
  - Root cause: Greedy `\w+` consumed all characters including digits, leaving nothing for `[0-9]+`
  - Fix: Implemented recursive backtracking in `matchAt()` function
  - Algorithm: Try greedy (max) first, reduce match length if next parts fail
  - O(n) worst case, minimal overhead for non-overlapping patterns
  - Rust comparison: PikeVM uses parallel state simulation (more complex); our approach uses O(1) membership tables + backtracking

### Added
- **Regression tests for overlapping character classes**
  - `TestCompositeSearcher_OverlappingCharClasses` - 17 test cases
  - Patterns: `[a-zA-Z0-9]+[0-9]+`, triple overlap, edge cases with `*` quantifier
  - `BenchmarkCompositeSearcher_Overlapping` - performance tracking

---

## [0.10.4] - 2026-01-14

### Fixed
- **Critical: Panic on concurrent usage of compiled Regexp** (#78, PR #80)
  - Bug: `index out of range` panic when using same `*Regexp` from multiple goroutines
  - Root cause: BoundedBacktracker and PikeVM had shared mutable state (`visited`, `queue`)
  - Fix: Implemented `sync.Pool` pattern (same as Go stdlib `regexp`)
  - Added `SearchState` struct for per-search mutable state
  - Added `BacktrackerState` and `PikeVMState` for externalized state
  - All search operations now thread-safe via pooled state
  - New concurrent tests: `TestConcurrentMatch`, `TestConcurrentFind`, etc.

- **32-bit platform compatibility for atomic operations**
  - Fixed `unaligned 64-bit atomic operation` panic on Linux 386
  - Stats struct moved to first field in Engine for 8-byte alignment
  - All stats counters now use `sync/atomic.AddUint64` for thread-safety

- **OnePass cache thread-safety**
  - Moved `onepassCache` from shared Engine to pooled `SearchState`
  - Eliminates data race in `FindSubmatch` operations

### Added
- **CI benchmarks for new optimizations**
  - `meta/composite_bench_test.go` - CompositeSearcher benchmarks
  - `meta/branch_dispatch_bench_test.go` - BranchDispatch benchmarks
  - Patterns: `[a-zA-Z]+\d+`, `^(\d+|UUID|hex32)`, HTTP methods

### Performance
- **Geomean: -3.84% improvement** (faster, not regression)
- Thread-safe operations with minimal overhead via sync.Pool

---

## [0.10.3] - 2026-01-08

### Fixed
- **Critical: FindStringSubmatch returned incorrect capture groups for `.+` patterns** (#77)
  - Bug: `^(.+)-(\d+)$` on "hello-123" returned `matches[1]="hello-123"` instead of `"hello"`
  - Root cause: `StateSplit` in PikeVM passed captures to both branches without cloning
  - COW (Copy-on-Write) mechanism failed because `refs=1`, causing in-place modifications
  - Fix: Clone captures for right branch to ensure proper COW semantics
  - Affected patterns: `.+`, `.+?`, `.*`, `.*?` in capture groups
  - Not affected: Explicit character classes (`[a-z]+`, `\w+`)

### Added
- **Comprehensive regression tests for capture groups**
  - `TestFindStringSubmatch_DotPlusCapture` - 16 test cases
  - `TestFindStringSubmatch_DotPlusCapture_StdlibCompatibility` - stdlib verification
  - `TestFindSubmatchIndex_DotPlusCapture` - index boundary tests

---

## [0.10.2] - 2026-01-07

### Fixed
- **Version pattern regression hotfix** (#75)
  - Restored DigitPrefilter for digit-lead patterns like `\d+\.\d+\.\d+`
  - v0.10.1 incorrectly chose ReverseInner with "." as inner literal
  - Performance restored: 8.2ms → 2.15ms (3.8x speedup)

### Changed
- **Lint config**: Added exclusion for unused AVX2 functions in `_amd64.go` files
  - These functions are tested but not integrated into production prefilter (#74)

---

## [0.10.1] - 2026-01-07

### Added
- **AVX2 Slim Teddy implementation** (#69)
  - New files: `prefilter/teddy_slim_avx2_amd64.go`, `prefilter/teddy_slim_avx2_amd64.s`
  - Shift algorithm from Rust aho-corasick crate (single load + VPERM2I128/VPALIGNR)
  - 15 GB/s throughput in direct benchmarks (2x faster than SSSE3)
  - **Note**: Not enabled in integrated prefilter due to regression in high false-positive workloads (#74)

- **Public optimization documentation** - `docs/OPTIMIZATIONS.md` (#71)
  - Documents 6 optimizations that beat Rust regex
  - DO NOT REGRESS comments in critical source files
  - Benchmark verification procedures

### Fixed
- **Version pattern strategy selection** (#70)
  - Patterns like `\d+\.\d+\.\d+` now use `UseReverseInner` with "." as inner literal
  - Removed incorrect digit-lead special case in `strategy.go`
  - Performance: 3.2x slower → ~1.2x slower vs Rust

### Known Issues
- **AVX2 Slim Teddy regression in integrated prefilter** (#74)
  - Direct SIMD: AVX2 is 2x faster than SSSE3 (15 GB/s vs 9 GB/s)
  - Integrated with verification loop: AVX2 is 6x slower (640µs vs 106µs on 64KB)
  - Root cause: Per-call overhead with ~4000 false positive candidates
  - Workaround: Keep SSSE3 for integrated Teddy prefilter
  - AVX2 functions available for direct use in specialized scenarios

---

## [0.10.0] - 2026-01-07

### Added
- **Fat Teddy 16-bucket SIMD prefilter for 33-64 patterns**
  - New strategy tier: Slim Teddy (2-32 patterns) → Fat Teddy (33-64) → Aho-Corasick (>64)
  - AVX2 assembly implementation with 9+ GB/s throughput
  - 16 buckets (vs Slim Teddy's 8) = 2x pattern capacity
  - Pure Go scalar fallback for non-AVX2 platforms
  - Algorithm from Rust aho-corasick `generic.rs` Fat<V, 2> implementation

- **Aho-Corasick fallback for small haystacks with Fat Teddy**
  - Fat Teddy's AVX2 SIMD has setup overhead slower than Aho-Corasick on small inputs
  - Automatic fallback for haystacks < 64 bytes (threshold based on benchmarks)
  - 2.4x faster on 37-byte haystacks with 50 patterns (267ns → 110ns)
  - Follows Rust regex's `minimum_len()` approach (`builder.rs:585`)

### Technical Details
- **fatTeddyMasks struct**: 32-byte SIMD masks (256-bit AVX2 vectors)
  - Low lane (bytes 0-15): buckets 0-7
  - High lane (bytes 16-31): buckets 8-15
- **AVX2 algorithm**:
  - VBROADCASTI128: Load 16 bytes, duplicate to both lanes
  - VPSHUFB: Parallel nibble lookup in bucket masks
  - VPALIGNR $15: Half-shift for 2-byte fingerprint alignment
  - VPMOVMSKB: Extract 32-bit candidate mask

### Performance
| Patterns | Engine | Throughput | vs Aho-Corasick |
|----------|--------|------------|-----------------|
| 40 | Fat Teddy AVX2 | 9.1 GB/s | **73x faster** |
| 40 | Fat Teddy scalar | 228 MB/s | **1.5x faster** |
| 70 | Aho-Corasick | 152 MB/s | baseline |

### Files
- `prefilter/teddy_fat.go` - Fat Teddy core implementation + MinimumLen()
- `prefilter/teddy_fat_amd64.go` - AVX2 dispatch
- `prefilter/teddy_avx2_amd64.s` - AVX2 assembly (~300 lines)
- `meta/meta.go` - Aho-Corasick fallback for small haystacks
- `meta/strategy.go` - strategy selection update (32→Fat Teddy, >64→Aho-Corasick)
- `meta/fat_teddy_fallback_test.go` - tests for fallback logic

---

## [0.9.5] - 2026-01-06

### Changed
- **Teddy pattern limit expanded from 8 to 32** (#67)
  - Slim Teddy now handles up to 32 patterns (was 8)
  - Strategy threshold updated: Aho-Corasick triggers at >32 patterns (was >8)
  - Follows Rust aho-corasick architecture (Slim Teddy = 32, Fat Teddy = 64)

### Fixed
- **Literal extraction for factored prefixes** (#67)
  - Problem: `syntax.Parse` factors `(Wanderlust|Weltanschauung)` → `W(anderlust|eltanschauung)`
  - Extractor returned `["W" incomplete]` instead of full literals
  - Caused wrong strategy selection: UseReverseSuffixSet instead of UseTeddy
  - New function: `expandLiteralAlternate()` expands factored patterns back
  - Benchmark fix: 376µs → 1.7µs (**220x faster**)

---

## [0.9.4] - 2026-01-06

### Changed
- **Streaming state machine for CharClassSearcher** - single-pass FindAll/Count
  - New methods: `FindAllIndices()`, `Count()` with streaming state machine
  - Eliminates per-match function call overhead
  - Based on Rust regex approach: SEARCHING/MATCHING states
  - Integrated into public API: `FindAll()`, `FindAllIndex()` use streaming path

### Performance
- **CharClassFindAll: 15-30% faster** (1500ns → 1100-1400ns on 1KB)
- **char_class gap vs Rust**: reduced from 2.6x to ~1.9x
- No regressions on other patterns (+0.05% geomean)

---

## [0.9.3] - 2026-01-06

### Changed
- **Teddy 2-byte fingerprint** - reduced false positives by ~90%
  - Changed default from 1-byte to 2-byte fingerprint
  - New SSSE3 assembly: `teddySlimSSSE3_2` in `prefilter/teddy_ssse3_amd64.s`
  - 1-byte: ~25% false positive rate on typical text
  - 2-byte: <0.5% false positive rate

- **Strategy selection reorder** - DigitPrefilter prioritized for digit-lead patterns
  - Moved DigitPrefilter check before tiny NFA fallback
  - Added `isDigitLeadPattern()` helper to reject single-byte inner literals for digit patterns
  - Prevents high-frequency literals (like `.`) from being used as inner search targets

### Performance

| Pattern | v0.9.2 | v0.9.3 | Change |
|---------|--------|--------|--------|
| literal_alt | 31ms | 8ms | **+4x faster** |
| version | 8.2ms | 2ms | **+4x faster** |
| IP | 3.9ms | 5.5ms | -43% (trade-off) |

**Trade-off note**: IP pattern is 43% slower but remains **2.2x faster than Rust regex**.
See #62 for future IP optimization research.

---

## [0.9.2] - 2026-01-06

### Changed
- **Simplified DigitPrefilter** - removed adaptive switching overhead
  - Problem: Adaptive FP tracking added ~50ms overhead on large data
  - Solution: Remove runtime tracking, use NFA state limit instead
  - New constant: `digitPrefilterMaxNFAStates = 100` (simple patterns only)
  - Complex patterns (IP with 74 states) now use plain DFA strategy

### Performance
- **IP pattern: 146x faster** (731ms → 5ms on 6MB data)
- All other patterns: 1.2-2.1x faster (reduced overhead)
- No regressions on small data

| Pattern | v0.9.1 | v0.9.2 | Speedup |
|---------|--------|--------|---------|
| ip | 731ms | 5ms | **146x** |
| char_class | 183ms | 113ms | **1.6x** |
| literal_alt | 61ms | 29ms | **2.1x** |

---

## [0.9.1] - 2026-01-05

### Fixed
- **DigitPrefilter adaptive switching** for high false-positive scenarios
  - Problem: DigitPrefilter was slow on dense digit data (many consecutive FPs)
  - Solution: Runtime adaptive switching - after 64 consecutive false positives, switch to DFA
  - Based on Rust regex insight: "prefilter with high FP rate makes search slower"
  - Sparse data: prefilter remains fast (100-3000x speedup via SIMD skip)
  - Dense data: adaptively switches to lazy DFA (3-5x speedup vs stdlib)
  - New stat: `Stats.PrefilterAbandoned` tracks adaptive switching events
  - New constant: `digitPrefilterAdaptiveThreshold = 64`

### Performance (IP regex benchmarks)

| Scenario | stdlib | coregex | Speedup |
|----------|--------|---------|---------|
| Sparse 64KB | 833 µs | 2.8 µs | **300x** |
| Dense 64KB | 8.5 µs | 2.4 µs | **3.5x** |
| No IPs 1MB | 60.7 ms | 19.8 µs | **3000x** |

---

## [0.9.0] - 2026-01-05

### Added
- **UseAhoCorasick strategy** for large literal alternations (>8 patterns)
  - Integrates `github.com/coregx/ahocorasick` v0.1.0 library
  - Extends "literal engine bypass" optimization beyond Teddy's 8-pattern limit
  - O(n) multi-pattern matching with ~1.6 GB/s throughput
  - **75-113x faster** than stdlib on 15-20 pattern alternations
  - Zero allocations for `IsMatch()`

- **DigitPrefilter strategy** for IP regex patterns - PR #56 (Fixes #50)
  - New `UseDigitPrefilter` strategy for patterns that must start with digits
  - AVX2 SIMD digit scanner (`simd/memchr_digit_amd64.s`)
  - AST analysis to detect digit-start patterns (IP validation, phone numbers)
  - **2500x faster** than stdlib on no-match scenarios
  - **39-152x faster** on sparse IP data

- **Paired-byte SIMD search** for `simd.Memmem()` - PR #55
  - Byte frequency table for optimal rare byte selection (like Rust's memchr crate)
  - `SelectRareBytes()` finds two rarest bytes in needle
  - `MemchrPair()` searches for two bytes at specific offset simultaneously
  - AVX2 assembly implementation for AMD64
  - SWAR (SIMD Within A Register) fallback for non-AVX2 and other architectures
  - Dramatically reduces false positives vs single-byte search

---

## [0.8.24] - 2025-12-14

### Fixed
- **Longest() mode performance** - BoundedBacktracker now supports leftmost-longest matching (Fixes #52)
  - Root cause: BoundedBacktracker was disabled entirely in Longest() mode, forcing PikeVM fallback
  - Solution: Implemented `backtrackFindLongest()` that explores all branches at splits
  - Found during GoAWK integration testing

### Performance (Longest() mode)

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| coregex Longest() | 450 ns | 133 ns | **3.4x faster** |
| Longest() overhead | +270% | +8% | Target was +10% |
| vs stdlib Longest() | 2.4x slower | **1.37x faster** | — |

### Technical
- `nfa/backtrack.go`: Added `longest` field, `SetLongest()`, `backtrackFindLongest()`
- `meta/meta.go`: Enabled BoundedBacktracker for Longest() mode, call `SetLongest()` on backtracker

---

## [0.8.23] - 2025-12-13

### Fixed
- **Unicode char class bug** - CharClassSearcher incorrectly handled runes 128-255
  - `[föd]+` on "fööd" returned "f" instead of "fööd"
  - Root cause: check was `> 255` but runes like ö (code point 246) are multi-byte in UTF-8 (0xC3 0xB6)
  - Fix: changed to `> 127` to only allow true ASCII (0-127) for byte lookup table
  - Found during GoAWK integration testing
  - PR: #51

---

## [0.8.22] - 2025-12-13

### Added
- **Small string optimization** - BoundedBacktracker for NFA patterns (Issue #29)
  - Patterns like `j[a-z]+p` now **1.4x faster than stdlib** (was 2-4x slower)
  - `\w+`, `[a-z]+` patterns: **15-20x faster** via CharClassSearcher
  - Zero allocations for all `*String` methods (MatchString, FindString, etc.)

- **GoAWK benchmarks** - Added GoAWK patterns for regression testing
  - `j[a-z]+p`, `\w+`, `[a-z]+` in BenchmarkMatchString and BenchmarkIsMatch

### Changed
- **BoundedBacktracker** now auto-enabled for UseNFA strategy with small patterns (<50 states)
  - Uses O(1) generation-based visited reset vs PikeVM's thread queues
  - Only for patterns that cannot match empty (avoids greedy semantics bugs)

### Technical
- `regex.go`: Added `stringToBytes()` using `unsafe.Slice` (like Rust's `as_bytes()`)
- `meta/meta.go`: Added `canMatchEmpty` detection, prefilter in NFA path
- `meta/meta_test.go`: Added BenchmarkIsMatch with GoAWK patterns
- `regex_test.go`: Added GoAWK patterns to BenchmarkMatchString, BenchmarkFindIndex

### Performance (small strings ~44 bytes vs stdlib)

| Operation | Pattern | Result |
|-----------|---------|--------|
| MatchString | `j[a-z]+p` | **1.4x faster** |
| MatchString | `\w+` | **18x faster** |
| MatchString | `[a-z]+` | **15x faster** |
| FindStringIndex | `j[a-z]+p` | **1.45x faster** |
| Split | `f[a-z]x` | **1.75x faster** |

---

## [0.8.21] - 2025-12-13

### Added
- **CharClassSearcher** - Specialized 256-byte lookup table for simple char_class patterns (Fixes #44)
  - Patterns like `[\w]+`, `\d+`, `[a-z]+` now use O(1) byte membership test
  - **23x faster** than stdlib (623ms → 27ms on 6MB input with 1.3M matches)
  - **2x faster than Rust regex**! (57ms → 27ms)
  - Zero allocations in hot path

- **UseCharClassSearcher strategy**
  - Auto-selected for simple char_class patterns without capture groups
  - Patterns WITH captures (`(\w)+`) continue to use BoundedBacktracker

- **Zero-allocation Count()** method
  - Uses `FindIndicesAt()` instead of `Find()` to avoid Match object allocation
  - Critical for benchmarks comparing with Rust `find_iter().count()`

### Fixed
- **DFA ByteClasses compression** (Rust-style optimization)
  - Dynamic stride based on equivalence classes instead of fixed 256
  - Memory-efficient: only allocate transitions for actual alphabet size
  - Compile memory for `hello` pattern: **1195KB → 598KB** (2x reduction)

- **Removed unused reverseDFA field** from Engine
  - Was creating redundant reverse DFA for ALL patterns (2x memory overhead)
  - ReverseAnchoredSearcher and other searchers create their own DFA when needed

- **Reverse NFA ByteClasses registration**
  - Added `SetRange()` calls in `updateByteRangeState` and `updateSparseState`
  - Fixes incorrect ByteClasses for reverse DFA (all bytes mapped to single class)
  - Matches Rust's approach in `nfa.rs`

### Technical
- New files:
  - `nfa/charclass_searcher.go` - CharClassSearcher implementation
  - `nfa/charclass_searcher_test.go` - Unit tests and benchmarks
  - `nfa/charclass_extract.go` - Byte range extraction from AST
  - `nfa/charclass_extract_test.go` - Extraction tests
- Modified: `meta/strategy.go` - Added `UseCharClassSearcher` strategy
- Modified: `meta/meta.go` - Engine integration, zero-alloc Count(), removed unused reverseDFA
- Modified: `meta/strategy_test.go` - Strategy selection tests
- Modified: `dfa/lazy/state.go` - Dynamic stride for ByteClasses compression
- Modified: `dfa/lazy/builder.go` - ByteClasses-aware state construction
- Modified: `dfa/lazy/lazy.go` - ByteClasses lookup in transitions
- Modified: `nfa/reverse.go` - SetRange() calls for ByteClasses registration

### Performance Summary (char_class patterns)

| Pattern | Input | stdlib | coregex | Rust | coregex vs Rust |
|---------|-------|--------|---------|------|-----------------|
| `[\w]+` | 6MB, 1.3M matches | 623ms | **27ms** | 57ms | **2.1x faster** |

Compile memory improvements (ByteClasses compression):

| Pattern | Before | After | Improvement |
|---------|--------|-------|-------------|
| `hello` | 1195KB | 598KB | **-50%** |
| char_class runtime | 180ms | 109ms | **-39%** |

---

## [0.8.20] - 2025-12-12

### Added
- **ReverseSuffixSet Strategy** - Multi-suffix patterns with Teddy prefilter
  - New strategy for patterns like `.*\.(txt|log|md)` where LCS (Longest Common Suffix) is empty
  - Uses Teddy SIMD prefilter to find any of the suffix literals
  - Reverse DFA confirms prefix pattern matches
  - **Novel optimization NOT present in rust-regex** (they fall back to Core strategy)
  - Pattern `.*\.(txt|log|md)`: **34-385x faster** than stdlib (scales with input size)

- **Suffix extraction cross_reverse operation**
  - Implemented rust-regex's `cross_reverse` algorithm for OpConcat
  - Suffix extraction now correctly prepends preceding literals
  - `.*\.(txt|log|md)` extracts `[".txt", ".log", ".md"]` (was `["txt", "log", "md"]`)
  - Required for Teddy prefilter to find full suffix including `.`

### Technical
- New file: `meta/reverse_suffix_set.go` - ReverseSuffixSetSearcher implementation
- Modified: `meta/strategy.go` - Added `UseReverseSuffixSet` strategy selection
- Modified: `meta/meta.go` - Engine integration for new strategy
- Modified: `literal/extractor.go` - cross_reverse for OpConcat suffix extraction
- Modified: `literal/extractor_test.go` - Updated test expectations for cross_reverse
- Strategy requirements: 2-8 suffix literals, each >= 2 bytes, non-start-anchored

### Performance Summary
Suffix alternation patterns now dramatically faster:

| Pattern | Input | stdlib | coregex | Speedup |
|---------|-------|--------|---------|---------|
| `.*\.(txt\|log\|md)` | 1KB | 15.5µs | **454ns** | **34x faster** |
| `.*\.(txt\|log\|md)` | 32KB | 1.95ms | **5µs** | **384x faster** |
| `.*\.(txt\|log\|md)` | 1MB | 57ms | **147µs** | **385x faster** |

---

## [0.8.19] - 2025-12-12

### Added
- **FindAll ReverseSuffix optimization** (Fixes #41)
  - `FindIndicesAt()` now supports `UseReverseSuffix` strategy
  - Added `ReverseSuffixSearcher.FindAt()` and `FindIndicesAt()` methods
  - Pattern `.*@example\.com` with `FindAll`: **87x faster** than stdlib (316ms → 3.6ms on 6MB input)

### Changed
- **ReverseSuffix Find() optimization**
  - Use `bytes.LastIndex` for O(n) single-pass suffix search (was O(k*n) loop)
  - Added `matchStartZero` flag for unanchored patterns (`.*@suffix`)
  - For `.*` prefix patterns, match always starts at position 0 - skip reverse DFA entirely
  - Pattern `.*@example\.com` with `Find`: **0ms** (was 362ms)

### Performance Summary
Inner literal patterns (`.*keyword` or `.*@suffix`) now dramatically faster:

| Pattern | Operation | stdlib | coregex | Speedup |
|---------|-----------|--------|---------|---------|
| `.*@example\.com` | FindAll (6MB) | 316ms | **3.6ms** | **87x faster** |
| `.*@example\.com` | Find (6MB) | ~300ms | **<1ms** | **300x+ faster** |
| `error\|warning\|...` | FindAll (6MB) | 759ms | **51ms** | **15x faster** |

---

## [0.8.18] - 2025-12-12

### Added
- **Teddy multi-pattern prefilter for alternations**
  - `expandLiteralCharClass()` reverses regex parser optimization (`ba[rz]` → `["bar", "baz"]`)
  - Patterns like `(foo|bar|baz|qux)` now use Teddy SIMD prefilter
  - Alternation patterns: **242x faster** (was 24x slower)

- **UseTeddy strategy (literal engine bypass)**
  - Exact literal alternations like `(foo|bar|baz)` skip DFA construction entirely
  - Compile time: **10x faster** (109µs → 11µs)
  - Memory: **31x less** (598KB → 19KB)
  - Inspired by Rust regex's "literal engine bypass" optimization

- **ReverseSuffix.Find() optimization**
  - Last-suffix algorithm for greedy semantics (find LAST candidate, not iterate all)
  - Pattern `.*\.txt`: **1.8x faster** than stdlib on 32KB+ inputs

- **ReverseAnchored.Find() zero-allocation**
  - Use `SearchReverse` instead of `PikeVM + reverseBytes`
  - Anchor-end patterns: improved from 13x slower to 3x slower

### Changed
- **BoundedBacktracker generation counter**
  - O(1) visited tracking instead of O(n) array clear
  - 32KB input: **3x faster** than stdlib (was 10x slower)

- **`(a|b|c)+` pattern recognition**
  - `isSimpleCharClass()` now looks through capture groups
  - Go parser optimizes `(a|b|c)+` to `Plus(Capture(CharClass))`
  - Now uses BoundedBacktracker: **2.5x faster** than stdlib (was 1.8x slower)

- **Single-character inner literals enabled**
  - Rare characters like `@` in email patterns provide significant speedup
  - `UseReverseInner` strategy now accepts 1-byte inner literals
  - Email pattern `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`: **11-42x faster** than stdlib

### Performance Summary
All tested patterns now faster than Go stdlib:

| Pattern | Before | After | Improvement |
|---------|--------|-------|-------------|
| `(foo\|bar\|baz\|qux)` | 24x slower | **242x faster** | +5800x |
| `(a\|b\|c)+` | 1.8x slower | **2.5x faster** | +4.5x |
| `\d+` | 2x slower | **4.5x faster** | +9x |
| `.*\.txt` | 1.2x slower | **1.8x faster** | +2.2x |
| Email pattern | - | **11-42x faster** | via ReverseInner with `@` |

---

## [0.8.17] - 2025-12-12

### Added
- **BoundedBacktracker Engine** (PR #38)
  - Recursive backtracking engine with bit-vector visited tracking for O(1) lookup
  - Optimal for character class patterns (`\d+`, `\w+`, `[a-z]+`) without good literals
  - 2-5x faster than PikeVM for simple patterns, now **2.5x faster than stdlib** (was 2-3x slower)
  - Automatic strategy selection via `UseBoundedBacktracker` in meta-engine
  - Memory-bounded: max 256KB visited bit vector (falls back to PikeVM for larger inputs)

---

## [0.8.16] - 2025-12-11

### Added
- **Character class pattern optimization** (PR #37, Fixes #33)
  - Simple patterns like `[0-9]+`, `\d+`, `\w+` now use NFA directly
  - Skip DFA overhead when no prefilter benefit
  - Added `isSimpleCharClass()` detection in strategy selection

### Changed
- **ReplaceAll optimization** (PR #37, Fixes #34)
  - Pre-allocate result buffer (input + 25%)
  - Reuse `matchIndices` buffer across iterations (was allocating per match)

- **FindAll/FindAllIndex optimization** (PR #37, Fixes #35)
  - Use `FindIndicesAt()` instead of `FindAt()` (avoids Match object creation)
  - Lazy allocation - only allocate when first match found
  - Pre-allocate with estimated capacity (10 matches per 1KB)

### Performance
- Find/hello: **85% faster** (619ns → 88ns)
- OnePassIsMatch: **19% faster**
- LazyDFARepetition: **21% faster**

---

## [0.8.15] - 2025-12-11

### Added
- **Zero-allocation `IsMatch()`** (PR #36, Fixes #31)
  - `PikeVM.IsMatch()` returns immediately on first match without computing positions
  - 0 B/op, 0 allocs/op in hot path
  - Speedup vs stdlib: 52-1863x faster (depending on input size)

- **Zero-allocation `FindIndices()`** (PR #36, Fixes #32)
  - `Engine.FindIndices()` returns `(start, end int, found bool)` tuple
  - 0 B/op, 0 allocs/op - no Match object allocation
  - Used internally by `Find()` and `FindIndex()` public API

### Changed
- `Find()` and `FindIndex()` now use `FindIndices()` internally
- `isMatchNFA()` now uses optimized `PikeVM.IsMatch()` instead of `Search()`

---

## [0.8.14] - 2025-12-11

### Added
- **Literal fast path optimization** (PR #30, Fixes #29)
  - Add `LiteralLen()` method to Prefilter interface
  - For exact literal patterns, bypass PikeVM entirely
  - Simple literals (`hello`, `foo`) now **~2x faster** than stdlib
  - Was 5x slower before this fix
  - Thanks to @benhoyt for detailed performance analysis

---

## [0.8.13] - 2025-12-08

### Fixed
- **32-bit platform support**: Fixed build failure on GOARCH=386
  - `internal/conv.IntToUint32()` used `n > math.MaxUint32` which overflows on 32-bit
  - Changed to `uint(n) > math.MaxUint32` for portable comparison
  - Discovered via GoAWK CI testing on Linux 386

---

## [0.8.12] - 2025-12-08

### Added
- **Issue #26: `Longest()` method now works correctly (leftmost-longest semantics)**
  - Previously was a no-op stub with incorrect documentation
  - Now properly implements POSIX leftmost-longest matching
  - Alternations like `(a|ab)` on "ab" return "ab" (longest) instead of "a" (first)
  - Essential for AWK/POSIX compatibility
  - Files: `regex.go`, `meta/meta.go`, `nfa/pikevm.go`
  - No performance regression in default (leftmost-first) mode

### Fixed
- **Documentation**: Corrected misleading docs claiming coregex used leftmost-longest by default
  - coregex uses leftmost-first (Perl) by default, matching Go stdlib
  - `Longest()` switches to leftmost-longest (POSIX) semantics

---

## [0.8.11] - 2025-12-08

### Fixed
- **Issue #24: ReverseAnchored patterns return wrong result on first call**
  - Pattern `a$` on input "ab" returned `true` on first call, `false` on subsequent calls
  - Root cause: `determinize()` returned error for dead states, triggering incorrect NFA fallback
  - The fallback PikeVM was created from reverse NFA but received unreversed input
  - Fix: `determinize()` now returns `(nil, nil)` for dead states (not an error condition)
  - Also fixed: empty string handling and strategy selection for patterns with start anchors
  - Files: `dfa/lazy/lazy.go`, `meta/reverse_anchored.go`, `meta/strategy.go`, `nfa/compile.go`

---

## [0.8.10] - 2025-12-07

### Fixed
- **Issue #8: Inline flags `(?s:...)`, `(?i:...)` now work correctly**
  - `compileAnyChar()` was checking global config instead of trusting the Op type from parser
  - Now correctly produces `OpAnyChar` (matches newlines) vs `OpAnyCharNotNL` based on inline flags
  - Examples: `(?s:^a.*c$)` matches `"a\nb\nc"`, `a(?s:.)b` matches `"a\nb"`
  - AWK integration: wrap patterns with `(?s:...)` for AWK-like behavior where `.` matches newlines

---

## [0.8.9] - 2025-12-07

### Fixed
- **Alternation with empty-matchable branches (e.g., `abc|.*?`)**
  - Literal extractor incorrectly extracted "abc" as required prefix
  - When ANY alternation branch has no prefix requirement, the whole alternation has none
  - Fixes patterns like `abc|.*?` where `.*?` can match empty string
  - `literal/extractor.go`: Fixed `OpAlternate` handling in extractPrefixes, extractSuffixes, extractInner

- **findAdaptive returning incorrect match start position**
  - `meta/meta.go`: `findAdaptive` assumed start=0 when DFA succeeds
  - Now correctly calls PikeVM to get actual match bounds
  - Fixes patterns like `\b(DEBUG|INFO|WARN|ERROR)\b` returning wrong position

- **Multiline alternation with Look states (`(?m)(?:^|a)+`)**
  - Fixed priority handling when left branch is a Look assertion that would fail
  - `nfa/pikevm.go`: Check if Look assertion succeeds before incrementing priority

- **DFA greedy extension for optional groups (timestamp pattern)**
  - `dfa/lazy/lazy.go`: Added `freshStartStates` tracking for leftmost-longest semantics
  - Patterns with optional groups now correctly find longest match

### Known Limitations
- `.{3}` on Unicode strings like "абв" matches bytes, not codepoints (documented, test skipped)

---

## [0.8.8] - 2025-12-07

### Fixed
- **Issue #15: DFA.IsMatch returns false for patterns with capture groups**
  - `epsilonClosure` in DFA builder didn't follow `StateCapture` transitions
  - Capture states are semantically epsilon transitions (record position but don't consume input)
  - Patterns like `\w+@([[:alnum:]]+\.)+[[:alnum:]]+[[:blank:]]+` now work correctly
  - Discovered via GoAWK integration testing (`datanonl.awk` test)

### Technical Details
- `dfa/lazy/builder.go`: Added `StateCapture` handling in `epsilonClosure` and `resolveWordBoundaries`
- New test: `TestIssue15_CaptureGroupIsMatch` with comprehensive capture group patterns

---

## [0.8.7] - 2025-12-07

### Fixed
- **Error message format now matches stdlib exactly**
  - Was: `regexp: error parsing regexp: error parsing regexp: invalid escape...` (duplicate prefix)
  - Now: `error parsing regexp: invalid escape...` (same as stdlib)
  - `CompileError.Error()` now returns `*syntax.Error` message directly without extra wrapping
  - Tests updated to verify exact match with stdlib error messages

### Technical Details
- `meta/meta.go`: Fixed `CompileError.Error()` to use `errors.As` and return syntax errors directly
- `error_test.go`: Updated to compare error messages with stdlib exactly

---

## [0.8.6] - 2025-12-07

### Fixed
- **Issue #14: FindAllIndex returns incorrect matches for `^` anchor**
  - `FindAllIndex`/`ReplaceAll` returned matches at every position for `^` patterns
  - Example: `FindAllIndex("12345", "^")` returned `[[0 0] [1 1] [2 2]...]` instead of `[[0 0]]`
  - Root cause: `FindAllIndex` sliced the input `b[pos:]`, making the engine think each position was the start
  - **Professional fix**: Added `FindAt(haystack, at)` methods throughout the engine stack
    - `PikeVM.SearchAt()` and `SearchWithCapturesAt()` - starts unanchored search from position
    - `DFA.FindAt()` and `findWithPrefilterAt()` - preserves absolute positions
    - `Engine.FindAt()` and `FindSubmatchAt()` - meta-engine coordination
  - All `FindAll*` and `ReplaceAll*` functions now use `FindAt` to preserve absolute positions
  - Anchors like `^` now correctly check against the TRUE input start, not sliced position

### Technical Details
- `nfa/pikevm.go`: Added `SearchAt()`, `SearchWithCapturesAt()`, `searchUnanchoredAt()`, `searchUnanchoredWithCapturesAt()`
- `dfa/lazy/lazy.go`: Added `FindAt()`, `findWithPrefilterAt()`
- `meta/meta.go`: Added `FindAt()`, `FindSubmatchAt()`, strategy-specific `*At` methods
- `regex.go`: Updated `FindAll`, `FindAllIndex`, `ReplaceAll` to use `FindAt` variants
- New test file: `anchor_test.go` with comprehensive stdlib compatibility tests
- All tests passing, golangci-lint: 0 issues

---

## [0.8.5] - 2025-12-05

### Fixed
- **Issue #12: Word boundary assertions `\b` and `\B` not working correctly**
  - `FindString`/`Find` returned empty while `MatchString`/`IsMatch` worked
  - Root causes identified and fixed:
    1. `findWithPrefilter()` missing word boundary checks at EOI and before byte consumption
    2. `resolveWordBoundaries()` incorrectly expanding through epsilon/split for non-boundary patterns
    3. Reverse DFA strategies incompatible with word boundary semantics
  - **Professional fix** following Rust regex-automata approach:
    - Added `checkWordBoundaryMatch()` and `checkEOIMatch()` to `findWithPrefilter()`
    - Rewrote `resolveWordBoundaries()` to only expand through actual boundary crossings
    - Added `hasWordBoundary()` to disable reverse strategies for `\b`/`\B` patterns
  - All word boundary patterns now match stdlib behavior exactly

### Technical Details
- `meta/strategy.go`: New `hasWordBoundary()` function detects word boundary patterns
- `dfa/lazy/lazy.go`: `findWithPrefilter()` now handles word boundaries like `searchAt()`
- `dfa/lazy/builder.go`: `resolveWordBoundaries()` only expands states after crossing `\b`/`\B`
- Comprehensive test suite: `word_boundary_test.go` with stdlib comparison
- All tests passing including race detector (via WSL2)
- golangci-lint: 0 issues

---

## [0.8.4] - 2025-12-04

### Fixed
- **Bug #10: `^` anchor not working correctly in MatchString**
  - Patterns like `^abc` were incorrectly matching at any position (e.g., "xabc")
  - Root cause: DFA's `epsilonClosure` didn't handle `StateLook` assertions properly
  - **Professional fix** following Rust regex-automata approach:
    - New `LookSet` type for tracking satisfied look assertions (`dfa/lazy/look.go`)
    - `epsilonClosure` now accepts `lookHave LookSet` parameter
    - Different start states for different positions (StartText, StartWord, StartLineLF, etc.)
    - Multiline `^` support: `LookStartLine` satisfied after `\n`
  - Fixed prefilter bypass bug: don't use prefilter for start-anchored patterns
  - Resolves [#10](https://github.com/coregx/coregex/issues/10)
  - Found during GoAWK testing

### Changed
- DFA now correctly handles start-anchored patterns (no NFA fallback needed)
- Strategy selection no longer forces NFA for `^` patterns

### Technical Details
- `StateLook` transitions only followed when look assertion is satisfied
- `LookSetFromStartKind()` maps start positions to satisfied assertions
- `ComputeStartState()` uses look-aware epsilon closure
- All tests passing with race detector enabled
- golangci-lint: 0 issues

---

## [0.8.3] - 2025-12-04

### Fixed
- **Bug #6: Crash on negated character classes** like `[^,]*`, `[^\n]`
  - Large complement classes (e.g., `[^\n]` = 1.1M codepoints) now use efficient Sparse state representation
  - Prevents memory explosion and "character class too large" errors
  - Optimized range-based compilation for classes >256 runes
  - Found during GoAWK testing

- **Bug #7: Case-insensitive character class matching** `[oO]+d` didn't match "food"
  - `compileLiteral()` now respects `FoldCase` flag from `regexp/syntax` parser
  - ASCII letters create proper alternation between upper/lower variants
  - Fixes patterns like `[oO]`, `[aA][bB]`, etc.
  - Found during GoAWK testing

### Tests
- Added comprehensive test suite `nfa/compile_bug_test.go` (402 lines, 33 test cases)
- All tests passing with race detector enabled

### Maintenance
- Removed 21 unused linter directives (gosec, nestif)
- Code formatting cleanup
- golangci-lint: 0 issues

---

## [0.8.2] - 2025-12-03

### Fixed
- **Critical: Infinite loop in `onepass.Build()` for patterns like `(.*)`**
  - Bug: byte overflow when iterating ranges with hi=255 caused hang
  - Affected patterns: `(.*)`, `^(.*)$`, `([_a-zA-Z][_a-zA-Z0-9]*)=(.*)`
  - Found during GoAWK testing

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
v0.6.0 → ReverseSuffix optimization (DONE ✅)
v0.7.0 → OnePass DFA (DONE ✅)
v0.8.0 → ReverseInner strategy (DONE ✅)
v0.8.14-18 → GoAWK integration, Teddy, BoundedBacktracker (DONE ✅)
v0.8.19 → FindAll ReverseSuffix optimization (DONE ✅)
v0.8.20 → ReverseSuffixSet for multi-suffix patterns (DONE ✅)
v0.9.x → Performance tuning, Teddy 2-byte fingerprint (DONE ✅)
v0.10.0 → Fat Teddy 33-64 patterns, AVX2 SIMD (DONE ✅)
v0.11.0 → UseAnchoredLiteral, Issue #79 fix (DONE ✅) ← CURRENT
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

[Unreleased]: https://github.com/coregx/coregex/compare/v0.11.4...HEAD
[0.11.4]: https://github.com/coregx/coregex/releases/tag/v0.11.4
[0.11.3]: https://github.com/coregx/coregex/releases/tag/v0.11.3
[0.11.2]: https://github.com/coregx/coregex/releases/tag/v0.11.2
[0.11.1]: https://github.com/coregx/coregex/releases/tag/v0.11.1
[0.11.0]: https://github.com/coregx/coregex/releases/tag/v0.11.0
[0.10.10]: https://github.com/coregx/coregex/releases/tag/v0.10.10
[0.10.9]: https://github.com/coregx/coregex/releases/tag/v0.10.9
[0.10.8]: https://github.com/coregx/coregex/releases/tag/v0.10.8
[0.10.7]: https://github.com/coregx/coregex/releases/tag/v0.10.7
[0.10.6]: https://github.com/coregx/coregex/releases/tag/v0.10.6
[0.10.5]: https://github.com/coregx/coregex/releases/tag/v0.10.5
[0.10.4]: https://github.com/coregx/coregex/releases/tag/v0.10.4
[0.10.3]: https://github.com/coregx/coregex/releases/tag/v0.10.3
[0.10.2]: https://github.com/coregx/coregex/releases/tag/v0.10.2
[0.10.1]: https://github.com/coregx/coregex/releases/tag/v0.10.1
[0.10.0]: https://github.com/coregx/coregex/releases/tag/v0.10.0
[0.9.5]: https://github.com/coregx/coregex/releases/tag/v0.9.5
[0.9.4]: https://github.com/coregx/coregex/releases/tag/v0.9.4
[0.9.3]: https://github.com/coregx/coregex/releases/tag/v0.9.3
[0.9.2]: https://github.com/coregx/coregex/releases/tag/v0.9.2
[0.9.1]: https://github.com/coregx/coregex/releases/tag/v0.9.1
[0.9.0]: https://github.com/coregx/coregex/releases/tag/v0.9.0
[0.8.24]: https://github.com/coregx/coregex/releases/tag/v0.8.24
[0.8.23]: https://github.com/coregx/coregex/releases/tag/v0.8.23
[0.8.22]: https://github.com/coregx/coregex/releases/tag/v0.8.22
[0.8.21]: https://github.com/coregx/coregex/releases/tag/v0.8.21
[0.8.20]: https://github.com/coregx/coregex/releases/tag/v0.8.20
[0.8.19]: https://github.com/coregx/coregex/releases/tag/v0.8.19
[0.1.0]: https://github.com/coregx/coregex/releases/tag/v0.1.0
