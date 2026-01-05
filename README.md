# coregex

[![GitHub Release](https://img.shields.io/github/v/release/coregx/coregex?style=flat-square&logo=github&color=blue)](https://github.com/coregx/coregex/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/coregx/coregex?style=flat-square&logo=go)](https://go.dev/dl/)
[![Go Reference](https://pkg.go.dev/badge/github.com/coregx/coregex.svg)](https://pkg.go.dev/github.com/coregx/coregex)
[![CI](https://img.shields.io/github/actions/workflow/status/coregx/coregex/test.yml?branch=main&style=flat-square&logo=github-actions&label=CI)](https://github.com/coregx/coregex/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/coregx/coregex?style=flat-square)](https://goreportcard.com/report/github.com/coregx/coregex)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)
[![GitHub Stars](https://img.shields.io/github/stars/coregx/coregex?style=flat-square&logo=github)](https://github.com/coregx/coregex/stargazers)
[![GitHub Issues](https://img.shields.io/github/issues/coregx/coregex?style=flat-square&logo=github)](https://github.com/coregx/coregex/issues)
[![GitHub Discussions](https://img.shields.io/github/discussions/coregx/coregex?style=flat-square&logo=github)](https://github.com/coregx/coregex/discussions)

High-performance regex engine for Go. Drop-in replacement for `regexp` with **3-3000x speedup**.

## Why coregex?

Go's stdlib `regexp` is intentionally simple — single NFA engine, no optimizations. This guarantees O(n) time but leaves performance on the table.

coregex brings Rust regex-crate architecture to Go:
- **Multi-engine**: Lazy DFA, PikeVM, OnePass, BoundedBacktracker
- **SIMD prefilters**: AVX2/SSSE3 for fast candidate rejection
- **Reverse search**: Suffix/inner literal patterns run 1000x+ faster
- **O(n) guarantee**: No backtracking, no ReDoS vulnerabilities

## Installation

```bash
go get github.com/coregx/coregex
```

Requires Go 1.25+. Minimal dependencies (`golang.org/x/sys`, `github.com/coregx/ahocorasick`).

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/coregx/coregex"
)

func main() {
    re := coregex.MustCompile(`\w+@\w+\.\w+`)

    text := []byte("Contact support@example.com for help")

    // Find first match
    fmt.Printf("Found: %s\n", re.Find(text))

    // Check if matches (zero allocation)
    if re.MatchString("test@email.com") {
        fmt.Println("Valid email format")
    }
}
```

## Performance

Cross-language benchmarks on 6MB input ([source](https://github.com/kolkov/regex-bench)):

| Pattern | Go stdlib | coregex | Rust regex | vs stdlib |
|---------|-----------|---------|------------|-----------|
| IP validation | 493 ms | 3.2 ms | 12 ms | **154x** |
| Inner `.*keyword.*` | 231 ms | 1.9 ms | 0.6 ms | **122x** |
| Suffix `.*\.txt` | 233 ms | 1.8 ms | 1.4 ms | **127x** |
| Literal alternation | 473 ms | 4.2 ms | 0.7 ms | **113x** |
| Email validation | 259 ms | 1.7 ms | 1.3 ms | **155x** |
| URL extraction | 266 ms | 2.8 ms | 0.9 ms | **96x** |
| Char class `[\w]+` | 525 ms | 119 ms | 52 ms | **4.4x** |

**Where coregex excels:**
- IP/phone patterns (`\d+\.\d+\.\d+\.\d+`) — SIMD digit prefilter, **2.7x faster than Rust!**
- Suffix patterns (`.*\.log`, `.*\.txt`) — reverse search optimization
- Inner literals (`.*error.*`, `.*@example\.com`) — bidirectional DFA
- Multi-pattern (`foo|bar|baz|...`) — Teddy (≤8) or Aho-Corasick (>8 patterns)

## Features

### Engine Selection

coregex automatically selects the optimal engine:

| Strategy | Pattern Type | Speedup |
|----------|--------------|---------|
| ReverseInner | `.*keyword.*` | 1000-3000x |
| DigitPrefilter | IP patterns `\d+\.\d+\.\d+\.\d+` | 40-2500x |
| ReverseSuffix | `.*\.txt` | 100-400x |
| AhoCorasick | `a\|b\|c\|...\|z` (>8 patterns) | 75-113x |
| CharClassSearcher | `[\w]+`, `\d+` | 20-25x |
| Teddy | `foo\|bar\|baz` (2-8 patterns) | 15-240x |
| LazyDFA | Complex with literals | 10-50x |
| OnePass | Anchored captures | 10x |
| BoundedBacktracker | Small patterns | 2-5x |

### API Compatibility

Drop-in replacement for `regexp.Regexp`:

```go
// stdlib
re := regexp.MustCompile(pattern)

// coregex — same API
re := coregex.MustCompile(pattern)
```

Supported methods:
- `Match`, `MatchString`, `MatchReader`
- `Find`, `FindString`, `FindAll`, `FindAllString`
- `FindIndex`, `FindStringIndex`, `FindAllIndex`
- `FindSubmatch`, `FindStringSubmatch`, `FindAllSubmatch`
- `ReplaceAll`, `ReplaceAllString`, `ReplaceAllFunc`
- `Split`, `SubexpNames`, `NumSubexp`
- `Longest`, `Copy`, `String`

### Zero-Allocation APIs

```go
// Zero allocations — returns bool
matched := re.IsMatch(text)

// Zero allocations — returns (start, end, found)
start, end, found := re.FindIndices(text)
```

### Configuration

```go
config := coregex.DefaultConfig()
config.DFAMaxStates = 10000      // Limit DFA cache
config.EnablePrefilter = true    // SIMD acceleration

re, err := coregex.CompileWithConfig(pattern, config)
```

## Syntax Support

Uses Go's `regexp/syntax` parser:

| Feature | Support |
|---------|---------|
| Character classes | `[a-z]`, `\d`, `\w`, `\s` |
| Quantifiers | `*`, `+`, `?`, `{n,m}` |
| Anchors | `^`, `$`, `\b`, `\B` |
| Groups | `(...)`, `(?:...)`, `(?P<name>...)` |
| Unicode | `\p{L}`, `\P{N}` |
| Flags | `(?i)`, `(?m)`, `(?s)` |
| Backreferences | Not supported (O(n) guarantee) |

## Architecture

```
Pattern → Parse → NFA → Literal Extract → Strategy Select
                                               ↓
                         ┌─────────────────────────────────┐
                         │ Engines (13 strategies):        │
                         │  LazyDFA, PikeVM, OnePass,      │
                         │  BoundedBacktracker,            │
                         │  ReverseInner, ReverseSuffix,   │
                         │  ReverseSuffixSet,              │
                         │  CharClassSearcher, Teddy,      │
                         │  DigitPrefilter, AhoCorasick    │
                         └─────────────────────────────────┘
                                               ↓
Input → Prefilter (SIMD) → Engine → Match Result
```

**SIMD Primitives** (AMD64):
- `memchr` — single byte search (AVX2)
- `memmem` — substring search (SSSE3)
- `teddy` — multi-pattern search (SSSE3)

Pure Go fallback on other architectures.

## Battle-Tested

coregex was [tested in GoAWK](https://github.com/benhoyt/goawk/pull/264). This real-world testing uncovered 15+ edge cases that synthetic benchmarks missed.

**We need more testers!** If you have a project using `regexp`, try coregex and [report issues](https://github.com/coregx/coregex/issues).

## Documentation

- [API Reference](https://pkg.go.dev/github.com/coregx/coregex)
- [CHANGELOG](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [Security Policy](SECURITY.md)

## Comparison

| | coregex | stdlib | regexp2 |
|---|---------|--------|---------|
| Performance | 3-3000x faster | Baseline | Slower |
| SIMD | AVX2/SSSE3 | No | No |
| O(n) guarantee | Yes | Yes | No |
| Backreferences | No | No | Yes |
| API | Drop-in | — | Different |

**Use coregex** for performance-critical code with O(n) guarantee.
**Use stdlib** for simple cases where performance doesn't matter.
**Use regexp2** if you need backreferences (accept exponential worst-case).

## Related

- [golang/go#26623](https://github.com/golang/go/issues/26623) — Go regexp performance discussion
- [golang/go#76818](https://github.com/golang/go/issues/76818) — Upstream path proposal
- [kolkov/regex-bench](https://github.com/kolkov/regex-bench) — Cross-language benchmarks

**Inspired by:**
- [Rust regex](https://github.com/rust-lang/regex) — Architecture
- [RE2](https://github.com/google/re2) — O(n) guarantees
- [Hyperscan](https://github.com/intel/hyperscan) — SIMD algorithms

## License

MIT — see [LICENSE](LICENSE).

---

**Status:** Pre-1.0 (API may change). Ready for testing and feedback.

[Releases](https://github.com/coregx/coregex/releases) · [Issues](https://github.com/coregx/coregex/issues) · [Discussions](https://github.com/coregx/coregex/discussions)
