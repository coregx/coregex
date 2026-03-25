# coregex - Development Roadmap

> **Strategic Focus**: Production-grade regex engine with RE2/rust-regex level optimizations

**Last Updated**: 2026-03-25 | **Current Version**: v0.12.19 | **Target**: v1.0.0 stable

---

## Vision

Build a **production-ready, high-performance regex engine** for Go that matches or exceeds RE2 and rust-regex performance through comprehensive optimizations.

### Current State vs Target

| Metric | Current (v0.12.19) | Target (v1.0.0) |
|--------|-------------------|-----------------|
| Inner literal speedup | **280-3154x** | ✅ Achieved |
| Case-insensitive speedup | **263x** | ✅ Achieved |
| Alternation speedup | **242x** | ✅ Achieved |
| Suffix alternation speedup | **34-385x** | ✅ Achieved |
| Small string perf | **1.4-20x faster** | ✅ Achieved |
| Reverse search | **Yes (4 strategies)** | ✅ Achieved |
| OnePass DFA | **Yes** | ✅ Achieved |
| Slim Teddy (2-32 patterns) | **Yes (SSSE3, 9GB/s)** | ✅ Achieved |
| Fat Teddy (33-64 patterns) | **Yes (AVX2, 9GB/s)** | ✅ Achieved |
| Aho-Corasick (>64 patterns) | **Yes (AC DFA prefilter)** | ✅ Achieved |
| Per-goroutine DFA cache | **Yes (Rust approach)** | ✅ Achieved |
| BoundedBacktracker | **Yes** | ✅ Achieved |
| CharClassSearcher | **Yes (35% faster than Rust!)** | ✅ Achieved |
| **Patterns faster than Rust** | **5 patterns** | ✅ Achieved |
| Test coverage 80%+ | **Yes (all packages ≥80%)** | ✅ Achieved |
| ARM NEON SIMD | No | Planned |
| Look-around | No | Planned |

---

## Release Strategy

```
v0.10.0 ✅ → Fat Teddy 33-64 patterns (AVX2, 9GB/s)
         ↓
v0.10.1-4 ✅ → Thread-safety, version pattern fixes
         ↓
v0.10.5 ✅ → CompositeSearcher backtracking fix (#81)
         ↓
v0.10.6 ✅ → CompositeSequenceDFA (5x for overlapping patterns), FindAllIndexCompact API
         ↓
v0.10.7 ✅ → UTF-8 fixes + 100% stdlib API compatibility
         ↓
v0.10.8-10 ✅ → FindAll perf fix, ReverseSuffix improvements
         ↓
v0.11.0 ✅ → UseAnchoredLiteral 32-133x speedup (#79), ASCII runtime detection
         ↓
v0.11.1 ✅ → UseMultilineReverseSuffix 3.5-5.7x speedup (#97)
         ↓
v0.11.2 ✅ → DFA verification (#99)
         ↓
v0.11.3 ✅ → Prefix fast path 319-552x speedup (#99)
         ↓
v0.12.0 ✅ → CompositeSearcher integration (#72)
         ↓
v0.12.1 ✅ → DFA bidirectional fallback, digit-run skip, bounded repetitions fix (#115)
         ↓
v0.12.2 ✅ → ReverseSuffixSet safety guard, matchStartZero fix (#116)
         ↓
v0.12.3 ✅ → Cross-product literal expansion, 110x speedup on regexdna (#119)
         ↓
v0.12.4 ✅ → Test coverage 80%+, CI improvements, awesome-go readiness
         ↓
v0.12.5 ✅ → Non-greedy quantifier fix, ReverseSuffix correctness (#124)
         ↓
v0.12.6 ✅ → BoundedBacktracker span-based CanHandle, ReplaceAllStringFunc O(n) (#127)
         ↓
v0.12.7 ✅ → PikeVM sparse-dispatch for dot patterns, 2.8-4.8x speedup (#132)
         ↓
v0.12.8 ✅ → Streaming ReplaceAll + DFA-first FindSubmatchAt (#135)
         ↓
v0.12.9 ✅ → Bidirectional DFA, Teddy/reverse NFA fixes
         ↓
v0.12.10 ✅ → Case-insensitive literals, DigitPrefilter O(n²) fix (#137)
         ↓
v0.12.11 ✅ → ReverseSuffix multi-wildcard, COREGEX_DEBUG logging
         ↓
v0.12.12 ✅ → Prefix trimming for case-fold literals (Teddy instead of AC)
         ↓
v0.12.13 ✅ → FatTeddy fix, prefilter acceleration, AC v0.2.1
         ↓
v0.12.14 ✅ → Concurrent safety fix for isMatchDFA prefilter (#137)
         ↓
v0.12.15 ✅ → Per-goroutine DFA cache, word boundary 30%→0.3% CPU, AC prefilter
         ↓
v0.12.16 ✅ → WrapLineAnchor for (?m)^ patterns
         ↓
v0.12.17 ✅ → Fix LogParser ARM64 regression, restore DFA/Teddy for (?m)^
         ↓
v0.12.18 ✅ → Flat DFA transition table, integrated prefilter, PikeVM skip-ahead
         ↓
v0.12.19 ✅ → Zero-alloc FindSubmatch, byte-based DFA cache, Rust-aligned visited limits
         ↓
v0.12.20 (Current) → Premultiplied/tagged StateIDs, break-at-match DFA determinize,
                      Phase 3 elimination (2-pass bidirectional DFA)
         ↓
v1.0.0-rc → Feature freeze, API locked
         ↓
v1.0.0 STABLE → Production release with API stability guarantee
```

### Completed Milestones

- ✅ **v0.1.0**: Multi-engine architecture, SIMD primitives
- ✅ **v0.2.0**: Capture groups support
- ✅ **v0.3.0**: Replace/Split functions
- ✅ **v0.4.0**: Core Optimizations, ReverseAnchored
- ✅ **v0.5.0**: Named captures
- ✅ **v0.6.0**: ReverseSuffix optimization (1000x+ for `.*\.txt`)
- ✅ **v0.7.0**: OnePass DFA (10x faster captures)
- ✅ **v0.8.0**: ReverseInner (3000x+ for `.*keyword.*`)
- ✅ **v0.8.14-18**: GoAWK integration fixes, Teddy prefilter, BoundedBacktracker
- ✅ **v0.8.19**: FindAll ReverseSuffix optimization (87x faster)
- ✅ **v0.8.20**: ReverseSuffixSet for multi-suffix patterns (34-385x faster)
- ✅ **v0.8.21**: CharClassSearcher (23x faster, 2x faster than Rust!)
- ✅ **v0.8.22**: Small string optimization (1.4-20x faster on ~44B inputs)
- ✅ **v0.9.x**: DigitPrefilter, Aho-Corasick integration, Teddy 2-byte fingerprint
- ✅ **v0.10.0**: Fat Teddy 16-bucket SIMD (33-64 patterns, 9+ GB/s), **5 patterns faster than Rust!**
- ✅ **v0.11.0**: UseAnchoredLiteral strategy (32-133x speedup), Issue #79 resolved
- ✅ **v0.11.1**: UseMultilineReverseSuffix strategy (3.5-5.7x speedup), Issue #97 resolved
- ✅ **v0.11.2**: DFA verification for multiline patterns, Issue #99
- ✅ **v0.11.3**: Prefix fast path (319-552x speedup), Issue #99 resolved
- ✅ **v0.12.0**: CompositeSearcher integration (#72)
- ✅ **v0.12.1**: DFA bidirectional fallback, digit-run skip, bounded repetitions fix (#115)
- ✅ **v0.12.2**: ReverseSuffixSet safety guard, matchStartZero fix (#116)
- ✅ **v0.12.3**: Cross-product literal expansion for regexdna patterns, 110x speedup (#119)
- ✅ **v0.12.4**: Test coverage 80%+, CI improvements, awesome-go readiness (#123)
- ✅ **v0.12.5**: Non-greedy quantifier fix, ReverseSuffix forward verification (#124)
- ✅ **v0.12.6**: BoundedBacktracker span-based CanHandle, ReplaceAllStringFunc O(n) (#127)
- ✅ **v0.12.7**: PikeVM sparse-dispatch for `.` patterns, 2.8-4.8x speedup (#132)
- ✅ **v0.12.8**: Streaming ReplaceAll + DFA-first FindSubmatchAt, Rust-style two-phase search (#135)
- ✅ **v0.12.9**: Bidirectional DFA, Teddy/reverse NFA fixes
- ✅ **v0.12.10**: Case-insensitive literals, DigitPrefilter O(n²) fix (#137)
- ✅ **v0.12.11**: ReverseSuffix multi-wildcard, COREGEX_DEBUG logging
- ✅ **v0.12.12**: Prefix trimming for case-fold literals
- ✅ **v0.12.13**: FatTeddy fix (ANDL→ORL, VPTEST), prefilter acceleration, AC v0.2.1
- ✅ **v0.12.14**: Concurrent safety fix for isMatchDFA prefilter (#137)
- ✅ **v0.12.15**: Per-goroutine DFA cache (Rust approach), word boundary 30%→0.3% CPU, 7 correctness fixes
- ✅ **v0.12.16**: WrapLineAnchor for (?m)^ patterns
- ✅ **v0.12.17**: Fix LogParser ARM64 regression — restore DFA/Teddy for (?m)^, partial prefilter
- ✅ **v0.12.18**: Flat DFA transition table (Rust approach), integrated prefilter skip-ahead in DFA+PikeVM, 4x unrolling — **35% faster than v0.12.14, 3x from Rust**

---

## v0.4.0 - Core Optimizations (HIGH PRIORITY)

**Goal**: Implement foundational optimizations from RE2/rust-regex

### Phase 1: Quick Wins

| ID | Feature | Impact | Complexity | Status |
|----|---------|--------|------------|--------|
| OPT-001 | Start State Caching (6 configs) | 5-20% + correctness | LOW | Planned |
| OPT-002 | Prefilter Effectiveness Tracking | Catastrophic slowdown prevention | LOW | Planned |
| OPT-003 | Early Match Termination | 2-10x for IsMatch() | LOW | Planned |

### Phase 2: Core Engine

| ID | Feature | Impact | Complexity | Status |
|----|---------|--------|------------|--------|
| OPT-004 | State Acceleration | 5-20x on loop states | MEDIUM | Planned |
| OPT-005 | ByteClasses | 4-8x memory reduction | MEDIUM | Planned |
| OPT-006 | Specialized Search Functions | 10-30% less branching | MEDIUM | Planned |

**Target**: 4-6 weeks

---

## v0.5.0 - Advanced Strategies (HIGH PRIORITY)

**Goal**: Implement reverse search strategies for 10-100x gains on suffix/inner patterns

| ID | Feature | Impact | Complexity | Status |
|----|---------|--------|------------|--------|
| OPT-007 | Reverse NFA/DFA Construction | Prerequisite | MEDIUM | Planned |
| OPT-008 | ReverseAnchored Strategy | 10-100x for `.*$` | MEDIUM | Planned |
| OPT-009 | ReverseSuffix Strategy | 10-100x for `.*\.txt` | MEDIUM | Planned |
| OPT-010 | ReverseInner Strategy | 10-100x for `prefix.*keyword.*suffix` | HIGH | Planned |

**Target**: 4-6 weeks

---

## v0.6.0 - Features & Polish (MEDIUM PRIORITY)

**Goal**: Complete feature set and secondary optimizations

| ID | Feature | Impact | Complexity | Status |
|----|---------|--------|------------|--------|
| FEAT-001 | Named Capture Groups | API completeness | MEDIUM | Planned |
| OPT-011 | OnePass DFA | 2-5x for simple patterns | HIGH | Planned |
| OPT-012 | Aho-Corasick Integration | Large multi-pattern | LOW | Planned |
| OPT-013 | Memory Layout Optimization | 5-15% cache efficiency | MEDIUM | Planned |

**Target**: 4 weeks

---

## v0.7.0 - Platform & Unicode (MEDIUM PRIORITY)

**Goal**: Cross-platform SIMD and Unicode optimizations

| ID | Feature | Impact | Complexity | Status |
|----|---------|--------|------------|--------|
| PLAT-001 | ARM NEON SIMD | Apple Silicon, ARM servers | HIGH | Planned |
| OPT-014 | UTF-8 Automata Optimization | Unicode performance | HIGH | **Partial** (v0.10.7) |

**Target**: 4-6 weeks

---

## v1.0.0 - Production Ready

**Requirements**:
- [x] All v0.4.0-v0.7.0 optimizations complete
- [ ] API stability guarantee
- [ ] Comprehensive documentation
- [ ] Performance regression tests
- [ ] Security audit
- [x] 80%+ test coverage (all packages ≥80%, meets awesome-go requirements)

**Guarantees**:
- API stability (no breaking changes in v1.x.x)
- Semantic versioning
- Long-term support

**Target**: Q2 2026

---

## Feature Comparison Matrix

| Feature | RE2 | rust-regex | coregex v0.12.8 | coregex v1.0 |
|---------|-----|------------|-----------------|--------------|
| Lazy DFA | ✅ | ✅ | ✅ | ✅ |
| Thompson NFA | ✅ | ✅ | ✅ | ✅ |
| PikeVM | ✅ | ✅ | ✅ | ✅ |
| Slim Teddy (≤32) | ❌ | ✅ | ✅ | ✅ |
| Fat Teddy (33-64) | ❌ | ✅ | ✅ | ✅ |
| Start State Cache | 8 | 6 | 6 | ✅ |
| Reverse Search | ✅ | ✅ (3) | ✅ (4) | ✅ |
| ReverseSuffixSet | ❌ | ❌ | ✅ | ✅ |
| OnePass DFA | ✅ | ✅ | ✅ | ✅ |
| BoundedBacktracker | ✅ | ✅ | ✅ | ✅ |
| Named Captures | ✅ | ✅ | ✅ | ✅ |
| Prefilter Tracking | ✅ | ✅ | ✅ | ✅ |
| Aho-Corasick | ❌ | ✅ | ✅ | ✅ |
| ARM NEON | ❌ | ✅ | ❌ | Planned |
| Look-around | ✅ | ❌ | ❌ | Planned |

---

## Performance Targets

### Current (v0.12.6) — AMD EPYC, 6MB input ✅ ACHIEVED

Cross-language benchmarks via [regex-bench](https://github.com/kolkov/regex-bench):

| Pattern | Go stdlib | coregex | Rust regex | vs stdlib | vs Rust |
|---------|-----------|---------|------------|-----------|---------|
| Literal alternation | 483 ms | 4.6 ms | 0.6 ms | **104x** | 7.8x slower |
| Multi-literal | 1401 ms | 12.7 ms | 4.6 ms | **110x** | 2.7x slower |
| Inner `.*keyword.*` | 232 ms | 0.25 ms | 0.28 ms | **926x** | **1.1x faster** |
| Suffix `.*\.txt` | 234 ms | 0.88 ms | 1.07 ms | **266x** | **1.2x faster** |
| Multiline `(?m)^/.*\.php` | 103 ms | 0.65 ms | 0.66 ms | **159x** | **~parity** |
| Email validation | 261 ms | 0.58 ms | 0.21 ms | **449x** | 2.7x slower |
| URL extraction | 258 ms | 0.63 ms | 0.34 ms | **409x** | 1.8x slower |
| IP address | 495 ms | 2.2 ms | 12.0 ms | **230x** | **5.5x faster** |
| Char class `[\w]+` | 525 ms | 40.7 ms | 50.3 ms | **12x** | **1.2x faster** |
| Alpha+digit | 261 ms | 25.7 ms | 11.9 ms | **10x** | 2.1x slower |
| Word+digit | 271 ms | 26.2 ms | 12.0 ms | **10x** | 2.1x slower |
| Word repeat `(\w{2,8})+` | 659 ms | 187 ms | 48.3 ms | **3.5x** | 3.8x slower |
| HTTP methods | 106 ms | 0.90 ms | 0.70 ms | **117x** | 1.2x slower |
| Anchored PHP | 0.00 ms | 0.01 ms | 0.01 ms | ~1x | ~parity |
| Multiline PHP | 103 ms | 0.65 ms | 0.66 ms | **159x** | **~parity** |

**5 patterns faster than Rust**: inner_literal, suffix, IP, char_class, multiline_php.

### Remaining for v1.0.0

| Feature | Status | Priority |
|---------|--------|----------|
| Close Teddy gap vs Rust (7.8x) | Blocked on Go 1.26 archsimd | High |
| ARM NEON SIMD | Planned | Medium |
| Look-around assertions | Planned | Medium |
| API stability guarantee | Required | High |

---

## Research Documentation

All optimization research is documented:

| Document | Content |
|----------|---------|
| `docs/dev/research/RE2_SMALL_INPUT_OPTIMIZATION_ANALYSIS.md` | RE2 thresholds and strategies |
| `docs/dev/research/RUST_REGEX_SMALL_INPUT_OPTIMIZATION_ANALYSIS.md` | rust-regex analysis |
| `docs/dev/research/OPTIMIZATION_OPPORTUNITIES.md` | Comprehensive gap analysis with code examples |

Reference implementations available locally:
- `docs/dev/reference/re2/` - RE2 source code
- `docs/dev/reference/rust-regex/` - rust-regex source code

---

## v0.11.3 - Prefix Verification Fast Path (Current) ✅

**Goal**: Rust-level performance for multiline patterns (Issue #99)

| Pattern | stdlib | coregex | Speedup |
|---------|--------|---------|---------|
| `(?m)^/.*\.php` IsMatch (1KB) | 100 µs | 182 ns | **552x** |
| `(?m)^/.*\.php` Find (1KB) | 81 µs | 240 ns | **338x** |
| `(?m)^/.*\.php` CountAll | 18.7 ms | 58 µs | **319x** |

**Completed**:
- [x] Prefix literal extraction and O(1) byte verification
- [x] SIMD backward scan with `bytes.LastIndexByte`
- [x] Skip-to-next-line optimization (avoids O(n²) worst case)
- [x] DFA fallback for complex patterns without extractable prefix

---

## v0.11.1 - UseMultilineReverseSuffix ✅

**Goal**: Line-aware suffix search for multiline patterns (Issue #97)

**Completed**:
- [x] `UseMultilineReverseSuffix` strategy (18th strategy)
- [x] `meta/reverse_suffix_multiline.go` implementation
- [x] Line-boundary detection algorithm
- [x] Superseded by v0.11.3 prefix fast path optimization

---

## v0.11.0 - UseAnchoredLiteral ✅

**Goal**: O(1) matching for `^prefix.*suffix$` patterns (Issue #79)

| Pattern | stdlib | coregex | Speedup |
|---------|--------|---------|---------|
| `^/.*[\w-]+\.php$` (short) | 241 ns | 7.6 ns | **32x** |
| `^/.*[\w-]+\.php$` (long) | 516 ns | 7.9 ns | **65x** |
| `^/.*[\w-]+\.php$` (no match) | 590 ns | 4.4 ns | **133x** |

**Completed**:
- [x] `UseAnchoredLiteral` strategy
- [x] `meta/anchored_literal.go` implementation (350 lines)
- [x] V11-002 ASCII runtime detection optimization
- [x] meta.go refactoring (2821 lines → 6 focused files)

---

## v0.12.0 - CompositeSearcher (Next)

**Goal**: Optimize concatenated character class patterns

| Issue | Pattern | Current | Target | Improvement |
|-------|---------|---------|--------|-------------|
| [#72](https://github.com/coregx/coregex/issues/72) | `\w+\s+\w+` | 691 ns/op | 131 ns/op | **5.3x faster** |

**Key tasks**:
- [ ] `UseCompositeSearcher` strategy
- [ ] `meta/composite_searcher.go` implementation
- [ ] Strategy selection integration

**Reference**: uawk implementation (MIT licensed)

### Completed in v0.10.1
- [x] AVX2 Slim Teddy implementation (not enabled in integrated prefilter, see #74) — #69
- [ ] AVX2 Slim Teddy integration (blocked by high false-positive regression) — #74
- [x] Version pattern uses ReverseInner — #70
- [x] Document optimizations beating Rust — #71

---

## Out of Scope

**Not planned**:
- Backtracking engines (catastrophic backtracking risk)
- PCRE/.NET regex flavors
- Regex visualization
- Code generation to native

---

## Release History

| Version | Date | Type | Key Changes |
|---------|------|------|-------------|
| **v0.12.8** | 2026-03-10 | Performance | **Streaming ReplaceAll + DFA-first FindSubmatchAt (#135)** |
| v0.12.8 | 2026-03-10 | Performance | PikeVM sparse-dispatch for `.` patterns, 2.8-4.8x speedup (#132) |
| v0.12.6 | 2026-03-08 | Fix | BoundedBacktracker span-based CanHandle, ReplaceAllStringFunc O(n) (#127) |
| v0.12.5 | 2026-03-08 | Fix | Non-greedy quantifier fix, ReverseSuffix correctness (#124) |
| v0.12.4 | 2026-03-01 | Test | Test coverage 80%+, CI improvements, awesome-go readiness |
| **v0.12.3** | 2026-02-16 | Performance | **Cross-product literal expansion, 110x regexdna speedup (#119)** |
| v0.12.2 | 2026-02-16 | Fix | ReverseSuffixSet safety guard, matchStartZero fix (#116) |
| v0.12.1 | 2026-02-15 | Performance | DFA bidirectional fallback, digit-run skip, bounded repetitions (#115) |
| v0.12.0 | 2026-02-07 | Feature | CompositeSearcher integration (#72) |
| **v0.11.3** | 2026-01-16 | Performance | **Prefix fast path 319-552x speedup (#99)** |
| v0.11.2 | 2026-01-16 | Performance | DFA verification for multiline (#99) |
| v0.11.1 | 2026-01-16 | Feature | UseMultilineReverseSuffix 3.5-5.7x speedup (#97) |
| v0.11.0 | 2026-01-15 | Feature | UseAnchoredLiteral 32-133x speedup (#79), ASCII runtime detection |
| v0.10.10 | 2026-01-15 | Fix | ReverseSuffix CharClass Plus whitelist |
| v0.10.9 | 2026-01-15 | Feature | UTF-8 suffix sharing, anchored suffix prefilter |
| v0.10.8 | 2026-01-15 | Performance | FindAll 600x faster for anchored patterns (#92) |
| v0.10.7 | 2026-01-15 | Feature | UTF-8 fixes + 100% stdlib API compatibility |
| v0.10.6 | 2026-01-14 | Feature | CompositeSequenceDFA (5x overlapping patterns), FindAllIndexCompact API |
| v0.10.5 | 2026-01-14 | Fix | CompositeSearcher backtracking for overlapping char classes (#81) |
| v0.10.4 | 2026-01-14 | Fix | Thread-safety for concurrent Regexp usage (#78) |
| v0.10.3 | 2026-01-08 | Fix | FindStringSubmatch capture groups fix (#77) |
| v0.10.2 | 2026-01-07 | Fix | Version pattern regression hotfix (#75) |
| v0.10.1 | 2026-01-07 | Fix | Version pattern ReverseInner (#70), optimization docs (#71) |
| **v0.10.0** | 2026-01-07 | Feature | **Fat Teddy AVX2, 5 patterns faster than Rust!** |
| v0.9.5 | 2026-01-06 | Fix | Teddy limit 8→32, literal extraction fix |
| v0.9.0-v0.9.4 | 2026-01-05 | Performance | DigitPrefilter, Aho-Corasick, 2-byte fingerprint |
| v0.8.20 | 2025-12-12 | Performance | ReverseSuffixSet (34-385x faster) |
| v0.8.19 | 2025-12-12 | Performance | FindAll ReverseSuffix (87x faster) |
| v0.8.18 | 2025-12-12 | Performance | Teddy prefilter for alternations (242x faster) |
| v0.8.17 | 2025-12-12 | Feature | BoundedBacktracker engine |
| v0.8.14-16 | 2025-12-11 | Fixes | GoAWK integration, literal fast path |
| v0.8.0 | 2025-11-29 | Performance | ReverseInner (3000x+ speedup) |
| v0.7.0 | 2025-11-28 | Feature | OnePass DFA |
| v0.6.0 | 2025-11-28 | Performance | ReverseSuffix (1000x+ speedup) |
| v0.5.0 | 2025-11-28 | Feature | Named captures |
| v0.4.0 | 2025-11-28 | Performance | ReverseAnchored, Core optimizations |
| v0.3.0 | 2025-11-27 | Feature | Replace/Split functions |
| v0.2.0 | 2025-11-27 | Feature | Capture groups |
| v0.1.0 | 2025-01-26 | Initial | Multi-engine architecture |

---

*Current: v0.12.8 | Target: v1.0.0*
