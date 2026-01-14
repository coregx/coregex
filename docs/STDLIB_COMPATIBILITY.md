# stdlib regexp Compatibility Guide

This document describes the compatibility of coregex with Go's standard library `regexp` package.

## Overview

**coregex** is designed to be a drop-in replacement for Go's stdlib `regexp` package, with the primary goal of providing significantly better performance (3-3000x speedup) while maintaining API compatibility.

## Compatibility Level

| Category | Status |
|----------|--------|
| **API Surface** | Full compatibility |
| **Pattern Syntax** | Full RE2 syntax support |
| **Core Semantics** | Compatible for typical use cases |
| **Edge Case Behavior** | Minor differences documented below |

## API Compatibility

### Fully Compatible Functions

The following functions have identical behavior to stdlib:

```go
// Compilation
Compile(expr string) (*Regex, error)
MustCompile(expr string) *Regex
CompilePOSIX(expr string) (*Regex, error)
MustCompilePOSIX(expr string) *Regex

// Matching
Match(pattern string, b []byte) (bool, error)
MatchString(pattern string, s string) (bool, error)
MatchReader(pattern string, r io.RuneReader) (bool, error)

// Regex methods - matching
(r *Regex) Match(b []byte) bool
(r *Regex) MatchString(s string) bool
(r *Regex) MatchReader(r io.RuneReader) bool

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
(r *Regex) Expand(dst, template []byte, src []byte, match []int) []byte
(r *Regex) ExpandString(dst []byte, template string, src string, match []int) []byte

// Regex methods - splitting
(r *Regex) Split(s string, n int) []string

// Regex methods - introspection
(r *Regex) String() string
(r *Regex) NumSubexp() int
(r *Regex) SubexpNames() []string
(r *Regex) SubexpIndex(name string) int
(r *Regex) LiteralPrefix() (prefix string, complete bool)
(r *Regex) Longest()

// Utility functions
QuoteMeta(s string) string
```

## Known Behavioral Differences

The following edge cases have documented behavioral differences from stdlib. These are marked as "known differences" in the test suite and are unlikely to affect typical use cases.

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

### 2. Negated Unicode Property Classes

**Affected patterns:** `\P{Script}+` (negated Unicode script classes)

**Behavior:** Boundary detection for negated Unicode property classes may produce different match positions.

**Example:**
```go
pattern := `\P{Han}+`
input := "abc中文def"
// May have different match boundaries
```

### 3. Case-Insensitive Overlapping Matches

**Affected patterns:** `(?i)pattern` with overlapping potential matches

**Behavior:** When case-insensitive patterns have overlapping matches, the specific matches returned may differ.

**Example:**
```go
pattern := `(?i)abc`
input := "ABCabcABC"
// Both find all matches, but boundary handling may differ
```

### 4. Combined Perl Flags

**Affected patterns:** `(?im)`, `(?ms)`, etc.

**Behavior:** Some combined flag patterns with case-insensitivity may have minor differences.

**Example:**
```go
pattern := `(?im)^HELLO`
input := "world\nhello\ntest"
// May have different behavior with combined flags
```

### 5. Negated Inverse Character Classes

**Affected patterns:** `[^\S\s]`, `[^\D\d]` (double-negated classes)

**Behavior:** These logically-empty character classes may be handled differently.

**Example:**
```go
pattern := `[^\S\s]`  // Matches nothing in stdlib (empty set)
// coregex may handle this edge case differently
```

### 6. Repeated Capture Groups

**Affected patterns:** `(a)*`, `(a)+`, `(a)?`, `((a|b)*)` (capture groups with repetition)

**Behavior:** When a capture group is repeated, stdlib captures the last matched iteration, while coregex may capture differently.

**Example:**
```go
pattern := `(a)*`
input := "aaa"
// stdlib: FindStringSubmatch returns ["aaa", "a"] (last 'a' captured)
// coregex: may return different capture group content
```

### 7. Empty Pattern Split

**Affected patterns:** Empty pattern `""` with Split function

**Behavior:** Splitting by empty pattern has different semantics between the engines.

**Example:**
```go
pattern := ``
input := "abc"
// stdlib: Split returns ["a", "b", "c"]
// coregex: may return different result
```

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
| v0.10.x | Go 1.21+ | High |
| v1.0.x (planned) | Go 1.21+ | Production-ready |

## See Also

- [OPTIMIZATIONS.md](./OPTIMIZATIONS.md) - Performance optimization details
- [README.md](../README.md) - Project overview
- [CHANGELOG.md](../CHANGELOG.md) - Version history
