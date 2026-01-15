# coregex Optimizations that Beat Rust regex

This document describes the 9 key optimizations in coregex that outperform the Rust regex crate.
These algorithms are critical to coregex's competitive advantage and **MUST NOT REGRESS**.

## Summary

| Optimization | File | Pattern Type | vs stdlib | Benchmark |
|--------------|------|--------------|-----------|-----------|
| **AnchoredLiteral** | `meta/anchored_literal.go` | `^prefix.*suffix$` | **32-133x faster** | anchored_literal |
| CharClassSearcher | `nfa/charclass_searcher.go` | `[\w]+`, `[a-z]+` | **23x faster** | char_class |
| CompositeSearcher | `nfa/composite.go` | `[a-zA-Z]+[0-9]+` | **5x faster** | composite |
| BranchDispatch | `nfa/branch_dispatch.go` | `^(\d+\|UUID\|hex32)` | **5-20x faster** | anchored_alt |
| DigitPrefilter | `prefilter/digit.go` | IP addresses, `\d+` | **3324x faster**\* | ip |
| ReverseSuffixSet | `meta/reverse_suffix_set.go` | `.*\.(txt\|log\|md)` | **260x faster** | suffix |
| ReverseInner | `meta/reverse_inner.go` | `.*email@.*` | **909x faster** | email |
| ReverseSuffix | `meta/reverse_suffix.go` | `.*\.txt` | **1124x faster** | suffix |
| AVX2 Slim Teddy | `prefilter/teddy_slim_avx2_amd64.s` | `foo\|bar\|baz` | **242x faster** | literal_alt |

<sub>\* No-match case on large input where SIMD prefilter skips entire input</sub>

**Benchmark source**: regex-bench v0.10.2 on Intel Core i7-1255U

---

## 1. AnchoredLiteral (32-133x faster than stdlib) - NEW in v0.11.0

**File**: `meta/anchored_literal.go`

**Pattern types**: Anchored patterns with prefix literals, wildcards, and suffix literals like `^/.*[\w-]+\.php$`

### Algorithm

AnchoredLiteral uses **O(1) specialized matching** for patterns that match the form `^prefix.*charclass*suffix$`.

```go
type AnchoredLiteralInfo struct {
    Prefix         []byte       // Required prefix literal (e.g., "/")
    Suffix         []byte       // Required suffix literal (e.g., ".php")
    CharClassTable *[256]bool   // Optional char class bridge (e.g., [\w-])
    CharClassMin   int          // Minimum chars for bridge
    WildcardMin    int          // Minimum wildcard length
    MinLength      int          // Total minimum length
}

func MatchAnchoredLiteral(info *AnchoredLiteralInfo, haystack []byte) bool {
    // O(1) length check
    if len(haystack) < info.MinLength {
        return false
    }
    // O(k) prefix check
    if !bytes.HasPrefix(haystack, info.Prefix) {
        return false
    }
    // O(k) suffix check
    if !bytes.HasSuffix(haystack, info.Suffix) {
        return false
    }
    // O(m) charclass bridge verification
    if info.CharClassTable != nil {
        // Verify bridge characters satisfy char class
    }
    return true
}
```

### Why faster than stdlib (and Rust)

1. **No NFA/DFA execution**: Direct byte comparisons vs state machine
2. **Early rejection**: Length check rejects 99%+ of non-matches in O(1)
3. **No backtracking**: Deterministic O(n) worst case
4. **Cache-friendly**: Sequential memory access for prefix/suffix

### Pattern detection

Strategy selection (`meta/strategy.go`) detects anchored literal patterns:

```go
func DetectAnchoredLiteral(re *syntax.Regexp) *AnchoredLiteralInfo {
    // Returns non-nil if pattern matches ^prefix.*charclass*suffix$ form
    // Supports: literal prefix, .*/+ wildcard, charclass+ bridge, literal suffix
}
```

### Benchmark data

```
Pattern: ^/.*[\w-]+\.php$
Input: Various URL paths

Case           stdlib    coregex   Speedup
Short (24B)    241 ns    7.6 ns    32x faster
Medium (45B)   347 ns    7.8 ns    44x faster
Long (78B)     516 ns    7.9 ns    65x faster
No match       590 ns    4.4 ns    133x faster
```

---

## 2. CharClassSearcher (35% faster than Rust)

**File**: `nfa/charclass_searcher.go`

**Pattern types**: Simple character class patterns like `[\w]+`, `[a-z]+`, `\d+`

### Algorithm

CharClassSearcher uses a **256-byte lookup table** for O(1) byte membership testing.
This replaces the NFA state machine overhead with a simple array lookup.

```go
type CharClassSearcher struct {
    membership [256]bool  // membership[b] = true if byte b matches
    minMatch   int        // 1 for +, 0 for *
}

func (s *CharClassSearcher) SearchAt(haystack []byte, at int) (int, int, bool) {
    // Find first matching byte
    for i := at; i < len(haystack); i++ {
        if s.membership[haystack[i]] {
            start := i
            // Greedy scan while bytes match
            for i < len(haystack) && s.membership[haystack[i]] {
                i++
            }
            return start, i, true
        }
    }
    return -1, -1, false
}
```

### Why faster than Rust

1. **No NFA state tracking**: Rust uses a bit-vector for visited states; coregex uses a single array lookup
2. **Single-pass state machine**: `FindAllIndices` uses SEARCHING/MATCHING states, no per-match function calls
3. **CPU branch prediction**: Consistent state transitions optimize for modern CPUs

### Design decision: No SIMD

SIMD optimization was evaluated but found **slower** for char_class patterns because:
- Matches are frequent (30-50% of positions)
- Matches are short (average 4-8 bytes)
- SIMD setup overhead exceeds scalar benefits

For large-scale character class search, Lazy DFA is used instead.

### Benchmark data

```
Pattern: [\w]+
Input: 1MB Wikipedia text

coregex:    33.5 ms
Rust regex: 53.0 ms
Speedup:    35% faster
```

---

## 3. CompositeSearcher (5x faster than stdlib)

**File**: `nfa/composite.go`

**Pattern types**: Concatenated character class patterns like `[a-zA-Z]+[0-9]+`, `\d+\s+\w+`

### Algorithm

CompositeSearcher uses **sequential lookup tables** for patterns that concatenate multiple character classes.
Each part has its own [256]bool membership table for O(1) byte classification.

```go
type CompositeSearcher struct {
    parts []*charClassPart
}

type charClassPart struct {
    membership [256]bool  // O(1) character classification
    minMatch   int        // 1 for +, 0 for *
    maxMatch   int        // 0 = unlimited
}

func (c *CompositeSearcher) matchAt(haystack []byte, pos int) (int, bool) {
    i := pos
    for _, part := range c.parts {
        // Greedy match: consume as many characters as possible
        matchLen := 0
        for i+matchLen < len(haystack) && part.membership[haystack[i+matchLen]] {
            matchLen++
        }
        if matchLen < part.minMatch {
            return -1, false
        }
        i += matchLen
    }
    return i, true
}
```

### Why faster than stdlib

1. **No NFA state machine**: Direct table lookup instead of state transitions
2. **Greedy single-pass**: No backtracking between parts
3. **Cache-friendly**: Sequential memory access through lookup tables

### Benchmark data

```
Pattern: [a-zA-Z]+[0-9]+
Input: "The quick brown fox123 jumps over456 lazy dog789"

stdlib:  808 ns/op
coregex: 170 ns/op
Speedup: 4.75x faster
```

---

## 4. BranchDispatch (5-20x faster than stdlib)

**File**: `nfa/branch_dispatch.go`

**Pattern types**: Anchored alternations with distinct first bytes like `^(\d+|UUID|hex32)`

### Algorithm

BranchDispatch uses **O(1) first-byte dispatch** for anchored alternation patterns.
Instead of trying all branches sequentially, it builds a dispatch table that maps
each possible first byte to the correct branch.

```go
type BranchDispatcher struct {
    dispatch [256]int8  // first_byte → branch_index (-1 = no match)
    branches []*syntax.Regexp
}

// For pattern ^(\d+|UUID|hex32):
// dispatch['0'-'9'] = 0  (digit branch)
// dispatch['U'] = 1       (UUID branch)
// dispatch['h'] = 2       (hex32 branch)
// dispatch[others] = -1   (no match)

func (d *BranchDispatcher) IsMatch(haystack []byte) bool {
    if len(haystack) == 0 {
        return d.canMatchEmpty
    }
    branchIdx := d.dispatch[haystack[0]]  // O(1) lookup
    if branchIdx < 0 {
        return false  // Early rejection
    }
    // Only try the selected branch
    return d.matchBranch(branchIdx, haystack)
}
```

### Why faster than stdlib

1. **O(1) branch selection**: Single array lookup vs O(branches) iteration
2. **Early rejection**: Non-matching first bytes rejected immediately
3. **Specialized matchers**: Each branch has optimized literal/charclass matcher

### Benchmark data

```
Pattern: ^(\d+|UUID|hex32)
Input: Various test cases

Case        stdlib    coregex   Speedup
Digits      225 ns    42 ns     5.4x faster
UUID        173 ns    39 ns     4.4x faster
No match    101 ns    4.9 ns    20.7x faster
```

---

## 5. DigitPrefilter (3324x faster on no-match)

**File**: `prefilter/digit.go`

**Pattern types**: Digit-lead patterns like IP addresses, phone numbers, numeric validators

### Algorithm

DigitPrefilter uses **SIMD-accelerated digit scanning** to skip non-digit regions.
This converts O(n*m) matching to O(k*m) where k = number of digit positions.

```go
func (p *DigitPrefilter) Find(haystack []byte, start int) int {
    return simd.MemchrDigitAt(haystack, start)  // AVX2 optimized
}
```

The meta-engine orchestrates:
1. DigitPrefilter finds next digit position
2. Lazy DFA verifies the match at that position
3. Skip to next digit position on mismatch

### Why faster than Rust

1. **Specialized prefilter**: Rust has no digit-specific prefilter; it falls back to Core strategy
2. **SIMD acceleration**: AVX2 processes 32 bytes/iteration vs byte-by-byte
3. **Skip-ahead**: Non-digit regions (often >80% of text) are skipped entirely

### Pattern detection

Strategy selection (`meta/strategy.go`) detects digit-lead patterns:

```go
func isDigitLeadPattern(re *syntax.Regexp) bool {
    // Returns true if ALL branches must start with digit [0-9]
    // Examples: \d+, [0-9]+, 25[0-5]|2[0-4][0-9]|...
}
```

### Benchmark data

```
Pattern: (?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.(?:25[0-5]|...) (IP address)
Input: 1MB access logs with ~0.5% IP addresses

coregex:    3.8 ms
Rust regex: 12.3 ms
Speedup:    3.3x faster
```

---

## 6. ReverseSuffixSet (27% faster than Rust) - UNIQUE TO COREGEX

**File**: `meta/reverse_suffix_set.go`

**Pattern types**: Multi-suffix alternations like `.*\.(txt|log|md)`

### Algorithm

ReverseSuffixSet combines **Teddy multi-pattern prefilter** with **reverse DFA verification**.
This is an optimization **NOT present in Rust regex** (they fall back to Core strategy).

```
Algorithm:
1. Build Teddy prefilter from all suffix literals [".txt", ".log", ".md"]
2. Teddy finds any suffix literal in haystack
3. Reverse DFA verifies prefix pattern from suffix position
4. For .* prefix patterns, match starts at position 0 (skip reverse scan)
```

### Why faster than Rust

1. **Rust has no ReverseSuffixSet**: When LCS (Longest Common Suffix) is empty, Rust falls back to UseBoth
2. **Teddy multi-pattern**: SIMD searches for multiple suffixes simultaneously
3. **Reverse DFA optimization**: For `.*` prefix, no reverse scan needed

### When used

Strategy selection detects multi-suffix patterns:

```go
func shouldUseReverseSuffixSet(prefixLiterals, suffixLiterals *literal.Seq) bool {
    // Returns true if:
    // - 2-32 suffix literals available
    // - Each suffix >= 2 bytes
    // - Not an exact alternation (those use UseTeddy)
}
```

### Benchmark data

```
Pattern: .*\.(txt|log|md|json|yaml|xml|csv|html)
Input: 1MB file listing

coregex:    1.0 ms
Rust regex: 1.3 ms
Speedup:    27% faster
```

---

## 7. ReverseInner (16% faster than Rust)

**File**: `meta/reverse_inner.go`

**Pattern types**: Inner literal patterns like `.*email@.*`, `ERROR.*connection.*timeout`

### Algorithm

ReverseInner uses **bidirectional DFA search** from the inner literal position:

```
Algorithm for pattern `prefix.*inner.*suffix`:
1. Prefilter finds "inner" literal candidates
2. For each candidate at position P:
   a. Reverse DFA scans backward from P to find match START
   b. Forward DFA scans forward from P+len(inner) to find match END
3. Early return on first confirmed match (leftmost-longest)
```

### Key optimization: AST splitting

The critical optimization (from Rust regex):
- Build reverse NFA from **PREFIX AST only** (not full pattern)
- Build forward NFA from **SUFFIX AST only** (not full pattern)

This enables true bidirectional search with minimal DFA states.

### Why faster than Rust

1. **Universal match optimization**: For `.*inner.*` patterns, skip DFA scans entirely
2. **Early return**: First confirmed match is leftmost by construction
3. **Quadratic detection**: Falls back to PikeVM when O(n^2) behavior detected

### Benchmark data

```
Pattern: .*user@example\.com.*
Input: 1MB email logs

coregex:    1.2 ms
Rust regex: 1.4 ms
Speedup:    16% faster
```

---

## 8. NFA for Small Patterns (2x faster than Rust)

**File**: `meta/strategy.go` (strategy selection)

**Pattern types**: Small anchored patterns like `^pattern$`, tiny NFAs (<20 states)

### Algorithm

For tiny patterns, coregex uses **PikeVM directly** without DFA overhead:

```go
func SelectStrategy(n *nfa.NFA, ...) Strategy {
    if nfaSize < 20 && hasGoodLiterals {
        return UseNFA  // Prefilter + PikeVM
    }
    if nfaSize < 20 {
        return UseNFA  // Pure PikeVM
    }
    // ... larger patterns use DFA
}
```

### Why faster than Rust

1. **No lazy DFA overhead**: DFA cache lookup and state construction have fixed costs
2. **Prefilter integration**: PikeVM uses prefilter for skip-ahead
3. **Optimal threshold**: 20 states is the crossover point where DFA benefits exceed overhead

### When Rust uses DFA unnecessarily

Rust's strategy selection can be too aggressive with DFA for small patterns.
coregex's threshold-based selection avoids this overhead.

### Benchmark data

```
Pattern: ^Hello, World!$
Input: 1MB text with pattern at various positions

coregex:    0.02 ms
Rust regex: 0.04 ms
Speedup:    2x faster
```

---

## 9. AVX2 Slim Teddy with Shift Algorithm (2x faster than SSSE3 in direct benchmarks)

**File**: `prefilter/teddy_slim_avx2_amd64.s`

**Pattern types**: Multi-pattern literal alternations like `foo|bar|baz` (2-32 patterns)

**Status**: Available for direct use. NOT enabled in integrated prefilter due to
regression in high false-positive workloads (see issue #74).

### Algorithm

AVX2 Slim Teddy processes 32 bytes per iteration using the **shift algorithm** from Rust aho-corasick.

Key insight: For 2-byte fingerprint matching, we need `mask0(byte_P) & mask1(byte_{P+1})` at each position P.

**Naive approach** (caused 6x regression on AMD EPYC):
```asm
VMOVDQU (SI), Y3       // Load 32 bytes at position 0
VMOVDQU 1(SI), Y10     // Load 32 bytes at position 1 (OVERLAPPING!)
// Two loads cross 32-byte cache line boundary = AMD penalty
```

**Shift algorithm** (from Rust):
```asm
// Single load per iteration
VMOVDQU (SI), Y3                      // Load 32 bytes ONCE

// Compute res0, res1 via nibble lookups
VPSHUFB Y4, Y0, Y6                    // res0 = mask0 lookup
VPSHUFB Y4, Y8, Y11                   // res1 = mask1 lookup

// Shift res0 right by 1, bringing in prev0[31]
VPERM2I128 $0x21, Y6, Y10, Y13        // tmp = [prev0.hi | res0.lo]
VPALIGNR $15, Y13, Y6, Y15            // res0_shifted = byte-align

// Combine and save prev0
VPAND Y11, Y15, Y7                    // result = res0_shifted & res1
VMOVDQA Y6, Y10                       // prev0 = res0 for next iteration
```

### Why faster than naive AVX2

1. **Single load vs two loads**: Halves memory bandwidth
2. **No cache line crossing**: AMD Zen 3 penalizes 32-byte boundary crossings
3. **Register-based prev0**: No additional memory access between iterations

### Cross-lane shift implementation

AVX2's `VPALIGNR` operates on 128-bit lanes independently. Cross-lane shift requires:

```
VPERM2I128 $0x21, prev0, self, tmp
  → tmp.lo = prev0.hi (bytes 16-31 of previous result)
  → tmp.hi = self.lo  (bytes 0-15 of current result)

VPALIGNR $15, tmp, self, result
  → For each 128-bit lane: shift right 15 bytes
  → result[0] = prev0[31] (the cross-lane byte!)
  → result[1..31] = self[0..30]
```

### Benchmark data

```
Pattern: error|warning|critical|fatal|debug|info|trace|... (15 patterns)
Input: 64KB log file

SSSE3 (16 bytes/iter):  12,252 ns/op = 5,348 MB/s
AVX2 naive (2 loads):   ~36,000 ns/op = 1,800 MB/s (6x slower!)
AVX2 shift (1 load):    4,174 ns/op = 15,699 MB/s (2.93x faster than SSSE3)
```

### AMD EPYC regression root cause

AMD EPYC 7763 (Zen 3) characteristics:
- 256-bit AVX2 operations split into two 128-bit µops
- 32-byte aligned loads: 1 cycle latency
- Unaligned 32-byte loads crossing cache line: 2+ cycles penalty
- Two overlapping loads at offset 0 and 1 = worst case

Intel processors (tested on i7-1255U) also benefit from shift algorithm but the penalty was less severe.

---

## Maintaining Performance

### DO NOT REGRESS Policy

Each optimization file has a header comment:

```go
// DO NOT REGRESS: This optimization beats Rust regex by X%.
// See docs/OPTIMIZATIONS.md for details.
```

### Benchmark verification

Before any changes to these files:

```bash
# Save baseline
bash scripts/bench.sh baseline

# Make changes

# Compare
bash scripts/bench.sh --compare baseline current
```

**Rule**: Regression >5% = BLOCKER

### Key metrics to monitor

| Optimization | Benchmark | Target |
|--------------|-----------|--------|
| AnchoredLiteral | `BenchmarkAnchoredLiteral` | <10 ns/op |
| CharClassSearcher | `BenchmarkCharClass` | <35 ms/MB |
| DigitPrefilter | `BenchmarkIP` | <4 ms/MB |
| ReverseSuffixSet | `BenchmarkSuffix` | <1.1 ms/MB |
| ReverseInner | `BenchmarkEmail` | <1.3 ms/MB |
| NFA small | `BenchmarkAnchored` | <0.025 ms/op |
| AVX2 Slim Teddy | `BenchmarkSlimTeddyDirect/AVX2` | >15 GB/s |

---

## References

- **Rust regex crate**: Architecture inspiration for multi-engine design
- **Rust aho-corasick**: Teddy shift algorithm (`packed/teddy/generic.rs`, `packed/vector.rs`)
- **RE2**: O(n) performance guarantees
- **Hyperscan**: Teddy algorithm for SIMD multi-pattern matching
- **regex-bench**: Cross-language regex benchmark suite

---

*Document version: 1.2.0*
*Last updated: 2026-01-15*
*Benchmark data: regex-bench v0.11.0*
