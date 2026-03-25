# coregex Architecture

Production-grade regex engine for Go achieving 3-3000x speedup over stdlib
through multi-engine architecture and SIMD optimizations.

## Execution Pipeline

```
Pattern → Parse → NFA Compile → Literal Extract → Strategy Select
                                                        ↓
                                    ┌──────────────────────────────────────┐
                                    │ Strategy (one of 15):                │
                                    │  UseNFA, UseDFA, UseBoth,            │
                                    │  UseReverseAnchored, UseReverseSuffix│
                                    │  UseOnePass, UseReverseInner,        │
                                    │  UseBoundedBacktracker, UseTeddy,    │
                                    │  UseReverseSuffixSet, UseCharClass,  │
                                    │  UseDigitPrefilter, UseAhoCorasick,  │
                                    │  UseComposite, UseAnchoredLiteral    │
                                    └──────────────────────────────────────┘
                                                        ↓
Input → Prefilter (memchr/memmem/teddy) → Engine Search → Match Result
```

## Engine Architecture (Rust-aligned)

### DFA Layer (`dfa/lazy/`)

- **Lazy DFA**: On-demand state construction with byte class compression
- **Flat transition table**: `flatTrans[sid+class]` — premultiplied offset, no multiply
- **Tagged State IDs**: match/dead/invalid encoded in high bits, single `IsTagged()` branch
- **Break-at-match**: Rust `determinize::next` (mod.rs:284) — stops NFA iteration at Match state,
  preventing prefix restarts while preserving greedy continuation (leftmost-first semantics)
- **Epsilon closure ordering**: Add-on-pop DFS with reverse Split push — matches Rust sparse set
  insertion order. Incremental per-target closure preserves Match-before-prefix ordering
- **2-pass bidirectional search**: Forward DFA → match end, reverse DFA → match start (no Phase 3)
- **Byte-based cache limit**: 2MB default (matches Rust `hybrid_cache_capacity`)
- **Cache clearing**: Up to 5 clears before NFA fallback (Rust approach)
- **Acceleration**: Detects self-loop states, uses SIMD memchr for skip-ahead
- **Integrated prefilter**: Skip-ahead at start state in DFA loop (Rust `hybrid/search.rs:232`)
- **Per-goroutine cache**: Immutable DFA + mutable DFACache (thread-safe)

### PikeVM Layer (`nfa/pikevm.go`)

- **Dual SlotTable**: Flat per-state capture storage (curr/next, swapped per byte)
  - Zero-allocation capture tracking (Rust `ActiveStates` pattern)
  - Stack-based epsilon closure with `RestoreCapture` frames
  - `searchThread` (12 bytes) vs legacy `thread` (40+ bytes with COW)
- **Integrated prefilter**: Skip-ahead when no active threads (Rust `pikevm.rs:1293`)
- **SearchMode**: Dynamic slot sizing (0=IsMatch, 2=Find, full=Captures)

### BoundedBacktracker (`nfa/backtrack.go`)

- Generation-based visited table (O(1) reset, uint16)
- Visited limit: 256KB for UseNFA (Rust default), 64MB for UseBoundedBacktracker (POSIX)
- Fallback to PikeVM when input exceeds capacity

### Prefilter Layer (`prefilter/`)

- **AVX2 memchr**: SIMD byte search (12x faster than `bytes.IndexByte`)
- **Memmem**: SIMD substring search with Rabin-Karp fingerprinting
- **Teddy**: SIMD multi-pattern matching (1-8 patterns, AVX2/SSSE3)
- **Aho-Corasick**: DFA-based multi-pattern for >8 patterns
- **DigitPrefilter**: SIMD digit detection for `\d+` patterns

## Memory Architecture

### Per-Pattern (compile-time, shared immutable)
- NFA graph (states, transitions)
- DFA configuration (byte classes, start map)
- Prefilter (literal tables, SIMD masks)
- Strategy-specific searchers (reverse DFA, composite, etc.)

### Per-Goroutine (search-time, pooled via sync.Pool)
- `SearchState` holds all mutable search state
- `DFACache`: flat transition table + state map (2MB default)
- `PikeVMState`: dual SlotTable + thread queues + visited set
- `BacktrackerState`: visited array + generation counter

### Memory Budget (Kostya LangArena, 13 patterns, 7MB log)

| Component | v0.12.18 | v0.12.19 |
|-----------|---------|---------|
| Total alloc (FindAll) | 89 MB | **25 MB** |
| RSS | 353 MB | **41 MB** |
| FindAllSubmatch (5 pat, 50K matches) | 554 MB | **26 MB** |

## Thread Safety

```
                    ┌──────────────┐
                    │   Engine     │ ← Immutable after compile
                    │  (shared)    │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              ↓            ↓            ↓
        ┌────────────┐ ┌────────────┐ ┌────────────┐
        │ SearchState│ │ SearchState│ │ SearchState│ ← Per-goroutine
        │(goroutine1)│ │(goroutine2)│ │(goroutine3)│    (sync.Pool)
        └────────────┘ └────────────┘ └────────────┘
```

## Key Design Decisions

1. **Multi-engine**: Strategy selection at compile time, not runtime
2. **Rust reference**: Architecture mirrors Rust regex crate (lazy DFA, PikeVM, prefilters)
3. **Leftmost-first match**: DFA break-at-match matches Rust semantics (verified via cargo run)
4. **Zero-alloc hot paths**: `IsMatch()`, `FindIndices()`, `Count()` — no heap allocation
5. **SIMD first**: AVX2/SSSE3 prefilters for x86_64, pure Go fallback for other archs

## References

- [Rust regex crate](https://github.com/rust-lang/regex) — primary architecture reference
- [RE2](https://github.com/google/re2) — O(n) performance guarantees
- [Hyperscan](https://github.com/intel/hyperscan) — SIMD multi-pattern (Teddy algorithm)
