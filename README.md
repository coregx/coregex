# coregex - Production-Grade Regex Engine for Go

> **3-3000x+ faster than stdlib through multi-engine architecture and SIMD optimizations**

[![GitHub Release](https://img.shields.io/github/v/release/coregx/coregex?include_prereleases&style=flat-square&logo=github&color=blue)](https://github.com/coregx/coregex/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/coregx/coregex?style=flat-square&logo=go)](https://go.dev/dl/)
[![Go Reference](https://pkg.go.dev/badge/github.com/coregx/coregex.svg)](https://pkg.go.dev/github.com/coregx/coregex)
[![GitHub Actions](https://img.shields.io/github/actions/workflow/status/coregx/coregex/test.yml?branch=main&style=flat-square&logo=github-actions&label=CI)](https://github.com/coregx/coregex/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/coregx/coregex?style=flat-square)](https://goreportcard.com/report/github.com/coregx/coregex)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)
[![GitHub Stars](https://img.shields.io/github/stars/coregx/coregex?style=flat-square&logo=github)](https://github.com/coregx/coregex/stargazers)
[![GitHub Issues](https://img.shields.io/github/issues/coregx/coregex?style=flat-square&logo=github)](https://github.com/coregx/coregex/issues)
[![GitHub discussions](https://img.shields.io/github/discussions/coregx/coregex)](https://github.com/coregx/coregex/discussions)

---

A **production-grade regex engine** for Go with dramatic performance improvements over the standard library. Inspired by Rust's regex crate, coregex uses a multi-engine architecture with SIMD-accelerated prefilters to achieve **3-3000x+ speedup** depending on pattern type (especially suffix patterns like `.*\.txt` and inner literal patterns like `.*keyword.*`).

## Features

⚡ **Performance**
- 🚀 **Up to 3000x+ faster** than Go's `regexp` package (inner literal patterns)
- 🎯 **SIMD-accelerated** search with AVX2/SSSE3 assembly (10-15x faster substring search)
- 📊 **Multi-pattern search** (Teddy SIMD algorithm for 2-8 literals) - **242x faster** for alternations
- 💾 **Zero allocations** in hot paths (`IsMatch`, `FindIndices` - 0 allocs/op)

🏗️ **Architecture**
- 🧠 **Meta-engine** orchestrates strategy selection (DFA/NFA/ReverseAnchored/ReverseInner/ReverseSuffixSet)
- ⚡ **Lazy DFA** with configurable caching (on-demand state construction)
- 🔄 **Pike VM** (Thompson's NFA) for guaranteed O(n×m) performance
- 🔙 **Reverse Search** for `$` anchor and suffix patterns (1000x+ speedup)
- 🎯 **ReverseInner** for `.*keyword.*` patterns with bidirectional DFA (3000x+ speedup)
- 🎯 **ReverseSuffixSet** for `.*\.(txt|log|md)` multi-suffix patterns (34-385x speedup) - **NEW in v0.8.20**
- ⚡ **OnePass DFA** for simple anchored patterns (10x faster captures, 0 allocs)
- ⚡ **BoundedBacktracker** for character class patterns (`\d+`, `\w+`, `(a|b|c)+`) - 2.5x faster than stdlib
- 📌 **Prefilter coordination** (memchr/memmem/teddy)

🎯 **API Design**
- Simple, drop-in replacement for `regexp` package
- Configuration system for performance tuning
- Thread-safe with concurrent compilation support
- Comprehensive error handling

## Installation

```bash
go get github.com/coregx/coregex
```

**Requirements:**
- Go 1.25 or later
- Zero external dependencies (except `golang.org/x/sys` for CPU feature detection)

## Quick Start

### Basic Usage

```go
package main

import (
	"fmt"
	"log"

	"github.com/coregx/coregex"
)

func main() {
	// Compile a regex pattern
	re, err := coregex.Compile(`\b\w+@\w+\.\w+\b`)
	if err != nil {
		log.Fatal(err)
	}

	// Find first match
	text := []byte("Contact us at support@example.com for help")
	if match := re.Find(text); match != nil {
		fmt.Printf("Found email: %s\n", match)
	}

	// Find all matches
	matches := re.FindAll(text, -1)
	for _, m := range matches {
		fmt.Printf("Match: %s\n", m)
	}
}
```

### Advanced Configuration

```go
package main

import (
	"log"

	"github.com/coregx/coregex"
)

func main() {
	// Create custom configuration for performance tuning
	config := coregex.DefaultConfig()
	config.DFAMaxStates = 10000        // Limit DFA cache size
	config.EnablePrefilter = true       // Use SIMD prefilters (default)
	config.UseObjectPools = true        // Zero-allocation mode (default)

	// Compile with custom config
	re, err := coregex.CompileWithConfig(`pattern`, config)
	if err != nil {
		log.Fatal(err)
	}

	// Use regex...
	text := []byte("search this text")
	match := re.Find(text)
	if match != nil {
		log.Printf("Found: %s", match)
	}
}
```

### Performance Example

```go
package main

import (
	"fmt"
	"regexp"
	"time"

	"github.com/coregx/coregex"
)

func benchmarkSearch(pattern string, text []byte) {
	// stdlib regexp
	start := time.Now()
	reStdlib := regexp.MustCompile(pattern)
	for i := 0; i < 10000; i++ {
		reStdlib.Find(text)
	}
	stdlibTime := time.Since(start)

	// coregex
	start = time.Now()
	reGoregex := coregex.MustCompile(pattern)
	for i := 0; i < 10000; i++ {
		reGoregex.Find(text)
	}
	coregexTime := time.Since(start)

	speedup := float64(stdlibTime) / float64(coregexTime)
	fmt.Printf("Speedup: %.1fx faster\n", speedup)
}
```

## Performance Benchmarks

**SIMD Primitives** (vs stdlib):
- `memchr` (single byte): **12.3x faster** (64KB input)
- `memmem` (substring): **14.2x faster** (64KB input, short needle)
- `teddy` (multi-pattern): **8.5x faster** (2-8 patterns)

**Regex Search** (vs `regexp`):

| Pattern Type | Input Size | stdlib | coregex | Speedup |
|--------------|------------|--------|---------|---------|
| Case-sensitive | 1KB | 688 ns | 196 ns | **3.5x faster** |
| Case-sensitive | 32KB | 9,715 ns | 8,367 ns | **1.2x faster** |
| **Case-insensitive** | 1KB | 24,110 ns | **262 ns** | **92x faster** |
| **Case-insensitive** | 32KB | 1,229,521 ns | **4,669 ns** | **263x faster** |
| **`.*\.txt` IsMatch** | 32KB | 1.3 ms | **855 ns** | **1,549x faster** |
| **`.*\.txt` IsMatch** | 1MB | 27 ms | **21 µs** | **1,314x faster** |
| **`.*keyword.*` IsMatch** | 250KB | 12.6 ms | **4 µs** | **3,154x faster** |
| **`.*keyword.*` Find** | 250KB | 15.2 ms | **8 µs** | **1,894x faster** |
| **`.*@example\.com` FindAll** | 6MB | 316 ms | **3.6 ms** | **87x faster** |
| **`(foo\|bar\|baz\|qux)`** | 1KB | 9.7 µs | **40 ns** | **242x faster** |
| **`.*\.(txt\|log\|md)`** | 1KB | 15.5 µs | **454 ns** | **34x faster** |
| **`\d+`** | 1KB | 6.7 µs | **1.5 µs** | **4.5x faster** |
| **`(a\|b\|c)+`** | 1KB | 7.3 µs | **3.0 µs** | **2.5x faster** |
| **Email pattern** | 1KB | 22 µs | **2 µs** | **11x faster** |
| **Email pattern** | 32KB | 640 µs | **15 µs** | **42x faster** |

**Key insights:**
- **Inner literal patterns** (`.*keyword.*`) see massive speedups (2000-3000x+) through ReverseInner optimization (v0.8.0)
- **Suffix patterns** (`.*\.txt`) see 1000x+ speedups through ReverseSuffix optimization
- **Suffix alternations** (`.*\.(txt|log|md)`) now **34-385x faster** via ReverseSuffixSet with Teddy prefilter (v0.8.20) - optimization NOT present in rust-regex!
- **FindAll with suffix patterns** (`.*@example\.com`) now **87x faster** via ReverseSuffix FindAll optimization (v0.8.19)
- **Alternation patterns** (`(foo|bar|baz|qux)`) now 242x faster via Teddy SIMD prefilter (v0.8.18)
- **Email patterns** now 11-42x faster via ReverseInner with `@` inner literal (v0.8.18)
- **Character class patterns** (`\d+`, `(a|b|c)+`) 2.5-4.5x faster via BoundedBacktracker (v0.8.17-18)
- **Case-insensitive** patterns (`(?i)...`) are also excellent (100-263x) - stdlib backtracking is slow, our DFA is fast
- **Simple patterns** see 1-5x improvement depending on literals

See [benchmark/](benchmark/) for detailed comparisons.

## Supported Features

### Current Features

| Feature              | Status | Notes |
|----------------------|--------|-------|
| **SIMD Primitives**  | ✅     | memchr, memchr2/3, memmem, teddy |
| **Literal Extraction** | ✅   | Prefix/suffix/inner literals |
| **Prefilter System** | ✅     | Automatic strategy selection |
| **Meta-Engine**      | ✅     | DFA/NFA/ReverseAnchored orchestration |
| **Lazy DFA**         | ✅     | On-demand state construction |
| **Pike VM (NFA)**    | ✅     | Thompson's construction |
| **Zero-alloc API**   | ✅     | **NEW in v0.8.15** - `IsMatch`, `FindIndices` with 0 allocs |
| **Reverse Search**   | ✅     | ReverseAnchored (v0.4.0), ReverseSuffix (v0.6.0), ReverseInner (v0.8.0), **ReverseSuffixSet (v0.8.20)** |
| **OnePass DFA**      | ✅     | **NEW in v0.7.0** - 10x faster captures, 0 allocs |
| **Unicode support**  | ✅     | Via `regexp/syntax` |
| **Capture groups**   | ✅     | FindSubmatch, FindSubmatchIndex |
| **Replace/Split**    | ✅     | ReplaceAll, ReplaceAllFunc, Split |
| **Named captures**   | ✅     | **NEW in v0.5.0** - SubexpNames() API |
| **Look-around**      | 📅     | Planned |
| **Backreferences**   | ❌     | Incompatible with O(n) guarantee |

### Regex Syntax

coregex uses Go's `regexp/syntax` for pattern parsing, supporting:
- ✅ Character classes `[a-z]`, `\d`, `\w`, `\s`
- ✅ Quantifiers `*`, `+`, `?`, `{n,m}`
- ✅ Anchors `^`, `$`, `\b`, `\B`
- ✅ Groups `(...)` and alternation `|`
- ✅ Unicode categories `\p{L}`, `\P{N}`
- ✅ Case-insensitive matching `(?i)`
- ✅ Non-capturing groups `(?:...)`
- ❌ Backreferences (not supported - O(n) performance guarantee)

## Known Limitations

**What Works:**
- ✅ All standard regex syntax (except backreferences)
- ✅ Unicode support via `regexp/syntax`
- ✅ SIMD acceleration on AMD64 (AVX2/SSSE3)
- ✅ Cross-platform (fallback to pure Go on other architectures)
- ✅ Thread-safe compilation and execution
- ✅ Zero external dependencies
- ✅ Capture groups with FindSubmatch API
- ✅ Named capture groups with SubexpNames() API
- ✅ Replace/Split with $0-$9 template expansion

**Current Limitations:**
- ⚠️ **Experimental API** - May change before v1.0
- ⚠️ No look-around assertions yet (planned)
- ⚠️ SIMD only on AMD64 (ARM NEON planned)

**Performance Notes:**
- 🚀 Best speedup on patterns with literal prefixes/suffixes
- 🚀 Excellent for log parsing, email/URL extraction
- 🚀 Simple literal patterns (`hello`, `foo`) are **~7x faster** than stdlib (v0.8.16)
- 🚀 **Zero-allocation** `IsMatch()` - returns immediately on first match (v0.8.15)
- 🚀 **Zero-allocation** `FindIndices()` - returns `(start, end, found)` tuple (v0.8.15)
- 🚀 Optimized `FindAll`/`ReplaceAll` with lazy allocation (v0.8.16)
- ⚡ Alternation patterns (`(foo|bar|baz)`) **242x faster** via Teddy SIMD prefilter (v0.8.18)
- ⚡ Character class patterns (`\d+`, `\w+`, `(a|b|c)+`) **2.5-4.5x faster** via BoundedBacktracker (v0.8.17-18)
- ⚡ First match slower (compilation cost), repeated matches faster

See [CHANGELOG.md](CHANGELOG.md) for detailed version history.

## Documentation

- **[Getting Started](docs/)** - Usage examples and tutorials
- **[API Reference](https://pkg.go.dev/github.com/coregx/coregex)** - Full API documentation
- **[CHANGELOG.md](CHANGELOG.md)** - Version history
- **[ROADMAP.md](ROADMAP.md)** - Future plans and development timeline
- **[SECURITY.md](SECURITY.md)** - Security policy and ReDoS prevention

## Development

### Building

```bash
# Clone repository
git clone https://github.com/coregx/coregex.git
cd coregex

# Build all packages
go build ./...

# Run tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run benchmarks
go test -bench=. -benchmem ./simd/
go test -bench=. -benchmem ./prefilter/
```

### Testing

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./simd/ -v
go test ./meta/ -v

# Run with coverage
go test -cover ./...

# Run linter (golangci-lint required)
golangci-lint run
```

### Pre-release Check

Before creating a release, run the comprehensive validation script:

```bash
bash scripts/pre-release-check.sh
```

This checks:
- ✅ Go version (1.25+)
- ✅ Code formatting (`gofmt`)
- ✅ `go vet` passes
- ✅ All tests pass (with race detector)
- ✅ Test coverage >70%
- ✅ `golangci-lint` passes
- ✅ Documentation present

---

## Contributing

Contributions are welcome! This is an experimental project and we'd love your help.

**Before contributing:**
1. Read [CONTRIBUTING.md](CONTRIBUTING.md) - Git Flow workflow and guidelines
2. Check [open issues](https://github.com/coregx/coregex/issues)
3. Join [GitHub Discussions](https://github.com/coregx/coregex/discussions)

**Ways to contribute:**
- 🐛 Report bugs and edge cases
- 💡 Suggest features
- 📝 Improve documentation
- 🔧 Submit pull requests
- ⭐ Star the project
- 🧪 Benchmark against stdlib and report results

**Priority areas:**
- Look-around assertions
- ARM NEON SIMD implementation
- More comprehensive benchmarks
- Performance profiling and optimization

---

## Comparison with Other Libraries

| Feature | coregex | stdlib `regexp` | regexp2 |
|---------|---------|----------------|---------|
| **Performance** | 🚀 3-3000x faster | Baseline | Slower (backtracking) |
| **SIMD acceleration** | ✅ AVX2/SSSE3 | ❌ No | ❌ No |
| **Prefilters** | ✅ Automatic | ❌ No | ❌ No |
| **Multi-engine** | ✅ DFA/NFA/PikeVM | ❌ Single | ❌ Backtracking only |
| **O(n) guarantee** | ✅ Yes | ✅ Yes | ❌ No (exponential worst-case) |
| **Backreferences** | ❌ Not supported | ❌ Not supported | ✅ Supported |
| **Capture groups** | ✅ Supported | ✅ Supported | ✅ Supported |
| **Named captures** | ✅ Supported | ✅ Supported | ✅ Supported |
| **Look-around** | 📅 Planned | ❌ Limited | ✅ Supported |
| **API compatibility** | ✅ Drop-in replacement | - | Different |
| **Maintained** | ✅ Active | ✅ Stdlib | ✅ Active |

> **Note on Backreferences**: Both `coregex` and stdlib `regexp` do NOT support backreferences (like `\1`, `\2`) because they are fundamentally incompatible with guaranteed O(n) linear time complexity. Backreferences require backtracking which can lead to exponential worst-case performance (ReDoS vulnerability). If you absolutely need backreferences, use `regexp2`, but be aware of the performance trade-offs.

**When to use coregex:**
- ✅ Performance-critical applications (log parsing, text processing)
- ✅ Patterns with literal prefixes/suffixes
- ✅ Multi-pattern search (email/URL extraction)
- ✅ When you need O(n) performance guarantee

**When to use stdlib `regexp`:**
- ✅ Simple patterns where performance doesn't matter
- ✅ Maximum stability and API compatibility

**When to use `regexp2`:**
- ✅ You need backreferences (not supported by coregex)
- ✅ Complex look-around assertions (v0.4.0 for coregex)
- ⚠️ Accept exponential worst-case performance

---

## Architecture Overview

```
                        +------------------------------------------+
                        |              Meta-Engine                 |
                        | Strategy: DFA/NFA/Reverse/OnePass/Teddy  |
                        +--------------------+---------------------+
                                             |
                        +--------------------+---------------------+
                        |          Prefilter Coordinator           |
                        |  memchr | memmem | teddy | aho-corasick  |
                        +--------------------+---------------------+
                                             |
    +--------+--------+--------+--------+--------+--------+--------+--------+
    |        |        |        |        |        |        |        |        |
    v        v        v        v        v        v        v        v        v
 +------+ +------+ +------+ +------+ +------+ +------+ +------+ +------+
 | Lazy | | Pike | |Revers| |Revers| |Revers| |Revers| |OnePas| |Boundd|
 | DFA  | | VM   | |Anchor| |Suffix| |Inner | |SufSet| | DFA  | |Bcktrk|
 +------+ +------+ +------+ +------+ +------+ +------+ +------+ +------+
    |        |        |        |        |        |        |        |
    +--------+--------+--------+--------+--------+--------+--------+
                                             |
                        +--------------------+---------------------+
                        |           SIMD Primitives                |
                        |            (AVX2/SSSE3)                  |
                        +------------------------------------------+

Strategies:
  - UseDFA:            Prefilter + Lazy DFA (patterns with literals)
  - UseNFA:            Pike VM only (tiny patterns, no literals)
  - UseTeddy:          Teddy prefilter only (exact alternations like foo|bar|baz)
  - UseReverseSuffix:  Backward search for suffix patterns (.*\.txt)
  - UseReverseSuffixSet: Teddy multi-suffix for alternations (.*\.(txt|log|md)) - NEW!
  - UseReverseInner:   Bidirectional search for inner literals (.*keyword.*)
  - UseOnePass:        Zero-alloc captures (simple anchored patterns)
  - UseBounded:        Bit-vector backtracker (char classes like \d+, \w+)
```

**Key components:**
1. **Meta-Engine** - Intelligent strategy selection based on pattern analysis
2. **Prefilter System** - Fast rejection of non-matching candidates
3. **Multi-Engine Execution** - DFA for speed, NFA for correctness
4. **ReverseAnchored** - For `$` anchor patterns (v0.4.0)
5. **ReverseSuffix** - 1000x+ speedup for `.*\.txt` suffix patterns (v0.6.0)
6. **OnePass DFA** - 10x faster captures with 0 allocations (v0.7.0)
7. **ReverseInner** - 3000x+ speedup for `.*keyword.*` patterns (v0.8.0)
8. **BoundedBacktracker** - 2.5x faster for character class patterns (`\d+`, `\w+`)
9. **UseTeddy** - 242x faster for exact alternations (`foo|bar|baz`) with literal engine bypass
10. **SIMD Primitives** - 10-15x faster byte/substring search

See package documentation on [pkg.go.dev](https://pkg.go.dev/github.com/coregx/coregex) for API details.

---

## Related Projects

Part of the [CoreGX](https://github.com/coregx) (Core Go eXtensions) ecosystem:
- More projects coming soon!

**Community:**
- [golang/go#26623](https://github.com/golang/go/issues/26623) - Go stdlib regexp performance discussion (we posted there!)

**Inspired by:**
- [Rust regex crate](https://github.com/rust-lang/regex) - Architecture and design
- [RE2](https://github.com/google/re2) - O(n) performance guarantees
- [Hyperscan](https://github.com/intel/hyperscan) - SIMD multi-pattern matching

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## Acknowledgments

- Rust regex crate team for architectural inspiration
- Russ Cox for Thompson's NFA articles and RE2
- Intel for Hyperscan and Teddy algorithm
- Go team for `regexp/syntax` parser
- All contributors to this project

---

## Support

- 📖 [API Reference](https://pkg.go.dev/github.com/coregx/coregex) - Full documentation
- 🐛 [Issue Tracker](https://github.com/coregx/coregex/issues) - Report bugs
- 💬 [Discussions](https://github.com/coregx/coregex/discussions) - Ask questions

---

**Status**: ⚠️ **Pre-1.0** - API may change before v1.0.0

**Ready for:** Testing, benchmarking, feedback, and experimental use

See [Releases](https://github.com/coregx/coregex/releases) for the latest version and [Discussions](https://github.com/coregx/coregex/discussions/3) for roadmap.

---

*Built with performance and correctness in mind by the coregex community*
