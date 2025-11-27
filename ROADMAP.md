# coregex - Development Roadmap

> **Strategic Focus**: Production-grade regex engine with RE2/rust-regex level optimizations

**Last Updated**: 2025-11-27 | **Current Version**: v0.3.0 | **Target**: v1.0.0 stable

---

## Vision

Build a **production-ready, high-performance regex engine** for Go that matches or exceeds RE2 and rust-regex performance through comprehensive optimizations.

### Current State vs Target

| Metric | Current (v0.3.0) | Target (v1.0.0) |
|--------|------------------|-----------------|
| Case-insensitive speedup | **263x** | 263x+ |
| Small input overhead | ~40ns | <15ns |
| Start state configs | 1 | 6 |
| State acceleration | No | Yes |
| Reverse search | No | Yes (3 strategies) |
| Prefilter tracking | No | Yes |
| OnePass DFA | No | Yes |

---

## Release Strategy

```
v0.3.0 (Current) ✅ → Replace/Split + Research complete
         ↓
v0.4.0 (Next) → Core Optimizations (6 tasks)
         ↓
v0.5.0 → Advanced Strategies (Reverse Search)
         ↓
v0.6.0 → Features & Polish (Named captures, OnePass)
         ↓
v0.7.0 → Platform & Unicode (ARM NEON, UTF-8)
         ↓
v1.0.0-rc → Feature freeze, API locked
         ↓
v1.0.0 STABLE → Production release with API stability guarantee
```

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
| OPT-014 | UTF-8 Automata Optimization | Unicode performance | HIGH | Planned |

**Target**: 4-6 weeks

---

## v1.0.0 - Production Ready

**Requirements**:
- [ ] All v0.4.0-v0.7.0 optimizations complete
- [ ] API stability guarantee
- [ ] Comprehensive documentation
- [ ] Performance regression tests
- [ ] Security audit
- [ ] 90%+ test coverage

**Guarantees**:
- API stability (no breaking changes in v1.x.x)
- Semantic versioning
- Long-term support

**Target**: Q2 2026

---

## Feature Comparison Matrix

| Feature | RE2 | rust-regex | coregex v0.3 | coregex v1.0 |
|---------|-----|------------|--------------|--------------|
| Lazy DFA | ✅ | ✅ | ✅ | ✅ |
| Thompson NFA | ✅ | ✅ | ✅ | ✅ |
| PikeVM | ✅ | ✅ | ✅ | ✅ |
| Teddy SIMD | ❌ | ✅ | ✅ | ✅ |
| Start State Cache | 8 | 6 | **1** | 6 |
| State Acceleration | ✅ | ✅ | ❌ | ✅ |
| Reverse Search | ✅ | ✅ (3) | ❌ | ✅ (3) |
| Prefilter Tracking | ✅ | ✅ | ❌ | ✅ |
| ByteClasses | ✅ | ✅ | ❌ | ✅ |
| OnePass DFA | ✅ | ✅ | ❌ | ✅ |
| Specialized Search | 8 | Trait | **1** | 8 |
| Aho-Corasick | ❌ | ✅ | ❌ | ✅ |
| Named Captures | ✅ | ✅ | ❌ | ✅ |
| ARM NEON | ❌ | ✅ | ❌ | ✅ |

---

## Performance Targets

### Current (v0.3.0)

| Pattern Type | stdlib | coregex | Speedup |
|--------------|--------|---------|---------|
| Case-sensitive 32KB | 9,715 ns | 8,367 ns | 1.2x |
| Case-insensitive 32KB | 1,229,521 ns | 4,669 ns | **263x** |
| Small input (16B) | 7 ns | 40 ns | **0.2x** (slower) |

### Target (v1.0.0)

| Pattern Type | Target Speedup | Optimization Required |
|--------------|----------------|----------------------|
| Case-sensitive | 2-5x | State acceleration, specialized search |
| Case-insensitive | 200-300x | Current level maintained |
| Small input | 1-2x | Start state cache, early termination |
| Suffix patterns | 10-100x | Reverse search strategies |
| Inner literal patterns | 10-100x | ReverseInner strategy |

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
| v0.3.0 | 2025-11-27 | Feature | Replace/Split, $0-$9 expansion |
| v0.2.0 | 2025-11-27 | Feature | Capture groups |
| v0.1.x | 2025-11-27 | Fixes | DFA cache, strategy selection, O(n²) |
| v0.1.0 | 2025-01-26 | Initial | Multi-engine architecture |

---

*Current: v0.3.0 | Next: v0.4.0 (Core Optimizations) | Target: v1.0.0*
