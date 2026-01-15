# stdlib regexp Compatibility Guide

This document describes the compatibility of coregex with Go's standard library `regexp` package.

## Overview

**coregex** is designed to be a drop-in replacement for Go's stdlib `regexp` package, with the primary goal of providing significantly better performance (3-3000x speedup) while maintaining API compatibility.

## Compatibility Level

| Category | Status |
|----------|--------|
| **API Surface** | **100%** (all stdlib methods implemented) |
| **Pattern Syntax** | Full RE2 syntax support |
| **Core Semantics** | Compatible for typical use cases |
| **Edge Case Behavior** | Minor differences documented below |

## API Compatibility

### Implemented Functions

The following functions have identical behavior to stdlib:

```go
// Compilation
Compile(expr string) (*Regex, error)
MustCompile(expr string) *Regex
CompilePOSIX(expr string) (*Regex, error)
MustCompilePOSIX(expr string) *Regex

// Package-level matching
Match(pattern string, b []byte) (bool, error)
MatchString(pattern string, s string) (bool, error)

// Regex methods - matching
(r *Regex) Match(b []byte) bool
(r *Regex) MatchString(s string) bool

// Regex methods - finding
(r *Regex) Find(b []byte) []byte
(r *Regex) FindString(s string) string
(r *Regex) FindIndex(b []byte) (loc []int)
(r *Regex) FindStringIndex(s string) (loc []int)
(r *Regex) FindSubmatch(b []byte) [][]byte
(r *Regex) FindStringSubmatch(s string) []string
(r *Regex) FindSubmatchIndex(b []byte) []int
(r *Regex) FindStringSubmatchIndex(s string) []int

// Regex methods - find all
(r *Regex) FindAll(b []byte, n int) [][]byte
(r *Regex) FindAllString(s string, n int) []string
(r *Regex) FindAllIndex(b []byte, n int) [][]int
(r *Regex) FindAllStringIndex(s string, n int) [][]int
(r *Regex) FindAllSubmatch(b []byte, n int) [][][]byte
(r *Regex) FindAllStringSubmatch(s string, n int) [][]string
(r *Regex) FindAllSubmatchIndex(b []byte, n int) [][]int
(r *Regex) FindAllStringSubmatchIndex(s string, n int) [][]int

// Regex methods - replacement
(r *Regex) ReplaceAll(src, repl []byte) []byte
(r *Regex) ReplaceAllString(src, repl string) string
(r *Regex) ReplaceAllLiteral(src, repl []byte) []byte
(r *Regex) ReplaceAllLiteralString(src, repl string) string
(r *Regex) ReplaceAllFunc(src []byte, repl func([]byte) []byte) []byte
(r *Regex) ReplaceAllStringFunc(src string, repl func(string) string) string

// Regex methods - splitting
(r *Regex) Split(s string, n int) []string

// Regex methods - introspection
(r *Regex) String() string
(r *Regex) NumSubexp() int
(r *Regex) SubexpNames() []string
(r *Regex) SubexpIndex(name string) int
(r *Regex) LiteralPrefix() (prefix string, complete bool)
(r *Regex) Longest()

// Regex methods - template expansion
(r *Regex) Expand(dst, template []byte, src []byte, match []int) []byte
(r *Regex) ExpandString(dst []byte, template string, src string, match []int) []byte

// Reader-based matching
MatchReader(pattern string, r io.RuneReader) (bool, error)
(r *Regex) MatchReader(r io.RuneReader) bool
(r *Regex) FindReaderIndex(r io.RuneReader) []int
(r *Regex) FindReaderSubmatchIndex(r io.RuneReader) []int

// Serialization
(r *Regex) Copy() *Regex              // Deprecated in Go 1.12
(r *Regex) MarshalText() ([]byte, error)
(r *Regex) UnmarshalText(text []byte) error

// Utility functions
QuoteMeta(s string) string
```

> **Note:** As of v0.10.7, coregex implements **100% of the stdlib regexp API**. All methods are fully functional.

## Known Behavioral Differences

The following edge cases have documented behavioral differences from stdlib. These are marked as "known differences" in the test suite and are unlikely to affect typical use cases.

> **Note:** As of v0.10.7, several previously documented differences have been fixed:
> - ~~Negated Unicode Property Classes~~ → Fixed in #91
> - ~~Negated Inverse Character Classes~~ → Fixed in #88
> - ~~Empty Pattern Split~~ → Fixed in #90
> - ~~Case-Insensitive Literal Prefilters~~ → Fixed in #87

### 1. Empty Match Handling

**Affected patterns:** `[a-c]*`, `.*`, `\b`, `^`, etc. (patterns that can match empty strings)

**Behavior:** In some edge cases, the number or position of empty matches may differ.

**Example:**
```go
pattern := `[a-c]*`
input := "abracadabra"
// stdlib: may produce different number of empty matches
// coregex: may differ in empty match positions
```

**Recommendation:** If your application relies on exact empty match behavior, test thoroughly with your specific patterns.

### 2. Repeated Capture Groups

**Affected patterns:** `(a)*`, `(a)+`, `(a)?`, `((a|b)*)` (capture groups with repetition)

**Behavior:** When a capture group is repeated, stdlib captures the last matched iteration, while coregex may capture differently.

**Example:**
```go
pattern := `(a)*`
input := "aaa"
// stdlib: FindStringSubmatch returns ["aaa", "a"] (last 'a' captured)
// coregex: may return different capture group content
```

### 3. Case-Insensitive Edge Cases

**Affected patterns:** `(?i)pattern` with complex overlapping or combined flags

**Behavior:** Some case-insensitive patterns with combined flags (`(?im)`, `(?ms)`) may have minor boundary differences in edge cases.

**Example:**
```go
pattern := `(?im)^HELLO`
input := "world\nhello\ntest"
// May have minor differences in specific edge cases
```

**Note:** Common case-insensitive patterns work correctly. This affects only complex edge cases with overlapping matches.

## Migration Guide

### Step 1: Simple Find-and-Replace

For most use cases, migration is as simple as:

```go
// Before
import "regexp"

re := regexp.MustCompile(`\w+`)

// After
import "github.com/coregx/coregex"

re := coregex.MustCompile(`\w+`)
```

### Step 2: Type Renaming

If you use the `*regexp.Regexp` type explicitly:

```go
// Before
func processPatterns(patterns []*regexp.Regexp) { ... }

// After
func processPatterns(patterns []*coregex.Regex) { ... }
```

### Step 3: Testing

Run your existing test suite to verify compatibility:

```bash
go test ./...
```

### Step 4: Performance Validation

Benchmark critical paths to confirm expected speedups:

```bash
go test -bench=. -benchmem ./...
```

## Fuzz Testing

The coregex package includes comprehensive fuzz tests that compare results against stdlib:

- `FuzzMatchStdlib` - Compares `MatchString` results
- `FuzzFindStdlib` - Compares `FindString` results
- `FuzzFindAllStdlib` - Compares `FindAllString` results
- `FuzzFindSubmatchStdlib` - Compares `FindStringSubmatch` results
- `FuzzReplaceStdlib` - Compares `ReplaceAllString` results
- `FuzzSplitStdlib` - Compares `Split` results
- `FuzzNumSubexpStdlib` - Compares `NumSubexp` results
- `FuzzQuoteMetaStdlib` - Compares `QuoteMeta` results

Run fuzz tests with:

```bash
go test -fuzz=FuzzMatchStdlib -fuzztime=60s
go test -fuzz=FuzzFindAllStdlib -fuzztime=60s
```

## Test Coverage

The test suite includes:

1. **Compilation tests** - Valid and invalid patterns match stdlib behavior
2. **Match tests** - `Match`, `MatchString` parity with stdlib
3. **Find tests** - All `Find*` variants compared with stdlib
4. **Replace tests** - `ReplaceAll*` with `$` expansion compatibility
5. **Split tests** - `Split` function parity
6. **Introspection tests** - `NumSubexp`, `SubexpNames`, `LiteralPrefix`
7. **Concurrent access tests** - Thread-safety verification
8. **Edge case tests** - Unicode, Perl flags, greedy vs non-greedy

## Reporting Issues

If you encounter a compatibility issue not documented above:

1. Check if the pattern involves a known difference category
2. Create a minimal reproduction case
3. Open an issue at: https://github.com/coregx/coregex/issues

Include:
- Pattern
- Input string
- Expected behavior (from stdlib)
- Actual behavior (from coregex)

## Performance vs Compatibility Trade-offs

coregex prioritizes:
1. **Correctness** for typical use cases
2. **Performance** (3-3000x speedup)
3. **API compatibility** with stdlib

Minor edge case differences are accepted when:
- They don't affect typical patterns
- The performance benefit is significant
- The behavior is still reasonable/correct

## Version Compatibility

| coregex Version | Go stdlib Version | Compatibility Level |
|-----------------|-------------------|---------------------|
| v0.10.7+ | Go 1.21+ | High (UTF-8/Unicode fixes) |
| v0.10.x | Go 1.21+ | High |
| v1.0.x (planned) | Go 1.21+ | Production-ready |

## See Also

- [OPTIMIZATIONS.md](./OPTIMIZATIONS.md) - Performance optimization details
- [README.md](../README.md) - Project overview
- [CHANGELOG.md](../CHANGELOG.md) - Version history
